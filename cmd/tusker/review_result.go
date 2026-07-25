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

func (s *RuntimeStore) SaveReviewResult(result ReviewResult) (bool, error) {
	if result.ResultRevision == "" {
		result.ResultRevision = reviewResultFingerprint(result)
	}
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
	if len(summary) > 800 {
		return tuskerError(errorInvalidArg, "review summary exceeds 800 characters")
	}
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
	proofFingerprint, gateFingerprint := reviewFingerprint(note, "proof"), reviewFingerprint(note, "gates")
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
	actor := firstNonEmpty(args.String("by"), "reviewer:agent")
	wf, wfErr := loadWorkflow(vault)
	if wfErr != nil {
		return wfErr
	}
	if actor != reviewerActorForNote(wf.Data.Reviewer.Actor, note) {
		return tuskerError(errorInvalidTransition, "reviewer actor is not authorized for this task")
	}
	result := ReviewResult{Schema: reviewResultSchema, ProjectID: v7ProjectID(vault), TaskID: id, TaskStateRev: state, WorkRevision: intField(note.Data, "work_revision"), ImplementationSHA: impl, AttemptID: attemptID, Actor: actor, Runner: attempt.Runner, RunnerProfile: attempt.Runner, Covers: covers, ProofFingerprint: proofFingerprint, GateFingerprint: gateFingerprint, Verdict: verdict, Summary: summary, Findings: findings, EvidenceRefs: uniqueStrings(splitCSV(args.String("evidence-ref"))), CreatedAt: time.Now().UTC().Format(time.RFC3339)}
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
func reviewFingerprint(note Note, scope string) string {
	sum := sha256.Sum256([]byte(scope + "\x00" + stringField(note.Data, "state_rev") + "\x00" + note.Body))
	return "sha256:" + hex.EncodeToString(sum[:])
}
func reviewResultFingerprint(r ReviewResult) string {
	r.ResultRevision = ""
	r.CreatedAt = ""
	raw, _ := json.Marshal(r)
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}
