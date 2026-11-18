#!/usr/bin/env python3
"""Static linked-source Go ratio for the headless macOS executable (not runtime coverage)."""
import json
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent

def nonblank(path):
    return sum(bool(line.strip()) for line in path.read_text(encoding="utf-8").splitlines())

def main():
    process = subprocess.run(["go", "list", "-deps", "-json", "./cmd/voicebridge"],
                             cwd=ROOT / "voice-rewrite", text=True, capture_output=True, check=True)
    decoder = json.JSONDecoder()
    idx, paths = 0, set()
    while idx < len(process.stdout):
        while idx < len(process.stdout) and process.stdout[idx].isspace():
            idx += 1
        if idx >= len(process.stdout):
            break
        obj, read = decoder.raw_decode(process.stdout[idx:])
        idx += read
        if (obj.get("Module") or {}).get("Path") == "voicerewrite":
            paths.update(Path(obj["Dir"]) / f for f in obj.get("GoFiles", []) + obj.get("CgoFiles", []))
    swift_files = sorted((ROOT / "macos/Headless").glob("*.swift"))
    build_script = (ROOT / "scripts/build-macos-headless.sh").read_text(encoding="utf-8")
    for swift in swift_files:
        if swift.relative_to(ROOT).as_posix() not in build_script:
            raise SystemExit(f"unaccounted Swift host source: {swift}")
    go_lines = sum(nonblank(p) for p in paths)
    swift_lines = sum(nonblank(swift) for swift in swift_files)
    if not go_lines or not swift_lines or len(swift_files) != 2:
        raise SystemExit("missing linked Go or two native Swift sources")
    percentage = go_lines / (go_lines + swift_lines) * 100
    print(f"Go: {go_lines} nonblank linked-source LOC; Swift: {swift_lines}; Go share: {percentage:.1f}%")
    print("This is a static linked-source inventory, NOT functional coverage.")
    if percentage < 90:
        raise SystemExit("Go share of headless-linked production source below 90%")

if __name__ == "__main__":
    main()
