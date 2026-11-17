import Foundation

/// All edits must be authorized against a captured target. Go's may_attempt_replace is not authorization.
public enum InteractionFailure: Error, Equatable, Sendable {
    case accessibilityPermissionDenied
    case noFrontmostApplication
    case noFocusedElement
    case selectionUnavailable
    case invalidSelection
    case secureField
    case notEditable
    case unsupportedNativeWrite
    case writeNotConfirmed
    case monitoringUnavailable
    case applicationChanged
    case focusChanged
    case selectionChanged
    case staleGeneration
    case staleSnapshot
    case cancelled
    case timedOut
    case systemError(Int32)
}

/// A focused editor may lack text-change AX notifications, but an observed
/// app focus plus a readable complete value permits final snapshot validation.
/// Unreadable controls must keep notification monitoring or fall back to copy.
public func canMonitorSelection(focusNotifications: Bool, textNotifications: Bool,
                                unsupportedTextNotifications: Bool, fullValue: String?) -> Bool {
    focusNotifications && (textNotifications || (unsupportedTextNotifications && fullValue != nil))
}

public struct TextRange: Equatable, Sendable {
    public let location: Int
    public let length: Int

    public init(location: Int, length: Int) {
        self.location = location
        self.length = length
    }
}

/// Implementations must compare the real focused element, never only its role or label.
public protocol FocusIdentity: Sendable {
    func isSameElement(as other: any FocusIdentity) -> Bool
}

/// Useful for an injected fake backend, not a secure native element identity.
public struct StringFocusIdentity: FocusIdentity, Sendable {
    public let value: String
    public init(_ value: String) { self.value = value }
    public func isSameElement(as other: any FocusIdentity) -> Bool {
        (other as? StringFocusIdentity)?.value == value
    }
}

public struct SelectionObservation: Sendable {
    public let processID: Int32
    public let identity: any FocusIdentity
    public let identityDescription: String
    public let selectedText: String
    public let range: TextRange
    public let isEditable: Bool
    public let isSecure: Bool
    /// Incremented by the native monitoring backend on AX focus/selection/value changes or app activation.
    public let revision: UInt64
    /// Complete AX text value, when exposed by the control. Required if the
    /// control does not support selection/value change notifications.
    public let fullValue: String?

    public init(processID: Int32, identity: any FocusIdentity,
                identityDescription: String, selectedText: String, range: TextRange,
                isEditable: Bool, isSecure: Bool, revision: UInt64,
                fullValue: String? = nil) {
        self.processID = processID
        self.identity = identity
        self.identityDescription = identityDescription
        self.selectedText = selectedText
        self.range = range
        self.isEditable = isEditable
        self.isSecure = isSecure
        self.revision = revision
        self.fullValue = fullValue
    }
}

/// Native target validation and writes run synchronously inside InteractionSession's actor.
/// Implementations must fail closed if the target cannot be verified or written safely.
public protocol AccessibilityClient: Sendable {
    func captureMonitoredSelection() throws -> SelectionObservation
    func currentSelection() throws -> SelectionObservation
    func writeSelectedText(_ text: String, expected: SelectionObservation, intent: SelectionIntent) throws
    func stopMonitoring()
}

public enum SelectionIntent: Equatable, Sendable {
    case replaceSelection
    case insertAtCaret
}

/// Constructor intentionally internal: an LLM or caller cannot mint a valid snapshot.
public struct SelectionSnapshot: Sendable {
    public let id: UUID
    public let generation: UInt64
    public let processID: Int32
    public let focusedElementIdentity: String
    public let originalSelectedText: String
    public let range: TextRange
    public let intent: SelectionIntent
    fileprivate let evidence: SelectionObservation
}

