import XCTest
@testable import VoiceRewriteInteraction

#if os(macOS)
final class MacModifierHoldTests: XCTestCase {
    func testShortRightControlDoesNotTriggerVoice() {
        var state = MacModifierHoldState()
        let token = state.press(.rightControl, unmodified: true)
        XCTAssertNotNil(token)
        XCTAssertNil(state.release(.rightControl))
        XCTAssertFalse(state.activate(.rightControl, token: token!))
    }

    func testRightControlHoldAndReleaseTriggersExactlyOnce() {
        var state = MacModifierHoldState()
        let token = state.press(.rightControl, unmodified: true)!
        XCTAssertTrue(state.activate(.rightControl, token: token))
        XCTAssertFalse(state.activate(.rightControl, token: token))
        XCTAssertEqual(state.release(.rightControl), .rightControl)
        XCTAssertNil(state.release(.rightControl))
        XCTAssertNil(state.clear())
    }

    func testRightOptionRequiresAloneAndRearmsAfterPhysicalRelease() {
        var state = MacModifierHoldState()
        XCTAssertNil(state.press(.rightOption, unmodified: false))
        XCTAssertFalse(state.activate(.rightOption, token: 1))
        XCTAssertNil(state.release(.rightOption))
        let token = state.press(.rightOption, unmodified: true)!
        XCTAssertTrue(state.activate(.rightOption, token: token))
        XCTAssertEqual(state.release(.rightOption), .rightOption)
    }

    func testOrdinaryShortcutCancelsPendingHoldUntilRelease() {
        var state = MacModifierHoldState()
        let token = state.press(.rightControl, unmodified: true)!
        XCTAssertNil(state.interrupt())
        XCTAssertFalse(state.activate(.rightControl, token: token))
        XCTAssertNil(state.press(.rightControl, unmodified: true))
        XCTAssertNil(state.release(.rightControl))
        let next = state.press(.rightControl, unmodified: true)!
        XCTAssertTrue(state.activate(.rightControl, token: next))
        XCTAssertEqual(state.release(.rightControl), .rightControl)
    }

    func testTypingOrAnotherModifierCancelsActiveHoldWithoutSubmitting() {
        var state = MacModifierHoldState()
        let token = state.press(.rightOption, unmodified: true)!
        XCTAssertTrue(state.activate(.rightOption, token: token))
        XCTAssertEqual(state.interrupt(), .rightOption)
        XCTAssertNil(state.release(.rightOption))
        XCTAssertNil(state.clear())
    }

    func testPermissionResetCannotActivateStaleTimer() {
        var state = MacModifierHoldState()
        let first = state.press(.rightControl, unmodified: true)!
        XCTAssertNil(state.clear())
        let second = state.press(.rightControl, unmodified: true)!
        XCTAssertFalse(state.activate(.rightControl, token: first))
        XCTAssertTrue(state.activate(.rightControl, token: second))
        XCTAssertEqual(state.clear(), .rightControl)
        XCTAssertNil(state.release(.rightControl))
    }

    func testAllSixPhysicalKeysHaveUniqueCodesAndCanonicalNames() {
        let physical: [MacModifierKey: Int64] = [
            .leftControl: 59, .rightControl: 62,
            .leftOption: 58, .rightOption: 61,
            .leftCommand: 55, .rightCommand: 54
        ]
        XCTAssertEqual(physical.count, MacModifierKey.allCases.count)
        XCTAssertEqual(Set(physical.values).count, 6)
        for (key, code) in physical {
            XCTAssertEqual(key.keyCode, code)
            XCTAssertEqual(MacModifierKey(keyCode: code), key)
            XCTAssertEqual(MacModifierKey(canonical: key.rawValue), key)
            XCTAssertNotEqual(key.keyCode, Int64(key.otherSideKeyCode))
            XCTAssertEqual(key.captureFlag, MacModifierKey(keyCode: code)?.captureFlag)
        }
        XCTAssertNil(MacModifierKey(keyCode: 0))
        XCTAssertNil(MacModifierKey(canonical: "Command"))
        XCTAssertEqual(SideHold.threshold, 0.250, accuracy: 0.0001)
    }

    func testEachModifierHasOwnShortAndHeldTransitions() {
        for key in MacModifierKey.allCases {
            var state = MacModifierHoldState()
            let shortToken = state.press(key, unmodified: true)!
            XCTAssertNil(state.release(key), "\(key) short press must remain silent")
            XCTAssertFalse(state.activate(key, token: shortToken))
            let hold = state.press(key, unmodified: true)!
            XCTAssertTrue(state.activate(key, token: hold))
            XCTAssertEqual(state.release(key), key)
            XCTAssertNil(state.release(key))
        }
    }

    func testModifiersCannotTriggerAcrossSidesAndComboCancelsHold() {
        for key in MacModifierKey.allCases {
            var state = MacModifierHoldState()
            let token = state.press(key, unmodified: true)!
            let opposite = MacModifierKey(keyCode: Int64(key.otherSideKeyCode))!
            XCTAssertNil(state.press(opposite, unmodified: false))
            XCTAssertFalse(state.activate(key, token: token))
            XCTAssertNil(state.release(key))
            let next = state.press(key, unmodified: true)!
            XCTAssertTrue(state.activate(key, token: next))
            XCTAssertEqual(state.interrupt(), key)
            XCTAssertNil(state.release(key))
        }
    }
}
#endif
