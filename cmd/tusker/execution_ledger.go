package main

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// ExecutionNodeKind distinguishes a batch/direct root from a Tusker-owned
// attempt and an observable (but usually provider-owned) child.  It is not a
// lease state: runs and leases remain the sole execution-ownership authority.
type ExecutionNodeKind string

const (
	ExecutionNodeRoot           ExecutionNodeKind = "root"
	ExecutionNodeManagedAttempt ExecutionNodeKind = "managed_attempt"
	ExecutionNodeProviderChild  ExecutionNodeKind = "provider_child"
)

type ExecutionRelationshipKind string

const (
	ExecutionRetryOf         ExecutionRelationshipKind = "retry_of"
	ExecutionResumeOf        ExecutionRelationshipKind = "resume_of"
	ExecutionForkOf          ExecutionRelationshipKind = "fork_of"
	ExecutionManagedChildOf  ExecutionRelationshipKind = "managed_child_of"
	ExecutionProviderChildOf ExecutionRelationshipKind = "provider_child_of"
)

// ExecutionRecord is immutable identity and correlation metadata.  Mutable
// lifecycle evidence deliberately stays out of this ledger and in the
// existing runtime store/provider observation layers.
type ExecutionRecord struct {
	ExecutionID         string            `json:"execution_id"`
	RootExecutionID     string            `json:"root_execution_id"`
	ParentExecutionID   string            `json:"parent_execution_id"`
	ProjectID           string            `json:"project_id"`
	NodeKind            ExecutionNodeKind `json:"node_kind"`
	DisplayName         string            `json:"display_name"`
	SearchLabel         string            `json:"search_label"`
	TaskID              string            `json:"task_id"`
	WaveID              string            `json:"wave_id"`
	WaveGeneration      int               `json:"wave_authorization_generation"`
	AttemptID           string            `json:"attempt_id"`
	SessionRef          string            `json:"session_ref"`
	Source              string            `json:"source"`
	Provider            string            `json:"provider"`
	ProviderSessionID   string            `json:"provider_session_id"`
	AgentType           string            `json:"agent_type"`
	ProviderChildHandle string            `json:"provider_child_handle"`
	Creator             string            `json:"creator"`
	LeaseGeneration     int               `json:"lease_generation"`
	CreatedAt           string            `json:"created_at"`
}

type ExecutionEdge struct {
	ParentExecutionID string                    `json:"parent_execution_id"`
	ChildExecutionID  string                    `json:"child_execution_id"`
	Kind              ExecutionRelationshipKind `json:"kind"`
	CreatedAt         string                    `json:"created_at"`
}

type DirectExecutionInput struct {
	ProjectID, DisplayName, TaskID, WaveID, SessionRef, Source, Provider, ProviderSessionID, AgentType, Creator string
}

type WaveExecutionRootInput struct {
	ProjectID, WaveID, DisplayName, Creator string
	AuthorizationGeneration                 int
}

type ManagedExecutionInput struct {
	ProjectID, ParentExecutionID, TaskID, WaveID, AttemptID, SessionRef, DisplayName, Source, Provider, ProviderSessionID, AgentType, Creator string
	LeaseGeneration                                                                                                                           int
}

type ProviderChildExecutionInput struct {
	ProjectID, ParentExecutionID, Provider, ProviderChildHandle, DisplayName, AgentType, ProviderSessionID, Creator string
}

// ExecutionView combines immutable identity with append-only operator facts.
// The ledger record is never rewritten: renames, provider correlation and
// delivery binding all have their own audited history.
type ExecutionView struct {
	ExecutionRecord
	EffectiveDisplayName string `json:"effective_display_name"`
	EffectiveSearchLabel string `json:"effective_search_label"`
	BoundTaskID          string `json:"bound_task_id"`
	BoundWaveID          string `json:"bound_wave_id"`
	BindingGeneration    int    `json:"binding_generation"`
	BindingAt            string `json:"binding_at"`
	ProofEligible        bool   `json:"proof_eligible"`
	ProviderSessionID    string `json:"effective_provider_session_id"`
	SessionRef           string `json:"effective_session_ref"`
}

type ExecutionBindingInput struct {
	ProjectID, ExecutionID, TaskID, WaveID, Actor string
}

type ExecutionAttachmentInput struct {
	ProjectID, ExecutionID, Provider, ProviderSessionID, SessionRef, Source, Actor string
}

func executionNow() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func newExecutionID() string { return "exec_" + newRecordID() }

func normalizeExecutionLabel(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return strings.ToLower(strings.Join(strings.Fields(value), " "))
		}
	}
	return ""
}

func validExecutionNodeKind(kind ExecutionNodeKind) bool {
	return kind == ExecutionNodeRoot || kind == ExecutionNodeManagedAttempt || kind == ExecutionNodeProviderChild
}

func validExecutionRelationship(kind ExecutionRelationshipKind) bool {
	switch kind {
	case ExecutionRetryOf, ExecutionResumeOf, ExecutionForkOf, ExecutionManagedChildOf, ExecutionProviderChildOf:
		return true
	default:
		return false
	}
}

