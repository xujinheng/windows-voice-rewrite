// This is appended to the host source for the native CI-only smoke binary.
// It is never linked into the production app.
// Same-file extension can exercise private host state without widening production access.
extension HeadlessApp {
    func verifyReboundRelease(code: Int64, before: HostConfig,
                              after: HostConfig, modifier: Bool) {
        config = before
        _ = try! GoCore.request(config: before.raw, 
                               audio: Data(), selection: nil)
        precondition(VRAction(Int32(code)) != 0)
        updatePress(code: code, down: true, delay: 3600)
        precondition(VRDown(Int32(code)) == 1 && VRArmed() == 1)
        config = after
        _ = try! GoCore.request(config: after.raw, 
                               audio: Data(), selection: nil)
        precondition(VRAction(Int32(code)) == 0)
        if modifier {
            handleModifier(code: code, flags: [])
        } else {
            updatePress(code: code, down: false, delay: 0)
        }
        precondition(VRDown(Int32(code)) == 0 && VRArmed() == 0)
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
        let finishedToken = VREpochStart()
        cancelFromPill()
        precondition(VREpochValid(finishedToken) == 1) // Dismissing a result is not cancellation.
        precondition(item.button?.title == "VR")
        let key: Int32 = 61
        precondition(VRAction(key) == 2)
        let held = VRPress(key, 0)
        precondition(held != 0 && VRActivate(key, held) == 1)
        let processingToken = VREpochStart()
        taskID = processingToken
        precondition(!pending)
        VRAbandon() // as on timed recording completion
        precondition(pending && VRArmed() == 0)
        _ = VRRelease(key)
        cancelFromPill()
        precondition(!pending && taskID == 0 && VREpochValid(processingToken) == 0) // In-flight work still cancels.
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
    // Exercise the production Swift -> native Go Hold path without posting HID events.
    // A shortcut can be rebound while its old physical key is still down.
    let configData = try! Data(contentsOf: URL(fileURLWithPath: "macos/Headless/config.example.json"))
    let original = try! JSONSerialization.jsonObject(with: configData) as! [String: Any]
    var mouseBound = original
    mouseBound["dictation_hotkey"] = "Mouse4"
    func host(_ raw: [String: Any]) -> HostConfig {
        let data = try! JSONSerialization.data(withJSONObject: raw)
        let json = String(decoding: data, as: UTF8.self)
        let output = try! GoCore.request(config: json, 
                                         audio: Data(), selection: nil)
        return HostConfig(raw: json, profile: output.profile!)
    }
    let hostApp = HeadlessApp()
    hostApp.verifyReboundRelease(code: 103, before: host(mouseBound),
                                 after: host(original), modifier: false)
    hostApp.verifyReboundRelease(code: 54, before: host(original),
                                 after: host(mouseBound), modifier: true)
    hostApp.verifyPillCancelLifecycle()
    hostApp.verifyPressTargetSurvivesDelayedActivation()
    print("AX focus, selection, clipboard budget, shortcut rebind, pill cancel and press-target smoke passed")
}
