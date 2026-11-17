import Foundation
import XCTest
#if os(macOS)
import AppKit
#endif
@testable import VoiceRewriteInteraction

private final class FakeAccessibility: AccessibilityClient, @unchecked Sendable {
    var observation = SelectionObservation(
        processID: 123, identity: StringFocusIdentity("editor-a"),
        identityDescription: "editor-a", selectedText: "hello",
        range: VoiceRewriteInteraction.TextRange(location: 2, length: 5), isEditable: true,
        isSecure: false, revision: 1)
    var captureFailure: InteractionFailure?
    var writeFailure: InteractionFailure?
    var writes: [String] = []
    var monitorStops = 0
    func captureMonitoredSelection() throws -> SelectionObservation {
        if let captureFailure { throw captureFailure }
        return observation
    }
    func currentSelection() throws -> SelectionObservation { observation }
    func writeSelectedText(_ text: String, expected: SelectionObservation) throws {
        if let writeFailure { throw writeFailure }
        writes.append(text)
    }
    func stopMonitoring() { monitorStops += 1 }
    func update(processID: Int32? = nil, focus: String? = nil, text: String? = nil,
                range: VoiceRewriteInteraction.TextRange? = nil, editable: Bool? = nil,
                secure: Bool? = nil, revision: UInt64? = nil, fullValue: String? = nil) {
        let old = observation
        observation = SelectionObservation(
            processID: processID ?? old.processID,
            identity: focus.map(StringFocusIdentity.init) ?? old.identity,
            identityDescription: focus ?? old.identityDescription,
            selectedText: text ?? old.selectedText,
            range: range ?? old.range,
            isEditable: editable ?? old.isEditable,
            isSecure: secure ?? old.isSecure,
            revision: revision ?? old.revision,
            fullValue: fullValue ?? old.fullValue)
    }
}

private final class FakeUptime: @unchecked Sendable {
    var now: TimeInterval = 1000
}

#if os(macOS)
final class NativeClipboardReceiptTests: XCTestCase {
    func testDictationTargetPolicyChecksWindowWhenAvailableWithoutRequiringAX() {
        XCTAssertTrue(DictationTargetPolicy.maySend(
            capturedPID: 123, foregroundPID: 123, secureInput: false))
        XCTAssertFalse(DictationTargetPolicy.maySend(
            capturedPID: 123, foregroundPID: 456, secureInput: false))
        XCTAssertFalse(DictationTargetPolicy.maySend(
            capturedPID: 123, foregroundPID: 123, secureInput: true))
        XCTAssertTrue(DictationTargetPolicy.sameWindow(captured: nil, current: 42))
        XCTAssertTrue(DictationTargetPolicy.sameWindow(captured: 42, current: nil))
        XCTAssertTrue(DictationTargetPolicy.sameWindow(captured: 42, current: 42))
        XCTAssertFalse(DictationTargetPolicy.sameWindow(captured: 42, current: 99))
        XCTAssertFalse(DictationTargetPolicy.canConfirmReadback(
            original: "same 😀", expected: "same 😀"),
            "unchanged AXValue is not proof that Cmd+V reached the editor")
        XCTAssertTrue(DictationTargetPolicy.canConfirmReadback(
            original: "Before", expected: "Before听写"))
    }

    func testLazyPasteboardReportsActualTextRead() throws {
        let pasteboard = NSPasteboard.withUniqueName()
        defer { pasteboard.releaseGlobally() }
        let board = MacClipboardBoard(board: pasteboard)
        let lease = ClipboardProtector(board: board)
        let token = try lease.beginTemporaryText("spoken text")
        XCTAssertNil(board.lastTextReadAt())
        XCTAssertEqual(pasteboard.string(forType: .string), "spoken text")
        XCTAssertNotNil(board.lastTextReadAt())
        try lease.finish(token)
        XCTAssertNil(board.lastTextReadAt())
    }

