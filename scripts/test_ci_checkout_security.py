"""Ensure read-only CI checkouts never persist Git credentials into test steps."""
from pathlib import Path
import unittest

WORKFLOWS = Path(__file__).resolve().parents[1] / ".github" / "workflows"
READ_ONLY = (
    "build.yml", "acceptance-evidence.yml", "direct2d-result.yml",
    "nightly.yml", "meeting-ui.yml", "macos-distribution.yml",
    "ready-build.yml",
)
PUBLISHERS = ("visual-matrix.yml", "demo-video.yml")


class CheckoutCredentialPolicyTests(unittest.TestCase):
    def test_read_only_jobs_do_not_persist_checkout_token(self):
        count = 0
        for name in READ_ONLY:
            lines = (WORKFLOWS / name).read_text(encoding="utf-8").splitlines()
            for i, line in enumerate(lines):
                if "- uses: actions/checkout@" not in line:
                    continue
                count += 1
                indent = len(line) - len(line.lstrip())
                block = []
                for next_line in lines[i + 1:]:
                    if (next_line.strip().startswith("- ") and
                            len(next_line) - len(next_line.lstrip()) <= indent):
                        break
                    block.append(next_line)
                with self.subTest(workflow=name, step=i + 1):
                    self.assertIn("persist-credentials: false", "\n".join(block))
        self.assertGreater(count, 15)

    def test_writable_media_publishers_are_explicitly_separated(self):
        for name in PUBLISHERS:
            content = (WORKFLOWS / name).read_text(encoding="utf-8")
            with self.subTest(workflow=name):
                self.assertIn("contents: write", content)
                self.assertIn("git push", content)


if __name__ == "__main__":
    unittest.main()
