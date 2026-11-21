// This is appended to the host source for the native CI-only smoke binary.
// It is never linked into the production app.
// Same-file extension can exercise private host state without widening production access.
extension HeadlessApp {
    func verifyFirstRunConfig() {
        let temp = FileManager.default.temporaryDirectory
            .appendingPathComponent("VoiceRewriteFirstRun-\(UUID().uuidString)")
        defer { try? FileManager.default.removeItem(at: temp) }
        item = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        configURL = temp.appendingPathComponent("nested/config.json")
        reload()
        let defaults = try! String(contentsOf: configURL, encoding: .utf8)
        let coreDefaults = try! GoCore.request(config: "{}", audio: Data(), selection: nil).defaultConfig
        precondition(defaults == coreDefaults)
        precondition(VRSnapshot(54, 0).role == 1 && VRSnapshot(61, 0).role == 2)
        try! "  {}\n".write(to: configURL, atomically: true, encoding: .utf8)
        reload()
        precondition((try? String(contentsOf: configURL, encoding: .utf8)) == defaults)

        let custom = #"{"dictation_hotkey":"Mouse4","hotkey_record":"Mouse5"}"#
        try! custom.write(to: configURL, atomically: true, encoding: .utf8)
        reload()
        precondition((try? String(contentsOf: configURL, encoding: .utf8)) == custom)
        precondition(VRSnapshot(103, 0).role == 1 && VRSnapshot(104, 0).role == 2)

        try! "invalid JSON".write(to: configURL, atomically: true, encoding: .utf8)
        reload()
        precondition((try? String(contentsOf: configURL, encoding: .utf8)) == "invalid JSON")
        precondition(item.button?.title == "Config error")
        NSStatusBar.system.removeStatusItem(item)
    }

    func verifyClipboardSnapshot() {
        let board = NSPasteboard(name: NSPasteboard.Name("VoiceRewriteScopeTest"))
        board.clearContents()
        precondition(board.setString("keep clipboard", forType: .string))
        precondition(snapshotClipboard(board)?.first?.string(forType: .string) == "keep clipboard")
    }

    func verifyReboundRelease(code: Int64, before: String,
                              after: String, modifier: Bool) {
        let initial = try! GoCore.request(config: before, audio: Data(), selection: nil)
        config = initial.profile!
        precondition(VRSnapshot(Int32(code), 0).role != 0)
        updatePress(code: code, down: true, delay: 3600)
        precondition(VRSnapshot(Int32(code), 0).down == 1 && VRSnapshot(0, 0).armed == 1)
        let heldRole = VRSnapshot(Int32(code), 0).held_role
        precondition(heldRole != 0)
        let rebound = try! GoCore.request(config: after, audio: Data(), selection: nil)
        config = rebound.profile!
        precondition(VRSnapshot(Int32(code), 0).role == 0 && VRSnapshot(Int32(code), 0).held_role == heldRole)
        if modifier {
            handleModifier(code: code, flags: [])
        } else {
            updatePress(code: code, down: false, delay: 0)
        }
        precondition(VRSnapshot(Int32(code), 0).down == 0 && VRSnapshot(0, 0).armed == 0 && VRSnapshot(Int32(code), 0).held_role == 0)
    }

    func verifyPressTargetSurvivesDelayedActivation() {
        item = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        targetPID = pid_t.max // The original editor vanished or lost focus during the hold threshold.
        begin(rewrite: false)
        precondition(!microphone.running && item.button?.title == "Focus changed")
        NSStatusBar.system.removeStatusItem(item)
    }

