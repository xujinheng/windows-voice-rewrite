#!/usr/bin/env python3
"""Static, evidence-checked macOS user-journey ownership benchmark.

The unit is one reviewed portable *decision point* in one user journey. This
is not CPU profiling, dynamic branch coverage or executed-language time.
Every result is tied to source evidence on both platforms where applicable.
"""
from __future__ import annotations

import argparse
import json
import re
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
CATALOG = ROOT / "scripts" / "reuse_journeys.json"


class BenchmarkError(Exception):
    pass


class Snapshot:
    def __init__(self, root: Path, revision: str | None = None):
        self.root = root
        self.revision = revision
        self.cache: dict[str, str | None] = {}
        self.go_ast: dict[str, dict] = {}

    def read(self, path: str) -> str | None:
        if path.startswith("/") or ".." in Path(path).parts:
            raise BenchmarkError("unsafe evidence path: " + path)
        if path not in self.cache:
            if self.revision:
                proc = subprocess.run(
                    ["git", "show", self.revision + ":" + path],
                    cwd=self.root, text=True, stdout=subprocess.PIPE,
                    stderr=subprocess.DEVNULL, check=False,
                )
                self.cache[path] = proc.stdout if proc.returncode == 0 else None
            else:
                file = self.root / path
                self.cache[path] = file.read_text(encoding="utf-8") if file.is_file() else None
        return self.cache[path]

    def paths(self) -> list[str]:
        if self.revision:
            proc = subprocess.run(
                ["git", "ls-tree", "-r", "--name-only", self.revision],
                cwd=self.root, text=True, capture_output=True, check=True,
            )
            return proc.stdout.splitlines()
        proc = subprocess.run(
            ["git", "ls-files", "-z"], cwd=self.root,
            capture_output=True, check=True,
        )
        return [p.decode("utf-8") for p in proc.stdout.split(b"\0") if p]


# Reviewed baseline is intentionally not editable through the catalog. New
# journey occurrences or changes to IDs/kinds require a deliberate code review
# of this lock, rather than quietly changing the denominator.
FROZEN = {
    "dictation_hold": ("trigger:N", "admission:P", "duration:P", "capture:N",
        "release:P", "stop_dispatch:P", "asr:P", "result_route:P", "paste:N",
        "write_receipt:P", "history:P"),
    "spoken_selection": ("ax_selection:N", "input_route:P", "record_dispatch:P",
        "spoken_asr:P", "model_inference:P", "write_authorization:P",
        "ui_followup:P", "ax_replace:N", "write_receipt:P"),
    "recording_deadline": ("capture_stop:N", "max_clock:P", "deadline_stop:P",
        "stale_timeout:P", "asr:P", "indicator:N"),
    "cancel_failure": ("release:N", "epoch:P", "cancel:P", "deadline:P",
        "paste_lease:N", "delivery_receipt:P"),
    "meeting_live": ("system_capture:N", "session_epoch:P", "pipeline:P",
        "backpressure:P", "source_route:P", "render:N"),
}
REUSED_UNITS = {
    "spoken_selection/record_dispatch": "dictation_hold/stop_dispatch",
    "recording_deadline/asr": "dictation_hold/asr",
    "cancel_failure/delivery_receipt": "dictation_hold/write_receipt",
}
FROZEN_KEYS = {
    jid + "/" + item.split(":")[0]: ("native_adapter" if item.endswith(":N")
                                    else "portable_decision")
    for jid, items in FROZEN.items() for item in items
}

def validate_catalog(catalog: dict, strict: bool = False) -> None:
    if catalog.get("schema_version") not in (1, 2) or not catalog.get("journeys"):
        raise BenchmarkError("invalid journey catalog")
    if strict and catalog.get("baseline_lock") != "go70-38-v1":
        raise BenchmarkError("reviewed baseline_lock must not be removed")
    keys = []
    units: dict[str, tuple[str, str]] = {}
    for journey in catalog["journeys"]:
        for step in journey["steps"]:
            key = journey["id"] + "/" + step["id"]
            keys.append(key)
            unit = step.get("logical_unit_id", REUSED_UNITS.get(key, key))
            if not isinstance(unit, str) or not unit:
                raise BenchmarkError("invalid logical_unit_id: " + key)
            if step["kind"] not in ("portable_decision", "native_adapter"):
                raise BenchmarkError("invalid stage kind: " + key)
            if unit in units and units[unit][1] != step["kind"]:
                raise BenchmarkError("logical unit mixes native and policy: " + unit)
            units.setdefault(unit, (key, step["kind"]))
    if len(set(keys)) != len(keys):
        raise BenchmarkError("duplicate journey-stage key")
    if catalog.get("baseline_lock") == "go70-38-v1":
        if set(keys) != set(FROZEN_KEYS):
            raise BenchmarkError("frozen 38 journey occurrences changed")
        for journey in catalog["journeys"]:
            for step in journey["steps"]:
                key = journey["id"] + "/" + step["id"]
                if step["kind"] != FROZEN_KEYS[key]:
                    raise BenchmarkError("native/portable reclassification: " + key)
                expected = REUSED_UNITS.get(key, key)
                if step.get("logical_unit_id") != expected:
                    raise BenchmarkError("logical_unit_id changed: " + key)
                if not step.get("swift") or not step["swift"].get("path"):
                    raise BenchmarkError("stage without fallback/native evidence: " + key)

