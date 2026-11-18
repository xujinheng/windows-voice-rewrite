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
        self.write(self.windows, "package main\nfunc host() {}\n")
        self.write(self.bridge, "package main\nfunc bridge() {}\n")
        self.write(self.go, "package ux\n// policy pending\n")
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
        self.write(self.go, "package ux\nfunc RouteDictationTranscript(ok bool) bool { return ok && true }\n")
        self.write(self.windows, "package main\nimport \"voicerewrite/internal/core/ux\"\nfunc host() { _ = ux.RouteDictationTranscript(true) }\n")
        self.write(self.bridge, "package main\nimport \"voicerewrite/internal/core/ux\"\nfunc bridge() { _ = ux.RouteDictationTranscript(true) }\n")
        after = b.run(b.Snapshot(self.root), self.catalog)
        self.assertEqual(after["total"]["go_portable_pct"], 100)
        self.assertFalse(b.compare(after, before))
        self.assertEqual(after["total"]["native_swift_boundaries"], 1)
        self.assertEqual(after["total"]["go_pct_delta"], 100)

    def test_bridge_only_does_not_count_as_shared(self):
        self.write(self.go, "package ux\nfunc RouteDictationTranscript(ok bool) bool { return ok && true }\n")
        self.write(self.bridge, "package main\nimport \"voicerewrite/internal/core/ux\"\nfunc bridge() { _ = ux.RouteDictationTranscript(true) }\n")
        current = b.run(b.Snapshot(self.root), self.catalog)
        self.assertEqual(current["total"]["go_portable_pct"], 0)
        self.assertEqual(current["total"]["swift_policy_decisions"], 1)

    def test_regression_and_missing_source_fail_closed(self):
        self.write(self.go, "package ux\nfunc RouteDictationTranscript(ok bool) bool { return ok && true }\n")
        self.write(self.windows, "package main\nimport \"voicerewrite/internal/core/ux\"\nfunc host() { _ = ux.RouteDictationTranscript(true) }\n")
        self.write(self.bridge, "package main\nimport \"voicerewrite/internal/core/ux\"\nfunc bridge() { _ = ux.RouteDictationTranscript(true) }\n")
        self.commit("shared-owner")
        before = b.run(b.Snapshot(self.root), self.catalog)
        self.write(self.bridge, "package main\n// removed ownership\n")
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
        self.write(self.go, "package ux\nfunc RouteDictationTranscript(ok bool) bool { return ok && true }\n")
        self.write(self.windows, "package main\nimport \"voicerewrite/internal/core/ux\"\nfunc host() { _ = ux.RouteDictationTranscript(true) }\n")
        self.write(self.bridge, "package main\nimport \"voicerewrite/internal/core/ux\"\nfunc bridge() { _ = ux.RouteDictationTranscript(true) }\n")
        self.assertEqual(
            b.run(b.Snapshot(self.root), self.catalog)["total"]["go_portable_pct"], 0
        )
        self.write(self.swift, "if hasInsertion {\n")
        self.assertEqual(
            b.run(b.Snapshot(self.root), self.catalog)["total"]["go_portable_pct"], 100
        )


    def test_comments_and_string_literature_cannot_fake_go_calls(self):
        self.write(self.go, "package ux\nfunc RouteDictationTranscript(ok bool) bool { return !ok }\n")
        self.write(self.windows, 'package main\nvar fake = "ux.RouteDictationTranscript()"\n// ux.RouteDictationTranscript(true)\n')
        self.write(self.bridge, 'package main\nimport "voicerewrite/internal/core/ux"\nfunc bridge() { _ = ux.RouteDictationTranscript(true) }\n')
        report = b.run(b.Snapshot(self.root), self.catalog)
        self.assertEqual(report["total"]["shared_go_decisions"], 0)

    def test_unrelated_package_alias_does_not_qualify(self):
        self.write(self.go, "package ux\nfunc RouteDictationTranscript(ok bool) bool { return !ok }\n")
        for path in (self.windows, self.bridge):
            self.write(path, 'package main\nimport ux "example.invalid/other/ux"\nfunc host() { _ = ux.RouteDictationTranscript(true) }\n')
        self.assertEqual(b.run(b.Snapshot(self.root), self.catalog)["total"]["shared_go_decisions"], 0)

    def test_empty_go_policy_does_not_get_credit(self):
        self.write(self.go, "package ux\nfunc RouteDictationTranscript(ok bool) bool { return true }\n")
        for path in (self.windows, self.bridge):
            self.write(path, 'package main\nimport "voicerewrite/internal/core/ux"\nfunc host() { _ = ux.RouteDictationTranscript(true) }\n')
        self.assertEqual(b.run(b.Snapshot(self.root), self.catalog)["total"]["shared_go_decisions"], 0)

    def test_missing_go_path_falls_back_not_credit(self):
        self.write(self.go, "package ux\nfunc RouteDictationTranscript(ok bool) bool { return !ok }\n")
        self.catalog["journeys"][0]["steps"][0]["go"][0]["path"] = "voice-rewrite/internal/core/ux/missing.go"
        self.assertEqual(b.run(b.Snapshot(self.root), self.catalog)["total"]["shared_go_decisions"], 0)

    def test_logical_unit_reused_across_native_and_policy_rejected(self):
        self.catalog["journeys"][0]["steps"][0]["logical_unit_id"] = "same_rule"
        self.catalog["journeys"][0]["steps"][1]["logical_unit_id"] = "same_rule"
        with self.assertRaisesRegex(b.BenchmarkError, "mixes native"):
            b.run(b.Snapshot(self.root), self.catalog)

    def test_catalog_frozen_denominator_is_immutable(self):
        self.catalog["baseline_lock"] = "go70-38-v1"
        with self.assertRaisesRegex(b.BenchmarkError, "frozen 38"):
            b.run(b.Snapshot(self.root), self.catalog)

    def test_meta_only_pr_has_zero_delta(self):
        before = b.run(b.Snapshot(self.root, self.base), self.catalog)
        after = b.run(b.Snapshot(self.root), self.catalog)
        self.assertFalse(b.compare(after, before))
        self.assertEqual(after["journey_occurrences"]["go_total_pct"], 0)
        self.assertEqual(after["unique_logical_units"]["total"], 2)

    def test_invalid_go_syntax_never_gets_benchmark_credit(self):
        self.write(self.go, "package ux\nfunc RouteDictationTranscript(\n")
        with self.assertRaisesRegex(b.BenchmarkError, "AST audit failed"):
            b.run(b.Snapshot(self.root), self.catalog)


    def test_official_lock_cannot_be_removed_or_reclassified(self):
        official = json.loads(b.CATALOG.read_text(encoding="utf-8"))
        b.validate_catalog(official, strict=True)
        official.pop("baseline_lock")
        with self.assertRaisesRegex(b.BenchmarkError, "baseline_lock"):
            b.validate_catalog(official, strict=True)
        official["baseline_lock"] = "go70-38-v1"
        official["journeys"][0]["steps"][0]["kind"] = "portable_decision"
        with self.assertRaisesRegex(b.BenchmarkError, "reclassification"):
            b.validate_catalog(official, strict=True)

    def test_official_logical_unit_remap_and_stage_removal_fail(self):
        official = json.loads(b.CATALOG.read_text(encoding="utf-8"))
        official["journeys"][0]["steps"][0]["logical_unit_id"] = "fake-owner"
        with self.assertRaisesRegex(b.BenchmarkError, "logical_unit_id changed"):
            b.validate_catalog(official)
        official["journeys"][0]["steps"][0]["logical_unit_id"] = "dictation_hold/trigger"
        official["journeys"][0]["steps"].pop()
        with self.assertRaisesRegex(b.BenchmarkError, "frozen 38"):
            b.validate_catalog(official)

    def test_scoped_go_call_does_not_count_an_unused_decoy(self):
        self.catalog["journeys"][0]["steps"][0]["go"][1]["within"] = "host"
        self.write(self.go, "package ux\nfunc RouteDictationTranscript(ok bool) bool { return !ok }\n")
        self.write(self.windows, 'package main\nimport "voicerewrite/internal/core/ux"\nfunc host() {}\nfunc decoy() { _ = ux.RouteDictationTranscript(true) }\n')
        self.write(self.bridge, 'package main\nimport "voicerewrite/internal/core/ux"\nfunc bridge() { _ = ux.RouteDictationTranscript(true) }\n')
        report = b.run(b.Snapshot(self.root), self.catalog)
        self.assertEqual(report["total"]["shared_go_decisions"], 0)


    def test_real_go_delegate_to_decision_counts_not_an_empty_delegate(self):
        self.write(self.go, """package ux
func RouteDictationTranscript(ok bool) bool { return decide(ok) }
func decide(ok bool) bool { if !ok { return false }; return true }
""")
        for path in (self.windows, self.bridge):
            self.write(path, """package main
import "voicerewrite/internal/core/ux"
func host() { _ = ux.RouteDictationTranscript(true) }
""")
        self.assertEqual(b.run(b.Snapshot(self.root), self.catalog)["total"]["shared_go_decisions"], 1)
        self.write(self.go, """package ux
func RouteDictationTranscript(ok bool) bool { return decide(ok) }
func decide(ok bool) bool { return true }
""")
        self.assertEqual(b.run(b.Snapshot(self.root), self.catalog)["total"]["shared_go_decisions"], 0)


if __name__ == "__main__":
    unittest.main()
