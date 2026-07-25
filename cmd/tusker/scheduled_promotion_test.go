package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setScheduledPromotionPolicyForTest(t *testing.T, vault, mode string) Workflow {
	t.Helper()
	if !fileExists(workflowPath(vault)) {
		if err := writeDefaultWorkflow(vault); err != nil {
			t.Fatal(err)
		}
	}
	path := workflowPath(vault)
	data, body, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	data["scheduled_promotion"] = map[string]any{"version": 1, "mode": mode}
	text, err := serializeDocument(data, body, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(path, text); err != nil {
		t.Fatal(err)
	}
	wf, err := loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	return wf.Data
}

func armScheduledPromotionWaveForTest(t *testing.T, vault, waveID string) {
	t.Helper()
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	wave := idx.Waves[waveID]
	fingerprint, issues := waveMaterialFingerprint(vault, idx, wave)
	if len(issues) != 0 {
		t.Fatalf("wave fingerprint issues: %v", issues)
	}
	data, body, err := parseFrontmatterMustRead(wave.AbsolutePath)
	if err != nil {
		t.Fatal(err)
	}
	data["authorization"] = "armed"
	data["authorization_fingerprint"] = fingerprint
	data["authorized_by"] = "human:test"
	data["authorized_at"] = "2026-07-25T00:00:00Z"
	if _, err := saveV7DocumentCAS(wave.AbsolutePath, data, body, v7FrontmatterOrder["wave"], stringField(data, "state_rev")); err != nil {
		t.Fatal(err)
	}
}

func commitScheduledPromotionWorkflowForTest(t *testing.T, repo, vault string) {
	t.Helper()
	commitScheduledPromotionWorkflowForWaveTest(t, repo, vault, "W-0001")
}

func commitScheduledPromotionWorkflowForWaveTest(t *testing.T, repo, vault, waveID string) {
	t.Helper()
	workflow := filepath.ToSlash(filepath.Join(".tusker", "WORKFLOW.md"))
	runGitDir(t, repo, "add", "--", workflow)
	runGitDir(t, repo, "commit", "-m", "configure scheduled promotion")
	worktree := filepath.Join(t.TempDir(), "scheduled-promotion-workflow")
	branch := "integration/" + waveID
	runGitDir(t, repo, "worktree", "add", "--detach", worktree, branch)
	if err := writeText(filepath.Join(worktree, workflow), mustReadIndexTest(t, filepath.Join(vault, "WORKFLOW.md"))); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, worktree, "add", "--", workflow)
	runGitDir(t, worktree, "commit", "-m", "configure scheduled promotion")
	next := strings.TrimSpace(gitDirOutput(t, worktree, "rev-parse", "HEAD"))
	runGitDir(t, repo, "worktree", "remove", "--force", worktree)
	runGitDir(t, repo, "update-ref", "refs/heads/"+branch, next)
}

func setScheduledPromotionGateForTest(t *testing.T, vault string, commands []string, profile string) Workflow {
	t.Helper()
	path := workflowPath(vault)
	data, body, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	data["orchestration"] = map[string]any{
		"gate": map[string]any{
			"profile":          profile,
			"harvest_commands": commands,
		},
	}
	text, err := serializeDocument(data, body, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(path, text); err != nil {
		t.Fatal(err)
	}
	wf, err := loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	return wf.Data
}

func commitCanonicalTaskStateToWaveIntegrationForTest(t *testing.T, repo, vault, taskID, waveID string) {
	t.Helper()
	branch := "integration/" + waveID
	old := strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", branch))
	worktree := filepath.Join(t.TempDir(), "scheduled-promotion-task-state")
	runGitDir(t, repo, "worktree", "add", "--detach", worktree, branch)
	rel := filepath.ToSlash(filepath.Join(".tusker", "work", "tasks", taskID+".md"))
	if err := writeText(filepath.Join(worktree, rel), mustReadIndexTest(t, filepath.Join(vault, "work", "tasks", taskID+".md"))); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, worktree, "add", "--", rel)
	runGitDir(t, worktree, "commit", "-m", "integrate task state "+taskID)
	next := strings.TrimSpace(gitDirOutput(t, worktree, "rev-parse", "HEAD"))
	runGitDir(t, repo, "worktree", "remove", "--force", worktree)
	runGitDir(t, repo, "update-ref", "refs/heads/"+branch, next, old)
}

func newScheduledPromotionRunForTest(t *testing.T, store *RuntimeStore, window string) DepartureRun {
	t.Helper()
	run, _, err := store.GetOrCreateDepartureRun(DepartureRun{
		ProjectID: "app", PolicyID: "scheduled-promotion/v1/promote",
		ScheduledWindow: window, State: DepartureStateGating,
	})
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func TestScheduledPromotionLandingStageNeverMovesMain(t *testing.T) {
	repo, vault := newLandReadyForMainAdvanceTest(t, "staged-only.txt", "staged\n")
	setScheduledPromotionPolicyForTest(t, vault, scheduledPromotionStage)
	before := strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "main"))
	if err := landV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "W-0001"}); err == nil || !strings.Contains(err.Error(), "policy refuses") {
		t.Fatalf("stage mode must refuse direct default advance, got %v", err)
	}
	if after := strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "main")); after != before {
		t.Fatalf("stage mode moved main: before=%s after=%s", before, after)
	}
	assertEqual(t, "staged\n", gitShowFile(t, repo, "integration/W-0001", "staged-only.txt"), "staging remains available")
}

