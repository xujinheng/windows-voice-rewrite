[![VoiceRewrite 30-second demo](docs/demo/voicerewrite-preview.webp)](docs/demo/voicerewrite-demo.mp4)

[▶ Watch the 30-second demo](docs/demo/voicerewrite-demo.mp4)

# VoiceRewrite

**Hold a shortcut to speak, then release to insert your words.** Windows supported; macOS is experimental.

<a href="README.md"><img alt="切换到中文" src="https://img.shields.io/badge/%E4%B8%AD%E6%96%87-Switch_language-2563eb?style=for-the-badge"></a>

## 🚀 Quick start

1. **Download the app.** On Windows, get `VoiceRewrite-Windows.exe` and double-click it—no installer. On M-series Macs, get `VoiceRewrite-Mac.dmg` and drag the app into Applications. For Intel Macs, get `VoiceRewrite-Mac-Intel.dmg`.
2. **Deploy speech recognition.** Open VoiceRewrite **Settings → Speech Recognition**. Select a download folder (defaults beside the application) and click **Deploy**. VoiceRewrite automatically downloads Qwen3-ASR-1.7B and a native llama.cpp CUDA/CPU/Metal runtime. No separate OneClick, Python or Homebrew. First use requires internet access and several GB of disk space.
3. **Dictate.** On Windows, hold Mouse4 (lower side button), speak and release. On Mac, hold Right Command. Customize shortcuts in Settings.

All managed downloads stay in **`<selected folder>/Qwen3-ASR/`**: model weights, runtime, download cache and server log. Quit VoiceRewrite and delete that folder to remove the model environment. Subsequent launches reuse installed files and restart the local ASR server.

**Downloads:** [GitHub Releases](https://github.com/xujinheng/windows-voice-rewrite/releases) · [Latest main build (Actions)](https://github.com/xujinheng/windows-voice-rewrite/actions/workflows/build.yml?query=branch%3Amain)

**Version note:** v0.1.1 is an older release that still requires the standalone OneClick and Windows installer. The integrated Deploy and single-EXE/Mac-DMG packaging will be available in a new build after this change is merged; they are not assets of v0.1.1.

## ✨ Rewriting, translating and explaining text

Dictation only needs the built-in ASR. For AI writing, install a text LLM with an app such as [LM Studio](https://lmstudio.ai/). Load a chat model, start its local API server (typically `http://127.0.0.1:1234/v1`), then test the connection in VoiceRewrite Settings. Use Mouse5 on Windows or Right Option on Mac for voice-guided rewriting.

## 🍎 macOS notes

Dictation and cross-app text insertion are still experimental on macOS. Test DMGs may be ad-hoc signed, without Apple Developer ID notarization; Gatekeeper can block them. Verify the download source and grant Microphone, Accessibility and Input Monitoring permissions when required.

[Windows guide](clients/windows/README.md) · [Mac guide](clients/macos/app/README.md) · [Privacy](PRIVACY.md) · [Report issues](https://github.com/xujinheng/windows-voice-rewrite/issues)

Audio and selected text are sent only to your configured local or remote model endpoints. Licensed under [BSD 3-Clause](LICENSE).
