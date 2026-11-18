import json
import tempfile
import unittest
from io import BytesIO
from pathlib import Path

from PIL import Image
from report import REQUIRED_CAPTURE_STATES, generate, require_complete_matrix, validate_manifest

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

    def test_comparison_gallery_links_to_existing_local_png(self):
        windows = self.make("windows")
        macos = self.make("macos-x64")
        output = self.root / "report"
        result = generate([windows, macos], output)
        image = result["rows"][0]["side_by_side"]
        self.assertTrue((output / image).is_file())
        gallery = (output / "index.html").read_text(encoding="utf-8")
        self.assertIn(f'href="{image}"', gallery)
        self.assertIn(f'src="{image}"', gallery)
        self.assertIn("NOT_VERIFIED_ON_PHYSICAL_DEVICE", gallery)
        markdown = (output / "report.md").read_text(encoding="utf-8")
        self.assertIn(f"( {image} )".replace(" ", ""), markdown)
        self.assertIn("inside the downloaded acceptance-comparison artifact", markdown)

    def test_theme_and_fixture_must_match(self):
        a = self.make("windows")
        b = self.make("macos-x64", fixture="another-fixture", theme="light")
        self.assertEqual(generate([a, b], self.root / "report")["paired_review_states"], 0)

    def test_incomplete_matrix_is_not_a_green_ci_result(self):
        windows = self.make("windows")
        macos = self.make("macos-arm64")
        other = self.make("macos-x64")
        result = generate([windows, macos, other], self.root / "report")
        self.assertEqual(result["paired_review_states"], 2)
        with self.assertRaisesRegex(ValueError, "missing required Windows/Mac state pairs"):
            require_complete_matrix(result)

    def test_a_missing_architecture_is_not_complete(self):
        windows = self.make("windows")
        macos = self.make("macos-arm64")
        result = generate([windows, macos], self.root / "report")
        with self.assertRaisesRegex(ValueError, "missing or unexpected capture platforms"):
            require_complete_matrix(result)

    def test_full_matrix_only_proves_captures_not_acceptance(self):
        manifests = []
        for platform in ["windows", "macos-arm64", "macos-x64"]:
            manifest = self.make(platform)
            entries = []
            for index, (state, fixture, theme) in enumerate(REQUIRED_CAPTURE_STATES):
                name = f"case-{index}.png"
                (manifest.parent / name).write_bytes(PNG)
                entries.append({"state": state, "fixture": fixture, "theme": theme,
                                "locale": "zh-CN", "scale": "logical-1x", "image": name})
            obj = json.loads(manifest.read_text())
            obj["captures"] = entries
            manifest.write_text(json.dumps(obj))
            manifests.append(manifest)
        result = generate(manifests, self.root / "report")
        self.assertEqual(require_complete_matrix(result), 32)
        self.assertEqual(result["paired_review_states"], 32)
        self.assertEqual(result["acceptance"], "NOT_VERIFIED_ON_PHYSICAL_DEVICE")
        self.assertTrue(all(row["status"] == "matched-for-review" for row in result["rows"]))

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
