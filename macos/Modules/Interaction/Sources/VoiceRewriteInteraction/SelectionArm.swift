import Foundation

/// What the host does after a selection capture. Spoken and typed captures
/// stay different: a spoken capture can start the microphone and release the
/// side button, and a typed capture can send the instruction or leave
/// processing set.
public struct SelectionCue: Equatable, Sendable {
    public var cancelTask: Bool
    public var cancelMicrophone: Bool
    public var startMicrophone: Bool
    public var beginRecording: Bool
    public var sendTask: Bool
    public var leave: Bool
    public var stopProcessing: Bool
    public var showError: Bool
    public var interactionStatus: String?
    public var recordingStatus: String?
    public var llmProbe: String?

    public init(
        cancelTask: Bool = false,
        cancelMicrophone: Bool = false,
        startMicrophone: Bool = false,
        beginRecording: Bool = false,
        sendTask: Bool = false,
        leave: Bool = false,
        stopProcessing: Bool = false,
        showError: Bool = false,
        interactionStatus: String? = nil,
        recordingStatus: String? = nil,
        llmProbe: String? = nil
    ) {
        self.cancelTask = cancelTask
        self.cancelMicrophone = cancelMicrophone
        self.startMicrophone = startMicrophone
        self.beginRecording = beginRecording
        self.sendTask = sendTask
        self.leave = leave
        self.stopProcessing = stopProcessing
        self.showError = showError
        self.interactionStatus = interactionStatus
        self.recordingStatus = recordingStatus
        self.llmProbe = llmProbe
    }
}

/// A spoken selection checks the token before the length limit.
/// A stale token cancels the task and leaves before the side button is released.
/// A selection that is too long does not start the microphone, and the host
/// still releases the side button.
public func rewriteVoiceAfterCapture(tokenValid: Bool, tooLong: String?) -> SelectionCue {
    if !tokenValid {
        return SelectionCue(cancelTask: true, leave: true)
    }
    if let tooLong {
        return SelectionCue(
            cancelTask: true,
            showError: true,
            interactionStatus: tooLong,
            recordingStatus: recordingIdle())
    }
    return SelectionCue(startMicrophone: true)
}

/// A microphone that starts after the spoken-selection token expired is
/// canceled. The recording pill stays on the capture phase.
public func rewriteVoiceMicrophoneReady(tokenValid: Bool) -> SelectionCue {
    if !tokenValid {
        return SelectionCue(cancelTask: true, cancelMicrophone: true, leave: true)
    }
    return SelectionCue(
        beginRecording: true,
        interactionStatus: rewriteSelectionLocked(),
        recordingStatus: rewriteRecording())
}

/// A spoken capture error always cancels the microphone and cancels the task
/// when one was opened. The error pill is shown only while the token is current.
public func rewriteVoiceCaptureFailed(tokenValid: Bool, status: String) -> SelectionCue {
    if !tokenValid {
        return SelectionCue(cancelTask: true, cancelMicrophone: true)
    }
    return SelectionCue(
        cancelTask: true,
        cancelMicrophone: true,
        showError: true,
        interactionStatus: status,
        recordingStatus: recordingIdle())
}

/// A typed selection leaves processing set when the token is stale.
/// A selection that is too long stops processing and does not send the instruction.
public func typedSelectionAfterCapture(tokenValid: Bool, tooLong: String?) -> SelectionCue {
    if !tokenValid {
        return SelectionCue(cancelTask: true, leave: true)
    }
    if let tooLong {
        return SelectionCue(
            cancelTask: true,
            leave: true,
            stopProcessing: true,
            showError: true,
            interactionStatus: tooLong,
            llmProbe: probeTaskIncomplete())
    }
    return SelectionCue(sendTask: true)
}

/// A typed capture error does not cancel the Accessibility task.
/// A stale token leaves processing set and shows nothing.
public func typedSelectionStartFailed(tokenValid: Bool, detail: String) -> SelectionCue {
    if !tokenValid {
        return SelectionCue()
    }
    return SelectionCue(
        stopProcessing: true,
        showError: true,
        interactionStatus: rewriteTaskNotStarted(detail),
        llmProbe: probeTaskIncomplete())
}
