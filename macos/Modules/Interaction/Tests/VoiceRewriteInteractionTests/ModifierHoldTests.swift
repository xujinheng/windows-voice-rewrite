import XCTest
import CoreGraphics
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

    func testLeftAndRightModifierCaptureFromFlagChanges() {
        // The event's physical keycode identifies the side. Do not ask the
        // asynchronous CGEventSource cache if the opposite key is down.
        for key in MacModifierKey.allCases {
            XCTAssertEqual(
                MacModifierKey.captureCandidate(keyCode: key.keyCode, flags: key.groupFlag),
                key, "must learn \(key.rawValue) from its own flagsChanged event")
            XCTAssertNil(MacModifierKey.captureCandidate(keyCode: key.keyCode, flags: []))
            XCTAssertNil(MacModifierKey.captureCandidate(
                keyCode: key.keyCode, flags: key.groupFlag.union(.maskShift)))
            XCTAssertNil(MacModifierKey.captureCandidate(
                keyCode: key.keyCode, flags: key.groupFlag.union(
                    key == .leftCommand || key == .rightCommand ? .maskAlternate : .maskCommand)))
        }
        XCTAssertNil(MacModifierKey.captureCandidate(keyCode: 0, flags: .maskControl))
        XCTAssertNil(MacModifierKey.captureCandidate(keyCode: 56, flags: .maskShift))
        XCTAssertEqual(MacModifierKey.captureCandidate(keyCode: 54, flags: .maskCommand), .rightCommand)
        XCTAssertEqual(MacModifierKey.captureCandidate(keyCode: 61, flags: .maskAlternate), .rightOption)
        XCTAssertEqual(MacModifierKey.captureCandidate(keyCode: 62, flags: .maskControl), .rightControl)
    }

    func testHIDUsageCodesAndKeyboardIdentityFailClosed() {
        let expected: [Int: MacModifierKey] = [
            0xE0: .leftControl, 0xE4: .rightControl,
            0xE2: .leftOption, 0xE6: .rightOption,
            0xE3: .leftCommand, 0xE7: .rightCommand
        ]
        for (usage, key) in expected {
            XCTAssertEqual(MacKeyboardHIDKey.modifier(usage: usage), key)
        }
        XCTAssertNil(MacKeyboardHIDKey.modifier(usage: 0x04)) // normal letter
        XCTAssertNil(MacKeyboardHIDKey.modifier(usage: 0xE1)) // Shift
        XCTAssertEqual(MacKeyboardDevice.identity(
            vendor: 1452, product: 100, location: 0, serial: "", builtIn: true),
            "builtin:1452:100:0")
        XCTAssertEqual(MacKeyboardDevice.identity(
            vendor: 111, product: 222, location: 4, serial: "abc", builtIn: false),
            "serial:111:222:abc")
        XCTAssertEqual(MacKeyboardDevice.identity(
            vendor: 111, product: 222, location: 4, serial: "", builtIn: false),
            "port:111:222:4")
        XCTAssertNil(MacKeyboardDevice.identity(
            vendor: 111, product: 222, location: 0, serial: "", builtIn: false))
        XCTAssertTrue(MacKeyboardDevice(id: "a", name: "Internal", builtIn: true).title.contains("内建"))
        XCTAssertTrue(MacKeyboardDevice(id: "b", name: "USB", builtIn: false).title.contains("外接"))
        XCTAssertFalse(MacKeyboardDevice(id: "a", name: "Unknown", builtIn: false,
                                         selectable: false).selectable)
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
