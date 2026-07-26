package main

import (
	"bytes"
	"context"
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

func TestScheduledPromotionGateFailureDetailPrefersLifecyclePersistenceError(t *testing.T) {
	cause := errors.New("full-gate lifecycle provider: persist certified lifecycle receipt for go test: database is read-only")
	detail := scheduledPromotionGateFailureDetail(cause, "# lifecycle_id=scope receipt_digest=sha256:fixture")
	if !strings.Contains(detail, "persist certified lifecycle receipt") || strings.Contains(detail, "receipt_digest") {
		t.Fatalf("gate failure hid its causal persistence error: %q", detail)
	}
}

func TestV7FullGateAllLedgerHitCollectsReceiptsInCommandOrder(t *testing.T) {
	repo := t.TempDir()
	runGitDir(t, repo, "init", "-b", "main")
	runGitDir(t, repo, "config", "user.email", "test@example.com")
	runGitDir(t, repo, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(repo, "candidate.txt"), []byte("candidate\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, repo, "add", ".")
	runGitDir(t, repo, "commit", "-m", "candidate")
	stateRoot := t.TempDir()
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	commands := []string{"go version >/dev/null", "go env GOOS >/dev/null"}
	policy := GateTierPolicy{Profile: "full", HarvestCommands: commands, IsolationProvider: "scripted"}
	runtime := defaultGateTierRuntime(store, "project", repo)
	treeHash, err := runtime.TreeHash(repo)
	if err != nil {
		t.Fatal(err)
	}
	toolchain := scheduledPromotionFullGateToolchainFingerprint(repo, commands, policy.IsolationProvider, stateRoot)
	binding := v7FullGateProviderBinding{ProjectID: "project", DepartureID: "departure-ledger-order", CandidateDigest: treeHash, GateProfile: policy.Profile, ProviderProfile: policy.IsolationProvider, Toolchain: toolchain}
	want := make([]GateProviderReceipt, len(commands))
	for index := len(commands) - 1; index >= 0; index-- {
		want[index] = scriptedV7ProviderReceipt(binding, commands[index], v7FullGateOutcomePassed, index)
		receipt := want[index]
		if err := store.RecordGateLedger(GateLedgerEntry{ProjectID: binding.ProjectID, TreeHash: treeHash, Command: commands[index], Profile: policy.Profile, Toolchain: toolchain, ProviderReceipt: &receipt}); err != nil {
			t.Fatal(err)
		}
	}
	state := &scriptedV7ProviderState{}
	previous := newV7FullGateProvider
	newV7FullGateProvider = func(profile, _, _ string) (v7FullGateProvider, error) {
		if profile != "scripted" {
			return nil, fmt.Errorf("unexpected provider %q", profile)
		}
		return &scriptedV7FullGateProvider{state: state}, nil
	}
	defer func() { newV7FullGateProvider = previous }()
	ctx := withV7FullGateDeparture(context.Background(), binding.DepartureID)
	execution := runV7GateTierOnRefContext(ctx, filepath.Join(repo, ".tusker"), repo, "HEAD", binding.ProjectID, policy, store)
	if execution.Err != nil || execution.Result.Outcome != gateOutcomeLedgerHit {
		t.Fatalf("all-ledger execution = %#v, %v", execution.Result, execution.Err)
	}
	if state.next != 0 {
		t.Fatalf("all-ledger hit executed %d provider commands", state.next)
	}
	if len(execution.ProviderReceipts) != len(want) {
		t.Fatalf("receipt count = %d, want %d", len(execution.ProviderReceipts), len(want))
	}
	for index := range want {
		if execution.ProviderReceipts[index] != want[index] || execution.ProviderReceipts[index].CommandDigest != v7FullGateTextDigest(commands[index]) {
			t.Fatalf("receipt[%d] = %#v, want %#v", index, execution.ProviderReceipts[index], want[index])
		}
	}
}

func TestV7FullGateProviderCommandsUseExactOrderedEvidenceRefs(t *testing.T) {
	repo := t.TempDir()
	runGitDir(t, repo, "init", "-b", "main")
	runGitDir(t, repo, "config", "user.email", "test@example.com")
	runGitDir(t, repo, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(repo, "candidate.txt"), []byte("candidate\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, repo, "add", ".")
	runGitDir(t, repo, "commit", "-m", "candidate")
	stateRoot := t.TempDir()
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	commands := []string{"go version >/dev/null", "go env GOOS >/dev/null"}
	state := &scriptedV7ProviderState{steps: []scriptedV7ProviderStep{
		{outcome: v7FullGateOutcomePassed, output: "first output\n"},
		{outcome: v7FullGateOutcomePassed, output: "second output\n"},
	}}
	previous := newV7FullGateProvider
	newV7FullGateProvider = func(string, string, string) (v7FullGateProvider, error) {
		return &scriptedV7FullGateProvider{state: state}, nil
	}
	defer func() { newV7FullGateProvider = previous }()
	execution := runV7GateTierOnRefContext(
		withV7FullGateDeparture(context.Background(), "departure-command-evidence"),
		filepath.Join(repo, ".tusker"), repo, "HEAD", "project",
		GateTierPolicy{Profile: "full", HarvestCommands: commands, IsolationProvider: "scripted"},
		store,
	)
	if execution.Err != nil || execution.Result.Outcome != gateOutcomePassed {
		t.Fatalf("two-command provider execution = %#v, %v", execution.Result, execution.Err)
	}
	if len(execution.ArtifactRefs) != 3 || execution.ArtifactRef != execution.ArtifactRefs[2] {
		t.Fatalf("ordered command evidence refs = %#v primary=%q", execution.ArtifactRefs, execution.ArtifactRef)
	}
	first, err := os.ReadFile(execution.ArtifactRefs[0])
	if err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(execution.ArtifactRefs[1])
	if err != nil {
		t.Fatal(err)
	}
	summary, err := os.ReadFile(execution.ArtifactRefs[2])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(first, []byte(commands[0])) || bytes.Contains(first, []byte(commands[1])) ||
		!bytes.Contains(second, []byte(commands[1])) || bytes.Contains(second, []byte(commands[0])) ||
		!bytes.Contains(summary, []byte(commands[0])) || !bytes.Contains(summary, []byte(commands[1])) {
		t.Fatalf("command evidence composition is ambiguous:\nfirst=%q\nsecond=%q\nsummary=%q", first, second, summary)
	}
}

func TestV7ScheduledPromotionFlakeRerunPreservesFirstProviderOutcome(t *testing.T) {
	repo, vault := newLandReadyForMainAdvanceTest(t, "flake-rerun.txt", "candidate\n")
	setScheduledPromotionPolicyForTest(t, vault, scheduledPromotionPromote)
	wf := setScheduledPromotionFlakeGateForTest(t, vault, []string{"go version >/dev/null"})
	armScheduledPromotionWaveForTest(t, vault, "W-0001")
	commitScheduledPromotionWorkflowForTest(t, repo, vault)
	state := &scriptedV7ProviderState{steps: []scriptedV7ProviderStep{
		{outcome: v7FullGateOutcomeFailed, output: "FLAKE intermittent fixture\n"},
		{outcome: v7FullGateOutcomePassed, output: "stable rerun\n"},
	}}
	previous := newV7FullGateProvider
	previousDurability := v7FullGateDurabilityHook
	newV7FullGateProvider = func(profile, _, _ string) (v7FullGateProvider, error) {
		if profile != "scripted" {
			return nil, fmt.Errorf("unexpected provider %q", profile)
		}
		return &scriptedV7FullGateProvider{state: state}, nil
	}
	v7FullGateDurabilityHook = func(stage string) error {
		if stage == "promotion_gate_artifact_synced" {
			state.artifactSynced = true
		}
		return nil
	}
	defer func() {
		newV7FullGateProvider = previous
		v7FullGateDurabilityHook = previousDurability
	}()
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run := newScheduledPromotionRunForTest(t, store, "2026-07-26T01:00:00Z")
	commit, err := promoteScheduledWave(vault, "app", "W-0001", wf, store, &run, "daemon:test")
	if err != nil {
		t.Fatal(err)
	}
	if commit == "" || state.next != 2 {
		t.Fatalf("flake rerun commit=%q executions=%d", commit, state.next)
	}
	if len(run.Gate.ProviderOutcomes) != 2 || run.Gate.ProviderOutcomes[0].Outcome != string(v7FullGateOutcomeFailed) || run.Gate.ProviderOutcomes[1].Outcome != string(v7FullGateOutcomePassed) {
		t.Fatalf("flake provider outcomes lost first attempt: %#v", run.Gate.ProviderOutcomes)
	}
	if len(run.Gate.ProviderReceipts) != 1 || run.Gate.ProviderReceipts[0] != run.Gate.ProviderOutcomes[1] {
		t.Fatalf("flake green receipt set = %#v, outcomes %#v", run.Gate.ProviderReceipts, run.Gate.ProviderOutcomes)
	}
	if len(run.Gate.ArtifactRefs) != 4 || run.Gate.ArtifactRef != run.Gate.ArtifactRefs[len(run.Gate.ArtifactRefs)-1] {
		t.Fatalf("flake ordered evidence refs = %#v, primary=%q", run.Gate.ArtifactRefs, run.Gate.ArtifactRef)
	}
	seenRefs := make(map[string]struct{})
	for _, ref := range run.Gate.ArtifactRefs {
		if _, duplicate := seenRefs[ref]; duplicate {
			t.Fatalf("flake evidence ref reused across commands/attempts: %#v", run.Gate.ArtifactRefs)
		}
		seenRefs[ref] = struct{}{}
		if _, err := os.Stat(ref); err != nil {
			t.Fatalf("flake evidence ref is not durable: %s: %v", ref, err)
		}
	}
	if len(state.finalized) != 2 || !containsV7ProviderOutcome(state.finalized, v7FullGateOutcomeFailed) || !containsV7ProviderOutcome(state.finalized, v7FullGateOutcomePassed) {
		t.Fatalf("flake attempt scopes not both finalized: %#v", state.finalized)
	}
	if state.finalizedBeforeArtifact {
		t.Fatal("flake red outcome was finalized before its artifact fsync")
	}
}

func TestV7ScheduledPromotionTypedProviderOutcomesBlockWithoutRepair(t *testing.T) {
	cases := []struct {
		name    string
		outcome v7FullGateOutcome
		err     error
		cancel  bool
	}{
		{name: "provider failed", outcome: v7FullGateOutcomeProvider, err: fmt.Errorf("%w: fixture provider failure", errV7FullGateProvider)},
		{name: "cancelled", outcome: v7FullGateOutcomeCanceled, err: context.Canceled, cancel: true},
		{name: "timed out", outcome: v7FullGateOutcomeTimedOut, err: context.DeadlineExceeded},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, vault := newLandReadyForMainAdvanceTest(t, "typed-provider.txt", "candidate\n")
			setScheduledPromotionPolicyForTest(t, vault, scheduledPromotionPromote)
			wf := setScheduledPromotionGateForTest(t, vault, []string{"scripted-command"}, "full")
			armScheduledPromotionWaveForTest(t, vault, "W-0001")
			commitScheduledPromotionWorkflowForTest(t, repo, vault)
			beforeMain := strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "main"))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			step := scriptedV7ProviderStep{outcome: tc.outcome, output: string(tc.outcome) + "\n", err: tc.err}
			if tc.cancel {
				step.onRun = cancel
			}
			state := &scriptedV7ProviderState{steps: []scriptedV7ProviderStep{step}}
			previous := newV7FullGateProvider
			previousDurability := v7FullGateDurabilityHook
			newV7FullGateProvider = func(profile, _, _ string) (v7FullGateProvider, error) {
				if profile != "test-fixture" {
					return nil, fmt.Errorf("unexpected provider %q", profile)
				}
				return &scriptedV7FullGateProvider{state: state}, nil
			}
			v7FullGateDurabilityHook = func(stage string) error {
				if stage == "promotion_gate_artifact_synced" {
					state.artifactSynced = true
				}
				return nil
			}
			defer func() {
				newV7FullGateProvider = previous
				v7FullGateDurabilityHook = previousDurability
			}()
			store, err := OpenRuntimeStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			run := newScheduledPromotionRunForTest(t, store, "2026-07-26T02:00:00Z")
			if _, err := promoteScheduledWaveContext(ctx, vault, "app", "W-0001", wf, store, &run, "daemon:test"); err == nil {
				t.Fatal("typed provider outcome unexpectedly promoted")
			}
			durable, err := store.FindDepartureRun(run.ID)
			if err != nil || durable == nil {
				t.Fatalf("read blocked departure: %#v, %v", durable, err)
			}
			if durable.State != DepartureStateBlocked || durable.Gate.Status != string(tc.outcome) || durable.Gate.Failure.Class != "provider" || durable.Gate.Failure.Action != string(tc.outcome) || len(durable.Gate.ProviderOutcomes) != 1 || durable.Gate.ProviderOutcomes[0].Outcome != string(tc.outcome) {
				t.Fatalf("typed provider routing = %#v", durable)
			}
			if durable.Gate.Failure.RepairTaskID != "" || durable.State == DepartureStateRepairing {
				t.Fatalf("typed provider outcome entered repair routing: %#v", durable.Gate.Failure)
			}
			if got := strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "main")); got != beforeMain {
				t.Fatalf("typed provider outcome moved main: %s -> %s", beforeMain, got)
			}
			if len(state.finalized) != 1 || state.finalized[0].Outcome != string(tc.outcome) {
				t.Fatalf("typed provider outcome was not finalized after durable block: %#v", state.finalized)
			}
			if state.finalizedBeforeArtifact {
				t.Fatal("non-green provider outcome was finalized before its artifact fsync")
			}
		})
	}
}

