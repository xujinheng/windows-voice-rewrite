# Coding-agent rules

Read `voice-rewrite/ARCHITECTURE.md`, relevant production code, open PRs and CI before changing anything. This file defines principles; architecture and platform-specific details belong in `ARCHITECTURE.md`.

## Product and ownership

- **Working behavior first.** Fix real defects before cosmetic cleanup. Preserve existing working features; a green CI run does not prove real-device behavior.
- **One Go business core.** Every portable decision has one Go owner. Native Windows/macOS code performs OS-specific work only; do not duplicate business policies or force OS APIs into fake shared abstractions.
- **Keep behavior honest.** Preserve necessary cancellation, permissions, focus and clipboard ownership checks. Sending input is not proof of insertion; do not retry uncertain delivery.
- **Design for the final state.** Remove obsolete internal compatibility, parallel implementations and speculative layers. Do not preserve code merely because it already exists.

## Simplicity and deletion

- **Equivalent behavior should use fewer readable production lines and shorter call chains.** Prefer direct logic over wrappers, redundant state, interfaces and managers. Do not trade correctness, useful tests or readability for a LOC score.
- Trace actual callers and end-to-end paths on **affected platforms** before editing. Find the real owner; delete superseded code and obsolete implementation-detail tests in the same change. Retain behavior-regression tests.
- Measure production LOC before/after and total PR additions/deletions. Explain necessary growth with concrete behavior or evidence; do not call added layers an improvement by default.

## Continuous delivery

1. Fix a concrete defect or make a small, independent deletion-first change. Avoid unrelated refactors and overlapping PR edits.
2. While one PR waits for CI, develop the next **independent** PR. Stack only when dependencies require it; rebase or retarget as predecessors merge.
3. Review the final diff and affected Windows/macOS paths; run relevant tests and required CI on the **exact head**. Never bypass failures. Separately identify physical-device acceptance gaps.
4. Merge only verified work, check main CI, report PR/SHA and LOC delta, and continue to the next meaningful task. Stop cosmetic churn when nothing substantive remains.

## Verification and documentation

- Keep CI proportional to the change and avoid redundant or paid infrastructure without approval. Preserve relevant cross-platform, regression and release coverage.
- Measure real user-visible latency and failures before claiming performance gains; label synthetic results honestly.
- Keep docs concise and current. Put technical architecture, paths and test commands in `ARCHITECTURE.md` or their owning files, not here. Close issues only after their acceptance criteria are met.
