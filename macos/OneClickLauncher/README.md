# VoiceRewrite OneClick for macOS

A **separate macOS 14+ SwiftUI application** for locally running a Qwen3-ASR compatible GGUF model through the existing `oneclick/` Go HTTP proxy and a user-selected `llama-server` binary. It is not a tab in the main VoiceRewrite app and does not require the Windows executable, Python or a pre-existing model server. It does **not** download or bundle model weights or llama.cpp. The bundle contains its own Go OneClick helper; Go is only required on the build machine.

## Build and launch

Requires Xcode Command Line Tools/Xcode and Go 1.23 on macOS. From the repository root:

```bash
bash macos/OneClickLauncher/build.sh arm64
bash macos/OneClickLauncher/build.sh x86_64
open .oneclick-build/$(uname -m)/VoiceRewrite-OneClick.app
```

Each app includes a *same-architecture* native UI and a statically built Go proxy. The script checks `lipo` architecture and ad-hoc code signature. These are development builds; production signing/notarization and installer artifacts are owned by the macOS distribution workstream, not this module.

Before opening the launcher, obtain a macOS llama.cpp `llama-server` build supporting the Qwen3-ASR GGUF and its audio encoder, along with **matching** `model.gguf` and `mmproj.gguf` files. Pick all three files using the native selectors. The llama-server binary must be executable and built for the Mac CPU architecture. The launcher validates the two GGUF file paths before starting but cannot verify their compatibility without a real backend run.

Select separate loopback public and backend ports (defaults 8081/8080), GPU layers (99 by default), context length (8192 by default), or enable CPU fallback / use 0 GPU layers. Metal is the macOS accelerator; an Intel build does not imply that Intel Metal acceleration exists or is supported by the chosen backend. The launcher never makes CUDA a macOS requirement.

Click **启动**, then wait for **服务已就绪**. The app will query `/health` and discover the model ID through `/v1/models`. On failure, inspect the live diagnostics for invalid file paths, port conflicts, process exits, model-loading errors or accelerator errors. **停止** sends SIGTERM to the owned Go service, which shuts down the HTTP proxy and kills its owned llama child. **重启** stops the previous instance first. Quitting the launcher also requests shutdown. If the app is force-killed or the OS crashes, child cleanup is not guaranteed; inspect the local ports before reopening.

## Connect the main VoiceRewrite application

With the default ports, copy the launcher URL `http://127.0.0.1:8081/v1` using the **复制 URL** button. Open VoiceRewrite → Settings → ASR service, paste it into **ASR base URL**, then copy the model ID displayed by OneClick into **ASR model ID**. A local OneClick ASR service normally needs no API key. Save and run **Test ASR connection**. The test uploads synthetic silence, not a personal recording, and does not prove actual transcription. The backend must support the model and an actual audio request for that.

The same proxy also forwards `POST /v1/chat/completions`, `POST /tokenize`, `POST /detokenize`, and `GET /v1/models`; audio requests go through `/v1/audio/transcriptions` or `/v1/audio/translations` (and their non-`/v1` variants). The proxy strips Qwen3-ASR `language …<asr_text>` metadata from completed transcription responses and forwards streaming chat responses unchanged. Tokenization routes are rooted at `http://127.0.0.1:8081`, **without** the `/v1` suffix.

**ASR models are not general-purpose LLMs.** To rewrite text, configure a suitable LLM in VoiceRewrite's separate LLM settings; merely pointing ASR and LLM at the same Qwen3-ASR model is not a supported guarantee of good rewriting.

## Tests without models

The following is runnable on both macOS runner architectures, and uses only the Go stdlib and native Apple toolchain:

```bash
bash macos/OneClickLauncher/check.sh
```

It executes `go test -race` and `go vet` in `oneclick/`, builds **both** arm64 and x86_64 app/helper bundles, checks `lipo` and signatures, and removes temporary build outputs. The Go integration tests create a stub executable and local mock HTTP server. They assert proxy readiness, cleaned transcription, models, chat, tokenization, missing model, port conflict, unexpected child exit, and SIGTERM/port cleanup. Fake responses are explicitly **not** evidence of successful inference.

Hardware verification still requires actual Apple Silicon and Intel Macs, appropriate `llama-server` binaries, real compatible GGUF+mmproj weights, Metal and CPU startup, transcription in Chinese/English, streamed prefix continuation, memory limits, repeated stop/restart and safe quit. No simulated success substitutes for this device testing.
