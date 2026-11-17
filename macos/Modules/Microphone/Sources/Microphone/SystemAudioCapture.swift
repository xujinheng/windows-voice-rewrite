#if os(macOS)
import AVFoundation
import AppKit
import CoreAudio
import CoreGraphics
import CoreMedia
import Foundation
import ScreenCaptureKit

public struct MeetingAudioSource: Identifiable, Equatable, Sendable {
    public let id: String
    public let name: String

    public init(id: String, name: String) {
        self.id = id
        self.name = name
    }
}

/// ScreenCaptureKit captures an actual application audio stream. Sample
/// conversion is native; segmentation and inference remain in the Go core.
/// Actor-confined, deterministic token guard for overlapping async starts.
/// Stop invalidates a start even before ScreenCaptureKit creates a stream.
public struct SystemAudioStartGate {
    private(set) var generation: UInt64 = 0
    private(set) var starting = false

    public init() {}

    public mutating func begin(streamExists: Bool) -> UInt64? {
        guard !starting && !streamExists else { return nil }
        generation &+= 1
        starting = true
        return generation
    }

    public func accepts(_ token: UInt64) -> Bool {
        starting && generation == token
    }

    public mutating func finish(_ token: UInt64) {
        if generation == token { starting = false }
    }

    public mutating func invalidate() {
        generation &+= 1
        starting = false
    }
}

/// Maximum number of 200 ms PCM packets awaiting the main actor. A blocked
/// GUI must not let the ScreenCaptureKit delivery queue grow without bound.
public struct SystemAudioDeliveryGate {
    public private(set) var pending = 0
    public let limit: Int

    public init(limit: Int = 8) {
        self.limit = limit
    }

    public mutating func reserve(active: Bool) -> Bool {
        guard active, pending < limit else { return false }
        pending += 1
        return true
    }

    public mutating func complete() {
        pending = max(0, pending - 1)
    }
}

@MainActor
public final class SystemAudioCapture {
    private var stream: SCStream?
    private var receiver: SystemAudioReceiver?
    private var startGate = SystemAudioStartGate()
    private var pendingStop: Task<Void, Never>?
    private var terminationObserver: NSObjectProtocol?

    public init() {}

    private func shareableContent() async throws -> SCShareableContent {
        do {
            return try await SCShareableContent.excludingDesktopWindows(
                false, onScreenWindowsOnly: false)
        } catch {
            if !CGPreflightScreenCaptureAccess() {
                throw SystemAudioError.permissionDenied
            }
            throw SystemAudioError.sourceDiscoveryFailed
        }
    }

    public func sources() async throws -> [MeetingAudioSource] {
        let content = try await shareableContent()
        var sources = [MeetingAudioSource(id: "system", name: "系统音频（不含 VoiceRewrite）")]
        for app in content.applications where app.processID != ProcessInfo.processInfo.processIdentifier {
            let name = app.applicationName.trimmingCharacters(in: .whitespacesAndNewlines)
            if !name.isEmpty {
                // Display a visible window title when available, like the Windows
                // process source picker. The process ID remains the stable source.
                let windowTitle = content.windows.first {
                    $0.owningApplication?.processID == app.processID &&
                        $0.isOnScreen &&
                        !($0.title ?? "").trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
                }?.title?.trimmingCharacters(in: .whitespacesAndNewlines)
                let displayName = windowTitle.map { "\(name) · \($0)" } ?? name
                sources.append(MeetingAudioSource(
                    id: String(app.processID), name: displayName))
            }
        }
        sources.sort { left, right in
            if left.id == "system" { return true }
            if right.id == "system" { return false }
            let leftZoom = left.name.localizedLowercase.hasPrefix("zoom")
            let rightZoom = right.name.localizedLowercase.hasPrefix("zoom")
            if leftZoom != rightZoom { return leftZoom }
            return left.name.localizedStandardCompare(right.name) == .orderedAscending
        }
        return sources
    }

