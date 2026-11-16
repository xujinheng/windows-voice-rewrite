#if os(macOS)
import AppKit
import ApplicationServices
import Carbon.HIToolbox
import Foundation

/// Actual CFEqual on AX handles is required: role, title, PID and hashes alone are not identity.
private struct AXFocusIdentity: FocusIdentity, @unchecked Sendable {
    let element: AXUIElement
    func isSameElement(as other: any FocusIdentity) -> Bool {
        guard let other = other as? AXFocusIdentity else { return false }
        return CFEqual(element, other.element)
    }
}

/// AX observer callbacks and NSWorkspace notifications can arrive on another thread.
private final class RevisionTracker: @unchecked Sendable {
    private let lock = NSLock()
    private var value: UInt64 = 0
    var revision: UInt64 {
        lock.lock(); defer { lock.unlock() }
        return value
    }
    func invalidate() {
        lock.lock(); defer { lock.unlock() }
        // Saturation is fail-closed (new observations stay at max).
        if value < UInt64.max { value += 1 }
    }
}

private let monitoredAXChange: AXObserverCallback = { _, _, _, opaque in
    guard let opaque else { return }
    Unmanaged<RevisionTracker>.fromOpaque(opaque).takeUnretainedValue().invalidate()
}

/// Internal to prevent bypassing InteractionSession's generation/selection authorization.
/// A new instance should be owned by exactly one InteractionSession actor.
final class MacAccessibilityClient: AccessibilityClient, @unchecked Sendable {
    private let tracker = RevisionTracker()
    private var observer: AXObserver?
    private var observedApplication: AXUIElement?
    private var observedElement: AXUIElement?
    private var workspaceObserver: NSObjectProtocol?

    init() {}

    static func accessibilityPermissionGranted(prompt: Bool = false) -> Bool {
        if prompt {
            // Prompt is opt-in; never surprise the user during ordinary calls.
            return AXIsProcessTrustedWithOptions(["AXTrustedCheckOptionPrompt": true] as CFDictionary)
        }
        return AXIsProcessTrusted()
    }

    private func readAttribute(_ element: AXUIElement, _ name: String) throws -> CFTypeRef {
        var result: CFTypeRef?
        let status = AXUIElementCopyAttributeValue(element, name as CFString, &result)
        guard status == .success, let result else {
            if status == .apiDisabled { throw InteractionFailure.accessibilityPermissionDenied }
            if status == .attributeUnsupported || status == .noValue {
                throw InteractionFailure.selectionUnavailable
            }
            throw InteractionFailure.systemError(status.rawValue)
        }
        return result
    }

    private func optionalAttribute(_ element: AXUIElement, _ name: String) throws -> CFTypeRef? {
        var result: CFTypeRef?
        let status = AXUIElementCopyAttributeValue(element, name as CFString, &result)
        if status == .attributeUnsupported || status == .noValue { return nil }
        guard status == .success else {
            throw status == .apiDisabled ? InteractionFailure.accessibilityPermissionDenied
                                         : InteractionFailure.systemError(status.rawValue)
        }
        return result
    }

