#if os(macOS)
import AppKit
import AVFoundation
import AudioToolbox
import CoreAudio
import Foundation

public enum MicrophonePermissionStatus: Equatable {
    case notDetermined
    case authorized
    case denied
    case restricted
}

public struct MicrophoneInputDevice: Identifiable, Equatable, Sendable {
    public let id: String
    public let name: String
}

/// Captures the selected macOS microphone through AVAudioEngine.
/// No simulated device, ASR, network work or Go integration lives in this package.
///
/// The returned Data is headerless, 16,000 Hz, mono, signed little-endian PCM16,
/// suitable for base64 encoding as `transcribe.pcm_base64` by the host integration.
/// Lifecycle state belongs to the main actor; blocking CoreAudio calls run on a worker.
@MainActor
public final class MicrophoneRecorder {
    private enum State {
        case idle, starting, recording, stopping, failed(MicrophoneError)
    }

    // One owner for all native CoreAudio start/stop/reset operations.
    // Parallel teardown and restart on the same physical input can deadlock
    // drivers or crash AVAudioEngine. Conversion has its own separate queue.
    private nonisolated static let audioLifecycle = DispatchQueue(
        label: "com.xujinheng.VoiceRewrite.audio.lifecycle", qos: .userInitiated)

    private var state: State = .idle
    private var recordingID: UUID?
    // Cancellation must not overlap a new native engine start with an
    // abandoned start still returning from the CoreAudio lifecycle queue.
    private var preparationInFlight = false
    private var recordingDeviceUID = ""
    private var engine: AVAudioEngine?
    private var pipeline: CapturePipeline?
    private var hasTap = false
    private var configurationObserver: NSObjectProtocol?
    private var sleepObserver: NSObjectProtocol?
    private var deadline: Task<Void, Never>?

    public init() {}

    /// Selectable physical devices are enumerated by the CoreAudio HAL.
    /// Stable device UIDs survive device-index changes.
    public nonisolated static func availableDevices() -> [MicrophoneInputDevice] {
        var address = AudioObjectPropertyAddress(
            mSelector: kAudioHardwarePropertyDevices,
            mScope: kAudioObjectPropertyScopeGlobal,
            mElement: kAudioObjectPropertyElementMain)
        var size: UInt32 = 0
        let system = AudioObjectID(kAudioObjectSystemObject)
        guard AudioObjectGetPropertyDataSize(system, &address, 0, nil, &size) == noErr,
              size > 0 else { return [] }
        var ids = [AudioDeviceID](repeating: 0, count: Int(size) / MemoryLayout<AudioDeviceID>.size)
        let result = ids.withUnsafeMutableBufferPointer { buffer in
            guard let pointer = buffer.baseAddress else { return OSStatus(-1) }
            return AudioObjectGetPropertyData(system, &address, 0, nil, &size, pointer)
        }
        guard result == noErr else { return [] }
        return ids.compactMap { id in
            guard inputDeviceUsable(id),
                  let uid = propertyString(id, selector: kAudioDevicePropertyDeviceUID),
                  let name = propertyString(id, selector: kAudioObjectPropertyName),
                  !uid.isEmpty else { return nil }
            return MicrophoneInputDevice(id: uid, name: name)
        }.sorted { $0.name.localizedCaseInsensitiveCompare($1.name) == .orderedAscending }
    }

    /// An unplugged or output-only device cannot be offered as a recording target.
    private nonisolated static func inputDeviceUsable(_ device: AudioDeviceID) -> Bool {
        var alive = AudioObjectPropertyAddress(
            mSelector: kAudioDevicePropertyDeviceIsAlive,
            mScope: kAudioObjectPropertyScopeGlobal,
            mElement: kAudioObjectPropertyElementMain)
        var flag: UInt32 = 0
        var flagBytes = UInt32(MemoryLayout<UInt32>.size)
        guard AudioObjectGetPropertyData(device, &alive, 0, nil, &flagBytes, &flag) == noErr,
              flag != 0 else { return false }
        var streams = AudioObjectPropertyAddress(
            mSelector: kAudioDevicePropertyStreams,
            mScope: kAudioDevicePropertyScopeInput,
            mElement: kAudioObjectPropertyElementMain)
        var size: UInt32 = 0
        return AudioObjectGetPropertyDataSize(device, &streams, 0, nil, &size) == noErr && size > 0
    }

