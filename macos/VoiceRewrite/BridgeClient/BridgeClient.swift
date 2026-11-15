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
    private var framer = JSONLFramer()

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
            framer = JSONLFramer()
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

    /// IDs are caller chosen or UUIDs; an outstanding duplicate is rejected without
    /// replacing the original completion. Calling from any thread is supported.
    @discardableResult
    func send(operation: String, fields: [String: JSONValue] = [:],
              id: String = UUID().uuidString,
              completion: @escaping Completion) -> String {
        state.sync {
            guard let stdin = input, process != nil else {
                DispatchQueue.main.async { completion(.failure(.notRunning)) }
                return
            }
            let line: Data
            do {
                line = try BridgeRequest(id: id, operation: operation, fields: fields).line()
                try replies.register(id: id, completion: completion)
            } catch {
                let failure = (error as? BridgeFailure) ?? .invalidRequest
                DispatchQueue.main.async { completion(.failure(failure)) }
                return
            }
            let session = epoch
            writer.async { [weak self] in
                do { try stdin.fileHandleForWriting.write(contentsOf: line) }
                catch {
                    self?.state.async { [weak self] in
                        self?.failSession(session, reason: .writeFailed)
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
        state.sync {
            epoch &+= 1
            let child = process
            let oldInput = input
            process = nil
            input = nil
            replies.failAll(.cancelled)
            if let child, child.isRunning { child.terminate() }
            try? oldInput?.fileHandleForWriting.close()
        }
    }

    private func readReplies(from handle: FileHandle, process child: Process, epoch session: UInt64) {
        var readerError: BridgeFailure?
        while true {
                let chunk = handle.availableData
                if chunk.isEmpty { break }
                let failure: BridgeFailure? = state.sync {
                    guard session == epoch else { return .cancelled }
                    do {
                        for line in try framer.append(chunk) {
                            let reply = try JSONDecoder().decode(BridgeReply.self, from: line)
                            try reply.validate()
                            replies.resolve(reply) // unknown/retired IDs are ignored
                        }
                        return nil
                    } catch {
                        return (error as? BridgeFailure) ?? .invalidReply
                    }
                }
                if let failure {
                    readerError = failure
                    if child.isRunning { child.terminate() }
                    break
                }
            }
        try? handle.close()
        child.waitUntilExit()
        state.sync {
            guard session == epoch else { return }
            let failure = readerError ?? (framer.hasPartialFrame ? .truncatedReply : .processExited(child.terminationStatus))
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
        if let child, child.isRunning { child.terminate() }
        try? oldInput?.fileHandleForWriting.close()
        DispatchQueue.main.async { [onExit] in onExit(reason) }
    }
}