func TestScheduledPromotionLandingUnconfiguredPolicyPreservesLegacyAdvance(t *testing.T) {
	_, vault := newLandTestRepo(t, 1, "true")
	if fileExists(workflowPath(vault)) {
		t.Fatal("legacy fixture unexpectedly has a workflow policy")
	}
	allowed, err := scheduledPromotionAllowsDefaultAdvance(vault)
	if err != nil || !allowed {
		t.Fatalf("an absent opt-in must preserve legacy explicit landing: allowed=%v err=%v", allowed, err)
	}
}

func TestScheduledPromotionLandingConfiguredPromoteReservesMainForDeparture(t *testing.T) {
	repo, vault := newLandReadyForMainAdvanceTest(t, "reserved-main.txt", "reserved\n")
	setScheduledPromotionPolicyForTest(t, vault, scheduledPromotionPromote)
	before := strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "main"))
	allowed, err := scheduledPromotionAllowsDefaultAdvance(vault)
	if err != nil || allowed {
		t.Fatalf("configured promote left legacy main authority enabled: allowed=%v err=%v", allowed, err)
	}
	err = landV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "W-0001"})
	if err == nil || !strings.Contains(err.Error(), "configured departures own main promotion") {
		t.Fatalf("manual wave landing was not fenced: %v", err)
	}
	if after := strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "main")); after != before {
		t.Fatalf("manual configured-policy landing moved main: before=%s after=%s", before, after)
	}
}

func TestScheduledPromotionLandingFrozenCandidateCASAndReplay(t *testing.T) {
	repo, vault := newLandReadyForMainAdvanceTest(t, "promoted.txt", "promoted\n")
	setScheduledPromotionPolicyForTest(t, vault, scheduledPromotionPromote)
	wf := setScheduledPromotionGateForTest(t, vault, []string{"test -f promoted.txt"}, "")
	armScheduledPromotionWaveForTest(t, vault, "W-0001")
	commitScheduledPromotionWorkflowForTest(t, repo, vault)
	stateRoot := t.TempDir()
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run := newScheduledPromotionRunForTest(t, store, "2026-07-25T20:00:00Z")
	commit, err := promoteScheduledWave(vault, "app", "W-0001", wf, store, &run, "daemon:test")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "main")); got != commit {
		t.Fatalf("promotion did not CAS main to committed SHA: got=%s want=%s", got, commit)
	}
	if run.State != DepartureStatePromoted || run.Promotion.CommittedSHA != commit || run.Gate.Status != "passed" || run.Candidate.CandidateSHA == "" || run.Gate.Toolchain == "" {
		t.Fatalf("promotion durable facts incomplete: %#v", run)
	}
	lease, err := store.FindResourceLease("gate:full")
	if err != nil || lease == nil || lease.State != resourceLeaseReleased || lease.ReleaseReason != "promotion passed" {
		t.Fatalf("full gate lease was not fenced and released: lease=%#v err=%v", lease, err)
	}
	replayed, err := promoteScheduledWave(vault, "app", "W-0001", wf, store, &run, "daemon:test")
	if err != nil || replayed != commit {
		t.Fatalf("replay must be idempotent: commit=%s replay=%s err=%v", commit, replayed, err)
	}
	if gitShowFile(t, repo, "main", filepath.ToSlash("promoted.txt")) != "promoted\n" {
		t.Fatal("promoted content missing from main")
	}
}

func TestScheduledPromotionLandingImplicitSingletonNeedsNoWaveArm(t *testing.T) {
	stateRoot := t.TempDir()
	repo, vault := newLandTestRepo(t, 1, "test -f singleton-promoted.txt")
	clearWaveBackpointer(t, vault, "APP-T-0001")
	setSingletonPromotionMode(t, vault, scheduledPromotionStage)
	setWaveTaskState(t, vault, "APP-T-0001", "review", "review", "")
	sourceSHA := commitLandBranch(t, repo, "task/APP-T-0001", "integration/W-0001", map[string]string{"singleton-promoted.txt": "yes\n"})
	setDepartureTaskSourceForTest(t, vault, "APP-T-0001", sourceSHA)
	if err := landFrozenSourcesAsIssuedDepartureInStateRoot(t, repo, vault,
		Args{"vault": vault, "quiet": "true", "actor": "daemon:departure:singleton-fixture", "_pos0": "APP-T-0001"},
		map[string]string{"APP-T-0001": sourceSHA},
		stateRoot,
	); err != nil {
		t.Fatalf("singleton staging failed: %v", err)
	}
	task, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	waveID := stringField(task, "wave")
	if waveID == "" {
		t.Fatal("singleton staging did not create an internal delivery unit")
	}
	wave, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "waves", waveID+".md"))
	if err != nil {
		t.Fatal(err)
	}
	if stringField(wave, "authorization") != "disarmed" || !v7ImplicitDeliveryUnit(Note{Data: wave}) {
		t.Fatalf("singleton must remain a disarmed non-dispatch delivery unit: %#v", wave)
	}
	setWaveTaskState(t, vault, "APP-T-0001", "done", "done", "2026-07-25T19:00:00Z")
	commitCanonicalTaskStateToWaveIntegrationForTest(t, repo, vault, "APP-T-0001", waveID)
	clearDepartureTaskSourceForTest(t, vault, "APP-T-0001")
	setScheduledPromotionPolicyForTest(t, vault, scheduledPromotionPromote)
	wf := setScheduledPromotionGateForTest(t, vault, []string{"test -f singleton-promoted.txt"}, "")
	commitScheduledPromotionWorkflowForWaveTest(t, repo, vault, waveID)
	clearWaveBackpointer(t, vault, "APP-T-0001")
	task, _, err = parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil || stringField(task, "wave") != "" {
		t.Fatalf("singleton promotion fixture retained an ordinary wave backpointer: %#v err=%v", task, err)
	}

	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run := newScheduledPromotionRunForTest(t, store, "2026-07-25T20:10:00Z")
	commit, err := promoteScheduledWave(vault, "app", waveID, wf, store, &run, "daemon:test")
	if err != nil {
		t.Fatal(err)
	}
	if got := gitShowFile(t, repo, commit, "singleton-promoted.txt"); got != "yes\n" {
		t.Fatalf("singleton content did not reach promoted revision: %q", got)
	}
	wave, _, err = parseFrontmatterMustRead(filepath.Join(vault, "work", "waves", waveID+".md"))
	if err != nil || stringField(wave, "authorization") != "disarmed" {
		t.Fatalf("promotion must not convert singleton landing authority into dispatch authority: %#v err=%v", wave, err)
	}
}

