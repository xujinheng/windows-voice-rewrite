"""Regression tests for GitHub Actions platform routing."""
import unittest
from ci_changes import classify


class ClassifyTests(unittest.TestCase):
    def test_docs_only(self):
        self.assertEqual(classify(["README.md", "docs/ui/screen.png"]), {
            "core": False, "macos": False, "windows": False, "oneclick": False, "ui": False, "direct2d": False})

    def test_native_macos(self):
        self.assertEqual(classify(["macos/Modules/Microphone/Sources/Record.swift"]), {
            "core": False, "macos": True, "windows": False, "oneclick": False, "ui": False, "direct2d": False})

    def test_shared_go(self):
        self.assertEqual(classify(["voice-rewrite/internal/core/meeting/meeting.go"]), {
            "core": True, "macos": True, "windows": True, "oneclick": False, "ui": True, "direct2d": False)

    def test_oneclick(self):
        self.assertEqual(classify(["oneclick/main.go"]), {
            "core": False, "macos": False, "windows": False, "oneclick": True, "ui": False, "direct2d": False})

    def test_windows_installer(self):
        self.assertEqual(classify(["installer/VoiceRewrite.iss"]), {
            "core": False, "macos": False, "windows": True, "oneclick": False, "ui": False, "direct2d": False})

    def test_workflow_change_rechecks_everything(self):
        self.assertTrue(all(classify([".github/workflows/build.yml"]).values()))

    def test_mixed_change(self):
        self.assertEqual(classify(["macos/VoiceRewrite/App/Main.swift", "oneclick/go.mod"]),
                         {"core": False, "macos": True, "windows": False, "oneclick": True, "ui": True, "direct2d": False})

    def test_ui_acceptance(self):
        self.assertTrue(classify(["macos/VoiceRewrite/UI/RecordingIndicator.swift"])["ui"])
        self.assertTrue(classify(["voice-rewrite/settings_windows.go"])["ui"])
        self.assertFalse(classify(["macos/Modules/Microphone/Recorder.swift"])["ui"])

    def test_direct2d_acceptance(self):
        self.assertTrue(classify(["voice-rewrite/popup_layout.go"])["direct2d"])
        self.assertFalse(classify(["voice-rewrite/internal/core/meeting/meeting.go"])["direct2d"])

    def test_portable_boundary(self):
        self.assertTrue(classify(["scripts/check-portable-core.py"])["core"])


if __name__ == "__main__":
    unittest.main()
