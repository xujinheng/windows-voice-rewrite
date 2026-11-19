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
    print("AX focus and selection identity smoke passed")
}