func TestScheduledPromotionLandingPreservesDivergentUntrackedControlState(t *testing.T) {
	repo := t.TempDir()
	runGitDir(t, repo, "init", "-b", "main")
	runGitDir(t, repo, "config", "user.email", "test@example.com")
	runGitDir(t, repo, "config", "user.name", "Test User")
	if err := writeText(filepath.Join(repo, "README.md"), "seed\n"); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, repo, "add", ".")
	runGitDir(t, repo, "commit", "-m", "seed")
	runGitDir(t, repo, "checkout", "-b", "candidate")
	rel := filepath.ToSlash(filepath.Join(".tusker", "work", "waves", "W-0002.md"))
	if err := writeText(filepath.Join(repo, rel), "candidate\n"); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, repo, "add", rel)
	runGitDir(t, repo, "commit", "-m", "candidate control state")
	candidate := strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "HEAD"))
	runGitDir(t, repo, "checkout", "main")
	if err := writeText(filepath.Join(repo, rel), "local divergent state\n"); err != nil {
		t.Fatal(err)
	}
	err := prepareV7IdenticalUntrackedStateForDefaultAdvance(repo, candidate)
	if err == nil || !strings.Contains(err.Error(), "divergent untracked Tusker state") {
		t.Fatalf("divergent untracked control state was not refused: %v", err)
	}
	if got := mustReadIndexTest(t, filepath.Join(repo, rel)); got != "local divergent state\n" {
		t.Fatalf("refusal modified divergent local state: %q", got)
	}
}

func TestScheduledPromotionLandingRunsFrozenFullGateContract(t *testing.T) {
	repo, vault := newLandReadyForMainAdvanceTest(t, "full-gate.txt", "candidate\n")
	setScheduledPromotionPolicyForTest(t, vault, scheduledPromotionPromote)
	wf := setScheduledPromotionGateForTest(t, vault, []string{"echo FULL_GATE_EXECUTED >&2; exit 1"}, "canonical")
	armScheduledPromotionWaveForTest(t, vault, "W-0001")
	commitScheduledPromotionWorkflowForTest(t, repo, vault)
	beforeMain := strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "main"))
	snapshot, err := scheduledPromotionSnapshot(vault, "app", "W-0001", wf)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Gate.Command != "echo FULL_GATE_EXECUTED >&2; exit 1" || snapshot.Gate.Profile != "canonical" || snapshot.Gate.Toolchain == "" {
		t.Fatalf("frozen full-gate identity is incomplete: %#v", snapshot.Gate)
	}

	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run := newScheduledPromotionRunForTest(t, store, "2026-07-25T20:20:00Z")
	_, err = promoteScheduledWave(vault, "app", "W-0001", wf, store, &run, "daemon:test")
	if err == nil || !strings.Contains(err.Error(), "FULL_GATE_EXECUTED") {
		t.Fatalf("promotion did not execute the declared full gate: %v", err)
	}
	if got := strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "main")); got != beforeMain {
		t.Fatalf("red full gate moved main: before=%s after=%s", beforeMain, got)
	}
	durable, err := store.FindDepartureRun(run.ID)
	if err != nil || durable == nil || durable.State != DepartureStateFailed || durable.Gate.Status != "failed" || durable.Gate.Failure.Identity == "" || durable.Gate.Failure.Action != "owner_rework" || durable.Gate.Failure.OwningTaskID != "APP-T-0001" || len(durable.Gate.Failure.ArtifactRefs) != 1 {
		t.Fatalf("red gate facts were not persisted: run=%#v err=%v", durable, err)
	}
	if strings.Contains(durable.Gate.Failure.ArtifactRefs[0], "FULL_GATE_EXECUTED") {
		t.Fatalf("raw gate output leaked into durable failure: %#v", durable.Gate.Failure)
	}
	raw, readErr := os.ReadFile(durable.Gate.Failure.ArtifactRefs[0])
	if readErr != nil || !strings.Contains(string(raw), "FULL_GATE_EXECUTED") {
		t.Fatalf("runtime gate artifact is not resolvable/raw: %q err=%v", raw, readErr)
	}
	durableJSON, marshalErr := json.Marshal(durable)
	if marshalErr != nil || strings.Contains(string(durableJSON), "$ echo FULL_GATE_EXECUTED") {
		t.Fatalf("raw gate output leaked into departure JSON: %s err=%v", durableJSON, marshalErr)
	}
	owner, _, ownerErr := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if ownerErr != nil || stringField(owner, "status") != "rework" {
		t.Fatalf("singleton owner was not canonically returned to rework: %#v err=%v", owner, ownerErr)
	}
}

