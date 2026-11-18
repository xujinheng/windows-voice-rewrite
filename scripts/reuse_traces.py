#!/usr/bin/env python3
"""Validate reviewed voiceflow-v1/Meeting-v1 event/effect fixtures and replay parity.

Evidence is credited ONLY when both real production replay commands are supplied.
A fixture-only run reports explicitly that production trace parity was not verified.

For a voiceflow.v1 event with receipt_from, runners must resolve that action's
actual command_id from the preceding effect trace, then send BOTH request_id
and receipt_id with the same ID. Remove receipt_from before passing the event
to Go Core or JSONL. It is a test-driver dependency, never a v1 wire field.
"""
from __future__ import annotations

import argparse
import json
import shlex
import subprocess
from pathlib import Path

FIXTURES = Path(__file__).with_name("reuse_voiceflow_traces.jsonl")
SCENARIOS = {
    "happy_dictation", "hold_release_before_ready", "recording_180s_deadline",
    "selected_rewrite", "selected_rewrite_preview", "cancel_stale_asr",
    "uncertain_paste", "meeting_queue_full_stop",
}
VOICE_EVENTS = {
    "record_arm", "press", "release", "record_ready", "record_limit",
    "audio_stopped", "asr_result", "model_result", "write_receipt",
    "cancel", "timeout", "focus_changed",
}
RECEIPTS = {"record_ready", "audio_stopped", "asr_result",
            "model_result", "write_receipt"}
EFFECT_SOURCES = {"start_native_capture", "stop_native_capture", "send_asr",
                  "send_model", "attempt_native_write"}


class TraceError(ValueError):
    pass


def load_fixtures(path: Path = FIXTURES) -> list[dict]:
    cases = []
    for line, raw in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
        if not raw.strip():
            raise TraceError(f"blank JSONL line {line}")
        try:
            case = json.loads(raw)
        except json.JSONDecodeError as exc:
            raise TraceError(f"JSONL line {line}: {exc}") from exc
        if (case.get("v") != 1 or not isinstance(case.get("name"), str)
                or case.get("protocol") not in ("voiceflow.v1", "meeting.v1")):
            raise TraceError(f"invalid trace version/name/protocol on {line}")
        if not isinstance(case.get("events"), list) or not case["events"]:
            raise TraceError(f"no events: {case['name']}")
        commands = set()
        for event in case["events"]:
            if (not isinstance(event, dict) or event.get("v") != 1
                    or not all(event.get(k) for k in
                               ("type", "session_id", "generation", "command_id"))):
                raise TraceError(f"unidentified event: {case['name']}")
            if event["command_id"] in commands:
                raise TraceError(f"duplicate command_id: {case['name']}")
            commands.add(event["command_id"])
            if case["protocol"] == "voiceflow.v1":
                if event["type"] not in VOICE_EVENTS:
                    raise TraceError(f"unknown voiceflow event: {event['type']}")
                if (event["type"] == "record_arm"
                        and event.get("mode") not in ("dictation", "rewrite")):
                    raise TraceError(f"record_arm requires mode: {case['name']}")
                if event["type"] == "timeout" and not event.get("timeout_origin"):
                    raise TraceError(f"timeout requires origin: {case['name']}")
                if event["type"] in RECEIPTS:
                    dynamic = bool(event.get("receipt_from"))
                    static = bool(event.get("receipt_id") and event.get("request_id"))
                    if dynamic == static or "success" not in event:
                        raise TraceError(f"receipt requires exactly one source and success: {case['name']}")
            if ("receipt_from" in event and event["receipt_from"]
                    not in EFFECT_SOURCES):
                raise TraceError(f"unsupported receipt source: {case['name']}")
        required = case.get("required_effects")
        forbidden = case.get("forbidden_effects")
        if (not isinstance(required, list) or not required
                or not isinstance(forbidden, list)
                or not all(isinstance(x, str) and x for x in required + forbidden)
                or set(required) & set(forbidden)):
            raise TraceError(f"invalid effect assertions: {case['name']}")
        counts = case.get("expected_effect_counts", {})
        if (not isinstance(counts, dict) or any(
                not isinstance(k, str) or not isinstance(v, int) or v < 0
                for k, v in counts.items())):
            raise TraceError(f"invalid effect counts: {case['name']}")
        cases.append(case)
    names = [c["name"] for c in cases]
    if len(names) != len(set(names)) or set(names) != SCENARIOS:
        raise TraceError("missing/duplicate/unreviewed voiceflow trace fixture")
    return cases


def normalize(result: dict) -> list[dict]:
    if result.get("v") != 1 or not isinstance(result.get("effects"), list):
        raise TraceError("runner returned invalid effects envelope")
    normalized = []
    for effect in result["effects"]:
        if not isinstance(effect, dict) or not isinstance(effect.get("type"), str):
            raise TraceError("runner returned an untyped effect")
        if effect["type"] == "native_execution_receipt":
            continue  # OS receipt, not a Go policy effect.
        normalized.append(effect)
    return normalized


def check_case(case: dict, windows: dict, macos: dict) -> None:
    win, mac = normalize(windows), normalize(macos)
    if win != mac:
        raise TraceError(f"{case['name']}: Windows Go / macOS bridge effects differ")
    kinds = [e["type"] for e in win]
    for kind in case["required_effects"]:
        if kind not in kinds:
            raise TraceError(f"{case['name']}: effect not executed: {kind}")
    for kind in case["forbidden_effects"]:
        if kind in kinds:
            raise TraceError(f"{case['name']}: forbidden effect executed: {kind}")
    for kind, count in case.get("expected_effect_counts", {}).items():
        if kinds.count(kind) != count:
            raise TraceError(f"{case['name']}: {kind} expected {count}, got {kinds.count(kind)}")
    if not win:
        raise TraceError(f"{case['name']}: empty Go effect trace")


def replay(command: str, case: dict, timeout: float = 20.0) -> dict:
    if not command:
        raise TraceError("production replay command is required")
    proc = subprocess.run(
        shlex.split(command), input=json.dumps(case, ensure_ascii=False) + "\n",
        text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
        timeout=timeout, check=False,
    )
    if proc.returncode != 0:
        raise TraceError(f"runner failed (exit={proc.returncode}): {proc.stderr[-600:]}")
    lines = proc.stdout.splitlines()
    if len(lines) != 1:
        raise TraceError("runner must emit exactly one JSONL trace result")
    try:
        return json.loads(lines[0])
    except json.JSONDecodeError as exc:
        raise TraceError("invalid trace runner JSON") from exc


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--windows-command", default="")
    parser.add_argument("--macos-command", default="")
    parser.add_argument("--fixtures", type=Path, default=FIXTURES)
    args = parser.parse_args()
    cases = load_fixtures(args.fixtures)
    if not (args.windows_command and args.macos_command):
        if args.windows_command or args.macos_command:
            raise TraceError("both actual host runners are required")
        print(f"Validated {len(cases)} versioned JSONL fixture contracts; "
              "production dual-host replay NOT VERIFIED (runners unavailable).")
        return 0
    for case in cases:
        check_case(case, replay(args.windows_command, case),
                   replay(args.macos_command, case))
    print(f"Verified {len(cases)} production dual-host event/effect traces")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (TraceError, OSError, subprocess.TimeoutExpired) as exc:
        raise SystemExit(f"voiceflow trace: {exc}") from exc
