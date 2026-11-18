import XCTest
@testable import Microphone

final class PCM16BufferTests: XCTestCase {
    func testDeniedPermissionReportsRecoveryAndRestrictedIsDistinct() {
        XCTAssertTrue(MicrophoneError.permissionDenied.localizedDescription.contains("系统设置"))
        XCTAssertTrue(MicrophoneError.permissionDenied.localizedDescription.contains("VoiceRewrite"))
        XCTAssertFalse(MicrophoneError.permissionRestricted.localizedDescription.contains("重启应用"))
    }

    func testSignedLittleEndianAndClipping() throws {
        var pcm = PCM16Buffer()
        try pcm.append([-2, -1, -0.5, 0, 0.5, 1, 2])
        let data = try pcm.finish()
        XCTAssertEqual(Array(data), [
            0x00, 0x80, 0x00, 0x80, 0x00, 0xc0, 0x00, 0x00,
            0x00, 0x40, 0xff, 0x7f, 0xff, 0x7f
        ])
        XCTAssertEqual(data.count, 7 * MemoryLayout<Int16>.size)
    }

    func testShortRecordingUsesBoundedInitialReserveAndCanStillGrow() throws {
        XCTAssertEqual(PCM16Buffer.initialReserveFrames, PCM16Buffer.sampleRate * 5)
        XCTAssertLessThan(PCM16Buffer.initialReserveFrames, PCM16Buffer.maximumFrames)
        var recording = PCM16Buffer()
        let sixSeconds = Array(repeating: Float(0), count: PCM16Buffer.sampleRate * 6)
        try recording.append(sixSeconds)
        XCTAssertEqual(try recording.finish().count, 6 * PCM16Buffer.sampleRate * 2)
    }

    func testSilenceAndBufferBoundaries() throws {
        var pcm = PCM16Buffer()
        try pcm.append([Float]())
        try pcm.append(Array(repeating: 0, count: 1))
        try pcm.append(Array(repeating: 0, count: 511))
        try pcm.append(Array(repeating: 0, count: 512))
        XCTAssertEqual(pcm.frameCount, 1_024)
        XCTAssertEqual(try pcm.finish(), Data(repeating: 0, count: 2_048))
    }

    func testMaximumDurationAndOneSampleOverflow() throws {
        var pcm = PCM16Buffer()
        let chunk = Array(repeating: Float(0), count: 1_000)
        for _ in 0..<2_880 { try pcm.append(chunk) }
        XCTAssertEqual(pcm.frameCount, 2_880_000)
        XCTAssertThrowsError(try pcm.append([0])) { error in
            XCTAssertEqual(error as? MicrophoneError, .durationExceeded)
        }
        // AVAudioConverter may supply a final tail after the duration cap.
        // The recorder must preserve already captured speech, never fail it.
        try pcm.appendCapped([0, 0].withUnsafeBufferPointer { $0 })
        XCTAssertEqual(pcm.frameCount, PCM16Buffer.maximumFrames)
        XCTAssertEqual(try pcm.finish().count, PCM16Buffer.maximumBytes)
    }

    func testCancelDiscardAndRepeatedStop() throws {
        var cancelled = PCM16Buffer()
        try cancelled.append([1, -1])
        cancelled.cancel()
        XCTAssertEqual(cancelled.frameCount, 0)
        XCTAssertThrowsError(try cancelled.finish()) { error in
            XCTAssertEqual(error as? MicrophoneError, .notRecording)
        }
        XCTAssertThrowsError(try cancelled.append([0]))

        var finished = PCM16Buffer()
        try finished.append([0.25])
        XCTAssertEqual(try finished.finish().count, 2)
        XCTAssertThrowsError(try finished.finish()) { error in
            XCTAssertEqual(error as? MicrophoneError, .notRecording)
        }
    }

    func testEmptyInputCannotSucceed() {
        var pcm = PCM16Buffer()
        XCTAssertThrowsError(try pcm.finish()) { error in
            XCTAssertEqual(error as? MicrophoneError, .noAudioCaptured)
        }
    }

    func testNaNAndInfinityFailWithoutAppendingPartialChunk() throws {
        var pcm = PCM16Buffer()
        XCTAssertThrowsError(try pcm.append([0.5, .nan])) { error in
            XCTAssertEqual(error as? MicrophoneError, .invalidAudioSample)
        }
        XCTAssertThrowsError(try pcm.append([.infinity]))
        XCTAssertEqual(pcm.frameCount, 0)
        try pcm.append([0])
        XCTAssertEqual(Array(try pcm.finish()), [0, 0])
    }
}
