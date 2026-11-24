import AppKit
import AVFoundation
import ApplicationServices
import CoreGraphics
import Foundation

// Native input, microphone, menu and text delivery; transcription and routing use linked Go Core.

// Native AX ranges distinguish repeated text; text-only selection is still supported.
struct AXSelection {
    let text: String
    let range: CFRange?

    func matches(_ current: AXSelection?) -> Bool {
        guard let current, text == current.text else { return false }
        guard let range, let other = current.range else { return true }
        return range.location == other.location && range.length == other.length
    }
}

private enum HostError: Error {
    case config(String), microphone(String), conversion, bridge(String)
}

// One-time Go presentation profile drives native AppKit drawing.
struct PillSpec: Decodable {
    let recordingWidth, workingWidth, height, cancelWidth, cancelInset: Int
    let clockX, clockWidth, iconY, waveStartX, waveStep, waveBarWidth, waveBars: Int
    let textX, textRightGap, spinnerDots, spinnerX, spinnerRadius, spinnerDotSize: Int
    let terminalMS: Int
    let pillColor, clockColor, labelColor, dictationAccent, rewriteAccent: [Int]
    let errorAccent, sentMark, fallbackMark, closeColor, closeHover, attentionGlyph: [Int]
    let processingDictation, processingRewrite, selectionLabel, rewritingLabel, applyingLabel, sentLabel, errorLabel, fallbackLabel: String
}

private struct HostProfile: Decodable {
    let maximumSeconds, modifierDelayMS, mouseDelayMS, pasteRestoreMS: Int
    let copyLabel, applyLabel, applyUnsafeLabel: String
    let pill: PillSpec
}

private final class Microphone {
    private let engine = AVAudioEngine()
    private let lock = NSLock()
    private var samples: [Float] = []
    private var sampleRate: Double = 0
    private var rms = 0.0
    var meterLevel: Double {
        lock.lock()
        defer { lock.unlock() }
        return rms
    }
    private(set) var running = false

    func start(maximumSeconds: Int) throws {
        guard !running else { throw HostError.microphone("already recording") }
        let input = engine.inputNode
        let format = input.outputFormat(forBus: 0)
        guard format.channelCount > 0, format.sampleRate > 0,
              format.commonFormat == .pcmFormatFloat32 else {
            throw HostError.microphone("unsupported input format")
        }
        lock.lock()
        samples = []
        rms = 0
        sampleRate = format.sampleRate
        let maxSamples = Int(format.sampleRate) * maximumSeconds
        lock.unlock()
        input.installTap(onBus: 0, bufferSize: 1024, format: format) { [weak self] buffer, _ in
            guard let self, let channels = buffer.floatChannelData else { return }
            self.lock.lock()
            defer { self.lock.unlock() }
            let available = max(0, maxSamples - self.samples.count)
            let count = min(Int(buffer.frameLength), available)
            guard count > 0 else { return }
            let channelCount = Int(buffer.format.channelCount)
            var sumSquares = 0.0
            for i in 0..<count {
                var mono: Float = 0
                for channel in 0..<channelCount { mono += channels[channel][i] }
                mono /= Float(channelCount)
                self.samples.append(mono)
                sumSquares += Double(mono) * Double(mono)
            }
            self.rms = (sumSquares / Double(count)).squareRoot()
        }
        do {
            engine.prepare()
            try engine.start()
            running = true
        } catch {
            input.removeTap(onBus: 0)
            throw error
        }
    }

    // Stop the native device immediately. The captured samples are immutable
    // and may be converted on the existing inference worker, not the AppKit thread.
    func stop(discard: Bool = false) throws -> (samples: [Float], rate: Double) {
        guard running else { throw HostError.microphone("not recording") }
        running = false
        engine.stop()
        engine.inputNode.removeTap(onBus: 0)
        lock.lock()
        let captured = discard ? [] : samples
        let rate = sampleRate
        samples = []
        rms = 0
        lock.unlock()
        return (captured, rate)
    }

    static func pcm(_ capture: (samples: [Float], rate: Double)) throws -> Data {
        let captured = capture.samples, rate = capture.rate
        guard !captured.isEmpty else { return Data() }
        // AVAudioConverter supplies quality resampling; never resample on the audio callback.
        guard let source = AVAudioFormat(commonFormat: .pcmFormatFloat32, sampleRate: rate,
                                         channels: 1, interleaved: false),
              let dest = AVAudioFormat(commonFormat: .pcmFormatInt16, sampleRate: 16_000,
                                       channels: 1, interleaved: false),
              let converter = AVAudioConverter(from: source, to: dest),
              let input = AVAudioPCMBuffer(pcmFormat: source, frameCapacity: AVAudioFrameCount(captured.count)) else {
            throw HostError.conversion
        }
        input.frameLength = AVAudioFrameCount(captured.count)
        captured.withUnsafeBufferPointer { pointer in
            if let base = pointer.baseAddress, let output = input.floatChannelData?[0] {
                output.update(from: base, count: captured.count)
            }
        }
        var submitted = false
        var data = Data()
        var rounds = 0
        while rounds < 10_000 {
            rounds += 1
            guard let output = AVAudioPCMBuffer(pcmFormat: dest, frameCapacity: 16_384) else {
                throw HostError.conversion
            }
            var error: NSError?
            let status = converter.convert(to: output, error: &error) { _, sourceStatus in
                if submitted {
                    sourceStatus.pointee = .endOfStream
                    return nil
                }
                submitted = true
                sourceStatus.pointee = .haveData
                return input
            }
            if let error { throw error }
            if let p = output.int16ChannelData?[0], output.frameLength > 0 {
                data.append(contentsOf: UnsafeRawBufferPointer(start: p, count: Int(output.frameLength) * 2))
            }
            if status == .endOfStream { break }
            if status == .error || (status == .inputRanDry && output.frameLength == 0) {
                throw HostError.conversion
            }
        }
        guard !data.isEmpty, rounds < 10_000 else { throw HostError.conversion }
        return data
    }
}