func TestScheduledPromotionRedFailureIntentResumesWithoutHalfAppliedRoute(t *testing.T) {
	_, vault := newLandReadyForMainAdvanceTest(t, "red-resume.txt", "candidate\n")
	setScheduledPromotionPolicyForTest(t, vault, scheduledPromotionPromote)
	wf := setScheduledPromotionGateForTest(t, vault, []string{"echo red-resume >&2; exit 1"}, "canonical")
	armScheduledPromotionWaveForTest(t, vault, "W-0001")
	commitScheduledPromotionWorkflowForTest(t, v7RepoRoot(vault), vault)
	stateRoot := t.TempDir()
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	run := newScheduledPromotionRunForTest(t, store, "2026-07-25T20:21:00Z")
	injected := errors.New("injected after durable red intent")
	oldHook := scheduledPromotionAfterFailureIntent
	scheduledPromotionAfterFailureIntent = func() error { return injected }
	_, err = promoteScheduledWave(vault, "app", "W-0001", wf, store, &run, "daemon:test")
	scheduledPromotionAfterFailureIntent = oldHook
	if !errors.Is(err, injected) {
		t.Fatalf("missing intent interruption: %v", err)
	}
	durable, err := store.FindDepartureRun(run.ID)
	if err != nil || durable == nil || durable.State != DepartureStateRepairing || durable.Gate.Failure.Identity == "" {
		t.Fatalf("red intent was not durable before canonical mutations: %#v err=%v", durable, err)
	}
	owner, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil || stringField(owner, "status") != "done" {
		t.Fatalf("interrupted routing mutated task before replay: %#v err=%v", owner, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	// Simulate a concurrent reconciler winning the final state CAS after the
	// canonical owner mutation. The following daemon pass must finish, rather
	// than asking an operator to replay the red departure.
	durable, err = store.FindDepartureRun(run.ID)
	if err != nil || durable == nil {
		t.Fatalf("restarted store lost repairing intent: %#v err=%v", durable, err)
	}
	oldCompletion := scheduledPromotionBeforeFailureCompletion
	scheduledPromotionBeforeFailureCompletion = func() error {
		_, err := store.TransitionDepartureRun(*durable, durable.StateRevision)
		return err
	}
	restarted := &Daemon{store: store}
	err = restarted.resumeRepairingDepartureRoutes(RegisteredProject{ProjectID: "app", VaultRoot: vault})
	scheduledPromotionBeforeFailureCompletion = oldCompletion
	if err == nil || !strings.Contains(err.Error(), "lost its departure CAS") {
		t.Fatalf("injected final CAS conflict was not surfaced for daemon retry: %v", err)
	}
	owner, _, err = parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil || stringField(owner, "status") != "rework" {
		t.Fatalf("first daemon route did not apply idempotent owner rework: %#v err=%v", owner, err)
	}
	if err := restarted.resumeRepairingDepartureRoutes(RegisteredProject{ProjectID: "app", VaultRoot: vault}); err != nil {
		t.Fatalf("daemon did not replay durable red route after CAS conflict: %v", err)
	}
	durable, err = store.FindDepartureRun(run.ID)
	if err != nil || durable == nil || durable.State != DepartureStateFailed {
		t.Fatalf("daemon reconciliation did not reach durable failure: %#v err=%v", durable, err)
	}
	owner, _, err = parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil || stringField(owner, "status") != "rework" {
		t.Fatalf("replayed isolated route did not rework owner: %#v err=%v", owner, err)
	}
}

func TestScheduledPromotionLandingHeartbeatsLongFullGateLease(t *testing.T) {
	repo, vault := newLandReadyForMainAdvanceTest(t, "long-gate.txt", "candidate\n")
	setScheduledPromotionPolicyForTest(t, vault, scheduledPromotionPromote)
	wf := setScheduledPromotionGateForTest(t, vault, []string{"sleep 0.15; test -f long-gate.txt"}, "")
	armScheduledPromotionWaveForTest(t, vault, "W-0001")
	commitScheduledPromotionWorkflowForTest(t, repo, vault)

	oldTTL := scheduledPromotionResourceLeaseTTL
	scheduledPromotionResourceLeaseTTL = 60 * time.Millisecond
	defer func() { scheduledPromotionResourceLeaseTTL = oldTTL }()
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run := newScheduledPromotionRunForTest(t, store, "2026-07-25T20:30:00Z")
	if _, err := promoteScheduledWave(vault, "app", "W-0001", wf, store, &run, "daemon:test"); err != nil {
		t.Fatalf("heartbeat did not preserve the full-gate fence: %v", err)
	}
	lease, err := store.FindResourceLease("gate:full")
	if err != nil || lease == nil || lease.State != resourceLeaseReleased || lease.ReleaseReason != "promotion passed" {
		t.Fatalf("long gate did not release its exact fenced lease: %#v err=%v", lease, err)
	}
}

func TestScheduledPromotionLandingCrashBeforeRefUpdateResumesExactIntent(t *testing.T) {
	repo, vault := newLandReadyForMainAdvanceTest(t, "pre-ref-crash.txt", "candidate\n")
	setScheduledPromotionPolicyForTest(t, vault, scheduledPromotionPromote)
	wf := setScheduledPromotionGateForTest(t, vault, []string{"test -f pre-ref-crash.txt"}, "")
	armScheduledPromotionWaveForTest(t, vault, "W-0001")
	commitScheduledPromotionWorkflowForTest(t, repo, vault)
	beforeMain := strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "main"))

	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run := newScheduledPromotionRunForTest(t, store, "2026-07-25T20:39:00Z")
	injected := errors.New("injected crash after durable ref intent")
	oldHook := scheduledPromotionAfterRefIntent
	scheduledPromotionAfterRefIntent = func() error { return injected }
	_, promoteErr := promoteScheduledWave(vault, "app", "W-0001", wf, store, &run, "daemon:test")
	scheduledPromotionAfterRefIntent = oldHook
	if !errors.Is(promoteErr, injected) {
		t.Fatalf("missing injected pre-ref failure: %v", promoteErr)
	}
	if got := strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "main")); got != beforeMain {
		t.Fatalf("pre-ref failure moved main: before=%s after=%s", beforeMain, got)
	}
	durable, err := store.FindDepartureRun(run.ID)
	if err != nil || durable == nil ||
		durable.Promotion.ExpectedSHA != beforeMain ||
		durable.Promotion.IntendedSHA == "" ||
		durable.Promotion.CommittedSHA != "" {
		t.Fatalf("exact pre-ref intent was not durable: %#v err=%v", durable, err)
	}
	project := RegisteredProject{ProjectID: "app", RepoRoot: repo, VaultRoot: vault}
	recoveries, err := store.ReconcileDepartureRunsForProject(project)
	if err != nil || len(recoveries) != 1 ||
		recoveries[0].Disposition != DepartureRecoveryResumable ||
		recoveries[0].ResumeState != DepartureStateGating ||
		recoveries[0].Run.State != DepartureStateGating {
		t.Fatalf("expected-old ref was not classified as a safe pre-ref resume: %#v err=%v", recoveries, err)
	}
	resumed := recoveries[0].Run
	commit, err := promoteScheduledWave(vault, "app", "W-0001", wf, store, &resumed, "daemon:test")
	if err != nil {
		t.Fatalf("resume exact ref intent: %v", err)
	}
	if commit != durable.Promotion.IntendedSHA || strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "main")) != durable.Promotion.IntendedSHA {
		t.Fatalf("pre-ref resume did not commit the exact intended SHA: commit=%s intent=%s", commit, durable.Promotion.IntendedSHA)
	}
}