func (s *RuntimeStore) migrateExecutionLedger() error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS execution_records (
			execution_id TEXT PRIMARY KEY,
			root_execution_id TEXT NOT NULL,
			parent_execution_id TEXT NOT NULL DEFAULT '',
			project_id TEXT NOT NULL,
			node_kind TEXT NOT NULL CHECK(node_kind IN ('root', 'managed_attempt', 'provider_child')),
			display_name TEXT NOT NULL DEFAULT '',
			search_label TEXT NOT NULL DEFAULT '',
			task_id TEXT NOT NULL DEFAULT '', wave_id TEXT NOT NULL DEFAULT '', wave_authorization_generation INTEGER NOT NULL DEFAULT 0,
			attempt_id TEXT NOT NULL DEFAULT '', session_ref TEXT NOT NULL DEFAULT '',
			source TEXT NOT NULL DEFAULT '', provider TEXT NOT NULL DEFAULT '',
			provider_session_id TEXT NOT NULL DEFAULT '', agent_type TEXT NOT NULL DEFAULT '',
			provider_child_handle TEXT NOT NULL DEFAULT '', creator TEXT NOT NULL DEFAULT '',
			lease_generation INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL,
			CHECK((node_kind = 'root' AND root_execution_id = execution_id AND parent_execution_id = '') OR (node_kind != 'root' AND parent_execution_id != ''))
		);`,
		`CREATE TABLE IF NOT EXISTS execution_edges (
			parent_execution_id TEXT NOT NULL, child_execution_id TEXT NOT NULL,
			kind TEXT NOT NULL, created_at TEXT NOT NULL,
			PRIMARY KEY(parent_execution_id, child_execution_id)
		);`,
		`CREATE TABLE IF NOT EXISTS execution_name_events (
			event_id TEXT PRIMARY KEY, execution_id TEXT NOT NULL, display_name TEXT NOT NULL,
			search_label TEXT NOT NULL, actor TEXT NOT NULL, created_at TEXT NOT NULL,
			UNIQUE(execution_id, display_name, created_at)
		);`,
		`CREATE TABLE IF NOT EXISTS execution_attachment_events (
			event_id TEXT PRIMARY KEY, execution_id TEXT NOT NULL, project_id TEXT NOT NULL,
			provider TEXT NOT NULL, provider_session_id TEXT NOT NULL, session_ref TEXT NOT NULL DEFAULT '',
			source TEXT NOT NULL DEFAULT '', actor TEXT NOT NULL, created_at TEXT NOT NULL,
			UNIQUE(project_id, provider, provider_session_id),
			UNIQUE(execution_id, provider, provider_session_id)
		);`,
		`CREATE TABLE IF NOT EXISTS execution_binding_events (
			event_id TEXT PRIMARY KEY, execution_id TEXT NOT NULL, generation INTEGER NOT NULL,
			action TEXT NOT NULL CHECK(action IN ('bind','detach','rebind')), task_id TEXT NOT NULL DEFAULT '',
			wave_id TEXT NOT NULL DEFAULT '', actor TEXT NOT NULL, created_at TEXT NOT NULL,
			UNIQUE(execution_id, generation)
		);`,
	}
	for _, statement := range statements {
		if _, err := s.exec(statement); err != nil {
			return err
		}
	}
	for _, column := range []struct{ name, stmt string }{
		{"parent_execution_id", `ALTER TABLE execution_records ADD COLUMN parent_execution_id TEXT NOT NULL DEFAULT ''`},
		{"wave_authorization_generation", `ALTER TABLE execution_records ADD COLUMN wave_authorization_generation INTEGER NOT NULL DEFAULT 0`},
	} {
		if err := s.ensureColumn("execution_records", column.name, column.stmt); err != nil {
			return err
		}
	}
	if _, err := s.exec(`UPDATE execution_records SET parent_execution_id = COALESCE((SELECT parent_execution_id FROM execution_edges WHERE execution_edges.child_execution_id = execution_records.execution_id), '') WHERE parent_execution_id = '' AND node_kind != 'root'`); err != nil {
		return err
	}
	for _, trigger := range []string{"execution_edges_validate_insert", "execution_edges_validate_update", "execution_edges_prevent_delete", "execution_records_identity_immutable", "execution_records_prevent_delete_with_edges", "execution_records_immutable", "execution_records_prevent_delete", "execution_name_events_validate_insert", "execution_attachment_events_validate_insert", "execution_binding_events_validate_insert", "execution_name_events_immutable", "execution_name_events_prevent_delete", "execution_attachment_events_immutable", "execution_attachment_events_prevent_delete", "execution_binding_events_immutable", "execution_binding_events_prevent_delete"} {
		if _, err := s.exec(`DROP TRIGGER IF EXISTS ` + trigger); err != nil {
			return err
		}
	}
	for _, statement := range []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS execution_records_attempt_id ON execution_records(attempt_id) WHERE attempt_id != '';`,
		`DROP INDEX IF EXISTS execution_provider_child_identity;`,
		`CREATE UNIQUE INDEX IF NOT EXISTS execution_provider_child_identity ON execution_records(project_id, parent_execution_id, provider, provider_child_handle) WHERE node_kind = 'provider_child' AND provider != '' AND provider_child_handle != '';`,
		`CREATE UNIQUE INDEX IF NOT EXISTS execution_edges_one_parent ON execution_edges(child_execution_id);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS execution_wave_generation_root ON execution_records(project_id, wave_id, wave_authorization_generation) WHERE node_kind = 'root' AND wave_id != '' AND wave_authorization_generation > 0;`,
		`CREATE INDEX IF NOT EXISTS execution_records_root ON execution_records(root_execution_id);`,
		`CREATE INDEX IF NOT EXISTS execution_binding_events_current ON execution_binding_events(execution_id, generation DESC);`,
		`CREATE INDEX IF NOT EXISTS execution_attachment_events_execution ON execution_attachment_events(execution_id, created_at DESC);`,
		`CREATE TRIGGER IF NOT EXISTS execution_edges_validate_insert BEFORE INSERT ON execution_edges BEGIN
			SELECT CASE WHEN NEW.kind NOT IN ('retry_of','resume_of','fork_of','managed_child_of','provider_child_of') THEN RAISE(ABORT, 'invalid execution edge kind') END;
			SELECT CASE WHEN NEW.parent_execution_id = NEW.child_execution_id THEN RAISE(ABORT, 'execution edge cannot be self-referential') END;
			SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM execution_records WHERE execution_id = NEW.parent_execution_id) OR NOT EXISTS (SELECT 1 FROM execution_records WHERE execution_id = NEW.child_execution_id) THEN RAISE(ABORT, 'execution edge endpoint not found') END;
			SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM execution_records parent JOIN execution_records child ON parent.project_id = child.project_id AND parent.root_execution_id = child.root_execution_id WHERE parent.execution_id = NEW.parent_execution_id AND child.execution_id = NEW.child_execution_id) THEN RAISE(ABORT, 'execution edge project/root mismatch') END;
			SELECT CASE WHEN EXISTS (WITH RECURSIVE ancestors(id) AS (SELECT NEW.parent_execution_id UNION ALL SELECT edge.parent_execution_id FROM execution_edges edge JOIN ancestors ON edge.child_execution_id = ancestors.id) SELECT 1 FROM ancestors WHERE id = NEW.child_execution_id) THEN RAISE(ABORT, 'execution edge cycle') END;
			SELECT CASE WHEN EXISTS (SELECT 1 FROM execution_records WHERE execution_id = NEW.child_execution_id AND ((node_kind = 'root') OR (node_kind = 'provider_child' AND NEW.kind != 'provider_child_of') OR (node_kind = 'managed_attempt' AND NEW.kind NOT IN ('managed_child_of','retry_of','resume_of','fork_of')))) THEN RAISE(ABORT, 'execution edge kind does not match child node') END;
		END;`,
		`CREATE TRIGGER IF NOT EXISTS execution_edges_validate_update BEFORE UPDATE ON execution_edges BEGIN
			SELECT CASE WHEN NEW.kind NOT IN ('retry_of','resume_of','fork_of','managed_child_of','provider_child_of') THEN RAISE(ABORT, 'invalid execution edge kind') END;
			SELECT CASE WHEN NEW.parent_execution_id = NEW.child_execution_id THEN RAISE(ABORT, 'execution edge cannot be self-referential') END;
			SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM execution_records WHERE execution_id = NEW.parent_execution_id) OR NOT EXISTS (SELECT 1 FROM execution_records WHERE execution_id = NEW.child_execution_id) THEN RAISE(ABORT, 'execution edge endpoint not found') END;
			SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM execution_records parent JOIN execution_records child ON parent.project_id = child.project_id AND parent.root_execution_id = child.root_execution_id WHERE parent.execution_id = NEW.parent_execution_id AND child.execution_id = NEW.child_execution_id) THEN RAISE(ABORT, 'execution edge project/root mismatch') END;
			SELECT CASE WHEN EXISTS (WITH RECURSIVE ancestors(id) AS (SELECT NEW.parent_execution_id UNION ALL SELECT edge.parent_execution_id FROM execution_edges edge JOIN ancestors ON edge.child_execution_id = ancestors.id WHERE NOT (edge.parent_execution_id = OLD.parent_execution_id AND edge.child_execution_id = OLD.child_execution_id)) SELECT 1 FROM ancestors WHERE id = NEW.child_execution_id) THEN RAISE(ABORT, 'execution edge cycle') END;
			SELECT CASE WHEN EXISTS (SELECT 1 FROM execution_records WHERE execution_id = NEW.child_execution_id AND ((node_kind = 'root') OR (node_kind = 'provider_child' AND NEW.kind != 'provider_child_of') OR (node_kind = 'managed_attempt' AND NEW.kind NOT IN ('managed_child_of','retry_of','resume_of','fork_of')))) THEN RAISE(ABORT, 'execution edge kind does not match child node') END;
			SELECT RAISE(ABORT, 'execution edges are immutable') WHERE NEW.parent_execution_id != OLD.parent_execution_id OR NEW.child_execution_id != OLD.child_execution_id OR NEW.kind != OLD.kind OR NEW.created_at != OLD.created_at;
		END;`,
		`CREATE TRIGGER IF NOT EXISTS execution_edges_prevent_delete BEFORE DELETE ON execution_edges BEGIN SELECT RAISE(ABORT, 'execution edges are immutable'); END;`,
		// Execution records are append-only. Renames and rebindings are later
		// audited operations that add history; they never rewrite this identity
		// ledger row or its immutable correlation facts.
		`CREATE TRIGGER IF NOT EXISTS execution_records_immutable BEFORE UPDATE ON execution_records BEGIN SELECT RAISE(ABORT, 'execution records are immutable'); END;`,
		`CREATE TRIGGER IF NOT EXISTS execution_records_prevent_delete BEFORE DELETE ON execution_records BEGIN SELECT RAISE(ABORT, 'execution records are immutable'); END;`,
		`CREATE TRIGGER IF NOT EXISTS execution_name_events_validate_insert BEFORE INSERT ON execution_name_events BEGIN SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM execution_records WHERE execution_id = NEW.execution_id) THEN RAISE(ABORT, 'execution name event execution not found') END; END;`,
		`CREATE TRIGGER IF NOT EXISTS execution_attachment_events_validate_insert BEFORE INSERT ON execution_attachment_events BEGIN SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM execution_records WHERE execution_id = NEW.execution_id AND project_id = NEW.project_id) THEN RAISE(ABORT, 'execution attachment event execution/project mismatch') END; END;`,
		`CREATE TRIGGER IF NOT EXISTS execution_binding_events_validate_insert BEFORE INSERT ON execution_binding_events BEGIN SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM execution_records WHERE execution_id = NEW.execution_id) THEN RAISE(ABORT, 'execution binding event execution not found') END; END;`,
		`CREATE TRIGGER IF NOT EXISTS execution_name_events_immutable BEFORE UPDATE ON execution_name_events BEGIN SELECT RAISE(ABORT, 'execution name events are immutable'); END;`,
		`CREATE TRIGGER IF NOT EXISTS execution_name_events_prevent_delete BEFORE DELETE ON execution_name_events BEGIN SELECT RAISE(ABORT, 'execution name events are immutable'); END;`,
		`CREATE TRIGGER IF NOT EXISTS execution_attachment_events_immutable BEFORE UPDATE ON execution_attachment_events BEGIN SELECT RAISE(ABORT, 'execution attachment events are immutable'); END;`,
		`CREATE TRIGGER IF NOT EXISTS execution_attachment_events_prevent_delete BEFORE DELETE ON execution_attachment_events BEGIN SELECT RAISE(ABORT, 'execution attachment events are immutable'); END;`,
		`CREATE TRIGGER IF NOT EXISTS execution_binding_events_immutable BEFORE UPDATE ON execution_binding_events BEGIN SELECT RAISE(ABORT, 'execution binding events are immutable'); END;`,
		`CREATE TRIGGER IF NOT EXISTS execution_binding_events_prevent_delete BEFORE DELETE ON execution_binding_events BEGIN SELECT RAISE(ABORT, 'execution binding events are immutable'); END;`,
	} {
		if _, err := s.exec(statement); err != nil {
			return err
		}
	}
	return s.backfillExecutionLedger()
}

