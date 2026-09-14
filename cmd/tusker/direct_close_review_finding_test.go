package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func prepareDirectCloseReviewFixture(t *testing.T) (*serveServer, Note, string, ReviewResult) {
	t.Helper()
	server := newServeEmptyNeedsFixture(t)
	writeServeCloseableReviewTask(t, server.vaultPath, "APP-T-0001", "low")
	runGitDir(t, server.repoRoot, "init", "-q")
	path := filepath.Join(server.vaultPath, "work", "tasks", "APP-T-0001.md")
	data, body, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	body = replaceSection(body, "## Verification", "| Covers | Check | Result | Notes |\n|---|---|---|---|\n| A1 | go test ./cmd/tusker -run TestServeCloseDefaultsHumanActorForHumanRequiredRisk -count=1 | pass | fixture proof |")
	data["work_revision"] = 1
	data["source_sha"] = "source-sha"
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(path, content); err != nil {
		t.Fatal(err)
	}
	if err := ensureV7TestReceiptScope(server.vaultPath, "APP-T-0001"); err != nil {
		t.Fatal(err)
	}
	data, body, err = parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	ownedPath := filepath.ToSlash(filepath.Join(".tusker-test-fixtures", "app-t-0001.txt"))
	if err := ensureDir(filepath.Dir(filepath.Join(server.repoRoot, filepath.FromSlash(ownedPath)))); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(server.repoRoot, filepath.FromSlash(ownedPath)), "stable verification fixture\n"); err != nil {
		t.Fatal(err)
	}
	data["owned_paths"] = []string{ownedPath}
	data["contract_fingerprint"] = directWaveTaskContractFingerprint(data, body)
	content, err = serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(path, content); err != nil {
		t.Fatal(err)
	}
	note, err := resolveV7Note(server.vaultPath, "APP-T-0001", "task")
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := v7RepoRoot(server.vaultPath)
	identity, _, err := v7VerificationReceiptIdentityFor(server.vaultPath, note, repoRoot, nil)
	if err != nil {
		t.Fatal(err)
	}
	rows := parseV7VerificationRows(note.Body)
	if len(rows) != 1 {
		t.Fatalf("expected one verification row, got %#v", rows)
	}
	rows[0].Notes = appendV7VerificationExecutionNote(rows[0], v7VerificationCommandObservation{
		ExitCode: 0, Digest: "sha256:test-fixture", MatchCount: 1,
	}, identity)
	body = replaceSection(note.Body, "## Verification", renderV7VerificationTable(rows))
	data = cloneMap(note.Data)
	data["state_rev"] = v7StateRev(data, body)
	content, err = serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(path, content); err != nil {
		t.Fatal(err)
	}
	note, err = resolveV7Note(server.vaultPath, "APP-T-0001", "task")
	if err != nil {
		t.Fatal(err)
	}
	material, err := v7CloseCurrentMaterial(server.vaultPath, note)
	if err != nil {
		t.Fatal(err)
	}
	result := ReviewResult{
		Schema: reviewResultSchema, ProjectID: "app", TaskID: "APP-T-0001",
		TaskStateRev: stringField(note.Data, "state_rev"), WorkRevision: 1,
		ImplementationSHA: "source-sha", AttemptID: "review-1", Actor: "reviewer:agent",
		Runner: "codex", RunnerProfile: "review", WorkerPolicyFP: "sha256:" + strings.Repeat("a", 64),
		Covers: []string{"A1"}, ProofFingerprint: "sha256:" + strings.Repeat("b", 64),
		GateFingerprint: "sha256:" + strings.Repeat("c", 64), MaterialFingerprint: material,
		Verdict: "changes_requested", Summary: "blocking finding", Findings: []string{softwareFactoryFinding(material, "blocking")},
		CreatedAt: "2026-09-14T00:00:00Z",
	}
	result.ResultRevision = reviewResultFingerprint(result)
	if _, err := server.store.SaveReviewResult(result); err != nil {
		t.Fatal(err)
	}
	return server, note, material, result
}

func TestDirectCloseRefusesDurableBlockingFinding(t *testing.T) {
	server, _, _, _ := prepareDirectCloseReviewFixture(t)
	writeServeTask(t, server.vaultPath, serveTaskSeed{
		ID: "APP-T-0002", Epic: "APP", Title: "Dependent", Status: "ready", Risk: "low", Priority: "p2",
		Dependencies: []string{"APP-T-0001:hard"},
	})

	err := closeV7Cmd(Args{
		"vault": server.vaultPath, "quiet": "true", "local": "true", "id": "APP-T-0001", "by": "reviewer:independent",
	})
	if err == nil || !strings.Contains(err.Error(), "blocking review finding") {
		t.Fatalf("direct close bypassed unresolved durable finding: %v", err)
	}
	note, err := resolveV7Note(server.vaultPath, "APP-T-0001", "task")
	if err != nil {
		t.Fatal(err)
	}
	if got := stringField(note.Data, "status"); got == "done" {
		t.Fatalf("refused direct close changed task status to done")
	}
	if _, err := reconcileV7ControlProjections(server.vaultPath, []string{"APP-T-0002"}, "test:review", "test"); err != nil {
		t.Fatal(err)
	}
	dependent, err := resolveV7Note(server.vaultPath, "APP-T-0002", "task")
	if err != nil {
		t.Fatal(err)
	}
	if got := stringField(dependent.Data, "readiness"); got != "blocked_by_dependency" {
		t.Fatalf("dependent reconciliation released work after refused close: readiness=%q", got)
	}
}