func TestScheduledPromotionPreRefReplayRejectsDisarmedWave(t *testing.T) {
	repo, vault := newLandReadyForMainAdvanceTest(t, "pre-ref-disarm.txt", "candidate\n")
	setScheduledPromotionPolicyForTest(t, vault, scheduledPromotionPromote)
	wf := setScheduledPromotionGateForTest(t, vault, []string{"test -f pre-ref-disarm.txt"}, "")
	armScheduledPromotionWaveForTest(t, vault, "W-0001")
	commitScheduledPromotionWorkflowForTest(t, repo, vault)
	mainBefore := strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "main"))

	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run := newScheduledPromotionRunForTest(t, store, "2026-07-25T20:39:30Z")
	injected := errors.New("injected crash before disarm replay")
	oldHook := scheduledPromotionAfterRefIntent
	scheduledPromotionAfterRefIntent = func() error { return injected }
	_, promoteErr := promoteScheduledWave(vault, "app", "W-0001", wf, store, &run, "daemon:test")
	scheduledPromotionAfterRefIntent = oldHook
	if !errors.Is(promoteErr, injected) {
		t.Fatalf("missing injected pre-ref failure: %v", promoteErr)
	}
	durable, err := store.FindDepartureRun(run.ID)
	if err != nil || durable == nil || durable.Promotion.AttemptedAt == "" {
		t.Fatalf("load durable pre-ref intent: %#v err=%v", durable, err)
	}
	if err := mutateWaveAuthorization(Args{
		"vault": vault, "_pos0": "W-0001",
		"by": "human:operator", "quiet": "true",
	}, "disarmed", nil); err != nil {
		t.Fatal(err)
	}
	wavePath := filepath.Join(vault, "work", "waves", "W-0001.md")
	operatorBytes := mustReadIndexTest(t, wavePath)
	operatorInfo, err := os.Stat(wavePath)
	if err != nil {
		t.Fatal(err)
	}

	resumed := *durable
	if _, err := promoteScheduledWave(vault, "app", "W-0001", wf, store, &resumed, "daemon:test"); err == nil ||
		!strings.Contains(err.Error(), "wave_authorization_not_armed:W-0001") {
		t.Fatalf("pre-ref replay ignored live disarm: %v", err)
	}
	if after := strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "main")); after != mainBefore {
		t.Fatalf("disarmed pre-ref replay moved main: before=%s after=%s", mainBefore, after)
	}
	if after := mustReadIndexTest(t, wavePath); after != operatorBytes {
		t.Fatal("pre-ref replay overwrote disarm bytes")
	}
	afterInfo, err := os.Stat(wavePath)
	if err != nil {
		t.Fatal(err)
	}
	if afterInfo.Mode().Perm() != operatorInfo.Mode().Perm() {
		t.Fatalf("pre-ref replay changed disarm mode: before=%v after=%v", operatorInfo.Mode().Perm(), afterInfo.Mode().Perm())
	}
}

