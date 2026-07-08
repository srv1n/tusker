# RESULT
```json
{
  "kind": "review",
  "verdict": "request_changes",
  "risk": "high",
  "summary": "The bundle has several real pre-push defects in daemon reconciliation, watchdog behavior, serve mutation handling, and the land lane. I provided a patch for the safest high-value fixes: terminal canonical state now wins during status-ready reconciliation, fresh wrapper heartbeats prevent false first-event stalls, serve command invocation no longer pipe-deadlocks or leaves stdout redirected on panic, and POST mutations reject cross-origin/non-loopback-host requests. The remaining land-lane and lease-claim issues should block the push until fixed or explicitly accepted.",
  "findings": [
    {
      "severity": "high",
      "path": "cmd/tusker/daemon.go",
      "line": 1217,
      "problem": "A status-ready run is classified before checking whether the canonical tracker state is terminal. If the canonical task was closed/done while a runner later exits 0 and its worktree tracker says review/done, line 1235 can score the run as waiting_for_review via completedRunnerReviewRequest instead of retiring it. Because line 1027 skips retirement while the status file exists, the released waiting_for_review row can persist against a terminal canonical task, violating monotone terminal state.",
      "fix": "After reading the runner status and resolving the canonical note, check canonicalStatusRetiresRuntimeRows before worktree-local completion handling; record the attempt outcome and retire the runtime row.",
      "creates_followup_task": false
    },
    {
      "severity": "high",
      "path": "cmd/tusker/daemon.go",
      "line": 3856,
      "problem": "The first-event watchdog ignores LastHeartbeatAt until FirstEventAt is set. A long non-streaming runner command can keep renewing the wrapper lease heartbeat but emit no events; after daemonFirstEventDeadline, line 3860 marks it as never started and interrupts/retries live work.",
      "fix": "Treat fresh wrapper heartbeat as proof the process started even when FirstEventAt is empty; only fail the pre-first-event path when both first event and heartbeat have gone stale.",
      "creates_followup_task": false
    },
    {
      "severity": "high",
      "path": "cmd/tusker/serve_actions.go",
      "line": 80,
      "problem": "serveInvokeCommand redirects global os.Stdout to an os.Pipe and does not drain the pipe while the CLI command runs. Any command writing more than the pipe buffer can deadlock the HTTP handler. If fn panics at line 90, os.Stdout is never restored and file descriptors leak; the top-level panic handler then returns a generic 500 instead of the mutation contract's visible refusal.",
      "fix": "Capture stdout through a temporary file or concurrently drained pipe, restore stdout in a defer, close/remove resources in all paths, and recover command panics into a refused serveActionResult error.",
      "creates_followup_task": false
    },
    {
      "severity": "high",
      "path": "cmd/tusker/serve_command.go",
      "line": 166,
      "problem": "POST mutations are accepted before any Origin/Referer/Host validation. Loopback binding does not prevent browser-driven CSRF against http://127.0.0.1:7420, and DNS rebinding/non-loopback Host headers are not rejected. A malicious page can issue mutation POSTs such as daemon stop, task close, gate waive, or land.",
      "fix": "For unsafe methods, require a loopback Host header and reject Origin/Referer values that are not same-origin before routing to handleAPIMutation.",
      "creates_followup_task": false
    },
    {
      "severity": "high",
      "path": "cmd/tusker/v7_land_cmd.go",
      "line": 269,
      "problem": "land --from accepts any resolvable ref/worktree commit and immediately creates the task branch from it. The auto path fallback at line 320 also matches any detached worktree whose absolute path merely contains the task id. A caller can branchify APP-T-0001 from APP-T-0002's worktree or an unrelated commit, then merge the wrong diff through the serialized lane.",
      "fix": "Validate explicit --from and auto-discovered worktrees against .tusker/workspace.json record_id and, for commit refs, against task-local tracker metadata or an explicit trusted override. Remove the broad strings.Contains absolute-path match or reduce it to exact, delimited workspace-key matches only.",
      "creates_followup_task": false
    },
    {
      "severity": "high",
      "path": "cmd/tusker/v7_land_cmd.go",
      "line": 694,
      "problem": "landV7WaveToMain moves refs/heads/<defaultBranch> with git update-ref even when that branch is checked out in a worktree. The branch ref advances but the checked-out worktree/index remains at the old tree, leaving the operator on a stale dirty-looking main that can accidentally commit or revert landed changes.",
      "fix": "Before update-ref, detect whether the default branch is checked out in any worktree. Either perform the main update through a clean checked-out worktree and reset it safely, or refuse with an actionable error unless the branch is not checked out.",
      "creates_followup_task": false
    },
    {
      "severity": "medium",
      "path": "cmd/tusker/daemon.go",
      "line": 2194,
      "problem": "dispatchRun claims work by mutating an in-memory RunStatus and blindly UpsertRun-ing it, even though RuntimeStore has an atomic ClaimRunLease at runtime_store.go:976. Workspace prep, prompt rendering, SaveAttempt, and the final upsert are not protected by a compare-and-swap against the row read at poll start. A concurrent control action or second daemon/once process can be overwritten by a stale dispatch claim.",
      "fix": "Move dispatch claiming to an atomic store operation with owner/generation/work_revision preconditions before external side effects, and make subsequent writes conditional on the same lease_owner and lease_generation.",
      "creates_followup_task": false
    }
  ]
}
```

