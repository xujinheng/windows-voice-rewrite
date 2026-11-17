#if os(macOS)
import AppKit
import ApplicationServices
import CoreGraphics
import Foundation

public enum SideMouseAction: Int, Sendable {
    case dictation = 3   // Mouse4: the first auxiliary button.
    case rewrite = 4     // Mouse5: the second auxiliary button.
}

/// Only explicitly bound auxiliary buttons are intercepted. Nil leaves a button
/// available to the mouse driver; the standard keyboard-shortcut configuration
/// retains Mouse4 dictation and Mouse5 rewrite by default.
public struct SideMouseBindings: Equatable, Sendable {
    public let dictationButton: Int?
    public let rewriteButton: Int?

    public init(dictationButton: Int? = 3, rewriteButton: Int? = 4) {
        self.dictationButton = dictationButton
        self.rewriteButton = rewriteButton
    }

    public var isValid: Bool {
        let allowed: Set<Int> = [3, 4]
        guard dictationButton.map({ allowed.contains($0) }) ?? true,
              rewriteButton.map({ allowed.contains($0) }) ?? true else { return false }
        return dictationButton == nil || rewriteButton == nil || dictationButton != rewriteButton
    }

    public func action(for button: Int) -> SideMouseAction? {
        if dictationButton == button { return .dictation }
        if rewriteButton == button { return .rewrite }
        return nil
    }

    public func button(for action: SideMouseAction) -> Int? {
        switch action {
        case .dictation: return dictationButton
        case .rewrite: return rewriteButton
        }
    }
}

public enum SideMouseFailure: Error, LocalizedError {
    case accessibilityDenied
    case eventTapUnavailable

    public var errorDescription: String? {
        switch self {
        case .accessibilityDenied:
            return "请在系统设置中为 VoiceRewrite 授予辅助功能权限"
        case .eventTapUnavailable:
            return "无法监听鼠标侧键：请检查输入监控权限、鼠标驱动和其他侧键映射软件"
        }
    }
}

// The same press/activate/release state transitions as Windows' sideButtonState.
// Timers carry a generation token so a late timer never starts a new press.
struct SideButtonHoldState {
    struct Press {
        var token: UInt64
        var active: Bool
    }

    enum Release: Equatable {
        case ignored, short, held
    }

    private(set) var pressed: [Int: Press] = [:]
    private var cancelledReleases: [Int: Release] = [:]
    private var sequence: UInt64 = 0

    var hasPendingPress: Bool { pressed.values.contains { !$0.active } }

    mutating func press(_ button: Int) -> UInt64? {
        guard pressed[button] == nil, cancelledReleases[button] == nil,
              sequence < UInt64.max else { return nil }
        sequence += 1
        pressed[button] = Press(token: sequence, active: false)
        return sequence
    }

    mutating func activate(_ button: Int, token: UInt64) -> Bool {
        guard var value = pressed[button], value.token == token, !value.active else { return false }
        value.active = true
        pressed[button] = value
        return true
    }

    mutating func release(_ button: Int) -> Release {
        guard let current = pressed.removeValue(forKey: button) else { return .ignored }
        return current.active ? .held : .short
    }

    mutating func reset() -> Bool {
        let hadActive = pressed.values.contains { $0.active }
        for (button, press) in pressed {
            cancelledReleases[button] = press.active ? .held : .short
        }
        pressed.removeAll()
        return hadActive
    }

    mutating func takeCancelledRelease(_ button: Int) -> Release {
        cancelledReleases.removeValue(forKey: button) ?? .ignored
    }

    mutating func clear() -> Bool {
        let hadActive = pressed.values.contains { $0.active }
        pressed.removeAll()
        cancelledReleases.removeAll()
        return hadActive
    }
}

private let sideReplayMarker: Int64 = 0x5652_5342
private let mouseTapCallback: CGEventTapCallBack = { _, type, event, userData in
    guard let userData else { return Unmanaged.passUnretained(event) }
    let driver = Unmanaged<SideMouseButtonDriver>.fromOpaque(userData).takeUnretainedValue()
    // The tap is installed on the main run loop. All event state belongs to the main actor.
    return MainActor.assumeIsolated { driver.handle(type: type, event: event) }
}

