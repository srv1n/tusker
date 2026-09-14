package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestSoftwareFactoryProofProductionExecutorRejectsZeroMatch(t *testing.T) {
	vault, id := acceptTestVaultWithTask(t)
	repo := v7RepoRoot(vault)
	if err := writeText(filepath.Join(repo, "go.mod"), "module fixture\n\ngo 1.22\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(repo, "proof_test.go"), "package fixture\nimport \"testing\"\nfunc TestExists(t *testing.T) {}\n"); err != nil {
		t.Fatal(err)
	}
	acceptPendingCommand(t, vault, id, "printf '=== RUN TestExists\\n'; go test ./... -run '^DefinitelyNoSuchTest$' -count=1 -v")
	task, err := resolveV7Note(vault, id, "task")
	if err != nil {
		t.Fatal(err)
	}
	_, _, failures, err := executeV7CommandVerificationRows(vault, task, nil, "reviewer:gate", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(failures) != 1 || !strings.Contains(failures[0].Message, "unsupported selector runner or shell syntax") {
		t.Fatalf("compound zero-match command retained PASS: %#v", failures)
	}
	rows := parseV7VerificationRows(mustBody(t, filepath.Join(vault, "work", "tasks", id+".md")))
	if len(rows) != 1 || rows[0].Result != "fail" || !strings.Contains(rows[0].Notes, "match_count=0") {
		t.Fatalf("zero-match receipt was not retained as failure: %#v", rows)
	}
}

func TestSoftwareFactoryProofProductionReceiptBindsCurrentMaterial(t *testing.T) {
	vault, id := acceptTestVaultWithTask(t)
	repo := v7RepoRoot(vault)
	if err := writeText(filepath.Join(repo, "go.mod"), "module fixture\n\ngo 1.22\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(repo, "proof_test.go"), "package fixture\nimport \"testing\"\nfunc TestExists(t *testing.T) {}\n"); err != nil {
		t.Fatal(err)
	}
	authored, err := resolveV7Note(vault, id, "task")
	if err != nil {
		t.Fatal(err)
	}
	if err := updateV7TaskCmd(Args{
		"vault": vault, "quiet": "true", "_pos0": id, "by": "agent:builder",
		"if-revision": stringField(authored.Data, "state_rev"), "owned-paths": "proof_test.go",
	}); err != nil {
		t.Fatal(err)
	}
	acceptPendingCommand(t, vault, id, "go test ./... -run '^TestExists$' -count=1 -v")
	task, err := resolveV7Note(vault, id, "task")
	if err != nil {
		t.Fatal(err)
	}
	beforeMaterial, beforeMaterialErr := v7VerificationCurrentScopedMaterial(vault, task)
	fresh, report, failures, err := executeV7CommandVerificationRows(vault, task, nil, "reviewer:gate", true)
	if err != nil || len(failures) != 0 || report.Status != "satisfied" {
		t.Fatalf("current matched proof rejected: status=%s failures=%#v err=%v", report.Status, failures, err)
	}
	rows := parseV7VerificationRows(fresh.Body)
	currentMaterial, materialErr := v7VerificationCurrentScopedMaterial(vault, fresh)
	if len(rows) != 1 || !v7VerificationReceiptCurrent(fresh, rows[0], currentMaterial, materialErr) || !strings.Contains(rows[0].Notes, "match_count=1") || !strings.Contains(rows[0].Notes, "material=") {
		status, _ := gitOutputTrim(repo, "status", "--short", "--untracked-files=all")
		t.Fatalf("executor did not bind the current proof identity: before=%q beforeErr=%v material=%q err=%v status=%q rows=%#v", beforeMaterial, beforeMaterialErr, currentMaterial, materialErr, status, rows)
	}
	forged := fresh
	forged.Data = map[string]any{}
	for key, value := range fresh.Data {
		forged.Data[key] = value
	}
	forged.Data["contract_fingerprint"] = "sha256:" + strings.Repeat("0", 64)
	if v7ProofGreenForAccept(forged, computeV7ProofReport(vault, forged, v7Index{})) {
		t.Fatal("accept retained a receipt bound to a stale task contract")
	}
	for _, result := range []string{"pending", "fail", "waived"} {
		t.Run("waiver cannot cover command "+result, func(t *testing.T) {
			variant := fresh
			variant.Data = cloneMap(fresh.Data)
			variant.Data["proof_status"] = "waived"
			variantRows := parseV7VerificationRows(fresh.Body)
			variantRows[0].Result = result
			variant.Body = replaceSection(fresh.Body, "## Verification", renderV7VerificationTable(variantRows))
			report := computeV7ProofReport(vault, variant, v7Index{})
			if v7ProofGreenForAccept(variant, report) || enforceV7AcceptanceClose(vault, variant, v7Index{}, false) == nil {
				t.Fatalf("task waiver covered executable row result %q: %#v", result, report)
			}
		})
	}
	if err := writeText(filepath.Join(repo, "proof_test.go"), "package fixture\nimport \"testing\"\nfunc TestExists(t *testing.T) {}\nfunc TestAmended(t *testing.T) {}\n"); err != nil {
		t.Fatal(err)
	}
	setAutomationV7TaskFields(t, vault, id, map[string]any{"status": "review", "proof_status": "waived", "readiness": "ready"})
	drifted, err := resolveV7Note(vault, id, "task")
	if err != nil {
		t.Fatal(err)
	}
	driftReport := computeV7ProofReport(vault, drifted, v7Index{})
	if v7ProofGreenForAccept(drifted, driftReport) || enforceV7AcceptanceClose(vault, drifted, v7Index{}, false) == nil {
		t.Fatalf("scoped source drift retained waived command authority: %#v", driftReport)
	}
	if err := acceptV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": id, "by": "reviewer:independent"}); err == nil {
		t.Fatal("accept closed scoped source drift under a task waiver")
	}
	if err := closeV7Cmd(Args{"vault": vault, "quiet": "true", "local": "true", "id": id, "by": "reviewer:independent"}); err == nil {
		t.Fatal("close closed scoped source drift under a task waiver")
	}
	project := registerAutomationTestProject(t, vault)
	if err := (&Daemon{}).closeExternalLoopTask(project, WorkflowFile{Data: defaultWorkflow()}, drifted, externalCollectReport{
		ReviewResult: &externalReviewResult{Verdict: "approve", Risk: "low"},
	}); err == nil {
		t.Fatal("daemon external close accepted a stale scoped-source receipt")
	}
	if err := updateV7TaskCmd(Args{
		"vault": vault, "quiet": "true", "_pos0": id, "by": "agent:builder",
		"if-revision": stringField(drifted.Data, "state_rev"), "title": "Amended proof contract",
	}); err != nil {
		t.Fatal(err)
	}
	amended, err := resolveV7Note(vault, id, "task")
	if err != nil {
		t.Fatal(err)
	}
	rows = parseV7VerificationRows(amended.Body)
	if len(rows) != 1 || rows[0].Result != "pending" || !strings.Contains(rows[0].Notes, "invalidated by task contract amendment") {
		t.Fatalf("material amendment retained a stale passed receipt: %#v", rows)
	}
}

