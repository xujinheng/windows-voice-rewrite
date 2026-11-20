# VoiceRewrite macOS

macOS is a compact headless menu-bar host for dictation and selected-text rewriting. It uses the **same in-process Go Core** as Windows for ASR/LLM and portable decisions, linked as a Go `c-archive` in the app executable.

Build and usage: [app/README.md](app/README.md).

There is no SwiftUI/Xcode project, settings window, Meeting UI or separate macOS business-logic implementation. Native Swift owns only Quartz input, AVAudioEngine, AppKit status pill, Accessibility selection and pasteboard delivery. macOS installation and editor compatibility remain experimental; see CI headless build and physical acceptance requirements.
