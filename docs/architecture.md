# Architecture: one Go Core, N native clients

This repository contains **one independent Go business core** and three peer clients. The folder structure is the dependency diagram:

```text
core/  <--- clients/windows/
       <--- clients/macos/bridge/ <--- clients/macos/app/ (Swift)
       <--- clients/cli/

core/localasr/ owns optional local ASR model/runtime deployment for both hosts.
```

## Ownership

- `core/` is the Go module `github.com/xujinheng/windows-voice-rewrite/core`. It builds without GUI, Windows, or Swift. Only Core owns portable task transitions, physical input roles, cancellation, ASR, rewrite, result routing, and meeting session decisions.
- `core/voice/` owns the unified `Core.Apply(Command)`, `Core.Snapshot(code, token)`, and `Core.Execute(...)` contract, emitting shared `Event` and `Result` values. Windows, macOS and CLI call this same engine.
- `core/meeting/` owns portable meeting session state and inference scheduling. The Windows client supplies the native audio source and HUD; macOS has not implemented a Meeting UI.
- `core/config/` is the public configuration model. `core/internal/speech/` is private ASR implementation; only code in Core may import it.
- Other shared public packages (`audio`, `provider`, `textedit`, `indicator`, `shortcut`, `contract`, `ux` and focused UI-model packages) are real reusable capabilities currently imported by native clients. Do not expose private packages through new forwarding wrappers.
- `clients/windows/` owns Win32, UIA, keyboard/mouse hooks, microphone capture, clipboard/editor effects, tray and UI windows, and the Windows build entrypoint. It is a `package main` inside the `clients/` Go module.
- `clients/macos/app/` owns AppKit/Quartz, accessibility, AVAudioEngine, clipboard and native drawing. `clients/macos/bridge/` is only the in-process Darwin Go/C ABI for the same Go Core; no separate voice pipeline or JSONL helper.
- `clients/cli/` is a **separately importable Go module** and executable contract witness; it must build without native dependencies or CGO.
- `core/localasr/` deploys and manages one optional local Qwen3-ASR runtime for both native hosts; it uses the same ASR client owned by Core.

## Dependency rules

1. Core never imports clients, Swift, GUI frameworks, Win32 packages, or a Windows `main` package.
2. Clients may only import **public** Core packages; `core/internal/` is enforced by Go's `internal` visibility rules.
3. macOS's C ABI translates physical facts and typed events and does not implement a second task engine.
4. No compatibility copy of the retired `voice-rewrite/`, `macos/Headless/`, or `cli/` source tree is permitted.
5. Move portable rules to their existing business owner; do not add Managers, Coordinators, or forwarding APIs just to preserve old layout.

## Go modules and builds

```text
core/go.mod           reusable business module
clients/go.mod        Windows native executable and macOS Go/C bridge
clients/cli/go.mod    independent headless CLI client
go.work                local multi-module development
```

From the repository root, use `cd core && go test ./...`, `cd clients && go test ./windows ./macos/bridge`, `cd clients/cli && go test ./...`. Build macOS with `scripts/build-macos-headless.sh`. The native platforms must still pass physical microphone, hotkey, focus, clipboard and insertion acceptance testing: Go contract equality is not evidence of OS-level parity.

## Review and maintenance

Prefer a direct call over a new package or wrapper. Judge changes by clear ownership, simpler dependencies, fewer mutable states, fewer indirections, and unchanged behavior—not by matching file counts or an artificial percent of Go lines. The CI checks Core source portability, shared entrypoints and native builds. See `docs/CI.md` and `tests/acceptance/` for verification.
