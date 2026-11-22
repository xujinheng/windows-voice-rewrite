# Release downloads and verification

VoiceRewrite ships one standalone Windows application (`VoiceRewrite-Windows.exe`) and macOS applications in DMGs (`VoiceRewrite-Mac.dmg` for Apple Silicon, `VoiceRewrite-Mac-Intel.dmg` for Intel). The application includes optional ASR deployment in Settings: choose a folder and press Deploy. It downloads only into `<selected folder>/Qwen3-ASR/`.

The Windows binary currently lacks an Authenticode publisher signature. macOS development DMGs are ad-hoc signed, not Apple Developer ID signed or notarized, and Gatekeeper may block them. Verify the downloads come from this repository before deciding to run unsigned builds.

Tagged Windows releases have GitHub provenance attestations:

```bash
gh attestation verify VoiceRewrite-Windows.exe --repo xujinheng/windows-voice-rewrite
```

Signed and notarized macOS distribution is published separately only when valid developer credentials and Apple notarization are available. Passing CI does not prove microphone, Accessibility or editor-insertion behavior on a physical device.

This project is licensed under [BSD 3-Clause](LICENSE).