type scriptedV7ProviderStep struct {
	outcome v7FullGateOutcome
	output  string
	err     error
	onRun   func()
}

type scriptedV7ProviderState struct {
	steps                   []scriptedV7ProviderStep
	next                    int
	finalized               []GateProviderReceipt
	artifactSynced          bool
	finalizedBeforeArtifact bool
}

type scriptedV7FullGateProvider struct {
	state   *scriptedV7ProviderState
	binding v7FullGateProviderBinding
}

func (p *scriptedV7FullGateProvider) BindFullGateProvider(binding v7FullGateProviderBinding) error {
	p.binding = binding
	return nil
}

func (p *scriptedV7FullGateProvider) Run(_ context.Context, _, command string) (v7FullGateProviderInvocation, error) {
	if p.state == nil || p.state.next >= len(p.state.steps) {
		return v7FullGateProviderInvocation{Outcome: v7FullGateOutcomeProvider}, fmt.Errorf("%w: scripted provider exhausted", errV7FullGateProvider)
	}
	index := p.state.next
	step := p.state.steps[index]
	p.state.next++
	receipt := scriptedV7ProviderReceipt(p.binding, command, step.outcome, index)
	if step.onRun != nil {
		step.onRun()
	}
	runErr := step.err
	if runErr == nil && step.outcome != v7FullGateOutcomePassed {
		switch step.outcome {
		case v7FullGateOutcomeFailed:
			runErr = &v7FullGateOutcomeError{Outcome: step.outcome, Cause: errors.New("fixture gate red")}
		case v7FullGateOutcomeCanceled:
			runErr = context.Canceled
		case v7FullGateOutcomeTimedOut:
			runErr = context.DeadlineExceeded
		default:
			runErr = fmt.Errorf("%w: scripted provider failure", errV7FullGateProvider)
		}
	}
	return v7FullGateProviderInvocation{Output: []byte(step.output), Outcome: step.outcome, Receipt: receipt}, runErr
}

