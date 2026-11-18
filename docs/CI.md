# Continuous integration

Each pull request runs `VoiceRewrite CI, builds and release`. Its path
classifier selects affected validation jobs. The stable `CI Gate` job fails
when any required job fails or is canceled; it passes only when all selected
jobs succeed. Configure **CI Gate** as the required CI check in branch
protection. Do **not** make path-filtered visual workflows required checks.

| PR change | Selected platform checks |
| --- | --- |
| Docs/README only | classifier and CI Gate |
| Portable/shared `voice-rewrite/` Go | Linux race, Windows, macOS ARM and Intel |
| Native `*_windows.go` only | Windows tests and GUI compile |
| `macos/` only | macOS ARM and Intel |
| `cmd/voicebridge/` only | Linux race and macOS ARM/Intel |
| `oneclick/` only | Windows OneClick |
| `installer/` only | Windows tests and GUI compile |
| Workflow/CI classifier | full validation matrix |

Specialized cross-platform UI and Direct2D acceptance are invoked as conditional
reusable jobs by the same PR workflow and are part of **CI Gate** when relevant.
Unrelated PRs do not launch those jobs. Both main pushes and version tags run
all platforms, ignoring PR change filters. A release tag validates the full
matrix, creates the curated Windows Release, and then calls the reusable signed
macOS distribution workflow. If signing credentials are absent, the macOS
notarized artifacts are skipped, not mislabeled as signed.

`nightly.yml` repeats race-enabled tests and native ARM/Intel/Windows suites
on standard GitHub-hosted runners. CI does not prove physical-keyboard delay,
USB/Bluetooth mouse latency, microphone permission or pasting into external
apps: device acceptance remains separate.

CI uses standard hosted runners only. Do not introduce larger runners, cloud
model API calls, or paid external services. Go caches and SwiftPM download
caches are scoped by runner and dependencies. Artifacts have bounded retention.
Track GitHub Actions job/runner minutes, cancel rate, cache hit rate, and
per-commit latency metrics; do not mistake synthetic-keyboard benchmarks for
physical button-down-to-visible latency.