func TestServeCloseRefusesDurableBlockingFinding(t *testing.T) {
	server, _, _, _ := prepareDirectCloseReviewFixture(t)
	var result serveActionResult
	servePost(t, server, "/api/tasks/APP-T-0001/close", `{}`, &result)
	if result.OK || !result.Refused || !strings.Contains(result.Reason, "blocking review finding") {
		t.Fatalf("serve close bypassed unresolved durable finding: %#v", result)
	}
}

func TestDirectCloseAcceptsIndependentExactMaterialFindingClosure(t *testing.T) {
	server, note, oldMaterial, prior := prepareDirectCloseReviewFixture(t)
	if err := writeText(filepath.Join(server.repoRoot, ".tusker-test-fixtures", "app-t-0001.txt"), "repair complete\n"); err != nil {
		t.Fatal(err)
	}
	current, err := resolveV7Note(server.vaultPath, "APP-T-0001", "task")
	if err != nil {
		t.Fatal(err)
	}
	newMaterial, err := v7CloseCurrentMaterial(server.vaultPath, current)
	if err != nil {
		t.Fatal(err)
	}
	if newMaterial == oldMaterial {
		t.Fatal("fixture repair did not change current material")
	}
	// The repaired material needs a fresh verification receipt as well as a
	// fresh durable review result. Updating the receipt advances state_rev;
	// bind the candidate to that exact revision below.
	current, err = resolveV7Note(server.vaultPath, "APP-T-0001", "task")
	if err != nil {
		t.Fatal(err)
	}
	identity, _, err := v7VerificationReceiptIdentityFor(server.vaultPath, current, v7RepoRoot(server.vaultPath), nil)
	if err != nil {
		t.Fatal(err)
	}
	rows := parseV7VerificationRows(current.Body)
	rows[0].Notes = appendV7VerificationExecutionNote(rows[0], v7VerificationCommandObservation{
		ExitCode: 0, Digest: "sha256:repair-fixture", MatchCount: 1,
	}, identity)
	body := replaceSection(current.Body, "## Verification", renderV7VerificationTable(rows))
	data := cloneMap(current.Data)
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(current.AbsolutePath, content); err != nil {
		t.Fatal(err)
	}
	note, err = resolveV7Note(server.vaultPath, "APP-T-0001", "task")
	if err != nil {
		t.Fatal(err)
	}
	if err := server.store.SaveAttempt(RunAttempt{
		AttemptID: prior.AttemptID, ProjectID: prior.ProjectID, RecordID: prior.TaskID,
		Lane: runLaneReview, WorkRevision: prior.WorkRevision, StartedAt: "2026-09-14T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	if err := server.store.SaveAttempt(RunAttempt{
		AttemptID: "review-2", ProjectID: prior.ProjectID, RecordID: prior.TaskID,
		Lane: runLaneReview, WorkRevision: prior.WorkRevision, StartedAt: "2026-09-14T01:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	candidate := prior
	candidate.AttemptID = "review-2"
	candidate.TaskStateRev = stringField(note.Data, "state_rev")
	candidate.MaterialFingerprint = newMaterial
	candidate.Verdict = "pass"
	candidate.Findings = nil
	candidate.Summary = "independent repair verified"
	candidate.ClosedFindings = []reviewerFindingClosure{{
		Schema: reviewerFindingClosureSchema, ID: "F-001",
		ClosureCondition: "re-run the exact proof and attach its receipt",
		Evidence:         []string{"repair-receipt"}, MaterialFingerprint: newMaterial,
	}}
	candidate.CreatedAt = "2026-09-14T01:00:00Z"
	candidate.ResultRevision = reviewResultFingerprint(candidate)
	if _, err := server.store.SaveReviewResult(candidate); err != nil {
		t.Fatal(err)
	}
	if err := closeV7Cmd(Args{
		"vault": server.vaultPath, "quiet": "true", "local": "true", "id": "APP-T-0001", "by": "reviewer:independent",
	}); err != nil {
		t.Fatalf("valid independent exact-material closure was refused: %v", err)
	}
	closed, err := resolveV7Note(server.vaultPath, "APP-T-0001", "task")
	if err != nil {
		t.Fatal(err)
	}
	if got := stringField(closed.Data, "status"); got != "done" {
		t.Fatalf("valid closure did not close task: status=%q", got)
	}
}