// CreateDirectExecution allocates the root before a provider launch. It does
// not claim work, bind delivery authority, or alter any run/lease row.
func (s *RuntimeStore) CreateDirectExecution(input DirectExecutionInput) (ExecutionRecord, error) {
	if strings.TrimSpace(input.ProjectID) == "" {
		return ExecutionRecord{}, tuskerError(errorInvalidArg, "direct execution requires project_id")
	}
	now := executionNow()
	record := ExecutionRecord{ExecutionID: newExecutionID(), ProjectID: input.ProjectID, NodeKind: ExecutionNodeRoot, DisplayName: strings.TrimSpace(input.DisplayName), TaskID: strings.TrimSpace(input.TaskID), WaveID: strings.TrimSpace(input.WaveID), SessionRef: strings.TrimSpace(input.SessionRef), Source: strings.TrimSpace(input.Source), Provider: strings.TrimSpace(input.Provider), ProviderSessionID: strings.TrimSpace(input.ProviderSessionID), AgentType: strings.TrimSpace(input.AgentType), Creator: strings.TrimSpace(input.Creator), CreatedAt: now}
	record.RootExecutionID = record.ExecutionID
	record.SearchLabel = normalizeExecutionLabel(record.DisplayName, record.ProviderSessionID, record.AgentType, record.Provider, record.ExecutionID)
	return record, s.insertExecutionRecord(record)
}