private struct SettingsRect: Decodable {
    let X, Y, W, H: Int
    func native(_ height: Int) -> NSRect {
        NSRect(x: CGFloat(X), y: CGFloat(height - Y - H), width: CGFloat(W), height: CGFloat(H))
    }
}
private struct SettingsForm: Decodable {
    let width, height: Int
    let hint: SettingsRect
    let running, captureHint: String
    let pages: [SettingsPage]
    let controls: [SettingsControl]
}
private struct SettingsPage: Decodable {
    let id, title, subtitle: String
    let nav, subtitleRect: SettingsRect
}
private struct SettingsControl: Decodable {
    let id, page, label, kind: String
    let rect, labelRect: SettingsRect
}

private final class SettingsPanel: NSPanel {
    override func performKeyEquivalent(with event: NSEvent) -> Bool {
        let modifiers = event.modifierFlags.intersection(.deviceIndependentFlagsMask)
        if (modifiers == .command || modifiers == .control),
           let editor = firstResponder as? NSTextView, editor.isFieldEditor {
            switch event.charactersIgnoringModifiers?.lowercased() {
            case "a": editor.selectAll(nil); return true
            case "c": editor.copy(nil); return true
            case "v": editor.paste(nil); return true
            case "x": editor.cut(nil); return true
            default: break
            }
        }
        return super.performKeyEquivalent(with: event)
    }
}

private struct BridgeReply: Decodable {
    let ok: Bool
    let result: Output?
    let error: Failure?
    struct Output: Decodable {
        let text: String?
        let route: String?
        let title: String?
        let profile: HostProfile?
        let defaultConfig: String?
        let form: SettingsForm?
        let values: [String: String]?
        let config: String?
        let status: String?
        let directory: String?
        let phase: String?
        let models: [String]?
        let capture: [String: String]?
    }
    struct Failure: Decodable { let code: String; let message: String }
}

// In-process Go Core. Event/animation calculations never cross IPC.
private let onCoreEvent: @convention(c) (Int32, UInt64) -> Void = { kind, token in
    DispatchQueue.main.async { MainActor.assumeIsolated { (NSApp.delegate as? HeadlessApp)?.reportCoreEvent(kind, token) } }
}

private enum GoCore {
    static func settings(_ action: String, config: String, values: [String: String] = [:],
                         kind: String = "") throws -> BridgeReply.Output {
        let current = try JSONSerialization.jsonObject(with: Data(config.utf8))
        let payload: [String: Any] = ["action": action, "config": current, "values": values, "kind": kind]
        let json = String(decoding: try JSONSerialization.data(withJSONObject: payload), as: UTF8.self)
        return try decode(json.withCString { VRSettings($0) })
    }

    static func decode(_ result: UnsafeMutablePointer<CChar>?) throws -> BridgeReply.Output {
        guard let result else { throw HostError.bridge("Go Core returned no response") }
        defer { VRFree(result) }
        let reply = try JSONDecoder().decode(BridgeReply.self,
                           from: Data(bytes: result, count: strlen(result)))
        guard reply.ok, let output = reply.result else {
            throw HostError.bridge(reply.error?.message ?? "Go Core operation failed")
        }
        return output
    }
}

@MainActor
private final class HeadlessApp: NSObject, NSApplicationDelegate {
    private var item: NSStatusItem!
    private var tap: CFMachPort?
    private var eventTapRetry: Timer?
    private var microphone = Microphone()
    private let pill = RecordingPill()
    private var resultPanel: NSPanel?
    private var settingsPanel: NSPanel?
    private var settingsProgressTimer: Timer?
    private var settingsInputs: [String: NSTextField] = [:]
    private var settingsInitialValues: [String: String] = [:]
    private var reloadAfterTask = false
    private var settingsPages: [String: NSView] = [:]
    private var settingsStatuses: [String: NSTextField] = [:]
    private var settingsHint: NSTextField?
    private var settingsDeployDirectory = ""
    private var settingsRunning = ""
    private var settingsCaptureHint = ""
    private var settingsSerial = 0
    private var settingsCapture: String?
    private var settingsCaptureCodes: [String: String] = [:]
    private var swallowMouseUp: Int64?
    private var firstRun = false
    private var previewApply: (pid: pid_t, window: AXUIElement?, selection: AXSelection, token: UInt64, text: String)?
    private var config: HostProfile!
    private var configURL: URL!
    private var logURL: URL { configURL.deletingLastPathComponent().appendingPathComponent("VoiceRewrite.log") }
    private var taskID: UInt64 = 0
    private var targetPID: pid_t = 0
    private var targetWindow: AXUIElement?
    private var selectedAtPress: AXSelection?
    private var pending: Bool { VRSnapshot(0, 0).armed == 0 && taskID != 0 }

