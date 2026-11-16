import XCTest
import VoiceRewriteInteraction
@testable import VoiceRewrite

final class MacShortcutTests: XCTestCase {
    func testDefaultShortcutsUseTheSameKeysAsTheFixedCarbonBindings() {
        let dictation = MacShortcut.hotKey(key: "R", ctrl: true, alt: true, shift: false)
        let spoken = MacShortcut.hotKey(key: "E", ctrl: true, alt: true, shift: false)
        let text = MacShortcut.hotKey(key: "T", ctrl: true, alt: true, shift: false)
        XCTAssertEqual(dictation, HotKeyShortcut(keyCode: 15, modifiers: [.control, .option]))
        XCTAssertEqual(spoken, HotKeyShortcut(keyCode: 14, modifiers: [.control, .option]))
        XCTAssertEqual(text, HotKeyShortcut(keyCode: 17, modifiers: [.control, .option]))
        XCTAssertEqual(MacShortcut.display("Ctrl+Alt+R"), "Ctrl+Option+R")
        XCTAssertNil(MacShortcut.hotKey(key: "R", ctrl: false, alt: false, shift: false))
    }

    func testDefaultKeyboardPlanPreservesBothMouseSideButtons() throws {
        let dictation = try XCTUnwrap(ShortcutBinding(canonical: "Ctrl+Alt+R"))
        let rewrite = try XCTUnwrap(ShortcutBinding(canonical: "Ctrl+Alt+E"))
        let text = try XCTUnwrap(ShortcutBinding(canonical: "Ctrl+Alt+T"))
        let plan = try XCTUnwrap(MacShortcutPlan(dictation: dictation, rewrite: rewrite, textEdit: text))
        XCTAssertEqual(plan.keyboard.count, 3)
        XCTAssertEqual(plan.sideButtons.action(for: 3), .dictation)
        XCTAssertEqual(plan.sideButtons.action(for: 4), .rewrite)
    }

    func testConfiguredMouseSwapAndUnboundPassThrough() throws {
        let text = try XCTUnwrap(ShortcutBinding(canonical: "Ctrl+Alt+T"))
        let swapped = try XCTUnwrap(MacShortcutPlan(
            dictation: XCTUnwrap(ShortcutBinding(canonical: "Mouse5")),
            rewrite: XCTUnwrap(ShortcutBinding(canonical: "Mouse4")), textEdit: text))
        XCTAssertEqual(swapped.sideButtons.action(for: 3), .rewrite)
        XCTAssertEqual(swapped.sideButtons.action(for: 4), .dictation)
        let onlyOne = try XCTUnwrap(MacShortcutPlan(
            dictation: XCTUnwrap(ShortcutBinding(canonical: "Mouse5")),
            rewrite: XCTUnwrap(ShortcutBinding(canonical: "Ctrl+Alt+E")), textEdit: text))
        XCTAssertNil(onlyOne.sideButtons.action(for: 3))
        XCTAssertEqual(onlyOne.sideButtons.action(for: 4), .dictation)
    }

    func testInvalidCollisionAndUnsupportedKeyNeverBuildPlan() throws {
        let r = try XCTUnwrap(ShortcutBinding(canonical: "Ctrl+Alt+R"))
        let t = try XCTUnwrap(ShortcutBinding(canonical: "Ctrl+Alt+T"))
        XCTAssertNil(MacShortcutPlan(dictation: r, rewrite: r, textEdit: t))
        let mouse = try XCTUnwrap(ShortcutBinding(canonical: "Mouse4"))
        XCTAssertNil(MacShortcutPlan(dictation: mouse, rewrite: mouse, textEdit: t))
        XCTAssertNil(MacShortcutPlan(dictation: r, rewrite: t, textEdit: mouse))
        let unsupported = try XCTUnwrap(ShortcutBinding(canonical: "F21"))
        XCTAssertNil(MacShortcutPlan(dictation: unsupported, rewrite: t, textEdit: r))
        XCTAssertEqual(MacShortcut.hotKey(key: "F13", ctrl: false, alt: false, shift: false)?.keyCode, 105)
        XCTAssertEqual(MacShortcut.hotKey(key: "F20", ctrl: false, alt: false, shift: false)?.keyCode, 90)
    }
}