    private func read() throws -> SelectionObservation {
        guard Self.accessibilityPermissionGranted() else { throw InteractionFailure.accessibilityPermissionDenied }
        guard let front = NSWorkspace.shared.frontmostApplication else {
            throw InteractionFailure.noFrontmostApplication
        }
        let pid = front.processIdentifier
        guard pid > 0 else { throw InteractionFailure.noFrontmostApplication }
        // An active secure-input session can hide a password field's AX subrole.
        guard !IsSecureEventInputEnabled() else { throw InteractionFailure.secureField }

        let app = AXUIElementCreateApplication(pid)
        AXUIElementSetMessagingTimeout(app, 0.4)
        let focusedRef: CFTypeRef
        do { focusedRef = try readAttribute(app, kAXFocusedUIElementAttribute) }
        catch InteractionFailure.selectionUnavailable { throw InteractionFailure.noFocusedElement }
        guard CFGetTypeID(focusedRef) == AXUIElementGetTypeID() else {
            throw InteractionFailure.noFocusedElement
        }
        let focused = (focusedRef as! AXUIElement)
        AXUIElementSetMessagingTimeout(focused, 0.4)
        var focusedPID: pid_t = 0
        let pidStatus = AXUIElementGetPid(focused, &focusedPID)
        guard pidStatus == .success else { throw InteractionFailure.systemError(pidStatus.rawValue) }
        guard focusedPID == pid else { throw InteractionFailure.applicationChanged }

        // Do not read the actual contents until secure-field checks finish.
        let role = (try readAttribute(focused, kAXRoleAttribute)) as? String ?? ""
        let subrole = (try optionalAttribute(focused, kAXSubroleAttribute)) as? String ?? ""
        guard !role.isEmpty else { throw InteractionFailure.noFocusedElement }
        if role == "AXSecureTextField" || subrole == "AXSecureTextField" ||
            role == "AXPasswordField" || subrole == "AXPasswordField" {
            throw InteractionFailure.secureField
        }
        for flag in ["AXProtectedContent", "AXPassword", "AXIsPassword", "AXSecure"] {
            if let protected = try optionalAttribute(focused, flag) as? Bool, protected {
                throw InteractionFailure.secureField
            }
        }
        if let enabled = try optionalAttribute(focused, kAXEnabledAttribute) as? Bool, !enabled {
            throw InteractionFailure.notEditable
        }
        guard role == "AXTextArea" || role == "AXTextField" || role == "AXComboBox" ||
              role == "AXSearchField" else {
            throw InteractionFailure.notEditable
        }
        let rangeValue = try readAttribute(focused, kAXSelectedTextRangeAttribute)
        guard CFGetTypeID(rangeValue) == AXValueGetTypeID() else {
            throw InteractionFailure.selectionUnavailable
        }
        let axValue = rangeValue as! AXValue
        guard AXValueGetType(axValue) == .cfRange else { throw InteractionFailure.selectionUnavailable }
        var range = CFRange(location: 0, length: 0)
        guard AXValueGetValue(axValue, .cfRange, &range) else {
            throw InteractionFailure.selectionUnavailable
        }
        // Some editable controls report no selected-text value for an empty caret.
        // This is safe only when the independently read AX range is zero-length.
        let selected = try optionalAttribute(focused, kAXSelectedTextAttribute)
        let text: String
        if let value = selected as? String {
            text = value
        } else if selected == nil && range.length == 0 {
            text = ""
        } else {
            throw InteractionFailure.selectionUnavailable
        }
        // Browser location bars may support a readable value while omitting
        // selected-text and value-change notifications. Capture the entire
        // value so a changed URL/search query prevents later insertion.
        let fullValue = (try? optionalAttribute(focused, kAXValueAttribute)) as? String
        return SelectionObservation(processID: pid,
                                    identity: AXFocusIdentity(element: focused),
                                    identityDescription: "AX(pid:\(pid),hash:\(CFHash(focused)))",
                                    selectedText: text,
                                    range: TextRange(location: range.location, length: range.length),
                                    isEditable: true, isSecure: false,
                                    revision: tracker.revision,
                                    fullValue: fullValue)
    }

