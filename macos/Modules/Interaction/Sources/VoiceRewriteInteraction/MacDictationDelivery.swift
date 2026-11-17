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
    fileprivate let windowID: CGWindowID?

    fileprivate init(processID: pid_t, focusedElement: AXUIElement?, windowID: CGWindowID?) {
        self.processID = processID
        self.focusedElement = focusedElement
        self.windowID = windowID
    }
}

public enum DictationTargetPolicy {
    public static func maySend(capturedPID: Int32, foregroundPID: Int32?, secureInput: Bool) -> Bool {
        capturedPID > 0 && foregroundPID == capturedPID && !secureInput
    }

    /// Missing CGWindow information is not an AX permission gate for dictation.
    /// Distinct, known foreground windows must never be treated as one target.
    public static func sameWindow(captured: UInt32?, current: UInt32?) -> Bool {
        guard let captured, let current else { return true }
        return captured == current
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

    private func foregroundWindowID(for pid: pid_t) -> CGWindowID? {
        guard let windows = CGWindowListCopyWindowInfo(
            [.optionOnScreenOnly, .excludeDesktopElements], kCGNullWindowID) as? [[String: Any]] else {
            return nil
        }
        for window in windows {
            guard (window[kCGWindowOwnerPID as String] as? Int) == Int(pid),
                  (window[kCGWindowLayer as String] as? Int) == 0,
                  let number = window[kCGWindowNumber as String] as? Int, number > 0 else { continue }
            return CGWindowID(number)
        }
        return nil
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
        return MacDictationTarget(processID: processID, focusedElement: focused,
                                   windowID: foregroundWindowID(for: processID))
    }

    private func revalidate(_ target: MacDictationTarget) throws {
        guard DictationTargetPolicy.maySend(
            capturedPID: target.processID,
            foregroundPID: NSWorkspace.shared.frontmostApplication?.processIdentifier,
            secureInput: IsSecureEventInputEnabled()) else {
            throw InteractionFailure.applicationChanged
        }
        if !DictationTargetPolicy.sameWindow(
            captured: target.windowID, current: foregroundWindowID(for: target.processID)) {
            throw InteractionFailure.focusChanged
        }
        if let current = focusedElement(in: target.processID) {
            if secureField(current) { throw InteractionFailure.secureField }
            if let original = target.focusedElement, !CFEqual(original, current) {
                throw InteractionFailure.focusChanged
            }
        }
    }


    /// Best-effort readback evidence only. AX support must never become a
    /// prerequisite for ordinary dictation into Chrome, Electron or TextEdit.
    private func expectedTextAfterPaste(_ text: String, target: MacDictationTarget) -> String? {
        guard let element = target.focusedElement else { return nil }
        AXUIElementSetMessagingTimeout(element, 0.25)
        var rawValue: CFTypeRef?
        var rawRange: CFTypeRef?
        guard AXUIElementCopyAttributeValue(element, kAXValueAttribute as CFString, &rawValue) == .success,
              let original = rawValue as? String,
              AXUIElementCopyAttributeValue(element, kAXSelectedTextRangeAttribute as CFString, &rawRange) == .success,
              let rawRange, CFGetTypeID(rawRange) == AXValueGetTypeID() else { return nil }
        let axRange = rawRange as! AXValue
        guard AXValueGetType(axRange) == .cfRange else { return nil }
        var range = CFRange(location: 0, length: 0)
        guard AXValueGetValue(axRange, .cfRange, &range) else { return nil }
        var rawSelected: CFTypeRef?
        let selected = AXUIElementCopyAttributeValue(
            element, kAXSelectedTextAttribute as CFString, &rawSelected) == .success
            ? rawSelected as? String : nil
        let textRange = TextRange(location: range.location, length: range.length)
        guard let selectedText = TextWriteVerification.selectedText(
            reported: selected, fullValue: original, range: textRange) else { return nil }
        return TextWriteVerification.expectedValue(
            original: original, selectedText: selectedText,
            range: textRange, replacement: text)
    }

    private func matchesReadback(_ expected: String, target: MacDictationTarget) -> Bool {
        guard let original = target.focusedElement,
              DictationTargetPolicy.maySend(
                capturedPID: target.processID,
                foregroundPID: NSWorkspace.shared.frontmostApplication?.processIdentifier,
                secureInput: IsSecureEventInputEnabled()),
              let current = focusedElement(in: target.processID),
              CFEqual(original, current), !secureField(current),
              DictationTargetPolicy.sameWindow(
                captured: target.windowID, current: foregroundWindowID(for: target.processID)) else {
            return false
        }
        var value: CFTypeRef?
        guard AXUIElementCopyAttributeValue(current, kAXValueAttribute as CFString, &value) == .success,
              let actual = value as? String else { return false }
        return TextWriteVerification.confirms(actual: actual, expected: expected)
    }

    /// A posted event is never itself a success receipt. Check an exact AXValue
    /// when exposed by the same focused control, without making it a paste gate.
    private func outcomeAfterPosting(expected: String?, target: MacDictationTarget,
                                     stillValid: () -> Bool) async -> DeliveryState {
        guard let expected else { return .sentUnverified }
        for attempt in 0..<3 {
            guard stillValid(), !Task<Never, Never>.isCancelled else { return .sentUnverified }
            if matchesReadback(expected, target: target) { return .confirmed }
            if attempt < 2 {
                do { try await Task.sleep(nanoseconds: 150_000_000) }
                catch { return .sentUnverified }
            }
        }
        return .sentUnverified
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
        var postedAny = false
        do {
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
            postedAny = true
            up.post(tap: .cgSessionEventTap)
            offset = end
        }
        } catch {
            // A cancellation or focus change after a chunk was posted can
            // leave a partial insertion. Never call it a clean non-write.
            if postedAny { throw InteractionFailure.writeNotConfirmed }
            throw error
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
        let lease: UUID
        do {
            lease = try await clipboard.beginWhenAvailable(text, stillValid: stillValid)
        } catch ClipboardFailure.snapshotUnavailable {
            let expected = expectedTextAfterPaste(text, target: target)
            try typeUnicode(text, to: target, stillValid: stillValid)
            return await outcomeAfterPosting(expected: expected, target: target, stillValid: stillValid)
        }
        let expected = expectedTextAfterPaste(text, target: target)
        do {
            try revalidate(target)
            guard clipboard.isOwned(lease) else { throw ClipboardFailure.ownershipLost }
            guard stillValid() else { throw InteractionFailure.cancelled }
            // Start before posting: a fast editor may read the promised text
            // synchronously, before CGEvent.post returns.
            let postingStartedAt = ProcessInfo.processInfo.systemUptime
            try postPaste()
            // Release ownership asynchronously after the receiver's read or a
            // bounded timeout. Never restore immediately after posting.
            clipboard.restoreAfterPaste(lease, postingStartedAt: postingStartedAt)
        } catch {
            try? clipboard.finish(lease)
            throw error
        }
        return await outcomeAfterPosting(expected: expected, target: target, stillValid: stillValid)
    }
}
#endif
