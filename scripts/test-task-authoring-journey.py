#!/usr/bin/env python3
"""Run the bounded task-authoring qualification lanes.

The script deliberately keeps local source/fixture checks, a read-only browser
run, and installed-runtime evidence separate. It never starts Tusker, arms a
wave, invokes a provider, or turns fixture output into a live claim.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import math
import os
from pathlib import Path
import re
import shlex
import shutil
import subprocess
import sys
from typing import Any, Optional


ROOT = Path(__file__).resolve().parents[1]
REPORT = ROOT / "docs/reports/software-factory/pilot.md"

SOURCE_PARITY_FILES: tuple[tuple[str, tuple[str, ...]], ...] = (
    (
        ".tusker/specs/direct-wave-authoring.md",
        ("SF-01", "SF-11", "SF-14", "Before launch record the exact installed build"),
    ),
    (
        "cmd/tusker/direct_wave_packet_test.go",
        ("TestSoftwareFactoryPacket", "TestDirectWavePacketHelpAndCapabilitiesMatchDispatch"),
    ),
    (
        "cmd/tusker/software_factory_proof_test.go",
        ("TestSoftwareFactoryProof", "v7VerificationReceiptCurrent"),
    ),
    (
        "cmd/tusker/software_factory_repair_test.go",
        ("TestSoftwareFactoryRepair", "AdmitExternalLoopEvent"),
    ),
    (
        "skills/tusker/SKILL.md",
        ("tusker capabilities --json", "manufacture proof"),
    ),
    (
        "docs/system/cli.md",
        ("tusker wave start", "tusker runner route", "tusker verify add"),
    ),
    (
        "e2e/agent_journey/prepare-v2.sh",
        ("wave create --file", "strategy: shared", "must never be typed"),
    ),
)


def command_text(command: list[str]) -> str:
    return shlex.join(command)


def reproducible_command(command: list[str], cwd: Path, environment: Optional[dict[str, str]] = None) -> str:
    """Render a replayable shell command with its exact cwd and bounded env."""
    assignments = ""
    setup = ""
    if environment:
        paths = [
            str(Path(environment[name]).expanduser().parent if name == "TUSKER_VALIDATION_LOCK_DIR" else Path(environment[name]).expanduser())
            for name in ("GOCACHE", "GOTMPDIR", "TUSKER_VALIDATION_LOCK_DIR")
            if environment.get(name)
        ]
        if paths:
            setup = "mkdir -p " + " ".join(shlex.quote(path) for path in paths) + " && "
        assignments = " ".join(
            f"{name}={shlex.quote(environment[name])}"
            for name in ("GOCACHE", "GOTMPDIR", "TUSKER_VALIDATION_LOCK_DIR")
            if environment.get(name)
        )
        if assignments:
            assignments += " "
    return f"cd {shlex.quote(str(cwd))} && {setup}{assignments}{shlex.join(command)}"


def first_failure(output: str) -> str:
    summary_only = re.compile(
        r"^(?:FAIL\s+[^ ]+\s+\[setup failed\]|FAIL\s+[^ ]+|ok\s+[^ ]+|\?\s+[^ ]+|"
        r"=== (?:RUN|PAUSE|CONT|NAME)|--- (?:PASS|FAIL|SKIP):)"
    )
    actionable = re.compile(
        r"(?:error|panic|undefined|cannot|no required module|no space left|timed out|"
        r"operation not permitted|permission denied|fatal|build failed|does not exist|"
        r"invalid|unexpected|missing|required)",
        re.I,
    )
    lines = [line.strip() for line in output.splitlines() if line.strip()]
    for line in lines:
        if summary_only.search(line):
            continue
        if actionable.search(line):
            return line
    for line in lines:
        if not summary_only.search(line):
            return line
    return "none observed"


def run_command(command: list[str], timeout: int = 420, environment: Optional[dict[str, str]] = None, cwd: Path = ROOT) -> tuple[int, str]:
    try:
        completed = subprocess.run(
            command,
            cwd=cwd,
            check=False,
            capture_output=True,
            text=True,
            timeout=timeout,
            env=environment,
        )
        return completed.returncode, (completed.stdout + completed.stderr).strip()
    except subprocess.TimeoutExpired as exc:
        output = "\n".join(
            value.decode(errors="replace") if isinstance(value, bytes) else value
            for value in (exc.stdout, exc.stderr)
            if value
        )
        return 124, output + "\ncommand timed out"


def offline_environment() -> dict[str, str]:
    """Keep Go build/link temporary state in a writable disposable volume."""
    environment = os.environ.copy()
    environment["PATH"] = "/usr/bin:/bin:/usr/local/bin:" + os.environ.get("PATH", "")
    defaults = {
        "GOCACHE": "/private/tmp/tusker-w27-offline-gocache",
        "GOTMPDIR": "/private/tmp/tusker-w27-offline-gotmp",
        "TUSKER_VALIDATION_LOCK_DIR": "/private/tmp/tusker-w27-offline-locks",
    }
    for name, default in defaults.items():
        value = environment.get(name, "").strip() or default
        path = Path(value).expanduser()
        (path.parent if name == "TUSKER_VALIDATION_LOCK_DIR" else path).mkdir(parents=True, exist_ok=True)
        environment[name] = value
    return environment


def source_identity() -> dict[str, Any]:
    """Identify this checkout without turning dirty source into installed proof."""
    revision_code, revision = run_command(["git", "rev-parse", "--verify", "HEAD"], timeout=30)
    status_code, status = run_command(["git", "status", "--short", "--untracked-files=all"], timeout=30)
    digest = hashlib.sha256()
    missing: list[str] = []
    for relative, _needles in SOURCE_PARITY_FILES:
        path = ROOT / relative
        if not path.is_file():
            missing.append(relative)
            continue
        digest.update(relative.encode("utf-8"))
        digest.update(b"\0")
        digest.update(path.read_bytes())
    status_lines = [line for line in status.splitlines() if line.strip()]
    return {
        "revision": revision.strip() if revision_code == 0 else "unknown",
        "revision_error": "" if revision_code == 0 else first_failure(revision),
        "dirty": bool(status_lines) if status_code == 0 else True,
        "dirty_entries": len(status_lines),
        "source_fingerprint": "sha256:" + digest.hexdigest() if not missing else "unknown",
        "missing_files": missing,
    }


def source_parity() -> dict[str, Any]:
    """Check that the current checkout still exposes the accepted source seams."""
    checks: list[str] = []
    failures: list[str] = []
    for relative, needles in SOURCE_PARITY_FILES:
        path = ROOT / relative
        if not path.is_file():
            failures.append(f"{relative}: missing")
            continue
        body = path.read_text(encoding="utf-8")
        missing = [needle for needle in needles if needle not in body]
        if missing:
            failures.append(f"{relative}: missing {', '.join(missing)}")
        else:
            checks.append(relative)
    canonical = (ROOT / "skills/tusker").resolve()
    for generated in (ROOT / ".agents/skills/tusker", ROOT / ".claude/skills/tusker"):
        if not generated.is_symlink() or generated.resolve() != canonical:
            failures.append(f"{generated.relative_to(ROOT)}: does not resolve to skills/tusker")
    if not failures:
        checks.append(".agents/skills/tusker and .claude/skills/tusker -> skills/tusker")
    return {"status": "PASS" if not failures else "FAIL", "checks": checks, "failures": failures}


def built_bundle_report() -> tuple[str, list[str]]:
    """Check the actual dist entry and every local asset it references."""
    dist = Path(os.environ.get("TUSKER_AUTHORING_BUILD_DIR", str(ROOT / "internal/serve/ui/dist")))
    index = dist / "index.html"
    if not index.is_file():
        return "NOT RUN", [f"built entry is missing: {index}"]
    body = index.read_text(encoding="utf-8")
    references = re.findall(r"(?:src|href)=\"([^\"]+)\"", body)
    assets = [ref for ref in references if ref.startswith("/assets/")]
    if not assets:
        return "NOT RUN", [f"built entry has no /assets/ references: {index}"]
    missing = [str(dist / ref.lstrip("/")) for ref in assets if not (dist / ref.lstrip("/")).is_file()]
    if missing:
        return "FAIL", ["missing built assets: " + ", ".join(missing)]
    if "/src/" in body:
        return "FAIL", [f"built entry still references source modules: {index}"]
    return "PASS", [f"{index} with {len(assets)} local assets"]


GO_CASES: list[tuple[str, str, str]] = [
    ("Q1", "wave authoring request creates the durable graph", "TestDirectWaveAuthoringBatchCreatesDurableGraph"),
    ("Q2", "authoring rollback, idempotence, and request-key conflict", "TestDirectWaveAuthoringRollbackIdempotencyAndConflict"),
    ("Q3", "direct task create and revision-guarded update", "TestDirectWaveAuthoringTaskUpdateCAS"),
    ("Q4", "direct-wave removal scan", "TestDirectWaveRemoval.*"),
    ("Q5", "wave review reads only durable material", "TestDirectWaveAuthorityReviewUsesOnlyDurableMaterial"),
    ("Q6", "one-action wave start queues eligible roots", "TestDirectWaveAuthorityWaveStartQueuesEligibleRoots"),
    ("Q7", "pause/resume preserves authority and continuity", "TestDirectWaveAutonomousPausePreservesAuthorityAndContinuity"),
    ("Q8", "task start scope and blockers", "TestDirectStartAuthorityBackgroundScopeAndBlockers"),
    ("Q9", "autonomous frontier advances after completion", "TestDirectWaveAutonomousFrontierAdvancesAfterCompletion"),
    ("Q10", "packets preserve wave context and exact task bodies", "TestDirectWavePacketWaveContextAndScopedBodies"),
    ("Q11", "direct Serve review and Start/Pause/Resume controls", "TestDirectWaveServe.*"),
    ("Q12", "read-only routing keeps work and review lanes honest", "TestExternalArchitectRouting.*"),
    ("Q13", "proof, dependency, and work-session contracts survive removal", "TestV7Proof.*|TestV7Dependencies.*|TestWorkSessionLifecycleCASAndExactOnce"),
]

UI_CASES: list[tuple[str, str, str]] = [
    ("Q14", "UI direct-wave review detail and authority controls", "test/direct-wave-authority.test.ts"),
]

W27_CASES: list[tuple[str, str, str]] = [
    ("SF-01..03", "packet round-trip, atomic rejection, and exact projection", "TestSoftwareFactoryPacket"),
    ("SF-04..06", "strict proof and independent finding closure", "TestSoftwareFactoryProof"),
    ("SF-07..09", "repair caps, restart reconciliation, and deterministic escalation", "TestSoftwareFactoryRepair"),
    (
        "SF-10",
        "material amendment, pause/restart, decision routing, duplicate claims, and lifecycle continuity",
        "TestSoftwareFactoryRepairExternalLoopCheckpointsAndAmendments|TestDirectWaveAutonomousPausePreservesAuthorityAndContinuity|TestDirectWaveAutonomousCrashBeforeQueueRecoversOnce|TestExternalArchitectRouting.*|TestWorkSessionLifecycleCASAndExactOnce",
    ),
    ("SF-11", "CLI help and capability dispatch parity", "TestDirectWavePacketHelpAndCapabilitiesMatchDispatch"),
]


def run_offline() -> dict[str, Any]:
    if not live_profile_mismatches({
        "requested_execute_profile": "one",
        "admitted_execute_profile": "two",
        "actual_execute_profile": "three",
    }):
        return {"status": "FAIL", "command": "internal live receipt guard", "detail": "contradictory profiles were accepted", "output": ""}
    environment = offline_environment()
    identity = source_identity()
    parity = source_parity()
    results: list[str] = []
    matched = 0
    failures: list[str] = []
    commands: list[str] = []
    synthetic_ok, synthetic_detail = synthetic_negative_checks()
    if synthetic_ok:
        results.append("Receipt guard PASS (" + synthetic_detail + ")")
    else:
        failures.append(synthetic_detail)
    if parity["status"] != "PASS":
        failures.append("source parity: " + " | ".join(parity["failures"]))
    for case_id, description, pattern in GO_CASES:
        command = [
            shutil.which("go") or "go",
            "test",
            "./cmd/tusker",
            "-run",
            f"^({pattern})$",
            "-count=1",
            "-timeout=6m",
            "-v",
        ]
        commands.append(reproducible_command(command, ROOT, environment))
        code, output = run_command(command, timeout=480, environment=environment)
        passed = re.findall(r"--- PASS: (Test\w+)", output)
        failed = re.findall(r"--- FAIL: (Test\w+)", output)
        skipped = re.findall(r"--- SKIP: (Test\w+)", output)
        if code == 0 and passed and not failed and not skipped:
            matched += len(passed)
            results.append(f"{case_id} PASS ({description}; {len(passed)} tests: {', '.join(sorted(set(passed)))})")
        elif code == 0 and not passed:
            failures.append(f"{case_id} matched zero tests ({description})")
        elif code == 0 and skipped:
            failures.append(f"{case_id} skipped {len(skipped)} tests ({description}); first failure: {first_failure(output)}")
        else:
            failures.append(f"{case_id} exit={code} ({description}); first failure: {first_failure(output)}")
    w27_results: list[str] = []
    w27_failures: list[str] = []
    for case_id, description, pattern in W27_CASES:
        command = [
            shutil.which("go") or "go",
            "test",
            "./cmd/tusker",
            "-run",
            f"^({pattern})",
            "-count=1",
            "-timeout=6m",
            "-v",
        ]
        commands.append(reproducible_command(command, ROOT, environment))
        code, output = run_command(command, timeout=480, environment=environment)
        passed = re.findall(r"--- PASS: (Test\w+)", output)
        failed = re.findall(r"--- FAIL: (Test\w+)", output)
        skipped = re.findall(r"--- SKIP: (Test\w+)", output)
        if code == 0 and passed and not failed and not skipped:
            matched += len(passed)
            w27_results.append(f"{case_id} PASS ({description}; {len(passed)} tests: {', '.join(sorted(set(passed)))})")
        elif code == 0 and not passed:
            message = f"{case_id} matched zero tests ({description})"
            w27_failures.append(message)
            failures.append(message)
        elif code == 0 and skipped:
            message = f"{case_id} skipped {len(skipped)} tests ({description}); first failure: {first_failure(output)}"
            w27_failures.append(message)
            failures.append(message)
        else:
            message = f"{case_id} exit={code} ({description}); first failure: {first_failure(output)}"
            w27_failures.append(message)
            failures.append(message)
    bun = shutil.which("bun")
    for case_id, description, test_file in UI_CASES:
        if not bun:
            failures.append(f"{case_id} bun is unavailable ({description})")
            continue
        command = [bun, "test", test_file]
        commands.append(reproducible_command(command, ROOT / "internal/serve/ui", environment))
        code, output = run_command(command, timeout=240, environment=environment, cwd=ROOT / "internal/serve/ui")
        count = re.search(r"(\d+) pass", output)
        if code == 0 and count and int(count.group(1)) > 0:
            matched += int(count.group(1))
            results.append(f"{case_id} PASS ({description}; {count.group(1)} assertions)")
        else:
            failures.append(f"{case_id} exit={code} ({description}); first failure: {first_failure(output)}")
    if matched == 0:
        status = "FAIL"
        detail = "offline lane matched zero cases"
    elif failures:
        status = "FAIL"
        detail = f"matched {matched} cases; failures: " + " | ".join(failures)
    else:
        status = "PASS"
        detail = f"Q1-Q{len(GO_CASES) + len(UI_CASES)} plus SF-01..11 matched {matched} cases; zero failures"
    return {
        "status": status,
        "command": "\n".join(commands),
        "detail": detail,
        "output": "\n".join(results + w27_results + failures),
        "results": results,
        "w27_results": w27_results,
        "w27_failures": w27_failures,
        "source_identity": identity,
        "source_parity": parity,
        "environment": {name: environment[name] for name in ("GOCACHE", "GOTMPDIR", "TUSKER_VALIDATION_LOCK_DIR")},
    }


def run_browser() -> dict[str, Any]:
    built_status, built_details = built_bundle_report()
    base = os.environ.get("TUSKER_REALWORK_BASE_URL", "").strip()
    project = os.environ.get("TUSKER_REALWORK_PROJECT", "").strip()
    blockers = []
    if not base:
        blockers.append("TUSKER_REALWORK_BASE_URL is not set")
    if not project:
        blockers.append("TUSKER_REALWORK_PROJECT is not set")
    if built_status == "FAIL":
        blockers.extend(built_details)
    if blockers:
        return {"status": "NOT RUN", "detail": "; ".join(blockers), "built": [built_status, built_details]}
    command = ["node", "internal/serve/ui/test/task-authoring.browser.mjs"]
    environment = os.environ.copy()
    environment.setdefault("TUSKER_REALWORK_OUT", "docs/reports/task-authoring/browser")
    try:
        completed = subprocess.run(
            command,
            cwd=ROOT,
            check=False,
            capture_output=True,
            text=True,
            timeout=240,
            env=environment,
        )
        output = (completed.stdout + completed.stderr).strip()
        if completed.returncode == 0:
            status = "PASS"
            detail = "read-only browser journey passed against the configured service"
        elif completed.returncode == 2:
            status = "NOT RUN"
            detail = "browser readiness blocked: " + first_failure(output)
        else:
            status = "FAIL"
            detail = f"exit={completed.returncode}; first failure: {first_failure(output)}"
        return {"status": status, "command": command_text(command), "detail": detail, "output": output, "built": [built_status, built_details]}
    except subprocess.TimeoutExpired:
        return {"status": "FAIL", "command": command_text(command), "detail": "browser command timed out", "built": [built_status, built_details]}


def live_profile_mismatches(receipt: dict[str, Any]) -> list[str]:
    return [
        lane for lane in ("execute", "review")
        if len({receipt.get(f"{stage}_{lane}_profile") for stage in ("requested", "admitted", "actual")}) != 1
    ]


def _installed_identity_blockers(receipt: dict[str, Any]) -> list[str]:
    identity = receipt.get("installed_identity")
    if not isinstance(identity, dict):
        return ["installed_identity"]
    blockers: list[str] = []
    revision = identity.get("revision")
    if not isinstance(revision, str) or not re.fullmatch(r"(?:[0-9a-fA-F]{40}|[0-9a-fA-F]{64}|sha256:[0-9a-fA-F]{64})", revision.strip()):
        blockers.append("installed_identity.revision must be a full revision/hash, not a version string")
    binary_sha256 = identity.get("binary_sha256")
    if not isinstance(binary_sha256, str) or not re.fullmatch(r"sha256:[0-9a-fA-F]{64}", binary_sha256.strip()):
        blockers.append("installed_identity.binary_sha256 must be sha256:<64 hex digits>")
    if identity.get("source_to_installed_parity") is not True:
        blockers.append("installed_identity.source_to_installed_parity=true")
    if not isinstance(identity.get("parity_method"), str) or not identity["parity_method"].strip():
        blockers.append("installed_identity.parity_method")
    return blockers


def _campaign_receipt_blockers(receipt: dict[str, Any]) -> list[str]:
    """Fail closed on the human-gated live campaign contract before CLI reads."""
    blockers: list[str] = []
    blockers.extend(_installed_identity_blockers(receipt))

    def finite_number(value: Any) -> bool:
        if isinstance(value, bool) or not isinstance(value, (int, float)):
            return False
        try:
            return math.isfinite(value)
        except (OverflowError, ValueError):
            return False

    def required_string(value: Any, label: str) -> None:
        if not isinstance(value, str) or not value.strip():
            blockers.append(label)

    def required_dict_fields(value: Any, label: str, fields: tuple[str, ...]) -> dict[str, Any]:
        if not isinstance(value, dict):
            blockers.append(label)
            return {}
        for field in fields:
            required_string(value.get(field), f"{label}.{field}")
        return value

    gate = required_dict_fields(receipt.get("human_gate"), "human_gate", ("gate_id", "owner", "action", "receipt", "status"))
    if gate.get("gate_id") != "FLW-G-0002":
        blockers.append("human_gate.gate_id=FLW-G-0002")
    if str(gate.get("status", "")).strip().lower() not in {"accepted", "waived", "authorized"}:
        blockers.append("human_gate.status=accepted|waived|authorized")
    if str(gate.get("action", "")).strip().lower() in {"return", "return_rework", "rework"}:
        blockers.append("human_gate.action must not return rework for a live campaign")

    topology = receipt.get("disposable_topology")
    if not isinstance(topology, dict):
        blockers.append("disposable_topology")
        topology = {}
    if topology.get("disposable") is not True:
        blockers.append("disposable_topology.disposable=true")
    for field in ("repo_path", "workspace_path"):
        value = topology.get(field)
        required_string(value, f"disposable_topology.{field}")
        if isinstance(value, str) and value.strip() and not Path(value).expanduser().is_absolute():
            blockers.append(f"disposable_topology.{field} must be absolute")
    dependent = topology.get("dependent_task_ids")
    independent = topology.get("independent_task_ids")
    if not isinstance(dependent, list) or len(dependent) != 2 or not all(isinstance(item, str) and item.strip() for item in dependent):
        blockers.append("disposable_topology.dependent_task_ids must contain exactly two task IDs")
        dependent = []
    elif len(set(dependent)) != len(dependent):
        blockers.append("disposable_topology.dependent_task_ids must be unique")
    if not isinstance(independent, list) or len(independent) != 1 or not all(isinstance(item, str) and item.strip() for item in independent):
        blockers.append("disposable_topology.independent_task_ids must contain exactly one task ID")
        independent = []
    elif len(set(independent)) != len(independent):
        blockers.append("disposable_topology.independent_task_ids must be unique")
    if set(dependent) & set(independent):
        blockers.append("disposable_topology dependent and independent task IDs must be disjoint")
    all_tasks = topology.get("task_ids")
    if not isinstance(all_tasks, list) or len(set(all_tasks)) != len(all_tasks) or set(all_tasks) != set(dependent) | set(independent):
        blockers.append("disposable_topology.task_ids must equal dependent plus independent task IDs")
    for field in ("owned_paths", "resources"):
        value = topology.get(field)
        if not isinstance(value, list) or not value or not all(isinstance(item, str) and item.strip() for item in value):
            blockers.append(f"disposable_topology.{field} must be a non-empty string list")
    ownership = topology.get("task_ownership")
    expected_tasks = set(dependent) | set(independent)
    if not isinstance(ownership, dict) or set(ownership) != expected_tasks:
        blockers.append("disposable_topology.task_ownership must map every task ID exactly once")
        ownership = {}
    owned_paths: dict[str, str] = {}
    owned_resources: dict[str, str] = {}

    def normalized_owned_path(value: str, task_id: str) -> str:
        path = Path(value).expanduser()
        if not path.is_absolute():
            blockers.append(f"disposable_topology.task_ownership[{task_id}].owned_paths must be absolute")
        return os.path.normpath(os.path.abspath(str(path)))

    def path_overlaps(first: str, second: str) -> bool:
        try:
            return os.path.commonpath((first, second)) in {first, second}
        except ValueError:
            return False

    for task_id in expected_tasks:
        item = ownership.get(task_id)
        if not isinstance(item, dict):
            blockers.append(f"disposable_topology.task_ownership[{task_id}] must be an object")
            continue
        for field, seen in (("owned_paths", owned_paths), ("resources", owned_resources)):
            values = item.get(field)
            if not isinstance(values, list) or not values or not all(isinstance(value, str) and value.strip() for value in values):
                blockers.append(f"disposable_topology.task_ownership[{task_id}].{field} must be a non-empty string list")
                continue
            for value in values:
                normalized = normalized_owned_path(value, task_id) if field == "owned_paths" else value.strip()
                overlap = None
                if field == "owned_paths":
                    for prior, owner in seen.items():
                        if path_overlaps(prior, normalized):
                            overlap = owner
                            break
                else:
                    overlap = seen.get(normalized)
                if overlap is not None:
                    if overlap == task_id:
                        blockers.append(f"disposable_topology.task_ownership[{task_id}].{field} must be unique")
                    else:
                        blockers.append(f"disposable_topology {field} must be pairwise disjoint")
                else:
                    seen[normalized] = task_id
    required_string(receipt.get("resource_owner"), "resource_owner")
    required_string(receipt.get("runtime_owner"), "runtime_owner")
    for field in ("permitted_mutations", "cleanup_targets"):
        value = receipt.get(field)
        if not isinstance(value, list) or not value or not all(isinstance(item, str) and item.strip() for item in value):
            blockers.append(f"{field} must be a non-empty string list")

    bounds = receipt.get("cumulative_bounds")
    if not isinstance(bounds, dict):
        blockers.append("cumulative_bounds")
        bounds = {}
    bound_fields = ("max_attempts", "max_elapsed_seconds", "max_repair_rounds", "max_human_interventions", "max_spend_usd")
    for field in bound_fields:
        value = bounds.get(field)
        if not finite_number(value) or value < 0 or (field != "max_human_interventions" and value == 0):
            blockers.append(f"cumulative_bounds.{field} must be a finite positive numeric bound")

    observed = receipt.get("observations")
    if not isinstance(observed, dict):
        blockers.append("observations")
        observed = {}
    observed_fields = ("attempts", "elapsed_seconds", "repair_rounds", "human_interventions")
    for field in observed_fields:
        value = observed.get(field)
        if not finite_number(value) or value < 0:
            blockers.append(f"observations.{field} must be a finite non-negative number")
    cost = observed.get("provider_cost")
    if not isinstance(cost, dict) or cost.get("status") != "known":
        blockers.append("observations.provider_cost must be known; unknown cost cannot qualify SF-14")
    elif not finite_number(cost.get("amount")) or cost.get("amount") < 0:
        blockers.append("observations.provider_cost.amount must be a finite non-negative number")
    required_string(cost.get("currency") if isinstance(cost, dict) else None, "observations.provider_cost.currency")
    if isinstance(cost, dict) and cost.get("currency") != "USD":
        blockers.append("observations.provider_cost.currency must be USD")
    for observed_name, bound_name in (
        ("attempts", "max_attempts"),
        ("elapsed_seconds", "max_elapsed_seconds"),
        ("repair_rounds", "max_repair_rounds"),
        ("human_interventions", "max_human_interventions"),
    ):
        value, bound = observed.get(observed_name), bounds.get(bound_name)
        if finite_number(value) and finite_number(bound) and value > bound:
            blockers.append(f"observations.{observed_name} exceeds cumulative_bounds.{bound_name}")
    if isinstance(cost, dict) and cost.get("status") == "known" and cost.get("currency") == "USD" and finite_number(cost.get("amount")) and finite_number(bounds.get("max_spend_usd")) and cost["amount"] > bounds["max_spend_usd"]:
        blockers.append("observations.provider_cost.amount exceeds cumulative_bounds.max_spend_usd")

    artifacts = receipt.get("artifact_identities")
    if not isinstance(artifacts, dict):
        blockers.append("artifact_identities")
        artifacts = {}
    for name in ("installed_binary", "failed_check", "correction", "review", "integration", "dependent_progression", "independent_progression"):
        required_string(artifacts.get(name), f"artifact_identities.{name}")

    progression = receipt.get("progression")
    if not isinstance(progression, dict):
        blockers.append("progression")
        progression = {}
    dependent_receipt = required_dict_fields(progression.get("dependent"), "progression.dependent", ("task_id", "attempt_id", "artifact_identity"))
    independent_receipt = required_dict_fields(progression.get("independent"), "progression.independent", ("task_id", "attempt_id", "artifact_identity"))
    integration_receipt = required_dict_fields(progression.get("integration"), "progression.integration", ("task_id", "attempt_id", "artifact_identity"))
    if dependent_receipt.get("released") is not True or dependent_receipt.get("accepted") is not True:
        blockers.append("progression.dependent.released=true and accepted=true")
    if independent_receipt.get("progressed") is not True:
        blockers.append("progression.independent.progressed=true")
    if integration_receipt.get("accepted") is not True:
        blockers.append("progression.integration.accepted=true")
    if dependent_receipt.get("task_id") not in dependent:
        blockers.append("progression.dependent.task_id must be a dependent task")
    if independent_receipt.get("task_id") not in independent:
        blockers.append("progression.independent.task_id must be the independent task")
    if integration_receipt.get("task_id") not in dependent:
        blockers.append("progression.integration.task_id must be a dependent task")
    if dependent_receipt.get("task_id") in dependent and integration_receipt.get("task_id") != dependent_receipt.get("task_id"):
        blockers.append("progression.integration.task_id must match the released dependent task")

    for lane in ("execute", "review"):
        for field in ("harness", "model", "effort"):
            values = [receipt.get(f"{stage}_{lane}_{field}") for stage in ("requested", "admitted", "actual")]
            if not all(isinstance(value, str) and value.strip() for value in values):
                blockers.append(f"requested/admitted/actual_{lane}_{field} must be exact non-empty values")
            elif len(set(values)) != 1:
                blockers.append(f"requested/admitted/actual {lane} {field} disagree")
    return blockers


def synthetic_negative_checks() -> tuple[bool, str]:
    """Exercise the fail-closed campaign receipt rules without live services."""
    base: dict[str, Any] = {
        "human_gate": {"gate_id": "FLW-G-0002", "owner": "Sarav", "action": "accept", "receipt": "signed", "status": "accepted"},
        "installed_identity": {
            "revision": "0123456789abcdef0123456789abcdef01234567",
            "binary_sha256": "sha256:" + "0" * 64,
            "source_to_installed_parity": True,
            "parity_method": "binary revision and sha256 matched source build receipt",
        },
        "disposable_topology": {
            "disposable": True,
            "repo_path": "/private/tmp/tusker-w27-repo",
            "workspace_path": "/private/tmp/tusker-w27-workspace",
            "dependent_task_ids": ["dep-1", "dep-2"],
            "independent_task_ids": ["ind-1"],
            "task_ids": ["dep-1", "dep-2", "ind-1"],
            "owned_paths": ["/private/tmp/tusker-w27-repo"],
            "resources": ["runtime-1"],
            "task_ownership": {
                "dep-1": {"owned_paths": ["/private/tmp/tusker-w27-repo/dep-1"], "resources": ["resource-1"]},
                "dep-2": {"owned_paths": ["/private/tmp/tusker-w27-repo/dep-2"], "resources": ["resource-2"]},
                "ind-1": {"owned_paths": ["/private/tmp/tusker-w27-repo/ind-1"], "resources": ["resource-3"]},
            },
        },
        "resource_owner": "runtime-owner",
        "runtime_owner": "runtime-owner",
        "permitted_mutations": ["task-owned files"],
        "cleanup_targets": ["/private/tmp/tusker-w27-repo"],
        "cumulative_bounds": {"max_attempts": 3, "max_elapsed_seconds": 600, "max_repair_rounds": 2, "max_human_interventions": 0, "max_spend_usd": 5},
        "observations": {"attempts": 1, "elapsed_seconds": 30, "repair_rounds": 1, "human_interventions": 0, "provider_cost": {"status": "known", "amount": 1, "currency": "USD"}},
        "artifact_identities": {name: f"{name}-artifact" for name in ("installed_binary", "failed_check", "correction", "review", "integration", "dependent_progression", "independent_progression")},
        "progression": {
            "dependent": {"task_id": "dep-1", "attempt_id": "dep-attempt", "artifact_identity": "dep-artifact", "released": True, "accepted": True},
            "independent": {"task_id": "ind-1", "attempt_id": "ind-attempt", "artifact_identity": "ind-artifact", "progressed": True},
            "integration": {"task_id": "dep-1", "attempt_id": "int-attempt", "artifact_identity": "int-artifact", "accepted": True},
        },
    }
    for lane in ("execute", "review"):
        for field in ("harness", "model", "effort"):
            base[f"requested_{lane}_{field}"] = f"{field}-value"
            base[f"admitted_{lane}_{field}"] = f"{field}-value"
            base[f"actual_{lane}_{field}"] = f"{field}-value"
    cases = (
        ("duplicate dependent IDs", {"disposable_topology": {"dependent_task_ids": ["dep-1", "dep-1"]}}, "dependent_task_ids must be unique"),
        ("integration outside dependent IDs", {"progression": {"integration": {"task_id": "ind-1"}}}, "integration.task_id must be a dependent task"),
        ("missing per-task ownership", {"disposable_topology": {"task_ownership": {}}}, "task_ownership must map every task ID"),
        ("overlapping owned paths", {"disposable_topology": {"task_ownership": {"dep-2": {"owned_paths": ["/private/tmp/tusker-w27-repo/dep-1"]}}}}, "owned_paths must be pairwise disjoint"),
        ("ancestor owned paths", {"disposable_topology": {"task_ownership": {"dep-2": {"owned_paths": ["/private/tmp/tusker-w27-repo/dep-1/nested"]}}}}, "owned_paths must be pairwise disjoint"),
        ("non-finite bound", {"cumulative_bounds": {"max_attempts": float("nan")}}, "max_attempts must be a finite"),
        ("non-finite cost", {"observations": {"provider_cost": {"amount": float("inf")}}}, "provider_cost.amount must be a finite"),
        ("non-USD cost", {"observations": {"provider_cost": {"currency": "EUR"}}}, "provider_cost.currency must be USD"),
    )
    for name, changes, expected in cases:
        candidate = json.loads(json.dumps(base, allow_nan=True))
        if name in {"overlapping owned paths", "ancestor owned paths"}:
            candidate["disposable_topology"]["task_ownership"]["dep-2"]["owned_paths"] = changes["disposable_topology"]["task_ownership"]["dep-2"]["owned_paths"]
        elif name == "non-finite cost":
            candidate["observations"]["provider_cost"]["amount"] = float("inf")
        elif name == "non-USD cost":
            candidate["observations"]["provider_cost"]["currency"] = "EUR"
        else:
            for section, values in changes.items():
                candidate[section].update(values)
        blockers = _campaign_receipt_blockers(candidate)
        if not any(expected in blocker for blocker in blockers):
            return False, f"synthetic negative check failed: {name} (expected {expected}; blockers={blockers})"
    return True, f"synthetic negative checks passed ({len(cases)} fail-closed cases)"


def read_live_receipt() -> tuple[str, dict[str, Any]]:
    path_value = os.environ.get("TUSKER_LIVE_RECEIPT", "").strip()
    if not path_value:
        required = [
            "TUSKER_LIVE_RECEIPT",
            "TUSKER_LIVE_BUILD_ID",
            "TUSKER_LIVE_PROJECT_ID",
            "TUSKER_LIVE_TASK_ID",
        ]
        return "NOT RUN", {"blockers": [f"missing {name}" for name in required]}
    path = Path(path_value).expanduser()
    if not path.is_file():
        return "NOT RUN", {"blockers": [f"live receipt does not exist: {path}"]}
    try:
        receipt = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        return "FAIL", {"blockers": [f"could not parse live receipt {path}: {exc}"]}
    if not isinstance(receipt, dict):
        return "FAIL", {"blockers": ["live receipt root must be a JSON object"]}
    required_values = {
        "receipt_schema": "tusker.live-qualification/v1",
        "evidence_kind": "live",
        "evidence_source": "installed_runtime",
        "build_id": os.environ.get("TUSKER_LIVE_BUILD_ID", ""),
        "project_id": os.environ.get("TUSKER_LIVE_PROJECT_ID", ""),
        "task_id": os.environ.get("TUSKER_LIVE_TASK_ID", ""),
    }
    missing = [key for key, value in required_values.items() if not value]
    mismatched = [key for key, value in required_values.items() if value and receipt.get(key) != value]
    campaign_blockers = _campaign_receipt_blockers(receipt)
    for key in (
        "wave_id",
        "execute_attempt_id",
        "review_attempt_id",
        "requested_execute_profile",
        "admitted_execute_profile",
        "actual_execute_profile",
        "requested_review_profile",
        "admitted_review_profile",
        "actual_review_profile",
        "observed_at",
        "observed_by",
        "execute_worker_policy_fingerprint",
        "review_worker_policy_fingerprint",
        "route_preview_revision",
        "route_preview_fingerprint",
        "mapping_removed_at",
        "mapping_restored_at",
        "reviewer_conversation",
    ):
        if not str(receipt.get(key, "")).strip():
            missing.append(key)
    if receipt.get("execute_attempt_id") == receipt.get("review_attempt_id"):
        missing.append("distinct execute/review attempt IDs")
    for key in ("failure_corrected", "independent_review", "accepted"):
        if receipt.get(key) is not True:
            missing.append(f"{key}=true")
    required_evidence = {
        "blocked_start_observation": ("no_directive_or_attempt_created",),
        "failed_check": ("attempt_id", "output"),
        "correction": ("attempt_id", "output"),
        "human_gate": ("gate_id", "owner", "action", "receipt"),
        "self_claim": ("attempt_id", "actor", "conversation", "workspace", "trigger"),
    }
    for section, fields in required_evidence.items():
        value = receipt.get(section)
        if not isinstance(value, dict):
            missing.append(section)
            continue
        for field in fields:
            field_value = value.get(field)
            if field == "no_directive_or_attempt_created":
                if field_value is not True:
                    missing.append(f"{section}.{field}=true")
            elif not str(field_value or "").strip():
                missing.append(f"{section}.{field}")
    self_claim = receipt.get("self_claim")
    if isinstance(self_claim, dict):
        if not str(self_claim.get("trigger", "")).startswith("self_implementation;"):
            missing.append("self_claim.trigger=self_implementation;...")
        if self_claim.get("conversation") == receipt.get("reviewer_conversation"):
            missing.append("distinct self-claim/reviewer conversations")
    if missing or mismatched or campaign_blockers:
        return "FAIL", {"path": str(path), "missing": missing, "mismatched": mismatched, "campaign_blockers": campaign_blockers}
    if disagree := live_profile_mismatches(receipt):
        return "FAIL", {"path": str(path), "blockers": [f"{lane} requested/admitted/actual profiles disagree" for lane in disagree]}

    tusker = os.environ.get("TUSKER_LIVE_TUSKER_BIN", "").strip() or shutil.which("tusker")
    if not tusker:
        return "NOT RUN", {"path": str(path), "blockers": ["installed tusker executable is unavailable"]}

    def tusker_json(*args: str, cwd: Path = ROOT) -> tuple[Optional[dict[str, Any]], str]:
        code, output = run_command([tusker, *args], cwd=cwd)
        if code != 0:
            return None, f"{' '.join(args)} failed: {first_failure(output)}"
        try:
            value = json.loads(output)
        except json.JSONDecodeError as exc:
            return None, f"{' '.join(args)} returned invalid JSON: {exc}"
        return (value, "") if isinstance(value, dict) else (None, f"{' '.join(args)} returned a non-object")

    version, error = tusker_json("version", "--json")
    if error or version is None:
        return "FAIL", {"path": str(path), "blockers": [error]}
    installed_identity = receipt["installed_identity"]
    if version.get("revision") != installed_identity["revision"] or version.get("binary_sha256") != installed_identity["binary_sha256"]:
        return "FAIL", {"path": str(path), "blockers": ["installed tusker revision/hash does not match the exact installed_identity receipt"]}
    if receipt["build_id"] not in {installed_identity["revision"], installed_identity["binary_sha256"]}:
        return "FAIL", {"path": str(path), "blockers": ["receipt build_id must be the installed revision or binary sha256, not a version string"]}

    inspection, error = tusker_json("runs", "inspect", receipt["task_id"], "--project", receipt["project_id"], "--json")
    if error or inspection is None:
        return "FAIL", {"path": str(path), "blockers": [error]}
    attempts = inspection.get("attempts", [])
    identity = inspection.get("identity") or {}
    registered_repo = str(identity.get("registered_repo_path", "")).strip()
    if not registered_repo:
        return "FAIL", {"path": str(path), "blockers": ["installed runtime omitted identity.registered_repo_path; refusing cwd fallback"]}
    project_root = Path(registered_repo).expanduser()
    if not project_root.is_absolute():
        return "FAIL", {"path": str(path), "blockers": ["installed identity.registered_repo_path must be absolute"]}
    project_root = project_root.resolve()
    if not project_root.is_dir():
        return "FAIL", {"path": str(path), "blockers": ["installed runtime did not identify a registered project repository"]}
    topology_repo = str(receipt["disposable_topology"]["repo_path"]).strip()
    if project_root != Path(topology_repo).expanduser().resolve():
        return "FAIL", {"path": str(path), "blockers": ["installed registered_repo_path does not match disposable_topology.repo_path"]}
    models, error = tusker_json("models", "show", "--json", cwd=project_root)
    if error or models is None or models.get("revision") != receipt["route_preview_revision"]:
        return "FAIL", {"path": str(path), "blockers": [error or "route preview revision does not match registered project configuration"]}
    routes: dict[str, Any] = {}
    for lane in ("execute", "review"):
        route, error = tusker_json("runner", "route", receipt["task_id"], "--lane", lane, "--json", cwd=project_root)
        if error or route is None or route.get("blockers"):
            return "FAIL", {"path": str(path), "blockers": [error or f"installed {lane} route is blocked"]}
        if route.get("profile") != receipt[f"actual_{lane}_profile"]:
            return "FAIL", {"path": str(path), "blockers": [f"{lane} actual profile does not match registered project route"]}
        for field in ("harness", "model", "effort"):
            if route.get(field) != receipt[f"actual_{lane}_{field}"]:
                return "FAIL", {"path": str(path), "blockers": [f"{lane} actual {field} does not match registered project route"]}
        routes[lane] = route
    route_fingerprint = "sha256:" + hashlib.sha256(json.dumps(routes, sort_keys=True, separators=(",", ":")).encode()).hexdigest()
    if route_fingerprint != receipt["route_preview_fingerprint"]:
        return "FAIL", {"path": str(path), "blockers": ["route preview fingerprint does not match authoritative execute/review routes"]}
    by_id = {str(item.get("AttemptID", item.get("attempt_id", ""))): item for item in attempts if isinstance(item, dict)}
    selected_attempts: dict[str, dict[str, Any]] = {}
    for lane in ("execute", "review"):
        attempt = by_id.get(receipt[f"{lane}_attempt_id"])
        if not attempt:
            return "FAIL", {"path": str(path), "blockers": [f"{lane} attempt is absent from installed runtime history"]}
        if attempt.get("Lane", attempt.get("lane")) != lane or attempt.get("Outcome", attempt.get("outcome")) != "succeeded":
            return "FAIL", {"path": str(path), "blockers": [f"{lane} attempt is not a successful {lane} attempt"]}
        if attempt.get("WorkerPolicyFP", attempt.get("worker_policy_fingerprint")) != receipt[f"{lane}_worker_policy_fingerprint"]:
            return "FAIL", {"path": str(path), "blockers": [f"{lane} attempt policy fingerprint does not match installed runtime"]}
        selected_attempts[lane] = attempt

    failed_claim = receipt["failed_check"]
    failed_attempt = by_id.get(str(failed_claim["attempt_id"]))
    if not failed_attempt or failed_attempt.get("Lane", failed_attempt.get("lane")) != "execute" or failed_attempt.get("Outcome", failed_attempt.get("outcome")) in ("", "succeeded"):
        return "FAIL", {"path": str(path), "blockers": ["failed_check does not identify a failed installed execute attempt"]}
    failed_output = " ".join(str(failed_attempt.get(key, "")) for key in ("LastError", "last_error", "LogsSummary", "logs_summary", "FinalSummary", "final_summary"))
    if str(failed_claim["output"]).strip() not in failed_output:
        return "FAIL", {"path": str(path), "blockers": ["failed_check output is not present in installed attempt evidence"]}

    correction_claim = receipt["correction"]
    if correction_claim["attempt_id"] != receipt["execute_attempt_id"]:
        return "FAIL", {"path": str(path), "blockers": ["correction does not identify the selected successful execute attempt"]}
    correction_output = " ".join(str(selected_attempts["execute"].get(key, "")) for key in ("LogsSummary", "logs_summary", "FinalSummary", "final_summary"))
    if str(correction_claim["output"]).strip() not in correction_output:
        return "FAIL", {"path": str(path), "blockers": ["correction output is not present in installed attempt evidence"]}

    authorizations = inspection.get("authorizations", [])
    self_claim = receipt["self_claim"]
    if self_claim["attempt_id"] not in by_id:
        return "FAIL", {"path": str(path), "blockers": ["self_claim attempt is absent from installed runtime history"]}
    if not any(
        item.get("attempt_id") == self_claim["attempt_id"]
        and item.get("actor") == self_claim["actor"]
        and item.get("trigger") == self_claim["trigger"]
        for item in authorizations if isinstance(item, dict)
    ):
        return "FAIL", {"path": str(path), "blockers": ["self_claim does not match installed authorization history"]}
    if identity.get("workspace_path") != self_claim["workspace"]:
        return "FAIL", {"path": str(path), "blockers": ["self_claim workspace does not match installed run identity"]}
    reviewer_conversation = receipt["reviewer_conversation"]
    if not any(
        item.get("attempt_id") == receipt["review_attempt_id"]
        and str(item.get("trigger", "")).startswith("work_review;")
        and f"conversation={reviewer_conversation}" in str(item.get("trigger", "")).split(";")
        for item in authorizations if isinstance(item, dict)
    ):
        return "FAIL", {"path": str(path), "blockers": ["reviewer conversation does not match installed authorization history"]}
    current_run = inspection.get("run", {})
    current_lane = current_run.get("lane")
    if current_lane in ("execute", "review") and current_run.get("runner_profile") != receipt[f"actual_{current_lane}_profile"]:
        return "FAIL", {"path": str(path), "blockers": [f"{current_lane} actual profile does not match installed runtime"]}
    successful_execute_started = selected_attempts["execute"].get("StartedAt", selected_attempts["execute"].get("started_at", ""))
    if not any(
        item.get("Lane", item.get("lane")) == "execute"
        and item.get("Outcome", item.get("outcome")) not in ("", "succeeded")
        and item.get("StartedAt", item.get("started_at", "")) < successful_execute_started
        for item in attempts if isinstance(item, dict)
    ):
        return "FAIL", {"path": str(path), "blockers": ["installed runtime has no failed execute attempt preceding the successful correction"]}

    closeout, error = tusker_json("closeout", "status", receipt["task_id"], "--json", cwd=project_root)
    if error or closeout is None:
        return "FAIL", {"path": str(path), "blockers": [error]}
    if closeout.get("machine_complete") is not True or closeout.get("reviewer_missing"):
        return "FAIL", {"path": str(path), "blockers": ["installed task is not machine-complete with independent review"]}

    # The current runtime does not durably expose these three historical
    # observations. Refuse a live PASS until installed authorities can
    # corroborate them; receipt prose alone is not evidence.
    return "FAIL", {
        "path": str(path),
        "blockers": [
            "installed runtime has no durable blocked-start observation authority",
            "installed runtime has no durable model-mapping removal/restoration history",
            "installed runtime has no durable completed human-gate receipt projection",
        ],
    }


def _w27_group_status(result: dict[str, Any], group: str) -> tuple[str, str]:
    for line in result.get("w27_results", []):
        if line.startswith(group + " PASS"):
            return "PASS", line
    for line in result.get("w27_failures", []):
        if line.startswith(group + " "):
            return "FAIL", line
    return "NOT RUN", "focused W-0027 gate was not invoked"


def _result_line(result: dict[str, Any], prefix: str) -> str:
    for line in result.get("results", []):
        if line.startswith(prefix + " "):
            return line
    return ""


def _acceptance_status(result: dict[str, Any], item: str, group: str) -> tuple[str, str]:
    status, evidence = _w27_group_status(result, group)
    if item == "SF-07":
        q9 = _result_line(result, "Q9")
        if status == "PASS" and q9:
            return "PARTIAL", f"repair-source gate passed; dependent pickup is covered by {q9}; independent-task progression is not covered by the cited source fixture"
        if status == "PASS":
            return "NOT RUN", "repair-source gate passed, but no dependent-pickup evidence was matched"
    if item == "SF-10" and status == "PASS":
        q14 = _result_line(result, "Q14")
        if q14:
            return "PARTIAL", f"backend amendment/pause/restart source gate and UI authority assertions passed ({q14}); installed integrated agreement remains NOT RUN"
        return "PARTIAL", "backend amendment/pause/restart source gate passed; UI assertion was not matched"
    if item == "SF-11" and status == "PASS":
        parity = result.get("source_parity", {}).get("status")
        if parity == "PASS":
            return "PARTIAL", f"source/current-checkout parity passed; {evidence}; installed help/API qualification is NOT RUN"
        return "NOT RUN", f"installed help/API qualification is NOT RUN; source parity={parity or 'unknown'}"
    return status, evidence


def render_report(mode: str, result: dict[str, Any]) -> str:
    built_status, built_details = built_bundle_report()
    identity = result.get("source_identity", {})
    parity = result.get("source_parity", {})
    live = result.get("live", {})
    browser = result.get("browser", {})
    matrix_groups = (
        ("SF-01", "packet round-trip preserves exact authored material", "SF-01..03"),
        ("SF-02", "invalid mappings and changed request keys leave no partial graph", "SF-01..03"),
        ("SF-03", "injected batch failure rolls back task, wave, gate, and events", "SF-01..03"),
        ("SF-04", "wrong-boundary, missing, failed, stale, and zero-match proof is rejected", "SF-04..06"),
        ("SF-05", "blocking findings remain open until exact-material closure", "SF-04..06"),
        ("SF-06", "required consumer/integration proof remains incomplete when absent", "SF-04..06"),
        ("SF-07", "accepted prerequisite releases dependent work", "SF-07..09"),
        ("SF-08", "repair caps and exhaustion survive restart", "SF-07..09"),
        ("SF-09", "uncertain admission reconciles without duplicate effects", "SF-07..09"),
        ("SF-10", "pause, resume, decision route, and duplicate-claim boundaries stay truthful", "SF-10"),
        ("SF-11", "shipped skill, source CLI/help, API, and capability seams agree", "SF-11"),
    )
    matrix: list[str] = ["| ID | Status | Evidence |", "|---|---|---|"]
    for item, description, group in matrix_groups:
        status, evidence = _acceptance_status(result, item, group)
        matrix.append(f"| {item} | {status} | {description}; {evidence} |")
    live_status = live.get("status", "NOT RUN")
    live_detail = live.get("detail", "live gate not invoked")
    for item, description in (
        ("SF-12", "real configured worker/reviewer detects, repairs, reviews, integrates, and releases dependent"),
        ("SF-13", "functional/recovery/quality/performance rows retain route, effort, time, rounds, and interventions"),
        ("SF-14", "provider cost and cumulative bounds are observed, with unknown cost never relabeled zero"),
    ):
        matrix.append(f"| {item} | {live_status} | {description}; {live_detail} |")
    lines = [
        "# W-0027 software-factory pilot",
        "",
        "This report is generated by `scripts/test-task-authoring-journey.py`. Source/local, browser, installed-runtime, provider, and human evidence remain separate.",
        "",
        "## Latest invocation",
        "",
        f"- Mode: `{mode}`",
        f"- Status: **{result['status']}**",
        f"- Detail: {result.get('detail', '')}",
        "",
        "## Source/current-checkout identity",
        "",
        f"- HEAD: `{identity.get('revision', 'not captured')}`; dirty checkout: `{identity.get('dirty', 'unknown')}` ({identity.get('dirty_entries', 'unknown')} entries).",
        f"- Focused source fingerprint: `{identity.get('source_fingerprint', 'not captured')}`.",
        f"- Source parity: **{parity.get('status', 'NOT RUN')}** — {('; '.join(parity.get('failures', []))) if parity.get('failures') else 'accepted source seams and canonical skill symlinks are present'}.",
        "- This identity describes the current checkout only; it is not installed-binary identity.",
        "",
        "## Deterministic prerequisites",
        "",
        "The offline lane runs Q1-Q14 plus focused SF-01..11 source tests. Every filtered Go command requires a nonzero PASS match and treats skipped substantive tests as incomplete; the first actionable failure is retained.",
        "",
        f"- Status: **{result.get('status', 'NOT RUN')}** — {result.get('detail', '')}",
        f"- Go environment: `{json.dumps(result.get('environment', {}), sort_keys=True)}`.",
        "",
        "## SF-01..14 acceptance map",
        "",
        *matrix,
        "",
        "SF-12..14 are not upgraded by deterministic fixtures. The current invocation launched no daemon, installed binary, provider, or architect route; live provider attempts in this invocation are 0, provider cost is unknown/not measured, and offline human interventions were none. SF-11 source parity is reported separately from its installed portion; SF-07 and SF-10 are partial because their cited source/fixture checks do not establish the full integrated topology.",
        "",
        "## Built bundle",
        "",
        f"- `{built_status}` — {'; '.join(built_details)}",
        "- A browser run is only reported below when a real service URL and registered project are supplied; a static bundle check does not establish UI behavior.",
        "",
        "## Browser and live boundaries",
        "",
        "| Lane | Status | Evidence |\n|---|---|---|",
        f"| Read-only browser journey | {browser.get('status', 'NOT RUN')} | {browser.get('detail', 'not invoked')} |",
        f"| Installed execute/review trial | {live_status} | {live_detail} |",
        "",
        "## Exact commands/output",
        "",
        "```text",
        result.get("command", "not invoked"),
        "```",
        "",
        "```text",
        result.get("output", result.get("detail", ""))[-12000:],
        "```",
        "",
        "The script never starts a daemon, arms a wave, invokes a provider, or treats fixture output as installed proof.",
        "",
        "## Exact live gate inputs still required",
        "",
        "FLW-G-0002 remains `waiting_on_human`. Sarav must supply or explicitly approve: (1) exact installed identity with a full revision, binary sha256, and explicit source-to-installed parity method; (2) an accepted/waived `human_gate.gate_id=FLW-G-0002` receipt; (3) configured execute/review routes with exact requested/admitted/actual harness, model, effort, and profiles; (4) an independently running runtime/resource owner; (5) an absolute disposable repo/workspace with exactly two dependent and one independent task, per-task owned-path/resource maps that are pairwise disjoint, permitted mutations, and cleanup targets; (6) finite cumulative attempt, elapsed-time, repair-round, human-intervention, and USD provider-spend ceilings plus observed cost/round/intervention values; (7) artifact identities for installed binary, failed check, correction, review, integration, and both progression receipts; (8) dependent release/integration and independent progression receipts bound to the declared task IDs; and (9) any verified decision-route endpoint. Without those inputs, SF-12..14 remain NOT RUN and A2/A3 are not claimed.",
        "",
    ]
    return "\n".join(lines)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--mode", choices=("offline", "browser", "live"), required=True)
    parser.add_argument("--report", default=str(REPORT))
    args = parser.parse_args()

    if args.mode == "offline":
        result = run_offline()
    elif args.mode == "browser":
        result = run_browser()
        result["browser"] = {"status": result["status"], "detail": result.get("detail", "")}
    else:
        status, data = read_live_receipt()
        result = {"status": status, "detail": json.dumps(data, sort_keys=True), "live": {"status": status, "detail": json.dumps(data, sort_keys=True)}}

    if args.mode != "live":
        live_status, live_data = read_live_receipt()
        result["live"] = {"status": live_status, "detail": json.dumps(live_data, sort_keys=True)}
    if args.mode != "browser":
        result["browser"] = run_browser() if args.mode == "offline" else {"status": "NOT RUN", "detail": "browser lane not invoked"}
    if args.mode != "offline":
        result.setdefault("command", "not invoked")
        result.setdefault("output", "")

    report_path = Path(args.report)
    if not report_path.is_absolute():
        report_path = ROOT / report_path
    report_path.parent.mkdir(parents=True, exist_ok=True)
    report_path.write_text(render_report(args.mode, result), encoding="utf-8")
    print(json.dumps({"mode": args.mode, "status": result["status"], "report": str(report_path)}, indent=2))
    return 0 if result["status"] == "PASS" else 2 if result["status"] == "NOT RUN" else 1


if __name__ == "__main__":
    sys.exit(main())
