package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func orchestrationGitRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	initializeOrchestrationGitRepo(t, repo)
	return repo
}

func initializeOrchestrationGitRepo(t *testing.T, repo string) {
	t.Helper()
	runGitDir(t, repo, "init", "-b", "main")
	runGitDir(t, repo, "config", "user.email", "test@example.com")
	runGitDir(t, repo, "config", "user.name", "Tusker Test")
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, repo, "add", "tracked.txt")
	runGitDir(t, repo, "commit", "-m", "base")
}

func TestClaimDeadHolderTakeover(t *testing.T) {
	store, candidateRun := ownershipStoreFixture(t, "APP-T-0002")
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	holder := candidateRun
	holder.RecordID, holder.ItemID = "APP-T-0001", "APP-T-0001"
	holder.LeaseState, holder.LeaseOwner = string(LeaseStateRunning), "dead-lane"
	holder.LeaseExpiresAt = now.Add(-time.Minute).Format(time.RFC3339)
	holder.LastHeartbeatAt = now.Add(-2 * time.Minute).Format(time.RFC3339)
	if err := store.UpsertRun(holder); err != nil {
		t.Fatal(err)
	}
	candidate := Note{Data: map[string]any{"id": candidateRun.ItemID, "owned_paths": []string{"migrations/0014_new.sql"}}}
	notes := map[string]Note{holder.ItemID: {Data: map[string]any{"id": holder.ItemID, "owned_paths": []string{"migrations"}}}, candidateRun.ItemID: candidate}
	service := newRunOwnershipService(store).withOwnedPathContext("", candidate, notes)
	service.now = func() time.Time { return now }
	service.projectConcurrencyLimit = 2
	result, err := service.claim(candidateRun, "takeover-lane")
	if err != nil || !result.Claimed {
		t.Fatalf("takeover failed: %#v %v", result, err)
	}
	former, _ := store.FindRun(holder.ItemID)
	if former.LeaseState != string(LeaseStateInterrupted) {
		t.Fatalf("dead holder not released: %#v", former)
	}
}

func TestClaimRefusalShape(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	candidate := Note{Data: map[string]any{"id": "APP-T-0002", "owned_paths": []string{"migrations/0014.sql"}}}
	notes := map[string]Note{"APP-T-0001": {Data: map[string]any{"id": "APP-T-0001", "owned_paths": []string{"migrations"}}}}
	runs := []RunStatus{{ItemID: "APP-T-0001", LeaseOwner: "lane-a", LeaseState: string(LeaseStateRunning), LeaseExpiresAt: now.Add(time.Hour).Format(time.RFC3339), StartedAt: now.Add(-time.Minute).Format(time.RFC3339)}}
	conflict, found := ownedPathConflict(candidate, notes, runs, now)
	if !found {
		t.Fatal("expected conflict")
	}
	err := tuskerError("OWNED_PATH_CONFLICT", "claim refused", withContext(conflict))
	var typed *TuskerError
	if !errors.As(err, &typed) || typed.Code != "OWNED_PATH_CONFLICT" {
		t.Fatalf("unstable error shape: %#v", err)
	}
	for _, field := range []string{"holder", "task_id", "lease_age", "liveness"} {
		if !strings.Contains(toString(typed.Context), field) {
			t.Fatalf("missing structured field %s: %#v", field, typed.Context)
		}
	}
}

func TestRunSubmitEndStateRequired(t *testing.T) {
	if _, err := captureRunEndState(t.TempDir(), "", "", "", time.Now()); err == nil || !strings.Contains(err.Error(), "gate_verdicts") {
		t.Fatalf("missing end state was not actionable: %v", err)
	}
}