// CreateWaveExecutionRoot records one logical root for one authorization
// generation. Its caller must supply the generation as part of creator/audit
// context; this ledger does not arm waves or manufacture a lease.
func (s *RuntimeStore) CreateWaveExecutionRoot(input WaveExecutionRootInput) (ExecutionRecord, error) {
	if strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.WaveID) == "" || input.AuthorizationGeneration <= 0 {
		return ExecutionRecord{}, tuskerError(errorInvalidArg, "wave execution root requires project_id, wave_id, and authorization generation")
	}
	if existing, found, err := s.waveExecutionRoot(input.ProjectID, input.WaveID, input.AuthorizationGeneration); err != nil {
		return ExecutionRecord{}, err
	} else if found {
		return existing, nil
	}
	now := executionNow()
	record := ExecutionRecord{ExecutionID: newExecutionID(), ProjectID: input.ProjectID, NodeKind: ExecutionNodeRoot, DisplayName: strings.TrimSpace(input.DisplayName), WaveID: strings.TrimSpace(input.WaveID), WaveGeneration: input.AuthorizationGeneration, Source: "daemon", Creator: strings.TrimSpace(input.Creator), CreatedAt: now}
	record.RootExecutionID = record.ExecutionID
	record.SearchLabel = normalizeExecutionLabel(record.DisplayName, record.WaveID, record.ExecutionID)
	if err := s.insertExecutionRecord(record); err != nil {
		if existing, found, lookupErr := s.waveExecutionRoot(input.ProjectID, input.WaveID, input.AuthorizationGeneration); lookupErr == nil && found {
			return existing, nil
		}
		return ExecutionRecord{}, err
	}
	return record, nil
}

func (s *RuntimeStore) waveExecutionRoot(projectID, waveID string, generation int) (ExecutionRecord, bool, error) {
	var record ExecutionRecord
	err := s.queryRowScan(`SELECT execution_id, root_execution_id, parent_execution_id, project_id, node_kind, display_name, search_label, task_id, wave_id, wave_authorization_generation, attempt_id, session_ref, source, provider, provider_session_id, agent_type, provider_child_handle, creator, lease_generation, created_at FROM execution_records WHERE project_id = ? AND wave_id = ? AND wave_authorization_generation = ? AND node_kind = 'root'`, []any{strings.TrimSpace(projectID), strings.TrimSpace(waveID), generation}, executionScanDest(&record)...)
	if err == sql.ErrNoRows {
		return ExecutionRecord{}, false, nil
	}
	if err != nil {
		return ExecutionRecord{}, false, err
	}
	return record, true, nil
}

func (s *RuntimeStore) CreateManagedExecution(input ManagedExecutionInput) (ExecutionRecord, error) {
	return s.createLeasedRelatedExecution(input, ExecutionManagedChildOf)
}

// CreateExecutionContinuation records a retry, resume, or fork without
// changing ownership semantics. The new attempt is still admitted through the
// same live SQLite lease fence as a managed child; the edge merely explains
// lineage and never grants concurrent ownership to the predecessor.
func (s *RuntimeStore) CreateExecutionContinuation(input ManagedExecutionInput, relationship ExecutionRelationshipKind) (ExecutionRecord, error) {
	if relationship != ExecutionRetryOf && relationship != ExecutionResumeOf && relationship != ExecutionForkOf {
		return ExecutionRecord{}, tuskerError(errorInvalidArg, "continuation requires retry, resume, or fork relationship")
	}
	return s.createLeasedRelatedExecution(input, relationship)
}

func (s *RuntimeStore) createLeasedRelatedExecution(input ManagedExecutionInput, relationship ExecutionRelationshipKind) (ExecutionRecord, error) {
	if strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.ParentExecutionID) == "" || strings.TrimSpace(input.TaskID) == "" || strings.TrimSpace(input.AttemptID) == "" {
		return ExecutionRecord{}, tuskerError(errorInvalidArg, "managed execution requires project, parent, task, and attempt IDs")
	}
	parent, err := s.Execution(input.ParentExecutionID)
	if err != nil || parent == nil {
		if err != nil {
			return ExecutionRecord{}, err
		}
		return ExecutionRecord{}, tuskerError(errorNotFound, "managed execution parent not found")
	}
	if parent.ProjectID != input.ProjectID {
		return ExecutionRecord{}, tuskerError(errorInvalidArg, "managed execution parent project mismatch")
	}
	// The only source of a managed child attempt is an already admitted live
	// run. Caller-supplied attempt/generation/readiness values cannot mint a
	// lease: they must match SQLite's ownership row atomically at insertion.
	run, err := s.managedExecutionRun(input.ProjectID, input.TaskID, input.AttemptID, input.LeaseGeneration)
	if err != nil {
		return ExecutionRecord{}, err
	}
	now := executionNow()
	record := ExecutionRecord{ExecutionID: newExecutionID(), RootExecutionID: parent.RootExecutionID, ParentExecutionID: parent.ExecutionID, ProjectID: input.ProjectID, NodeKind: ExecutionNodeManagedAttempt, DisplayName: strings.TrimSpace(input.DisplayName), TaskID: run.ItemID, WaveID: strings.TrimSpace(input.WaveID), AttemptID: run.ActiveAttemptID, SessionRef: strings.TrimSpace(input.SessionRef), Source: strings.TrimSpace(input.Source), Provider: strings.TrimSpace(input.Provider), ProviderSessionID: strings.TrimSpace(input.ProviderSessionID), AgentType: strings.TrimSpace(input.AgentType), Creator: strings.TrimSpace(input.Creator), LeaseGeneration: run.LeaseGeneration, CreatedAt: now}
	record.SearchLabel = normalizeExecutionLabel(record.DisplayName, record.TaskID, record.AgentType, record.ExecutionID)
	if err := s.insertManagedExecution(record, ExecutionEdge{ParentExecutionID: parent.ExecutionID, ChildExecutionID: record.ExecutionID, Kind: relationship, CreatedAt: now}); err != nil {
		return ExecutionRecord{}, err
	}
	return record, nil
}

