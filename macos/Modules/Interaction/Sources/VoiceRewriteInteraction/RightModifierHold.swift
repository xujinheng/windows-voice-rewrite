#if os(macOS)
import AppKit
import ApplicationServices
import CoreGraphics
import Foundation

/// Physical right-hand modifier key codes, independent of keyboard layout.
public enum RightModifierKey: String, Equatable, Sendable {
    case control = "RightCtrl"
    case option = "RightOption"

    public var keyCode: Int64 {
        switch self {
        case .control: return 62 // kVK_RightControl
        case .option: return 61 // kVK_RightOption
        }
    }
}

/// A bare right modifier only becomes voice input after a deliberate hold.
/// All flagsChanged/keyDown events pass through to the focused application.
/// Once another key or modifier is used, it cannot re-arm until key-up.
public struct RightModifierHoldState: Sendable {
    public private(set) var held: RightModifierKey?
    public private(set) var active = false
    private var blocked = false
    private var sequence: UInt64 = 0

    public init() {}

    public mutating func press(_ key: RightModifierKey, unmodified: Bool) -> UInt64? {
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

    public mutating func activate(_ key: RightModifierKey, token: UInt64) -> Bool {
        guard held == key, sequence == token, !blocked, !active else { return false }
        active = true
        return true
    }

    /// Returns an activated action that must be cancelled, not submitted.
    public mutating func interrupt() -> RightModifierKey? {
        guard held != nil else { return nil }
        let canceled = active ? held : nil
        blocked = true
        active = false
        return canceled
    }

    /// Short presses are ignored; release an activated hold exactly once.
    public mutating func release(_ key: RightModifierKey) -> RightModifierKey? {
        guard held == key else { return nil }
        let released = active ? held : nil
        held = nil
        active = false
        blocked = false
        return released
    }

    public mutating func clear() -> RightModifierKey? {
        let canceled = active ? held : nil
        held = nil
        active = false
        blocked = false
        sequence &+= 1
        return canceled
    }
}

public enum RightModifierFailure: Error, LocalizedError {
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

private let rightModifierTapCallback: CGEventTapCallBack = { _, type, event, userData in
    guard let userData else { return Unmanaged.passUnretained(event) }
    let driver = Unmanaged<RightModifierHoldDriver>.fromOpaque(userData).takeUnretainedValue()
    return MainActor.assumeIsolated { driver.handle(type: type, event: event) }
}

/// A listen-only Quartz tap: never consumes Right Control, Right Option, or
/// combinations in another program. It shares the mouse driver's permission
/// model without replacing its Mouse4/Mouse5 tap.
@MainActor
public final class RightModifierHoldDriver {
    private let threshold: TimeInterval
    private let onHold: @MainActor (RightModifierKey, Int32?) -> Void
    private let onRelease: @MainActor (RightModifierKey) -> Void
    private let onCancel: @MainActor () -> Void
    private let onFault: @MainActor (RightModifierFailure) -> Void
    private var tap: CFMachPort?
    private var source: CFRunLoopSource?
    private var state = RightModifierHoldState()
    private var dictation: RightModifierKey?
    private var rewrite: RightModifierKey?
    private var suspended = false
    private var paused = false

    public init(threshold: TimeInterval = SideHold.threshold,
                onHold: @escaping @MainActor (RightModifierKey, Int32?) -> Void,
                onRelease: @escaping @MainActor (RightModifierKey) -> Void,
                onCancel: @escaping @MainActor () -> Void,
                onFault: @escaping @MainActor (RightModifierFailure) -> Void = { _ in }) {
        self.threshold = threshold
        self.onHold = onHold
        self.onRelease = onRelease
        self.onCancel = onCancel
        self.onFault = onFault
    }

    public func setBindings(dictation: RightModifierKey?, rewrite: RightModifierKey?) {
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
        guard AXIsProcessTrusted() else { throw RightModifierFailure.accessibilityDenied }
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
            callback: rightModifierTapCallback, userInfo: Unmanaged.passUnretained(self).toOpaque()
        ) else { throw RightModifierFailure.eventTapUnavailable }
        guard let loopSource = CFMachPortCreateRunLoopSource(kCFAllocatorDefault, port, 0) else {
            CFMachPortInvalidate(port)
            throw RightModifierFailure.eventTapUnavailable
        }
        tap = port
        source = loopSource
        CFRunLoopAddSource(CFRunLoopGetMain(), loopSource, .commonModes)
        CGEvent.tapEnable(tap: port, enable: true)
        guard CGEvent.tapIsEnabled(tap: port) else {
            shutdown()
            throw RightModifierFailure.eventTapUnavailable
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
        let flags = event.flags
        let key: RightModifierKey?
        let pressed: Bool
        switch code {
        case RightModifierKey.control.keyCode:
            key = .control
            pressed = flags.contains(.maskControl)
        case RightModifierKey.option.keyCode:
            key = .option
            pressed = flags.contains(.maskAlternate)
        default:
            if state.interrupt() != nil {
                DispatchQueue.main.async { [weak self] in self?.onCancel() }
            }
            return Unmanaged.passUnretained(event)
        }
        guard let key else { return Unmanaged.passUnretained(event) }
        if !pressed {
            if let released = state.release(key) {
                DispatchQueue.main.async { [weak self] in self?.onRelease(released) }
            }
            return Unmanaged.passUnretained(event)
        }
        guard dictation == key || rewrite == key else {
            if state.interrupt() != nil {
                DispatchQueue.main.async { [weak self] in self?.onCancel() }
            }
            return Unmanaged.passUnretained(event)
        }
        let otherKeys: CGEventFlags = [.maskCommand, .maskShift, .maskControl, .maskAlternate]
        let expected: CGEventFlags = key == .control ? .maskControl : .maskAlternate
        let alone = flags.intersection(otherKeys) == expected
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
