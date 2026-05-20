package main

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type RuntimeStore struct {
	db        *sql.DB
	stateRoot string
}

type ProjectHealth string

const (
	projectHealthHealthy  ProjectHealth = "healthy"
	projectHealthDegraded ProjectHealth = "degraded"
	projectHealthError    ProjectHealth = "error"
	projectHealthDisabled ProjectHealth = "disabled"
)

type RegisteredProject struct {
	ProjectID    string        `json:"project_id"`
	ProjectKey   string        `json:"project_key"`
	Name         string        `json:"name"`
	RepoRoot     string        `json:"repo_root"`
	VaultRoot    string        `json:"vault_root"`
	WorkflowPath string        `json:"workflow_path"`
	Enabled      bool          `json:"enabled"`
	Health       ProjectHealth `json:"health"`
	LastPollAt   string        `json:"last_poll_at"`
	LastError    string        `json:"last_error"`
}

type RunStatus struct {
	ProjectID       string `json:"project_id"`
	RecordID        string `json:"record_id"`
	ItemID          string `json:"item_id"`
	Runner          string `json:"runner"`
	Lane            string `json:"lane"`
	LeaseState      string `json:"lease_state"`
	AttemptOutcome  string `json:"attempt_outcome"`
	ActiveAttemptID string `json:"active_attempt_id"`
	WorkspacePath   string `json:"workspace_path"`
	SessionRef      string `json:"session_ref"`
	ProcessPID      int    `json:"process_pid"`
	PromptPath      string `json:"prompt_path"`
	EventSinkPath   string `json:"event_sink_path"`
	RawLogPath      string `json:"raw_log_path"`
	StatusPath      string `json:"status_path"`
	WorkRevision    int    `json:"work_revision"`
	AttemptCount    int    `json:"attempt_count"`
	NextRetryAt     string `json:"next_retry_at"`
	LastError       string `json:"last_error"`
	LastEventAt     string `json:"last_event_at"`
	StartedAt       string `json:"started_at"`
	UpdatedAt       string `json:"updated_at"`
}

type RunAttempt struct {
	AttemptID     string
	ProjectID     string
	RecordID      string
	ItemID        string
	Runner        string
	Lane          string
	WorkRevision  int
	WorkspacePath string
	SessionRef    string
	Outcome       string
	ExitCode      int
	PromptPath    string
	EventSinkPath string
	RawLogPath    string
	StatusPath    string
	ProcessPID    int
	LastError     string
	StartedAt     string
	FinishedAt    string
}

