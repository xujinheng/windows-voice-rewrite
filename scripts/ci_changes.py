#!/usr/bin/env python3
"""Conservatively classify PR changes by affected platform.

Main and release tags run the full matrix in build.yml. Only PRs are scoped.
"""
import argparse
import fnmatch
import os
import sys


def classify(paths):
    scope = {"core": False, "macos": False, "windows": False, "oneclick": False,
             "ui": False, "direct2d": False}
    for raw in paths:
        path = raw.strip()
        if not path:
            continue
        if any(fnmatch.fnmatchcase(path, pattern) for pattern in (
            "tests/acceptance/**", "scripts/capture-acceptance-windows.ps1",
            "scripts/test-meeting-ui.ps1", "voice-rewrite/visual_preview_windows.go",
            "voice-rewrite/meeting_ui_e2e_windows_test.go",
            "voice-rewrite/settings*.go", "voice-rewrite/handy_status_windows.go",
            "voice-rewrite/popup_windows.go", "voice-rewrite/meeting*.go",
            "voice-rewrite/meeting/**",
            "voice-rewrite/internal/core/indicator/**",
            "voice-rewrite/internal/core/settingsform/**",
        )):
            scope["ui"] = True
        if any(fnmatch.fnmatchcase(path, pattern) for pattern in (
            "voice-rewrite/result_direct2d_windows.go",
            "voice-rewrite/appearance_windows.go", "voice-rewrite/visual_skin_windows.go",
            "voice-rewrite/popup_windows.go", "voice-rewrite/popup_dismiss_windows.go",
            "voice-rewrite/popup_layout.go", "voice-rewrite/popup_layout_test.go",
            "voice-rewrite/answer_visual_preview_windows.go",
            "voice-rewrite/visual_preview_windows.go",
            "voice-rewrite/desktop_geometry.go", "voice-rewrite/desktop_monitor_windows.go",
            "voice-rewrite/result_history.go", "voice-rewrite/window_windows.go",
            "voice-rewrite/windows.go", "voice-rewrite/go.mod", "voice-rewrite/go.sum",
        )):
            scope["direct2d"] = True
        # CI governance and reusable action changes must validate the whole matrix.
        if (path.startswith((".github/workflows/", ".github/actions/"))
                or path == ".github/dependabot.yml"
                or path == "AGENTS.md"
                or path == "scripts/ci_changes.py"
                or path.startswith("scripts/test_ci_")):
            return dict.fromkeys(scope, True)
        if path.startswith("voice-rewrite/"):
            if path.startswith("voice-rewrite/asrtext/"):
                # OneClick imports this shared package through its local module replacement.
                scope.update(core=True, macos=True, windows=True, oneclick=True)
            elif path.startswith("voice-rewrite/cmd/voicebridge/"):
                scope.update(core=True, macos=True)
            elif path.startswith("voice-rewrite/meeting/"):
                # Headless macOS does not link Meeting.
                scope.update(core=True, windows=True)
            elif path == "voice-rewrite/windows.go" or path.endswith(("_windows.go", "_windows_test.go")):
                scope["windows"] = True
            elif path.endswith(("_darwin.go", "_darwin_test.go", "_macos.go")):
                scope["macos"] = True
            else:
                # Shared Go, generated contracts and dependencies affect both hosts.
                scope.update(core=True, macos=True, windows=True)
        elif path.startswith("macos/"):
            scope["macos"] = True
        elif path.startswith("oneclick/"):
            scope["oneclick"] = True
        elif path.startswith("installer/"):
            scope["windows"] = True
        elif path.startswith("scripts/"):
            if path == "scripts/check_headless_scope.py":
                scope.update(core=True, macos=True)
            if "macos" in path or "swift" in path or "acceptance" in path:
                scope["macos"] = True
            if "windows" in path or "meeting-ui" in path or "acceptance" in path:
                scope["windows"] = True
            if "portable-core" in path or "contract" in path:
                scope["core"] = True
    return scope


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", default=os.getenv("GITHUB_OUTPUT"))
    args = parser.parse_args()
    result = classify(sys.stdin.read().splitlines())
    lines = "".join("{}={}\n".format(key, str(value).lower()) for key, value in result.items())
    if args.output:
        with open(args.output, "a", encoding="utf-8") as output:
            output.write(lines)
    print(lines, end="")


if __name__ == "__main__":
    main()
