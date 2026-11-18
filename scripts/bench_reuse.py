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


def present(snapshot: Snapshot, proof: dict) -> bool:
    source = snapshot.read(proof["path"])
    return source is not None and proof["contains"] in source


def decide_owner(snapshot: Snapshot, step: dict, journey_id: str) -> str:
    marker = journey_id + "/" + step["id"]
    if step["kind"] == "native_adapter":
        if not present(snapshot, step["swift"]):
            raise BenchmarkError("missing native adapter evidence " + marker + ": " + str(step["swift"]))
        return "native_swift"
    if step["kind"] != "portable_decision":
        raise BenchmarkError("invalid kind for " + marker)
    if "go" in step:
        go_present = all(present(snapshot, item) for item in step["go"])
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
    journeys = []
    for journey in catalog["journeys"]:
        seen = set()
        counts = {"shared_go": 0, "swift_policy": 0, "native_swift": 0}
        stages = []
        for step in journey["steps"]:
            if step["id"] in seen:
                raise BenchmarkError("duplicate stage " + journey["id"] + "/" + step["id"])
            seen.add(step["id"])
            owner = decide_owner(snapshot, step, journey["id"])
            counts[owner] += 1
            stages.append({"id": step["id"], "title": step["title"], "owner": owner})
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
        "schema_version": 1,
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
    inv = report["source_inventory"]
    lines += [
        "",
        f"**Source inventory only (not an executed-code ratio):** "
        f"shared Go Core + voicebridge {inv['shared_go']['nonblank_lines']:,} nonblank lines; "
        f"hand-authored macOS Swift {inv['native_swift']['nonblank_lines']:,}; "
        f"generated Swift {inv['generated_swift']['nonblank_lines']:,}. "
        f"Go share in this explicitly scoped source inventory: "
        f"{inv['shared_go_source_share_pct']:.1f}%.",
        "",
        f"Baseline: \`{base}\`." if base else "Baseline unavailable (new benchmark or shallow checkout).",
        "The full per-stage evidence and owners are in \`scripts/reuse_journeys.json\`; "
        "changing or removing a decision point requires code review.",
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
    if catalog.get("schema_version") != 1 or not catalog.get("journeys"):
        raise BenchmarkError("invalid journey catalog")
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
        old_report = run(old_snap, catalog)
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
