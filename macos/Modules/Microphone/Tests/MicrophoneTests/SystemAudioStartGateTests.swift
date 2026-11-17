import XCTest
@testable import Microphone

final class SystemAudioStartGateTests: XCTestCase {
    func testStopDuringSourceDiscoveryCannotResurrectOldCapture() throws {
        var gate = SystemAudioStartGate()
        let first = try XCTUnwrap(gate.begin(streamExists: false))
        XCTAssertTrue(gate.accepts(first))
        gate.invalidate() // stop() while SCShareableContent is still suspended
        XCTAssertFalse(gate.accepts(first))
        gate.finish(first) // old start() resumes and executes defer
        let second = try XCTUnwrap(gate.begin(streamExists: false))
        XCTAssertNotEqual(first, second)
        XCTAssertTrue(gate.accepts(second))
        gate.finish(second)
    }

    func testOverlappingStartsAreRejectedAndLateFinishCannotClearNewStart() throws {
        var gate = SystemAudioStartGate()
        let first = try XCTUnwrap(gate.begin(streamExists: false))
        XCTAssertNil(gate.begin(streamExists: false))
        gate.invalidate()
        let second = try XCTUnwrap(gate.begin(streamExists: false))
        gate.finish(first)
        XCTAssertTrue(gate.accepts(second))
        XCTAssertNil(gate.begin(streamExists: false))
        gate.finish(second)
        XCTAssertNotNil(gate.begin(streamExists: false))
    }

    func testAlreadyRunningStreamBlocksNewStart() {
        var gate = SystemAudioStartGate()
        XCTAssertNil(gate.begin(streamExists: true))
        XCTAssertNotNil(gate.begin(streamExists: false))
    }
}