# Findings

1. **High — `cmd/tusker/daemon.go:1217`**  
   Terminal canonical state is not checked after a runner status file appears. A terminal task can keep or regain a non-terminal runtime row if the runner exits cleanly and the worktree-local tracker says review/done. The patch adds a terminal retirement guard immediately after exit classification.

2. **High — `cmd/tusker/daemon.go:3856`**  
   The watchdog kills live, silent runners before the first observed event. A wrapper heartbeat updates `LastHeartbeatAt`, but the pre-first-event branch ignores it and trips after `daemonFirstEventDeadline`. The patch treats a fresh heartbeat as live progress before any event arrives.

3. **High — `cmd/tusker/serve_actions.go:80`**  
   `serveInvokeCommand` can deadlock on stdout volume and corrupt process-global stdout on panic. The patch switches capture to a temporary file, restores stdout in a defer, cleans up resources, and turns panics into visible command failures.

4. **High — `cmd/tusker/serve_command.go:166`**  
   Mutation POSTs have no CSRF/DNS-rebinding guard. The patch rejects non-loopback Host headers and cross-origin Origin/Referer values before mutation routing.

5. **High — `cmd/tusker/v7_land_cmd.go:269` and `cmd/tusker/v7_land_cmd.go:320`**  
   Branchification can select the wrong source commit. `--from` is unbound to the task, and auto-discovery accepts a substring match in the absolute worktree path. This is not patched because the correct commit-level validation depends on how much task metadata is guaranteed to be committed.

6. **High — `cmd/tusker/v7_land_cmd.go:694`**  
   Main is advanced by `update-ref` without synchronizing a checked-out main worktree. This can wedge the operator’s checkout after a successful land. This is not patched because the right behavior needs a product decision: refuse when main is checked out, or safely reset only clean checked-out worktrees.

7. **Medium — `cmd/tusker/daemon.go:2194`**  
   Dispatch does not use the existing atomic lease-claim primitive. The current blind `UpsertRun` can overwrite concurrent runtime transitions. This should be fixed with store-level CAS around claim and follow-up updates.

# Architecture notes

The daemon has two authority planes, canonical vault and worktree-local tracker, but terminal canonical state must dominate all runtime reconciliation paths. The original code handled terminal retirement only when no status file was ready, which created an exception exactly at the riskiest moment: a finished attempt racing with human/merge state.

The serve layer still runs CLI commands in-process. The patch narrows the stdout/panic failure mode, but the model remains fragile because CLI code can mutate globals, environment, working directory, process groups, and shared runtime store state inside the HTTP handler.

The land lane performs low-level ref surgery directly against the source repository. The `commit-tree`/`update-ref` approach is powerful, but it needs explicit checked-out-branch handling and source-commit provenance checks before it is safe as the main merge path.

