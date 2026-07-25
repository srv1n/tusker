package main

// The completion reactor is deliberately boring.  A review result is already
// the reviewer’s complete authority; this file turns that immutable record
// into a resumable sequence of mechanical Git/tracker operations.  In
// particular, it never asks a model to resolve a clean merge or choose a
// lifecycle transition.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	completionPhasePlanned         = "planned"
	completionPhaseStaging         = "staging"
	completionPhaseStaged          = "staged"
	completionPhaseGated           = "gated"
	completionPhaseRefIntent       = "ref_intent"
	completionPhaseRefCommitted    = "ref_committed"
	completionPhaseAudited         = "audited"
	completionPhaseWoken           = "woken"
	completionPhaseFailureIntent   = "failure_intent"
	completionPhaseFailureHandback = "failure_handback"
	completionPhaseFailureReleased = "failure_released"
	completionPhaseFailureAudited  = "failure_audited"
	completionPhaseTerminal        = "terminal"
)

type completionTransaction struct {
	Schema            string `json:"schema"`
	ID                string `json:"id"`
	ProjectID         string `json:"project_id"`
	TaskID            string `json:"task_id"`
	WorkRevision      int    `json:"work_revision"`
	ImplementationSHA string `json:"implementation_sha"`
	ReviewAttempt     string `json:"review_attempt"`
	ResultRevision    string `json:"result_revision"`
	IntegrationBase   string `json:"integration_base"`
	IntegrationRef    string `json:"integration_ref"`
	Phase             string `json:"phase"`
	StagedSHA         string `json:"staged_sha,omitempty"`
	Failure           string `json:"failure,omitempty"`
	Disposition       string `json:"disposition,omitempty"`
	CreatedAt         string `json:"created_at"`
	UpdatedAt         string `json:"updated_at"`
}

// completionReactorCrashHook is test-only fault injection. Production leaves
// it nil. Hooks run after an idempotent side effect and before its phase is
// persisted, which is the only crash window worth proving.
var completionReactorCrashHook func(string, *completionTransaction) error

func injectCompletionReactorCrash(point string, transaction *completionTransaction) error {
	if completionReactorCrashHook == nil {
		return nil
	}
	return completionReactorCrashHook(point, transaction)
}

func completionTransactionID(projectID string, result ReviewResult, integrationBase string) string {
	parts := []string{projectID, result.TaskID, fmt.Sprintf("%d", result.WorkRevision), result.ImplementationSHA, result.AttemptID, result.ResultRevision, integrationBase}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "completion:" + hex.EncodeToString(sum[:])
}

func (s *RuntimeStore) CompletionTransaction(id string) (*completionTransaction, error) {
	var raw string
	err := s.queryRowScan(`SELECT transaction_json FROM completion_transactions WHERE transaction_id=?`, []any{id}, &raw)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			return nil, nil
		}
		return nil, err
	}
	var transaction completionTransaction
	if err := json.Unmarshal([]byte(raw), &transaction); err != nil {
		return nil, err
	}
	return &transaction, nil
}

func (s *RuntimeStore) CompletionTransactionForResult(projectID, taskID, resultRevision string) (*completionTransaction, error) {
	var raw string
	err := s.queryRowScan(`SELECT transaction_json FROM completion_transactions WHERE project_id=? AND task_id=? AND result_revision=? ORDER BY updated_at DESC LIMIT 1`, []any{projectID, taskID, resultRevision}, &raw)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			return nil, nil
		}
		return nil, err
	}
	var transaction completionTransaction
	if err := json.Unmarshal([]byte(raw), &transaction); err != nil {
		return nil, err
	}
	return &transaction, nil
}

