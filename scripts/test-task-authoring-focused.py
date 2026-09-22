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
import os
from pathlib import Path
import re
import shlex
import shutil
import subprocess
import sys
from typing import Any, Optional


ROOT = Path(__file__).resolve().parents[1]
REPORT = ROOT / "docs/reports/task-authoring/first-trial.md"

SOURCE_PARITY_FILES: tuple[tuple[str, tuple[str, ...]], ...] = (
    (
        ".tusker/work/tasks/FLW-T-0041.md",
        ("FLW-T-0041", "A fresh authoring exercise", "NOT RUN"),
    ),
    (
        "cmd/tusker/task_authoring_journey_test.go",
        ("TestTaskAuthoringJourney", "taskAuthoringJourneyRequest", "configureTaskAuthoringJourneyProfiles"),
    ),
    (
        "internal/serve/ui/test/task-authoring.browser.mjs",
        ("task-authoring-ui-acceptance", "TUSKER_AUTHORING_SCENARIO_TASK_ID", "route-blocked"),
    ),
    (
        "skills/tusker/SKILL.md",
        ("tusker capabilities --json", "manufacture proof"),
    ),
    (
        "docs/reports/task-authoring/first-trial.md",
        ("FLW-T-0041", "A1", "A2", "A3"),
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
        "GOCACHE": "/private/tmp/tusker-w23-offline-gocache",
        "GOTMPDIR": "/private/tmp/tusker-w23-offline-gotmp",
        "TUSKER_VALIDATION_LOCK_DIR": "/private/tmp/tusker-w23-offline-locks",
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


OFFLINE_CASES: list[tuple[str, str, str]] = [
    ("A1/A2", "fresh classified wave, standalone task, routes and human boundary", "TestTaskAuthoringJourney"),
]


def run_offline() -> dict[str, Any]:
    environment = offline_environment()
    identity = source_identity()
    parity = source_parity()
    results: list[str] = []
    matched = 0
    failures: list[str] = []
    commands: list[str] = []
    if parity["status"] != "PASS":
        failures.append("source parity: " + " | ".join(parity["failures"]))
    for case_id, description, pattern in OFFLINE_CASES:
        command = [
            shutil.which("go") or "go",
            "test",
            "./cmd/tusker",
            "-run",
            f"^{pattern}$",
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
    if matched == 0:
        status = "FAIL"
        detail = "focused W-0023 offline lane matched zero cases"
    elif failures:
        status = "FAIL"
        detail = f"matched {matched} cases; failures: " + " | ".join(failures)
    else:
        status = "PASS"
        detail = f"focused W-0023 offline lane matched {matched} test(s); zero failures"
    return {
        "status": status,
        "command": "\n".join(commands),
        "detail": detail,
        "output": "\n".join(results + failures),
        "results": results,
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
        return {"status": "FAIL", "detail": "; ".join(built_details), "built": [built_status, built_details]}
    if built_status != "PASS":
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


def _live_receipt_blockers(receipt: dict[str, Any]) -> list[str]:
    """List missing installed-trial evidence without manufacturing a failure."""
    blockers = _installed_identity_blockers(receipt)

    def required(value: Any, label: str) -> None:
        if not isinstance(value, str) or not value.strip():
            blockers.append(label)

    gate = receipt.get("human_gate")
    if not isinstance(gate, dict):
        blockers.append("human_gate")
    else:
        for field in ("gate_id", "owner", "action", "receipt", "status"):
            required(gate.get(field), f"human_gate.{field}")

    for lane in ("execute", "review"):
        for field in ("harness", "model", "effort"):
            for stage in ("requested", "admitted", "actual"):
                required(receipt.get(f"{stage}_{lane}_{field}"), f"{stage}_{lane}_{field}")

    for field in (
        "wave_id", "execute_attempt_id", "review_attempt_id", "observed_at", "observed_by",
        "execute_worker_policy_fingerprint", "review_worker_policy_fingerprint",
        "route_preview_revision", "route_preview_fingerprint", "mapping_removed_at",
        "mapping_restored_at", "reviewer_conversation",
    ):
        required(receipt.get(field), field)
    if receipt.get("execute_attempt_id") == receipt.get("review_attempt_id") and receipt.get("execute_attempt_id"):
        blockers.append("distinct execute/review attempt IDs")
    for key in ("failure_corrected", "independent_review", "accepted"):
        if receipt.get(key) is not True:
            blockers.append(f"{key}=true")

    for section, fields in {
        "blocked_start_observation": ("no_directive_or_attempt_created",),
        "failed_check": ("attempt_id", "output"),
        "correction": ("attempt_id", "output"),
        "self_claim": ("attempt_id", "actor", "conversation", "workspace", "trigger"),
    }.items():
        value = receipt.get(section)
        if not isinstance(value, dict):
            blockers.append(section)
            continue
        for field in fields:
            if field == "no_directive_or_attempt_created":
                if value.get(field) is not True:
                    blockers.append(f"{section}.{field}=true")
            else:
                required(value.get(field), f"{section}.{field}")
    self_claim = receipt.get("self_claim")
    if isinstance(self_claim, dict):
        if not str(self_claim.get("trigger", "")).startswith("self_implementation;"):
            blockers.append("self_claim.trigger=self_implementation;...")
        if self_claim.get("conversation") == receipt.get("reviewer_conversation"):
            blockers.append("distinct self-claim/reviewer conversations")
    return blockers


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
    trial_blockers = _live_receipt_blockers(receipt)
    if mismatched:
        return "FAIL", {"path": str(path), "mismatched": mismatched, "blockers": trial_blockers}
    if missing or trial_blockers:
        return "NOT RUN", {"path": str(path), "blockers": missing + trial_blockers}
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
        return "NOT RUN", {"path": str(path), "blockers": [error or "installed tusker version is unavailable"]}
    installed_identity = receipt["installed_identity"]
    if version.get("revision") != installed_identity["revision"] or version.get("binary_sha256") != installed_identity["binary_sha256"]:
        return "FAIL", {"path": str(path), "blockers": ["installed tusker revision/hash does not match the exact installed_identity receipt"]}
    if receipt["build_id"] not in {installed_identity["revision"], installed_identity["binary_sha256"]}:
        return "FAIL", {"path": str(path), "blockers": ["receipt build_id must be the installed revision or binary sha256, not a version string"]}

    inspection, error = tusker_json("runs", "inspect", receipt["task_id"], "--project", receipt["project_id"], "--json")
    if error or inspection is None:
        return "NOT RUN", {"path": str(path), "blockers": [error or "installed runtime inspection is unavailable"]}
    attempts = inspection.get("attempts", [])
    identity = inspection.get("identity") or {}
    registered_repo = str(identity.get("registered_repo_path", "")).strip()
    if not registered_repo:
        return "NOT RUN", {"path": str(path), "blockers": ["installed runtime omitted identity.registered_repo_path; refusing cwd fallback"]}
    project_root = Path(registered_repo).expanduser()
    if not project_root.is_absolute():
        return "NOT RUN", {"path": str(path), "blockers": ["installed identity.registered_repo_path must be absolute"]}
    project_root = project_root.resolve()
    if not project_root.is_dir():
        return "NOT RUN", {"path": str(path), "blockers": ["installed runtime did not identify a registered project repository"]}
    models, error = tusker_json("models", "show", "--json", cwd=project_root)
    if error or models is None or models.get("revision") != receipt["route_preview_revision"]:
        if error or models is None:
            return "NOT RUN", {"path": str(path), "blockers": [error or "installed model mapping is unavailable"]}
        return "FAIL", {"path": str(path), "blockers": ["route preview revision does not match registered project configuration"]}
    routes: dict[str, Any] = {}
    for lane in ("execute", "review"):
        route, error = tusker_json("runner", "route", receipt["task_id"], "--lane", lane, "--json", cwd=project_root)
        if error or route is None:
            return "NOT RUN", {"path": str(path), "blockers": [error or f"installed {lane} route is unavailable"]}
        if route.get("blockers"):
            return "NOT RUN", {"path": str(path), "blockers": [f"installed {lane} route is blocked"]}
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
        return "NOT RUN", {"path": str(path), "blockers": [error or "installed closeout authority is unavailable"]}
    if closeout.get("machine_complete") is not True or closeout.get("reviewer_missing"):
        return "FAIL", {"path": str(path), "blockers": ["installed task is not machine-complete with independent review"]}

    # Receipt fields describe the campaign, but the installed CLI currently
    # exposes no durable authority for these three historical observations.
    # Keep the live lane open until installed APIs can corroborate them.
    return "NOT RUN", {
        "path": str(path),
        "blockers": [
            "installed runtime has no durable blocked-start observation authority",
            "installed runtime has no durable model-mapping removal/restoration history",
            "installed runtime has no durable completed human-gate receipt projection",
        ],
    }


def render_report(mode: str, result: dict[str, Any]) -> str:
    built_status, built_details = built_bundle_report()
    identity = result.get("source_identity", {})
    parity = result.get("source_parity", {})
    live = result.get("live", {})
    browser = result.get("browser", {})
    live_status = live.get("status", "NOT RUN")
    live_detail = live.get("detail", "live gate not invoked")
    offline_status = result["status"] if mode == "offline" else "NOT RUN"
    offline_detail = result.get("detail", "offline lane not invoked") if mode == "offline" else "offline lane not invoked"
    if mode == "offline" and browser.get("status") != "PASS":
        a2_status = "PARTIAL" if offline_status == "PASS" else offline_status
        a2_detail = f"CLI fixture: {offline_detail}; browser: {browser.get('detail', 'not invoked')}"
    elif offline_status == "PASS" and browser.get("status") == "PASS":
        a2_status = "PASS"
        a2_detail = f"CLI fixture and browser passed; {browser.get('detail', '')}"
    else:
        a2_status = browser.get("status", "NOT RUN") if mode == "browser" else "NOT RUN"
        a2_detail = browser.get("detail", "offline and browser lanes not invoked")
    matrix: list[str] = ["| ID | Status | Evidence |", "|---|---|---|"]
    matrix.extend([
        f"| A1 | {'PARTIAL' if offline_status == 'PASS' else offline_status} | Local fixture covers classified wave, standalone task, provenance and human action; unprompted architect output remains unverified. {offline_detail} |",
        f"| A2 | {a2_status} | Public CLI and read-only built-browser route, blockers and independent-review boundary; {a2_detail} |",
        f"| A3 | {live.get('status', 'NOT RUN')} | Installed execute/review trial with matching selected and actual identities, correction and accepted review; {live.get('detail', 'live gate not invoked')} |",
    ])
    lines = [
        "# W-0023 first trial",
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
        "The offline lane runs the focused FLW-T-0041 fixture only. The filtered Go command requires a nonzero PASS match and treats skipped substantive tests as incomplete; the first actionable failure is retained.",
        "",
        f"- Status: **{result.get('status', 'NOT RUN')}** — {result.get('detail', '')}",
        f"- Go environment: `{json.dumps(result.get('environment', {}), sort_keys=True)}`.",
        "",
        "## FLW-T-0041 acceptance map",
        "",
        *matrix,
        "",
        "The current invocation launches no daemon, installed binary, provider, or architect route. Fixture and browser evidence remain separate from the installed trial; a missing installed authority leaves A3 NOT RUN.",
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
        "A3 remains open until the operator supplies an exact installed identity, an accepted human-gate receipt, configured execute/review routes, an independently running daemon, and real attempt/review/correction evidence. The driver does not select a provider or model profile. Missing setup is reported as NOT RUN; observed contradictions remain FAIL.",
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