def prepare_ast(snapshot: Snapshot, catalog: dict) -> None:
    paths = {e["path"] for j in catalog["journeys"] for step in j["steps"]
             for e in step.get("go", [])}
    sources = {p: snapshot.read(p) for p in paths}
    sources = {p: src for p, src in sources.items() if src is not None}
    if not sources:
        return
    proc = subprocess.run(
        ["go", "run", str(Path(__file__).with_name("reuse_evidence.go"))],
        cwd=snapshot.root, input=json.dumps({"files": sources}),
        text=True, capture_output=True, check=False,
    )
    if proc.returncode:
        raise BenchmarkError("Go AST audit failed: " + proc.stderr.strip())
    snapshot.go_ast = json.loads(proc.stdout)["files"]

def go_proof(snapshot: Snapshot, proof: dict, purpose: str, owner: dict | None = None) -> bool:
    path = proof["path"]
    meta = snapshot.go_ast.get(path)
    if not meta:
        return False
    anchor = proof["contains"]
    if purpose == "function":
        found = re.search(r"func\s+(?:\([^)]*\)\s*)?([A-Za-z_]\w*)\s*\(", anchor)
        if not found:
            raise BenchmarkError("Go owner must name a specific function: " + path)
        function = found.group(1)
        if meta["functions"].get(function, False):
            return True
        # An entrypoint may delegate to an actual decision implementation
        # in the same parsed file (speech.Transcribe -> TranscribeWithPrompt).
        # An isolated forwarding-only wrapper still fails: its target must
        # itself contain a real branch, loop, assignment, or operation.
        return any(meta["functions"].get(callee, False)
                   for callee in meta["scoped_calls"].get(function, [])
                   if "." not in callee)
    found = re.search(r"([A-Za-z_]\w*)\.([A-Za-z_]\w*)\s*\(", anchor)
    if not found:
        raise BenchmarkError("Go host proof must name a package call: " + path)
    alias, func = found.groups()
    imported = meta["imports"].get(alias, "")
    # A local method or an unrelated import with a matching symbol is not
    # the portable package owner. Require a real imported Go core package.
    parent = owner["path"].rsplit("/", 1)[0].split("internal/core/", 1)[-1]
    calls = (meta["scoped_calls"].get(proof["within"], [])
             if proof.get("within") else meta["calls"])
    return (alias + "." + func in calls
            and imported.endswith("/internal/core/" + parent))

def present(snapshot: Snapshot, proof: dict) -> bool:
    source = snapshot.read(proof["path"])
    if source is None:
        return False
    # A comment claiming to call Go cannot serve as call-site evidence.
    # This remains static evidence rather than a complete parser/call graph.
    executable_lines = re.sub(r"(?s)/\*.*?\*/|//[^\n]*", "", source)
    return proof["contains"] in executable_lines


def decide_owner(snapshot: Snapshot, step: dict, journey_id: str) -> str:
    marker = journey_id + "/" + step["id"]
    if step["kind"] == "native_adapter":
        if not present(snapshot, step["swift"]):
            raise BenchmarkError("missing native adapter evidence " + marker + ": " + str(step["swift"]))
        return "native_swift"
    if step["kind"] != "portable_decision":
        raise BenchmarkError("invalid kind for " + marker)
    if "go" in step:
        go_present = (len(step["go"]) == 3
                      and go_proof(snapshot, step["go"][0], "function")
                      and all(go_proof(snapshot, item, "host", step["go"][0])
                              for item in step["go"][1:]))
        has_duplicate = any(present(snapshot, item) for item in step.get("go_forbidden_when", []))
        if go_present and not has_duplicate:
            return "shared_go"
    if present(snapshot, step["swift"]):
        return "swift_policy"
    raise BenchmarkError(
        "unmapped portable decision " + marker + ": neither authoritative Go "
        "evidence nor Swift fallback exists; update the reviewed catalog"
    )


