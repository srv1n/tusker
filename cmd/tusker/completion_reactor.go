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
	completionPhasePlanned      = "planned"
	completionPhaseStaging      = "staging"
	completionPhaseStaged       = "staged"
	completionPhaseGated        = "gated"
	completionPhaseRefIntent    = "ref_intent"
	completionPhaseRefCommitted = "ref_committed"
	completionPhaseAudited      = "audited"
	completionPhaseWoken        = "woken"
	completionPhaseTerminal     = "terminal"
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
	CreatedAt         string `json:"created_at"`
	UpdatedAt         string `json:"updated_at"`
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
	wave, _, hasWave := armedWaveForTask(project.VaultRoot, note)
	if !hasWave && mode == completionReactorModeAuthoritative {
		if _, _, err := ensureV7ImplicitSingletonDeliveryUnit(project.VaultRoot, result.TaskID, Args{"quiet": "true", "by": "daemon:completion-reactor"}); err != nil {
			return err
		}
		note, err = resolveV7Note(project.VaultRoot, result.TaskID, "task")
		if err != nil {
			return err
		}
		wave, _, hasWave = armedWaveForTask(project.VaultRoot, note)
	}
	if !hasWave {
		// Shadow is deliberately observational: an unbound task is a useful
		// comparison result, not permission to manufacture a delivery unit.
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
		if transaction.Phase == completionPhaseTerminal {
			return nil
		}
		finding := strings.Join(result.Findings, "\n")
		if err := returnReviewerFindingToImplementer(project.VaultRoot, result.TaskID, finding, "daemon:completion-reactor"); err != nil {
			return err
		}
		transaction.Phase = completionPhaseTerminal
		return d.store.SaveCompletionTransaction(&transaction)
	case "blocked":
		if result.Blocker == "human" {
			// The submit command proved a genuine human gate.  Preserve review
			// state and park the reactor; inventing done/rework would lie.
			transaction.Phase = completionPhaseTerminal
			return d.store.SaveCompletionTransaction(&transaction)
		}
		if transaction.Phase == completionPhaseTerminal {
			return nil
		}
		if err := returnReviewerFindingToImplementer(project.VaultRoot, result.TaskID, "Review blocked ("+result.Blocker+"): "+result.Summary, "daemon:completion-reactor"); err != nil {
			return err
		}
		transaction.Phase = completionPhaseTerminal
		return d.store.SaveCompletionTransaction(&transaction)
	case "pass":
		return d.completePassingReview(project, result, &transaction)
	default:
		return tuskerError(errorInvalidArg, "stored review result has unknown verdict")
	}
}

func (d *Daemon) completePassingReview(project RegisteredProject, result ReviewResult, transaction *completionTransaction) error {
	if transaction.Phase == completionPhaseTerminal {
		return nil
	}
	note, err := resolveV7Note(project.VaultRoot, result.TaskID, "task")
	if err != nil {
		return err
	}
	if reason := completionReviewDrift(project.VaultRoot, note, result); reason != "" {
		return d.failCompletion(project, transaction, reason)
	}
	if !v7GitRepo(project.RepoRoot) {
		return d.failCompletion(project, transaction, "completion reactor requires a Git repository")
	}
	if transaction.IntegrationBase == "" || transaction.IntegrationBase == "unresolved" {
		return d.failCompletion(project, transaction, "integration base is not frozen")
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
		return d.failCompletion(project, transaction, "integration base drift: expected "+transaction.IntegrationBase+", got "+currentBase)
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
			return d.failCompletion(project, transaction, stageErr.Error())
		}
		transaction.StagedSHA, transaction.Phase = staged, completionPhaseStaged
		if err := d.store.SaveCompletionTransaction(transaction); err != nil {
			return err
		}
	}
	if transaction.Phase == completionPhaseStaged {
		pass, summary, gateErr := gateExactReviewCompletion(project.VaultRoot, project.RepoRoot, transaction.StagedSHA, result)
		if gateErr != nil {
			return d.failCompletion(project, transaction, gateErr.Error())
		}
		if !pass {
			return d.failCompletion(project, transaction, summary)
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
				return d.failCompletion(project, transaction, "integration ref create compare-and-swap failed: "+firstActionableLine("", err.Error()))
			}
		} else if currentBase == transaction.IntegrationBase {
			if err := updateGitRef(project.RepoRoot, transaction.IntegrationRef, transaction.StagedSHA, transaction.IntegrationBase); err != nil {
				return d.failCompletion(project, transaction, "integration ref compare-and-swap failed: "+firstActionableLine("", err.Error()))
			}
		} else if currentBase != transaction.StagedSHA {
			return d.failCompletion(project, transaction, "integration ref diverged: expected base "+transaction.IntegrationBase+" or staged "+transaction.StagedSHA+", got "+currentBase)
		}
		transaction.Phase = completionPhaseRefCommitted
		if err := d.store.SaveCompletionTransaction(transaction); err != nil {
			return err
		}
	}
	if transaction.Phase == completionPhaseRefCommitted {
		wave, _, ok := armedWaveForTask(project.VaultRoot, note)
		if !ok {
			return d.failCompletion(project, transaction, "task lost wave binding after staging")
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

func (d *Daemon) failCompletion(project RegisteredProject, transaction *completionTransaction, reason string) error {
	reason = "completion reactor " + transaction.ID + ": " + limitLandingSummary(reason, 500)
	transaction.Failure, transaction.Phase = reason, completionPhaseTerminal
	if err := d.store.SaveCompletionTransaction(transaction); err != nil {
		return err
	}
	// A CAS/ref divergence is deliberately not retried against a new base.  It
	// becomes one attributable rework result with a stable transaction identity.
	return kickV7LandingTaskToRework(project.VaultRoot, transaction.TaskID, reason, "daemon:completion-reactor")
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
