# Windows downloads

CI builds a portable ZIP and an Inno Setup installer. Tagged preview releases include SHA-256 checksums and GitHub artifact provenance attestations.

Verify a downloaded artifact with the published checksum. For tag builds with attestations, use:

```bash
gh attestation verify PATH --repo xujinheng/windows-voice-rewrite
```

The executable and installer currently lack Authenticode publisher signatures, so Windows SmartScreen may warn.

The repository does not currently have a project-wide license. Releases are provided as unsigned preview builds.

## macOS downloads

Apple Silicon (`arm64`) and Intel (`x64`) development packages in GitHub Actions are explicitly named `*-Dev.zip` / `*-Dev.dmg`. They are ad-hoc signed, not Developer ID signed and not notarized. Gatekeeper or macOS permission prompts may appear; these packages are not advertised as friction-free installations.

When the repository has a valid Developer ID Application certificate and Apple notarization credentials, the macOS signed distribution workflow produces `VoiceRewrite-<version>-macOS-<arch>-Notarized.zip` and `.dmg`, each with a SHA-256 sidecar. Each signed app and its embedded `voicebridge` are checked for the same single CPU architecture and signing team, Hardened Runtime, Apple-stapled ticket and Gatekeeper assessment. The drag-to-Applications DMG is separately notarized and stapled. Tagged assets are added to the existing curated Windows GitHub Release only after both architectures pass verification; missing Apple credentials never cause Dev artifacts to be relabeled Notarized.

Check a downloaded archive and its adjacent sidecar with `shasum -a 256 -c FILE.sha256`. macOS installation, permissions, replacement upgrades and data-preserving uninstall instructions are in [macos/README.md](macos/README.md).

Developer ID signing, notarization and checksum validation do not substitute for physical-device microphone, Accessibility, side-button and Meeting permission acceptance tests.
