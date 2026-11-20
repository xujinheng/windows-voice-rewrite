# VoiceRewrite architecture

VoiceRewrite is **one Go business core with two native desktop hosts**. Windows
owns the established Win32 feature set. macOS is a deliberately small headless
status-bar app for dictation and selected-text rewrite, not a second application
stack. `oneclick/` is an independent standalone ASR-server bootstrapper with its
own Go module; it must not be coupled to the desktop host just for reuse.

## Runtime boundaries

Windows (`voice-rewrite/*.go` and `*_windows.go`) owns Win32 messages, UIA,
WASAPI/WinMM, hooks, clipboard handles, native windows and Meeting HUD pixels.
Keep physical side effects and their thread/OS lifetime here; shared portable
decisions belong under `internal/core`.

macOS (`macos/Headless/main.swift`, `RecordingPill.swift`) owns Quartz events,
AVAudioEngine/AVAudioConverter, AX focus/selection, NSPasteboard leases,
AppKit panel and status-menu drawing. It links Go `cmd/voicebridge` as a Darwin
**C archive in the same process**, exposing a small explicit C ABI through
`VoiceCore.h`. It does not start a Go helper, exchange audio-frame IPC, or
implement a second voice-policy state machine.

On macOS, physical shortcut events call Go `input.Hold`, task generations and
provider cancellation call Go `session.Epoch`, and each waveform/spinner/clock
frame calls Go `indicator` through the in-process ABI. Swift performs the OS
operations and draws the returned pixels. The macOS bridge and the single Windows recording worker call the same `voice.Run`,
which owns PCM admission, `speech.Transcribe`, `textedit.ResolveSelectedTask`
and result routing. The Go task owns the shared transcription and spoken-task deadlines.
`cmd/voicebridge` is a C archive, not a JSONL server or helper process.

**Delivery is not equivalent to text insertion.** Native hosts verify current
window/selection and clipboard ownership before attempting one paste; a sent
input event does not prove editor acceptance, and uncertain pastes are never
automatically retried. Only the OS adapter can perform these final checks.

## Package ownership

- `internal/core/config`: durable configuration and validation.
- `internal/core/audio`: PCM and source framing, Meeting segmentation.
- `internal/core/provider`: model discovery and private HTTP transport.
- `internal/core/speech`: ASR request/response and transcript processing.
- `internal/core/textedit`: selected-text intent and model rewriting.
- `internal/core/input`, `session`: hold lifecycle and cancellable task epoch.
- `internal/core/indicator`, `ux`, `clipboard`: presentation decisions,
  delivery policies and clipboard guards. Native hosts own the system APIs.
- `internal/core/meeting`: Windows-only Meeting business logic. Its
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
