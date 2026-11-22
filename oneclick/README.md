# Qwen3-ASR OneClick

A portable, self-contained **Qwen3-ASR-1.7B** server for **Windows x64** and **Apple Silicon Mac (M1 and later)**. One Go implementation handles downloads, SHA-256 verification, resumable transfers, runtime installation, process lifetime and the OpenAI-compatible ASR API. There is no Python, pip, Homebrew or separate macOS service.

## Get started

**Windows:** Download `Qwen3-ASR-OneClick-Windows-x64.exe` from a new Release, move it into its own writable folder, and double-click it.

**Mac (Apple Silicon):** Download `Qwen3-ASR-OneClick-Mac-arm64.zip` from a new Release, extract the ZIP into a writable location (for example, Downloads), and double-click **`Start OneClick.command`** inside the extracted folder. Leave the Terminal window open. This portable bundle is **not Developer ID signed or notarized**, so Gatekeeper may require manual authorization. Verify its origin before deciding to run it. macOS Intel is not supported by this automatic installer.

On first launch the program downloads the Q8_0 Qwen3-ASR weights and bf16 audio projector from ModelScope, and the pinned upstream llama.cpp runtime via download mirrors:

- **Windows:** NVIDIA CUDA 12.4 when available and working; otherwise CPU.
- **Apple Silicon:** native macOS ARM64 llama.cpp Metal build; CPU fallback if Metal device detection fails.

Downloads may take several gigabytes. Once `READY!` appears, VoiceRewrite can use **`http://127.0.0.1:8080/v1`** with model ID **`qwen3-asr`**. The managed llama.cpp server listens only on `127.0.0.1:8082`. Closing the window / pressing Ctrl+C stops the managed backend. This utility provides **ASR**, not a separate text-rewriting LLM.

## Everything stays in one folder

For macOS, the unpacked ZIP looks like this after the first launch:

```text
Qwen3-ASR-OneClick-Mac-arm64/
├── Start OneClick.command
├── Qwen3-ASR-OneClick
├── LICENSE
├── config.json
├── .oneclick.lock
└── Qwen3-ASR/
    ├── models/
    │   ├── Qwen3-ASR-1.7B-Q8_0.gguf
    │   └── mmproj-Qwen3-ASR-1.7B-bf16.gguf
    ├── runtime/
    │   └── metal/           # llama.cpp executable and libraries
    └── downloads/          # resumable .part files
```

On Windows, the same `Qwen3-ASR/` and `config.json` layout is used next to the EXE; the runtime directory is `cuda/` or `cpu/`. Interrupted downloads are resumed where supported, intact model files are not re-downloaded, and archives are removed once installed. SHA-256 checksums are pinned to known upstream releases. Concurrent launches are guarded; a second launch detects a running OneClick service.

**To remove OneClick and everything it downloaded, stop OneClick and delete its entire containing folder.** There is no managed installation under `/Applications`, `~/Library/Application Support`, `~/.cache` or `/usr/local`. macOS itself may retain ordinary system logs, Gatekeeper metadata or Terminal history. A custom `config.json` pointing to external files or an attached external server remains outside this cleanup contract.

### Advanced settings

`config.json` is created automatically beside the launcher. Edit this file to specify an existing `llama-server`, custom models, ports, GPU layers or attachment to a separately managed backend. Explicit custom paths are respected: OneClick never tries to download or remove those external files. Existing Windows defaults and ASR compatibility are unchanged. An older VoiceRewrite configuration still using `whisper-1` must change `asr_model` to `qwen3-asr`.

## API contract

| Method | Paths | Behavior |
| --- | --- | --- |
| GET | `/health` | Returns 200 when the backend is ready, otherwise 503. |
| POST | `/v1/audio/transcriptions`, `/audio/transcriptions`, `/v1/audio/translations`, `/audio/translations` | Forwards audio requests; removes Qwen3-ASR language metadata from successful JSON `text` fields or plain-text responses. |
| POST | `/v1/chat/completions`, `/chat/completions` | Forwards chat requests, including audio input and a trailing assistant prefix. |
| POST | `/tokenize`, `/detokenize` | Forwards llama.cpp token requests unchanged, including special-token options. |
| GET | `/v1/models`, `/models` | Forwards model discovery requests. |

All other method/path combinations return 404. API requests return 503 while the model loads. Request bodies, query parameters, and authorization headers are forwarded to the backend. Token APIs are rooted at the server address: clients configured with an ASR base ending in `/v1` must remove that suffix before appending `/tokenize` or `/detokenize`.

`text/event-stream` responses forward response headers and flush each received chunk immediately. Stream bytes are not parsed or stripped of ASR metadata; the consuming client assembles and cleans the final transcript. Client cancellation cancels the upstream request. A failed upstream stream terminates the downstream response instead of presenting a complete successful stream.

Non-streamed responses are buffered up to 128 MiB; larger responses return 502 instead of truncated content. Chat, token, model, and error response bodies are passed through without transcript cleanup.

Assistant prefilling must be enabled on the backend for prefix continuation. [Current llama.cpp server documentation](https://github.com/ggml-org/llama.cpp/blob/master/tools/server/README.md) specifies that it is enabled by default. Keep that default when launching or attaching to a server; do not disable it with `--no-prefill-assistant` or environment overrides. Check the installed build with `llama-server --version` and `llama-server --help` for its supported options.
