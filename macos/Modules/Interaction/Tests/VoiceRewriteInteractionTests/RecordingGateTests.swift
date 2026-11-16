import XCTest
@testable import VoiceRewriteInteraction

final class RecordingGateTests: XCTestCase {
    func testRecordingToggleStopsAnActiveTakeAndIgnoresAPause() {
        XCTAssertEqual(recordingToggle(paused: true, recording: false, starting: false, processing: false), .ignore)
        XCTAssertEqual(recordingToggle(paused: true, recording: true, starting: false, processing: false), .stop)
        XCTAssertEqual(recordingToggle(paused: false, recording: false, starting: true, processing: false), .ignore)
        XCTAssertEqual(recordingToggle(paused: false, recording: false, starting: false, processing: false), .start)
    }

    func testRewriteToggleRequiresALiveTarget() {
        XCTAssertEqual(rewriteToggle(paused: false, recording: true, voiceSelection: false, starting: false, processing: false, targetPID: 4), .ignore)
        XCTAssertEqual(rewriteToggle(paused: false, recording: true, voiceSelection: true, starting: false, processing: false, targetPID: 4), .stop)
        XCTAssertEqual(rewriteToggle(paused: false, recording: false, voiceSelection: false, starting: false, processing: false, targetPID: 0), .missingTarget)
        XCTAssertEqual(rewriteToggle(paused: false, recording: false, voiceSelection: false, starting: false, processing: false, targetPID: 9), .start(9))
        XCTAssertEqual(rewriteToggle(paused: true, recording: false, voiceSelection: false, starting: false, processing: false, targetPID: 9), .ignore)
    }

    func testSelectionTaskRequiresATargetAndInstruction() {
        XCTAssertEqual(selectionTaskStart(paused: true, targetPID: 4, instruction: "改短"), .paused)
        XCTAssertEqual(selectionTaskStart(paused: false, targetPID: nil, instruction: "改短"), .missingTarget)
        XCTAssertEqual(selectionTaskStart(paused: false, targetPID: 4, instruction: " \n"), .missingInstruction)
        XCTAssertEqual(selectionTaskStart(paused: false, targetPID: 4, instruction: "改短"), .start(4))
    }
}
