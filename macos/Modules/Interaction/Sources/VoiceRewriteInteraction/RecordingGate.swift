import Foundation

public enum RecordingToggle: Equatable, Sendable {
    case ignore
    case stop
    case start
}

public func recordingToggle(paused: Bool, recording: Bool, starting: Bool, processing: Bool) -> RecordingToggle {
    if paused && !recording {
        return .ignore
    }
    if recording {
        return .stop
    }
    if starting || processing {
        return .ignore
    }
    return .start
}

public enum RewriteToggle: Equatable, Sendable {
    case ignore
    case stop
    case missingTarget
    case start(Int32)
}

public func rewriteToggle(paused: Bool, recording: Bool, voiceSelection: Bool, starting: Bool, processing: Bool, targetPID: Int32?) -> RewriteToggle {
    if paused && !recording {
        return .ignore
    }
    if recording {
        return voiceSelection ? .stop : .ignore
    }
    if starting || processing {
        return .ignore
    }
    guard let targetPID, targetPID > 0 else {
        return .missingTarget
    }
    return .start(targetPID)
}

public enum SelectionTaskStart: Equatable, Sendable {
    case paused
    case missingTarget
    case missingInstruction
    case start(Int32)
}

public func selectionTaskStart(paused: Bool, targetPID: Int32?, instruction: String) -> SelectionTaskStart {
    if paused {
        return .paused
    }
    guard let targetPID, targetPID > 0 else {
        return .missingTarget
    }
    if instruction.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
        return .missingInstruction
    }
    return .start(targetPID)
}
