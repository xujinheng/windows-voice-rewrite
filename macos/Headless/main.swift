import AppKit
import AVFoundation
import ApplicationServices
import CoreGraphics
import Foundation

// Native boundaries only: device input, microphone, menu bar and OS text delivery.
// All transcription, prompt/intent and routing decisions live in voicebridge (Go).
private enum Action { case dictation, rewrite }
private enum HostError: Error {
    case config(String), microphone(String), selection, conversion, bridge(String), targetChanged
}

private struct HostConfig {
    let raw: [String: Any]
    let dictation: String
    let rewrite: String
    let maximumSeconds: Int

    static func load(_ url: URL) throws -> HostConfig {
        let data = try Data(contentsOf: url)
        guard let json = try JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            throw HostError.config("config.json must be a JSON object")
        }
        let dictation = json["dictation_hotkey"] as? String ?? "RightCommand"
        let rewrite = json["hotkey_record"] as? String ?? "RightOption"
        let allowed = ["RightCommand", "RightOption", "Mouse4", "Mouse5"]
        guard allowed.contains(dictation), allowed.contains(rewrite), dictation != rewrite else {
            throw HostError.config("Use distinct RightCommand, RightOption, Mouse4 or Mouse5 shortcuts")
        }
        let limit = json["max_record_seconds"] as? Int ?? 180
        guard (1...180).contains(limit) else {
            throw HostError.config("max_record_seconds must be 1...180")
        }
        return HostConfig(raw: json, dictation: dictation, rewrite: rewrite, maximumSeconds: limit)
    }
}