func (s *RuntimeStore) insertManagedExecution(record ExecutionRecord, edge ExecutionEdge) error {
	return s.withBusyRetry(func() error {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		var itemID, activeAttempt, leaseState string
		var generation int
		err = tx.QueryRow(`SELECT item_id, active_attempt_id, lease_generation, lease_state FROM runs WHERE project_id = ? AND (item_id = ? OR record_id = ?)`, record.ProjectID, record.TaskID, record.TaskID).Scan(&itemID, &activeAttempt, &generation, &leaseState)
		if err != nil {
			if err == sql.ErrNoRows {
				return tuskerError(errorInvalidArg, "managed execution requires an independently admitted live run lease")
			}
			return err
		}
		if activeAttempt != record.AttemptID || generation != record.LeaseGeneration || (leaseState != string(LeaseStateClaimed) && leaseState != string(LeaseStateRunning)) {
			return tuskerError(errorInvalidArg, "managed execution lease no longer matches SQLite authority")
		}
		if err := insertExecutionWithEdgeTx(tx, record, edge); err != nil {
			return err
		}
		return tx.Commit()
	})
}

func (s *RuntimeStore) managedExecutionRun(projectID, taskID, attemptID string, generation int) (*RunStatus, error) {
	if generation <= 0 {
		return nil, tuskerError(errorInvalidArg, "managed execution requires a live lease generation")
	}
	runs, err := s.ListRuns()
	if err != nil {
		return nil, err
	}
	for _, run := range runs {
		if run.ProjectID == projectID && (run.ItemID == taskID || run.RecordID == taskID) && run.ActiveAttemptID == attemptID && run.LeaseGeneration == generation && (run.LeaseState == string(LeaseStateClaimed) || run.LeaseState == string(LeaseStateRunning)) {
			copy := run
			return &copy, nil
		}
	}
	return nil, tuskerError(errorInvalidArg, "managed execution requires an independently admitted live run lease")
}

// UpsertProviderChildExecution is intentionally idempotent on the provider's
// child handle under the same root. It only records correlation facts and
// cannot create a task claim or independent lease.
func (s *RuntimeStore) UpsertProviderChildExecution(input ProviderChildExecutionInput) (ExecutionRecord, bool, error) {
	if strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.ParentExecutionID) == "" || strings.TrimSpace(input.Provider) == "" || strings.TrimSpace(input.ProviderChildHandle) == "" {
		return ExecutionRecord{}, false, tuskerError(errorInvalidArg, "provider child requires project, parent, provider, and child handle")
	}
	parent, err := s.Execution(input.ParentExecutionID)
	if err != nil || parent == nil {
		if err != nil {
			return ExecutionRecord{}, false, err
		}
		return ExecutionRecord{}, false, tuskerError(errorNotFound, "provider child parent not found")
	}
	if parent.ProjectID != input.ProjectID {
		return ExecutionRecord{}, false, tuskerError(errorInvalidArg, "provider child parent project mismatch")
	}
	existing, found, err := s.providerChildExecution(input.ProjectID, parent.ExecutionID, input.Provider, input.ProviderChildHandle)
	if err != nil {
		return ExecutionRecord{}, false, err
	}
	if found {
		return existing, false, nil
	}
	now := executionNow()
	record := ExecutionRecord{ExecutionID: newExecutionID(), RootExecutionID: parent.RootExecutionID, ParentExecutionID: parent.ExecutionID, ProjectID: input.ProjectID, NodeKind: ExecutionNodeProviderChild, DisplayName: strings.TrimSpace(input.DisplayName), TaskID: parent.TaskID, WaveID: parent.WaveID, Source: "provider", Provider: strings.TrimSpace(input.Provider), ProviderSessionID: strings.TrimSpace(input.ProviderSessionID), AgentType: strings.TrimSpace(input.AgentType), ProviderChildHandle: strings.TrimSpace(input.ProviderChildHandle), Creator: strings.TrimSpace(input.Creator), CreatedAt: now}
	record.SearchLabel = normalizeExecutionLabel(record.DisplayName, record.AgentType, record.ProviderChildHandle, record.ExecutionID)
	if err := s.insertExecutionWithEdge(record, ExecutionEdge{ParentExecutionID: parent.ExecutionID, ChildExecutionID: record.ExecutionID, Kind: ExecutionProviderChildOf, CreatedAt: now}); err != nil {
		// A concurrent replay may win between the lookup and INSERT. The unique
		// provider identity is the idempotency fence; return its winner instead
		// of leaking a constraint race to an untrusted adapter.
		if existing, found, lookupErr := s.providerChildExecution(input.ProjectID, parent.ExecutionID, input.Provider, input.ProviderChildHandle); lookupErr == nil && found {
			return existing, false, nil
		}
		return ExecutionRecord{}, false, err
	}
	return record, true, nil
}

func (s *RuntimeStore) providerChildExecution(projectID, parentID, provider, handle string) (ExecutionRecord, bool, error) {
	var record ExecutionRecord
	err := s.queryRowScan(`SELECT execution_id, root_execution_id, parent_execution_id, project_id, node_kind, display_name, search_label, task_id, wave_id, wave_authorization_generation, attempt_id, session_ref, source, provider, provider_session_id, agent_type, provider_child_handle, creator, lease_generation, created_at FROM execution_records WHERE project_id = ? AND parent_execution_id = ? AND provider = ? AND provider_child_handle = ? AND node_kind = 'provider_child'`, []any{projectID, parentID, strings.TrimSpace(provider), strings.TrimSpace(handle)}, executionScanDest(&record)...)
	if err == sql.ErrNoRows {
		return ExecutionRecord{}, false, nil
	}
	if err != nil {
		return ExecutionRecord{}, false, err
	}
	return record, true, nil
}

