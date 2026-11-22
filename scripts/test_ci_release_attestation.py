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

    def test_pr_builds_cannot_request_provenance_writer_permissions(self):
        for name, following in (("build", "build-oneclick"),
                                ("build-oneclick", "attest-windows-release")):
            with self.subTest(job=name):
                content = job(self.workflow, name, following)
                self.assertIn("permissions:\n      contents: read", content)
                self.assertNotIn("id-token: write", content)
                self.assertNotIn("attestations: write", content)
                self.assertNotIn("uses: actions/attest@", content)
                self.assertIn("persist-credentials: false", content)

    def test_attestation_job_requires_trusted_release_tag_and_outputs(self):
        content = job(self.workflow, "attest-windows-release", "release")
        self.assertIn("if: startsWith(github.ref, 'refs/tags/v')", content)
        self.assertIn("needs: [build, build-oneclick]", content)
        self.assertIn("id-token: write", content)
        self.assertIn("attestations: write", content)
        self.assertIn("contents: read", content)
        self.assertEqual(content.count("uses: actions/attest@"), 3)
        self.assertEqual(content.count("uses: actions/download-artifact@"), 3)
        for artifact in ("VoiceRewrite-App-Windows-x64-PortableZIP",
                         "VoiceRewrite-App-Windows-x64-Installer",
                         "VoiceRewrite-ASR-OneClick-Windows-x64-EXE"):
            self.assertIn("name: " + artifact, content)
        self.assertIn("sha256sum -c SHA256SUMS.txt", content)
        self.assertIn("sha256sum -c INSTALLER-SHA256SUMS.txt", content)
        self.assertIn("test \"${#installers[@]}\" -eq 1", content)

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
