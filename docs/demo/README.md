# Product demonstration

The [30-second demo](voicerewrite-demo.mp4) shows dictation (Mouse4), selected-text rewriting into English and questions (Mouse5). The README displays the [animated preview](voicerewrite-preview.webp).

The selected Chinese sentence, English rewrite and spoken commands are scripted examples, not a live model request. The recording indicator is captured from VoiceRewrite on Windows.

Regenerate the files with Python, Pillow and FFmpeg:

```bash
python scripts/render-demo-video.py --assets docs/ui --output docs/demo
```

The [demo video workflow](../../.github/workflows/demo-video.yml) runs when reviewed `docs/ui/*.png` screenshots change, or by manual dispatch. It uploads the resulting media as an artifact; committing it to protected `main` requires a reviewed PR.