func TestRunSubmitHarnessGitFactsAuthoritative(t *testing.T) {
	repo := orchestrationGitRepo(t)
	state, err := captureRunEndState(repo, "A1=pass,A2=pass", "wrong", "deadbeef", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if state.Branch != "main" || len(state.HeadSHA) != 40 || state.Dirty || len(state.Discrepancies) != 2 {
		t.Fatalf("unexpected authoritative end state: %#v", state)
	}
}

func TestRunSubmitEndStateAttemptProjection(t *testing.T) {
	store, run := ownershipStoreFixture(t, "APP-T-END")
	service := newRunOwnershipService(store)
	claimed, err := service.claim(run, "owner")
	if err != nil || !claimed.Claimed {
		t.Fatal(err)
	}
	state := RunEndState{Schema: "tusker.run-end-state/v1", Branch: "main", HeadSHA: strings.Repeat("a", 40), WorktreePath: "/tmp/repo", GateVerdicts: map[string]string{"A1": "pass"}, CapturedAt: time.Now().UTC().Format(time.RFC3339)}
	if _, err := service.finishWithEndState(run.RecordID, "owner", AttemptOutcomeSucceeded, "done", "A1 pass", "", &state); err != nil {
		t.Fatal(err)
	}
	attempts, err := store.ListAttemptsForRun(run.ProjectID, run.RecordID)
	if err != nil || len(attempts) != 1 || attempts[0].EndState.HeadSHA != state.HeadSHA {
		t.Fatalf("end state not projected: %#v %v", attempts, err)
	}
}

func TestStreamBoardProjection(t *testing.T) {
	rows := []streamRow{{Lane: "execute", TaskID: "APP-T-0001", Runner: "codex", WorktreePath: "/tmp/app", Branch: "task/a", OwnedPaths: []string{"cmd"}, LastHeartbeat: "2026-07-20T00:00:00Z", HeartbeatAge: "1m", Status: "running"}}
	board := renderStreamBoard(rows)
	for _, want := range []string{"APP-T-0001", "task/a", "/tmp/app", "cmd", "running"} {
		if !strings.Contains(board, want) {
			t.Fatalf("board missing %s: %s", want, board)
		}
	}
}

func TestStreamsJSON(t *testing.T) {
	row := streamRow{TaskID: "APP-T-0001", Status: "running", Branch: "task/a"}
	if row.TaskID == "" || row.Status == "" || row.Branch == "" {
		t.Fatalf("unstable row: %#v", row)
	}
}

func TestStreamBoardStaleFlag(t *testing.T) {
	now := time.Now().UTC()
	run := RunStatus{LeaseState: string(LeaseStateRunning), LeaseExpiresAt: now.Add(-time.Minute).Format(time.RFC3339)}
	if runFreshness(&run, now) != "stale" {
		t.Fatalf("expired lane was not stale")
	}
}

func TestStreamRefreshIgnoresNonVaultDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := refreshStreamBoardForVault(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "dashboards", "streams.md")); !os.IsNotExist(err) {
		t.Fatalf("stream refresh wrote outside a Tusker vault: %v", err)
	}
}

func TestGateLedgerCheck(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	entry := GateLedgerEntry{ID: "gate-1", ProjectID: "app", TreeHash: "tree", Command: "go test ./...", Profile: "default", PassedAt: time.Now().UTC().Format(time.RFC3339)}
	if err := store.RecordGateLedger(entry); err != nil {
		t.Fatal(err)
	}
	hit, err := store.FindGateLedger("app", "tree", entry.Command, entry.Profile)
	if err != nil || hit == nil || hit.ID != entry.ID {
		t.Fatalf("ledger miss: %#v %v", hit, err)
	}
}