func TestScheduledPromotionFinalAuthorityEpochLinearizesManagedDisarm(t *testing.T) {
	repo, vault := newLandReadyForMainAdvanceTest(t, "epoch-linearization.txt", "candidate\n")
	setScheduledPromotionPolicyForTest(t, vault, scheduledPromotionPromote)
	wf := setScheduledPromotionGateForTest(t, vault, []string{"test -f epoch-linearization.txt"}, "")
	armScheduledPromotionWaveForTest(t, vault, "W-0001")
	commitScheduledPromotionWorkflowForTest(t, repo, vault)
	mainBefore := strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "main"))

	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run := newScheduledPromotionRunForTest(t, store, "2026-07-25T20:39:45Z")

	oldFinalHook := scheduledPromotionAfterFinalAuthority
	oldEpochObserver := v7MaterialEpochLockObserver
	defer func() {
		scheduledPromotionAfterFinalAuthority = oldFinalHook
		v7MaterialEpochLockObserver = oldEpochObserver
	}()
	writerAttempted := make(chan struct{}, 1)
	writerDone := make(chan error, 1)
	hookCalls := 0
	scheduledPromotionAfterFinalAuthority = func() error {
		hookCalls++
		v7MaterialEpochLockObserver = func() {
			writerAttempted <- struct{}{}
		}
		go func() {
			writerDone <- mutateWaveAuthorization(Args{
				"vault": vault, "_pos0": "W-0001",
				"by": "human:operator", "quiet": "true",
			}, "disarmed", nil)
		}()
		select {
		case <-writerAttempted:
			v7MaterialEpochLockObserver = nil
		case err := <-writerDone:
			if err != nil {
				return errors.New("managed disarm failed before the material epoch: " + err.Error())
			}
			return errors.New("managed disarm completed before the material epoch")
		case <-time.After(5 * time.Second):
			return errors.New("managed disarm did not reach the material epoch")
		}
		select {
		case err := <-writerDone:
			if err != nil {
				return errors.New("managed disarm crossed the held material epoch: " + err.Error())
			}
			return errors.New("managed disarm crossed the held material epoch")
		default:
			return nil
		}
	}

	commit, err := promoteScheduledWave(vault, "app", "W-0001", wf, store, &run, "daemon:test")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-writerDone:
		if err != nil {
			t.Fatalf("managed disarm failed after the ref epoch: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("managed disarm remained blocked after the ref epoch")
	}
	if hookCalls != 1 {
		t.Fatalf("final authority hook calls=%d, want 1", hookCalls)
	}
	if after := strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "main")); after != commit || after == mainBefore {
		t.Fatalf("promotion did not linearize before disarm: before=%s after=%s commit=%s", mainBefore, after, commit)
	}
	wave, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "waves", "W-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	if got := stringField(wave, "authorization"); got != "disarmed" {
		t.Fatalf("managed disarm did not apply after promotion: authorization=%q", got)
	}
}

func TestScheduledPromotionFinalAuthorityEpochLinearizesDeliveryReimport(t *testing.T) {
	repo, vault := newLandTestRepo(t, 1, "test -f epoch-reimport.txt")
	if err := writeText(filepath.Join(repo, "docs", "specs", "delivery.md"), "# Delivery\n"); err != nil {
		t.Fatal(err)
	}
	planPath := writeDeliveryTestPlan(t, vault, validDeliveryPlan())
	importArgs := Args{"vault": vault, "plan": planPath, "by": "human:test", "quiet": "true"}
	if err := deliveryImportCmd(importArgs); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, repo, "add", "-A")
	runGitDir(t, repo, "commit", "-m", "record reimport contract")
	runGitDir(t, repo, "branch", "-f", "integration/W-0001", "main")

	sourceSHA := commitLandBranch(t, repo, "task/APP-T-0001", "integration/W-0001", map[string]string{"epoch-reimport.txt": "candidate\n"})
	setDepartureTaskSourceForTest(t, vault, "APP-T-0001", sourceSHA)
	if err := landFrozenSourcesAsIssuedDeparture(t, repo, vault,
		Args{"vault": vault, "quiet": "true", "actor": "daemon:departure:reimport-fixture", "_pos0": "APP-T-0001"},
		map[string]string{"APP-T-0001": sourceSHA},
	); err != nil {
		t.Fatal(err)
	}
	setWaveTaskState(t, vault, "APP-T-0001", "done", "done", "2026-07-25T02:00:00Z")
	commitCanonicalTaskStateToIntegration(t, repo, vault, "APP-T-0001")
	setScheduledPromotionPolicyForTest(t, vault, scheduledPromotionPromote)
	wf := setScheduledPromotionGateForTest(t, vault, []string{"test -f epoch-reimport.txt"}, "")
	armScheduledPromotionWaveForTest(t, vault, "W-0001")
	commitScheduledPromotionWorkflowForTest(t, repo, vault)
	mainBefore := strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "main"))

	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run := newScheduledPromotionRunForTest(t, store, "2026-07-25T20:41:00Z")

	oldFinalHook := scheduledPromotionAfterFinalAuthority
	oldEpochObserver := v7MaterialEpochLockObserver
	defer func() {
		scheduledPromotionAfterFinalAuthority = oldFinalHook
		v7MaterialEpochLockObserver = oldEpochObserver
	}()
	importAttempted := make(chan struct{}, 1)
	importDone := make(chan error, 1)
	scheduledPromotionAfterFinalAuthority = func() error {
		v7MaterialEpochLockObserver = func() {
			importAttempted <- struct{}{}
		}
		go func() {
			importDone <- deliveryImportCmd(cloneArgs(importArgs))
		}()
		select {
		case <-importAttempted:
			v7MaterialEpochLockObserver = nil
		case err := <-importDone:
			return fmt.Errorf("delivery re-import entered the final ref-CAS gap: %v", err)
		case <-time.After(5 * time.Second):
			return errors.New("delivery re-import did not reach the material epoch")
		}
		select {
		case err := <-importDone:
			return fmt.Errorf("delivery re-import crossed the held material epoch: %v", err)
		default:
			return nil
		}
	}

	commit, err := promoteScheduledWave(vault, "app", "W-0001", wf, store, &run, "daemon:test")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-importDone:
		if err != nil {
			t.Fatalf("same-contract delivery re-import failed after the ref epoch: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("delivery re-import remained blocked after the ref epoch")
	}
	if after := strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "main")); after != commit || after == mainBefore {
		t.Fatalf("promotion did not linearize before delivery re-import: before=%s after=%s commit=%s", mainBefore, after, commit)
	}
}

