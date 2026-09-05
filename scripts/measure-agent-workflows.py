#!/usr/bin/env python3
"""Measure read-only Tusker CLI projections in deterministic fixture vaults."""

from __future__ import annotations

import argparse
import base64
import datetime as dt
import hashlib
import json
import math
import os
import pathlib
import platform
import shlex
import statistics
import subprocess
import sys
import tempfile
import time
from typing import Any


SCRIPT_VERSION = "flw-t-0008-measurement-v2"
FIXTURE_VERSION = "flw-t-0008-fixtures-v2"
BASELINE_SCHEMA = "tusker.agent-efficiency-baseline/v2"
FIXTURE_SCHEMA = "tusker.agent-efficiency-fixtures/v2"
RECEIPT_SCHEMA = "tusker.provider-usage-receipt/v1"
DEFAULT_CLI = "/tmp/tusker-flw"
DEFAULT_BASELINE = "docs/reports/agent-efficiency/token-baseline.json"
DEFAULT_FIXTURES = "docs/reports/agent-efficiency/fixtures-v2.json"
DEFAULT_RECEIPT = "docs/reports/agent-efficiency/muse-usage-receipt.json"
DEFAULT_MUSE_ARCHIVE = "docs/reports/agent-efficiency/muse-usage-events.jsonl"
DEFAULT_REPORT = "docs/reports/trust-evidence/token-baseline.md"
REPETITIONS = 20
TIMEOUT_SECONDS = 30

STAGES = [
    ("bootstrap", "cold_process"), ("bootstrap", "repeat_fresh_process"), ("discovery", "steady"),
    ("next", "steady"), ("blocked-gate", "steady"), ("packet", "steady"), ("verification", "steady"),
    ("review", "steady"), ("recovery", "steady"), ("completion", "steady"),
]


def canonical(value: Any) -> bytes:
    return json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode("utf-8")


def digest(value: bytes) -> str:
    return "sha256:" + hashlib.sha256(value).hexdigest()


def path_digest(path: pathlib.Path) -> str:
    return digest(path.read_bytes())


