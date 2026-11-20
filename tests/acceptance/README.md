# Native acceptance: Windows UI and macOS Headless

The current product has two distinct native hosts: Windows Win32 UI (including Meeting) and a minimal macOS menu-bar app with an AppKit recording pill. **There is no macOS SwiftUI settings/results/Meeting app or `macos/VoiceRewrite.xcodeproj`**. Do not require screenshots from that retired project or claim Windows/macOS screenshot parity.

## Automated evidence

Windows, on an interactive Windows desktop with Go and PowerShell:

```powershell
cd voice-rewrite
go build -trimpath -ldflags '-H=windowsgui' -o VoiceRewrite.exe .
cd ..
pwsh -File scripts/capture-acceptance-windows.ps1
pwsh -File scripts/test-meeting-ui.ps1
```

The current `.github/workflows/acceptance-evidence.yml` also runs the Windows native Meeting/popup E2E tests and uploads `acceptance-windows` screenshots/logs. A locked or disconnected desktop may invalidate captured UI evidence.

macOS 13+ with Go and Xcode command-line tools, on an actual Mac:

```sh
bash scripts/build-macos-headless.sh "$(uname -m)" "$PWD/dist"
python3 scripts/check_headless_scope.py
```

The `.github/workflows/build.yml` macOS matrix builds **arm64 and x86_64**, verifies app structure/signature, and compiles a native Swift→Go ABI/focus/hold synthetic smoke test from `scripts/test-macos-focus.swift`. The smoke is **not** a screenshot, microphone, or third-party editor delivery test.

## Physical device acceptance (not established by CI)

Record the OS and CPU, exact hardware/driver, app and installer SHA256, editor version, permissions, action, actual outcome, and redacted evidence in `device-matrix.csv`; validate with:

```sh
python3 tests/acceptance/manual_matrix.py tests/acceptance/device-matrix.csv
```

Verify physical Mouse4/Mouse5 short-click navigation and long hold, keyboard modifiers, microphone/ASR, Unicode and mixed Chinese/English, clipboard ownership/restore, focus changes during processing, actual editable/noneditable targets (including AX-denied editors), cancellation and result recovery, and Windows Meeting. Test macOS menu-bar/pill behavior in its actual Headless UI, **not** in a nonexistent settings window.

**Passing Go/Swift compilation, synthetic event tests or UI screenshots is not device acceptance.** Do not claim a real-world editor paste, system permission or microphone pass without observing it on the corresponding physical machine.
