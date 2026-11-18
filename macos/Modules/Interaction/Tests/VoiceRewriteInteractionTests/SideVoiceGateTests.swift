import XCTest
@testable import VoiceRewriteInteraction

final class SideVoiceGateTests: XCTestCase {
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

    func testSideRewriteNeverSpeculativelyStartsMicrophoneAcrossNativeAppStates() {
        // Selection capture belongs to Go. Native Mouse5 pre-capture is
        // forbidden even if AX is available and microphone permission exists.
        for authorized in [false, true] {
            for paused in [false, true] {
                for meetingRunning in [false, true] {
                    for busy in [false, true] {
                        XCTAssertFalse(shouldPreCaptureDictation(
                            action: .rewrite, authorized: authorized,
                            paused: paused, meetingRunning: meetingRunning, busy: busy),
                            "Mouse5 rewrite must not start microphone before Go admission")
                    }
                }
            }
        }
    }

    func testSideDictationPreCaptureIsDeniedForEveryNativeBlockerCombination() {
        // Partial combinations had only been checked one at a time. Verify
        // that any independent OS blocker prevents speculative Mouse4 audio.
        for paused in [false, true] {
            for meetingRunning in [false, true] {
                for busy in [false, true] {
                    for authorized in [false, true] {
                        let allowed = authorized && !paused && !meetingRunning && !busy
                        XCTAssertEqual(shouldPreCaptureDictation(
                            action: .dictation, authorized: authorized,
                            paused: paused, meetingRunning: meetingRunning, busy: busy), allowed)
                    }
                }
            }
        }
    }
}
