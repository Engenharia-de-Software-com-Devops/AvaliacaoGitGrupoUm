#!/usr/bin/env python3
"""Aggregate Go coverage blocks into executable physical source lines."""

import re
import sys
from pathlib import Path

BLOCK = re.compile(r"^(.+):(\d+)\.(\d+),(\d+)\.(\d+) \d+ (\d+)$")


def measure(profile, filenames):
    files = {name: Path(name).read_text(encoding="utf-8").splitlines() for name in filenames}
    lines = {name: {} for name in filenames}
    for record in profile.splitlines()[1:]:
        match = BLOCK.fullmatch(record)
        if not match:
            raise ValueError(f"invalid coverage record: {record}")
        path, start_line, start_col, end_line, end_col, hits = match.groups()
        name = next((name for name in filenames if path == name or path.endswith("/" + name)), None)
        if name is None:
            continue
        start_line, start_col, end_line, end_col, hits = map(int, (start_line, start_col, end_line, end_col, hits))
        for number in range(start_line, end_line + 1):
            source = files[name][number - 1]
            first = start_col - 1 if number == start_line else 0
            last = end_col - 1 if number == end_line else len(source)
            fragment = source[first:last].split("//", 1)[0].strip(" \t{}();,")
            if fragment:
                lines[name][number] = lines[name].get(number, False) or hits > 0
    return lines


def main():
    if len(sys.argv) < 3:
        raise SystemExit("usage: line_coverage.py PROFILE FILE...")
    measured = measure(Path(sys.argv[1]).read_text(encoding="utf-8"), sys.argv[2:])
    failed = False
    for name, lines in measured.items():
        covered = sum(lines.values())
        total = len(lines)
        print(f"{name}: {covered}/{total} executable lines covered")
        if not total or covered != total:
            failed = True
            missing = [str(number) for number, hit in sorted(lines.items()) if not hit]
            print("  uncovered: " + (", ".join(missing) or "no coverage data"))
    if failed:
        raise SystemExit(1)


if __name__ == "__main__":
    main()