    func captureMonitoredSelection() throws -> SelectionObservation {
        stopMonitoring()
        let before = try read()
        guard let identity = before.identity as? AXFocusIdentity else {
            throw InteractionFailure.noFocusedElement
        }
        var created: AXObserver?
        let result = AXObserverCreate(before.processID, monitoredAXChange, &created)
        guard result == .success, let created else { throw InteractionFailure.monitoringUnavailable }
        let app = AXUIElementCreateApplication(before.processID)
        let ptr = Unmanaged.passUnretained(tracker).toOpaque()
        let appStatus = AXObserverAddNotification(created, app,
                                kAXFocusedUIElementChangedNotification as CFString, ptr)
        // A selection change can be signaled via either notification, depending on app.
        let textStatus = AXObserverAddNotification(created, identity.element,
                                kAXSelectedTextChangedNotification as CFString, ptr)
        let valueStatus = AXObserverAddNotification(created, identity.element,
                                kAXValueChangedNotification as CFString, ptr)
        // Some browser address bars do not publish text-change notifications.
        // In that case, keep observing app focus and require a readable full
        // value that is compared again immediately before any write.
        let hasTextNotifications = textStatus == .success || valueStatus == .success
        let unsupportedTextNotifications =
            (textStatus == .notificationUnsupported || textStatus == .notImplemented) &&
            (valueStatus == .notificationUnsupported || valueStatus == .notImplemented)
        guard canMonitorSelection(
            focusNotifications: appStatus == .success,
            textNotifications: hasTextNotifications,
            unsupportedTextNotifications: unsupportedTextNotifications,
            fullValue: before.fullValue) else {
            throw InteractionFailure.monitoringUnavailable
        }
        observer = created
        observedApplication = app
        observedElement = identity.element
        CFRunLoopAddSource(CFRunLoopGetMain(), AXObserverGetRunLoopSource(created), CFRunLoopMode.commonModes)
        workspaceObserver = NSWorkspace.shared.notificationCenter.addObserver(
            forName: NSWorkspace.didActivateApplicationNotification, object: nil, queue: nil
        ) { [tracker] _ in tracker.invalidate() }
        // Check again after monitoring is installed to close the setup window.
        do {
            let after = try read()
            guard before.processID == after.processID,
                  before.identity.isSameElement(as: after.identity),
                  before.range == after.range,
                  before.selectedText == after.selectedText,
                  before.fullValue == nil || before.fullValue == after.fullValue else {
                throw InteractionFailure.selectionChanged
            }
            return after
        } catch {
            stopMonitoring()
            throw error
        }
    }

    func currentSelection() throws -> SelectionObservation { try read() }

    private let pasteboard = ClipboardProtector(board: MacClipboardBoard())

    private func revalidate(_ expected: SelectionObservation) throws -> AXUIElement {
        let now = try read()
        guard now.processID == expected.processID else { throw InteractionFailure.applicationChanged }
        guard now.identity.isSameElement(as: expected.identity) else { throw InteractionFailure.focusChanged }
        guard now.revision == expected.revision, now.range == expected.range,
              now.selectedText == expected.selectedText,
              expected.fullValue == nil || now.fullValue == expected.fullValue else {
            throw InteractionFailure.selectionChanged
        }
        guard let target = (expected.identity as? AXFocusIdentity)?.element else {
            throw InteractionFailure.focusChanged
        }
        return target
    }

    private func wholeValue(_ target: AXUIElement) throws -> String? {
        try optionalAttribute(target, kAXValueAttribute) as? String
    }

    // Verify only the target's actual focused AX element after an attempted write.
    // The selected range is expected to change, so pre-write validation cannot
    // be reused as the post-write receipt.
    private func confirmWrite(_ target: AXUIElement, expected: SelectionObservation,
                              expectedValue: String) -> Bool {
        let deadline = ProcessInfo.processInfo.systemUptime + 1.25
        repeat {
            guard Self.accessibilityPermissionGranted(),
                  NSWorkspace.shared.frontmostApplication?.processIdentifier == expected.processID,
                  !IsSecureEventInputEnabled() else { return false }
            let app = AXUIElementCreateApplication(expected.processID)
            AXUIElementSetMessagingTimeout(app, 0.4)
            guard let focused = try? readAttribute(app, kAXFocusedUIElementAttribute),
                  CFGetTypeID(focused) == AXUIElementGetTypeID(),
                  CFEqual(focused, target) else { return false }
            if let value = try? wholeValue(target),
               TextWriteVerification.confirms(actual: value, expected: expectedValue) {
                return true
            }
            Thread.sleep(forTimeInterval: 0.025)
        } while ProcessInfo.processInfo.systemUptime < deadline
        return false
    }

    private func postPaste(expected: SelectionObservation) throws {
        // Foreground delivery is required by several editors, including native and
        // Chromium text controls. Never post into an unverified focus.
        _ = try revalidate(expected)
        guard NSWorkspace.shared.frontmostApplication?.processIdentifier == expected.processID else {
            throw InteractionFailure.applicationChanged
        }
        guard let source = CGEventSource(stateID: .hidSystemState),
              let down = CGEvent(keyboardEventSource: source, virtualKey: CGKeyCode(kVK_ANSI_V), keyDown: true),
              let up = CGEvent(keyboardEventSource: source, virtualKey: CGKeyCode(kVK_ANSI_V), keyDown: false) else {
            throw InteractionFailure.writeNotConfirmed
        }
        down.flags = .maskCommand
        up.flags = .maskCommand
        down.post(tap: .cgSessionEventTap)
        up.post(tap: .cgSessionEventTap)
    }

