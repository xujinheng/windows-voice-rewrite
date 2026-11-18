#!/usr/bin/env python3
"""Assert real XCTest coverage from xcodebuild output, including skipped tests.

XCTest emits both "Executed 78 tests, with 0 failures" and
"Executed 78 tests, with 2 tests skipped and 0 failures".
Count *passing*, not simply discovered, tests; skipped tests do not pass.
"""

import re
import sys
from pathlib import Path

SUMMARY = re.compile(
    r"Executed (\d+) tests?, with (?:(\d+) tests? skipped and )?(\d+) failures?"
)


def passing_counts(log: str) -> list[int]:
    """Passing count for every XCTest suite summary that reports zero failures."""
    return [
        int(total) - int(skipped or 0)
        for total, skipped, failures in SUMMARY.findall(log)
        if int(failures) == 0 and int(total) >= int(skipped or 0)
    ]


def verify_xctest(log: str, platform: str, min_passing: int = 69) -> int:
    counts = passing_counts(log)
    passed = max(counts, default=0)
    print(f"{platform} XCTest passing suite counts: {counts[-8:]}")
    if "** TEST SUCCEEDED **" not in log or passed < min_passing:
        raise ValueError(
            f"{platform} full XCTest suite missing or below "
            f"{min_passing} passing tests (found {passed})"
        )
    return passed


if __name__ == "__main__":
    if len(sys.argv) != 4:
        raise SystemExit("usage: ci_xctest_coverage.py LOG_FILE PLATFORM MIN_PASSING")
    log = Path(sys.argv[1]).read_text(encoding="utf-8", errors="replace")
    try:
        verify_xctest(log, sys.argv[2], int(sys.argv[3]))
    except ValueError as exc:
        raise SystemExit(str(exc)) from exc
