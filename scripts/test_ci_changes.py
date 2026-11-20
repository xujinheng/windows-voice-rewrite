"""Regression tests for GitHub Actions platform routing."""
import unittest
from ci_changes import classify


class ClassifyTests(unittest.TestCase):
    def test_docs_only(self):
        self.assertEqual(classify(["README.md", "docs/ui/screen.png"]), {
            "core": False, "macos": False, "windows": False, "oneclick": False, "ui": False, "direct2d": False})

    def test_native_macos(self):
        self.assertEqual(classify(["macos/Headless/main.swift"]), {
            "core": False, "macos": True, "windows": False, "oneclick": False, "ui": False, "direct2d": False})

    def test_shared_go(self):
        self.assertEqual(classify(["voice-rewrite/internal/core/meeting/meeting.go"]), {
            "core": True, "macos": False, "windows": True, "oneclick": False, "ui": True, "direct2d": False})

    def test_windows_only_native_code(self):
        selected = classify(["voice-rewrite/task_worker_windows.go"])
        self.assertEqual(selected, {
            "core": False, "macos": False, "windows": True,
            "oneclick": False, "ui": False, "direct2d": False})

    def test_windows_host_without_filename_suffix(self):
        selected = classify(["voice-rewrite/windows.go"])
        self.assertEqual(selected, {
            "core": False, "macos": False, "windows": True,
            "oneclick": False, "ui": False, "direct2d": True})

    def test_root_main_package_does_not_link_into_mac(self):
        for path in ("voice-rewrite/main.go", "voice-rewrite/config_store.go",
                     "voice-rewrite/meeting_state.go"):
            with self.subTest(path=path):
                selected = classify([path])
                self.assertFalse(selected["core"])
                self.assertFalse(selected["macos"])
                self.assertTrue(selected["windows"])
        # Module dependencies and unsuffixed tests still follow the broad conservative scope.
        self.assertTrue(classify(["voice-rewrite/go.mod"])["macos"])
        self.assertTrue(classify(["voice-rewrite/main_test.go"])["core"])

    def test_voicebridge_only(self):
        selected = classify(["voice-rewrite/cmd/voicebridge/main.go"])
        self.assertEqual(selected, {
            "core": True, "macos": True, "windows": False,
            "oneclick": False, "ui": False, "direct2d": False})

    def test_shared_asrtext_rebuilds_oneclick_too(self):
        selected = classify(["voice-rewrite/asrtext/clean.go"])
        self.assertEqual(selected, {
            "core": True, "macos": True, "windows": True, "oneclick": True,
            "ui": False, "direct2d": False,
        })

    def test_oneclick(self):
        self.assertEqual(classify(["oneclick/main.go"]), {
            "core": False, "macos": False, "windows": False, "oneclick": True, "ui": False, "direct2d": False})

    def test_windows_installer(self):
        self.assertEqual(classify(["installer/VoiceRewrite.iss"]), {
            "core": False, "macos": False, "windows": True, "oneclick": False, "ui": False, "direct2d": False})

    def test_workflow_change_rechecks_everything(self):
        for path in (".github/workflows/build.yml", ".github/actions/setup/action.yml",
                     ".github/dependabot.yml", "scripts/test_ci_workflows.py",
                     "scripts/test_ci_trigger_ownership.py"):
            with self.subTest(path=path):
                self.assertTrue(all(classify([path]).values()))

    def test_mixed_change(self):
        self.assertEqual(classify(["macos/Headless/main.swift", "oneclick/config.example.json"]),
                         {"core": False, "macos": True, "windows": False, "oneclick": True, "ui": False, "direct2d": False})

    def test_ui_acceptance(self):
        self.assertTrue(classify(["voice-rewrite/internal/core/indicator/indicator.go"])["ui"])
        self.assertTrue(classify(["voice-rewrite/settings_windows.go"])["ui"])
        self.assertFalse(classify(["macos/Headless/RecordingPill.swift"])["ui"])

    def test_windows_only_ui_does_not_build_mac(self):
        routed = classify(["voice-rewrite/settings_windows.go"])
        self.assertTrue(routed["ui"])
        self.assertTrue(routed["windows"])
        self.assertFalse(routed["macos"])
        self.assertFalse(routed["core"])

    def test_headless_mac_adapter_does_not_schedule_windows_ui(self):
        routed = classify(["macos/Headless/RecordingPill.swift"])
        self.assertFalse(routed["ui"])
        self.assertTrue(routed["macos"])
        self.assertFalse(routed["windows"])
        self.assertFalse(routed["oneclick"])

    def test_direct2d_acceptance(self):
        self.assertTrue(classify(["voice-rewrite/popup_layout.go"])["direct2d"])
        self.assertFalse(classify(["voice-rewrite/internal/core/meeting/meeting.go"])["direct2d"])

    def test_headless_scope_change_runs_real_audit(self):
        self.assertEqual(classify(["scripts/check_headless_scope.py"]), {
            "core": True, "macos": True, "windows": False,
            "oneclick": False, "ui": False, "direct2d": False,
        })

    def test_portable_boundary(self):
        self.assertTrue(classify(["scripts/check-portable-core.py"])["core"])


if __name__ == "__main__":
    unittest.main()