/// Intercepts only bound side buttons. A short press is replayed as the original
/// down/up pair; a hold is consumed and routed to the native voice workflow.
/// With no event tap, the OS retains normal mouse behavior.
@MainActor
public final class SideMouseButtonDriver {
    private let threshold: TimeInterval
    private let onPress: @MainActor (SideMouseAction, Int32?) -> Void
    private let onPreviewDismissed: @MainActor () -> Void
    private let onHold: @MainActor (SideMouseAction, Int32?) -> Void
    private let onRelease: @MainActor (SideMouseAction) -> Void
    private let onCancel: @MainActor () -> Void
    private let onObservedButton: @MainActor (Int) -> Void

    private var tap: CFMachPort?
    private var source: CFRunLoopSource?
    private var state = SideButtonHoldState()
    private var downEvents: [Int: CGEvent] = [:]
    private var targetPIDs: [Int: Int32] = [:]
    private var voicePaused = false
    private var bindings: SideMouseBindings

    public func setBindings(_ next: SideMouseBindings) {
        guard next.isValid, bindings != next else { return }
        let pending = state.hasPendingPress
        let active = state.reset()
        // Keep the intercepted down event until its physical up arrives,
        // even if this button becomes unbound during the press.
        targetPIDs.removeAll()
        bindings = next
        if active { onCancel() }
        else if pending { onPreviewDismissed() }
    }

    public func setVoicePaused(_ paused: Bool) {
        voicePaused = paused
        guard paused else { return }
        let pending = state.hasPendingPress
        let active = state.reset()
        // A captured short press still needs its original click replayed on up.
        targetPIDs.removeAll()
        if active { onCancel() }
        else if pending { onPreviewDismissed() }
    }

    public init(
        threshold: TimeInterval = SideHold.threshold,
        bindings: SideMouseBindings = SideMouseBindings(),
        onPress: @escaping @MainActor (SideMouseAction, Int32?) -> Void = { _, _ in },
        onPreviewDismissed: @escaping @MainActor () -> Void = {},
        onHold: @escaping @MainActor (SideMouseAction, Int32?) -> Void,
        onRelease: @escaping @MainActor (SideMouseAction) -> Void,
        onCancel: @escaping @MainActor () -> Void,
        onObservedButton: @escaping @MainActor (Int) -> Void = { _ in }
    ) {
        self.threshold = threshold
        self.bindings = bindings
        self.onPress = onPress
        self.onPreviewDismissed = onPreviewDismissed
        self.onHold = onHold
        self.onRelease = onRelease
        self.onCancel = onCancel
        self.onObservedButton = onObservedButton
    }

    public func start() throws {
        guard AXIsProcessTrusted() else { throw SideMouseFailure.accessibilityDenied }
        if let tap {
            guard CFMachPortIsValid(tap) else { throw SideMouseFailure.eventTapUnavailable }
            if !CGEvent.tapIsEnabled(tap: tap) {
                CGEvent.tapEnable(tap: tap, enable: true)
            }
            guard CGEvent.tapIsEnabled(tap: tap) else {
                throw SideMouseFailure.eventTapUnavailable
            }
            return
        }
        let mask = (CGEventMask(1) << CGEventType.otherMouseDown.rawValue)
            | (CGEventMask(1) << CGEventType.otherMouseUp.rawValue)
        guard let created = CGEvent.tapCreate(
            tap: .cgSessionEventTap, place: .headInsertEventTap,
            options: .defaultTap, eventsOfInterest: mask,
            callback: mouseTapCallback, userInfo: Unmanaged.passUnretained(self).toOpaque()
        ) else {
            throw SideMouseFailure.eventTapUnavailable
        }
        guard let runLoopSource = CFMachPortCreateRunLoopSource(kCFAllocatorDefault, created, 0) else {
            CFMachPortInvalidate(created)
            throw SideMouseFailure.eventTapUnavailable
        }
        tap = created
        source = runLoopSource
        CFRunLoopAddSource(CFRunLoopGetMain(), runLoopSource, .commonModes)
        CGEvent.tapEnable(tap: created, enable: true)
        guard CGEvent.tapIsEnabled(tap: created) else {
            shutdown()
            throw SideMouseFailure.eventTapUnavailable
        }
    }

