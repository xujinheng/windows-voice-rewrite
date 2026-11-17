#if os(macOS)
import AVFoundation
import CoreAudio
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
@MainActor
public final class SystemAudioCapture {
    private var stream: SCStream?
    private var receiver: SystemAudioReceiver?

    public init() {}

    public func sources() async throws -> [MeetingAudioSource] {
        let content = try await SCShareableContent.excludingDesktopWindows(
            false, onScreenWindowsOnly: false)
        var sources = [MeetingAudioSource(id: "system", name: "系统音频（不含 VoiceRewrite）")]
        for app in content.applications where app.processID != ProcessInfo.processInfo.processIdentifier {
            let name = app.applicationName.trimmingCharacters(in: .whitespacesAndNewlines)
            if !name.isEmpty {
                sources.append(MeetingAudioSource(
                    id: String(app.processID), name: name))
            }
        }
        sources.sort { left, right in
            if left.id == "system" { return true }
            if right.id == "system" { return false }
            return left.name.localizedStandardCompare(right.name) == .orderedAscending
        }
        return sources
    }

    public func start(sourceID: String, onPCM: @escaping @MainActor (Data) -> Void,
                      onFailure: @escaping @MainActor (String) -> Void) async throws {
        guard stream == nil else { throw SystemAudioError.alreadyRunning }
        let content = try await SCShareableContent.excludingDesktopWindows(
            false, onScreenWindowsOnly: false)
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
            throw error
        }
        guard self.stream === created else {
            receiver.invalidate()
            try? await created.stopCapture()
            throw SystemAudioError.startCanceled
        }
    }

    public func stop() async {
        guard let stream else { return }
        receiver?.invalidate()
        self.stream = nil
        self.receiver = nil
        try? await stream.stopCapture()
    }
}

public enum SystemAudioError: Error, LocalizedError {
    case alreadyRunning
    case noDisplay
    case sourceGone
    case startCanceled

    public var errorDescription: String? {
        switch self {
        case .alreadyRunning: return "会议音频采集已经启动"
        case .noDisplay: return "未找到可采集的屏幕；请检查显示器连接"
        case .sourceGone: return "所选应用已经退出，请重新选择音频来源"
        case .startCanceled: return "会议音频采集已停止"
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
        stateLock.unlock()
    }

    private var isActive: Bool {
        stateLock.lock()
        defer { stateLock.unlock() }
        return active
    }

    func stream(_ stream: SCStream, didStopWithError error: Error) {
        guard isActive else { return }
        invalidate()
        Task { @MainActor in onFailure("系统音频采集已中断；请检查屏幕与系统音频录制权限") }
    }

    func stream(_ stream: SCStream, didOutputSampleBuffer sampleBuffer: CMSampleBuffer,
                of type: SCStreamOutputType) {
        guard type == .audio, sampleBuffer.isValid, isActive else { return }
        do {
            try process(sampleBuffer)
        } catch {
            invalidate()
            Task { @MainActor in onFailure("无法转换系统音频；请重新启动会议采集") }
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
            guard isActive else { return }
            Task { @MainActor [weak self] in
                guard self?.isActive == true else { return }
                onPCM(chunk)
            }
        }
    }
}
#endif