    func verifyPillCancelLifecycle() {
        item = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        let finishedToken = VRCommand(Int32(VR_COMMAND_START), 0, 0, 0).token
        cancelFromPill()
        precondition(VRSnapshot(0, finishedToken).valid == 1) // Dismissing a result is not cancellation.
        precondition(item.button?.title == "VoiceRewrite")
        let key: Int32 = 61
        precondition(VRSnapshot(key, 0).role == 2)
        let held = VRCommand(Int32(VR_COMMAND_PRESS), key, 0, 0).token
        precondition(held != 0 && VRCommand(Int32(VR_COMMAND_ACTIVATE), key, held, 0).kind == Int32(VR_EVENT_ACTIVATED))
        let processingToken = VRCommand(Int32(VR_COMMAND_START), 0, 0, 0).token
        taskID = processingToken
        precondition(!pending)
        VRCommand(Int32(VR_COMMAND_ABANDON), 0, 0, 0) // as on timed recording completion
        precondition(pending && VRSnapshot(0, 0).armed == 0)
        _ = VRCommand(Int32(VR_COMMAND_RELEASE), key, 0, 0).release
        cancelFromPill()
        precondition(!pending && taskID == 0 && VRSnapshot(0, processingToken).valid == 0) // In-flight work still cancels.
        precondition(item.button?.title == "Cancelled")
        NSStatusBar.system.removeStatusItem(item)
    }
}
MainActor.assumeIsolated {
    // The shipped status-bar app creates NSApplication before querying AppKit/Quartz.
    _ = NSApplication.shared
    let window = AXUIElementCreateApplication(90001)
    let otherWindow = AXUIElementCreateApplication(90002)
    let editor = AXUIElementCreateApplication(90003)
    let otherEditor = AXUIElementCreateApplication(90004)
    // AX focused elements have an owning PID; never accept another app's
    // selected text even when the system-wide focus changes mid-capture.
    var owner: pid_t = 0
    precondition(AXUIElementGetPid(editor, &owner) == .success && owner == 90003)
    precondition(AXUIElementGetPid(otherEditor, &owner) == .success && owner != 90003)
    let focus = FocusIdentity(window: window, element: editor, title: "Original tab")
    precondition(focus.matches(window: window, element: editor, title: "Original tab"))
    precondition(!focus.matches(window: nil, element: editor, title: "Original tab"))
    precondition(!focus.matches(window: window, element: nil, title: "Original tab"))
    precondition(!focus.matches(window: otherWindow, element: editor, title: "Original tab"))
    precondition(!focus.matches(window: window, element: otherEditor, title: "Original tab"))
    precondition(!focus.matches(window: window, element: editor, title: "Switched tab"))
    let initial = AXSelection(text: "repeat", range: CFRange(location: 5, length: 6))
    precondition(initial.matches(AXSelection(text: "repeat", range: CFRange(location: 5, length: 6))))
    precondition(!initial.matches(AXSelection(text: "repeat", range: CFRange(location: 30, length: 6))))
    precondition(!initial.matches(AXSelection(text: "repeat", range: nil)))
    precondition(!initial.matches(AXSelection(text: "changed", range: CFRange(location: 5, length: 6))))
    let textOnly = AXSelection(text: "repeat", range: nil)
    precondition(textOnly.matches(AXSelection(text: "repeat", range: nil)))
    precondition(!textOnly.matches(AXSelection(text: "changed", range: nil)))
    // Dictation must tolerate caret and tab-title changes within the same window.
    let dictationTarget = FocusIdentity(window: window, element: nil, title: nil)
    precondition(dictationTarget.matches(window: window, element: otherEditor, title: "Switched tab"))
    precondition(!dictationTarget.matches(window: otherWindow, element: editor, title: "Original tab"))
    // Dictation can retain whatever AX identity was actually available.
    let partial = FocusIdentity(window: window, element: nil, title: "Original tab")
    precondition(partial.matches(window: window, element: nil, title: "Original tab"))
    precondition(!partial.matches(window: otherWindow, element: nil, title: "Original tab"))
    let noAX = FocusIdentity(window: nil, element: nil, title: nil)
    precondition(noAX.matches(window: nil, element: nil, title: nil))
    precondition(noAX.matches(window: nil, element: nil, title: "Other"))
    // Exercise the actual native C ABI over the Go clipboard ownership budget.
    precondition(VRClipboardSnapshotFits(1, 0) == 1)
    precondition(VRClipboardSnapshotFits(16 << 20, 32 << 20) == 1)
    precondition(VRClipboardSnapshotFits((16 << 20) + 1, 0) == 0)
    precondition(VRClipboardSnapshotFits(1, 48 << 20) == 0)
    // The native PCM conversion can run after recording is stopped without
    // accessing AVAudioEngine on a worker or requiring a physical microphone.
    let pcm = try! Microphone.pcm((samples: Array(repeating: Float(0.01), count: 19200), rate: 48000))
    precondition(pcm.count > 3200 && pcm.count % 2 == 0)
    // Exercise the production Swift -> native Go Hold path without posting HID events.
    // A shortcut can be rebound while its old physical key is still down.
    let defaults = try! GoCore.request(config: "{}", audio: Data(), selection: nil).defaultConfig!
    let original = try! JSONSerialization.jsonObject(with: Data(defaults.utf8)) as! [String: Any]
    var mouseBound = original
    mouseBound["dictation_hotkey"] = "Mouse4"
    func host(_ raw: [String: Any]) -> String {
        String(decoding: try! JSONSerialization.data(withJSONObject: raw), as: UTF8.self)
    }
    let hostApp = HeadlessApp()
    hostApp.verifyClipboardSnapshot()
    hostApp.verifyReboundRelease(code: 103, before: host(mouseBound),
                                 after: host(original), modifier: false)
    hostApp.verifyReboundRelease(code: 54, before: host(original),
                                 after: host(mouseBound), modifier: true)
    hostApp.verifyPillCancelLifecycle()
    hostApp.verifyPressTargetSurvivesDelayedActivation()
    HeadlessApp().verifyFirstRunConfig()
    print("AX focus, selection, clipboard budget, shortcut rebind, first-run config, pill cancel and press-target smoke passed")
}
