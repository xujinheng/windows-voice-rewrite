import AppKit
import AVFoundation
import ApplicationServices
import CoreGraphics
import Foundation

// Native input, microphone, menu and text delivery; transcription and routing use linked Go Core.
private enum Action { case dictation, rewrite }

// Native AX ranges distinguish repeated text; text-only selection is still supported.
struct AXSelection {
    let text: String
    let range: CFRange?

    func matches(_ current: AXSelection?) -> Bool {
        guard let current, text == current.text else { return false }
        guard let range else { return true }
        guard let other = current.range else { return false }
        return range.location == other.location && range.length == other.length
    }
}

// Match available AX identities; ordinary dictation need not have AX attributes.
struct FocusIdentity {
    let window: AXUIElement?
    let element: AXUIElement?
    let title: String?

    func matches(window: AXUIElement?, element: AXUIElement?, title: String?) -> Bool {
        if let saved = self.window, window.map({ CFEqual(saved, $0) }) != true { return false }
        if let saved = self.element, element.map({ CFEqual(saved, $0) }) != true { return false }
        return self.title == nil || self.title == title
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
    let dictation, rewrite: String
    let maximumSeconds, modifierDelayMS, mouseDelayMS, pasteRestoreMS: Int
    let pill: PillSpec
}

private struct HostConfig {
    let raw: String
    let profile: HostProfile
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
private let onASRTranscribed: @convention(c) (UInt64) -> Void = { token in
    DispatchQueue.main.async { MainActor.assumeIsolated { (NSApp.delegate as? HeadlessApp)?.reportTranscribed(token) } }
}

private enum GoCore {
    static func request(config: String, operation: String, audio: Data,
                        selection: String?, token: UInt64 = 0) throws -> BridgeReply.Output {
        let result = config.withCString { cfg in
            if operation == "host_profile" { return VRProfile(cfg) }
            return operation.withCString { op in
                (selection ?? "").withCString { selected in
                    audio.withUnsafeBytes { pcm in
                        VRRun(cfg, op, pcm.bindMemory(to: UInt8.self).baseAddress,
                              Int32(audio.count), selected, token, onASRTranscribed)
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
    private var pending: Bool { activeAction == nil && taskID != 0 }
    private var lastResult: String?

    func applicationDidFinishLaunching(_ notification: Notification) {
        NSApp.setActivationPolicy(.accessory)
        item = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        item.button?.title = "VR"
        let menu = NSMenu()
        menu.addItem(NSMenuItem(title: "Copy last result", action: #selector(copyResult), keyEquivalent: "c"))
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
        guard activeAction == nil && !pending else { show("Reload after current task"); return }
        do {
            let json = try String(contentsOf: configURL, encoding: .utf8)
            guard let profile = try GoCore.request(config: json, operation: "host_profile",
                                                   audio: Data(), selection: nil).profile
            else { throw HostError.config("missing Go host profile") }
            config = HostConfig(raw: json, profile: profile)
            pill.configure(profile.pill)
            show("VR")
        } catch {
            show("Config error")
            fputs("VoiceRewrite config: \(error)\n", stderr)
        }
    }
    @objc private func copyResult() {
        guard let lastResult else { show("No result"); return }
        NSPasteboard.general.clearContents()
        show(NSPasteboard.general.setString(lastResult, forType: .string) ? "Copied" : "Copy failed")
    }
    @objc private func quit() { VREpochCancel(); NSApp.terminate(nil) }
    private func show(_ text: String) { item.button?.title = text }
    fileprivate func reportTranscribed(_ token: UInt64) { if pending && taskID == token && VREpochValid(token) == 1 { pill.working(.rewriting); show(config.profile.pill.rewritingLabel) } }

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
                if app.microphone.running || app.pending || VRArmed() != 0 {
                    app.cancelFromPill()
                } else {
                    VREpochCancel()
                }
                if let tap = app.tap { CGEvent.tapEnable(tap: tap, enable: true) }
                return Unmanaged.passUnretained(event)
            }
            let code = event.getIntegerValueField(.keyboardEventKeycode)
            let mouse = event.getIntegerValueField(.mouseEventButtonNumber)
            if type == .flagsChanged {
                app.handleModifier(code: code, flags: event.flags)
            } else if type == .keyDown {
                if VRArmed() != 0 && app.taskID == 0 {
                    VRAbandon()
                    if app.microphone.running { _ = try? app.microphone.stop(discard: true) }
                    app.activeAction = nil
                }
            } else if type == .otherMouseDown || type == .otherMouseUp {
                if event.getIntegerValueField(.eventSourceUserData) == 0x56525842 {
                    return Unmanaged.passUnretained(event)
                }
                if mouse == 3 || mouse == 4 {
                    let name = mouse == 3 ? "Mouse4" : "Mouse5"
                    if (type == .otherMouseDown && (name == app.config.profile.dictation ||
                        name == app.config.profile.rewrite)) ||
                        (type == .otherMouseUp && VRDown(Int32(100 + mouse)) != 0) {
                        app.updatePress(code: 100 + mouse, name: name,
                                        down: type == .otherMouseDown,
                                        delay: Double(app.config.profile.mouseDelayMS) / 1000,
                                        cursor: event.location)
                        return nil
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
        updatePress(code: code, name: name,
                    down: VRDown(Int32(code)) == 0 && flags.contains(family),
                    delay: Double(config.profile.modifierDelayMS) / 1000)
    }
    private func updatePress(code: Int64, name: String, down: Bool, delay: TimeInterval,
                             cursor: CGPoint? = nil) {
        let key = Int32(code)
        if down {
            guard name == config.profile.dictation || name == config.profile.rewrite else { return }
            let token = VRPress(key, pending ? 1 : 0)
            guard token != 0 else { return }
            activeAction = name == config.profile.dictation ? .dictation : .rewrite
            targetPID = NSWorkspace.shared.frontmostApplication?.processIdentifier ?? 0
            taskID = 0
            // Pre-roll starts on physical down; a short click discards it.
            do { try microphone.start(maximumSeconds: config.profile.maximumSeconds) }
            catch { VRAbandon(); reset("Mic error"); fputs("Microphone: \(error)\n", stderr); return }
            DispatchQueue.main.asyncAfter(deadline: .now() + delay) { [weak self] in
                guard let self, VRActivate(key, token) == 1,
                      let action = self.activeAction else { return }
                self.begin(action)
            }
        } else {
            let outcome = VRRelease(key)
            if outcome == 2 && microphone.running { finish() }
            else if outcome == 2 || outcome == 3 {
                if microphone.running { _ = try? microphone.stop(discard: true) }
                activeAction = nil
            }
            if (outcome == 1 || outcome == 3), let cursor {
                let source = CGEventSource(stateID: .hidSystemState)
                for kind in [CGEventType.otherMouseDown, .otherMouseUp] {
                    guard let event = CGEvent(mouseEventSource: source, mouseType: kind,
                                              mouseCursorPosition: cursor, mouseButton: .center) else { return }
                    event.setIntegerValueField(.mouseEventButtonNumber, value: code - 100)
                    event.setIntegerValueField(.eventSourceUserData, value: 0x56525842)
                    event.post(tap: .cghidEventTap)
                }
            }
        }
    }

    private func begin(_ action: Action) {
        guard targetPID > 0 else { reset("No target"); return }
        guard NSWorkspace.shared.frontmostApplication?.processIdentifier == targetPID else { reset("Focus changed"); return }
        let window = focusedWindow(), element = focusedElement()
        guard action == .dictation || (window != nil && element != nil) else { reset("Target unavailable"); return }
        targetFocus = FocusIdentity(window: window, element: element,
                                    title: window.flatMap(windowTitle))
        guard microphone.running else { reset("Mic error"); return }
        taskID = VREpochStart()
        pill.recording(rewrite: action == .rewrite, started: Date(),
                       meter: { [weak self] in self?.microphone.meterLevel ?? 0 })
        let token = taskID
        DispatchQueue.main.asyncAfter(deadline: .now() + Double(config.profile.maximumSeconds)) { [weak self] in
            guard let self, VREpochValid(token) == 1, self.microphone.running else { return }
            self.finish()
        }
    }
    private func finish() {
        guard microphone.running, let action = activeAction else { return }
        activeAction = nil
        pill.working(action == .dictation ? .processing : .selection)
        show(action == .dictation ? config.profile.pill.processingDictation : config.profile.pill.selectionLabel)
        let configValue = config.raw
        let pid = targetPID
        let token = taskID
        do {
            let pcm = try microphone.stop()
            if action == .rewrite {
                let window = focusedWindow()
                guard NSWorkspace.shared.frontmostApplication?.processIdentifier == pid,
                      targetFocus?.matches(window: window, element: focusedElement(),
                                           title: window.flatMap(windowTitle)) == true else { reset("Focus changed"); return }
            }
            let selectedValue = action == .rewrite ? selectionFromAX() : nil
            if action == .rewrite && selectedValue == nil { reset("Select text"); return }
            if action == .rewrite { pill.working(.processing); show(config.profile.pill.processingRewrite) }
            DispatchQueue.global(qos: .userInitiated).async { [weak self] in
                let result = Result {
                    try GoCore.request(config: configValue,
                                      operation: action == .dictation ? "transcribe" : "selected_voice_task",
                                      audio: pcm, selection: selectedValue?.text, token: token)
                }
                DispatchQueue.main.async {
                    guard let self, VREpochValid(token) == 1 else { return }
                    defer { self.clearSession() }
                    switch result {
                    case .failure(let error):
                        self.reset(self.config.profile.pill.errorLabel)
                        fputs("VoiceRewrite: \(error)\n", stderr)
                    case .success(let value):
                        guard let text = value.text, !text.isEmpty else { self.reset("Empty"); return }
                        self.lastResult = text
                        guard value.route == "replace" || value.route == "insert" else {
                            self.reset(self.config.profile.pill.fallbackLabel, phase: .fallback); return
                        }
                        guard self.targetStillValid() else { self.reset("Focus changed"); return }
                        if action == .rewrite && selectedValue?.matches(self.selectionFromAX()) != true {
                            self.reset("Selection changed"); return
                        }
                        if value.route == "replace" { self.pill.working(.applying); self.show(self.config.profile.pill.applyingLabel) }
                        self.paste(text)
                    }
                }
            }
        } catch { reset("Audio error"); fputs("Audio: \(error)\n", stderr) }
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

    private func windowTitle(_ window: AXUIElement) -> String? {
        var value: CFTypeRef?
        guard AXUIElementCopyAttributeValue(window, kAXTitleAttribute as CFString,
                                             &value) == .success else { return nil }
        return value as? String
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

    private func targetStillValid() -> Bool {
        let window = focusedWindow()
        return VREpochValid(taskID) == 1 &&
            NSWorkspace.shared.frontmostApplication?.processIdentifier == targetPID &&
            targetFocus?.matches(window: window, element: focusedElement(),
                                 title: window.flatMap(windowTitle)) == true
    }

    private func paste(_ text: String) {
        // Prebuild the single paste event; a sent event is not confirmed insertion.
        let source = CGEventSource(stateID: .hidSystemState)
        guard let down = CGEvent(keyboardEventSource: source, virtualKey: 9, keyDown: true),
              let up = CGEvent(keyboardEventSource: source, virtualKey: 9, keyDown: false) else {
            reset("Paste unavailable")
            return
        }
        let board = NSPasteboard.general
        let stamp = board.changeCount
        let originals = board.pasteboardItems
        if originals == nil && board.types?.isEmpty == false { pasteUnicode(text); return }
        // NSPasteboardItem is not NSCopying; preserve each advertised data type.
        var saved: [NSPasteboardItem] = []
        var totalBytes = 0
        for item in originals ?? [] {
            let copy = NSPasteboardItem()
            for type in item.types {
                guard let data = item.data(forType: type),
                      VRClipboardSnapshotFits(UInt64(data.count), UInt64(totalBytes)) != 0,
                      copy.setData(data, forType: type) else {
                    pasteUnicode(text); return
                }
                totalBytes += data.count
            }
            saved.append(copy)
        }
        guard board.changeCount == stamp else { reset("Clipboard changed"); return }
        guard targetStillValid() else { reset("Focus changed"); return }
        board.clearContents()
        let cleared = board.changeCount
        guard board.setString(text, forType: .string) else {
            // Failed writes must not erase a newer clipboard owner.
            if board.changeCount == cleared && !saved.isEmpty { _ = board.writeObjects(saved) }
            reset("Paste failed")
            return
        }
        let lease = board.changeCount
        down.flags = .maskCommand
        up.flags = .maskCommand
        // Never paste clipboard content replaced by another app.
        guard board.changeCount == lease else { reset("Clipboard changed"); return }
        down.post(tap: .cghidEventTap)
        up.post(tap: .cghidEventTap)
        show(config.profile.pill.sentLabel)
        pill.result(.sent)
        // Only changeCount, not matching text, fences clipboard ownership.
        DispatchQueue.main.asyncAfter(deadline: .now() + Double(config.profile.pasteRestoreMS) / 1000) {
            guard board.changeCount == lease else { return }
            board.clearContents()
            if !saved.isEmpty { _ = board.writeObjects(saved) }
        }
    }
    private func pasteUnicode(_ text: String) {
        let source = CGEventSource(stateID: .hidSystemState)
        guard let down = CGEvent(keyboardEventSource: source, virtualKey: 49, keyDown: true),
              let up = CGEvent(keyboardEventSource: source, virtualKey: 49, keyDown: false)
        else { reset("Paste unavailable"); return }
        let units = Array(text.utf16)
        var start = 0
        while start < units.count {
            guard targetStillValid() else { reset("Focus changed"); return }
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
        show(config.profile.pill.sentLabel)
        pill.result(.sent)
    }

    private func cancelFromPill() {
        if activeAction == nil && !pending && !microphone.running { pill.dismiss(); show("VR"); return }
        VREpochCancel()
        if microphone.running { _ = try? microphone.stop(discard: true) }
        clearSession()
        pill.dismiss()
        show("Cancelled")
    }

    private func reset(_ message: String, phase: PillAppearance = .error) {
        if microphone.running { _ = try? microphone.stop(discard: true) }
        if pill.isVisible { pill.result(phase) }
        clearSession()
        show(message)
    }

    private func clearSession() {
        activeAction = nil
        targetFocus = nil
        taskID = 0
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