    private func guardedPaste(_ text: String, expected: SelectionObservation,
                              expectedValue: String) throws {
        let lease = try pasteboard.beginTemporaryText(text)
        do {
            // Clipboard staging is externally visible. Revalidate again after staging.
            let target = try revalidate(expected)
            guard pasteboard.isOwned(lease) else { throw ClipboardFailure.ownershipLost }
            try postPaste(expected: expected)

            // A posted paste is not proof of delivery. Allow the editor time to
            // publish the new AXValue without posting a second keystroke.
            let confirmed = confirmWrite(target, expected: expected, expectedValue: expectedValue)

            // Never overwrite a user or clipboard manager's newer clipboard contents.
            try pasteboard.finish(lease)
            guard confirmed else { throw InteractionFailure.writeNotConfirmed }
        } catch {
            // A failed/uncertain paste may still have reached the target: never retry.
            try? pasteboard.finish(lease)
            throw error
        }
    }

    func writeSelectedText(_ text: String, expected: SelectionObservation) throws {
        let target = try revalidate(expected)
        let baseline = try wholeValue(target)
        guard let expectedValue = TextWriteVerification.expectedValue(
            original: baseline, selectedText: expected.selectedText,
            range: expected.range, replacement: text) else {
            throw InteractionFailure.unsupportedNativeWrite
        }

        var settable = DarwinBoolean(false)
        let support = AXUIElementIsAttributeSettable(
            target, kAXSelectedTextAttribute as CFString, &settable)
        if support == .success && settable.boolValue {
            let code = AXUIElementSetAttributeValue(target, kAXSelectedTextAttribute as CFString, text as CFString)
            if code == .success {
                // Some browsers update AXValue after SetAttributeValue returns.
                // Confirm asynchronously visible content before reporting failure;
                // an unconfirmed write is never retried with a paste.
                guard confirmWrite(target, expected: expected, expectedValue: expectedValue) else {
                    throw InteractionFailure.writeNotConfirmed
                }
                return
            }
            // A browser may report a transient AX error even when the edit
            // is applied. Confirm the exact expected text before giving up,
            // but never repeat a write whose outcome is uncertain.
            if code == .apiDisabled { throw InteractionFailure.accessibilityPermissionDenied }
            if code != .attributeUnsupported && code != .notImplemented && code != .noValue {
                if confirmWrite(target, expected: expected, expectedValue: expectedValue) { return }
                throw InteractionFailure.writeNotConfirmed
            }
        } else if support != .success && support != .attributeUnsupported &&
                    support != .notImplemented && support != .noValue {
            throw support == .apiDisabled ? InteractionFailure.accessibilityPermissionDenied
                                         : InteractionFailure.systemError(support.rawValue)
        }

        // Only paste when the entire original value and UTF-16 range can be verified.
        // Unsupported web editors without readable AXValue remain preview/copy only.
        try guardedPaste(text, expected: expected, expectedValue: expectedValue)
    }

    func stopMonitoring() {
        if let observer {
            CFRunLoopRemoveSource(CFRunLoopGetMain(), AXObserverGetRunLoopSource(observer), CFRunLoopMode.commonModes)
        }
        if let workspaceObserver {
            NSWorkspace.shared.notificationCenter.removeObserver(workspaceObserver)
        }
        observer = nil
        workspaceObserver = nil
        observedApplication = nil
        observedElement = nil
        tracker.invalidate()
    }

    deinit { stopMonitoring() }
}

public extension InteractionSession {
    /// Construct the production backend; no public raw write method is exposed.
    static func macOS() -> InteractionSession { InteractionSession(client: MacAccessibilityClient()) }

    /// Calling with prompt=true asks macOS to show the Accessibility permission prompt.
    static func accessibilityPermissionGranted(prompt: Bool = false) -> Bool {
        MacAccessibilityClient.accessibilityPermissionGranted(prompt: prompt)
    }
}
#endif
