import XCTest
@testable import VoiceRewriteInteraction

final class SideVoiceGateTests: XCTestCase {
    func testKeyboardKeyReleaseDuringMicrophoneStartStopsOnce() {
        var gate = KeyboardHoldGate()
        XCTAssertTrue(gate.begin(.dictation))
        XCTAssertFalse(gate.begin(.rewrite))
        XCTAssertEqual(gate.release(.dictation, starting: true, recording: false), .none)
        XCTAssertTrue(gate.releasedDuringStart)
        XCTAssertFalse(gate.begin(.dictation))
        XCTAssertEqual(gate.finishStart(.dictation, recording: true), .stop)
        XCTAssertNil(gate.action)
        XCTAssertEqual(gate.finishStart(.dictation, recording: true), .none)
        XCTAssertTrue(gate.begin(.rewrite))
        XCTAssertEqual(gate.release(.rewrite, starting: false, recording: true), .stop)
    }

    func testKeyboardFailureAndCancellationPermitRapidNextPress() {
        var gate = KeyboardHoldGate()
        XCTAssertTrue(gate.begin(.rewrite))
        XCTAssertEqual(gate.release(.dictation, starting: true, recording: false), .none)
        XCTAssertEqual(gate.release(.rewrite, starting: true, recording: false), .none)
        XCTAssertEqual(gate.finishStart(.rewrite, recording: false), .clear)
        XCTAssertTrue(gate.begin(.dictation))
        gate.clear()
        XCTAssertEqual(gate.release(.dictation, starting: false, recording: true), .none)
        XCTAssertTrue(gate.begin(.rewrite))
        XCTAssertEqual(gate.release(.rewrite, starting: false, recording: false), .clear)
    }

    func testEarlyMicrophoneOnlyStartsForAuthorizedIdleDictation() {
        XCTAssertTrue(shouldPreCaptureDictation(action: .dictation, authorized: true,
                                                paused: false, meetingRunning: false, busy: false))
        XCTAssertFalse(shouldPreCaptureDictation(action: .rewrite, authorized: true,
                                                 paused: false, meetingRunning: false, busy: false))
        XCTAssertFalse(shouldPreCaptureDictation(action: .dictation, authorized: false,
                                                 paused: false, meetingRunning: false, busy: false))
        XCTAssertFalse(shouldPreCaptureDictation(action: .dictation, authorized: true,
                                                 paused: true, meetingRunning: false, busy: false))
        XCTAssertFalse(shouldPreCaptureDictation(action: .dictation, authorized: true,
                                                 paused: false, meetingRunning: true, busy: false))
        XCTAssertFalse(shouldPreCaptureDictation(action: .dictation, authorized: true,
                                                 paused: false, meetingRunning: false, busy: true))
    }

    func testSideReleaseDuringEarlyMicrophoneStartMustWaitForPromotion() {
        var gate = SideVoiceGate()
        gate.began(.dictation)
        XCTAssertEqual(gate.release(.dictation, starting: true, recording: false), .wait)
        XCTAssertEqual(gate.finishStart(.dictation, recording: true), .stop)
        gate.clear()
        XCTAssertEqual(gate.finishStart(.dictation, recording: true), .none)
    }

    func testHoldStartsOnlyWhenIdle() {
        var gate = SideVoiceGate()
        XCTAssertEqual(gate.hold(paused: true, busy: false), .dismissPreview)
        XCTAssertEqual(gate.hold(paused: false, busy: true), .dismissPreview)
        gate.began(.dictation)
        XCTAssertEqual(gate.hold(paused: false, busy: false), .dismissPreview)
        gate.clear()
        gate.suppressNextHold()
        XCTAssertEqual(gate.hold(paused: false, busy: false), .ignored)
        XCTAssertFalse(gate.ignoreNext)
        XCTAssertEqual(gate.hold(paused: false, busy: false), .start)
    }

    func testReleaseWaitsUntilRecordingHasStarted() {
        var gate = SideVoiceGate()
        gate.began(.rewrite)
        XCTAssertEqual(gate.release(.dictation, starting: true, recording: false), .none)
        XCTAssertEqual(gate.release(.rewrite, starting: true, recording: false), .wait)
        XCTAssertEqual(gate.pendingRelease, .rewrite)
        XCTAssertEqual(gate.finishStart(.dictation, recording: true), .none)
        XCTAssertEqual(gate.finishStart(.rewrite, recording: true), .stop)
        XCTAssertNil(gate.pendingRelease)
        XCTAssertEqual(gate.active, .rewrite)
        gate.began(.dictation)
        XCTAssertEqual(gate.release(.dictation, starting: false, recording: false), .clear)
        XCTAssertNil(gate.active)
    }
}
