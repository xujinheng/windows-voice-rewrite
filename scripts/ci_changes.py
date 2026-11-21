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
            "scripts/test-meeting-ui.ps1", "clients/windows/visual_preview_windows.go",
            "clients/windows/meeting_ui_e2e_windows_test.go",
            "clients/windows/settings*.go", "clients/windows/handy_status_windows.go",
            "clients/windows/popup_windows.go", "clients/windows/meeting*.go",
            "core/meeting/**",
            "core/indicator/**",
            "core/settingsform/**",
        )):
            scope["ui"] = True
        if any(fnmatch.fnmatchcase(path, pattern) for pattern in (
            "clients/windows/result_direct2d_windows.go",
            "clients/windows/appearance_windows.go", "clients/windows/visual_skin_windows.go",
            "clients/windows/popup_windows.go", "clients/windows/popup_dismiss_windows.go",
            "core/popup/layout.go", "core/popup/layout_test.go",
            "clients/windows/answer_visual_preview_windows.go",
            "clients/windows/visual_preview_windows.go",
            "clients/windows/desktop_geometry.go", "clients/windows/desktop_monitor_windows.go",
            "clients/windows/result_history.go", "clients/windows/window_windows.go",
            "clients/windows/windows.go", "clients/go.mod", "go.work",
        )):
            scope["direct2d"] = True
        # CI governance and reusable action changes must validate the whole matrix.
        if (path.startswith((".github/workflows/", ".github/actions/"))
                or path == ".github/dependabot.yml"
                or path == "AGENTS.md"
                or path == "scripts/ci_changes.py"
                or path.startswith("scripts/test_ci_")):
            return dict.fromkeys(scope, True)
        if path.startswith("core/"):
            if path.startswith("core/asrtext/"):
                scope.update(core=True, macos=True, windows=True, oneclick=True)
            elif path.startswith("core/meeting/"):
                scope.update(core=True, windows=True)
            else:
                scope.update(core=True, macos=True, windows=True)
        elif path == "assets/VoiceRewrite.svg":
            scope["macos"] = True
        elif path.startswith("clients/macos/bridge/"):
            scope.update(core=True, macos=True)
        elif path.startswith("clients/macos/"):
            scope["macos"] = True
        elif path.startswith("clients/windows/"):
            scope["windows"] = True
        elif path.startswith("clients/cli/"):
            scope["core"] = True
        elif path in ("go.work", "clients/go.mod") or path.startswith(("voice-rewrite/", "macos/", "cli/")):
            # Removed legacy paths and module workspace changes affect every host.
            scope.update(core=True, macos=True, windows=True, oneclick=True)
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
