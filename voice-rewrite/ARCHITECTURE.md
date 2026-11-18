# VoiceRewrite architecture — Go-first host boundaries

The shared Go Core is authoritative for JSON config normalization, ASR, LLM,
spoken selected-text rewriting, error classification and result routing.
Windows is the existing Win32 host. macOS is a small headless `NSStatusItem`
host whose native Swift files are `macos/Headless/main.swift` (native
input/audio/AX/paste) and `macos/Headless/RecordingPill.swift` (floating
non-activating status panel).

## macOS production path

1. Quartz tap identifies physical RightCommand/RightOption or Mouse4/Mouse5.
   A 200/250-ms hold threshold avoids ordinary modifier taps.
2. AVAudioEngine captures microphone audio while the hold is active and
   AVAudioConverter produces 16-kHz mono PCM16 after recording stops.
3. Swift launches the existing bundled Go `cmd/voicebridge` executable and
   sends one JSONL request (`transcribe` or `selected_voice_task`) with the
   configured JSON fields. No synchronized per-audio-frame IPC.
4. Go Core owns the provider request, instruction parsing, response and route.
5. macOS displays the original 232×50 waveform/clock pill on capture and a
252×50 processing/result pill without activating the foreground editor. Its
cancel action stops native capture or invalidates a pending result generation.
macOS checks foreground target/selected-text freshness, sends at most one
   native paste, and conditionally restores its pasteboard lease. A sent event
   is not a confirmed editor write. No automatic retry on uncertainty.

There is **no macOS copy of the ASR/LLM decision logic** and no second Swift
session coordinator. Native capture lifetime and input permissions necessarily
remain in the macOS adapter; the Go bridge owns its request timeout.

## Shared Go packages

`internal/core/config`: config parsing/normalization.
`internal/core/audio`: PCM validation and framing.
`internal/core/speech`: transcription and ASR providers.
`internal/core/textedit`: selected-text instruction/model integration.
`internal/core/ux`: result classification and routing.
`cmd/voicebridge`: JSONL boundary used by macOS.
Windows uses the Go Core directly from its Win32 main package.

## Source ownership and tests

`scripts/check_headless_scope.py` counts only production Go source included
by `go list -deps ./cmd/voicebridge` and the two compiled Swift host files; standard library,
Go tooling, test source, Windows-only host and inactive files do not count.
A >=90% linked-source Go ratio is a **source metric**, not an assertion that
90% of dynamic instructions, features or decision points run in Go.

CI compiles both Apple Silicon and Intel .app bundles and continues to run Go
race tests and Windows host checks. Physical microphone, editor integration,
AX permission, side-button hardware and actual ASR/LLM accuracy require
explicit real-device testing; CI compilation cannot prove those operations.

All old macOS SwiftUI/Meeting/settings/visual parity checks were intentionally
retired after the product scope was changed to this headless version. Git history
retains the previous implementation.
