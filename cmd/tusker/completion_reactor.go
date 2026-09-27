package main

// The completion reactor is deliberately boring.  A review result is already
// the reviewer’s complete authority; this file turns that immutable record
// into a resumable sequence of mechanical Git/tracker operations.  In
// particular, it never asks a model to resolve a clean merge or choose a
// lifecycle transition.

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

const (
	completionTransactionSchema    = "tusker.completion-transaction/v4"
	completionPhasePlanned         = "planned"
	completionPhaseStaging         = "staging"
	completionPhaseStaged          = "staged"
	completionPhaseGated           = "gated"
	completionPhaseRefIntent       = "ref_intent"
	completionPhaseRefCommitted    = "ref_committed"
	completionPhaseCanonicalIntent = "canonical_intent"
	completionPhaseCanonicalDone   = "canonical_done"
	completionPhaseAudited         = "audited"
	completionPhaseWoken           = "woken"
	completionPhaseFailureIntent   = "failure_intent"
	completionPhaseFailureHandback = "failure_handback"
	completionPhaseFailureReleased = "failure_released"
	completionPhaseFailureAudited  = "failure_audited"
	completionPhaseTerminal        = "terminal"
	completionRepairRequiredError  = "COMPLETION_REPAIR_REQUIRED"
)

type completionTransaction struct {
	Schema                 string `json:"schema"`
	ID                     string `json:"id"`
	ProjectID              string `json:"project_id"`
	TaskID                 string `json:"task_id"`
	WorkRevision           int    `json:"work_revision"`
	ImplementationSHA      string `json:"implementation_sha"`
	ReviewAttempt          string `json:"review_attempt"`
	ResultRevision         string `json:"result_revision"`
	ReviewedTaskStateRev   string `json:"reviewed_task_state_rev"`
	WaveID                 string `json:"wave_id"`
	WaveAuthorityKind      string `json:"wave_authority_kind"`
	WaveAuthorizationFP    string `json:"wave_authorization_fingerprint,omitempty"`
	WaveMaterialFP         string `json:"wave_material_fingerprint"`
	CloseAuthorityFP       string `json:"close_authority_fingerprint,omitempty"`
	WorkerPolicyFP         string `json:"worker_policy_fingerprint"`
	CompletionAuthorityID  string `json:"completion_authority_id,omitempty"`
	CompletionAuthoritySig []byte `json:"completion_authority_signature,omitempty"`
	IntegrationBase        string `json:"integration_base"`
	IntegrationRef         string `json:"integration_ref"`
	StagingRef             string `json:"staging_ref"`
	Phase                  string `json:"phase"`
	StagedSHA              string `json:"staged_sha,omitempty"`
	StagedTaskBlob         string `json:"staged_task_blob,omitempty"`
	StagedTaskMode         string `json:"staged_task_mode,omitempty"`
	StagedReceiptBlob      string `json:"staged_receipt_blob,omitempty"`
	StagedReceiptMode      string `json:"staged_receipt_mode,omitempty"`
	Failure                string `json:"failure,omitempty"`
	Disposition            string `json:"disposition,omitempty"`
	CreatedAt              string `json:"created_at"`
	UpdatedAt              string `json:"updated_at"`
}

type persistedReviewResultRow struct {
	ProjectID    string
	TaskID       string
	WorkRevision int
	AttemptID    string
	Result       ReviewResult
	Repair       error
}

// completionReactorCrashHook is test-only fault injection. Production leaves
// it nil. Hooks run after an idempotent side effect and before its phase is
// persisted, which is the only crash window worth proving.
var completionReactorCrashHook func(string, *completionTransaction) error

type completionCrashInterruption struct {
	point string
	err   error
}

func completionTransactionID(projectID string, result ReviewResult, integrationBase string, frozenAuthority ...string) string {
	parts := []string{projectID, result.TaskID, fmt.Sprintf("%d", result.WorkRevision), result.ImplementationSHA, result.AttemptID, result.ResultRevision, integrationBase}
	parts = append(parts, frozenAuthority...)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "completion:" + hex.EncodeToString(sum[:])
}