func TestGateLedgerRecordAndInvalidate(t *testing.T) {
	repo := orchestrationGitRepo(t)
	first, err := workspaceTreeStateHash(repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := workspaceTreeStateHash(repo)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("tracked file change did not invalidate tree hash")
	}
	if err := os.WriteFile(filepath.Join(repo, "new-source.go"), []byte("package sample\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	third, err := workspaceTreeStateHash(repo)
	if err != nil {
		t.Fatal(err)
	}
	if second == third {
		t.Fatal("untracked source file did not invalidate tree hash")
	}
	if err := os.Symlink("tracked.txt", filepath.Join(repo, "tracked-link")); err != nil {
		t.Fatal(err)
	}
	withLink, err := workspaceTreeStateHash(repo)
	if err != nil {
		t.Fatal(err)
	}
	if third == withLink {
		t.Fatal("untracked symlink did not invalidate tree hash")
	}
	if err := os.MkdirAll(filepath.Join(repo, ".tusker", "work", "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".tusker", "work", "tasks", "TASK.md"), []byte("status: review\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fourth, err := workspaceTreeStateHash(repo)
	if err != nil {
		t.Fatal(err)
	}
	if withLink != fourth {
		t.Fatal("mutable Tusker task state invalidated source gate hash")
	}
}

func TestProofRowLedgerCitation(t *testing.T) {
	if !v7VerificationCheckLooksExact("ledger: gate-abc123") {
		t.Fatal("ledger citation rejected")
	}
}

func TestNamespaceLintConfig(t *testing.T) {
	pattern, err := compileNamespaceGlob("**/migrations/*_*.sql")
	if err != nil || !pattern.MatchString("db/migrations/0014_add.sql") {
		t.Fatalf("configured glob failed: %v", err)
	}
}

func TestNamespaceLintDuplicate(t *testing.T) {
	repo := t.TempDir()
	vault := filepath.Join(repo, ".tusker")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := writeDefaultWorkflow(vault); err != nil {
		t.Fatal(err)
	}
	workflowPath := filepath.Join(vault, "WORKFLOW.md")
	data, body, err := parseFrontmatterMustRead(workflowPath)
	if err != nil {
		t.Fatal(err)
	}
	data["orchestration"] = map[string]any{"namespace_lints": []any{map[string]any{"name": "migrations", "glob": "migrations/*_*.sql", "capture_regex": `(\d{4})_`, "naming_recommendation": "prefer timestamp names"}}}
	content, err := serializeDocument(data, body, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(workflowPath, content); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "migrations"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"0014_a.sql", "0014_b.sql"} {
		if err := os.WriteFile(filepath.Join(repo, "migrations", name), []byte("-- migration\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	issues := validateCollisionProneNamespaces(vault)
	if len(issues) != 1 || issues[0].Code != "NAMESPACE_COLLISION" || !strings.Contains(issues[0].Message, "0014_a.sql") || !strings.Contains(issues[0].Message, "0014_b.sql") {
		t.Fatalf("duplicate namespace not diagnosed: %#v", issues)
	}
}

func TestPlanBranchFacts(t *testing.T) {
	repo := orchestrationGitRepo(t)
	runGitDir(t, repo, "checkout", "-b", "task/a")
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("branch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, repo, "commit", "-am", "branch")
	facts, err := captureGitBranchFacts(repo, "main", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if facts.Branch != "task/a" || facts.Ahead != 1 || facts.Behind != 0 {
		t.Fatalf("bad branch facts: %#v", facts)
	}
}

func TestPlanBranchAgeWarning(t *testing.T) {
	plan := automationDispatchPlan{Warnings: []automationPlanWarning{{Code: "BRANCH_AGE", Message: "old"}}}
	if len(plan.Warnings) != 1 || plan.Warnings[0].Code != "BRANCH_AGE" {
		t.Fatalf("warning shape changed: %#v", plan)
	}
}

func TestPlanBranchFactsAbsent(t *testing.T) {
	if _, err := captureGitBranchFacts(t.TempDir(), "main", time.Now()); err == nil {
		t.Fatal("non-git workspace unexpectedly produced facts")
	}
}

func TestBatchGateFirstActionableFailure(t *testing.T) {
	excerpt := actionableGateFailure("noise\nFAIL TestThing\nerror: boom\nmore", errors.New("exit 1"))
	if !strings.Contains(excerpt, "FAIL TestThing") || !strings.Contains(excerpt, "error: boom") || strings.Contains(excerpt, "noise") {
		t.Fatalf("bad excerpt: %q", excerpt)
	}
}

func TestBatchGateSchedule(t *testing.T) {
	repo := orchestrationGitRepo(t)
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	daemon := &Daemon{store: store}
	wf := defaultWorkflow()
	wf.Orchestration.BatchGate = BatchGatePolicy{Enabled: true, PeriodHours: 24, Commands: []string{"true"}}
	project := RegisteredProject{ProjectID: "app", RepoRoot: repo}
	now := time.Now().UTC()
	if err := daemon.scheduleBatchGateIfDue(project, wf, now); err != nil {
		t.Fatal(err)
	}
	if err := daemon.scheduleBatchGateIfDue(project, wf, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	latest, err := store.latestBatchGateRun("app")
	if err != nil || latest == nil {
		t.Fatalf("scheduled run missing: %#v %v", latest, err)
	}
}

func TestBatchGateRedSpawnsRepair(t *testing.T) {
	repo := orchestrationGitRepo(t)
	vault := filepath.Join(repo, ".tusker")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	daemon := &Daemon{store: store}
	project := RegisteredProject{ProjectID: "app", RepoRoot: repo, VaultRoot: vault}
	policy := BatchGatePolicy{Enabled: true, Commands: []string{"printf 'FAIL repair me\\n'; exit 1"}, MaxRepairs: 1}
	for i := 0; i < 2; i++ {
		daemon.executeBatchGate(project, policy, BatchGateRun{ID: "batch-red-" + toString(i), ProjectID: "app", StartedAt: time.Now().UTC().Format(time.RFC3339)})
	}
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, task := range idx.Tasks {
		if stringField(task.Data, "batch_gate_command") == policy.Commands[0] {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("repeated red created %d repair tasks", count)
	}
}

func TestBatchGateGreenRecorded(t *testing.T) {
	repo := orchestrationGitRepo(t)
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	daemon := &Daemon{store: store}
	project := RegisteredProject{ProjectID: "app", RepoRoot: repo, VaultRoot: filepath.Join(repo, ".tusker")}
	policy := BatchGatePolicy{Enabled: true, Commands: []string{"true"}, FeatureProfile: "canonical"}
	daemon.executeBatchGate(project, policy, BatchGateRun{ID: "batch-green", ProjectID: "app", StartedAt: time.Now().UTC().Format(time.RFC3339)})
	tree, _ := workspaceTreeStateHash(repo)
	hit, err := store.FindGateLedger("app", tree, "true", "canonical")
	if err != nil || hit == nil {
		t.Fatalf("green gate was not ledgered: %#v %v", hit, err)
	}
}

func TestIntegratorWorkKindOwnsSharedNamespaces(t *testing.T) {
	wf := defaultWorkflow()
	wf.Orchestration.SharedNamespaces = []string{"migrations", "go.sum"}
	notes := map[string]Note{"APP-T-0001": {Data: map[string]any{"id": "APP-T-0001", "work_kind": "integrator", "owned_paths": []string{"generated"}}}}
	owned := orchestrationOwnedPathNotes(notes, wf)
	paths := normalizeList(owned["APP-T-0001"].Data["owned_paths"])
	for _, want := range []string{"generated", "migrations", "go.sum"} {
		if !containsString(paths, want) {
			t.Fatalf("integrator missing %s: %#v", want, paths)
		}
	}
}

func TestIntegratorClaimOwnsNamespaces(t *testing.T) { TestIntegratorWorkKindOwnsSharedNamespaces(t) }

func TestIntegratorPacketComposesEndStatesAndOverlap(t *testing.T) {
	reports := []integratorLaneReport{
		{TaskID: "APP-T-0001", EndState: RunEndState{Branch: "task/a", HeadSHA: strings.Repeat("a", 40), WorktreePath: "/tmp/a", GateVerdicts: map[string]string{"A1": "pass"}}, Files: []string{"shared.go", "a.go"}},
		{TaskID: "APP-T-0002", EndState: RunEndState{Branch: "task/b", HeadSHA: strings.Repeat("b", 40), WorktreePath: "/tmp/b", Dirty: true, GateVerdicts: map[string]string{"A1": "pass"}}, Files: []string{"shared.go", "b.go"}},
	}
	overlap := integratorOverlapRows(reports)
	if len(overlap) != 1 || !strings.Contains(overlap[0], "shared.go") {
		t.Fatalf("overlap missing: %#v", overlap)
	}
	if formatGateVerdicts(reports[0].EndState.GateVerdicts) != "A1=pass" {
		t.Fatal("gate verdicts missing")
	}
}

func TestIntegratorPacketMergeBrief(t *testing.T) { TestIntegratorPacketComposesEndStatesAndOverlap(t) }

func TestIntegratorPacketRequiresDoctrine(t *testing.T) {
	packet := integratorPacket(t.TempDir(), Note{Data: map[string]any{"id": "APP-T-0008"}}, v7Index{})
	if !strings.Contains(packet, "references/INTEGRATION_MERGE.md") {
		t.Fatalf("doctrine route missing: %s", packet)
	}
}

func TestIntegratorPacketRequiredReads(t *testing.T) { TestIntegratorPacketRequiresDoctrine(t) }
