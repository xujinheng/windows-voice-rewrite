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

    func testReusableScratchRollbackPreservesEarlierChunks() throws {
        var pcm = PCM16Buffer(reserveFrames: 2)
        try pcm.append([-1, 0.5])
        let before = pcm.frameCount
        XCTAssertThrowsError(try pcm.append([0.25, .nan, 1])) { error in
            XCTAssertEqual(error as? MicrophoneError, .invalidAudioSample)
        }
        XCTAssertEqual(pcm.frameCount, before)
        try pcm.append([-0.5, 1])
        XCTAssertEqual(Array(try pcm.finish()),
                       [0x00, 0x80, 0x00, 0x40, 0x00, 0xc0, 0xff, 0x7f])
    }

    func testReusableScratchMatchesReferenceAcrossChunkSizes() throws {
        var pcm = PCM16Buffer(reserveFrames: 1)
        var expected = [UInt8]()
        for size in [1, 7, 512, 4_096, 17] {
            let samples = (0..<size).map { Float(($0 % 31) - 15) / 10 }
            try pcm.append(samples)
            for sample in samples {
                let clipped = max(-1.0, min(1.0, sample))
                let magnitude = Int((clipped * 32_768.0).rounded())
                let word = UInt16(bitPattern: Int16(clamping: magnitude))
                expected.append(UInt8(truncatingIfNeeded: word))
                expected.append(UInt8(truncatingIfNeeded: word >> 8))
            }
        }
        XCTAssertEqual(Array(try pcm.finish()), expected)
    }

    /// Same-run A/B of the original temporary-array encoder against the
    /// production reusable-scratch encoder. These are CPU hot-path microbenchmarks,
    /// not key-to-audio or end-to-end ASR latency measurements.
    func testPCM16EncoderHotPathABEvidence() throws {
        let samples = (0..<4_096).map { Float(($0 % 257) - 128) / 128 }
        let chunkCount = 128
        let totalFrames = samples.count * chunkCount

        func previousEncoder() -> Data {
            var bytes = Data()
            bytes.reserveCapacity(totalFrames * 2)
            for _ in 0..<chunkCount {
                var chunk = [UInt8]()
                chunk.reserveCapacity(samples.count * 2)
                for sample in samples {
                    let clipped = max(-1.0, min(1.0, sample))
                    let magnitude = Int((clipped * 32_768.0).rounded())
                    let word = UInt16(bitPattern: Int16(clamping: magnitude))
                    chunk.append(UInt8(truncatingIfNeeded: word))
                    chunk.append(UInt8(truncatingIfNeeded: word >> 8))
                }
                bytes.append(contentsOf: chunk)
            }
            return bytes
        }

        func currentEncoder() throws -> Data {
            var encoder = PCM16Buffer(reserveFrames: totalFrames)
            for _ in 0..<chunkCount { try encoder.append(samples) }
            return try encoder.finish()
        }

        func middle(_ values: [Double]) -> Double {
            values.sorted()[values.count / 2]
        }

        var oldTimes = [Double]()
        var newTimes = [Double]()
        for round in 0..<5 {
            let previous: Data
            let current: Data
            if round.isMultiple(of: 2) {
                let started = ProcessInfo.processInfo.systemUptime
                previous = previousEncoder()
                oldTimes.append((ProcessInfo.processInfo.systemUptime - started) * 1_000)
                let next = ProcessInfo.processInfo.systemUptime
                current = try currentEncoder()
                newTimes.append((ProcessInfo.processInfo.systemUptime - next) * 1_000)
            } else {
                let started = ProcessInfo.processInfo.systemUptime
                current = try currentEncoder()
                newTimes.append((ProcessInfo.processInfo.systemUptime - started) * 1_000)
                let next = ProcessInfo.processInfo.systemUptime
                previous = previousEncoder()
                oldTimes.append((ProcessInfo.processInfo.systemUptime - next) * 1_000)
            }
            XCTAssertEqual(current, previous)
        }
        print("PCM16_ENCODING_AB legacy_p50_ms=\(middle(oldTimes)) scratch_p50_ms=\(middle(newTimes)) rounds=5 frames_per_round=\(totalFrames)")
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
