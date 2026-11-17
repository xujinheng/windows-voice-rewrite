import Foundation

/// How far a shortcut refresh proceeds.
/// A stale token or a failed parse stops the chain.
/// Registration waits until both voice shortcuts have parsed.
public enum ShortcutRefresh: Equatable, Sendable {
    case stop
    case parseNext
    case register
}

public func shortcutRefresh(tokenValid: Bool, parsed: Bool, remaining: Int) -> ShortcutRefresh {
    if !tokenValid || !parsed {
        return .stop
    }
    if remaining > 0 {
        return .parseNext
    }
    return .register
}

/// Normalized shortcuts are stored only after macOS accepts the registration.
public func shortcutRefreshStores(registered: Bool) -> Bool {
    registered
}
