#if os(macOS)
import AppKit
import ApplicationServices
import Carbon.HIToolbox
import Foundation

/// Ordinary dictation targets the foreground application, not an AX text model.
/// A readable AX focus is useful evidence, but lack of AX support never vetoes
/// an otherwise valid keyboard paste.
public struct MacDictationTarget: @unchecked Sendable {
    public let processID: pid_t
    fileprivate let focusedElement: AXUIElement?

    fileprivate init(processID: pid_t, focusedElement: AXUIElement?) {
        self.processID = processID
        self.focusedElement = focusedElement
    }
}

public enum DictationTargetPolicy {
    public static func maySend(capturedPID: Int32, foregroundPID: Int32?, secureInput: Bool) -> Bool {
        capturedPID > 0 && foregroundPID == capturedPID && !secureInput
    }
}

/// The only normal dictation writer. Selected-text edits retain the separate
/// strict AccessibilityClient and InteractionSession authorization.
@MainActor
public final class MacDictationDelivery {
    private let clipboard = MacClipboardTransactions.shared

    public init() {}

    private func focusedElement(in pid: pid_t) -> AXUIElement? {
        guard AXIsProcessTrusted() else { return nil }
        let application = AXUIElementCreateApplication(pid)
        AXUIElementSetMessagingTimeout(application, 0.25)
        var value: CFTypeRef?
        guard AXUIElementCopyAttributeValue(application, kAXFocusedUIElementAttribute as CFString, &value) == .success,
              let value, CFGetTypeID(value) == AXUIElementGetTypeID() else { return nil }
        return value as! AXUIElement
    }

    private func secureField(_ focused: AXUIElement) -> Bool {
        for key in [kAXRoleAttribute, kAXSubroleAttribute] {
            var value: CFTypeRef?
            if AXUIElementCopyAttributeValue(focused, key as CFString, &value) == .success,
               let name = value as? String,
               ["AXSecureTextField", "AXPasswordField"].contains(name) {
                return true
            }
        }
        for key in ["AXProtectedContent", "AXPassword", "AXIsPassword", "AXSecure"] {
            var value: CFTypeRef?
            if AXUIElementCopyAttributeValue(focused, key as CFString, &value) == .success,
               let flag = value as? Bool, flag { return true }
        }
        return false
    }

    public func capture(processID: Int32) throws -> MacDictationTarget {
        guard DictationTargetPolicy.maySend(
            capturedPID: processID,
            foregroundPID: NSWorkspace.shared.frontmostApplication?.processIdentifier,
            secureInput: IsSecureEventInputEnabled()) else {
            throw InteractionFailure.applicationChanged
        }
        let focused = focusedElement(in: processID)
        if let focused, secureField(focused) { throw InteractionFailure.secureField }
        return MacDictationTarget(processID: processID, focusedElement: focused)
    }

    private func revalidate(_ target: MacDictationTarget) throws {
        guard DictationTargetPolicy.maySend(
            capturedPID: target.processID,
            foregroundPID: NSWorkspace.shared.frontmostApplication?.processIdentifier,
            secureInput: IsSecureEventInputEnabled()) else {
            throw InteractionFailure.applicationChanged
        }
        if let current = focusedElement(in: target.processID) {
            if secureField(current) { throw InteractionFailure.secureField }
            if let original = target.focusedElement, !CFEqual(original, current) {
                throw InteractionFailure.focusChanged
            }
        }
    }

    private func requireKeyboardEventPermission() throws {
        guard CGPreflightPostEventAccess() else {
            throw InteractionFailure.keyboardEventPermissionDenied
        }
    }

    private func postPaste() throws {
        try requireKeyboardEventPermission()
        guard let source = CGEventSource(stateID: .hidSystemState),
              let down = CGEvent(keyboardEventSource: source, virtualKey: CGKeyCode(kVK_ANSI_V), keyDown: true),
              let up = CGEvent(keyboardEventSource: source, virtualKey: CGKeyCode(kVK_ANSI_V), keyDown: false) else {
            throw InteractionFailure.unsupportedNativeWrite
        }
        down.flags = .maskCommand
        up.flags = .maskCommand
        down.post(tap: .cgSessionEventTap)
        up.post(tap: .cgSessionEventTap)
    }