func (p *scriptedV7FullGateProvider) MatchesGateProviderReceipt(receipt *GateProviderReceipt) bool {
	return receipt != nil && v7CertifiedGateProviderReceipt(receipt) && receipt.ProviderProfile == p.binding.ProviderProfile && receipt.ProviderClosureDigest == testV7FullGateProviderReceipt.ProviderClosureDigest
}

func (p *scriptedV7FullGateProvider) FinalizeFullGateProviderOutcome(receipt GateProviderReceipt) error {
	if !p.state.artifactSynced {
		p.state.finalizedBeforeArtifact = true
	}
	p.state.finalized = append(p.state.finalized, receipt)
	return nil
}

func (*scriptedV7FullGateProvider) Close() error { return nil }

func scriptedV7ProviderReceipt(binding v7FullGateProviderBinding, command string, outcome v7FullGateOutcome, index int) GateProviderReceipt {
	receipt := testV7FullGateProviderReceipt
	receipt.Outcome = string(outcome)
	receipt.ProjectID = binding.ProjectID
	receipt.DepartureID = binding.DepartureID
	receipt.CandidateDigest = binding.CandidateDigest
	receipt.CommandDigest = v7FullGateTextDigest(command)
	receipt.Profile = binding.GateProfile
	receipt.ProviderProfile = binding.ProviderProfile
	receipt.Toolchain = binding.Toolchain
	receipt.RequestDigest = v7FullGateTextDigest(fmt.Sprintf("%s\x00%s\x00%d", binding.DepartureID, command, index))
	receipt.LifecycleID = fmt.Sprintf("scripted:scope:%d", index)
	receipt.ReceiptDigest = v7FullGateTextDigest("receipt\x00" + receipt.RequestDigest)
	receipt.ResultDigest = v7FullGateTextDigest("result\x00" + receipt.RequestDigest)
	receipt.OutputDigest = v7FullGateTextDigest("output\x00" + receipt.RequestDigest)
	return receipt
}

