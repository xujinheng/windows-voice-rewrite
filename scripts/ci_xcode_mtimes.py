#!/usr/bin/env python3
"""Stabilize git-tracked file mtimes for Xcode incremental CI.

Git checkout gives the same unchanged source a new mtime on every hosted
runner. A cached DerivedData/Build database then treats every source as dirty.
A content-derived timestamp makes an unchanged path stable across checkouts
while giving every changed file an independently invalidated timestamp.

Run on a clean CI checkout BEFORE restoring the DerivedData Build cache.
Never touch untracked files, symlinks or generated Xcode outputs.
"""

import argparse
import hashlib
import os
from pathlib import Path
import subprocess


def normalize(root: Path) -> int:
    root = root.resolve()
    tracked = subprocess.check_output(
        ["git", "ls-files", "-z", "--cached"], cwd=root
    ).split(b"\0")
    updated = 0
    for raw in tracked:
        if not raw:
            continue
        path = root / os.fsdecode(raw)
        if not path.is_file() or path.is_symlink():
            continue

        digest = hashlib.sha256()
        with path.open("rb") as file:
            for chunk in iter(lambda: file.read(1024 * 1024), b""):
                digest.update(chunk)
        signature = digest.digest()
        seconds = 1_200_000_000 + int.from_bytes(signature[:4], "big") % 200_000_000
        nanos = int.from_bytes(signature[4:8], "big") % 1_000_000_000
        stable_ns = seconds * 1_000_000_000 + nanos
        os.utime(path, ns=(stable_ns, stable_ns))
        updated += 1
    return updated


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", type=Path, default=Path.cwd())
    args = parser.parse_args()
    count = normalize(args.root)
    print(f"Normalized {count} tracked files with content-derived nanosecond mtimes")


if __name__ == "__main__":
    main()
