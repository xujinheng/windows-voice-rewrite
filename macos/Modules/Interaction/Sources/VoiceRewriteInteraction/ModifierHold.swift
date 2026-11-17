#if os(macOS)
import AppKit
import ApplicationServices
import CoreGraphics
import Foundation

/// The six physical left/right keys, independent of the keyboard layout.
/// Bare modifier holds belong to the macOS host and are never Carbon hotkeys.
public enum MacModifierKey: String, CaseIterable, Equatable, Sendable {
    case leftControl = "LeftCtrl"
    case rightControl = "RightCtrl"
    case leftOption = "LeftOption"
    case rightOption = "RightOption"
    case leftCommand = "LeftCommand"
    case rightCommand = "RightCommand"

    public init?(canonical: String) {
        switch canonical.trimmingCharacters(in: .whitespacesAndNewlines).lowercased() {
        case "leftctrl", "leftcontrol": self = .leftControl
        case "rightctrl", "rightcontrol": self = .rightControl
        case "leftoption", "leftalt": self = .leftOption
        case "rightoption", "rightalt": self = .rightOption
        case "leftcommand", "leftcmd": self = .leftCommand
        case "rightcommand", "rightcmd": self = .rightCommand
        default: return nil
        }
    }

    public init?(keyCode: Int64) {
        switch keyCode {
        case 59: self = .leftControl
        case 62: self = .rightControl
        case 58: self = .leftOption
        case 61: self = .rightOption
        case 55: self = .leftCommand
        case 54: self = .rightCommand
        default: return nil
        }
    }

    public var keyCode: Int64 {
        switch self {
        case .leftControl: return 59
        case .rightControl: return 62
        case .leftOption: return 58
        case .rightOption: return 61
        case .leftCommand: return 55
        case .rightCommand: return 54
        }
    }

    public var otherSideKeyCode: CGKeyCode {
        switch self {
        case .leftControl: return 62
        case .rightControl: return 59
        case .leftOption: return 61
        case .rightOption: return 58
        case .leftCommand: return 54
        case .rightCommand: return 55
        }
    }

    public var groupFlag: CGEventFlags {
        switch self {
        case .leftControl, .rightControl: return .maskControl
        case .leftOption, .rightOption: return .maskAlternate
        case .leftCommand, .rightCommand: return .maskCommand
        }
    }

    /// NSEvent flags for the same modifier family, used by key-learning UI.
    public var captureFlag: NSEvent.ModifierFlags {
        switch self {
        case .leftControl, .rightControl: return .control
        case .leftOption, .rightOption: return .option
        case .leftCommand, .rightCommand: return .command
        }
    }

    public var displayName: String {
        switch self {
        case .leftControl: return "左 Control（长按）"
        case .rightControl: return "右 Control（长按）"
        case .leftOption: return "左 Option（长按）"
        case .rightOption: return "右 Option（长按）"
        case .leftCommand: return "左 Command（长按）"
        case .rightCommand: return "右 Command（长按）"
        }
    }
}

/// A single physical modifier only becomes voice input after a deliberate hold.
/// All flagsChanged/keyDown events pass through to the focused application.
/// Once another key or modifier is used, it cannot re-arm until key-up.
public struct MacModifierHoldState: Sendable {
    public private(set) var held: MacModifierKey?
    public private(set) var active = false
    private var blocked = false
    private var sequence: UInt64 = 0

    public init() {}

    public mutating func press(_ key: MacModifierKey, unmodified: Bool) -> UInt64? {
        if held != nil {
            _ = interrupt()
            return nil
        }
        held = key
        blocked = !unmodified
        guard !blocked else { return nil }
        sequence &+= 1
        return sequence
    }

    public mutating func activate(_ key: MacModifierKey, token: UInt64) -> Bool {
        guard held == key, sequence == token, !blocked, !active else { return false }
        active = true
        return true
    }

