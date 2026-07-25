package main

// The completion reactor is deliberately boring.  A review result is already
// the reviewer’s complete authority; this file turns that immutable record
// into a resumable sequence of mechanical Git/tracker operations.  In
// particular, it never asks a model to resolve a clean merge or choose a
// lifecycle transition.

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	completionTransactionSchema    = "tusker.completion-transaction/v3"
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
	Schema               string `json:"schema"`
	ID                   string `json:"id"`
	ProjectID            string `json:"project_id"`
	TaskID               string `json:"task_id"`
	WorkRevision         int    `json:"work_revision"`
	ImplementationSHA    string `json:"implementation_sha"`
	ReviewAttempt        string `json:"review_attempt"`
	ResultRevision       string `json:"result_revision"`
	ReviewedTaskStateRev string `json:"reviewed_task_state_rev"`
	WaveID               string `json:"wave_id"`
	WaveAuthorityKind    string `json:"wave_authority_kind"`
	WaveAuthorizationFP  string `json:"wave_authorization_fingerprint,omitempty"`
	WaveMaterialFP       string `json:"wave_material_fingerprint"`
	CloseAuthorityFP     string `json:"close_authority_fingerprint,omitempty"`
	IntegrationBase      string `json:"integration_base"`
	IntegrationRef       string `json:"integration_ref"`
	StagingRef           string `json:"staging_ref"`
	Phase                string `json:"phase"`
	StagedSHA            string `json:"staged_sha,omitempty"`
	StagedTaskBlob       string `json:"staged_task_blob,omitempty"`
	StagedTaskMode       string `json:"staged_task_mode,omitempty"`
	StagedReceiptBlob    string `json:"staged_receipt_blob,omitempty"`
	StagedReceiptMode    string `json:"staged_receipt_mode,omitempty"`
	Failure              string `json:"failure,omitempty"`
	Disposition          string `json:"disposition,omitempty"`
	CreatedAt            string `json:"created_at"`
	UpdatedAt            string `json:"updated_at"`
}

// completionReactorCrashHook is test-only fault injection. Production leaves
// it nil. Hooks run after an idempotent side effect and before its phase is
// persisted, which is the only crash window worth proving.
var completionReactorCrashHook func(string, *completionTransaction) error

type completionCrashInterruption struct {
	point string
	err   error
}

func (e *completionCrashInterruption) Error() string {
	return "completion crash at " + e.point + ": " + e.err.Error()
}
func (e *completionCrashInterruption) Unwrap() error { return e.err }

func injectCompletionReactorCrash(point string, transaction *completionTransaction) error {
	if completionReactorCrashHook == nil {
		return nil
	}
	if err := completionReactorCrashHook(point, transaction); err != nil {
		return &completionCrashInterruption{point: point, err: err}
	}
	return nil
}

func isCompletionCrashInterruption(err error) bool {
	var interruption *completionCrashInterruption
	return errors.As(err, &interruption)
}

func completionTransactionID(projectID string, result ReviewResult, integrationBase string, frozenAuthority ...string) string {
	parts := []string{projectID, result.TaskID, fmt.Sprintf("%d", result.WorkRevision), result.ImplementationSHA, result.AttemptID, result.ResultRevision, integrationBase}
	parts = append(parts, frozenAuthority...)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "completion:" + hex.EncodeToString(sum[:])
}