    func testNativePasteboardRejectsStaleSnapshotWithoutOverwritingNewClipboard() throws {
        let pasteboard = NSPasteboard.withUniqueName()
        defer { pasteboard.releaseGlobally() }
        pasteboard.clearContents()
        XCTAssertTrue(pasteboard.setString("original", forType: .string))
        let board = MacClipboardBoard(board: pasteboard)
        let snapshot = try board.capture()
        pasteboard.clearContents()
        XCTAssertTrue(pasteboard.setString("new owner", forType: .string))

        XCTAssertThrowsError(try board.installTemporaryText(
            "dictation", owner: UUID(), expectedChangeCount: snapshot.changeCount)) {
            XCTAssertEqual($0 as? ClipboardFailure, .snapshotUnavailable)
        }
        XCTAssertEqual(pasteboard.string(forType: .string), "new owner")
    }

    func testFastReceiverReadBeforeReceiptRegistration() async throws {
        let pasteboard = NSPasteboard.withUniqueName()
        defer { pasteboard.releaseGlobally() }
        pasteboard.clearContents()
        XCTAssertTrue(pasteboard.setString("original", forType: .string))
        let board = MacClipboardBoard(board: pasteboard)
        let lease = ClipboardProtector(board: board)
        let token = try lease.beginTemporaryText("speech")
        // The event was posted and the destination consumed the promised text
        // before the clipboard restoration callback could be registered.
        let postingStartedAt = ProcessInfo.processInfo.systemUptime - 1.0
        XCTAssertEqual(pasteboard.string(forType: .string), "speech")
        XCTAssertNotNil(board.lastTextReadAt())
        lease.restoreAfterPaste(token, postingStartedAt: postingStartedAt)
        try await Task.sleep(nanoseconds: 450_000_000)
        XCTAssertEqual(pasteboard.string(forType: .string), "original")
    }

    func testReceiptRequiresMinimumVisibilityAndQuietPeriod() {
        XCTAssertFalse(PasteTiming.restoreReady(elapsed: 0.4, sinceLastRead: 0.3, hasRead: true))
        XCTAssertFalse(PasteTiming.restoreReady(elapsed: 0.9, sinceLastRead: 0.01, hasRead: true))
        XCTAssertTrue(PasteTiming.restoreReady(elapsed: 0.9, sinceLastRead: 0.3, hasRead: true))
        XCTAssertFalse(PasteTiming.restoreReady(elapsed: 2.9, sinceLastRead: nil, hasRead: false))
        XCTAssertTrue(PasteTiming.restoreReady(elapsed: 3.0, sinceLastRead: nil, hasRead: false))
    }

    func testDeliveryStatesMatchGoCore() {
        XCTAssertEqual(DeliveryState.blocked.rawValue, 0)
        XCTAssertEqual(DeliveryState.sentUnverified.rawValue, 1)
        XCTAssertEqual(DeliveryState.confirmed.rawValue, 2)
        XCTAssertEqual(DeliveryState.uncertain.rawValue, 3)
        XCTAssertTrue(DeliveryState.blocked.requiresManualResult)
        XCTAssertFalse(DeliveryState.sentUnverified.requiresManualResult)
        XCTAssertFalse(DeliveryState.confirmed.requiresManualResult)
        XCTAssertTrue(DeliveryState.uncertain.requiresManualResult)
    }

    func testReceiptTimingsComeFromSharedGoCore() {
        XCTAssertEqual(PasteTiming.quietMilliseconds, 200)
        XCTAssertEqual(PasteTiming.minimumMilliseconds, 700)
        XCTAssertEqual(PasteTiming.maxMilliseconds, 3000)
        XCTAssertEqual(PasteTiming.pollMilliseconds, 100)
    }
}
#endif

final class SelectionGuardTests: XCTestCase {
    func testBrowserAddressBarMonitorFallbackNeedsFocusAndFullValue() {
        XCTAssertTrue(canMonitorSelection(focusNotifications: true, textNotifications: true,
                                         unsupportedTextNotifications: false, fullValue: nil))
        XCTAssertTrue(canMonitorSelection(focusNotifications: true, textNotifications: false,
                                         unsupportedTextNotifications: true, fullValue: "https://example.test"))
        XCTAssertFalse(canMonitorSelection(focusNotifications: true, textNotifications: false,
                                          unsupportedTextNotifications: true, fullValue: nil))
        XCTAssertFalse(canMonitorSelection(focusNotifications: true, textNotifications: false,
                                          unsupportedTextNotifications: false, fullValue: "https://example.test"))
        XCTAssertFalse(canMonitorSelection(focusNotifications: false, textNotifications: true,
                                          unsupportedTextNotifications: true, fullValue: "https://example.test"))
    }