    /// A missing explicit choice is never silently substituted with the system default.
    public nonisolated static func selectionUnavailable(_ uid: String, in devices: [MicrophoneInputDevice]) -> Bool {
        !uid.isEmpty && !devices.contains(where: { $0.id == uid })
    }

    private nonisolated static func propertyString(_ device: AudioDeviceID, selector: AudioObjectPropertySelector) -> String? {
        var property = AudioObjectPropertyAddress(
            mSelector: selector, mScope: kAudioObjectPropertyScopeGlobal,
            mElement: kAudioObjectPropertyElementMain)
        var value: CFString?
        var size = UInt32(MemoryLayout<CFString?>.size)
        guard AudioObjectGetPropertyData(device, &property, 0, nil, &size, &value) == noErr else { return nil }
        return value as String?
    }

    private nonisolated static func deviceID(for uid: String) -> AudioDeviceID? {
        var address = AudioObjectPropertyAddress(
            mSelector: kAudioHardwarePropertyDevices,
            mScope: kAudioObjectPropertyScopeGlobal,
            mElement: kAudioObjectPropertyElementMain)
        var size: UInt32 = 0
        let system = AudioObjectID(kAudioObjectSystemObject)
        guard AudioObjectGetPropertyDataSize(system, &address, 0, nil, &size) == noErr else { return nil }
        var ids = [AudioDeviceID](repeating: 0, count: Int(size) / MemoryLayout<AudioDeviceID>.size)
        let result = ids.withUnsafeMutableBufferPointer {
            guard let pointer = $0.baseAddress else { return OSStatus(-1) }
            return AudioObjectGetPropertyData(system, &address, 0, nil, &size, pointer)
        }
        guard result == noErr else { return nil }
        return ids.first {
            inputDeviceUsable($0) &&
            propertyString($0, selector: kAudioDevicePropertyDeviceUID) == uid
        }
    }

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

