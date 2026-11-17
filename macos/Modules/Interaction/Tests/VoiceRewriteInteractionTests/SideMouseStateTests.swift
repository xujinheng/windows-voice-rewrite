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
        XCTAssertEqual(mapping.button(for: .rewrite), 3)
        XCTAssertNil(mapping.button(for: .dictation))
        let swapped = SideMouseBindings(dictationButton: 4, rewriteButton: 3)
        XCTAssertEqual(swapped.button(for: .dictation), 4)
        XCTAssertEqual(swapped.button(for: .rewrite), 3)
    }

    func testMacAuxiliaryButtonNumbers() {
        XCTAssertEqual(SideMouseAction(rawValue: 3), .dictation)
        XCTAssertEqual(SideMouseAction(rawValue: 4), .rewrite)
        XCTAssertNil(SideMouseAction(rawValue: 2))
        XCTAssertNil(SideMouseAction(rawValue: 5))
    }

    func testPressReleasedBeforeThresholdDiscardedButNavigationIsShort() {
        var state = SideButtonHoldState()
        let token = state.press(3)!
        XCTAssertTrue(state.hasPendingPress)
        // The native microphone may already be recording; the input state
        // must still classify release as short and reject the delayed timer.
        XCTAssertEqual(state.release(3), .short)
        XCTAssertFalse(state.activate(3, token: token))
        XCTAssertFalse(state.hasPendingPress)
        XCTAssertNotNil(state.press(3))
        XCTAssertEqual(state.release(3), .short)
    }

    func testReleaseImmediatelyAfterPromotionKeepsHoldClassification() {
        var state = SideButtonHoldState()
        let token = state.press(3)!
        XCTAssertTrue(state.activate(3, token: token))
        XCTAssertEqual(state.release(3), .held)
        XCTAssertFalse(state.activate(3, token: token))
        XCTAssertEqual(state.release(3), .ignored)
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

    func testPausedOrReboundPendingMouseClickIsReplayedOnlyOnce() {
        var state = SideButtonHoldState()
        let pendingToken = state.press(3)!
        XCTAssertFalse(state.reset())
        XCTAssertFalse(state.activate(3, token: pendingToken))
        XCTAssertEqual(state.takeCancelledRelease(3), .short)
        XCTAssertEqual(state.takeCancelledRelease(3), .ignored)
        XCTAssertEqual(state.release(3), .ignored)
    }

    func testCancelledHoldNeverReplaysNavigationClick() {
        var state = SideButtonHoldState()
        let heldToken = state.press(4)!
        XCTAssertTrue(state.activate(4, token: heldToken))
        XCTAssertTrue(state.reset())
        XCTAssertEqual(state.takeCancelledRelease(4), .held)
        XCTAssertEqual(state.takeCancelledRelease(4), .ignored)
        XCTAssertEqual(state.release(4), .ignored)
    }

    func testCancelledPressMustFinishBeforeNewDownIsAccepted() {
        var state = SideButtonHoldState()
        let first = state.press(3)!
        XCTAssertFalse(state.reset())
        XCTAssertNil(state.press(3))
        XCTAssertFalse(state.activate(3, token: first))
        XCTAssertEqual(state.takeCancelledRelease(3), .short)
        XCTAssertNotNil(state.press(3))
        XCTAssertEqual(state.release(3), .short)
    }

    func testDisabledTapClearsCancelledReleaseMarkers() {
        var state = SideButtonHoldState()
        _ = state.press(3)
        XCTAssertFalse(state.reset())
        XCTAssertEqual(state.takeCancelledRelease(3), .short)
        _ = state.press(4)
        XCTAssertFalse(state.reset())
        XCTAssertFalse(state.clear())
        XCTAssertEqual(state.takeCancelledRelease(4), .ignored)
        XCTAssertNotNil(state.press(4))
        XCTAssertEqual(state.release(4), .short)
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
