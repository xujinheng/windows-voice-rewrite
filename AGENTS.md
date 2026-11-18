# Repository instructions for coding agents

This file describes the **current product**, not its history. Read `voice-rewrite/ARCHITECTURE.md` and inspect latest `main`, open PRs, CI and issues before editing.

## Product priorities

- **Usability first.** VoiceRewrite must reliably insert speech-derived text into ordinary editors. Fix input failures before Meeting expansion, polish or abstract refactoring. A green build is not a working app.
- **Learn from working products.** Preserve Windows Mouse4 dictation, Mouse5 rewrite, shortcuts and result cards. Study Handy's native macOS paste/clipboard mechanisms without copying its stack; replace flawed code rather than defend it.
- **Dictation favors compatibility.** Remember the original app, release trigger keys and send one native paste. Incomplete AX text/selection attributes or notifications must not alone block insertion. Use Unicode only when clipboard paste cannot be prepared, not because another paste is pending.
- **Selected-text rewriting is distinct.** Verify intent, selection, target and task before replacement. Derive missing browser selections from verified AXValue and UTF-16 range when possible; otherwise preview. Do not impose rewrite-grade AX checks on ordinary dictation.
- **Make delivery reliable and honest.** Serialize paste leases, restore only owned clipboard data, bound waits, honor cancellation, never retry a possibly delivered paste. Distinguish ASR success, event sent, confirmed insertion and uncertain outcome; sent is not confirmed.
- **Minimize permissions and setup.** Require only necessary OS access, explain denial recovery and use stable signing/notarization for distribution. AX text availability is not required for a normal paste.

## Cross-platform product principles

- **One Go Core, two deliberately different hosts.** Windows keeps its full Win32 feature set. macOS is a minimal headless NSStatusItem dictation/rewrite host; it intentionally has no settings GUI, Meeting, history or result cards.
- **Change once, improve both.** Go Core or generated contracts own portable audio/ASR/LLM, text routing, task/delivery rules, configuration, UI copy, layout and state transitions. A typical business/UI change needs one shared edit.
- **Split only at OS boundaries.** Windows owns Win32/UIA/WASAPI, hooks, DPAPI and rendering; macOS owns the single Swift AppKit/AVAudioEngine/AX/Quartz/pasteboard adapter and reads a JSON config; no second business model or Keychain settings screen. Native adapters execute OS calls, not portable algorithms.
- **Prevent drift and regressions.** Keep both macOS native Swift adapter files thin; do not introduce parallel rules or synchronous per-frame IPC. Test both adapters and Go Core; protect working Windows behavior.
- **Prove linked source ownership, not inflated repository percentages.** Run `python3 scripts/check_headless_scope.py` after Go setup. Count only Go files linked into the headless Go bridge plus both native Swift
  boundary files. Never keep unreferenced Meeting Go packages merely to maintain
  a Go percentage; check the actual shared task entrypoints and native LOC cap. Preserve real Windows Go tests and macOS Swift compilation checks. This static metric is not dynamic runtime ownership; verify actual ASR and paste paths on real devices.

## Strict Go-first ownership

- **macOS Swift is an OS adapter, not another business app.** Keep only AVAudioEngine/CoreAudio capture, Quartz/HID events, AX selection/focus, AppKit status and rendering, clipboard delivery, and their OS-specific safety/cancellation handling in Swift. Shared Go owns config defaults/validation, transcription, rewrite, route decisions, status labels, palette, geometry and presentation timings.
- **Reuse existing Windows Go Core first.** Before adding a macOS behavior, locate its existing Go implementation. Extend that shared owner rather than adding a Swift policy layer or a second controller/state machine.
- **Render natively from one Go specification.** macOS may read a small validated Go startup profile; do not maintain a second list of UI colors, strings, sizes or delays, and never add per-frame IPC. Platform-specific pixels/window APIs remain native and cannot be copied verbatim from Windows.
- **Delete duplicates instead of adding wrappers.** Each migration must remove the superseded Swift decisions, protect Windows call chains, retain clipboard/focus/cancellation guarantees, and show actual before/after native LOC. No Go/Swift reuse percentage or pixel-perfect parity claim without runtime evidence.

## Final-state only