func executionScanDest(record *ExecutionRecord) []any {
	return []any{&record.ExecutionID, &record.RootExecutionID, &record.ParentExecutionID, &record.ProjectID, &record.NodeKind, &record.DisplayName, &record.SearchLabel, &record.TaskID, &record.WaveID, &record.WaveGeneration, &record.AttemptID, &record.SessionRef, &record.Source, &record.Provider, &record.ProviderSessionID, &record.AgentType, &record.ProviderChildHandle, &record.Creator, &record.LeaseGeneration, &record.CreatedAt}
}

func (s *RuntimeStore) Execution(id string) (*ExecutionRecord, error) {
	var record ExecutionRecord
	err := s.queryRowScan(`SELECT execution_id, root_execution_id, parent_execution_id, project_id, node_kind, display_name, search_label, task_id, wave_id, wave_authorization_generation, attempt_id, session_ref, source, provider, provider_session_id, agent_type, provider_child_handle, creator, lease_generation, created_at FROM execution_records WHERE execution_id = ?`, []any{strings.TrimSpace(id)}, executionScanDest(&record)...)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &record, nil
}

// ExecutionView returns the current operator projection without changing the
// immutable execution identity. A binding only becomes eligible at its own
// generation boundary; there is intentionally no way to certify prior facts.
func (s *RuntimeStore) ExecutionView(id string) (*ExecutionView, error) {
	record, err := s.Execution(id)
	if err != nil || record == nil {
		return nil, err
	}
	view := &ExecutionView{ExecutionRecord: *record, EffectiveDisplayName: record.DisplayName, EffectiveSearchLabel: record.SearchLabel, ProviderSessionID: record.ProviderSessionID, SessionRef: record.SessionRef}
	if err := s.queryRowScan(`SELECT display_name, search_label FROM execution_name_events WHERE execution_id = ? ORDER BY created_at DESC, event_id DESC LIMIT 1`, []any{record.ExecutionID}, &view.EffectiveDisplayName, &view.EffectiveSearchLabel); err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if err := s.queryRowScan(`SELECT provider_session_id, session_ref FROM execution_attachment_events WHERE execution_id = ? ORDER BY created_at DESC, event_id DESC LIMIT 1`, []any{record.ExecutionID}, &view.ProviderSessionID, &view.SessionRef); err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	var action string
	if err := s.queryRowScan(`SELECT generation, action, task_id, wave_id, created_at FROM execution_binding_events WHERE execution_id = ? ORDER BY generation DESC LIMIT 1`, []any{record.ExecutionID}, &view.BindingGeneration, &action, &view.BoundTaskID, &view.BoundWaveID, &view.BindingAt); err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if action == "detach" {
		view.BoundTaskID, view.BoundWaveID = "", ""
	}
	view.ProofEligible = view.BindingGeneration > 0 && action != "detach" && view.BoundTaskID != ""
	return view, nil
}

func (s *RuntimeStore) ListUnboundDirectExecutions(projectID string) ([]ExecutionView, error) {
	rows, err := s.query(`SELECT execution_id FROM execution_records WHERE project_id = ? AND node_kind = 'root' AND source IN ('direct_codex','direct_claude','codex_cloud','direct') ORDER BY created_at DESC`, []any{strings.TrimSpace(projectID)})
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ExecutionView
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		view, err := s.ExecutionView(id)
		if err != nil {
			return nil, err
		}
		if view != nil && !view.ProofEligible {
			out = append(out, *view)
		}
	}
	return out, rows.Err()
}

func (s *RuntimeStore) RenameExecution(projectID, id, displayName, actor string) (*ExecutionView, error) {
	projectID, id, displayName = strings.TrimSpace(projectID), strings.TrimSpace(id), strings.TrimSpace(displayName)
	if projectID == "" || id == "" || displayName == "" {
		return nil, tuskerError(errorInvalidArg, "execution rename requires project, id, and display name")
	}
	record, err := s.Execution(id)
	if err != nil {
		return nil, err
	}
	if record == nil || record.ProjectID != projectID {
		return nil, tuskerError(errorNotFound, "execution not found")
	}
	if err := s.withBusyRetry(func() error {
		_, err := s.exec(`INSERT INTO execution_name_events(event_id, execution_id, display_name, search_label, actor, created_at) VALUES(?,?,?,?,?,?)`, "exec-name-"+strings.ToLower(newRecordID()), id, displayName, normalizeExecutionLabel(displayName), strings.TrimSpace(actor), executionNow())
		return err
	}); err != nil {
		return nil, err
	}
	return s.ExecutionView(id)
}

// AttachExecution correlates a provider-owned session after launch. Replaying
// the same provider identity is harmless; a provider identity may never be
// silently moved to a different Tusker execution.
func (s *RuntimeStore) AttachExecution(input ExecutionAttachmentInput) (*ExecutionView, bool, error) {
	input.ProjectID, input.ExecutionID, input.Provider, input.ProviderSessionID = strings.TrimSpace(input.ProjectID), strings.TrimSpace(input.ExecutionID), strings.TrimSpace(input.Provider), strings.TrimSpace(input.ProviderSessionID)
	if input.ProjectID == "" || input.ExecutionID == "" || input.Provider == "" || input.ProviderSessionID == "" {
		return nil, false, tuskerError(errorInvalidArg, "execution attach requires project, id, provider, and provider session id")
	}
	record, err := s.Execution(input.ExecutionID)
	if err != nil || record == nil {
		if err == nil {
			err = tuskerError(errorNotFound, "execution not found")
		}
		return nil, false, err
	}
	if record.ProjectID != input.ProjectID {
		return nil, false, tuskerError(errorNotFound, "execution not found")
	}
	created := false
	err = s.withBusyRetry(func() error {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		var existingID string
		err = tx.QueryRow(`SELECT execution_id FROM execution_attachment_events WHERE project_id = ? AND provider = ? AND provider_session_id = ?`, record.ProjectID, input.Provider, input.ProviderSessionID).Scan(&existingID)
		if err == nil {
			if existingID != record.ExecutionID {
				return tuskerError(errorInvalidTransition, "provider session is already attached to another execution")
			}
			return tx.Commit()
		}
		if err != sql.ErrNoRows {
			return err
		}
		if _, err = tx.Exec(`INSERT INTO execution_attachment_events(event_id, execution_id, project_id, provider, provider_session_id, session_ref, source, actor, created_at) VALUES(?,?,?,?,?,?,?,?,?)`, "exec-attach-"+strings.ToLower(newRecordID()), record.ExecutionID, record.ProjectID, input.Provider, input.ProviderSessionID, strings.TrimSpace(input.SessionRef), strings.TrimSpace(input.Source), strings.TrimSpace(input.Actor), executionNow()); err != nil {
			return err
		}
		created = true
		return tx.Commit()
	})
	if err != nil {
		return nil, false, err
	}
	view, err := s.ExecutionView(record.ExecutionID)
	return view, created, err
}

