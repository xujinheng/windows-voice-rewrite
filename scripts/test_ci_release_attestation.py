"""Keep release signing privileges isolated from untrusted PR build jobs."""
from pathlib import Path
import unittest

WORKFLOW = Path(__file__).resolve().parents[1] / ".github/workflows/build.yml"


def job(source, name, following):
    return source.split("\n  " + name + ":\n", 1)[1].split(
        "\n  " + following + ":\n", 1)[0]


class ReleaseAttestationScopeTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.workflow = WORKFLOW.read_text(encoding="utf-8")

    def test_pr_build_cannot_request_provenance_writer_permissions(self):
        content = job(self.workflow, "build", "attest-windows-release")
        self.assertIn("permissions:\\n      contents: read", content)
        self.assertNotIn("id-token: write", content)
        self.assertNotIn("attestations: write", content)
        self.assertNotIn("uses: actions/attest@", content)
        self.assertIn("persist-credentials: false", content)

    def test_attestation_is_for_the_only_windows_download(self):
        content = job(self.workflow, "attest-windows-release", "release")
        self.assertIn("if: startsWith(github.ref, 'refs/tags/v')", content)
        self.assertIn("needs: build", content)
        self.assertIn("id-token: write", content)
        self.assertIn("attestations: write", content)
        self.assertIn("contents: read", content)
        self.assertEqual(content.count("uses: actions/attest@"), 1)
        self.assertEqual(content.count("uses: actions/download-artifact@"), 1)
        self.assertIn("name: VoiceRewrite-Windows", content)
        self.assertIn("test -s provenance/VoiceRewrite-Windows.exe", content)
        self.assertNotIn("OneClick", content)
        self.assertNotIn("Installer", content)

    def test_publishing_waits_for_release_attestations(self):
        content = job(self.workflow, "release", "windows-acceptance")
        self.assertIn("if: startsWith(github.ref, 'refs/tags/v')", content)
        self.assertIn("attest-windows-release]", content)
        self.assertIn("contents: write", content)
        self.assertNotIn("id-token: write", content)
        self.assertNotIn("attestations: write", content)
        gate = job(self.workflow, "ci-gate", "macos-signed-distribution")
        self.assertNotIn("attest-windows-release", gate)


if __name__ == "__main__":
    unittest.main()
