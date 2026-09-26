package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSoftwareFactoryRepairAtomicAdmissionPersistsCapsAndIgnoresReason(t *testing.T) {
	stateRoot := t.TempDir()
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	caps := ExternalLoopCaps{MaxCycles: 20, MaxRepairContinuations: 1, MaxExternalThreads: 20, WallClockTimeoutHours: 8}
	first, err := store.AdmitExternalLoopEvent(ExternalLoopEvent{
		ProjectID: "project-1", RecordID: "TASK-T-0001", ItemID: "TASK-T-0001", Runner: "chatgpt-browser",
		JobID: "job-1", AttemptID: "attempt-1", Stage: externalLoopStageApplyFailed,
		Action: externalLoopActionContinueThreadOnFailure, Reason: "first provider wording",
	}, caps, true, nil)
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if !first.Created || first.Event.Action != externalLoopActionContinueThreadOnFailure || first.Counters.RepairContinuations != 0 {
		_ = store.Close()
		t.Fatalf("first repair admission = %#v", first)
	}
	second, err := store.AdmitExternalLoopEvent(ExternalLoopEvent{
		ProjectID: "project-1", RecordID: "TASK-T-0001", ItemID: "TASK-T-0001", Runner: "chatgpt-browser",
		JobID: "job-1", AttemptID: "attempt-1", Stage: externalLoopStageApplyFailed,
		Action: externalLoopActionContinueThreadOnFailure, Reason: "different provider wording",
	}, ExternalLoopCaps{MaxCycles: 1, MaxRepairContinuations: 9, MaxExternalThreads: 1, WallClockTimeoutHours: 1}, false, nil)
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if second.Created || second.Event.EventID != first.Event.EventID || second.Event.Action != first.Event.Action {
		_ = store.Close()
		t.Fatalf("reason text changed event identity: first=%#v second=%#v", first.Event, second.Event)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	persisted, found, err := reopened.ExternalLoopCapsFor("project-1", "TASK-T-0001")
	if err != nil || !found {
		t.Fatalf("persisted caps found=%v caps=%#v err=%v", found, persisted, err)
	}
	if persisted != caps {
		t.Fatalf("effective caps changed across restart: got %#v want %#v", persisted, caps)
	}
	capSeed, err := reopened.AdmitExternalLoopEvent(ExternalLoopEvent{
		ProjectID: "project-2", RecordID: "TASK-T-0006", ItemID: "TASK-T-0006", JobID: "job-seed", AttemptID: "attempt-seed",
		Stage: externalLoopStageApplyFailed, Action: externalLoopActionContinueThreadOnFailure,
		PayloadJSON: `{"work_revision":1,"material_fingerprint":"material-6"}`,
	}, ExternalLoopCaps{MaxCycles: 10, MaxRepairContinuations: 1, MaxExternalThreads: 10, WallClockTimeoutHours: 8}, true, nil)
	if err != nil || !capSeed.Created {
		t.Fatalf("cap seed admission failed: %#v %v", capSeed, err)
	}
	capBlocked, err := reopened.AdmitExternalLoopEvent(ExternalLoopEvent{
		ProjectID: "project-2", RecordID: "TASK-T-0006", ItemID: "TASK-T-0006", JobID: "job-next", AttemptID: "attempt-next",
		Stage: externalLoopStageApplyFailed, Action: externalLoopActionContinueThreadOnFailure,
		PayloadJSON: `{"work_revision":1,"material_fingerprint":"material-6"}`,
	}, ExternalLoopCaps{MaxCycles: 10, MaxRepairContinuations: 1, MaxExternalThreads: 10, WallClockTimeoutHours: 8}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	var capPayload map[string]any
	if err := json.Unmarshal([]byte(capBlocked.Event.PayloadJSON), &capPayload); err != nil {
		t.Fatal(err)
	}
	if capBlocked.Event.Action != externalLoopActionEscalateHuman || stringValue(capPayload["requested_action"]) != externalLoopActionContinueThreadOnFailure {
		t.Fatalf("cap escalation lost original requested action: %#v", capBlocked.Event)
	}
}

func TestSoftwareFactoryRepairConcurrentAdmissionHonorsRepairCap(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	caps := ExternalLoopCaps{MaxCycles: 50, MaxRepairContinuations: 1, MaxExternalThreads: 50, WallClockTimeoutHours: 8}
	if _, err := store.AdmitExternalLoopEvent(ExternalLoopEvent{
		ProjectID: "project-1", RecordID: "TASK-T-0002", ItemID: "TASK-T-0002", Runner: "chatgpt-browser",
		JobID: "job-seed", AttemptID: "attempt-seed", Stage: externalLoopStageApplyFailed,
		Action: externalLoopActionContinueThreadOnFailure, Reason: "seed",
	}, caps, true, nil); err != nil {
		t.Fatal(err)
	}

	const contenders = 8
	results := make(chan ExternalLoopAdmission, contenders)
	errs := make(chan error, contenders)
	var wg sync.WaitGroup
	for i := 0; i < contenders; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			admission, admissionErr := store.AdmitExternalLoopEvent(ExternalLoopEvent{
				ProjectID: "project-1", RecordID: "TASK-T-0002", ItemID: "TASK-T-0002", Runner: "chatgpt-browser",
				JobID: "job-" + fmt.Sprint(i), AttemptID: "attempt-" + fmt.Sprint(i), Stage: externalLoopStageApplyFailed,
				Action: externalLoopActionContinueThreadOnFailure, Reason: fmt.Sprintf("contender %d", i),
			}, caps, false, nil)
			if admissionErr != nil {
				errs <- admissionErr
				return
			}
			results <- admission
		}(i)
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	continueCount := 0
	escalationCount := 0
	for admission := range results {
		switch admission.Event.Action {
		case externalLoopActionContinueThreadOnFailure:
			continueCount++
		case externalLoopActionEscalateHuman:
			escalationCount++
		default:
			t.Fatalf("unexpected concurrent action: %#v", admission.Event)
		}
	}
	if continueCount != 0 || escalationCount != contenders {
		t.Fatalf("cap admission was not serialized: continue=%d escalated=%d", continueCount, escalationCount)
	}
	events, err := store.ListExternalLoopEvents("project-1", "TASK-T-0002")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != contenders+1 {
		t.Fatalf("unexpected admitted event count: %d", len(events))
	}
	counters := externalLoopCountersForEvents(events)
	if counters.RepairContinuations != 1 {
		t.Fatalf("repair cap count=%d, want 1", counters.RepairContinuations)
	}
}

func TestSoftwareFactoryRepairRestartDoesNotSuppressUnacknowledgedEffects(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	apply := ExternalLoopEvent{ProjectID: "project-1", RecordID: "TASK-T-0003", ItemID: "TASK-T-0003", JobID: "job-apply", Stage: externalLoopStageCollected, Action: externalLoopActionApplyPatch, Status: "ok"}
	if _, _, err := store.SaveExternalLoopEvent(apply); err != nil {
		t.Fatal(err)
	}
	handled, err := externalLoopJobAlreadyHandledForNote(store, apply.ProjectID, apply.RecordID, apply.JobID, Note{})
	if err != nil {
		t.Fatal(err)
	}
	if handled {
		t.Fatal("collection admission suppressed an unacknowledged apply effect")
	}
	if !externalLoopEffectNeedsReconciliation(RunStatus{
		LeaseState: string(LeaseStateReleased), AttemptOutcome: string(AttemptOutcomeSucceeded),
	}, &apply) {
		t.Fatal("released collected effect was not marked for reconciliation")
	}
	if externalLoopEffectNeedsReconciliation(RunStatus{
		LeaseState: string(LeaseStateClaimed), AttemptOutcome: string(AttemptOutcomeNone),
	}, &apply) {
		t.Fatal("claimed effect must not be blindly replayed")
	}
	closeEvent := ExternalLoopEvent{ProjectID: "project-1", RecordID: "TASK-T-0004", ItemID: "TASK-T-0004", JobID: "job-close", Stage: externalLoopStageCollected, Action: externalLoopActionCloseTask, Status: "ok"}
	saved, created, err := store.SaveExternalLoopEvent(closeEvent)
	if err != nil {
		t.Fatal(err)
	}
	replayed := closeEvent
	replayed.Reason = "different close wording"
	replayed, replayCreated, err := store.SaveExternalLoopEvent(replayed)
	if err != nil {
		t.Fatal(err)
	}
	if !created || replayCreated || replayed.EventID != saved.EventID {
		t.Fatalf("direct event save was not reason-independent: first=%#v replay=%#v", saved, replayed)
	}
	handled, err = externalLoopJobAlreadyHandledForNote(store, closeEvent.ProjectID, closeEvent.RecordID, closeEvent.JobID, Note{})
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatal("terminal close admission was not recognized after restart")
	}
	closeMaterial := "sha256:" + strings.Repeat("1", 64)
	closeNote := Note{Data: map[string]any{"id": closeEvent.RecordID, "work_revision": 1, "material_fingerprint": closeMaterial, "status": "review"}}
	closeEvent.PayloadJSON = `{"work_revision":1,"material_fingerprint":"` + closeMaterial + `"}`
	if _, _, err := store.SaveExternalLoopEvent(ExternalLoopEvent{
		ProjectID: closeEvent.ProjectID, RecordID: closeEvent.RecordID, ItemID: closeEvent.ItemID,
		JobID: "job-close-reconcile", AttemptID: "attempt-close-reconcile", Stage: externalLoopStageCollected,
		Action: externalLoopActionCloseTask, Status: "ok", PayloadJSON: closeEvent.PayloadJSON,
	}); err != nil {
		t.Fatal(err)
	}
	handled, err = externalLoopJobAlreadyHandledForNote(store, closeEvent.ProjectID, closeEvent.RecordID, "job-close-reconcile", closeNote)
	if err != nil {
		t.Fatal(err)
	}
	if handled {
		t.Fatal("unclosed canonical task close effect was suppressed")
	}
	closedNote := closeNote
	closedNote.Data = map[string]any{"id": closeEvent.RecordID, "work_revision": 1, "material_fingerprint": closeMaterial, "status": "done", "closed_at": "2026-09-14T00:00:00Z"}
	handled, err = externalLoopJobAlreadyHandledForNote(store, closeEvent.ProjectID, closeEvent.RecordID, "job-close-reconcile", closedNote)
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatal("canonical task close effect was not recognized")
	}
}