    public func start(deviceUID: String = "", maximumSeconds: Int = 60) async throws {
        guard case .idle = state, !preparationInFlight else {
            throw MicrophoneError.alreadyRecording
        }
        // Host configuration owns the requested limit; native PCM bounds
        // still protect against unexpectedly large inputs.
        let duration = min(max(1, maximumSeconds), PCM16Buffer.maximumSeconds)
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
            preparationInFlight = true
            // AVAudioEngine inputNode, AudioUnitSetProperty, installTap and
            // start() can synchronously wait on a missing/reconfiguring driver.
            // They must not run on the key/indicator AppKit event loop.
            let prepared = try await withCheckedThrowingContinuation {
                (continuation: CheckedContinuation<(AVAudioEngine, CapturePipeline), Error>) in
                Self.audioLifecycle.async { [weak self] in
                    do {
                        let pair = try Self.prepareEngine(deviceUID: deviceUID, attempt: attempt) { id, reason in
                            Task { @MainActor [weak self] in
                                self?.handleFailure(id: id, error: reason)
                            }
                        }
                        continuation.resume(returning: pair)
                    } catch {
                        continuation.resume(throwing: error)
                    }
                }
            }
            let (instance, processor) = prepared
            guard recordingID == attempt else {
                // The cleanup must be queued *before* admitting a fresh start.
                Self.cleanupEngineAsync(instance, hasTap: true)
                preparationInFlight = false
                throw MicrophoneError.cancelled
            }
            preparationInFlight = false
            engine = instance
            hasTap = true
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
            recordingDeviceUID = deviceUID
            state = .recording
            deadline = Task { [weak self, id = processor.id] in
                do {
                    try await Task.sleep(nanoseconds: UInt64(duration) * 1_000_000_000)
                } catch { return }
                self?.handleFailure(id: id, error: .durationExceeded)
            }
        } catch {
            preparationInFlight = false
            if recordingID == attempt {
                teardownEngine()
                pipeline?.cancel()
                pipeline = nil
                recordingID = nil
                recordingDeviceUID = ""
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
        // A recording state requires a successfully started engine. Avoid
        // calling CoreAudio.isRunning or HAL device discovery synchronously
        // on the main actor when the input device is disappearing.
        let engineWasRunning = engine != nil
        let stoppedDeviceUID = recordingDeviceUID
        recordingDeviceUID = ""
        // CoreAudio may block removeTap/stop/reset indefinitely when an input
        // device is reconfigured or disappears. Never wait for those native
        // calls on AppKit's main actor; the host's processing watchdog can
        // recover independently if teardown fails to complete.
        await teardownEngineAfterStop()
        let deviceStillAvailable: Bool
        if stoppedDeviceUID.isEmpty {
            deviceStillAvailable = true
        } else {
            deviceStillAvailable = await withCheckedContinuation { continuation in
                DispatchQueue.global(qos: .utility).async {
                    continuation.resume(returning: Self.deviceID(for: stoppedDeviceUID) != nil)
                }
            }
        }
        guard let processor = pipeline else {
            state = .idle
            recordingID = nil
            throw MicrophoneError.notRecording
        }
        let id = recordingID
        guard engineWasRunning && deviceStillAvailable else {
            processor.cancel()
            pipeline = nil
            recordingID = nil
            state = .idle
            throw deviceStillAvailable ? MicrophoneError.recordingInterrupted : MicrophoneError.noInputDevice
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
        recordingDeviceUID = ""
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
        recordingDeviceUID = ""
        state = .failed(error)
    }

    /// The entire CoreAudio startup is performed off the main run loop. The
    /// tap only captures PCM; UI callbacks remain on the main actor.
    private nonisolated static func prepareEngine(
        deviceUID: String, attempt: UUID,
        onFailure: @escaping (UUID, MicrophoneError) -> Void
    ) throws -> (AVAudioEngine, CapturePipeline) {
        guard AVCaptureDevice.default(for: .audio) != nil || !deviceUID.isEmpty else {
            throw MicrophoneError.noInputDevice
        }
        let instance = AVAudioEngine()
        let input = instance.inputNode
        var installedTap = false
        do {
            if !deviceUID.isEmpty {
                guard let id = deviceID(for: deviceUID),
                      let unit = input.audioUnit else { throw MicrophoneError.noInputDevice }
                var device = id
                let result = AudioUnitSetProperty(unit, kAudioOutputUnitProperty_CurrentDevice,
                    kAudioUnitScope_Global, 0, &device, UInt32(MemoryLayout<AudioDeviceID>.size))
                guard result == noErr else { throw MicrophoneError.engineStartFailed }
            }
            let format = input.outputFormat(forBus: 0)
            guard format.channelCount > 0, format.sampleRate > 0 else {
                throw MicrophoneError.noInputDevice
            }
            let processor = try CapturePipeline(
                id: attempt, inputFormat: format, onFailure: onFailure)
            input.installTap(onBus: 0, bufferSize: 4_096, format: format) { buffer, _ in
                processor.enqueue(buffer)
            }
            installedTap = true
            do { try instance.start() }
            catch { throw MicrophoneError.engineStartFailed }
            guard instance.isRunning else { throw MicrophoneError.engineStartFailed }
            if !deviceUID.isEmpty && deviceID(for: deviceUID) == nil {
                throw MicrophoneError.noInputDevice
            }
            return (instance, processor)
        } catch {
            // This cleanup is already on the background worker.
            closeEngine(instance, hasTap: installedTap)
            throw error
        }
    }

    private nonisolated static func closeEngine(_ instance: AVAudioEngine, hasTap: Bool) {
        if hasTap { instance.inputNode.removeTap(onBus: 0) }
        instance.stop()
        instance.reset()
    }

    private nonisolated static func cleanupEngineAsync(_ instance: AVAudioEngine, hasTap: Bool) {
        Self.audioLifecycle.async {
            closeEngine(instance, hasTap: hasTap)
        }
    }

    /// Detach the native engine before awaiting teardown, so cancellation
    /// cannot synchronously repeat a stuck CoreAudio stop on the main thread.
    private func teardownEngineAfterStop() async {
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
        let stoppedEngine = engine
        let removeTap = hasTap
        engine = nil
        hasTap = false
        guard let stoppedEngine else { return }
        await withCheckedContinuation { (continuation: CheckedContinuation<Void, Never>) in
            Self.audioLifecycle.async {
                Self.closeEngine(stoppedEngine, hasTap: removeTap)
                continuation.resume()
            }
        }
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
        let previous = engine
        let tapInstalled = hasTap
        hasTap = false
        engine = nil
        if let previous {
            Self.cleanupEngineAsync(previous, hasTap: tapInstalled)
        }
    }
}
#endif
