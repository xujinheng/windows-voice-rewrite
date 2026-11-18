# VoiceRewrite macOS

The macOS product is now a **headless menu-bar application**, sharing its ASR,
LLM, configuration and task result routing with the existing Go Core.

Build and usage: [Headless/README.md](Headless/README.md).

There is no SwiftUI/Xcode workspace, settings window, Meeting UI, or independent
macOS business-logic implementation. The single `Headless/main.swift` file
contains platform APIs only: keyboard/mouse, audio, menu bar and paste/selection.
The Go `cmd/voicebridge` runs as a bundled helper. Windows remains a separate
Win32 host using the same Go processing packages.
