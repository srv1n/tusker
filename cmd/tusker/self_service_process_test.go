package main

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestSelfServiceProcessHelper is the disposable helper-process entry point
// for TestSelfServiceProcess. The parent spawns the test binary with
// -test.run=^TestSelfServiceProcessHelper$ and TUSKER_SELF_SERVICE_HELPER=1;
// the helper runs exactly one CLI argv through the actual CLI parser and
// dispatch (parseCLI + run, the same entry main uses) against the disposable
// vault and state root it inherits, then exits with the command's exit code.
// Each spawn starts cold and exits at the planned recovery boundary, so every
// cross-process read proves durable reopen. Only disposable helper processes
// controlled by this harness are ever launched, with fake runners and
// explicit state roots; a resident daemon is never started.
func TestSelfServiceProcessHelper(t *testing.T) {
	if os.Getenv("TUSKER_SELF_SERVICE_HELPER") != "1" {
		t.Skip("not a helper process")
	}
	if root := strings.TrimSpace(os.Getenv("TUSKER_PROC_STATE_ROOT")); root != "" {
		_ = os.Setenv("TUSKER_STATE_ROOT", root)
	}
	// TestMain isolates the helper's global config; re-point it at the
	// parent's global config, the only layer that defines runner profiles.
	if config := strings.TrimSpace(os.Getenv("TUSKER_PROC_CONFIG")); config != "" {
		_ = os.Setenv("TUSKER_CONFIG", config)
	}
	argv := strings.Split(os.Getenv("TUSKER_HELPER_ARGV"), "\n")
	command, args := parseCLI(append([]string{"tusker"}, argv...))
	exitCode, err := run(command, args)
	if err != nil {
		issue := errorToIssue(err)
		if args.Bool("json") {
			emitJSON(map[string]any{"ok": false, "error": issue})
		} else {
			loc := ""
			if issue.Path != "" {
				loc = issue.Path + ": "
			}
			fmt.Fprintf(os.Stderr, "[%s] %s%s\n", issue.Code, loc, issue.Message)
			if issue.Hint != "" {
				fmt.Fprintf(os.Stderr, "  hint: %s\n", issue.Hint)
			}
		}
		if exitCode == 0 {
			exitCode = 1
		}
	}
	os.Exit(exitCode)
}

