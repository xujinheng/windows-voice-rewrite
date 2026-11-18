#!/usr/bin/env python3
"""Paired, real-audio Chinese/English ASR benchmark (stdlib only).

No synthetic transcripts or calls to paid services from CI. All reference
sentences are recording scripts, not measurements.
"""
import argparse
import json
import math
import os
import random
import re
import statistics
import sys
import time
import urllib.error
import urllib.request
import uuid
import wave
from pathlib import Path

ZH = re.compile(r"[\u3400-\u9fff]")
EN = re.compile(r"[A-Za-z]+(?:[.+/#-][A-Za-z0-9]+)*|[0-9]+")
MIXED = re.compile(r"[\u3400-\u9fff]|[A-Za-z]+(?:[.+/#-][A-Za-z0-9]+)*|[0-9]+")
STRICT_MIXED = re.compile(MIXED.pattern + r"|[^\s]")  # punctuation-aware strict mode
VARIANTS = ("baseline", "short", "terms")
SHORT_HINT = "Vocabulary: Chinese and English technical terms, as spoken."
TERMS_HINT = "Vocabulary: benchmark, prompt, Go, Swift, macOS, Windows, PR, CI, rebase, merge, dependency, issue, latency, regression, OpenAI, Qwen3-ASR, llama.cpp, VoiceRewrite, P50, P95, GitHub Actions, Go Core."


def records(path):
    with open(path, encoding="utf-8") as file:
        return [json.loads(line) for line in file if line.strip()]


def edit_distance(left, right):
    previous = list(range(len(right) + 1))
    for i, x in enumerate(left, 1):
        current = [i]
        for j, y in enumerate(right, 1):
            current.append(min(current[-1] + 1, previous[j] + 1,
                               previous[j - 1] + int(x != y)))
        previous = current
    return previous[-1]


def normalize(text, strict=False):
    tokens = (STRICT_MIXED if strict else MIXED).findall(text)
    return tokens if strict else [token.casefold() for token in tokens]


def contains_term(text, term, strict=False):
    # Delimiter-aware lexical membership; no substring "PR" inside "APR".
    source = text if strict else text.casefold()
    needle = term if strict else term.casefold()
    pattern = r"(?<![A-Za-z0-9])" + r"\s+".join(
        re.escape(x) for x in needle.split()) + r"(?![A-Za-z0-9])"
    return re.search(pattern, source) is not None


def percentile(values, p):
    if not values:
        return None
    ordered = sorted(values)
    i = (len(ordered) - 1) * p
    lo, hi = math.floor(i), math.ceil(i)
    return round(ordered[lo] + (ordered[hi] - ordered[lo]) * (i - lo), 3)


def div(n, d):
    return round(n / d, 6) if d else None


def clip_scores(item, text, glossary, strict=False):
    ref = item["reference"]
    terms = item["english_terms"]
    zh_ref = ZH.findall(ref)
    zh_out = ZH.findall(text)
    en_ref = EN.findall(ref)
    en_out = EN.findall(text)
    if not strict:
        en_ref = [x.casefold() for x in en_ref]
        en_out = [x.casefold() for x in en_out]
    ref_mix, out_mix = normalize(ref, strict), normalize(text, strict)
    # Precision is scoped to the declared fixed benchmark glossary. Free-form
    # unknown English words are not claimed to have been fully classified.
    present = {term for term in glossary if contains_term(text, term, strict)}
    expected = {term for term in terms if contains_term(ref, term, strict)}
    tp = len(present & expected)
    fp = len(present - expected)
    return {
        "english_exact_token_accuracy": max(0.0, 1 - edit_distance(en_ref, en_out) / len(en_ref)) if en_ref else None,
        "english_word_error_rate": div(edit_distance(en_ref, en_out), max(len(en_ref), 1)),
        "chinese_cer": div(edit_distance(zh_ref, zh_out), max(len(zh_ref), 1)) if zh_ref else None,
        "mixed_character_error_rate": div(edit_distance(list("".join(ref_mix)), list("".join(out_mix))), max(len("".join(ref_mix)), 1)),
        "mixed_token_error_rate": div(edit_distance(ref_mix, out_mix), max(len(ref_mix), 1)),
        "term_recall": div(tp, len(expected)) if expected else None,
        "term_tp": tp,
        "term_fp": fp,
        "term_fn": len(expected - present),
        "negative_hotword_false_positives": sum(contains_term(text, h, strict) for h in item.get("forbidden_terms", [])),
        "number_exact": div(sum(contains_term(text, n, strict) for n in set(re.findall(r"\b\d+\b", ref))), len(set(re.findall(r"\b\d+\b", ref)))) if re.search(r"\d", ref) else None,
    }


