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
        case .durationExceeded: return "The configured maximum recording duration was reached."
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
    // Native safety cap agrees with the portable Go Core's 180s bound.
    static let maximumSeconds = 180
    static let maximumFrames = sampleRate * maximumSeconds
    static let maximumBytes = maximumFrames * MemoryLayout<Int16>.size

    private(set) var frameCount = 0
    private var bytes = Data()
    private var state: State = .active

    private enum State { case active, finished, cancelled }

    // A five-second initial reserve avoids repeatedly allocating the full
    // 180-second upper bound (~5.5 MiB) on every brief Hold dictation.
    // Data grows only when a real recording exceeds this initial budget.
    static let initialReserveFrames = sampleRate * 5

    init(reserveFrames: Int = initialReserveFrames) {
        // Meeting 200ms packets explicitly request much less than this.
        // The hard 180-second cap continues to apply in append().
        bytes.reserveCapacity(min(Self.maximumFrames, max(0, reserveFrames)) * 2)
    }

    mutating func append(_ samples: UnsafeBufferPointer<Float>) throws {
        guard state == .active else { throw MicrophoneError.cancelled }
        guard samples.count <= Self.maximumFrames - frameCount else {
            throw MicrophoneError.durationExceeded
        }
        guard !samples.isEmpty else { return }
        // The previous encoder allocated a temporary [UInt8] and then copied
        // every audio chunk into Data. Write directly into the pre-reserved PCM
        // buffer instead. A malformed sample must roll back this entire chunk
        // without changing earlier audio or the recorded frame count.
        let previousCount = bytes.count
        bytes.count = previousCount + samples.count * 2
        do {
            try bytes.withUnsafeMutableBytes { raw in
                let destination = raw.bindMemory(to: UInt8.self)
                for index in 0..<samples.count {
                    let sample = samples[index]
                    guard sample.isFinite else { throw MicrophoneError.invalidAudioSample }
                    // Saturate before converting; -1 maps to -32768 and +1 to +32767.
                    let clipped = max(-1.0, min(1.0, sample))
                    let magnitude = Int((clipped * 32_768.0).rounded())
                    let word = UInt16(bitPattern: Int16(clamping: magnitude))
                    let offset = previousCount + index * 2
                    destination[offset] = UInt8(truncatingIfNeeded: word)
                    destination[offset + 1] = UInt8(truncatingIfNeeded: word >> 8)
                }
            }
        } catch {
            bytes.count = previousCount
            throw error
        }
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