def inventory(snapshot: Snapshot) -> dict:
    groups = {
        "shared_go": {"files": 0, "nonblank_lines": 0},
        "native_swift": {"files": 0, "nonblank_lines": 0},
        "generated_swift": {"files": 0, "nonblank_lines": 0},
    }
    for path in snapshot.paths():
        if "/Tests/" in path or path.endswith("_test.go") or path.endswith("Tests.swift"):
            continue
        group = None
        if path.endswith(".go") and (
            path.startswith("voice-rewrite/internal/core/")
            or path.startswith("voice-rewrite/cmd/voicebridge/")
        ):
            group = "shared_go"
        elif path.endswith(".swift") and (
            path.startswith("macos/VoiceRewrite/")
            or path.startswith("macos/Modules/")
        ):
            # Generated Swift is attributable to Go contracts, not hand-authored
            # native business logic. Keep it visible as a separate category.
            if path.endswith("/ContractLimits.swift") or path.endswith("/SettingsView.swift"):
                group = "generated_swift"
            else:
                group = "native_swift"
        if group is None:
            continue
        source = snapshot.read(path)
        if source is None:
            continue
        groups[group]["files"] += 1
        groups[group]["nonblank_lines"] += sum(1 for line in source.splitlines() if line.strip())
    total = sum(v["nonblank_lines"] for v in groups.values())
    groups["total_nonblank_lines"] = total
    groups["shared_go_source_share_pct"] = round(
        100 * groups["shared_go"]["nonblank_lines"] / total, 1
    ) if total else 0.0
    return groups


def run(snapshot: Snapshot, catalog: dict) -> dict:
    validate_catalog(catalog)
    prepare_ast(snapshot, catalog)
    journeys = []
    unique: dict[str, str] = {}
    for journey in catalog["journeys"]:
        seen = set()
        counts = {"shared_go": 0, "swift_policy": 0, "native_swift": 0}
        stages = []
        for step in journey["steps"]:
            if step["id"] in seen:
                raise BenchmarkError("duplicate stage " + journey["id"] + "/" + step["id"])
            seen.add(step["id"])
            owner = decide_owner(snapshot, step, journey["id"])
            unit = step.get("logical_unit_id", REUSED_UNITS.get(journey["id"] + "/" + step["id"], journey["id"] + "/" + step["id"]))
            if unit in unique and unique[unit] != owner:
                raise BenchmarkError("logical unit has conflicting owners: " + unit)
            unique[unit] = owner
            counts[owner] += 1
            stages.append({"id": step["id"], "title": step["title"], "owner": owner, "logical_unit_id": unit})
        portable = counts["shared_go"] + counts["swift_policy"]
        if portable == 0:
            raise BenchmarkError("journey has zero portable decisions: " + journey["id"])
        journeys.append({
            "id": journey["id"], "title": journey["title"],
            "go_decisions": counts["shared_go"],
            "swift_decisions": counts["swift_policy"],
            "native_boundaries": counts["native_swift"],
            "portable_decisions": portable,
            "go_portable_pct": round(100 * counts["shared_go"] / portable, 1),
            "stages": stages,
        })
    go = sum(j["go_decisions"] for j in journeys)
    swift = sum(j["swift_decisions"] for j in journeys)
    native = sum(j["native_boundaries"] for j in journeys)
    return {
        "schema_version": 2,
        "journey_occurrences": {"total": go + swift + native, "portable": go + swift,
                                "native": native, "go": go, "swift": swift,
                                "go_total_pct": round(100 * go / (go + swift + native), 1)},
        "unique_logical_units": {"total": len(unique),
                                 **{owner: list(unique.values()).count(owner) for owner in
                                    ("shared_go", "swift_policy", "native_swift")},
                                 "owners": dict(sorted(unique.items()))},
        "measurement": "reviewed static journey decision ownership (not dynamic runtime sampling)",
        "revision": snapshot.revision or "working_tree",
        "total": {
            "shared_go_decisions": go,
            "swift_policy_decisions": swift,
            "native_swift_boundaries": native,
            "portable_decisions": go + swift,
            "go_portable_pct": round(100 * go / (go + swift), 1),
        },
        "journeys": journeys,
        "source_inventory": inventory(snapshot),
    }


