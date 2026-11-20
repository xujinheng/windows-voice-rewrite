"""Static regression checks for CI event ownership and duplicate workflows.

Avoid a PyYAML runtime dependency in the fast path-classifier job.
"""
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parent.parent / ".github" / "workflows"


def workflow(name):
    return (ROOT / name).read_text(encoding="utf-8")


def events(name):
    # Workflow top-level 'on' uses zero indentation; 'permissions' marks its end.
    return workflow(name).split("\non:\n", 1)[1].split("\npermissions:", 1)[0]


class WorkflowEventOwnership(unittest.TestCase):
    def test_main_direct2d_is_reusable_not_a_second_push(self):
        direct = events("direct2d-result.yml")
        self.assertIn("  workflow_call:", direct)
        self.assertIn("  workflow_dispatch:", direct)
        self.assertNotIn("  push:", direct)
        main = workflow("build.yml")
        self.assertIn("needs.changes.outputs.direct2d == 'true' && !startsWith(github.ref, 'refs/tags/')", main)
        self.assertIn("uses: ./.github/workflows/direct2d-result.yml", main)

    def test_media_capture_is_read_only_and_not_a_recursive_publish_chain(self):
        demo = events("demo-video.yml")
        image_push = demo.split("  push:\n", 1)[1].split("  workflow_dispatch:", 1)[0]
        self.assertIn("'docs/ui/*.png'", image_push)
        self.assertIn("  workflow_dispatch:", demo)
        self.assertNotIn("  workflow_run:", demo)
        screenshot = events("visual-matrix.yml")
        self.assertIn("    branches: [main]", screenshot)
        self.assertNotIn("'scripts/render-demo-video.py'", screenshot)
        self.assertNotIn("'.github/workflows/demo-video.yml'", screenshot)
        for name in ("visual-matrix.yml", "demo-video.yml"):
            content = workflow(name)
            self.assertIn("contents: read", content)
            self.assertNotIn("git push", content)

    def test_nightly_verifies_its_own_workflow_on_main_only(self):
        nightly = events("nightly.yml")
        self.assertIn("  push:\n    branches: [main]\n    paths: ['.github/workflows/nightly.yml']", nightly)
        self.assertIn("  schedule:", nightly)
        self.assertIn("  workflow_dispatch:", nightly)
        self.assertNotIn("'voice-rewrite/", nightly)

    def test_ready_apps_do_not_fire_on_unrelated_labels(self):
        ready = events("ready-build.yml")
        self.assertIn("    types: [edited]", ready)
        self.assertIn("  workflow_dispatch:", ready)
        self.assertNotIn("labeled", ready)
        content = workflow("ready-build.yml")
        self.assertIn("github.event.changes.title", content)
        self.assertIn("contains(github.event.pull_request.title, '[ready-to-test]')", content)


if __name__ == "__main__":
    unittest.main()
