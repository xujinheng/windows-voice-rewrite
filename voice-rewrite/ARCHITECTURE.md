# Architecture

VoiceRewrite has native Windows (Go/Win32) and macOS (SwiftUI/AppKit with a bundled Go `voicebridge`) desktop hosts. Portable Go packages under `internal/core` contain shared processing, provider protocols, connection probing and task orchestration. Platform adapters own device access, UI, input, configuration files and OS credential storage.

## Portable core

| Package | Responsibility |
| --- | --- |
| `internal/core/audio` | Validate interleaved PCM16 formats and packet sizes; mix channels, resample to 16 kHz and assemble 20 ms mono frames; provide audio framing, voice segmentation, preview timing, transcript chunking, the shared 0.1s capture floor, a 0–1000 recording-indicator RMS level, and the `FrameSource` contract. |
| `internal/core/indicator` | Recording-pill phases, geometry, labels, opacity, waveform motion, progress-wheel beads, microphone-level gain, and the shared pill colors. Hosts paint the window and own the microphone. |
| `internal/core/input` | Map a physical side button to dictation, rewrite, or ignore, and track one button's press, hold, and release. Windows starts recording after 250 ms. macOS starts after 280 ms. Hosts own the event tap or mouse hook. |
| `internal/core/shortcut` | Canonical voice hotkeys: mouse side buttons, function keys, and Ctrl/Alt/Shift combinations. Bare typing keys and Pause/Break are rejected, and two voice actions cannot share a shortcut. Startup rejects a shared side button, keyboard dictation or a side button outside hold mode, and a voice hotkey that matches exit. macOS maps the key name to its own virtual key codes. macOS settings default to Ctrl+Alt+R, Ctrl+Alt+E, and Ctrl+Alt+T. A shortcut rejection sentence is shown unchanged; other shortcut failures keep the macOS prefix. Settings capture ignores modifier keys and rejects a held Windows key before parsing. macOS Accessibility and clipboard failures use the Interaction module's status sentences. Saving a pair trims both fields, validates rewrite, then dictation, and reports whether either stored value changed. |
| `internal/core/desktop` | Keep a window inside a monitor work area, including monitors with negative coordinates, and center one in that area. |
| `internal/core/popup` | Result-card size for a text length, the editor, note, close, copy, and apply frames, and which clicks dismiss the card instead of the text or its buttons. |
| `internal/core/settingsform` | Logical-pixel geometry for the settings window: stretching rows, the shared field, label, and shortcut rectangles, the minimum window size, footer anchors, scroll steps, and the navigation, probe, note, footer, hint, capture, and microphone boxes. Hosts scale them to the monitor DPI. |
| `internal/core/texttarget` | Win32 edit, rich edit, and WinForms edit classes, including password style, plus Chrome, Edge, Firefox, CEF, and Electron window classes. A renderer surface only authorizes a guarded paste probe. When a password probe cannot run, the whole browser or Electron window is treated as sensitive. |
| `internal/core/logfile` | Rotate a diagnostic log before it is opened: one active file up to 4 MiB and three numbered backups. Hosts choose the path and write the lines. macOS uses the same size, backup count, and rotation steps. |
| `internal/core/jsonl` | Voicebridge frame limits shared with the macOS client: protocol version 1, an 8 MiB frame, a 128-byte request ID, and 65536 retained request IDs. |
| `internal/core/session` | The active voice-task generation. Starting another generation cancels the previous context. Hosts still verify the editor target before writing. |
| `internal/core/history` | Current-session voice results in memory: at most 10 entries and 4 MiB, newest first, never a partial transcript. Menu text is the category and time only. Both hosts use the same empty, clear, and section titles. |
| `internal/core/update` | Compare the installed version with GitHub releases and produce the shared status text. Hosts display the result and open the releases page. The checker does not download installers. |
| `internal/core/clipboard` | Whether a captured clipboard may be restored, and how a synthetic copy retry spends its time budget. Hosts own the native pasteboard. |
| `internal/core/ux` | Fixed recovery text for known provider, ASR, and editor failures, the shared result-card titles, the copy and apply status lines, the refusal to save settings during a voice task, the tray and pause sentences, and the password-field refusals for dictation, selection, and rewrite. Unknown errors stay with the host so private request data is not echoed. Owned sentences, including an empty transcript, a short recording, a password refusal, and a selection that is too long, are shown unchanged on both hosts. An unclassified macOS bridge failure keeps its ASR or model-failure prefix. macOS keeps the same title, copy-status, save-refusal, pause, and password strings. Windows save success still says shortcuts and devices take effect after restart; macOS keeps its own saved sentence. |
| `internal/core/config` | Application settings schema and defaults, JSON parsing/serialization, normalization, API base-URL validation and credential-field transformation callbacks. Base URLs are trimmed http(s) addresses without userinfo, query, or fragment. Recording length is kept from 1 to 180 seconds and otherwise returned to 60. Meeting recognition is multipart or llama-prefix, listed in that settings order. API keys longer than 4000 bytes are rejected. A settings save keeps the current audio device unless the combo index is in range. No filesystem access or OS encryption implementation. |
| `internal/core/provider` | Safe HTTP clients, redirect blocking, bounded responses, model discovery, HTTP errors and reasoning-tag removal. |
| `internal/core/speech` | WAV/multipart ASR requests, transcript processing, chunking and request cancellation. |
| `internal/core/serviceprobe` | Shared ASR silent-WAV upload and LLM model-list connection probing for both desktop hosts, including the idle, running, and stale settings lines. |
| `internal/core/textedit` | Selected-text length/cancellation validation, spoken-command ASR-to-edit orchestration, intent classification and parsing, explicit-command write authorization, routing, plain-text rewrite and shared model completion transport. Both hosts refuse a selection over the rune limit with the same sentence before recording or sending it. A missing or non-positive limit uses the same 12000-rune default. |
| `internal/core/meeting` | Stream segmentation, adaptive ASR previews, session epochs/pause/resume/cancellation (`SessionRuntime`), bounded final/preview queues and revision-fenced caption translation (`Pipeline`), captions, replies, inference priorities, history and layout policies. Profile notes are limited to 4000 background runes and 2000 term runes before trimming. A reply refinement accepts one of the three suggestions and at most 2000 runes. Meeting button ids, actions, the next view flags, and the HUD status, button, reply, and history sentences live in this package. Toolbar widths, pane visibility, the reading and profile stacks, divider hit bands and marks, reading font faces, the transcript bottom check, and the space reserved for chrome come from the same layout rules. Streaming panels share one repaint interval. |

