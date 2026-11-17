import Foundation

/// Errors surfaced to the macOS host. A failed capture never returns apparently valid PCM.
public enum MicrophoneError: Error, Equatable, LocalizedError {
    case permissionDenied
    case permissionRestricted
    case noInputDevice
    case unsupportedInputFormat
    case converterUnavailable
    case conversionFailed
    case engineStartFailed
    case deviceConfigurationChanged
    case recordingInterrupted
    case inputOverrun
    case durationExceeded
    case invalidAudioSample
    case noAudioCaptured
    case alreadyRecording
    case notRecording
    case cancelled

    public var errorDescription: String? {
        switch self {
        case .permissionDenied: return "macOS 已拒绝麦克风访问。请在「系统设置 → 隐私与安全性 → 麦克风」中允许 VoiceRewrite，重启应用后重试。"
        case .permissionRestricted: return "麦克风访问受到 macOS 或设备管理策略限制。"
        case .noInputDevice: return "No usable default microphone is available."
        case .unsupportedInputFormat: return "The default microphone uses an unsupported PCM format."
        case .converterUnavailable: return "Cannot create a 16 kHz mono audio converter."
        case .conversionFailed: return "Could not convert microphone input into 16 kHz mono PCM."
        case .engineStartFailed: return "The macOS microphone audio engine failed to start."
        case .deviceConfigurationChanged: return "The microphone device or audio configuration changed during recording."
        case .recordingInterrupted: return "Microphone recording was interrupted."
        case .inputOverrun: return "Microphone processing could not keep up with live input."
        case .durationExceeded: return "The maximum recording duration of 60 seconds was reached."
        case .invalidAudioSample: return "Microphone data contained invalid floating-point samples."
        case .noAudioCaptured: return "No microphone audio was captured."
        case .alreadyRecording: return "Microphone recording is already starting or active."
        case .notRecording: return "There is no active microphone recording to stop."
        case .cancelled: return "Microphone recording was cancelled."
        }
    }
}

/// Streaming sample-to-byte encoder for the Go Core's raw PCM contract.
/// Input is already 16,000 Hz, mono Float32. This type does not generate audio.
struct PCM16Buffer {
    static let sampleRate = 16_000
    static let maximumSeconds = 60
    static let maximumFrames = sampleRate * maximumSeconds
    static let maximumBytes = maximumFrames * MemoryLayout<Int16>.size

    private(set) var frameCount = 0
    private var bytes = Data()
    private var state: State = .active

    private enum State { case active, finished, cancelled }

    init(reserveFrames: Int = maximumFrames) {
        // Short system-audio packets must not reserve a full minute of memory
        // for every callback; microphone recording still reserves its limit.
        bytes.reserveCapacity(min(Self.maximumFrames, max(0, reserveFrames)) * 2)
    }

    mutating func append(_ samples: UnsafeBufferPointer<Float>) throws {
        guard state == .active else { throw MicrophoneError.cancelled }
        guard samples.count <= Self.maximumFrames - frameCount else {
            throw MicrophoneError.durationExceeded
        }
        var encoded = [UInt8]()
        encoded.reserveCapacity(samples.count * 2)
        for sample in samples {
            guard sample.isFinite else { throw MicrophoneError.invalidAudioSample }
            // Saturate before converting; -1 maps to -32768 and +1 to +32767.
            let clipped = max(-1.0, min(1.0, sample))
            let magnitude = Int((clipped * 32_768.0).rounded())
            let word = UInt16(bitPattern: Int16(clamping: magnitude))
            encoded.append(UInt8(truncatingIfNeeded: word))
            encoded.append(UInt8(truncatingIfNeeded: word >> 8))
        }
        bytes.append(contentsOf: encoded)
        frameCount += samples.count
    }

    mutating func append(_ samples: [Float]) throws {
        try samples.withUnsafeBufferPointer { try append($0) }
    }

    mutating func finish() throws -> Data {
        guard state == .active else { throw MicrophoneError.notRecording }
        state = .finished
        guard frameCount > 0 else { throw MicrophoneError.noAudioCaptured }
        return bytes
    }

    mutating func cancel() {
        state = .cancelled
        bytes.removeAll(keepingCapacity: false)
        frameCount = 0
    }
}
