# Qwen3-ASR OneClick

Standalone Windows x64 Go launcher/proxy for `llama-server`, providing Qwen3-ASR and OpenAI-compatible endpoints. ASR marker normalization is shared with VoiceRewrite through the local `voicerewrite/asrtext` Go module; builds require `oneclick/` and `voice-rewrite/` in the same source checkout. The macOS VoiceRewrite host is a separate minimal menu-bar app that connects to a configured ASR server; it does not ship an old SwiftUI OneClick launcher.

## Windows: double-click to install and start

1. Download `Qwen3-ASR-OneClick-Windows-x64.exe` from the project's release/build.
2. Place the EXE in a writable folder (for example, `D:\\ASR`) and double-click it.
3. The launcher creates `Qwen3-ASR/` **beside the EXE**, downloads the Q8_0 main GGUF and **bf16 audio projector** from **ModelScope (魔搭)**, then installs a pinned llama.cpp Windows release via mainland-friendly download mirrors. Both are verified against upstream SHA-256 digests.
4. It detects an NVIDIA CUDA GPU and tries the CUDA 12.4 backend; otherwise it installs the CPU backend. If the CUDA device check fails, it falls back to CPU.
5. When the console prints `READY!`, leave it open. The ASR API is `http://127.0.0.1:8080/v1` by default, matching VoiceRewrite's default ASR endpoint; no manual endpoint change is needed. llama.cpp listens internally on `127.0.0.1:8082`.

The API model ID is `qwen3-asr`, matching the Windows and macOS VoiceRewrite defaults; the loaded model is Qwen3-ASR-1.7B, not Whisper. Existing client configuration files that still specify `whisper-1` must change `asr_model` to `qwen3-asr` when switching to this launcher version.

No Python, pip, Git, or manual model management is required. First setup needs internet and several gigabytes of free disk space. ModelScope hosts the weights and projector; the llama.cpp mirrors relay **official GitHub release archives**, so no direct access to GitHub or Hugging Face is required. Mirror availability varies by network.

### Idempotency and file layout

- `Qwen3-ASR/models/`: `Qwen3-ASR-1.7B-Q8_0.gguf` and `mmproj-Qwen3-ASR-1.7B-bf16.gguf`, each re-checked against SHA-256 and downloaded only if missing or corrupt. An existing Q8_0 projector is not selected automatically.
- `Qwen3-ASR/runtime/cuda/` or `Qwen3-ASR/runtime/cpu/`: pinned llama.cpp build with its DLL dependencies.
- `Qwen3-ASR/downloads/`: resumable `.part` files and temporary verified archives; ZIP files are removed after successful installation.
- `config.json`: created alongside the EXE on first launch. Existing explicit paths and externally managed llama-server setups remain supported.

Repeating the launch **does not download intact files again**. Interrupted downloads resume where supported. Simultaneous launches are serialized with a Windows mutex. If the service is already running, a second double-click reports its URL rather than starting a conflicting server. All network listeners default to `127.0.0.1`; closing the console stops the managed child process.

### Advanced manual configuration

If you already have `llama-server.exe` and matching GGUF files, place them alongside the EXE or edit `config.json` to point at your existing files. Custom paths are not auto-downloaded or overwritten. Existing `config.json` values are preserved; to migrate an older 8081 installation, change `listen_address` to `127.0.0.1:8080` and `backend_port` to `8082` before restarting. Model loading and GPU support still need hardware validation on a Windows machine.

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

Assistant prefilling must be enabled on the backend for prefix continuation. [Current llama.cpp server documentation](https://github.com/ggml-org/llama.cpp/blob/master/tools/server/README.md) specifies that it is enabled by default. Keep that default when launching or attaching to a server; do not disable it with `--no-prefill-assistant` or environment overrides. Check the installed build with `llama-server.exe --version` and `llama-server.exe --help` for its supported options.