// BindExecution appends a new authority generation. The task/wave agreement is
// resolved by the CLI before this call; the store independently fences a live
// Tusker lease so direct observation cannot steal an active delivery owner.
func (s *RuntimeStore) BindExecution(input ExecutionBindingInput, action string) (*ExecutionView, error) {
	input.ProjectID, input.ExecutionID, input.TaskID, input.WaveID = strings.TrimSpace(input.ProjectID), strings.TrimSpace(input.ExecutionID), strings.TrimSpace(input.TaskID), strings.TrimSpace(input.WaveID)
	if action != "bind" && action != "detach" && action != "rebind" {
		return nil, tuskerError(errorInvalidArg, "invalid execution binding action")
	}
	if input.ProjectID == "" || input.ExecutionID == "" || (action != "detach" && input.TaskID == "") {
		return nil, tuskerError(errorInvalidArg, "execution binding requires project, id, and task")
	}
	record, err := s.Execution(input.ExecutionID)
	if err != nil || record == nil {
		if err == nil {
			err = tuskerError(errorNotFound, "execution not found")
		}
		return nil, err
	}
	if record.ProjectID != input.ProjectID {
		return nil, tuskerError(errorNotFound, "execution not found")
	}
	err = s.withBusyRetry(func() error {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if action != "detach" {
			var count int
			if err = tx.QueryRow(`SELECT COUNT(*) FROM runs WHERE project_id = ? AND (item_id = ? OR record_id = ?) AND terminal = 0 AND lease_state IN ('claimed','running')`, record.ProjectID, input.TaskID, input.TaskID).Scan(&count); err != nil {
				return err
			}
			if count > 0 {
				return tuskerError(errorInvalidTransition, "execution binding refuses conflicting live task owner")
			}
		}
		var generation int
		if err = tx.QueryRow(`SELECT COALESCE(MAX(generation), 0) + 1 FROM execution_binding_events WHERE execution_id = ?`, record.ExecutionID).Scan(&generation); err != nil {
			return err
		}
		_, err = tx.Exec(`INSERT INTO execution_binding_events(event_id, execution_id, generation, action, task_id, wave_id, actor, created_at) VALUES(?,?,?,?,?,?,?,?)`, "exec-bind-"+strings.ToLower(newRecordID()), record.ExecutionID, generation, action, input.TaskID, input.WaveID, strings.TrimSpace(input.Actor), executionNow())
		if err != nil {
			return err
		}
		return tx.Commit()
	})
	if err != nil {
		return nil, err
	}
	return s.ExecutionView(record.ExecutionID)
}

func (s *RuntimeStore) insertExecutionRecord(record ExecutionRecord) error {
	return s.insertExecutionWithEdge(record, ExecutionEdge{})
}

func (s *RuntimeStore) insertExecutionWithEdge(record ExecutionRecord, edge ExecutionEdge) error {
	if !validExecutionNodeKind(record.NodeKind) || strings.TrimSpace(record.ExecutionID) == "" || strings.TrimSpace(record.RootExecutionID) == "" || strings.TrimSpace(record.ProjectID) == "" {
		return tuskerError(errorInvalidArg, "invalid execution record")
	}
	return s.withBusyRetry(func() error {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if err := insertExecutionWithEdgeTx(tx, record, edge); err != nil {
			return err
		}
		return tx.Commit()
	})
}

