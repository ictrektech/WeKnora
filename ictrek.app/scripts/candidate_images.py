#!/usr/bin/env python3
"""Validate an explicit candidate image map without evaluating shell syntax."""
import re
import sys
from pathlib import Path


IMAGE = re.compile(r"[a-z0-9.-]+(?::[0-9]+)?/[a-z0-9_./-]+:[A-Za-z0-9_][A-Za-z0-9_.-]*(?:@sha256:[0-9a-f]{64})?")


def write_candidate_images(source, target, expected):
    expected = tuple(expected)
    values = {}
    for number, line in enumerate(Path(source).read_text(encoding="utf-8").splitlines(), 1):
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        key, separator, value = line.partition("=")
        if not separator or key not in expected:
            raise ValueError(f"candidate image map has an unexpected key at line {number}")
        if key in values:
            raise ValueError(f"candidate image map repeats {key}")
        if not IMAGE.fullmatch(value) or "." not in value.split("/", 1)[0].split(":", 1)[0]:
            raise ValueError(f"candidate image map has an invalid registry reference for {key}")
        values[key] = value
    missing = set(expected) - values.keys()
    if missing:
        raise ValueError("candidate image map is missing: " + ", ".join(sorted(missing)))
    Path(target).write_text("".join(f"{key}={values[key]}\n" for key in expected), encoding="utf-8")


if __name__ == "__main__":
    try:
        write_candidate_images(sys.argv[1], sys.argv[2], sys.argv[3:])
    except (ValueError, OSError) as error:
        raise SystemExit(str(error))
