import Foundation

public struct ProbeRequest: Equatable, Sendable {
    public var name: String
    public var operation: String
    public init(name: String, operation: String) {
        self.name = name
        self.operation = operation
    }
}

/// Which status line a helper operation writes.
/// A spoken selection writes both lines. It is still not a settings probe.
public enum ProbeField: Equatable, Sendable {
    case asr
    case llm
    case both
}

public func probeField(_ operation: HelperOperation) -> ProbeField {
    switch operation {
    case .transcribe:
        return .asr
    case .selectedTask:
        return .llm
    case .spokenSelectedTask:
        return .both
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
