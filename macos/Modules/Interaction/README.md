# VoiceRewrite macOS Interaction

Standalone Swift Package for the native macOS host.
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
                generation: generation, intent: .replaceSelection)
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

**Ordinary dictation** uses `MacDictationDelivery`, which captures the foreground application's process ID (and an optional AX focus identity). It does not require Accessibility text attributes, selection ranges or editor notifications. The host stores the target until transcription finishes, then performs one native paste or, before any paste is attempted, a direct Unicode input fallback when the clipboard cannot be preserved.

**Selected-text editing** uses `InteractionSession` with `intent: .replaceSelection`. It requires a stable and readable AX selection, rejects stale focus, and validates the original full value before rewriting. Go intent flags are not native write authorization.

### Cross-application insertion

Ordinary dictation posts a single Cmd+V with the original application still active. AX is optional and only assists with detecting focused-field changes and protected inputs. Without AX the host cannot guarantee that the original text field remains focused inside the same application. The user chooses this compatibility-oriented behavior. There is no automatic retry after a paste event.

The native pasteboard provides text lazily and records a receipt when the destination requests it. Restoration is delayed until reads have been quiet for 200ms, or a bounded 8-second timeout. The original clipboard is restored only while the temporary lease is still owned. A new dictation arriving during another paste lease can use direct Unicode input instead.

The temporary pasteboard transaction captures all representations of all items (up to a bounded total size) and refuses data it cannot restore. It includes an owner marker and checks `changeCount` before restoring. When another process changes the clipboard, its content is left alone. Temporary contents are tagged transient/concealed for cooperative clipboard managers; those tags are conventions, not a privacy guarantee. If the original clipboard has unrepresentable types or the target cannot be verified, no paste is attempted. Results remain visible in the macOS app with a manual copy button.

Cross-process event delivery and AX observations are not atomic. A focus change between the final check and a queued input event remains a platform risk. Dictation paste delivery is best effort; unsupported AX targeting or unsafe clipboard snapshots may still require manual copying. Explicit rewriting requires full AX confirmation. Physical-device acceptance is required before claiming per-application compatibility.

## Tests

```sh
cd macos/Modules/Interaction
swift test
```

Platform-neutral authorization, lifecycle, clipboard and text-verification tests run on Linux. macOS CI builds the AppKit / Accessibility / Carbon adapters on Apple Silicon and Intel. Neither test runner supplies physical-mouse or real external-editor acceptance evidence.

## Physical macOS acceptance checklist

1. Build with Xcode's Swift toolchain (`swift test`) and instantiate the package from a real native host. Grant Accessibility in System Settings, then restart the host if macOS requires it.
2. Launch TextEdit in a plain-text document; test ordinary dictation at an empty caret and over a preselected phrase. Then test selected-text rewrite as a separate explicit command.
3. Start a task, then change selected text, caret position, focus within TextEdit, or activate another app before delivering the delayed result. Verify no text is changed. Repeat with an ABA change (switch away and back), cancellation, timeout and overlapping generations.
4. Focus a system password/secure-text field; verify `secureField` and no text access/write. Revoke Accessibility; verify `accessibilityPermissionDenied` and an actionable permission prompt path.
5. Register the same shortcut twice and try a known system-reserved shortcut; confirm duplicate/conflict errors. Unregister/re-register; close the host and confirm no shortcut remains registered.
6. In a disposable plain-text editor that exposes a non-settable `AXSelectedText` but readable `AXValue`, confirm paste writes at the original range and the clipboard is restored. Test change-count takeover, delayed paste, missing AXValue, unrepresentable clipboard formats, emoji offsets, and unconfirmed writes. Confirm that an uncertain outcome does not retry or overwrite clipboard changes.

### Remaining risks and boundaries

* AX target identity, notifications, and full-value support vary by app. Unsupported observation or unverifiable paste is denied; untested applications can remain unsupported. CI compilation does not establish real editor compatibility.
* macOS has no atomic compare-and-replace across the host and external application. AX notifications may be late and queued keyboard events may reach an app after focus changes. Guarded writes are best effort, not a guarantee against every external race. Strict zero-risk environments must disable cross-app writing.
* If a process crashes, the OS releases Carbon registrations; graceful host termination should explicitly call `shutdown`. Native app lifecycle and UI integration are outside this module.
