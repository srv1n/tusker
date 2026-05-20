#!/usr/bin/env python3
"""Audit a directory of Markdown files with rough Diátaxis classifications."""
from __future__ import annotations

import argparse
import csv
import json
import re
import sys
from pathlib import Path
from typing import Dict, List

SCRIPT_DIR = Path(__file__).resolve().parent
sys.path.insert(0, str(SCRIPT_DIR))
from classify_doc import classify  # type: ignore  # noqa: E402


def title_for(path: Path, text: str) -> str:
    match = re.search(r"^#\s+(.+)$", text, flags=re.MULTILINE)
    return match.group(1).strip() if match else path.stem.replace("-", " ").title()


def audit(root: Path) -> List[Dict[str, object]]:
    rows: List[Dict[str, object]] = []
    for path in sorted(root.rglob("*.md")):
        if ".git" in path.parts:
            continue
        text = path.read_text(encoding="utf-8", errors="replace")
        result = classify(text)
        rows.append({
            "path": str(path.relative_to(root)),
            "title": title_for(path, text),
            "mode_guess": result["mode_guess"],
            "top_mode": result["top_mode"],
            "word_count": result["word_count"],
            "red_flags": " | ".join(result["red_flags"]),
            "scores": json.dumps(result["scores"], sort_keys=True),
        })
    return rows


def write_csv(rows: List[Dict[str, object]], out: Path) -> None:
    out.parent.mkdir(parents=True, exist_ok=True)
    with out.open("w", encoding="utf-8", newline="") as f:
        writer = csv.DictWriter(f, fieldnames=["path", "title", "mode_guess", "top_mode", "word_count", "red_flags", "scores"])
        writer.writeheader()
        writer.writerows(rows)


def main() -> None:
    parser = argparse.ArgumentParser(description="Audit Markdown docs with rough Diátaxis classifications.")
    parser.add_argument("root", type=Path, help="Directory to scan")
    parser.add_argument("--format", choices=["csv", "json"], default="csv")
    parser.add_argument("--output", type=Path, help="Output file. Defaults to stdout.")
    args = parser.parse_args()

    rows = audit(args.root)
    if args.format == "json":
        data = json.dumps(rows, indent=2, ensure_ascii=False)
        if args.output:
            args.output.parent.mkdir(parents=True, exist_ok=True)
            args.output.write_text(data + "\n", encoding="utf-8")
        else:
            print(data)
    else:
        if args.output:
            write_csv(rows, args.output)
        else:
            writer = csv.DictWriter(sys.stdout, fieldnames=["path", "title", "mode_guess", "top_mode", "word_count", "red_flags", "scores"])
            writer.writeheader()
            writer.writerows(rows)


if __name__ == "__main__":
    main()