    public func shutdown() {
        let pending = state.hasPendingPress
        let active = state.clear()
        downEvents.removeAll()
        targetPIDs.removeAll()
        if let tap { CGEvent.tapEnable(tap: tap, enable: false) }
        if let source { CFRunLoopRemoveSource(CFRunLoopGetMain(), source, .commonModes) }
        if let tap { CFMachPortInvalidate(tap) }
        source = nil
        tap = nil
        if active { onCancel() }
        else if pending { onPreviewDismissed() }
    }

    fileprivate func handle(type: CGEventType, event: CGEvent) -> Unmanaged<CGEvent>? {
        if type == .tapDisabledByTimeout || type == .tapDisabledByUserInput {
            let pending = state.hasPendingPress
            let active = state.clear()
            downEvents.removeAll()
            targetPIDs.removeAll()
            DispatchQueue.main.async { [weak self] in
                if active { self?.onCancel() }
                else if pending { self?.onPreviewDismissed() }
            }
            if type == .tapDisabledByTimeout, let tap {
                CGEvent.tapEnable(tap: tap, enable: true)
            }
            return Unmanaged.passUnretained(event)
        }
        if event.getIntegerValueField(.eventSourceUserData) == sideReplayMarker {
            return Unmanaged.passUnretained(event)
        }
        let button = Int(event.getIntegerValueField(.mouseEventButtonNumber))
        // A pause or binding change can cancel a pending press after its down
        // event was suppressed. Replay that click on release, even if unbound.
        if type == .otherMouseUp {
            switch state.takeCancelledRelease(button) {
            case .short:
                targetPIDs.removeValue(forKey: button)
                if let down = downEvents.removeValue(forKey: button) {
                    replay(down: down, up: event)
                }
                return nil
            case .held:
                targetPIDs.removeValue(forKey: button)
                downEvents.removeValue(forKey: button)
                return nil
            case .ignored:
                break
            }
        }
        if voicePaused {
            return Unmanaged.passUnretained(event)
        }
        if type == .otherMouseDown && button >= 3 {
            DispatchQueue.main.async { [weak self] in self?.onObservedButton(button) }
        }
        guard let action = bindings.action(for: button) else {
            return Unmanaged.passUnretained(event)
        }
        switch type {
        case .otherMouseDown:
            guard let token = state.press(button) else { return nil }
            // A CGEventTap must return quickly. SwiftUI/AppKit panels are shown
            // after the callback, only if the same short-press is still pending.
            let targetPID = NSWorkspace.shared.frontmostApplication?.processIdentifier
            downEvents[button] = event.copy()
            targetPIDs[button] = targetPID
            DispatchQueue.main.async { [weak self] in
                guard let self, !self.voicePaused,
                      let press = self.state.pressed[button],
                      press.token == token, !press.active else { return }
                self.onPress(action, targetPID)
            }
            DispatchQueue.main.asyncAfter(deadline: .now() + threshold) { [weak self] in
                guard let self, self.state.activate(button, token: token) else { return }
                self.downEvents.removeValue(forKey: button)
                self.onHold(action, self.targetPIDs[button])
            }
            return nil
        case .otherMouseUp:
            let release = state.release(button)
            let down = downEvents.removeValue(forKey: button)
            targetPIDs.removeValue(forKey: button)
            switch release {
            case .short:
                DispatchQueue.main.async { [weak self] in self?.onPreviewDismissed() }
                if let down { replay(down: down, up: event) }
            case .held:
                DispatchQueue.main.async { [weak self] in self?.onRelease(action) }
            case .ignored:
                return Unmanaged.passUnretained(event)
            }
            return nil
        default:
            return Unmanaged.passUnretained(event)
        }
    }

    private func replay(down: CGEvent, up: CGEvent) {
        // Tag synthetic clicks so the event tap passes them through exactly once.
        for original in [down, up] {
            guard let event = original.copy() else { continue }
            event.setIntegerValueField(.eventSourceUserData, value: sideReplayMarker)
            event.post(tap: .cghidEventTap)
        }
    }

    deinit {
        MainActor.assumeIsolated { shutdown() }
    }
}
#endif