    func testGenerationInvalidatesOldTask() async throws {
        let fake = FakeAccessibility(), session = InteractionSession(client: fake)
        let old = try await session.beginTask(targetProcessID: 123)
        let snapshot = try await session.captureSelection(generation: old, intent: .replaceSelection)
        let current = try await session.beginTask(targetProcessID: 123)
        XCTAssertNotEqual(old, current)
        do { try await session.replaceIfCurrent(snapshot, with: "BAD", generation: old); XCTFail() }
        catch { XCTAssertEqual(error as? InteractionFailure, .staleGeneration) }
        XCTAssertTrue(fake.writes.isEmpty)
    }

    func testSelectionChangeRejectsWrite() async throws {
        let fake = FakeAccessibility(), session = InteractionSession(client: fake)
        let gen = try await session.beginTask(targetProcessID: 123)
        let snapshot = try await session.captureSelection(generation: gen, intent: .replaceSelection)
        fake.update(text: "world", revision: 2)
        do { try await session.replaceIfCurrent(snapshot, with: "BAD", generation: gen); XCTFail() }
        catch { XCTAssertEqual(error as? InteractionFailure, .selectionChanged) }
        XCTAssertTrue(fake.writes.isEmpty)
    }

    func testFocusChangeRejectsWrite() async throws {
        let fake = FakeAccessibility(), session = InteractionSession(client: fake)
        let gen = try await session.beginTask(targetProcessID: 123)
        let snapshot = try await session.captureSelection(generation: gen, intent: .replaceSelection)
        fake.update(focus: "other", revision: 2)
        do { try await session.replaceIfCurrent(snapshot, with: "BAD", generation: gen); XCTFail() }
        catch { XCTAssertEqual(error as? InteractionFailure, .focusChanged) }
        XCTAssertTrue(fake.writes.isEmpty)
    }

    func testDifferentAppAtCaptureIsNotAuthorized() async throws {
        let fake = FakeAccessibility(), session = InteractionSession(client: fake)
        let token = try await session.beginTask(targetProcessID: 999)
        do { _ = try await session.captureSelection(generation: token, intent: .replaceSelection); XCTFail() }
        catch { XCTAssertEqual(error as? InteractionFailure, .applicationChanged) }
        XCTAssertTrue(fake.writes.isEmpty)
    }

    func testAppChangeRejectsWrite() async throws {
        let fake = FakeAccessibility(), session = InteractionSession(client: fake)
        let gen = try await session.beginTask(targetProcessID: 123)
        let snapshot = try await session.captureSelection(generation: gen, intent: .replaceSelection)
        fake.update(processID: 124, revision: 2)
        do { try await session.replaceIfCurrent(snapshot, with: "BAD", generation: gen); XCTFail() }
        catch { XCTAssertEqual(error as? InteractionFailure, .applicationChanged) }
        XCTAssertTrue(fake.writes.isEmpty)
    }

    func testSecureFieldAppearsAfterCapture() async throws {
        let fake = FakeAccessibility(), session = InteractionSession(client: fake)
        let gen = try await session.beginTask(targetProcessID: 123)
        let snapshot = try await session.captureSelection(generation: gen, intent: .replaceSelection)
        fake.update(secure: true, revision: 2)
        do { try await session.replaceIfCurrent(snapshot, with: "BAD", generation: gen); XCTFail() }
        catch { XCTAssertEqual(error as? InteractionFailure, .secureField) }
        XCTAssertTrue(fake.writes.isEmpty)
    }

    func testABASelectionChangeRejectedByRevision() async throws {
        let fake = FakeAccessibility(), session = InteractionSession(client: fake)
        let gen = try await session.beginTask(targetProcessID: 123)
        let snapshot = try await session.captureSelection(generation: gen, intent: .replaceSelection)
        // Text, caret, and focused element are the same again, but AX monitor saw a change.
        fake.update(revision: 3)
        do { try await session.replaceIfCurrent(snapshot, with: "BAD", generation: gen); XCTFail() }
        catch { XCTAssertEqual(error as? InteractionFailure, .selectionChanged) }
        XCTAssertTrue(fake.writes.isEmpty)
    }

