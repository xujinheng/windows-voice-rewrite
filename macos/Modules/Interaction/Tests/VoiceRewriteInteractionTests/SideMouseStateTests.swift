import XCTest
@testable import VoiceRewriteInteraction

final class SideMouseStateTests: XCTestCase {
    func testSideMouseBindingsRejectConflictsAndPreserveUnboundButtons() {
        XCTAssertTrue(SideMouseBindings().isValid)
        XCTAssertFalse(SideMouseBindings(dictationButton: 4, rewriteButton: 4).isValid)
        XCTAssertFalse(SideMouseBindings(dictationButton: 5, rewriteButton: nil).isValid)
        let mapping = SideMouseBindings(dictationButton: nil, rewriteButton: 3)
        XCTAssertTrue(mapping.isValid)
        XCTAssertNil(mapping.action(for: 4))
        XCTAssertEqual(mapping.action(for: 3), .rewrite)
    }

    func testMacAuxiliaryButtonNumbers() {
        XCTAssertEqual(SideMouseAction(rawValue: 3), .dictation)
        XCTAssertEqual(SideMouseAction(rawValue: 4), .rewrite)
        XCTAssertNil(SideMouseAction(rawValue: 2))
        XCTAssertNil(SideMouseAction(rawValue: 5))
    }

    func testShortPressReplaysAndExpiredTimerCannotActivate() {
        var state = SideButtonHoldState()
        let token = state.press(3)
        XCTAssertNotNil(token)
        XCTAssertEqual(state.release(3), .short)
        XCTAssertFalse(state.activate(3, token: token!))
        XCTAssertEqual(state.release(3), .ignored)
    }

    func testLongPressActivatesOnceAndReleasesOnce() {
        var state = SideButtonHoldState()
        let token = state.press(4)!
        XCTAssertTrue(state.activate(4, token: token))
        XCTAssertFalse(state.activate(4, token: token))
        XCTAssertEqual(state.release(4), .held)
        XCTAssertEqual(state.release(4), .ignored)
    }

    func testDoubleDownAndLateTimerCannotStartNewPress() {
        var state = SideButtonHoldState()
        let first = state.press(3)!
        XCTAssertNil(state.press(3))
        XCTAssertEqual(state.release(3), .short)
        let second = state.press(3)!
        XCTAssertNotEqual(first, second)
        XCTAssertFalse(state.activate(3, token: first))
        XCTAssertTrue(state.activate(3, token: second))
        XCTAssertTrue(state.reset())
        XCTAssertEqual(state.release(3), .ignored)
    }

    func testDifferentButtonsHaveIndependentTokens() {
        var state = SideButtonHoldState()
        let a = state.press(3)!
        let b = state.press(4)!
        XCTAssertTrue(state.activate(3, token: a))
        XCTAssertEqual(state.release(4), .short)
        XCTAssertEqual(state.release(3), .held)
        XCTAssertFalse(state.activate(4, token: b))
    }
}