func softwareFactoryFinding(material string, kind string) string {
	return softwareFactoryFindingWithScope(material, kind, "")
}

func softwareFactoryFindingWithScope(material, kind, repairScope string) string {
	scope := ""
	if repairScope != "" {
		scope = `,"repair_scope":"` + repairScope + `"`
	}
	return `{"schema":"` + reviewerFindingSchema + `","id":"F-001","kind":"` + kind + `","acceptance":["A1"],"evidence":["receipt-1"],"consequence":"acceptance is unproven","closure_condition":"re-run the exact proof and attach its receipt"` + scope + `,"material_fingerprint":"` + material + `"}`
}

func softwareFactoryReviewResult(finding string, material string) ReviewResult {
	result := ReviewResult{
		Schema:              reviewResultSchema,
		ProjectID:           "project",
		TaskID:              "APP-T-0001",
		TaskStateRev:        "sha256:task",
		WorkRevision:        1,
		ImplementationSHA:   "abc123",
		AttemptID:           "review-1",
		Actor:               "reviewer:agent",
		Runner:              "codex",
		RunnerProfile:       "review",
		WorkerPolicyFP:      "sha256:" + strings.Repeat("a", 64),
		Covers:              []string{"A1"},
		ProofFingerprint:    "sha256:proof",
		GateFingerprint:     "sha256:gates",
		MaterialFingerprint: material,
		Verdict:             "changes_requested",
		Summary:             "actionable finding",
		Findings:            []string{finding},
		EvidenceRefs:        []string{},
		CreatedAt:           "2026-09-14T00:00:00Z",
	}
	result.ResultRevision = reviewResultFingerprint(result)
	return result
}

