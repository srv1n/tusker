# 09 - ChatGPT Pro Handoff Orchestration

Date: 2026-06-04

Status: handoff note for Tusker implementation

Audience: Tusker runtime, runner, workflow, and skill maintainers

## Summary

The ChatGPT Pro handoff loop should be built into Tusker where the behavior is
Tusker-specific: task transitions, attempts, runtime artifact state, evidence,
dispatch, caps, and closeout. Browser automation remains outside Tusker behind
`rzn-browser` / `chatgpt-handoff`, because Tusker should not know ChatGPT DOM
selectors, download buttons, model menus, or session-cookie quirks.

The immediate goal is to complete the seam from a finished ChatGPT job to a
Tusker-owned Codex apply attempt:

```text
ChatGPT job done
  -> Tusker collects/fetches artifacts through browser transport
  -> Tusker classifies returned files
  -> notes/reviews become review_packet evidence
  -> patches become apply inputs, not evidence
  -> Tusker dispatches Codex through the existing automation daemon
  -> Codex applies/tests in an isolated workspace
  -> Tusker records verification evidence and moves to review/rework/blocked
```

Do not build a second Tusker-flavored control plane in `chatgpt-handoff`.
`chatgpt-handoff` is transport. Tusker is the ledger and orchestrator.

## Current Facts

| Area | Current state |
|---|---|
| Browser transport | `rzn-browser` can post to ChatGPT Project and `chatgpt_read.json` can capture assistant sandbox downloads into one in-page zip. This avoids Chrome's multi-download gate. |
| Pack transport | `chatgpt-handoff fetch <job-or-chat-id>` can land fetched files under an `architect/<job_id>/` directory today. |
| Live proof case | Kurpod produced a real ChatGPT Pro result with patch, notes, and bundle attachments. Those files were fetched successfully. |
| Tusker task | Kurpod task `KCR-T-0012` was created under epic `KCR` for "Fix iOS Open Vault crash + streaming encryption perf". It is intentionally backlog until acceptance and verification are added. |
| Tusker runtime | This checkout contains the V7 automation command surface (`go run ./cmd/tusker automation --help`), but the installed `tusker` on PATH may lag and not expose `automation`. Update/install before relying on PATH. |
| Codex dispatch | Tusker already has a guarded Codex app-server runner path: workspace prep, cwd invariant, runtime store, attempts, events, logs, review packet, and review/rework handling. Use it. |

## Decisions

### 1. Tusker Owns Tusker Semantics

Tusker should own:

- task lookup and lifecycle transitions;
- attempt/run/session/runtime state;
- external job references;
- artifact classification;
- apply input metadata;
- evidence creation;
- dispatch and retry policy;
- cycle counters and hard caps;
- notifications and closeout.

Do not hand-edit protected task fields such as `status`, `readiness`,
`next_owner`, `next_action`, `agent_action`, `accepted_by`, `closed_at`, or
closeout fields. Use Tusker control commands.

### 2. Browser Transport Stays Outside Tusker

Tusker should call browser transport commands, not implement them.

Allowed external surface:

```sh
chatgpt-handoff fetch <job-id-or-chat-id> --json
chatgpt-handoff tusker-status --job <job-id> --json
chatgpt-handoff tusker-collect --job <job-id> --json
```

If a new transport flag is needed, keep it generic, for example `--out-dir` or
`--no-scroll`. Do not add task lifecycle, evidence, readiness, or dispatch logic
to `chatgpt-handoff`.

### 3. Patch Files Are Apply Inputs, Not Evidence

A returned patch is not Tusker evidence. There is no deliverable-style evidence
kind for "patch" in the evidence enum, and treating raw patches as proof is the
wrong model anyway.

Use this mapping:

| Returned artifact | Tusker handling |
|---|---|
| `*.patch`, `*.diff` | Apply input. Store in runtime/apply-input metadata and make available to the Codex workspace. Not evidence. |
| `*.md`, notes, review write-up | `review_packet` evidence when it summarizes findings or architecture/review output. |
| apply log, focused test log | `verification_summary` or `log_excerpt` after the apply attempt runs. |
| zip/bundle | Runtime artifact unless explicitly promoted for a proof requirement. |
| full raw transcript | Runtime/debug artifact. Do not promote by default. |

### 4. No Direct Codex Shell-Out

Do not run `codex app-server` directly from the handoff package or a loose
orchestrator script. That bypasses Tusker's workspace, runtime store, evidence,
review packet, retry, and inspection machinery.

The apply path should go through:

```sh
go run ./cmd/tusker automation explain <TASK-ID> --json
go run ./cmd/tusker automation dispatch <TASK-ID> --json
go run ./cmd/tusker runs inspect <TASK-ID> --json
```

After the installed binary is refreshed, the same commands should work as
`tusker automation ...`.

### 5. No Per-Job Brief Injection

The apply behavior should not depend on stuffing a custom brief into each task.
Use:

- a static `WORKFLOW.md` stanza for external apply inputs;
- a stable artifact convention;
- runtime metadata passed into prompt rendering where needed.

The LLM should read the task contract, the workflow runbook, and the apply input
directory. It should not need hidden per-job prose to know what to do.

## Target Architecture

```text
Human / external request
  |
  v
Tusker task
  - acceptance contract
  - verification contract
  - proof mode
  - durable lifecycle
  |
  v
ChatGPT browser runner attempt
  - start/status via chatgpt-handoff
  - long-running browser job
  - cloud_task_id stored in runtime
  |
  v
Tusker collect/resolve step
  - calls browser transport fetch
  - classifies returned files
  - records review evidence
  - stores patch apply inputs
  - emits next action
  |
  v
Tusker automation dispatch
  - existing Codex app-server runner
  - isolated workspace/worktree
  - apply/check/test
  |
  v
Tusker review/rework/blocked/done flow
```

## Required Tusker Work

### Phase 0 - Reconcile The Command Surface

This checkout has automation code that the installed `tusker` binary may not
expose. Before validating this feature through PATH:

```sh
go run ./cmd/tusker automation --help
tusker automation --help
```

If `go run` works and `tusker` does not, refresh the installed binary.

Also reconcile V7 automation trigger states. V7 runnable agent work is
`status: ready|rework`, `readiness: ready`, and `next_owner: agent|agent:*`.
Legacy `active` is not V7 lifecycle truth.

Recommended `tusker.yaml` automation overlay shape:

```yaml
automation:
  trigger_states: [ready, rework]
  default_runner: codex_app_server
  enabled_runners: [codex_app_server, codex_exec, codex_cloud, claude-code]
  workspace:
    root: "workspaces"
    strategy: worktree
```

Use the exact schema names currently accepted by `cmd/tusker/workflow.go`; the
point is that automation should not depend on durable `active` for V7 tasks.

### 2a - Tusker-Native ChatGPT Artifact Resolution

Build this in Tusker, not in `chatgpt-handoff`.

Recommended command shape:

```sh
tusker automation collect-external <TASK-ID> \
  --runner chatgpt-browser \
  --job <job-id> \
  --json
```

The name can change. The contract should not.

Input:

- a Tusker task id;
- a runner name or attempt/run selector;
- a ChatGPT job id, or the latest runtime `cloud_task_id` for the task;
- repo/vault/project context from Tusker discovery.

Behavior:

1. Load the task capsule and runtime run/attempt for the job.
2. Invoke configured browser transport, for example:

   ```sh
   chatgpt-handoff fetch <job-id> --json
   ```

3. Land or normalize files under a task-stable directory:

   ```text
   architect/<TASK-ID>/
   ```

   Current transport may land `architect/<job_id>/`; Tusker can move/copy into
   the task-stable directory. If transport grows a generic `--out-dir`, Tusker
   may pass it.

4. Classify artifacts:

   - patch/apply input: `*.patch`, `*.diff`;
   - review/notes: `*.md`, `*.txt` if content is review/architecture notes;
   - bundle/runtime: `*.zip`, unknown binary files;
   - failure: no files, unreadable files, ambiguous artifact set.

