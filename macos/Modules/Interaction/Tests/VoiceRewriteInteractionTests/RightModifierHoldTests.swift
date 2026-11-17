import XCTest
@testable import VoiceRewriteInteraction

#if os(macOS)
final class RightModifierHoldTests: XCTestCase {
    func testShortRightControlDoesNotTriggerVoice() {
        var state = RightModifierHoldState()
        let token = state.press(.control, unmodified: true)
        XCTAssertNotNil(token)
        XCTAssertNil(state.release(.control))
        XCTAssertFalse(state.activate(.control, token: token!))
    }

    func testRightControlHoldAndReleaseTriggersExactlyOnce() {
        var state = RightModifierHoldState()
        let token = state.press(.control, unmodified: true)!
        XCTAssertTrue(state.activate(.control, token: token))
        XCTAssertFalse(state.activate(.control, token: token))
        XCTAssertEqual(state.release(.control), .control)
        XCTAssertNil(state.release(.control))
        XCTAssertNil(state.clear())
    }

    func testRightOptionRequiresAloneAndRearmsAfterPhysicalRelease() {
        var state = RightModifierHoldState()
        XCTAssertNil(state.press(.option, unmodified: false))
        XCTAssertFalse(state.activate(.option, token: 1))
        XCTAssertNil(state.release(.option))
        let token = state.press(.option, unmodified: true)!
        XCTAssertTrue(state.activate(.option, token: token))
        XCTAssertEqual(state.release(.option), .option)
    }

    func testOrdinaryShortcutCancelsPendingHoldUntilRelease() {
        var state = RightModifierHoldState()
        let token = state.press(.control, unmodified: true)!
        XCTAssertNil(state.interrupt())
        XCTAssertFalse(state.activate(.control, token: token))
        XCTAssertNil(state.press(.control, unmodified: true))
        XCTAssertNil(state.release(.control))
        let next = state.press(.control, unmodified: true)!
        XCTAssertTrue(state.activate(.control, token: next))
        XCTAssertEqual(state.release(.control), .control)
    }

    func testTypingOrAnotherModifierCancelsActiveHoldWithoutSubmitting() {
        var state = RightModifierHoldState()
        let token = state.press(.option, unmodified: true)!
        XCTAssertTrue(state.activate(.option, token: token))
        XCTAssertEqual(state.interrupt(), .option)
        XCTAssertNil(state.release(.option))
        XCTAssertNil(state.clear())
    }

    func testPermissionResetCannotActivateStaleTimer() {
        var state = RightModifierHoldState()
        let first = state.press(.control, unmodified: true)!
        XCTAssertNil(state.clear())
        let second = state.press(.control, unmodified: true)!
        XCTAssertFalse(state.activate(.control, token: first))
        XCTAssertTrue(state.activate(.control, token: second))
        XCTAssertEqual(state.clear(), .control)
        XCTAssertNil(state.release(.control))
    }

    func testRightKeyVirtualCodesAndHoldThreshold() {
        XCTAssertEqual(RightModifierKey.control.keyCode, 62)
        XCTAssertEqual(RightModifierKey.option.keyCode, 61)
        XCTAssertEqual(SideHold.threshold, 0.250, accuracy: 0.0001)
    }
}
#endif