    func applicationDidFinishLaunching(_ notification: Notification) {
        NSApp.setActivationPolicy(.accessory)
        item = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        item.button?.title = "VoiceRewrite"
        let menu = NSMenu()
        menu.addItem(NSMenuItem(title: "复制上次结果", action: #selector(copyResult), keyEquivalent: "c"))
        menu.addItem(NSMenuItem(title: "设置…", action: #selector(openSettings), keyEquivalent: ","))
        menu.addItem(NSMenuItem(title: "重新加载配置", action: #selector(reload), keyEquivalent: "r"))
        menu.addItem(NSMenuItem(title: "显示配置文件", action: #selector(showConfig), keyEquivalent: ""))
        menu.addItem(NSMenuItem(title: "查看日志", action: #selector(showLog), keyEquivalent: ""))
        menu.addItem(NSMenuItem(title: "退出", action: #selector(quit), keyEquivalent: "q"))
        for entry in menu.items where entry.action != nil { entry.target = self }
        item.menu = menu
        pill.onCancel = { [weak self] in self?.cancelFromPill() }
        pill.onRecordingSecond = { [weak self] seconds in self?.show("● \(seconds)s") }
        pill.prepare()
        let defaultPath = FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".config/voicerewrite/config.json")
        configURL = ProcessInfo.processInfo.arguments.dropFirst().first.map {
            URL(fileURLWithPath: $0)
        } ?? defaultPath
        do { _ = try GoCore.decode(logURL.path.withCString { VRPrepareLog($0) }) }
        catch { fputs("VoiceRewrite log: \(error)\n", stderr) }
        reload()
        guard config != nil else { return }
        if firstRun { DispatchQueue.main.async { [weak self] in self?.openSettings() } }
        _ = AXIsProcessTrustedWithOptions([kAXTrustedCheckOptionPrompt.takeUnretainedValue() as String: true] as CFDictionary)
        AVCaptureDevice.requestAccess(for: .audio) { [weak self] allowed in
            DispatchQueue.main.async {
                guard let self else { return }
                if allowed { self.startEventTap() }
                else { self.show("麦克风权限未授予") }
            }
        }
    }

    @objc private func reload() {
        guard VRSnapshot(0, 0).armed == 0 && !pending else { reloadAfterTask = true; show("当前任务结束后重新加载"); return }
        reloadAfterTask = false
        do {
            let json: String
            var needsDefaults = false
            do {
                json = try String(contentsOf: configURL, encoding: .utf8)
                needsDefaults = json.trimmingCharacters(in: .whitespacesAndNewlines) == "{}"
            } catch {
                let failure = error as NSError
                guard failure.domain == NSCocoaErrorDomain && failure.code == NSFileReadNoSuchFileError else { throw error }
                json = "{}" // Go Core owns the persisted defaults and macOS shortcut bindings.
                needsDefaults = true
            }
            let output = try GoCore.decode(json.withCString { VRProfile($0) })
            guard let profile = output.profile else { throw HostError.config("missing Go host profile") }
            if needsDefaults {
                firstRun = true
                guard let defaults = output.defaultConfig else { throw HostError.config("missing Go defaults") }
                try FileManager.default.createDirectory(at: configURL.deletingLastPathComponent(),
                                                        withIntermediateDirectories: true)
                try defaults.write(to: configURL, atomically: true, encoding: .utf8)
            }
            config = profile
            pill.configure(profile.pill)
            if eventTapRetry == nil { show("VoiceRewrite") }
        } catch {
            show("配置错误")
            fputs("VoiceRewrite config: \(error)\n", stderr)
        }
    }
    @objc private func showLog() { NSWorkspace.shared.open(logURL) }
    @objc private func showConfig() { NSWorkspace.shared.activateFileViewerSelecting([configURL]) }
    @objc private func openSettings() {
        if let panel = settingsPanel, panel.isVisible {
            NSApp.activate(ignoringOtherApps: true)
            panel.makeKeyAndOrderFront(nil)
            return
        }
        settingsCapture = nil
        settingsSerial += 1
        do {
            let json = try String(contentsOf: configURL, encoding: .utf8)
            let output = try GoCore.settings("form", config: json)
            guard let form = output.form, let values = output.values else { return }
            let panel = SettingsPanel(contentRect: NSRect(x: 0, y: 0, width: CGFloat(form.width), height: CGFloat(form.height)),
                                styleMask: [.titled, .closable], backing: .buffered, defer: false)
            panel.title = "VoiceRewrite"
            panel.isReleasedWhenClosed = false
            let height = form.height
            let content = panel.contentView!
            settingsInputs = [:]
            settingsInitialValues = values
            settingsPages = [:]
            settingsStatuses = [:]
            settingsDeployDirectory = values["asr_deploy_dir"] ?? ""
            let deployStatus = try? GoCore.decode(VRASRProgress())
            settingsRunning = form.running
                settingsCaptureHint = form.captureHint
            settingsCaptureCodes = output.capture ?? [:]
            for page in form.pages {
                let pageView = NSView(frame: content.bounds)
                pageView.isHidden = page.id != form.pages.first?.id
                content.addSubview(pageView)
                settingsPages[page.id] = pageView
                let subtitle = NSTextField(labelWithString: page.subtitle)
                subtitle.frame = page.subtitleRect.native(height)
                subtitle.textColor = .secondaryLabelColor
                pageView.addSubview(subtitle)
            }
            for page in form.pages {
                let button = NSButton(title: page.title, target: self, action: #selector(settingsPage))
                button.identifier = NSUserInterfaceItemIdentifier(page.id)
                button.frame = page.nav.native(height)
                content.addSubview(button)
            }
            for entry in form.controls {
                let parent = settingsPages[entry.page] ?? content
                switch entry.kind {
                case "text", "secret", "shortcut", "stepper":
                    let label = NSTextField(labelWithString: entry.label)
                    label.frame = entry.labelRect.native(height)
                    parent.addSubview(label)
                    let field: NSTextField = entry.kind == "secret" ? NSSecureTextField() : NSTextField()
                    field.frame = entry.rect.native(height)
                    field.stringValue = values[entry.id] ?? ""
                    field.identifier = NSUserInterfaceItemIdentifier(entry.id)
                    parent.addSubview(field)
                    settingsInputs[entry.id] = field
                case "note":
                    let note = NSTextField(wrappingLabelWithString: entry.label)
                    note.frame = entry.rect.native(height)
                    note.textColor = .secondaryLabelColor
                    parent.addSubview(note)
                case "status":
                    let field = NSTextField(labelWithString: entry.label)
                    field.frame = entry.rect.native(height)
                    field.maximumNumberOfLines = 2
                    field.lineBreakMode = .byWordWrapping
                    field.textColor = .secondaryLabelColor
                    parent.addSubview(field)
                    settingsStatuses[entry.id] = field
                case "button":
                    let button = NSButton(title: entry.label, target: self, action: #selector(settingsAction))
                    button.identifier = NSUserInterfaceItemIdentifier(entry.id)
                    button.frame = entry.rect.native(height)
                    parent.addSubview(button)
                default: break
                }
            }
            let previous = deployStatus?.directory == settingsDeployDirectory ? (deployStatus?.status ?? "") : ""
            let initialHint = previous.hasPrefix("ASR 部署失败：") ? "ASR 部署失败（点击查看原因）"
                : (previous.isEmpty ? "部署位置：" + settingsDeployDirectory : previous)
            let hint = NSTextField(labelWithString: initialHint)
            hint.frame = form.hint.native(height)
            hint.textColor = .secondaryLabelColor
            hint.addGestureRecognizer(NSClickGestureRecognizer(target: self, action: #selector(settingsShowDeployError)))
            content.addSubview(hint)
            settingsHint = hint
            settingsPanel = panel
            settingsProgressTimer?.invalidate()
            settingsProgressTimer = Timer.scheduledTimer(withTimeInterval: 0.5, repeats: true) { [weak self] timer in
                guard let self, self.settingsPanel?.isVisible == true else {
                    timer.invalidate()
                    return
                }
                if let progress = try? GoCore.decode(VRASRProgress()), progress.directory == self.settingsDeployDirectory {
                    self.settingsHint?.stringValue = progress.status ?? "部署位置：" + self.settingsDeployDirectory
                }
            }
            panel.center()
            NSApp.activate(ignoringOtherApps: true)
            panel.makeKeyAndOrderFront(nil)
        } catch {
            show("配置错误")
            fputs("VoiceRewrite settings: \(error)\n", stderr)
        }
    }

    @objc private func settingsShowDeployError() {
        guard let progress = try? GoCore.decode(VRASRProgress()),
              progress.phase == "failed", progress.directory == settingsDeployDirectory,
              let message = progress.status else { return }
        let alert = NSAlert()
        alert.messageText = "ASR 部署失败"
        alert.informativeText = message
        alert.addButton(withTitle: "复制错误")
        alert.addButton(withTitle: "关闭")
        if alert.runModal() == .alertFirstButtonReturn {
            NSPasteboard.general.clearContents()
            NSPasteboard.general.setString(message, forType: .string)
        }
    }

    @objc private func settingsPage(_ sender: NSButton) {
        let selected = sender.identifier?.rawValue
        settingsCapture = nil
        settingsSerial += 1
        for (id, view) in settingsPages { view.isHidden = id != selected }
    }

    @objc private func settingsAction(_ sender: NSButton) {
        guard let id = sender.identifier?.rawValue else { return }
        switch id {
        case "close":
            settingsCapture = nil
            settingsPanel?.close()
        case "capture_rewrite", "capture_dictation":
            settingsCapture = id == "capture_rewrite" ? "rewrite_shortcut" : "dictation_shortcut"
            settingsHint?.stringValue = settingsCaptureHint
        case "advanced":
            showConfig()
        case "select_asr_directory":
            let picker = NSOpenPanel()
            picker.canChooseDirectories = true
            picker.canChooseFiles = false
            picker.canCreateDirectories = true
            picker.allowsMultipleSelection = false
            if !settingsDeployDirectory.isEmpty {
                picker.directoryURL = URL(fileURLWithPath: settingsDeployDirectory, isDirectory: true)
            }
            if picker.runModal() == .OK, let url = picker.url {
                do {
                    let json = try String(contentsOf: configURL, encoding: .utf8)
                    let saved = try GoCore.settings("save", config: json, values: ["asr_deploy_dir": url.path])
                    guard let updated = saved.config else { throw HostError.config("部署目录未保存") }
                    try updated.write(to: configURL, atomically: true, encoding: .utf8)
                    settingsDeployDirectory = url.path
                    reload()
                    settingsHint?.stringValue = "部署位置已保存：" + url.path
                } catch {
                    settingsHint?.stringValue = "保存部署目录失败：" + String(describing: error)
                }
            }
        case "deploy_asr":
            do {
                settingsHint?.stringValue = try GoCore.decode(settingsDeployDirectory.withCString { VRDeploy($0) }).status ?? "ASR 正在准备部署"
            } catch {
                settingsHint?.stringValue = String(describing: error)
            }
        case "save", "probe_asr", "probe_llm", "discover_asr_models", "discover_llm_models":
            guard let json = try? String(contentsOf: configURL, encoding: .utf8) else { return }
            let currentValues = settingsInputs.mapValues { $0.stringValue }
            let values = id == "save"
                ? currentValues.filter { settingsInitialValues[$0.key] != $0.value }
                : currentValues
            settingsSerial += 1
            let serial = settingsSerial
            if id == "save" {
                do {
                    let output = try GoCore.settings("save", config: json, values: values)
                    guard let updated = output.config else { return }
                    try updated.write(to: configURL, atomically: true, encoding: .utf8)
                    settingsInitialValues = currentValues
                    reload()
                    firstRun = false
                    settingsHint?.stringValue = reloadAfterTask ? "设置已保存，任务结束后生效" : output.status ?? ""
                } catch { settingsHint?.stringValue = String(describing: error) }
                return
            }
            let kind = id.contains("asr") ? "ASR" : "LLM"
            let statusID = kind == "ASR" ? "probe_asr_status" : "probe_llm_status"
            let action = id.hasPrefix("probe") ? "probe" : "discover"
            if action == "probe" { settingsStatuses[statusID]?.stringValue = settingsRunning }
            DispatchQueue.global(qos: .userInitiated).async { [weak self] in
                let result = Result { try GoCore.settings(action, config: json, values: values, kind: kind) }
                DispatchQueue.main.async {
                    guard let self, self.settingsSerial == serial, self.settingsPanel?.isVisible == true else { return }
                    switch result {
                    case .failure(let error):
                        self.settingsHint?.stringValue = String(describing: error)
                    case .success(let output):
                        if action == "probe" {
                            self.settingsStatuses[statusID]?.stringValue = output.status ?? ""
                        } else if let models = output.models {
                            let fieldID = kind == "ASR" ? "asr_model" : "text_model"
                            let menu = NSMenu()
                            for model in models {
                                let choice = NSMenuItem(title: model, action: #selector(self.settingsPickModel), keyEquivalent: "")
                                choice.identifier = NSUserInterfaceItemIdentifier(fieldID)
                                choice.target = self
                                menu.addItem(choice)
                            }
                            if models.isEmpty {
                                self.settingsHint?.stringValue = output.status ?? ""
                            } else {
                                menu.popUp(positioning: nil, at: NSPoint(x: 0, y: sender.bounds.height), in: sender)
                            }
                        }
                    }
                }
            }
        default: break
        }
    }

    @objc private func settingsPickModel(_ item: NSMenuItem) {
        guard let id = item.identifier?.rawValue else { return }
        settingsInputs[id]?.stringValue = item.title
    }

    @objc private func copyResult() {
        guard let result = VRLatestResult() else { show("暂无结果"); return }
        defer { VRFree(result) }
        NSPasteboard.general.clearContents()
        show(NSPasteboard.general.setString(String(cString: result), forType: .string) ? "Copied" : "Copy failed")
    }
    @objc private func applyResult() {
        guard let request = previewApply, taskID == 0,
              VRSnapshot(0, 0).armed == 0, VRSnapshot(0, request.token).valid == 1,
              let app = NSRunningApplication(processIdentifier: request.pid),
              app.isActive || app.activate(options: []) else {
            show(config.applyUnsafeLabel)
            return
        }
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.2) { [weak self] in
            guard let self, self.previewApply?.token == request.token,
                  self.taskID == 0, VRSnapshot(0, request.token).valid == 1 else { return }
            self.targetPID = request.pid
            self.targetWindow = request.window
            self.taskID = request.token
            self.pasteSelection(request.selection, with: request.text, token: request.token) { [weak self] sent in
                guard let self, self.taskID == request.token else { return }
                if sent == true {
                    self.resultPanel?.close()
                    self.resultPanel = nil
                    self.previewApply = nil
                } else if sent == nil {
                    self.presentPreview(request.text, title: self.resultPanel?.title)
                } else {
                    self.show(self.config.applyUnsafeLabel)
                }
                self.clearSession()
            }
        }
    }
    @objc private func quit() { VRCommand(Int32(VR_COMMAND_CANCEL), 0, 0, 0); NSApp.terminate(nil) }
    func applicationWillTerminate(_ notification: Notification) { VRStopASR() }
    private func show(_ text: String) { item.button?.title = text }
    fileprivate func reportCoreEvent(_ kind: Int32, _ token: UInt64) { if kind == Int32(VR_EVENT_TRANSCRIBED) && pending && taskID == token && VRSnapshot(0, token).valid == 1 { pill.working(.rewriting); show(config.pill.rewritingLabel) } }

    private func startEventTap() {
        guard tap == nil else { return }
        let eventMask = (UInt64(1) << CGEventType.flagsChanged.rawValue)
            | (UInt64(1) << CGEventType.keyDown.rawValue)
            | (UInt64(1) << CGEventType.otherMouseDown.rawValue)
            | (UInt64(1) << CGEventType.otherMouseUp.rawValue)
        let callback: CGEventTapCallBack = { _, type, event, info in
            guard let info else { return Unmanaged.passUnretained(event) }
            let app = Unmanaged<HeadlessApp>.fromOpaque(info).takeUnretainedValue()
            if type == .tapDisabledByTimeout || type == .tapDisabledByUserInput {
                // A lost key-up must not leave Go's physical Hold armed or
                // allow an old inference response to paste into the editor.
                if app.microphone.running || app.pending || VRSnapshot(0, 0).armed != 0 {
                    app.cancelFromPill()
                } else {
                    VRCommand(Int32(VR_COMMAND_CANCEL), 0, 0, 0)
                }
                if let tap = app.tap { CGEvent.tapEnable(tap: tap, enable: true) }
                return Unmanaged.passUnretained(event)
            }
            let code = event.getIntegerValueField(.keyboardEventKeycode)
            let mouse = event.getIntegerValueField(.mouseEventButtonNumber)
            if type == .otherMouseUp && app.swallowMouseUp == mouse {
                app.swallowMouseUp = nil
                return nil
            }
            if let field = app.settingsCapture, app.settingsPanel?.isVisible == true {
                let key = type == .flagsChanged ? code : type == .otherMouseDown ? 100 + mouse : -1
                if let name = app.settingsCaptureCodes[String(key)] {
                    app.settingsInputs[field]?.stringValue = name
                    app.settingsCapture = nil
                    app.settingsHint?.stringValue = ""
                    if type == .otherMouseDown {
                        app.swallowMouseUp = mouse
                        return nil
                    }
                    return Unmanaged.passUnretained(event)
                }
                if type == .keyDown && code == 53 {
                    app.settingsCapture = nil
                    app.settingsHint?.stringValue = ""
                    return nil
                }
            }
            if type == .flagsChanged {
                app.handleModifier(code: code, flags: event.flags)
            } else if type == .keyDown {
                if VRSnapshot(0, 0).armed != 0 && app.taskID == 0 {
                    VRCommand(Int32(VR_COMMAND_ABANDON), 0, 0, 0)
                    if app.microphone.running { _ = try? app.microphone.stop(discard: true) }
                }
            } else if type == .otherMouseDown || type == .otherMouseUp {
                if event.getIntegerValueField(.eventSourceUserData) == 0x56525842 {
                    return Unmanaged.passUnretained(event)
                }
                if mouse == 3 || mouse == 4 {
                    if (type == .otherMouseDown && VRSnapshot(Int32(100 + mouse), 0).role != 0) ||
                        (type == .otherMouseUp && VRSnapshot(Int32(100 + mouse), 0).down != 0) {
                        if app.updatePress(code: 100 + mouse,
                                           down: type == .otherMouseDown,
                                           delay: Double(app.config.mouseDelayMS) / 1000,
                                           cursor: event.location) {
                            return nil
                        }
                    }
                }
            }
            return Unmanaged.passUnretained(event)
        }
        tap = CGEvent.tapCreate(tap: .cgSessionEventTap, place: .headInsertEventTap,
                                options: .defaultTap, eventsOfInterest: eventMask,
                                callback: callback,
                                userInfo: Unmanaged.passUnretained(self).toOpaque())
        guard let tap else {
            show(AXIsProcessTrusted() ? "Input unavailable" : "Allow Accessibility")
            // TCC permission may be granted after this first attempt. Retry the
            // native event tap without restarting the app or rebinding Go Core.
            if eventTapRetry == nil {
                eventTapRetry = Timer.scheduledTimer(withTimeInterval: 2, repeats: true) { [weak self] timer in
                    guard let self else { timer.invalidate(); return }
                    MainActor.assumeIsolated { self.startEventTap() }
                }
            }
            return
        }
        eventTapRetry?.invalidate()
        eventTapRetry = nil
        if let source = CFMachPortCreateRunLoopSource(kCFAllocatorDefault, tap, 0) {
            CFRunLoopAddSource(CFRunLoopGetMain(), source, CFRunLoopMode.commonModes)
        }
        CGEvent.tapEnable(tap: tap, enable: true)
        show("VoiceRewrite")
    }

    private func handleModifier(code: Int64, flags: CGEventFlags) {
        let family: CGEventFlags
        switch code {
        case 54: family = .maskCommand
        case 61: family = .maskAlternate
        default: return
        }
        let state = VRSnapshot(Int32(code), 0)
        guard state.role != 0 || state.down != 0 else { return }
        updatePress(code: code,
                    down: state.down == 0 && flags.contains(family),
                    delay: Double(config.modifierDelayMS) / 1000)
    }
    @discardableResult
    private func updatePress(code: Int64, down: Bool, delay: TimeInterval,
                             cursor: CGPoint? = nil) -> Bool {
        let key = Int32(code)
        if down {
            resultPanel?.close()
            resultPanel = nil
            previewApply = nil
            let press = VRCommand(Int32(VR_COMMAND_PRESS), key, 0, pending ? 1 : 0)
            let token = press.token
            guard token != 0 else { return false }
            targetPID = NSWorkspace.shared.frontmostApplication?.processIdentifier ?? 0
            targetWindow = focusedWindow()
            selectedAtPress = nil
            taskID = 0
            do { try microphone.start(maximumSeconds: config.maximumSeconds) }
            catch { VRCommand(Int32(VR_COMMAND_ABANDON), 0, 0, 0); reset("麦克风错误"); fputs("Microphone: \(error)\n", stderr); return true }
            if press.role == 2 {
                // Snapshot while the original selection is still active. Do not
                // block the CGEvent tap with synchronous Accessibility requests.
                DispatchQueue.main.async { [weak self] in
                    guard let self, VRSnapshot(key, token).can_activate == 1 else { return }
                    self.selectedAtPress = self.selectionFromAX()
                }
            }
            DispatchQueue.main.asyncAfter(deadline: .now() + delay) { [weak self] in
                guard let self, VRCommand(Int32(VR_COMMAND_ACTIVATE), key, token, 0).kind == Int32(VR_EVENT_ACTIVATED) else { return }
                self.begin(rewrite: VRSnapshot(key, 0).held_role == 2)
            }
        } else {
            let event = VRCommand(Int32(VR_COMMAND_RELEASE), key, 0, 0)
            let rewrite = event.role == 2
            let outcome = event.release
            guard outcome != 0 else { return false }
            if outcome == 2 && microphone.running { finish(rewrite: rewrite) }
            else if outcome == 2 || outcome == 3 {
                if microphone.running { _ = try? microphone.stop(discard: true) }
            }
            if (outcome == 1 || outcome == 3), let cursor {
                let source = CGEventSource(stateID: .hidSystemState)
                for kind in [CGEventType.otherMouseDown, .otherMouseUp] {
                    guard let event = CGEvent(mouseEventSource: source, mouseType: kind,
                                              mouseCursorPosition: cursor, mouseButton: .center) else { return true }
                    event.setIntegerValueField(.mouseEventButtonNumber, value: code - 100)
                    event.setIntegerValueField(.eventSourceUserData, value: 0x56525842)
                    event.post(tap: .cghidEventTap)
                }
            }
        }
        return true
    }

    private func begin(rewrite: Bool) {
        guard targetPID > 0 else { reset("No target"); return }
        guard NSWorkspace.shared.frontmostApplication?.processIdentifier == targetPID else { reset("Focus changed"); return }
        // Do not require an AX editor for a browser selection: recording and
        // the fallback result must still work without it.
        guard microphone.running else { reset("麦克风错误"); return }
        taskID = VRCommand(Int32(VR_COMMAND_START), 0, 0, 0).token
        pill.recording(rewrite: rewrite, started: Date(),
                       meter: { [weak self] in self?.microphone.meterLevel ?? 0 })
        let token = taskID
        DispatchQueue.main.asyncAfter(deadline: .now() + Double(config.maximumSeconds)) { [weak self] in
            guard let self, VRSnapshot(0, token).valid == 1, self.microphone.running else { return }
            self.finish(rewrite: rewrite)
        }
    }
    private func finish(rewrite: Bool) {
        guard microphone.running, taskID != 0 else { return }
        VRCommand(Int32(VR_COMMAND_ABANDON), 0, 0, 0)
        pill.working(rewrite ? .selection : .processing)
        show(rewrite ? config.pill.selectionLabel : config.pill.processingDictation)
        let token = taskID
        do {
            let capture = try microphone.stop()
            let submit: (AXSelection?) -> Void = { [weak self] selected in
                guard let self, VRSnapshot(0, token).valid == 1 else { return }
                guard !rewrite || selected != nil else { self.reset("Select text"); return }
                if rewrite { self.pill.working(.processing); self.show(self.config.pill.processingRewrite) }
                DispatchQueue.global(qos: .userInitiated).async { [weak self] in
                    let result = Result {
                        let pcm = try Microphone.pcm(capture)
                        return try (selected?.text ?? "").withCString { source in
                            try pcm.withUnsafeBytes { bytes in
                                try GoCore.decode(VRRun(rewrite ? 1 : 0,
                                    bytes.bindMemory(to: UInt8.self).baseAddress,
                                    Int32(pcm.count), source, token, onCoreEvent))
                            }
                        }
                    }
                    DispatchQueue.main.async {
                        guard let self, VRSnapshot(0, token).valid == 1 else { return }
                        switch result {
                        case .failure(let error):
                            self.reset(self.config.pill.errorLabel)
                            fputs("VoiceRewrite: \(error)\n", stderr)
                        case .success(let value):
                            guard let text = value.text, !text.isEmpty else { self.reset("Empty"); return }
                            let action = (value.route ?? "").withCString {
                                VRDeliveryAction($0, self.targetStillValid() ? 1 : 0, selected == nil ? 0 : 1)
                            }
                            if action == Int32(VR_DELIVERY_REPLACE), let selected {
                                self.pill.working(.applying)
                                self.show(self.config.pill.applyingLabel)
                                self.pasteSelection(selected, with: text, token: token) { [weak self] sent in
                                    guard let self, self.taskID == token else { return }
                                    if sent != true {
                                        self.presentPreview(text, title: value.title,
                                                            selection: sent == false ? selected : nil)
                                    }
                                    self.clearSession()
                                }
                                return
                            }
                            if action == Int32(VR_DELIVERY_INSERT) {
                                if self.paste(text) != true { self.presentPreview(text, title: value.title) }
                            } else {
                                // A replace result remains manually applicable after focus moves.
                                self.presentPreview(text, title: value.title,
                                                    selection: action == Int32(VR_DELIVERY_PREVIEW_APPLY) ? selected : nil)
                            }
                            self.clearSession()
                        }
                    }
                }
            }
            // Prefer the press-time selection; never adopt another app's
            // selection after focus changed during recording.
            let selected = rewrite ? (selectedAtPress ?? (targetStillValid() ? selectionFromAX() : nil)) : nil
            if rewrite && selected == nil && targetStillValid() {
                // Browser text can be copyable even when AXSelectedText is absent.
                copySelection(token: token) { text in
                    submit(text.map { AXSelection(text: $0, range: nil) })
                }
            } else {
                submit(selected)
            }
        } catch { reset("音频错误"); fputs("Audio: \(error)\n", stderr) }
    }

    // Native presentation only: Go Core retains the result and decides when
    // a preview is required; no clipboard mutation occurs until Copy is clicked.
    private func presentPreview(_ text: String, title: String?, selection: AXSelection? = nil) {
        resultPanel?.close()
        previewApply = nil
        if let selection, taskID != 0 {
            previewApply = (targetPID, targetWindow, selection, taskID, text)
        }
        let panel = NSPanel(contentRect: NSRect(x: 0, y: 0, width: 500, height: 310),
                            styleMask: [.titled, .closable, .resizable], backing: .buffered, defer: false)
        panel.title = title ?? "VoiceRewrite Result"
        panel.isReleasedWhenClosed = false
        panel.level = .floating
        panel.hidesOnDeactivate = false
        panel.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary]
        panel.minSize = NSSize(width: 360, height: 220)
        let scroll = NSTextView.scrollableTextView()
        scroll.frame = NSRect(x: 16, y: 56, width: 468, height: 238)
        scroll.autoresizingMask = [.width, .height]
        if let view = scroll.documentView as? NSTextView {
            view.string = text
            view.isEditable = false
            view.isSelectable = true
            view.font = .systemFont(ofSize: 14)
        }
        let copy = NSButton(title: config.copyLabel, target: self, action: #selector(copyResult))
        copy.frame = NSRect(x: 16, y: 14, width: 120, height: 30)
        panel.contentView?.addSubview(scroll)
        panel.contentView?.addSubview(copy)
        if previewApply != nil {
            let apply = NSButton(title: config.applyLabel, target: self, action: #selector(applyResult))
            apply.frame = NSRect(x: 152, y: 14, width: 138, height: 30)
            panel.contentView?.addSubview(apply)
        }
        panel.center()
        panel.orderFrontRegardless()
        resultPanel = panel
        pill.result(.fallback)
        show(panel.title)
    }

    private func focusedWindow() -> AXUIElement? {
        var value: CFTypeRef?
        guard AXUIElementCopyAttributeValue(AXUIElementCreateApplication(targetPID),
                kAXFocusedWindowAttribute as CFString, &value) == .success, let value else { return nil }
        guard CFGetTypeID(value) == AXUIElementGetTypeID() else { return nil }
        return (value as! AXUIElement)
    }

    private func focusedElement() -> AXUIElement? {
        var value: CFTypeRef?
        guard AXUIElementCopyAttributeValue(AXUIElementCreateSystemWide(),
                kAXFocusedUIElementAttribute as CFString, &value) == .success, let value,
              CFGetTypeID(value) == AXUIElementGetTypeID() else { return nil }
        let element = value as! AXUIElement
        var owner: pid_t = 0
        // Never read an AX selection from a different process.
        guard AXUIElementGetPid(element, &owner) == .success, owner == targetPID else { return nil }
        return element
    }

    private func selectionFromAX() -> AXSelection? {
        guard let focused = focusedElement() else { return nil }
        var value: CFTypeRef?
        let status = AXUIElementCopyAttributeValue(focused, kAXSelectedTextAttribute as CFString, &value)
        let direct = status == .success ? (value as? String).flatMap { $0.isEmpty ? nil : $0 } : nil
        // Preserve text-only selection or a position-aware range when supported.
        var selectedRange: CFTypeRef?
        guard AXUIElementCopyAttributeValue(focused, kAXSelectedTextRangeAttribute as CFString,
                                            &selectedRange) == .success,
              let selectedRange, CFGetTypeID(selectedRange) == AXValueGetTypeID() else {
            return direct.map { AXSelection(text: $0, range: nil) }
        }
        let rangeValue = selectedRange as! AXValue
        var range = CFRange(location: 0, length: 0)
        guard AXValueGetType(rangeValue) == .cfRange,
              AXValueGetValue(rangeValue, .cfRange, &range),
              range.location >= 0, range.length > 0 else {
            return direct.map { AXSelection(text: $0, range: nil) }
        }
        if let direct { return AXSelection(text: direct, range: range) }
        // A range-only editor is queried for just the selected span.
        var text: CFTypeRef?
        guard AXUIElementCopyParameterizedAttributeValue(
            focused, kAXStringForRangeParameterizedAttribute as CFString,
            rangeValue, &text) == .success, let string = text as? String,
              !string.isEmpty else { return nil }
        return AXSelection(text: string, range: range)
    }

    // Copy selected browser text without adopting it as a paste target.
    // Only the clipboard generation created by our copy may be restored.
    private func copySelection(token: UInt64, completion: @escaping (String?) -> Void) {
        let board = NSPasteboard.general
        guard let saved = snapshotClipboard(board),
              let source = CGEventSource(stateID: .hidSystemState),
              let down = CGEvent(keyboardEventSource: source, virtualKey: 8, keyDown: true),
              let up = CGEvent(keyboardEventSource: source, virtualKey: 8, keyDown: false)
        else { completion(nil); return }
        let before = board.changeCount
        down.flags = .maskCommand
        up.flags = .maskCommand
        // Let the physical modifier key-up reach the editor before
        // synthesizing Cmd+C; otherwise the copy chord can be altered.
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.05) { [weak self] in
            guard let self, VRSnapshot(0, token).valid == 1,
                  self.targetStillValid(), board.changeCount == before else {
                completion(nil); return
            }
            down.post(tap: .cghidEventTap)
            up.post(tap: .cghidEventTap)
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.18) {
                let lease = board.changeCount
                let text = lease == before ? nil : board.string(forType: .string)
                if lease != before && board.changeCount == lease {
                    board.clearContents()
                    if !saved.isEmpty { _ = board.writeObjects(saved) }
                }
                completion(text?.isEmpty == false ? text : nil)
            }
        }
    }

    private func snapshotClipboard(_ board: NSPasteboard) -> [NSPasteboardItem]? {
        let originals = board.pasteboardItems
        if originals == nil && board.types?.isEmpty == false { return nil }
        var saved: [NSPasteboardItem] = []
        var totalBytes = 0
        for item in originals ?? [] {
            let copy = NSPasteboardItem()
            for type in item.types {
                guard let data = item.data(forType: type),
                      VRClipboardSnapshotFits(UInt64(data.count), UInt64(totalBytes)) != 0,
                      copy.setData(data, forType: type) else { return nil }
                totalBytes += data.count
            }
            saved.append(copy)
        }
        return saved
    }

    private func targetStillValid() -> Bool {
        guard VRSnapshot(0, taskID).valid == 1,
              NSWorkspace.shared.frontmostApplication?.processIdentifier == targetPID else { return false }
        guard let targetWindow else { return true }
        return focusedWindow().map { CFEqual(targetWindow, $0) } == true
    }

    // One native replacement path for automatic delivery and the result's Apply button.
    // Windows likewise checks the current selection by copying before replacing.
    private func pasteSelection(_ selected: AXSelection, with text: String,
                                token: UInt64, completed: @escaping (Bool?) -> Void) {
        guard targetStillValid() else { completed(false); return }
        if selected.matches(selectionFromAX()) {
            completed(paste(text))
        } else {
            copySelection(token: token) { [weak self] current in
                guard let self, self.taskID == token, VRSnapshot(0, token).valid == 1 else { return }
                completed(current == selected.text && self.targetStillValid() ? self.paste(text) : false)
            }
        }
    }

    private func paste(_ text: String) -> Bool? {
        let source = CGEventSource(stateID: .hidSystemState)
        guard let down = CGEvent(keyboardEventSource: source, virtualKey: 9, keyDown: true),
              let up = CGEvent(keyboardEventSource: source, virtualKey: 9, keyDown: false) else {
            return false
        }
        let board = NSPasteboard.general
        let stamp = board.changeCount
        guard let saved = snapshotClipboard(board) else { return pasteUnicode(text) }
        guard board.changeCount == stamp else { return false }
        guard targetStillValid() else { return false }
        board.clearContents()
        let cleared = board.changeCount
        guard board.setString(text, forType: .string) else {
            // Failed writes must not erase a newer clipboard owner.
            if board.changeCount == cleared && !saved.isEmpty { _ = board.writeObjects(saved) }
            return false
        }
        let lease = board.changeCount
        down.flags = .maskCommand
        up.flags = .maskCommand
        guard board.changeCount == lease, targetStillValid() else {
            if board.changeCount == lease { board.clearContents(); _ = board.writeObjects(saved) }
            return false
        }
        down.post(tap: .cghidEventTap)
        up.post(tap: .cghidEventTap)
        show(config.pill.sentLabel)
        pill.result(.sent)
        // Only changeCount, not matching text, fences clipboard ownership.
        DispatchQueue.main.asyncAfter(deadline: .now() + Double(config.pasteRestoreMS) / 1000) {
            guard board.changeCount == lease else { return }
            board.clearContents()
            if !saved.isEmpty { _ = board.writeObjects(saved) }
        }
        return true
    }
    // nil means some Unicode chunks may have been sent; never offer another Apply.
    private func pasteUnicode(_ text: String) -> Bool? {
        let source = CGEventSource(stateID: .hidSystemState)
        guard let down = CGEvent(keyboardEventSource: source, virtualKey: 49, keyDown: true),
              let up = CGEvent(keyboardEventSource: source, virtualKey: 49, keyDown: false)
        else { return false }
        let units = Array(text.utf16)
        var start = 0
        while start < units.count {
            guard targetStillValid() else { return start == 0 ? false : nil }
            var end = min(start + 20, units.count)
            if end < units.count && (0xD800...0xDBFF).contains(units[end - 1]) { end -= 1 }
            for event in [down, up] {
                units[start..<end].withUnsafeBufferPointer {
                    event.keyboardSetUnicodeString(stringLength: $0.count, unicodeString: $0.baseAddress)
                }
                event.post(tap: .cghidEventTap)
            }
            start = end
        }
        show(config.pill.sentLabel)
        pill.result(.sent)
        return true
    }

    private func cancelFromPill() {
        if VRSnapshot(0, 0).armed == 0 && !pending && !microphone.running { pill.dismiss(); show("VoiceRewrite"); return }
        VRCommand(Int32(VR_COMMAND_CANCEL), 0, 0, 0)
        if microphone.running { _ = try? microphone.stop(discard: true) }
        clearSession()
        pill.dismiss()
        show("已取消")
    }

    private func reset(_ message: String, phase: PillAppearance = .error) {
        if microphone.running { _ = try? microphone.stop(discard: true) }
        if pill.isVisible { pill.result(phase) }
        clearSession()
        show(message)
    }

    private func clearSession() {
        VRCommand(Int32(VR_COMMAND_ABANDON), 0, 0, 0)
        targetWindow = nil
        selectedAtPress = nil
        taskID = 0
        if reloadAfterTask {
            reloadAfterTask = false
            reload()
        }
    }
}

#if !FOCUS_GUARD_TEST
MainActor.assumeIsolated {
    let app = NSApplication.shared
    let delegate = HeadlessApp()
    app.delegate = delegate
    app.run()
}
#endif