func (s *RuntimeStore) CompletionTransaction(id string) (*completionTransaction, error) {
	var rowID, projectID, taskID, resultRevision, phase, raw string
	err := s.queryRowScan(
		`SELECT transaction_id,project_id,task_id,result_revision,phase,transaction_json FROM completion_transactions WHERE transaction_id=?`,
		[]any{id}, &rowID, &projectID, &taskID, &resultRevision, &phase, &raw,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	var transaction completionTransaction
	if err := json.Unmarshal([]byte(raw), &transaction); err != nil {
		return nil, completionPersistedRowRepairError(
			"completion transaction", projectID, taskID, 0, "", resultRevision,
			"transaction JSON is malformed: "+err.Error(),
		)
	}
	if transaction.ID != rowID || transaction.ProjectID != projectID || transaction.TaskID != taskID ||
		transaction.ResultRevision != resultRevision || transaction.Phase != phase {
		return nil, completionPersistedRowRepairError(
			"completion transaction", projectID, taskID, 0, "", resultRevision,
			"SQL identity does not match transaction JSON",
		)
	}
	return &transaction, nil
}

func (s *RuntimeStore) CompletionTransactionForResult(projectID, taskID, resultRevision string) (*completionTransaction, error) {
	var rowID, rowProjectID, rowTaskID, rowResultRevision, phase, raw string
	err := s.queryRowScan(
		`SELECT transaction_id,project_id,task_id,result_revision,phase,transaction_json FROM completion_transactions WHERE project_id=? AND task_id=? AND result_revision=? ORDER BY updated_at DESC LIMIT 1`,
		[]any{projectID, taskID, resultRevision},
		&rowID, &rowProjectID, &rowTaskID, &rowResultRevision, &phase, &raw,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	var transaction completionTransaction
	if err := json.Unmarshal([]byte(raw), &transaction); err != nil {
		return nil, completionPersistedRowRepairError(
			"completion transaction", rowProjectID, rowTaskID, 0, "", rowResultRevision,
			"transaction JSON is malformed: "+err.Error(),
		)
	}
	if transaction.ID != rowID || transaction.ProjectID != rowProjectID || transaction.TaskID != rowTaskID ||
		transaction.ResultRevision != rowResultRevision || transaction.Phase != phase {
		return nil, completionPersistedRowRepairError(
			"completion transaction", rowProjectID, rowTaskID, 0, "", rowResultRevision,
			"SQL identity does not match transaction JSON",
		)
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

func (s *RuntimeStore) ListReviewResults(projectID string) ([]persistedReviewResultRow, error) {
	rows, err := s.query(
		`SELECT project_id,task_id,work_revision,attempt_id,result_json FROM review_results WHERE project_id=? ORDER BY task_id, work_revision, attempt_id`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []persistedReviewResultRow
	for rows.Next() {
		var row persistedReviewResultRow
		var rawWorkRevision any
		var raw string
		if err := rows.Scan(&row.ProjectID, &row.TaskID, &rawWorkRevision, &row.AttemptID, &raw); err != nil {
			return nil, err
		}
		row.WorkRevision = intFromAny(rawWorkRevision)
		// Revision 0 is valid: a V7 task has no work_revision until a candidate
		// is projected (F27).
		if row.WorkRevision < 0 {
			row.Repair = completionPersistedRowRepairError(
				"review result", row.ProjectID, row.TaskID, 0, row.AttemptID, "",
				"SQL work_revision identity is invalid",
			)
			out = append(out, row)
			continue
		}
		if err := json.Unmarshal([]byte(raw), &row.Result); err != nil {
			row.Repair = completionPersistedRowRepairError(
				"review result", row.ProjectID, row.TaskID, row.WorkRevision, row.AttemptID, "",
				"result JSON is malformed: "+err.Error(),
			)
			out = append(out, row)
			continue
		}
		if row.Result.ProjectID != row.ProjectID || row.Result.TaskID != row.TaskID ||
			row.Result.WorkRevision != row.WorkRevision || row.Result.AttemptID != row.AttemptID {
			row.Repair = completionPersistedRowRepairError(
				"review result", row.ProjectID, row.TaskID, row.WorkRevision, row.AttemptID, row.Result.ResultRevision,
				"SQL identity does not match signed result JSON",
			)
			out = append(out, row)
			continue
		}
		if err := validatePersistedReviewResult(row.Result); err != nil {
			row.Repair = completionPersistedRowRepairError(
				"review result", row.ProjectID, row.TaskID, row.WorkRevision, row.AttemptID, row.Result.ResultRevision,
				"immutable result validation failed: "+err.Error(),
			)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func completionPersistedRowRepairError(kind, projectID, taskID string, workRevision int, attemptID, resultRevision, reason string) error {
	context := map[string]any{
		"kind": kind, "project": projectID, "task": taskID, "reason": reason,
	}
	if workRevision > 0 {
		context["work_revision"] = workRevision
	}
	if attemptID != "" {
		context["attempt"] = attemptID
	}
	if resultRevision != "" {
		context["result_revision"] = resultRevision
	}
	return tuskerError(
		completionRepairRequiredError,
		kind+" cannot be authenticated: "+reason,
		withContext(context),
		withHint("preserve the suspect row for audit and repair only this task; independent reviewed work may continue"),
	)
}

// reconcileReviewCompletion consumes saved typed results.  disabled and
// legacy deliberately do nothing here: their prior authority paths retain
// exact compatibility.  Shadow records a frozen observational plan only.
type priorBlockingReviewFinding struct {
	reviewerFindingRecord
	AttemptID    string
	CreatedAt    string
	WorkRevision int
}

// completionReviewAttemptBefore orders same-revision review results by the
// durable attempt record first. CreatedAt is reviewer-supplied metadata and
// may be second-resolution (or equal across retries), so it is only a fallback
// when an attempt timestamp is unavailable. Attempt IDs provide the final
// deterministic tie-breaker for equal timestamps.
func completionReviewAttemptBefore(attempts map[string]RunAttempt, candidateID, currentID, candidateCreatedAt, currentCreatedAt string) bool {
	candidateID = strings.TrimSpace(candidateID)
	currentID = strings.TrimSpace(currentID)
	if candidateID == "" || currentID == "" || candidateID == currentID {
		return false
	}
	if candidate, candidateOK := attempts[candidateID]; candidateOK {
		if current, currentOK := attempts[currentID]; currentOK {
			candidateAt, candidateTimeOK := completionTimestamp(candidate.StartedAt)
			currentAt, currentTimeOK := completionTimestamp(current.StartedAt)
			if candidateTimeOK && currentTimeOK {
				if candidateAt.Before(currentAt) {
					return true
				}
				if candidateAt.After(currentAt) {
					return false
				}
			}
		}
	}
	candidateAt, candidateTimeOK := completionTimestamp(candidateCreatedAt)
	currentAt, currentTimeOK := completionTimestamp(currentCreatedAt)
	if candidateTimeOK && currentTimeOK {
		if candidateAt.Before(currentAt) {
			return true
		}
		if candidateAt.After(currentAt) {
			return false
		}
	}
	return candidateID < currentID
}

func completionReviewAttemptAfter(attempts map[string]RunAttempt, candidateID, currentID, candidateCreatedAt, currentCreatedAt string) bool {
	return completionReviewAttemptBefore(attempts, currentID, candidateID, currentCreatedAt, candidateCreatedAt)
}

// validateReviewFindingClosure reads the durable review-result history rather
// than the mutable task body. Every prior blocking finding must be explicitly
// closed by a later attempt on the exact current material. Findings that name
// source/material repair additionally require a different material fingerprint;
// proof-only findings may close after a proof/task-ledger update that leaves
// implementation material unchanged. Advisory findings are intentionally
// excluded.
func (d *Daemon) validateReviewFindingClosure(projectID string, result ReviewResult) error {
	rows, err := d.store.ListReviewResults(projectID)
	if err != nil {
		return err
	}
	attemptRows, err := d.store.ListAttemptsForRun(projectID, result.TaskID)
	if err != nil {
		return err
	}
	attempts := make(map[string]RunAttempt, len(attemptRows))
	for _, attempt := range attemptRows {
		attempts[attempt.AttemptID] = attempt
	}
	prior := map[string]priorBlockingReviewFinding{}
	for _, row := range rows {
		candidate := row.Result
		if row.Repair != nil || candidate.TaskID != result.TaskID || candidate.Schema != reviewResultSchema || candidate.Verdict != "changes_requested" || candidate.ResultRevision == result.ResultRevision {
			continue
		}
		if candidate.WorkRevision > result.WorkRevision ||
			(candidate.WorkRevision == result.WorkRevision && !completionReviewAttemptBefore(attempts, candidate.AttemptID, result.AttemptID, candidate.CreatedAt, result.CreatedAt)) {
			continue
		}
		for _, raw := range candidate.Findings {
			finding, parseErr := parseReviewerFinding(raw)
			if parseErr != nil {
				return fmt.Errorf("prior blocking review finding is invalid: %w", parseErr)
			}
			if finding.Kind != "blocking" {
				continue
			}
			current := prior[finding.ID]
			if current.AttemptID == "" || candidate.WorkRevision > current.WorkRevision ||
				(candidate.WorkRevision == current.WorkRevision && completionReviewAttemptAfter(attempts, candidate.AttemptID, current.AttemptID, candidate.CreatedAt, current.CreatedAt)) {
				prior[finding.ID] = priorBlockingReviewFinding{reviewerFindingRecord: finding, AttemptID: candidate.AttemptID, CreatedAt: candidate.CreatedAt, WorkRevision: candidate.WorkRevision}
			}
		}
	}
	if len(prior) == 0 {
		if len(result.ClosedFindings) != 0 {
			return fmt.Errorf("review result closes findings that are not present in durable review history")
		}
		return nil
	}
	closures := map[string]reviewerFindingClosure{}
	for _, closure := range result.ClosedFindings {
		closures[closure.ID] = closure
	}
	for id, finding := range prior {
		closure, ok := closures[id]
		if !ok {
			return fmt.Errorf("blocking review finding %s remains unclosed", id)
		}
		if finding.AttemptID == result.AttemptID {
			return fmt.Errorf("blocking review finding %s was not closed by an independent review attempt", id)
		}
		if finding.ClosureCondition != closure.ClosureCondition {
			return fmt.Errorf("closure condition for blocking review finding %s does not match the durable finding", id)
		}
		if finding.MaterialFingerprint == "" {
			return fmt.Errorf("blocking review finding %s is missing its reviewed material", id)
		}
		if reviewerFindingRequiresMaterialChange(finding.reviewerFindingRecord) && finding.MaterialFingerprint == result.MaterialFingerprint {
			return fmt.Errorf("blocking review finding %s was not reviewed on new material", id)
		}
		if closure.MaterialFingerprint != result.MaterialFingerprint {
			return fmt.Errorf("closure for blocking review finding %s is not bound to the current material", id)
		}
	}
	for id := range closures {
		if _, ok := prior[id]; !ok {
			return fmt.Errorf("review result closes unknown blocking finding %s", id)
		}
	}
	return nil
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
	_, _, compatible := completionWaveAuthorizationCompatibility(vaultPath, idx, wave)
	return wave, compatible
}

func resultTaskID(task Note) string {
	return strings.ToUpper(strings.TrimSpace(stringField(task.Data, "id")))
}

type completionCloseAuthorityProjection struct {
	Schema            string                       `json:"schema"`
	TaskID            string                       `json:"task_id"`
	TaskStateRev      string                       `json:"task_state_rev"`
	Actor             string                       `json:"actor"`
	Risk              string                       `json:"risk"`
	RequiredAcceptor  string                       `json:"required_acceptor"`
	RequiredEvidence  []string                     `json:"required_evidence"`
	RequiredGateKinds []string                     `json:"required_gate_kinds"`
	ProofFingerprint  string                       `json:"proof_fingerprint"`
	GateFingerprint   string                       `json:"gate_fingerprint"`
	Dependencies      []v7CloseDependencyAuthority `json:"dependencies"`
}

func completionFrozenAuthorityParts(transaction *completionTransaction) []string {
	if transaction == nil {
		return nil
	}
	return []string{
		transaction.WaveID,
		transaction.WaveAuthorityKind,
		transaction.WaveAuthorizationFP,
		transaction.WaveMaterialFP,
		transaction.CloseAuthorityFP,
		transaction.WorkerPolicyFP,
		transaction.IntegrationRef,
	}
}

func completionFrozenAuthorityComplete(transaction *completionTransaction, requireClose bool) bool {
	if transaction == nil || transaction.WaveID == "" || transaction.WaveAuthorityKind == "" ||
		transaction.WaveMaterialFP == "" || transaction.IntegrationRef == "" || transaction.WorkerPolicyFP == "" ||
		(requireClose && transaction.CloseAuthorityFP == "") {
		return false
	}
	switch transaction.WaveAuthorityKind {
	case "armed":
		return transaction.WaveAuthorizationFP != ""
	case "implicit":
		return transaction.WaveAuthorizationFP == ""
	case "task_directive":
		return transaction.WaveAuthorizationFP != ""
	default:
		return false
	}
}

func completionFrozenAuthorityRepairError(transaction *completionTransaction, reason string) error {
	context := map[string]any{"reason": reason}
	if transaction != nil {
		context["transaction"] = transaction.ID
		context["phase"] = transaction.Phase
		context["task"] = transaction.TaskID
		context["staged"] = transaction.StagedSHA
		context["integration_ref"] = transaction.IntegrationRef
	}
	return tuskerError(completionRepairRequiredError, "completion transaction cannot authenticate its frozen authority: "+reason,
		withContext(context),
		withHint("repair or replace the persisted transaction authority; do not hand back or close the reviewed task"))
}

// completionWaveAuthorizationCompatibility admits one narrow migration shape:
// older scheduled waves were armed before integration_base_sha was persisted.
// If their integration ref is still absent, the newly frozen base is the
// current clean default tip, and the stored fingerprint exactly matches the
// otherwise identical pre-base material, completion may perform the intended
// zero-old CAS. New direct Start waves include the base in material and do
// not enter this compatibility path.
func completionWaveAuthorizationCompatibility(vaultPath string, idx v7Index, wave Note) (material, stored string, compatible bool) {
	material, _ = waveMaterialFingerprint(vaultPath, idx, wave)
	stored = stringField(wave.Data, "authorization_fingerprint")
	if stringField(wave.Data, "authorization") != "armed" || stored == "" {
		return material, stored, false
	}
	if stored == material {
		return material, stored, true
	}
	base := strings.TrimSpace(stringField(wave.Data, "integration_base_sha"))
	ref := "refs/heads/" + v7WaveIntegrationBranch(wave)
	repoRoot := v7RepoRoot(vaultPath)
	if base == "" || !v7GitRepo(repoRoot) || gitRefExists(repoRoot, ref) || !waveIntegrationBaseClean(vaultPath, wave) {
		return material, stored, false
	}
	legacy := wave
	legacy.Data = cloneMap(wave.Data)
	delete(legacy.Data, "integration_base_sha")
	legacyMaterial, issues := waveMaterialFingerprint(vaultPath, idx, legacy)
	return material, stored, len(issues) == 0 && stored == legacyMaterial
}

func completionPhaseHasCommittedRef(phase string) bool {
	switch phase {
	case completionPhaseRefCommitted, completionPhaseCanonicalIntent, completionPhaseCanonicalDone, completionPhaseAudited, completionPhaseWoken, completionPhaseTerminal:
		return true
	default:
		return false
	}
}

func completionCanonicalTaskMatches(task Note, result ReviewResult, transaction *completionTransaction) bool {
	proofStatus := stringField(task.Data, "proof_status")
	stamp := completionResultTimestamp(result)
	fact, factOK := v7TaskCloseAuthorityFromAny(task.Data["close_authority"])
	if !factOK || validateV7TaskCloseAuthorityFact(
		fact, "", result.TaskID, result.Actor, task.Body,
	) != nil {
		return false
	}
	return transaction != nil &&
		fact.TransactionID == transaction.ID &&
		fact.Project == stringField(task.Data, "project") &&
		fact.ReviewResultRevision == result.ResultRevision &&
		fact.ReviewedTaskStateRev == transaction.ReviewedTaskStateRev &&
		fact.CloseAuthorityFingerprint == transaction.CloseAuthorityFP &&
		stringField(task.Data, "status") == "done" &&
		stringField(task.Data, "readiness") == "done" &&
		(proofStatus == "satisfied" || proofStatus == "waived") &&
		intField(task.Data, "work_revision") == result.WorkRevision &&
		stringField(task.Data, "source_sha") == result.ImplementationSHA &&
		stringField(task.Data, "accepted_by") == result.Actor &&
		stringField(task.Data, "accepted_at") == stamp &&
		stringField(task.Data, "closed_at") == stamp &&
		stringField(task.Data, "updated_by") == result.Actor &&
		stringField(task.Data, "next_owner") == "none" &&
		stringField(task.Data, "next_source") == "status" &&
		stringField(task.Data, "next_ref") == "" &&
		stringField(task.Data, "next_action") == "" &&
		strings.Contains(task.Body, "[tusker-review-result:"+result.ResultRevision+"]")
}

func completionTimestamp(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

// stageExactReviewCompletion stages from the frozen commit, merges the exact
// reviewed commit (not task/<id>), and writes the integrated done projection.
// The gate is a separate persisted phase so a crash after staging never needs
// another merge commit.
func completionStagingRef(transactionID string) string {
	return "refs/tusker/completion/" + strings.TrimPrefix(strings.TrimSpace(transactionID), "completion:")
}

type completionStagingCandidate struct {
	SHA string
	// TaskBlob is the raw hash-object result for the generated canonical
	// bytes. The builder returns it only after the index, tree, and commit all
	// resolve the task path to that same object.
	TaskBlob string
	// TaskMode binds the complete tree entry, not just its content object.
	// Task contracts are always regular, non-executable files.
	TaskMode    string
	ReceiptBlob string
	ReceiptMode string
}

// stageExactCompletionTaskBlob bypasses attributes and clean filters entirely.
// The generated bytes are written as a raw Git blob and installed directly in
// the merge index; both the object and index entry are authenticated before
// write-tree can consume them.
type completionGitTreeEntry struct {
	Mode string
	Type string
	OID  string
}

func completionGitTreeEntryAt(repoRoot, ref, taskRel string) (completionGitTreeEntry, error) {
	raw, err := gitOutputTrim(repoRoot, "ls-tree", ref, "--", taskRel)
	if err != nil {
		return completionGitTreeEntry{}, err
	}
	fields := strings.Fields(raw)
	if len(fields) < 4 {
		return completionGitTreeEntry{}, tuskerError(errorInvalidTransition, "reviewed task tree entry is missing or malformed")
	}
	entry := completionGitTreeEntry{Mode: fields[0], Type: fields[1], OID: fields[2]}
	if entry.Mode != "100644" || entry.Type != "blob" || entry.OID == "" {
		return completionGitTreeEntry{}, tuskerError(errorInvalidTransition, "reviewed task tree entry must be a regular non-executable blob")
	}
	return entry, nil
}

func completionResultTimestamp(result ReviewResult) string {
	if parsed, ok := completionTimestamp(result.CreatedAt); ok {
		return parsed.UTC().Format(time.RFC3339)
	}
	// Legacy typed-result fixtures omitted CreatedAt. A fixed epoch keeps their
	// staged object deterministic without weakening any authority field.
	return "2000-01-01T00:00:00Z"
}

func validateCompletionStagingCandidate(vaultPath, repoRoot, candidate, integrationBase string, result ReviewResult, transaction *completionTransaction) error {
	if transaction == nil || transaction.StagedTaskBlob == "" || transaction.StagedTaskMode != "100644" || transaction.StagedReceiptBlob == "" || transaction.StagedReceiptMode != "100644" {
		return completionFrozenAuthorityRepairError(transaction, "staged task/receipt tree entry attestation is missing or invalid")
	}
	parents, err := gitOutputTrim(repoRoot, "rev-list", "--parents", "-n", "1", candidate)
	fields := strings.Fields(parents)
	if err != nil || len(fields) != 3 || fields[0] != candidate || fields[1] != integrationBase || fields[2] != result.ImplementationSHA {
		return tuskerError(errorInvalidTransition, "completion staging ref must have the frozen integration base and exact reviewed SHA as its only parents")
	}
	rel, err := completionTaskRepoRelativePath(repoRoot, vaultPath, result.TaskID)
	if err != nil {
		return err
	}
	taskEntry, err := completionGitTreeEntryAt(repoRoot, candidate, rel)
	if err != nil {
		return err
	}
	if taskEntry.OID != transaction.StagedTaskBlob || taskEntry.Mode != transaction.StagedTaskMode {
		return tuskerError(errorInvalidTransition, "completion staging ref does not retain its generated task tree entry")
	}
	receiptRel := completionReceiptRepoPath(completionReceiptID(transaction.ID))
	receiptEntry, err := completionGitTreeEntryAt(repoRoot, candidate, receiptRel)
	if err != nil || receiptEntry.OID != transaction.StagedReceiptBlob || receiptEntry.Mode != transaction.StagedReceiptMode {
		return tuskerError(errorInvalidTransition, "completion staging ref does not retain its generated receipt tree entry")
	}
	raw, err := gitOutputTrim(repoRoot, "show", candidate+":"+rel)
	if err != nil {
		return err
	}
	data, body, err := parseFrontmatter(raw)
	if err != nil {
		return err
	}
	if stringField(data, "status") != "done" || !strings.Contains(body, "[tusker-review-result:"+result.ResultRevision+"]") {
		return tuskerError(errorInvalidTransition, "completion staging ref lacks the reviewed done projection")
	}
	receiptRaw, err := gitCombined(repoRoot, "show", candidate+":"+receiptRel)
	if err != nil {
		return err
	}
	if err := validateCompletionReceipt([]byte(receiptRaw), rel, taskEntry, result, transaction, data, body); err != nil {
		return tuskerError(errorInvalidTransition, "completion staging receipt is invalid: "+err.Error())
	}
	message, err := gitOutputTrim(repoRoot, "show", "-s", "--format=%B", candidate)
	if err != nil || !completionCommitMessageBindsTransaction(message, transaction.ID) {
		return tuskerError(errorInvalidTransition, "completion staging ref belongs to another transaction")
	}
	return nil
}

func completionCommitMessageBindsTransaction(message, transactionID string) bool {
	transactionID = strings.TrimSpace(transactionID)
	if transactionID == "" {
		return false
	}
	marker := "Tusker-Completion: " + transactionID
	for _, line := range strings.Split(strings.ReplaceAll(message, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == marker {
			return true
		}
	}
	return false
}

func completionTaskRepoRelativePath(repoRoot, vaultPath, taskID string) (string, error) {
	repoRoot = canonicalProjectPath(repoRoot)
	vaultPath = canonicalProjectPath(vaultPath)
	relVault, err := filepath.Rel(repoRoot, vaultPath)
	if err != nil || filepath.IsAbs(relVault) || relVault == ".." || strings.HasPrefix(filepath.Clean(relVault), ".."+string(filepath.Separator)) {
		return "", tuskerError(errorInvalidTransition, "cannot locate completion task inside the repository")
	}
	rel := filepath.Clean(filepath.Join(relVault, "work", "tasks", strings.ToUpper(strings.TrimSpace(taskID))+".md"))
	if filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", tuskerError(errorInvalidTransition, "completion task path escapes the repository")
	}
	return filepath.ToSlash(rel), nil
}
