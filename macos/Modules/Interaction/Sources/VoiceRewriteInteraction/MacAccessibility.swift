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
        var settable = DarwinBoolean(false)
        let writeSupport = AXUIElementIsAttributeSettable(focused, kAXSelectedTextAttribute as CFString, &settable)
        if writeSupport == .attributeUnsupported || writeSupport == .notImplemented ||
           writeSupport == .noValue {
            throw InteractionFailure.unsupportedNativeWrite
        }
        guard writeSupport == .success else {
            throw writeSupport == .apiDisabled ? InteractionFailure.accessibilityPermissionDenied
                                                : InteractionFailure.systemError(writeSupport.rawValue)
        }
        guard settable.boolValue else { throw InteractionFailure.unsupportedNativeWrite }

        let selected = try readAttribute(focused, kAXSelectedTextAttribute)
        guard let text = selected as? String else { throw InteractionFailure.selectionUnavailable }
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
        return SelectionObservation(processID: pid,
                                    identity: AXFocusIdentity(element: focused),
                                    identityDescription: "AX(pid:\(pid),hash:\(CFHash(focused)))",
                                    selectedText: text,
                                    range: TextRange(location: range.location, length: range.length),
                                    isEditable: true, isSecure: false,
                                    revision: tracker.revision)
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
        guard appStatus == .success,
              textStatus == .success || valueStatus == .success else {
            throw InteractionFailure.monitoringUnavailable
        }
        observer = created
        observedApplication = app
        observedElement = identity.element
        CFRunLoopAddSource(CFRunLoopGetMain(), AXObserverGetRunLoopSource(created), kCFRunLoopCommonModes)
        workspaceObserver = NSWorkspace.shared.notificationCenter.addObserver(
            forName: NSWorkspace.didActivateApplicationNotification, object: nil, queue: nil
        ) { [tracker] _ in tracker.invalidate() }
        // Check again after monitoring is installed to close the setup window.
        do {
            let after = try read()
            guard before.processID == after.processID,
                  before.identity.isSameElement(as: after.identity),
                  before.range == after.range,
                  before.selectedText == after.selectedText else {
                throw InteractionFailure.selectionChanged
            }
            return after
        } catch {
            stopMonitoring()
            throw error
        }
    }

    func currentSelection() throws -> SelectionObservation { try read() }

    func writeSelectedText(_ text: String, expected: SelectionObservation) throws {
        // This check is deliberately inside the native adapter as well as the actor guard.
        let now = try read()
        guard now.processID == expected.processID else { throw InteractionFailure.applicationChanged }
        guard now.identity.isSameElement(as: expected.identity) else { throw InteractionFailure.focusChanged }
        guard now.revision == expected.revision, now.range == expected.range,
              now.selectedText == expected.selectedText else {
            throw InteractionFailure.selectionChanged
        }
        guard let target = (expected.identity as? AXFocusIdentity)?.element else {
            throw InteractionFailure.focusChanged
        }
        let code = AXUIElementSetAttributeValue(target, kAXSelectedTextAttribute as CFString, text as CFString)
        switch code {
        case .success: return
        case .attributeUnsupported, .notImplemented, .noValue: throw InteractionFailure.unsupportedNativeWrite
        case .apiDisabled: throw InteractionFailure.accessibilityPermissionDenied
        default: throw InteractionFailure.systemError(code.rawValue)
        }
    }

    func stopMonitoring() {
        if let observer {
            CFRunLoopRemoveSource(CFRunLoopGetMain(), AXObserverGetRunLoopSource(observer), kCFRunLoopCommonModes)
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
