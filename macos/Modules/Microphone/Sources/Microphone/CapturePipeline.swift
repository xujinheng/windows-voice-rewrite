#if os(macOS)
import AVFoundation
import Foundation

/// A real-time-safe(ish) handoff: the tap only copies a bounded PCM chunk and enqueues
/// it; resampling, encoding and all other potentially expensive work run off the tap.
/// No requests, disk writes or UI calls occur in the audio callback.
final class CapturePipeline: @unchecked Sendable {
    let id: UUID
    private let queue = DispatchQueue(label: "VoiceRewrite.Microphone.convert", qos: .userInitiated)
    private let lock = NSLock()
    private var phase: Phase = .accepting
    private var pendingFrames = 0
    private var failure: MicrophoneError?
    private var measuredRMS: Float = 0
    // The audio callback advances this heartbeat even for digital silence.
    // A dead/removed input device can stop callbacks without delivering an
    // AVAudioEngineConfigurationChange notification.
    private var lastCaptureAt = ProcessInfo.processInfo.systemUptime
    private let queueLimitFrames: Int
    private let converter: NativeAudioConverter
    private var encoded = PCM16Buffer()
    private let onFailure: (UUID, MicrophoneError) -> Void

    private enum Phase { case accepting, finishing, cancelled }

    init(id: UUID, inputFormat: AVAudioFormat,
         onFailure: @escaping (UUID, MicrophoneError) -> Void) throws {
        self.id = id
        converter = try NativeAudioConverter(inputFormat: inputFormat)
        queueLimitFrames = max(8_192, Int(inputFormat.sampleRate * 2))
        self.onFailure = onFailure
    }

    var rmsLevel: Float {
        lock.lock()
        defer { lock.unlock() }
        return measuredRMS
    }

    /// Pure liveness threshold for tests. Silence is valid input: only the
    /// absence of callbacks is a failure, never a low RMS reading.
    static func isStalled(lastBufferAt: TimeInterval, now: TimeInterval,
                          threshold: TimeInterval = 8) -> Bool {
        now >= lastBufferAt && now - lastBufferAt >= threshold
    }

    func markStarted() {
        lock.lock()
        lastCaptureAt = ProcessInfo.processInfo.systemUptime
        lock.unlock()
    }

    func hasStalled(now: TimeInterval = ProcessInfo.processInfo.systemUptime) -> Bool {
        lock.lock()
        defer { lock.unlock() }
        return phase == .accepting && Self.isStalled(lastBufferAt: lastCaptureAt, now: now)
    }

    func enqueue(_ buffer: AVAudioPCMBuffer) {
        let count = Int(buffer.frameLength)
        guard count > 0 else { return }
        lock.lock()
        guard phase == .accepting, failure == nil else {
            lock.unlock()
            return
        }
        guard count <= queueLimitFrames - pendingFrames else {
            lock.unlock()
            fail(.inputOverrun)
            return
        }
        pendingFrames += count
        lastCaptureAt = ProcessInfo.processInfo.systemUptime
        lock.unlock()

        // Engine formats are required to be noninterleaved Float32 at start().
        guard let from = buffer.floatChannelData,
              let copy = AVAudioPCMBuffer(pcmFormat: buffer.format,
                                          frameCapacity: buffer.frameLength),
              let to = copy.floatChannelData else {
            releasePending(count)
            fail(.unsupportedInputFormat)
            return
        }
        copy.frameLength = buffer.frameLength
        for channel in 0..<Int(buffer.format.channelCount) {
            to[channel].update(from: from[channel], count: count)
        }
        queue.async { [self] in
            defer { releasePending(count) }
            lock.lock()
            let shouldProcess = phase != .cancelled && failure == nil
            lock.unlock()
            guard shouldProcess else { return }
            do {
                // Meter on the worker, never the real-time input callback.
                let channelCount = Int(copy.format.channelCount)
                if let channels = copy.floatChannelData {
                    var energy = 0.0
                    for channel in 0..<channelCount {
                        for frame in 0..<count {
                            let value = Double(channels[channel][frame])
                            energy += value * value
                        }
                    }
                    let level = Float(sqrt(energy / Double(count * channelCount)))
                    lock.lock()
                    measuredRMS = level.isFinite ? min(1, level) : 0
                    lock.unlock()
                }
                try converter.consume(copy, into: &encoded)
            } catch let error as MicrophoneError {
                fail(error)
            } catch {
                fail(.conversionFailed)
            }
        }
    }

    func finish() async throws -> Data {
        lock.lock()
        guard phase == .accepting || phase == .finishing else {
            lock.unlock()
            throw MicrophoneError.cancelled
        }
        phase = .finishing
        lock.unlock()
        return try await withCheckedThrowingContinuation { continuation in
            queue.async { [self] in
                lock.lock()
                let stopped = phase == .cancelled
                let reason = failure
                lock.unlock()
                if stopped {
                    continuation.resume(throwing: MicrophoneError.cancelled)
                    return
                }
                if let reason {
                    encoded.cancel()
                    continuation.resume(throwing: reason)
                    return
                }
                do {
                    try converter.flush(into: &encoded)
                    continuation.resume(returning: try encoded.finish())
                } catch {
                    encoded.cancel()
                    continuation.resume(throwing: error)
                }
            }
        }
    }

    func cancel() {
        lock.lock()
        phase = .cancelled
        lock.unlock()
        queue.async { [self] in encoded.cancel() }
    }

    private func releasePending(_ count: Int) {
        lock.lock()
        pendingFrames -= count
        lock.unlock()
    }

    private func fail(_ reason: MicrophoneError) {
        lock.lock()
        let notify = phase != .cancelled && failure == nil
        if notify { failure = reason; phase = .finishing }
        lock.unlock()
        if notify { onFailure(id, reason) }
    }
}
#endif
