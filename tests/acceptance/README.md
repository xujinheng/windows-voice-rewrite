# Native UI acceptance (current repository)

Windows has native settings, result cards, status pill and Meeting. macOS is a minimal **Headless menu-bar app**: it has no Xcode workspace, settings window, Meeting or result-card screenshot matrix. Do not report cross-platform GUI screenshot parity.

## Automated checks

- **Windows:** the `Windows UI acceptance evidence (headless Mac excluded)` workflow (`.github/workflows/acceptance-evidence.yml`) builds the Win32 application, runs native popup/Meeting E2E, and saves screenshots/logs. On a Windows desktop, run `pwsh -File scripts/capture-acceptance-windows.ps1` and `pwsh -File scripts/test-meeting-ui.ps1` from the repository root.
- **macOS arm64 and x86_64:** `.github/workflows/build.yml` calls `bash scripts/build-macos-headless.sh ARCH OUTPUT VERSION`, then compiles and executes `scripts/test-macos-focus.swift` appended to the real host source with `FOCUS_GUARD_TEST`. It checks Go C ABI linkage, synthetic focus/selection/shortcut events, configuration and compilation. It does **not** produce native GUI screenshot parity evidence.
- **Historical report tools:** `tests/acceptance/report.py` and `manual_matrix.py` still accept their own input manifests, but do not imply that a current macOS Xcode screenshot suite exists.

## Physical acceptance remains open

CI cannot certify a real mouse side button/keyboard hold, live microphone, third-party AX/UIA editor insertion, focus switching, multi-format clipboard contention, Unicode fallback, unsigned app installation or Meeting audio. Record actual device, OS/editor version, hardware/driver, observed result and redacted evidence. The `device-matrix.csv` examples are `not_run`, not passes; validate completed records with `python3 tests/acceptance/manual_matrix.py tests/acceptance/device-matrix.csv`.

Never call a compile-only or synthetic run **device-accepted**. Do not upload raw microphone audio, selected text, clipboard contents, API keys or private meeting transcripts.
