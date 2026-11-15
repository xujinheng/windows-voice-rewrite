# Architecture

VoiceRewrite runs a native Go/Win32 Windows application. Its reusable algorithms
are organized as **platform-independent Go packages**, and Windows-specific
capture, GUI, input, clipboard and encryption stay in the Windows shell.

## Portable core

| Package | Owns | Dependencies |
| --- | --- | --- |
| `internal/core/audio` | 16 kHz PCM framing, voice segmentation, preview timing, stereo-to-mono resampling, chunking and transcript joining; `FrameSource` contract | Go standard library |
| `internal/core/config` | Shared application settings schema and defaults; no persistence or credentials access | Go standard library |
| `internal/core/speech` | WAV serialization, multipart ASR uploads, transcript cleaning, chunking and cancellation | `core/audio`, `core/config`, `core/provider` |
| `internal/core/textedit` | Selected-text rewrite requests and response validation | `core/config`, `core/provider` |
| `internal/core/meeting` | Streaming meeting ASR, completion protocol and prompts, caption/English interpretation, versioned caption state and reading projections, cross-platform audio stream segmentation and adaptive previews, history follow/unread policy, pane sizing/preferences and inference scheduler with priority preemption | `core/config`, `core/provider`, `core/speech` |
| `internal/core/provider` | HTTP timeouts, redirect blocking, safe errors, URL validation, model discovery and reasoning-token stripping | `core/config` |

The core packages are tested as independent packages under Linux, Windows
and macOS CI. No core package imports Win32, WASAPI, COM, user32 or a GUI API.
macOS CI also compiles the core for Darwin amd64 and arm64.

## Platform integration

| Layer | Current source | Responsibility |
| --- | --- | --- |
| Native audio adapter | `meeting_capture_windows.go`, `meeting_windows.go`, `microphone_windows.go` | WASAPI process loopback and microphone capture; expose normalized PCM frames through `audio.FrameSource` |
| Native input/output | `mouse_windows.go`, `recording_windows.go`, `clipboard_windows.go`, `unicode_input_windows.go`, `uia_password_windows.go` | Global keys, selection, secure text insertion and clipboard |
| Native UI | `meeting_hud_windows.go`, `settings_windows.go`, `popup_windows.go`, `tray_windows.go` | Win32 event loop, layout presentation and control dispatch |
| Application orchestration | `meeting_session.go`, `meeting_state.go`, `intent.go` | Session lifecycle, native HUD view records and text-edit coordination; maps portable caption state, audio and inference events into Win32 controls |
| Configuration/credentials | `config.go`, `config_store.go`, `secrets_windows.go` | Platform storage paths, credential persistence and Windows DPAPI; settings data type lives in `core/config` |

The native application session lifecycle and HUD view data remain in `package main`; the ASR, text-edit and Meeting model clients are reusable core packages. The Meeting session receives configuration persistence as a callback supplied by its host application. A macOS application can use the shared services directly with its own audio source, GUI, input and secrets adapter. Reusing the entire Meeting session lifecycle on macOS still requires extracting the orchestration and HUD view state from `package main`.

## Data flow

```text
Win32 meeting process selector
    -> Windows WASAPI FrameSource adapter
    -> core/audio (PCM conversion)
    -> core/meeting (audio stream segmentation, adaptive snapshots)
    -> meeting_session (session lifecycle, caption queues and HUD state)
    -> core/speech + core/meeting (ASR, translation, English expression)
    -> core/provider (model discovery, safe HTTP client)
    -> core/meeting (caption revision, reading panels, English, scheduling, layout policies)
    -> Windows Meeting HUD (native EDIT controls and drag dividers)
```

The original application enforces its editor/selection target and task
generation before text insertion. Text-selection content and meeting transcripts
are untrusted input to the configured model services. Redirects to unconfigured
model hosts are rejected. API keys are protected with Windows DPAPI.

For local setup see [README](README.md); for platform testing see
[TESTING](TESTING.md) and [visual testing](VISUAL-TESTING.md).