Core packages are built and tested without Win32, WASAPI, COM or native GUI
imports. The CI boundary check compares core source/test file sets and direct
platform API imports on Linux, Darwin and Windows. Linux runs race-enabled Go
tests; a real macOS runner executes core tests and compiles the core for Darwin
amd64 and arm64. Xcode CI also builds and tests the native macOS application on Apple Silicon and Intel runners. Hardware audio, Keychain and cross-app Accessibility permissions require physical-device verification.

## Native Windows integration

| Area | Implementation and ownership |
| --- | --- |
| Meeting audio | `meeting_capture_windows.go` owns WASAPI process-loopback activation, COM buffers, events and process lifetime. It validates native packets before passing PCM16 data to `audio.PCMConverter`. `meeting_windows.go` and `microphone_windows.go` retain host device integration. |
| Meeting coordinator | `meeting_session.go` constructs `meeting.Pipeline` and supplies inference, caption, metrics and HUD hooks. It retains host configuration snapshots, model scheduling, UI state and native presentation; `meeting_state.go` holds host view state. |
| Text selection and insertion | `task_worker_windows.go` invokes `textedit.ResolveSelectedTask` and `textedit.RouteTask`, and uses `session.Epoch` to cancel stale work. `clipboard_windows.go`, `uia_password_windows.go`, `unicode_input_windows.go`, `mouse_windows.go` and related Win32 code own foreground checks, clipboard restoration, guarded paste and input. Restoration is allowed only by `clipboard.MayRestore`. |
| Windows UI | `meeting_hud_windows.go`, `settings_windows.go`, `popup_windows.go`, `result_direct2d_windows.go`, `tray_windows.go` and `handy_status_windows.go` render native controls. The recording pill reads its phases and geometry from `internal/core/indicator`. |
| Configuration and credentials | `config.go` exposes the shared settings model to the host. `config_store.go` owns user paths and private/atomic file operations; `secrets_windows.go` encrypts/decrypts credentials with Windows DPAPI. `secrets_other.go` is not a macOS Keychain adapter. |

## Meeting data flow

```text
Windows process selection and WASAPI capture
    -> core/audio PCM16 validation, channel mixing and 16 kHz framing
    -> core/meeting stream segmentation and adaptive audio turns
    -> core/meeting SessionRuntime and Pipeline (epochs, ASR priority, revisions)
    -> host inference hooks using core/speech, core/meeting and core/provider
    -> core/meeting caption, translation and reply state
    -> Windows native Meeting HUD
```

Selected text and meeting transcripts are untrusted model input. The HTTP
client rejects cross-host redirects, and configuration validates the chosen
API endpoints. Text replacement requires an explicit editing command and
passes the native foreground/selection guard. Windows credentials are stored
using the current user's DPAPI protection.

## Native macOS integration

`macos/VoiceRewrite.xcodeproj` builds the SwiftUI/AppKit host and bundles native Go `voicebridge`. The Helper calls the same `internal/core` packages as Windows; `probe_asr` and `probe_llm` expose identical service diagnostics. `internal/contract` is the settings catalog: page titles, control labels, order, which controls each host shows, and the Windows command for each settings button. `cmd/contractgen` writes the macOS indicator math, result copy, failure-status rule, session history, result-card frames, selection limit, side-button hold, endpoint defaults, base-URL check, and settings form from that catalog and the Go core. Per-frame pill drawing stays in the host. CI runs `go run ./cmd/contractgen -check` and fails when a generated file differs. ASR and text-edit failures use `ux.Classify`, so macOS shows the same recovery text as Windows when the failure is in the shared catalog. The `selected_voice_task` operation runs the same spoken-selection ASR + textedit orchestration used by Windows; macOS captures the physical microphone and validates the original Accessibility selection before attempting a result write. `Microphone` captures audio, `VoiceRewriteStorage` owns private settings and Keychain credentials, and `VoiceRewriteInteraction` owns Carbon shortcuts and guarded Accessibility insertion. Swift handles the user interface, platform permissions and lifecycle. The macOS recording pill uses the same phases, labels, sizes and fade timing as Windows. AppKit owns the non-activating panel. The microphone starts only after the side-button hold, so a short navigation click does not open the input device.

macOS Meeting application-audio capture remains a separate integration requirement. Physical microphone, Keychain and Accessibility behavior plus distribution signing must be verified before public macOS release.

For setup see [README](README.md); for platform and manual checks see
[TESTING](TESTING.md) and [visual testing](VISUAL-TESTING.md).