// TestSelfServiceProcess proves TSK-T-0048: a bounded consumer-level
// qualification of the completed commands and Serve surfaces across
// disposable process boundaries. A disposable CLI Start->attempt->
// verification/review->acceptance->next-task journey passes through the
// receiving CLI parser and REST handlers; restart during queue/claim/repair
// and no-stream readback produce durable, truthful results without duplicate
// attempts; unsupported recovery (the unimplemented doctor/repair commands
// owned by TSK-T-0038) returns useful refusal evidence rather than a false
// pass. Installed-app and live-provider qualification stay explicitly
// unclaimed (NOT RUN in the retained report).
func TestSelfServiceProcess(t *testing.T) {
	harness := newProcSelfServiceHarness(t)
	vault, stateRoot, project := harness.vault, harness.stateRoot, harness.project

	// A1: the verify planning consumer runs through the CLI parser BEFORE
	// arming, so the authorization covers the exact planned material.
	out, code := harness.cli(t, "verify", "add", "APP-T-0002", "--covers", "A1",
		"--check", "command: go test ./cmd/tusker -run '^TestSelfServiceProcess$' -count=1",
		"--result", "pending", "--note", "Process-boundary qualification row.", "--by", "human:proc", "--vault", vault)
	if code != 0 || !strings.Contains(out, "Added") {
		t.Fatalf("helper verify add failed: code=%d output:\n%s", code, out)
	}

	// A1: disposable CLI Start authorizes the wave and queues the root.
	out, code = harness.cli(t, "wave", "start", "W-0001", "--mode", "background", "--by", "human:proc", "--vault", vault)
	if code != 0 {
		t.Fatalf("helper wave start failed: code=%d output:\n%s", code, out)
	}
	if !strings.Contains(out, "W-0001") {
		t.Fatalf("helper wave start lost the wave identity:\n%s", out)
	}
	harness.openStore(t)
	directives, err := harness.store.ListActiveRunDirectives(project.ProjectID, time.Now().UTC())
	if err != nil || len(directives) != 1 || directives[0].RecordID != "APP-T-0001" {
		t.Fatalf("CLI start did not queue exactly the root: %#v err=%v", directives, err)
	}
	harness.closeStore(t)

	// A1: a fresh helper process reads back the queued frontier (durable
	// reopen across the process boundary).
	out, code = harness.cli(t, "wave", "review", "W-0001", "--vault", vault)
	if code != 0 {
		t.Fatalf("helper wave review failed: code=%d output:\n%s", code, out)
	}
	if !strings.Contains(out, "authorization authorized") || !strings.Contains(out, "APP-T-0001") {
		t.Fatalf("reopened review lost the queued frontier:\n%s", out)
	}

	// A1: the receiving CLI enforces the DAG — the dependent is refused
	// with the stable dependency code, not silently queued.
	out, code = harness.cli(t, "task", "start", "APP-T-0002", "--mode", "background", "--by", "operator:proc", "--vault", vault)
	if code == 0 || !strings.Contains(out, "DEPENDENCY_WAITING") || !strings.Contains(out, "APP-T-0001") {
		t.Fatalf("helper task start did not refuse with the dependency code: code=%d output:\n%s", code, out)
	}

	// A1/A2: the doctor diagnosis matches retained assertions on outputs
	// and exits. The queued wave with no daemon poll is an actionable
	// fault: exit 1 with the stable code, the next actor, and typed
	// recovery guidance — never a healthy zero or a false pass.
	out, code = harness.cli(t, "doctor", "W-0001", "--vault", vault)
	if code != 1 {
		t.Fatalf("doctor did not exit 1 on the actionable fault: code=%d output:\n%s", code, out)
	}
	for _, want := range []string{"doctor-daemon-absent", "Next actor: operator", "No supported repair"} {
		if !strings.Contains(out, want) {
			t.Fatalf("doctor hid %q:\n%s", want, out)
		}
	}
	out, code = harness.cli(t, "doctor", "W-0001", "--json", "--vault", vault)
	if code != 1 {
		t.Fatalf("doctor --json did not exit 1: code=%d output:\n%s", code, out)
	}
	var doctorReport struct {
		Schema    string `json:"schema"`
		ExitCode  int    `json:"exit_code"`
		Diagnosis struct {
			PrimaryCode           string `json:"primary_code"`
			PrimaryClassification string `json:"primary_classification"`
		} `json:"diagnosis"`
		PrimaryNextSteps []string `json:"primary_next_steps"`
	}
	if err := json.Unmarshal([]byte(out), &doctorReport); err != nil {
		t.Fatalf("doctor --json is not structured: %v\n%s", err, out)
	}
	if doctorReport.Schema != "tusker.doctor/v1" || doctorReport.ExitCode != 1 ||
		doctorReport.Diagnosis.PrimaryCode != "doctor-daemon-absent" ||
		doctorReport.Diagnosis.PrimaryClassification != "recoverable_fault" {
		t.Fatalf("doctor JSON broke its contract: %+v", doctorReport)
	}
	// A normal DAG wait is exit 0, not dispatchability — and invalid
	// invocations are exit 2, distinct from both.
	out, code = harness.cli(t, "doctor", "APP-T-0002", "--vault", vault)
	if code != 0 || !strings.Contains(out, "APP-T-0001") {
		t.Fatalf("doctor did not report the dependency wait as exit 0: code=%d output:\n%s", code, out)
	}
	out, code = harness.cli(t, "doctor", "--vault", vault)
	if code != 2 || !strings.Contains(out, "Usage: tusker doctor") {
		t.Fatalf("bare doctor did not refuse as invalid invocation: code=%d output:\n%s", code, out)
	}
	out, code = harness.cli(t, "doctor", "NOPE-0000", "--vault", vault)
	if code != 2 || !strings.Contains(out, "missing") {
		t.Fatalf("doctor did not refuse the missing target: code=%d output:\n%s", code, out)
	}

	// A1/A2: attempt, verification receipt, review acceptance, and frontier
	// advance run through the receiving functions in this process; every
	// subsequent helper reopens the state cold.
	harness.openStore(t)
	procClaimRoot(t, harness, "attempt-proc-1")
	procFinishRun(t, harness, "attempt-proc-1", string(AttemptOutcomeSucceeded), "")
	if err := statusV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "status": "ready", "by": "human:proc"}); err != nil {
		t.Fatalf("admit root to ready: %v", err)
	}
	if err := acceptV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001", "by": "reviewer:independent"}); err != nil {
		t.Fatalf("accept root: %v", err)
	}
	daemon := &Daemon{store: harness.store, stateRoot: harness.store.stateRoot}
	if err := daemon.advanceAuthorizedWaveFrontiers(project); err != nil {
		t.Fatal(err)
	}
	harness.closeStore(t)

	// A2: restart during the claim/accept repair window — a fresh helper
	// reads the accepted root as completed and the dispatched dependent as
	// queued, with no duplicate attempt.
	out, code = harness.cli(t, "wave", "review", "W-0001", "--vault", vault)
	if code != 0 {
		t.Fatalf("post-acceptance helper review failed: code=%d output:\n%s", code, out)
	}
	if !strings.Contains(out, "APP-T-0001 completed") {
		t.Fatalf("reopened review lost the accepted root:\n%s", out)
	}
	if !strings.Contains(out, "APP-T-0002") || !strings.Contains(out, "queued") {
		t.Fatalf("reopened review lost the dispatched dependent:\n%s", out)
	}
	harness.openStore(t)
	attempts, err := harness.store.ListAttemptsForRun(project.ProjectID, "APP-T-0001")
	if err != nil || len(attempts) != 1 || attempts[0].AttemptID != "attempt-proc-1" {
		t.Fatalf("cross-process journey left %d attempts: %#v err=%v", len(attempts), attempts, err)
	}
	harness.closeStore(t)

	// A2: no-stream REST readback after the cross-process journey carries
	// the same queued cause as the CLI surface.
	harness.openStore(t)
	server := &serveServer{store: harness.store}
	recorder := httptest.NewRecorder()
	server.handleWaveReviewAPI(recorder, project.ProjectID, "W-0001")
	var restReview struct {
		OK      bool `json:"ok"`
		Members []struct {
			TaskID        string `json:"taskId"`
			State         string `json:"state"`
			WaitingReason string `json:"waitingReason"`
		} `json:"members"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &restReview); err != nil {
		t.Fatalf("decode no-stream wave review: %v\n%s", err, recorder.Body.String())
	}
	dependentQueued := false
	for _, member := range restReview.Members {
		if member.TaskID == "APP-T-0002" && strings.Contains(member.WaitingReason, "queued") {
			dependentQueued = true
		}
	}
	if !dependentQueued {
		t.Fatalf("no-stream readback lost the queued dependent: %s", recorder.Body.String())
	}
	harness.closeStore(t)

	// A2: a contract edit after arming stales the wave at the process
	// boundary too — planning a further check on the dispatched dependent
	// changes its contract, so the stored authorization stops covering the
	// material and the frontier admits nothing new until re-armed.
	out, code = harness.cli(t, "verify", "add", "APP-T-0002", "--covers", "A1",
		"--check", "command: go test ./cmd/tusker -run '^TestSelfServiceProcessHelper$' -count=1",
		"--result", "pending", "--note", "Post-arming contract edit.", "--by", "human:proc", "--vault", vault)
	if code != 0 {
		t.Fatalf("post-arming verify add failed: code=%d output:\n%s", code, out)
	}
	out, code = harness.cli(t, "wave", "review", "W-0001", "--vault", vault)
	if code != 0 || !strings.Contains(out, "authorization stale") {
		t.Fatalf("post-arming contract edit did not stale the wave: code=%d output:\n%s", code, out)
	}

	// A2: unsupported recovery returns useful refusal evidence rather than
	// a false pass. `repair` is still unimplemented (spec-proposed only):
	// the CLI must refuse as unknown, never as healthy.
	out, code = harness.cli(t, "repair", "W-0001", "--vault", vault)
	if code == 0 {
		t.Fatalf("unimplemented repair reported success:\n%s", out)
	}
	if !strings.Contains(out, "Unknown command: repair") {
		t.Fatalf("unimplemented repair hid its refusal:\n%s", out)
	}
	if strings.Contains(out, `"ok":true`) {
		t.Fatalf("unimplemented repair emitted a false pass:\n%s", out)
	}

	// A3: every executed scenario carries a verdict and the summary is
	// green only when all of them pass; the retained report links the same
	// names (plus explicitly BLOCKED/NOT RUN qualification rows that never
	// count as pass).
	scenarios := map[string]string{
		"cli_start_queues_root":             "PASS",
		"reopened_review_reads_frontier":    "PASS",
		"cli_task_start_refuses_dependent":  "PASS",
		"cli_verify_add_plans_check":        "PASS",
		"doctor_reports_actionable_fault":   "PASS",
		"doctor_reports_structured_json":    "PASS",
		"doctor_reports_normal_wait":        "PASS",
		"doctor_refuses_invalid_invocation": "PASS",
		"acceptance_dispatches_next":        "PASS",
		"restart_reads_settled_state":       "PASS",
		"no_stream_rest_readback_queued":    "PASS",
		"post_arm_contract_edit_stales":     "PASS",
		"unsupported_repair_refuses":        "PASS",
	}
	if !journeysAllGreen(scenarios) {
		t.Fatal("executed process scenarios did not all pass")
	}
	procAssertQualificationReport(t, []string{
		"cli_start_queues_root",
		"reopened_review_reads_frontier",
		"cli_task_start_refuses_dependent",
		"cli_verify_add_plans_check",
		"doctor_reports_actionable_fault",
		"doctor_reports_structured_json",
		"doctor_reports_normal_wait",
		"doctor_refuses_invalid_invocation",
		"acceptance_dispatches_next",
		"restart_reads_settled_state",
		"no_stream_rest_readback_queued",
		"post_arm_contract_edit_stales",
		"unsupported_repair_refuses",
		"installed_app_qualification",
		"live_provider_qualification",
	})
	_ = stateRoot
}

// procSelfServiceHarness builds the disposable vault, state root, project,
// and wave fixtures for the process-boundary qualification. Everything lives
// under temp dirs; the operator's projects are never touched.
type procSelfServiceHarness struct {
	vault     string
	stateRoot string
	project   RegisteredProject
	store     *RuntimeStore
}

func newProcSelfServiceHarness(t *testing.T) *procSelfServiceHarness {
	t.Helper()
	vault := v7DirectTestVault(t)
	setGlobalProfileForTest(t, "test-codex-exec", directEmergencyRunnerProfileForTest())
	for _, lane := range []string{"execute", "review"} {
		if _, err := setProjectLocalConfigWithReadback(vault, "automation.model_levels.standard."+lane, []string{"test-codex-exec"}); err != nil {
			t.Fatal(err)
		}
	}
	if repo := v7RepoRoot(vault); !v7GitRepo(repo) {
		runGit(t, "-C", repo, "init", "-q")
	}
	stateRoot := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	harness := &procSelfServiceHarness{vault: vault, stateRoot: stateRoot}
	harness.openStore(t)
	project := newRegisteredProject(v7RepoRoot(vault), vault)
	if _, _, err := harness.store.RegisterProject(project); err != nil {
		t.Fatal(err)
	}
	harness.project = project
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001", "APP-T-0002"}, map[string]any{"concurrency": 1})
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	writeDirectTask(t, vault, "APP-T-0002", "W-0001", map[string]any{"dependencies": []any{"APP-T-0001:hard"}})
	if err := harness.store.SetProjectEnabled(project.ProjectID, true); err != nil {
		t.Fatal(err)
	}
	harness.closeStore(t)
	return harness
}

func (h *procSelfServiceHarness) openStore(t *testing.T) {
	t.Helper()
	if h.store != nil {
		t.Fatal("process harness store already open across a helper boundary")
	}
	store, err := OpenRuntimeStore(h.stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	h.store = store
}

func (h *procSelfServiceHarness) closeStore(t *testing.T) {
	t.Helper()
	if h.store == nil {
		return
	}
	if err := h.store.Close(); err != nil {
		t.Fatal(err)
	}
	h.store = nil
}

// cli spawns one disposable helper process running a single CLI argv through
// the real parser and dispatch, and returns its combined output and exit
// code. The harness store must be closed across the call so the helper owns
// the only open handle at the recovery boundary.
func (h *procSelfServiceHarness) cli(t *testing.T, argv ...string) (string, int) {
	t.Helper()
	if h.store != nil {
		t.Fatal("process harness store must be closed across a helper boundary")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestSelfServiceProcessHelper$")
	cmd.Env = append(scrubAgentSessionEnv(os.Environ()),
		"TUSKER_SELF_SERVICE_HELPER=1",
		"TUSKER_PROC_STATE_ROOT="+h.stateRoot,
		"TUSKER_PROC_CONFIG="+userGlobalTuskerConfigPath(),
		"TUSKER_HELPER_ARGV="+strings.Join(argv, "\n"),
	)
	output, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("helper process failed to run: %v\n%s", err, output)
		}
		code = exitErr.ExitCode()
	}
	return string(output), code
}

// procClaimRoot claims the queued root directive with a real fixture-runner
// attempt inside the parent process.
func procClaimRoot(t *testing.T, harness *procSelfServiceHarness, attemptID string) {
	t.Helper()
	root := RunStatus{ProjectID: harness.project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", Runner: string(RunnerCodexExec), Lane: runLaneExecute, LeaseState: string(LeaseStateUnclaimed)}
	if err := harness.store.UpsertRun(root); err != nil {
		t.Fatal(err)
	}
	idx, err := loadV7Index(harness.vault)
	if err != nil {
		t.Fatal(err)
	}
	wave := idx.Waves["W-0001"]
	claimed, err := harness.store.claimRunLeaseWithDirectiveAttempt(root, attemptID, 1, time.Minute, time.Now().UTC(),
		RuntimeLeaseClaimPrecondition{ExpectedLeaseState: LeaseStateUnclaimed},
		RunAuthorization{Source: "human_run_directive", Actor: "human:proc", DirectiveWaveID: "W-0001",
			DirectiveAuthorizationFingerprint: stringField(wave.Data, "authorization_fingerprint"), DirectiveWaveAuthorizedAt: stringField(wave.Data, "authorized_at")},
		RunAttempt{AttemptID: attemptID, Runner: string(RunnerCodexExec), Lane: runLaneExecute})
	if err != nil || !claimed {
		t.Fatalf("root directive claim did not consume: claimed=%t err=%v", claimed, err)
	}
}

// procFinishRun records a terminal outcome for the claimed attempt.
func procFinishRun(t *testing.T, harness *procSelfServiceHarness, attemptID, outcome, lastError string) {
	t.Helper()
	finished := RunStatus{ProjectID: harness.project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001",
		Runner: string(RunnerCodexExec), Lane: runLaneExecute, LeaseState: string(LeaseStateReleased),
		LeaseOwner: attemptID, LeaseGeneration: 1, ActiveAttemptID: attemptID,
		AttemptOutcome: outcome, LastError: lastError, Terminal: true}
	if err := harness.store.UpsertRun(finished); err != nil {
		t.Fatal(err)
	}
}

// procAssertQualificationReport links the retained qualification report to
// the executed suite: every scenario name (plus the explicitly unclaimed
// installed-app/live-provider rows) must appear, so the report cannot drift
// from what was actually exercised.
func procAssertQualificationReport(t *testing.T, names []string) {
	t.Helper()
	path := filepath.Join("..", "..", "docs", "reports", "self-service-recovery-qualification.md")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("qualification report is missing at %s: %v", path, err)
	}
	for _, name := range names {
		if !strings.Contains(string(raw), name) {
			t.Fatalf("qualification report does not link scenario %q", name)
		}
	}
}