func containsV7ProviderOutcome(receipts []GateProviderReceipt, outcome v7FullGateOutcome) bool {
	for _, receipt := range receipts {
		if receipt.Outcome == string(outcome) {
			return true
		}
	}
	return false
}

func setScheduledPromotionFlakeGateForTest(t *testing.T, vault string, commands []string) Workflow {
	t.Helper()
	path := workflowPath(vault)
	data, body, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	data["orchestration"] = map[string]any{
		"gate": map[string]any{
			"profile":                "full",
			"harvest_commands":       commands,
			"isolation_provider":     "scripted",
			"flake_failure_patterns": []string{"FLAKE"},
			"flake_failure_action":   "rerun",
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
			"profile":            profile,
			"harvest_commands":   commands,
			"isolation_provider": "test-fixture",
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

func TestScheduledPromotionPreRefRecoveryRejectsRevokedProviderAndReusesCertifiedLedger(t *testing.T) {
	repo, vault := newLandReadyForMainAdvanceTest(t, "pre-ref-recovery.txt", "candidate\n")
	setScheduledPromotionPolicyForTest(t, vault, scheduledPromotionPromote)
	marker := filepath.Join(t.TempDir(), "gate-runs")
	wf := setScheduledPromotionGateForTest(t, vault, []string{"printf x >> " + yamlQuoteForShellTest(marker)}, "")
	armScheduledPromotionWaveForTest(t, vault, "W-0001")
	commitScheduledPromotionWorkflowForTest(t, repo, vault)
	mainBefore := strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "main"))
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run := newScheduledPromotionRunForTest(t, store, "2026-07-25T20:39:15Z")
	injected := errors.New("injected pre-ref crash")
	oldHook := scheduledPromotionAfterRefIntent
	scheduledPromotionAfterRefIntent = func() error { return injected }
	_, promoteErr := promoteScheduledWave(vault, "app", "W-0001", wf, store, &run, "daemon:test")
	scheduledPromotionAfterRefIntent = oldHook
	if !errors.Is(promoteErr, injected) {
		t.Fatalf("missing injected pre-ref failure: %v", promoteErr)
	}
	if got := mustReadIndexTest(t, marker); got != "x" {
		t.Fatalf("initial full gate runs = %q, want one", got)
	}
	durable, err := store.FindDepartureRun(run.ID)
	if err != nil || durable == nil {
		t.Fatalf("load durable pre-ref intent: %#v err=%v", durable, err)
	}

	oldProvider := newV7FullGateProvider
	defer func() { newV7FullGateProvider = oldProvider }()
	newV7FullGateProvider = func(string, string, string) (v7FullGateProvider, error) {
		return nil, errors.New("provider revoked")
	}
	resumed := *durable
	if _, err := promoteScheduledWave(vault, "app", "W-0001", wf, store, &resumed, "daemon:test"); err == nil || !strings.Contains(err.Error(), "full_gate_provider_unavailable") {
		t.Fatalf("revoked provider did not block recovery: %v", err)
	}
	newV7FullGateProvider = oldProvider

	resumed = *durable
	if _, err := promoteScheduledWave(vault, "app", "W-0001", wf, store, &resumed, "daemon:test"); err != nil {
		t.Fatalf("ledger-only recovery failed: %v", err)
	}
	if got := mustReadIndexTest(t, marker); got != "x" {
		t.Fatalf("recovery reran gate instead of using certified ledger: %q", got)
	}
	if got := strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "main")); got == mainBefore {
		t.Fatal("ledger-only recovery did not promote the exact intent")
	}
}