    func testSecureFieldRejectedEvenIfSettable() async throws {
        let fake = FakeAccessibility(), session = InteractionSession(client: fake)
        fake.update(secure: true)
        let gen = try await session.beginTask(targetProcessID: 123)
        do { _ = try await session.captureSelection(generation: gen, intent: .replaceSelection); XCTFail() }
        catch { XCTAssertEqual(error as? InteractionFailure, .secureField) }
        XCTAssertTrue(fake.writes.isEmpty)
    }

    func testPermissionDenied() async throws {
        let fake = FakeAccessibility(), session = InteractionSession(client: fake)
        fake.captureFailure = .accessibilityPermissionDenied
        let gen = try await session.beginTask(targetProcessID: 123)
        do { _ = try await session.captureSelection(generation: gen, intent: .replaceSelection); XCTFail() }
        catch { XCTAssertEqual(error as? InteractionFailure, .accessibilityPermissionDenied) }
        XCTAssertTrue(fake.writes.isEmpty)
    }

    func testCancellationAndTimeout() async throws {
        let fake = FakeAccessibility()
        let session = InteractionSession(client: fake, uptime: { 1000 })
        let gen = try await session.beginTask(targetProcessID: 123, timeout: 4)
        let snapshot = try await session.captureSelection(generation: gen, intent: .replaceSelection)
        await session.cancelTask(generation: gen)
        do { try await session.replaceIfCurrent(snapshot, with: "BAD", generation: gen); XCTFail() }
        catch { XCTAssertEqual(error as? InteractionFailure, .cancelled) }
        let clock = FakeUptime()
        let expired = InteractionSession(client: fake, uptime: { clock.now })
        let token = try await expired.beginTask(targetProcessID: 123, timeout: 2)
        let snap = try await expired.captureSelection(generation: token, intent: .replaceSelection)
        clock.now = 1003
        do { try await expired.replaceIfCurrent(snap, with: "BAD", generation: token); XCTFail() }
        catch { XCTAssertEqual(error as? InteractionFailure, .timedOut) }
        XCTAssertTrue(fake.writes.isEmpty)
    }

    #if os(macOS)
    func testDictationTargetAllowsNormalInputWithoutAXTextModel() {
        XCTAssertTrue(DictationTargetPolicy.maySend(
            capturedPID: 123, foregroundPID: 123, secureInput: false))
        XCTAssertFalse(DictationTargetPolicy.maySend(
            capturedPID: 123, foregroundPID: 456, secureInput: false))
        XCTAssertFalse(DictationTargetPolicy.maySend(
            capturedPID: 123, foregroundPID: 123, secureInput: true))
        XCTAssertFalse(DictationTargetPolicy.maySend(
            capturedPID: 0, foregroundPID: 123, secureInput: false))
    }
    #endif

    func testUnsupportedWriteCannotBeRetried() async throws {
        let fake = FakeAccessibility(), session = InteractionSession(client: fake)
        let gen = try await session.beginTask(targetProcessID: 123)
        let snapshot = try await session.captureSelection(generation: gen, intent: .replaceSelection)
        fake.writeFailure = .unsupportedNativeWrite
        do { try await session.replaceIfCurrent(snapshot, with: "new", generation: gen); XCTFail() }
        catch { XCTAssertEqual(error as? InteractionFailure, .unsupportedNativeWrite) }
        fake.writeFailure = nil
        do { try await session.replaceIfCurrent(snapshot, with: "new", generation: gen); XCTFail() }
        catch { XCTAssertEqual(error as? InteractionFailure, .staleSnapshot) }
        XCTAssertTrue(fake.writes.isEmpty)
    }
}

@MainActor
private final class FakeHotKeyDriver: HotKeyDriver {
    var next: UInt32 = 1
    var registered = Set<HotKeyToken>()
    var released: [HotKeyToken] = []
    var failure: HotKeyFailure?
    var rejectKeyCode: UInt32?
    func register(_ shortcut: HotKeyShortcut) throws -> HotKeyToken {
        if let failure { throw failure }
        if shortcut.keyCode == rejectKeyCode { throw HotKeyFailure.systemConflict(-9878) }
        let token = HotKeyToken(id: next)
        next += 1
        registered.insert(token)
        return token
    }
    func unregister(_ token: HotKeyToken) {
        registered.remove(token)
        released.append(token)
    }
}

