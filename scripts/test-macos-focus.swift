// This is appended to the host source for the native CI-only smoke binary.
// It is never linked into the production app.
MainActor.assumeIsolated {
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
    // Dictation can retain whatever AX identity was actually available.
    let partial = FocusIdentity(window: window, element: nil, title: "Original tab")
    precondition(partial.matches(window: window, element: nil, title: "Original tab"))
    precondition(!partial.matches(window: otherWindow, element: nil, title: "Original tab"))
    let noAX = FocusIdentity(window: nil, element: nil, title: nil)
    precondition(noAX.matches(window: nil, element: nil, title: nil))
    precondition(noAX.matches(window: nil, element: nil, title: "Other"))
    // Construct without posting: UTF-16, surrogate pairs and combining marks
    // must survive the native fallback, including both key-down and key-up.
    let sample = "a中🙂e\u{301}"
    guard let events = unicodeKeyEvents(sample) else { preconditionFailure("Unicode event creation") }
    precondition(events.count == sample.count * 2)
    for (index, character) in sample.enumerated() {
        var size = 0
        var buffer = [UniChar](repeating: 0, count: 20)
        buffer.withUnsafeMutableBufferPointer {
            events[index * 2].keyboardGetUnicodeString(maxStringLength: $0.count,
                actualStringLength: &size, unicodeString: $0.baseAddress)
        }
        precondition(String(decoding: buffer.prefix(size), as: UTF16.self) == String(character))
    }
    precondition(unicodeKeyEvents(String(repeating: "\u{0301}", count: 21)) == nil)
    // Exercise the production Swift -> native Go Hold path without posting HID events.
    // A shortcut can be rebound while its old physical key is still down.
    let configData = try! Data(contentsOf: URL(fileURLWithPath: "macos/Headless/config.example.json"))
    let original = try! JSONSerialization.jsonObject(with: configData) as! [String: Any]
    var mouseBound = original
    mouseBound["dictation_hotkey"] = "Mouse4"
    func host(_ raw: [String: Any]) -> HostConfig {
        let profile = try! GoCore.request(config: raw, operation: "host_profile",
                                          audio: Data(), selection: nil).profile!
        return HostConfig(raw: raw, profile: profile)
    }
    let hostApp = HeadlessApp()
    hostApp.config = host(mouseBound)
    hostApp.updatePress(code: 103, name: "Mouse4", down: true, delay: 3600)
    precondition(VRDown(103) == 1 && VRArmed() == 1)
    hostApp.config = host(original) // Simulate a successful reload before mouse-up.
    precondition(VRDown(103) == 1) // The original press is still owned by Go.
    hostApp.updatePress(code: 103, name: "Mouse4", down: false, delay: 0)
    precondition(VRDown(103) == 0 && VRArmed() == 0)

    hostApp.updatePress(code: 54, name: "RightCommand", down: true, delay: 3600)
    precondition(VRDown(54) == 1 && VRArmed() == 1)
    hostApp.config = host(mouseBound)
    hostApp.handleModifier(code: 54, flags: [])
    precondition(VRDown(54) == 0 && VRArmed() == 0)
    print("AX focus, selection, Unicode and shortcut rebind smoke passed")
}