- **No legacy obligations.** No user base requires old internal APIs, configs or workflows. Preserve working UX, not obsolete code; choose the clean final design.
- Delete duplicate state machines, transitional shims, obsolete functions, dead tests/assets and abandoned paths. A deliberate current input alternative is not a legacy compatibility shim.
- Keep one owner per feature. Update all call sites, tests and docs and remove superseded code together. Git history stores old implementations; `main` describes only the current design.
- **No speculative AI scaffolding.** Inline a single-caller wrapper that only negates flags, returns a constant struct, or translates one boolean into a string/error; do not write tests solely to legitimize that wrapper. Avoid hypothetical interfaces, extra controllers and redundant platform logic. Keep actual cancellation, privacy, clipboard, concurrency and OS security checks; test the real effect at its boundary.

## CI cost and execution efficiency

- **Zero-cost CI by default.** This repository is public. Use only standard GitHub-hosted runners (currently `ubuntu-latest`, `windows-latest`, `macos-15`, `macos-15-intel`). Do not introduce larger/billable runners, paid external CI services, paid test APIs, or storage-increasing settings without the repository owner's explicit approval. Public-repo standard runner compute is free; artifact/cache storage can still have billing limits, so do not claim the entire workflow is unconditionally free.
- **One build or test per necessary verification scope.** Before editing workflows, inspect all `.github/workflows/*` triggers, job matrices, test filters and build commands. Eliminate duplicate `go test`, `swift test`, `xcodebuild`, packaging, and screenshot generation when the same platform, source and test/evidence purpose are already covered. Prefer adding relevant checks to an existing platform build job instead of creating another expensive macOS matrix. Preserve genuinely distinct functional, visual, architectural and real-device evidence.
- **Make CI work proportional to changes.** Use scoped `paths` filters when safe, cancel superseded PR runs using `concurrency`, retain necessary release/tag builds, and avoid building installers/releases on ordinary PRs. Keep required branch-protection checks reliable: a skipped optional workflow must not strand a required check.
- **Cache selectively, measure results.** Reuse `actions/setup-go` module/build caching with the correct module dependency path. Only add SwiftPM/Xcode/Python caches when their measured save exceeds download/upload cost; do not upload huge DerivedData or build directories by default. Never reuse architecture-specific native build products across Intel/ARM or Windows/macOS, and do not cache secrets.
- **Bound Actions storage.** Set explicit short `retention-days` (normally 7 days for screenshots/diagnostics; extend only when needed), upload small structured performance reports instead of redundant full apps, and review artifact/cache sizes and retention before adding new CI evidence.
- **Performance means user-perceived latency.** For dictation, measure key/button trigger to indicator displayed, trigger to recording ready, and release to inserted visible text, including P50/P95/worst case, failures and timeouts. Run deterministic CI journey tests through production state transitions, reuse existing builds, and label synthetic microphone/ASR/editor stages honestly. Do not confuse CPU microbenchmarks with real HID, CoreAudio, network ASR or cross-app insertion acceptance.
- **Report actual impact.** For CI changes, state which duplicate work was removed or reused, what caches/storage settings changed, and whether latest relevant CI passed. Do not claim a performance improvement without measured before/after evidence. If cost or runner eligibility is uncertain, verify current GitHub billing rules before making a change.

## Documentation and execution

- Document current usage, builds and limits, never a diary, versioned `README-V*.md` / `ARCHITECTURE-V*.md`, migrations, rejected approaches or interim-process commentary.
- Inspect current code, implement the end state, delete old paths and test. Prefer one PR per coherent task; avoid parallel implementations and procedural reports.
- Run Go unit/race and Windows native UI/Meeting regressions plus macOS Apple
  Silicon/Intel direct Swift compilation as relevant; do not resurrect the
  deleted Xcode project or generated Swift contracts. Test clipboard races, rapid dictation, focus/permission changes, cancellation, browser editors and Unicode.
- CI cannot prove physical mic, mouse side buttons, permissions or insertion into third-party editors. Track device acceptance separately; never call a build user-ready or claim confirmed insertion without evidence.
- Close completed/superseded issues and consolidate unfinished scope without claiming delivery. Report PR, merge SHA, CI and blockers concisely in the conversation, not source documents.

Do not weaken credential security or cancellation to mask broken input; remove needless dictation gates rather than normalizing manual copying.