5. Record review notes as Tusker evidence:

   ```sh
   tusker evidence add <TASK-ID> \
     --kind review_packet \
     --covers A1,A2 \
     --summary "ChatGPT Pro handoff notes collected for <job-id>." \
     --path <notes-path>
   ```

   Covers should be selected by the caller or derived conservatively from task
   acceptance. Do not spray `TASK` if the acceptance ids are known.

6. Store patch files as runtime apply inputs, not evidence.

7. Return structured JSON:

   ```json
   {
     "schema": "tusker.external_collect/v1",
     "task_id": "KCR-T-0012",
     "runner": "chatgpt-browser",
     "job_id": "cgpt_...",
     "artifact_dir": "architect/KCR-T-0012",
     "patches": ["architect/KCR-T-0012/fix.patch"],
     "review_packets": ["architect/KCR-T-0012/notes.md"],
     "bundles": ["architect/KCR-T-0012/context.zip"],
     "evidence_added": ["KCR-E-...."],
     "next_action": "apply_patch",
     "dispatchable": false,
     "blockers": ["task is backlog", "acceptance missing proof mapping"]
   }
   ```

Idempotence requirements:

- running collection twice for the same job must not duplicate evidence;
- artifact identity should use path plus content hash where practical;
- if `architect/<TASK-ID>/` already has files, do not overwrite unrelated human
  files without a clear collision rule;
- classification failures should produce `next_action: escalate`, not mutate
  task lifecycle fields directly.

Failure policy:

| Failure | Outcome |
|---|---|
| Browser auth/captcha/rate limit | external/human gate or blocked runtime state |
| no artifacts fetched | blocked runtime state with job id and chat id |
| more than one patch | escalate unless task explicitly allows multi-patch apply |
| patch file unreadable | blocked runtime state |
| review packet evidence add fails | fail the collect step; do not proceed to apply |

### 2b - Workflow-Driven Codex Apply Attempt

After 2a produces exactly one apply input and the task is groomed to runnable
state, dispatch Codex through Tusker automation.

Prerequisites for a V7 task:

- `status: ready` or `status: rework`;
- `readiness: ready`;
- `next_owner: agent` or `agent:<runner>`;
- no open gates or dependency blockers;
- acceptance items have proof mapping;
- `## Verification` exists and contains exact commands or manual proof;
- `proof_mode` and `proof_required` are coherent.

Use:

```sh
go run ./cmd/tusker automation explain KCR-T-0012 --json
go run ./cmd/tusker automation dispatch KCR-T-0012 --json
go run ./cmd/tusker runs inspect KCR-T-0012 --json
```

Do not use raw `codex app-server`.

#### Apply Input Visibility

Tusker's workspace manager prepares an isolated worktree. Untracked files in the
source repo are not automatically present in that worktree. Therefore 2b must
make apply inputs visible to the runner by one of these explicit mechanisms:

1. copy `architect/<TASK-ID>/` into the prepared workspace before prompt render;
2. expose runtime artifact paths in the rendered prompt and allow read access to
   those absolute paths;
3. require the artifact directory to be tracked before dispatch.

Preferred: copy the task artifact directory into the prepared workspace and make
the rendered prompt point at the workspace-local path. This keeps the runner's
working set honest and avoids depending on source-root side effects.

#### Static WORKFLOW.md Stanza

Add a stable workflow stanza, not per-job injected prose:

```md
## External Apply Inputs

Some tasks may have external apply inputs collected by Tusker under
`architect/{{ note.id }}/` or a workspace-local mirror of that directory.

When that directory contains exactly one `*.patch` or `*.diff` file:

1. inspect the task acceptance and verification contract first;
2. run `git apply --check --3way <patch>`;
3. apply with `git apply --3way <patch>` only after the check passes;
4. resolve conflicts only when the resolution is mechanical and clearly within
   the task contract;
5. run the task verification commands;
6. record compact verification evidence;
7. use `tusker finish <TASK-ID> --request-review` when machine proof is complete;
8. create a concrete gate or move to rework/blocked when proof cannot be
   completed.

If there are zero patches, multiple patches, a patch outside scope, or an
ambiguous conflict, stop and report the blocker through Tusker. Do not invent or
silently repair patches.
```

