#if os(macOS)
import AVFoundation
import Foundation
import XCTest
@testable import Microphone

/// Offline measurements, not a virtual-microphone or ASR-quality test.
/// Run only via VOICE_REWRITE_PERF=1 to keep normal unit tests fast.
final class MicrophonePerformanceTests: XCTestCase {
    private func clock() -> Double { ProcessInfo.processInfo.systemUptime }

    private func p95(_ values: [Double]) -> Double {
        let sorted = values.sorted()
        return sorted[max(0, Int(ceil(Double(sorted.count) * 0.95)) - 1)]
    }

    private func metric(_ samples: [Double], audioSeconds: Double) -> [String: Double] {
        let sum = samples.reduce(0, +)
        return [
            "elapsed_ms": sum * 1_000,
            "p95_chunk_ms": p95(samples) * 1_000,
            "realtime_factor": sum / audioSeconds,
            "iterations": Double(samples.count),
        ]
    }

    func testOfflineAudioPerformanceAndWriteJSONReport() throws {
        try XCTSkipUnless(ProcessInfo.processInfo.environment["VOICE_REWRITE_PERF"] == "1",
                          "Performance work runs in the dedicated macOS CI matrix")

        // 30 s of PCM16 at 16 kHz, delivered in realistic 100 ms chunks.
        // This calls production PCM16Buffer.append and exercises buffer growth.
        let samples = [Float](repeating: 0.125, count: 1_600)
        var pcm = PCM16Buffer()
        var pcmTimings: [Double] = []
        for _ in 0..<300 {
            let start = clock()
            try pcm.append(samples)
            pcmTimings.append(clock() - start)
        }
        let pcmBytes = try pcm.finish()
        XCTAssertEqual(pcmBytes.count, 30 * 16_000 * 2)

        // 10 s of 48 kHz stereo converted with the production converter.
        // No microphone permissions, physical devices or network required.
        let inputFormat = try XCTUnwrap(AVAudioFormat(commonFormat: .pcmFormatFloat32,
            sampleRate: 48_000, channels: 2, interleaved: false))
        let input = try XCTUnwrap(AVAudioPCMBuffer(pcmFormat: inputFormat,
                                                   frameCapacity: 4_000))
        input.frameLength = 4_000
        for channel in 0..<2 {
            for frame in 0..<4_000 {
                input.floatChannelData![channel][frame] = 0.05
            }
        }
        let converter = try NativeAudioConverter(inputFormat: inputFormat)
        var output = PCM16Buffer()
        var convertTimings: [Double] = []
        for _ in 0..<120 {
            let start = clock()
            try converter.consume(input, into: &output)
            convertTimings.append(clock() - start)
        }
        let flushStart = clock()
        try converter.flush(into: &output)
        convertTimings.append(clock() - flushStart)
        let converted = try output.finish()
        XCTAssertLessThanOrEqual(abs(converted.count - 10 * 16_000 * 2), 256)

        // 60 s of PCM to Base64 + JSON, matching the payload size and
        // serialization primitives used by BridgeClient's background writer.
        // This does NOT claim to benchmark real cloud ASR latency.
        let audio = Data(repeating: 0, count: 60 * 16_000 * 2)
        var wireTimings: [Double] = []
        for _ in 0..<5 {
            let start = clock()
            let object: [String: Any] = [
                "v": 1, "op": "transcribe",
                "pcm_base64": audio.base64EncodedString(),
            ]
            let encoded = try JSONSerialization.data(withJSONObject: object)
            XCTAssertGreaterThan(encoded.count, audio.count)
            wireTimings.append(clock() - start)
        }

        let report: [String: Any] = [
            "schema_version": 1,
            "benchmark": "offline_mac_audio",
            "arch": {
                #if arch(arm64)
                return "arm64"
                #elseif arch(x86_64)
                return "x86_64"
                #else
                return "unknown"
                #endif
            }(),
            "metrics": [
                "pcm16_encode_30s": metric(pcmTimings, audioSeconds: 30),
                "native_resample_10s": metric(convertTimings, audioSeconds: 10),
                "base64_json_60s": metric(wireTimings, audioSeconds: 60),
            ],
        ]
        let json = try JSONSerialization.data(withJSONObject: report, options: [.prettyPrinted, .sortedKeys])
        if let outputPath = ProcessInfo.processInfo.environment["VOICE_REWRITE_PERF_OUTPUT"] {
            let url = URL(fileURLWithPath: outputPath)
            try FileManager.default.createDirectory(at: url.deletingLastPathComponent(),
                                                     withIntermediateDirectories: true)
            try json.write(to: url, options: .atomic)
        }
        print("VOICE_REWRITE_PERF " + String(decoding: json, as: UTF8.self))

        // Generous correctness/timeout gates only. GitHub-hosted CPU is noisy.
        // Track the measured values above for tighter, hardware-specific baselines.
        XCTAssertLessThan(pcmTimings.reduce(0, +), 8,
                          "Encoding 30 s of audio must not consume 8 s of CI CPU")
        XCTAssertLessThan(convertTimings.reduce(0, +), 15,
                          "Offline converter must remain faster than unusable real time")
        XCTAssertLessThan(wireTimings.reduce(0, +), 8,
                          "Repeated 60 s audio serialization must be bounded")
    }
}
#endif
