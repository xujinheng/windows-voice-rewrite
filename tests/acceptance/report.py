#!/usr/bin/env python3
"""Review-only same-state visual evidence. No screenshot can prove hardware acceptance."""
import argparse
import hashlib
import json
from pathlib import Path

IDENTITY = ("state", "fixture", "theme", "locale", "scale")
ALLOWED = {"windows", "macos-arm64", "macos-x64"}


def validate_manifest(path):
    root = path.parent.resolve()
    obj = json.loads(path.read_text(encoding="utf-8-sig"))
    if obj.get("schema") != 1 or obj.get("platform") not in ALLOWED:
        raise ValueError(f"{path}: unsupported manifest or platform")
    if obj.get("classification") != "simulated-ui":
        raise ValueError(f"{path}: only simulated-ui CI evidence is allowed")
    seen, entries = set(), []
    for entry in obj.get("captures", []):
        if not all(isinstance(entry.get(k), str) and entry[k] for k in IDENTITY):
            raise ValueError(f"{path}: missing state metadata")
        name = entry.get("image")
        if not isinstance(name, str) or not name.endswith(".png"):
            raise ValueError(f"{path}: image must be PNG")
        image = (root / name).resolve()
        if root not in image.parents or not image.is_file():
            raise ValueError(f"{path}: unsafe or missing image: {name}")
        data = image.read_bytes()
        if len(data) < 200 or not data.startswith(b"\x89PNG\r\n\x1a\n"):
            raise ValueError(f"{path}: invalid PNG: {name}")
        key = tuple(entry[k] for k in IDENTITY)
        if key in seen:
            raise ValueError(f"{path}: duplicate state: {key}")
        seen.add(key)
        entries.append((key, image))
    if not entries:
        raise ValueError(f"{path}: no screenshots were rendered")
    return obj, entries


def comparison(left, right, output):
    from PIL import Image, ImageChops, ImageOps, ImageStat
    with Image.open(left) as a0, Image.open(right) as b0:
        a, b = a0.convert("RGB"), b0.convert("RGB")
    def shrink(im):
        if im.height > 720:
            return im.resize((max(1, round(im.width * 720 / im.height)), 720),
                             Image.Resampling.LANCZOS)
        return im
    a, b = shrink(a), shrink(b)
    image = Image.new("RGB", (a.width + b.width + 24, max(a.height, b.height) + 24),
                      "#e5e7eb")
    image.paste(a, (8, 8))
    image.paste(b, (a.width + 16, 8))
    image.save(output)
    size = min(a.width, b.width), min(a.height, b.height)
    if 0 in size:
        raise ValueError("empty screenshot")
    delta = ImageChops.difference(
        ImageOps.fit(a, size, method=Image.Resampling.LANCZOS),
        ImageOps.fit(b, size, method=Image.Resampling.LANCZOS))
    return {
        "windows_size": list(a.size),
        "macos_size": list(b.size),
        "descriptive_mean_absolute_rgb": round(sum(ImageStat.Stat(delta).mean) / 3, 2),
        "warning": "Never classify parity as passed by raw pixel difference."
    }


def generate(paths, output):
    output.mkdir(parents=True, exist_ok=True)
    manifests = [validate_manifest(p) for p in paths]
    platforms = [manifest["platform"] for manifest, _ in manifests]
    if len(platforms) != len(set(platforms)):
        raise ValueError("duplicate platform manifest")
    index = {meta["platform"]: dict(cases) for meta, cases in manifests}
    windows = index.get("windows", {})
    rows = []
    for platform, cases in sorted(index.items()):
        if platform == "windows":
            continue
        # Keys include state AND fixture/theme/locale/scale. Do not compare
        # different UI states just because PNG filenames match.
        for key in sorted(set(windows) | set(cases)):
            w, m = windows.get(key), cases.get(key)
            row = {"platform": platform, "identity": dict(zip(IDENTITY, key)),
                   "status": "matched-for-review" if w and m else "missing-platform-capture",
                   "windows_present": bool(w), "macos_present": bool(m)}
            if w and m:
                name = f"{platform}-{hashlib.sha256(repr(key).encode()).hexdigest()[:12]}.png"
                row["side_by_side"] = name
                row["visual"] = comparison(w, m, output / name)
            rows.append(row)
    report = {
        "schema": 1,
        "classification": "simulated-ui",
        "acceptance": "NOT_VERIFIED_ON_PHYSICAL_DEVICE",
        "platforms": platforms,
        "captured_by_platform": {name: len(cases) for name, cases in index.items()},
        "paired_review_states": sum(x["status"] == "matched-for-review" for x in rows),
        "rows": rows
    }
    (output / "report.json").write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n")
    md = [
        "# Same-state UI evidence — review required", "",
        "**NOT a hardware verification or an automatic parity pass.**",
        "Only identical state, fixture, theme, locale and logical scale are paired.", "",
        "| Platform | State | Fixture | Theme | Evidence |",
        "|---|---|---|---|---|"
    ]
    for row in rows:
        i = row["identity"]
        evidence = (f"[Compare side by side]({row['side_by_side']})"
                    if "side_by_side" in row else "Missing matching state")
        md.append(f"| {row['platform']} | {i['state']} | {i['fixture']} | {i['theme']} | {evidence} |")
    md.extend(["", "A matched PNG proves a synthetic view rendered, not device insertion.",
               "Mouse4/5, microphone, permissions, Zoom/Teams, editor delivery and signed installs",
               "remain in Issue #73's physical acceptance matrix."])
    (output / "report.md").write_text("\n".join(md) + "\n", encoding="utf-8")
    return report


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("manifests", nargs="+", type=Path)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    report = generate(args.manifests, args.output)
    print(json.dumps({k: v for k, v in report.items() if k != "rows"}, ensure_ascii=False))


if __name__ == "__main__":
    main()
