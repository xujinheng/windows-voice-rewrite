import Foundation

/// Why a macOS settings save stops before the typed keys are cleared.
/// A busy voice task wins over a long credential. A long credential wins over an invalid shortcut plan.
public enum SettingsSaveRefusal: Equatable, Sendable {
    case busy
    case credential
    case shortcut
    case ready
}

public func settingsSaveRefusal(busy: Bool, credentialTooLong: Bool, planValid: Bool) -> SettingsSaveRefusal {
    if busy {
        return .busy
    }
    if credentialTooLong {
        return .credential
    }
    if !planValid {
        return .shortcut
    }
    return .ready
}
