package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

const reviewResultSchema = "tusker.review-result/v1"

const (
	reviewResultMaxSummary       = 800
	reviewResultMaxFindings      = 20
	reviewResultMaxFindingChars  = 800
	reviewResultMaxEvidenceRefs  = 20
	reviewResultMaxEvidenceChars = 400
)

func (s *RuntimeStore) SaveReviewResult(result ReviewResult) (bool, error) {
	if err := normalizeReviewResult(&result); err != nil {
		return false, err
	}
	expectedRevision := reviewResultFingerprint(result)
	if result.ResultRevision != "" && result.ResultRevision != expectedRevision {
		return false, tuskerError(errorInvalidArg, "review result revision does not match its immutable payload")
	}
	result.ResultRevision = expectedRevision
	raw, err := json.Marshal(result)
	if err != nil {
		return false, err
	}
	existing := ""
	err = s.queryRowScan(`SELECT result_json FROM review_results WHERE project_id=? AND task_id=? AND work_revision=? AND attempt_id=?`, []any{result.ProjectID, result.TaskID, result.WorkRevision, result.AttemptID}, &existing)
	if err == nil {
		var prior ReviewResult
		if json.Unmarshal([]byte(existing), &prior) == nil && prior.ResultRevision == result.ResultRevision {
			return true, nil
		}
		return false, tuskerError(errorInvalidTransition, "conflicting reviewer verdict already exists")
	}
	if !strings.Contains(err.Error(), "no rows") {
		return false, err
	}
	_, err = s.exec(`INSERT INTO review_results(project_id,task_id,work_revision,attempt_id,result_json) VALUES(?,?,?,?,?)`, result.ProjectID, result.TaskID, result.WorkRevision, result.AttemptID, string(raw))
	return false, err
}

func (s *RuntimeStore) HasReviewResult(projectID, taskID string, workRevision int, attemptID string) (bool, error) {
	var value string
	err := s.queryRowScan(`SELECT result_json FROM review_results WHERE project_id=? AND task_id=? AND work_revision=? AND attempt_id=?`, []any{projectID, taskID, workRevision, attemptID}, &value)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// HasReviewResultForWork is deliberately work-revision scoped. A valid result
// is a terminal reviewer output for this handoff; until the later deterministic
// reactor consumes it, dispatching a fresh reviewer would create duplicate and
// potentially conflicting judgments.
func (s *RuntimeStore) HasReviewResultForWork(projectID, taskID string, workRevision int) (bool, error) {
	var count int
	err := s.queryRowScan(`SELECT COUNT(*) FROM review_results WHERE project_id=? AND task_id=? AND work_revision=?`, []any{projectID, taskID, workRevision}, &count)
	return count > 0, err
}

func reviewSubmitCmd(args Args) error {
	vault, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	id := strings.ToUpper(strings.TrimSpace(firstNonEmpty(args.String("id"), args.String("_pos0"))))
	attemptID := strings.TrimSpace(args.String("attempt"))
	verdict := strings.TrimSpace(args.String("verdict"))
	if id == "" || attemptID == "" {
		return tuskerError(errorMissingArg, "Usage: tusker review submit <TASK-ID> --attempt <ATTEMPT-ID> --verdict pass|changes_requested|blocked --covers A1")
	}
	note, err := resolveV7Note(vault, id, "task")
	if err != nil {
		return err
	}
	if stringField(note.Data, "status") != "review" {
		return tuskerError(errorInvalidTransition, "review result requires task status review")
	}
	covers := uniqueStrings(splitCSV(args.String("covers")))
	summary := strings.TrimSpace(args.String("summary"))
	findings := uniqueStrings(splitCSV(args.String("finding")))
	blocker := strings.TrimSpace(args.String("blocker"))
	switch verdict {
	case "pass":
		accepted := uniqueStrings(v7AcceptanceIDs(note.Body))
		sort.Strings(accepted)
		got := append([]string(nil), covers...)
		sort.Strings(got)
		if strings.Join(accepted, ",") != strings.Join(got, ",") {
			return tuskerError(errorInvalidArg, "pass requires exact complete acceptance coverage")
		}
		proof, proofErr := loadV7ProofReport(vault, id)
		if proofErr != nil {
			return proofErr
		}
		if proof.Status != "satisfied" || len(proof.OpenGates) != 0 {
			return tuskerError(errorInvalidTransition, "pass requires currently satisfied objective proof and gates")
		}
	case "changes_requested":
		if len(findings) == 0 {
			return tuskerError(errorInvalidArg, "changes_requested requires an actionable finding")
		}
	case "blocked":
		if blocker != "machine" && blocker != "infrastructure" && blocker != "human" {
			return tuskerError(errorInvalidArg, "blocked requires --blocker machine|infrastructure|human")
		}
		if blocker == "human" {
			humanBlocked, humanErr := reviewHasOpenHumanBlocker(vault, id)
			if humanErr != nil {
				return humanErr
			}
			if !humanBlocked {
				return tuskerError(errorInvalidTransition, "blocked human result requires an open human-owned gate")
			}
		}
	default:
		return tuskerError(errorInvalidArg, "verdict must be pass, changes_requested, or blocked")
	}
	state := stringField(note.Data, "state_rev")
	impl := firstNonEmpty(stringField(note.Data, "source_sha"), stringField(note.Data, "source_commit"))
	if impl == "" {
		return tuskerError(errorInvalidTransition, "review result requires implementation source SHA")
	}
	if expected := strings.TrimSpace(args.String("task-rev")); expected == "" || expected != state {
		return tuskerError(errorInvalidTransition, "stale task revision")
	}
	if expected := strings.TrimSpace(args.String("source-sha")); expected == "" || expected != impl {
		return tuskerError(errorInvalidTransition, "stale implementation source SHA")
	}
	proofFingerprint, gateFingerprint, snapshotErr := reviewObjectiveSnapshots(vault, note)
	if snapshotErr != nil {
		return snapshotErr
	}
	if expected := strings.TrimSpace(args.String("proof-fingerprint")); expected == "" || expected != proofFingerprint {
		return tuskerError(errorInvalidTransition, "stale proof fingerprint")
	}
	if expected := strings.TrimSpace(args.String("gate-fingerprint")); expected == "" || expected != gateFingerprint {
		return tuskerError(errorInvalidTransition, "stale gate fingerprint")
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	attempt, err := store.ReviewAttempt(attemptID)
	if err != nil {
		return err
	}
	if expected := atoiSafe(args.String("work-rev")); expected == 0 || expected != intField(note.Data, "work_revision") || attempt.RecordID != id || attempt.Lane != runLaneReview || attempt.WorkRevision != expected {
		return tuskerError(errorInvalidTransition, "stale or unauthorized reviewer attempt")
	}
	run, err := activeReviewRunForAttempt(store, v7ProjectID(vault), id, attempt)
	if err != nil {
		return err
	}
	actor := firstNonEmpty(args.String("by"), "reviewer:agent")
	wf, wfErr := loadWorkflow(vault)
	if wfErr != nil {
		return wfErr
	}
	if actor != reviewerActorForNote(wf.Data.Reviewer.Actor, note) {
		return tuskerError(errorInvalidTransition, "reviewer actor is not authorized for this task")
	}
	result := ReviewResult{Schema: reviewResultSchema, ProjectID: v7ProjectID(vault), TaskID: id, TaskStateRev: state, WorkRevision: intField(note.Data, "work_revision"), ImplementationSHA: impl, AttemptID: attemptID, Actor: actor, Runner: run.Runner, RunnerProfile: run.RunnerProfile, Covers: covers, ProofFingerprint: proofFingerprint, GateFingerprint: gateFingerprint, Verdict: verdict, Blocker: blocker, Summary: summary, Findings: findings, EvidenceRefs: uniqueStrings(splitCSV(args.String("evidence-ref"))), CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	if err := normalizeReviewResult(&result); err != nil {
		return err
	}
	result.ResultRevision = reviewResultFingerprint(result)
	replay, err := store.SaveReviewResult(result)
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "replay": replay, "result": result})
	} else {
		fmt.Printf("Recorded %s review result for %s. No merge, landing, close, or ref move occurred.\n", verdict, id)
	}
	return nil
}

