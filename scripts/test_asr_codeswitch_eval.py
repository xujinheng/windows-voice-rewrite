import unittest
from pathlib import Path

import asr_codeswitch_eval as bench


MANIFEST = Path(__file__).parent / "asr_codeswitch_manifest.jsonl"


class ASRCodeSwitchBenchmarkTests(unittest.TestCase):
    def test_manifest_has_realistic_but_unrecorded_coverage(self):
        rows = bench.records(MANIFEST)
        self.assertGreaterEqual(len(rows), 80)
        self.assertGreaterEqual(sum(r["category"] == "zh_en_mixed" for r in rows), 50)
        self.assertGreaterEqual(sum(r["category"] == "negative" for r in rows), 4)
        self.assertTrue(all(r["audio_path"] is None for r in rows))
        self.assertEqual(len({r["id"] for r in rows}), len(rows))

    def test_missing_recordings_are_not_measurements(self):
        result = bench.summarize(bench.records(MANIFEST), [])
        self.assertFalse(result["real_audio_measured"])
        self.assertTrue(all(x["status"] == "not_measured" for x in result["variants"].values()))
        self.assertIn("not_measured", bench.render_markdown(result))

    def test_metrics_detect_case_numbers_hallucinated_words(self):
        row = {"id": "fixture", "reference": "PR 132 在 macOS 上",
               "english_terms": ["PR 132", "macOS"],
               "forbidden_terms": ["Kubernetes"]}
        good = bench.clip_scores(row, "PR 132 在 macOS 上", ["PR 132", "macOS", "Kubernetes"])
        self.assertEqual(good["term_recall"], 1)
        self.assertEqual(good["chinese_cer"], 0)
        bad = bench.clip_scores(row, "pr 123 在 MACOS 上 Kubernetes",
                                ["PR 132", "macOS", "Kubernetes"])
        self.assertEqual(bad["negative_hotword_false_positives"], 1)
        self.assertEqual(bad["number_exact"], 0)
        self.assertGreater(bad["mixed_token_error_rate"], 0)
        strict = bench.clip_scores(row, "pr 132 在 MACOS 上",
                                   ["PR 132", "macOS"], strict=True)
        self.assertLess(strict["term_recall"], 1)
        strict_clean = bench.clip_scores(row, "PR 132 在 macOS 上", ["PR 132", "macOS"], strict=True)
        strict_extra_punct = bench.clip_scores(row, "PR 132，在 macOS 上", ["PR 132", "macOS"], strict=True)
        self.assertGreater(strict_extra_punct["mixed_token_error_rate"], strict_clean["mixed_token_error_rate"])

    def test_paired_requires_identical_audio_and_all_observations(self):
        manifest = [{"id": "one", "reference": "macOS 在这", "english_terms": ["macOS"]}]
        def result(v, hypothesis, sha="same"):
            return {"id": "one", "variant": v, "status": "ok",
                    "audio_sha256": sha, "hypothesis": hypothesis,
                    "requests": 1, "latency_ms": 24}
        outputs = [result("baseline", "你好"), result("short", "macOS 在这"),
                   result("terms", "macOS 在这")]
        report = bench.summarize(manifest, outputs)
        self.assertTrue(report["real_audio_measured"])
        self.assertGreater(report["paired_deltas"]["short"]["candidate_minus_baseline"]["term_recall"]["delta"], 0)
        self.assertEqual(report["paired_deltas"]["terms"]["additional_requests"], 0)
        outputs[-1]["audio_sha256"] = "different"
        report = bench.summarize(manifest, outputs)
        self.assertFalse(report["real_audio_measured"])
        self.assertEqual(report["paired_deltas"]["terms"]["status"], "not_measured")

    def test_duplicate_result_cannot_bias_score(self):
        manifest = [{"id": "a", "reference": "中文", "english_terms": []}]
        observation = {"id": "a", "variant": "baseline", "status": "ok",
                       "audio_sha256": "same", "hypothesis": "中文"}
        with self.assertRaises(ValueError):
            bench.summarize(manifest, [observation, observation])
        self.assertEqual(bench.edit_distance("123", "132"), 2)
        self.assertEqual(bench.percentile([10, 20, 30, 40], 0.95), 38.5)

    def test_network_runner_requires_explicit_permission(self):
        class Args:
            allow_network = False
            endpoint = "http://127.0.0.1:1/v1"
            model = "whisper-1"
        with self.assertRaises(ValueError):
            bench.run_audio([], Args())


if __name__ == "__main__":
    unittest.main()