func (s *RuntimeStore) CompletionTransaction(id string) (*completionTransaction, error) {
	var raw string
	err := s.queryRowScan(`SELECT transaction_json FROM completion_transactions WHERE transaction_id=?`, []any{id}, &raw)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
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
		if errors.Is(err, sql.ErrNoRows) {
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
		if err := validatePersistedReviewResult(result); err != nil {
			return nil, tuskerError(errorInvalidTransition, "stored review result failed immutable validation: "+err.Error())
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
	var parkedRepair error
	for _, result := range results {
		if err := d.reactToReviewResult(project, wf, result, mode); err != nil {
			if errorToIssue(err).Code == completionRepairRequiredError {
				// A repair is an operator-visible outcome for this transaction, not
				// a reason to starve independent reviewed work in the same project.
				if parkedRepair == nil {
					parkedRepair = err
				}
				continue
			}
			return err
		}
	}
	return parkedRepair
}

func (d *Daemon) reactToReviewResult(project RegisteredProject, wf Workflow, result ReviewResult, mode completionReactorMode) error {
	if err := validatePersistedReviewResult(result); err != nil {
		return tuskerError(errorInvalidTransition, "stored review result failed immutable validation: "+err.Error())
	}
	if result.Schema != reviewResultSchema {
		// v1 rows remain visible/auditable but their timestamp was outside the
		// signed payload. Do not turn an upgrade into a lifecycle outage or a
		// silent authority grant; a fresh v2 reviewer result is required.
		return nil
	}
	if prior, err := d.store.CompletionTransactionForResult(project.ProjectID, result.TaskID, result.ResultRevision); err != nil {
		return err
	} else if prior != nil {
		if err := authenticateCompletionFrozenAuthority(project.ProjectID, result, prior); err != nil {
			return err
		}
		if mode == completionReactorModeShadow {
			return nil
		}
		switch result.Verdict {
		case "changes_requested":
			return d.beginCompletionDisposition(project, result, prior, "rework", strings.Join(result.Findings, "\n"))
		case "blocked":
			return d.beginCompletionDisposition(project, result, prior, "park", "Review blocked ("+result.Blocker+"): "+result.Summary)
		case "pass":
			return d.completePassingReview(project, result, prior)
		}
	}
	note, err := resolveV7Note(project.VaultRoot, result.TaskID, "task")
	if err != nil {
		return err
	}
	wave, hasWave := completionWaveForReviewedTask(project.VaultRoot, note)
	if !hasWave {
		// Binding after the reviewer froze TaskStateRev would invalidate the
		// result. requestV7ReviewAfterHandoff owns singleton creation before
		// reviewer dispatch; a result that somehow lacks that binding remains
		// awaiting manual repair without lifecycle or Git mutation.
		return nil
	}
	waveAuthorityKind, waveAuthorizationFP, waveMaterialFP, err := completionWaveAuthoritySnapshot(project.VaultRoot, wave)
	if err != nil {
		return err
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
	closeAuthorityFP := ""
	if result.Verdict == "pass" {
		closeAuthorityFP, err = completionCloseAuthoritySnapshot(project.VaultRoot, base, result)
		if err != nil {
			return err
		}
	}
	transaction := completionTransaction{
		Schema: completionTransactionSchema, ProjectID: project.ProjectID, TaskID: result.TaskID,
		WorkRevision: result.WorkRevision, ImplementationSHA: result.ImplementationSHA, ReviewAttempt: result.AttemptID,
		ResultRevision: result.ResultRevision, ReviewedTaskStateRev: result.TaskStateRev,
		WaveID: stringField(wave.Data, "id"), WaveAuthorityKind: waveAuthorityKind,
		WaveAuthorizationFP: waveAuthorizationFP, WaveMaterialFP: waveMaterialFP, CloseAuthorityFP: closeAuthorityFP,
		IntegrationBase: base, IntegrationRef: integrationRef, Phase: completionPhasePlanned,
	}
	transaction.ID = completionTransactionID(project.ProjectID, result, base, completionFrozenAuthorityParts(&transaction)...)
	transaction.StagingRef = completionStagingRef(transaction.ID)
	if prior, err := d.store.CompletionTransaction(transaction.ID); err != nil {
		return err
	} else if prior != nil {
		transaction = *prior
	}
	if err := authenticateCompletionFrozenAuthority(project.ProjectID, result, &transaction); err != nil {
		return err
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

// completionCloseAuthoritySnapshot runs the same objective policy and proof
// guards as the canonical close ceremony, then fingerprints only the
// eligibility inputs that made this exact typed result closeable.
func completionCloseAuthoritySnapshot(vaultPath, integrationView string, result ReviewResult) (string, error) {
	projection, err := completionCloseAuthorityProjectionSnapshot(vaultPath, integrationView, result)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(projection)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func completionCloseAuthorityProjectionSnapshot(vaultPath, integrationView string, result ReviewResult) (completionCloseAuthorityProjection, error) {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return completionCloseAuthorityProjection{}, err
	}
	task, ok := idx.Tasks[result.TaskID]
	if !ok {
		return completionCloseAuthorityProjection{}, tuskerError(errorNotFound, "V7 task not found: "+result.TaskID)
	}
	preflight, err := v7ClosePreflight(vaultPath, task, idx, v7ClosePreflightRequest{
		Actor: result.Actor, Action: "close", RequireReview: true,
		DependencyRef: integrationView, ExpectedTaskID: result.TaskID,
		ExpectedStateRev: result.TaskStateRev, ExpectedTaskState: "review",
	})
	if err != nil {
		return completionCloseAuthorityProjection{}, err
	}
	proof, gates, err := reviewObjectiveSnapshots(vaultPath, preflight.Task)
	if err != nil {
		return completionCloseAuthorityProjection{}, err
	}
	if proof != result.ProofFingerprint || gates != result.GateFingerprint {
		return completionCloseAuthorityProjection{}, tuskerError(errorInvalidTransition, result.TaskID+": completion close authority drifted from the typed review snapshots")
	}
	projection := completionCloseAuthorityProjection{
		Schema: "tusker.completion-close-authority/v2", TaskID: result.TaskID,
		TaskStateRev: result.TaskStateRev, Actor: result.Actor, Risk: preflight.Risk,
		RequiredAcceptor: preflight.Policy.RequiredAcceptor, RequiredEvidence: preflight.RequiredEvidence,
		RequiredGateKinds: preflight.RequiredGateKinds, ProofFingerprint: proof, GateFingerprint: gates,
		Dependencies: preflight.DependencyAuthority,
	}
	return projection, nil
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
		transaction.IntegrationRef,
	}
}

func completionFrozenAuthorityComplete(transaction *completionTransaction, requireClose bool) bool {
	if transaction == nil || transaction.WaveID == "" || transaction.WaveAuthorityKind == "" ||
		transaction.WaveMaterialFP == "" || transaction.IntegrationRef == "" ||
		(requireClose && transaction.CloseAuthorityFP == "") {
		return false
	}
	switch transaction.WaveAuthorityKind {
	case "armed":
		return transaction.WaveAuthorizationFP != ""
	case "implicit":
		return transaction.WaveAuthorizationFP == ""
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

func authenticateCompletionFrozenAuthority(projectID string, result ReviewResult, transaction *completionTransaction) error {
	if transaction == nil || transaction.Schema != completionTransactionSchema {
		return completionFrozenAuthorityRepairError(transaction, "transaction schema is legacy or missing")
	}
	if !completionFrozenAuthorityComplete(transaction, result.Verdict == "pass") {
		return completionFrozenAuthorityRepairError(transaction, "wave or close authority snapshot is missing")
	}
	if !v7CloseAuthorityDigest(transaction.WaveMaterialFP, "sha256:") ||
		(transaction.WaveAuthorityKind == "armed" && !v7CloseAuthorityDigest(transaction.WaveAuthorizationFP, "sha256:")) ||
		(result.Verdict == "pass" && !v7CloseAuthorityDigest(transaction.CloseAuthorityFP, "sha256:")) {
		return completionFrozenAuthorityRepairError(transaction, "frozen authority fingerprint is malformed")
	}
	if transaction.ProjectID != projectID || transaction.TaskID != result.TaskID ||
		transaction.WorkRevision != result.WorkRevision || transaction.ImplementationSHA != result.ImplementationSHA ||
		transaction.ReviewAttempt != result.AttemptID || transaction.ResultRevision != result.ResultRevision ||
		transaction.ReviewedTaskStateRev != result.TaskStateRev {
		return completionFrozenAuthorityRepairError(transaction, "immutable typed-result identity drifted")
	}
	expected := completionTransactionID(projectID, result, transaction.IntegrationBase, completionFrozenAuthorityParts(transaction)...)
	if transaction.ID != expected {
		return completionFrozenAuthorityRepairError(transaction, "transaction ID does not bind the persisted authority snapshot")
	}
	if transaction.StagingRef == "" || transaction.StagingRef != completionStagingRef(transaction.ID) {
		return completionFrozenAuthorityRepairError(transaction, "durable staging ref is missing or does not match the transaction")
	}
	if completionPhaseRequiresStagedTaskAttestation(transaction) &&
		(transaction.StagedSHA == "" || transaction.StagedTaskBlob == "" || transaction.StagedTaskMode != "100644" || transaction.StagedReceiptBlob == "" || transaction.StagedReceiptMode != "100644") {
		return completionFrozenAuthorityRepairError(transaction, "staged task/receipt blob and regular-file mode attestation is missing or invalid")
	}
	if err := validateCompletionTransactionPhase(transaction, result); err != nil {
		return err
	}
	return nil
}

func validateCompletionTransactionPhase(transaction *completionTransaction, result ReviewResult) error {
	if transaction == nil {
		return completionFrozenAuthorityRepairError(nil, "transaction is missing")
	}
	switch transaction.Phase {
	case completionPhasePlanned, completionPhaseStaging, completionPhaseStaged, completionPhaseGated,
		completionPhaseRefIntent, completionPhaseRefCommitted, completionPhaseCanonicalIntent,
		completionPhaseCanonicalDone, completionPhaseAudited, completionPhaseWoken,
		completionPhaseFailureIntent, completionPhaseFailureHandback, completionPhaseFailureReleased,
		completionPhaseFailureAudited, completionPhaseTerminal:
	default:
		return completionFrozenAuthorityRepairError(transaction, "unknown completion phase")
	}
	if transaction.Disposition != "" && transaction.Disposition != "rework" && transaction.Disposition != "park" {
		return completionFrozenAuthorityRepairError(transaction, "unknown completion disposition")
	}
	if transaction.Disposition != "" && !completionFailurePhase(transaction.Phase) && transaction.Phase != completionPhaseTerminal {
		return completionFrozenAuthorityRepairError(transaction, "failure disposition appears in a successful completion phase")
	}
	if transaction.Disposition == "" && completionFailurePhase(transaction.Phase) {
		return completionFrozenAuthorityRepairError(transaction, "failure phase is missing a disposition")
	}
	// A freshly persisted non-pass result starts planned; beginCompletionDisposition
	// is the next intent-first write that supplies its typed disposition.
	if transaction.Disposition == "" && result.Verdict != "pass" && transaction.Phase != completionPhasePlanned {
		return completionFrozenAuthorityRepairError(transaction, "non-pass review cannot use successful completion phases")
	}
	return nil
}

func completionPhaseRequiresStagedTaskAttestation(transaction *completionTransaction) bool {
	if transaction == nil {
		return false
	}
	// staging is an intent phase. A crash or an adversarial staging ref can
	// leave partial corroboration there; stageExactReviewCompletion classifies
	// it against a freshly derived candidate before any CAS, rather than making
	// the row an unactionable authentication dead-end.
	if transaction.Phase == completionPhaseStaging {
		return false
	}
	if transaction.StagedSHA != "" || transaction.StagedTaskBlob != "" || transaction.StagedTaskMode != "" || transaction.StagedReceiptBlob != "" || transaction.StagedReceiptMode != "" {
		return true
	}
	switch transaction.Phase {
	case completionPhaseStaged, completionPhaseGated, completionPhaseRefIntent, completionPhaseRefCommitted,
		completionPhaseCanonicalIntent, completionPhaseCanonicalDone, completionPhaseAudited, completionPhaseWoken:
		return true
	case completionPhaseTerminal:
		return transaction.Disposition == ""
	default:
		return false
	}
}

func completionPreCASCloseAuthority(vaultPath string, result ReviewResult, transaction *completionTransaction) error {
	current, err := completionCloseAuthoritySnapshot(vaultPath, transaction.IntegrationBase, result)
	if err != nil {
		return completionFrozenAuthorityRepairError(transaction, "current close eligibility is unavailable: "+err.Error())
	}
	if current != transaction.CloseAuthorityFP {
		return completionFrozenAuthorityRepairError(transaction, "close eligibility drifted from its frozen pre-CAS snapshot")
	}
	return nil
}

func completionWaveAuthoritySnapshot(vaultPath string, wave Note) (string, string, string, error) {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return "", "", "", err
	}
	waveID := stringField(wave.Data, "id")
	current, ok := idx.Waves[waveID]
	if !ok {
		return "", "", "", tuskerError(errorInvalidTransition, "completion wave binding is missing: "+waveID)
	}
	material, _ := waveMaterialFingerprint(vaultPath, idx, current)
	if v7ImplicitDeliveryUnit(current) {
		return "implicit", "", material, nil
	}
	auth := waveAuthorizationProjection(vaultPath, idx, current)
	stored := stringField(current.Data, "authorization_fingerprint")
	if stringField(auth, "state") != "armed" || boolFromAny(auth["stale"]) || stored == "" || stored != material {
		return "", "", "", tuskerError(errorInvalidTransition, "completion wave authorization is not an exact armed material snapshot")
	}
	return "armed", stored, material, nil
}

// completionAuthorizedWave verifies the current tracker binding. Failure
// dispositions use it before any Git point of no return. Successful completion
// additionally authenticates the frozen authorization/material snapshot below.
func completionAuthorizedWave(vaultPath string, transaction *completionTransaction) (Note, error) {
	if transaction == nil || strings.TrimSpace(transaction.WaveID) == "" {
		return Note{}, tuskerError(errorInvalidTransition, "completion transaction has no frozen wave binding")
	}
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return Note{}, err
	}
	wave, ok := idx.Waves[transaction.WaveID]
	if !ok {
		return Note{}, tuskerError(errorInvalidTransition, "completion wave binding is missing: "+transaction.WaveID)
	}
	task, ok := idx.Tasks[transaction.TaskID]
	if !ok || stringField(task.Data, "wave") != transaction.WaveID || !containsString(normalizeList(wave.Data["members"]), transaction.TaskID) {
		return Note{}, tuskerError(errorInvalidTransition, "completion task lost its frozen wave membership")
	}
	if v7ImplicitDeliveryUnit(wave) {
		members := normalizeList(wave.Data["members"])
		if len(members) != 1 || members[0] != transaction.TaskID || stringField(wave.Data, "delivery_task") != transaction.TaskID {
			return Note{}, tuskerError(errorInvalidTransition, "implicit completion wave no longer binds exactly one reviewed task")
		}
	}
	expectedRef := "refs/heads/" + v7WaveIntegrationBranch(wave)
	if transaction.IntegrationRef != expectedRef {
		return Note{}, tuskerError(errorInvalidTransition, "completion wave integration ref drifted from its frozen transaction")
	}
	return wave, nil
}

func completionPreCASAuthorizedWave(vaultPath string, transaction *completionTransaction) (Note, error) {
	wave, err := completionAuthorizedWave(vaultPath, transaction)
	if err != nil {
		return Note{}, err
	}
	if transaction.WaveAuthorityKind == "" || transaction.WaveMaterialFP == "" {
		return Note{}, tuskerError(errorInvalidTransition, "completion transaction lacks frozen wave material authority")
	}
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return Note{}, err
	}
	current, ok := idx.Waves[transaction.WaveID]
	if !ok {
		return Note{}, tuskerError(errorInvalidTransition, "completion wave binding is missing: "+transaction.WaveID)
	}
	material, _ := waveMaterialFingerprint(vaultPath, idx, current)
	if material != transaction.WaveMaterialFP {
		return Note{}, tuskerError(errorInvalidTransition, "completion wave material drifted from its frozen authorization")
	}
	switch transaction.WaveAuthorityKind {
	case "implicit":
		if !v7ImplicitDeliveryUnit(current) || transaction.WaveAuthorizationFP != "" {
			return Note{}, tuskerError(errorInvalidTransition, "implicit completion wave drifted from its frozen authority")
		}
	case "armed":
		auth := waveAuthorizationProjection(vaultPath, idx, current)
		stored := stringField(current.Data, "authorization_fingerprint")
		if stringField(auth, "state") != "armed" || boolFromAny(auth["stale"]) ||
			stored == "" || stored != transaction.WaveAuthorizationFP || stored != transaction.WaveMaterialFP {
			return Note{}, tuskerError(errorInvalidTransition, "completion wave authorization drifted from its frozen material authority")
		}
	default:
		return Note{}, tuskerError(errorInvalidTransition, "completion transaction has unknown frozen wave authority kind")
	}
	return wave, nil
}

func completionPostCommitWave(vaultPath string, transaction *completionTransaction) (Note, error) {
	if transaction == nil || strings.TrimSpace(transaction.WaveID) == "" {
		return Note{}, tuskerError(completionRepairRequiredError, "committed completion lost its frozen wave identity")
	}
	wave, err := resolveV7Note(vaultPath, transaction.WaveID, "wave")
	if err != nil {
		return Note{}, tuskerError(completionRepairRequiredError, "committed completion wave is unavailable for audit",
			withContext(map[string]any{
				"transaction": transaction.ID, "wave": transaction.WaveID,
				"staged": transaction.StagedSHA, "integration_ref": transaction.IntegrationRef,
			}),
			withHint("restore the frozen wave record, then replay completion; do not hand back integrated work"))
	}
	return wave, nil
}

func ensureCompletionWaveBinding(vaultPath string, transaction *completionTransaction) error {
	if transaction == nil {
		return tuskerError(errorInvalidArg, "completion transaction is required")
	}
	if transaction.WaveID == "" {
		return completionFrozenAuthorityRepairError(transaction, "wave binding is missing")
	}
	_, err := completionAuthorizedWave(vaultPath, transaction)
	return err
}

func (d *Daemon) completePassingReview(project RegisteredProject, result ReviewResult, transaction *completionTransaction) error {
	if transaction == nil {
		return completionFrozenAuthorityRepairError(nil, "transaction is missing")
	}
	if err := authenticateCompletionFrozenAuthority(project.ProjectID, result, transaction); err != nil {
		return err
	}
	if transaction.Phase == completionPhaseTerminal && transaction.Disposition != "" {
		return d.validateTerminalCompletionDisposition(project, result, transaction)
	}
	if completionFailurePhase(transaction.Phase) {
		return d.resumeCompletionDisposition(project, result, transaction)
	}
	if !v7GitRepo(project.RepoRoot) {
		if completionPhaseAcceptsCommittedRef(transaction.Phase) {
			return tuskerError(errorInvalidTransition, "completion reactor cannot reconcile a possibly committed ref without its Git repository")
		}
		return d.failCompletion(project, result, transaction, "completion reactor requires a Git repository")
	}
	if transaction.IntegrationBase == "" || transaction.IntegrationBase == "unresolved" {
		if completionPhaseAcceptsCommittedRef(transaction.Phase) {
			return tuskerError(errorInvalidTransition, "completion reactor cannot reconcile a possibly committed ref without its frozen integration base")
		}
		return d.failCompletion(project, result, transaction, "integration base is not frozen")
	}
	refExists := gitRefExists(project.RepoRoot, transaction.IntegrationRef)
	currentBase := ""
	var err error
	if refExists {
		currentBase, err = gitOutputTrim(project.RepoRoot, "rev-parse", transaction.IntegrationRef)
		if err != nil {
			return err
		}
	}

	// Ref intent is the point of no return. Reconcile the observable Git
	// outcome before consulting any mutable tracker, wave, proof, or gate
	// state. A crash after update-ref must durably become ref_committed even
	// when another completion has already advanced the same integration ref.
	refCommitted := completionPhaseHasCommittedRef(transaction.Phase)
	if transaction.Phase == completionPhaseRefIntent && refExists && currentBase != transaction.IntegrationBase {
		if transaction.StagedSHA == "" || !gitMergeBaseAncestor(project.RepoRoot, transaction.StagedSHA, currentBase) {
			return completionRefDivergenceError(transaction, currentBase)
		}
		if err := authenticateCommittedCompletionRef(project.VaultRoot, project.RepoRoot, currentBase, result, transaction); err != nil {
			return err
		}
		transaction.Phase = completionPhaseRefCommitted
		if err := d.store.SaveCompletionTransaction(transaction); err != nil {
			return err
		}
		refCommitted = true
	} else if refCommitted {
		if err := authenticateCommittedCompletionRef(project.VaultRoot, project.RepoRoot, currentBase, result, transaction); err != nil {
			return err
		}
	} else if refExists && currentBase != transaction.IntegrationBase {
		return d.failCompletion(project, result, transaction, "integration base drift: expected "+transaction.IntegrationBase+", got "+currentBase)
	}
	if !refCommitted && completionPhaseHasStagedObject(transaction.Phase) {
		if err := authenticateCompletionStagedObject(project.VaultRoot, project.RepoRoot, result, transaction); err != nil {
			return err
		}
	}

	note, err := resolveV7Note(project.VaultRoot, result.TaskID, "task")
	if err != nil {
		return err
	}
	if !refCommitted {
		if transaction.WaveID == "" {
			if err := ensureCompletionWaveBinding(project.VaultRoot, transaction); err != nil {
				return d.failCompletion(project, result, transaction, err.Error())
			}
			if err := d.store.SaveCompletionTransaction(transaction); err != nil {
				return err
			}
		}
		if _, err := completionPreCASAuthorizedWave(project.VaultRoot, transaction); err != nil {
			return d.failCompletion(project, result, transaction, err.Error())
		}
		if reason := completionReviewDrift(project.VaultRoot, note, result); reason != "" {
			return d.failCompletion(project, result, transaction, reason)
		}
		if err := completionPreCASCloseAuthority(project.VaultRoot, result, transaction); err != nil {
			return err
		}
	}
	switch transaction.Phase {
	case completionPhaseCanonicalDone, completionPhaseAudited, completionPhaseWoken, completionPhaseTerminal:
		if err := projectCompletionTaskToCanonical(project.VaultRoot, project.RepoRoot, result, transaction); err != nil {
			return err
		}
	}
	if transaction.Phase == completionPhaseTerminal {
		// The branch above has re-proved canonical bytes under the task lock;
		// authenticateCommittedCompletionRef already proved trusted receipt
		// ancestry. Terminal replay is therefore an idempotent no-op.
		return nil
	}

	if transaction.Phase == completionPhasePlanned {
		transaction.Phase = completionPhaseStaging
		if err := d.store.SaveCompletionTransaction(transaction); err != nil {
			return err
		}
		if err := injectCompletionReactorCrash("staging_intent", transaction); err != nil {
			return err
		}
	}
	if transaction.Phase == completionPhaseStaging {
		staged, stageErr := stageExactReviewCompletion(
			project.VaultRoot, project.RepoRoot, transaction.IntegrationBase, result, transaction,
			func() error { return d.store.SaveCompletionTransaction(transaction) },
		)
		if stageErr != nil {
			if isCompletionCrashInterruption(stageErr) {
				return stageErr
			}
			if errorToIssue(stageErr).Code == completionRepairRequiredError || gitRefExists(project.RepoRoot, transaction.StagingRef) {
				return d.classifyCompletionStagingFailure(project, result, transaction, stageErr.Error())
			}
			return d.failCompletion(project, result, transaction, stageErr.Error())
		}
		transaction.StagedSHA, transaction.StagedTaskBlob, transaction.StagedTaskMode = staged.SHA, staged.TaskBlob, staged.TaskMode
		transaction.StagedReceiptBlob, transaction.StagedReceiptMode, transaction.Phase = staged.ReceiptBlob, staged.ReceiptMode, completionPhaseStaged
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
		if err := injectCompletionReactorCrash("gate", transaction); err != nil {
			return err
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
		if _, err := completionPreCASAuthorizedWave(project.VaultRoot, transaction); err != nil {
			return d.failCompletion(project, result, transaction, err.Error())
		}
		if err := completionPreCASCloseAuthority(project.VaultRoot, result, transaction); err != nil {
			return err
		}
		var casErr error
		if !refExists {
			casErr = updateGitRef(project.RepoRoot, transaction.IntegrationRef, transaction.StagedSHA, strings.Repeat("0", 40))
		} else if currentBase == transaction.IntegrationBase {
			casErr = updateGitRef(project.RepoRoot, transaction.IntegrationRef, transaction.StagedSHA, transaction.IntegrationBase)
		} else {
			return completionRefDivergenceError(transaction, currentBase)
		}
		if casErr != nil {
			observedExists := gitRefExists(project.RepoRoot, transaction.IntegrationRef)
			observed := ""
			if observedExists {
				observed, err = gitOutputTrim(project.RepoRoot, "rev-parse", transaction.IntegrationRef)
				if err != nil {
					return err
				}
			}
			if observedExists && gitMergeBaseAncestor(project.RepoRoot, transaction.StagedSHA, observed) {
				if err := authenticateCommittedCompletionRef(project.VaultRoot, project.RepoRoot, observed, result, transaction); err != nil {
					return err
				}
				transaction.Phase = completionPhaseRefCommitted
				if err := d.store.SaveCompletionTransaction(transaction); err != nil {
					return err
				}
			} else if observedExists && observed != transaction.IntegrationBase {
				return completionRefDivergenceError(transaction, observed)
			} else {
				return d.failCompletion(project, result, transaction, "integration ref compare-and-swap failed: "+firstActionableLine("", casErr.Error()))
			}
		}
		if transaction.Phase == completionPhaseRefIntent {
			if err := injectCompletionReactorCrash("ref_commit", transaction); err != nil {
				return err
			}
			transaction.Phase = completionPhaseRefCommitted
			if err := d.store.SaveCompletionTransaction(transaction); err != nil {
				return err
			}
		}
	}
	if transaction.Phase == completionPhaseRefCommitted {
		transaction.Phase = completionPhaseCanonicalIntent
		if err := d.store.SaveCompletionTransaction(transaction); err != nil {
			return err
		}
	}
	if transaction.Phase == completionPhaseCanonicalIntent {
		if err := projectCompletionTaskToCanonical(project.VaultRoot, project.RepoRoot, result, transaction); err != nil {
			return err
		}
		if err := injectCompletionReactorCrash("canonical_projection", transaction); err != nil {
			return err
		}
		transaction.Phase = completionPhaseCanonicalDone
		if err := d.store.SaveCompletionTransaction(transaction); err != nil {
			return err
		}
	}
	if transaction.Phase == completionPhaseCanonicalDone {
		authority, err := newCompletionTaskCloseAuthority(project.VaultRoot, result, transaction)
		if err != nil {
			return err
		}
		if err := emitV7TaskClosedEvent(
			project.VaultRoot, result.TaskID, result.Actor, authority.ClosedAt,
			"review", "typed review "+result.ResultRevision, &authority,
		); err != nil {
			return err
		}
		wave, err := completionPostCommitWave(project.VaultRoot, transaction)
		if err != nil {
			return err
		}
		entry := v7LandingAuditEntry{Task: result.TaskID, Branch: result.ImplementationSHA, Target: strings.TrimPrefix(transaction.IntegrationRef, "refs/heads/"), GateResult: "pass", GateSummary: "typed review completion reactor", Commit: transaction.StagedSHA, Actor: "daemon:completion-reactor", Timestamp: time.Now().UTC().Format(time.RFC3339)}
		if err := appendV7WaveLandingAudit(project.VaultRoot, stringField(wave.Data, "id"), []v7LandingAuditEntry{entry}, "daemon:completion-reactor"); err != nil {
			return err
		}
		if err := injectCompletionReactorCrash("audit", transaction); err != nil {
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
		d.scheduleProjectReconcile(project.ProjectID)
		if err := injectCompletionReactorCrash("wake", transaction); err != nil {
			return err
		}
		transaction.Phase = completionPhaseWoken
		if err := d.store.SaveCompletionTransaction(transaction); err != nil {
			return err
		}
	}
	if transaction.Phase != completionPhaseWoken {
		return completionFrozenAuthorityRepairError(transaction, "cannot terminalize incomplete successful completion phase")
	}
	transaction.Phase = completionPhaseTerminal
	return d.store.SaveCompletionTransaction(transaction)
}

func completionPhaseAcceptsCommittedRef(phase string) bool {
	switch phase {
	case completionPhaseRefIntent, completionPhaseRefCommitted, completionPhaseCanonicalIntent, completionPhaseCanonicalDone, completionPhaseAudited, completionPhaseWoken, completionPhaseTerminal:
		return true
	default:
		return false
	}
}

func completionPhaseHasCommittedRef(phase string) bool {
	switch phase {
	case completionPhaseRefCommitted, completionPhaseCanonicalIntent, completionPhaseCanonicalDone, completionPhaseAudited, completionPhaseWoken, completionPhaseTerminal:
		return true
	default:
		return false
	}
}

func completionPhaseHasStagedObject(phase string) bool {
	switch phase {
	case completionPhaseStaged, completionPhaseGated, completionPhaseRefIntent:
		return true
	default:
		return false
	}
}

func completionRefDivergenceError(transaction *completionTransaction, current string) error {
	if transaction == nil {
		return tuskerError("CAS_CONFLICT", "completion integration ref diverged without a transaction")
	}
	return tuskerError("CAS_CONFLICT", "completion integration ref diverged after durable ref intent",
		withContext(map[string]any{
			"ref": transaction.IntegrationRef, "base": transaction.IntegrationBase,
			"staged": transaction.StagedSHA, "current": current,
		}),
		withHint("do not hand back or rewind work until the integration ancestry is reconciled"))
}

// authenticateCommittedCompletionRef proves both reachability and tree
// retention. A later same-wave completion may advance the integration tip, but
// it must retain this transaction's exact staged task blob and the durable
// staging ref must still authenticate the reviewed merge object.
func authenticateCommittedCompletionRef(vaultPath, repoRoot, current string, result ReviewResult, transaction *completionTransaction) error {
	if transaction == nil || transaction.StagedSHA == "" || current == "" ||
		!gitMergeBaseAncestor(repoRoot, transaction.StagedSHA, current) {
		return completionRefDivergenceError(transaction, current)
	}
	// The integration ancestry is the trust root after CAS. A staging ref may
	// corroborate recovery but is never required to authenticate a receipt that
	// is already reachable from the trusted integration history.
	if err := validateCompletionStagingCandidate(vaultPath, repoRoot, transaction.StagedSHA, transaction.IntegrationBase, result, transaction); err != nil {
		return tuskerError("CAS_CONFLICT", "committed completion staged object failed authentication: "+err.Error())
	}
	taskRel, err := completionTaskRepoRelativePath(repoRoot, vaultPath, result.TaskID)
	if err != nil {
		return err
	}
	if transaction.StagedTaskBlob == "" || transaction.StagedTaskMode == "" {
		return completionFrozenAuthorityRepairError(transaction, "committed completion is missing its staged task tree entry attestation")
	}
	stagedEntry, err := completionGitTreeEntryAt(repoRoot, transaction.StagedSHA, taskRel)
	if err != nil {
		return tuskerError("CAS_CONFLICT", "committed completion lost its exact staged task tree entry")
	}
	if stagedEntry.OID != transaction.StagedTaskBlob || stagedEntry.Mode != transaction.StagedTaskMode {
		return tuskerError("CAS_CONFLICT", "committed completion staged tree no longer matches its generated task entry",
			withContext(map[string]any{
				"task": result.TaskID, "generated_blob": transaction.StagedTaskBlob,
				"generated_mode": transaction.StagedTaskMode, "staged_blob": stagedEntry.OID, "staged_mode": stagedEntry.Mode,
			}))
	}
	currentEntry, err := completionGitTreeEntryAt(repoRoot, current, taskRel)
	if err != nil {
		return tuskerError("CAS_CONFLICT", "completion integration descendant no longer retains the staged task")
	}
	if currentEntry != stagedEntry {
		return tuskerError("CAS_CONFLICT", "completion integration descendant changed the transaction's staged task entry",
			withContext(map[string]any{
				"task": result.TaskID, "staged_blob": stagedEntry.OID, "staged_mode": stagedEntry.Mode,
				"current_blob": currentEntry.OID, "current_mode": currentEntry.Mode, "current_type": currentEntry.Type,
			}))
	}
	return nil
}

func authenticateCompletionStagedObject(vaultPath, repoRoot string, result ReviewResult, transaction *completionTransaction) error {
	if transaction == nil || transaction.StagedSHA == "" || transaction.StagingRef == "" {
		return completionFrozenAuthorityRepairError(transaction, "staged completion object identity is missing")
	}
	if !gitRefExists(repoRoot, transaction.StagingRef) {
		return completionFrozenAuthorityRepairError(transaction, "durable staging ref is missing")
	}
	stagingTip, err := gitOutputTrim(repoRoot, "rev-parse", transaction.StagingRef)
	if err != nil {
		return err
	}
	if stagingTip != transaction.StagedSHA {
		return tuskerError("CAS_CONFLICT", "completion staging ref no longer authenticates its staged object",
			withContext(map[string]any{"staging_ref": transaction.StagingRef, "expected": transaction.StagedSHA, "current": stagingTip}))
	}
	return validateCompletionStagingCandidate(vaultPath, repoRoot, transaction.StagedSHA, transaction.IntegrationBase, result, transaction)
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

// classifyCompletionStagingFailure records an authenticated external staging
// failure as a durable, non-handback parked disposition. This is deliberately
// distinct from a corrupt transaction row: we never write new facts into a row
// whose own authority cannot be authenticated.
func (d *Daemon) classifyCompletionStagingFailure(project RegisteredProject, result ReviewResult, transaction *completionTransaction, reason string) error {
	if transaction == nil {
		return completionFrozenAuthorityRepairError(nil, "cannot classify missing completion transaction")
	}
	if transaction.Phase == completionPhaseTerminal {
		return nil
	}
	if !completionFailurePhase(transaction.Phase) {
		transaction.Disposition = "park"
		transaction.Failure = "completion repair " + transaction.ID + ": " + limitLandingSummary(reason, 500)
		transaction.Phase = completionPhaseFailureIntent
		if err := d.store.SaveCompletionTransaction(transaction); err != nil {
			return err
		}
	}
	if err := d.resumeCompletionDisposition(project, result, transaction); err != nil {
		return err
	}
	return nil
}

func (d *Daemon) beginCompletionDisposition(project RegisteredProject, result ReviewResult, transaction *completionTransaction, disposition, reason string) error {
	if err := authenticateCompletionFrozenAuthority(project.ProjectID, result, transaction); err != nil {
		return err
	}
	if transaction.Phase == completionPhaseTerminal {
		return nil
	}
	if err := ensureCompletionWaveBinding(project.VaultRoot, transaction); err != nil {
		return err
	}
	if transaction.ReviewedTaskStateRev == "" {
		transaction.ReviewedTaskStateRev = result.TaskStateRev
	}
	if !completionFailurePhase(transaction.Phase) {
		transaction.Disposition = disposition
		transaction.Failure = strings.TrimSpace(reason)
		transaction.Phase = completionPhaseFailureIntent
		if err := d.store.SaveCompletionTransaction(transaction); err != nil {
			return err
		}
		if err := injectCompletionReactorCrash("failure_intent", transaction); err != nil {
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
	if err := validateCompletionTransactionPhase(transaction, result); err != nil {
		return err
	}
	if transaction.Phase == completionPhaseFailureIntent {
		if transaction.Disposition == "rework" {
			if err := d.returnCompletionFindingToImplementer(project, result, transaction); err != nil {
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

// Terminal failure rows are not permissions to skip their observable effects.
// Rework needs the persistent handback marker (unless a newer owner won), and
// both dispositions must have released the old review lease before becoming a
// terminal no-op.
func (d *Daemon) validateTerminalCompletionDisposition(project RegisteredProject, result ReviewResult, transaction *completionTransaction) error {
	if transaction.Phase != completionPhaseTerminal || transaction.Disposition == "" {
		return completionFrozenAuthorityRepairError(transaction, "terminal failure disposition is malformed")
	}
	task, err := resolveV7Note(project.VaultRoot, result.TaskID, "task")
	if err != nil {
		return err
	}
	if transaction.Disposition == "rework" && !strings.Contains(task.Body, completionHandbackMarker(transaction.ID)) {
		newer, newerErr := d.newerReviewOwnsCompletionTask(project.ProjectID, result)
		if newerErr != nil || !newer {
			return completionFrozenAuthorityRepairError(transaction, "terminal rework lacks durable handback effect")
		}
	}
	runs, err := d.store.ListRuns()
	if err != nil {
		return err
	}
	for _, run := range runs {
		if run.ProjectID == project.ProjectID && run.RecordID == result.TaskID && run.Lane == runLaneReview && run.WorkRevision == result.WorkRevision && run.ActiveAttemptID == result.AttemptID && isDispatchingLeaseState(run.LeaseState) {
			return completionFrozenAuthorityRepairError(transaction, "terminal failure still owns active review lease")
		}
	}
	return nil
}

func completionHandbackMarker(transactionID string) string {
	return "<!-- tusker:completion-handback " + strings.TrimSpace(transactionID) + " -->"
}

func (d *Daemon) returnCompletionFindingToImplementer(project RegisteredProject, result ReviewResult, transaction *completionTransaction) error {
	task, err := resolveV7Note(project.VaultRoot, result.TaskID, "task")
	if err != nil {
		return err
	}
	currentWork := intField(task.Data, "work_revision")
	currentSource := firstNonEmpty(stringField(task.Data, "source_sha"), stringField(task.Data, "source_commit"))
	currentState := stringField(task.Data, "state_rev")
	status := stringField(task.Data, "status")
	marker := completionHandbackMarker(transaction.ID)

	// A later implementation/review owns the task now. The old transaction
	// still finishes release/audit bookkeeping, but never rewinds that work.
	newerReview, err := d.newerReviewOwnsCompletionTask(project.ProjectID, result)
	if err != nil {
		return err
	}
	if currentWork > result.WorkRevision || newerReview {
		return nil
	}
	if currentWork < result.WorkRevision {
		return tuskerError("CAS_CONFLICT", "completion handback found an older work revision", withContext(map[string]any{"task": result.TaskID, "expected_work_revision": result.WorkRevision, "current_work_revision": currentWork}))
	}
	switch status {
	case "done", "cancelled", "superseded":
		// Terminal monotonicity also applies before our marker is written. A
		// crash after failure_intent must finish bookkeeping, not rewind a
		// concurrent terminal decision or poison every future poll.
		return nil
	}
	if strings.Contains(task.Body, marker) {
		switch status {
		case "rework":
			return nil
		case "review":
			if currentSource != result.ImplementationSHA {
				return tuskerError("CAS_CONFLICT", "completion handback marker exists on a different source revision")
			}
			// This is our own crash between the marker write and status flip.
			return statusV7Cmd(Args{
				"vault": project.VaultRoot, "quiet": "true", "local": "true",
				"id": result.TaskID, "status": "rework", "by": "daemon:completion-reactor",
				"reason": reviewerFindingReturnReason,
			})
		case "done", "cancelled", "superseded":
			return nil // terminal monotonicity wins over an old handback.
		default:
			return tuskerError("CAS_CONFLICT", "completion handback marker found on unrelated same-revision status",
				withContext(map[string]any{"task": result.TaskID, "status": status}))
		}
	}
	if currentWork == result.WorkRevision && currentSource == result.ImplementationSHA &&
		currentState == transaction.ReviewedTaskStateRev && status == "review" {
		finding := marker + "\n\n" + transaction.Failure
		return returnReviewerFindingToImplementer(project.VaultRoot, result.TaskID, finding, "daemon:completion-reactor")
	}
	return tuskerError("CAS_CONFLICT", "completion handback refused unrelated same-revision drift",
		withContext(map[string]any{
			"task": result.TaskID, "expected_state_rev": transaction.ReviewedTaskStateRev, "current_state_rev": currentState,
			"expected_source": result.ImplementationSHA, "current_source": currentSource, "status": status,
		}),
		withHint("inspect the newer task/review state; do not replay the old handback over it"))
}

func (d *Daemon) newerReviewOwnsCompletionTask(projectID string, result ReviewResult) (bool, error) {
	runs, err := d.store.ListRuns()
	if err != nil {
		return false, err
	}
	for _, run := range runs {
		if run.ProjectID != projectID || run.RecordID != result.TaskID || run.Lane != runLaneReview ||
			run.ActiveAttemptID == "" || run.ActiveAttemptID == result.AttemptID ||
			!isDispatchingLeaseState(run.LeaseState) {
			continue
		}
		if run.WorkRevision > result.WorkRevision {
			return true, nil
		}
		if run.WorkRevision != result.WorkRevision {
			continue
		}
		attempts, attemptErr := d.store.ListAttemptsForRun(projectID, result.TaskID)
		if attemptErr != nil {
			return false, attemptErr
		}
		if completionAttemptFollowsResult(attempts, run.ActiveAttemptID, result) {
			return true, nil
		}
	}
	results, err := d.store.ListReviewResults(projectID)
	if err != nil {
		return false, err
	}
	for _, candidate := range results {
		if candidate.TaskID == result.TaskID && candidate.ResultRevision != result.ResultRevision &&
			(candidate.WorkRevision > result.WorkRevision ||
				(candidate.WorkRevision == result.WorkRevision && candidate.AttemptID != result.AttemptID &&
					completionTimestampAfter(candidate.CreatedAt, result.CreatedAt))) {
			return true, nil
		}
	}
	return false, nil
}

func completionAttemptFollowsResult(attempts []RunAttempt, candidateID string, result ReviewResult) bool {
	candidateAt, reviewedAt := "", ""
	for _, attempt := range attempts {
		switch attempt.AttemptID {
		case candidateID:
			candidateAt = attempt.StartedAt
		case result.AttemptID:
			reviewedAt = attempt.StartedAt
		}
	}
	if completionTimestampAfter(candidateAt, result.CreatedAt) {
		return true
	}
	return strings.TrimSpace(result.CreatedAt) == "" && completionTimestampAfter(candidateAt, reviewedAt)
}

func completionTimestampAfter(candidate, prior string) bool {
	candidateAt, candidateOK := completionTimestamp(candidate)
	priorAt, priorOK := completionTimestamp(prior)
	return candidateOK && priorOK && candidateAt.After(priorAt)
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
	wave, err := completionAuthorizedWave(project.VaultRoot, transaction)
	if err != nil {
		return err
	}
	waveID := stringField(wave.Data, "id")
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

func stageExactReviewCompletion(
	vaultPath, repoRoot, integrationBase string,
	result ReviewResult,
	transaction *completionTransaction,
	persistAttestation func() error,
) (completionStagingCandidate, error) {
	if transaction == nil || transaction.StagingRef == "" {
		return completionStagingCandidate{}, tuskerError(errorInvalidArg, "completion staging requires a persisted transaction ref")
	}
	if gitRefExists(repoRoot, transaction.StagingRef) {
		candidateSHA, err := gitOutputTrim(repoRoot, "rev-parse", transaction.StagingRef)
		if err != nil {
			return completionStagingCandidate{}, err
		}
		// A staging-intent crash predates receipt attestations. Re-derive them
		// from the deterministic candidate before judging the extant ref, so a
		// forged ref is classified by its actual parent/tree mismatch instead of
		// being hidden behind a missing new-field error.
		if transaction.StagedReceiptBlob == "" || transaction.StagedReceiptMode == "" {
			expected, buildErr := buildExactReviewCompletionCandidate(vaultPath, repoRoot, integrationBase, result, transaction)
			if buildErr != nil {
				return completionStagingCandidate{}, buildErr
			}
			if transaction.StagedTaskBlob == "" {
				transaction.StagedTaskBlob, transaction.StagedTaskMode = expected.TaskBlob, expected.TaskMode
			}
			transaction.StagedReceiptBlob, transaction.StagedReceiptMode = expected.ReceiptBlob, expected.ReceiptMode
		}
		if err := validateCompletionStagingCandidate(vaultPath, repoRoot, candidateSHA, integrationBase, result, transaction); err != nil {
			return completionStagingCandidate{}, err
		}
		expected, err := buildExactReviewCompletionCandidate(vaultPath, repoRoot, integrationBase, result, transaction)
		if err != nil {
			return completionStagingCandidate{}, err
		}
		if candidateSHA != expected.SHA {
			return completionStagingCandidate{}, tuskerError(errorInvalidTransition, "completion staging ref does not match the deterministic reviewed completion object")
		}
		return expected, nil
	}
	candidate, err := buildExactReviewCompletionCandidate(vaultPath, repoRoot, integrationBase, result, transaction)
	if err != nil {
		return completionStagingCandidate{}, err
	}
	transaction.StagedSHA = candidate.SHA
	transaction.StagedTaskBlob = candidate.TaskBlob
	transaction.StagedTaskMode = candidate.TaskMode
	transaction.StagedReceiptBlob = candidate.ReceiptBlob
	transaction.StagedReceiptMode = candidate.ReceiptMode
	if err := validateCompletionStagingCandidate(vaultPath, repoRoot, candidate.SHA, integrationBase, result, transaction); err != nil {
		return completionStagingCandidate{}, err
	}
	if persistAttestation == nil {
		return completionStagingCandidate{}, tuskerError(errorInvalidArg, "completion staging requires durable task entry attestation")
	}
	if err := persistAttestation(); err != nil {
		return completionStagingCandidate{}, err
	}
	if err := injectCompletionReactorCrash("staging_commit", transaction); err != nil {
		return completionStagingCandidate{}, err
	}
	if err := updateGitRef(repoRoot, transaction.StagingRef, candidate.SHA, strings.Repeat("0", 40)); err != nil {
		if existing, readErr := gitOutputTrim(repoRoot, "rev-parse", transaction.StagingRef); readErr != nil || existing != candidate.SHA {
			return completionStagingCandidate{}, tuskerError(errorInvalidTransition, "completion staging ref compare-and-swap failed: "+firstActionableLine("", err.Error()))
		}
	}
	if err := injectCompletionReactorCrash("staging_ref", transaction); err != nil {
		return completionStagingCandidate{}, err
	}
	return candidate, nil
}

func buildExactReviewCompletionCandidate(vaultPath, repoRoot, integrationBase string, result ReviewResult, transaction *completionTransaction) (completionStagingCandidate, error) {
	if transaction == nil {
		return completionStagingCandidate{}, tuskerError(errorInvalidArg, "completion staging requires a persisted transaction")
	}
	taskRel, err := completionTaskRepoRelativePath(repoRoot, vaultPath, result.TaskID)
	if err != nil {
		return completionStagingCandidate{}, err
	}
	tmp, err := os.MkdirTemp("", "tusker-completion-stage-*")
	if err != nil {
		return completionStagingCandidate{}, err
	}
	defer func() {
		_ = exec.Command("git", "-C", repoRoot, "worktree", "remove", "--force", tmp).Run()
		_ = os.RemoveAll(tmp)
	}()
	if output, err := gitCombined(repoRoot, "worktree", "add", "--detach", tmp, integrationBase); err != nil {
		return completionStagingCandidate{}, tuskerError(errorInvalidTransition, "failed to create completion staging worktree: "+firstActionableLine(output, err.Error()))
	}
	if output, err := gitCombined(tmp, "merge", "--no-ff", "--no-commit", result.ImplementationSHA); err != nil {
		return completionStagingCandidate{}, tuskerError(errorInvalidTransition, landingFailureSummary("merge "+result.ImplementationSHA, output, err))
	}
	if err := removeV7WorkspaceMetadataFromLanding(tmp); err != nil {
		return completionStagingCandidate{}, err
	}
	if err := materializeReviewedDone(tmp, vaultPath, taskRel, result, transaction); err != nil {
		return completionStagingCandidate{}, err
	}
	generated, err := os.ReadFile(filepath.Join(tmp, filepath.FromSlash(taskRel)))
	if err != nil {
		return completionStagingCandidate{}, err
	}
	taskEntry, err := stageExactCompletionTaskBlob(tmp, taskRel, generated)
	if err != nil {
		return completionStagingCandidate{}, err
	}
	receipt, receiptRaw, err := newCompletionReceipt(vaultPath, taskRel, taskEntry, result, transaction)
	if err != nil {
		return completionStagingCandidate{}, err
	}
	receiptRel := completionReceiptRepoPath(receipt.ReceiptID)
	receiptEntry, err := stageExactCompletionBlob(tmp, receiptRel, receiptRaw)
	if err != nil {
		return completionStagingCandidate{}, err
	}
	tree, err := gitOutputTrim(tmp, "write-tree")
	if err != nil {
		return completionStagingCandidate{}, tuskerError(errorInvalidTransition, "failed to freeze reviewed task closure tree: "+firstActionableLine("", err.Error()))
	}
	treeTaskEntry, err := completionGitTreeEntryAt(tmp, tree, taskRel)
	if err != nil || treeTaskEntry != taskEntry {
		return completionStagingCandidate{}, tuskerError(errorInvalidTransition, "reviewed task tree does not retain the exact generated regular-file entry")
	}
	treeReceiptEntry, err := completionGitTreeEntryAt(tmp, tree, receiptRel)
	if err != nil || treeReceiptEntry != receiptEntry {
		return completionStagingCandidate{}, tuskerError(errorInvalidTransition, "completion receipt tree does not retain the exact generated regular-file entry")
	}
	if err := guardV7LandingTerminalTaskRewindsAt(tmp, integrationBase, tree); err != nil {
		return completionStagingCandidate{}, err
	}
	message := "Complete reviewed task " + result.TaskID + "\n\nTusker-Completion: " + transaction.ID
	commit := exec.Command("git", "-C", tmp, "-c", "commit.gpgsign=false", "commit-tree", tree,
		"-p", integrationBase, "-p", result.ImplementationSHA)
	commit.Stdin = strings.NewReader(message + "\n")
	stamp := completionResultTimestamp(result)
	commit.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Tusker", "GIT_AUTHOR_EMAIL=tusker@localhost",
		"GIT_COMMITTER_NAME=Tusker", "GIT_COMMITTER_EMAIL=tusker@localhost",
		"GIT_AUTHOR_DATE="+stamp, "GIT_COMMITTER_DATE="+stamp,
	)
	output, err := commit.CombinedOutput()
	if err != nil {
		return completionStagingCandidate{}, tuskerError(errorInvalidTransition, "failed to commit reviewed task closure: "+firstActionableLine(string(output), err.Error()))
	}
	sha := strings.TrimSpace(string(output))
	if sha == "" {
		return completionStagingCandidate{}, tuskerError(errorInvalidTransition, "failed to commit reviewed task closure: commit-tree returned no object")
	}
	commitTaskEntry, err := completionGitTreeEntryAt(repoRoot, sha, taskRel)
	if err != nil || commitTaskEntry != taskEntry {
		return completionStagingCandidate{}, tuskerError(errorInvalidTransition, "reviewed completion commit does not retain the exact generated regular-file entry")
	}
	commitReceiptEntry, err := completionGitTreeEntryAt(repoRoot, sha, receiptRel)
	if err != nil || commitReceiptEntry != receiptEntry {
		return completionStagingCandidate{}, tuskerError(errorInvalidTransition, "completion commit does not retain the exact generated receipt entry")
	}
	return completionStagingCandidate{SHA: sha, TaskBlob: taskEntry.OID, TaskMode: taskEntry.Mode, ReceiptBlob: receiptEntry.OID, ReceiptMode: receiptEntry.Mode}, nil
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

func stageExactCompletionTaskBlob(worktree, taskRel string, generated []byte) (completionGitTreeEntry, error) {
	stage, err := gitOutputTrim(worktree, "ls-files", "--stage", "--", taskRel)
	fields := strings.Fields(stage)
	if err != nil || len(fields) < 3 || fields[2] != "0" {
		return completionGitTreeEntry{}, tuskerError(errorInvalidTransition, "reviewed task does not have one resolved stage-zero index entry")
	}
	mode := fields[0]
	if mode != "100644" {
		return completionGitTreeEntry{}, tuskerError(errorInvalidTransition, "reviewed task must remain a regular non-executable file")
	}
	hash := exec.Command("git", "-C", worktree, "hash-object", "-w", "--stdin")
	hash.Stdin = bytes.NewReader(generated)
	output, err := hash.CombinedOutput()
	if err != nil {
		return completionGitTreeEntry{}, tuskerError(errorInvalidTransition, "failed to write exact reviewed task blob: "+firstActionableLine(string(output), err.Error()))
	}
	blob := strings.TrimSpace(string(output))
	if blob == "" {
		return completionGitTreeEntry{}, tuskerError(errorInvalidTransition, "failed to write exact reviewed task blob: hash-object returned no object")
	}
	object := exec.Command("git", "-C", worktree, "cat-file", "blob", blob)
	roundTrip, err := object.Output()
	if err != nil || !bytes.Equal(roundTrip, generated) {
		return completionGitTreeEntry{}, tuskerError(errorInvalidTransition, "reviewed task blob does not match the exact generated bytes")
	}
	if output, err := gitCombined(worktree, "update-index", "--add", "--cacheinfo", mode, blob, taskRel); err != nil {
		return completionGitTreeEntry{}, tuskerError(errorInvalidTransition, "failed to install exact reviewed task blob in index: "+firstActionableLine(output, err.Error()))
	}
	index, err := gitOutputTrim(worktree, "ls-files", "--stage", "--", taskRel)
	indexFields := strings.Fields(index)
	if err != nil || len(indexFields) < 3 || indexFields[0] != mode || indexFields[1] != blob || indexFields[2] != "0" {
		return completionGitTreeEntry{}, tuskerError(errorInvalidTransition, "reviewed task index entry does not authenticate the exact generated blob")
	}
	return completionGitTreeEntry{Mode: mode, Type: "blob", OID: blob}, nil
}

// stageExactCompletionBlob uses raw hash-object/update-index like the task
// path, but can introduce a sibling receipt that did not exist at the base.
func stageExactCompletionBlob(worktree, rel string, raw []byte) (completionGitTreeEntry, error) {
	hash := exec.Command("git", "-C", worktree, "hash-object", "-w", "--stdin")
	hash.Stdin = bytes.NewReader(raw)
	output, err := hash.CombinedOutput()
	if err != nil {
		return completionGitTreeEntry{}, tuskerError(errorInvalidTransition, "failed to write exact completion receipt blob: "+firstActionableLine(string(output), err.Error()))
	}
	blob := strings.TrimSpace(string(output))
	if blob == "" {
		return completionGitTreeEntry{}, tuskerError(errorInvalidTransition, "completion receipt hash-object returned no object")
	}
	if output, err := gitCombined(worktree, "update-index", "--add", "--cacheinfo", "100644,"+blob+","+rel); err != nil {
		return completionGitTreeEntry{}, tuskerError(errorInvalidTransition, "failed to stage exact completion receipt blob: "+firstActionableLine(output, err.Error()))
	}
	index, indexErr := gitOutputTrim(worktree, "ls-files", "--stage", "--", rel)
	fields := strings.Fields(index)
	if indexErr != nil || len(fields) < 3 || fields[0] != "100644" || fields[1] != blob || fields[2] != "0" {
		return completionGitTreeEntry{}, tuskerError(errorInvalidTransition, "completion receipt index entry does not authenticate exact blob")
	}
	return completionGitTreeEntry{Mode: "100644", Type: "blob", OID: blob}, nil
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
	if err != nil || !strings.Contains(message, "Tusker-Completion: "+transaction.ID) {
		return tuskerError(errorInvalidTransition, "completion staging ref belongs to another transaction")
	}
	return nil
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

func materializeReviewedDone(stageRoot, vaultPath, taskRel string, result ReviewResult, transaction *completionTransaction) error {
	path := filepath.Join(stageRoot, filepath.FromSlash(taskRel))
	if info, err := os.Lstat(path); err != nil {
		return err
	} else if !info.Mode().IsRegular() {
		return tuskerError(errorInvalidTransition, "reviewed task must remain a regular file before completion materialization")
	}
	canonicalPath := filepath.Join(canonicalProjectPath(vaultPath), "work", "tasks", result.TaskID+".md")
	canonicalData, canonicalBody, err := parseFrontmatterMustRead(canonicalPath)
	if err != nil {
		return err
	}
	if canonicalRev := stringField(canonicalData, "state_rev"); canonicalRev == "" || !v7StateRevMatches(canonicalData, canonicalBody, canonicalRev) {
		return tuskerError("CAS_CONFLICT", "canonical task bytes changed without a refreshed state_rev before staging")
	}
	if stringField(canonicalData, "id") != result.TaskID ||
		stringField(canonicalData, "state_rev") != result.TaskStateRev ||
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
	now := completionResultTimestamp(result)
	authority, err := newCompletionTaskCloseAuthority(vaultPath, result, transaction)
	if err != nil {
		return err
	}
	applyV7TaskCloseProjection(data, result.Actor, now, authority.mapValue())
	data["source_sha"] = result.ImplementationSHA
	data["verified_by"], data["verified_at"] = result.Actor, now
	body = upsertV7VerificationRow(body, v7VerificationRow{
		CoverText: strings.Join(result.Covers, ","),
		Check:     "typed review " + result.AttemptID,
		Result:    "pass",
		Notes:     "[tusker-review-result:" + result.ResultRevision + "] " + result.Summary,
	})
	_, err = saveV7DocumentCAS(path, data, body, v7FrontmatterOrder["task"], stringField(data, "state_rev"))
	return err
}

// projectCompletionTaskToCanonical copies the authenticated staged task blob
// byte-for-byte under the normal document lock. The original reviewed
// state_rev is the CAS base; the immutable result marker makes a replay
// distinguish our completed write from unrelated terminal state.
func projectCompletionTaskToCanonical(vaultPath, repoRoot string, result ReviewResult, transaction *completionTransaction) error {
	if transaction == nil || transaction.StagedSHA == "" {
		return tuskerError(errorInvalidTransition, "canonical completion projection requires a staged commit")
	}
	rel, err := completionTaskRepoRelativePath(repoRoot, vaultPath, result.TaskID)
	if err != nil {
		return err
	}
	stagedRaw, err := gitCombined(repoRoot, "show", transaction.StagedSHA+":"+rel)
	if err != nil {
		return err
	}
	stagedData, stagedBody, err := parseFrontmatter(stagedRaw)
	if err != nil {
		return err
	}
	staged := Note{Data: stagedData, Body: stagedBody}
	if effectiveV7Kind(stagedData) != "task" || stringField(stagedData, "id") != result.TaskID ||
		!v7StateRevMatches(stagedData, stagedBody, stringField(stagedData, "state_rev")) ||
		!completionCanonicalTaskMatches(staged, result, transaction) {
		return tuskerError(errorInvalidTransition, "staged completion task is not the exact reviewed done projection")
	}

	taskPath := filepath.Join(vaultPath, "work", "tasks", result.TaskID+".md")
	lock, err := acquireV7DocumentLock(taskPath, v7DocumentLockTimeout)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()

	currentRaw, err := os.ReadFile(taskPath)
	if err != nil {
		return err
	}
	if string(currentRaw) == stagedRaw {
		return nil
	}
	currentData, currentBody, err := parseFrontmatter(string(currentRaw))
	if err != nil {
		return err
	}
	currentState := stringField(currentData, "state_rev")
	if currentState != "" && !v7StateRevMatches(currentData, currentBody, currentState) {
		return tuskerError("CAS_CONFLICT", "canonical completion task content changed without a refreshed state_rev",
			withPath(taskPath), withContext(map[string]any{"current_rev": currentState, "actual_rev": v7StateRev(currentData, currentBody)}))
	}
	if stringField(currentData, "id") != result.TaskID {
		return tuskerError("CAS_CONFLICT", "canonical completion task identity drifted under task lock", withPath(taskPath))
	}
	current := Note{Data: currentData, Body: currentBody}
	if completionCanonicalTaskMatches(current, result, transaction) {
		return tuskerError("CAS_CONFLICT", "canonical completion marker exists with bytes that differ from the authenticated staged task",
			withPath(taskPath), withContext(map[string]any{"task": result.TaskID, "result_revision": result.ResultRevision}))
	}
	currentSource := firstNonEmpty(stringField(currentData, "source_sha"), stringField(currentData, "source_commit"))
	if currentState != transaction.ReviewedTaskStateRev ||
		stringField(currentData, "status") != "review" ||
		intField(currentData, "work_revision") != result.WorkRevision ||
		currentSource != result.ImplementationSHA {
		return tuskerError("CAS_CONFLICT", "canonical completion projection refused task drift after integration CAS",
			withPath(taskPath), withContext(map[string]any{
				"task": result.TaskID, "expected_state_rev": transaction.ReviewedTaskStateRev, "current_state_rev": currentState,
				"expected_source": result.ImplementationSHA, "current_source": currentSource, "status": stringField(currentData, "status"),
			}))
	}
	if err := atomicReplaceV7Document(taskPath, stagedRaw); err != nil {
		return err
	}
	invalidateCachedNote(taskPath)
	return nil
}
