package main

// Real-work lifecycle behavioral tests.
//
// Each test exercises the ordinary product pathway (existing runtime,
// attempt, review, and workspace services) through the supported CLI
// commands. Zero matches is not proof: the required runner reports the
// executed count.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func mustParseJSONTest(t *testing.T, output string) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatalf("invalid JSON output: %v\n%s", err, output)
	}
	return payload
}

// Disposable reproduction of the WUX-T-0013 incident: `attempt start`
// claimed a runtime work session while `finish` looked only at file attempt
// notes and returned "No attempt exists".
func TestRealWorkLifecycleAliasConsistency(t *testing.T) {
	vault, _ := workSessionFixture(t, 1)
	captureStdout(t, func() {
		if err := attemptV7Cmd(Args{"vault": vault, "id": "APP-T-0001", "_pos0": "start", "by": "agent:luna", "quiet": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	linked, err := latestV7AttemptID(vault, "APP-T-0001")
	if err != nil {
		t.Fatalf("finish chain cannot resolve the attempt linked by attempt start: %v", err)
	}
	note, err := resolveV7Note(vault, linked, "attempt")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(stringField(note.Data, "runtime_attempt_id")) == "" {
		t.Fatalf("linked file attempt is not bound to the runtime session: %#v", note.Data)
	}
	// The incident's finish command must no longer report "No attempt exists".
	// Either the chain proceeds to proof validation or it completes the
	// handoff; both prove the created session is recognized.
	err = finishV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "by": "agent:luna"})
	if err != nil {
		if strings.Contains(err.Error(), "No attempt exists") {
			t.Fatalf("start/finish mismatch persists: err=%v", err)
		}
	} else {
		note, err := resolveV7Note(vault, linked, "attempt")
		if err != nil {
			t.Fatal(err)
		}
		if stringField(note.Data, "status") != "handoff" {
			t.Fatalf("finish completed without handoff: %#v", note.Data)
		}
	}
	// Progress exposes the same stable identities the UI projects.
	progress := mustParseJSONTest(t, captureStdout(t, func() {
		if err := workProgressCmd(Args{"vault": vault, "id": "APP-T-0001"}); err != nil {
			t.Fatal(err)
		}
	}))
	if progress["task_id"] != "APP-T-0001" || progress["stage"] != "in_progress" {
		t.Fatalf("progress projection = %#v", progress)
	}
}

// Shared-checkout route: start where the implementation lives, reconcile the
// binding read-only, then submit. Stale revisions are refused, never
// silently adopted.
func TestRealWorkLifecycleSharedCheckoutReconciliation(t *testing.T) {
	vault, _ := workSessionFixture(t, 1)
	configureWorkSessionMaterialScope(t, vault)
	// The supported shared-checkout route claims from the checkout that
	// holds the implementation instead of a fresh empty worktree.
	if _, err := setProjectLocalConfigWithReadback(vault, "workspace.strategy", "shared"); err != nil {
		t.Fatal(err)
	}
	if err := startWorkSessionTest(t, vault, "APP-T-0001", "agent:implementer"); err != nil {
		t.Fatal(err)
	}
	reconciled := mustParseJSONTest(t, captureStdout(t, func() {
		if err := workReconcileCmd(Args{"vault": vault, "id": "APP-T-0001", "by": "agent:implementer"}); err != nil {
			t.Fatal(err)
		}
	}))
	if reconciled["ok"] != true || reconciled["material_fingerprint"] == "" {
		t.Fatalf("reconcile binding = %#v", reconciled)
	}
	// No execution history is invented by the read-only reconcile.
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	before, _ := store.ListAttemptsForRun(mustProjectIDTest(t, store, "APP-T-0001"), "APP-T-0001")
	_ = store.Close()
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{"work_revision": 999})
	if err := workReconcileCmd(Args{"vault": vault, "id": "APP-T-0001", "by": "agent:implementer"}); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale revision reconcile = %v", err)
	}
	store, err = OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	after, _ := store.ListAttemptsForRun(mustProjectIDTest(t, store, "APP-T-0001"), "APP-T-0001")
	if len(after) != len(before) {
		t.Fatalf("reconcile invented execution history: %d -> %d", len(before), len(after))
	}
}

func mustProjectIDTest(t *testing.T, store *RuntimeStore, recordID string) string {
	t.Helper()
	run, err := store.FindRun(recordID)
	if err != nil || run == nil {
		t.Fatalf("run not found: %s err=%v", recordID, err)
	}
	return run.ProjectID
}

