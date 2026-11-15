# Testing

## Automated CI

`.github/workflows/build.yml` runs for every pull request, pushes to `main`, version tags matching `v*`, and manual dispatches. Its checks are:

- Ubuntu: verify Go formatting, run `go test -race -count=1 ./...` across the `voice-rewrite` module (including every `internal/core` package), check the portable core source/import boundary across Linux, macOS and Windows, and compile `./internal/core/...` with `CGO_ENABLED=0`.
- Real `macos-15` runner: run `go test -count=1 ./internal/core/...`, then compile those packages for both Darwin arm64 and amd64.
- Windows: run `go test ./...` and `go vet -unsafeptr=false ./...` for `voice-rewrite`; run `go test ./...` and `go vet ./...` for `oneclick`.

The portable core boundary check requires the same Go and test source files across Linux, Darwin and Windows and forbids native OS syscall imports in `internal/core`. A Windows-specific source file under `internal/core` is not a substitute for a platform adapter outside that directory.

PRs do not build or upload general-purpose GUI/OneClick executables, installers or release archives in the build workflow. Pushes to `main` and version tags build and upload the Windows executables, portable ZIP, and installer; version tags also perform artifact attestations and publish releases when the release notes are present.

`.github/workflows/meeting-ui.yml` runs the Win32 Meeting E2E Go test and a real GUI screenshot/interaction script on matching pull requests, pushes to `meeting-preview`, or manual dispatch. `.github/workflows/direct2d-result.yml` runs native Direct2D result-card tests on matching pull requests, pushes to `main`, or manual dispatch. These UI jobs build temporary test executables on Windows, not distributable packages. `.github/workflows/visual-matrix.yml` captures the Windows UI on relevant `main` pushes or manual dispatch; `.github/workflows/demo-video.yml` renders demo media on its configured `main` paths or manual dispatch.

## Local Windows checks

Run the Go test suite and Windows GUI build before merging:

```powershell
cd voice-rewrite
gofmt -l .
go test ./...
go vet -unsafeptr=false ./...
go build -trimpath -ldflags="-H windowsgui -s -w" -o VoiceRewrite.exe .
```

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

## Automated screenshots

[Windows UI screenshots](VISUAL-TESTING.md) documents the GitHub Actions visual checks. Screenshot previews use deterministic example data; interactive scenarios above require manual verification.

A test checklist is not evidence that the listed scenario passed.
