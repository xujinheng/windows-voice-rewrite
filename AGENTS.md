# Coding-agent rules

Read `voice-rewrite/ARCHITECTURE.md`, affected production call chains, latest `main`, open PRs and CI before editing. This file states durable product and engineering constraints, not project history.

## Product and platform boundaries

- **Working behavior first.** Fix input failures before cosmetic cleanup. Preserve Windows Mouse4 dictation, Mouse5 selected-text rewrite, shortcuts, result cards and Meeting. macOS is intentionally a minimal status-bar dictation/rewrite host, not a second settings/Meeting app. A green build is not real-device acceptance.
- **One Go business core.** Portable config, ASR/LLM, text routing, task policy, UI specification and state decisions have one Go owner. Windows/macOS adapters perform native audio, input, focus, clipboard and rendering operations only. Do not duplicate business decisions in Swift/Windows or introduce fake sharing through wrappers. `oneclick/` is an independent ASR bootstrapper, not a desktop host.
- **Dictation and rewrite have different delivery requirements.** Ordinary dictation should paste into common editors even if AX text/selection attributes are unavailable; do not apply rewrite-grade checks to it. Selected-text rewrite must verify the target and selection before replacement, otherwise preview instead of replacing blindly.
- **Delivery is an OS boundary, not proof of insertion.** Preserve focus/epoch checks, clipboard ownership and restoration, cancellation and necessary permissions. Do not retry a paste that may already have been delivered; distinguish ASR success, event sent and confirmed editor insertion. Avoid speculative security gates that obstruct working input.

## Simplicity and final-state design

- **Equivalent behavior should take fewer readable production lines and shorter call chains.** Find the simplest direct algorithm; eliminate single-caller forwarding wrappers, mirrored state, dead paths, unused contracts and unnecessary Manager/Controller/Coordinator layers. Do not game LOC with dense one-liners, missing error handling or deleted meaningful tests.
- **One decision, one owner.** Trace actual production callers end-to-end on each affected platform before refactoring. Sharing an import is not sharing a decision. Move portable logic to its existing Go owner and delete superseded branches, fields and obsolete implementation-detail tests **in the same PR**. Preserve behavior-regression tests and working Windows paths.
- **No transitional architecture by default.** This project does not need compatibility for unused internal APIs or abandoned workflows. Prefer the clean final state; do not add speculative interfaces, controllers, state machines, bridge protocols or extra IPC. Keep real OS lifecycle and concurrency safeguards.
- **Make code growth explicit.** Report production LOC before/after, total additions/deletions and ownership/call-chain simplification per PR. Prefer net deletion for equivalent behavior. Justify added lines with concrete behavior, correctness or test evidence; LOC is a metric, not a substitute for working software.

## Continuous PR pipeline and verification

1. Fix a real defect or choose the smallest independent deletion-first refactor. Review adjacent PRs and actual callers; do not mix unrelated work.
2. While PR A waits for CI, develop independent PR B on its own branch. Stack only for real dependencies, rebase/retarget as predecessors merge, and avoid overlapping changes or redundant CI.
3. Review the **exact final diff and head SHA**, run relevant Go, Windows and macOS checks, and require passing CI before merge. Do not bypass failures. Separately record real-device gaps: physical microphone/side buttons, focus changes, clipboard, Unicode and insertion into third-party editors.
4. Merge verified work, check main CI, report PR/SHA and LOC delta, then continue to the next meaningful task without waiting for another instruction. Stop cosmetic churn when only device acceptance remains.

## CI, performance and documentation

- Keep CI proportional to changes: standard GitHub-hosted runners, no paid infrastructure without approval, no redundant builds or excessive artifacts. Preserve meaningful Windows regressions, Go/race tests, both macOS architectures and release checks.
- Measure trigger-to-indicator, recording readiness and release-to-visible-text latency (P50/P95/failures) before claiming performance gains. Synthetic CI is not a physical-device benchmark.
- Keep README and architecture docs concise and current; put platform/API details and test commands in their owning docs/scripts, not here. Close issues only when their acceptance criteria are met.
