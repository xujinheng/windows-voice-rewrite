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
reusable jobs by the unified CI workflow for affected PRs and `main` pushes.
They are part of **CI Gate** on PRs when relevant; there is no second
independent Direct2D push workflow. Unrelated PRs do not launch those jobs. Both main pushes and version tags run
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

Adding subsequent commits does **not** rerun the ready-build workflow;
remove and re-add the `[ready-to-test]` title marker to request fresh artifacts.
Ordinary PR label events do not launch empty build runs. Alternatively, use
`workflow_dispatch` against the PR branch to build its head on demand.
Always confirm the workflow's recorded source SHA matches the desired PR
version; a passing CI Gate is still required for release/merge readiness.

Each push to `main` publishes one Windows EXE and one VoiceRewrite app-bundle ZIP per macOS
architecture. The same existing ARM64/Intel native jobs also compile the separate
Mac OneClick launcher and upload one architecture-checked, ad-hoc signed **Dev** app ZIP
for each; `[ready-to-test]` PRs can fetch these launchers as well. This is not
Developer ID signing or Apple notarization. **Only version tags** produce the full Windows installer, Windows
portable archive and macOS developer ZIP/DMG release assets. The separately
signed/notarized release pipeline is unchanged.

Native macOS Swift adapter suites, real-macOS Go Core tests, two Darwin
cross-build checks, and the Swift-to-Go handshake share a separate required
**parallel** job. The independent Microphone, Storage and Interaction Swift
packages execute concurrently on that runner with one compiler job apiece,
retain separate failure logs and require at least 17, 12 and 95 passing tests
respectively (124 in total). Both Xcode app test jobs run concurrently with that job, so
the ARM Xcode runner no longer waits for the independent adapter and bridge
suites. All jobs still participate in the final CI Gate.

