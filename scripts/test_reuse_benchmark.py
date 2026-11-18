"""Regression tests for the static cross-platform reuse benchmark."""
import json
import subprocess
import tempfile
import unittest
from pathlib import Path

import bench_reuse as b


class ReuseBenchmarkTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        subprocess.run(["git", "init", "-q"], cwd=self.root, check=True)
        subprocess.run(["git", "config", "user.email", "ci@example.invalid"], cwd=self.root, check=True)
        subprocess.run(["git", "config", "user.name", "CI Test"], cwd=self.root, check=True)
        self.go = "voice-rewrite/internal/core/ux/route.go"
        self.windows = "voice-rewrite/task_worker_windows.go"
        self.bridge = "voice-rewrite/cmd/voicebridge/main.go"
        self.swift = "macos/VoiceRewrite/App/WorkspaceModel.swift"
        self.native = "macos/Modules/Microphone/Sources/Microphone/MicrophoneRecorder.swift"
        self.catalog = {
            "schema_version": 1,
            "journeys": [{
                "id": "dictation",
                "title": "dictation",
                "steps": [
                    {
                        "id": "route",
                        "title": "transcript route",
                        "kind": "portable_decision",
                        "swift": {"path": self.swift, "contains": "if hasInsertion {"},
                        "go": [
                            {"path": self.go, "contains": "func RouteDictationTranscript("},
                            {"path": self.windows, "contains": "ux.RouteDictationTranscript("},
                            {"path": self.bridge, "contains": "ux.RouteDictationTranscript("},
                        ],
                    },
                    {
                        "id": "hardware",
                        "title": "native microphone",
                        "kind": "native_adapter",
                        "swift": {"path": self.native, "contains": "AVAudioEngine"},
                    },
                ],
            }],
        }
        self.write(self.swift, "if hasInsertion {\n}\n")
        self.write(self.native, "let recorder = AVAudioEngine()\n")
        self.write(self.windows, "// Windows host\n")
        self.write(self.bridge, "// macOS Go helper\n")
        self.write(self.go, "// Go policy not yet implemented\n")
        self.write("scripts/reuse_journeys.json", json.dumps(self.catalog))
        self.commit("baseline")
        self.base = subprocess.check_output(
            ["git", "rev-parse", "HEAD"], cwd=self.root, text=True
        ).strip()

    def write(self, path, content):
        full = self.root / path
        full.parent.mkdir(parents=True, exist_ok=True)
        full.write_text(content, encoding="utf-8")

    def commit(self, title):
        subprocess.run(["git", "add", "."], cwd=self.root, check=True)
        subprocess.run(["git", "commit", "-qm", title], cwd=self.root, check=True)

    def test_new_shared_go_owner_increases_score_not_native_ratio(self):
        before = b.run(b.Snapshot(self.root, self.base), self.catalog)
        self.assertEqual(before["total"]["shared_go_decisions"], 0)
        self.assertEqual(before["total"]["swift_policy_decisions"], 1)
        self.assertEqual(before["total"]["native_swift_boundaries"], 1)
        self.write(self.go, "func RouteDictationTranscript() {}\n")
        self.write(self.windows, "ux.RouteDictationTranscript()\n")
        self.write(self.bridge, "ux.RouteDictationTranscript()\n")
        after = b.run(b.Snapshot(self.root), self.catalog)
        self.assertEqual(after["total"]["go_portable_pct"], 100)
        self.assertFalse(b.compare(after, before))
        self.assertEqual(after["total"]["native_swift_boundaries"], 1)
        self.assertEqual(after["total"]["go_pct_delta"], 100)

    def test_bridge_only_does_not_count_as_shared(self):
        self.write(self.go, "func RouteDictationTranscript() {}\n")
        self.write(self.bridge, "ux.RouteDictationTranscript()\n")
        current = b.run(b.Snapshot(self.root), self.catalog)
        self.assertEqual(current["total"]["go_portable_pct"], 0)
        self.assertEqual(current["total"]["swift_policy_decisions"], 1)

    def test_regression_and_missing_source_fail_closed(self):
        self.write(self.go, "func RouteDictationTranscript() {}\n")
        self.write(self.windows, "ux.RouteDictationTranscript()\n")
        self.write(self.bridge, "ux.RouteDictationTranscript()\n")
        self.commit("shared-owner")
        before = b.run(b.Snapshot(self.root), self.catalog)
        self.write(self.bridge, "// removed ownership\n")
        after = b.run(b.Snapshot(self.root), self.catalog)
        self.assertTrue(b.compare(after, before))
        self.write(self.swift, "// fallback missing\n")
        with self.assertRaisesRegex(b.BenchmarkError, "unmapped portable"):
            b.run(b.Snapshot(self.root), self.catalog)

    def test_swift_duplicate_blocks_false_go_credit(self):
        self.catalog["journeys"][0]["steps"][0]["go_forbidden_when"] = [
            {"path": self.swift, "contains": "func modelReply("}
        ]
        self.write(self.swift, "if hasInsertion {\nfunc modelReply() {}\n")
        self.write(self.go, "func RouteDictationTranscript() {}\n")
        self.write(self.windows, "ux.RouteDictationTranscript()\n")
        self.write(self.bridge, "ux.RouteDictationTranscript()\n")
        self.assertEqual(
            b.run(b.Snapshot(self.root), self.catalog)["total"]["go_portable_pct"], 0
        )
        self.write(self.swift, "if hasInsertion {\n")
        self.assertEqual(
            b.run(b.Snapshot(self.root), self.catalog)["total"]["go_portable_pct"], 100
        )


if __name__ == "__main__":
    unittest.main()
