# Tusker — GPT-5.5 Pro Extended code review

## What this is
Tusker is a repo-local, markdown-native task tracker + agent-orchestration harness. One Go binary (`cmd/tusker`, `package main`) is the CLI, a resident **daemon** that dispatches coding-agent runners (Codex `codex exec`, one process per attempt) into git worktrees to work tracked tasks, and a local web UI (`internal/serve`, a React/TanStack SPA the daemon serves). The attached zip is the full source at the current commit (generated UI bundles and `node_modules` are excluded).

A self-sustaining agent loop was just built over several waves and is **pre-push** (≈125 local commits). This review gates the push. Be adversarial: try to break the loop, the merge lane, and the new HTTP surface. Skip style/lint nits — I want real defects.

## Review targets, ranked

### 1. Daemon runtime correctness — highest priority
Files: `cmd/tusker/daemon.go`, `run_runtime_commands.go`, `runner.go`, `runner_codex_live.go`, `sentinel.go`, `runtime_store.go`, `daemon_guard.go`.
The dispatch → run → classify → retry/park loop. Recent, load-bearing changes:
- **Completion classifier / write-back.** A runner that finishes writes its review-flip to a **worktree-local** tracker copy. The daemon must treat that worktree state as the terminal completion signal (`waiting_for_review`) at clean exit, and must NOT re-dispatch or classify it `early_exit`. Design rule: the **canonical vault** is authority for *dispatch eligibility*; the **worktree tracker** is authority for *this-attempt completion*. Hunt for: races between the completion read and canonical state, cases where genuinely-finished work still churns to the park guard, and the inverse — a failed/killed runner falsely scored complete.
- **Capacity & invariants.** Active-run cap counts only running/claimed leases (queued rows must not self-block). An "invariant circuit" opens on stale held leases and freezes dispatch. A heartbeat watchdog must tolerate in-flight silence (long non-streaming commands emit no events) yet still catch real hangs. `close` retires runtime rows; terminal task state must be monotone under merge/reconcile. Hunt for: concurrency bugs, TOCTOU, lease/lock races, leaked goroutines/handles, wrong or non-idempotent state transitions, and any way the circuit either fails to open on a real stuck lease or opens spuriously and deadlocks the fleet.

### 2. `tusker land` merge lane
File: `cmd/tusker/v7_land_cmd.go`.
Auto-branchifies a detached completed worktree and merges it to `main` through a serialized integration branch; a batch where nothing lands must exit non-zero **before** applying any rework transitions. Hunt for: unsafe/irreversible git operations, partial-failure states that lose work or leave the tree wedged, incorrect conflict handling, and branchification picking the wrong commit/worktree.

### 3. Serve control surface (new, least-reviewed, security-sensitive)
Files: `cmd/tusker/serve_actions.go`, `serve_command.go`, `serve_runs.go`, `serve_types.go`.
New HTTP `POST` endpoints invoke the **same `*V7Cmd` functions the CLI uses**, in-process, via `serveInvokeCommand(args, cmd)`. Mutations exposed: task status / close / land, wave land, gate satisfy|waive|obsolete, evidence add, feedback add, daemon start|stop|resume|limits. It is unauthenticated by design (localhost, single operator). Hunt for: argument-map misuse or injection into the command layer, path traversal, an endpoint able to wedge or crash the daemon, missing guards (canonical-state, proof/risk gating on close), CSRF/SSRF exposure for a local web server, and whether the "every mutation returns a visible success or refusal, never silent" contract actually holds on every path (including error and panic paths).

## What I want back
1. **A severity-ranked review** (Critical / High / Medium / Low) of real defects: correctness, concurrency, security, data-loss, resource leaks — with file:line and a concrete failure scenario for each.
2. **A single unified diff patch** (must apply with `git apply --3way` against the attached tree) implementing the concrete high-value fixes. Anything too risky to patch blind: describe the fix in the notes instead.

Model: GPT-5.5, Pro Extended effort. Depth over breadth — take the time.
