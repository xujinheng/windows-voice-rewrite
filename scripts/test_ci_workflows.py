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
        self.assertNotIn("xcodebuild", source)
        self.assertNotIn("test-swift-modules", source)
        self.assertIn("needs.build-macos-headless.result", source)
        self.assertIn("go test -race -count=1 ./...", source)

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

    def test_source_budget_and_user_path(self):
        sources = [ROOT / "macos/Headless/main.swift",
                   ROOT / "macos/Headless/RecordingPill.swift"]
        for path in sources:
            self.assertTrue(path.is_file())
            self.assertNotIn("import SwiftUI", path.read_text())
        nonblank = sum(sum(bool(line.strip()) for line in path.read_text().splitlines())
                       for path in sources)
        self.assertLessEqual(nonblank, 1000)
        script = (ROOT / "scripts/build-macos-headless.sh").read_text()
        for path in sources:
            self.assertIn(path.relative_to(ROOT).as_posix(), script)
        pill = sources[1].read_text()
        host = sources[0].read_text()
        self.assertIn("styleMask: [.borderless, .nonactivatingPanel]", pill)
        self.assertIn("width: 232, height: 50", pill)
        self.assertIn("width: CGFloat = view.phase == .recording ? 232 : 252", pill)
        self.assertIn("self?.cancelFromPill()", host)
        self.assertIn("pill.recording(", host)
        self.assertIn("pill.processing()", host)
        self.assertIn("pill.result(.sent)", host)
        self.assertIn("pill.dismiss()", host)
        self.assertTrue((ROOT / "macos/Headless/config.example.json").is_file())
        self.assertTrue((ROOT / "scripts/check_headless_scope.py").is_file())

if __name__ == "__main__":
    unittest.main()