def macro(rows, field):
    values = [row[field] for row in rows if row[field] is not None]
    return round(statistics.mean(values), 6) if values else None


def bootstrap_paired(rows_a, rows_b, field, repetitions=2000):
    pairs = [(a[field], b[field]) for a, b in zip(rows_a, rows_b)
             if a[field] is not None and b[field] is not None]
    if not pairs:
        return None
    rng = random.Random(143)
    samples = []
    for _ in range(repetitions):
        draw = [pairs[rng.randrange(len(pairs))] for __ in pairs]
        samples.append(statistics.mean(b - a for a, b in draw))
    return [percentile(samples, .025), percentile(samples, .975)]


def summarize(manifest, results):
    ids = {item["id"] for item in manifest}
    if len(ids) != len(manifest):
        raise ValueError("duplicate manifest IDs")
    glossary = sorted({term for x in manifest for term in x["english_terms"]} |
                      {term for x in manifest for term in x.get("forbidden_terms", [])})
    by_variant = {}
    for result in results:
        key = (result["id"], result["variant"])
        if result["id"] not in ids or result["variant"] not in VARIANTS:
            raise ValueError(f"unknown clip/variant: {key}")
        if key in by_variant:
            raise ValueError(f"duplicate result: {key}")
        by_variant[key] = result
    output = {"protocol": "paired-asr-v1", "manifest_clips": len(manifest),
              "real_audio_measured": False, "variants": {}, "paired_deltas": {},
              "notes": "Vocabulary-scoped precision; loose mixed metrics ignore whitespace/punctuation; strict mixed metrics preserve casing and punctuation, ignoring whitespace. Han characters count as separate mixed WER tokens."}
    for variant in VARIANTS:
        observed = [by_variant.get((item["id"], variant)) for item in manifest]
        complete = bool(observed) and all(row is not None and row.get("status") == "ok" and isinstance(row.get("hypothesis"), str) and row.get("audio_sha256") for row in observed)
        if not complete:
            output["variants"][variant] = {"status": "not_measured", "coverage": sum(row is not None for row in observed),
                                            "clips": len(manifest), "failures": sum(row is not None and row.get("status") != "ok" for row in observed)}
            continue
        rows_loose = [clip_scores(item, row["hypothesis"], glossary) for item, row in zip(manifest, observed)]
        rows_strict = [clip_scores(item, row["hypothesis"], glossary, strict=True) for item, row in zip(manifest, observed)]
        def metrics(rows):
            tp, fp, fn = (sum(x[field] for x in rows) for field in ("term_tp", "term_fp", "term_fn"))
            return {**{field: macro(rows, field) for field in (
                "english_exact_token_accuracy", "english_word_error_rate",
                "chinese_cer", "mixed_character_error_rate", "mixed_token_error_rate",
                "term_recall", "number_exact")},
                "term_precision": div(tp, tp + fp), "term_micro_recall": div(tp, tp + fn),
                "term_f1": div(2 * tp, 2 * tp + fp + fn),
                "hotword_false_positives": sum(r["negative_hotword_false_positives"] for r in rows)}
        lat = [row.get("latency_ms") for row in observed if isinstance(row.get("latency_ms"), (int, float))]
        output["variants"][variant] = {
            "status": "measured", "clips": len(observed), "loose": metrics(rows_loose),
            "strict": metrics(rows_strict), "latency_p50_ms": percentile(lat, .5),
            "latency_p95_ms": percentile(lat, .95),
            "requests": sum(row.get("requests", 1) for row in observed),
            "errors": 0, "timeouts": 0}
    # A/B uses exactly the same SHA-256 audio per clip in every arm, or refuses.
    for candidate in ("short", "terms"):
        base = [by_variant.get((item["id"], "baseline")) for item in manifest]
        arm = [by_variant.get((item["id"], candidate)) for item in manifest]
        if not all(a and b and a.get("status") == b.get("status") == "ok"
                   and a.get("audio_sha256") and a["audio_sha256"] == b.get("audio_sha256")
                   for a, b in zip(base, arm)):
            output["paired_deltas"][candidate] = {"status": "not_measured"}
            continue
        a = [clip_scores(item, x["hypothesis"], glossary) for item, x in zip(manifest, base)]
        b = [clip_scores(item, x["hypothesis"], glossary) for item, x in zip(manifest, arm)]
        diffs = {}
        for field in ("term_recall", "chinese_cer", "english_exact_token_accuracy", "mixed_token_error_rate"):
            paired = [(x[field], y[field]) for x, y in zip(a, b) if x[field] is not None and y[field] is not None]
            diffs[field] = {"delta": round(statistics.mean(y - x for x, y in paired), 6) if paired else None,
                            "bootstrap_95_ci": bootstrap_paired(a, b, field)}
        output["paired_deltas"][candidate] = {"status": "measured", "candidate_minus_baseline": diffs,
                                               "additional_requests": sum(x.get("requests", 1)-y.get("requests", 1) for x,y in zip(arm,base))}
    output["real_audio_measured"] = all(v["status"] == "measured" for v in output["variants"].values()) and all(
        v["status"] == "measured" for v in output["paired_deltas"].values())
    return output