func activeReviewRunForAttempt(store *RuntimeStore, projectID, taskID string, attempt RunAttempt) (RunStatus, error) {
	if attempt.ProjectID != projectID || attempt.RecordID != taskID || attempt.Lane != runLaneReview {
		return RunStatus{}, tuskerError(errorInvalidTransition, "reviewer attempt is not authorized for this project and task")
	}
	runs, err := store.ListRuns()
	if err != nil {
		return RunStatus{}, err
	}
	for _, run := range runs {
		if run.ProjectID != projectID || run.RecordID != taskID || run.Lane != runLaneReview || run.WorkRevision != attempt.WorkRevision || run.ActiveAttemptID != attempt.AttemptID {
			continue
		}
		if !isDispatchingLeaseState(run.LeaseState) || run.Runner == "" || run.RunnerProfile == "" || run.Runner != attempt.Runner {
			break
		}
		return run, nil
	}
	return RunStatus{}, tuskerError(errorInvalidTransition, "reviewer attempt is not the current active review attempt")
}

func normalizeReviewResult(result *ReviewResult) error {
	result.Schema = strings.TrimSpace(result.Schema)
	result.ProjectID = strings.TrimSpace(result.ProjectID)
	result.TaskID = strings.TrimSpace(result.TaskID)
	result.TaskStateRev = strings.TrimSpace(result.TaskStateRev)
	result.ImplementationSHA = strings.TrimSpace(result.ImplementationSHA)
	result.AttemptID = strings.TrimSpace(result.AttemptID)
	result.Actor = strings.TrimSpace(result.Actor)
	result.Runner = strings.TrimSpace(result.Runner)
	result.RunnerProfile = strings.TrimSpace(result.RunnerProfile)
	result.Verdict = strings.TrimSpace(result.Verdict)
	result.Blocker = strings.TrimSpace(result.Blocker)
	result.Summary = strings.TrimSpace(result.Summary)
	result.ProofFingerprint = strings.TrimSpace(result.ProofFingerprint)
	result.GateFingerprint = strings.TrimSpace(result.GateFingerprint)
	result.Covers = sortedUniqueStrings(result.Covers)
	result.Findings = sortedUniqueStrings(result.Findings)
	result.EvidenceRefs = sortedUniqueStrings(result.EvidenceRefs)
	if result.Schema != reviewResultSchema || result.ProjectID == "" || result.TaskID == "" || result.TaskStateRev == "" || result.WorkRevision <= 0 || result.ImplementationSHA == "" || result.AttemptID == "" || result.Actor == "" || result.Runner == "" || result.RunnerProfile == "" || result.ProofFingerprint == "" || result.GateFingerprint == "" {
		return tuskerError(errorInvalidArg, "review result is missing immutable authority fields")
	}
	if result.Summary == "" || len(result.Summary) > reviewResultMaxSummary {
		return tuskerError(errorInvalidArg, "review summary must be non-empty and at most 800 characters")
	}
	if len(result.Findings) > reviewResultMaxFindings || len(result.EvidenceRefs) > reviewResultMaxEvidenceRefs {
		return tuskerError(errorInvalidArg, "review result has too many findings or evidence references")
	}
	for _, finding := range result.Findings {
		if finding == "" || len(finding) > reviewResultMaxFindingChars {
			return tuskerError(errorInvalidArg, "review finding must be non-empty and at most 800 characters")
		}
	}
	for _, ref := range result.EvidenceRefs {
		if ref == "" || len(ref) > reviewResultMaxEvidenceChars {
			return tuskerError(errorInvalidArg, "review evidence reference must be non-empty and at most 400 characters")
		}
	}
	switch result.Verdict {
	case "pass":
		if len(result.Covers) == 0 || len(result.Findings) != 0 || result.Blocker != "" {
			return tuskerError(errorInvalidArg, "pass result cannot carry findings or a blocker and must cover acceptance")
		}
	case "changes_requested":
		if len(result.Findings) == 0 || result.Blocker != "" {
			return tuskerError(errorInvalidArg, "changes_requested requires findings and cannot carry a blocker")
		}
	case "blocked":
		if len(result.Findings) != 0 || (result.Blocker != "machine" && result.Blocker != "infrastructure" && result.Blocker != "human") {
			return tuskerError(errorInvalidArg, "blocked requires one typed blocker and cannot carry findings")
		}
	default:
		return tuskerError(errorInvalidArg, "review result verdict is invalid")
	}
	return nil
}

