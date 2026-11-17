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
    func testRepeatedCancelAndRestartRejectEveryPreviousStart() throws {
        var gate = SystemAudioStartGate()
        let first = try XCTUnwrap(gate.begin(streamExists: false))
        gate.invalidate()
        let second = try XCTUnwrap(gate.begin(streamExists: false))
        gate.invalidate()
        let third = try XCTUnwrap(gate.begin(streamExists: false))
        gate.finish(first)
        gate.finish(second)
        XCTAssertFalse(gate.accepts(first))
        XCTAssertFalse(gate.accepts(second))
        XCTAssertTrue(gate.accepts(third))
        gate.finish(third)
        XCTAssertNil(gate.begin(streamExists: true))
    }

    func testSlowMainActorAudioDeliveryIsBoundedAndCanRecover() {
        var gate = SystemAudioDeliveryGate(limit: 2)
        XCTAssertTrue(gate.reserve(active: true))
        XCTAssertTrue(gate.reserve(active: true))
        XCTAssertFalse(gate.reserve(active: true))
        XCTAssertEqual(gate.pending, 2)
        gate.complete()
        XCTAssertTrue(gate.reserve(active: true))
        XCTAssertFalse(gate.reserve(active: false))
        gate.complete()
        gate.complete()
        gate.complete() // a canceled completion cannot underflow the counter
        XCTAssertEqual(gate.pending, 0)
        XCTAssertTrue(gate.reserve(active: true))
    }

}