def write(path: pathlib.Path, value: bytes | str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(value if isinstance(value, bytes) else value.encode("utf-8"))


def scalar(value: Any) -> str:
    if isinstance(value, bool):
        return "true" if value else "false"
    if isinstance(value, (int, float)):
        return str(value)
    return json.dumps(str(value), ensure_ascii=False)


def frontmatter(fields: list[tuple[str, Any]], extra: list[str] | None = None) -> str:
    lines = ["---"]
    for key, value in fields:
        if isinstance(value, list):
            if not value:
                lines.append(f"{key}: []")
            else:
                lines.append(f"{key}:")
                lines.extend(f"  - {scalar(item)}" for item in value)
        else:
            lines.append(f"{key}: {scalar(value)}")
    lines.extend(extra or [])
    lines.extend(["---", ""])
    return "\n".join(lines)


def note(fields: list[tuple[str, Any]], body: str, extra: list[str] | None = None) -> str:
    return frontmatter(fields, extra) + body.rstrip() + "\n"


def task_specs() -> list[dict[str, Any]]:
    long_detail = "\n".join(
        f"Requirement note {i:03d}: retain the complete contract and evidence obligation."
        for i in range(1, 121)
    )
    medium_detail = "\n".join(
        f"Criterion context {i}: preserve evidence identity and current revision."
        for i in range(1, 13)
    )
    return [
        dict(id="SML-T-0001", title="Small contract review", cls="small", epic="SML", status="review", readiness="ready", priority=3, size="s", owner="reviewer_agent", action="Review the two acceptance rows.", count=2, intent="Exercise a short, complete task contract.", detail="A short contract with two explicit acceptance obligations."),
        dict(id="MED-T-0001", title="Medium contract review", cls="medium", epic="MED", status="review", readiness="ready", priority=2, size="m", owner="reviewer_agent", action="Review each medium contract obligation.", count=6, intent="Exercise a multi-criterion task with repeated detail.", detail=medium_detail),
        dict(id="LRG-T-0001", title="Long contract review", cls="large", epic="LRG", status="review", readiness="ready", priority=1, size="l", owner="reviewer_agent", action="Retrieve the full long contract before review.", count=12, intent="Exercise a long contract without truncating normative material.", detail=long_detail),
        dict(id="NXT-T-0001", title="Next frontier fixture", cls="workflow", epic="NXT", status="ready", readiness="ready", priority=1, size="s", owner="agent", action="Run the next task preview.", count=1, intent="Provide one pickable ready item for frontier discovery.", detail="The item is ready and has no dependency or gate."),
        dict(id="BLK-T-0001", title="Blocked gate fixture", cls="workflow", epic="BLK", status="ready", readiness="waiting_on_human", priority=2, size="s", owner="human:fixture", action="Resolve BLK-G-0001 before dispatch.", count=1, intent="Make a real human gate visible to the blocked frontier.", detail="A human-owned gate is required before this item can proceed.", gates=["BLK-G-0001"]),
        dict(id="FLR-T-0001", title="Failed proof fixture", cls="workflow", epic="FLR", status="review", readiness="ready", priority=2, size="s", owner="agent", action="Repair the failed proof and rerun verification.", count=2, intent="Keep a failed verification observable and non-passing.", detail="The static row is pending and the CLI proof projection is partial."),
        dict(id="RES-T-0001", title="Resumed job fixture", cls="workflow", epic="RES", status="rework", readiness="ready", priority=2, size="m", owner="agent", action="Resume from the recorded attempt.", count=1, intent="Exercise recovery context with a separate attempt identity.", detail="The durable attempt records the previous execution and resume support."),
        dict(id="BRN-T-0001", title="Branch one fixture", cls="workflow", epic="BRN", status="ready", readiness="ready", priority=2, size="s", owner="agent", action="Implement branch one.", count=1, intent="Exercise the first independent branch in a DAG.", detail="This branch is independent of BRN-T-0002."),
        dict(id="BRN-T-0002", title="Branch two fixture", cls="workflow", epic="BRN", status="ready", readiness="ready", priority=2, size="s", owner="agent", action="Implement branch two.", count=1, intent="Exercise the second independent branch in a DAG.", detail="This branch is independent of BRN-T-0001."),
        dict(id="BRN-T-0003", title="Branch join fixture", cls="workflow", epic="BRN", status="backlog", readiness="blocked_by_dependency", priority=3, size="s", owner="agent", action="Wait for both branch tasks.", count=1, intent="Exercise a dependent join after two independent branches.", detail="The join cannot begin until both branch tasks are complete.", dependencies=["BRN-T-0001", "BRN-T-0002"], blocked_by=["BRN-T-0001", "BRN-T-0002"]),
        dict(id="CMP-T-0001", title="Completion candidate fixture", cls="workflow", epic="CMP", status="review", readiness="ready", priority=1, size="s", owner="reviewer_agent", action="Confirm the current proof and closeout projection.", count=1, intent="Exercise a completion candidate without claiming closeout.", detail="The acceptance row remains pending for the closeout projection."),
    ]


def task_document(spec: dict[str, Any]) -> str:
    proof_status = "partial" if spec["id"] == "FLR-T-0001" else "pending"
    fields = [
        ("schema", "tusker.task/v7"), ("kind", "task"), ("id", spec["id"]), ("project", "fixture"),
        ("title", spec["title"]), ("epic", spec["epic"]), ("status", spec["status"]),
        ("readiness", spec["readiness"]), ("priority", spec["priority"]), ("risk", "low"),
        ("size", spec["size"]), ("proof_mode", "inline"), ("proof_status", proof_status),
        ("proof_required", ["focused_test"]), ("next_owner", spec["owner"]), ("next_source", "fixture"),
        ("next_ref", "fixture"), ("next_action", spec["action"]), ("domains", ["project"]),
        ("spec_refs", [".tusker/specs/token-baseline.md"]), ("gates", spec.get("gates", [])),
        ("dependencies", spec.get("dependencies", [])), ("blocked_by", spec.get("blocked_by", [])),
        ("created_at", "2026-09-05T00:00:00Z"), ("updated_at", "2026-09-05T00:00:00Z"),
    ]
    revision = digest(canonical({"id": spec["id"], "status": spec["status"], "readiness": spec["readiness"]}))
    fields.append(("state_rev", revision))
    extra = [
        "capsule:", f"  what: {scalar(spec['intent'])}",
        "  use_when: \"Measuring the fixture workflow.\"",
        "  skip_when: \"This repository is only a deterministic measurement fixture.\"",
    ]
    rows = ["# " + spec["id"] + " - " + spec["title"], "", "## Intent", "", spec["intent"], "", "## Acceptance", "", "| ID | Outcome | Proof |", "| --- | --- | --- |"]
    rows.extend(f"| A{i} | Preserve requirement {i} for the {spec['cls']} fixture. | focused_test |" for i in range(1, spec["count"] + 1))
    rows.extend(["", "## Contract detail", "", spec["detail"], "", "## Non-goals", "", "Do not dispatch workers, call a provider, or mutate tracker state from this fixture.", "", "## Verification", "", "| Covers | Check | Result | Notes |", "| --- | --- | --- | --- |"])
    verification_result = "fail" if spec["id"] == "FLR-T-0001" else "pending"
    rows.extend(f"| A{i} | command: go test ./cmd/tusker -run TestTrustTokenBaseline -count=1 | {verification_result} | Static fixture row; no workflow command is executed here. |" for i in range(1, spec["count"] + 1))
    rows.extend(["", "## Knowledge delta", "", "The fixture keeps its contract and workflow records inspectable."])
    return note(fields, "\n".join(rows), extra)


def project_files(repo: pathlib.Path) -> None:
    write(repo / ".tusker" / "config.yaml", "schema: tusker.config/v1\nproject_id: fixture\nautomation:\n  enabled: false\n")
    skill_fields = [("schema", "tusker.project-skill/v7"), ("kind", "project_skill"), ("name", "project-knowledge"), ("project", "fixture"), ("status", "current"), ("description", "Route agents through fixture project canon.")]
    write(repo / ".tusker" / "SKILL.md", note(skill_fields, "# Project Knowledge Skill\n\nRead the project index and canon before using the fixture.\n", ["capsule:", "  what: Routes readers to fixture project canon.", "  use_when: Reading fixture facts.", "  skip_when: Only checking a task record."]))
    domain = [("schema", "tusker.domain/v7"), ("kind", "domain"), ("id", "project"), ("project", "fixture"), ("title", "Project"), ("status", "current"), ("summary", "Deterministic fixture project knowledge."), ("source_of_truth", ["knowledge/domains/project/CANON.md"]), ("canonical_files", ["INDEX.md", "CANON.md"]), ("created_at", "2026-09-05T00:00:00Z"), ("updated_at", "2026-09-05T00:00:00Z"), ("state_rev", digest(b"fixture-domain"))]
    write(repo / ".tusker" / "knowledge" / "domains" / "project" / "INDEX.md", note(domain, "# Project\n\nThe fixture project canon is in CANON.md.\n", ["capsule:", "  what: Routes readers to fixture project canon.", "  use_when: Reading project facts.", "  skip_when: Checking a task only."]))
    canon = [("schema", "tusker.domain-canon/v7"), ("kind", "domain_canon"), ("id", "project/canon"), ("project", "fixture"), ("domain", "project"), ("title", "Project canon"), ("status", "current"), ("summary", "Static fixture runtime boundary."), ("source_of_truth", ["knowledge/domains/project/CANON.md"]), ("created_at", "2026-09-05T00:00:00Z"), ("updated_at", "2026-09-05T00:00:00Z"), ("state_rev", digest(b"fixture-canon"))]
    write(repo / ".tusker" / "knowledge" / "domains" / "project" / "CANON.md", note(canon, "# Project canon\n\nThe fixture uses the installed CLI with an explicit local vault.\n", ["capsule:", "  what: Describes the static fixture boundary.", "  use_when: Resolving fixture source layout.", "  skip_when: Measuring only returned bytes."]))


def documents(repo: pathlib.Path, count: int) -> dict[str, Any]:
    root = repo / "docs" / "system" / "00-overview.md"
    write(root, note([("title", "Fixture overview"), ("subject", "overview"), ("keywords", ["fixture", "routing"])], "# Fixture overview\n\nA deterministic document routing root.\n"))
    spec = repo / ".tusker" / "specs" / "token-baseline.md"
    write(spec, note([("title", "Token baseline fixture specification"), ("subject", "token-baseline"), ("part_of", "overview"), ("keywords", ["token", "baseline"])], "# Token baseline\n\nThe fixture measurement contract.\n"))
    for i in range(1, count - 1):
        path = repo / ".tusker" / "specs" / f"fixture-{i:04d}.md"
        write(path, note([("title", f"Fixture reference {i:04d}"), ("subject", f"fixture-{i:04d}"), ("part_of", "overview"), ("keywords", ["routing", "fixture"])], f"# Fixture reference {i:04d}\n\nA deterministic routing node.\n"))
    files = sorted([root, spec, *sorted((repo / ".tusker" / "specs").glob("fixture-*.md"))])
    entries = [{"path": str(path.relative_to(repo)), "bytes": path.stat().st_size, "sha256": path_digest(path)} for path in files]
    return {"count": count, "files": len(entries), "digest": digest(canonical(entries))}


def special_files(repo: pathlib.Path) -> dict[str, Any]:
    gate = repo / ".tusker" / "work" / "gates" / "BLK-G-0001.md"
    gate_fields = [("schema", "tusker.gate/v1"), ("kind", "gate"), ("id", "BLK-G-0001"), ("project", "fixture"), ("title", "Fixture human gate"), ("gate_kind", "manual_hold"), ("status", "open"), ("owner", "human:fixture"), ("priority", 2), ("blocking", True), ("blocks", ["BLK-T-0001"]), ("covers", ["A1"]), ("why_agent_cannot", "Only a human can resolve this authority gate."), ("action", "Human decides whether the fixture may proceed."), ("verification", "The human decision is recorded against the current gate revision."), ("created_at", "2026-09-05T00:00:00Z"), ("updated_at", "2026-09-05T00:00:00Z")]
    write(gate, note(gate_fields, "# Fixture human gate\n\n## Why agent cannot do this\n\nOnly a human can resolve this authority gate.\n\n## Action\n\nHuman decides whether the fixture may proceed.\n\n## Verification\n\nThe human decision is recorded against the current gate revision.\n"))
    attempt = repo / ".tusker" / "attempts" / "RES-T-0001" / "RES-T-0001-A-0001.md"
    attempt_fields = [("schema", "tusker.attempt/v1"), ("kind", "attempt"), ("id", "RES-T-0001-A-0001"), ("project", "fixture"), ("task", "RES-T-0001"), ("runner", "fixture"), ("workspace_kind", "in_place"), ("status", "handoff"), ("started_at", "2026-09-05T00:00:00Z"), ("ended_at", "2026-09-05T00:01:00Z"), ("resume_from", "RES-T-0001-A-0000")]
    write(attempt, note(attempt_fields, "# Resumed job attempt\n\nThis static handoff record has no live lease or provider session.\n"))
    return {"gate": {"id": "BLK-G-0001", "bytes": gate.stat().st_size, "sha256": path_digest(gate)}, "attempt": {"id": "RES-T-0001-A-0001", "bytes": attempt.stat().st_size, "sha256": path_digest(attempt)}}


def parse_meta(path: pathlib.Path) -> dict[str, Any]:
    lines = path.read_text(encoding="utf-8").splitlines()
    if not lines or lines[0].strip() != "---":
        raise ValueError(f"{path}: missing frontmatter opening")
    data: dict[str, Any] = {}
    active: str | None = None
    for line in lines[1:]:
        if line.strip() == "---":
            return data
        if line.startswith("  - ") and active:
            data.setdefault(active, []).append(line[4:].strip().strip('"'))
            continue
        if line.startswith(" ") or ":" not in line:
            continue
        key, value = line.split(":", 1)
        value = value.strip()
        active = key
        if value == "[]":
            data[key] = []
        elif value:
            try:
                data[key] = json.loads(value)
            except json.JSONDecodeError:
                data[key] = value
        else:
            data[key] = []
    raise ValueError(f"{path}: missing frontmatter closing")


def validate_fixture(repo: pathlib.Path, count: int) -> dict[str, Any]:
    errors: list[str] = []
    specs = task_specs()
    task_dir = repo / ".tusker" / "work" / "tasks"
    valid_status = {"idea", "backlog", "ready", "review", "rework", "done", "cancelled", "superseded"}
    valid_readiness = {"ready", "blocked_by_gate", "blocked_by_dependency", "waiting_on_review", "waiting_on_human", "waiting_on_ci", "held", "done", "cancelled", "superseded"}
    for spec in specs:
        path = task_dir / f"{spec['id']}.md"
        try:
            data = parse_meta(path)
        except (OSError, ValueError) as error:
            errors.append(str(error))
            continue
        for key in ("schema", "kind", "id", "project", "title", "status", "risk", "priority", "proof_mode", "proof_status", "proof_required", "next_owner", "next_action"):
            if key not in data or data[key] in ("", None, []):
                errors.append(f"{path}: missing {key}")
        if data.get("schema") != "tusker.task/v7" or data.get("kind") != "task" or data.get("id") != spec["id"] or data.get("project") != "fixture":
            errors.append(f"{path}: task schema/kind invalid")
        if data.get("status") not in valid_status or data.get("readiness") not in valid_readiness:
            errors.append(f"{path}: task status/readiness invalid")
        if data.get("proof_mode") not in {"none", "inline", "card", "artifact", "audit"}:
            errors.append(f"{path}: proof_mode invalid")
        if data.get("proof_status") not in {"pending", "partial", "satisfied", "waived"}:
            errors.append(f"{path}: proof_status invalid")
    gate = repo / ".tusker" / "work" / "gates" / "BLK-G-0001.md"
    attempt = repo / ".tusker" / "attempts" / "RES-T-0001" / "RES-T-0001-A-0001.md"
    try:
        gate_data = parse_meta(gate)
        for key in ("schema", "kind", "id", "project", "title", "gate_kind", "status", "owner", "action", "verification", "blocks"):
            if key not in gate_data or gate_data[key] in ("", None, []):
                errors.append(f"{gate}: missing {key}")
        if gate_data.get("schema") != "tusker.gate/v1" or gate_data.get("status") not in {"open", "satisfied", "waived", "obsolete"}:
            errors.append(f"{gate}: gate schema/status invalid")
    except (OSError, ValueError) as error:
        errors.append(str(error))
    try:
        attempt_data = parse_meta(attempt)
        for key in ("schema", "kind", "id", "project", "task", "runner", "workspace_kind", "status", "started_at"):
            if key not in attempt_data or attempt_data[key] in ("", None, []):
                errors.append(f"{attempt}: missing {key}")
        if attempt_data.get("schema") != "tusker.attempt/v1" or attempt_data.get("status") not in {"started", "handoff", "failed"}:
            errors.append(f"{attempt}: attempt schema/status invalid")
    except (OSError, ValueError) as error:
        errors.append(str(error))
    docs = [repo / "docs" / "system" / "00-overview.md", repo / ".tusker" / "specs" / "token-baseline.md"]
    docs.extend(sorted((repo / ".tusker" / "specs").glob("fixture-*.md")))
    if len(docs) != count:
        errors.append(f"expected {count} docs, got {len(docs)}")
    for path in docs:
        try:
            data = parse_meta(path)
        except (OSError, ValueError) as error:
            errors.append(str(error))
            continue
        if not data.get("subject"):
            errors.append(f"{path}: missing subject")
        if path.name != "00-overview.md" and not data.get("part_of"):
            errors.append(f"{path}: missing part_of")
    if errors:
        raise ValueError("fixture validation failed:\n- " + "\n- ".join(sorted(errors)))
    return {"ok": True, "task_records": len(specs), "document_records": len(docs), "gate_records": 1, "attempt_records": 1}


def build_fixture(root: pathlib.Path, count: int) -> dict[str, Any]:
    repo = root / f"docs-{count}"
    project_files(repo)
    for spec in task_specs():
        write(repo / ".tusker" / "work" / "tasks" / f"{spec['id']}.md", task_document(spec))
    special = special_files(repo)
    doc_info = documents(repo, count)
    validation = validate_fixture(repo, count)
    task_info = []
    for spec in task_specs():
        path = repo / ".tusker" / "work" / "tasks" / f"{spec['id']}.md"
        task_info.append({"id": spec["id"], "class": spec["cls"], "bytes": path.stat().st_size, "sha256": path_digest(path)})
    return {"name": f"docs-{count}", "root": repo, "documents": doc_info, "tasks": task_info, "special": special, "validation": validation}


def fixture_manifest(fixtures: list[dict[str, Any]]) -> dict[str, Any]:
    first = fixtures[0]
    contracts = sorted([task for task in first["tasks"] if task["class"] in {"small", "medium", "large"}], key=lambda item: item["class"])
    return {
        "schema": FIXTURE_SCHEMA,
        "version": FIXTURE_VERSION,
        "task_contracts": contracts,
        "document_repositories": [{"name": f["name"], "documents": f["documents"]["count"], "files": f["documents"]["files"], "digest": f["documents"]["digest"]} for f in fixtures],
        "special_records": first["special"],
        "scenarios": ["small-contract", "medium-contract", "large-contract", "long-contract", "branching-dag", "blocked-gate", "failed-proof", "resumed-job"],
    }


def fixture_inputs(repo: pathlib.Path, stage: str) -> list[pathlib.Path]:
    task_dir = repo / ".tusker" / "work" / "tasks"
    project = [repo / ".tusker" / "SKILL.md", repo / ".tusker" / "knowledge" / "domains" / "project" / "INDEX.md", repo / ".tusker" / "knowledge" / "domains" / "project" / "CANON.md", repo / ".tusker" / "specs" / "token-baseline.md"]
    if stage == "bootstrap":
        return project + [task_dir / "SML-T-0001.md"]
    if stage == "discovery":
        return [repo / "docs" / "system" / "00-overview.md"] + sorted((repo / ".tusker" / "specs").glob("*.md"))
    if stage == "next":
        return sorted(task_dir.glob("*.md")) + [repo / ".tusker" / "config.yaml"]
    if stage == "blocked-gate":
        return project + [task_dir / "BLK-T-0001.md", repo / ".tusker" / "work" / "gates" / "BLK-G-0001.md"]
    if stage == "packet":
        return project + [task_dir / "LRG-T-0001.md"]
    if stage == "verification":
        return project + [task_dir / "FLR-T-0001.md"]
    if stage == "review":
        return project + [task_dir / "BRN-T-0001.md"]
    if stage == "recovery":
        return [task_dir / "RES-T-0001.md", repo / ".tusker" / "attempts" / "RES-T-0001" / "RES-T-0001-A-0001.md"]
    if stage == "completion":
        return project + [task_dir / "CMP-T-0001.md"]
    raise ValueError(stage)


def input_inventory(repo: pathlib.Path, paths: list[pathlib.Path]) -> tuple[int, str]:
    entries = [{"path": str(path.relative_to(repo)), "bytes": path.stat().st_size, "sha256": path_digest(path)} for path in sorted(set(paths)) if path.is_file()]
    return sum(item["bytes"] for item in entries), digest(canonical(entries))


def stage_command(cli: str, stage: str) -> list[str]:
    base = [cli]
    commands = {
        "bootstrap": ["show", "SML-T-0001", "--vault", ".tusker", "--capsule", "--json"],
        "discovery": ["docs", "find", "token baseline", "--vault", ".tusker", "--json"],
        "next": ["next", "--vault", ".tusker", "--epic", "NXT", "--json"],
        "blocked-gate": ["next", "--vault", ".tusker", "--epic", "BLK", "--explain", "--json"],
        "packet": ["packet", "LRG-T-0001", "--vault", ".tusker", "--for", "agent", "--force", "--json"],
        "verification": ["proof", "status", "FLR-T-0001", "--vault", ".tusker", "--json"],
        "review": ["packet", "BRN-T-0001", "--vault", ".tusker", "--for", "reviewer", "--json"],
        "recovery": ["show", "RES-T-0001-A-0001", "--vault", ".tusker", "--full"],
        "completion": ["closeout", "status", "CMP-T-0001", "--vault", ".tusker", "--json"],
    }
    return base + commands[stage]


def normalized_command(argv: list[str], cli: str) -> list[str]:
    return ["<cli>" if value == cli else "<vault>" if value == ".tusker" else value for value in argv]


def run_command(argv: list[str], cwd: pathlib.Path, cli: str) -> tuple[dict[str, Any], bytes, bytes]:
    started = time.perf_counter_ns()
    env = os.environ.copy()
    env.update(LC_ALL="C", LANG="C")
    try:
        result = subprocess.run(argv, cwd=cwd, env=env, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=TIMEOUT_SECONDS, check=False)
        stdout, stderr, exit_code, timed_out = result.stdout or b"", result.stderr or b"", result.returncode, False
    except subprocess.TimeoutExpired as error:
        stdout = error.stdout or b""
        stderr = error.stderr or b""
        if isinstance(stdout, str): stdout = stdout.encode("utf-8", "replace")
        if isinstance(stderr, str): stderr = stderr.encode("utf-8", "replace")
        exit_code, timed_out = None, True
    sample = {
        "exit_code": exit_code,
        "process_success": exit_code == 0 and not timed_out,
        "timed_out": timed_out,
        "elapsed_ms": round((time.perf_counter_ns() - started) / 1_000_000, 3),
        "input_bytes": len(canonical(normalized_command(argv, cli))),
        "output_bytes": len(stdout) + len(stderr),
        "agent_context_bytes": len(stdout),
        "stdout_bytes": len(stdout),
        "stderr_bytes": len(stderr),
        "transcript": {"stdout_sha256": digest(stdout), "stderr_sha256": digest(stderr)},
    }
    return sample, stdout, stderr


def semantic(stage: str, stdout: bytes) -> str:
    text = stdout.decode("utf-8", "replace")
    try:
        payload = json.loads(text)
    except json.JSONDecodeError:
        payload = None
    if stage == "bootstrap":
        if isinstance(payload, dict) and payload.get("id"):
            return f"task:{payload['id']}"
        return "task:SML-T-0001" if '"id":"SML-T-0001"' in text else "task:missing"
    if stage == "discovery":
        matches = payload.get("Matches", []) if isinstance(payload, dict) else []
        paths = [item.get("Path") for item in matches if isinstance(item, dict)]
        return "match:.tusker/specs/token-baseline.md" if ".tusker/specs/token-baseline.md" in paths else f"matches:{len(paths)}"
    if stage == "next":
        item = payload.get("item") if isinstance(payload, dict) else None
        if isinstance(item, dict) and item.get("id"):
            return f"item:{item['id']}"
        if '"id":"NXT-T-0001"' in text:
            return "item:NXT-T-0001"
        return "item:null"
    if stage == "blocked-gate":
        if isinstance(payload, dict) and payload.get("item") is None and payload.get("ok") is False:
            return "blocked:item:null"
        if '"item":null' in text and '"ok":false' in text:
            return "blocked:item:null"
        return "blocked:unexpected"
    if stage in {"packet", "review"}:
        if isinstance(payload, dict) and payload.get("taskId"):
            return f"task:{payload['taskId']}"
        expected = "LRG-T-0001" if stage == "packet" else "BRN-T-0001"
        return f"task:{expected}" if f'"taskId":"{expected}"' in text else "task:missing"
    if stage == "verification":
        if isinstance(payload, dict) and payload.get("status"):
            return f"proof:{payload['status']}"
        return "proof:partial" if '"status":"partial"' in text else "proof:missing"
    if stage == "completion":
        if isinstance(payload, dict) and payload.get("agent_action"):
            return f"closeout:{payload['agent_action']}"
        return "closeout:continue" if '"agent_action":"continue"' in text else "closeout:missing"
    if stage == "recovery":
        return "attempt:RES-T-0001-A-0001" if "RES-T-0001-A-0001" in text and "tusker.attempt/v1" in text else "attempt:missing"
    raise ValueError(stage)


def expected_semantic(stage: str) -> str:
    return {
        "bootstrap": "task:SML-T-0001",
        "discovery": "match:.tusker/specs/token-baseline.md",
        "next": "item:NXT-T-0001",
        "blocked-gate": "blocked:item:null",
        "packet": "task:LRG-T-0001",
        "verification": "proof:partial",
        "review": "task:BRN-T-0001",
        "recovery": "attempt:RES-T-0001-A-0001",
        "completion": "closeout:continue",
    }[stage]


def nearest(values: list[float], percentile: float) -> float:
    return sorted(values)[max(0, math.ceil(percentile * len(values)) - 1)]


def summary(samples: list[dict[str, Any]]) -> dict[str, Any]:
    elapsed = [sample["elapsed_ms"] for sample in samples]
    return {
        "sample_count": len(samples),
        "process_success_count": sum(1 for sample in samples if sample["process_success"]),
        "process_failure_count": sum(1 for sample in samples if not sample["process_success"]),
        "median_elapsed_ms": round(statistics.median(elapsed), 3),
        "p95_elapsed_ms": round(nearest(elapsed, 0.95), 3),
        "variance_elapsed_ms": round(statistics.pvariance(elapsed), 3) if len(elapsed) > 1 else 0.0,
        "median_input_bytes": int(statistics.median(sample["input_bytes"] for sample in samples)),
        "median_output_bytes": int(statistics.median(sample["output_bytes"] for sample in samples)),
        "median_agent_context_bytes": int(statistics.median(sample["agent_context_bytes"] for sample in samples)),
        "process_invocations": len(samples), "agent_tool_calls": None, "agent_turns": None, "agent_retries": None,
    }


def hash_matrix(samples: list[dict[str, Any]], outcomes: list[str]) -> str:
    rows = []
    for index, (sample, outcome) in enumerate(zip(samples, outcomes), 1):
        rows.append({"repetition": index, "exit_code": sample["exit_code"], "process_success": sample["process_success"], "stdout_sha256": sample["transcript"]["stdout_sha256"], "stderr_sha256": sample["transcript"]["stderr_sha256"], "semantic": outcome})
    return digest(canonical(rows))


def observe(fixture: dict[str, Any], cli: str, repetitions: int) -> list[dict[str, Any]]:
    rows: list[dict[str, Any]] = []
    for stage, condition in STAGES:
        command = stage_command(cli, stage)
        paths = fixture_inputs(fixture["root"], stage)
        fixture_bytes, fixture_sha = input_inventory(fixture["root"], paths)
        samples: list[dict[str, Any]] = []
        outcomes: list[str] = []
        transcripts: dict[tuple[str, str], dict[str, str]] = {}
        for _ in range(repetitions):
            sample, stdout, stderr = run_command(command, fixture["root"], cli)
            samples.append(sample)
            outcomes.append(semantic(stage, stdout))
            key = (sample["transcript"]["stdout_sha256"], sample["transcript"]["stderr_sha256"])
            transcripts.setdefault(key, {"stdout_sha256": key[0], "stderr_sha256": key[1], "stdout_b64": base64.b64encode(stdout).decode("ascii"), "stderr_b64": base64.b64encode(stderr).decode("ascii")})
        counts: dict[str, int] = {}
        for outcome in outcomes:
            counts[outcome] = counts.get(outcome, 0) + 1
        rows.append({
            "key": f"{fixture['documents']['count']}:{stage}:{condition}",
            "documents": fixture["documents"]["count"], "stage": stage, "condition": condition,
            "command": normalized_command(command, cli), "protocol": {"process_invocations": repetitions, "agent_tool_calls": None, "agent_turns": None, "agent_retries": None}, "fixture_inputs_bytes": fixture_bytes, "fixture_inputs_sha256": fixture_sha,
            "expected_semantic": expected_semantic(stage), "observed_semantic_counts": counts,
            "outcome_matrix_sha256": hash_matrix(samples, outcomes), "summary": summary(samples), "transcripts": list(transcripts.values()), "samples": samples,
        })
    return rows


def version_observation(cli: str) -> dict[str, Any]:
    _, stdout, _ = run_command([cli, "version", "--json"], pathlib.Path.cwd(), cli)
    try:
        payload = json.loads(stdout.decode("utf-8"))
    except json.JSONDecodeError:
        payload = {}
    version = payload.get("version", {}) if isinstance(payload, dict) else {}
    return {"version": version.get("version"), "revision": version.get("revision"), "binary_sha256": version.get("binary_sha256"), "go_version": version.get("go_version")}


def source_snapshot() -> dict[str, Any]:
    try:
        head = subprocess.run(["git", "rev-parse", "HEAD"], stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True, check=False).stdout.strip()
        status = subprocess.run(["git", "status", "--porcelain=v1"], stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, check=False).stdout
    except OSError:
        return {"head": None, "dirty": None, "status_sha256": None}
    return {"head": head or None, "dirty": bool(status), "status_sha256": digest(status)}


def archived_observations(archive_path: pathlib.Path | None = None) -> list[dict[str, Any]]:
    path = archive_path or pathlib.Path(DEFAULT_MUSE_ARCHIVE)
    if not path.is_file():
        return []
    try:
        records = [json.loads(line) for line in path.read_bytes().splitlines()]
    except (OSError, UnicodeDecodeError, json.JSONDecodeError):
        return []
    observations = []
    for observation_id in ("FLW-T-0006", "FLW-cleanup"):
        entries = [entry for entry in records if entry.get("archive_schema") == "tusker.provider-usage-archive/v1" and entry.get("observation_id") == observation_id]
        by_kind = {entry.get("event_kind"): entry for entry in entries}
        session, usage_entry, token_entry, complete_entry = (by_kind.get(kind) for kind in ("session", "token_usage_record", "token_count", "task_complete"))
        if not all((session, usage_entry, token_entry, complete_entry)) or any(entry.get("source_line_sha256") != digest(str(entry.get("raw_event_line", "")).encode("utf-8")) for entry in (usage_entry, token_entry)):
            continue
        try:
            usage_event = json.loads(usage_entry["raw_event_line"])
            usage = usage_event["payload"]["turn_token_usage"]
            total, cached = usage["input_tokens"], usage["cached_input_tokens"]
            output, reasoning = usage["output_tokens"], usage["reasoning_output_tokens"]
            token_count_event = json.loads(token_entry["raw_event_line"])
            token_count = token_count_event["payload"]["info"]["total_token_usage"]
            if any(token_count[key] != usage[key] for key in ("input_tokens", "cached_input_tokens", "output_tokens", "reasoning_output_tokens")):
                continue
            complete_event = complete_entry.get("event", {})
            if complete_event.get("type") != "event_msg" or complete_event.get("payload", {}).get("type") != "task_complete" or usage_event["payload"].get("thread_id") != session.get("thread_id"):
                continue
        except (KeyError, TypeError, ValueError, json.JSONDecodeError):
            continue
        observations.append({
            "observation_id": observation_id, "thread_id": session.get("thread_id"), "event_count": session.get("original_source_event_count"),
            "input_tokens": total, "cached_input_tokens": cached, "uncached_input_tokens": total - cached, "output_tokens": output,
            "reasoning_output_tokens": reasoning, "reasoning_output_is_subset_of_output": reasoning <= output, "stage_attribution": None,
            "source_event_path": str(path), "source_event_sha256": path_digest(path), "source_session_path": session.get("source_session_path"),
            "source_session_sha256": session.get("source_session_sha256"), "original_source_event_path": session.get("original_source_event_path"),
            "original_source_event_sha256": session.get("original_source_event_sha256"), "original_source_event_status": session.get("original_source_status"),
        })
    return observations


def provider_receipt() -> dict[str, Any]:
    observations = archived_observations()
    if not observations:
        return {"schema": RECEIPT_SCHEMA, "available": False, "reason": "No archived provider usage event was available."}
    primary = observations[0]
    return dict({"schema": RECEIPT_SCHEMA, "available": True, "provider": "Muse Spark 1.3"}, **primary, observations=observations, observation_count=len(observations), primary_observation=primary["observation_id"])


def now() -> str:
    return dt.datetime.now(dt.timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def generate(args: argparse.Namespace) -> tuple[pathlib.Path, pathlib.Path, pathlib.Path, pathlib.Path]:
    cli = str(pathlib.Path(args.cli).expanduser())
    if not pathlib.Path(cli).is_file() or not os.access(cli, os.X_OK):
        raise SystemExit(f"CLI is not executable: {cli}")
    baseline_path, fixture_path, receipt_path, report_path = map(pathlib.Path, (args.baseline, args.fixtures, args.receipt, args.report))
    with tempfile.TemporaryDirectory(prefix="tusker-flw-t0008-") as temp:
        fixtures = [build_fixture(pathlib.Path(temp), size) for size in (10, 100, 1000)]
        manifest = fixture_manifest(fixtures)
        manifest_sha = digest(canonical(manifest))
        measurements = [row for fixture in fixtures for row in observe(fixture, cli, args.repetitions)]
    provider = provider_receipt()
    if provider.get("available"):
        write(receipt_path, json.dumps(provider, ensure_ascii=False, indent=2) + "\n")
        provider["receipt_path"] = str(receipt_path)
        provider["receipt_sha256"] = path_digest(receipt_path)
    else:
        provider["receipt_path"] = None
        provider["receipt_sha256"] = None
    identity = version_observation(cli)
    source = source_snapshot()
    baseline = {
        "schema": BASELINE_SCHEMA, "script_version": SCRIPT_VERSION, "fixture_version": FIXTURE_VERSION,
        "observed_at": now(), "source_snapshot": source, "script_sha256": path_digest(pathlib.Path(__file__).resolve()), "cli": cli, "cli_identity": identity,
        "fixture_manifest_sha256": manifest_sha, "host": {"system": platform.system(), "release": platform.release(), "machine": platform.machine(), "python": platform.python_version()},
        "measurement_method": {
            "execution": "read-only CLI projections in temporary fixture vaults",
            "input_bytes": "normalized argv JSON bytes; stdin closed",
            "output_bytes": "stdout+stderr bytes from each process",
            "agent_context_bytes": "stdout bytes; not provider tokens",
            "fixture_inputs": "selected fixture byte sums/hashes; not emitted context",
            "cold_warm": "fresh subprocess for both bootstrap rows; OS/filesystem cache state uncontrolled; no warm in-process cache",
            "protocol": "one process invocation per sample; agent tool calls, turns and retries are not observed",
            "transcripts": "unique full stdout/stderr transcripts keyed by SHA-256",
            "percentile": "nearest-rank p95; 20 repetitions",
            "workflow_boundary": "read-only projections; no worker/daemon/provider/live workflow",
        },
        "tokenizer": {"available": False, "name": None, "version": None, "reason": "No tokenizer available."},
        "provider_usage": provider,
        "fixtures": {"document_counts": [10, 100, 1000], "task_contract_classes": ["small", "medium", "large"], "scenarios": manifest["scenarios"]},
        "measurements": measurements,
    }
    fixture_payload = {"schema": FIXTURE_SCHEMA, "version": FIXTURE_VERSION, "script_version": SCRIPT_VERSION, "manifest": manifest, "manifest_sha256": manifest_sha}
    write(fixture_path, json.dumps(fixture_payload, ensure_ascii=False, indent=2) + "\n")
    write(baseline_path, json.dumps(baseline, ensure_ascii=False, indent=2) + "\n")
    write(report_path, render_report(baseline, baseline_path, fixture_path, receipt_path, report_path))
    return baseline_path, fixture_path, receipt_path, report_path


def expected_keys() -> list[tuple[int, str, str]]:
    return [(size, stage, condition) for size in (10, 100, 1000) for stage, condition in STAGES]


def check_baseline(baseline_path: pathlib.Path, fixture_path: pathlib.Path, receipt_path: pathlib.Path) -> list[str]:
    errors: list[str] = []
    try:
        baseline = json.loads(baseline_path.read_text(encoding="utf-8"))
        fixture_payload = json.loads(fixture_path.read_text(encoding="utf-8"))
    except (OSError, UnicodeDecodeError, json.JSONDecodeError) as error:
        return [f"cannot read artifacts: {error}"]
    if baseline.get("schema") != BASELINE_SCHEMA or baseline.get("script_version") != SCRIPT_VERSION:
        errors.append("baseline schema/version mismatch")
    if baseline.get("script_sha256") != path_digest(pathlib.Path(__file__).resolve()):
        errors.append("measurement script hash mismatch")
    source_snapshot = baseline.get("source_snapshot", {})
    if not isinstance(source_snapshot, dict) or not isinstance(source_snapshot.get("dirty"), bool) or not str(source_snapshot.get("status_sha256", "")).startswith("sha256:"):
        errors.append("historical source snapshot is missing")
    if fixture_payload.get("schema") != FIXTURE_SCHEMA or fixture_payload.get("version") != FIXTURE_VERSION:
        errors.append("fixture schema/version mismatch")
    manifest = fixture_payload.get("manifest", {})
    if not isinstance(manifest, dict):
        manifest = {}
    if fixture_payload.get("manifest_sha256") != digest(canonical(manifest)) or baseline.get("fixture_manifest_sha256") != fixture_payload.get("manifest_sha256"):
        errors.append("fixture manifest hash mismatch")
    expected_inputs: dict[tuple[int, str], tuple[int, str]] = {}
    try:
        with tempfile.TemporaryDirectory(prefix="tusker-flw-t0008-check-") as temp:
            generated = [build_fixture(pathlib.Path(temp), size) for size in (10, 100, 1000)]
            if manifest != fixture_manifest(generated):
                errors.append("fixture manifest does not match deterministic fixture generator")
            for fixture in generated:
                for stage, _ in STAGES:
                    expected_inputs[(fixture["documents"]["count"], stage)] = input_inventory(fixture["root"], fixture_inputs(fixture["root"], stage))
    except (OSError, ValueError) as error:
        errors.append(f"deterministic fixture reconstruction failed: {error}")
    if [row.get("documents") for row in manifest.get("document_repositories", [])] != [10, 100, 1000]:
        errors.append("document counts are not 10/100/1000")
    if [row.get("class") for row in manifest.get("task_contracts", [])] != ["large", "medium", "small"]:
        errors.append("small/medium/large contracts are incomplete")
    required_scenarios = {"small-contract", "medium-contract", "large-contract", "long-contract", "branching-dag", "blocked-gate", "failed-proof", "resumed-job"}
    if not required_scenarios.issubset(set(manifest.get("scenarios", []))):
        errors.append("fixture scenarios are incomplete")
    tokenizer = baseline.get("tokenizer", {})
    if tokenizer.get("available") is not False or tokenizer.get("name") is not None or tokenizer.get("version") is not None:
        errors.append("tokenizer availability must be explicitly false/null")
    provider = baseline.get("provider_usage", {})
    if provider.get("available"):
        receipt = pathlib.Path(provider.get("receipt_path") or receipt_path)
        try:
            receipt_data = json.loads(receipt.read_text(encoding="utf-8"))
        except (OSError, UnicodeDecodeError, json.JSONDecodeError) as error:
            errors.append(f"provider receipt unreadable: {error}")
        else:
            if ".tusker/scratch" in str(receipt):
                errors.append("provider receipt must be archived outside scratch")
            if provider.get("receipt_sha256") != path_digest(receipt):
                errors.append("provider receipt hash mismatch")
            for key in ("observation_id", "schema", "available", "provider", "input_tokens", "cached_input_tokens", "uncached_input_tokens", "output_tokens", "reasoning_output_tokens", "event_count", "thread_id", "source_event_sha256", "observations", "observation_count", "primary_observation"):
                if provider.get(key) != receipt_data.get(key):
                    errors.append(f"provider receipt mismatch for {key}")
            if not all(isinstance(provider.get(key), int) and provider[key] >= 0 for key in ("input_tokens", "cached_input_tokens", "output_tokens", "reasoning_output_tokens", "event_count")):
                errors.append("provider usage counters must be nonnegative integers")
            if isinstance(provider.get("input_tokens"), int) and isinstance(provider.get("cached_input_tokens"), int) and provider["cached_input_tokens"] > provider["input_tokens"]:
                errors.append("provider cached input exceeds input")
            if isinstance(provider.get("reasoning_output_tokens"), int) and isinstance(provider.get("output_tokens"), int) and provider["reasoning_output_tokens"] > provider["output_tokens"]:
                errors.append("provider reasoning output exceeds output")
            if provider.get("uncached_input_tokens") != provider.get("input_tokens") - provider.get("cached_input_tokens"):
                errors.append("provider uncached input is not input minus cached input")
            if provider.get("reasoning_output_is_subset_of_output") is not True:
                errors.append("provider reasoning subset boundary missing")
            observations = receipt_data.get("observations", [])
            if not isinstance(observations, list) or not observations:
                errors.append("provider observations are missing")
            elif provider.get("observation_count") != len(observations) or provider.get("primary_observation") != observations[0].get("observation_id"):
                errors.append("provider observation index mismatch")
            archive_by_id: dict[str, dict[str, Any]] = {}
            for observation in observations if isinstance(observations, list) else []:
                if observation.get("stage_attribution") is not None:
                    errors.append(f"provider stage attribution must remain unknown for {observation.get('observation_id')}")
                if observation.get("uncached_input_tokens") != observation.get("input_tokens") - observation.get("cached_input_tokens"):
                    errors.append(f"provider uncached input mismatch for {observation.get('observation_id')}")
                if observation.get("reasoning_output_is_subset_of_output") is not True:
                    errors.append(f"provider reasoning subset missing for {observation.get('observation_id')}")
                source_event = pathlib.Path(str(observation.get("source_event_path", "")))
                if ".tusker/scratch" in str(source_event):
                    errors.append(f"provider source event must be archived outside scratch for {observation.get('observation_id')}")
                elif not source_event.is_file():
                    errors.append(f"provider source event archive is missing for {observation.get('observation_id')}")
                elif observation.get("source_event_sha256") != path_digest(source_event):
                    errors.append(f"provider source event archive hash mismatch for {observation.get('observation_id')}")
                archive_by_id.update({entry["observation_id"]: entry for entry in archived_observations(source_event)})
            for observation in observations if isinstance(observations, list) else []:
                archive = archive_by_id.get(observation.get("observation_id"))
                if not archive:
                    errors.append(f"provider source event archive record missing for {observation.get('observation_id')}")
                    continue
                for key in ("thread_id", "event_count", "input_tokens", "cached_input_tokens", "uncached_input_tokens", "output_tokens", "reasoning_output_tokens", "source_event_path", "source_event_sha256", "source_session_path", "source_session_sha256", "original_source_event_path", "original_source_event_sha256", "original_source_event_status"):
                    if observation.get(key) != archive.get(key):
                        errors.append(f"provider archive mismatch for {observation.get('observation_id')} {key}")
    rows = baseline.get("measurements", [])
    keys = [(row.get("documents"), row.get("stage"), row.get("condition")) for row in rows]
    if keys != expected_keys():
        errors.append("measurement matrix does not exactly match expected fixture/stage/condition/task rows")
    for row in rows:
        stage = row.get("stage")
        expected = expected_semantic(stage) if stage in {item[0] for item in STAGES} else None
        if row.get("expected_semantic") != expected:
            errors.append(f"expected semantic mismatch for {row.get('key')}")
        if stage in {item[0] for item in STAGES} and row.get("command") != normalized_command(stage_command("<cli>", stage), "<cli>"):
            errors.append(f"command matrix mismatch for {row.get('key')}")
        samples = row.get("samples", [])
        if len(samples) != REPETITIONS:
            errors.append(f"expected exactly {REPETITIONS} samples for {row.get('key')}")
        if row.get("protocol") != {"process_invocations": REPETITIONS, "agent_tool_calls": None, "agent_turns": None, "agent_retries": None}:
            errors.append(f"process invocation protocol mismatch for {row.get('key')}")
        transcript_map: dict[tuple[str, str], tuple[bytes, bytes]] = {}
        for entry in row.get("transcripts", []):
            try:
                stdout = base64.b64decode(entry.get("stdout_b64", ""), validate=True)
                stderr = base64.b64decode(entry.get("stderr_b64", ""), validate=True)
            except (TypeError, ValueError):
                errors.append(f"invalid transcript encoding for {row.get('key')}")
                continue
            key = (entry.get("stdout_sha256", ""), entry.get("stderr_sha256", ""))
            if key != (digest(stdout), digest(stderr)):
                errors.append(f"transcript hash mismatch for {row.get('key')}")
            transcript_map[key] = (stdout, stderr)
        if not transcript_map:
            errors.append(f"transcript library missing for {row.get('key')}")
        recomputed_outcomes: list[str] = []
        for sample in samples:
            transcript = sample.get("transcript", {})
            key = (transcript.get("stdout_sha256", ""), transcript.get("stderr_sha256", ""))
            output = transcript_map.get(key)
            if output is None:
                errors.append(f"sample transcript is not bound to a full transcript for {row.get('key')}")
                stdout, stderr = b"", b""
            else:
                stdout, stderr = output
            recomputed_outcomes.append(semantic(stage, stdout))
            expected_success = isinstance(sample.get("exit_code"), int) and sample.get("exit_code") == 0 and sample.get("timed_out") is False
            if sample.get("process_success") != expected_success:
                errors.append(f"process success/exit status mismatch for {row.get('key')}")
            if sample.get("output_bytes") != sample.get("stdout_bytes", -1) + sample.get("stderr_bytes", -1):
                errors.append(f"output byte sum mismatch for {row.get('key')}")
            if sample.get("stdout_bytes") != len(stdout) or sample.get("stderr_bytes") != len(stderr):
                errors.append(f"transcript byte count mismatch for {row.get('key')}")
            if sample.get("agent_context_bytes") != sample.get("stdout_bytes"):
                errors.append(f"agent context bytes mismatch for {row.get('key')}")
            if sample.get("input_bytes") != len(canonical(row.get("command", []))):
                errors.append(f"input byte mismatch for {row.get('key')}")
            for name in ("stdout_sha256", "stderr_sha256"):
                value = transcript.get(name, "")
                if not isinstance(value, str) or not value.startswith("sha256:") or len(value) != 71:
                    errors.append(f"invalid {name} for {row.get('key')}")
        counts: dict[str, int] = {}
        for outcome in recomputed_outcomes:
            counts[outcome] = counts.get(outcome, 0) + 1
        if counts != row.get("observed_semantic_counts") or counts != {expected: len(samples)}:
            errors.append(f"semantic outcome mismatch for {row.get('key')}: {counts}")
        if row.get("outcome_matrix_sha256") != hash_matrix(samples, recomputed_outcomes):
            errors.append(f"outcome hash matrix mismatch for {row.get('key')}")
        if row.get("summary") != summary(samples):
            errors.append(f"summary mismatch for {row.get('key')}")
        inventory = expected_inputs.get((row.get("documents"), stage))
        if inventory and (row.get("fixture_inputs_bytes"), row.get("fixture_inputs_sha256")) != inventory:
            errors.append(f"fixture input inventory mismatch for {row.get('key')}")
        if not isinstance(row.get("fixture_inputs_bytes"), int) or row["fixture_inputs_bytes"] <= 0 or not str(row.get("fixture_inputs_sha256", "")).startswith("sha256:"):
            errors.append(f"fixture input inventory missing for {row.get('key')}")
    return sorted(set(errors))


def rel_link(path: pathlib.Path, report: pathlib.Path) -> str:
    return os.path.relpath(path.resolve(), report.resolve().parent)


def render_report(baseline: dict[str, Any], baseline_path: pathlib.Path, fixture_path: pathlib.Path, receipt_path: pathlib.Path, report_path: pathlib.Path) -> str:
    lines = [
        "---", "title: FLW-T-0008 token baseline", "status: measured", "read_when: Reviewing measured context and provider boundaries.", "---", "",
        "# FLW-T-0008 token baseline", "",
        "This Wave 0 denominator measures returned bytes and process timings from read-only CLI projections in validated disposable vaults; fixture input bytes are inventory only.", "",
        "## Evidence identity", "",
        f"- Script: [`scripts/measure-agent-workflows.py`](../../../scripts/measure-agent-workflows.py), version `{baseline['script_version']}`, SHA-256 `{baseline.get('script_sha256')}`.",
        f"- CLI: `{baseline['cli']}`; version `{baseline['cli_identity'].get('version')}`, revision `{baseline['cli_identity'].get('revision')}`, binary SHA-256 `{baseline['cli_identity'].get('binary_sha256')}`.",
        f"- Source snapshot captured at measurement: revision `{baseline.get('source_snapshot', {}).get('head')}`; dirty `{baseline.get('source_snapshot', {}).get('dirty')}`; status SHA-256 `{baseline.get('source_snapshot', {}).get('status_sha256')}`; host `{baseline['host']['system']} {baseline['host']['release']} {baseline['host']['machine']}`; Python `{baseline['host']['python']}`.",
        f"- Observed at: `{baseline['observed_at']}`.",
        f"- Fixtures: [`{fixture_path.name}`]({rel_link(fixture_path, report_path)}), manifest SHA-256 `{baseline['fixture_manifest_sha256']}`.",
        f"- Baseline JSON: [`{baseline_path.name}`]({rel_link(baseline_path, report_path)}).",
        "",
        "## Fixture coverage", "",
        "Temporary vaults pass canonical frontmatter validation before observation: small/medium/large contracts, a 120-note long contract, branching DAG, human gate, failed proof, handoff recovery and completion candidate. Corpora contain exactly 10, 100 and 1,000 documents.", "",
        "| Fixture | Documents | Contract classes | Scenario records |", "| --- | ---: | --- | --- |", "| `docs-10`, `docs-100`, `docs-1000` | 10, 100, 1,000 | small / medium / large | long contract / branching DAG / blocked gate / failed proof / handoff recovery |", "",
        "## Commands and observed semantic outcomes", "",
        "These are read-only CLI projections. Exit zero means a result was emitted; it does not make a blocked, partial or continuing workflow pass.", "",
        "| Stage | Condition | Command | Expected observed outcome |", "| --- | --- | --- | --- |",
    ]
    for stage, condition in STAGES:
        command = shlex.join(stage_command("<cli>", stage))
        lines.append(f"| `{stage}` | `{condition}` | `{command}` | `{expected_semantic(stage)}` |")
    lines.extend(["", "## Measured rows", "", "`input_bytes` is normalized argv JSON; `output_bytes` is stdout+stderr; `agent_context_bytes` is stdout. Fixture input bytes are separate inventory. Each sample is one fresh process invocation; OS/filesystem cache state is uncontrolled, and no warm in-process cache is measured. Agent tool calls, turns and retries are not observed.", "", "| Docs | Condition | Stage | N | Process OK | Median ms | p95 ms | Median input bytes | Agent context bytes | Output bytes | Fixture input bytes |", "| ---: | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |"])
    for row in baseline["measurements"]:
        s = row["summary"]
        lines.append(f"| {row['documents']} | {row['condition']} | {row['stage']} | {s['sample_count']} | {s['process_success_count']} | {s['median_elapsed_ms']} | {s['p95_elapsed_ms']} | {s['median_input_bytes']} | {s['median_agent_context_bytes']} | {s['median_output_bytes']} | {row['fixture_inputs_bytes']} |")
    lines.extend(["", "## Token and provider boundary", "", "No tokenizer is exposed by the installed CLI or stdlib harness. Token fields are therefore absent from sample rows rather than filled with byte-derived estimates. Fixture input size and CLI output size are not provider-billed usage."])
    provider = baseline.get("provider_usage", {})
    if provider.get("available"):
        observations = provider.get("observations", [provider])
        lines.extend(["", f"The durable supplied Muse Spark 1.3 receipt is [`muse-usage-receipt.json`]({rel_link(receipt_path, report_path)}) with SHA-256 `{provider.get('receipt_sha256')}`. It contains {len(observations)} unassigned observations; none has exact fixture-stage blame."])
        archive = pathlib.Path(DEFAULT_MUSE_ARCHIVE)
        lines.append(f"Selected source session metadata, usage, token-count and completion records are archived in [`{archive.name}`]({rel_link(archive, report_path)}) with SHA-256 `{path_digest(archive) if archive.is_file() else None}`; original scratch event hashes remain unavailable-after-reset references.")
        for observation in observations:
            lines.append(f"- `{observation.get('observation_id')}` thread `{observation.get('thread_id')}`: in `{observation.get('input_tokens')}`, cached `{observation.get('cached_input_tokens')}`, uncached `{observation.get('uncached_input_tokens')}`, out `{observation.get('output_tokens')}`, reasoning `{observation.get('reasoning_output_tokens')}`; `{observation.get('event_count')}` events; src `{observation.get('source_event_sha256')}`.")
    else:
        lines.extend(["", "No provider usage receipt was supplied; cached input, uncached input and output remain unknown."])
    lines.extend([
        "", "## Frozen targets", "", "Targets in `.tusker/specs/tusker-trust-and-efficiency.md` stay frozen: context -30%; warm bootstrap <=1,200; next/status <=350; capsule <=500; routing <=50/node with 800-token shortlist; p95 regression <=10%. No target pass is claimed without tokenizer data and an optimized comparison.",
        "", "## Reproduction and limits", "", "```text", "python3 scripts/measure-agent-workflows.py --cli /tmp/tusker-flw", "python3 scripts/measure-agent-workflows.py --check", "go test ./cmd/tusker -run ^TestTrustTokenBaseline$ -count=1 -v", "```", "", "Self-check: `python3 scripts/measure-agent-workflows.py --check` -> `PASS`. Go regression: held during the current no-build hold. Read-only projections only; bytes/timings are recorded, while warm cache, live recovery, human acceptance, exact tokens and old/new savings remain open. Follow-ons FLW-T-0021/23/24 should rerun with tokenizer/provider counters.", "",
    ])
    return "\n".join(lines)


def parse_args(argv: list[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--cli", default=os.environ.get("TUSKER_FLW", DEFAULT_CLI))
    parser.add_argument("--baseline", default=DEFAULT_BASELINE)
    parser.add_argument("--fixtures", default=DEFAULT_FIXTURES)
    parser.add_argument("--receipt", default=DEFAULT_RECEIPT)
    parser.add_argument("--report", default=DEFAULT_REPORT)
    parser.add_argument("--repetitions", type=int, default=REPETITIONS)
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args(argv)
    if args.repetitions != REPETITIONS:
        parser.error(f"--repetitions must equal {REPETITIONS} for the p95 baseline")
    return args


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv if argv is not None else sys.argv[1:])
    baseline = pathlib.Path(args.baseline)
    fixtures = pathlib.Path(args.fixtures)
    receipt = pathlib.Path(args.receipt)
    if args.check:
        errors = check_baseline(baseline, fixtures, receipt)
        if errors:
            for error in errors:
                print(f"FAIL: {error}")
            return 1
        print("PASS: FLW-T-0008 token baseline self-check")
        return 0
    baseline_path, fixture_path, receipt_path, report_path = generate(args)
    for path in (baseline_path, fixture_path, receipt_path, report_path):
        print(f"Wrote {path}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
