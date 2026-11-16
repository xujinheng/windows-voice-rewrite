import Foundation

/// Where a stopped recording goes. A stale token sends nothing and leaves the
/// probe lines unchanged. Spoken selection and dictation use different probe lines.
public struct RecordingDispatch: Equatable, Sendable {
    public var send: Bool
    public var spoken: Bool
    public var cancelProbes: Bool
    public var asrProbe: String?
    public var llmProbe: String?

    public init(send: Bool = false, spoken: Bool = false, cancelProbes: Bool = false, asrProbe: String? = nil, llmProbe: String? = nil) {
        self.send = send
        self.spoken = spoken
        self.cancelProbes = cancelProbes
        self.asrProbe = asrProbe
        self.llmProbe = llmProbe
    }
}

public func recordingDispatch(tokenValid: Bool, voiceSelection: Bool) -> RecordingDispatch {
    if !tokenValid {
        return RecordingDispatch()
    }
    if voiceSelection {
        return RecordingDispatch(
            send: true,
            spoken: true,
            cancelProbes: true,
            asrProbe: probeRewriteListeningASR(),
            llmProbe: probeRewriteWaitingLLM())
    }
    return RecordingDispatch(send: true, cancelProbes: true, asrProbe: probeTranscribing())
}

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
