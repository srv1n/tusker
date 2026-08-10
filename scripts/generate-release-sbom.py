#!/usr/bin/env python3
"""Emit a deterministic CycloneDX 1.5 inventory from locked Go and Bun inputs."""
import json
import subprocess
from pathlib import Path


def go_modules():
    raw = subprocess.check_output(["go", "list", "-m", "-json", "all"], text=True)
    decoder = json.JSONDecoder()
    position = 0
    result = []
    while position < len(raw):
        while position < len(raw) and raw[position].isspace():
            position += 1
        if position >= len(raw):
            break
        module, position = decoder.raw_decode(raw, position)
        name = module.get("Path")
        version = module.get("Version") or "workspace"
        if name:
            result.append({"type": "library", "name": name, "version": version, "purl": f"pkg:golang/{name}@{version}"})
    return result


def bun_packages():
    try:
        lock_text = Path("internal/serve/ui/bun.lock").read_text()
        # Bun's lockfile is JSON-shaped but permits trailing commas. Remove
        # only commas outside quoted strings immediately before a closing
        # object/array delimiter, then require the result to be valid JSON.
        cleaned = []
        in_string = False
        escaped = False
        for index, character in enumerate(lock_text):
            if character == '"' and not escaped:
                in_string = not in_string
            if character == ',' and not in_string:
                lookahead = lock_text[index + 1 :].lstrip()
                if lookahead.startswith(("}", "]")):
                    continue
            cleaned.append(character)
            escaped = character == "\\" and not escaped
            if character != "\\":
                escaped = False
        lock = json.loads("".join(cleaned))
    except (OSError, json.JSONDecodeError) as error:
        raise SystemExit(f"unable to parse Bun lockfile: {error}") from error
    packages = lock.get("packages")
    if lock.get("lockfileVersion") != 1 or not isinstance(packages, dict) or not packages:
        raise SystemExit("Bun lockfile has an unsupported or empty packages map")
    result = []
    for name, entry in packages.items():
        if not isinstance(name, str) or not isinstance(entry, list) or not entry or not isinstance(entry[0], str):
            raise SystemExit(f"Bun lockfile package entry is malformed: {name!r}")
        resolved = entry[0]
        name, _, version = resolved.rpartition("@")
        if not name or not version:
            raise SystemExit(f"Bun lockfile package has no resolved version: {resolved!r}")
        result.append({"type": "library", "name": name, "version": version, "purl": f"pkg:npm/{name}@{version}"})
    if len(result) != len(packages):
        raise SystemExit("Bun SBOM component count does not match lockfile package count")
    return result


components = go_modules() + bun_packages()
components.sort(key=lambda item: (item["purl"], item["name"]))
document = {
    "bomFormat": "CycloneDX",
    "specVersion": "1.5",
    "version": 1,
    "metadata": {"component": {"type": "application", "name": "tusker"}},
    "components": components,
}
print(json.dumps(document, sort_keys=True, separators=(",", ":")))