Intel native tests use an exact Go 1.23.12 toolchain. A version-and-architecture
keyed Actions cache keeps the complete installed official Go toolchain
(including setup-go's completion marker); cache misses fall back to normal
official setup-go installation. This caches only the reusable toolchain, not
Xcode test results, and does not bypass compilation or validation.

PR and `main` Intel and ARM Xcode builds restore architecture-separated `DerivedData/Build`
directories using exact Xcode toolchain/project/package cache keys and fixed
runner paths. Before
restoration, `scripts/ci_xcode_mtimes.py` assigns content-derived nanosecond
timestamps to tracked regular files. Changed sources therefore get different
timestamps and must rebuild; unchanged Swift objects can be reused. The full native ARM and Intel XCTest schemes are executed on every selected
PR and `main` push, each with a minimum 69-passing-tests log assertion, architecture-specific
app/helper checks and unchanged visual evidence uploads. Coverage assertions count actual passing tests even when XCTest reports optional skipped tests, while rejecting fewer than 69 passes or any unsuccessful XCTest run. `xcresult` and XCTest pass/fail outputs
are not in the saved cache. These caches are enabled on pull requests **and main pushes**, with the same
content-based invalidation and full XCTest execution. GitHub keeps branch cache
scopes separate: the first main build can be cold, while later main builds can
reuse compiled products. **Version tags remain cold, clean Xcode builds** to
avoid reusing non-release Build outputs in release packaging. Cache-hit
and actual Xcode seconds are reported for PR and main jobs in the job summary. Any toolchain
fingerprint or project/package graph change forces a new cache.

## Opening side-by-side visual comparisons

GitHub Actions artifacts are downloads, not a web-hosted PNG directory. In the
cross-platform comparison Job Summary, use **Download all side-by-side
comparisons**, unzip the `acceptance-comparison` artifact and open `index.html`.
Every "Compare side by side" relative PNG link also works from the downloaded
`report.md`. Do **not** link to `/actions/runs/<png-filename>`: that path is
invalid and produces a 404. The Job Summary links to the real upload-artifact
`artifact-url`, which requires GitHub login and expires with retention.

Ready-to-test macOS builds reuse the same PR-scoped DerivedData/Build cache
as required ARM and Intel XCTest jobs, with the same exact Xcode toolchain,
project/package fingerprint and content-derived tracked source timestamps.
Every ready build still executes native xcodebuild, recompiles/signs its bundled
Go helper, verifies native app/helper architecture and signature, and checks
the ZIP. Neither XCTest results nor signed release assets are cached.
Intel ready builds also reuse the official Go toolchain cache. Cache hit and
Xcode build seconds are recorded in each ready job summary.

## Workflow ownership and main-branch fan-out

The default branch has one continuous validation workflow: `build.yml`.
It owns platform compilation, shared Go and Swift tests, UI comparison,
Direct2D acceptance, PR CI Gate, and tagged releases. The specialized
`acceptance-evidence.yml`, `direct2d-result.yml` and
`macos-distribution.yml` are invoked as reusable jobs; direct invocation
is reserved for manual diagnostics. A `main` push that edits Direct2D code
runs its acceptance **inside the main CI**, not as a competing push run.

The README media pipeline has one primary automatic chain:
`visual-matrix.yml` (rebuild/capture/publish screenshots) followed by
`demo-video.yml` (render video on successful completion). Changes to the
video renderer or its workflow also trigger the screenshot workflow so
there is no overlapping video push run for those changes. Direct edits to
published `docs/ui/*.png` images still trigger video rendering via a narrow
push path filter; automated screenshot commits made with `GITHUB_TOKEN` do
not cause a duplicate push event. A manual demo-only regeneration is
available via `workflow_dispatch`.
The screenshots workflow retains its own write permission, bounded
artifacts and main-head race check. The video workflow likewise checks
that the source branch is main and that main has not moved before it
publishes. Generated media commits made with `GITHUB_TOKEN` do not
trigger recursive CI runs.

`ready-build.yml` accepts only an explicit title-edit marker or manual
workflow dispatch. Labeling a PR (including Dependabot's automatic labels)
no longer enqueues skipped ready-build runs. GitHub still retains historical
workflow names in its Actions UI; removing an old YAML file does not delete
past run records.

## Checkout credentials and CI coverage

Non-publishing CI, reusable acceptance, nightly and developer-build jobs use
`actions/checkout` with `persist-credentials: false`. Code under test cannot
read GitHub's checkout credential from the local Git configuration. The
`changes` job still uses its separate, read-only `GH_TOKEN` specifically
for GitHub's pull-request/compare APIs. The README screenshot and video
workflows are the explicit exceptions: they commit generated media to
`main`, retain `contents: write` only in their publishing workflows, and
verify that `main` has not advanced before publishing.

The PR path classifier treats workflow code, reusable actions, Dependabot
configuration and CI policy regression tests as cross-platform changes.
Changes to `ci_xcode_mtimes.py` and `ci_xctest_coverage.py` trigger both
macOS ARM/Intel native XCTest jobs, even when no application source changed.
Fast Python regression tests check both path-routing and checkout token
policy on every PR. Ordinary documentation edits remain lightweight.

## Release-only provenance permissions

Windows compilation and OneClick jobs now use only `contents: read` for
ordinary PRs and main builds. The release-tag-only
`attest-windows-release` job is the **sole** Windows provenance job granted
`id-token: write` and `attestations: write`. It downloads the three
release build outputs from the **same workflow run**, checks the portable
and installer SHA-256 manifests, and attests the exact portable archive,
installer and OneClick executable. The curated GitHub Release cannot be
published until these attestations succeed. Native source compilation,
release packages and PR CI Gate tests are unchanged.

This extra standard Ubuntu runner is used **only for version tags**, not PRs
or main pushes. No new permanent artifacts are uploaded by the provenance
job. The tagged release route is guarded by static CI policy tests; a
successful PR does not replace a separate release-tag smoke test.
