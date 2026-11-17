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

- **One product, two native hosts.** Windows and macOS share feature coverage, settings, labels, defaults, UI structure/order and workflows. Native look and platform APIs may differ, not intended behavior.
- **Change once, improve both.** Go Core or generated contracts own portable audio/ASR/LLM, text routing, task/delivery rules, configuration, UI copy, layout and state transitions. A typical business/UI change needs one shared edit.
- **Split only at OS boundaries.** Windows owns Win32/UIA/WASAPI, hooks, DPAPI and rendering; macOS owns SwiftUI/AppKit, AVAudioEngine, AX/Quartz, pasteboard and Keychain. Native adapters execute OS calls, not portable algorithms.
- **Prevent drift and regressions.** Generate Swift contracts from authoritative Go sources; do not maintain parallel rules or use synchronous per-frame IPC. Test both adapters and Go Core; protect working Windows behavior.

## Final-state only

- **No legacy obligations.** No user base requires old internal APIs, configs or workflows. Preserve working UX, not obsolete code; choose the clean final design.
- Delete duplicate state machines, transitional shims, obsolete functions, dead tests/assets and abandoned paths. A deliberate current input alternative is not a legacy compatibility shim.
- Keep one owner per feature. Update all call sites, tests and docs and remove superseded code together. Git history stores old implementations; `main` describes only the current design.

## Documentation and execution

- Document current usage, builds and limits, never a diary, versioned `README-V*.md` / `ARCHITECTURE-V*.md`, migrations, rejected approaches or interim-process commentary.
- Inspect current code, implement the end state, delete old paths and test. Prefer one PR per coherent task; avoid parallel implementations and procedural reports.
- Run Go unit/race, contract generation and Windows/macOS Apple Silicon and Intel Swift/Xcode CI as relevant. Test clipboard races, rapid dictation, focus/permission changes, cancellation, browser editors and Unicode.
- CI cannot prove physical mic, mouse side buttons, permissions or insertion into third-party editors. Track device acceptance separately; never call a build user-ready or claim confirmed insertion without evidence.
- Close completed/superseded issues and consolidate unfinished scope without claiming delivery. Report PR, merge SHA, CI and blockers concisely in the conversation, not source documents.

Do not weaken credential security or cancellation to mask broken input; remove needless dictation gates rather than normalizing manual copying.
