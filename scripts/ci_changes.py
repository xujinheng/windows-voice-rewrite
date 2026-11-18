#!/usr/bin/env python3
"""Conservatively classify PR changes by affected platform.

Main and release tags run the full matrix in build.yml. Only PRs are scoped.
"""
import argparse
import os
import sys


def classify(paths):
    scope = {"core": False, "macos": False, "windows": False, "oneclick": False}
    for raw in paths:
        path = raw.strip()
        if not path:
            continue
        if path.startswith(".github/workflows/") or path in (
            "scripts/ci_changes.py", "scripts/test_ci_changes.py", "AGENTS.md",
        ):
            return dict.fromkeys(scope, True)
        if path.startswith("voice-rewrite/"):
            scope.update(core=True, macos=True, windows=True)
        elif path.startswith("macos/"):
            scope["macos"] = True
        elif path.startswith("oneclick/"):
            scope["oneclick"] = True
        elif path.startswith("installer/"):
            scope["windows"] = True
        elif path.startswith("scripts/"):
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