func TestSoftwareFactoryRepairEscalationAndFindingIdentityAreDeterministic(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	decision := SupervisorDecision{ProjectID: "project-1", RecordID: "TASK-T-0005", Kind: string(SupervisorDecisionStopForHuman), Reason: "external loop escalation event event-1: cap reached", ContextSignal: "external_loop"}
	first, err := store.SaveSupervisorDecision(decision)
	if err != nil {
		t.Fatal(err)
	}
	decision.CreatedAt = "later"
	decision.Reason = "external loop escalation event event-1: different wording"
	second, err := store.SaveSupervisorDecision(decision)
	if err != nil {
		t.Fatal(err)
	}
	if first.DecisionID == "" || first.DecisionID != second.DecisionID {
		t.Fatalf("decision identity was not deterministic: first=%#v second=%#v", first, second)
	}
	decisions, err := store.ListSupervisorDecisionsForRun(decision.ProjectID, decision.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 1 {
		t.Fatalf("restart replay created %d decision requests", len(decisions))
	}

	rawFinding, err := json.Marshal(reviewerFindingRecord{Schema: reviewerFindingSchema, ID: "F-001", Kind: "blocking", Acceptance: []string{"A1"}, Evidence: []string{"receipt-1"}, Consequence: "repair is required", ClosureCondition: "rerun targeted review", MaterialFingerprint: "sha256:material"})
	if err != nil {
		t.Fatal(err)
	}
	result := externalLoopAdvanceResult{Collect: &externalCollectReport{ReviewResult: &externalReviewResult{Verdict: "changes_requested", Findings: []string{string(rawFinding)}}}}
	payload := externalLoopPayloadForResult(result)
	ids := normalizeList(payload["blocking_finding_ids"])
	if len(ids) != 1 || ids[0] != "F-001" {
		t.Fatalf("blocking finding identity was not carried into repair payload: %#v", payload)
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	refs := externalLoopBlockingFindingRefsFromPayload(string(rawPayload))
	if len(refs) != 1 || refs[0] != "finding ID F-001 acceptance IDs A1" {
		t.Fatalf("blocking finding references were not actionable: %#v", refs)
	}
	if _, _, err := store.SaveExternalLoopEvent(ExternalLoopEvent{
		ProjectID: "project-1", RecordID: "TASK-T-0005", ItemID: "TASK-T-0005", JobID: "job-repair",
		Stage: externalLoopStageApplyFailed, Action: externalLoopActionContinueThreadOnFailure, Status: "ok",
		PayloadJSON: string(rawPayload),
	}); err != nil {
		t.Fatal(err)
	}
	launch := (&Daemon{store: store}).externalLoopLaunchContext("project-1", "TASK-T-0005")
	if !strings.Contains(launch.Reason, "blocking finding IDs: F-001") {
		t.Fatalf("targeted repair launch lost blocking finding identity: %#v", launch)
	}
	blocked, err := externalLoopApplyPolicy(&automationCommandContext{
		Store:   store,
		Project: RegisteredProject{ProjectID: "project-1"},
	}, Note{Data: map[string]any{"id": "TASK-T-0006"}}, externalLoopPolicyInput{
		RecordID: "TASK-T-0006", Runner: "chatgpt-browser", JobID: "job-blocked", AttemptID: "attempt-blocked",
		Stage: externalLoopStageCollected, Action: externalLoopActionApplyPatch, Reason: "contract drift",
		Blockers: []string{"contract contradiction"}, Payload: map[string]any{"next_action": externalLoopActionApplyPatch},
	}, ExternalLoopCaps{MaxCycles: 5, MaxRepairContinuations: 2, MaxExternalThreads: 5, WallClockTimeoutHours: 8})
	if err != nil {
		t.Fatal(err)
	}
	if blocked.NextAction != externalLoopActionEscalateHuman || blocked.Event == nil {
		t.Fatalf("blocked transition did not become an escalation: %#v", blocked)
	}
	if !strings.Contains(blocked.Event.PayloadJSON, "requested_action") {
		t.Fatalf("escalation did not retain the incomplete action: %#v", blocked.Event)
	}
	decisions, err = store.ListSupervisorDecisionsForRun("project-1", "TASK-T-0006")
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 1 || !strings.Contains(decisions[0].Reason, "contract contradiction") {
		t.Fatalf("blocked transition decision was not actionable: %#v", decisions)
	}
	if _, err := externalLoopApplyPolicy(&automationCommandContext{
		Store:   store,
		Project: RegisteredProject{ProjectID: "project-1"},
	}, Note{Data: map[string]any{"id": "TASK-T-0006"}}, externalLoopPolicyInput{
		RecordID: "TASK-T-0006", Runner: "chatgpt-browser", JobID: "job-blocked", AttemptID: "attempt-blocked",
		Stage: externalLoopStageCollected, Action: externalLoopActionApplyPatch, Reason: "different wording",
		Blockers: []string{"contract contradiction"}, Payload: map[string]any{"next_action": externalLoopActionApplyPatch},
	}, ExternalLoopCaps{MaxCycles: 1, MaxRepairContinuations: 1, MaxExternalThreads: 1, WallClockTimeoutHours: 1}); err != nil {
		t.Fatal(err)
	}
	decisions, err = store.ListSupervisorDecisionsForRun("project-1", "TASK-T-0006")
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 1 {
		t.Fatalf("blocked transition replay created %d decision requests", len(decisions))
	}
}

func TestSoftwareFactoryRepairExternalLoopCheckpointsAndAmendments(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	caps := ExternalLoopCaps{MaxCycles: 10, MaxRepairContinuations: 10, MaxExternalThreads: 10, WallClockTimeoutHours: 8}
	ctx := &automationCommandContext{Store: store, Project: RegisteredProject{ProjectID: "project-1"}}
	note := Note{Data: map[string]any{"id": "TASK-T-0007", "work_revision": 1, "material_fingerprint": "material-old"}}
	input := externalLoopPolicyInput{
		RecordID: "TASK-T-0007", Runner: "chatgpt-browser", JobID: "job-7", AttemptID: "attempt-7",
		Stage: externalLoopStageCollected, Action: externalLoopActionApplyPatch, Reason: "apply is blocked",
		Payload: map[string]any{"work_revision": 1, "material_fingerprint": "material-old"},
	}
	escalation := externalLoopEventForInput(ctx, note, input, externalLoopActionEscalateHuman, "apply is blocked", []string{"operator approval required"})
	admission, err := store.AdmitExternalLoopEvent(escalation, caps, true, []string{"operator approval required"})
	if err != nil {
		t.Fatal(err)
	}
	if !admission.Created || admission.Event.Action != externalLoopActionEscalateHuman {
		t.Fatalf("expected an unacknowledged escalation checkpoint, got %#v", admission)
	}
	handled, err := externalLoopJobAlreadyHandledForNote(store, "project-1", "TASK-T-0007", "job-7", note)
	if err != nil {
		t.Fatal(err)
	}
	if handled {
		t.Fatal("unacknowledged escalation was treated as complete")
	}

	// A later action variant for the same job/attempt must be held at the
	// original escalation until its canonical supervisor decision exists.
	variant := input
	variant.AttemptID = "attempt-variant"
	variant.Reason = "different provider wording"
	variant.Payload = map[string]any{"work_revision": 1, "material_fingerprint": "material-old", "requested_action": externalLoopActionApplyPatch}
	variantEvent := externalLoopEventForInput(ctx, note, variant, externalLoopActionApplyPatch, variant.Reason, nil)
	variantAdmission, err := store.AdmitExternalLoopEvent(variantEvent, caps, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if variantAdmission.Created || variantAdmission.Event.EventID != admission.Event.EventID || variantAdmission.Event.Action != externalLoopActionEscalateHuman {
		t.Fatalf("later action bypassed unacknowledged escalation: %#v", variantAdmission)
	}

	if _, err := store.SaveSupervisorDecision(SupervisorDecision{
		ProjectID: "project-1", RecordID: "TASK-T-0007", AttemptID: "attempt-7",
		Kind: "diagnostic_note", ContextSignal: "external_loop",
		Reason: "external loop escalation event " + admission.Event.EventID + ": unrelated note",
	}); err != nil {
		t.Fatal(err)
	}
	handled, err = externalLoopJobAlreadyHandledForNote(store, "project-1", "TASK-T-0007", "job-7", note)
	if err != nil {
		t.Fatal(err)
	}
	if handled {
		t.Fatal("non-stop supervisor note acknowledged the escalation")
	}

	decision, err := store.SaveSupervisorDecision(SupervisorDecision{
		ProjectID: "project-1", RecordID: "TASK-T-0007", AttemptID: "attempt-7",
		Kind: string(SupervisorDecisionStopForHuman), ContextSignal: "external_loop",
		Reason: "external loop escalation event " + admission.Event.EventID + ": operator approval required",
	})
	if err != nil {
		t.Fatal(err)
	}
	if decision.DecisionID == "" {
		t.Fatal("supervisor decision checkpoint was not persisted")
	}
	handled, err = externalLoopJobAlreadyHandledForNote(store, "project-1", "TASK-T-0007", "job-7", note)
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatal("acknowledged escalation was not recognized on restart")
	}

	// An amended task revision/material gets a fresh admission even when the
	// provider reuses the same job and attempt identifiers.
	amended := note
	amended.Data = map[string]any{"id": "TASK-T-0007", "work_revision": 2, "material_fingerprint": "material-new"}
	amendedInput := input
	amendedInput.Reason = "apply against amended material"
	amendedInput.Payload = map[string]any{"work_revision": 2, "material_fingerprint": "material-new"}
	amendedResult, err := externalLoopApplyPolicy(ctx, amended, amendedInput, caps)
	if err != nil {
		t.Fatal(err)
	}
	if !amendedResult.EventCreated || amendedResult.Event == nil || amendedResult.Event.EventID == admission.Event.EventID {
		t.Fatalf("amended material reused the old admission: %#v", amendedResult)
	}
	events, err := store.ListExternalLoopEvents("project-1", "TASK-T-0007")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("expected one old and one amended event, got %#v", events)
	}
}

func TestSoftwareFactoryRepairExternalArtifactParserCanonicalizesFindingsAndBlocksMalformedInput(t *testing.T) {
	material := "sha256:" + strings.Repeat("a", 64)
	finding := map[string]any{
		"schema": reviewerFindingSchema, "id": "F-EXT-1", "kind": "blocking",
		"acceptance": []string{"A1"}, "evidence": []string{"review-1"},
		"consequence": "the exact patch needs repair", "closure_condition": "rerun A1", "material_fingerprint": material,
	}
	authority := ReviewResult{
		Schema: reviewResultSchema, ProjectID: "project-1", TaskID: "TASK-T-0009", TaskStateRev: "state-1",
		WorkRevision: 1, ImplementationSHA: "source-1", AttemptID: "review-1", Actor: "reviewer:agent",
		Runner: "chatgpt-browser", RunnerProfile: "review-profile", WorkerPolicyFP: "sha256:" + strings.Repeat("b", 64),
		Covers: []string{"A1"}, ProofFingerprint: "proof-1", GateFingerprint: "gates-1", MaterialFingerprint: material,
		Verdict: "changes_requested", Summary: "the patch needs one correction", Findings: []string{mustJSON(t, finding)}, CreatedAt: "2026-09-14T00:00:00Z",
	}
	authority.ResultRevision = reviewResultFingerprint(authority)
	resultValues := map[string]any{}
	rawAuthority, err := json.Marshal(authority)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(rawAuthority, &resultValues); err != nil {
		t.Fatal(err)
	}
	resultValues["kind"] = "review"
	resultValues["risk"] = "low"
	resultValues["findings"] = []any{finding}
	resultJSON, err := json.Marshal(resultValues)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "review.md")
	if err := os.WriteFile(path, []byte("```json\n"+string(resultJSON)+"\n```\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	parsed, err := externalReviewResultFromArtifacts([]externalArtifact{{Path: path, Kind: "review_packet"}})
	if err != nil {
		t.Fatal(err)
	}
	if parsed == nil || parsed.Authority.Schema != reviewResultSchema || len(parsed.Findings) != 1 || strings.Contains(parsed.Findings[0], "map[") {
		t.Fatalf("structured finding was not canonical JSON: %#v", parsed)
	}
	if _, err := parseReviewerFinding(parsed.Findings[0]); err != nil {
		t.Fatalf("canonical finding no longer satisfies typed finding contract: %v", err)
	}

	badPath := filepath.Join(t.TempDir(), "review.md")
	badValues := map[string]any{}
	if err := json.Unmarshal(resultJSON, &badValues); err != nil {
		t.Fatal(err)
	}
	badValues["findings"] = []any{map[string]any{"id": "missing-schema"}}
	bad, err := json.Marshal(badValues)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(badPath, bad, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := externalReviewResultFromArtifacts([]externalArtifact{{Path: badPath, Kind: "review_packet"}}); err == nil || !strings.Contains(err.Error(), "external review finding is invalid") {
		t.Fatalf("malformed structured finding did not block parsing: %v", err)
	}
}

func TestSoftwareFactoryRepairExternalReviewAuthorityBindsCurrentMaterial(t *testing.T) {
	repo := orchestrationGitRepo(t)
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	facts, err := captureGitBranchFacts(repo, "main", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	note := Note{Data: map[string]any{
		"id": "TASK-T-0009", "kind": "task", "schema": "tusker.task/v7", "status": "review",
		"state_rev": "state-1", "work_revision": 1, "source_sha": facts.Head, "owned_paths": []string{"tracked.txt"},
	}, Body: "# Current task\n"}
	material, err := workspaceTreeStateHashForPaths(repo, []string{"tracked.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAttempt(RunAttempt{
		AttemptID: "execute-1", ProjectID: "project-1", RecordID: "TASK-T-0009", ItemID: "TASK-T-0009",
		Runner: "codex_exec", Lane: runLaneExecute, WorkRevision: 1, WorkspacePath: repo, Outcome: string(AttemptOutcomeSucceeded),
		EndState: RunEndState{Schema: "tusker.run-end-state/v2", HeadSHA: facts.Head, WorktreePath: repo, MaterialFingerprint: material, MaterialScope: []string{"tracked.txt"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAttempt(RunAttempt{
		AttemptID: "review-1", ProjectID: "project-1", RecordID: "TASK-T-0009", ItemID: "TASK-T-0009",
		Runner: "chatgpt-browser", Lane: runLaneReview, WorkRevision: 1, WorkspacePath: repo, ParentAttemptID: "execute-1",
	}); err != nil {
		t.Fatal(err)
	}
	ctx := &automationCommandContext{
		Store:    store,
		Project:  RegisteredProject{ProjectID: "project-1", RepoRoot: repo, VaultRoot: repo},
		Workflow: WorkflowFile{Data: Workflow{Reviewer: ReviewerPolicy{Actor: "reviewer:agent"}}},
	}
	run := RunStatus{ProjectID: "project-1", RecordID: "TASK-T-0009", ItemID: "TASK-T-0009", Runner: "chatgpt-browser", RunnerProfile: "review-profile", Lane: runLaneReview, ActiveAttemptID: "review-1", WorkRevision: 1}
	authority := ReviewResult{
		Schema: reviewResultSchema, ProjectID: "project-1", TaskID: "TASK-T-0009", TaskStateRev: "state-1", WorkRevision: 1,
		ImplementationSHA: facts.Head, AttemptID: "review-1", Actor: "reviewer:agent", Runner: "chatgpt-browser", RunnerProfile: "review-profile",
		WorkerPolicyFP: "sha256:" + strings.Repeat("c", 64), Covers: []string{"A1"}, ProofFingerprint: "proof-1", GateFingerprint: "gates-1",
		MaterialFingerprint: material, Verdict: "pass", Summary: "accepted", CreatedAt: "2026-09-14T00:00:00Z",
	}
	authority.ResultRevision = reviewResultFingerprint(authority)
	path := filepath.Join(t.TempDir(), "review.json")
	values := map[string]any{}
	raw, err := json.Marshal(authority)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &values); err != nil {
		t.Fatal(err)
	}
	values["kind"], values["risk"] = "review", "low"
	raw, err = json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	parsed, err := externalReviewResultFromArtifacts([]externalArtifact{{Path: path, Kind: "review_packet"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateExternalReviewAuthority(ctx, note, run, parsed); err != nil {
		t.Fatalf("current v3 review material was rejected: %v", err)
	}
	payload := externalLoopPayloadForResult(externalLoopAdvanceResult{TaskID: note.Data["id"].(string), MaterialFingerprint: parsed.MaterialFingerprint, Collect: &externalCollectReport{ReviewResult: parsed}})
	if got := stringValue(payload["material_fingerprint"]); got != material {
		t.Fatalf("event material identity drifted: got %q want %q", got, material)
	}

	stale := authority
	stale.MaterialFingerprint = strings.Repeat("d", 64)
	stale.ResultRevision = reviewResultFingerprint(stale)
	staleRaw, err := json.Marshal(stale)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, staleRaw, 0o644); err != nil {
		t.Fatal(err)
	}
	staleParsed, err := externalReviewResultFromArtifacts([]externalArtifact{{Path: path, Kind: "review_packet"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateExternalReviewAuthority(ctx, note, run, staleParsed); err == nil || !strings.Contains(err.Error(), "material fingerprint") {
		t.Fatalf("stale/arbitrary material was accepted: %v", err)
	}

	advisory := authority
	advisory.Verdict = "changes_requested"
	advisory.Covers = nil
	advisory.Findings = []string{mustJSON(t, map[string]any{"schema": reviewerFindingSchema, "id": "A-001", "kind": "advisory", "evidence": []string{"note"}})}
	advisory.ResultRevision = reviewResultFingerprint(advisory)
	advisoryRaw, err := json.Marshal(advisory)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, advisoryRaw, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := externalReviewResultFromArtifacts([]externalArtifact{{Path: path, Kind: "review_packet"}}); err == nil || !strings.Contains(err.Error(), "at least one blocking") {
		t.Fatalf("advisory-only changes_requested did not block repair: %v", err)
	}
}

func TestSoftwareFactoryRepairExternalReviewControllerBindsDTOBeforeAdmission(t *testing.T) {
	repo := orchestrationGitRepo(t)
	vault := repairTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "External review authority", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	facts, err := captureGitBranchFacts(repo, "main", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{
		"status": "review", "readiness": "waiting_on_review", "next_owner": "reviewer:agent",
		"work_revision": 1, "source_sha": facts.Head, "owned_paths": []string{"tracked.txt"}, "spec_refs": []string{},
	})
	note, err := resolveNote(vault, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	material, err := workspaceTreeStateHashForPaths(repo, []string{"tracked.txt"})
	if err != nil {
		t.Fatal(err)
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveAttempt(RunAttempt{
		AttemptID: "execute-authority", ProjectID: "project-1", RecordID: "APP-T-0001", ItemID: "APP-T-0001",
		Runner: "codex_exec", Lane: runLaneExecute, WorkRevision: 1, WorkspacePath: repo, Outcome: string(AttemptOutcomeSucceeded),
		EndState: RunEndState{Schema: "tusker.run-end-state/v2", HeadSHA: facts.Head, WorktreePath: repo, MaterialFingerprint: material, MaterialScope: []string{"tracked.txt"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAttempt(RunAttempt{
		AttemptID: "review-authority", ProjectID: "project-1", RecordID: "APP-T-0001", ItemID: "APP-T-0001",
		Runner: "chatgpt-browser", Lane: runLaneReview, WorkRevision: 1, WorkspacePath: repo, ParentAttemptID: "execute-authority",
	}); err != nil {
		t.Fatal(err)
	}
	policy := "sha256:" + strings.Repeat("1", 64)
	run := RunStatus{
		ProjectID: "project-1", RecordID: "APP-T-0001", ItemID: "APP-T-0001", Runner: "chatgpt-browser", RunnerProfile: "review-profile",
		WorkerPolicyFP: policy, Lane: runLaneReview, LeaseState: string(LeaseStateReleased), AttemptOutcome: string(AttemptOutcomeSucceeded),
		ActiveAttemptID: "review-authority", WorkRevision: 1,
	}
	authority := ReviewResult{
		Schema: reviewResultSchema, ProjectID: "project-1", TaskID: "APP-T-0001", TaskStateRev: stringField(note.Data, "state_rev"), WorkRevision: 1,
		ImplementationSHA: facts.Head, AttemptID: "review-authority", Actor: "reviewer:agent", Runner: "chatgpt-browser", RunnerProfile: "review-profile",
		WorkerPolicyFP: policy, Covers: []string{"A1"}, ProofFingerprint: "proof-1", GateFingerprint: "gates-1", MaterialFingerprint: material,
		Verdict: "pass", Summary: "accepted", CreatedAt: "2026-09-14T00:00:00Z",
	}
	authority.ResultRevision = reviewResultFingerprint(authority)
	raw, err := json.Marshal(authority)
	if err != nil {
		t.Fatal(err)
	}
	var artifactValues map[string]any
	if err := json.Unmarshal(raw, &artifactValues); err != nil {
		t.Fatal(err)
	}
	artifactValues["kind"] = "review"
	artifactValues["risk"] = "low"
	raw, err = json.Marshal(artifactValues)
	if err != nil {
		t.Fatal(err)
	}
	artifactDir := writeExternalFetchFiles(t, map[string]string{"review.md": string(raw)})
	previousFetch := runExternalCollectFetch
	runExternalCollectFetch = func(_ context.Context, req externalFetchRequest) (externalFetchResult, error) {
		return externalFetchResult{JobID: req.JobID, ArtifactDir: artifactDir, Files: []string{"review.md"}}, nil
	}
	t.Cleanup(func() { runExternalCollectFetch = previousFetch })
	ctx := &automationCommandContext{
		StateRoot: DefaultStateRoot(), Store: store,
		Project:     RegisteredProject{ProjectID: "project-1", RepoRoot: repo, VaultRoot: vault},
		Workflow:    WorkflowFile{Data: defaultWorkflow()},
		ProjectRuns: map[string]RunStatus{"APP-T-0001": run},
	}
	report, err := ctx.collectExternal(note, run, run.Runner, "job-authority", Args{"covers": "A1"})
	if err != nil {
		t.Fatal(err)
	}
	if report.ReviewResult == nil || report.ReviewResult.Authority.Schema != reviewResultSchema {
		t.Fatalf("controller did not retain authoritative DTO: %#v", report)
	}
	if report.ReviewResult.Risk != "" {
		t.Fatalf("controller trusted non-authoritative provider risk metadata: %#v", report.ReviewResult)
	}
	if report.NextAction != externalLoopActionCloseTask || len(report.Blockers) != 0 {
		t.Fatalf("current authoritative pass did not produce close action: %#v", report)
	}
	policyResult, err := externalLoopApplyPolicy(ctx, note, externalLoopPolicyInput{
		TaskID: "APP-T-0001", RecordID: "APP-T-0001", Runner: run.Runner, JobID: "job-authority", AttemptID: run.ActiveAttemptID,
		Stage: externalLoopStageCollected, Action: report.NextAction, Reason: "authoritative review accepted", Payload: map[string]any{
			"review_result":        report.ReviewResult.Authority,
			"material_fingerprint": report.ReviewResult.MaterialFingerprint,
		},
	}, externalLoopCapsFromWorkflow(ctx.Workflow.Data))
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(policyResult.Event.PayloadJSON), &payload); err != nil {
		t.Fatal(err)
	}
	if stringValue(payload["material_fingerprint"]) != material {
		t.Fatalf("controller event did not use exact reviewed workspace material: %#v", payload)
	}
}

func TestSoftwareFactoryRepairWallClockAndPartialCapsRemainDurable(t *testing.T) {
	firstStore, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	initialAdmission, err := firstStore.AdmitExternalLoopEventWithOverrides(ExternalLoopEvent{
		ProjectID: "project-first", RecordID: "TASK-T-FIRST", ItemID: "TASK-T-FIRST", JobID: "job-first", AttemptID: "attempt-first",
		Stage: externalLoopStageApplyFailed, Action: externalLoopActionContinueThreadOnFailure,
	}, ExternalLoopCaps{MaxCycles: 4, MaxRepairContinuations: 2, MaxExternalThreads: 4, WallClockTimeoutHours: 8}, ExternalLoopCaps{MaxCycles: 9}, true, nil)
	if err != nil {
		_ = firstStore.Close()
		t.Fatal(err)
	}
	if initialAdmission.Caps.MaxCycles != 9 || initialAdmission.Caps.MaxRepairContinuations != 2 || initialAdmission.Caps.MaxExternalThreads != 4 || initialAdmission.Caps.WallClockTimeoutHours != 8 {
		_ = firstStore.Close()
		t.Fatalf("first-admission partial override did not merge with configured caps: %#v", initialAdmission.Caps)
	}
	if err := firstStore.Close(); err != nil {
		t.Fatal(err)
	}
	stateRoot := t.TempDir()
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	initial := ExternalLoopCaps{MaxCycles: 4, MaxRepairContinuations: 2, MaxExternalThreads: 4, WallClockTimeoutHours: 1}
	first, err := store.AdmitExternalLoopEvent(ExternalLoopEvent{
		ProjectID: "project-1", RecordID: "TASK-T-0010", ItemID: "TASK-T-0010", JobID: "job-old", AttemptID: "attempt-old",
		Stage: externalLoopStageBlocked, Action: externalLoopActionEscalateHuman, Status: "blocked", CreatedAt: time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339Nano),
	}, initial, true, []string{"operator approval required"})
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if _, err := store.SaveSupervisorDecision(SupervisorDecision{ProjectID: "project-1", RecordID: "TASK-T-0010", Kind: string(SupervisorDecisionStopForHuman), ContextSignal: "external_loop", Reason: "external loop escalation event " + first.Event.EventID + ": acknowledged"}); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	partial := ExternalLoopCaps{MaxCycles: 9}
	second, err := reopened.AdmitExternalLoopEventWithOverrides(ExternalLoopEvent{
		ProjectID: "project-1", RecordID: "TASK-T-0010", ItemID: "TASK-T-0010", JobID: "job-new", AttemptID: "attempt-new",
		Stage: externalLoopStageApplyFailed, Action: externalLoopActionContinueThreadOnFailure,
	}, ExternalLoopCaps{MaxCycles: 1, MaxRepairContinuations: 1, MaxExternalThreads: 1, WallClockTimeoutHours: 1}, partial, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if second.Caps.MaxCycles != 9 || second.Caps.MaxRepairContinuations != initial.MaxRepairContinuations || second.Caps.MaxExternalThreads != initial.MaxExternalThreads || second.Caps.WallClockTimeoutHours != initial.WallClockTimeoutHours {
		t.Fatalf("partial override relaxed or replaced persisted caps: %#v", second.Caps)
	}
	if second.Event.Action != externalLoopActionEscalateHuman || !strings.Contains(strings.Join(second.Blockers, "; "), "wall-clock timeout") {
		t.Fatalf("persisted wall-clock timeout did not block progression: %#v", second)
	}
}

func TestSoftwareFactoryRepairDaemonCloseSuppressionRequiresCurrentCanonicalEffect(t *testing.T) {
	note := Note{Data: map[string]any{
		"id": "TASK-T-0011", "kind": "task", "schema": "tusker.task/v7", "work_revision": 1,
		"material_fingerprint": "sha256:" + strings.Repeat("e", 64), "status": "review",
	}}
	currentMaterial := stringField(note.Data, "material_fingerprint")
	newStore := func(t *testing.T) *RuntimeStore {
		t.Helper()
		store, err := OpenRuntimeStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.Close() })
		return store
	}
	save := func(t *testing.T, store *RuntimeStore, job, status, material string) {
		t.Helper()
		event := ExternalLoopEvent{
			ProjectID: "project-1", RecordID: "TASK-T-0011", ItemID: "TASK-T-0011", JobID: job,
			Stage: externalLoopStageCollected, Action: externalLoopActionCloseTask, Status: status,
			PayloadJSON: fmt.Sprintf(`{"work_revision":1,"material_fingerprint":%q}`, material),
		}
		if _, _, err := store.SaveExternalLoopEvent(event); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("review_is_not_suppressed_by_successful_close_admission", func(t *testing.T) {
		store := newStore(t)
		save(t, store, "job-current", "ok", currentMaterial)
		handled, err := externalLoopCloseTaskRecordedForCurrentRun(store, "project-1", "TASK-T-0011", note, RunStatus{})
		if err != nil {
			t.Fatal(err)
		}
		if handled {
			t.Fatal("historical close admission suppressed review before canonical task close")
		}
	})
	t.Run("canonical_close_is_required", func(t *testing.T) {
		store := newStore(t)
		save(t, store, "job-current", "ok", currentMaterial)
		closed := note
		closed.Data = cloneMap(note.Data)
		closed.Data["status"] = "done"
		closed.Data["closed_at"] = "2026-09-14T00:00:00Z"
		handled, err := externalLoopCloseTaskRecordedForCurrentRun(store, "project-1", "TASK-T-0011", closed, RunStatus{})
		if err != nil {
			t.Fatal(err)
		}
		if !handled {
			t.Fatal("canonical close effect was not recognized")
		}
	})
	t.Run("failed_or_prior_material_close_cannot_suppress", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			status string
			mat    string
		}{
			{name: "failed", status: "failed", mat: currentMaterial},
			{name: "prior_material", status: "ok", mat: "sha256:" + strings.Repeat("f", 64)},
		} {
			t.Run(tc.name, func(t *testing.T) {
				store := newStore(t)
				closed := note
				closed.Data = cloneMap(note.Data)
				closed.Data["status"] = "done"
				closed.Data["closed_at"] = "2026-09-14T00:00:00Z"
				save(t, store, "job-"+tc.name, tc.status, tc.mat)
				handled, err := externalLoopCloseTaskRecordedForCurrentRun(store, "project-1", "TASK-T-0011", closed, RunStatus{})
				if err != nil {
					t.Fatal(err)
				}
				if handled {
					t.Fatalf("%s close incorrectly suppressed review", tc.name)
				}
			})
		}
	})
}

func TestSoftwareFactoryRepairDaemonReviewCallerDoesNotSuppressHistoricalClose(t *testing.T) {
	vault := repairTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Close caller", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{
		"status": "review", "readiness": "waiting_on_review", "next_owner": "reviewer:agent", "work_revision": 1,
	})
	project := registerAutomationTestProject(t, vault)
	note, err := resolveNote(vault, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	material := externalLoopMaterialFingerprint(note, nil)
	if material == "" {
		t.Fatal("test task did not produce a material fingerprint")
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run := RunStatus{
		ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", Runner: string(RunnerCodexExec),
		Lane: runLaneReview, LeaseState: string(LeaseStateReleased), AttemptOutcome: string(AttemptOutcomeSucceeded), WorkRevision: 1,
	}
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	closeEvent := ExternalLoopEvent{
		ProjectID: project.ProjectID, RecordID: run.RecordID, ItemID: run.ItemID, JobID: "historical-close",
		Stage: externalLoopStageCollected, Action: externalLoopActionCloseTask, Status: "ok",
		PayloadJSON: fmt.Sprintf(`{"work_revision":1,"material_fingerprint":%q}`, material),
	}
	if _, _, err := store.SaveExternalLoopEvent(closeEvent); err != nil {
		t.Fatal(err)
	}
	// A durable review result makes the real daemon caller observable: the
	// review loop must process it after rejecting the historical close, rather
	// than taking the old blanket-suppression continue path.
	result := ReviewResult{
		Schema: reviewResultSchemaV2, ProjectID: project.ProjectID, TaskID: run.RecordID,
		TaskStateRev: stringField(note.Data, "state_rev"), WorkRevision: 1, ImplementationSHA: "source-1",
		AttemptID: "review-result-attempt", Actor: "reviewer:agent", Runner: string(RunnerCodexExec),
		Covers: []string{"A1"}, ProofFingerprint: "proof-1", GateFingerprint: "gates-1",
		Verdict: "pass", Summary: "accepted", CreatedAt: "2026-09-14T00:00:00Z",
	}
	result.ResultRevision = reviewResultFingerprint(result)
	if _, err := store.SaveReviewResult(result); err != nil {
		t.Fatal(err)
	}
	daemon, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	latest := latestRunForRecord(t, daemon.store, project.ProjectID, run.RecordID)
	if !strings.Contains(latest.LastError, "typed review result recorded; awaiting review reactor") {
		t.Fatalf("daemon review caller suppressed the current review after historical close: %#v", latest)
	}
}

func TestSoftwareFactoryRepairEmptyProviderIdentityReplayFailsClosedAcrossRestart(t *testing.T) {
	stateRoot := t.TempDir()
	legacy := ExternalLoopEvent{
		ProjectID: "project-1", RecordID: "TASK-T-0012", ItemID: "TASK-T-0012", Runner: "chatgpt-browser",
		AttemptID: "attempt-1", Stage: externalLoopStageApplyFailed, Action: externalLoopActionContinueThreadOnFailure,
		PayloadJSON: `{"work_revision":1,"material_fingerprint":"material-1"}`,
	}
	legacy.IdempotencyKey = externalLoopIdempotencyKey(legacy)
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.SaveExternalLoopEvent(legacy); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	caps := ExternalLoopCaps{MaxCycles: 10, MaxRepairContinuations: 10, MaxExternalThreads: 1, WallClockTimeoutHours: 8}
	reopened, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	blocked, err := reopened.AdmitExternalLoopEvent(legacy, caps, true, nil)
	if err != nil {
		_ = reopened.Close()
		t.Fatal(err)
	}
	if !blocked.Created || blocked.Event.Action != externalLoopActionEscalateHuman || !strings.Contains(strings.Join(blocked.Blockers, "; "), "stable provider job id") {
		_ = reopened.Close()
		t.Fatalf("legacy empty-identity replay was not converted to a durable escalation: %#v", blocked)
	}
	blockedEventID := blocked.Event.EventID
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}

	restarted, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	replayed, err := restarted.AdmitExternalLoopEvent(legacy, caps, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Created || replayed.Event.EventID != blockedEventID || replayed.Event.Action != externalLoopActionEscalateHuman {
		t.Fatalf("restart did not converge on the durable identity escalation: %#v", replayed)
	}
	events, err := restarted.ListExternalLoopEvents(legacy.ProjectID, legacy.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || externalLoopCountersForEvents(events).ExternalThreads != 0 {
		t.Fatalf("unsafe replay duplicated effects or spent an untracked thread: %#v", events)
	}
}

func TestSoftwareFactoryRepairDaemonRefusesThreadOpeningWithoutProviderJobID(t *testing.T) {
	vault := repairTestVault(t)
	writeDaemonExternalLoopConfig(t, vault, `true`)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Missing provider identity", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	project := registerAutomationTestProject(t, vault)
	seedCollectedExternalApplyState(t, project, "APP-T-0001", "")
	seedApplyResultRun(t, project, "APP-T-0001", string(LeaseStateRetryQueued), string(AttemptOutcomeFailed), "apply failed")
	wfFile, err := loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	notes, err := listOperationalNotes(vault)
	if err != nil {
		t.Fatal(err)
	}
	note, err := resolveNote(vault, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}

	daemon, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	run := latestRunForRecord(t, daemon.store, project.ProjectID, "APP-T-0001")
	run, changed, err := daemon.autoAdvanceExternalApplyResult(context.Background(), project, wfFile, notes, note, run)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("missing provider identity did not produce a durable controller result")
	}

	events, err := daemon.store.ListExternalLoopEvents(project.ProjectID, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	counters := externalLoopCountersForEvents(events)
	if counters.ExternalThreads != 0 {
		t.Fatalf("empty provider identity changed external thread count: %#v", counters)
	}
	var blocked *ExternalLoopEvent
	for i := range events {
		if normalizeExternalLoopStage(events[i].Stage) == externalLoopStageApplyFailed {
			blocked = &events[i]
			break
		}
	}
	if blocked == nil || normalizeExternalLoopAction(blocked.Action) != externalLoopActionEscalateHuman || !strings.Contains(strings.Join(externalLoopBlockersFromPayload(blocked.PayloadJSON), "; "), "stable provider job id") {
		t.Fatalf("missing provider identity was not durably blocked: %#v", events)
	}
	if run.AttemptCount != 1 || strings.TrimSpace(run.CloudTaskID) != "" || !strings.Contains(run.LastError, "stable provider job id") {
		t.Fatalf("missing provider identity reached continuation dispatch: %#v", run)
	}
}

// repairTestVault mirrors the V7 fixture shape without invoking runner catalog
// discovery. The catalog probes external binaries asynchronously; this focused
// repair suite exercises persistence/controller boundaries and should not inherit
// that unrelated process-output race under -race.
func repairTestVault(t *testing.T) string {
	t.Helper()
	t.Setenv("TUSKER_STATE_ROOT", filepath.Join(t.TempDir(), "state"))
	vault := filepath.Join(t.TempDir(), defaultRepoVaultDir)
	if err := writeText(managedTuskerConfigPath(vault), `schema: tusker.config/v1
project_id: app

storage:
  root: .tusker
  generated_root: .tusker/_generated
  evidence_root: .tusker/evidence
  events_root: .tusker/events
  attempts_root: .tusker/attempts

runtime:
  lease_backend: local
  lease_ttl_minutes: 120
  mutation_mode: single_user_local

automation:
  enabled: false
  dispatch_scope: all_eligible
`); err != nil {
		t.Fatal(err)
	}
	if err := bootstrapV7(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := writeDefaultWorkflow(vault); err != nil {
		t.Fatal(err)
	}
	if err := newV7Epic(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "App work.", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	for _, domain := range []string{"backend", "frontend"} {
		dir := filepath.Join(vault, "knowledge", "domains", domain)
		if err := ensureDir(dir); err != nil {
			t.Fatal(err)
		}
		if err := writeText(filepath.Join(dir, "INDEX.md"), "# "+domain+"\n"); err != nil {
			t.Fatal(err)
		}
		if err := writeText(filepath.Join(dir, "CANON.md"), "# "+domain+" canon\n"); err != nil {
			t.Fatal(err)
		}
	}
	return vault
}

func TestSoftwareFactoryRepairPreclaimIntentRecoveryClaimsExactlyOnce(t *testing.T) {
	stateRoot := t.TempDir()
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	old := RunStatus{
		ProjectID: "project-1", RecordID: "TASK-T-0008", ItemID: "TASK-T-0008", Runner: "chatgpt-browser", Lane: runLaneExecute,
		LeaseState: string(LeaseStateReleased), LeaseOwner: "old-owner", LeaseGeneration: 7,
		AttemptOutcome: string(AttemptOutcomeSucceeded), WorkRevision: 1, AttemptCount: 3,
		CloudTaskID: "external-job-8",
	}
	if err := store.UpsertRun(old); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	note := Note{Data: map[string]any{"id": "TASK-T-0008", "work_revision": 2}}
	prepared := externalLoopDispatchLeaseSnapshot(old, externalLoopApplyDispatchRun(RegisteredProject{ProjectID: "project-1"}, note, old, "codex_exec"))
	if err := store.UpsertRunPreservingLease(prepared); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	persisted, err := reopened.FindRunScoped("project-1", "TASK-T-0008")
	if err != nil || persisted == nil {
		t.Fatalf("persisted pre-claim intent missing: %#v %v", persisted, err)
	}
	if persisted.Runner != "codex_exec" || persisted.WorkRevision != 2 || persisted.LeaseState != string(LeaseStateReleased) || persisted.AttemptOutcome != string(AttemptOutcomeSucceeded) {
		t.Fatalf("restart lost new intent or clobbered old live state: %#v", persisted)
	}
	ownership := newRunOwnershipService(reopened)
	first, err := ownership.claimExistingWithAuthorization(*persisted, "retry-attempt-1", RunAuthorization{Source: "daemon_auto", Actor: "daemon", Trigger: "poll"}, RunAttempt{AttemptID: "retry-attempt-1", Runner: "codex_exec", Lane: runLaneExecute, WorkRevision: 2})
	if err != nil || !first.Claimed {
		t.Fatalf("recovered pre-claim intent did not claim: %#v %v", first, err)
	}
	latest, err := reopened.FindRunScoped("project-1", "TASK-T-0008")
	if err != nil || latest == nil {
		t.Fatalf("claimed run missing: %#v %v", latest, err)
	}
	second, err := ownership.claimExistingWithAuthorization(*latest, "retry-attempt-2", RunAuthorization{Source: "daemon_auto", Actor: "daemon", Trigger: "poll"}, RunAttempt{AttemptID: "retry-attempt-2", Runner: "codex_exec", Lane: runLaneExecute, WorkRevision: 2})
	if err != nil {
		t.Fatal(err)
	}
	if second.Claimed {
		t.Fatal("recovery claimed the same pre-claim intent twice")
	}
	attempts, err := reopened.ListAttemptsForRun("project-1", "TASK-T-0008")
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 1 || attempts[0].AttemptID != "retry-attempt-1" {
		t.Fatalf("pre-claim recovery created duplicate attempts: %#v", attempts)
	}
}

func TestSoftwareFactoryRepairProviderReservationCapsInitialContinuationFailureAndRestart(t *testing.T) {
	stateRoot := t.TempDir()
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	seed := func(recordID string) RunStatus {
		run := RunStatus{ProjectID: "project-1", RecordID: recordID, ItemID: recordID, Runner: "chatgpt-browser", Lane: runLaneExecute, LeaseState: string(LeaseStateReleased), AttemptOutcome: string(AttemptOutcomeSucceeded), WorkRevision: 1}
		if err := store.UpsertRun(run); err != nil {
			t.Fatal(err)
		}
		return run
	}
	claim := func(run RunStatus, attemptID, lane string) (bool, error) {
		return store.claimRunLeaseWithDaemonAttempt(run, attemptID, run.LeaseGeneration+1, defaultRunLeaseTTL, time.Now().UTC(), RuntimeLeaseClaimPrecondition{
			ExpectedLeaseState: LeaseState(run.LeaseState), ExpectedOwner: run.LeaseOwner, ExpectedLeaseGeneration: run.LeaseGeneration, ExpectedWorkRevision: run.WorkRevision,
		}, RunAuthorization{Source: "daemon_auto", Actor: "daemon", Trigger: "poll"}, RunAttempt{
			AttemptID: attemptID, Runner: run.Runner, Lane: lane, WorkRevision: run.WorkRevision,
			ProviderIdempotencyKey: providerReservationKey(run.ProjectID, run.RecordID, attemptID, run.WorkRevision, lane), ExternalThreadCap: 1,
		})
	}

	first := seed("TASK-T-PROVIDER")
	claimed, err := claim(first, "initial-provider-attempt", runLaneExecute)
	if err != nil || !claimed {
		t.Fatalf("initial provider reservation was not atomic with claim: claimed=%t err=%v", claimed, err)
	}
	if count, err := store.ExternalThreadReservationCount(first.ProjectID, first.RecordID); err != nil || count != 1 {
		t.Fatalf("initial external thread count = %d, %v", count, err)
	}
	key, err := store.ProviderIdempotencyKeyForAttempt(first.ProjectID, first.RecordID, "initial-provider-attempt")
	if err != nil || key == "" {
		t.Fatalf("provider idempotency key was not persisted before Start: key=%q err=%v", key, err)
	}

	failed, err := store.FindRunScoped(first.ProjectID, first.RecordID)
	if err != nil || failed == nil {
		t.Fatal(err)
	}
	failed.LeaseState, failed.LeaseOwner = string(LeaseStateReleased), ""
	failed.AttemptOutcome, failed.ActiveAttemptID = string(AttemptOutcomeFailed), ""
	if err := store.UpsertRun(*failed); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAttempt(RunAttempt{AttemptID: "initial-provider-attempt", ProjectID: first.ProjectID, RecordID: first.RecordID, Outcome: string(AttemptOutcomeFailed)}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	failed, err = store.FindRunScoped(first.ProjectID, first.RecordID)
	if err != nil || failed == nil {
		t.Fatal(err)
	}
	claimed, err = claim(*failed, "continuation-provider-attempt", runLaneReview)
	if err != nil {
		t.Fatal(err)
	}
	if claimed {
		t.Fatal("terminal provider failure was forgotten after restart and continuation exceeded the external-thread cap")
	}
	if count, err := store.ExternalThreadReservationCount(first.ProjectID, first.RecordID); err != nil || count != 1 {
		t.Fatalf("failed provider reservation count after restart = %d, %v", count, err)
	}

	unrelated := seed("TASK-T-UNRELATED")
	claimed, err = claim(unrelated, "unrelated-provider-attempt", runLaneExecute)
	if err != nil || !claimed {
		t.Fatalf("one task's exhausted provider cap stalled unrelated work: claimed=%t err=%v", claimed, err)
	}
}

func TestSoftwareFactoryRepairProviderReservationCrashBeforeLocalWriteParksWithoutRedispatch(t *testing.T) {
	stateRoot := t.TempDir()
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	wf := defaultWorkflow()
	wf.Runners["chatgpt-browser"] = RunnerDefinition{
		Kind: string(RunnerCodexCloud), Command: "native-cloud-start", StatusCommand: "native-cloud-status {{cloud_task_id}}",
	}
	run := RunStatus{
		ProjectID: "project-1", RecordID: "TASK-T-AMBIGUOUS", ItemID: "TASK-T-AMBIGUOUS", Runner: "chatgpt-browser", RunnerHarness: "chatgpt-browser",
		Lane: runLaneExecute, LeaseState: string(LeaseStateReleased), AttemptOutcome: string(AttemptOutcomeSucceeded), WorkRevision: 1,
		EventSinkPath: filepath.Join(t.TempDir(), "never-written.events.jsonl"),
	}
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	key := providerReservationKey(run.ProjectID, run.RecordID, "ambiguous-attempt", run.WorkRevision, run.Lane)
	claimed, err := store.claimRunLeaseWithDaemonAttempt(run, "ambiguous-attempt", 1, defaultRunLeaseTTL, now, RuntimeLeaseClaimPrecondition{
		ExpectedLeaseState: LeaseStateReleased, ExpectedWorkRevision: 1,
	}, RunAuthorization{Source: "daemon_auto", Actor: "daemon"}, RunAttempt{
		AttemptID: "ambiguous-attempt", Runner: run.Runner, Lane: run.Lane, WorkRevision: 1, ProviderIdempotencyKey: key, ExternalThreadCap: 2,
	})
	if err != nil || !claimed {
		t.Fatalf("claim/reservation failed: claimed=%t err=%v", claimed, err)
	}
	claimedRun, err := store.FindRunScoped(run.ProjectID, run.RecordID)
	if err != nil || claimedRun == nil {
		t.Fatal(err)
	}
	parked, changed, err := (&Daemon{store: store}).recoverUnstartedDirectedClaim(context.Background(), wf, *claimedRun, now.Add(time.Second))
	if err != nil || !changed {
		t.Fatalf("ambiguous provider accept blocked daemon reconciliation: changed=%t err=%v", changed, err)
	}
	if parked.LeaseState != string(LeaseStateParkedNoProgress) || parked.AttemptOutcome != string(AttemptOutcomeBlocked) || !parked.Terminal || !strings.Contains(parked.LastError, "ambiguous") {
		t.Fatalf("ambiguous provider accept was not durably fenced: %#v", parked)
	}
	if got, err := store.ProviderIdempotencyKeyForAttempt(run.ProjectID, run.RecordID, "ambiguous-attempt"); err != nil || got != key {
		t.Fatalf("crash lost provider reservation: got=%q want=%q err=%v", got, key, err)
	}
	if err := store.UpsertRun(parked); err != nil {
		t.Fatal(err)
	}
	unrelated := RunStatus{ProjectID: run.ProjectID, RecordID: "TASK-T-AFTER-AMBIGUOUS", ItemID: "TASK-T-AFTER-AMBIGUOUS", Runner: string(RunnerCodexExec), Lane: runLaneExecute, LeaseState: string(LeaseStateReleased), AttemptOutcome: string(AttemptOutcomeSucceeded), WorkRevision: 1}
	if err := store.UpsertRun(unrelated); err != nil {
		t.Fatal(err)
	}
	progressed, err := store.claimRunLeaseWithDaemonAttempt(unrelated, "unrelated-local-attempt", 1, defaultRunLeaseTTL, now.Add(2*time.Second), RuntimeLeaseClaimPrecondition{
		ExpectedLeaseState: LeaseStateReleased, ExpectedWorkRevision: 1, ProjectConcurrencyLimit: 1,
	}, RunAuthorization{Source: "daemon_auto", Actor: "daemon"}, RunAttempt{AttemptID: "unrelated-local-attempt", Runner: unrelated.Runner, Lane: unrelated.Lane, WorkRevision: 1})
	if err != nil || !progressed {
		t.Fatalf("ambiguous provider task stalled unrelated dispatch: claimed=%t err=%v", progressed, err)
	}
}

func TestSoftwareFactoryRepairProviderReservationUsesHandoffIdAndLookup(t *testing.T) {
	root := t.TempDir()
	prompt := filepath.Join(root, "prompt.md")
	if err := writeText(prompt, "review this\n"); err != nil {
		t.Fatal(err)
	}
	key := "tusker-provider-key"
	executor := &fakeCodexCloudExecutor{outputs: [][]byte{
		[]byte(`{"task_id":"tusker-provider-key","status":"queued"}`),
		[]byte(`{"task_id":"tusker-provider-key","status":"running"}`),
	}}
	runner := &CodexCloudRunner{Config: CodexCloudConfig{
		EnvironmentID: "chatgpt-browser", ApplyMode: "manual", PRMode: "none", ExternalCollect: true,
		Command: "chatgpt-handoff tusker-start --kind review --json", StatusCommand: "chatgpt-handoff tusker-status --job {{cloud_task_id}} --json", CollectCommand: "chatgpt-handoff tusker-collect --job {{cloud_task_id}} --json",
	}, Executor: executor}
	if !runner.SupportsReservationLookup() {
		t.Fatal("handoff adapter reservation lookup was not detected")
	}
	started, err := runner.Start(context.Background(), StartRequest{
		AttemptID: "attempt-1", ProviderIdempotencyKey: key, WorkspacePath: root, PromptPath: prompt,
		EventSinkPath: filepath.Join(root, "events.jsonl"), RawLogPath: filepath.Join(root, "raw.log"), StatusPath: filepath.Join(root, "status.json"),
	})
	if err != nil || started.CloudTaskID != key {
		t.Fatalf("handoff start did not finalize reservation: result=%#v err=%v", started, err)
	}
	if !strings.Contains(executor.requests[0].Command, "--id '"+key+"'") {
		t.Fatalf("handoff start did not receive durable idempotency key: %q", executor.requests[0].Command)
	}
	assertContainsEnv(t, executor.requests[0].Env, "TUSKER_PROVIDER_IDEMPOTENCY_KEY="+key)
	lookedUp, err := runner.Reconcile(context.Background(), ReconcileRequest{CloudTaskID: key})
	if err != nil || lookedUp.CloudTaskID != key || lookedUp.CloudStatus != "running" {
		t.Fatalf("handoff reservation lookup failed: result=%#v err=%v", lookedUp, err)
	}
	if !strings.Contains(executor.requests[1].Command, "tusker-status --job "+key) {
		t.Fatalf("handoff recovery did not look up the reserved job: %q", executor.requests[1].Command)
	}
}

func TestSoftwareFactoryRepairCodexCloudStartCheckpointPreventsRedispatch(t *testing.T) {
	stateRoot := t.TempDir()
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := ensureDir(workspace); err != nil {
		t.Fatal(err)
	}
	promptPath := filepath.Join(root, "prompt.md")
	eventSinkPath := filepath.Join(root, "cloud.events.jsonl")
	rawLogPath := filepath.Join(root, "cloud.raw.log")
	if err := writeText(promptPath, "start this cloud task\n"); err != nil {
		t.Fatal(err)
	}
	executor := &fakeCodexCloudExecutor{outputs: [][]byte{[]byte(`{"task_id":"cloud-task-crash-window","status":"queued","environment_id":"env-prod"}`)}}
	runner := &CodexCloudRunner{Config: codexCloudTestConfig("manual", "none"), Executor: executor}
	if _, err := runner.Start(context.Background(), StartRequest{
		ProjectID: "project-1", RecordID: "TASK-T-CLOUD-CRASH", ItemID: "TASK-T-CLOUD-CRASH", AttemptID: "attempt-cloud-crash",
		WorkRevision: 3, WorkspacePath: workspace, PromptPath: promptPath, EventSinkPath: eventSinkPath, RawLogPath: rawLogPath,
		StatusPath: filepath.Join(root, "cloud.status.json"),
	}); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	wf := defaultWorkflow()
	wf.Runners["chatgpt-browser"] = RunnerDefinition{
		Kind: string(RunnerCodexCloud), Command: "native-cloud-start", StatusCommand: "native-cloud-status {{cloud_task_id}}",
	}
	run := RunStatus{
		ProjectID: "project-1", RecordID: "TASK-T-CLOUD-CRASH", ItemID: "TASK-T-CLOUD-CRASH", Runner: "chatgpt-browser", RunnerHarness: "chatgpt-browser",
		Lane: runLaneExecute, LeaseState: string(LeaseStateClaimed), LeaseOwner: "attempt-cloud-crash", LeaseGeneration: 1,
		AttemptOutcome: string(AttemptOutcomeNone), ActiveAttemptID: "attempt-cloud-crash", WorkRevision: 3,
		AttemptCount: 1, WorkspacePath: workspace, PromptPath: promptPath, EventSinkPath: eventSinkPath, RawLogPath: rawLogPath,
		StatusPath: filepath.Join(root, "cloud.status.json"), StartedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano),
	}
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAttempt(RunAttempt{
		AttemptID: "attempt-cloud-crash", ProjectID: run.ProjectID, RecordID: run.RecordID, ItemID: run.ItemID,
		Runner: run.Runner, Lane: run.Lane, WorkRevision: run.WorkRevision, WorkspacePath: workspace,
		PromptPath: promptPath, EventSinkPath: eventSinkPath, RawLogPath: rawLogPath, StatusPath: run.StatusPath,
		ProviderIdempotencyKey: "tusker-cloud-crash-key", Outcome: string(AttemptOutcomeNone), ProcessPID: 0, StartedAt: run.StartedAt,
	}); err != nil {
		t.Fatal(err)
	}

	daemon := &Daemon{store: store}
	recovered, changed, err := daemon.recoverUnstartedDirectedClaim(context.Background(), wf, run, now.Add(time.Second))
	if err != nil || !changed {
		t.Fatalf("durable cloud start was not adopted after simulated crash: changed=%t err=%v run=%#v", changed, err, recovered)
	}
	if recovered.CloudTaskID != "cloud-task-crash-window" || recovered.LeaseState != string(LeaseStateClaimed) || recovered.ActiveAttemptID != run.ActiveAttemptID {
		t.Fatalf("cloud checkpoint lost lease/task identity: %#v", recovered)
	}
	attempts, err := store.ListAttemptsForRun(run.ProjectID, run.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 1 || attempts[0].CloudTaskID != recovered.CloudTaskID {
		t.Fatalf("cloud checkpoint did not atomically update attempt: %#v", attempts)
	}
	if key, err := store.ProviderIdempotencyKeyForAttempt(run.ProjectID, run.RecordID, run.ActiveAttemptID); err != nil || key != "tusker-cloud-crash-key" {
		t.Fatalf("provider job finalization replaced its reservation key: key=%q err=%v", key, err)
	}

	// A restarted controller sees the already-adopted operation and proceeds to
	// reconcile it; it must not requeue the consumed claim or call Start again.
	restarted, changed, err := daemon.recoverUnstartedDirectedClaim(context.Background(), wf, recovered, now.Add(2*time.Second))
	if err != nil || changed {
		t.Fatalf("adopted cloud start was not idempotent: changed=%t err=%v run=%#v", changed, err, restarted)
	}
	if len(executor.requests) != 1 {
		t.Fatalf("cloud provider was redispatched after recovery: %d starts", len(executor.requests))
	}
}