func TestSoftwareFactoryProofRejectsUnresolvedReviewFindings(t *testing.T) {
	material := "sha256:" + strings.Repeat("b", 64)
	valid := softwareFactoryReviewResult(softwareFactoryFinding(material, "blocking"), material)
	if err := validatePersistedReviewResult(valid); err != nil {
		t.Fatalf("valid structured blocking finding rejected: %v", err)
	}
	for name, mutate := range map[string]func(*ReviewResult){
		"freeform": func(result *ReviewResult) {
			result.Findings = []string{"fix acceptance"}
		},
		"advisory only": func(result *ReviewResult) {
			result.Findings = []string{softwareFactoryFinding(material, "advisory")}
		},
		"stale material": func(result *ReviewResult) {
			result.Findings = []string{softwareFactoryFinding("sha256:"+strings.Repeat("c", 64), "blocking")}
		},
		"duplicate id": func(result *ReviewResult) {
			result.Findings = []string{softwareFactoryFinding(material, "blocking"), softwareFactoryFinding(material, "advisory")}
		},
		"invalid repair scope": func(result *ReviewResult) {
			result.Findings = []string{softwareFactoryFindingWithScope(material, "blocking", "ledger")}
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			mutate(&candidate)
			candidate.ResultRevision = reviewResultFingerprint(candidate)
			if err := validatePersistedReviewResult(candidate); err == nil {
				t.Fatal("unresolved reviewer finding retained completion authority")
			}
		})
	}
}

func TestSoftwareFactoryProofDurableMaterialFindingClosureRequiresNewMaterialAndAttempt(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	oldMaterial := "sha256:" + strings.Repeat("1", 64)
	newMaterial := "sha256:" + strings.Repeat("2", 64)
	finding := softwareFactoryFinding(oldMaterial, "blocking")
	prior := softwareFactoryReviewResult(finding, oldMaterial)
	prior.CreatedAt = "2026-09-14T00:00:00Z"
	if _, err := store.SaveReviewResult(prior); err != nil {
		t.Fatal(err)
	}
	candidate := prior
	candidate.AttemptID = "review-2"
	candidate.MaterialFingerprint = newMaterial
	candidate.Verdict = "pass"
	candidate.Findings = nil
	candidate.Summary = "repair independently verified"
	// Reviewer timestamps can collide at second resolution. Durable attempt
	// chronology, not CreatedAt alone, must still retain the prior finding.
	candidate.CreatedAt = prior.CreatedAt
	for _, attempt := range []RunAttempt{
		{AttemptID: prior.AttemptID, ProjectID: "project", RecordID: prior.TaskID, Lane: runLaneReview, WorkRevision: prior.WorkRevision, StartedAt: "2026-09-14T00:00:00Z"},
		{AttemptID: candidate.AttemptID, ProjectID: "project", RecordID: candidate.TaskID, Lane: runLaneReview, WorkRevision: candidate.WorkRevision, StartedAt: "2026-09-14T01:00:00Z"},
	} {
		if err := store.SaveAttempt(attempt); err != nil {
			t.Fatal(err)
		}
	}
	candidate.ResultRevision = reviewResultFingerprint(candidate)
	daemon := &Daemon{store: store}
	if err := daemon.validateReviewFindingClosure("project", candidate); err == nil {
		t.Fatal("pass without an explicit durable closure attestation was accepted")
	}
	sameMaterial := candidate
	sameMaterial.ClosedFindings = []reviewerFindingClosure{{
		Schema: reviewerFindingClosureSchema, ID: "F-001", ClosureCondition: "re-run the exact proof and attach its receipt",
		Evidence: []string{"receipt-2"}, MaterialFingerprint: oldMaterial,
	}}
	sameMaterial.MaterialFingerprint = oldMaterial
	sameMaterial.ResultRevision = reviewResultFingerprint(sameMaterial)
	if err := daemon.validateReviewFindingClosure("project", sameMaterial); err == nil {
		t.Fatal("material finding was closed without changing implementation material")
	}
	closure := reviewerFindingClosure{
		Schema: reviewerFindingClosureSchema, ID: "F-001", ClosureCondition: "re-run the exact proof and attach its receipt",
		Evidence: []string{"receipt-2"}, MaterialFingerprint: newMaterial,
	}
	candidate.ClosedFindings = []reviewerFindingClosure{closure}
	candidate.ResultRevision = reviewResultFingerprint(candidate)
	if _, err := store.SaveReviewResult(candidate); err != nil {
		t.Fatal(err)
	}
	if err := daemon.validateReviewFindingClosure("project", candidate); err != nil {
		t.Fatalf("valid independent closure was rejected: %v", err)
	}
	candidate.AttemptID = prior.AttemptID
	candidate.ResultRevision = reviewResultFingerprint(candidate)
	if err := daemon.validateReviewFindingClosure("project", candidate); err == nil {
		t.Fatal("same review attempt was allowed to close its own finding")
	}
}