    public func start(sourceID: String, onPCM: @escaping @MainActor (Data) -> Void,
                      onFailure: @escaping @MainActor (String) -> Void) async throws {
        guard let token = startGate.begin(streamExists: stream != nil) else {
            throw SystemAudioError.alreadyRunning
        }
        defer { startGate.finish(token) }
        // Never restart ScreenCaptureKit before the preceding native stream has
        // finished tearing down. A canceled discovery still checks its token.
        await pendingStop?.value
        guard startGate.accepts(token) else { throw SystemAudioError.startCanceled }
        let content = try await shareableContent()
        // The first suspension is above: stop() must cancel source discovery too.
        guard startGate.accepts(token) else { throw SystemAudioError.startCanceled }
        guard let display = content.displays.first else {
            throw SystemAudioError.noDisplay
        }
        let filter: SCContentFilter
        if sourceID == "system" {
            let excluded = content.applications.filter {
                $0.processID == ProcessInfo.processInfo.processIdentifier
            }
            filter = SCContentFilter(display: display, excludingApplications: excluded,
                                     exceptingWindows: [])
        } else {
            guard let pid = Int32(sourceID),
                  let application = content.applications.first(where: {
                      $0.processID == pid && $0.processID != ProcessInfo.processInfo.processIdentifier
                  }) else {
                throw SystemAudioError.sourceGone
            }
            filter = SCContentFilter(display: display, including: [application],
                                     exceptingWindows: [])
        }
        let configuration = SCStreamConfiguration()
        configuration.width = 2
        configuration.height = 2
        configuration.minimumFrameInterval = CMTime(value: 1, timescale: 1)
        configuration.queueDepth = 3
        configuration.capturesAudio = true
        configuration.excludesCurrentProcessAudio = true
        configuration.sampleRate = 48_000
        configuration.channelCount = 2
        let receiver = SystemAudioReceiver(onPCM: onPCM, onFailure: onFailure)
        let created = SCStream(filter: filter, configuration: configuration, delegate: receiver)
        try created.addStreamOutput(receiver, type: .audio,
                                    sampleHandlerQueue: receiver.queue)
        // Retain the in-flight stream before suspending. A close/stop during
        // startCapture must see and invalidate the pending native capture.
        self.receiver = receiver
        self.stream = created
        do {
            try await created.startCapture()
        } catch {
            receiver.invalidate()
            if self.stream === created {
                self.stream = nil
                self.receiver = nil
            }
            try? created.removeStreamOutput(receiver, type: .audio)
            if !startGate.accepts(token) { throw SystemAudioError.startCanceled }
            if !CGPreflightScreenCaptureAccess() {
                throw SystemAudioError.permissionDenied
            }
            throw SystemAudioError.captureFailed
        }
        guard startGate.accepts(token), self.stream === created else {
            receiver.invalidate()
            try? await created.stopCapture()
            try? created.removeStreamOutput(receiver, type: .audio)
            throw SystemAudioError.startCanceled
        }
        if sourceID != "system", let pid = Int32(sourceID) {
            // A running app may disappear without a ScreenCaptureKit error.
            // The notification is scoped to this stream, not the next meeting.
            terminationObserver = NSWorkspace.shared.notificationCenter.addObserver(
                forName: NSWorkspace.didTerminateApplicationNotification,
                object: nil, queue: .main
            ) { [weak self] notification in
                guard let app = notification.userInfo?[NSWorkspace.applicationUserInfoKey]
                    as? NSRunningApplication, app.processIdentifier == pid else { return }
                Task { @MainActor [weak self] in
                    guard let self, self.stream === created else { return }
                    onFailure(SystemAudioError.sourceGone.localizedDescription)
                }
            }
            if NSRunningApplication(processIdentifier: pid) == nil {
                cancel()
                throw SystemAudioError.sourceGone
            }
        }
    }

    /// Immediately fence callbacks and in-flight discovery. Unlike scheduling
    /// an async stop from the UI, this cannot run after a newer meeting starts.
    public func cancel() {
        startGate.invalidate()
        if let terminationObserver {
            NSWorkspace.shared.notificationCenter.removeObserver(terminationObserver)
            self.terminationObserver = nil
        }
        receiver?.invalidate()
        let oldStream = stream
        let oldReceiver = receiver
        stream = nil
        receiver = nil
        guard let oldStream else { return }
        let previousStop = pendingStop
        pendingStop = Task {
            await previousStop?.value
            try? await oldStream.stopCapture()
            if let oldReceiver {
                try? oldStream.removeStreamOutput(oldReceiver, type: .audio)
            }
        }
    }

    /// Native teardown can be awaited when ordering is important (pause).
    public func stop() async {
        cancel()
        await pendingStop?.value
    }

    public func waitForStop() async {
        await pendingStop?.value
    }
}

public enum SystemAudioError: Error, LocalizedError {
    case alreadyRunning
    case noDisplay
    case sourceGone
    case startCanceled
    case permissionDenied
    case sourceDiscoveryFailed
    case captureFailed

    public var errorDescription: String? {
        switch self {
        case .alreadyRunning: return "会议音频采集已经启动"
        case .noDisplay: return "未找到可采集的屏幕；请检查显示器连接"
        case .sourceGone: return "所选应用已经退出，请重新选择音频来源"
        case .startCanceled: return "会议音频采集已停止"
        case .permissionDenied: return "屏幕与系统音频录制权限未授权；请在系统设置 → 隐私与安全性中允许 VoiceRewrite"
        case .sourceDiscoveryFailed: return "读取会议音频来源失败；请检查屏幕共享及系统音频权限"
        case .captureFailed: return "启动会议音频失败；请重新选择来源并检查屏幕与系统音频录制权限"
        }
    }
}

