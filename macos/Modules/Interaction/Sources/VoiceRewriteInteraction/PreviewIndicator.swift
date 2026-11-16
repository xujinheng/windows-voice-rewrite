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
/// Becoming visible starts both clocks.
/// A phase or pending change moves only the change clock.
/// A mode-name change does not, because this function does not take the mode.
public struct IndicatorTiming: Equatable, Sendable {
    public var started: Date
    public var changed: Date
}

public func indicatorTiming(wasVisible: Bool, wasPending: Bool, wasPhase: Int, pending: Bool, phase: Int, now: Date, started: Date, changed: Date) -> IndicatorTiming {
    var nextStarted = started
    var nextChanged = changed
    if !wasVisible {
        nextStarted = now
    }
    if !wasVisible || wasPhase != phase || wasPending != pending {
        nextChanged = now
    }
    return IndicatorTiming(started: nextStarted, changed: nextChanged)
}
