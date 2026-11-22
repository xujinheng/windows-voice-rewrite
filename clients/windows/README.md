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

On Windows 11, start Zoom or another conference app. From the tray choose **开始会议模式 · 选择音频来源** and select the app. The topmost, resizable meeting window uses the same native light/dark palette as the main app. A fixed **current speaker** section shows the Chinese meaning in a larger primary reading area, with the original English in a separate smaller area below it. The status line reports whether transcription or translation is provisional, finalized, or waiting to synchronize with a revised original; it does not guess whether anyone is demanding an answer. The history contains only older completed turns; incoming recognition and translations never reflow it. Scroll up to read or select old captions without jumping back to the bottom. A new-content counter appears while you are reading, and **回到最新** explicitly resets it. The Chinese current utterance, original English, completed conversation history, reply suggestions, generated English and its recognized Chinese source are six independently height-adjustable sections. Drag the small horizontal handles between sections to redistribute space; each side has a readable minimum height. The layout scales with the window and remembers the split proportions after closing. Focused English mode and **专注字幕** show only the dividers between currently visible sections. Live updates do not change your chosen proportions or steal the historical scroll position. Recognition is shown after completed audio snapshots rather than with every decoder token. A Chinese translation appears only after the translation request completes; partially generated translations are never displayed or restarted from the beginning. Previous translations remain readable while the next snapshot is processed. The session retains its transcript rather than discarding older turns after a small fixed count. Scrolling back to the bottom resumes following automatically. The type hierarchy uses a larger Microsoft YaHei UI font for the current Chinese translation, a secondary English font for the source, and lower-emphasis typography for the archive. **专注字幕** hides the reply/expression tools but keeps both the current and historical reading areas.

**会议资料** stores your role, actual experience, constraints and terminology. Terms guide Qwen ASR spelling; background and terms also accompany translation and reply requests. Leave unknown facts blank. Suggestions must not invent experience or commitments. The profile pane also selects the meeting ASR protocol:

- `llama-prefix` (default): Qwen3-ASR GGUF on llama.cpp, using streamed audio chat completions and the server's tokenizer to roll back five text tokens between snapshots. It requires `/v1/chat/completions`, `/tokenize`, `/detokenize`, and enabled assistant prefilling. The embedded local llama.cpp server exposes these routes. This is cumulative-audio transcription with text continuation, not native incremental audio encoding. It still uploads and re-encodes the active audio window.
- `multipart`: explicit segmented recognition using the configured `/audio/transcriptions` provider. No prefix continuation is used.

Recognition snapshots adapt between 0.8 and 2.4 seconds according to measured ASR/translation request time, with a bounded 11-second audio window. A final segment cancels an obsolete preview; bounded queues replace stale waiting work and show the discard count. Unsupported prefix APIs produce an actionable error rather than silently changing protocols. Keep the existing model name/alias in Settings. A port number alone does not identify whether the bundled or an external llama-server is listening there.

Meeting ASR and LLM requests share a single scheduler, suitable for both models residing on one GPU. Your spoken expression and manual refinement have highest priority, followed by final recognition, translation, provisional recognition and reply suggestions. Lower-priority suggestions yield when subtitles need the GPU. Cancellation stops the client request; how promptly GPU computation stops depends on the backend. Meeting requests send `chat_template_kwargs.enable_thinking=false`; the server's chat template must honor it.

The compact status header shows the audio source and processing state, with a notice when obsolete tasks were skipped. Speech recognition and translation timings remain available in the application logs for troubleshooting.

Captions are translated from the latest recognized segment only, using a dedicated translation prompt. No reply or turn-taking metadata is requested in subtitle generation. Unexpected output labels are removed, and translations that are implausibly long for the source are rejected rather than displayed as recaps. The speech segmenter waits through short pauses instead of ending a turn immediately. Reply suggestions are created only when you select **生成建议**, avoiding unsolicited GPU work and distracting reply changes. This action uses the latest fully recognized speech, not a provisional sentence. The displayed batch remains readable when another batch is prepared; **查看新建议** switches to it. Select **1 / 2 / 3** to read and copy a completed reply. Enter a refinement instruction (up to 600 Unicode characters) and choose **改写回复** to place the revised English in **我的英文表达**. Refinement changes only the selected completed suggestion; it never borrows unrelated meeting history. Glossary hints used for suggestions match the actual utterance or recent relevant turns rather than injecting every saved term. Nothing is sent into a meeting chat automatically.

In an active meeting, hold the **dictation shortcut** (Mouse4 by default), speak a Chinese sentence, then release to get a faithful, speakable English translation in **我的英文表达**. Hold the **rewrite/voice-command shortcut** (Mouse5 by default) for a general-purpose spoken-English assistant: ask a spontaneous question, state your opinion, proactively respond even if nobody explicitly asked you, request an explanation, or ask the model for a specific factual answer you need to say aloud. For example, “问他这两个方案的 schedule” produces a question to other participants; “有多少印度人会说英语，我要告诉大家” requests a concrete English answer rather than a question back to you. The model infers the task from your speech instead of matching a fixed list of command phrases. Only explicit references to earlier discussion include recent meeting context; unrelated questions are not pushed toward project topics. Knowledge answers are based on the configured LLM and are not live web-verified; historical statistics should include their date and appropriate uncertainty. These two meeting shortcuts do not require selecting text or automatically paste into other applications. A new complete English result **automatically expands** its reading area and temporarily hides the reply tools; choose **收起英文** to restore the usual view, or **放大英文** to reopen it at any time. Long English responses can be scrolled and copied without truncation, while the original recognized Chinese speech stays in a separate compact area for verification. While another speech request runs, the last completed English expression stays readable and copyable. A request error appears alongside the recognized source and never destroys the last usable English. A completed voice request also opens the expression view when you are using **专注字幕**. If the LLM fails, the panel shows the specific error; use Settings to check the configured text-model endpoint. Outside meeting mode, Mouse4 retains normal raw ASR insertion and Mouse5 retains selection-based rewriting. **冻结画面** keeps the current captions and suggestions in place while processing continues; **暂停采集** releases meeting capture and cancels its outstanding work. Close the window to stop the session. **专注字幕**, **A− / A+**, individually dragged section sizes, and the normal window position/size are remembered in `%APPDATA%\VoiceRewrite\meeting-ui.json`.

Choose **查看会议模式演示** or run `VoiceRewrite.exe --meeting-demo` for offline sample data. Live mode uses only the configured endpoints. Browser capture can include multiple tabs in the selected app's process tree. See [privacy](../../PRIVACY.md).

## Build

Requires Go 1.23 or newer. On Windows:

```powershell
cd clients/windows
go test ./...
go vet -unsafeptr=false ./...
go build -trimpath -ldflags="-H windowsgui -s -w" -o VoiceRewrite.exe .
```

See [architecture](../../docs/architecture.md), [Windows testing](../../docs/testing.md), [privacy](../../PRIVACY.md) and [download instructions](../../README.md).

## License

VoiceRewrite is distributed under the [BSD 3-Clause License](../../LICENSE). The [third-party notices](../../docs/third-party-notices.md) remain in effect.
