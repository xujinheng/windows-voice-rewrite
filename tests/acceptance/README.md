# Cross-platform native UI acceptance (#84 / #73)

The workflow `Cross-platform UI acceptance evidence` is a **test workflow**, separate from K's installer/release workflows. It builds real Windows Win32 and native macOS (Apple Silicon/Intel) views and retains screenshots and diagnostic logs. Existing `visual-matrix.yml`, `meeting-ui.yml`, `scripts/test-meeting-ui.ps1`, Swift Interaction and AppKit tests remain authoritative; this workflow reuses their renderers and meeting harness.

## Execute

* On Windows with an interactive desktop: from `voice-rewrite/`, run `go build -trimpath -ldflags '-H=windowsgui' -o VoiceRewrite.exe .`, then from repo root run `pwsh -File scripts/capture-acceptance-windows.ps1` and `pwsh -File scripts/test-meeting-ui.ps1`. A disconnected/locked desktop may produce invalid screenshots.
* On macOS run `VOICEREWRITE_ACCEPTANCE_PLATFORM=macos-arm64 VOICEREWRITE_ACCEPTANCE_DIR="$PWD/acceptance-output/macos-arm64" xcodebuild -project macos/VoiceRewrite.xcodeproj -scheme VoiceRewrite -configuration Debug -destination 'platform=macOS' -only-testing:VoiceRewriteTests/VisualAcceptanceTests -resultBundlePath /tmp/acceptance.xcresult test`. Set `macos-x64` for Intel. The Xcode suite attaches PNG evidence and writes a manifest; it does **not** start the microphone/Go model service or send text to external editors.
* Generate review report: `python3 -m pip install Pillow`, then `python3 tests/acceptance/report.py acceptance-output/windows/manifest.json acceptance-output/macos-arm64/manifest.json acceptance-output/macos-x64/manifest.json --output acceptance-report --require-complete-matrix`. Run parser regressions: `python3 -m unittest discover -s tests/acceptance -v`.
* Validate future real-device records: `python3 tests/acceptance/manual_matrix.py tests/acceptance/device-matrix.csv`.

## Exact-state comparison contract

Every image is recorded with `state`, `fixture`, `theme`, `locale`, and `scale`. **All five must match** before side-by-side comparison. A renderer showing a different state is not a visual mismatch: it is missing evidence. Same-state reports contain normalized image diagnostics for reviewer investigation, never an automatic pixel parity pass. Review header/navigation/fields and font hierarchy, action roles, scaling, focus, viewport clipping, floating panel placement, six-pane splits, scroll-to-latest and color contrast. Different fonts, color profiles, native chrome and DPI need human interpretation.

The initial capture set includes:
* Windows — three settings pages; preview, recording, processing, error and event-sent pill; copy-only result (light/dark); real Win32 Meeting demo full/focus/expanded/large text.
* macOS ARM and Intel — all three settings pages (at the same 860×530 viewport as the actual settings windows), the same five pill phases, copy-only result (light/dark), and populated Go Core offline Meeting full/focus/expanded/large-type views. Each architecture also captures an intentionally **unpaired** empty Meeting reader.
* The CI report requires **14 specific matched states per Mac architecture, 28 pairs total**. Incomplete or mismatched manifests fail the evidence job; matched states remain **review-only** and do not prove visual parity.
* **Not covered by the synthetic screenshots** — result-card manual apply, live audio and third-party editor delivery, native permission recovery and signed installation. Windows Meeting E2E exercises buttons, pane drags, font and scroll state; Swift Interaction tests exercise native clipboard and input decisions, not physical third-party insertion.

## Release-gate matrix (#73)

`device-matrix.csv` contains `not_run` examples only. A person operating the device must record the CPU, exact OS and target editor version, mouse model and driver, actual permission state, installer SHA256, action and witnessed outcome, and a redacted evidence reference. The validator rejects any `pass`, `fail` or `blocked` entry with missing facts, including an invalid 64-character SHA. Cover hardware Mouse4/Mouse5 short navigation and long holds (250ms on both hosts), R/E keyboard combinations, microphone permission and actual ASR, unicode/emoji, clipboard multi-format/ownership takeover, within-app and between-app focus, password/Secure Input, AX-denied browser textareas/contenteditable, Electron/VS Code, Zoom/Teams source/child-process audio and install/upgrade/restart with verified signatures and notarization.

No CI runner establishes true mouse hardware, physical microphone, third-party editor injection, signed Finder installation or actual meeting audio. CI statuses must say **代码检查通过** or **模拟测试通过**, not `真机已验收` and never `284/284` without physical verification. Never upload real microphone audio, clipboard contents, API keys or private meeting transcripts.
