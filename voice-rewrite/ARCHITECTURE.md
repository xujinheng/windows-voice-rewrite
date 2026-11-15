# Architecture

VoiceRewrite runs a native Go/Win32 Windows application. Its reusable algorithms
are organized as **platform-independent Go packages**, and Windows-specific
capture, GUI, input, clipboard and encryption stay in the Windows shell.

## Portable core

| Package | Owns | Dependencies |
| --- | --- | --- |
| `internal/core/audio` | 16 kHz PCM framing, voice segmentation, preview timing, resampling from stereo capture, chunking and transcript joining; `FrameSource` contract | Go standard library |
| `internal/core/meeting` | Subtitle and spoken-English policies, history follow/unread behavior, minimum-size pane layout, draggable divider math, JSON layout preferences, inference-priority scheduler | Go standard library |
| `internal/core/provider` | HTTP transport with timeouts, redirect blocking, endpoint construction, safe HTTP errors and API base URL validation | Go standard library |

The core packages are tested as independent packages under Linux, Windows
and macOS CI. No core package imports Win32, WASAPI, COM, user32 or a GUI API.
macOS CI also compiles the core for Darwin amd64 and arm64.

## Platform integration

| Layer | Current source | Responsibility |
| --- | --- | --- |
| Native audio adapter | `meeting_capture_windows.go`, `meeting_windows.go`, `microphone_windows.go` | WASAPI process loopback and microphone capture; expose normalized PCM frames through `audio.FrameSource` |
| Native input/output | `mouse_windows.go`, `recording_windows.go`, `clipboard_windows.go`, `unicode_input_windows.go`, `uia_password_windows.go` | Global keys, selection, secure text insertion and clipboard |
| Native UI | `meeting_hud_windows.go`, `settings_windows.go`, `popup_windows.go`, `tray_windows.go` | Win32 event loop, layout presentation and control dispatch |
| Application orchestration | `meeting_session.go`, `meeting_state.go`, `meeting_model.go`, `meeting_asr.go`, `asr.go`, `llm.go` | Meeting state, model requests and app lifecycle; consumes portable core packages |
| Configuration/credentials | `config.go`, `config_store.go`, `secrets_windows.go` | User configuration, platform-native secret protection and file paths |

The remaining app orchestration resides in `package main` and is not yet an
independent reusable package. A future macOS shell needs its own GUI, input,
audio capture and credential implementation, and should extract the remaining
orchestration behind narrow service interfaces rather than import the Windows
executable package.

## Data flow

```text
Win32 meeting process selector
    -> Windows WASAPI FrameSource adapter
    -> core/audio (PCM conversion and voice segmentation)
    -> meeting_session (capture scheduling, ASR and translation calls)
    -> core/provider (HTTP client safety)
    -> core/meeting (subtitle, English, scheduling, layout policies)
    -> Windows Meeting HUD (native EDIT controls and drag dividers)
```

The original application enforces its editor/selection target and task
generation before text insertion. Text-selection content and meeting transcripts
are untrusted input to the configured model services. Redirects to unconfigured
model hosts are rejected. API keys are protected with Windows DPAPI.

For local setup see [README](README.md); for platform testing see
[TESTING](TESTING.md) and [visual testing](VISUAL-TESTING.md).
