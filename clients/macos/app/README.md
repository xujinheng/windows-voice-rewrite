# VoiceRewrite macOS: Go-first headless host

This is a **minimal menu-bar host**, not a second SwiftUI business application.
The existing Go `cmd/voicebridge` is linked into the native executable as a C archive. Swift directly calls Go `voice.Core` (key holds, task generations, cancellation, transcription and routing) and `indicator`, shared with Windows. No helper process or per-frame IPC is needed.
Swift contains only Quartz keyboard/side-button input, AVAudioEngine capture,
Accessibility selection, focus-checked paste, a native NSStatusItem menu icon and
a non-activating AppKit recording pill (232×50, expanding to 252×50).

## Build and run

On macOS 13+ with Xcode command line tools and Go 1.23:

```sh
bash scripts/build-macos-headless.sh "$(uname -m)" "$PWD/dist" 0.1.0
mkdir -p ~/.config/voicerewrite
cp clients/macos/app/config.example.json ~/.config/voicerewrite/config.json
open dist/VoiceRewrite.app
```

ASR and LLM endpoints must be running. Loopback URLs in the example are only
placeholders, not bundled servers. The optional first process argument specifies
another config file path. Default: `~/.config/voicerewrite/config.json`.
Reload the file with the menu-bar **Reload config** action.

Hold **RightCommand** to dictate; hold **RightOption** over selected text to
speak a rewrite instruction. Both keys use a 200 ms arm delay; configured mouse
side buttons use 250 ms. Configure either shortcut as `Mouse4` or `Mouse5`.
Mouse button events assigned to voice shortcuts are intercepted to avoid
accidental browser navigation.

Grant microphone, Accessibility and Input Monitoring permissions when prompted.
The rewrite selection reader first uses `AXSelectedText`. If an editor does not expose it, the native adapter tries `AXSelectedTextRange` with `AXStringForRange`. Both paths read only the selected text and are rechecked before paste; unsupported editors still fail closed. No synthetic Cmd+C / clipboard fallback is used.
The app refuses unsafe rewrites (missing or changed selection, focus change),
never retries a potentially delivered paste, and only restores clipboard data if
the inserted text still owns the clipboard. `Sent ✓` means a paste event was
sent, **not** confirmed insertion.

## Scope and validation

No settings GUI, Meeting, history panel or result cards. This host reuses the
Go `host_profile`, `transcribe` and `selected_voice_task` operations via native C calls; it does not introduce
another Go task reducer or duplicate portable ASR/LLM logic.

CI compilation is not physical end-to-end acceptance. Validate both modifier
keys, Mouse4/Mouse5, microphone permissions, dictation and rewritten selections
in several actual editors with real ASR/LLM endpoints before merging.