func sortedUniqueStrings(values []string) []string {
	set := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			set[value] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
func reviewFingerprint(note Note, scope string) string {
	sum := sha256.Sum256([]byte(scope + "\x00" + stringField(note.Data, "state_rev") + "\x00" + note.Body))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func reviewObjectiveSnapshots(vault string, note Note) (string, string, error) {
	proof, err := loadV7ProofReport(vault, stringField(note.Data, "id"))
	if err != nil {
		return "", "", err
	}
	proofView := struct {
		Status                           string
		Acceptance, Missing, ModeMissing []string
		Covered                          map[string][]string
		InlineRows                       []v7VerificationRow
		Evidence                         []string
		ProofOwner                       map[string]string
	}{proof.Status, proof.Acceptance, proof.Missing, proof.ModeMissing, proof.Covered, proof.InlineRows, proof.Evidence, proof.ProofOwner}
	proofRaw, _ := json.Marshal(proofView)
	proofSum := sha256.Sum256(proofRaw)
	idx, err := loadV7Index(vault)
	if err != nil {
		return "", "", err
	}
	type gateView struct {
		ID, Status, StateRev, Owner string
		Blocking                    bool
		Covers                      []string
	}
	gates := []gateView{}
	for _, gate := range idx.Gates {
		if v7GateTouchesTask(gate, stringField(note.Data, "id")) {
			gates = append(gates, gateView{stringField(gate.Data, "id"), stringField(gate.Data, "status"), stringField(gate.Data, "state_rev"), stringField(gate.Data, "owner"), boolField(gate.Data, "blocking"), normalizeList(gate.Data["covers"])})
		}
	}
	sort.Slice(gates, func(i, j int) bool { return gates[i].ID < gates[j].ID })
	gateRaw, _ := json.Marshal(gates)
	gateSum := sha256.Sum256(gateRaw)
	return "sha256:" + hex.EncodeToString(proofSum[:]), "sha256:" + hex.EncodeToString(gateSum[:]), nil
}

func reviewHasOpenHumanBlocker(vaultPath, taskID string) (bool, error) {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return false, err
	}
	for _, gate := range idx.Gates {
		if !v7GateTouchesTask(gate, taskID) || stringField(gate.Data, "status") != "open" || !boolField(gate.Data, "blocking") {
			continue
		}
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(stringField(gate.Data, "owner"))), "human:") {
			return true, nil
		}
	}
	return false, nil
}
func reviewResultFingerprint(r ReviewResult) string {
	r.ResultRevision = ""
	r.CreatedAt = ""
	raw, _ := json.Marshal(r)
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}
