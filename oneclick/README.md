# Qwen3-ASR OneClick

Windows launcher for `llama-server.exe` with audio transcription and streamed chat continuation APIs.

## Configure

1. Download `llama.cpp` for Windows from [official releases](https://github.com/ggml-org/llama.cpp/releases). Use a build supporting Qwen3-ASR audio input, assistant prefilling, streaming chat completions, and the tokenization APIs below. The launcher does not bundle or pin llama.cpp.
2. Obtain a compatible Qwen3-ASR GGUF and matching `mmproj` GGUF.
3. Place `Qwen3-ASR-OneClick.exe` beside `llama-server.exe` or set `llama_server_path` in `config.json`.
4. Copy `config.example.json` to `config.json`, then set the model paths.
5. Run the launcher.

Default proxy endpoint: `http://127.0.0.1:8081/v1`. Default backend: `http://127.0.0.1:8080`.

For VoiceRewrite's default ASR endpoint (`http://127.0.0.1:8080/v1`), start from [config.voicerewrite.example.json](config.voicerewrite.example.json), which uses port 8082 for the backend.

The launcher can attach to an existing server. GPU acceleration is configurable; CPU fallback requires `allow_cpu: true`. The default launch arguments use 99 GPU layers, an 8192-token context, and one inference slot (`-np 1`). GPU memory must accommodate the ASR model, audio encoder, context, and any other resident models. Keep the unauthenticated API bound to loopback.

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