func TestScheduledPromotionLandingCrashAfterRefUpdateRecoversCommittedIntent(t *testing.T) {
	repo, vault := newLandReadyForMainAdvanceTest(t, "crash-window.txt", "candidate\n")
	setScheduledPromotionPolicyForTest(t, vault, scheduledPromotionPromote)
	wf := setScheduledPromotionGateForTest(t, vault, []string{"test -f crash-window.txt"}, "")
	armScheduledPromotionWaveForTest(t, vault, "W-0001")
	commitScheduledPromotionWorkflowForTest(t, repo, vault)
	beforeMain := strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "main"))

	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run := newScheduledPromotionRunForTest(t, store, "2026-07-25T20:40:00Z")
	injected := errors.New("injected crash after ref update")
	oldHook := scheduledPromotionAfterRefUpdate
	scheduledPromotionAfterRefUpdate = func() error { return injected }
	_, promoteErr := promoteScheduledWave(vault, "app", "W-0001", wf, store, &run, "daemon:test")
	scheduledPromotionAfterRefUpdate = oldHook
	if !errors.Is(promoteErr, injected) {
		t.Fatalf("missing injected post-ref failure: %v", promoteErr)
	}
	afterMain := strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "main"))
	if afterMain == beforeMain {
		t.Fatal("failure seam did not occur after the ref update")
	}
	durable, err := store.FindDepartureRun(run.ID)
	if err != nil || durable == nil ||
		durable.Promotion.AttemptedAt == "" ||
		durable.Promotion.IntendedSHA != afterMain ||
		durable.Promotion.CommittedSHA != "" {
		t.Fatalf("pre-ref intent was not durable across the crash window: %#v err=%v", durable, err)
	}
	project := RegisteredProject{ProjectID: "app", RepoRoot: repo, VaultRoot: vault}
	recoveries, err := store.ReconcileDepartureRunsForProject(project)
	if err != nil || len(recoveries) != 1 ||
		recoveries[0].Disposition != DepartureRecoveryResumable ||
		recoveries[0].ResumeState != DepartureStatePromoted ||
		recoveries[0].Run.State != DepartureStatePromoted ||
		recoveries[0].Run.Promotion.CommittedSHA != afterMain {
		t.Fatalf("intended ref outcome was not recovered as committed: %#v err=%v", recoveries, err)
	}
	recovered := recoveries[0].Run
	if replayed, replayErr := promoteScheduledWave(vault, "app", "W-0001", wf, store, &recovered, "daemon:test"); replayErr != nil || replayed != afterMain {
		t.Fatalf("committed intent replay failed: replay=%s err=%v", replayed, replayErr)
	}
}