def render_markdown(report):
    lines = ["# Chinese/English ASR paired benchmark", "",
             f"Real audio fully measured: **{report['real_audio_measured']}**", "",
             "| Arm | Status | English term recall | Chinese CER | Hallucinated hotwords | P50/P95 (ms) | API requests |",
             "| --- | --- | --- | --- | --- | --- | --- |"]
    for name, values in report["variants"].items():
        m = values.get("loose", {})
        lines.append(f"| {name} | {values['status']} | {m.get('term_micro_recall','not_measured')} | {m.get('chinese_cer','not_measured')} | {m.get('hotword_false_positives','not_measured')} | {values.get('latency_p50_ms','not_measured')} / {values.get('latency_p95_ms','not_measured')} | {values.get('requests','not_measured')} |")
    lines.extend(["", "95% paired bootstrap CI and deltas (candidate minus baseline):", "",
                  "```json", json.dumps(report["paired_deltas"], ensure_ascii=False, indent=2), "```", "",
                  report["notes"], ""])
    return "\n".join(lines)


def run_audio(manifest, args):
    # Explicit network authorization prevents accidentally spending money in CI.
    if not args.allow_network or not args.endpoint or not args.model:
        raise ValueError("run requires --allow-network, --endpoint and --model")
    from hashlib import sha256
    endpoint = args.endpoint.rstrip("/")
    if not endpoint.endswith("/audio/transcriptions"):
        endpoint += "/audio/transcriptions"
    if not endpoint.startswith(("http://", "https://")):
        raise ValueError("endpoint must be http(s)")
    api_key = os.environ.get(args.api_key_env, "")
    output = []
    for clip in manifest:
        path = clip.get("audio_path")
        if not path:
            raise ValueError("record every clip first; no WAV for " + clip["id"])
        file = Path(args.audio_dir, path).resolve()
        if not file.is_relative_to(Path(args.audio_dir).resolve()):
            raise ValueError("audio_path escapes audio directory")
        with wave.open(str(file), "rb") as wav:
            if wav.getnchannels() != 1 or wav.getsampwidth() != 2 or wav.getframerate() != 16000 or wav.getcomptype() != "NONE":
                raise ValueError("all clips must be mono PCM16 16 kHz: " + clip["id"])
        wav_bytes = file.read_bytes()
        digest = sha256(wav_bytes).hexdigest()
        for variant, hint in (("baseline", ""), ("short", SHORT_HINT), ("terms", TERMS_HINT)):
            boundary = uuid.uuid4().hex
            fields = [("model", args.model), ("response_format", "json")]
            if hint:
                fields.append(("prompt", hint))
            body = bytearray()
            for key, value in fields:
                body.extend(f"--{boundary}\r\nContent-Disposition: form-data; name=\"{key}\"\r\n\r\n{value}\r\n".encode())
            body.extend(f"--{boundary}\r\nContent-Disposition: form-data; name=\"file\"; filename=\"voice.wav\"\r\nContent-Type: audio/wav\r\n\r\n".encode())
            body.extend(wav_bytes)
            body.extend(f"\r\n--{boundary}--\r\n".encode())
            headers = {"Content-Type": f"multipart/form-data; boundary={boundary}"}
            if api_key:
                headers["Authorization"] = "Bearer " + api_key
            request = urllib.request.Request(endpoint, bytes(body), headers=headers)
            begin = time.monotonic()
            result = {"id": clip["id"], "variant": variant, "audio_sha256": digest,
                      "model": args.model, "requests": 1}
            try:
                with urllib.request.urlopen(request, timeout=args.timeout) as response:
                    raw = response.read(3 << 20)
                    parsed = json.loads(raw)
                    result["hypothesis"] = parsed["text"] if isinstance(parsed, dict) else parsed
                    result["status"] = "ok"
            except (urllib.error.HTTPError, urllib.error.URLError, TimeoutError, ValueError, KeyError) as exc:
                result["status"] = "error"
                result["error_type"] = type(exc).__name__
            result["latency_ms"] = round((time.monotonic() - begin) * 1000, 3)
            output.append(result)
    return output


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=("evaluate", "run"))
    parser.add_argument("--manifest", default="scripts/asr_codeswitch_manifest.jsonl")
    parser.add_argument("--results", help="JSONL of actual provider results (evaluate)")
    parser.add_argument("--json-output", required=True)
    parser.add_argument("--markdown-output", required=True)
    parser.add_argument("--audio-dir", default=".")
    parser.add_argument("--endpoint")
    parser.add_argument("--model")
    parser.add_argument("--api-key-env", default="ASR_API_KEY")
    parser.add_argument("--timeout", type=int, default=90)
    parser.add_argument("--allow-network", action="store_true")
    args = parser.parse_args(argv)
    manifest = records(args.manifest)
    if args.mode == "run":
        output = run_audio(manifest, args)
        if not args.results:
            raise ValueError("run requires --results path to save raw observations")
        Path(args.results).write_text("".join(json.dumps(x, ensure_ascii=False)+"\n" for x in output), encoding="utf-8")
    else:
        output = records(args.results) if args.results else []
    report = summarize(manifest, output)
    Path(args.json_output).write_text(json.dumps(report, ensure_ascii=False, indent=2)+"\n", encoding="utf-8")
    Path(args.markdown_output).write_text(render_markdown(report), encoding="utf-8")
    print(f"measured={report['real_audio_measured']} clips={len(manifest)}")
    return 0 if report["real_audio_measured"] or args.mode == "evaluate" else 2


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (ValueError, OSError, KeyError, json.JSONDecodeError) as exc:
        print(f"benchmark not measured: {type(exc).__name__}: {exc}", file=sys.stderr)
        sys.exit(2)
