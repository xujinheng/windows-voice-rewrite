import Foundation

/// One child process, one serialized writer, one serialized state machine.
/// Transport callbacks are always delivered on the main queue. stdout is protocol-only;
/// stderr is drained separately and intentionally discarded to avoid leaking private text.
final class BridgeClient {
    typealias Completion = ReplyRegistry.Completion
    private let executableURL: URL
    private let onExit: (BridgeFailure) -> Void
    private let writeTimeout: TimeInterval
    private let state = DispatchQueue(label: "com.xujinheng.VoiceRewrite.bridge.state")
    private let writer = DispatchQueue(label: "com.xujinheng.VoiceRewrite.bridge.writer")
    private let replies = ReplyRegistry()
    private var process: Process?
    private var launching = false
    private var writingID: String?
    private var input: Pipe?
    private var epoch: UInt64 = 0

    init(executableURL: URL, writeTimeout: TimeInterval = 10,
         onExit: @escaping (BridgeFailure) -> Void = { _ in }) {
        self.executableURL = executableURL
        self.writeTimeout = writeTimeout
        self.onExit = onExit
    }

    var processID: Int32? { state.sync { process?.processIdentifier } }

    func start() throws {
        guard executableURL.isFileURL,
              FileManager.default.isExecutableFile(atPath: executableURL.path) else {
            throw BridgeFailure.helperMissing
        }
        // Reserve a generation, then launch outside the state lock. Process.run()
        // can block on macOS while dyld/Gatekeeper resolves a cold executable;
        // no UI send/cancel/stop should ever wait for that lock.
        let session: UInt64 = try state.sync {
            guard process == nil && !launching else { throw BridgeFailure.alreadyRunning }
            launching = true
            epoch &+= 1
            return epoch
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
        do {
            try child.run()
            // The parent owns only stdin's writer and stdout/stderr readers.
            // Retaining unused ends makes pipe EOF/EPIPE detection unreliable
            // and can strand the reader or a blocked large-audio writer.
            try? stdin.fileHandleForReading.close()
            try? stdout.fileHandleForWriting.close()
            try? stderr.fileHandleForWriting.close()
        } catch {
            state.sync {
                if epoch == session { launching = false }
            }
            throw BridgeFailure.launchFailed(error.localizedDescription)
        }
        let accepted: Bool = state.sync {
            guard epoch == session && launching else { return false }
            launching = false
            input = stdin
            process = child
            return true
        }
        guard accepted else {
            DispatchQueue.global(qos: .utility).async {
                if child.isRunning { child.terminate() }
            }
            throw BridgeFailure.cancelled
        }
        DispatchQueue.global(qos: .userInitiated).async { [self] in
            readReplies(from: stdout.fileHandleForReading, process: child, epoch: session)
        }
        DispatchQueue.global(qos: .utility).async {
            let handle = stderr.fileHandleForReading
            while !handle.availableData.isEmpty { /* discard */ }
            try? handle.close()
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
                    guard let self, self.epoch == session,
                          self.replies.contains(id: id) else { return false }
                    self.writingID = id
                    return true
                }) == true else { return }
                // The Helper may stop consuming stdin without closing it.
                // An indefinitely blocked pipe writer would prevent all later
                // requests/cancellations. Recycle the session after a finite
                // *transport* stall; model inference is not on this deadline.
                DispatchQueue.global(qos: .utility).asyncAfter(
                    deadline: .now() + (self?.writeTimeout ?? 10)
                ) { [weak self] in
                    self?.state.async { [weak self] in
                        guard let self, self.epoch == session,
                              self.writingID == id else { return }
                        self.failSession(session, reason: .writeFailed)
                    }
                }
                try stdin.fileHandleForWriting.write(contentsOf: line)
                self?.state.async { [weak self] in
                    guard let self, self.epoch == session,
                          self.writingID == id else { return }
                    self.writingID = nil
                }
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
            launching = false
            writingID = nil
            let previous = (process, input)
            process = nil
            input = nil
            replies.failAll(.cancelled)
            return previous
        }
        DispatchQueue.global(qos: .utility).async {
            if let child = previous.0, child.isRunning { child.terminate() }
            try? previous.1?.fileHandleForWriting.close()
        }
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
        writingID = nil
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
