import AppKit
import AVFoundation
import ApplicationServices
import CoreGraphics
import Foundation

// Native boundaries only: device input, microphone, menu bar and OS text delivery.
// All transcription, intent and routing decisions call the linked Go Core.
private enum Action { case dictation, rewrite }

// Require stable native window and focused-element identities, not editor text.
// Also check the window title when exposed to catch browser tab changes.
struct FocusIdentity {
    let window: AXUIElement
    let element: AXUIElement
    let title: String?

    func matches(window: AXUIElement?, element: AXUIElement?, title: String?) -> Bool {
        guard let window, let element,
              CFEqual(self.window, window), CFEqual(self.element, element) else { return false }
        return self.title == nil || self.title == title
    }
}
private enum HostError: Error {
    case config(String), microphone(String), conversion, bridge(String)
}

// Presentation values come from the Windows-owned Go indicator package.
// Swift only decodes the one-time profile and draws native AppKit pixels.
struct PillSpec: Decodable {
    let recordingWidth, workingWidth, height, cancelWidth, cancelInset: Int
    let clockX, clockWidth, iconY, waveStartX, waveStep, waveBarWidth, waveBars: Int
    let textX, textRightGap, spinnerDots, spinnerX, spinnerRadius, spinnerDotSize: Int
    let terminalMS: Int
    let pillColor, clockColor, labelColor, dictationAccent, rewriteAccent: [Int]
    let errorAccent, sentMark, fallbackMark, closeColor, closeHover, attentionGlyph: [Int]
    let processingDictation, processingRewrite, sentLabel, errorLabel, fallbackLabel: String
}

private struct HostProfile: Decodable {
    let dictation, rewrite: String
    let maximumSeconds, modifierDelayMS, mouseDelayMS, pasteRestoreMS: Int
    let pill: PillSpec
}

private struct HostConfig {
    let raw: [String: Any]
    let profile: HostProfile

    static func load(_ url: URL) throws -> HostConfig {
        let data = try Data(contentsOf: url)
        guard let json = try JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            throw HostError.config("config.json must be a JSON object")
        }
        guard let profile = try GoCore.request(config: json, operation: "host_profile",
                                               audio: Data(), selection: nil).profile else {
            throw HostError.config("missing Go host profile")
        }
        return HostConfig(raw: json, profile: profile)
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

    func stop(discard: Bool = false) throws -> Data {
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
        guard !discard, !captured.isEmpty else { return Data() }
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
        let profile: HostProfile?
    }
    struct Failure: Decodable { let code: String; let message: String }
}