def catalog_keys(catalog: dict) -> set[str]:
    return {j["id"] + "/" + s["id"] for j in catalog["journeys"] for s in j["steps"]}


def compare(current: dict, baseline: dict) -> list[str]:
    issues = []
    indexed = {j["id"]: j for j in baseline["journeys"]}
    for journey in current["journeys"]:
        old = indexed.get(journey["id"])
        if old is not None:
            journey["base_go_portable_pct"] = old["go_portable_pct"]
            journey["go_pct_delta"] = round(journey["go_portable_pct"] - old["go_portable_pct"], 1)
            if journey["go_portable_pct"] < old["go_portable_pct"]:
                issues.append("shared-Go decision share fell in " + journey["id"])
    current_inventory = current["source_inventory"]
    baseline_inventory = baseline["source_inventory"]
    for kind in ("shared_go", "native_swift", "generated_swift"):
        current_inventory[kind]["base_nonblank_lines"] = baseline_inventory[kind]["nonblank_lines"]
        current_inventory[kind]["delta_nonblank_lines"] = (
            current_inventory[kind]["nonblank_lines"] - baseline_inventory[kind]["nonblank_lines"]
        )
    current_inventory["base_go_source_share_pct"] = baseline_inventory["shared_go_source_share_pct"]
    current["unique_logical_units"]["base_shared_go"] = baseline["unique_logical_units"]["shared_go"]
    current["unique_logical_units"]["go_delta"] = (
        current["unique_logical_units"]["shared_go"] - baseline["unique_logical_units"]["shared_go"])
    if current["unique_logical_units"]["shared_go"] < baseline["unique_logical_units"]["shared_go"]:
        issues.append("unique Go-owned unit count regressed")
    current["journey_occurrences"]["base_go_total_pct"] = baseline["journey_occurrences"]["go_total_pct"]
    current["journey_occurrences"]["go_total_pct_delta"] = round(
        current["journey_occurrences"]["go_total_pct"] - baseline["journey_occurrences"]["go_total_pct"], 1)
    current["total"]["base_go_portable_pct"] = baseline["total"]["go_portable_pct"]
    current["total"]["go_pct_delta"] = round(
        current["total"]["go_portable_pct"] - baseline["total"]["go_portable_pct"], 1
    )
    if current["total"]["go_portable_pct"] < baseline["total"]["go_portable_pct"]:
        issues.append("overall shared-Go decision share regressed")
    return issues


