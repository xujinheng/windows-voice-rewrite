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
}
