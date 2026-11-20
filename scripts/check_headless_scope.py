#!/usr/bin/env python3
"""Audit the actual headless dependency graph and prevent a second native business stack."""
import json
import os
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent

def nonblank(path):
    return sum(bool(line.strip()) for line in path.read_text(encoding="utf-8").splitlines())

def main():
    # Always inspect the shipped Darwin C archive, including on Linux CI.
    # Linux-only go list skips native_darwin.go and would count the deleted
    # bridge main as if it were the production speech entrypoint.
    go_env = {**os.environ, "GOOS": "darwin", "GOARCH": "arm64", "CGO_ENABLED": "1"}
    process = subprocess.run(["go", "list", "-deps", "-json", "./cmd/voicebridge"],
                             cwd=ROOT / "voice-rewrite", env=go_env,
                             text=True, capture_output=True, check=True)
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
    # Linux go list omits the Darwin C ABI, but Darwin go list already
    # includes it. Count the compiled source once on either runner.
    paths.add(ROOT / "voice-rewrite/cmd/voicebridge/native_darwin.go")
    go_lines = sum(nonblank(p) for p in paths)
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
    for sym in ("VRAction(", "VRPress(", "VRActivate(", "VRRelease(", "VREpochValid(", "VRWave(", "VRSpinner(", "VRLevel(", "VRDuration(", "VRRun(", "VRHeldRole(", "VRDelivery("):
        if sym not in swift:
            raise SystemExit(f"native Swift host does not call Go Core directly: {sym}")
    if any(token in swift for token in ("RightCommand", "RightOption", "Mouse4", "Mouse5",
                                           "config.profile.dictation", "config.profile.rewrite")):
        raise SystemExit("native host must get shortcut-to-mode decisions from Go")
    if any(token in swift for token in ("private enum Action", "activeAction", "activeKey", "struct HostConfig")):
        raise SystemExit("Swift must not mirror Go-owned task role state")
    if any(token in swift for token in ('"selected_voice_task"', '"transcribe"', '"host_profile"')):
        raise SystemExit("Swift must not restore stringly typed task dispatch")
    if "Process()" in swift or "voicebridge" in swift:
        raise SystemExit("macOS retains a separate Go bridge subprocess")
    if not required.issubset(imports):
        raise SystemExit("macOS Go bridge lost a shared ASR/LLM/config dependency")
    if "voicerewrite/internal/core/meeting" in imports:
        raise SystemExit("removed macOS Meeting must not stay linked just to inflate Go LOC")
    bridge = " ".join((ROOT / "voice-rewrite/cmd/voicebridge" / p).read_text(encoding="utf-8")
                      for p in ("main.go", "native_darwin.go"))
    windows = (ROOT / "voice-rewrite/task_worker_windows.go").read_text(encoding="utf-8")
    if any(token in windows for token in ("func processRecordedHold(", "func processDictationHold(", "func processAudioAndRewrite(")):
        raise SystemExit("Windows recreated separate voice task orchestration next to voice.Core")
    if (ROOT / "voice-rewrite/cmd/voicebridge/bridge_test.go").exists() or "safeProcessRequest" in bridge:
        raise SystemExit("retired duplicate macOS voice task must not return")
    meeting = (ROOT / "voice-rewrite/meeting_session.go").read_text(encoding="utf-8")
    # Both *production entrypoints* must pass through the same task owner.
    # Provider-level calls alone could still leave two independent pipelines.
    if any(token in bridge for token in ("func serve(", "func dispatch(", "func readFrame(", "PCMBase64", "req.Op")):
        raise SystemExit("unshipped JSONL bridge reintroduced a second desktop transport")
    if "voicerewrite/voice" not in imports or any(
        ".Execute(" not in host for host in (bridge, windows)
    ) or "voice.Delivery(" not in windows or "voice.Delivery(" not in bridge:

        raise SystemExit("both hosts must invoke the same Go voice task")
    # Scan every Windows desktop host and the shipped Go bridge, not just the
    # current worker filenames. Meeting is a separate Go Core product pipeline.
    forbidden = ("speech.Transcribe(", "textedit.ResolveSelectedTask(",
                 "textedit.RouteTask(", "textedit.Rewrite(")
    desktop = ROOT / "voice-rewrite"
    for path in [*desktop.glob("cmd/voicebridge/*.go"), *desktop.glob("*.go")]:
        if path.name.endswith("_test.go") or (path.parent == desktop and path.name.startswith("meeting")):
            continue
        source = path.read_text(encoding="utf-8")
        if ('"voicerewrite/internal/core/speech"' in source or
            path != desktop / "windows.go" and '"voicerewrite/internal/core/textedit"' in source or
            any(token in source for token in forbidden)):
            raise SystemExit(f"desktop host duplicates voice policy: {path.relative_to(ROOT)}")
    if any(token in swift for token in ("URLSession", "URLRequest", "/v1/chat/completions",
                                        "/v1/audio/transcriptions")):
        raise SystemExit("Swift host implements provider business calls instead of calling Go")
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