def markdown(report: dict, base: str | None, errors: list[str]) -> str:
    total = report["total"]
    lines = [
        "### Cross-platform code reuse benchmark",
        "",
        "> **Audited static user-journey decisions** — not runtime instruction "
        "counts, CPU time, or measured execution coverage. Each journey step "
        "counts once; native OS adapters are excluded from the portable denominator.",
        "",
        f"**Go-owned portable decisions: {total['shared_go_decisions']}/{total['portable_decisions']} "
        f"({total['go_portable_pct']:.1f}%)**; Swift-owned: {total['swift_policy_decisions']}; "
        f"separate native boundaries: {total['native_swift_boundaries']}.",
        f"**Frozen journey occurrences:** {report['journey_occurrences']['go']}/"
        f"{report['journey_occurrences']['total']} Go-owned "
        f"({report['journey_occurrences']['go_total_pct']:.1f}%); "
        f"{report['journey_occurrences']['portable']} portable, "
        f"{report['journey_occurrences']['native']} native.",
        f"**Unique logical units:** {report['unique_logical_units']['total']} "
        f"(Go {report['unique_logical_units']['shared_go']}, "
        f"Swift {report['unique_logical_units']['swift_policy']}, "
        f"native {report['unique_logical_units']['native_swift']}).",
        "",
        "| User journey | Go logic | Swift logic | Native boundaries | Go share | Baseline Go share |",
        "|---|---:|---:|---:|---:|---:|",
    ]
    for journey in report["journeys"]:
        previous = (str(journey["base_go_portable_pct"]) + "%") if "base_go_portable_pct" in journey else "—"
        lines.append(
            f"| {journey['title']} | {journey['go_decisions']} | {journey['swift_decisions']} "
            f"| {journey['native_boundaries']} | {journey['go_portable_pct']:.1f}% | {previous} |"
        )
    if "go_delta" in report["unique_logical_units"]:
        lines += [
            f"**Against base:** journey Go/38 "
            f"{report['journey_occurrences']['go_total_pct_delta']:+.1f} pp; "
            f"unique Go-owned units {report['unique_logical_units']['go_delta']:+d}."
        ]
    lines += [
        "",
        "<details><summary>Audited decision-stage owners (expand)</summary>",
        "",
        "| Journey / stage | Owner |",
        "|---|---|",
    ]
    for journey in report["journeys"]:
        for stage in journey["stages"]:
            lines.append(f"| {journey['id']} / {stage['title']} | `{stage['owner']}` (unit: `{stage['logical_unit_id']}`) |")
    lines += ["", "</details>"]
    inv = report["source_inventory"]
    source_deltas = ""
    if "base_nonblank_lines" in inv["shared_go"]:
        source_deltas = (
            f" Relative to base: Go {inv['shared_go']['delta_nonblank_lines']:+,} lines; "
            f"Swift {inv['native_swift']['delta_nonblank_lines']:+,} lines; "
            f"generated Swift {inv['generated_swift']['delta_nonblank_lines']:+,} lines."
        )
    lines += [
        "",
        f"**Source inventory only (not an executed-code ratio):** "
        f"shared Go Core + voicebridge {inv['shared_go']['nonblank_lines']:,} nonblank lines; "
        f"hand-authored macOS Swift {inv['native_swift']['nonblank_lines']:,}; "
        f"generated Swift {inv['generated_swift']['nonblank_lines']:,}. "
        f"Go share in this explicitly scoped source inventory: "
        f"{inv['shared_go_source_share_pct']:.1f}%.{source_deltas}",
        "",
        f"Baseline: \`{base}\`." if base else "Baseline unavailable (new benchmark or shallow checkout).",
        "The full per-stage evidence and owners are in \`scripts/reuse_journeys.json\`; "
        "changing or removing a decision point requires code review.",
        "**Coverage limitations:** Go AST checks actual source functions, meaningful "
        "bodies and imported host call expressions; this is not a whole-program "
        "call graph, effect execution proof or runtime trace. Swift exclusion "
        "requires curated forbidden anchors, and physical device/e2e acceptance "
        "must be checked separately.",
    ]
    if errors:
        lines += ["", "**Benchmark gate failed:** " + "; ".join(errors)]
    return "\n".join(lines) + "\n"


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--baseline-revision", default="")
    parser.add_argument("--json-output", default="")
    parser.add_argument("--summary", default="")
    parser.add_argument("--no-regression", action="store_true")
    parser.add_argument("--root", default=str(ROOT), help="test fixture repository")
    args = parser.parse_args()
    root = Path(args.root)
    catalog = json.loads((root / "scripts" / "reuse_journeys.json").read_text(encoding="utf-8"))
    validate_catalog(catalog, strict=(root.resolve() == ROOT.resolve()))
    current = run(Snapshot(root), catalog)
    errors = []
    if args.baseline_revision:
        old_snap = Snapshot(root, args.baseline_revision)
        old_text = old_snap.read("scripts/reuse_journeys.json")
        if old_text is not None:
            old_catalog = json.loads(old_text)
            removed = catalog_keys(old_catalog) - catalog_keys(catalog)
            if removed:
                errors.append("removed journey decisions without review: " + ", ".join(sorted(removed)))
        # Keep historical Go ownership proofs intact. A previously reviewed
        # Swift *fallback* may have become stale before this branch (for
        # example #131 changed the 60s recorder default to 180s). Refresh
        # only that fallback with a currently valid anchor present IN THE OLD
        # source, never inventing old Go ownership or changing its stage kind.
        if old_text is not None:
            current_by_key = {
                j["id"] + "/" + step["id"]: step
                for j in catalog["journeys"] for step in j["steps"]
            }
            for journey in old_catalog["journeys"]:
                for step in journey["steps"]:
                    current_step = current_by_key.get(journey["id"] + "/" + step["id"])
                    if (current_step is not None and step["kind"] == current_step["kind"]
                            and not present(old_snap, step["swift"])
                            and present(old_snap, current_step["swift"])):
                        step["swift"] = current_step["swift"]
        old_report = run(old_snap, old_catalog if old_text is not None else catalog)
        errors.extend(compare(current, old_report))
    if args.json_output:
        Path(args.json_output).write_text(
            json.dumps(current, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
        )
    doc = markdown(current, args.baseline_revision or None, errors)
    print(doc)
    if args.summary:
        with Path(args.summary).open("a", encoding="utf-8") as f:
            f.write(doc)
    if errors and args.no_regression:
        return 1
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (BenchmarkError, OSError, ValueError, KeyError, subprocess.CalledProcessError) as exc:
        print(f"reuse benchmark: {exc}", file=sys.stderr)
        sys.exit(2)