    /// Returns an activated action that must be cancelled, not submitted.
    public mutating func interrupt() -> MacModifierKey? {
        guard held != nil else { return nil }
        let canceled = active ? held : nil
        blocked = true
        active = false
        return canceled
    }

    /// Short presses are ignored; release an activated hold exactly once.
    public mutating func release(_ key: MacModifierKey) -> MacModifierKey? {
        guard held == key else { return nil }
        let released = active ? held : nil
        held = nil
        active = false
        blocked = false
        return released
    }

    public mutating func clear() -> MacModifierKey? {
        let canceled = active ? held : nil
        held = nil
        active = false
        blocked = false
        sequence &+= 1
        return canceled
    }
}

public enum MacModifierHoldFailure: Error, LocalizedError {
    case accessibilityDenied
    case eventTapUnavailable

    public var errorDescription: String? {
        switch self {
        case .accessibilityDenied:
            return "单键长按需要辅助功能权限；请先在系统设置中授权 VoiceRewrite"
        case .eventTapUnavailable:
            return "右侧修饰键监听不可用；请检查输入监控权限和其他键盘映射工具"
        }
    }
}

private let macModifierTapCallback: CGEventTapCallBack = { _, type, event, userData in
    guard let userData else { return Unmanaged.passUnretained(event) }
    let driver = Unmanaged<MacModifierHoldDriver>.fromOpaque(userData).takeUnretainedValue()
    return MainActor.assumeIsolated { driver.handle(type: type, event: event) }
}

/// A listen-only Quartz tap: never consumes Control, Option, Command, or
/// their combinations in another program. It shares the mouse driver's permission
/// model without replacing its Mouse4/Mouse5 tap.
@MainActor
public final class MacModifierHoldDriver {
    private let threshold: TimeInterval
    private let onHold: @MainActor (MacModifierKey, Int32?) -> Void
    private let onRelease: @MainActor (MacModifierKey) -> Void
    private let onCancel: @MainActor () -> Void
    private let onFault: @MainActor (MacModifierHoldFailure) -> Void
    private var tap: CFMachPort?
    private var source: CFRunLoopSource?
    private var state = MacModifierHoldState()
    private var dictation: MacModifierKey?
    private var rewrite: MacModifierKey?
    private var suspended = false
    private var paused = false

    public init(threshold: TimeInterval = SideHold.threshold,
                onHold: @escaping @MainActor (MacModifierKey, Int32?) -> Void,
                onRelease: @escaping @MainActor (MacModifierKey) -> Void,
                onCancel: @escaping @MainActor () -> Void,
                onFault: @escaping @MainActor (MacModifierHoldFailure) -> Void = { _ in }) {
        self.threshold = threshold
        self.onHold = onHold
        self.onRelease = onRelease
        self.onCancel = onCancel
        self.onFault = onFault
    }

    public func setBindings(dictation: MacModifierKey?, rewrite: MacModifierKey?) {
        guard self.dictation != dictation || self.rewrite != rewrite else { return }
        if state.clear() != nil { onCancel() }
        self.dictation = dictation
        self.rewrite = rewrite
    }

    public func setSuspended(_ value: Bool) {
        suspended = value
        if value, state.clear() != nil { onCancel() }
    }

    public func setVoicePaused(_ value: Bool) {
        paused = value
        if value, state.clear() != nil { onCancel() }
    }