// An unrelated worktree note must never be treated as an import of the live
// implementation.
func TestRealWorkLifecycleWrongWorkspaceRefusal(t *testing.T) {
	vault, _ := workSessionFixture(t, 1)
	if err := startWorkSessionTest(t, vault, "APP-T-0001", "agent:a"); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(vault, "attempts", "APP-T-0001")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data := map[string]any{
		"schema": "tusker.attempt/v1", "kind": "attempt", "id": "APP-T-0001-A-0001",
		"runtime_attempt_id": "FOREIGN-ACTIVE", "lane": runLaneExecute,
		"project": v7ProjectID(vault), "task": "APP-T-0001",
		"runner": "codex", "workspace_kind": "copy",
		"workspace_path": string(filepath.Separator) + filepath.Join("tmp", "foreign-checkout"),
		"branch":         "task/foreign", "status": "started",
	}
	body := "# APP-T-0001-A-0001 · Agent attempt summary\n\n## Outcome\n\nForeign.\n"
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["attempt"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(dir, "APP-T-0001-A-0001.md"), content); err != nil {
		t.Fatal(err)
	}
	if _, err := ensureFileAttemptForHandoff(vault, "APP-T-0001", "agent:a"); err == nil || !strings.Contains(err.Error(), "not bound to the live work session") {
		t.Fatalf("unrelated worktree material was not refused: %v", err)
	}
}

// Review freshness: a tampered task revision or source SHA is refused with an
// actionable remedy instead of certifying stale work.
func TestRealWorkLifecycleStaleReviewRefusal(t *testing.T) {
	vault, _ := workSessionFixture(t, 1)
	configureWorkSessionMaterialScope(t, vault)
	if err := startWorkSessionTest(t, vault, "APP-T-0001", "agent:implementer"); err != nil {
		t.Fatal(err)
	}
	captureStdout(t, func() {
		if err := workSessionLifecycleCmd(Args{"id": "APP-T-0001", "by": "agent:implementer", "deliverable": "implementation", "verification": "A1 pass", "gate-verdicts": "A1=pass"}, "submit"); err != nil {
			t.Fatal(err)
		}
	})
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	run, err := store.FindRun("APP-T-0001")
	if err != nil || run == nil {
		t.Fatal(err)
	}
	attempts, err := store.ListAttemptsForRun(run.ProjectID, run.RecordID)
	_ = store.Close()
	if err != nil || len(attempts) != 1 {
		t.Fatal(err)
	}
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{"source_sha": attempts[0].EndState.HeadSHA})
	var packet workSessionPacket
	reviewJSON := captureStdout(t, func() {
		if err := workSessionReviewCmd(Args{"vault": vault, "id": "APP-T-0001", "by": "reviewer:agent", "source": "codex"}); err != nil {
			t.Fatal(err)
		}
	})
	if err := json.Unmarshal([]byte(reviewJSON), &packet); err != nil {
		t.Fatal(err)
	}
	if packet.Run == nil || packet.Run.ActiveAttemptID == "" || packet.MaterialFingerprint == "" {
		t.Fatalf("review packet lacks binding: %#v", packet)
	}
	current, err := resolveV7Note(vault, "APP-T-0001", "task")
	if err != nil {
		t.Fatal(err)
	}
	if err := reviewSubmitCmd(Args{"vault": vault, "id": "APP-T-0001", "attempt": packet.Run.ActiveAttemptID, "by": "reviewer:agent", "verdict": "changes_requested", "summary": "needs one correction", "finding": "repair A1", "task-rev": "stale-revision", "source-sha": stringField(current.Data, "source_sha"), "work-rev": strconv.Itoa(packet.Revision), "proof-fingerprint": packet.ProofFingerprint, "gate-fingerprint": packet.GateFingerprint, "material-fingerprint": packet.MaterialFingerprint}); err == nil || !strings.Contains(err.Error(), "stale task revision") {
		t.Fatalf("stale task revision was not refused: %v", err)
	}
	if err := reviewSubmitCmd(Args{"vault": vault, "id": "APP-T-0001", "attempt": packet.Run.ActiveAttemptID, "by": "reviewer:agent", "verdict": "changes_requested", "summary": "needs one correction", "finding": "repair A1", "task-rev": stringField(current.Data, "state_rev"), "source-sha": "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef", "work-rev": strconv.Itoa(packet.Revision), "proof-fingerprint": packet.ProofFingerprint, "gate-fingerprint": packet.GateFingerprint, "material-fingerprint": packet.MaterialFingerprint}); err == nil || !strings.Contains(err.Error(), "stale implementation source SHA") {
		t.Fatalf("stale source SHA was not refused: %v", err)
	}
}

