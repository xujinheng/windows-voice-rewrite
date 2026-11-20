# Coding-agent rules

Read `voice-rewrite/ARCHITECTURE.md`, the current production call chain, latest `main`, open PRs and relevant CI before editing. This file describes the **desired current code**, not project history.

## Product and ownership

- **Working dictation first.** Preserve Windows Mouse4 dictation, Mouse5 selected-text rewrite, shortcuts, result cards and Meeting. macOS is a small status-bar dictation/rewrite host, not a second settings/Meeting app. A green build is not real-device acceptance.
- **One Go business core.** Portable config, ASR/LLM, text decisions, task/cancellation rules, clipboard budgets and UI specifications belong to the existing Go owner. Windows keeps Win32/UIA/audio/hooks/clipboard/UI; macOS keeps AppKit/AVAudioEngine/AX/Quartz/pasteboard. The hosts perform OS calls, not parallel business decisions. `oneclick/` is a separate ASR-server bootstrapper.
- **Do not confuse intent with delivery.** Dictation favors native paste compatibility; incomplete AX text attributes must not alone block ordinary insertion. Selected-text rewrite requires a verified target and selection, with preview fallback. Verify task epoch, focus and clipboard ownership; do not retry a possibly delivered paste. An event sent is not confirmed editor insertion. Preserve real cancellation, permission and OS resource safeguards.
- **Keep the final design only.** No legacy compatibility shims for unused internal APIs, duplicate state machines, transitional bridges, second policy copies or speculative security layers. Preserve actual user behavior, not obsolete implementation. Git history keeps old code.

## Code reduction is a design requirement

- **Fewer production lines for equivalent behavior is a primary quality metric.** Prefer the smallest direct algorithm and shortest call chain. LOC is not permission to hide complexity in dense one-liners, delete meaningful tests or omit necessary error handling.
- Before adding code, trace the **full production path** from input to user-visible effect on **both** platforms. Identify the single owner of each decision, duplicated branches, state mirrors, thin wrappers, dead functions and unused contracts. Check actual callers, not merely imports or shared filenames.
- **Delete before abstracting.** Inline one-caller wrappers that only forward, negate a flag, return constants or reformat an error. Remove the old branch, tests for deleted-only abstractions and unused fields when migrating ownership. Do not introduce Manager/Controller/Coordinator, new interfaces or extra IPC unless a concrete production need cannot be expressed directly.
- Share **business decisions**, not OS API calls. Do not force different native APIs into a fake shared wrapper or keep duplicate host policies under a shared name. Count the production code actually compiled/linked; do not inflate a Go-sharing percentage with unused packages.
- Every PR reports **production LOC before/after, total diff (+/-), state/call-chain owners removed, and any necessary added lines**. Prefer net deletion. A positive diff requires specific behavior, test or correctness evidence; do not defend it by vague “architecture improvement.” No arbitrary LOC target may override correct behavior.

## Continuous PR pipeline

1. Select the smallest **independent, behavior-preserving deletion** or a specific cross-platform defect. Inspect real call sites and adjacent PRs; do not rewrite the whole architecture or mix unrelated changes.
2. Implement the final path and delete superseded code in the **same PR**. Keep Windows and macOS production paths aligned through Go; test real effects rather than constructing tests only to justify a wrapper.
3. Run relevant Go unit/race, Windows build/behavior tests, macOS ARM64/Intel Go+Swift builds and linked-ABI smoke tests. Follow CI failures to their cause; never skip or weaken checks. Physical microphone, Mouse4/5, clipboard, editor insertion and focus changes need **separate real-device acceptance**.
4. **Pipeline work:** while PR A waits for CI, start independent PR B on its own branch. Use stacked PRs only for real dependencies; show each PR's own diff and rebase/retarget after its parent merges. Avoid overlapping edits and redundant CI. Never merge a failing, stale or unchecked head.
5. Merge only after reviewing the exact final diff, regressions, current head SHA and required CI. Check main CI afterward. Record concise PR link, merge SHA, production/total LOC delta and outstanding physical-test gaps; do not claim success from compilation alone.
6. Continue to the next verified deletion or defect without waiting for a new planning instruction. **Stop making cosmetic PRs** when no meaningful redundancy or behavior issue remains; report the remaining acceptance work instead.

## CI, performance and documentation

- Use standard GitHub-hosted runners; no paid runners/services without approval. Scope CI to affected paths, cancel superseded runs, avoid duplicate builds and unnecessary artifacts/caches. Do not remove distinct functional or release checks just to reduce CI time.
- Measure user-perceived trigger→indicator, trigger→recording, release→visible text (P50/P95/failures) on actual devices; label synthetic CI results honestly. Do not claim latency or recognition gains without before/after evidence.
- Keep `README.md` and `ARCHITECTURE.md` concise and current, not a refactor diary. Update the architecture when ownership changes; keep issue history in GitHub, not source documents.
- Use `python3 scripts/check_headless_scope.py` and existing Swift native LOC guard; inspect real linked paths. Preserve native Swift compilation, Windows regressions, cancellation, clipboard races, Unicode and focus tests. Close issues only when their **behavioral** acceptance is complete.
