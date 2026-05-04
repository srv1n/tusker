#!/usr/bin/env python3
"""Rough Diátaxis classifier for Markdown/text files.

This is a heuristic helper, not a substitute for judgment.
"""
from __future__ import annotations

import argparse
import json
import re
from pathlib import Path
from typing import Dict, List

ACTION_VERBS = {
    "run", "create", "configure", "install", "deploy", "verify", "open", "click",
    "set", "add", "remove", "update", "copy", "paste", "start", "stop", "restart",
    "check", "choose", "select", "edit", "save", "build", "test", "connect",
}
REFERENCE_HINTS = {
    "parameter", "parameters", "field", "fields", "option", "options", "default",
    "type", "schema", "endpoint", "return", "returns", "error", "errors", "status",
    "attribute", "attributes", "method", "methods", "class", "classes", "config",
}
EXPLANATION_HINTS = {
    "why", "because", "rationale", "trade-off", "tradeoffs", "trade-offs",
    "concept", "concepts", "background", "architecture", "design", "history",
    "mental model", "overview", "understanding", "about",
}
TUTORIAL_HINTS = {
    "tutorial", "getting started", "first", "we will", "we'll", "learn", "lesson",
    "before we start", "what we will make", "next steps",
}
HOWTO_HINTS = {
    "how to", "how-to", "troubleshooting", "prerequisites", "before you begin",
    "steps", "procedure", "workflow", "task",
}


def read_text(path: Path) -> str:
    return path.read_text(encoding="utf-8", errors="replace")


def count_terms(text: str, terms: set[str]) -> int:
    t = text.lower()
    return sum(len(re.findall(r"\b" + re.escape(term) + r"\b", t)) for term in terms)


def count_imperative_lines(text: str) -> int:
    count = 0
    for line in text.splitlines():
        stripped = line.strip().lower()
        if not stripped or stripped.startswith(("#", "- [", "|")):
            continue
        first = re.split(r"\s+", stripped)[0].strip("`*_>:-")
        if first in ACTION_VERBS:
            count += 1
    return count


def classify(text: str) -> Dict[str, object]:
    lower = text.lower()
    headings = re.findall(r"^#{1,6}\s+(.+)$", text, flags=re.MULTILINE)
    table_rows = sum(1 for line in text.splitlines() if line.strip().startswith("|"))
    code_blocks = text.count("```") // 2
    imperative_lines = count_imperative_lines(text)
    word_count = len(re.findall(r"\w+", text))

    scores = {
        "tutorial": count_terms(lower, TUTORIAL_HINTS) + (2 if "we will" in lower or "we'll" in lower else 0),
        "how-to": count_terms(lower, HOWTO_HINTS) + imperative_lines,
        "reference": count_terms(lower, REFERENCE_HINTS) + table_rows // 2,
        "explanation": count_terms(lower, EXPLANATION_HINTS) + len(re.findall(r"\bwhy\b", lower)) * 2,
    }

    if re.search(r"^#\s+how to\b", lower, flags=re.MULTILINE):
        scores["how-to"] += 5
    if re.search(r"^#\s+.*reference\b", lower, flags=re.MULTILINE):
        scores["reference"] += 5
    if re.search(r"^#\s+(about|understanding)\b", lower, flags=re.MULTILINE):
        scores["explanation"] += 4
    if "expected output" in lower or "you should see" in lower:
        scores["tutorial"] += 3
        scores["how-to"] += 1

    ordered = sorted(scores.items(), key=lambda kv: kv[1], reverse=True)
    top_mode, top_score = ordered[0]
    second_mode, second_score = ordered[1]
    mixed = second_score >= max(3, top_score * 0.75)

    red_flags: List[str] = []
    if top_mode == "tutorial" and count_terms(lower, {"option", "choose", "alternative", "if"}) > 8:
        red_flags.append("Tutorial may contain too many branches/options; consider moving variants to how-to guides.")
    if top_mode == "how-to" and count_terms(lower, {"why", "concept", "background", "learn"}) > 6:
        red_flags.append("How-to guide may contain teaching/explanation creep.")
    if top_mode == "reference" and imperative_lines > 8:
        red_flags.append("Reference may contain procedure creep.")
    if top_mode == "explanation" and imperative_lines > 8:
        red_flags.append("Explanation may contain step-by-step instructions; split into a how-to guide.")
    if word_count < 150:
        red_flags.append("Very short document; classification confidence is low.")

    return {
        "mode_guess": "mixed" if mixed else top_mode,
        "top_mode": top_mode,
        "scores": scores,
        "word_count": word_count,
        "headings": headings[:20],
        "signals": {
            "imperative_lines": imperative_lines,
            "table_rows": table_rows,
            "code_blocks": code_blocks,
        },
        "red_flags": red_flags,
    }


def main() -> None:
    parser = argparse.ArgumentParser(description="Roughly classify a Markdown/text file using Diátaxis modes.")
    parser.add_argument("path", type=Path, help="Path to a Markdown or text file")
    parser.add_argument("--json", action="store_true", help="Emit JSON")
    args = parser.parse_args()

    text = read_text(args.path)
    result = classify(text)

    if args.json:
        print(json.dumps(result, indent=2, ensure_ascii=False))
    else:
        print(f"Mode guess: {result['mode_guess']}")
        print(f"Scores: {result['scores']}")
        print(f"Word count: {result['word_count']}")
        if result["red_flags"]:
            print("Red flags:")
            for flag in result["red_flags"]:
                print(f"- {flag}")


if __name__ == "__main__":
    main()