/// A new generation invalidates all preceding snapshots, even if the app and text look identical.
/// Never `await` a network operation inside this actor: capture, then perform ASR/LLM elsewhere,
/// then call replaceIfCurrent/insertIfCurrent. AX validation and the AX write have no await gap.
public actor InteractionSession {
    private let client: any AccessibilityClient
    private let uptime: @Sendable () -> TimeInterval
    private var generation: UInt64 = 0
    private var deadline: TimeInterval = 0
    private var authorizedProcessID: Int32 = 0
    private var activeSnapshotID: UUID?
    private var cancelled = false

    public init(client: any AccessibilityClient,
                uptime: @escaping @Sendable () -> TimeInterval = { ProcessInfo.processInfo.systemUptime }) {
        self.client = client
        self.uptime = uptime
    }

    @discardableResult
    public func beginTask(targetProcessID: Int32, timeout: TimeInterval = 60) throws -> UInt64 {
        // Deny an unbounded deadline or generation wraparound instead of reusing a token.
        guard targetProcessID > 0 else { throw InteractionFailure.noFrontmostApplication }
        guard timeout.isFinite, timeout > 0,
              generation < UInt64.max else { throw InteractionFailure.timedOut }
        client.stopMonitoring()
        generation += 1
        cancelled = false
        authorizedProcessID = targetProcessID
        activeSnapshotID = nil
        deadline = uptime() + timeout
        guard deadline.isFinite else { throw InteractionFailure.timedOut }
        return generation
    }

    public func cancelTask(generation token: UInt64) {
        guard token == generation else { return }
        cancelled = true
        activeSnapshotID = nil
        client.stopMonitoring()
    }

    public func shutdown() {
        cancelled = true
        activeSnapshotID = nil
        client.stopMonitoring()
    }

    private func verifyGeneration(_ token: UInt64) throws {
        guard token == generation, token != 0 else { throw InteractionFailure.staleGeneration }
        guard !cancelled else { throw InteractionFailure.cancelled }
        guard uptime() <= deadline else {
            cancelled = true
            activeSnapshotID = nil
            client.stopMonitoring()
            throw InteractionFailure.timedOut
        }
    }

    private func checkAllowed(_ reading: SelectionObservation, intent: SelectionIntent) throws {
        guard !reading.isSecure else { throw InteractionFailure.secureField }
        guard reading.isEditable else { throw InteractionFailure.notEditable }
        guard reading.processID > 0, reading.range.location >= 0,
              reading.range.length >= 0,
              reading.range.length <= Int.max - reading.range.location,
              reading.selectedText.utf16.count == reading.range.length else {
            throw InteractionFailure.invalidSelection
        }
        switch intent {
        case .replaceSelection:
            guard reading.range.length > 0 else { throw InteractionFailure.invalidSelection }
        case .dictation:
            // Dictation inserts at the caret or replaces the existing selection.
            break
        }
    }

    public func captureSelection(generation token: UInt64,
                                 intent: SelectionIntent) throws -> SelectionSnapshot {
        try verifyGeneration(token)
        // A second capture always retires the first snapshot.
        activeSnapshotID = nil
        let reading = try client.captureMonitoredSelection()
        do {
            try verifyGeneration(token)
            guard reading.processID == authorizedProcessID else { throw InteractionFailure.applicationChanged }
            try checkAllowed(reading, intent: intent)
            let id = UUID()
            activeSnapshotID = id
            return SelectionSnapshot(id: id, generation: token,
                                     processID: reading.processID,
                                     focusedElementIdentity: reading.identityDescription,
                                     originalSelectedText: reading.selectedText,
                                     range: reading.range, intent: intent, evidence: reading)
        } catch {
            client.stopMonitoring()
            throw error
        }
    }

    public func replaceIfCurrent(_ snapshot: SelectionSnapshot,
                                 with text: String, generation token: UInt64) throws {
        guard snapshot.intent == .replaceSelection else { throw InteractionFailure.invalidSelection }
        try applyIfCurrent(snapshot, text: text, generation: token)
    }

    public func insertIfCurrent(_ snapshot: SelectionSnapshot,
                                text: String, generation token: UInt64) throws {
        guard snapshot.intent == .dictation else { throw InteractionFailure.invalidSelection }
        try applyIfCurrent(snapshot, text: text, generation: token)
    }

    private func applyIfCurrent(_ snapshot: SelectionSnapshot,
                                text: String, generation token: UInt64) throws {
        try verifyGeneration(token)
        guard snapshot.generation == token, activeSnapshotID == snapshot.id else {
            throw InteractionFailure.staleSnapshot
        }
        // Single-use authorization: even an AX error must not cause the same stale write to retry.
        activeSnapshotID = nil
        defer { client.stopMonitoring() }
        let current = try client.currentSelection()
        try checkAllowed(current, intent: snapshot.intent)
        guard current.processID == snapshot.processID else { throw InteractionFailure.applicationChanged }
        guard snapshot.evidence.identity.isSameElement(as: current.identity) else {
            throw InteractionFailure.focusChanged
        }
        guard snapshot.evidence.revision == current.revision,
              snapshot.range == current.range,
              snapshot.originalSelectedText == current.selectedText,
              snapshot.evidence.fullValue == nil || snapshot.evidence.fullValue == current.fullValue else {
            throw InteractionFailure.selectionChanged
        }
        // No suspension point before native revalidation and either AX or guarded paste.
        // An unconfirmed paste is single-use and never retried automatically.
        try client.writeSelectedText(text, expected: current, intent: snapshot.intent)
    }
}