    /// Direct Unicode is a fallback *before* a paste is posted, when the
    /// clipboard cannot be faithfully snapshotted or another lease is active.
    /// Never retry after a possibly delivered paste.
    private func typeUnicode(_ text: String, to target: MacDictationTarget,
                             stillValid: () -> Bool) throws {
        try requireKeyboardEventPermission()
        guard let source = CGEventSource(stateID: .hidSystemState) else {
            throw InteractionFailure.unsupportedNativeWrite
        }
        let units = Array(text.utf16)
        var offset = 0
        while offset < units.count {
            guard stillValid() else { throw InteractionFailure.cancelled }
            try revalidate(target)
            var end = min(offset + 16, units.count)
            if end < units.count && end > offset &&
                (0xD800...0xDBFF).contains(units[end - 1]) &&
                (0xDC00...0xDFFF).contains(units[end]) { end -= 1 }
            guard end > offset,
                  let down = CGEvent(keyboardEventSource: source, virtualKey: 0, keyDown: true),
                  let up = CGEvent(keyboardEventSource: source, virtualKey: 0, keyDown: false) else {
                throw InteractionFailure.unsupportedNativeWrite
            }
            let chunk = Array(units[offset..<end])
            chunk.withUnsafeBufferPointer { buffer in
                down.keyboardSetUnicodeString(stringLength: buffer.count, unicodeString: buffer.baseAddress!)
                up.keyboardSetUnicodeString(stringLength: buffer.count, unicodeString: buffer.baseAddress!)
            }
            down.post(tap: .cgSessionEventTap)
            up.post(tap: .cgSessionEventTap)
            offset = end
        }
    }

    private func waitForModifierRelease(stillValid: () -> Bool) async throws {
        let timeout = ProcessInfo.processInfo.systemUptime + 2.0
        let modifiers: CGEventFlags = [.maskCommand, .maskAlternate, .maskControl, .maskShift]
        while !CGEventSource.flagsState(.hidSystemState).intersection(modifiers).isEmpty {
            guard stillValid() else { throw InteractionFailure.cancelled }
            guard ProcessInfo.processInfo.systemUptime < timeout else {
                throw InteractionFailure.modifiersHeld
            }
            try await Task.sleep(nanoseconds: 25_000_000)
        }
    }

    public func deliver(_ text: String, to target: MacDictationTarget,
                        stillValid: () -> Bool) async throws -> DeliveryState {
        guard !text.isEmpty else { throw InteractionFailure.invalidSelection }
        try await waitForModifierRelease(stillValid: stillValid)
        guard stillValid() else { throw InteractionFailure.cancelled }
        try revalidate(target)
        // Only unavailable clipboard formats justify using a different input
        // mechanism. A concurrent paste must wait for its existing lease.
        // Never switch methods solely because another transcript is pending.
        let lease: UUID
        let deadline = ProcessInfo.processInfo.systemUptime +
            Double(PasteTiming.maxMilliseconds + PasteTiming.minimumMilliseconds) / 1000
        while true {
            guard stillValid() else { throw InteractionFailure.cancelled }
            try revalidate(target)
            do {
                lease = try clipboard.beginTemporaryText(text)
                break
            } catch ClipboardFailure.busy {
                guard ProcessInfo.processInfo.systemUptime < deadline else {
                    throw ClipboardFailure.busy
                }
                try await Task.sleep(nanoseconds: 50_000_000)
            } catch ClipboardFailure.snapshotUnavailable {
                try typeUnicode(text, to: target, stillValid: stillValid)
                return .sentUnverified
            }
        }
        do {
            try revalidate(target)
            guard clipboard.isOwned(lease) else { throw ClipboardFailure.ownershipLost }
            guard stillValid() else { throw InteractionFailure.cancelled }
            // Start before posting: a fast editor may read the promised text
            // synchronously, before CGEvent.post returns.
            let postingStartedAt = ProcessInfo.processInfo.systemUptime
            try postPaste()
            // A queued Cmd+V is not a consumption receipt. Prefer a real paste
            // read notification; otherwise retain the transcript until timeout.
            clipboard.restoreAfterPaste(lease, postingStartedAt: postingStartedAt)
            return .sentUnverified
        } catch {
            try? clipboard.finish(lease)
            throw error
        }
    }
}
#endif
