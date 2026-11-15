# Architecture

VoiceRewrite's runnable desktop application is a native Go/Win32 Windows application.
Portable Go packages under `internal/core` contain reusable processing, model
protocols and task orchestration. Platform adapters own device access, UI,
input, configuration files and OS credential storage. There is no native
macOS application in this repository.

## Portable core

| Package | Responsibility |
| --- | --- |
| `internal/core/audio` | Validate interleaved PCM16 formats and packet sizes; mix channels, resample to 16 kHz and assemble 20 ms mono frames; provide audio framing, voice segmentation, preview timing, transcript chunking and the `FrameSource` contract. |
| `internal/core/config` | Application settings schema and defaults, JSON parsing/serialization, normalization, API base-URL validation and credential-field transformation callbacks. No filesystem access or OS encryption implementation. |
| `internal/core/provider` | Safe HTTP clients, redirect blocking, bounded responses, model discovery, HTTP errors and reasoning-tag removal. |
| `internal/core/speech` | WAV/multipart ASR requests, transcript processing, chunking and request cancellation. |
| `internal/core/textedit` | Selected-text length/cancellation validation, intent classification and parsing, explicit-command write authorization, routing, plain-text rewrite and shared model completion transport. |
| `internal/core/meeting` | Stream segmentation, adaptive ASR previews, session epochs/pause/resume/cancellation (`SessionRuntime`), bounded final/preview queues and revision-fenced caption translation (`Pipeline`), captions, replies, inference priorities, history and layout policies. |

Core packages are built and tested without Win32, WASAPI, COM or native GUI
imports. The CI boundary check compares core source/test file sets and direct
platform API imports on Linux, Darwin and Windows. Linux runs race-enabled Go
tests; a real macOS runner executes core tests and compiles the core for Darwin
amd64 and arm64. These checks establish portable core compatibility, not a
working macOS desktop application.

## Native Windows integration

| Area | Implementation and ownership |
| --- | --- |
| Meeting audio | `meeting_capture_windows.go` owns WASAPI process-loopback activation, COM buffers, events and process lifetime. It validates native packets before passing PCM16 data to `audio.PCMConverter`. `meeting_windows.go` and `microphone_windows.go` retain host device integration. |
| Meeting coordinator | `meeting_session.go` constructs `meeting.Pipeline` and supplies inference, caption, metrics and HUD hooks. It retains host configuration snapshots, model scheduling, UI state and native presentation; `meeting_state.go` holds host view state. |
| Text selection and insertion | `task_worker_windows.go` invokes `textedit.ResolveSelectedTask` and `textedit.RouteTask`. `clipboard_windows.go`, `uia_password_windows.go`, `unicode_input_windows.go`, `mouse_windows.go` and related Win32 code own foreground checks, clipboard restoration, guarded paste and input. |
| Windows UI | `meeting_hud_windows.go`, `settings_windows.go`, `popup_windows.go`, `result_direct2d_windows.go`, `tray_windows.go` and their supporting Win32/Direct2D code render and dispatch native controls. |
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

A macOS host requires native audio/process capture, microphone, global input,
Accessibility-based text selection and insertion, clipboard, Keychain,
menu-bar/window UI and application lifecycle integrations. It must validate
permissions, device behavior, native UI E2E and distribution signing on macOS
before it can be considered a native application.

For setup see [README](README.md); for platform and manual checks see
[TESTING](TESTING.md) and [visual testing](VISUAL-TESTING.md).