    public func start() throws {
        guard AXIsProcessTrusted() else { throw MacModifierHoldFailure.accessibilityDenied }
        if let tap {
            if CFMachPortIsValid(tap) {
                if !CGEvent.tapIsEnabled(tap: tap) { CGEvent.tapEnable(tap: tap, enable: true) }
                if CGEvent.tapIsEnabled(tap: tap) { return }
            }
            shutdown()
        }
        let mask = (CGEventMask(1) << CGEventType.flagsChanged.rawValue)
            | (CGEventMask(1) << CGEventType.keyDown.rawValue)
        guard let port = CGEvent.tapCreate(
            tap: .cgSessionEventTap, place: .headInsertEventTap,
            options: .listenOnly, eventsOfInterest: mask,
            callback: macModifierTapCallback, userInfo: Unmanaged.passUnretained(self).toOpaque()
        ) else { throw MacModifierHoldFailure.eventTapUnavailable }
        guard let loopSource = CFMachPortCreateRunLoopSource(kCFAllocatorDefault, port, 0) else {
            CFMachPortInvalidate(port)
            throw MacModifierHoldFailure.eventTapUnavailable
        }
        tap = port
        source = loopSource
        CFRunLoopAddSource(CFRunLoopGetMain(), loopSource, .commonModes)
        CGEvent.tapEnable(tap: port, enable: true)
        guard CGEvent.tapIsEnabled(tap: port) else {
            shutdown()
            throw MacModifierHoldFailure.eventTapUnavailable
        }
    }

    public func shutdown() {
        if state.clear() != nil { onCancel() }
        if let tap { CGEvent.tapEnable(tap: tap, enable: false) }
        if let source { CFRunLoopRemoveSource(CFRunLoopGetMain(), source, .commonModes) }
        if let tap { CFMachPortInvalidate(tap) }
        source = nil
        tap = nil
    }

    fileprivate func handle(type: CGEventType, event: CGEvent) -> Unmanaged<CGEvent>? {
        if type == .tapDisabledByTimeout || type == .tapDisabledByUserInput {
            if state.clear() != nil {
                DispatchQueue.main.async { [weak self] in self?.onCancel() }
            }
            DispatchQueue.main.async { [weak self] in
                guard let self else { return }
                do { try self.start() }
                catch { self.onFault(.eventTapUnavailable) }
            }
            return Unmanaged.passUnretained(event)
        }
        guard !suspended, !paused else { return Unmanaged.passUnretained(event) }
        if type == .keyDown {
            if state.interrupt() != nil {
                DispatchQueue.main.async { [weak self] in self?.onCancel() }
            }
            return Unmanaged.passUnretained(event)
        }
        guard type == .flagsChanged else { return Unmanaged.passUnretained(event) }
        let code = event.getIntegerValueField(.keyboardEventKeycode)
        guard let key = MacModifierKey(keyCode: code) else {
            if state.interrupt() != nil {
                DispatchQueue.main.async { [weak self] in self?.onCancel() }
            }
            return Unmanaged.passUnretained(event)
        }
        let flags = event.flags
        // A flagsChanged event uses the physical virtual keycode to identify
        // which side moved. The aggregate flags cannot identify that side.
        // For the held key, this event is its release even if the other side
        // of the same modifier remains pressed.
        if state.held == key {
            if let released = state.release(key) {
                DispatchQueue.main.async { [weak self] in self?.onRelease(released) }
            }
            return Unmanaged.passUnretained(event)
        }
        guard flags.contains(key.groupFlag) else {
            return Unmanaged.passUnretained(event)
        }
        guard dictation == key || rewrite == key else {
            if state.interrupt() != nil {
                DispatchQueue.main.async { [weak self] in self?.onCancel() }
            }
            return Unmanaged.passUnretained(event)
        }
        let ordinaryModifierFlags: CGEventFlags = [.maskCommand, .maskShift, .maskControl, .maskAlternate]
        let alone = flags.intersection(ordinaryModifierFlags) == key.groupFlag &&
            !CGEventSource.keyState(.combinedSessionState, key: key.otherSideKeyCode)
        if let token = state.press(key, unmodified: alone) {
            let pid = NSWorkspace.shared.frontmostApplication?.processIdentifier
            DispatchQueue.main.asyncAfter(deadline: .now() + threshold) { [weak self] in
                guard let self, !self.suspended, !self.paused,
                      self.state.activate(key, token: token) else { return }
                self.onHold(key, pid)
            }
        }
        return Unmanaged.passUnretained(event)
    }

    deinit { MainActor.assumeIsolated { shutdown() } }
}
#endif
