import json
import tempfile
import unittest
from io import BytesIO
from pathlib import Path

from PIL import Image
from report import generate, validate_manifest

image = Image.new("RGB", (64, 64))
image.putdata([((i * 23) % 256, (i * 19) % 256, (i * 17) % 256)
               for i in range(64 * 64)])
buffer = BytesIO()
image.save(buffer, format="PNG")
PNG = buffer.getvalue()


class EvidenceTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)

    def make(self, platform, state="pill.recording", fixture="synthetic-voice", theme="dark"):
        root = self.root / platform
        root.mkdir(exist_ok=True)
        (root / "shot.png").write_bytes(PNG)
        manifest = root / "manifest.json"
        manifest.write_text(json.dumps({
            "schema": 1, "platform": platform, "classification": "simulated-ui",
            "captures": [{
                "state": state, "fixture": fixture, "theme": theme, "locale": "zh-CN",
                "scale": "logical-1x", "image": "shot.png"
            }]
        }))
        return manifest

    def test_different_states_never_pair(self):
        windows = self.make("windows", "pill.processing")
        macos = self.make("macos-arm64", "pill.failure")
        result = generate([windows, macos], self.root / "report")
        self.assertEqual(result["paired_review_states"], 0)
        self.assertEqual(len(result["rows"]), 2)
        self.assertEqual(result["acceptance"], "NOT_VERIFIED_ON_PHYSICAL_DEVICE")

    def test_same_state_is_only_review_evidence(self):
        windows = self.make("windows")
        macos = self.make("macos-x64")
        result = generate([windows, macos], self.root / "report")
        self.assertEqual(result["paired_review_states"], 1)
        self.assertEqual(result["rows"][0]["status"], "matched-for-review")
        self.assertNotIn("pass", result["rows"][0])

    def test_theme_and_fixture_must_match(self):
        a = self.make("windows")
        b = self.make("macos-x64", fixture="another-fixture", theme="light")
        self.assertEqual(generate([a, b], self.root / "report")["paired_review_states"], 0)

    def test_path_traversal_and_missing_image_are_rejected(self):
        path = self.make("windows")
        doc = json.loads(path.read_text())
        doc["captures"][0]["image"] = "../other.png"
        path.write_text(json.dumps(doc))
        with self.assertRaises(ValueError):
            validate_manifest(path)

    def test_hardware_claim_is_rejected(self):
        path = self.make("windows")
        doc = json.loads(path.read_text())
        doc["classification"] = "physical-pass"
        path.write_text(json.dumps(doc))
        with self.assertRaises(ValueError):
            validate_manifest(path)


if __name__ == "__main__":
    unittest.main()