func TestSoftwareFactoryProofDurableProofFindingClosureAllowsSameMaterial(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	material := "sha256:" + strings.Repeat("3", 64)
	prior := softwareFactoryReviewResult(softwareFactoryFindingWithScope(material, "blocking", reviewerFindingRepairScopeProof), material)
	if _, err := store.SaveReviewResult(prior); err != nil {
		t.Fatal(err)
	}
	candidate := prior
	candidate.AttemptID = "review-proof-2"
	candidate.Verdict = "pass"
	candidate.Findings = nil
	candidate.Summary = "proof independently verified"
	candidate.ClosedFindings = []reviewerFindingClosure{{
		Schema: reviewerFindingClosureSchema, ID: "F-001", ClosureCondition: "re-run the exact proof and attach its receipt",
		Evidence: []string{"receipt-proof-2"}, MaterialFingerprint: material,
	}}
	for _, attempt := range []RunAttempt{
		{AttemptID: prior.AttemptID, ProjectID: "project", RecordID: prior.TaskID, Lane: runLaneReview, WorkRevision: prior.WorkRevision, StartedAt: "2026-09-14T00:00:00Z"},
		{AttemptID: candidate.AttemptID, ProjectID: "project", RecordID: candidate.TaskID, Lane: runLaneReview, WorkRevision: candidate.WorkRevision, StartedAt: "2026-09-14T01:00:00Z"},
	} {
		if err := store.SaveAttempt(attempt); err != nil {
			t.Fatal(err)
		}
	}
	candidate.ResultRevision = reviewResultFingerprint(candidate)
	if err := (&Daemon{store: store}).validateReviewFindingClosure("project", candidate); err != nil {
		t.Fatalf("proof-only finding should close on unchanged exact material: %v", err)
	}

	stale := candidate
	stale.ClosedFindings = []reviewerFindingClosure{{
		Schema: reviewerFindingClosureSchema, ID: "F-001", ClosureCondition: "re-run the exact proof and attach its receipt",
		Evidence: []string{"stale-receipt"}, MaterialFingerprint: "sha256:" + strings.Repeat("4", 64),
	}}
	stale.ResultRevision = reviewResultFingerprint(stale)
	if err := (&Daemon{store: store}).validateReviewFindingClosure("project", stale); err == nil {
		t.Fatal("closure bound to stale material was accepted")
	}
}

func TestSoftwareFactoryProofAuthoritativeCLIAndStoreBindMaterial(t *testing.T) {
	project, daemon, wfFile, run := reviewProposalDaemonFixture(t)
	defer daemon.Close()
	note, err := resolveV7Note(project.VaultRoot, run.RecordID, "task")
	if err != nil {
		t.Fatal(err)
	}
	proof, gates, err := reviewObjectiveSnapshots(project.VaultRoot, note)
	if err != nil {
		t.Fatal(err)
	}
	source := firstNonEmpty(stringField(note.Data, "source_sha"), stringField(note.Data, "source_commit"))
	material, err := reviewAttemptMaterialFingerprint(daemon.store, run.ProjectID, run.RecordID, run.ActiveAttemptID, run.WorkRevision, source)
	if err != nil {
		t.Fatal(err)
	}
	finding := softwareFactoryFinding(material, "blocking")
	if err := reviewSubmitCmd(Args{
		"vault": project.VaultRoot, "id": run.RecordID, "attempt": run.ActiveAttemptID,
		"by": reviewerActorForNote(wfFile.Data.Reviewer.Actor, note), "verdict": "changes_requested",
		"summary": "authoritative material-bound finding", "finding": finding,
		"task-rev": stringField(note.Data, "state_rev"), "source-sha": source,
		"work-rev": strconv.Itoa(run.WorkRevision), "proof-fingerprint": proof,
		"gate-fingerprint": gates,
	}); err != nil {
		t.Fatalf("authoritative CLI submission rejected: %v", err)
	}
	rows, err := daemon.store.ListReviewResults(project.ProjectID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("persisted authoritative review rows=%#v err=%v", rows, err)
	}
	if rows[0].Result.Schema != reviewResultSchema || rows[0].Result.MaterialFingerprint != material {
		t.Fatalf("CLI submission did not persist exact v3 material: %#v", rows[0].Result)
	}

	missingMaterial := rows[0].Result
	missingMaterial.MaterialFingerprint = ""
	missingMaterial.ResultRevision = reviewResultFingerprint(missingMaterial)
	if _, err := daemon.store.SaveReviewResult(missingMaterial); err == nil {
		t.Fatal("SaveReviewResult accepted an authoritative result without material identity")
	}
}