// Accepted completion is distinct from merely exiting: submit moves work to
// review, close before review is refused, and only the reviewed ceremony
// reaches done.
func TestRealWorkLifecycleCompletionState(t *testing.T) {
	vault, _ := workSessionFixture(t, 2)
	configureWorkSessionMaterialScope(t, vault)
	setAutomationV7TaskFields(t, vault, "APP-T-0002", map[string]any{"owned_paths": []string{"owned-second"}})
	if err := startWorkSessionTest(t, vault, "APP-T-0001", "agent:implementer"); err != nil {
		t.Fatal(err)
	}
	captureStdout(t, func() {
		if err := workSessionLifecycleCmd(Args{"id": "APP-T-0001", "by": "agent:implementer", "deliverable": "implementation", "verification": "A1 pass", "gate-verdicts": "A1=pass"}, "submit"); err != nil {
			t.Fatal(err)
		}
	})
	data, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	if stringField(data, "status") != "review" {
		t.Fatalf("submit did not move task to review: %q", stringField(data, "status"))
	}
	progress := mustParseJSONTest(t, captureStdout(t, func() {
		if err := workProgressCmd(Args{"vault": vault, "id": "APP-T-0001"}); err != nil {
			t.Fatal(err)
		}
	}))
	if progress["stage"] != "submitted" {
		t.Fatalf("completion stage = %#v", progress)
	}
	// Close before review is refused: exiting a process is not acceptance.
	if err := closeV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0002", "by": "reviewer:agent", "reason": "premature"}); err == nil {
		t.Fatal("close before review succeeded")
	}
	// Accepted completion through the ordinary reviewed ceremony.
	plain := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": plain, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": plain, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Proof policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": plain, "quiet": "true", "epic": "APP", "title": "Inline proof close", "risk": "low", "priority": "p2", "proof-mode": "inline", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": plain, "quiet": "true", "id": "APP-T-0001", "runner": "codex"}, attemptV7StartCmd)
	mustV7Proof(t, Args{"vault": plain, "quiet": "true", "_pos1": "APP-T-0001", "covers": "A1", "check": "go test ./cmd/tusker -run TestProof -count=1", "result": "pass", "note": "Focused proof passed."}, v7TestVerificationMutation)
	if err := finishV7Cmd(Args{"vault": plain, "quiet": "true", "id": "APP-T-0001", "attempt": "APP-T-0001-A-0001", "summary": "Implementation complete.", "local": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := closeV7Cmd(Args{"vault": plain, "quiet": "true", "id": "APP-T-0001", "by": "reviewer:agent", "reason": "inline proof accepted", "local": "true"}); err != nil {
		t.Fatal(err)
	}
	done, _, err := parseFrontmatterMustRead(filepath.Join(plain, "work", "tasks", "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	if stringField(done, "status") != "done" {
		t.Fatalf("accepted completion status = %q", stringField(done, "status"))
	}
}

// Cancellation releases ownership and prevents late success; retry preserves
// history in a truthful new attempt.
func TestRealWorkLifecycleCancellationLateResult(t *testing.T) {
	vault, _ := workSessionFixture(t, 1)
	configureWorkSessionMaterialScope(t, vault)
	if err := startWorkSessionTest(t, vault, "APP-T-0001", "agent:a"); err != nil {
		t.Fatal(err)
	}
	captureStdout(t, func() {
		if err := workRealLifecycleCmd(Args{"vault": vault, "id": "APP-T-0001", "by": "agent:a", "reason": "operator stop"}, "cancel"); err != nil {
			t.Fatal(err)
		}
	})
	// Late success after cancellation is refused: the lease is gone.
	if err := workSessionLifecycleCmd(Args{"id": "APP-T-0001", "by": "agent:a", "deliverable": "late", "verification": "A1 pass", "gate-verdicts": "A1=pass"}, "submit"); err == nil {
		t.Fatal("late submit after cancellation succeeded")
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	run, err := store.FindRun("APP-T-0001")
	if err != nil || run == nil {
		t.Fatal(err)
	}
	projectID := run.ProjectID
	before, err := store.ListAttemptsForRun(projectID, run.RecordID)
	_ = store.Close()
	if err != nil || len(before) != 1 {
		t.Fatalf("cancelled history = %#v err=%v", before, err)
	}
	captureStdout(t, func() {
		if err := workRealLifecycleCmd(Args{"vault": vault, "id": "APP-T-0001", "by": "agent:a", "source": "codex"}, "retry"); err != nil {
			t.Fatal(err)
		}
	})
	// Retry while live is refused: cancel first.
	if err := workRealLifecycleCmd(Args{"vault": vault, "id": "APP-T-0001", "by": "agent:a", "source": "codex"}, "retry"); err == nil {
		t.Fatal("retry over a live session succeeded")
	}
	store, err = OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	after, err := store.ListAttemptsForRun(projectID, "APP-T-0001")
	if err != nil || len(after) != 2 {
		t.Fatalf("retry history = %#v err=%v", after, err)
	}
	live, err := store.FindRun("APP-T-0001")
	if err != nil || live == nil || live.LeaseOwner != "agent:a" || LeaseState(live.LeaseState) == LeaseStateReleased {
		t.Fatalf("retry did not open a live session: %#v err=%v", live, err)
	}
}

// Two authorized independent sessions may overlap; dependents wait on native
// completion; arming a wave requires human authority and never auto-starts.
func TestRealWorkLifecycleAllowedParallelWaves(t *testing.T) {
	vault, _ := workSessionFixture(t, 4)
	if _, err := setProjectLocalConfigWithReadback(vault, "runtime.max_active_runs_per_project", 4); err != nil {
		t.Fatal(err)
	}
	if err := startWorkSessionTest(t, vault, "APP-T-0001", "agent:a"); err != nil {
		t.Fatal(err)
	}
	if err := startWorkSessionTest(t, vault, "APP-T-0002", "agent:b"); err != nil {
		t.Fatal(err)
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	first, _ := store.FindRun("APP-T-0001")
	second, _ := store.FindRun("APP-T-0002")
	_ = store.Close()
	if first == nil || second == nil || first.LeaseOwner == second.LeaseOwner {
		t.Fatalf("independent sessions did not overlap: %#v %#v", first, second)
	}
	// Dependents wait for native completion rules.
	setAutomationV7TaskFields(t, vault, "APP-T-0003", map[string]any{"dependencies": []string{"APP-T-0001:hard"}})
	if err := startWorkSessionTest(t, vault, "APP-T-0003", "agent:c"); workSessionErrorCode(err) != "WORK_SESSION_DEPENDENCY_BLOCKED" {
		t.Fatalf("dependent overlap = %v", err)
	}
	// Later readiness does not imply automatic start: arming needs a human.
	setAutomationV7TaskFields(t, vault, "APP-T-0004", map[string]any{
		"artifact_contract": map[string]any{"kind": "implementation", "path": "owned/APP-T-0004", "summary": "Delivers the fourth parallel task."},
	})
	if err := waveV7CreateCmd(Args{"vault": vault, "quiet": "true", "local": "true", "by": "agent:w3", "id": "W-0001", "_pos0": "Fourth wave", "_pos1": "APP-T-0004"}); err != nil {
		t.Fatal(err)
	}
	setAutomationV7TaskFields(t, vault, "APP-T-0004", map[string]any{"wave": "W-0001"})
	note, err := resolveV7Note(vault, "APP-T-0004", "task")
	if err != nil {
		t.Fatal(err)
	}
	if blockers := v7TaskDispatchBlockersWithAuthorization(vault, note, true); len(blockers) == 0 {
		t.Fatal("disarmed wave member reports no authorization blocker")
	}
	if err := waveV7ArmCmd(Args{"vault": vault, "id": "W-0001", "local": "true", "by": "agent:w3"}); err == nil || !strings.Contains(strings.ToLower(err.Error()), "human") {
		t.Fatalf("agent arm was not refused on human authority: %v", err)
	}
	// The human arm runs through the same mutation service the CLI calls;
	// the green preflight environment is the established test primitive for
	// infrastructure preconditions, while the human authority stays real.
	green := greenWaveEnvironment()
	if err := mutateWaveAuthorization(Args{"vault": vault, "_pos0": "W-0001", "local": "true", "by": "human:sarav", "quiet": "true"}, "armed", &green); err != nil {
		t.Fatal(err)
	}
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	if stringField(idx.Waves["W-0001"].Data, "authorization") != "armed" {
		t.Fatalf("wave authorization = %q", stringField(idx.Waves["W-0001"].Data, "authorization"))
	}
	store, err = OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if run, _ := store.FindRun("APP-T-0004"); run != nil {
		t.Fatalf("arming auto-started work: %#v", run)
	}
}
