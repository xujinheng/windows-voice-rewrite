import Foundation
import XCTest
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

final class SelectionGuardTests: XCTestCase {
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

    func testBrowserAddressBarValueChangeRejectsInsertion() async throws {
        let fake = FakeAccessibility(), session = InteractionSession(client: fake)
        fake.update(text: "", range: VoiceRewriteInteraction.TextRange(location: 7, length: 0),
                    fullValue: "example")
        let gen = try await session.beginTask(targetProcessID: 123)
        let snapshot = try await session.captureSelection(generation: gen, intent: .insertAtCaret)
        // Address bar value changed without a selection change notification:
        // unchanged PID, focus, selection and revision must still fail closed.
        fake.update(fullValue: "examplE")
        do { try await session.insertIfCurrent(snapshot, text: "spoken", generation: gen); XCTFail() }
        catch { XCTAssertEqual(error as? InteractionFailure, .selectionChanged) }
        XCTAssertTrue(fake.writes.isEmpty)
    }

    func testStableBrowserAddressBarValueAllowsInsertion() async throws {
        let fake = FakeAccessibility(), session = InteractionSession(client: fake)
        fake.update(text: "", range: VoiceRewriteInteraction.TextRange(location: 7, length: 0),
                    fullValue: "example")
        let gen = try await session.beginTask(targetProcessID: 123)
        let snapshot = try await session.captureSelection(generation: gen, intent: .insertAtCaret)
        try await session.insertIfCurrent(snapshot, text: "spoken", generation: gen)
        XCTAssertEqual(fake.writes, ["spoken"])
    }

    func testInsertRequiresCaretAndIsSingleUse() async throws {
        let fake = FakeAccessibility(), session = InteractionSession(client: fake)
        fake.update(text: "", range: VoiceRewriteInteraction.TextRange(location: 1, length: 0))
        let gen = try await session.beginTask(targetProcessID: 123)
        let snapshot = try await session.captureSelection(generation: gen, intent: .insertAtCaret)
        try await session.insertIfCurrent(snapshot, text: "ASR", generation: gen)
        XCTAssertEqual(fake.writes, ["ASR"])
        do { try await session.insertIfCurrent(snapshot, text: "AGAIN", generation: gen); XCTFail() }
        catch { XCTAssertEqual(error as? InteractionFailure, .staleSnapshot) }
    }

    func testNoInsertionOverExistingSelection() async throws {
        let fake = FakeAccessibility(), session = InteractionSession(client: fake)
        let gen = try await session.beginTask(targetProcessID: 123)
        do { _ = try await session.captureSelection(generation: gen, intent: .insertAtCaret); XCTFail() }
        catch { XCTAssertEqual(error as? InteractionFailure, .invalidSelection) }
        XCTAssertTrue(fake.writes.isEmpty)
    }

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
    var restores = 0
    func capture() throws -> ClipboardContents { current }
    func installTemporaryText(_ text: String, owner: UUID) throws -> Int {
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
    func testUnverifiableAXValueCannotAuthorizeAnyWrite() {
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
