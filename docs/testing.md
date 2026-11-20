# Testing VoiceRewrite

## Portable Go Core and Windows host
```sh
cd voice-rewrite
go test -race -count=1 ./...
```
The main CI continues Windows executable compilation, Windows UI capture and
Direct2D regression checks. Do not remove or suppress failing Windows tests.

## Interactive acceptance checklist

Use disposable text and a Windows 11 desktop with ASR and LLM services configured.

- [ ] Fresh launch: configure both API endpoints, microphone and shortcut; save and reopen Settings.
- [ ] Short-click Mouse4/Mouse5 in Chrome and Edge: Back/Forward works once, including rapid clicks; no recording starts.
- [ ] Hold Mouse4/Mouse5 longer than 250 ms: speaking immediately on pressing the button includes the first words; releasing finishes the recording and browser navigation does not occur.
- [ ] Short clicks discard provisional audio: no transcription request, saved history entry or recording indicator appears.
- [ ] Release just before or after 250 ms; test simultaneous presses, pause/resume and exit while held.
- [ ] Mouse4: dictate into Notepad, VS Code and a browser text field; release to insert.
- [ ] Mouse5: select text and request a rewrite or translation; confirm only the intended selection changes.
- [ ] Mouse5: select read-only text and ask a question; confirm the answer card opens without modifying the source.
- [ ] Result card: copy the full answer, scroll long text, close with the button, title bar and Escape.
- [ ] Switch editor or selection while an API request is pending; stale output must not replace newer text.
- [ ] Cancel an active task; confirm network work stops and nothing is inserted.
- [ ] Test clipboard contents containing text, HTML, rich text, file lists and unsupported formats.
- [ ] Test native password controls and browser password fields; confirm input is blocked where detected.
- [ ] Try connection failures: invalid key, unreachable host, missing endpoint, invalid model ID.
- [ ] Verify the tray, pause/resume, session history and exit restore mouse-button behavior.
- [ ] Test at 100%, 125%, 150% and 200% system display scaling, including two monitors with different scaling.
- [ ] Test fresh installation, upgrade and uninstall; user settings should survive uninstall.

## macOS headless
```sh
python3 -m unittest discover -s scripts -p 'test_ci_*.py' -v
python3 scripts/check_headless_scope.py
bash scripts/build-macos-headless.sh "$(uname -m)" "$PWD/dist"
```
On macOS 13+ the script must produce a signed .app with a native Swift menu-bar
host and a native Go voicebridge. CI builds both arm64 and x86_64 architectures, including the non-activating
AppKit floating pill. Static CI checks require both Swift adapter files to be
compiled and counted toward the source ratio.
The report measures only Go packages actually linked by voicebridge. The old
macOS Meeting bridge is forbidden as a dependency and must not count toward
the numerator. CI additionally enforces <=850 native Swift nonblank LOC, the
same Go speech/selected-task entrypoints on Windows and macOS, and preservation
of Windows Meeting Core. A raw Go/Swift percentage is **reported**, not
artificially maintained with dead code; it is not functional coverage.

### Physical-device acceptance (not CI-verified)
- Permit Microphone, Accessibility and Input Monitoring access
- Confirm the floating bottom-center pill appears at hold activation, shows a
  live MM:SS and microphone waveform (pink dictation / lavender rewrite),
  changes to processing, and dismisses after terminal success/error feedback
- Click the pill × during recording: audio is discarded; during processing:
  stale Go completion must not insert text; neither action steals editor focus
- Confirm the separate macOS menu-bar icon is still available to reload/quit
- Hold RightCommand, release after dictation and confirm **one** insertion
- Hold RightOption over selected text, say an instruction, verify the selection
  has not changed and rewrite once
- Repeat with Mouse4 and Mouse5; ordinary short presses must not start recording
- Validate Chinese + English ASR with actual recordings, no synthetic claim
- Test Safari, Chrome, Notes, VS Code and Terminal, including switching focus
  while requests are pending
- Assert 180-second auto stop, insufficient audio, API timeouts, clipboard
  changes, app re-launch, repeated sessions, denial of permissions
- Compare key/button-to-status, start-to-recording-ready, release-to-visible
  text P50/P95, and explicitly count failures/uncertain deliveries

Retired Xcode SwiftUI tests and cross-platform screenshot matrices no longer
describe the headless product. Their history remains in Git, not in CI.
