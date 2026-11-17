import Foundation

public struct HotKeyModifiers: OptionSet, Hashable, Sendable {
    public let rawValue: UInt8
    public init(rawValue: UInt8) { self.rawValue = rawValue }
    public static let command = HotKeyModifiers(rawValue: 1 << 0)
    public static let option = HotKeyModifiers(rawValue: 1 << 1)
    public static let control = HotKeyModifiers(rawValue: 1 << 2)
    public static let shift = HotKeyModifiers(rawValue: 1 << 3)
}

public struct HotKeyShortcut: Hashable, Sendable {
    /// Hardware virtual key code, not a Unicode scalar or localized character.
    public let keyCode: UInt32
    public let modifiers: HotKeyModifiers
    public init(keyCode: UInt32, modifiers: HotKeyModifiers) {
        self.keyCode = keyCode
        self.modifiers = modifiers
    }
}

public enum HotKeyFailure: Error, Equatable {
    case duplicate
    case invalidShortcut
    case systemConflict(Int32)
    case registrationFailed(Int32)
}

public struct HotKeyToken: Hashable, Sendable {
    public let id: UInt32
    public init(id: UInt32) { self.id = id }
}

/// Event-pair admission for native Carbon hotkeys. Dropped or repeated
/// callbacks never become a second recording, including after re-registration.
public struct HotKeyPressState: Sendable {
    private var down = Set<HotKeyToken>()

    public init() {}

    @discardableResult
    public mutating func transition(_ token: HotKeyToken, pressed: Bool) -> Bool {
        if pressed { return down.insert(token).inserted }
        return down.remove(token) != nil
    }

    public mutating func forget(_ token: HotKeyToken) {
        down.remove(token)
    }

    public mutating func clear() {
        down.removeAll()
    }
}

@MainActor
public protocol HotKeyDriver: AnyObject {
    func register(_ shortcut: HotKeyShortcut) throws -> HotKeyToken
    func unregister(_ token: HotKeyToken)
}

/// The host owns this registry and must call shutdown on application termination.
@MainActor
public final class HotKeyRegistry {
    private let driver: any HotKeyDriver
    private var registrations: [HotKeyShortcut: HotKeyToken] = [:]

    public init(driver: any HotKeyDriver) { self.driver = driver }

    public func register(_ shortcut: HotKeyShortcut) throws {
        let bareFunctionKey = Self.functionKeyCodes.contains(shortcut.keyCode)
        guard shortcut.keyCode <= 127, !shortcut.modifiers.isEmpty || bareFunctionKey,
              shortcut.modifiers.rawValue & ~UInt8(0x0F) == 0 else {
            throw HotKeyFailure.invalidShortcut
        }
        guard registrations[shortcut] == nil else { throw HotKeyFailure.duplicate }
        registrations[shortcut] = try driver.register(shortcut)
    }

    public func replace(with shortcuts: [HotKeyShortcut]) throws {
        guard Set(shortcuts).count == shortcuts.count else { throw HotKeyFailure.duplicate }
        let existing = Set(registrations.keys)
        let desired = Set(shortcuts)
        var added: [HotKeyShortcut] = []
        do {
            for shortcut in shortcuts where !existing.contains(shortcut) {
                try register(shortcut)
                added.append(shortcut)
            }
        } catch {
            for shortcut in added { unregister(shortcut) }
            throw error
        }
        for shortcut in existing where !desired.contains(shortcut) {
            unregister(shortcut)
        }
    }

    public func unregister(_ shortcut: HotKeyShortcut) {
        guard let token = registrations.removeValue(forKey: shortcut) else { return }
        driver.unregister(token)
    }

    public func shutdown() {
        for token in registrations.values { driver.unregister(token) }
        registrations.removeAll()
    }

    public var registeredCount: Int { registrations.count }

    private static let functionKeyCodes: Set<UInt32> = [122, 120, 99, 118, 96, 97, 98, 100, 101, 109, 103, 111, 105, 107, 113, 106, 64, 79, 80, 90]

    deinit {
        // The driver must remain owned by the registry; OS hotkeys are never left registered.
        MainActor.assumeIsolated {
            for token in registrations.values { driver.unregister(token) }
        }
    }
}