func insertExecutionWithEdgeTx(tx *sql.Tx, record ExecutionRecord, edge ExecutionEdge) error {
	_, err := tx.Exec(`INSERT INTO execution_records (execution_id, root_execution_id, parent_execution_id, project_id, node_kind, display_name, search_label, task_id, wave_id, wave_authorization_generation, attempt_id, session_ref, source, provider, provider_session_id, agent_type, provider_child_handle, creator, lease_generation, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, record.ExecutionID, record.RootExecutionID, record.ParentExecutionID, record.ProjectID, record.NodeKind, record.DisplayName, record.SearchLabel, record.TaskID, record.WaveID, record.WaveGeneration, record.AttemptID, record.SessionRef, record.Source, record.Provider, record.ProviderSessionID, record.AgentType, record.ProviderChildHandle, record.Creator, record.LeaseGeneration, record.CreatedAt)
	if err != nil {
		return err
	}
	if edge.ChildExecutionID == "" {
		return nil
	}
	if !validExecutionRelationship(edge.Kind) || edge.ChildExecutionID != record.ExecutionID || edge.ParentExecutionID != record.ParentExecutionID {
		return tuskerError(errorInvalidArg, "invalid execution relationship")
	}
	if record.NodeKind == ExecutionNodeManagedAttempt && edge.Kind != ExecutionManagedChildOf && edge.Kind != ExecutionRetryOf && edge.Kind != ExecutionResumeOf && edge.Kind != ExecutionForkOf {
		return tuskerError(errorInvalidArg, "managed execution requires typed lineage edge")
	}
	if record.NodeKind == ExecutionNodeProviderChild && edge.Kind != ExecutionProviderChildOf {
		return tuskerError(errorInvalidArg, "provider execution requires provider-child edge")
	}
	_, err = tx.Exec(`INSERT INTO execution_edges (parent_execution_id, child_execution_id, kind, created_at) VALUES (?, ?, ?, ?)`, edge.ParentExecutionID, edge.ChildExecutionID, edge.Kind, edge.CreatedAt)
	return err
}

func legacyExecutionID(attemptID string) string {
	sum := sha256.Sum256([]byte(attemptID))
	return fmt.Sprintf("exec_legacy_%x", sum[:12])
}

// backfillExecutionLedger is restart-safe: legacy attempts keep their original
// rows while the deterministic compatibility projection is inserted once.
type legacyExecutionAttempt struct{ id, project, record, item, runner, session, parent, childType, started string }

func (s *RuntimeStore) backfillExecutionLedger() error {
	rows, err := s.query(`SELECT attempt_id, project_id, record_id, item_id, runner, session_ref, parent_attempt_id, child_type, started_at FROM attempts ORDER BY started_at, attempt_id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var attempts []legacyExecutionAttempt
	for rows.Next() {
		var a legacyExecutionAttempt
		if err := rows.Scan(&a.id, &a.project, &a.record, &a.item, &a.runner, &a.session, &a.parent, &a.childType, &a.started); err != nil {
			return err
		}
		attempts = append(attempts, a)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return s.withBusyRetry(func() error {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		byID := map[string]legacyExecutionAttempt{}
		for _, a := range attempts {
			byID[a.id] = a
		}
		var rootFor func(string, map[string]bool) (string, error)
		rootFor = func(id string, seen map[string]bool) (string, error) {
			a, ok := byID[id]
			if !ok || a.parent == "" {
				return id, nil
			}
			if seen[id] {
				return "", tuskerError(errorInvalidArg, "legacy attempt relationship cycle")
			}
			seen[id] = true
			return rootFor(a.parent, seen)
		}
		for _, a := range attempts {
			if a.parent != "" {
				if _, present := byID[a.parent]; !present {
					placeholder := ExecutionRecord{ExecutionID: legacyExecutionID(a.parent), RootExecutionID: legacyExecutionID(a.parent), ProjectID: a.project, NodeKind: ExecutionNodeRoot, DisplayName: "legacy parent " + a.parent, Source: "legacy_missing_parent", Creator: "migration:execution-ledger", CreatedAt: firstNonEmpty(a.started, executionNow())}
					placeholder.SearchLabel = normalizeExecutionLabel(placeholder.DisplayName, placeholder.ExecutionID)
					if err := insertLegacyExecutionTx(tx, placeholder, ""); err != nil {
						return err
					}
				}
			}
			root, err := rootFor(a.id, map[string]bool{})
			if err != nil {
				return err
			}
			kind := ExecutionNodeRoot
			if a.parent != "" {
				kind = ExecutionNodeManagedAttempt
			}
			record := ExecutionRecord{ExecutionID: legacyExecutionID(a.id), RootExecutionID: legacyExecutionID(root), ParentExecutionID: legacyExecutionID(a.parent), ProjectID: a.project, NodeKind: kind, TaskID: a.item, AttemptID: a.id, SessionRef: a.session, Source: "legacy_attempt", Provider: a.runner, AgentType: a.childType, Creator: "migration:execution-ledger", CreatedAt: firstNonEmpty(a.started, executionNow())}
			if kind == ExecutionNodeRoot {
				record.ParentExecutionID = ""
			}
			record.SearchLabel = normalizeExecutionLabel(record.TaskID, record.AgentType, record.Provider, record.ExecutionID)
			if err := insertLegacyExecutionTx(tx, record, a.parent); err != nil {
				return err
			}
		}
		return tx.Commit()
	})
}

func insertLegacyExecutionTx(tx *sql.Tx, record ExecutionRecord, parentAttemptID string) error {
	_, err := tx.Exec(`INSERT INTO execution_records (execution_id, root_execution_id, parent_execution_id, project_id, node_kind, display_name, search_label, task_id, wave_id, wave_authorization_generation, attempt_id, session_ref, source, provider, provider_session_id, agent_type, provider_child_handle, creator, lease_generation, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(execution_id) DO NOTHING`, record.ExecutionID, record.RootExecutionID, record.ParentExecutionID, record.ProjectID, record.NodeKind, record.DisplayName, record.SearchLabel, record.TaskID, record.WaveID, record.WaveGeneration, record.AttemptID, record.SessionRef, record.Source, record.Provider, record.ProviderSessionID, record.AgentType, record.ProviderChildHandle, record.Creator, record.LeaseGeneration, record.CreatedAt)
	if err != nil {
		return err
	}
	var existing ExecutionRecord
	if err := tx.QueryRow(`SELECT execution_id, root_execution_id, parent_execution_id, project_id, node_kind, display_name, search_label, task_id, wave_id, wave_authorization_generation, attempt_id, session_ref, source, provider, provider_session_id, agent_type, provider_child_handle, creator, lease_generation, created_at FROM execution_records WHERE execution_id = ?`, record.ExecutionID).Scan(executionScanDest(&existing)...); err != nil {
		return err
	}
	// Every ledger field is immutable identity or an immutable correlation fact.
	// Backfill has no enrichment exception: accepting a same-project/attempt row
	// with different metadata would make restart replay silently rewrite lineage.
	if existing != record {
		return tuskerError(errorInvalidArg, "execution ledger backfill conflict")
	}
	if parentAttemptID != "" {
		_, err = tx.Exec(`INSERT INTO execution_edges (parent_execution_id, child_execution_id, kind, created_at) VALUES (?, ?, ?, ?) ON CONFLICT(parent_execution_id, child_execution_id) DO NOTHING`, legacyExecutionID(parentAttemptID), record.ExecutionID, ExecutionManagedChildOf, record.CreatedAt)
		if err != nil {
			return err
		}
		var existingKind string
		if err := tx.QueryRow(`SELECT kind FROM execution_edges WHERE child_execution_id = ?`, record.ExecutionID).Scan(&existingKind); err != nil && err != sql.ErrNoRows {
			return err
		}
		if existingKind != "" && existingKind != string(ExecutionManagedChildOf) {
			return tuskerError(errorInvalidArg, "execution ledger backfill edge conflict")
		}
	}
	return nil
}