func TestSoftwareFactoryProofWorkerProposalBindsClosureAfterDaemonMaterial(t *testing.T) {
	project, daemon, wfFile, run := reviewProposalDaemonFixture(t)
	defer daemon.Close()
	note, err := resolveV7Note(project.VaultRoot, run.RecordID, "task")
	if err != nil {
		t.Fatal(err)
	}
	data, body, err := parseFrontmatterMustRead(note.AbsolutePath)
	if err != nil {
		t.Fatal(err)
	}
	body = replaceSection(body, "## Verification", "| Covers | Check | Result | Notes |\n|---|---|---|---|\n| A1 | manual proof: daemon boundary fixture | pass | daemon-boundary fixture proof |")
	data["proof_status"] = "satisfied"
	data["proof_required"] = []string{}
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(note.AbsolutePath, content); err != nil {
		t.Fatal(err)
	}
	note, err = resolveV7Note(project.VaultRoot, run.RecordID, "task")
	if err != nil {
		t.Fatal(err)
	}
	if report, reportErr := loadV7ProofReport(project.VaultRoot, run.RecordID); reportErr != nil || report.Status != "satisfied" {
		t.Fatalf("worker proposal fixture proof is not satisfied: report=%#v err=%v", report, reportErr)
	}
	proof, gates, err := reviewObjectiveSnapshots(project.VaultRoot, note)
	if err != nil {
		t.Fatal(err)
	}
	source := firstNonEmpty(stringField(note.Data, "source_sha"), stringField(note.Data, "source_commit"))
	material, err := reviewAttemptMaterialFingerprint(daemon.store, run.ProjectID, run.RecordID, run.ActiveAttemptID, run.WorkRevision, source)
	if err != nil {
		t.Fatal(err)
	}
	closureRaw, err := json.Marshal(reviewerFindingClosure{
		Schema: reviewerFindingClosureSchema, ID: "F-001", ClosureCondition: "re-run the exact proof and attach its receipt",
		Evidence: []string{"receipt-2"}, MaterialFingerprint: material,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TUSKER_ATTEMPT_ID", run.ActiveAttemptID)
	t.Setenv("TUSKER_CANONICAL_VAULT", project.VaultRoot)
	t.Setenv("TUSKER_CANONICAL_PROJECT_ID", v7ProjectID(project.VaultRoot))
	output := captureStdout(t, func() {
		err := reviewSubmitCmd(Args{
			"vault": project.VaultRoot, "id": run.RecordID, "attempt": run.ActiveAttemptID,
			"by": reviewerActorForNote(wfFile.Data.Reviewer.Actor, note), "verdict": "pass",
			"summary": "worker closure is bound after daemon material computation", "covers": strings.Join(v7AcceptanceIDs(note.Body), ","),
			"closure": string(closureRaw), "task-rev": stringField(note.Data, "state_rev"), "source-sha": source,
			"work-rev": strconv.Itoa(run.WorkRevision), "proof-fingerprint": proof, "gate-fingerprint": gates,
		})
		if err != nil {
			t.Fatal(err)
		}
	})
	if !strings.HasPrefix(output, reviewProposalMarker) {
		t.Fatalf("worker review submit did not emit a proposal: %q", output)
	}
	var proposal reviewProposal
	if err := json.Unmarshal([]byte(strings.TrimPrefix(strings.TrimSpace(output), reviewProposalMarker)), &proposal); err != nil {
		t.Fatal(err)
	}
	if proposal.Result.Schema != reviewResultSchemaV2 || len(proposal.Result.ClosedFindings) != 1 || proposal.Result.ClosedFindings[0].MaterialFingerprint != material {
		t.Fatalf("worker proposal did not preserve closure material: %#v", proposal.Result)
	}
	if err := writeText(run.RawLogPath, output); err != nil {
		t.Fatal(err)
	}
	if err := writeRunnerStatusFile(run.StatusPath, 0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := daemon.reconcileRun(context.Background(), project, wfFile, run); err != nil {
		t.Fatalf("daemon rejected a closure after binding exact proposal material: %v", err)
	}
	rows, err := daemon.store.ListReviewResults(project.ProjectID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("worker closure proposal was not persisted exactly once: rows=%#v err=%v", rows, err)
	}
	if rows[0].Result.Schema != reviewResultSchema || rows[0].Result.MaterialFingerprint != material || len(rows[0].Result.ClosedFindings) != 1 || rows[0].Result.ClosedFindings[0].MaterialFingerprint != material {
		t.Fatalf("daemon did not bind exact material before validating closure: %#v", rows[0].Result)
	}
}

func TestSoftwareFactoryProofCompletionRejectsMaterialDrift(t *testing.T) {
	_, project, daemon, result := completionReactorFixture(t, true)
	defer daemon.Close()
	result.MaterialFingerprint = "sha256:" + strings.Repeat("f", 64)
	result.ResultRevision = reviewResultFingerprint(result)
	err := daemon.reactToReviewResult(project, completionAuthorityTestWorkflow(), result, completionReactorModeAuthoritative)
	if err == nil || errorToIssue(err).Code != completionRepairRequiredError || !strings.Contains(strings.ToLower(err.Error()), "material") {
		t.Fatalf("completion accepted or misclassified material drift: %v", err)
	}
}

func TestSoftwareFactoryProofCompletionRequiresDurableIndependentClosure(t *testing.T) {
	vault, project, daemon, prior := completionReactorFixture(t, true)
	defer daemon.Close()
	prior.Verdict = "changes_requested"
	prior.Findings = []string{completionTestFinding(prior.MaterialFingerprint, "F-001", "repair the exact acceptance regression")}
	prior.CreatedAt = "2026-09-14T00:00:00Z"
	prior.ResultRevision = reviewResultFingerprint(prior)
	if _, err := daemon.store.SaveReviewResult(prior); err != nil {
		t.Fatal(err)
	}

	source := commitLandBranch(t, project.RepoRoot, "source/closure-repair", prior.ImplementationSHA, map[string]string{"reviewed.txt": "repaired\n"})
	setAutomationV7TaskFields(t, vault, prior.TaskID, map[string]any{
		"status": "review", "readiness": "waiting_on_review", "source_sha": source, "work_revision": prior.WorkRevision,
	})
	recordCompletionTestProof(t, vault, prior.TaskID)
	armScheduledPromotionWaveForTest(t, vault, "W-0001")
	candidate := completionResultForReviewedTask(t, vault, project, prior.TaskID, "review-closure-repair", "independent repair verification")
	candidate.CreatedAt = "2026-09-14T01:00:00Z"
	if err := daemon.reactToReviewResult(project, completionAuthorityTestWorkflow(), candidate, completionReactorModeAuthoritative); err == nil {
		t.Fatal("completion accepted a pass without durable closure attestation")
	}
	candidate.ClosedFindings = []reviewerFindingClosure{{
		Schema: reviewerFindingClosureSchema, ID: "F-001", ClosureCondition: "re-run the exact acceptance proof",
		Evidence: []string{"repair-receipt"}, MaterialFingerprint: candidate.MaterialFingerprint,
	}}
	candidate.ResultRevision = reviewResultFingerprint(candidate)
	if _, err := daemon.store.SaveReviewResult(candidate); err != nil {
		t.Fatal(err)
	}
	if err := daemon.reactToReviewResult(project, completionAuthorityTestWorkflow(), candidate, completionReactorModeAuthoritative); err != nil {
		t.Fatalf("independent exact-material closure was rejected: %v", err)
	}
	transaction, err := daemon.store.CompletionTransactionForResult(project.ProjectID, candidate.TaskID, candidate.ResultRevision)
	if err != nil || transaction == nil || transaction.Phase != completionPhaseTerminal {
		t.Fatalf("durable closure did not complete: transaction=%#v err=%v", transaction, err)
	}
}