final class HotKeyTests: XCTestCase {
    func testCarbonKeyReleaseDebouncesAndDoesNotReplayAfterUnregister() {
        var ledger = HotKeyPressState()
        let first = HotKeyToken(id: 1), second = HotKeyToken(id: 2)
        XCTAssertTrue(ledger.transition(first, pressed: true))
        XCTAssertFalse(ledger.transition(first, pressed: true))
        XCTAssertTrue(ledger.transition(second, pressed: true))
        XCTAssertTrue(ledger.transition(first, pressed: false))
        XCTAssertFalse(ledger.transition(first, pressed: false))
        ledger.forget(second)
        XCTAssertFalse(ledger.transition(second, pressed: false))
        XCTAssertTrue(ledger.transition(first, pressed: true))
        ledger.clear()
        XCTAssertFalse(ledger.transition(first, pressed: false))
    }

    func testRegisterDuplicateConflictAndRelease() async throws {
        try await MainActor.run {
        let driver = FakeHotKeyDriver(), registry = HotKeyRegistry(driver: driver)
        let shortcut = HotKeyShortcut(keyCode: 7, modifiers: [.command, .shift])
        try registry.register(shortcut)
        XCTAssertEqual(registry.registeredCount, 1)
        XCTAssertThrowsError(try registry.register(shortcut)) {
            XCTAssertEqual($0 as? HotKeyFailure, .duplicate)
        }
        registry.unregister(shortcut)
        XCTAssertEqual(driver.released.count, 1)
        XCTAssertEqual(registry.registeredCount, 0)
        driver.failure = .systemConflict(-9878)
        XCTAssertThrowsError(try registry.register(shortcut)) {
            XCTAssertEqual($0 as? HotKeyFailure, .systemConflict(-9878))
        }
        XCTAssertTrue(driver.registered.isEmpty)
        }
    }

    func testFailedReplacementKeepsOldKeysAndRollsBackPartialAdditions() async throws {
        try await MainActor.run {
            let driver = FakeHotKeyDriver(), registry = HotKeyRegistry(driver: driver)
            let oldOne = HotKeyShortcut(keyCode: 2, modifiers: [.command])
            let oldTwo = HotKeyShortcut(keyCode: 3, modifiers: [.option])
            let added = HotKeyShortcut(keyCode: 4, modifiers: [.control])
            let rejected = HotKeyShortcut(keyCode: 5, modifiers: [.shift, .control])
            try registry.register(oldOne)
            try registry.register(oldTwo)
            driver.rejectKeyCode = rejected.keyCode
            XCTAssertThrowsError(try registry.replace(with: [oldOne, added, rejected])) {
                XCTAssertEqual($0 as? HotKeyFailure, .systemConflict(-9878))
            }
            XCTAssertEqual(registry.registeredCount, 2)
            XCTAssertEqual(driver.registered.count, 2)
            XCTAssertEqual(driver.released.count, 1)

            driver.rejectKeyCode = nil
            try registry.replace(with: [oldTwo, oldOne])
            XCTAssertEqual(driver.next, 4)
            XCTAssertEqual(driver.released.count, 1)

            try registry.replace(with: [oldTwo, added])
            XCTAssertEqual(registry.registeredCount, 2)
            XCTAssertEqual(driver.registered.count, 2)
            XCTAssertEqual(driver.released.count, 2)
            XCTAssertThrowsError(try registry.replace(with: [oldTwo, oldTwo])) {
                XCTAssertEqual($0 as? HotKeyFailure, .duplicate)
            }
            XCTAssertEqual(registry.registeredCount, 2)
        }
    }

    func testShutdownReleasesAll() async throws {
        try await MainActor.run {
        let driver = FakeHotKeyDriver(), registry = HotKeyRegistry(driver: driver)
        try registry.register(.init(keyCode: 2, modifiers: [.command]))
        try registry.register(.init(keyCode: 3, modifiers: [.option]))
        registry.shutdown()
        XCTAssertEqual(driver.released.count, 2)
        XCTAssertTrue(driver.registered.isEmpty)
        XCTAssertEqual(registry.registeredCount, 0)
        }
    }
}

