# Testing VoiceRewrite

## Portable Go Core and Windows host
```sh
cd voice-rewrite
go test -race -count=1 ./...
```
The main CI continues Windows executable compilation, Windows UI capture and
Direct2D regression checks. Do not remove or suppress failing Windows tests.

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
A linked-source Go percentage >=90 is a static source metric, not evidence of
user-facing correctness.

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
