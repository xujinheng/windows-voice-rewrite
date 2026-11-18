"""Regression guards for main and PR native cache parity and release build safety."""

from pathlib import Path
import unittest

WORKFLOW = Path(__file__).resolve().parents[1] / ".github/workflows" / "build.yml"
ENABLED = "if: github.event_name == 'pull_request' || github.ref == 'refs/heads/main'"
REPORT = "if: always() && (github.event_name == 'pull_request' || github.ref == 'refs/heads/main')"


class NativeCachePolicyTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.workflow = WORKFLOW.read_text(encoding="utf-8")

    def _job(self, start, end):
        return self.workflow.split("\n  " + start + ":\n", 1)[1].split(
            "\n  " + end + ":\n", 1
        )[0]

    def test_both_architectures_cache_only_pr_and_main(self):
        for start, end, arch, caption in (
            ("test-core-macos", "build-macos-intel", "arm", "ARM"),
            ("build-macos-intel", "build", "intel", "Intel"),
        ):
            with self.subTest(job=start):
                job = self._job(start, end)
                self.assertEqual(job.count(ENABLED), 4)
                self.assertEqual(job.count(REPORT), 1)
                self.assertIn("run: python3 scripts/ci_xcode_mtimes.py", job)
                self.assertIn("id: " + arch + "-derived", job)
                self.assertIn("key: " + arch + "-xcode-build-", job)
                self.assertIn("Assert full " + caption + " XCTest coverage", job)
                self.assertIn("Report " + caption + " incremental cache", job)
                self.assertIn("max(counts, default=0) < 69", job)
                self.assertIn("** TEST SUCCEEDED **", job)
                self.assertNotIn("if: github.event_name == 'pull_request'\n", job)

    def test_tags_still_run_cold(self):
        # Every native Xcode cache step requires a PR event or exact main ref;
        # release tags cannot satisfy either condition.
        for name, following in (
            ("test-core-macos", "build-macos-intel"),
            ("build-macos-intel", "build"),
        ):
            with self.subTest(job=name):
                job = self._job(name, following)
                self.assertEqual(job.count("if: github.event_name == 'pull_request' || github.ref == 'refs/heads/main'"), 4)
                self.assertEqual(job.count("if: always() && (github.event_name == 'pull_request' || github.ref == 'refs/heads/main')"), 1)
        self.assertIn("tags: [v*]", self.workflow)


if __name__ == "__main__":
    unittest.main()
