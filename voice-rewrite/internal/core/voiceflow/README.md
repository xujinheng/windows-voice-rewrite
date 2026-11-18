# Voice task events and effects (v1)

**Source of authority:** Go `voiceflow.Engine.Apply(Event)` and the
machine-readable [JSON Schema v1](schema.v1.json). Native hosts execute
effects, verify OS/clipboard/focus permissions, and return receipts. The
bridge does not capture audio, paste text, or drive AppKit/Win32.

## JSONL transport

This operation runs inside the existing local voicebridge JSONL v1 stream:

```json
{"v":1,"id":"transport-1","op":"voice_event","event":{"v":1,"type":"record_arm","session_id":"session-1","generation":1,"command_id":"host-arm-1","mode":"dictation","can_insert":true}}
```

A successful envelope has `v`, `id`, `ok:true` and a result:

```json
{"v":1,"id":"transport-1","ok":true,"result":{"accepted":true,"duplicate":false,"session_id":"session-1","generation":1,"phase":"armed","effects":[{"type":"show_preview","session_id":"session-1","generation":1,"command_id":"session-1/1/1","request_id":"session-1/1/1","text":""},{"type":"set_phase","session_id":"session-1","generation":1,"command_id":"session-1/1/2","request_id":"session-1/1/2","phase":"armed","text":""}]}}
```

`session_id` identifies the logical voice session; `generation` increases
strictly with each new `record_arm` **for the same session**. Each inbound
event uses a fresh `command_id` for replay protection, independent of its
transport `id`. When an effect triggers asynchronous work, echo both its
`command_id` as `receipt_id` and its `request_id` as `request_id` in
`record_ready`, `audio_stopped`, `asr_result`, `model_result` or
`write_receipt`. Never invent a receipt or reuse a completed effect ID.

## Required lifecycle

1. `record_arm` → preview and armed phase; `press` → `start_native_capture`.
2. `record_ready` → recording phase, or stop immediately if a physical
   `release`/`record_limit` already happened during startup.
3. `release` or `record_limit` → `stop_native_capture`.
4. `audio_stopped` → `send_asr`; successful nonempty `asr_result`
   → `attempt_native_write` for authorized dictation targets, or
   `send_model` for rewrite.
5. `model_result` routes to native write only with explicit authorization;
   otherwise show a preview.
6. `write_receipt` commits history and success **only when the OS adapter
   confirms delivery**; uncertain or failed writes show a manual preview,
   not a second paste.

`cancel`, `focus_changed` or `timeout` (with `timeout_origin`) retire
the active generation. Wrong, duplicated, stale and post-cancellation
receipts produce zero effects. No native capture or transcription network
calls are made by the reducer, and PCM audio frames continue through the
existing non-event bridge path: **do not send one event per audio frame**.

## Adapter integration for subsequent PRs (B–F)

- Use the same Go event/effect contract in Windows and macOS rather than
  duplicating the orchestration decisions in Swift or Windows UI callbacks.
- macOS already has a fail-closed decode-only `VoiceFlowTransitionV1` and
  `VoiceFlowEffectV1` in `BridgeProtocol.swift`. Add effect executors only at
  OS boundaries. Windows may call `voiceflow.New().Apply(...)` directly.
- Keep microphone setup, audio capture, Accessibility/UIA, credential
  handling, target focus validation and clipboard/paste safety native.
- Record exact effect IDs in pending native operations; report completion
  once, tied to the original session and generation. A posted paste event
  is **not** proof of insertion.
- Tests: `cd voice-rewrite && go test -race -count=1 ./internal/core/voiceflow ./cmd/voicebridge`;
  `go run ./cmd/contractgen -check`; then repository CI on both OSes.
- This PR establishes the shared contract. It does **not** claim that all
  production voice journeys already run through the new engine; that
  migration belongs to B–F and must be measured by the reviewed benchmark.

Error envelopes use `voice_event_version` for unknown nested event version
and `invalid_voice_event` for invalid event fields. Envelope-level JSONL
and duplicate request ID handling remain unchanged.
