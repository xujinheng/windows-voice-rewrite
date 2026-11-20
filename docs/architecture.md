# VoiceRewrite architecture

VoiceRewrite is **one importable Go Core (`github.com/xujinheng/windows-voice-rewrite/core/voice`) with native desktop adapters**. Windows
owns the established Win32 feature set. macOS is a deliberately small headless
status-bar app for dictation and selected-text rewrite, not a second application
stack. `oneclick/` is an independent standalone ASR-server bootstrapper with its
own Go module; it must not be coupled to the desktop host just for reuse.

## Runtime boundaries

Windows (`clients/windows/*.go` and `*_windows.go`) owns Win32 messages, UIA,
WASAPI/WinMM, hooks, clipboard handles, native windows and Meeting HUD pixels.
Keep physical side effects and their thread/OS lifetime here; shared portable
decisions belong to the public `voice` and `meeting` Go packages and private dependencies.

macOS (`clients/macos/app/main.swift`, `RecordingPill.swift`) owns Quartz events,
AVAudioEngine/AVAudioConverter, AX focus/selection, NSPasteboard leases,
AppKit panel and status-menu drawing. It links Go `cmd/voicebridge` as a Darwin
**C archive in the same process**, exposing a small explicit C ABI through
`VoiceCore.h`. It does not start a Go helper, exchange audio-frame IPC, or
implement a second voice-policy state machine.

## One public lifecycle contract

All clients import the same Go Core and submit `voice.Command` values to
`Core.Apply`. The returned `voice.Event` is a typed command receipt, not
a separate host decision. `Core.Snapshot(physicalCode, generation)` returns
one atomic `voice.State` view (bound role, frozen held role, hold/activation,
task generation validity, phase and shutdown). This replaces the old public
`Press`, `Release`, `Activate`, `Start`, `Cancel`, `Role`, `HeldRole`,
`Down`, `Valid` and related getters; do not add forwarding aliases.

`Core.Execute` consumes OS-captured PCM/selection and produces the existing
`voice.Result` route. Its optional callback emits the **same**
`voice.Event` vocabulary for transcription, completion and failure. Clients
own capture, editor revalidation and OS side effects, not business event
classification. The macOS in-process bridge exports only `VRCommand` and
`VRSnapshot` for lifecycle input/state, and `VRRun` for task inference;
the Windows adapter invokes the same Go methods directly. CLI exercises
those public methods in an independent Go module.

On macOS, physical shortcut events, task generations, cancellation and voice
execution all call Go `voice.Core` (including task-role snapshots across shortcut rebinds); each waveform/spinner/clock frame calls Go
`indicator` through the in-process ABI. Swift performs the OS
operations and draws the returned pixels. The native macOS C ABI and the single Windows recording worker call `voice.Core.Execute` directly,
which owns PCM admission, `speech.Transcribe`, `textedit.ResolveSelectedTask`
and result routing. The Go task owns the shared transcription and spoken-task deadlines.
`cmd/voicebridge` is a C archive, not a JSONL server or helper process.

**Delivery is not equivalent to text insertion.** Native hosts verify current
window/selection and clipboard ownership before attempting one paste; a sent
input event does not prove editor acceptance, and uncertain pastes are never
automatically retried. Only the OS adapter can perform these final checks.

## Package ownership

- `config`: public, durable configuration and validation, importable alongside `voice` from `github.com/xujinheng/windows-voice-rewrite/core`.
- `internal/core/audio`: PCM and source framing, Meeting segmentation.
- `internal/core/provider`: model discovery and private HTTP transport.
- `internal/core/speech`: ASR request/response and transcript processing.
- `internal/core/textedit`: selected-text intent and model rewriting.
- `voice`: public client-facing Go Core package; owns holds, task generations, cancellation, results and voice execution.
- `internal/core/indicator`, `ux`, `clipboard`: presentation decisions,
  delivery policies and clipboard guards. Native hosts own the system APIs.
- `meeting`: public Go Meeting engine, independent of any native desktop host. Its
  `SessionRuntime` owns pause/epochs; `Pipeline` owns ASR/translation queues;
  `Scheduler` owns single-GPU inference priority; `ReplyState` owns
  displayed/pending reply generation. They are distinct concerns, not four
  competing authorities for the same voice task.
- `internal/contract`, `settingsform`, `popup`, `shortcut`: shared native
  UI commands, geometry and shortcut rules; no parallel settings app on macOS.

## Verification and maintenance

Before changing behavior, follow its Windows production entrypoint through
the Go owner and into the native OS adapter. Remove superseded decisions and
dead state along with the change; **do not create standalone state holders
that are only called by their own tests**. Do not split a native file merely
because it is long if doing so scatters the same platform lifecycle.

The CI gate runs Go unit/race tests, Windows builds/UI and native Swift + Go
C-archive builds for both macOS architectures. The macOS ABI smoke test calls
the linked Go exports and benchmarks their direct overhead.
`scripts/check_headless_scope.py` audits only compiled linked Go sources and
the two Swift host files; source ratios are not runtime-coverage or UX
measurements. Physical microphone, mouse side buttons, Accessibility,
clipboard ownership, network performance and third-party editor insertion
still require real-device acceptance.