#### Expected Apply Evidence

After Codex applies and tests, evidence should be based on what proved
acceptance, not on the raw patch.

Examples:

```sh
tusker evidence add KCR-T-0012 \
  --kind verification_summary \
  --covers A1,A2 \
  --summary "Applied external patch in Tusker workspace; focused iOS crash and streaming checks passed." \
  --path <apply-summary-or-test-log>
```

Use `log_excerpt` only for concise failure excerpts. Do not attach full raw
terminal transcripts by default.

## Runner Config Direction

Keep the current `codex_cloud` shim for ChatGPT until the loop is proven. The
first-class `chatgpt_browser` runner is an end-state, not the next step.

Bootstrap runner example:

```yaml
runners:
  chatgpt-browser:
    kind: codex_cloud
    environment_id: chatgpt-browser
    apply_mode: manual
    pr_mode: none
    command: >
      chatgpt-handoff tusker-start --kind dev --json
    status_command: >
      chatgpt-handoff tusker-status --job {{cloud_task_id}} --json
    collect_command: >
      chatgpt-handoff tusker-collect --job {{cloud_task_id}} --json
```

Important: `apply_mode: manual` here means "do not let the external runner apply
anything." It must not mean "wait for the human every time." Tusker should treat
the returned patch as an apply input and route to a Tusker-owned Codex apply
attempt when policy allows.

If the current `codex_cloud` reconcile path maps `manual + apply_ref` directly to
`waiting_for_human`, adjust the ChatGPT/Tusker integration so completed ChatGPT
jobs can enter the Tusker collect/resolve step instead of hard-stopping at human
wait.

## Cycle Accounting And Caps

Once 2a and 2b work manually, add hard caps in Tusker runtime state.

Recommended defaults:

| Counter | Default |
|---|---:|
| ChatGPT review/dev cycles per task | 3 |
| Patch repair continuations | 2 |
| Total external threads per task | 5 |
| Wall clock timeout | 8 hours |

The LLM can recommend `continue`, `review-next`, `close`, or `escalate`, but
Tusker enforces the cap. Do not rely on prompt text for this.

Suggested next-action enum:

```text
record_research_artifact
apply_patch
request_review_next
continue_thread_with_failure
close_task
escalate_human
```

Each action should have structured inputs and a reason. Tusker should reject
actions that exceed caps or lack required artifact/proof state.

## Grooming KCR-T-0012

Before dispatch, `KCR-T-0012` needs a real acceptance and verification contract.

Suggested acceptance:

| ID | Acceptance | Proof |
|---|---|---|
| A1 | iOS Open Vault no longer crashes when invoking native file picking. | Focused iOS/manual or simulator proof that the picker path no longer calls blocking UI-thread code. |
| A2 | Vault streaming encryption/decryption uses bounded memory for large files. | Focused test or instrumentation proving streaming behavior and no full-file buffering on the hot path. |
| A3 | Existing vault open/encryption flows still pass relevant regression checks. | Existing focused test command or documented manual smoke. |

Suggested verification section:

```md
## Verification

- A1: <exact iOS simulator/device command or manual proof packet path>
- A2: <exact focused streaming test command>
- A3: <exact regression test command>
```

Then move through Tusker commands, not frontmatter edits:

```sh
tusker status KCR-T-0012 ready --reason "Acceptance and verification are defined."
go run ./cmd/tusker automation explain KCR-T-0012 --json
```

If the installed binary has been updated, replace `go run ./cmd/tusker` with
`tusker`.

## Build Order

