"""Regression tests for GitHub Actions platform routing."""
import unittest
from ci_changes import classify


class ClassifyTests(unittest.TestCase):
    def test_docs_only(self):
        self.assertEqual(classify(["README.md", "docs/ui/screen.png"]), {
            "core": False, "macos": False, "windows": False, "ui": False, "direct2d": False})

    def test_svg_icon_rebuilds_macos(self):
        affected = classify(["assets/VoiceRewrite.svg"])
        self.assertTrue(affected["macos"])
        self.assertFalse(affected["windows"])

    def test_native_macos(self):
        self.assertEqual(classify(["clients/macos/app/main.swift"]), {
            "core": False, "macos": True, "windows": False, "ui": False, "direct2d": False})

    def test_shared_go(self):
        self.assertEqual(classify(["core/meeting/session.go"]), {
            "core": True, "macos": False, "windows": True, "ui": True, "direct2d": False})

    def test_windows_only_native_code(self):
        selected = classify(["clients/windows/task_worker_windows.go"])
        self.assertEqual(selected, {
            "core": False, "macos": False, "windows": True,
            "ui": False, "direct2d": False})

    def test_windows_host_without_filename_suffix(self):
        selected = classify(["clients/windows/windows.go"])
        self.assertEqual(selected, {
            "core": False, "macos": False, "windows": True,
            "ui": False, "direct2d": True})

    def test_voicebridge_only(self):
        selected = classify(["clients/macos/bridge/main.go"])
        self.assertEqual(selected, {
            "core": True, "macos": True, "windows": False,
            "ui": False, "direct2d": False})

    def test_shared_asrtext_rebuilds_both_hosts(self):
        selected = classify(["core/asrtext/clean.go"])
        self.assertEqual(selected, {
            "core": True, "macos": True, "windows": True,
            "ui": False, "direct2d": False,
        })

    def test_independent_cli(self):
        self.assertEqual(classify(["clients/cli/main.go"]), {
            "core": True, "macos": False, "windows": False,
            "ui": False, "direct2d": False})

    def test_local_asr_in_core(self):
        self.assertEqual(classify(["core/localasr/deploy.go"]), {
            "core": True, "macos": True, "windows": True, "ui": False, "direct2d": False})

    def test_workflow_change_rechecks_everything(self):
        for path in (".github/workflows/build.yml", ".github/actions/setup/action.yml",
                     ".github/dependabot.yml", "scripts/test_ci_workflows.py",
                     "scripts/test_ci_trigger_ownership.py"):
            with self.subTest(path=path):
                self.assertTrue(all(classify([path]).values()))

    def test_mixed_change(self):
        self.assertEqual(classify(["clients/macos/app/main.swift", "core/localasr/deploy.go"]),
                         {"core": True, "macos": True, "windows": True, "ui": False, "direct2d": False})

    def test_ui_acceptance(self):
        self.assertTrue(classify(["core/indicator/indicator.go"])["ui"])
        self.assertTrue(classify(["clients/windows/settings_windows.go"])["ui"])
        self.assertFalse(classify(["clients/macos/app/RecordingPill.swift"])["ui"])

    def test_windows_only_ui_does_not_build_mac(self):
        routed = classify(["clients/windows/settings_windows.go"])
        self.assertTrue(routed["ui"])
        self.assertTrue(routed["windows"])
        self.assertFalse(routed["macos"])
        self.assertFalse(routed["core"])

    def test_headless_mac_adapter_does_not_schedule_windows_ui(self):
        routed = classify(["clients/macos/app/RecordingPill.swift"])
        self.assertFalse(routed["ui"])
        self.assertTrue(routed["macos"])
        self.assertFalse(routed["windows"])
        
    def test_direct2d_acceptance(self):
        self.assertTrue(classify(["core/popup/layout.go"])["direct2d"])
        self.assertFalse(classify(["core/meeting/session.go"])["direct2d"])

    def test_headless_scope_change_runs_real_audit(self):
        self.assertEqual(classify(["scripts/check_headless_scope.py"]), {
            "core": True, "macos": True, "windows": False,
            "ui": False, "direct2d": False,
        })

    def test_portable_boundary(self):
        self.assertTrue(classify(["scripts/check-portable-core.py"])["core"])


if __name__ == "__main__":
    unittest.main()
