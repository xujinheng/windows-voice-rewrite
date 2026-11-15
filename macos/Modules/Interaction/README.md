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

A `SelectionSnapshot` is a single-use capability bound to a generation, foreground PID, AX focus-element identity (compared with `CFEqual`), original selected text, UTF-16 selection range, and monotonic focus/value revision. All replacement and insertion requests revalidate this evidence synchronously before modifying the original application. An edit requires an explicit edit intent; ordinary dictation requires an empty caret selection. Password, protected, and secure-input fields are refused before text is read.

### Cross-application insertion

For a target that permits setting `AXSelectedText`, the client uses the native AX API. When this attribute is not writable, the macOS adapter can stage a plain-text paste, but **only** if the focused element exposes a complete readable `AXValue` and the original selection exactly matches the captured UTF-16 range. A focused app/element or selection change prevents issuing the paste. The synthetic Cmd+V events are addressed to the original process and the client compares the resulting full `AXValue` against the calculated replacement, not against a clipboard-read receipt. A timeout or failed confirmation reports an uncertain result and never automatically retries, because the target may already have received the paste.

The temporary pasteboard transaction captures all representations of all items (up to a bounded total size) and refuses data it cannot restore. It includes an owner marker and checks `changeCount` before restoring. When another process changes the clipboard, its content is left alone. Temporary contents are tagged transient/concealed for cooperative clipboard managers; those tags are conventions, not a privacy guarantee. If the original clipboard has unrepresentable types or the target cannot be verified, no paste is attempted. Results remain visible in the macOS app with a manual copy button.

Cross-process event delivery and AX observations are not atomic. A focus change between the final check and a queued input event remains a platform risk. A successful paste must be verified with the full AX value; unsupported browser editors may therefore require manual copying. Physical-device acceptance is required before claiming per-application compatibility.

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
6. In a disposable TextEdit/plain-text harness, test a deliberately non-settable `AXSelectedText` target with readable `AXValue`: confirm paste writes at the original range and the clipboard is restored. Test change-count takeover, delayed paste, missing AXValue, unrepresentable clipboard formats, emoji offsets, and unconfirmed writes. Confirm that an uncertain outcome does not retry or overwrite clipboard changes.

### Remaining risks and boundaries

* AX target identity, notifications, and full-value support vary by app. Unsupported observation or unverifiable paste is denied; untested applications can remain unsupported. CI compilation does not establish real editor compatibility.
* macOS has no atomic compare-and-replace across the host and external application. AX notifications may be late and queued keyboard events may reach an app after focus changes. Guarded writes are best effort, not a guarantee against every external race. Strict zero-risk environments must disable cross-app writing.
* If a process crashes, the OS releases Carbon registrations; graceful host termination should explicitly call `shutdown`. Native app lifecycle and UI integration are outside this module.
