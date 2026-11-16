import Foundation

/// A voice-pause click. Busy refuses the click and leaves the pause flag unchanged.
/// Turning voice back on uses the idle sentence, not the fresh-listener sentence.
public struct VoicePauseTurn: Equatable, Sendable {
    public var toggle: Bool
    public var interactionStatus: String?
    public var sideStatus: String?

    public init(toggle: Bool, interactionStatus: String? = nil, sideStatus: String? = nil) {
        self.toggle = toggle
        self.interactionStatus = interactionStatus
        self.sideStatus = sideStatus
    }
}

public func voicePauseTurn(busy: Bool, currentlyPaused: Bool, busyStatus: String, pausedStatus: String, idleStatus: String) -> VoicePauseTurn {
    if busy {
        return VoicePauseTurn(toggle: false, interactionStatus: busyStatus)
    }
    if currentlyPaused {
        return VoicePauseTurn(toggle: true, sideStatus: idleStatus)
    }
    return VoicePauseTurn(toggle: true, sideStatus: pausedStatus)
}

public enum SideMouseStart: Equatable, Sendable {
    case paused
    case listening
    case unchanged
}

/// A fresh side-mouse listener reports listening, or paused when voice is already paused.
/// Starting an existing listener again leaves the status line alone.
public func sideMouseStart(fresh: Bool, paused: Bool) -> SideMouseStart {
    if !fresh {
        return .unchanged
    }
    if paused {
        return .paused
    }
    return .listening
}
