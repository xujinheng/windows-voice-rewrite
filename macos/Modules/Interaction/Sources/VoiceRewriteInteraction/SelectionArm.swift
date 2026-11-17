import Foundation

/// The host applies typed-selection capture outcomes after the native
/// InteractionSession has validated the original AX element and range.
public struct SelectionCue: Equatable, Sendable {
    public var cancelTask: Bool
    public var sendTask: Bool
    public var leave: Bool
    public var stopProcessing: Bool
    public var showError: Bool
    public var interactionStatus: String?
    public var llmProbe: String?

    public init(
        cancelTask: Bool = false,
        sendTask: Bool = false,
        leave: Bool = false,
        stopProcessing: Bool = false,
        showError: Bool = false,
        interactionStatus: String? = nil,
        llmProbe: String? = nil
    ) {
        self.cancelTask = cancelTask
        self.sendTask = sendTask
        self.leave = leave
        self.stopProcessing = stopProcessing
        self.showError = showError
        self.interactionStatus = interactionStatus
        self.llmProbe = llmProbe
    }
}

/// Spoken Hold/Toggle captures the selection on release rather than delaying
/// microphone startup. The active process must still be the captured one.
public func voiceSelectionReleaseMayCapture(tokenValid: Bool, targetPID: Int32,
                                            foregroundPID: Int32?) -> Bool {
    tokenValid && targetPID > 0 && targetPID == foregroundPID
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
