#!/usr/bin/env python3
"""Emit one deterministic, validated release provenance document."""
import json
import re
import sys


if len(sys.argv) != 6:
    raise SystemExit("usage: generate-release-provenance.py VERSION COMMIT EPOCH GO_VERSION TARGETS")

version, commit, epoch, go_version, targets_text = sys.argv[1:]
if not re.fullmatch(r"v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?", version):
    raise SystemExit("invalid release version")
if not re.fullmatch(r"[0-9a-f]{40}", commit):
    raise SystemExit("invalid release commit")
if not re.fullmatch(r"[0-9]+", epoch):
    raise SystemExit("invalid source date epoch")

targets = targets_text.split()
if not targets or any(not re.fullmatch(r"(?:darwin|linux|windows)/(?:arm64|amd64)", target) for target in targets):
    raise SystemExit("invalid release target matrix")

document = {
    "schema": "tusker.release-provenance/v1",
    "version": version,
    "commit": commit,
    "source_date_epoch": int(epoch),
    "go": go_version,
    "targets": targets,
}
print(json.dumps(document, sort_keys=True, separators=(",", ":")))
