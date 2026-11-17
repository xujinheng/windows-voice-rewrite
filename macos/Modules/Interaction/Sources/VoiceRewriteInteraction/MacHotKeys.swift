#if os(macOS)
import AppKit
import Carbon.HIToolbox
import Foundation

private let carbonHotKeyCallback: EventHandlerUPP = { _, event, userData in
    guard let event, let userData else { return noErr }
    var hotKeyID = EventHotKeyID()
    let result = GetEventParameter(event, EventParamName(kEventParamDirectObject),
                                   EventParamType(typeEventHotKeyID), nil,
                                   MemoryLayout<EventHotKeyID>.size, nil, &hotKeyID)
    guard result == noErr else { return result }
    // A separate AppKit owner registers the independent global Quit command.
    // Never consume events with another signature; Carbon must pass them on.
    guard hotKeyID.signature == OSType(0x5652_5752) else { return OSStatus(eventNotHandledErr) }
    let driver = Unmanaged<CarbonHotKeyDriver>.fromOpaque(userData).takeUnretainedValue()
    let identifier = hotKeyID.id
    // Pin the foreground target when the OS delivers the hotkey, before any async work.
    let foregroundPID = NSWorkspace.shared.frontmostApplication?.processIdentifier
    // Never block Carbon's event callback; delivery is always on the main actor.
    DispatchQueue.main.async {
        MainActor.assumeIsolated { driver.dispatch(identifier: identifier, foregroundPID: foregroundPID) }
    }
    return noErr
}

/// Real global keyboard shortcuts. RegisterEventHotKey needs no Accessibility permission.
/// Construct and retain on the main actor. Call shutdown on app termination.
@MainActor
public final class CarbonHotKeyDriver: HotKeyDriver {
    private var eventHandler: EventHandlerRef?
    private var registrations: [HotKeyToken: (shortcut: HotKeyShortcut, ref: EventHotKeyRef)] = [:]
    private var nextIdentifier: UInt32 = 1
    private let onPressed: @MainActor (HotKeyShortcut, Int32?) -> Void

    public init(onPressed: @escaping @MainActor (HotKeyShortcut, Int32?) -> Void) {
        self.onPressed = onPressed
    }

    public func register(_ shortcut: HotKeyShortcut) throws -> HotKeyToken {
        guard nextIdentifier < UInt32.max else { throw HotKeyFailure.registrationFailed(-1) }
        if eventHandler == nil {
            var spec = EventTypeSpec(eventClass: OSType(kEventClassKeyboard),
                                     eventKind: UInt32(kEventHotKeyPressed))
            var handler: EventHandlerRef?
            let result = InstallEventHandler(GetApplicationEventTarget(), carbonHotKeyCallback,
                                             1, &spec, Unmanaged.passUnretained(self).toOpaque(),
                                             &handler)
            guard result == noErr, let handler else {
                throw HotKeyFailure.registrationFailed(result)
            }
            eventHandler = handler
        }
        var flags: UInt32 = 0
        if shortcut.modifiers.contains(.command) { flags |= UInt32(cmdKey) }
        if shortcut.modifiers.contains(.option) { flags |= UInt32(optionKey) }
        if shortcut.modifiers.contains(.control) { flags |= UInt32(controlKey) }
        if shortcut.modifiers.contains(.shift) { flags |= UInt32(shiftKey) }
        let token = HotKeyToken(id: nextIdentifier)
        let hotKeyID = EventHotKeyID(signature: OSType(0x5652_5752), id: token.id) // 'VRWR'
        var ref: EventHotKeyRef?
        let result = RegisterEventHotKey(shortcut.keyCode, flags, hotKeyID,
                                         GetApplicationEventTarget(), 0, &ref)
        guard result == noErr, let ref else {
            // RegisterEventHotKey reports system-reserved or taken shortcuts as a conflict.
            if result == eventHotKeyExistsErr { throw HotKeyFailure.systemConflict(result) }
            throw HotKeyFailure.registrationFailed(result)
        }
        registrations[token] = (shortcut, ref)
        nextIdentifier += 1
        return token
    }

    public func unregister(_ token: HotKeyToken) {
        guard let registration = registrations.removeValue(forKey: token) else { return }
        UnregisterEventHotKey(registration.ref)
        if registrations.isEmpty, let eventHandler {
            RemoveEventHandler(eventHandler)
            self.eventHandler = nil
        }
    }

    /// Release all Carbon objects on app exit, even if registry teardown was skipped.
    public func shutdown() {
        for registration in registrations.values { UnregisterEventHotKey(registration.ref) }
        registrations.removeAll()
        if let eventHandler { RemoveEventHandler(eventHandler) }
        eventHandler = nil
    }

    fileprivate func dispatch(identifier: UInt32, foregroundPID: Int32?) {
        guard let registration = registrations[HotKeyToken(id: identifier)] else { return }
        onPressed(registration.shortcut, foregroundPID)
    }

    deinit { MainActor.assumeIsolated { shutdown() } }
}
#endif
