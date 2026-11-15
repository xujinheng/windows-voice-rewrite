#!/usr/bin/env python3
"""Reject platform-specific source files and OS API imports in shared Go core."""

import json
import os
from pathlib import Path
import subprocess
import sys

MODULE = Path(__file__).resolve().parents[1] / "voice-rewrite"
TARGETS = ("linux", "darwin", "windows")
SOURCE_FIELDS = ("GoFiles", "CgoFiles", "TestGoFiles", "XTestGoFiles")
IMPORT_FIELDS = ("Imports", "TestImports", "XTestImports")
NATIVE_IMPORTS = {"syscall", "golang.org/x/sys/windows", "golang.org/x/sys/unix"}


def packages_for(goos):
    env = os.environ.copy()
    env.update(GOOS=goos, GOARCH="amd64", CGO_ENABLED="0")
    result = subprocess.run(
        ["go", "list", "-json", "./internal/core/..."],
        cwd=MODULE, env=env, capture_output=True, text=True, check=False,
    )
    if result.returncode:
        raise RuntimeError(f"{goos}: go list failed:\n{result.stderr.strip()}")
    decoder = json.JSONDecoder()
    text = result.stdout
    pos = 0
    packages = {}
    while pos < len(text):
        if text[pos].isspace():
            pos += 1
            continue
        package, end = decoder.raw_decode(text, pos)
        pos = end
        if package.get("Error") or package.get("DepsErrors"):
            raise RuntimeError(f"{goos}: go list reported errors for {package['ImportPath']}")
        name = package["ImportPath"]
        source = {field: tuple(sorted(package.get(field, []))) for field in SOURCE_FIELDS}
        imports = set().union(*(package.get(field, []) for field in IMPORT_FIELDS))
        forbidden = sorted(
            imp for imp in imports
            if imp in NATIVE_IMPORTS or imp.startswith("github.com/lxn/win")
        )
        if forbidden:
            raise RuntimeError(f"{goos}: {name} imports platform APIs: {forbidden}")
        packages[name] = source
    if not packages:
        raise RuntimeError(f"{goos}: no internal/core packages discovered")
    return packages


def main():
    baseline = packages_for("linux")
    errors = []
    for goos in TARGETS[1:]:
        current = packages_for(goos)
        if set(current) != set(baseline):
            errors.append(
                f"{goos}: core package set differs from Linux: "
                f"missing={sorted(set(baseline) - set(current))}, "
                f"extra={sorted(set(current) - set(baseline))}"
            )
        for name in sorted(set(baseline) & set(current)):
            for field in SOURCE_FIELDS:
                if baseline[name][field] != current[name][field]:
                    errors.append(
                        f"{goos}: {name} {field} differs from Linux: "
                        f"linux={baseline[name][field]}, {goos}={current[name][field]}"
                    )
    if errors:
        raise RuntimeError("\n".join(errors))
    print(f"Verified {len(baseline)} portable core packages on Linux, Darwin and Windows")


if __name__ == "__main__":
    try:
        main()
    except (RuntimeError, ValueError) as exc:
        print(f"Portable core boundary failure: {exc}", file=sys.stderr)
        sys.exit(1)
