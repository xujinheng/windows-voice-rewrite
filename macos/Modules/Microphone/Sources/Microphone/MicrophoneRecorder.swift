#if os(macOS)
import AppKit
import AVFoundation
import Foundation

public enum MicrophonePermissionStatus: Equatable {
    case notDetermined
    case authorized
    case denied
    case restricted
}

/// Captures the *current system-default* macOS microphone through AVAudioEngine.
/// No simulated device, ASR, network work or Go integration lives in this package.
///
/// The returned Data is headerless, 16,000 Hz, mono, signed little-endian PCM16,
/// suitable for base64 encoding as `transcribe.pcm_base64` by the host integration.
/// All lifecycle calls must occur on the main actor; calls are serialized.
@MainActor
public final class MicrophoneRecorder {
    private enum State {
        case idle, starting, recording, stopping, failed(MicrophoneError)
    }

    private var state: State = .idle
    private var recordingID: UUID?
    private var engine: AVAudioEngine?
    private var pipeline: CapturePipeline?
    private var hasTap = false
    private var configurationObserver: NSObjectProtocol?
    private var sleepObserver: NSObjectProtocol?
    private var deadline: Task<Void, Never>?

    public init() {}

    public var permissionStatus: MicrophonePermissionStatus {
        switch AVCaptureDevice.authorizationStatus(for: .audio) {
        case .authorized: return .authorized
        case .denied: return .denied
        case .restricted: return .restricted
        case .notDetermined: return .notDetermined
        @unknown default: return .restricted
        }
    }

    /// The latest captured-buffer RMS amplitude, normalized to 0...1. Does not
    /// require the UI to access the engine's audio callback thread.
    public var rmsLevel: Float { pipeline?.rmsLevel ?? 0 }

    /// Explicit permission request. When already decided this reads, rather than prompts.
    public func requestPermission() async -> MicrophonePermissionStatus {
        if permissionStatus == .notDetermined {
            _ = await withCheckedContinuation { (continuation: CheckedContinuation<Bool, Never>) in
                AVCaptureDevice.requestAccess(for: .audio) { granted in
                    continuation.resume(returning: granted)
                }
            }
        }
        return permissionStatus
    }

    public func start() async throws {
        guard case .idle = state else { throw MicrophoneError.alreadyRecording }
        state = .starting
        let attempt = UUID()
        recordingID = attempt
        do {
            let access = await requestPermission()
            guard recordingID == attempt else { throw MicrophoneError.cancelled }
            switch access {
            case .authorized: break
            case .denied, .notDetermined: throw MicrophoneError.permissionDenied
            case .restricted: throw MicrophoneError.permissionRestricted
            }
            guard AVCaptureDevice.default(for: .audio) != nil else {
                throw MicrophoneError.noInputDevice
            }
            let instance = AVAudioEngine()
            let input = instance.inputNode
            let format = input.outputFormat(forBus: 0)
            guard format.channelCount > 0, format.sampleRate > 0 else {
                throw MicrophoneError.noInputDevice
            }
            let processor = try CapturePipeline(id: attempt, inputFormat: format) { [weak self] id, reason in
                Task { @MainActor [weak self] in
                    self?.handleFailure(id: id, error: reason)
                }
            }
            engine = instance
            pipeline = processor
            configurationObserver = NotificationCenter.default.addObserver(
                forName: .AVAudioEngineConfigurationChange, object: instance, queue: nil
            ) { [weak self, id = processor.id] _ in
                Task { @MainActor [weak self] in
                    self?.handleFailure(id: id, error: .deviceConfigurationChanged)
                }
            }
            sleepObserver = NSWorkspace.shared.notificationCenter.addObserver(
                forName: NSWorkspace.willSleepNotification, object: nil, queue: nil
            ) { [weak self, id = processor.id] _ in
                Task { @MainActor [weak self] in
                    self?.handleFailure(id: id, error: .recordingInterrupted)
                }
            }
            input.installTap(onBus: 0, bufferSize: 4_096, format: format) { buffer, _ in
                processor.enqueue(buffer)
            }
            hasTap = true
            do {
                try instance.start()
            } catch {
                throw MicrophoneError.engineStartFailed
            }
            guard instance.isRunning else { throw MicrophoneError.engineStartFailed }
            state = .recording
            deadline = Task { [weak self, id = processor.id] in
                do {
                    try await Task.sleep(nanoseconds: 60_000_000_000)
                } catch { return }
                self?.handleFailure(id: id, error: .durationExceeded)
            }
        } catch {
            if recordingID == attempt {
                teardownEngine()
                pipeline?.cancel()
                pipeline = nil
                recordingID = nil
                state = .idle
            }
            throw error
        }
    }

    /// Stops native capture before draining conversion work and returning raw PCM.
    /// Repeated stop, empty input, device loss and conversion failures throw.
    public func stop() async throws -> Data {
        switch state {
        case .failed(let reason):
            state = .idle
            pipeline = nil
            recordingID = nil
            throw reason
        case .recording: break
        default: throw MicrophoneError.notRecording
        }
        state = .stopping
        let engineWasRunning = engine?.isRunning == true
        teardownEngine()
        guard let processor = pipeline else {
            state = .idle
            recordingID = nil
            throw MicrophoneError.notRecording
        }
        let id = recordingID
        guard engineWasRunning else {
            processor.cancel()
            pipeline = nil
            recordingID = nil
            state = .idle
            throw MicrophoneError.recordingInterrupted
        }
        do {
            let bytes = try await processor.finish()
            guard recordingID == id else { throw MicrophoneError.cancelled }
            pipeline = nil
            recordingID = nil
            state = .idle
            return bytes
        } catch {
            if recordingID == id {
                pipeline = nil
                recordingID = nil
                state = .idle
            }
            throw error
        }
    }

    /// Idempotent; discards captured bytes and rejects an in-flight start/stop.
    public func cancel() {
        recordingID = nil
        teardownEngine()
        pipeline?.cancel()
        pipeline = nil
        state = .idle
    }

    private func handleFailure(id: UUID, error: MicrophoneError) {
        guard recordingID == id else { return }
        guard case .recording = state else { return }
        teardownEngine()
        pipeline?.cancel()
        state = .failed(error)
    }

    private func teardownEngine() {
        deadline?.cancel()
        deadline = nil
        if let observer = configurationObserver {
            NotificationCenter.default.removeObserver(observer)
            configurationObserver = nil
        }
        if let observer = sleepObserver {
            NSWorkspace.shared.notificationCenter.removeObserver(observer)
            sleepObserver = nil
        }
        if hasTap {
            engine?.inputNode.removeTap(onBus: 0)
            hasTap = false
        }
        engine?.stop()
        engine?.reset()
        engine = nil
    }
}
#endif
