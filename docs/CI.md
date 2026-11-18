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

## Fast development and ready-to-test packages

Ordinary PR commits run only the affected tests and compile checks; they do
**not** generate download bundles. This keeps the merge gate fast.

When an agent has finished a PR and the head commit is ready for device testing,
edit the PR title to include `[ready-to-test]`. The **Ready-to-test builds**
workflow fires on that title edit and uploads one runnable payload per platform:
- `VoiceRewrite-Ready-Windows-x64`: only `VoiceRewrite.exe`.
- `VoiceRewrite-Ready-macOS-arm64-Dev`: one ZIP containing `VoiceRewrite.app`.
- `VoiceRewrite-Ready-macOS-x64-Dev`: one ZIP containing `VoiceRewrite.app`.

A macOS `.app` is a directory bundle. The smallest practical GitHub-download
artifact is a `ditto` ZIP preserving its executability, resources and ad-hoc
signature. These development apps are **not Apple-notarized**; the user may
need to explicitly allow opening the app. There are no DMGs, additional
portable Windows ZIPs, installers or duplicate checksum files in this workflow.

A `build:ready` PR label also triggers a one-off build when added, if the
repository has that label. Adding subsequent commits does **not** rerun the
ready-build workflow; remove/re-add the label or remove/re-add the title marker
to request newer artifacts. An explicit `workflow_dispatch` against a PR
branch can build its head on demand. Always confirm the workflow's recorded
source SHA matches the desired PR version; a passing CI Gate is still required
for release/merge readiness.

Each push to `main` publishes one Windows EXE and one app-bundle ZIP per macOS
architecture. **Only version tags** produce the full Windows installer, Windows
portable archive and macOS developer ZIP/DMG release assets. The separately
signed/notarized release pipeline is unchanged.

Native macOS Swift adapter suites are a separate required **parallel** job.
This eliminates their previous serial placement before the Xcode app tests,
without omitting the packages or weakening the final CI Gate.

## Opening side-by-side visual comparisons

GitHub Actions artifacts are downloads, not a web-hosted PNG directory. In the
cross-platform comparison Job Summary, use **Download all side-by-side
comparisons**, unzip the `acceptance-comparison` artifact and open `index.html`.
Every "Compare side by side" relative PNG link also works from the downloaded
`report.md`. Do **not** link to `/actions/runs/<png-filename>`: that path is
invalid and produces a 404. The Job Summary links to the real upload-artifact
`artifact-url`, which requires GitHub login and expires with retention.
