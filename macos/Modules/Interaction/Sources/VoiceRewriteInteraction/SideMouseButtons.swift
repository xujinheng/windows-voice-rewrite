#if os(macOS)
import AppKit
import ApplicationServices
import CoreGraphics
import Foundation

public enum SideMouseAction: Int, Sendable {
    case dictation = 3   // Mouse4: the first auxiliary button.
    case rewrite = 4     // Mouse5: the second auxiliary button.
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
    private var sequence: UInt64 = 0

    mutating func press(_ button: Int) -> UInt64? {
        guard pressed[button] == nil, sequence < UInt64.max else { return nil }
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
        pressed.removeAll()
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
    private let onHold: @MainActor (SideMouseAction, Int32?) -> Void
    private let onRelease: @MainActor (SideMouseAction) -> Void
    private let onCancel: @MainActor () -> Void

    private var tap: CFMachPort?
    private var source: CFRunLoopSource?
    private var state = SideButtonHoldState()
    private var downEvents: [Int: CGEvent] = [:]
    private var targetPIDs: [Int: Int32] = [:]

    public init(
        threshold: TimeInterval = 0.28,
        onHold: @escaping @MainActor (SideMouseAction, Int32?) -> Void,
        onRelease: @escaping @MainActor (SideMouseAction) -> Void,
        onCancel: @escaping @MainActor () -> Void
    ) {
        self.threshold = threshold
        self.onHold = onHold
        self.onRelease = onRelease
        self.onCancel = onCancel
    }

    public func start() throws {
        guard tap == nil else { return }
        guard AXIsProcessTrusted() else { throw SideMouseFailure.accessibilityDenied }
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
    }

    public func shutdown() {
        let active = state.reset()
        downEvents.removeAll()
        targetPIDs.removeAll()
        if let tap { CGEvent.tapEnable(tap: tap, enable: false) }
        if let source { CFRunLoopRemoveSource(CFRunLoopGetMain(), source, .commonModes) }
        if let tap { CFMachPortInvalidate(tap) }
        source = nil
        tap = nil
        if active { onCancel() }
    }

    fileprivate func handle(type: CGEventType, event: CGEvent) -> Unmanaged<CGEvent>? {
        if type == .tapDisabledByTimeout || type == .tapDisabledByUserInput {
            let active = state.reset()
            downEvents.removeAll()
            targetPIDs.removeAll()
            if active { onCancel() }
            if type == .tapDisabledByTimeout, let tap {
                CGEvent.tapEnable(tap: tap, enable: true)
            }
            return Unmanaged.passUnretained(event)
        }
        if event.getIntegerValueField(.eventSourceUserData) == sideReplayMarker {
            return Unmanaged.passUnretained(event)
        }
        let button = Int(event.getIntegerValueField(.mouseEventButtonNumber))
        guard let action = SideMouseAction(rawValue: button) else {
            return Unmanaged.passUnretained(event)
        }
        switch type {
        case .otherMouseDown:
            guard let token = state.press(button) else { return nil }
            downEvents[button] = event.copy()
            targetPIDs[button] = NSWorkspace.shared.frontmostApplication?.processIdentifier
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
                if let down { replay(down: down, up: event) }
            case .held:
                onRelease(action)
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
