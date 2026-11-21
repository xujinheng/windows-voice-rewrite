# VoiceRewrite macOS: Go-first headless host

This is a **minimal menu-bar host**, not a second SwiftUI business application.
The thin Go/C bridge in `clients/macos/bridge/` is linked into the native executable as a C archive. Swift directly calls Go `voice.Core` (key holds, task generations, cancellation, transcription and routing) and `indicator`, shared with Windows. No helper process or per-frame IPC is needed.
Swift contains only Quartz keyboard/side-button input, AVAudioEngine capture,
Accessibility selection, focus-checked paste, a native NSStatusItem menu icon and
a non-activating AppKit recording pill (232×50, expanding to 252×50).

## Build and run

On macOS 13+ with Xcode command line tools and Go 1.23:

```sh
bash scripts/build-macos-headless.sh "$(uname -m)" "$PWD/dist" 0.1.0
open dist/VoiceRewrite.app
```

The first launch creates `~/.config/voicerewrite/config.json` containing `{}` if
it does not exist. Go Core applies the default model endpoints and macOS shortcuts;
existing or invalid config files are never replaced. ASR (and LLM for rewriting)
must still run separately; the default localhost URLs do not start those servers.
Use the menu-bar **Show config in Finder** to locate the file and **Reload config**
after edits. The optional first process argument specifies another config file path.

Hold **RightCommand** to dictate; hold **RightOption** over selected text to
speak a rewrite instruction. Both keys use a 200 ms arm delay; configured mouse
side buttons use 250 ms. Configure either shortcut as `Mouse4` or `Mouse5`.
Mouse button events assigned to voice shortcuts are intercepted to avoid
accidental browser navigation.

Grant Microphone and Accessibility permissions when prompted. The app uses a
modifying Quartz event tap for shortcuts; it does not explicitly request Input
Monitoring access. Verify shortcut permissions on the target macOS version.
The rewrite shortcut first snapshots available AX selected text when pressed,
using `AXSelectedText` or `AXSelectedTextRange` with `AXStringForRange`.
When the selection is inaccessible through AX, the app tries Cmd+C on release
and restores the original clipboard contents when its snapshot is supported and
the copied clipboard entry is still current. Browser page text, PDFs and other
noneditable selections can still be rewritten or explained in the result preview
with a Copy action; the app never pastes into an unverified editor. It skips the
clipboard fallback if it cannot back up the clipboard safely or the target
changed. The app does not retry an uncertain paste. `Sent ✓` means an input
event was sent, **not** that insertion was confirmed.

## Scope and validation

No settings GUI, Meeting or full history panel. Go Core supplies the
result text, title and `insert`/`replace`/`preview` route. macOS checks the
actual editor and attempts the native paste; if it cannot, the result remains
available in a small selectable AppKit preview with a **Copy result** button.
The menu-bar **Copy last result** uses the same Go Core history. Unlike Windows,
the preview has no **Apply to original** button. No second delivery reducer or
separate ABI protocol is needed.

CI compilation is not physical end-to-end acceptance. Validate both modifier
keys, Mouse4/Mouse5, microphone permissions, dictation and rewritten selections
in several actual editors with real ASR/LLM endpoints before merging.
