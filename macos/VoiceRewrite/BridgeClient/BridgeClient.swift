import Foundation

/// One child process, one serialized writer, one serialized state machine.
/// Transport callbacks are always delivered on the main queue. stdout is protocol-only;
/// stderr is drained separately and intentionally discarded to avoid leaking private text.
final class BridgeClient {
    typealias Completion = ReplyRegistry.Completion
    private let executableURL: URL
    private let onExit: (BridgeFailure) -> Void
    private let state = DispatchQueue(label: "com.xujinheng.VoiceRewrite.bridge.state")
    private let writer = DispatchQueue(label: "com.xujinheng.VoiceRewrite.bridge.writer")
    private let replies = ReplyRegistry()
    private var process: Process?
    private var input: Pipe?
    private var epoch: UInt64 = 0

    init(executableURL: URL, onExit: @escaping (BridgeFailure) -> Void = { _ in }) {
        self.executableURL = executableURL
        self.onExit = onExit
    }

    var processID: Int32? { state.sync { process?.processIdentifier } }

    func start() throws {
        try state.sync {
            guard process == nil else { throw BridgeFailure.alreadyRunning }
            guard executableURL.isFileURL,
                  FileManager.default.isExecutableFile(atPath: executableURL.path) else {
                throw BridgeFailure.helperMissing
            }
            let child = Process()
            child.executableURL = executableURL
            child.arguments = []
            let stdin = Pipe()
            let stdout = Pipe()
            let stderr = Pipe()
            child.standardInput = stdin
            child.standardOutput = stdout
            child.standardError = stderr
            do { try child.run() }
            catch { throw BridgeFailure.launchFailed(error.localizedDescription) }

            epoch &+= 1
            let session = epoch
            input = stdin
            process = child
            DispatchQueue.global(qos: .userInitiated).async { [self] in
                readReplies(from: stdout.fileHandleForReading, process: child, epoch: session)
            }
            DispatchQueue.global(qos: .utility).async {
                let handle = stderr.fileHandleForReading
                while !handle.availableData.isEmpty { /* discard */ }
                try? handle.close()
            }
        }
    }

    /// Payload construction (including PCM Base64/JSON) happens on the writer,
    /// never on AppKit's main thread or while holding the bridge state queue.
    @discardableResult
    func send(operation: String, fields: [String: JSONValue] = [:],
              id: String = UUID().uuidString,
              completion: @escaping Completion) -> String {
        enqueue(operation: operation, fields: fields, pcm: nil, id: id, completion: completion)
    }

    /// Pass raw PCM across the main-thread boundary; the large Base64 allocation
    /// is owned by the background writer rather than the recording UI.
    @discardableResult
    func sendPCM(operation: String, pcm: Data, fields: [String: JSONValue] = [:],
                 id: String = UUID().uuidString,
                 completion: @escaping Completion) -> String {
        enqueue(operation: operation, fields: fields, pcm: pcm, id: id, completion: completion)
    }

    private func enqueue(operation: String, fields: [String: JSONValue], pcm: Data?,
                         id: String, completion: @escaping Completion) -> String {
        let destination: (Pipe, UInt64)? = state.sync {
            guard let stdin = input, process != nil else {
                DispatchQueue.main.async { completion(.failure(.notRunning)) }
                return nil
            }
            do {
                try replies.register(id: id, completion: completion)
            } catch {
                DispatchQueue.main.async {
                    completion(.failure((error as? BridgeFailure) ?? .invalidRequest))
                }
                return nil
            }
            return (stdin, epoch)
        }
        guard let (stdin, session) = destination else { return id }
        writer.async { [weak self] in
            do {
                var payload = fields
                if let pcm { payload["pcm_base64"] = .string(pcm.base64EncodedString()) }
                let line = try BridgeRequest(id: id, operation: operation, fields: payload).line()
                // Cancellation/reconnect may have happened while encoding.
                guard self?.state.sync(execute: {
                    guard let self else { return false }
                    return self.epoch == session && self.replies.contains(id: id)
                }) == true else { return }
                try stdin.fileHandleForWriting.write(contentsOf: line)
            } catch {
                let failure = (error as? BridgeFailure)
                self?.state.async { [weak self] in
                    guard let self, self.epoch == session else { return }
                    if let failure {
                        self.replies.fail(id: id, reason: failure)
                    } else {
                        self.failSession(session, reason: .writeFailed)
                    }
                }
            }
        }
        return id
    }

    /// Retires the original ID immediately, so a late success can never reach the UI.
    /// The cancellation frame is sent best-effort; its reply has its own request ID.
    @discardableResult
    func cancel(id: String) -> Bool {
        let removed = state.sync { replies.cancel(id: id) }
        if removed {
            send(operation: "cancel", fields: ["target_id": .string(id)]) { _ in }
        }
        return removed
    }

    /// Called at app termination and before reconnecting. No outstanding callback survives.
    func stop() {
        let previous: (Process?, Pipe?) = state.sync {
            epoch &+= 1
            let previous = (process, input)
            process = nil
            input = nil
            replies.failAll(.cancelled)
            return previous
        }
        if let child = previous.0, child.isRunning { child.terminate() }
        try? previous.1?.fileHandleForWriting.close()
    }

    private func readReplies(from handle: FileHandle, process child: Process, epoch session: UInt64) {
        // Framing and JSON decoding can be expensive for Meeting snapshots.
        // Do them on this reader thread, not under the queue used by UI send().
        var framer = JSONLFramer()
        var readerError: BridgeFailure?
        while true {
            let chunk = handle.availableData
            if chunk.isEmpty { break }
            do {
                for line in try framer.append(chunk) {
                    let reply = try JSONDecoder().decode(BridgeReply.self, from: line)
                    try reply.validate()
                    state.async { [weak self] in
                        guard let self, self.epoch == session else { return }
                        self.replies.resolve(reply)
                    }
                }
            } catch {
                readerError = (error as? BridgeFailure) ?? .invalidReply
                if child.isRunning { child.terminate() }
                break
            }
        }
        try? handle.close()
        // A process can close stdout without exiting. Waiting for its termination
        // here would strand every outstanding callback indefinitely. Retire
        // requests immediately; failSession terminates a surviving child.
        state.sync {
            guard session == epoch else { return }
            let failure = readerError ??
                (framer.hasPartialFrame || child.isRunning
                    ? .truncatedReply : .processExited(child.terminationStatus))
            failSession(session, reason: failure)
        }
    }

    /// Must run on `state`.
    private func failSession(_ session: UInt64, reason: BridgeFailure) {
        guard session == epoch else { return }
        epoch &+= 1
        let child = process
        let oldInput = input
        process = nil
        input = nil
        replies.failAll(reason)
        DispatchQueue.global(qos: .utility).async {
            if let child, child.isRunning { child.terminate() }
            try? oldInput?.fileHandleForWriting.close()
        }
        DispatchQueue.main.async { [onExit] in onExit(reason) }
    }
}
