# VoiceRewrite

Native Windows 11 client for push-to-talk dictation and AI-assisted text editing.

## Setup

1. Run `VoiceRewrite.exe`. Open **Settings** from the system tray if the window is not already shown.
2. Set the **ASR endpoint**, model ID and optional API key. The endpoint must support `POST /v1/audio/transcriptions`.
3. Set the **LLM endpoint**, model ID and optional API key. The endpoint must support `POST /v1/chat/completions`.
4. Choose a microphone and dictation shortcut. Mouse4 is the default; F1–F24 can be configured.
5. Test both connections, save the settings and restart after changing the input device or shortcut.

Example endpoints are `http://127.0.0.1:8080/v1` for ASR and `http://127.0.0.1:1234/v1` for LLM. Configure and start the services independently.

The ASR connection test submits a generated silent WAV to the transcription endpoint. Confirm microphone recognition by dictating into an editor.

## Controls

| Action | Behavior |
| --- | --- |
| Hold **Mouse4** for 250 ms, speak, release | Transcribe and insert text at the cursor |
| Select text, hold **Mouse5** for 250 ms, speak, release | Rewrite, translate or ask a question |
| Result card | Copy output or apply a replacement when available |
| Tray menu | Open Settings, pause hotkeys, view recent results, check for updates, exit |

A short Mouse4/Mouse5 click performs its normal browser Back/Forward action. Audio is buffered locally from button-down and discarded on short clicks. For long holds, recording includes speech from the initial press; the indicator and tone appear after 250 ms.

Editing a selection requires the original editor and selection to remain valid. If insertion cannot be completed safely, the text is shown in a copyable result card.

Up to ten recent results are available from the tray during the current session. They are not written to disk. Use **Clear session history** or exit to discard them.

## Meeting mode

On Windows 11, start Zoom or another conference app. From the tray choose **开始会议模式 · 选择音频来源** and select the app. The topmost, resizable window separates provisional recognition from completed captions. Confirmed text for the current ASR continuation stays separate from the revisable tail; the final utterance is archived with Chinese translation. Completed history keeps its text selection and scroll position during live updates.

**会议资料** stores your role, actual experience, constraints and terminology. Terms guide Qwen ASR spelling; background and terms also accompany translation and reply requests. Leave unknown facts blank. Suggestions must not invent experience or commitments. The profile pane also selects the meeting ASR protocol:

- `llama-prefix` (default): Qwen3-ASR GGUF on llama.cpp, using streamed audio chat completions and the server's tokenizer to roll back five text tokens between snapshots. It requires `/v1/chat/completions`, `/tokenize`, `/detokenize`, and enabled assistant prefilling. The current OneClick proxy exposes these routes. This is cumulative-audio transcription with text continuation, not native incremental audio encoding. It still uploads and re-encodes the active audio window.
- `multipart`: explicit segmented recognition using the configured `/audio/transcriptions` provider. No prefix continuation is used.

Recognition snapshots adapt between 0.8 and 2.4 seconds according to measured ASR/translation request time, with a bounded 9-second audio window. A final segment cancels an obsolete preview; bounded queues replace stale waiting work and show the discard count. Unsupported prefix APIs produce an actionable error rather than silently changing protocols. Keep the existing model name/alias in Settings. A port number alone does not identify whether OneClick or llama-server is listening there.

Meeting ASR and LLM requests share a single scheduler, suitable for both models residing on one GPU. Your spoken expression and manual refinement have highest priority, followed by final recognition, translation, provisional recognition and reply suggestions. Lower-priority suggestions yield when subtitles need the GPU. Cancellation stops the client request; how promptly GPU computation stops depends on the backend. Meeting requests send `chat_template_kwargs.enable_thinking=false`; the server's chat template must honor it.

The status area shows the audio source, peak input level, processing stage, pending/skipped work, and ASR/translation first-text and complete-request timings. Timings include scheduler wait but exclude audio collection before the snapshot. They are measurements, not latency guarantees.

After an utterance, translation also assesses whether the speaker is handing over the floor using recent context. Automatic suggestions wait for a quiet gap. **生成建议** explicitly requests suggestions for the latest transcript. The displayed batch remains readable when another batch is prepared; **切换新建议** switches to it. Select **1 / 2 / 3** to read and copy a completed reply. Enter a refinement instruction and choose **改写所选** to place the revised English in **我的英文表达**. Nothing is sent into a meeting chat automatically.

Hold the configured dictation shortcut (Mouse4 by default), say your intended meaning in Chinese, and release it to produce English. **暂停显示** freezes reading while processing continues; **暂停采集** releases meeting capture and cancels its outstanding work. Close the window to stop the session. **仅字幕**, **A− / A+**, and the normal window position/size are remembered in `%APPDATA%\VoiceRewrite\meeting-ui.json`.

Choose **查看会议模式演示** or run `VoiceRewrite.exe --meeting-demo` for offline sample data. Live mode uses only the configured endpoints. Browser capture can include multiple tabs in the selected app's process tree. See [privacy](../PRIVACY.md).

## Build

Requires Go 1.23 or newer. On Windows:

```powershell
cd voice-rewrite
go test ./...
go vet -unsafeptr=false ./...
go build -trimpath -ldflags="-H windowsgui -s -w" -o VoiceRewrite.exe .
```

See [architecture](ARCHITECTURE.md), [Windows testing](TESTING.md), [privacy](../PRIVACY.md) and [installer instructions](../installer/README.md).

## License

VoiceRewrite is distributed under the [BSD 3-Clause License](../LICENSE). The [third-party notices](THIRD_PARTY_NOTICES.md) remain in effect.
