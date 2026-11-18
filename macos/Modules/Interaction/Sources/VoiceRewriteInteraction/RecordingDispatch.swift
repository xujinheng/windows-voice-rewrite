import Foundation

/// A microphone stop that fails after the token expired leaves processing set.
public struct RecordingStopFailure: Equatable, Sendable {
    public var apply: Bool
    public var stopProcessing: Bool

    public init(apply: Bool = false, stopProcessing: Bool = false) {
        self.apply = apply
        self.stopProcessing = stopProcessing
    }
}

public func recordingStopFailure(tokenValid: Bool) -> RecordingStopFailure {
    if !tokenValid {
        return RecordingStopFailure()
    }
    return RecordingStopFailure(apply: true, stopProcessing: true)
}