| Step | Owner | Deliverable | Proof |
|---|---|---|---|
| 0 | Tusker | Installed/source command surface reconciled; V7 automation trigger states configured. | `tusker automation --help` and `automation explain` work from PATH. |
| 2a | Tusker + transport callout | Tusker command collects ChatGPT artifacts, normalizes to `architect/<TASK-ID>/`, records review evidence, stores patch apply inputs, emits next-action JSON. | Offline test with fake `chatgpt-handoff fetch`; idempotence test; classification test. |
| 2a guard | Pack/browser tests | Fetch path param contract covers `attachments_scroll`; fetch defaults are documented and tested. | Offline param test proves only declared read params are emitted. |
| Groom | Product/Tusker | `KCR-T-0012` has acceptance/proof/verification and is runnable. | `automation explain KCR-T-0012 --json` shows dispatchable or only expected external blockers. |
| 2b | Tusker | Workflow stanza plus workspace artifact visibility; Codex apply attempt runs via automation dispatch. | `automation dispatch`; `runs inspect`; verification evidence; task reaches review/rework/blocked honestly. |
| Later | Tusker | Auto-advance, caps, notify/closeout. | Daemon e2e over at least one ChatGPT cycle and one Codex apply cycle. |
| Last | Tusker | First-class `chatgpt_browser` runner. | Native runner tests replacing shim fixtures. |

## Tests To Add

### Tusker Unit/Integration

- fake transport collect returns one patch and one notes file;
- notes file creates one `review_packet` evidence record;
- patch file is stored as apply input and not evidence;
- repeated collect is idempotent;
- multiple patches produces `next_action: escalate`;
- no artifacts produces blocked/escalate result;
- workspace prepare copies apply inputs into the runner workspace or prompt
  rendering exposes the approved path;
- `automation explain` shows the external apply input and selected runner;
- `automation dispatch` rejects tasks without acceptance proof mapping;
- `manual apply_ref` from ChatGPT runner does not hard-stop as human-wait when
  Tusker policy says auto-collect/apply is allowed.

### Pack/Browser Guard Tests

- `chatgpt-handoff fetch` emits only declared `chatgpt_read` params, including
  `attachments_scroll`;
- fetch result JSON includes `artifact_dir` or equivalent path data;
- fetch can be faked without browser access for Tusker tests;
- transport failures are machine-readable and do not require parsing logs.

## Open Questions

1. Command naming: use `automation collect-external`, `runs collect`, or another
   operator name. The contract matters more than the label.
2. Runtime model: whether apply inputs live in a new runtime table, in run
   artifacts JSON, or as structured attempt artifacts. Avoid protected task
   frontmatter.
3. Artifact directory ownership: whether `architect/<TASK-ID>/` is committed,
   ignored-but-visible, or copied into runtime state. The runner workspace must
   see it deterministically.
4. Notification mechanism: after cap/converge/fail, use existing closeout/gate
   paths first. Do not invent a separate notification ledger unless Tusker lacks
   one.
5. First-class runner timing: defer until the shim has proven the full collect
   and apply loop.

## Non-Goals For The Next Slice

- Do not implement ChatGPT DOM handling in Tusker.
- Do not create a Tusker lifecycle command inside `chatgpt-handoff`.
- Do not directly spawn `codex app-server`.
- Do not auto-close tasks.
- Do not silently repair malformed patches.
- Do not attach raw patches as evidence.
- Do not add Claude/Gemini parity before the Codex path works.
- Do not build the first-class `chatgpt_browser` runner before 2a/2b are proven.

## Hand-Off Recommendation

Start with 2a in Tusker.

The smallest useful PR is:

1. add a Tusker command that calls fakeable browser transport and classifies
   artifacts;
2. store patch files as apply inputs outside task frontmatter;
3. record notes/reviews as `review_packet` evidence;
4. return stable next-action JSON;
5. add tests for one-patch, notes-only, multi-patch, no-artifact, and idempotent
   collection.

After that lands, wire 2b through the existing automation dispatch path and prove
the Kurpod patch with `KCR-T-0012`.