func TestV7ScheduledPromotionNewDepartureLedgerHitCrashAfterIntentReplaysExactProof(t *testing.T) {
	repo, vault := newLandReadyForMainAdvanceTest(t, "new-departure-ledger.txt", "candidate\n")
	setScheduledPromotionPolicyForTest(t, vault, scheduledPromotionPromote)
	marker := filepath.Join(t.TempDir(), "gate-runs")
	command := "printf x >> " + yamlQuoteForShellTest(marker) + "; test -f new-departure-ledger.txt"
	wf := setScheduledPromotionGateForTest(t, vault, []string{command}, "")
	armScheduledPromotionWaveForTest(t, vault, "W-0001")
	commitScheduledPromotionWorkflowForTest(t, repo, vault)
	stateRoot := t.TempDir()
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := scheduledPromotionGatePolicy(vault, wf)
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	originalDeparture := "departure-that-produced-reusable-proof"
	seed := runV7GateTierOnRefContext(withV7FullGateDeparture(context.Background(), originalDeparture), vault, repo, "integration/W-0001", "app", policy, store)
	if seed.Err != nil || seed.Result.Outcome != gateOutcomePassed || len(seed.ProviderReceipts) != 1 || seed.ProviderReceipts[0].DepartureID != originalDeparture {
		_ = store.Close()
		t.Fatalf("seed reusable provider proof = %#v, %v", seed, seed.Err)
	}
	if got := mustReadIndexTest(t, marker); got != "x" {
		_ = store.Close()
		t.Fatalf("seed gate executions = %q", got)
	}
	run := newScheduledPromotionRunForTest(t, store, "2026-07-25T20:39:17Z")
	if run.ID == originalDeparture {
		_ = store.Close()
		t.Fatal("fixture failed to allocate a new departure")
	}
	injected := errors.New("injected crash after new-departure ledger-only intent")
	oldHook := scheduledPromotionAfterRefIntent
	scheduledPromotionAfterRefIntent = func() error { return injected }
	_, promoteErr := promoteScheduledWave(vault, "app", "W-0001", wf, store, &run, "daemon:test")
	scheduledPromotionAfterRefIntent = oldHook
	if !errors.Is(promoteErr, injected) {
		_ = store.Close()
		t.Fatalf("new-departure intent crash = %v", promoteErr)
	}
	if got := mustReadIndexTest(t, marker); got != "x" {
		_ = store.Close()
		t.Fatalf("all-ledger hit reran provider command: %q", got)
	}
	durable, err := store.FindDepartureRun(run.ID)
	if err != nil || durable == nil || len(durable.Gate.ProviderReceipts) != 1 || durable.Gate.ProviderReceipts[0].DepartureID != originalDeparture {
		_ = store.Close()
		t.Fatalf("new departure did not preserve exact reusable proof: %#v, %v", durable, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	resumed, err := restarted.FindDepartureRun(run.ID)
	if err != nil || resumed == nil {
		t.Fatalf("restart durable intent: %#v, %v", resumed, err)
	}
	commit, err := promoteScheduledWave(vault, "app", "W-0001", wf, restarted, resumed, "daemon:test")
	if err != nil {
		t.Fatalf("restart rejected departure-agnostic reusable proof: %v", err)
	}
	if commit == "" || strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "main")) != commit {
		t.Fatalf("replay did not advance the exact intent: commit=%s", commit)
	}
	if got := mustReadIndexTest(t, marker); got != "x" {
		t.Fatalf("restart reran provider command: %q", got)
	}
}

