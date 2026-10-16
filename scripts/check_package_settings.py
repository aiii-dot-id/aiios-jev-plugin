#!/usr/bin/env python3
"""Read-only post-package check: every declared settings field must survive."""

import argparse
import json
from pathlib import Path
import sys


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate JSON member: " + key)
        result[key] = value
    return result


def reject_constant(value):
    raise ValueError("non-JSON number: " + value)


def load_json(path):
    return json.loads(path.read_text(encoding="utf-8"),
                      object_pairs_hook=unique_object,
                      parse_constant=reject_constant)


def check_settings(author_path, stage):
    author = load_json(author_path)
    expected = author.get("settings") if isinstance(author, dict) else None
    if not isinstance(expected, list) or not expected:
        raise ValueError("plugin.json must declare a nonempty settings array")
    actual = load_json(stage / "install-root" / "settings.json")
    # Ignore object order/formatting, but preserve array order, all fields, and
    # JSON value types (Python's plain equality would equate true with 1).
    canonical = lambda value: json.dumps(value, sort_keys=True, allow_nan=False)
    if canonical(expected) != canonical(actual):
        raise ValueError(
            "staged settings differ from plugin.json (missing, changed, or added "
            "metadata); use an aiisdk CLI built from SDK 0.1.18 or later and "
            "repackage, never patch the staged package")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("stage", type=Path, help="packaged staging directory")
    parser.add_argument("--plugin", type=Path,
                        default=Path(__file__).resolve().parents[1] / "plugin.json",
                        help="author declaration (default: this repository's plugin.json)")
    args = parser.parse_args()
    try:
        check_settings(args.plugin, args.stage)
    except (OSError, ValueError) as error:
        print("settings check failed: " + str(error), file=sys.stderr)
        return 1
    print("settings check passed: complete author settings preserved")
    return 0


if __name__ == "__main__":
    sys.exit(main())
