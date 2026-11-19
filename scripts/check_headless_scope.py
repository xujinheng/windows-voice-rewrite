#!/usr/bin/env python3
"""Audit the actual headless dependency graph and prevent a second native business stack."""
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
    idx, paths, imports = 0, set(), set()
    while idx < len(process.stdout):
        while idx < len(process.stdout) and process.stdout[idx].isspace():
            idx += 1
        if idx >= len(process.stdout):
            break
        obj, read = decoder.raw_decode(process.stdout[idx:])
        idx += read
        if (obj.get("Module") or {}).get("Path") == "voicerewrite":
            imports.add(obj["ImportPath"])
            paths.update(Path(obj["Dir"]) / f for f in obj.get("GoFiles", []) + obj.get("CgoFiles", []))
    swift_files = sorted((ROOT / "macos/Headless").glob("*.swift"))
    build_script = (ROOT / "scripts/build-macos-headless.sh").read_text(encoding="utf-8")
    for swift in swift_files:
        if swift.relative_to(ROOT).as_posix() not in build_script:
            raise SystemExit(f"unaccounted Swift host source: {swift}")
    # The Darwin-only cgo export surface is linked into the same executable.
    # Linux go list cannot include it, so count that actual compiled source once.
    native = ROOT / "voice-rewrite/cmd/voicebridge/native_darwin.go"
    go_lines = sum(nonblank(p) for p in paths) + nonblank(native)
    swift_lines = sum(nonblank(swift) for swift in swift_files)
    if not go_lines or not swift_lines or len(swift_files) != 2:
        raise SystemExit("missing linked Go or two native Swift sources")
    required = {
        "voicerewrite/internal/core/config",
        "voicerewrite/internal/core/speech",
        "voicerewrite/internal/core/textedit",
        "voicerewrite/internal/core/provider",
    }
    if "-buildmode=c-archive" not in build_script or "VoiceCore.h" not in build_script:
        raise SystemExit("macOS must link in-process Go Core, not spawn a helper")
    swift = " ".join(p.read_text(encoding="utf-8") for p in swift_files)
    for sym in ("VRPress(", "VRActivate(", "VRRelease(", "VREpochValid(", "VRWave(", "VRSpinner(", "VRLevel(", "VRDuration(", "VRRun("):
        if sym not in swift:
            raise SystemExit(f"native Swift host does not call Go Core directly: {sym}")
    if "Process()" in swift or "voicebridge" in swift:
        raise SystemExit("macOS retains a separate Go bridge subprocess")
    if not required.issubset(imports):
        raise SystemExit("macOS Go bridge lost a shared ASR/LLM/config dependency")
    if "voicerewrite/internal/core/meeting" in imports:
        raise SystemExit("removed macOS Meeting must not stay linked just to inflate Go LOC")
    bridge = (ROOT / "voice-rewrite/cmd/voicebridge/main.go").read_text(encoding="utf-8")
    windows = (ROOT / "voice-rewrite/task_worker_windows.go").read_text(encoding="utf-8")
    meeting = (ROOT / "voice-rewrite/meeting_session.go").read_text(encoding="utf-8")
    for call in ("speech.Transcribe(", "textedit.ResolveSpokenTask("):
        if call not in bridge or call not in windows:
            raise SystemExit(f"both hosts must call the same Go operation: {call}")
    if "meetingcore.NewSessionRuntime()" not in meeting:
        raise SystemExit("Windows Meeting Core is not preserved")
    if any(token in bridge for token in ("meeting_start", "meeting_feed", "meeting_replies")):
        raise SystemExit("obsolete macOS Meeting protocol is still linked")
    percentage = go_lines / (go_lines + swift_lines) * 100
    print(f"Go: {go_lines} nonblank linked-source LOC; Swift: {swift_lines}; Go share: {percentage:.1f}%")
    print("Actual linked-source inventory, NOT dynamic coverage. No dead Meeting code is counted.")
    # A raw >=90% ratio rewarded leaving unused Meeting modules in voicebridge.
    # Enforce native boundary size and real shared entrypoints instead.
    if swift_lines > 850 or go_lines <= swift_lines:
        raise SystemExit("macOS adapter grew beyond the 850-line Go-first native budget")

if __name__ == "__main__":
    main()
