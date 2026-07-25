package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// DepartureState is intentionally independent of task and attempt lifecycle
// states. Departures are daemon-owned system work, not synthetic task runs.
type DepartureState string

const (
	DepartureStateDue        DepartureState = "due"
	DepartureStateEvaluating DepartureState = "evaluating"
	DepartureStateStaging    DepartureState = "staging"
	DepartureStateGating     DepartureState = "gating"
	DepartureStatePromoted   DepartureState = "promoted"
	DepartureStateReleasing  DepartureState = "releasing"
	DepartureStatePassed     DepartureState = "passed"
	DepartureStateSkipped    DepartureState = "skipped"
	DepartureStateBlocked    DepartureState = "blocked"
	DepartureStateFailed     DepartureState = "failed"
	DepartureStateRepairing  DepartureState = "repairing"
)

// DepartureCandidate pins the exact inputs that a later promotion must
// revalidate. It contains fingerprints and references only; raw command logs
// belong in runtime scratch/artifacts, never in a durable departure row.
type DepartureCandidate struct {
	TaskStateRevisions       map[string]string `json:"task_state_revisions,omitempty"`
	TaskSourceSHAs           map[string]string `json:"task_source_shas,omitempty"`
	WaveAuthorization        string            `json:"wave_authorization,omitempty"`
	IntegrationBaseSHA       string            `json:"integration_base_sha,omitempty"`
	CandidateSHA             string            `json:"candidate_sha,omitempty"`
	CandidateTreeHash        string            `json:"candidate_tree_hash,omitempty"`
	ExpectedDefaultBranchSHA string            `json:"expected_default_branch_sha,omitempty"`
}

type DepartureGate struct {
	Command     string           `json:"command,omitempty"`
	Profile     string           `json:"profile,omitempty"`
	Toolchain   string           `json:"toolchain,omitempty"`
	TreeHash    string           `json:"tree_hash,omitempty"`
	Status      string           `json:"status,omitempty"`
	StartedAt   string           `json:"started_at,omitempty"`
	FinishedAt  string           `json:"finished_at,omitempty"`
	ArtifactRef string           `json:"artifact_ref,omitempty"`
	Failure     DepartureFailure `json:"failure,omitempty"`
}

// DepartureFailure keeps promotion-red evidence referential and bounded. The
// raw command log stays in the runtime artifact named here, never in SQLite or
// a generated repair task.
type DepartureFailure struct {
	Class        string                 `json:"class,omitempty"`
	Identity     string                 `json:"identity,omitempty"`
	OwningTaskID string                 `json:"owning_task_id,omitempty"`
	BisectionRef string                 `json:"bisection_ref,omitempty"`
	ArtifactRefs []string               `json:"artifact_refs,omitempty"`
	RepairTaskID string                 `json:"repair_task_id,omitempty"`
	ModelTriage  bool                   `json:"model_triage,omitempty"`
	Packet       PromotionFailurePacket `json:"packet,omitempty"`
}

// DeparturePromotion distinguishes an intent from an observed committed ref.
// Recovery treats an intent with no committed ref as ambiguous: retrying a ref
// move would be unsafe because the process may have died after the remote move.
type DeparturePromotion struct {
	ExpectedRef  string `json:"expected_ref,omitempty"`
	ExpectedSHA  string `json:"expected_sha,omitempty"`
	CommittedRef string `json:"committed_ref,omitempty"`
	CommittedSHA string `json:"committed_sha,omitempty"`
	AttemptedAt  string `json:"attempted_at,omitempty"`
	CommittedAt  string `json:"committed_at,omitempty"`
}

