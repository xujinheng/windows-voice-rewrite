import Foundation

/// Native microphone pre-capture is only used for a bound Mouse4 press while
/// permission is already granted. A short navigation click must never prompt.
public func shouldPreCaptureDictation(action: SideMouseAction, authorized: Bool,
                                     paused: Bool, meetingRunning: Bool,
                                     busy: Bool) -> Bool {
    action == .dictation && authorized && !paused && !meetingRunning && !busy
}

