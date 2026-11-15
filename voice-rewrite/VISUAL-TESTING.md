# UI screenshots

The [Windows UI screenshots workflow](../.github/workflows/visual-matrix.yml) builds and starts `VoiceRewrite.exe` on a Windows runner, captures the settings window, result card and recording indicator, and checks their bounds.

The workflow covers light/dark themes, long result text, settings scrolling and internal 125–200% UI scale overrides. It publishes README images under `docs/ui/`. The [demo video workflow](../.github/workflows/demo-video.yml) generates the walkthrough from those images.

The screenshot inputs use example text and preview states. Internal scale overrides do not test Windows system DPI changes. Physical multi-monitor behavior, keyboard accessibility and end-to-end dictation are covered by the [interactive checklist](TESTING.md).

## Meeting Windows E2E

[Windows Meeting E2E](../.github/workflows/meeting-ui.yml) runs on a standard GitHub-hosted Windows runner. A Windows-only Go test opens the production Meeting Win32 window and native EDIT controls, replays 66 completed utterances, and asserts that reading older history does not jump when more captions arrive. It verifies returning to latest, primary Chinese vs secondary English geometry, actual native button dispatch, font resizing, large spoken-English results, retaining the last copyable result on model failure, and showing a new spoken result while in captions-only mode.

The same job compiles and launches `VoiceRewrite.exe --meeting-demo`, sends native button commands, checks real control visibility, and captures four actual desktop PNGs (expanded English, full workspace, captions-only, larger type) plus JSON diagnostics. The images are retained as short-lived Actions artifacts for seven days. These are GUI integration tests, not a recording of real meeting sound: microphone, process-loopback audio, Zoom/Teams, external ASR/LLM behavior and physical mouse buttons still require a Windows audio test machine.