private final class FakeClipboard: ClipboardBoard, @unchecked Sendable {
    var current = ClipboardContents(items: [["public.utf8-plain-text": Data("old".utf8)]], changeCount: 1)
    var activeOwner: UUID?
    var simulateWriteFailure = false
    var simulateRestoreFailure = false
    var externalWriteBeforeStage = false
    var restores = 0
    var captureBarrier: DispatchSemaphore?
    var onCapture: (() -> Void)?
    func capture() throws -> ClipboardContents {
        onCapture?()
        if let captureBarrier {
            _ = captureBarrier.wait(timeout: .now() + 2)
        }
        return current
    }
    func installTemporaryText(_ text: String, owner: UUID, expectedChangeCount: Int) throws -> Int {
        if externalWriteBeforeStage {
            externalWriteBeforeStage = false
            current = ClipboardContents(items: [["text": Data("external update".utf8)]],
                                        changeCount: current.changeCount + 1)
            activeOwner = nil
        }
        guard current.changeCount == expectedChangeCount else { throw ClipboardFailure.snapshotUnavailable }
        activeOwner = owner
        current = ClipboardContents(items: [["text": Data(text.utf8)]], changeCount: current.changeCount + 1)
        if simulateWriteFailure { throw ClipboardFailure.writeFailed }
        return current.changeCount
    }
    func ownedChangeCount(owner: UUID) -> Int? { activeOwner == owner ? current.changeCount : nil }
    func restore(_ previous: ClipboardContents) throws {
        if simulateRestoreFailure { throw ClipboardFailure.restoreFailed }
        restores += 1
        activeOwner = nil
        current = ClipboardContents(items: previous.items, changeCount: current.changeCount + 1)
    }
}

final class ClipboardTests: XCTestCase {
    func testRestoresAllTypesWhenStillOwned() async throws {
        let board = FakeClipboard(), protector = ClipboardProtector(board: board)
        let original = board.current.items
        let lease = try protector.beginTemporaryText("secret")
        try protector.finish(lease)
        XCTAssertEqual(board.current.items, original)
        XCTAssertEqual(board.restores, 1)
    }

    @MainActor
    func testSlowExternalClipboardSnapshotDoesNotBlockUIOrPublishCancelledPaste() async throws {
        let board = FakeClipboard()
        let barrier = DispatchSemaphore(value: 0)
        let snapshotStarted = expectation(description: "background clipboard snapshot began")
        board.captureBarrier = barrier
        board.onCapture = { snapshotStarted.fulfill() }
        let protector = ClipboardProtector(board: board)
        var active = true
        let queued = Task {
            try await protector.beginWhenAvailable("stale voice text", stillValid: { active })
        }
        // This can complete only if a blocked external pasteboard provider
        // is not running on the test's MainActor (AppKit's UI thread).
        await fulfillment(of: [snapshotStarted], timeout: 1)
        active = false
        barrier.signal()
        do {
            _ = try await queued.value
            XCTFail("cancelled snapshot installed stale pasteboard text")
        } catch {
            XCTAssertEqual(error as? InteractionFailure, .cancelled)
        }
        XCTAssertNil(board.activeOwner)
        XCTAssertEqual(board.current.items[0]["public.utf8-plain-text"], Data("old".utf8))
    }

    func testAsyncClipboardSnapshotRejectsConcurrentExternalClipboardChange() async throws {
        let board = FakeClipboard()
        board.externalWriteBeforeStage = true
        let protector = ClipboardProtector(board: board)
        do {
            _ = try await protector.beginWhenAvailable("speech", stillValid: { true })
            XCTFail("stale async clipboard snapshot must not overwrite another app")
        } catch {
            XCTAssertEqual(error as? ClipboardFailure, .snapshotUnavailable)
        }
        XCTAssertEqual(board.current.items, [["text": Data("external update".utf8)]])
        XCTAssertNil(board.activeOwner)
    }

