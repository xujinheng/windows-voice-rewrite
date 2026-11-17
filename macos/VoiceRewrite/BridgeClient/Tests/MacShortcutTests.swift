import XCTest
import VoiceRewriteInteraction
@testable import VoiceRewrite

final class MacShortcutTests: XCTestCase {
    func testFirstLaunchSettingsRequirementEndsOnlyAfterSaving() throws {
        let folder = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: folder) }
        let settings = folder.appendingPathComponent("settings.json")
        XCTAssertTrue(InitialSetup.required(settingsFile: settings))
        try Data("{}".utf8).write(to: settings)
        XCTAssertFalse(InitialSetup.required(settingsFile: settings))
    }

    func testDefaultShortcutsUseTheSameKeysAsTheFixedCarbonBindings() {
        let dictation = MacShortcut.hotKey(key: "R", ctrl: true, alt: true, shift: false)
        let spoken = MacShortcut.hotKey(key: "E", ctrl: true, alt: true, shift: false)
        let text = MacShortcut.hotKey(key: "T", ctrl: true, alt: true, shift: false)
        XCTAssertEqual(dictation, HotKeyShortcut(keyCode: 15, modifiers: [.control, .option]))
        XCTAssertEqual(spoken, HotKeyShortcut(keyCode: 14, modifiers: [.control, .option]))
        XCTAssertEqual(text, HotKeyShortcut(keyCode: 17, modifiers: [.control, .option]))
        XCTAssertEqual(MacShortcut.display("Ctrl+Alt+R"), "Ctrl+Option+R")
        XCTAssertNil(MacShortcut.hotKey(key: "R", ctrl: false, alt: false, shift: false))
        XCTAssertNil(MacShortcut.hotKey(key: "R", ctrl: false, alt: false, shift: true))
    }

    func testDefaultKeyboardPlanPreservesBothMouseSideButtons() throws {
        let dictation = try XCTUnwrap(ShortcutBinding(canonical: "RightCtrl"))
        let rewrite = try XCTUnwrap(ShortcutBinding(canonical: "RightOption"))
        let plan = try XCTUnwrap(MacShortcutPlan(dictation: dictation, rewrite: rewrite))
        XCTAssertEqual(plan.keyboard.count, 0)
        XCTAssertEqual(plan.modifierDictation, .control)
        XCTAssertEqual(plan.modifierRewrite, .option)
        XCTAssertEqual(MacShortcut.display("RightCtrl"), "右 Control（长按）")
        XCTAssertEqual(MacShortcut.display("RightOption"), "右 Option（长按）")
        XCTAssertEqual(plan.sideButtons.action(for: 3), .dictation)
        XCTAssertEqual(plan.sideButtons.action(for: 4), .rewrite)
        // The stored text command remains callable in-app, never a Carbon global key.
        let text = try XCTUnwrap(ShortcutBinding(canonical: "Ctrl+Alt+T"))
        XCTAssertNotNil(MacShortcut.hotKey(key: text.key, ctrl: text.ctrl, alt: text.alt, shift: text.shift))
    }

    func testConfiguredMouseSwapAndUnboundPassThrough() throws {
        let swapped = try XCTUnwrap(MacShortcutPlan(
            dictation: XCTUnwrap(ShortcutBinding(canonical: "Mouse5")),
            rewrite: XCTUnwrap(ShortcutBinding(canonical: "Mouse4"))))
        XCTAssertEqual(swapped.sideButtons.action(for: 3), .rewrite)
        XCTAssertEqual(swapped.sideButtons.action(for: 4), .dictation)
        let onlyOne = try XCTUnwrap(MacShortcutPlan(
            dictation: XCTUnwrap(ShortcutBinding(canonical: "Mouse5")),
            rewrite: XCTUnwrap(ShortcutBinding(canonical: "Ctrl+Alt+E"))))
        XCTAssertNil(onlyOne.sideButtons.action(for: 3))
        XCTAssertEqual(onlyOne.sideButtons.action(for: 4), .dictation)
    }

    func testInvalidCollisionAndUnsupportedKeyNeverBuildPlan() throws {
        let r = try XCTUnwrap(ShortcutBinding(canonical: "Ctrl+Alt+R"))
        XCTAssertNil(MacShortcutPlan(dictation: r, rewrite: r))
        let mouse = try XCTUnwrap(ShortcutBinding(canonical: "Mouse4"))
        XCTAssertNil(MacShortcutPlan(dictation: mouse, rewrite: mouse))
        XCTAssertNil(ShortcutBinding(canonical: "Ctrl+Ctrl+R"))
        XCTAssertNil(ShortcutBinding(canonical: "Ctrl+Mouse4"))
        let unsupported = try XCTUnwrap(ShortcutBinding(canonical: "F21"))
        XCTAssertNil(MacShortcutPlan(dictation: unsupported, rewrite: r))
        XCTAssertEqual(MacShortcut.hotKey(key: "F13", ctrl: false, alt: false, shift: false)?.keyCode, 105)
        XCTAssertEqual(MacShortcut.hotKey(key: "F20", ctrl: false, alt: false, shift: false)?.keyCode, 90)
    }

    func testExitShortcutIsIndependentAndHoldToggleRejectsUnsafeSideBindings() throws {
        let dictation = try XCTUnwrap(ShortcutBinding(canonical: "Ctrl+Alt+R"))
        let rewrite = try XCTUnwrap(ShortcutBinding(canonical: "Ctrl+Alt+E"))
        let exit = try XCTUnwrap(ShortcutBinding(canonical: "Ctrl+Alt+F10"))
        let hold = try XCTUnwrap(MacShortcutPlan(
            dictation: dictation, rewrite: rewrite, exit: exit, recordMode: "hold"))
        XCTAssertEqual(hold.keyboard.count, 3)
        XCTAssertNotNil(hold.exit)
        XCTAssertNotEqual(hold.exit, hold.dictation)
        XCTAssertNotEqual(hold.exit, hold.rewrite)
        XCTAssertEqual(hold.sideButtons.action(for: 3), .dictation)
        XCTAssertNil(MacShortcutPlan(dictation: dictation, rewrite: rewrite, exit: dictation))
        XCTAssertNil(MacShortcutPlan(dictation: dictation, rewrite: rewrite, exit: rewrite))
        XCTAssertNil(MacShortcutPlan(dictation: dictation, rewrite: rewrite,
                                      exit: ShortcutBinding(canonical: "Mouse4")))
        XCTAssertNil(MacShortcutPlan(dictation: dictation, rewrite: rewrite, recordMode: "invalid"))

        let toggle = try XCTUnwrap(MacShortcutPlan(
            dictation: dictation, rewrite: rewrite, exit: exit, recordMode: "toggle"))
        XCTAssertNil(toggle.sideButtons.action(for: 3))
        XCTAssertNil(toggle.sideButtons.action(for: 4))
        XCTAssertNil(MacShortcutPlan(
            dictation: try XCTUnwrap(ShortcutBinding(canonical: "Mouse4")),
            rewrite: rewrite, exit: exit, recordMode: "toggle"))
    }

    func testRightModifierHoldCannotAccidentallyBecomeToggleOrExit() throws {
        let rightControl = try XCTUnwrap(ShortcutBinding(canonical: "RightCtrl"))
        let rightOption = try XCTUnwrap(ShortcutBinding(canonical: "RightOption"))
        let plan = try XCTUnwrap(MacShortcutPlan(dictation: rightControl, rewrite: rightOption))
        XCTAssertEqual(plan.sideButtons.action(for: 3), .dictation)
        XCTAssertEqual(plan.sideButtons.action(for: 4), .rewrite)
        XCTAssertNil(MacShortcutPlan(dictation: rightControl, rewrite: rightOption, recordMode: "toggle"))
        XCTAssertNil(MacShortcutPlan(dictation: rightControl, rewrite: rightControl))
        XCTAssertNil(MacShortcutPlan(dictation: rightControl, rewrite: rightOption, exit: rightControl))
        XCTAssertNil(ShortcutBinding(canonical: "Ctrl+RightCtrl"))
        let custom = try XCTUnwrap(MacShortcutPlan(
            dictation: XCTUnwrap(ShortcutBinding(canonical: "Ctrl+Alt+R")),
            rewrite: XCTUnwrap(ShortcutBinding(canonical: "Ctrl+Alt+E"))))
        XCTAssertEqual(custom.keyboard.count, 2)
        XCTAssertNil(custom.modifierDictation)
        XCTAssertNil(custom.modifierRewrite)
    }

    @MainActor
    func testShortcutSaveTransactionSkipsPersistenceWhenRegistrationFails() throws {
        var operations: [String] = []
        let accepted = try ShortcutSaveTransaction.run(
            activate: { operations.append("activate"); return false },
            persist: { operations.append("persist") },
            restore: { operations.append("restore") }
        )
        XCTAssertFalse(accepted)
        XCTAssertEqual(operations, ["activate"])
    }

    @MainActor
    func testShortcutSaveTransactionRestoresOnPersistenceFailure() {
        enum TestFailure: Error { case disk }
        var operations: [String] = []
        XCTAssertThrowsError(try ShortcutSaveTransaction.run(
            activate: { operations.append("activate"); return true },
            persist: { operations.append("persist"); throw TestFailure.disk },
            restore: { operations.append("restore") }
        ))
        XCTAssertEqual(operations, ["activate", "persist", "restore"])
    }

    @MainActor
    func testShortcutSaveTransactionCommitsOnlyAfterRegistration() throws {
        var operations: [String] = []
        let accepted = try ShortcutSaveTransaction.run(
            activate: { operations.append("activate"); return true },
            persist: { operations.append("persist") },
            restore: { operations.append("restore") }
        )
        XCTAssertTrue(accepted)
        XCTAssertEqual(operations, ["activate", "persist"])
    }
}
