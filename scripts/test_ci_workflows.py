"""CI invariants for the Go-first macOS app (no Xcode project or UI duplication)."""
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[1]
WORKFLOWS = ROOT / ".github/workflows"

class HeadlessWorkflowTests(unittest.TestCase):
    def test_pr_builds_both_mac_architectures_once(self):
        source = (WORKFLOWS / "build.yml").read_text()
        self.assertEqual(source.count("  build-macos-headless:\n"), 1)
        job = source.split("  build-macos-headless:\n", 1)[1].split("\n  build:\n", 1)[0]
        self.assertIn("macos-15, arch: arm64", job)
        self.assertIn("macos-15-intel, arch: x86_64", job)
        self.assertIn("bash scripts/build-macos-headless.sh", job)
        self.assertIn("cat scripts/test-macos-focus.swift >>", job)
        self.assertIn("go test -race -count=1 ./...", source)
        self.assertTrue((ROOT / "scripts/test-macos-focus.swift").is_file())
        self.assertNotIn("cmd/contractgen", (WORKFLOWS / "nightly.yml").read_text())

    def test_ready_and_distribution_build_same_app(self):
        for filename in ("ready-build.yml", "nightly.yml", "macos-distribution.yml"):
            with self.subTest(filename=filename):
                source = (WORKFLOWS / filename).read_text()
                self.assertIn("scripts/build-macos-headless.sh", source)
                self.assertNotIn("xcodebuild", source)
        release = (WORKFLOWS / "macos-distribution.yml").read_text()
        self.assertIn("codesign --force --timestamp --options runtime", release)
        ready = (WORKFLOWS / "ready-build.yml").read_text()
        self.assertIn("github.event.changes.title", ready)

    def test_macos_artifacts_ship_existing_verified_dmg_packaging(self):
        ready = (WORKFLOWS / "ready-build.yml").read_text()
        main = (WORKFLOWS / "build.yml").read_text()
        for workflow in (ready, main):
            with self.subTest(workflow="ready" if workflow is ready else "main"):
                self.assertIn("bash scripts/package-macos-dmg.sh", workflow)
                self.assertIn("bash scripts/verify-macos-package.sh dmg", workflow)
                self.assertNotIn("path: ${{ runner.temp }}/ready/VoiceRewrite.app", workflow)
                self.assertNotIn("path: ${{ runner.temp }}/headless/VoiceRewrite.app", workflow)
        self.assertIn("VoiceRewrite-Ready-macOS-${{ matrix.arch }}.dmg", ready)
        self.assertNotIn("VoiceRewrite-Ready-macOS-${{ matrix.arch }}.zip", ready)
        self.assertIn("asset: VoiceRewrite-Mac}", main)
        self.assertIn("asset: VoiceRewrite-Mac-Intel}", main)
        self.assertEqual(main.count("name: ${{ matrix.asset }}"), 2)
        self.assertNotIn("&& '' || '-Intel'", main)
        self.assertIn("Package experimental Mac DMG for tagged Release", main)
        self.assertIn("bash scripts/verify-macos-package.sh dmg", main)

    def test_release_has_one_app_per_platform(self):
        workflow = (WORKFLOWS / "build.yml").read_text()
        self.assertIn("release/VoiceRewrite-Windows.exe", workflow)
        self.assertIn("release/VoiceRewrite-Mac*.dmg", workflow)
        self.assertIn("name: VoiceRewrite-Windows", workflow)
        self.assertNotIn("build-oneclick:", workflow)
        self.assertNotIn("VoiceRewrite-Setup-", workflow)
        self.assertNotIn("oneclick/package/", workflow)
        self.assertIn("gh api -X POST", workflow)
        for path in (ROOT / "README.md", ROOT / "README.en.md"):
            readme = path.read_text(encoding="utf-8")
            self.assertIn("docs/demo/voicerewrite-demo.mp4", readme)

    def test_native_sources_and_user_path(self):
        sources = [ROOT / "clients/macos/app/main.swift",
                   ROOT / "clients/macos/app/RecordingPill.swift"]
        for path in sources:
            self.assertTrue(path.is_file())
            self.assertNotIn("import SwiftUI", path.read_text())
        nonblank = sum(sum(bool(line.strip()) for line in path.read_text().splitlines())
                       for path in sources)
        self.assertGreater(nonblank, 0)
        script = (ROOT / "scripts/build-macos-headless.sh").read_text()
        for path in sources:
            self.assertIn(path.relative_to(ROOT).as_posix(), script)
        pill = sources[1].read_text()
        host = sources[0].read_text()
        self.assertIn("styleMask: [.borderless, .nonactivatingPanel]", pill)
        self.assertIn("-buildmode=c-archive", script)
        self.assertNotIn("Process()", host)
        self.assertNotIn("voicebridge", host)
        self.assertTrue((ROOT / "scripts/check_headless_scope.py").is_file())

    def test_nightly_obeys_core_and_client_module_boundaries(self):
        nightly = (WORKFLOWS / "nightly.yml").read_text()
        self.assertIn("cache-dependency-path: core/go.mod", nightly)
        self.assertIn("working-directory: core\n        run: go test -race -count=3", nightly)
        self.assertIn("working-directory: clients\n        run: go test -race -count=1 ./macos/bridge", nightly)
        self.assertIn("Push-Location clients/windows", nightly)
        self.assertNotIn("cmd/voicebridge", nightly)
        self.assertNotIn("internal/core", nightly)
        self.assertNotIn("Push-Location voice-rewrite", nightly)
        visual = (WORKFLOWS / "visual-matrix.yml").read_text()
        for package in ("settingsform", "popup", "indicator", "desktop", "contract", "ux"):
            self.assertIn(f"'core/{package}/**'", visual)

if __name__ == "__main__":
    unittest.main()
