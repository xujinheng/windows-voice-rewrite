import Foundation

/// Native microphone pre-capture is only used for a bound Mouse4 press while
/// permission is already granted. A short navigation click must never prompt.
public func shouldPreCaptureDictation(action: SideMouseAction, authorized: Bool,
                                     paused: Bool, meetingRunning: Bool,
                                     busy: Bool) -> Bool {
    action == .dictation && authorized && !paused && !meetingRunning && !busy
}

/// Whether a side-button hold may start voice work, and what a later release does.
/// The host still owns the microphone, the indicator, and the shortcut plan.
public struct SideVoiceGate: Equatable, Sendable {
    public var active: SideMouseAction?
    public var pendingRelease: SideMouseAction?
    public var ignoreNext = false

    public init() {}

    public enum HoldEffect: Equatable, Sendable {
        case dismissPreview
        case ignored
        case start
    }

    public mutating func hold(paused: Bool, busy: Bool) -> HoldEffect {
        if paused || busy || active != nil {
            return .dismissPreview
        }
        if ignoreNext {
            ignoreNext = false
            return .ignored
        }
        return .start
    }

    public mutating func began(_ action: SideMouseAction) {
        active = action
        pendingRelease = nil
    }

    public enum ReleaseEffect: Equatable, Sendable {
        case none
        case wait
        case stop
        case clear
    }

    public mutating func release(_ action: SideMouseAction, starting: Bool, recording: Bool) -> ReleaseEffect {
        guard active == action else { return .none }
        if starting {
            pendingRelease = action
            return .wait
        }
        if recording {
            return .stop
        }
        active = nil
        return .clear
    }

    public mutating func finishStart(_ action: SideMouseAction, recording: Bool) -> ReleaseEffect {
        guard pendingRelease == action else { return .none }
        pendingRelease = nil
        if recording {
            return .stop
        }
        active = nil
        return .clear
    }

    public mutating func clear() {
        active = nil
        pendingRelease = nil
    }

    public mutating func suppressNextHold() {
        ignoreNext = true
    }

    public mutating func allowNextHold() {
        ignoreNext = false
    }
}