func TestScheduledPromotionLandingIntentRecoveryBlocksDriftAndLegacyRows(t *testing.T) {
	t.Run("third ref value blocks", func(t *testing.T) {
		repo, vault := newLandReadyForMainAdvanceTest(t, "intent-drift.txt", "candidate\n")
		setScheduledPromotionPolicyForTest(t, vault, scheduledPromotionPromote)
		wf := setScheduledPromotionGateForTest(t, vault, []string{"test -f intent-drift.txt"}, "")
		armScheduledPromotionWaveForTest(t, vault, "W-0001")
		commitScheduledPromotionWorkflowForTest(t, repo, vault)
		store, err := OpenRuntimeStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		run := newScheduledPromotionRunForTest(t, store, "2026-07-25T20:41:00Z")
		injected := errors.New("injected pre-ref crash")
		oldHook := scheduledPromotionAfterRefIntent
		scheduledPromotionAfterRefIntent = func() error { return injected }
		_, promoteErr := promoteScheduledWave(vault, "app", "W-0001", wf, store, &run, "daemon:test")
		scheduledPromotionAfterRefIntent = oldHook
		if !errors.Is(promoteErr, injected) {
			t.Fatalf("missing injected pre-ref failure: %v", promoteErr)
		}
		durable, err := store.FindDepartureRun(run.ID)
		if err != nil || durable == nil {
			t.Fatalf("load durable intent: %#v err=%v", durable, err)
		}
		external := commitLandBranch(t, repo, "external-main", "main", map[string]string{"external-main.txt": "external\n"})
		runGitDir(t, repo, "update-ref", "refs/heads/main", external, durable.Promotion.ExpectedSHA)
		recoveries, err := store.ReconcileDepartureRunsForProject(RegisteredProject{ProjectID: "app", RepoRoot: repo, VaultRoot: vault})
		if err != nil || len(recoveries) != 1 ||
			recoveries[0].Disposition != DepartureRecoveryBlocked ||
			recoveries[0].Run.State != DepartureStateBlocked ||
			!strings.Contains(recoveries[0].Reason, "matches neither") {
			t.Fatalf("third ref value did not fail closed: %#v err=%v", recoveries, err)
		}
	})

	t.Run("legacy intent without intended sha blocks", func(t *testing.T) {
		repo, vault := newLandReadyForMainAdvanceTest(t, "legacy-intent.txt", "candidate\n")
		store, err := OpenRuntimeStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		mainSHA := strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "main"))
		run, _, err := store.GetOrCreateDepartureRun(DepartureRun{
			ProjectID: "app", PolicyID: "scheduled-promotion/v1/promote", ScheduledWindow: "2026-07-25T20:42:00Z",
			State: DepartureStateGating,
			Promotion: DeparturePromotion{
				ExpectedRef: "main", ExpectedSHA: mainSHA,
				AttemptedAt: time.Now().UTC().Format(time.RFC3339Nano),
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		recoveries, err := store.ReconcileDepartureRunsForProject(RegisteredProject{ProjectID: "app", RepoRoot: repo, VaultRoot: vault})
		if err != nil || len(recoveries) != 1 ||
			recoveries[0].Run.ID != run.ID ||
			recoveries[0].Disposition != DepartureRecoveryBlocked ||
			!strings.Contains(recoveries[0].Reason, "legacy promotion intent lacks intended_sha") {
			t.Fatalf("legacy promotion intent did not fail closed: %#v err=%v", recoveries, err)
		}
	})
}

func TestScheduledPromotionLandingRefusesCandidateDrift(t *testing.T) {
	repo, vault := newLandReadyForMainAdvanceTest(t, "drift.txt", "one\n")
	wf := setScheduledPromotionPolicyForTest(t, vault, scheduledPromotionPromote)
	armScheduledPromotionWaveForTest(t, vault, "W-0001")
	commitScheduledPromotionWorkflowForTest(t, repo, vault)
	before, err := scheduledPromotionSnapshot(vault, "app", "W-0001", wf)
	if err != nil {
		t.Fatal(err)
	}
	commitLandBranch(t, repo, "candidate-drift", "integration/W-0001", map[string]string{"drift.txt": "two\n"})
	newSHA := strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "candidate-drift"))
	runGitDir(t, repo, "update-ref", "refs/heads/integration/W-0001", newSHA, before.Candidate.CandidateSHA)
	after, err := scheduledPromotionSnapshot(vault, "app", "W-0001", wf)
	if err != nil {
		t.Fatal(err)
	}
	if got := scheduledPromotionSnapshotDrift(before, after); got != "candidate" {
		t.Fatalf("candidate drift=%q, want candidate", got)
	}
}

func TestScheduledPromotionLandingNamesEveryFrozenInputDrift(t *testing.T) {
	base := scheduledPromotionCandidateSnapshot{
		WaveID: "W-0001",
		Candidate: DepartureCandidate{
			TaskStateRevisions:       map[string]string{"APP-T-0001": "state-1"},
			TaskSourceSHAs:           map[string]string{"APP-T-0001": "source-1"},
			WaveAuthorization:        "auth-1",
			IntegrationBaseSHA:       "integration-1",
			CandidateSHA:             "candidate-1",
			CandidateTreeHash:        "tree-1",
			ExpectedDefaultBranchSHA: "main-1",
		},
		Gate: DepartureGate{Command: "gate-1", Profile: "profile-1", Toolchain: "toolchain-1", TreeHash: "tree-1"},
	}
	tests := []struct {
		want   string
		mutate func(*scheduledPromotionCandidateSnapshot)
	}{
		{"wave", func(next *scheduledPromotionCandidateSnapshot) { next.WaveID = "W-0002" }},
		{"candidate", func(next *scheduledPromotionCandidateSnapshot) { next.Candidate.CandidateSHA = "candidate-2" }},
		{"integration", func(next *scheduledPromotionCandidateSnapshot) { next.Candidate.IntegrationBaseSHA = "integration-2" }},
		{"default_ref", func(next *scheduledPromotionCandidateSnapshot) { next.Candidate.ExpectedDefaultBranchSHA = "main-2" }},
		{"authorization", func(next *scheduledPromotionCandidateSnapshot) { next.Candidate.WaveAuthorization = "auth-2" }},
		{"task", func(next *scheduledPromotionCandidateSnapshot) {
			next.Candidate.TaskStateRevisions = map[string]string{"APP-T-0001": "state-2"}
		}},
		{"gate", func(next *scheduledPromotionCandidateSnapshot) { next.Gate.Profile = "profile-2" }},
	}
	for _, test := range tests {
		t.Run(test.want, func(t *testing.T) {
			next := base
			test.mutate(&next)
			if got := scheduledPromotionSnapshotDrift(base, next); got != test.want {
				t.Fatalf("drift=%q, want %q", got, test.want)
			}
		})
	}
}
