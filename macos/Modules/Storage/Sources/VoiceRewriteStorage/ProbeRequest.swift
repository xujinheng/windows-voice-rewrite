import Foundation

public struct ProbeRequest: Equatable, Sendable {
    public var name: String
    public var operation: String
    public init(name: String, operation: String) {
        self.name = name
        self.operation = operation
    }
}

/// The service probe name and bridge operation. A spoken selection task is not a settings probe.
public func probeRequest(_ operation: HelperOperation) -> ProbeRequest? {
    switch operation {
    case .transcribe:
        return ProbeRequest(name: "ASR", operation: "probe_asr")
    case .selectedTask:
        return ProbeRequest(name: "LLM", operation: "probe_llm")
    case .spokenSelectedTask:
        return nil
    }
}
