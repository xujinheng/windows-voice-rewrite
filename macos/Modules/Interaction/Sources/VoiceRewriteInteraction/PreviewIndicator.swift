import Foundation

/// Whether a side-button press may show a pending recording preview.
/// Recording, starting, or processing hides it.
/// Dictation and rewrite keep different mode names.
public enum SidePreview: Equatable, Sendable {
    case hidden
    case dictation
    case rewrite
}

public func sidePreview(recording: Bool, starting: Bool, processing: Bool, dictation: Bool) -> SidePreview {
    if recording || starting || processing {
        return .hidden
    }
    if dictation {
        return .dictation
    }
    return .rewrite
}

/// Dismissing a preview always allows the next hold.
/// Only a pending indicator is closed. A visible recording pill stays up.
public struct PreviewDismiss: Equatable, Sendable {
    public var allowNextHold: Bool
    public var dismissIndicator: Bool
}

public func previewDismiss(pending: Bool) -> PreviewDismiss {
    PreviewDismiss(allowNextHold: true, dismissIndicator: pending)
}

/// The recording pill clock.
/// Becoming visible or beginning a new recording starts both clocks.
/// Other phase/pending changes move only the change clock.
/// A mode-name change does not, because this function does not take the mode.
public struct IndicatorTiming: Equatable, Sendable {
    public var started: Date
    public var changed: Date
}

public func indicatorTiming(wasVisible: Bool, wasPending: Bool, wasPhase: Int, pending: Bool, phase: Int, now: Date, started: Date, changed: Date) -> IndicatorTiming {
    var nextStarted = started
    var nextChanged = changed
    // Phase 1 is indicator.Recording in the shared Go indicator contract.
    // An old terminal/processing pill may still be visible when the next
    // recording begins. Do not carry its elapsed time into the new session.
    // Pending -> active, however, is still the *same* recording.
    let newRecording = phase == 1 && (wasPhase != 1 || (pending && !wasPending))
    if !wasVisible || newRecording {
        nextStarted = now
    }
    if !wasVisible || newRecording || wasPhase != phase || wasPending != pending {
        nextChanged = now
    }
    return IndicatorTiming(started: nextStarted, changed: nextChanged)
}