// In-process Go Core. Event/animation calculations never cross IPC.
private enum GoCore {
    static func request(config: [String: Any], operation: String, audio: Data,
                        selection: String?, token: UInt64 = 0) throws -> BridgeReply.Output {
        let data = try JSONSerialization.data(withJSONObject: config)
        let json = String(decoding: data, as: UTF8.self)
        let result = json.withCString { cfg in
            if operation == "host_profile" { return VRProfile(cfg) }
            return operation.withCString { op in
                (selection ?? "").withCString { selected in
                    audio.withUnsafeBytes { pcm in
                        VRRun(cfg, op, pcm.bindMemory(to: UInt8.self).baseAddress,
                              Int32(audio.count), selected, token)
                    }
                }
            }
        }
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
    private var microphone = Microphone()
    private let pill = RecordingPill()
    private var config: HostConfig!
    private var configURL: URL!
    private var activeAction: Action?
    private var taskID: UInt64 = 0
    private var targetPID: pid_t = 0
    private var targetFocus: FocusIdentity?
    private var selected: String?
    private var pending = false

    func applicationDidFinishLaunching(_ notification: Notification) {
        NSApp.setActivationPolicy(.accessory)
        item = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        item.button?.title = "VR"
        let menu = NSMenu()
        menu.addItem(NSMenuItem(title: "Reload config", action: #selector(reload), keyEquivalent: "r"))
        menu.addItem(NSMenuItem(title: "Quit", action: #selector(quit), keyEquivalent: "q"))
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
            pill.configure(config.profile.pill)
            show("VR")
        } catch {
            show("Config error")
            fputs("VoiceRewrite config: \(error)\n", stderr)
        }
    }
    @objc private func quit() { VREpochCancel(); NSApp.terminate(nil) }
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
                if VRArmed() != 0 && !app.microphone.running {
                    VRAbandon()
                    app.activeAction = nil
                }
            } else if type == .otherMouseDown || type == .otherMouseUp {
                if event.getIntegerValueField(.eventSourceUserData) == 0x56525842 {
                    return Unmanaged.passUnretained(event)
                }
                if mouse == 3 || mouse == 4 {
                    let name = mouse == 3 ? "Mouse4" : "Mouse5"
                    if name == app.config.profile.dictation || name == app.config.profile.rewrite {
                        app.updatePress(code: 100 + mouse, name: name,
                                        down: type == .otherMouseDown,
                                        delay: Double(app.config.profile.mouseDelayMS) / 1000,
                                        cursor: event.location)
                        return nil // Intercept hold; replay an ordinary short click below
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
        if let source = CFMachPortCreateRunLoopSource(kCFAllocatorDefault, tap, 0) {
            CFRunLoopAddSource(CFRunLoopGetMain(), source, CFRunLoopMode.commonModes)
        }
        CGEvent.tapEnable(tap: tap, enable: true)
    }

    private func handleModifier(code: Int64, flags: CGEventFlags) {
        let (name, family): (String, CGEventFlags)
        switch code {
        case 54: (name, family) = ("RightCommand", .maskCommand)
        case 61: (name, family) = ("RightOption", .maskAlternate)
        default: return
        }
        // Right-side flagsChanged events can fire while another modifier is held.
        // A tracked press is a release; a new press requires the matching flag.
        updatePress(code: code, name: name,
                    down: VRDown(Int32(code)) == 0 && flags.contains(family),
                    delay: Double(config.profile.modifierDelayMS) / 1000)
    }
    private func updatePress(code: Int64, name: String, down: Bool, delay: TimeInterval,
                             cursor: CGPoint? = nil) {
        guard name == config.profile.dictation || name == config.profile.rewrite else { return }
        let key = Int32(code)
        if down {
            let token = VRPress(key, pending ? 1 : 0)
            guard token != 0 else { return }
            activeAction = name == config.profile.dictation ? .dictation : .rewrite
            DispatchQueue.main.asyncAfter(deadline: .now() + delay) { [weak self] in
                guard let self, VRActivate(key, token) == 1,
                      let action = self.activeAction else { return }
                self.begin(action)
            }
        } else {
            let outcome = VRRelease(key)
            if outcome == 2 && microphone.running { finish() }
            else if outcome == 2 || outcome == 3 { activeAction = nil }
            if (outcome == 1 || outcome == 3), let cursor {
                replayClick(button: code - 100, cursor: cursor)
            }
        }
    }
    private func replayClick(button: Int64, cursor: CGPoint) {
        let source = CGEventSource(stateID: .hidSystemState)
        for kind in [CGEventType.otherMouseDown, .otherMouseUp] {
            guard let event = CGEvent(mouseEventSource: source, mouseType: kind,
                                      mouseCursorPosition: cursor, mouseButton: .center) else { return }
            event.setIntegerValueField(.mouseEventButtonNumber, value: button)
            event.setIntegerValueField(.eventSourceUserData, value: 0x56525842)
            event.post(tap: .cghidEventTap)
        }
    }

    private func begin(_ action: Action) {
        targetPID = NSWorkspace.shared.frontmostApplication?.processIdentifier ?? 0
        guard targetPID > 0 else { reset("No target"); return }
        // No AX text access needed: only verify a stable destination identity.
        guard let window = focusedWindow(), let element = focusedElement() else {
            reset("Target unavailable"); return
        }
        targetFocus = FocusIdentity(window: window, element: element, title: windowTitle(window))
        selected = nil
        if action == .rewrite {
            guard let text = selectionFromAX(), !text.isEmpty else {
                reset("Select text"); return
            }
            selected = text
        }
        taskID = VREpochStart()
        do {
            try microphone.start(maximumSeconds: config.profile.maximumSeconds)
            pill.recording(rewrite: action == .rewrite, started: Date(),
                           meter: { [weak self] in self?.microphone.meterLevel ?? 0 })
            let token = taskID
            DispatchQueue.main.asyncAfter(deadline: .now() + Double(config.profile.maximumSeconds)) { [weak self] in
                guard let self, VREpochValid(token) == 1, self.microphone.running else { return }
                self.finish()
            }
        } catch { reset("Mic error"); fputs("Microphone: \(error)\n", stderr) }
    }
    private func finish() {
        guard microphone.running, let action = activeAction else { return }
        activeAction = nil
        pending = true
        pill.processing()
        show(action == .dictation ? config.profile.pill.processingDictation : config.profile.pill.processingRewrite)
        let configValue = config.raw
        let selectedValue = selected
        let pid = targetPID
        let token = taskID
        do {
            let pcm = try microphone.stop()
            DispatchQueue.global(qos: .userInitiated).async { [weak self] in
                let result = Result {
                    try GoCore.request(config: configValue,
                                      operation: action == .dictation ? "transcribe" : "selected_voice_task",
                                      audio: pcm, selection: selectedValue, token: token)
                }
                DispatchQueue.main.async {
                    guard let self, VREpochValid(token) == 1 else { return }
                    self.pending = false
                    switch result {
                    case .failure(let error):
                        self.reset(self.config.profile.pill.errorLabel)
                        fputs("VoiceRewrite: \(error)\n", stderr)
                    case .success(let value):
                        guard let text = value.text, !text.isEmpty else {
                            self.reset("Empty"); return
                        }
                        guard value.route == "replace" || (action == .dictation && value.route == "insert") else {
                            self.reset(self.config.profile.pill.fallbackLabel, phase: .fallback); return
                        }
                        guard NSWorkspace.shared.frontmostApplication?.processIdentifier == pid else {
                            self.reset("Focus changed"); return
                        }
                        let currentWindow = self.focusedWindow()
                        guard let focus = self.targetFocus,
                              focus.matches(window: currentWindow,
                                            element: self.focusedElement(),
                                            title: currentWindow.flatMap(self.windowTitle)) else {
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
        return (value as! AXUIElement)
    }

    private func windowTitle(_ window: AXUIElement) -> String? {
        var value: CFTypeRef?
        guard AXUIElementCopyAttributeValue(window, kAXTitleAttribute as CFString,
                                             &value) == .success else { return nil }
        return value as? String
    }

    private func selectionFromAX() -> String? {
        guard let focused = focusedElement() else { return nil }
        var value: CFTypeRef?
        guard AXUIElementCopyAttributeValue(focused, kAXSelectedTextAttribute as CFString,
                                            &value) == .success else { return nil }
        return value as? String
    }

    private func paste(_ text: String) {
        // Do not retry uncertain writes: a Cmd+V event is not proof of editor insertion.
        // Prepare both events before touching the clipboard; don't report a
        // paste event if CoreGraphics could not create one.
        let source = CGEventSource(stateID: .hidSystemState)
        guard let down = CGEvent(keyboardEventSource: source, virtualKey: 9, keyDown: true),
              let up = CGEvent(keyboardEventSource: source, virtualKey: 9, keyDown: false) else {
            reset("Paste unavailable")
            return
        }
        let board = NSPasteboard.general
        let stamp = board.changeCount
        let originals = board.pasteboardItems
        guard originals != nil || board.types?.isEmpty != false else {
            reset("Clipboard unsupported"); return
        }
        // NSPasteboardItem is not NSCopying; preserve each advertised data type.
        var saved: [NSPasteboardItem] = []
        for item in originals ?? [] {
            let copy = NSPasteboardItem()
            for type in item.types {
                guard let data = item.data(forType: type), copy.setData(data, forType: type) else {
                    reset("Clipboard unsupported"); return
                }
            }
            saved.append(copy)
        }
        guard board.changeCount == stamp else { reset("Clipboard changed"); return }
        board.clearContents()
        guard board.setString(text, forType: .string) else {
            board.clearContents()
            if !saved.isEmpty { _ = board.writeObjects(saved) }
            reset("Paste failed")
            return
        }
        let lease = board.changeCount
        down.flags = .maskCommand
        up.flags = .maskCommand
        down.post(tap: .cghidEventTap)
        up.post(tap: .cghidEventTap)
        show(config.profile.pill.sentLabel)
        pill.result(.sent)
        // Text equality alone does not prove ownership: another app can copy
        // the very same text. changeCount protects that user's newer clipboard.
        DispatchQueue.main.asyncAfter(deadline: .now() + Double(config.profile.pasteRestoreMS) / 1000) {
            guard board.changeCount == lease else { return }
            board.clearContents()
            if !saved.isEmpty { _ = board.writeObjects(saved) }
        }
    }
    // Clicking the floating pill cancels the active capture or suppresses a
    // pending bridge result. It must never retry or deliver a stale write.
    private func cancelFromPill() {
        VREpochCancel()
        if microphone.running { _ = try? microphone.stop(discard: true) }
        clearSession()
        pill.dismiss()
        show("Cancelled")
    }

    private func reset(_ message: String, phase: PillAppearance = .error) {
        if pill.isVisible { pill.result(phase) }
        clearSession()
        show(message)
    }

    private func clearSession() {
        activeAction = nil
        selected = nil
        targetFocus = nil
        pending = false
    }
}

#if FOCUS_GUARD_TEST
MainActor.assumeIsolated {
    let window = AXUIElementCreateApplication(90001)
    let otherWindow = AXUIElementCreateApplication(90002)
    let editor = AXUIElementCreateApplication(90003)
    let otherEditor = AXUIElementCreateApplication(90004)
    let focus = FocusIdentity(window: window, element: editor, title: "Original tab")
    precondition(focus.matches(window: window, element: editor, title: "Original tab"))
    precondition(!focus.matches(window: nil, element: editor, title: "Original tab"))
    precondition(!focus.matches(window: window, element: nil, title: "Original tab"))
    precondition(!focus.matches(window: otherWindow, element: editor, title: "Original tab"))
    precondition(!focus.matches(window: window, element: otherEditor, title: "Original tab"))
    precondition(!focus.matches(window: window, element: editor, title: "Switched tab"))
    print("AX focus identity smoke passed")
}
#else
MainActor.assumeIsolated {
    let app = NSApplication.shared
    let delegate = HeadlessApp()
    app.delegate = delegate
    app.run()
}
#endif
