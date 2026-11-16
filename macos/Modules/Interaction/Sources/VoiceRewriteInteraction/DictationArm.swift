import Foundation

public struct DictationArmResult: Equatable, Sendable {
    public var interactionStatus: String?
    public var keepInsert: Bool
    public var startMicrophone: Bool
}

/// A dictation capture decides the status before the microphone starts.
/// A missing target or a failed capture still records. A stale token does not.
public func dictationArm(hasTarget: Bool, tokenValid: Bool, captured: Bool, captureFailure: String?) -> DictationArmResult {
    if !hasTarget {
        return DictationArmResult(interactionStatus: dictationNoTarget(), keepInsert: false, startMicrophone: tokenValid)
    }
    if !captured {
        return DictationArmResult(interactionStatus: captureFailure, keepInsert: false, startMicrophone: tokenValid)
    }
    if !tokenValid {
        return DictationArmResult(interactionStatus: nil, keepInsert: false, startMicrophone: false)
    }
    return DictationArmResult(interactionStatus: dictationCaretLocked(), keepInsert: true, startMicrophone: true)
}

public struct DictationMicResult: Equatable, Sendable {
    public var recordingStatus: String?
    public var showRecording: Bool
    public var showError: Bool
    public var cancelMicrophone: Bool
}

/// A microphone that starts after the token expired is canceled and does not show the recording pill.
public func dictationMicrophoneReady(started: Bool, tokenValid: Bool, failure: String) -> DictationMicResult {
    if !started {
        return DictationMicResult(recordingStatus: dictationMicrophoneFailed(failure), showRecording: false, showError: true, cancelMicrophone: false)
    }
    if !tokenValid {
        return DictationMicResult(recordingStatus: nil, showRecording: false, showError: false, cancelMicrophone: true)
    }
    return DictationMicResult(recordingStatus: dictationRecording(), showRecording: true, showError: false, cancelMicrophone: false)
}
