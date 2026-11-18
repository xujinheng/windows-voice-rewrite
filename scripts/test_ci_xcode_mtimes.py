"""Regression tests for deterministic Xcode incremental-cache invalidation."""

from pathlib import Path
import os
import subprocess
import sys
import tempfile
import unittest

from ci_xcode_mtimes import normalize


class NormalizeMtimesTest(unittest.TestCase):
    def test_unchanged_stable_changed_invalidated_and_untracked_untouched(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            subprocess.run(["git", "init", "-q"], cwd=root, check=True)
            source = root / "Example.swift"
            source.write_text("let answer = 1\n", encoding="utf-8")
            other = root / "Other.swift"
            other.write_text("let other = 3\n", encoding="utf-8")
            subprocess.run(["git", "add", "Example.swift", "Other.swift"],
                           cwd=root, check=True)
            untracked = root / "NotCached.swift"
            untracked.write_text("no change\n", encoding="utf-8")
            original_untracked_ns = untracked.stat().st_mtime_ns

            self.assertEqual(normalize(root), 2)
            first = source.stat().st_mtime_ns
            other_first = other.stat().st_mtime_ns
            os.utime(source, None)  # simulate a fresh checkout
            self.assertEqual(normalize(root), 2)
            self.assertEqual(source.stat().st_mtime_ns, first)

            source.write_text("let answer = 2\n", encoding="utf-8")
            self.assertEqual(normalize(root), 2)
            self.assertNotEqual(source.stat().st_mtime_ns, first)
            self.assertEqual(other.stat().st_mtime_ns, other_first)
            self.assertEqual(untracked.stat().st_mtime_ns, original_untracked_ns)

            source.write_text("let answer = 1\n", encoding="utf-8")
            normalize(root)
            self.assertEqual(source.stat().st_mtime_ns, first)

    def test_missing_tracked_path_is_skipped(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            subprocess.run(["git", "init", "-q"], cwd=root, check=True)
            source = root / "removed.swift"
            source.write_text("initial\n", encoding="utf-8")
            subprocess.run(["git", "add", "removed.swift"], cwd=root, check=True)
            source.unlink()
            self.assertEqual(normalize(root), 0)


if __name__ == "__main__":
    unittest.main()
