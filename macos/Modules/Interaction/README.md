# VoiceRewrite macOS Interaction

Standalone Swift Package, restricted to `macos/Modules/Interaction/**` (Issue #46).
Requires macOS 12+, a native Swift host application, and user-granted **Privacy & Security → Accessibility** permission for the host. Hotkeys use Carbon and do **not** require Accessibility; cross-app AX reads/writes do. `InteractionSession.accessibilityPermissionGranted(prompt: true)` explicitly requests the system prompt; callers should expose a permission-help UI.

## Integrating from a Swift App

Add the local Swift package at `macos/Modules/Interaction`, import `VoiceRewriteInteraction`, and retain the session, driver and registry for the life of the app:

```swift
import VoiceRewriteInteraction

let session = InteractionSession.macOS()
let driver = CarbonHotKeyDriver { shortcut, foregroundPID in
    guard let foregroundPID, foregroundPID > 0 else { return }
    // Start the task from the hotkey handler; never activate a VoiceRewrite window
    // before capturing the user's focused target.
    Task {
        do {
            let generation = try await session.beginTask(targetProcessID: foregroundPID, timeout: 60)
            let target = try await session.captureSelection(
                generation: generation, intent: .insertAtCaret)
            // Perform ASR/LLM off of the UI/AX path; retain both target and generation.
            // let asrText = try await recognizeSpeech()
            // try await session.insertIfCurrent(target, text: asrText, generation: generation)
        } catch {
            // Show an error, e.g. permission denied, stale snapshot, unsupported AX.
        }
    }
}
let hotkeys = HotKeyRegistry(driver: driver)
try hotkeys.register(.init(keyCode: 15, modifiers: [.command, .shift]))
// On cancel: await session.cancelTask(generation: generation)
// On app exit: hotkeys.shutdown(); driver.shutdown(); await session.shutdown()
```

**Replacement** is a different, explicit user action: only capture `intent: .replaceSelection` if the user invoked the replace-selection command, and call `replaceIfCurrent(snapshot,with:generation:)` only for that snapshot. Ordinary ASR insertion requires a zero-length caret selection; selected text cannot be replaced by the insertion route. No Go intent flag (including `may_attempt_replace`) is sufficient for native authorization.

`SelectionSnapshot` is a one-use capability bound to an actor-issued generation token, the foreground PID captured at the hotkey event, the actual AX focus handle (compared using `CFEqual`), original selected text, UTF-16 character range and a monotonic AX/activation revision. The native adapter re-reads and rechecks the same target **immediately before** `AXUIElementSetAttributeValue(kAXSelectedTextAttribute)`. It does not synthesize keystrokes and does not send Cmd+V; unsupported editors return `unsupportedNativeWrite` or `monitoringUnavailable`, not a risky fallback. Password/secure-input fields are refused before reading text. Timeout, cancellation, a newer task, or app/selection/focus drift invalidates authorization. AX operations are synchronous within the dedicated actor, and there is no network wait in the AX path.

## Clipboard policy

The production editing path never accesses NSPasteboard. `ClipboardProtector` / `MacClipboardBoard` are optional **staging-only** primitives, with typed snapshots, an owner UUID marker, a change-count check, conditional restoration and explicit error reporting. They do not perform a paste; the module deliberately has **no clipboard-based text-insertion fallback**. Data that cannot be safely copied (such as promised or oversized types) causes snapshot refusal. If clipboard ownership changes, restoration refuses rather than overwriting a user's clipboard. Some clipboard formats cannot be reproduced perfectly, so these helpers are **not** a guarantee of lossless persistence and must not be used as a reason to enable synthetic paste.

## Tests

```sh
cd macos/Modules/Interaction
swift test
```

Shared authorization, lifecycle, registry, and clipboard policy tests also run on Linux. **These mocks are not macOS device acceptance evidence.** The current development environment is Linux and cannot compile the macOS `AppKit` / `ApplicationServices` / Carbon portions or test actual AX writes.

## Real macOS acceptance checklist (not yet executed)

1. Build with Xcode's Swift toolchain (`swift test`) and instantiate the package from a real native host. Grant Accessibility in System Settings, then restart the host if macOS requires it.
2. Launch TextEdit in a plain-text document; focus an empty caret, trigger the configured global shortcut while TextEdit remains frontmost, capture an insertion snapshot, and verify an ASR string is inserted *only* at that caret using AX. Repeat with selected text and explicit replace-selection intent.
3. Start a task, then change selected text, caret position, focus within TextEdit, or activate another app before delivering the delayed result. Verify no text is changed. Repeat with an ABA change (switch away and back), cancellation, timeout and overlapping generations.
4. Focus a system password/secure-text field; verify `secureField` and no text access/write. Revoke Accessibility; verify `accessibilityPermissionDenied` and an actionable permission prompt path.
5. Register the same shortcut twice and try a known system-reserved shortcut; confirm duplicate/conflict errors. Unregister/re-register; close the host and confirm no shortcut remains registered.
6. Exercise `MacClipboardBoard` only in a disposable test harness: stage, restore, simulate clipboard takeover and failed restoration, and confirm no Cmd+V ever occurs.

### Remaining risks and boundaries

* AX target identity/change notifications vary across apps and versions. Unsupported observation/selected-text write is denied; untested applications can remain unsupported. In particular, TextEdit success is **not claimed** without macOS evidence.
* macOS does not provide an atomic “compare focus + selection and replace” operation spanning AX and the remote app. Even with live AX notifications and a final native recheck, a narrow race can occur between the final check and the AX write, and AX notifications may be delayed. This package therefore offers best-effort guarded native writes, **not a mathematical guarantee** against all external focus/selection races. A strict zero-risk policy should disable edits where an atomic editor API is unavailable.
* If a process crashes, the OS releases Carbon registrations; graceful host termination should explicitly call `shutdown`. Native app lifecycle and UI integration are outside this module.