/// The stream callback owns one serial queue. It never performs IPC, inference,
/// or expensive work on an audio/graphics real-time callback.
private final class SystemAudioReceiver: NSObject, SCStreamOutput, SCStreamDelegate {
    let queue = DispatchQueue(label: "VoiceRewrite.Meeting.SystemAudio", qos: .userInitiated)
    private let onPCM: @MainActor (Data) -> Void
    private let onFailure: @MainActor (String) -> Void
    private let stateLock = NSLock()
    private var active = true
    private var canceled = false
    private var deliveryGate = SystemAudioDeliveryGate()
    private var converter: NativeAudioConverter?
    private var currentFormat: AVAudioFormat?
    private var pending = Data()

    init(onPCM: @escaping @MainActor (Data) -> Void,
         onFailure: @escaping @MainActor (String) -> Void) {
        self.onPCM = onPCM
        self.onFailure = onFailure
    }

    func invalidate() {
        stateLock.lock()
        active = false
        canceled = true
        stateLock.unlock()
    }

    private var mayReportFailure: Bool {
        stateLock.lock()
        defer { stateLock.unlock() }
        return !canceled
    }

    private func fail(_ message: String) {
        stateLock.lock()
        let shouldSchedule = active && !canceled
        active = false
        stateLock.unlock()
        guard shouldSchedule else { return }
        Task { @MainActor [weak self] in
            guard let self, self.mayReportFailure else { return }
            self.onFailure(message)
        }
    }

    private var isActive: Bool {
        stateLock.lock()
        defer { stateLock.unlock() }
        return active
    }

    private func reserveDelivery() -> Bool {
        stateLock.lock()
        defer { stateLock.unlock() }
        return deliveryGate.reserve(active: active)
    }

    private func completeDelivery() {
        stateLock.lock()
        deliveryGate.complete()
        stateLock.unlock()
    }

    func stream(_ stream: SCStream, didStopWithError error: Error) {
        fail("系统音频采集已中断；请检查屏幕与系统音频录制权限")
    }

    func stream(_ stream: SCStream, didOutputSampleBuffer sampleBuffer: CMSampleBuffer,
                of type: SCStreamOutputType) {
        guard type == .audio, sampleBuffer.isValid, isActive else { return }
        do {
            try process(sampleBuffer)
        } catch {
            fail("无法转换系统音频；请重新启动会议采集")
        }
    }

    private func process(_ sample: CMSampleBuffer) throws {
        guard let description = sample.formatDescription?.audioStreamBasicDescription,
              description.mFormatID == kAudioFormatLinearPCM,
              description.mSampleRate.isFinite, description.mSampleRate > 0,
              description.mBitsPerChannel == 32,
              description.mFormatFlags & kAudioFormatFlagIsFloat != 0,
              (1...2).contains(Int(description.mChannelsPerFrame)),
              sample.numSamples > 0,
              sample.numSamples <= 16_384,
              let format = AVAudioFormat(standardFormatWithSampleRate: description.mSampleRate,
                                         channels: description.mChannelsPerFrame),
              let owned = AVAudioPCMBuffer(pcmFormat: format,
                                           frameCapacity: AVAudioFrameCount(sample.numSamples)),
              let channels = owned.floatChannelData else {
            throw MicrophoneError.unsupportedInputFormat
        }
        let frames = sample.numSamples
        try sample.withAudioBufferList { list, _ in
            let channelCount = Int(description.mChannelsPerFrame)
            guard list.count == 1 || list.count == channelCount else {
                throw MicrophoneError.unsupportedInputFormat
            }
            for channel in 0..<channelCount {
                let index = list.count == 1 ? 0 : channel
                let requiredBytes = frames * MemoryLayout<Float>.size *
                    (list.count == 1 ? channelCount : 1)
                guard let raw = list[index].mData,
                      Int(list[index].mDataByteSize) >= requiredBytes else {
                    throw MicrophoneError.unsupportedInputFormat
                }
                let source = raw.assumingMemoryBound(to: Float.self)
                for frame in 0..<frames {
                    channels[channel][frame] = list.count == 1
                        ? source[frame * channelCount + channel] : source[frame]
                }
            }
        }
        owned.frameLength = AVAudioFrameCount(frames)
        if converter == nil || currentFormat != format {
            converter = try NativeAudioConverter(inputFormat: format)
            currentFormat = format
        }
        guard let converter else { throw MicrophoneError.converterUnavailable }
        var encoded = PCM16Buffer(reserveFrames: 16_384)
        try converter.consume(owned, into: &encoded)
        if encoded.frameCount == 0 { return }
        pending.append(try encoded.finish())
        // Each chunk is ten 20ms Go Core frames (6400 bytes), not a
        // per-sample RPC. Bounds and framing are enforced again in voicebridge.
        while pending.count >= 6_400 {
            let chunk = Data(pending.prefix(6_400))
            pending.removeFirst(6_400)
            guard reserveDelivery() else {
                fail("会议音频处理过载；请检查系统负载并重启采集")
                return
            }
            Task { @MainActor [weak self] in
                guard let self else { return }
                self.completeDelivery()
                guard self.isActive else { return }
                self.onPCM(chunk)
            }
        }
    }
}
#endif