func TestScheduledPromotionPreRefRecoveryRevalidatesPolicyInsideMaterialEpoch(t *testing.T) {
	repo, vault := newLandReadyForMainAdvanceTest(t, "pre-ref-epoch-policy.txt", "candidate\n")
	setScheduledPromotionPolicyForTest(t, vault, scheduledPromotionPromote)
	wf := setScheduledPromotionGateForTest(t, vault, []string{"test -f pre-ref-epoch-policy.txt"}, "")
	armScheduledPromotionWaveForTest(t, vault, "W-0001")
	commitScheduledPromotionWorkflowForTest(t, repo, vault)
	mainBefore := strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "main"))
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run := newScheduledPromotionRunForTest(t, store, "2026-07-25T20:39:20Z")
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
		t.Fatalf("load durable pre-ref intent: %#v err=%v", durable, err)
	}

	oldObserver := v7MaterialEpochLockObserver
	defer func() { v7MaterialEpochLockObserver = oldObserver }()
	changed := false
	v7MaterialEpochLockObserver = func() {
		if changed {
			return
		}
		changed = true
		writerLock, err := acquireV7MaterialEpochLock(vault)
		if err != nil {
			t.Fatalf("acquire policy writer material epoch: %v", err)
		}
		defer func() { _ = writerLock.Close() }()
		setScheduledPromotionGateForTest(t, vault, []string{"test -f policy-revoked.txt"}, "")
	}
	resumed := *durable
	if _, err := promoteScheduledWave(vault, "app", "W-0001", wf, store, &resumed, "daemon:test"); err == nil || !strings.Contains(err.Error(), "full_gate_contract_drift:gate") {
		t.Fatalf("policy mutation in material-epoch gap did not block recovery: %v", err)
	}
	if !changed {
		t.Fatal("test did not mutate policy at material epoch acquisition")
	}
	if after := strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "main")); after != mainBefore {
		t.Fatalf("epoch policy drift moved main: before=%s after=%s", mainBefore, after)
	}
}

func TestValidateV7PromotionGatePolicyBoundsHostileConfig(t *testing.T) {
	commands := make([]string, v7PromotionGateMaxCommands+1)
	for i := range commands {
		commands[i] = "true"
	}
	if err := validateV7PromotionGatePolicy(GateTierPolicy{HarvestCommands: commands}); err == nil {
		t.Fatal("oversized harvest command list was accepted")
	}
	if err := validateV7PromotionGatePolicy(GateTierPolicy{HarvestCommands: []string{strings.Repeat("x", v7PromotionGateMaxCommandBytes+1)}}); err == nil {
		t.Fatal("oversized harvest command was accepted")
	}
}

func TestPromotionGateTranscriptAggregateIsBounded(t *testing.T) {
	transcript := v7GateBoundedOutput{max: 32, truncationNotice: "[truncated]"}
	for range 64 {
		if _, err := transcript.Write([]byte("0123456789")); err != nil {
			t.Fatal(err)
		}
	}
	got := string(transcript.Bytes())
	if !strings.Contains(got, "[truncated]") || len(got) > 32+len("[truncated]") {
		t.Fatalf("aggregate transcript was not bounded: %d bytes %q", len(got), got)
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
