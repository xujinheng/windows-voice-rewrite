"""Ensure read-only CI checkouts never persist Git credentials into test steps."""
from pathlib import Path
import unittest

WORKFLOWS = Path(__file__).resolve().parents[1] / ".github" / "workflows"
READ_ONLY = (
    "build.yml", "acceptance-evidence.yml", "direct2d-result.yml",
    "nightly.yml", "meeting-ui.yml", "macos-distribution.yml",
    "ready-build.yml", "visual-matrix.yml", "demo-video.yml",
)


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
        # Runner/jobs were deliberately deduplicated; every checkout is still audited.
        self.assertGreaterEqual(count, 15)

    def test_media_workflows_have_no_direct_push_to_protected_main(self):
        for name in ("visual-matrix.yml", "demo-video.yml"):
            content = (WORKFLOWS / name).read_text(encoding="utf-8")
            with self.subTest(workflow=name):
                self.assertIn("contents: read", content)
                self.assertNotIn("git push", content)


if __name__ == "__main__":
    unittest.main()