func (s *RuntimeStore) SaveCompletionTransaction(transaction *completionTransaction) error {
	if transaction == nil || transaction.ID == "" || transaction.ProjectID == "" || transaction.TaskID == "" || transaction.ResultRevision == "" || transaction.Phase == "" {
		return tuskerError(errorInvalidArg, "completion transaction is missing immutable identity fields")
	}
	transaction.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if transaction.CreatedAt == "" {
		transaction.CreatedAt = transaction.UpdatedAt
	}
	raw, err := json.Marshal(transaction)
	if err != nil {
		return err
	}
	_, err = s.exec(`INSERT INTO completion_transactions(transaction_id,project_id,task_id,result_revision,phase,transaction_json,updated_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(transaction_id) DO UPDATE SET phase=excluded.phase, transaction_json=excluded.transaction_json, updated_at=excluded.updated_at`, transaction.ID, transaction.ProjectID, transaction.TaskID, transaction.ResultRevision, transaction.Phase, string(raw), transaction.UpdatedAt)
	return err
}

func (s *RuntimeStore) ListReviewResults(projectID string) ([]ReviewResult, error) {
	rows, err := s.query(`SELECT result_json FROM review_results WHERE project_id=? ORDER BY task_id, work_revision, attempt_id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReviewResult
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var result ReviewResult
		if err := json.Unmarshal([]byte(raw), &result); err != nil {
			return nil, err
		}
		out = append(out, result)
	}
	return out, rows.Err()
}

// reconcileReviewCompletion consumes saved typed results.  disabled and
// legacy deliberately do nothing here: their prior authority paths retain
// exact compatibility.  Shadow records a frozen observational plan only.
func (d *Daemon) reconcileReviewCompletion(project RegisteredProject, wf Workflow) error {
	mode := completionReactorMode(wf.CompletionReactor.Effective)
	if mode == completionReactorModeDisabled || mode == completionReactorModeLegacy || mode == "" {
		return nil
	}
	results, err := d.store.ListReviewResults(project.ProjectID)
	if err != nil {
		return err
	}
	for _, result := range results {
		if err := d.reactToReviewResult(project, wf, result, mode); err != nil {
			return err
		}
	}
	return nil
}

func (d *Daemon) reactToReviewResult(project RegisteredProject, wf Workflow, result ReviewResult, mode completionReactorMode) error {
	note, err := resolveV7Note(project.VaultRoot, result.TaskID, "task")
	if err != nil {
		return err
	}
	if prior, err := d.store.CompletionTransactionForResult(project.ProjectID, result.TaskID, result.ResultRevision); err != nil {
		return err
	} else if prior != nil && mode == completionReactorModeAuthoritative {
		switch result.Verdict {
		case "changes_requested":
			return d.beginCompletionDisposition(project, result, prior, "rework", strings.Join(result.Findings, "\n"))
		case "blocked":
			return d.beginCompletionDisposition(project, result, prior, "park", "Review blocked ("+result.Blocker+"): "+result.Summary)
		case "pass":
			return d.completePassingReview(project, result, prior)
		}
	}
	wave, hasWave := completionWaveForReviewedTask(project.VaultRoot, note)
	if !hasWave {
		// Binding after the reviewer froze TaskStateRev would invalidate the
		// result. requestV7ReviewAfterHandoff owns singleton creation before
		// reviewer dispatch; a result that somehow lacks that binding remains
		// awaiting manual repair without lifecycle or Git mutation.
		return nil
	}
	integrationRef := "refs/heads/" + v7WaveIntegrationBranch(wave)
	// A newly armed wave may deliberately be ref-less.  Its authorization
	// freezes integration_base_sha; the first successful completion creates the
	// integration ref with a zero-old CAS, never by an eager ensure/create.
	base := strings.TrimSpace(stringField(wave.Data, "integration_base_sha"))
	if v7GitRepo(project.RepoRoot) && gitRefExists(project.RepoRoot, integrationRef) {
		base, _ = gitOutputTrim(project.RepoRoot, "rev-parse", integrationRef)
	}
	if base == "" {
		base = "unresolved"
	}
	transaction := completionTransaction{
		Schema: "tusker.completion-transaction/v1", ProjectID: project.ProjectID, TaskID: result.TaskID,
		WorkRevision: result.WorkRevision, ImplementationSHA: result.ImplementationSHA, ReviewAttempt: result.AttemptID,
		ResultRevision: result.ResultRevision, IntegrationBase: base, IntegrationRef: integrationRef, Phase: completionPhasePlanned,
	}
	transaction.ID = completionTransactionID(project.ProjectID, result, base)
	if prior, err := d.store.CompletionTransaction(transaction.ID); err != nil {
		return err
	} else if prior != nil {
		transaction = *prior
	}
	if err := d.store.SaveCompletionTransaction(&transaction); err != nil {
		return err
	}
	if mode == completionReactorModeShadow {
		return nil
	}

	switch result.Verdict {
	case "changes_requested":
		return d.beginCompletionDisposition(project, result, &transaction, "rework", strings.Join(result.Findings, "\n"))
	case "blocked":
		// A typed blocker is a park, not a disguised completion or automatic
		// rework. review submit already proved a genuine gate for human.
		return d.beginCompletionDisposition(project, result, &transaction, "park", "Review blocked ("+result.Blocker+"): "+result.Summary)
	case "pass":
		return d.completePassingReview(project, result, &transaction)
	default:
		return tuskerError(errorInvalidArg, "stored review result has unknown verdict")
	}
}

func completionWaveForReviewedTask(vaultPath string, task Note) (Note, bool) {
	waveID := stringField(task.Data, "wave")
	if waveID == "" {
		return Note{}, false
	}
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return Note{}, false
	}
	wave, ok := idx.Waves[waveID]
	if !ok {
		return Note{}, false
	}
	if v7ImplicitDeliveryUnit(wave) {
		members := normalizeList(wave.Data["members"])
		return wave, len(members) == 1 && members[0] == resultTaskID(task) && stringField(wave.Data, "delivery_task") == resultTaskID(task)
	}
	auth := waveAuthorizationProjection(vaultPath, idx, wave)
	return wave, stringField(auth, "state") == "armed" && !boolFromAny(auth["stale"])
}

func resultTaskID(task Note) string {
	return strings.ToUpper(strings.TrimSpace(stringField(task.Data, "id")))
}

func (d *Daemon) completePassingReview(project RegisteredProject, result ReviewResult, transaction *completionTransaction) error {
	if transaction.Phase == completionPhaseTerminal {
		return nil
	}
	if completionFailurePhase(transaction.Phase) {
		return d.resumeCompletionDisposition(project, result, transaction)
	}
	note, err := resolveV7Note(project.VaultRoot, result.TaskID, "task")
	if err != nil {
		return err
	}
	if reason := completionReviewDrift(project.VaultRoot, note, result); reason != "" {
		return d.failCompletion(project, result, transaction, reason)
	}
	if !v7GitRepo(project.RepoRoot) {
		return d.failCompletion(project, result, transaction, "completion reactor requires a Git repository")
	}
	if transaction.IntegrationBase == "" || transaction.IntegrationBase == "unresolved" {
		return d.failCompletion(project, result, transaction, "integration base is not frozen")
	}
	refExists := gitRefExists(project.RepoRoot, transaction.IntegrationRef)
	currentBase := ""
	if refExists {
		currentBase, err = gitOutputTrim(project.RepoRoot, "rev-parse", transaction.IntegrationRef)
		if err != nil {
			return err
		}
	}
	if refExists && currentBase != transaction.IntegrationBase && transaction.Phase != completionPhaseRefIntent && transaction.Phase != completionPhaseRefCommitted && transaction.Phase != completionPhaseAudited && transaction.Phase != completionPhaseWoken {
		return d.failCompletion(project, result, transaction, "integration base drift: expected "+transaction.IntegrationBase+", got "+currentBase)
	}

	if transaction.Phase == completionPhasePlanned {
		transaction.Phase = completionPhaseStaging
		if err := d.store.SaveCompletionTransaction(transaction); err != nil {
			return err
		}
	}
	if transaction.Phase == completionPhaseStaging {
		staged, stageErr := stageExactReviewCompletion(project.VaultRoot, project.RepoRoot, transaction.IntegrationBase, result)
		if stageErr != nil {
			return d.failCompletion(project, result, transaction, stageErr.Error())
		}
		transaction.StagedSHA, transaction.Phase = staged, completionPhaseStaged
		if err := d.store.SaveCompletionTransaction(transaction); err != nil {
			return err
		}
	}
	if transaction.Phase == completionPhaseStaged {
		pass, summary, gateErr := gateExactReviewCompletion(project.VaultRoot, project.RepoRoot, transaction.StagedSHA, result)
		if gateErr != nil {
			return d.failCompletion(project, result, transaction, gateErr.Error())
		}
		if !pass {
			return d.failCompletion(project, result, transaction, summary)
		}
		transaction.Phase = completionPhaseGated
		if err := d.store.SaveCompletionTransaction(transaction); err != nil {
			return err
		}
	}
	if transaction.Phase == completionPhaseGated {
		transaction.Phase = completionPhaseRefIntent
		if err := d.store.SaveCompletionTransaction(transaction); err != nil {
			return err
		}
	}
	if transaction.Phase == completionPhaseRefIntent {
		if !refExists {
			if err := updateGitRef(project.RepoRoot, transaction.IntegrationRef, transaction.StagedSHA, strings.Repeat("0", 40)); err != nil {
				return d.failCompletion(project, result, transaction, "integration ref create compare-and-swap failed: "+firstActionableLine("", err.Error()))
			}
		} else if currentBase == transaction.IntegrationBase {
			if err := updateGitRef(project.RepoRoot, transaction.IntegrationRef, transaction.StagedSHA, transaction.IntegrationBase); err != nil {
				return d.failCompletion(project, result, transaction, "integration ref compare-and-swap failed: "+firstActionableLine("", err.Error()))
			}
		} else if currentBase != transaction.StagedSHA {
			return d.failCompletion(project, result, transaction, "integration ref diverged: expected base "+transaction.IntegrationBase+" or staged "+transaction.StagedSHA+", got "+currentBase)
		}
		transaction.Phase = completionPhaseRefCommitted
		if err := d.store.SaveCompletionTransaction(transaction); err != nil {
			return err
		}
	}
	if transaction.Phase == completionPhaseRefCommitted {
		wave, _, ok := armedWaveForTask(project.VaultRoot, note)
		if !ok {
			return d.failCompletion(project, result, transaction, "task lost wave binding after staging")
		}
		entry := v7LandingAuditEntry{Task: result.TaskID, Branch: result.ImplementationSHA, Target: strings.TrimPrefix(transaction.IntegrationRef, "refs/heads/"), GateResult: "pass", GateSummary: "typed review completion reactor", Commit: transaction.StagedSHA, Actor: "daemon:completion-reactor", Timestamp: time.Now().UTC().Format(time.RFC3339)}
		if err := appendV7WaveLandingAudit(project.VaultRoot, stringField(wave.Data, "id"), []v7LandingAuditEntry{entry}, "daemon:completion-reactor"); err != nil {
			return err
		}
		transaction.Phase = completionPhaseAudited
		if err := d.store.SaveCompletionTransaction(transaction); err != nil {
			return err
		}
	}
	if transaction.Phase == completionPhaseAudited {
		// Reconciliation derives the affected closure and normal capacity policy
		// picks the frontier.  Do not release/promote a default ref here.
		transaction.Phase = completionPhaseWoken
		if err := d.store.SaveCompletionTransaction(transaction); err != nil {
			return err
		}
	}
	transaction.Phase = completionPhaseTerminal
	return d.store.SaveCompletionTransaction(transaction)
}

func completionReviewDrift(vaultPath string, note Note, result ReviewResult) string {
	if stringField(note.Data, "status") != "review" {
		return "stale review state: task is " + stringField(note.Data, "status")
	}
	if stringField(note.Data, "state_rev") != result.TaskStateRev {
		return "stale review revision"
	}
	if intField(note.Data, "work_revision") != result.WorkRevision {
		return "work revision drift"
	}
	if firstNonEmpty(stringField(note.Data, "source_sha"), stringField(note.Data, "source_commit")) != result.ImplementationSHA {
		return "implementation source drift"
	}
	proof, gates, err := reviewObjectiveSnapshots(vaultPath, note)
	if err != nil {
		return "objective snapshot unavailable: " + err.Error()
	}
	if proof != result.ProofFingerprint {
		return "proof fingerprint drift"
	}
	if gates != result.GateFingerprint {
		return "gate fingerprint drift"
	}
	return ""
}

func completionFailurePhase(phase string) bool {
	switch phase {
	case completionPhaseFailureIntent, completionPhaseFailureHandback, completionPhaseFailureReleased, completionPhaseFailureAudited:
		return true
	default:
		return false
	}
}

func (d *Daemon) failCompletion(project RegisteredProject, result ReviewResult, transaction *completionTransaction, reason string) error {
	if completionFailurePhase(transaction.Phase) {
		return d.resumeCompletionDisposition(project, result, transaction)
	}
	reason = "completion reactor " + transaction.ID + ": " + limitLandingSummary(reason, 500)
	return d.beginCompletionDisposition(project, result, transaction, "rework", reason)
}

func (d *Daemon) beginCompletionDisposition(project RegisteredProject, result ReviewResult, transaction *completionTransaction, disposition, reason string) error {
	if transaction.Phase == completionPhaseTerminal {
		return nil
	}
	if !completionFailurePhase(transaction.Phase) {
		transaction.Disposition = disposition
		transaction.Failure = strings.TrimSpace(reason)
		transaction.Phase = completionPhaseFailureIntent
		if err := d.store.SaveCompletionTransaction(transaction); err != nil {
			return err
		}
	}
	return d.resumeCompletionDisposition(project, result, transaction)
}

// resumeCompletionDisposition is an intent-first transaction. Every side
// effect is idempotent and its phase is saved only after it succeeds. A crash
// can therefore repeat handback/release/audit, but can never strand a terminal
// row in review or erase a newer owner.
func (d *Daemon) resumeCompletionDisposition(project RegisteredProject, result ReviewResult, transaction *completionTransaction) error {
	if transaction.Phase == completionPhaseFailureIntent {
		if transaction.Disposition == "rework" {
			if err := returnReviewerFindingToImplementer(project.VaultRoot, transaction.TaskID, transaction.Failure, "daemon:completion-reactor"); err != nil {
				return err
			}
		}
		if err := injectCompletionReactorCrash("failure_handback", transaction); err != nil {
			return err
		}
		transaction.Phase = completionPhaseFailureHandback
		if err := d.store.SaveCompletionTransaction(transaction); err != nil {
			return err
		}
	}
	if transaction.Phase == completionPhaseFailureHandback {
		if err := d.releaseCompletionReviewOwnership(project, result, transaction); err != nil {
			return err
		}
		if err := injectCompletionReactorCrash("failure_release", transaction); err != nil {
			return err
		}
		transaction.Phase = completionPhaseFailureReleased
		if err := d.store.SaveCompletionTransaction(transaction); err != nil {
			return err
		}
	}
	if transaction.Phase == completionPhaseFailureReleased {
		if err := auditCompletionFailure(project, result, transaction); err != nil {
			return err
		}
		if err := injectCompletionReactorCrash("failure_audit", transaction); err != nil {
			return err
		}
		transaction.Phase = completionPhaseFailureAudited
		if err := d.store.SaveCompletionTransaction(transaction); err != nil {
			return err
		}
	}
	if transaction.Phase == completionPhaseFailureAudited {
		transaction.Phase = completionPhaseTerminal
		return d.store.SaveCompletionTransaction(transaction)
	}
	return nil
}

func (d *Daemon) releaseCompletionReviewOwnership(project RegisteredProject, result ReviewResult, transaction *completionTransaction) error {
	runs, err := d.store.ListRuns()
	if err != nil {
		return err
	}
	for _, run := range runs {
		if run.ProjectID != project.ProjectID || run.RecordID != result.TaskID || run.Lane != runLaneReview || run.WorkRevision != result.WorkRevision {
			continue
		}
		if run.ActiveAttemptID != "" && run.ActiveAttemptID != result.AttemptID {
			// Never release a newer review owner while replaying an old result.
			return nil
		}
		if transaction.Disposition == "park" {
			run.LeaseState = string(LeaseStateParkedNoProgress)
		} else {
			run.LeaseState = string(LeaseStateReleased)
		}
		run.AttemptOutcome = string(AttemptOutcomeBlocked)
		run.NextRetryAt = ""
		run.LastError = transaction.Failure
		run.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		run.Terminal = false
		clearActiveExecution(&run)
		return d.store.UpsertRun(run)
	}
	return nil
}

func auditCompletionFailure(project RegisteredProject, result ReviewResult, transaction *completionTransaction) error {
	task, err := resolveV7Note(project.VaultRoot, result.TaskID, "task")
	if err != nil {
		return err
	}
	waveID := stringField(task.Data, "wave")
	if waveID == "" {
		return nil
	}
	wave, err := resolveV7Note(project.VaultRoot, waveID, "wave")
	if err != nil {
		return err
	}
	entry := v7LandingAuditEntry{
		Task: result.TaskID, Branch: result.ImplementationSHA, Target: v7WaveIntegrationBranch(wave),
		DefectID: transaction.ID, GateResult: "fail", GateSummary: transaction.Failure,
		Actor: "daemon:completion-reactor", Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	return appendV7WaveLandingAudit(project.VaultRoot, waveID, []v7LandingAuditEntry{entry}, "daemon:completion-reactor")
}

// stageExactReviewCompletion stages from the frozen commit, merges the exact
// reviewed commit (not task/<id>), and writes the integrated done projection.
// The gate is a separate persisted phase so a crash after staging never needs
// another merge commit.
func stageExactReviewCompletion(vaultPath, repoRoot, integrationBase string, result ReviewResult) (string, error) {
	tmp, err := os.MkdirTemp("", "tusker-completion-stage-*")
	if err != nil {
		return "", err
	}
	defer func() {
		_ = exec.Command("git", "-C", repoRoot, "worktree", "remove", "--force", tmp).Run()
		_ = os.RemoveAll(tmp)
	}()
	if output, err := gitCombined(repoRoot, "worktree", "add", "--detach", tmp, integrationBase); err != nil {
		return "", tuskerError(errorInvalidTransition, "failed to create completion staging worktree: "+firstActionableLine(output, err.Error()))
	}
	if output, err := gitCombined(tmp, "merge", "--no-ff", "--no-edit", result.ImplementationSHA); err != nil {
		return "", tuskerError(errorInvalidTransition, landingFailureSummary("merge "+result.ImplementationSHA, output, err))
	}
	if err := materializeReviewedDone(tmp, vaultPath, result); err != nil {
		return "", err
	}
	if output, err := gitCombined(tmp, "add", "--", filepath.ToSlash(filepath.Join(relativeFromRepo(repoRoot, vaultPath), "work", "tasks", result.TaskID+".md"))); err != nil {
		return "", tuskerError(errorInvalidTransition, "failed to stage reviewed task closure: "+firstActionableLine(output, err.Error()))
	}
	if output, err := gitCombined(tmp, "commit", "-m", "Complete reviewed task "+result.TaskID); err != nil {
		return "", tuskerError(errorInvalidTransition, "failed to commit reviewed task closure: "+firstActionableLine(output, err.Error()))
	}
	sha, err := gitOutputTrim(tmp, "rev-parse", "HEAD")
	return sha, err
}

func gateExactReviewCompletion(vaultPath, repoRoot, stagedSHA string, result ReviewResult) (bool, string, error) {
	tmp, err := os.MkdirTemp("", "tusker-completion-gate-*")
	if err != nil {
		return false, "", err
	}
	defer func() {
		_ = exec.Command("git", "-C", repoRoot, "worktree", "remove", "--force", tmp).Run()
		_ = os.RemoveAll(tmp)
	}()
	if output, err := gitCombined(repoRoot, "worktree", "add", "--detach", tmp, stagedSHA); err != nil {
		return false, "", tuskerError(errorInvalidTransition, "failed to create completion gate worktree: "+firstActionableLine(output, err.Error()))
	}
	pass, summary := runV7LandingGate(vaultPath, tmp, "typed-review:"+result.ResultRevision)
	return pass, summary, nil
}

func materializeReviewedDone(stageRoot, vaultPath string, result ReviewResult) error {
	repoRoot := v7RepoRoot(vaultPath)
	relVault, err := filepath.Rel(repoRoot, vaultPath)
	if err != nil || filepath.IsAbs(relVault) || strings.HasPrefix(filepath.Clean(relVault), "..") {
		return tuskerError(errorInvalidTransition, "cannot locate staged tracker")
	}
	path := filepath.Join(stageRoot, relVault, "work", "tasks", result.TaskID+".md")
	canonicalPath := filepath.Join(vaultPath, "work", "tasks", result.TaskID+".md")
	canonicalData, canonicalBody, err := parseFrontmatterMustRead(canonicalPath)
	if err != nil {
		return err
	}
	if stringField(canonicalData, "state_rev") != result.TaskStateRev ||
		intField(canonicalData, "work_revision") != result.WorkRevision ||
		firstNonEmpty(stringField(canonicalData, "source_sha"), stringField(canonicalData, "source_commit")) != result.ImplementationSHA {
		return tuskerError(errorInvalidTransition, "canonical task drifted from exact reviewed revision before staging")
	}
	canonical, err := serializeDocument(canonicalData, canonicalBody, v7FrontmatterOrder["task"])
	if err != nil {
		return err
	}
	if err := writeText(path, canonical); err != nil {
		return err
	}
	data, body, err := parseFrontmatterMustRead(path)
	if err != nil {
		return err
	}
	if intField(data, "work_revision") != result.WorkRevision || firstNonEmpty(stringField(data, "source_sha"), stringField(data, "source_commit")) != result.ImplementationSHA {
		return tuskerError(errorInvalidTransition, "staged task drifted from exact reviewed source")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	data["status"], data["readiness"] = "done", "complete"
	data["verified_at"], data["updated_at"], data["updated_by"] = now, now, "daemon:completion-reactor"
	data["next_owner"], data["next_source"], data["next_ref"], data["next_action"] = "", "completion_reactor", result.ResultRevision, "Integrated after typed review pass."
	row := "| " + strings.Join(result.Covers, ",") + " | typed review " + result.AttemptID + " | pass | [tusker-review-result:" + result.ResultRevision + "] " + strings.ReplaceAll(result.Summary, "|", "/") + " |"
	body = appendCompletionVerification(body, row)
	_, err = saveV7DocumentCAS(path, data, body, v7FrontmatterOrder["task"], stringField(data, "state_rev"))
	return err
}

func appendCompletionVerification(body, row string) string {
	section := fenceAwareSectionContent(body, "## Verification")
	if section == "" {
		return strings.TrimSpace(body) + "\n\n## Verification\n\n| Covers | Check | Result | Notes |\n|---|---|---|---|\n" + row + "\n"
	}
	return strings.Replace(body, section, strings.TrimRight(section, "\n")+"\n"+row, 1)
}