    func testSecondDictationWaitsForClipboardLease() async throws {
        let board = FakeClipboard(), protector = ClipboardProtector(board: board)
        let first = try protector.beginTemporaryText("first")
        let queued = Task {
            try await protector.beginWhenAvailable("second", stillValid: { true })
        }
        try await Task.sleep(nanoseconds: 120_000_000)
        XCTAssertEqual(board.current.items[0]["text"], Data("first".utf8))
        try protector.finish(first)
        let second = try await queued.value
        XCTAssertTrue(protector.isOwned(second))
        XCTAssertEqual(board.current.items[0]["text"], Data("second".utf8))
        try protector.finish(second)
    }

    func testCancelledQueuedDictationDoesNotChangeClipboard() async throws {
        let board = FakeClipboard(), protector = ClipboardProtector(board: board)
        let first = try protector.beginTemporaryText("first")
        let queued = Task {
            try await protector.beginWhenAvailable("second", stillValid: { false })
        }
        do {
            _ = try await queued.value
            XCTFail("cancelled work acquired a lease")
        } catch {
            XCTAssertEqual(error as? InteractionFailure, .cancelled)
        }
        XCTAssertEqual(board.current.items[0]["text"], Data("first".utf8))
        try protector.finish(first)
    }

    func testOwnershipLostNeverClobbersUserClipboard() async throws {
        let board = FakeClipboard(), protector = ClipboardProtector(board: board)
        let lease = try protector.beginTemporaryText("secret")
        board.activeOwner = nil
        board.current = ClipboardContents(items: [["text": Data("user".utf8)]], changeCount: 44)
        do { try protector.finish(lease); XCTFail() }
        catch { XCTAssertEqual(error as? ClipboardFailure, .ownershipLost) }
        XCTAssertEqual(board.current.items[0]["text"], Data("user".utf8))
        XCTAssertEqual(board.restores, 0)
    }

    func testZeroChangeCountIsNotOwnership() {
        XCTAssertFalse(ClipboardOwnership.mayRestore(hasOriginal: true, owned: 0, current: 0))
        XCTAssertFalse(ClipboardOwnership.mayRestore(hasOriginal: true, owned: 11, current: 12))
        XCTAssertFalse(ClipboardOwnership.mayRestore(hasOriginal: false, owned: 11, current: 11))
        XCTAssertFalse(ClipboardOwnership.mayRestore(hasOriginal: true, owned: 11, current: nil))
        XCTAssertTrue(ClipboardOwnership.mayRestore(hasOriginal: true, owned: 11, current: 11))
    }

    func testExternalClipboardTakeoverDuringSnapshotDoesNotRestoreStaleContents() throws {
        let board = FakeClipboard(), protector = ClipboardProtector(board: board)
        board.externalWriteBeforeStage = true
        XCTAssertThrowsError(try protector.beginTemporaryText("speech")) {
            XCTAssertEqual($0 as? ClipboardFailure, .snapshotUnavailable)
        }
        XCTAssertEqual(board.current.items, [["text": Data("external update".utf8)]])
        XCTAssertEqual(board.restores, 0)
        // A failed stage must leave no stranded lease for the next dictation.
        let next = try protector.beginTemporaryText("second")
        XCTAssertTrue(protector.isOwned(next))
        try protector.finish(next)
        XCTAssertEqual(board.current.items, [["text": Data("external update".utf8)]])
    }

    func testPartialWriteFailureRestoresOnlyIfOwned() async throws {
        let board = FakeClipboard(), protector = ClipboardProtector(board: board)
        let original = board.current.items
        board.simulateWriteFailure = true
        do { _ = try protector.beginTemporaryText("secret"); XCTFail() }
        catch { XCTAssertEqual(error as? ClipboardFailure, .writeFailed) }
        XCTAssertEqual(board.current.items, original)
        XCTAssertEqual(board.restores, 1)
    }

    func testRestoreFailureReported() async throws {
        let board = FakeClipboard(), protector = ClipboardProtector(board: board)
        let lease = try protector.beginTemporaryText("secret")
        board.simulateRestoreFailure = true
        do { try protector.finish(lease); XCTFail() }
        catch { XCTAssertEqual(error as? ClipboardFailure, .restoreFailed) }
    }
}