type RunTurn struct {
	AttemptID    string `json:"attempt_id"`
	ProjectID    string `json:"project_id"`
	RecordID     string `json:"record_id"`
	TurnID       string `json:"turn_id"`
	TurnIndex    int    `json:"turn_index"`
	SessionRef   string `json:"session_ref"`
	Status       string `json:"status"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	TotalTokens  int    `json:"total_tokens"`
	StartedAt    string `json:"started_at"`
	CompletedAt  string `json:"completed_at"`
	LastEventAt  string `json:"last_event_at"`
	LastError    string `json:"last_error"`
}

type SupervisorDecisionKind string

const (
	SupervisorDecisionContinueThread SupervisorDecisionKind = "continue_thread"
	SupervisorDecisionResumeSession  SupervisorDecisionKind = "resume_session"
	SupervisorDecisionForkThread     SupervisorDecisionKind = "fork_thread"
	SupervisorDecisionNewBranch      SupervisorDecisionKind = "new_branch"
	SupervisorDecisionNewRevision    SupervisorDecisionKind = "new_revision"
	SupervisorDecisionStopForAudit   SupervisorDecisionKind = "stop_for_audit"
	SupervisorDecisionStopForHuman   SupervisorDecisionKind = "stop_for_human"

	supervisorDecisionContinueThread = string(SupervisorDecisionContinueThread)
	supervisorDecisionResumeSession  = string(SupervisorDecisionResumeSession)
	supervisorDecisionForkThread     = string(SupervisorDecisionForkThread)
	supervisorDecisionNewBranch      = string(SupervisorDecisionNewBranch)
	supervisorDecisionNewRevision    = string(SupervisorDecisionNewRevision)
	supervisorDecisionStopForAudit   = string(SupervisorDecisionStopForAudit)
	supervisorDecisionStopForHuman   = string(SupervisorDecisionStopForHuman)
)

type RuntimeSupervisorDecision struct {
	DecisionID          string `json:"decision_id"`
	ProjectID           string `json:"project_id"`
	RecordID            string `json:"record_id"`
	ItemID              string `json:"item_id"`
	Runner              string `json:"runner"`
	WorkRevision        int    `json:"work_revision"`
	AttemptID           string `json:"attempt_id"`
	ParentAttemptID     string `json:"parent_attempt_id"`
	SessionRef          string `json:"session_ref"`
	ParentSessionRef    string `json:"parent_session_ref"`
	TargetAttemptID     string `json:"target_attempt_id"`
	TargetSessionRef    string `json:"target_session_ref"`
	Kind                string `json:"kind"`
	Reason              string `json:"reason"`
	BranchName          string `json:"branch_name"`
	WorkspacePath       string `json:"workspace_path"`
	ValidationDelta     string `json:"validation_delta"`
	MergeRule           string `json:"merge_rule"`
	LeaseState          string `json:"lease_state"`
	ContextSignal       string `json:"context_signal"`
	InputTokens         int    `json:"input_tokens"`
	OutputTokens        int    `json:"output_tokens"`
	TotalTokens         int    `json:"total_tokens"`
	ContextWindowTokens int    `json:"context_window_tokens"`
	CreatedAt           string `json:"created_at"`
}

type SupervisorDecision = RuntimeSupervisorDecision

type RunnerSession struct {
	ProjectID      string `json:"project_id"`
	RecordID       string `json:"record_id"`
	Runner         string `json:"runner"`
	SessionRef     string `json:"session_ref"`
	LastMessageRef string `json:"last_message_ref"`
	WorkspacePath  string `json:"workspace_path"`
	CurrentItemID  string `json:"current_item_id"`
	WorkRevision   int    `json:"work_revision"`
	LastAttemptID  string `json:"last_attempt_id"`
	State          string `json:"state"`
	Resumable      bool   `json:"resumable"`
	StartedAt      string `json:"started_at"`
	LastSeenAt     string `json:"last_seen_at"`
	EndedAt        string `json:"ended_at"`
	LastError      string `json:"last_error"`
}

func OpenRuntimeStore(stateRoot string) (*RuntimeStore, error) {
	if err := ensureDir(stateRoot); err != nil {
		return nil, err
	}
	dbPath := filepath.Join(stateRoot, "daemon.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	store := &RuntimeStore{db: db, stateRoot: stateRoot}
	if err := store.Migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *RuntimeStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *RuntimeStore) Migrate() error {
	statements := []string{
		`PRAGMA journal_mode = WAL;`,
		`CREATE TABLE IF NOT EXISTS projects (
			project_id TEXT PRIMARY KEY,
			project_key TEXT NOT NULL,
			name TEXT NOT NULL,
			repo_root TEXT NOT NULL,
			vault_root TEXT NOT NULL,
			workflow_path TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			health TEXT NOT NULL DEFAULT 'healthy',
			last_poll_at TEXT NOT NULL DEFAULT '',
			last_error TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE TABLE IF NOT EXISTS runs (
			project_id TEXT NOT NULL,
			record_id TEXT NOT NULL,
			item_id TEXT NOT NULL DEFAULT '',
			runner TEXT NOT NULL DEFAULT '',
			lane TEXT NOT NULL DEFAULT '',
			lease_state TEXT NOT NULL DEFAULT 'unclaimed',
			attempt_outcome TEXT NOT NULL DEFAULT 'none',
			active_attempt_id TEXT NOT NULL DEFAULT '',
			workspace_path TEXT NOT NULL DEFAULT '',
			session_ref TEXT NOT NULL DEFAULT '',
			process_pid INTEGER NOT NULL DEFAULT 0,
			prompt_path TEXT NOT NULL DEFAULT '',
			event_sink_path TEXT NOT NULL DEFAULT '',
			raw_log_path TEXT NOT NULL DEFAULT '',
			status_path TEXT NOT NULL DEFAULT '',
			work_revision INTEGER NOT NULL DEFAULT 0,
			attempt_count INTEGER NOT NULL DEFAULT 0,
			next_retry_at TEXT NOT NULL DEFAULT '',
			last_error TEXT NOT NULL DEFAULT '',
			last_event_at TEXT NOT NULL DEFAULT '',
			started_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT '',
			PRIMARY KEY(project_id, record_id)
		);`,
		`CREATE TABLE IF NOT EXISTS attempts (
			attempt_id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			record_id TEXT NOT NULL,
			item_id TEXT NOT NULL DEFAULT '',
			runner TEXT NOT NULL DEFAULT '',
			lane TEXT NOT NULL DEFAULT '',
			work_revision INTEGER NOT NULL DEFAULT 0,
			workspace_path TEXT NOT NULL DEFAULT '',
			session_ref TEXT NOT NULL DEFAULT '',
			process_pid INTEGER NOT NULL DEFAULT 0,
			outcome TEXT NOT NULL DEFAULT 'none',
			exit_code INTEGER NOT NULL DEFAULT 0,
			prompt_path TEXT NOT NULL DEFAULT '',
			event_sink_path TEXT NOT NULL DEFAULT '',
			raw_log_path TEXT NOT NULL DEFAULT '',
			status_path TEXT NOT NULL DEFAULT '',
			last_error TEXT NOT NULL DEFAULT '',
			started_at TEXT NOT NULL DEFAULT '',
			finished_at TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE TABLE IF NOT EXISTS turns (
			attempt_id TEXT NOT NULL,
			project_id TEXT NOT NULL,
			record_id TEXT NOT NULL,
			turn_id TEXT NOT NULL,
			turn_index INTEGER NOT NULL DEFAULT 0,
			session_ref TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT '',
			input_tokens INTEGER NOT NULL DEFAULT 0,
			output_tokens INTEGER NOT NULL DEFAULT 0,
			total_tokens INTEGER NOT NULL DEFAULT 0,
			started_at TEXT NOT NULL DEFAULT '',
			completed_at TEXT NOT NULL DEFAULT '',
			last_event_at TEXT NOT NULL DEFAULT '',
			last_error TEXT NOT NULL DEFAULT '',
			PRIMARY KEY(attempt_id, turn_id)
		);`,
		`CREATE TABLE IF NOT EXISTS supervisor_decisions (
			decision_id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			record_id TEXT NOT NULL,
			item_id TEXT NOT NULL DEFAULT '',
			runner TEXT NOT NULL DEFAULT '',
			work_revision INTEGER NOT NULL DEFAULT 0,
			attempt_id TEXT NOT NULL DEFAULT '',
			parent_attempt_id TEXT NOT NULL DEFAULT '',
			session_ref TEXT NOT NULL DEFAULT '',
			parent_session_ref TEXT NOT NULL DEFAULT '',
			target_attempt_id TEXT NOT NULL DEFAULT '',
			target_session_ref TEXT NOT NULL DEFAULT '',
			kind TEXT NOT NULL,
			reason TEXT NOT NULL DEFAULT '',
			branch_name TEXT NOT NULL DEFAULT '',
			workspace_path TEXT NOT NULL DEFAULT '',
			validation_delta TEXT NOT NULL DEFAULT '',
			merge_rule TEXT NOT NULL DEFAULT '',
			lease_state TEXT NOT NULL DEFAULT '',
			context_signal TEXT NOT NULL DEFAULT '',
			input_tokens INTEGER NOT NULL DEFAULT 0,
			output_tokens INTEGER NOT NULL DEFAULT 0,
			total_tokens INTEGER NOT NULL DEFAULT 0,
			context_window_tokens INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE TABLE IF NOT EXISTS sessions (
			project_id TEXT NOT NULL,
			record_id TEXT NOT NULL,
			runner TEXT NOT NULL,
			session_ref TEXT NOT NULL,
			last_message_ref TEXT NOT NULL DEFAULT '',
			workspace_path TEXT NOT NULL DEFAULT '',
			current_item_id TEXT NOT NULL DEFAULT '',
			work_revision INTEGER NOT NULL DEFAULT 0,
			last_attempt_id TEXT NOT NULL DEFAULT '',
			state TEXT NOT NULL DEFAULT 'open',
			resumable INTEGER NOT NULL DEFAULT 1,
			started_at TEXT NOT NULL DEFAULT '',
			last_seen_at TEXT NOT NULL DEFAULT '',
			ended_at TEXT NOT NULL DEFAULT '',
			last_error TEXT NOT NULL DEFAULT '',
			PRIMARY KEY(project_id, session_ref)
		);`,
		`CREATE TABLE IF NOT EXISTS daemon_settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL DEFAULT ''
		);`,
	}
	for _, stmt := range statements {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}
	if err := s.ensureColumn("runs", "work_revision", `ALTER TABLE runs ADD COLUMN work_revision INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	if err := s.ensureColumn("runs", "lane", `ALTER TABLE runs ADD COLUMN lane TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := s.ensureColumn("runs", "active_attempt_id", `ALTER TABLE runs ADD COLUMN active_attempt_id TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := s.ensureColumn("runs", "process_pid", `ALTER TABLE runs ADD COLUMN process_pid INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	if err := s.ensureColumn("runs", "prompt_path", `ALTER TABLE runs ADD COLUMN prompt_path TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := s.ensureColumn("runs", "event_sink_path", `ALTER TABLE runs ADD COLUMN event_sink_path TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := s.ensureColumn("runs", "raw_log_path", `ALTER TABLE runs ADD COLUMN raw_log_path TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := s.ensureColumn("runs", "status_path", `ALTER TABLE runs ADD COLUMN status_path TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := s.ensureColumn("runs", "attempt_count", `ALTER TABLE runs ADD COLUMN attempt_count INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	if err := s.ensureColumn("runs", "next_retry_at", `ALTER TABLE runs ADD COLUMN next_retry_at TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := s.ensureColumn("runs", "last_error", `ALTER TABLE runs ADD COLUMN last_error TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := s.ensureColumn("runs", "last_event_at", `ALTER TABLE runs ADD COLUMN last_event_at TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := s.ensureColumn("attempts", "process_pid", `ALTER TABLE attempts ADD COLUMN process_pid INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	if err := s.ensureColumn("attempts", "lane", `ALTER TABLE attempts ADD COLUMN lane TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := s.ensureColumn("attempts", "status_path", `ALTER TABLE attempts ADD COLUMN status_path TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := s.ensureColumn("sessions", "last_message_ref", `ALTER TABLE sessions ADD COLUMN last_message_ref TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	for _, column := range []struct {
		name string
		stmt string
	}{
		{"turn_index", `ALTER TABLE turns ADD COLUMN turn_index INTEGER NOT NULL DEFAULT 0`},
		{"session_ref", `ALTER TABLE turns ADD COLUMN session_ref TEXT NOT NULL DEFAULT ''`},
		{"status", `ALTER TABLE turns ADD COLUMN status TEXT NOT NULL DEFAULT ''`},
		{"input_tokens", `ALTER TABLE turns ADD COLUMN input_tokens INTEGER NOT NULL DEFAULT 0`},
		{"output_tokens", `ALTER TABLE turns ADD COLUMN output_tokens INTEGER NOT NULL DEFAULT 0`},
		{"total_tokens", `ALTER TABLE turns ADD COLUMN total_tokens INTEGER NOT NULL DEFAULT 0`},
		{"started_at", `ALTER TABLE turns ADD COLUMN started_at TEXT NOT NULL DEFAULT ''`},
		{"completed_at", `ALTER TABLE turns ADD COLUMN completed_at TEXT NOT NULL DEFAULT ''`},
		{"last_event_at", `ALTER TABLE turns ADD COLUMN last_event_at TEXT NOT NULL DEFAULT ''`},
		{"last_error", `ALTER TABLE turns ADD COLUMN last_error TEXT NOT NULL DEFAULT ''`},
	} {
		if err := s.ensureColumn("turns", column.name, column.stmt); err != nil {
			return err
		}
	}
	for _, column := range []struct {
		name string
		stmt string
	}{
		{"item_id", `ALTER TABLE supervisor_decisions ADD COLUMN item_id TEXT NOT NULL DEFAULT ''`},
		{"runner", `ALTER TABLE supervisor_decisions ADD COLUMN runner TEXT NOT NULL DEFAULT ''`},
		{"work_revision", `ALTER TABLE supervisor_decisions ADD COLUMN work_revision INTEGER NOT NULL DEFAULT 0`},
		{"attempt_id", `ALTER TABLE supervisor_decisions ADD COLUMN attempt_id TEXT NOT NULL DEFAULT ''`},
		{"parent_attempt_id", `ALTER TABLE supervisor_decisions ADD COLUMN parent_attempt_id TEXT NOT NULL DEFAULT ''`},
		{"session_ref", `ALTER TABLE supervisor_decisions ADD COLUMN session_ref TEXT NOT NULL DEFAULT ''`},
		{"parent_session_ref", `ALTER TABLE supervisor_decisions ADD COLUMN parent_session_ref TEXT NOT NULL DEFAULT ''`},
		{"target_attempt_id", `ALTER TABLE supervisor_decisions ADD COLUMN target_attempt_id TEXT NOT NULL DEFAULT ''`},
		{"target_session_ref", `ALTER TABLE supervisor_decisions ADD COLUMN target_session_ref TEXT NOT NULL DEFAULT ''`},
		{"kind", `ALTER TABLE supervisor_decisions ADD COLUMN kind TEXT NOT NULL DEFAULT ''`},
		{"reason", `ALTER TABLE supervisor_decisions ADD COLUMN reason TEXT NOT NULL DEFAULT ''`},
		{"branch_name", `ALTER TABLE supervisor_decisions ADD COLUMN branch_name TEXT NOT NULL DEFAULT ''`},
		{"workspace_path", `ALTER TABLE supervisor_decisions ADD COLUMN workspace_path TEXT NOT NULL DEFAULT ''`},
		{"validation_delta", `ALTER TABLE supervisor_decisions ADD COLUMN validation_delta TEXT NOT NULL DEFAULT ''`},
		{"merge_rule", `ALTER TABLE supervisor_decisions ADD COLUMN merge_rule TEXT NOT NULL DEFAULT ''`},
		{"lease_state", `ALTER TABLE supervisor_decisions ADD COLUMN lease_state TEXT NOT NULL DEFAULT ''`},
		{"context_signal", `ALTER TABLE supervisor_decisions ADD COLUMN context_signal TEXT NOT NULL DEFAULT ''`},
		{"input_tokens", `ALTER TABLE supervisor_decisions ADD COLUMN input_tokens INTEGER NOT NULL DEFAULT 0`},
		{"output_tokens", `ALTER TABLE supervisor_decisions ADD COLUMN output_tokens INTEGER NOT NULL DEFAULT 0`},
		{"total_tokens", `ALTER TABLE supervisor_decisions ADD COLUMN total_tokens INTEGER NOT NULL DEFAULT 0`},
		{"context_window_tokens", `ALTER TABLE supervisor_decisions ADD COLUMN context_window_tokens INTEGER NOT NULL DEFAULT 0`},
		{"created_at", `ALTER TABLE supervisor_decisions ADD COLUMN created_at TEXT NOT NULL DEFAULT ''`},
	} {
		if err := s.ensureColumn("supervisor_decisions", column.name, column.stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *RuntimeStore) ensureColumn(tableName, columnName, stmt string) error {
	rows, err := s.db.Query(`PRAGMA table_info(` + tableName + `)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, kind string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if name == columnName {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = s.db.Exec(stmt)
	return err
}

func (s *RuntimeStore) UpsertProject(project RegisteredProject) error {
	if project.Health == "" {
		project.Health = projectHealthHealthy
	}
	_, err := s.db.Exec(`INSERT INTO projects (
		project_id, project_key, name, repo_root, vault_root, workflow_path, enabled, health, last_poll_at, last_error
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(project_id) DO UPDATE SET
		project_key=excluded.project_key,
		name=excluded.name,
		repo_root=excluded.repo_root,
		vault_root=excluded.vault_root,
		workflow_path=excluded.workflow_path,
		enabled=excluded.enabled,
		health=excluded.health,
		last_poll_at=excluded.last_poll_at,
		last_error=excluded.last_error`,
		project.ProjectID, project.ProjectKey, project.Name, project.RepoRoot, project.VaultRoot, project.WorkflowPath,
		boolToInt(project.Enabled), string(project.Health), project.LastPollAt, project.LastError)
	return err
}

func (s *RuntimeStore) RemoveProject(projectID string) error {
	if _, err := s.db.Exec(`DELETE FROM turns WHERE project_id = ?`, projectID); err != nil {
		return err
	}
	if _, err := s.db.Exec(`DELETE FROM supervisor_decisions WHERE project_id = ?`, projectID); err != nil {
		return err
	}
	if _, err := s.db.Exec(`DELETE FROM attempts WHERE project_id = ?`, projectID); err != nil {
		return err
	}
	if _, err := s.db.Exec(`DELETE FROM sessions WHERE project_id = ?`, projectID); err != nil {
		return err
	}
	if _, err := s.db.Exec(`DELETE FROM runs WHERE project_id = ?`, projectID); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM projects WHERE project_id = ?`, projectID)
	return err
}

func (s *RuntimeStore) SetProjectEnabled(projectID string, enabled bool) error {
	health := projectHealthDisabled
	if enabled {
		health = projectHealthHealthy
	}
	result, err := s.db.Exec(`UPDATE projects
		SET enabled = ?, health = ?, last_error = CASE WHEN ? = 1 THEN last_error ELSE '' END
		WHERE project_id = ?`,
		boolToInt(enabled), string(health), boolToInt(enabled), projectID)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return tuskerError(errorNotFound, "project not found: "+projectID)
	}
	return nil
}

func (s *RuntimeStore) CountProjectActiveRuns(projectID string) (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*)
		FROM runs
		WHERE project_id = ? AND lease_state IN ('claimed', 'running', 'retry_queued')`, projectID).Scan(&count)
	return count, err
}

func (s *RuntimeStore) ListProjects() ([]RegisteredProject, error) {
	rows, err := s.db.Query(`SELECT project_id, project_key, name, repo_root, vault_root, workflow_path, enabled, health, last_poll_at, last_error FROM projects ORDER BY name, repo_root`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RegisteredProject
	for rows.Next() {
		var project RegisteredProject
		var enabled int
		var health string
		if err := rows.Scan(&project.ProjectID, &project.ProjectKey, &project.Name, &project.RepoRoot, &project.VaultRoot, &project.WorkflowPath, &enabled, &health, &project.LastPollAt, &project.LastError); err != nil {
			return nil, err
		}
		project.Enabled = enabled != 0
		project.Health = ProjectHealth(health)
		out = append(out, project)
	}
	return out, rows.Err()
}

func (s *RuntimeStore) ListRuns() ([]RunStatus, error) {
	rows, err := s.db.Query(`SELECT project_id, record_id, item_id, runner, lane, lease_state, attempt_outcome, active_attempt_id, workspace_path, session_ref, process_pid, prompt_path, event_sink_path, raw_log_path, status_path, work_revision, attempt_count, next_retry_at, last_error, last_event_at, started_at, updated_at FROM runs ORDER BY updated_at DESC, project_id, item_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RunStatus
	for rows.Next() {
		var run RunStatus
		if err := rows.Scan(&run.ProjectID, &run.RecordID, &run.ItemID, &run.Runner, &run.Lane, &run.LeaseState, &run.AttemptOutcome, &run.ActiveAttemptID, &run.WorkspacePath, &run.SessionRef, &run.ProcessPID, &run.PromptPath, &run.EventSinkPath, &run.RawLogPath, &run.StatusPath, &run.WorkRevision, &run.AttemptCount, &run.NextRetryAt, &run.LastError, &run.LastEventAt, &run.StartedAt, &run.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

func (s *RuntimeStore) UpsertRun(run RunStatus) error {
	if run.LeaseState == "" {
		run.LeaseState = string(LeaseStateUnclaimed)
	}
	if run.AttemptOutcome == "" {
		run.AttemptOutcome = string(AttemptOutcomeNone)
	}
	if strings.TrimSpace(run.UpdatedAt) == "" {
		run.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	_, err := s.db.Exec(`INSERT INTO runs (
		project_id, record_id, item_id, runner, lane, lease_state, attempt_outcome, active_attempt_id, workspace_path, session_ref, process_pid, prompt_path, event_sink_path, raw_log_path, status_path, work_revision, attempt_count, next_retry_at, last_error, last_event_at, started_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(project_id, record_id) DO UPDATE SET
		item_id=excluded.item_id,
		runner=excluded.runner,
		lane=excluded.lane,
		lease_state=excluded.lease_state,
		attempt_outcome=excluded.attempt_outcome,
		active_attempt_id=excluded.active_attempt_id,
		workspace_path=excluded.workspace_path,
		session_ref=excluded.session_ref,
		process_pid=excluded.process_pid,
		prompt_path=excluded.prompt_path,
		event_sink_path=excluded.event_sink_path,
		raw_log_path=excluded.raw_log_path,
		status_path=excluded.status_path,
		work_revision=excluded.work_revision,
		attempt_count=excluded.attempt_count,
		next_retry_at=excluded.next_retry_at,
		last_error=excluded.last_error,
		last_event_at=excluded.last_event_at,
		started_at=excluded.started_at,
		updated_at=excluded.updated_at`,
		run.ProjectID, run.RecordID, run.ItemID, run.Runner, run.Lane, run.LeaseState, run.AttemptOutcome, run.ActiveAttemptID, run.WorkspacePath, run.SessionRef, run.ProcessPID, run.PromptPath, run.EventSinkPath, run.RawLogPath, run.StatusPath, run.WorkRevision, run.AttemptCount, run.NextRetryAt, run.LastError, run.LastEventAt, run.StartedAt, run.UpdatedAt)
	return err
}

func (s *RuntimeStore) SaveAttempt(attempt RunAttempt) error {
	_, err := s.db.Exec(`INSERT INTO attempts (
		attempt_id, project_id, record_id, item_id, runner, lane, work_revision, workspace_path, session_ref, process_pid, outcome, exit_code, prompt_path, event_sink_path, raw_log_path, status_path, last_error, started_at, finished_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(attempt_id) DO UPDATE SET
		project_id=excluded.project_id,
		record_id=excluded.record_id,
		item_id=excluded.item_id,
		runner=excluded.runner,
		lane=excluded.lane,
		work_revision=excluded.work_revision,
		workspace_path=excluded.workspace_path,
		session_ref=excluded.session_ref,
		process_pid=excluded.process_pid,
		outcome=excluded.outcome,
		exit_code=excluded.exit_code,
		prompt_path=excluded.prompt_path,
		event_sink_path=excluded.event_sink_path,
		raw_log_path=excluded.raw_log_path,
		status_path=excluded.status_path,
		last_error=excluded.last_error,
		started_at=excluded.started_at,
		finished_at=excluded.finished_at`,
		attempt.AttemptID, attempt.ProjectID, attempt.RecordID, attempt.ItemID, attempt.Runner, attempt.Lane, attempt.WorkRevision, attempt.WorkspacePath, attempt.SessionRef, attempt.ProcessPID, attempt.Outcome, attempt.ExitCode, attempt.PromptPath, attempt.EventSinkPath, attempt.RawLogPath, attempt.StatusPath, attempt.LastError, attempt.StartedAt, attempt.FinishedAt)
	return err
}

func (s *RuntimeStore) SaveTurn(turn RunTurn) error {
	if strings.TrimSpace(turn.TurnID) == "" {
		return tuskerError(errorInvalidArg, "turn_id is required")
	}
	if strings.TrimSpace(turn.AttemptID) == "" {
		return tuskerError(errorInvalidArg, "attempt_id is required")
	}
	if strings.TrimSpace(turn.LastEventAt) == "" {
		turn.LastEventAt = time.Now().UTC().Format(time.RFC3339)
	}
	_, err := s.db.Exec(`INSERT INTO turns (
		attempt_id, project_id, record_id, turn_id, turn_index, session_ref, status, input_tokens, output_tokens, total_tokens, started_at, completed_at, last_event_at, last_error
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(attempt_id, turn_id) DO UPDATE SET
		project_id=excluded.project_id,
		record_id=excluded.record_id,
		turn_index=excluded.turn_index,
		session_ref=CASE WHEN excluded.session_ref != '' THEN excluded.session_ref ELSE turns.session_ref END,
		status=CASE
			WHEN turns.status IN ('completed', 'failed', 'interrupted') AND excluded.status = 'running' THEN turns.status
			WHEN excluded.status != '' THEN excluded.status
			ELSE turns.status
		END,
		input_tokens=CASE WHEN excluded.input_tokens > 0 THEN excluded.input_tokens ELSE turns.input_tokens END,
		output_tokens=CASE WHEN excluded.output_tokens > 0 THEN excluded.output_tokens ELSE turns.output_tokens END,
		total_tokens=CASE WHEN excluded.total_tokens > 0 THEN excluded.total_tokens ELSE turns.total_tokens END,
		started_at=CASE WHEN excluded.started_at != '' THEN excluded.started_at ELSE turns.started_at END,
		completed_at=CASE WHEN excluded.completed_at != '' THEN excluded.completed_at ELSE turns.completed_at END,
		last_event_at=CASE
			WHEN turns.status IN ('completed', 'failed', 'interrupted') AND excluded.status = 'running' THEN turns.last_event_at
			WHEN excluded.last_event_at != '' THEN excluded.last_event_at
			ELSE turns.last_event_at
		END,
		last_error=CASE WHEN excluded.last_error != '' THEN excluded.last_error ELSE turns.last_error END`,
		turn.AttemptID, turn.ProjectID, turn.RecordID, turn.TurnID, turn.TurnIndex, turn.SessionRef, turn.Status, turn.InputTokens, turn.OutputTokens, turn.TotalTokens, turn.StartedAt, turn.CompletedAt, turn.LastEventAt, turn.LastError)
	return err
}

func (s *RuntimeStore) NextTurnIndex(projectID, recordID, attemptID string) (int, error) {
	var index int
	err := s.db.QueryRow(`SELECT COALESCE(MAX(turn_index) + 1, 0)
		FROM turns
		WHERE project_id = ? AND record_id = ? AND attempt_id = ?`, projectID, recordID, attemptID).Scan(&index)
	return index, err
}

func (s *RuntimeStore) ListTurnsForRun(projectID, recordID string) ([]RunTurn, error) {
	rows, err := s.db.Query(`SELECT attempt_id, project_id, record_id, turn_id, turn_index, session_ref, status, input_tokens, output_tokens, total_tokens, started_at, completed_at, last_event_at, last_error
		FROM turns
		WHERE project_id = ? AND record_id = ?
		ORDER BY turn_index ASC, started_at ASC, last_event_at ASC, turn_id ASC`, projectID, recordID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RunTurn
	for rows.Next() {
		var turn RunTurn
		if err := rows.Scan(&turn.AttemptID, &turn.ProjectID, &turn.RecordID, &turn.TurnID, &turn.TurnIndex, &turn.SessionRef, &turn.Status, &turn.InputTokens, &turn.OutputTokens, &turn.TotalTokens, &turn.StartedAt, &turn.CompletedAt, &turn.LastEventAt, &turn.LastError); err != nil {
			return nil, err
		}
		out = append(out, turn)
	}
	return out, rows.Err()
}

func (s *RuntimeStore) ListTurnsForAttempt(attemptID string) ([]RunTurn, error) {
	rows, err := s.db.Query(`SELECT attempt_id, project_id, record_id, turn_id, turn_index, session_ref, status, input_tokens, output_tokens, total_tokens, started_at, completed_at, last_event_at, last_error
		FROM turns
		WHERE attempt_id = ?
		ORDER BY turn_index ASC, started_at ASC, last_event_at ASC, turn_id ASC`, attemptID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RunTurn
	for rows.Next() {
		var turn RunTurn
		if err := rows.Scan(&turn.AttemptID, &turn.ProjectID, &turn.RecordID, &turn.TurnID, &turn.TurnIndex, &turn.SessionRef, &turn.Status, &turn.InputTokens, &turn.OutputTokens, &turn.TotalTokens, &turn.StartedAt, &turn.CompletedAt, &turn.LastEventAt, &turn.LastError); err != nil {
			return nil, err
		}
		out = append(out, turn)
	}
	return out, rows.Err()
}

func (s *RuntimeStore) SaveRuntimeSupervisorDecision(decision RuntimeSupervisorDecision) (RuntimeSupervisorDecision, error) {
	if strings.TrimSpace(decision.ProjectID) == "" {
		return decision, tuskerError(errorInvalidArg, "project_id is required")
	}
	if strings.TrimSpace(decision.RecordID) == "" {
		return decision, tuskerError(errorInvalidArg, "record_id is required")
	}
	if strings.TrimSpace(decision.Kind) == "" {
		return decision, tuskerError(errorInvalidArg, "supervisor decision kind is required")
	}
	if strings.TrimSpace(decision.DecisionID) == "" {
		decision.DecisionID = newRecordID()
	}
	if strings.TrimSpace(decision.CreatedAt) == "" {
		decision.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if decision.TargetAttemptID == "" {
		decision.TargetAttemptID = decision.AttemptID
	}
	if decision.AttemptID == "" {
		decision.AttemptID = decision.TargetAttemptID
	}
	if decision.TargetSessionRef == "" {
		decision.TargetSessionRef = decision.SessionRef
	}
	if decision.SessionRef == "" {
		decision.SessionRef = decision.TargetSessionRef
	}
	_, err := s.db.Exec(`INSERT INTO supervisor_decisions (
		decision_id, project_id, record_id, item_id, runner, work_revision, attempt_id, parent_attempt_id, session_ref, parent_session_ref, target_attempt_id, target_session_ref, kind, reason, branch_name, workspace_path, validation_delta, merge_rule, lease_state, context_signal, input_tokens, output_tokens, total_tokens, context_window_tokens, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(decision_id) DO UPDATE SET
		project_id=excluded.project_id,
		record_id=excluded.record_id,
		item_id=excluded.item_id,
		runner=excluded.runner,
		work_revision=excluded.work_revision,
		attempt_id=excluded.attempt_id,
		parent_attempt_id=excluded.parent_attempt_id,
		session_ref=excluded.session_ref,
		parent_session_ref=excluded.parent_session_ref,
		target_attempt_id=excluded.target_attempt_id,
		target_session_ref=excluded.target_session_ref,
		kind=excluded.kind,
		reason=excluded.reason,
		branch_name=excluded.branch_name,
		workspace_path=excluded.workspace_path,
		validation_delta=excluded.validation_delta,
		merge_rule=excluded.merge_rule,
		lease_state=excluded.lease_state,
		context_signal=excluded.context_signal,
		input_tokens=excluded.input_tokens,
		output_tokens=excluded.output_tokens,
		total_tokens=excluded.total_tokens,
		context_window_tokens=excluded.context_window_tokens,
		created_at=excluded.created_at`,
		decision.DecisionID, decision.ProjectID, decision.RecordID, decision.ItemID, decision.Runner, decision.WorkRevision, decision.AttemptID, decision.ParentAttemptID, decision.SessionRef, decision.ParentSessionRef, decision.TargetAttemptID, decision.TargetSessionRef, decision.Kind, decision.Reason, decision.BranchName, decision.WorkspacePath, decision.ValidationDelta, decision.MergeRule, decision.LeaseState, decision.ContextSignal, decision.InputTokens, decision.OutputTokens, decision.TotalTokens, decision.ContextWindowTokens, decision.CreatedAt)
	return decision, err
}

func (s *RuntimeStore) SaveSupervisorDecision(decision SupervisorDecision) (SupervisorDecision, error) {
	return s.SaveRuntimeSupervisorDecision(decision)
}

func (s *RuntimeStore) ListRuntimeSupervisorDecisionsForRun(projectID, recordID string) ([]RuntimeSupervisorDecision, error) {
	rows, err := s.db.Query(`SELECT decision_id, project_id, record_id, item_id, runner, work_revision, attempt_id, parent_attempt_id, session_ref, parent_session_ref, target_attempt_id, target_session_ref, kind, reason, branch_name, workspace_path, validation_delta, merge_rule, lease_state, context_signal, input_tokens, output_tokens, total_tokens, context_window_tokens, created_at
		FROM supervisor_decisions
		WHERE project_id = ? AND record_id = ?
		ORDER BY created_at ASC, decision_id ASC`, projectID, recordID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRuntimeSupervisorDecisions(rows)
}

func (s *RuntimeStore) ListSupervisorDecisionsForRun(projectID, recordID string) ([]SupervisorDecision, error) {
	return s.ListRuntimeSupervisorDecisionsForRun(projectID, recordID)
}

func (s *RuntimeStore) ListRuntimeSupervisorDecisionsForAttempt(attemptID string) ([]RuntimeSupervisorDecision, error) {
	rows, err := s.db.Query(`SELECT decision_id, project_id, record_id, item_id, runner, work_revision, attempt_id, parent_attempt_id, session_ref, parent_session_ref, target_attempt_id, target_session_ref, kind, reason, branch_name, workspace_path, validation_delta, merge_rule, lease_state, context_signal, input_tokens, output_tokens, total_tokens, context_window_tokens, created_at
		FROM supervisor_decisions
		WHERE attempt_id = ? OR target_attempt_id = ?
		ORDER BY created_at ASC, decision_id ASC`, attemptID, attemptID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRuntimeSupervisorDecisions(rows)
}

func scanRuntimeSupervisorDecisions(rows *sql.Rows) ([]RuntimeSupervisorDecision, error) {
	var out []RuntimeSupervisorDecision
	for rows.Next() {
		var decision RuntimeSupervisorDecision
		if err := rows.Scan(&decision.DecisionID, &decision.ProjectID, &decision.RecordID, &decision.ItemID, &decision.Runner, &decision.WorkRevision, &decision.AttemptID, &decision.ParentAttemptID, &decision.SessionRef, &decision.ParentSessionRef, &decision.TargetAttemptID, &decision.TargetSessionRef, &decision.Kind, &decision.Reason, &decision.BranchName, &decision.WorkspacePath, &decision.ValidationDelta, &decision.MergeRule, &decision.LeaseState, &decision.ContextSignal, &decision.InputTokens, &decision.OutputTokens, &decision.TotalTokens, &decision.ContextWindowTokens, &decision.CreatedAt); err != nil {
			return nil, err
		}
		if decision.AttemptID == "" {
			decision.AttemptID = decision.TargetAttemptID
		}
		if decision.SessionRef == "" {
			decision.SessionRef = decision.TargetSessionRef
		}
		out = append(out, decision)
	}
	return out, rows.Err()
}

func (s *RuntimeStore) SaveSession(session RunnerSession) error {
	_, err := s.db.Exec(`INSERT INTO sessions (
		project_id, record_id, runner, session_ref, last_message_ref, workspace_path, current_item_id, work_revision, last_attempt_id, state, resumable, started_at, last_seen_at, ended_at, last_error
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(project_id, session_ref) DO UPDATE SET
		record_id=excluded.record_id,
		runner=excluded.runner,
		last_message_ref=excluded.last_message_ref,
		workspace_path=excluded.workspace_path,
		current_item_id=excluded.current_item_id,
		work_revision=excluded.work_revision,
		last_attempt_id=excluded.last_attempt_id,
		state=excluded.state,
		resumable=excluded.resumable,
		started_at=excluded.started_at,
		last_seen_at=excluded.last_seen_at,
		ended_at=excluded.ended_at,
		last_error=excluded.last_error`,
		session.ProjectID, session.RecordID, session.Runner, session.SessionRef, session.LastMessageRef, session.WorkspacePath, session.CurrentItemID, session.WorkRevision, session.LastAttemptID, session.State, boolToInt(session.Resumable), session.StartedAt, session.LastSeenAt, session.EndedAt, session.LastError)
	return err
}

func (s *RuntimeStore) LatestSession(projectID, recordID, runner string) (*RunnerSession, error) {
	row := s.db.QueryRow(`SELECT project_id, record_id, runner, session_ref, last_message_ref, workspace_path, current_item_id, work_revision, last_attempt_id, state, resumable, started_at, last_seen_at, ended_at, last_error
		FROM sessions
		WHERE project_id = ? AND record_id = ? AND runner = ?
		ORDER BY last_seen_at DESC, started_at DESC
		LIMIT 1`, projectID, recordID, runner)
	var session RunnerSession
	var resumable int
	err := row.Scan(&session.ProjectID, &session.RecordID, &session.Runner, &session.SessionRef, &session.LastMessageRef, &session.WorkspacePath, &session.CurrentItemID, &session.WorkRevision, &session.LastAttemptID, &session.State, &resumable, &session.StartedAt, &session.LastSeenAt, &session.EndedAt, &session.LastError)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	session.Resumable = resumable != 0
	return &session, nil
}

func (s *RuntimeStore) ListSessionsForRun(projectID, recordID, runner string) ([]RunnerSession, error) {
	query := `SELECT project_id, record_id, runner, session_ref, last_message_ref, workspace_path, current_item_id, work_revision, last_attempt_id, state, resumable, started_at, last_seen_at, ended_at, last_error
		FROM sessions
		WHERE project_id = ? AND record_id = ?`
	args := []any{projectID, recordID}
	if strings.TrimSpace(runner) != "" {
		query += ` AND runner = ?`
		args = append(args, runner)
	}
	query += ` ORDER BY last_seen_at DESC, started_at DESC, session_ref DESC`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RunnerSession
	for rows.Next() {
		var session RunnerSession
		var resumable int
		if err := rows.Scan(&session.ProjectID, &session.RecordID, &session.Runner, &session.SessionRef, &session.LastMessageRef, &session.WorkspacePath, &session.CurrentItemID, &session.WorkRevision, &session.LastAttemptID, &session.State, &resumable, &session.StartedAt, &session.LastSeenAt, &session.EndedAt, &session.LastError); err != nil {
			return nil, err
		}
		session.Resumable = resumable != 0
		out = append(out, session)
	}
	return out, rows.Err()
}

func (s *RuntimeStore) MarkSessionState(projectID, sessionRef, state, endedAt, lastError string, resumable bool) error {
	_, err := s.db.Exec(`UPDATE sessions
		SET state = ?, ended_at = ?, last_error = ?, resumable = ?, last_seen_at = ?
		WHERE project_id = ? AND session_ref = ?`,
		state, endedAt, lastError, boolToInt(resumable), time.Now().UTC().Format(time.RFC3339), projectID, sessionRef)
	return err
}

func (s *RuntimeStore) ForceRetryNow(identity string) (bool, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := s.db.Exec(`UPDATE runs SET lease_state = ?, next_retry_at = ?, updated_at = ? WHERE item_id = ? OR record_id = ?`, string(LeaseStateRetryQueued), now, now, identity, identity)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

func (s *RuntimeStore) GetSetting(key string) (string, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM daemon_settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return value, err
}

func (s *RuntimeStore) SetSetting(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO daemon_settings (key, value)
		VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func (s *RuntimeStore) GlobalActiveRunLimit() (int, error) {
	value, err := s.GetSetting("max_active_runs")
	if err != nil {
		return 0, err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, tuskerError(errorConfigInvalid, "daemon max_active_runs setting is invalid")
	}
	if parsed <= 0 {
		return 0, tuskerError(errorConfigInvalid, "daemon max_active_runs setting must be > 0")
	}
	return parsed, nil
}

func (s *RuntimeStore) SetGlobalActiveRunLimit(limit int) error {
	if limit <= 0 {
		return tuskerError(errorInvalidArg, "--max-active-runs must be > 0", withContext(map[string]any{"arg": "--max-active-runs", "value": limit}))
	}
	return s.SetSetting("max_active_runs", strconv.Itoa(limit))
}

func (s *RuntimeStore) DeleteRunsNotIn(projectID string, keepRecordIDs map[string]struct{}) error {
	rows, err := s.db.Query(`SELECT record_id FROM runs WHERE project_id = ?`, projectID)
	if err != nil {
		return err
	}
	defer rows.Close()
	var stale []string
	for rows.Next() {
		var recordID string
		if err := rows.Scan(&recordID); err != nil {
			return err
		}
		if _, ok := keepRecordIDs[recordID]; !ok {
			stale = append(stale, recordID)
		}
	}
	for _, recordID := range stale {
		if _, err := s.db.Exec(`DELETE FROM supervisor_decisions WHERE project_id = ? AND record_id = ?`, projectID, recordID); err != nil {
			return err
		}
		if _, err := s.db.Exec(`DELETE FROM runs WHERE project_id = ? AND record_id = ?`, projectID, recordID); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (s *RuntimeStore) DaemonStatus() (map[string]any, error) {
	var projectCount int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM projects`).Scan(&projectCount); err != nil {
		return nil, err
	}
	var runCount int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM runs WHERE lease_state IN ('claimed', 'running', 'retry_queued')`).Scan(&runCount); err != nil {
		return nil, err
	}
	globalLimit, err := s.GlobalActiveRunLimit()
	if err != nil {
		return nil, err
	}
	source := "daemon.db"
	if globalLimit <= 0 {
		globalLimit = 2
		source = "default"
	}
	return map[string]any{
		"state_root":          s.stateRoot,
		"projects":            projectCount,
		"activeRuns":          runCount,
		"max_active_runs":     globalLimit,
		"limit_source":        source,
		"default_limit_value": 2,
	}, nil
}

func (s *RuntimeStore) TouchProjectPoll(projectID string) error {
	_, err := s.db.Exec(`UPDATE projects SET last_poll_at = ?, last_error = '' WHERE project_id = ?`, time.Now().UTC().Format(time.RFC3339), projectID)
	return err
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func projectKeyFromPath(repoRoot string) string {
	key := filepath.Base(repoRoot)
	if key == "" || key == "." || key == string(filepath.Separator) {
		key = "project"
	}
	return key
}

func newRegisteredProject(repoRoot, vaultRoot string) RegisteredProject {
	return RegisteredProject{
		ProjectID:    newRecordID(),
		ProjectKey:   projectKeyFromPath(repoRoot),
		Name:         filepath.Base(repoRoot),
		RepoRoot:     repoRoot,
		VaultRoot:    vaultRoot,
		WorkflowPath: workflowPath(vaultRoot),
		Enabled:      true,
		Health:       projectHealthHealthy,
	}
}

func runsCmd(args Args) error {
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	runs, err := store.ListRuns()
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "count": len(runs), "runs": runs})
		return nil
	}
	if len(runs) == 0 {
		fmt.Println("(no runs)")
		return nil
	}
	for _, run := range runs {
		retry := ""
		if strings.TrimSpace(run.NextRetryAt) != "" {
			retry = " retry=" + run.NextRetryAt
		}
		fmt.Printf("%s %-12s %-12s rev=%d attempts=%d%s\n", firstNonEmpty(run.ItemID, run.RecordID), run.Runner, run.LeaseState, run.WorkRevision, run.AttemptCount, retry)
	}
	return nil
}
