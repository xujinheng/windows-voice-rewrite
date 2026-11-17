#if os(macOS)
import AVFoundation
import XCTest
@testable import Microphone

final class NativeAudioConverterTests: XCTestCase {
    private func format(_ rate: Double, channels: AVAudioChannelCount) -> AVAudioFormat {
        AVAudioFormat(commonFormat: .pcmFormatFloat32, sampleRate: rate,
                      channels: channels, interleaved: false)!
    }

    private func makeBuffer(format: AVAudioFormat, frames: Int, value: Float) -> AVAudioPCMBuffer {
        let buffer = AVAudioPCMBuffer(pcmFormat: format, frameCapacity: AVAudioFrameCount(frames))!
        buffer.frameLength = AVAudioFrameCount(frames)
        for channel in 0..<Int(format.channelCount) {
            for index in 0..<frames {
                buffer.floatChannelData![channel][index] = value
            }
        }
        return buffer
    }

    private func checkResampling(rate: Double, channels: AVAudioChannelCount,
                                 frameSizes: [Int]) throws {
        let sourceFormat = format(rate, channels: channels)
        let converter = try NativeAudioConverter(inputFormat: sourceFormat)
        var pcm = PCM16Buffer()
        for chunk in frameSizes {
            try converter.consume(makeBuffer(format: sourceFormat, frames: chunk, value: 0), into: &pcm)
        }
        try converter.flush(into: &pcm)
        let actualFrames = pcm.frameCount
        let expectedFrames = Int((Double(frameSizes.reduce(0, +)) * 16_000 / rate).rounded())
        // AVAudioConverter may compensate for its filter priming on the final flush.
        XCTAssertLessThanOrEqual(abs(actualFrames - expectedFrames), 128)
        let bytes = try pcm.finish()
        XCTAssertEqual(bytes.count, actualFrames * 2)
        XCTAssertTrue(bytes.allSatisfy { $0 == 0 }, "Silence must remain silence")
    }

    func test44100MonoInSmallChunks() throws {
        try checkResampling(rate: 44_100, channels: 1,
                            frameSizes: Array(repeating: 441, count: 100))
    }

    func test48000StereoInUnevenChunks() throws {
        try checkResampling(rate: 48_000, channels: 2,
                            frameSizes: [1, 2, 255, 4_096, 4_096, 4_096, 4_096,
                                         4_096, 4_096, 4_096, 4_096, 4_096,
                                         4_096, 4_096, 4_096, 4_096, 4_096, 4_096])
    }

    func test48000MonoAnd44100Stereo() throws {
        try checkResampling(rate: 48_000, channels: 1,
                            frameSizes: Array(repeating: 480, count: 100))
        try checkResampling(rate: 44_100, channels: 2,
                            frameSizes: Array(repeating: 441, count: 100))
    }

    func testDisconnectedExplicitMicrophoneIsNotSilentlyReplaced() {
        let devices = [
            MicrophoneInputDevice(id: "mic-a", name: "Desk microphone"),
            MicrophoneInputDevice(id: "mic-b", name: "Headset"),
        ]
        XCTAssertFalse(MicrophoneRecorder.selectionUnavailable("", in: []))
        XCTAssertFalse(MicrophoneRecorder.selectionUnavailable("mic-a", in: devices))
        XCTAssertTrue(MicrophoneRecorder.selectionUnavailable("unplugged-mic", in: devices))
        XCTAssertTrue(MicrophoneRecorder.selectionUnavailable("mic-a", in: []))
    }

    func testStereoMixDownProducesAudio() throws {
        let fmt = format(48_000, channels: 2)
        let converter = try NativeAudioConverter(inputFormat: fmt)
        var pcm = PCM16Buffer()
        let chunk = makeBuffer(format: fmt, frames: 4_096, value: 0.4)
        try converter.consume(chunk, into: &pcm)
        try converter.flush(into: &pcm)
        let bytes = try pcm.finish()
        XCTAssertTrue(bytes.contains(where: { $0 != 0 }))
        XCTAssertEqual(bytes.count % 2, 0)
    }
}
#endif