private final class Microphone {
    private let engine = AVAudioEngine()
    private let lock = NSLock()
    private var samples: [Float] = []
    private var sampleRate: Double = 0
    private var maxSamples = 0
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
        maxSamples = Int(format.sampleRate) * maximumSeconds
        lock.unlock()
        input.installTap(onBus: 0, bufferSize: 1024, format: format) { [weak self] buffer, _ in
            guard let self, let channels = buffer.floatChannelData else { return }
            self.lock.lock()
            defer { self.lock.unlock() }
            let available = max(0, self.maxSamples - self.samples.count)
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

    func stop() throws -> Data {
        guard running else { throw HostError.microphone("not recording") }
        running = false
        engine.stop()
        engine.inputNode.removeTap(onBus: 0)
        lock.lock()
        let captured = samples
        let rate = sampleRate
        samples = []
        rms = 0
        lock.unlock()
        guard captured.count >= Int(rate / 10) else {
            throw HostError.microphone("audio shorter than 100 ms")
        }
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

private struct BridgeReply: Decodable {
    let ok: Bool
    let result: Output?
    let error: Failure?
    struct Output: Decodable {
        let text: String?
        let route: String?
    }
    struct Failure: Decodable { let code: String; let message: String }
}

private enum GoCore {
    static func request(config: [String: Any], operation: String,
                        audio: Data, selection: String?) throws -> BridgeReply.Output {
        let executable = Bundle.main.executableURL!
            .deletingLastPathComponent().appendingPathComponent("voicebridge")
        guard FileManager.default.isExecutableFile(atPath: executable.path) else {
            throw HostError.bridge("missing bundled voicebridge")
        }
        let process = Process()
        process.executableURL = executable
        let stdin = Pipe(), stdout = Pipe()
        process.standardInput = stdin
        process.standardOutput = stdout
        process.standardError = FileHandle.standardError
        let payload: [String: Any] = [
            "v": 1, "id": UUID().uuidString, "op": operation,
            "config": config, "pcm_base64": audio.base64EncodedString(),
            "can_insert": true, "selected_text": selection ?? ""
        ]
        let request = try JSONSerialization.data(withJSONObject: payload)
        try process.run()
        stdin.fileHandleForWriting.write(request + Data([10]))
        try? stdin.fileHandleForWriting.close()
        // Drain stdout while the helper is alive to avoid pipe-buffer deadlocks.
        let output = stdout.fileHandleForReading.readDataToEndOfFile()
        process.waitUntilExit()
        guard process.terminationStatus == 0,
              let line = output.split(separator: 10).last,
              let reply = try? JSONDecoder().decode(BridgeReply.self, from: Data(line)) else {
            throw HostError.bridge("voicebridge failed or returned malformed JSON")
        }
        guard reply.ok, let result = reply.result else {
            throw HostError.bridge(reply.error?.message ?? "Go Core operation failed")
        }
        return result
    }
}

@MainActor
private final class HeadlessApp: NSObject, NSApplicationDelegate {
    private var item: NSStatusItem!
    private var tap: CFMachPort?
    private var source: CFRunLoopSource?
    private var microphone = Microphone()
    private let pill = RecordingPill()
    private var config: HostConfig!
    private var configURL: URL!
    private var pressed = Set<Int64>()
    private var activeKey: Int64?
    private var activeAction: Action?
    private var started: Date?
    private var timer: Timer?
    private var generation: UInt64 = 0
    private var targetPID: pid_t = 0
    private var selected: String?
    private var pending = false

    func applicationDidFinishLaunching(_ notification: Notification) {
        NSApp.setActivationPolicy(.accessory)
        item = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        item.button?.title = "VR"
        let menu = NSMenu()
        menu.addItem(NSMenuItem(title: "VoiceRewrite — headless", action: nil, keyEquivalent: ""))
        menu.addItem(NSMenuItem(title: "Reload config", action: #selector(reload), keyEquivalent: "r"))
        menu.addItem(NSMenuItem.separator())
        menu.addItem(NSMenuItem(title: "Quit", action: #selector(quit), keyEquivalent: "q"))
        for entry in menu.items where entry.action != nil { entry.target = self }
        item.menu = menu
        pill.onCancel = { [weak self] in self?.cancelFromPill() }
        pill.prepare()
        let defaultPath = FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".config/voicerewrite/config.json")
        configURL = ProcessInfo.processInfo.arguments.dropFirst().first.map {
            URL(fileURLWithPath: $0)
        } ?? defaultPath
        reload()
        guard config != nil else { return }
        _ = AXIsProcessTrustedWithOptions([kAXTrustedCheckOptionPrompt.takeUnretainedValue() as String: true] as CFDictionary)
        AVCaptureDevice.requestAccess(for: .audio) { [weak self] allowed in
            DispatchQueue.main.async {
                guard let self else { return }
                if allowed { self.startEventTap() }
                else { self.show("Mic denied") }
            }
        }
    }

    @objc private func reload() {
        do {
            config = try HostConfig.load(configURL)
            show("VR")
        } catch {
            show("Config error")
            fputs("VoiceRewrite config: \(error)\n", stderr)
        }
    }
    @objc private func quit() { NSApp.terminate(nil) }
    private func show(_ text: String) { item.button?.title = text }

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
                if let tap = app.tap { CGEvent.tapEnable(tap: tap, enable: true) }
                return Unmanaged.passUnretained(event)
            }
            let code = event.getIntegerValueField(.keyboardEventKeycode)
            let mouse = event.getIntegerValueField(.mouseEventButtonNumber)
            // The event tap executes on the main runloop; never perform audio/IPC here.
            if type == .flagsChanged {
                app.handleModifier(code: code, flags: event.flags)
            } else if type == .keyDown {
                if app.activeKey != nil && !app.microphone.running { app.generation &+= 1 }
            } else if type == .otherMouseDown || type == .otherMouseUp {
                if mouse == 3 || mouse == 4 {
                    let name = mouse == 3 ? "Mouse4" : "Mouse5"
                    if name == app.config.dictation || name == app.config.rewrite {
                        app.handleButton(code: 100 + mouse, down: type == .otherMouseDown)
                        return nil // a configured voice button must not navigate the browser
                    }
                }
            }
            return Unmanaged.passUnretained(event)
        }
        tap = CGEvent.tapCreate(tap: .cgSessionEventTap, place: .headInsertEventTap,
                                options: .defaultTap, eventsOfInterest: eventMask,
                                callback: callback,
                                userInfo: Unmanaged.passUnretained(self).toOpaque())
        guard let tap else { show("Input denied"); return }
        source = CFMachPortCreateRunLoopSource(kCFAllocatorDefault, tap, 0)
        if let source { CFRunLoopAddSource(CFRunLoopGetMain(), source, CFRunLoopMode.commonModes) }
        CGEvent.tapEnable(tap: tap, enable: true)
    }

    private func handleModifier(code: Int64, flags: CGEventFlags) {
        let (name, family): (String, CGEventFlags)
        switch code {
        case 54: (name, family) = ("RightCommand", .maskCommand)
        case 61: (name, family) = ("RightOption", .maskAlternate)
        default: return
        }
        guard name == config.dictation || name == config.rewrite else { return }
        if pressed.contains(code) {
            pressed.remove(code)
            release(code)
        } else if flags.contains(family) {
            pressed.insert(code)
            arm(code: code, action: name == config.dictation ? .dictation : .rewrite,
                delay: 0.20)
        }
    }
    private func handleButton(code: Int64, down: Bool) {
        let name = code == 103 ? "Mouse4" : "Mouse5"
        if down {
            pressed.insert(code)
            arm(code: code, action: name == config.dictation ? .dictation : .rewrite,
                delay: 0.25)
        } else {
            pressed.remove(code)
            release(code)
        }
    }
    private func arm(code: Int64, action: Action, delay: TimeInterval) {
        guard activeKey == nil, !pending else { return }
        activeKey = code
        activeAction = action
        generation &+= 1
        let token = generation
        DispatchQueue.main.asyncAfter(deadline: .now() + delay) { [weak self] in
            guard let self, self.generation == token, self.pressed.contains(code),
                  self.activeKey == code else { return }
            self.begin(action)
        }
    }
    private func begin(_ action: Action) {
        targetPID = NSWorkspace.shared.frontmostApplication?.processIdentifier ?? 0
        guard targetPID > 0 else { reset("No target"); return }
        selected = nil
        if action == .rewrite {
            guard let text = selectionFromAX(), !text.isEmpty else {
                reset("Select text"); return
            }
            selected = text
        }
        do {
            try microphone.start(maximumSeconds: config.maximumSeconds)
            let startTime = Date()
            started = startTime
            pill.recording(rewrite: action == .rewrite, started: startTime,
                           meter: { [weak self] in self?.microphone.meterLevel ?? 0 })
            show("● 0s")
            timer?.invalidate()
            timer = Timer.scheduledTimer(withTimeInterval: 0.5, repeats: true) { [weak self] _ in
                DispatchQueue.main.async {
                    guard let self, let started = self.started else { return }
                    let elapsed = Int(Date().timeIntervalSince(started))
                    self.show("● \(elapsed)s")
                    if elapsed >= self.config.maximumSeconds { self.finish() }
                }
            }
        } catch { reset("Mic error"); fputs("Microphone: \(error)\n", stderr) }
    }
    private func release(_ code: Int64) {
        guard activeKey == code else { return }
        generation &+= 1
        if microphone.running { finish() }
        else { activeKey = nil; activeAction = nil }
    }
    private func finish() {
        guard microphone.running, let action = activeAction else { return }
        timer?.invalidate()
        timer = nil
        started = nil
        activeKey = nil
        activeAction = nil
        pending = true
        pill.processing()
        show("Processing…")
        let configValue = config.raw
        let selectedValue = selected
        let pid = targetPID
        let token = generation
        do {
            let pcm = try microphone.stop()
            DispatchQueue.global(qos: .userInitiated).async { [weak self] in
                let result = Result {
                    try GoCore.request(config: configValue,
                                      operation: action == .dictation ? "transcribe" : "selected_voice_task",
                                      audio: pcm, selection: selectedValue)
                }
                DispatchQueue.main.async {
                    guard let self, self.generation == token else { return }
                    self.pending = false
                    switch result {
                    case .failure(let error):
                        self.reset("ASR error")
                        fputs("VoiceRewrite: \(error)\n", stderr)
                    case .success(let value):
                        guard let text = value.text, !text.isEmpty else {
                            self.reset("Empty"); return
                        }
                        guard value.route == "replace" || (action == .dictation && value.route == "insert") else {
                            self.reset("Preview only"); return
                        }
                        guard NSWorkspace.shared.frontmostApplication?.processIdentifier == pid else {
                            self.reset("Focus changed"); return
                        }
                        if action == .rewrite && self.selectionFromAX() != selectedValue {
                            self.reset("Selection changed"); return
                        }
                        self.paste(text)
                    }
                }
            }
        } catch { pending = false; reset("Audio error"); fputs("Audio: \(error)\n", stderr) }
    }

    private func selectionFromAX() -> String? {
        let system = AXUIElementCreateSystemWide()
        var focused: CFTypeRef?
        guard AXUIElementCopyAttributeValue(system, kAXFocusedUIElementAttribute as CFString,
                                            &focused) == .success, let focused else { return nil }
        var value: CFTypeRef?
        guard AXUIElementCopyAttributeValue(focused as! AXUIElement,
                                            kAXSelectedTextAttribute as CFString, &value) == .success
        else { return nil }
        return value as? String
    }

    private func paste(_ text: String) {
        // Do not retry uncertain writes: a Cmd+V event is not proof of editor insertion.
        let board = NSPasteboard.general
        let old = board.pasteboardItems?.compactMap { $0.copy() as? NSPasteboardItem } ?? []
        board.clearContents()
        guard board.setString(text, forType: .string) else { reset("Paste failed"); return }
        let source = CGEventSource(stateID: .hidSystemState)
        let down = CGEvent(keyboardEventSource: source, virtualKey: 9, keyDown: true)
        let up = CGEvent(keyboardEventSource: source, virtualKey: 9, keyDown: false)
        down?.flags = .maskCommand
        up?.flags = .maskCommand
        down?.post(tap: .cghidEventTap)
        up?.post(tap: .cghidEventTap)
        show("Sent ✓")
        pill.result(.sent)
        // Restore only if this lease still owns the clipboard.
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.7) {
            if board.string(forType: .string) == text {
                board.clearContents()
                if !old.isEmpty { _ = board.writeObjects(old) }
            }
        }
    }
    // Clicking the floating pill cancels the active capture or suppresses a
    // pending bridge result. It must never retry or deliver a stale write.
    private func cancelFromPill() {
        generation &+= 1
        if microphone.running { _ = try? microphone.stop() }
        timer?.invalidate()
        timer = nil
        activeKey = nil
        activeAction = nil
        started = nil
        selected = nil
        pending = false
        pill.dismiss()
        show("Cancelled")
    }

    private func reset(_ message: String) {
        if pill.isVisible {
            pill.result(message == "Preview only" ? .fallback : .error)
        }
        timer?.invalidate()
        timer = nil
        activeKey = nil
        activeAction = nil
        pending = false
        started = nil
        selected = nil
        show(message)
    }
}

MainActor.assumeIsolated {
    let app = NSApplication.shared
    let delegate = HeadlessApp()
    app.delegate = delegate
    app.run()
}