# Missing tests

Missing tests before this patch: status-ready runner reconciliation when the canonical task has already moved to a terminal status; pre-first-event runner silence with a fresh wrapper heartbeat; cross-origin and non-loopback-Host mutation rejection; `serveInvokeCommand` panic recovery; and `serveInvokeCommand` output larger than an OS pipe buffer.

Still missing after this patch: wrong-task `tusker land --from` rejection, auto-discovery rejecting substring-only worktree matches, checked-out-main landing behavior, and atomic dispatch claim races.

I verified the patch applies with `git apply --3way` against the uploaded tree. I could not run `go test ./cmd/tusker` in this environment because the local Go toolchain is `go1.23.2`, while `go.mod` requires Go `1.25.8`; toolchain download failed because internet access is disabled.

# If you change one thing

Make canonical terminal task state win over every status-ready runtime path. That prevents stale attempts from resurrecting terminal work and reduces churn in the daemon loop.

Patch file: [tusker-review-fixes.patch](sandbox:/mnt/data/tusker-review-fixes.patch)

```diff
diff --git a/cmd/tusker/daemon.go b/cmd/tusker/daemon.go
index e519baa..4a4b6af 100644
--- a/cmd/tusker/daemon.go
+++ b/cmd/tusker/daemon.go
@@ -1215,6 +1215,16 @@ func (d *Daemon) reconcileRun(ctx context.Context, project RegisteredProject, wf
 			return run, changed, err
 		}
 		classification := classifyRunnerProcessExit(run, status, note, project.VaultRoot, wfFile.Data.Tracker.ActiveStates)
+		if canonicalStatusRetiresRuntimeRows(wfFile.Data, classification.trackerState) {
+			outcome := classification.outcome
+			if status.ExitCode != 0 {
+				outcome = AttemptOutcomeFailed
+			}
+			run.AttemptOutcome = string(outcome)
+			run.LastError = classification.reason
+			updateRunAttemptFromRun(d.store, run, outcome, status.ExitCode, classification.reason, finished)
+			return d.retireCanonicalRuntimeRun(ctx, project, run, classification.trackerState, "daemon:reconcile", "status-ready")
+		}
 		if status.ExitCode == 0 {
 			if classification.outcome == AttemptOutcomeTurnCapExhausted {
 				reason := classification.reason
@@ -3854,26 +3864,34 @@ func latestRunEventAt(run RunStatus) (time.Time, bool) {
 }
 
 func runStallReason(run RunStatus, wf Workflow, now time.Time) (bool, string) {
+	runner, _, err := runnerForName(run.Runner, wf)
+	heartbeatCapable := err == nil && runner.Capabilities().Heartbeats
+	timeout := heartbeatDeadThresholdForRun(run, wf)
 	if strings.TrimSpace(run.FirstEventAt) == "" {
-		startedAt, ok := parseRunTimestamp(firstNonEmpty(run.ProcessStartedAt, run.StartedAt, run.UpdatedAt))
+		startedAt, startedOK := parseRunTimestamp(firstNonEmpty(run.ProcessStartedAt, run.StartedAt, run.UpdatedAt))
+		if heartbeatCapable {
+			if lastHeartbeatAt, heartbeatOK := parseRunTimestamp(run.LastHeartbeatAt); heartbeatOK {
+				if now.Sub(lastHeartbeatAt) <= timeout {
+					return false, ""
+				}
+				if !startedOK || lastHeartbeatAt.After(startedAt) || lastHeartbeatAt.Equal(startedAt) {
+					return true, fmt.Sprintf("runner heartbeat dead before first event: no heartbeat since %s", lastHeartbeatAt.Format(time.RFC3339))
+				}
+			}
+		}
 		deadline := firstEventDeadlineForRun(run, wf)
-		if ok && now.Sub(startedAt) > deadline {
+		if startedOK && now.Sub(startedAt) > deadline {
 			return true, fmt.Sprintf("runner never started: no first event within %s of spawn", deadline)
 		}
 		return false, ""
 	}
-	runner, _, err := runnerForName(run.Runner, wf)
-	if err != nil || !runner.Capabilities().Heartbeats {
+	if !heartbeatCapable {
 		return false, ""
 	}
 	lastHeartbeatAt, ok := parseRunTimestamp(firstNonEmpty(run.LastHeartbeatAt, run.LastEventAt, run.FirstEventAt))
 	if !ok {
 		return false, ""
 	}
-	timeout := daemonHeartbeatDeadThreshold
-	if wf.Codex.StallTimeoutMS > 0 && RunnerName(run.Runner) == RunnerCodex {
-		timeout = time.Duration(wf.Codex.StallTimeoutMS) * time.Millisecond
-	}
 	if now.Sub(lastHeartbeatAt) <= timeout {
 		return false, ""
 	}
@@ -3896,6 +3914,13 @@ func firstEventDeadlineForRun(run RunStatus, wf Workflow) time.Duration {
 	return daemonFirstEventDeadline
 }
 
+func heartbeatDeadThresholdForRun(run RunStatus, wf Workflow) time.Duration {
+	if wf.Codex.StallTimeoutMS > 0 && RunnerName(run.Runner) == RunnerCodex {
+		return time.Duration(wf.Codex.StallTimeoutMS) * time.Millisecond
+	}
+	return daemonHeartbeatDeadThreshold
+}
+
 func codexExecInFlightCommandTimeout(wf Workflow) time.Duration {
 	if wf.Codex.TurnTimeoutMS > 0 {
 		return time.Duration(wf.Codex.TurnTimeoutMS) * time.Millisecond
diff --git a/cmd/tusker/runner_codex_exec_test.go b/cmd/tusker/runner_codex_exec_test.go
index 2614c44..ee61044 100644
--- a/cmd/tusker/runner_codex_exec_test.go
+++ b/cmd/tusker/runner_codex_exec_test.go
@@ -311,6 +311,35 @@ func TestCodexExecIdleHeartbeatReason(t *testing.T) {
 	}
 }
 
+func TestFirstEventDeadlineToleratesFreshWrapperHeartbeat(t *testing.T) {
+	started := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
+	run := RunStatus{
+		Runner:           string(RunnerCodexExec),
+		StartedAt:        started.Format(time.RFC3339),
+		ProcessStartedAt: started.Format(time.RFC3339),
+		LastHeartbeatAt:  started.Add(10 * time.Minute).Format(time.RFC3339),
+	}
+	stalled, reason := runStallReason(run, defaultWorkflow(), started.Add(10*time.Minute+time.Second))
+	assertEqual(t, false, stalled, "fresh wrapper heartbeat without first event stalled")
+	assertEqual(t, "", reason, "fresh wrapper heartbeat without first event reason")
+}
+
+func TestFirstEventDeadlineReportsDeadWrapperHeartbeat(t *testing.T) {
+	started := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
+	heartbeatAt := started.Add(daemonFirstEventDeadline + time.Minute)
+	run := RunStatus{
+		Runner:           string(RunnerCodexExec),
+		StartedAt:        started.Format(time.RFC3339),
+		ProcessStartedAt: started.Format(time.RFC3339),
+		LastHeartbeatAt:  heartbeatAt.Format(time.RFC3339),
+	}
+	stalled, reason := runStallReason(run, defaultWorkflow(), heartbeatAt.Add(daemonHeartbeatDeadThreshold+time.Second))
+	assertEqual(t, true, stalled, "dead wrapper heartbeat before first event stalled")
+	if !strings.Contains(reason, "runner heartbeat dead before first event") {
+		t.Fatalf("expected dead heartbeat reason, got %q", reason)
+	}
+}
+
 func TestCodexExecCompletionRecordsSucceeded(t *testing.T) {
 	vault := automationTestVault(t)
 	disableReviewerForTest(t, vault)
diff --git a/cmd/tusker/serve_actions.go b/cmd/tusker/serve_actions.go
index b375b6f..ad6b01a 100644
--- a/cmd/tusker/serve_actions.go
+++ b/cmd/tusker/serve_actions.go
@@ -2,6 +2,7 @@ package main
 
 import (
 	"encoding/json"
+	"fmt"
 	"io"
 	"net/http"
 	"os"
@@ -77,25 +78,34 @@ func serveBaseArgs(s *serveServer) Args {
 	return Args{"vault": s.vaultPath, "repo": s.repoRoot}
 }
 
-func serveInvokeCommand(args Args, fn func(Args) error) (string, error) {
+func serveInvokeCommand(args Args, fn func(Args) error) (output string, runErr error) {
 	serveCommandStdoutMu.Lock()
 	defer serveCommandStdoutMu.Unlock()
 
 	previous := os.Stdout
-	reader, writer, err := os.Pipe()
+	tmp, err := os.CreateTemp("", "tusker-serve-stdout-*")
 	if err != nil {
 		return "", err
 	}
-	os.Stdout = writer
-	runErr := fn(args)
-	_ = writer.Close()
-	os.Stdout = previous
-	out, readErr := io.ReadAll(reader)
-	_ = reader.Close()
-	if readErr != nil && runErr == nil {
-		runErr = readErr
-	}
-	return strings.TrimSpace(string(out)), runErr
+	tmpPath := tmp.Name()
+	defer func() {
+		os.Stdout = previous
+		_, _ = tmp.Seek(0, 0)
+		out, readErr := io.ReadAll(tmp)
+		output = strings.TrimSpace(string(out))
+		if readErr != nil && runErr == nil {
+			runErr = readErr
+		}
+		_ = tmp.Close()
+		_ = os.Remove(tmpPath)
+		if recovered := recover(); recovered != nil {
+			runErr = tuskerError(errorHookFailed, fmt.Sprintf("serve command panic: %v", recovered))
+		}
+	}()
+
+	os.Stdout = tmp
+	runErr = fn(args)
+	return output, runErr
 }
 
 func serveCommandResult(command, output string, err error) serveActionResult {
diff --git a/cmd/tusker/serve_command.go b/cmd/tusker/serve_command.go
index 98dfd88..5c48e9b 100644
--- a/cmd/tusker/serve_command.go
+++ b/cmd/tusker/serve_command.go
@@ -10,6 +10,7 @@ import (
 	"mime"
 	"net"
 	"net/http"
+	"net/url"
 	"os"
 	"path/filepath"
 	"sort"
@@ -135,6 +136,64 @@ func serveIsLoopbackHost(host string) bool {
 	return ip != nil && ip.IsLoopback()
 }
 
+func serveMutationOriginRefusal(r *http.Request) string {
+	if !serveRequestHostIsLoopback(r.Host) {
+		return "refused mutation for non-loopback Host header"
+	}
+	for _, header := range []string{"Origin", "Referer"} {
+		raw := strings.TrimSpace(r.Header.Get(header))
+		if raw == "" {
+			continue
+		}
+		if !serveSameOrigin(raw, r.Host) {
+			return "refused cross-origin mutation"
+		}
+	}
+	return ""
+}
+
+func serveRequestHostIsLoopback(hostport string) bool {
+	host := strings.TrimSpace(hostport)
+	if host == "" {
+		return false
+	}
+	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
+		host = parsedHost
+	}
+	return serveIsLoopbackHost(host)
+}
+
+func serveSameOrigin(rawOrigin, requestHost string) bool {
+	u, err := url.Parse(rawOrigin)
+	if err != nil || u == nil || strings.TrimSpace(u.Host) == "" {
+		return false
+	}
+	return serveCanonicalOriginHost(u.Host, u.Scheme) == serveCanonicalOriginHost(requestHost, "http")
+}
+
+func serveCanonicalOriginHost(hostport, scheme string) string {
+	host := strings.TrimSpace(hostport)
+	port := ""
+	normalizedScheme := strings.ToLower(strings.TrimSpace(scheme))
+	if normalizedScheme == "" {
+		normalizedScheme = "http"
+	}
+	if parsedHost, parsedPort, err := net.SplitHostPort(host); err == nil {
+		host = parsedHost
+		port = parsedPort
+	}
+	host = strings.ToLower(strings.Trim(host, "[] "))
+	if port == "" {
+		switch normalizedScheme {
+		case "https":
+			port = "443"
+		default:
+			port = "80"
+		}
+	}
+	return normalizedScheme + "://" + net.JoinHostPort(host, port)
+}
+
 func parseTruthyQuery(value string) bool {
 	switch strings.ToLower(strings.TrimSpace(value)) {
 	case "1", "true", "t", "yes", "y", "on", "all":
@@ -163,6 +222,12 @@ func (s *serveServer) handleAPI(w http.ResponseWriter, r *http.Request) {
 	if path == "" {
 		path = "/"
 	}
+	if r.Method == http.MethodPost {
+		if reason := serveMutationOriginRefusal(r); reason != "" {
+			serveJSON(w, http.StatusForbidden, serveActionResult{OK: false, Refused: true, Reason: reason})
+			return
+		}
+	}
 	if s.handleAPIMutation(w, r, path) {
 		return
 	}
diff --git a/cmd/tusker/serve_command_test.go b/cmd/tusker/serve_command_test.go
index 0763b15..b7bb908 100644
--- a/cmd/tusker/serve_command_test.go
+++ b/cmd/tusker/serve_command_test.go
@@ -24,7 +24,7 @@ func TestServeReadOnlyAndLocalhost(t *testing.T) {
 	}
 
 	server := newServeFixture(t)
-	req := httptest.NewRequest(http.MethodPost, "/api/tasks", nil)
+	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7420/api/tasks", nil)
 	rec := httptest.NewRecorder()
 	server.ServeHTTP(rec, req)
 	assertEqual(t, http.StatusMethodNotAllowed, rec.Code, "mutating API route status")
@@ -317,6 +317,31 @@ func TestServeReadParityEndpointsSerialize(t *testing.T) {
 	assertEqual(t, 2, daemon.MaxActiveRuns, "daemon active limit default")
 }
 
+func TestServeMutationRejectsCrossOrigin(t *testing.T) {
+	server := newServeFixture(t)
+	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7420/api/tasks/APP-T-0001/status", bytes.NewBufferString(`{"status":"active"}`))
+	req.Header.Set("Content-Type", "application/json")
+	req.Header.Set("Origin", "https://attacker.example")
+	rec := httptest.NewRecorder()
+	server.ServeHTTP(rec, req)
+	assertEqual(t, http.StatusForbidden, rec.Code, "cross-origin mutation status")
+	if !strings.Contains(rec.Body.String(), "refused cross-origin mutation") {
+		t.Fatalf("expected cross-origin refusal, got %q", rec.Body.String())
+	}
+}
+
+func TestServeMutationRejectsNonLoopbackHost(t *testing.T) {
+	server := newServeFixture(t)
+	req := httptest.NewRequest(http.MethodPost, "http://evil.example/api/tasks/APP-T-0001/status", bytes.NewBufferString(`{"status":"active"}`))
+	req.Header.Set("Content-Type", "application/json")
+	rec := httptest.NewRecorder()
+	server.ServeHTTP(rec, req)
+	assertEqual(t, http.StatusForbidden, rec.Code, "non-loopback host mutation status")
+	if !strings.Contains(rec.Body.String(), "non-loopback Host") {
+		t.Fatalf("expected host refusal, got %q", rec.Body.String())
+	}
+}
+
 func TestServeMutationEndpointsReturnVisibleRefusals(t *testing.T) {
 	server := newServeFixture(t)
 	cases := []struct {
@@ -552,7 +577,7 @@ func serveDecode(t *testing.T, server *serveServer, path string, out any) {
 
 func servePost(t *testing.T, server *serveServer, path, body string, out any) {
 	t.Helper()
-	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
+	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7420"+path, bytes.NewBufferString(body))
 	req.Header.Set("Content-Type", "application/json")
 	rec := httptest.NewRecorder()
 	server.ServeHTTP(rec, req)
```