final class TextWriteVerificationTests: XCTestCase {
    func testBrowserSelectionUsesVerifiedValueWhenAXSelectedTextMissing() {
        let range = VoiceRewriteInteraction.TextRange(location: 1, length: 2)
        XCTAssertEqual(TextWriteVerification.selectedText(
            reported: nil, fullValue: "A😀B", range: range), "😀")
        XCTAssertEqual(TextWriteVerification.selectedText(
            reported: "", fullValue: "A😀B", range: range), "😀")
        XCTAssertNil(TextWriteVerification.selectedText(
            reported: "wrong", fullValue: "A😀B", range: range))
        XCTAssertNil(TextWriteVerification.selectedText(
            reported: nil, fullValue: "A😀B",
            range: VoiceRewriteInteraction.TextRange(location: 2, length: 1)))
        XCTAssertNil(TextWriteVerification.selectedText(
            reported: nil, fullValue: nil, range: range))
        XCTAssertEqual(TextWriteVerification.selectedText(
            reported: "ok", fullValue: nil,
            range: VoiceRewriteInteraction.TextRange(location: 0, length: 2)), "ok")
    }

    func testMissingAXValueCannotConfirmRewrite() {
        XCTAssertNil(TextWriteVerification.expectedValue(
            original: nil, selectedText: "",
            range: .init(location: 0, length: 0), replacement: "dictation"))
        XCTAssertNil(TextWriteVerification.expectedValue(
            original: nil, selectedText: "old",
            range: .init(location: 0, length: 3), replacement: "rewrite"))
        XCTAssertEqual(TextWriteVerification.expectedValue(
            original: "", selectedText: "",
            range: .init(location: 0, length: 0), replacement: "dictation"), "dictation")
    }

    func testInsertionAndReplacementUseUTF16Ranges() {
        let baseline = "A😀B"
        XCTAssertEqual(TextWriteVerification.expectedValue(
            original: baseline, selectedText: "",
            range: .init(location: 3, length: 0), replacement: "中"), "A😀中B")
        XCTAssertEqual(TextWriteVerification.expectedValue(
            original: baseline, selectedText: "😀",
            range: .init(location: 1, length: 2), replacement: "ok"), "AokB")
    }

    func testStaleSelectionAndInvalidRangeCannotConfirmPaste() {
        XCTAssertNil(TextWriteVerification.expectedValue(
            original: "hello", selectedText: "HELLO",
            range: .init(location: 0, length: 5), replacement: "new"))
        XCTAssertNil(TextWriteVerification.expectedValue(
            original: "hello", selectedText: "",
            range: .init(location: Int.max, length: 1), replacement: "new"))
        XCTAssertNil(TextWriteVerification.expectedValue(
            original: "hello", selectedText: "lo",
            range: .init(location: 9, length: 2), replacement: "new"))
        XCTAssertNil(TextWriteVerification.expectedValue(
            original: "A😀B", selectedText: "",
            range: .init(location: 2, length: 0), replacement: "bad"))
        XCTAssertFalse(TextWriteVerification.confirms(actual: "A", expected: "AB"))
    }

    func testDifferentUnicodeEncodingIsNotFalsePositive() {
        XCTAssertFalse(TextWriteVerification.confirms(actual: "\u{00E9}", expected: "e\u{0301}"))
    }
}

final class SelectionLimitTests: XCTestCase {
    func testLengthUsesUnicodeScalarsAndTheSharedSentence() {
        XCTAssertNil(SelectionLimit.tooLongMessage(limit: 3, selected: "abc"))
        XCTAssertEqual(SelectionLimit.tooLongMessage(limit: 3, selected: "中文测试"), "选中文字超过 3 字限制")
        XCTAssertNil(SelectionLimit.tooLongMessage(limit: 0, selected: String(repeating: "a", count: 12000)))
        XCTAssertEqual(
            SelectionLimit.tooLongMessage(limit: 0, selected: String(repeating: "a", count: 12001)),
            "选中文字超过 12000 字限制")
        XCTAssertNil(SelectionLimit.tooLongMessage(selected: ""))
        XCTAssertEqual(SelectionLimit.tooLongMessage(limit: 1, selected: "e\u{0301}"), "选中文字超过 1 字限制")
    }
}
