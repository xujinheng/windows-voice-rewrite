# Chinese/English code-switching A/B protocol — Issue #143

## Status, 2026-11-18

**107 reproducible, non-private recording scripts; 0 WAV recorded.**
All 107 `audio_path` values are `null`, intentionally. There is **no baseline
WER/CER, recognition gain, prompt adoption, ASR runtime version or recording
latency observation** in this environment. The manifest is not an audio dataset.
Do not treat the zero-audio report as evidence of improvement.

The default user mode remains `off` so no user is forced onto an unverified
recognition prompt or an extra paid inference path. There is no post-ASR LLM,
no language force, no keyword field, no text replacement, no retry on 400/422.
The `auto-supported` mode only opts in on the official OpenAI API origin
and recognized model IDs. **Matching a model name does not prove a non-official
server consumes the field.** The `vocabulary-only` mode is for explicitly
verified endpoints and sends one multipart `prompt` per chunk. Meeting has a
separate private profile. The macOS UI exposes these settings; on Windows they
are currently editable in the user JSON config. Windows UI parity requires
additional work and is not claimed here.

## Recordings and licenses

The reference scripts in `scripts/asr_codeswitch_manifest.jsonl` were created
for VoiceRewrite; permission is granted to re-record and redistribute original
voices reading these scripts. Recording/audio ownership remains with the
speaker: obtain their explicit permission and a distributable license before
committing a WAV. **Never upload a user's existing recordings** or private
meeting audio. Do not label synthesized TTS as "real speaker A/B".

Each manifest entry has `id`, `reference`, `english_terms`, `category`,
`audio_path` (initially null), and optional `forbidden_terms`,
`target_chunks`. Record **the complete utterance without text correction**,
including hesitations, at 16kHz mono signed PCM16 WAV (no altered punctuation
or replacement terms); set `audio_path` to a relative WAV file. Keep
independent slow/fast speakers, pauses, Chinese only, English only, negative
hotword, code-switching and >50s/100s split clips. For long chunk fixtures, physically record speech long enough to cross the 25-second Go chunk windows, verifying actual WAV duration and multiple chunks:
`target_chunks` is an intended scenario, not proof of duration.

## Reproduce

All commands from repository root. Python 3.9+ stdlib only.

```sh
python3 -m unittest discover -s scripts -p 'test_asr_codeswitch_eval.py' -v
python3 scripts/asr_codeswitch_eval.py evaluate \
  --json-output /tmp/asr-empty.json --markdown-output /tmp/asr-empty.md

# After recording and licensing the same WAV once per clip, use a local ASR
# endpoint that the user explicitly allows (never in CI):
ASR_API_KEY=... python3 scripts/asr_codeswitch_eval.py run \
  --allow-network --endpoint http://127.0.0.1:8080/v1 \
  --model whisper-1 --audio-dir /path/to/licensed/wavs \
  --results /tmp/actual-asr.jsonl \
  --json-output /tmp/actual-asr.json --markdown-output /tmp/actual-asr.md
```

`run` executes baseline (no multipart prompt), short bilingual context, and
technical lexicon context **in the same invocation** on each source WAV, no
text changes or separate preprocessing. Each result records SHA-256, latency,
attempt count and either actual ASR output or failure type. The runner requires
explicit `--allow-network`, validates WAV format, forbids audio-directory
traversal and will refuse incomplete manifests. Set environment variable
`ASR_API_KEY` only if necessary. No CI network ASR invocation is allowed.
Capture server name, binary/runtime `--version`, model SHA/build, seed,
hardware and configuration outside of secrets; run baseline and candidate
at comparable load. A 200 HTTP status or forwarded multipart body **does not
prove prompt adoption**; compare error types and paired transcripts with
evidence from the actual backend implementation.

The runner does not control an ASR backend's random seed or remote load:
repeat trials with a fixed model/seed and interleaved randomized arm order
before concluding real effect. Also evaluate real Go segmentation behavior
separately from the single-WAV runner for long speech. For insertion latency,
capture native recorder-to-insert timings (runner only measures ASR round
trips). Latency numbers from the offline runner are not end-to-end.

## Definitions and quality gate

A/B coverage is accepted only when **every** clip has an `ok` observation
and identical audio SHA-256 across arms; otherwise `not_measured`, not a
selectively filtered average. The standalone CLI computes case-sensitive
strict and case-folded loose metrics. Tokenizer: maximal Latin
alphanumeric/dot/hyphen/slash/hash/plus chunks and Han codepoints as separate
tokens, punctuation/whitespace excluded. Mixed `CER` uses characters of
these normalized tokens; mixed `WER` is the mixed token edit rate (each
Han codepoint counts as one unit, so it is not conventional word-segmented
Chinese WER). Chinese CER is Han-only Levenshtein distance over Han
characters, English WER is Latin/numeric-token Levenshtein distance.
Term recall, precision, F1 and hotword false positives use the **fixed
manifest glossary**, never a claimed exhaustive semantic entity scorer.
Numeric accuracy checks reference digits. The strict mixed metrics preserve punctuation and exact casing (whitespace still ignored); loose metrics discard punctuation and case-fold Latin tokens.

Per-clip macro and global
micro metrics are both included.

Paired differences are candidate minus baseline, with 2,000 reproducible
clip-resampled bootstrap draws (95% percentile interval, seed 143).
P50/P95 uses linear-interpolated observed latency values.
The gate should require: relative English term recall improvement >=20%,
Chinese CER increase <=0.02 absolute, no material rise in negative hotword
false positives, P95 actual end-to-end increase <=150ms and no extra API
round-trip. These are **future acceptance thresholds**, not achieved results.

Endpoint capability matrix:
- Official OpenAI `gpt-transcribe` supports optional multipart `prompt`, `keywords[]`, and `languages[]`; this PR sends only `prompt` and does not alter the configured model or force a single language. Reference: https://developers.openai.com/api/docs/guides/speech-to-text
- Whisper-like multipart: `prompt` is potentially vocabulary context; check
  each model's official support and actual inference build.
- OneClick proxy: request bytes forwarded, but no evidence that attached
  Qwen3-ASR GGUF llama-server consumes the multipart `prompt`.
- Arbitrary OpenAI-compatible: unknown -> do not automatically opt in.
- Qwen3-ASR native context/hotwords: different model APIs; no unsupported
  `keywords`, `languages`, or forced `language=en` are sent.

Limitations before merge: real-WAV A/B and native physical insertion timing,
Windows settings UI controls, OneClick prompt-consumption integration, and
platform Xcode/Windows CI must be checked. Leave PR open if any blocker remains.