// DepartureRelease stores only deterministic release identity/result facts.
// Credentials and raw release output are deliberately excluded.
type DepartureRelease struct {
	Profile        string `json:"profile,omitempty"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
	Revision       string `json:"revision,omitempty"`
	Status         string `json:"status,omitempty"`
	AttemptedAt    string `json:"attempted_at,omitempty"`
	CompletedAt    string `json:"completed_at,omitempty"`
	RollbackRef    string `json:"rollback_ref,omitempty"`
	ArtifactRef    string `json:"artifact_ref,omitempty"`
}

type DepartureRun struct {
	ID                   string             `json:"id"`
	ProjectID            string             `json:"project_id"`
	PolicyID             string             `json:"policy_id"`
	ScheduledWindow      string             `json:"scheduled_window"`
	State                DepartureState     `json:"state"`
	StateRevision        int                `json:"state_revision"`
	Candidate            DepartureCandidate `json:"candidate"`
	Gate                 DepartureGate      `json:"gate"`
	Promotion            DeparturePromotion `json:"promotion"`
	Release              DepartureRelease   `json:"release"`
	SkipReason           string             `json:"skip_reason"`
	BlockReason          string             `json:"block_reason"`
	ModelInvocationCount int                `json:"model_invocation_count"`
	CreatedAt            string             `json:"created_at"`
	UpdatedAt            string             `json:"updated_at"`
}

type DepartureRecoveryDisposition string

const (
	DepartureRecoveryTerminal  DepartureRecoveryDisposition = "terminal"
	DepartureRecoveryResumable DepartureRecoveryDisposition = "resumable"
	DepartureRecoveryBlocked   DepartureRecoveryDisposition = "blocked"
)

type DepartureRecovery struct {
	Run         DepartureRun                 `json:"run"`
	Disposition DepartureRecoveryDisposition `json:"disposition"`
	ResumeState DepartureState               `json:"resume_state,omitempty"`
	Reason      string                       `json:"reason,omitempty"`
}

func departureNow() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func departureTerminal(state DepartureState) bool {
	switch state {
	case DepartureStateSkipped, DepartureStateBlocked, DepartureStatePassed, DepartureStateFailed, DepartureStateRepairing:
		return true
	default:
		return false
	}
}

func departureReleaseFinal(release DepartureRelease) bool {
	return release.CompletedAt != "" && release.Status != ""
}

func (s *RuntimeStore) GetOrCreateDepartureRun(input DepartureRun) (DepartureRun, bool, error) {
	if strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.PolicyID) == "" || strings.TrimSpace(input.ScheduledWindow) == "" {
		return DepartureRun{}, false, fmt.Errorf("departure project, policy, and scheduled window are required")
	}
	if input.ID == "" {
		input.ID = "departure-" + strings.ToLower(newRecordID())
	}
	if input.State == "" {
		input.State = DepartureStateDue
	}
	if input.StateRevision == 0 {
		input.StateRevision = 1
	}
	if input.CreatedAt == "" {
		input.CreatedAt = departureNow()
	}
	if input.UpdatedAt == "" {
		input.UpdatedAt = input.CreatedAt
	}
	values, err := departureJSONValues(input)
	if err != nil {
		return DepartureRun{}, false, err
	}
	result, err := s.exec(`INSERT INTO departure_runs (
		id, project_id, policy_id, scheduled_window, state, state_revision,
		candidate_json, gate_json, promotion_json, release_json, skip_reason,
		block_reason, model_invocation_count, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(project_id, policy_id, scheduled_window) DO NOTHING`,
		input.ID, input.ProjectID, input.PolicyID, input.ScheduledWindow, input.State, input.StateRevision,
		values.candidate, values.gate, values.promotion, values.release, input.SkipReason,
		input.BlockReason, input.ModelInvocationCount, input.CreatedAt, input.UpdatedAt)
	if err != nil {
		return DepartureRun{}, false, err
	}
	if changed, _ := result.RowsAffected(); changed == 1 {
		return input, true, nil
	}
	existing, err := s.FindDepartureRunByWindow(input.ProjectID, input.PolicyID, input.ScheduledWindow)
	if err != nil {
		return DepartureRun{}, false, err
	}
	if existing == nil {
		return DepartureRun{}, false, errors.New("departure create conflict without durable record")
	}
	return *existing, false, nil
}

// CreateDepartureRun is a readable alias for callers that do not care whether
// a concurrent scheduler already created the same policy window.
func (s *RuntimeStore) CreateDepartureRun(input DepartureRun) (DepartureRun, bool, error) {
	return s.GetOrCreateDepartureRun(input)
}

func (s *RuntimeStore) FindDepartureRun(id string) (*DepartureRun, error) {
	return s.findDepartureRun(`WHERE id = ?`, id)
}

func (s *RuntimeStore) FindDepartureRunByWindow(projectID, policyID, scheduledWindow string) (*DepartureRun, error) {
	return s.findDepartureRun(`WHERE project_id = ? AND policy_id = ? AND scheduled_window = ?`, projectID, policyID, scheduledWindow)
}

func (s *RuntimeStore) findDepartureRun(where string, args ...any) (*DepartureRun, error) {
	var row DepartureRun
	var candidate, gate, promotion, release string
	err := s.queryRowScan(`SELECT id, project_id, policy_id, scheduled_window, state, state_revision,
		candidate_json, gate_json, promotion_json, release_json, skip_reason, block_reason,
		model_invocation_count, created_at, updated_at FROM departure_runs `+where, args,
		&row.ID, &row.ProjectID, &row.PolicyID, &row.ScheduledWindow, &row.State, &row.StateRevision,
		&candidate, &gate, &promotion, &release, &row.SkipReason, &row.BlockReason, &row.ModelInvocationCount, &row.CreatedAt, &row.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := decodeDepartureJSON(&row, candidate, gate, promotion, release); err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *RuntimeStore) ListDepartureRuns(projectID string) ([]DepartureRun, error) {
	query := `SELECT id, project_id, policy_id, scheduled_window, state, state_revision, candidate_json, gate_json, promotion_json, release_json, skip_reason, block_reason, model_invocation_count, created_at, updated_at FROM departure_runs`
	args := []any{}
	if projectID != "" {
		query += ` WHERE project_id = ?`
		args = append(args, projectID)
	}
	query += ` ORDER BY scheduled_window DESC, created_at DESC`
	rows, err := s.query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []DepartureRun
	for rows.Next() {
		run, err := scanDepartureRun(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, run)
	}
	return result, rows.Err()
}

// TransitionDepartureRun is an optimistic CAS. On false, the caller must
// reload the row; it must never overwrite the winner's terminal facts.
func (s *RuntimeStore) TransitionDepartureRun(next DepartureRun, expectedRevision int) (bool, error) {
	if next.ID == "" || expectedRevision <= 0 || next.State == "" {
		return false, fmt.Errorf("departure id, state, and expected revision are required")
	}
	values, err := departureJSONValues(next)
	if err != nil {
		return false, err
	}
	nextRevision := expectedRevision + 1
	now := departureNow()
	result, err := s.exec(`UPDATE departure_runs SET state = ?, state_revision = ?, candidate_json = ?, gate_json = ?, promotion_json = ?, release_json = ?, skip_reason = ?, block_reason = ?, model_invocation_count = ?, updated_at = ?
		WHERE id = ? AND state_revision = ?`, next.State, nextRevision, values.candidate, values.gate, values.promotion, values.release,
		next.SkipReason, next.BlockReason, next.ModelInvocationCount, now, next.ID, expectedRevision)
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return changed == 1, nil
}

// UpdateDepartureRunIfRevision is retained as the direct store-shaped name for
// integrations; it has exactly the same CAS semantics as TransitionDepartureRun.
func (s *RuntimeStore) UpdateDepartureRunIfRevision(next DepartureRun, expectedRevision int) (bool, error) {
	return s.TransitionDepartureRun(next, expectedRevision)
}

// ReconcileDepartureRuns is safe to call at startup. It only marks rows
// blocked where an irreversible external action may have happened but was not
// durably committed. All other nonterminal rows are returned as resumable.
func (s *RuntimeStore) ReconcileDepartureRuns(projectID string) ([]DepartureRecovery, error) {
	runs, err := s.ListDepartureRuns(projectID)
	if err != nil {
		return nil, err
	}
	result := make([]DepartureRecovery, 0, len(runs))
	for _, run := range runs {
		recovery := classifyDepartureRecovery(run)
		if recovery.Disposition == DepartureRecoveryBlocked && run.State != DepartureStateBlocked {
			next := run
			next.State = DepartureStateBlocked
			next.BlockReason = recovery.Reason
			updated, updateErr := s.TransitionDepartureRun(next, run.StateRevision)
			if updateErr != nil {
				return nil, updateErr
			}
			if !updated {
				// A concurrent winner owns the new truth. Reload it rather than
				// replacing its result with our stale classification.
				latest, readErr := s.FindDepartureRun(run.ID)
				if readErr != nil {
					return nil, readErr
				}
				if latest != nil {
					recovery = classifyDepartureRecovery(*latest)
					recovery.Run = *latest
				}
			} else {
				next.StateRevision++
				next.UpdatedAt = departureNow()
				recovery.Run = next
			}
		}
		result = append(result, recovery)
	}
	return result, nil
}

func classifyDepartureRecovery(run DepartureRun) DepartureRecovery {
	recovery := DepartureRecovery{Run: run}
	if departureTerminal(run.State) {
		recovery.Disposition = DepartureRecoveryTerminal
		return recovery
	}
	if run.Promotion.AttemptedAt != "" && (run.Promotion.CommittedRef == "" || run.Promotion.CommittedSHA == "") {
		recovery.Disposition, recovery.Reason = DepartureRecoveryBlocked, "promotion outcome is ambiguous after an attempted ref update"
		return recovery
	}
	if run.Release.AttemptedAt != "" && !departureReleaseFinal(run.Release) {
		recovery.Disposition, recovery.Reason = DepartureRecoveryBlocked, "release outcome is ambiguous after an attempted release"
		return recovery
	}
	if run.Promotion.CommittedRef != "" || run.Promotion.CommittedSHA != "" {
		if run.Promotion.CommittedRef == "" || run.Promotion.CommittedSHA == "" {
			recovery.Disposition, recovery.Reason = DepartureRecoveryBlocked, "promotion record has an incomplete committed ref"
			return recovery
		}
		if departureReleaseFinal(run.Release) {
			recovery.Disposition = DepartureRecoveryResumable
			if run.Release.Status == "failed" {
				recovery.ResumeState = DepartureStateFailed
			} else {
				recovery.ResumeState = DepartureStatePassed
			}
			return recovery
		}
		if run.Release.Profile != "" {
			recovery.Disposition, recovery.ResumeState = DepartureRecoveryResumable, DepartureStateReleasing
			return recovery
		}
		recovery.Disposition, recovery.ResumeState = DepartureRecoveryResumable, DepartureStatePassed
		return recovery
	}
	if run.State == DepartureStatePromoted || run.State == DepartureStateReleasing {
		recovery.Disposition, recovery.Reason = DepartureRecoveryBlocked, "departure state implies a ref move but no committed ref was recorded"
		return recovery
	}
	recovery.Disposition, recovery.ResumeState = DepartureRecoveryResumable, run.State
	return recovery
}

type departureJSON struct{ candidate, gate, promotion, release string }

func departureJSONValues(run DepartureRun) (departureJSON, error) {
	candidate, err := json.Marshal(run.Candidate)
	if err != nil {
		return departureJSON{}, err
	}
	gate, err := json.Marshal(run.Gate)
	if err != nil {
		return departureJSON{}, err
	}
	promotion, err := json.Marshal(run.Promotion)
	if err != nil {
		return departureJSON{}, err
	}
	release, err := json.Marshal(run.Release)
	if err != nil {
		return departureJSON{}, err
	}
	return departureJSON{string(candidate), string(gate), string(promotion), string(release)}, nil
}

type departureScanner interface{ Scan(...any) error }

func scanDepartureRun(scanner departureScanner) (DepartureRun, error) {
	var run DepartureRun
	var candidate, gate, promotion, release string
	err := scanner.Scan(&run.ID, &run.ProjectID, &run.PolicyID, &run.ScheduledWindow, &run.State, &run.StateRevision,
		&candidate, &gate, &promotion, &release, &run.SkipReason, &run.BlockReason, &run.ModelInvocationCount, &run.CreatedAt, &run.UpdatedAt)
	if err != nil {
		return DepartureRun{}, err
	}
	if err := decodeDepartureJSON(&run, candidate, gate, promotion, release); err != nil {
		return DepartureRun{}, err
	}
	return run, nil
}

func decodeDepartureJSON(run *DepartureRun, candidate, gate, promotion, release string) error {
	if err := json.Unmarshal([]byte(candidate), &run.Candidate); err != nil {
		return fmt.Errorf("decode departure %s candidate: %w", run.ID, err)
	}
	if err := json.Unmarshal([]byte(gate), &run.Gate); err != nil {
		return fmt.Errorf("decode departure %s gate: %w", run.ID, err)
	}
	if err := json.Unmarshal([]byte(promotion), &run.Promotion); err != nil {
		return fmt.Errorf("decode departure %s promotion: %w", run.ID, err)
	}
	if err := json.Unmarshal([]byte(release), &run.Release); err != nil {
		return fmt.Errorf("decode departure %s release: %w", run.ID, err)
	}
	return nil
}
