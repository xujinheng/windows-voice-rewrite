#!/usr/bin/env python3
"""Validate physical-device evidence claims for #73; never auto-mark real hardware."""
import csv
import sys
from pathlib import Path

COLUMNS = [
    "run_id", "platform", "cpu", "os_version", "mouse_model", "driver_version",
    "app_version", "installer_sha256", "permission_state", "target_app",
    "target_version", "scenario", "result", "evidence", "notes"
]
RESULTS = {"not_run", "pass", "fail", "blocked"}
PLATFORMS = {"windows", "macos-arm64", "macos-x64"}


def validate(path):
    with path.open(newline="", encoding="utf-8-sig") as source:
        rows = csv.DictReader(source)
        if rows.fieldnames != COLUMNS:
            raise ValueError("physical-device matrix columns do not match schema")
        for n, row in enumerate(rows, start=2):
            if row["result"] not in RESULTS or row["platform"] not in PLATFORMS:
                raise ValueError(f"row {n}: invalid platform or result")
            if row["result"] == "not_run":
                continue
            required = [
                "run_id", "cpu", "os_version", "mouse_model", "driver_version",
                "app_version", "installer_sha256", "permission_state", "target_app",
                "target_version", "scenario", "evidence"
            ]
            missing = [name for name in required if not (row.get(name) or "").strip()]
            if missing:
                raise ValueError(f"row {n}: real outcome without device evidence: {missing}")
            sha = row["installer_sha256"].strip().lower()
            if len(sha) != 64 or any(ch not in "0123456789abcdef" for ch in sha):
                raise ValueError(f"row {n}: invalid installer SHA256")
    return True


if __name__ == "__main__":
    try:
        validate(Path(sys.argv[1]))
    except (OSError, ValueError, IndexError) as error:
        sys.exit(str(error))
    print("Matrix schema valid. not_run rows are NOT accepted.")
