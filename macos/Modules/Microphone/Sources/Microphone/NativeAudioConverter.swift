#if os(macOS)
import AVFoundation
import Foundation

/// Owns the AVAudioConverter and is called only on the capture pipeline's serial queue.
/// The converter stays alive across input chunks so its resampling state is preserved.
final class NativeAudioConverter {
    private let converter: AVAudioConverter
    private let outputBuffer: AVAudioPCMBuffer

    init(inputFormat: AVAudioFormat) throws {
        guard inputFormat.commonFormat == .pcmFormatFloat32,
              !inputFormat.isInterleaved,
              (inputFormat.channelCount == 1 || inputFormat.channelCount == 2),
              inputFormat.sampleRate.isFinite, inputFormat.sampleRate > 0 else {
            throw MicrophoneError.unsupportedInputFormat
        }
        guard let format = AVAudioFormat(commonFormat: .pcmFormatFloat32,
                                         sampleRate: 16_000,
                                         channels: 1,
                                         interleaved: false),
              let created = AVAudioConverter(from: inputFormat, to: format) else {
            throw MicrophoneError.converterUnavailable
        }
        converter = created
        // Capture input chunks are limited to <= 4096 frames by the engine tap.
        let outputCapacity = AVAudioFrameCount(max(1_024,
            Int(ceil(Double(4_096) * 16_000 / inputFormat.sampleRate)) + 256))
        // The conversion worker is serial. A single reusable destination
        // avoids allocating an AVAudioPCMBuffer on every microphone callback.
        guard let output = AVAudioPCMBuffer(pcmFormat: format,
                                           frameCapacity: outputCapacity) else {
            throw MicrophoneError.converterUnavailable
        }
        outputBuffer = output
    }

    func consume(_ input: AVAudioPCMBuffer, into pcm: inout PCM16Buffer) throws {
        var delivered = false
        try pump(into: &pcm) { _, status in
            if delivered {
                status.pointee = .noDataNow
                return nil
            }
            delivered = true
            status.pointee = .haveData
            return input
        }
    }

    func flush(into pcm: inout PCM16Buffer) throws {
        try pump(into: &pcm) { _, status in
            status.pointee = .endOfStream
            return nil
        }
    }

    private func pump(
        into pcm: inout PCM16Buffer,
        source: @escaping AVAudioConverterInputBlock
    ) throws {
        // Bounded loop; a converter which cannot make forward progress must fail.
        for _ in 0..<32 {
            let converted = outputBuffer
            // AVAudioConverter writes a new chunk, never the previous output.
            converted.frameLength = 0
            var conversionError: NSError?
            let result = converter.convert(to: converted,
                                           error: &conversionError,
                                           withInputFrom: source)
            if conversionError != nil || result == .error {
                throw MicrophoneError.conversionFailed
            }
            if converted.frameLength > 0 {
                guard let channels = converted.floatChannelData else {
                    throw MicrophoneError.conversionFailed
                }
                let samples = UnsafeBufferPointer(start: channels[0],
                                                  count: Int(converted.frameLength))
                try pcm.appendCapped(samples)
            }
            switch result {
            case .haveData:
                guard converted.frameLength > 0 else {
                    throw MicrophoneError.conversionFailed
                }
            case .inputRanDry, .endOfStream:
                return
            case .error:
                throw MicrophoneError.conversionFailed
            @unknown default:
                throw MicrophoneError.conversionFailed
            }
        }
        throw MicrophoneError.conversionFailed
    }
}
#endif
