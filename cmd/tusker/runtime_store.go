package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	moderncsqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

type RuntimeStore struct {
	db        *sql.DB
	stateRoot string
}

type GateLedgerEntry struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	TreeHash  string `json:"tree_hash"`
	Command   string `json:"command"`
	Profile   string `json:"profile"`
	// Toolchain is the deterministic fingerprint of the executables and
	// versions that produced this proof. An empty value is legacy evidence and
	// intentionally never satisfies a toolchain-bound lookup.
	Toolchain  string `json:"toolchain"`
	Host       string `json:"host"`
	DurationMS int64  `json:"duration_ms"`
	PassedAt   string `json:"passed_at"`
	// ProviderReceipt is the certified lifecycle identity for a green full
	// promotion gate. It is intentionally structured rather than an artifact
	// annotation, so reuse can be tied to the provider's measured scope.
	ProviderReceipt *GateProviderReceipt `json:"provider_receipt,omitempty"`
}

type GateProviderReceipt struct {
	Schema                string `json:"schema"`
	Outcome               string `json:"outcome"`
	ProjectID             string `json:"project_id"`
	DepartureID           string `json:"departure_id"`
	RequestDigest         string `json:"request_digest"`
	CandidateDigest       string `json:"candidate_digest"`
	CommandDigest         string `json:"command_digest"`
	Profile               string `json:"profile"`
	ProviderProfile       string `json:"provider_profile"`
	Toolchain             string `json:"toolchain"`
	ProviderDigest        string `json:"provider_digest"`
	ProviderClosureDigest string `json:"provider_closure_digest"`
	ClientDigest          string `json:"client_digest"`
	LifecycleID           string `json:"lifecycle_id"`
	ReceiptDigest         string `json:"receipt_digest"`
	RuntimeDigest         string `json:"runtime_digest"`
	PolicyDigest          string `json:"policy_digest"`
	AttestationDigest     string `json:"attestation_digest"`
	ImageOrVMID           string `json:"image_or_vm_id"`
	CapabilitiesDigest    string `json:"capabilities_digest"`
	ContainmentDigest     string `json:"containment_digest"`
	CleanupDigest         string `json:"cleanup_digest"`
	ResultDigest          string `json:"result_digest"`
	OutputDigest          string `json:"output_digest"`
	CleanupCertified      bool   `json:"cleanup_certified"`
}

func (s *RuntimeStore) LatestGateLedgerBefore(projectID, command, profile, toolchain, before string) (*GateLedgerEntry, error) {
	var entry GateLedgerEntry
	if strings.TrimSpace(toolchain) == "" {
		return nil, nil
	}
	var receiptJSON string
	err := s.queryRowScan(`SELECT id, project_id, tree_hash, command, profile, toolchain, host, duration_ms, passed_at, provider_receipt_json FROM gate_ledger WHERE project_id = ? AND command = ? AND profile = ? AND toolchain = ? AND passed_at < ? ORDER BY passed_at DESC LIMIT 1`, []any{projectID, command, profile, toolchain, before}, &entry.ID, &entry.ProjectID, &entry.TreeHash, &entry.Command, &entry.Profile, &entry.Toolchain, &entry.Host, &entry.DurationMS, &entry.PassedAt, &receiptJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := decodeGateProviderReceipt(receiptJSON, &entry); err != nil {
		return nil, err
	}
	return &entry, nil
}

// LatestCompleteGateLedgerBefore returns the newest tree for which every
// command in the declared full gate has a green ledger entry before `before`.
func (s *RuntimeStore) LatestCompleteGateLedgerBefore(projectID string, commands []string, profile, toolchain, before string) (*GateLedgerEntry, error) {
	cutoff, err := time.Parse(time.RFC3339Nano, before)
	if err != nil {
		return nil, err
	}
	need := map[string]bool{}
	for _, command := range commands {
		if command = strings.TrimSpace(command); command != "" {
			need[command] = true
		}
	}
	if len(need) == 0 {
		return nil, nil
	}
	if strings.TrimSpace(toolchain) == "" {
		return nil, nil
	}
	rows, err := s.query(`SELECT id, project_id, tree_hash, command, profile, toolchain, host, duration_ms, passed_at, provider_receipt_json FROM gate_ledger WHERE project_id = ? AND profile = ? AND toolchain = ?`, projectID, profile, toolchain)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type bucket struct {
		seen   map[string]bool
		latest GateLedgerEntry
		at     time.Time
	}
	buckets := map[string]*bucket{}
	for rows.Next() {
		var entry GateLedgerEntry
		var receiptJSON string
		if err := rows.Scan(&entry.ID, &entry.ProjectID, &entry.TreeHash, &entry.Command, &entry.Profile, &entry.Toolchain, &entry.Host, &entry.DurationMS, &entry.PassedAt, &receiptJSON); err != nil {
			return nil, err
		}
		if err := decodeGateProviderReceipt(receiptJSON, &entry); err != nil {
			return nil, err
		}
		at, parseErr := time.Parse(time.RFC3339Nano, entry.PassedAt)
		if parseErr != nil || !at.Before(cutoff) || !need[entry.Command] {
			continue
		}
		b := buckets[entry.TreeHash]
		if b == nil {
			b = &bucket{seen: map[string]bool{}}
			buckets[entry.TreeHash] = b
		}
		b.seen[entry.Command] = true
		if at.After(b.at) {
			b.latest, b.at = entry, at
		}
	}
	var best *bucket
	for _, b := range buckets {
		if len(b.seen) != len(need) {
			continue
		}
		if best == nil || b.at.After(best.at) {
			best = b
		}
	}
	if best == nil {
		return nil, nil
	}
	return &best.latest, nil
}

type BatchGateRun struct {
	ID           string `json:"id"`
	ProjectID    string `json:"project_id"`
	TreeHash     string `json:"tree_hash"`
	Profile      string `json:"profile"`
	CommandsJSON string `json:"commands_json"`
	Status       string `json:"status"`
	StartedAt    string `json:"started_at"`
	FinishedAt   string `json:"finished_at"`
	FirstFailure string `json:"first_failure"`
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
	ProjectID          string                     `json:"project_id"`
	RecordID           string                     `json:"record_id"`
	ItemID             string                     `json:"item_id"`
	Runner             string                     `json:"runner"`
	RunnerProfile      string                     `json:"runner_profile"`
	RunnerHarness      string                     `json:"runner_harness"`
	RunnerModel        string                     `json:"runner_model"`
	RunnerEffort       string                     `json:"runner_effort"`
	WorkerPolicyFP     string                     `json:"worker_policy_fingerprint"`
	ExecutePolicyFP    string                     `json:"execute_policy_fingerprint"`
	Lane               string                     `json:"lane"`
	LeaseState         string                     `json:"lease_state"`
	LeaseOwner         string                     `json:"lease_owner"`
	LeaseGeneration    int                        `json:"lease_generation"`
	LeaseExpiresAt     string                     `json:"lease_expires_at"`
	LeaseHost          string                     `json:"lease_host"`
	AttemptOutcome     string                     `json:"attempt_outcome"`
	ActiveAttemptID    string                     `json:"active_attempt_id"`
	WorkspacePath      string                     `json:"workspace_path"`
	SessionRef         string                     `json:"session_ref"`
	CloudTaskID        string                     `json:"cloud_task_id"`
	CloudStatus        string                     `json:"cloud_status"`
	CloudEnvironmentID string                     `json:"cloud_environment_id"`
	CloudAttemptNumber int                        `json:"cloud_attempt_number"`
	PullRequestURL     string                     `json:"pull_request_url"`
	ApplyRef           string                     `json:"apply_ref"`
	LogsSummary        string                     `json:"logs_summary"`
	FinalSummary       string                     `json:"final_summary"`
	ProcessPID         int                        `json:"process_pid"`
	ProcessPGID        int                        `json:"process_pgid"`
	ProcessStartedAt   string                     `json:"process_started_at"`
	PromptPath         string                     `json:"prompt_path"`
	EventSinkPath      string                     `json:"event_sink_path"`
	RawLogPath         string                     `json:"raw_log_path"`
	StatusPath         string                     `json:"status_path"`
	WorkRevision       int                        `json:"work_revision"`
	AttemptCount       int                        `json:"attempt_count"`
	NextRetryAt        string                     `json:"next_retry_at"`
	LastError          string                     `json:"last_error"`
	Infrastructure     *RunnerInfrastructureBlock `json:"infrastructure,omitempty"`
	LastEventAt        string                     `json:"last_event_at"`
	FirstEventAt       string                     `json:"first_event_at"`
	LastHeartbeatAt    string                     `json:"last_heartbeat_at"`
	Terminal           bool                       `json:"terminal"`
	StartedAt          string                     `json:"started_at"`
	UpdatedAt          string                     `json:"updated_at"`
	// HandRun records, per RUN, whether this claim was made by hand in a live
	// interactive session rather than handed out by the automation daemon. It is
	// stamped once at claim time (ClaimRunLease) and preserved by later upserts,
	// so a historical row keeps the origin of the claim that produced it even
	// after the same task is later re-claimed by a different origin.
	HandRun bool `json:"hand_run"`
	// HandRunStamped is true when the hand_run column carried an explicit value
	// (i.e. the run was claimed after the field existed). When false the origin is
	// unknown and callers fall back to the legacy task-keyed marker file.
	HandRunStamped bool `json:"-"`
}

type RunAuthorization struct {
	ProjectID                string `json:"project_id"`
	RecordID                 string `json:"record_id"`
	LeaseGeneration          int    `json:"lease_generation"`
	Source                   string `json:"source"`
	Actor                    string `json:"actor"`
	Trigger                  string `json:"trigger"`
	ProjectAutomationEnabled bool   `json:"project_automation_enabled"`
	CreatedAt                string `json:"created_at"`
}

// RunDirective is a durable, human-authorized request for the resident daemon
// to execute one named task once. It is deliberately separate from a lease:
// only the daemon may consume it into a normal claim/attempt.
type RunDirective struct {
	ProjectID string `json:"project_id"`
	RecordID  string `json:"record_id"`
	Actor     string `json:"actor"`
	CreatedAt string `json:"created_at"`
	ExpiresAt string `json:"expires_at"`
	State     string `json:"state"`
	Reason    string `json:"reason"`
}

type RunIdentityMetadata struct {
	ProjectID     string `json:"project_id"`
	RecordID      string `json:"record_id"`
	RepoRoot      string `json:"registered_repo_path"`
	WorkspacePath string `json:"workspace_path"`
	WorkspaceMode string `json:"workspace_mode"`
	Runner        string `json:"runner"`
	Branch        string `json:"branch,omitempty"`
	Head          string `json:"head,omitempty"`
	CreatedAt     string `json:"created_at"`
}

type RunAttempt struct {
	AttemptID          string
	ProjectID          string
	RecordID           string
	ItemID             string
	Runner             string
	Lane               string
	WorkerPolicyFP     string
	WorkRevision       int
	WorkspacePath      string
	SessionRef         string
	ParentAttemptID    string
	ChildType          string
	BranchName         string
	MergeRule          string
	FanoutGroup        string
	CloudTaskID        string
	CloudStatus        string
	CloudEnvironmentID string
	CloudAttemptNumber int
	PullRequestURL     string
	ApplyRef           string
	LogsSummary        string
	FinalSummary       string
	EndStateJSON       string      `json:"-"`
	EndState           RunEndState `json:"end_state,omitempty"`
	Outcome            string
	ExitCode           int
	TurnsUsed          int
	PromptPath         string
	EventSinkPath      string
	RawLogPath         string
	StatusPath         string
	ProcessPID         int
	LastError          string
	StartedAt          string
	FinishedAt         string
}

// ReviewResult is the sole durable reviewer lifecycle output. It is keyed by
// the reviewed task revision and reviewer attempt, so prose and process exit
// cannot be mistaken for acceptance.
type ReviewResult struct {
	Schema            string   `json:"schema"`
	ProjectID         string   `json:"project_id"`
	TaskID            string   `json:"task_id"`
	TaskStateRev      string   `json:"task_state_rev"`
	WorkRevision      int      `json:"work_revision"`
	ImplementationSHA string   `json:"implementation_sha"`
	AttemptID         string   `json:"attempt_id"`
	Actor             string   `json:"actor"`
	Runner            string   `json:"runner"`
	RunnerProfile     string   `json:"runner_profile"`
	WorkerPolicyFP    string   `json:"worker_policy_fingerprint"`
	Covers            []string `json:"covers"`
	ProofFingerprint  string   `json:"proof_fingerprint"`
	GateFingerprint   string   `json:"gate_fingerprint"`
	Verdict           string   `json:"verdict"`
	Blocker           string   `json:"blocker,omitempty"`
	Summary           string   `json:"summary"`
	Findings          []string `json:"findings,omitempty"`
	EvidenceRefs      []string `json:"evidence_refs,omitempty"`
	ResultRevision    string   `json:"result_revision"`
	CreatedAt         string   `json:"created_at"`
}

func (s *RuntimeStore) ReviewAttempt(attemptID string) (RunAttempt, error) {
	var attempt RunAttempt
	err := s.queryRowScan(`SELECT attempt_id, project_id, record_id, runner, lane, worker_policy_fingerprint, work_revision FROM attempts WHERE attempt_id = ?`, []any{attemptID}, &attempt.AttemptID, &attempt.ProjectID, &attempt.RecordID, &attempt.Runner, &attempt.Lane, &attempt.WorkerPolicyFP, &attempt.WorkRevision)
	return attempt, err
}

type RunEndState struct {
	Schema          string            `json:"schema"`
	Branch          string            `json:"branch"`
	HeadSHA         string            `json:"head_sha"`
	WorktreePath    string            `json:"worktree_path"`
	Dirty           bool              `json:"dirty"`
	GateVerdicts    map[string]string `json:"gate_verdicts"`
	ReportedBranch  string            `json:"reported_branch,omitempty"`
	ReportedHeadSHA string            `json:"reported_head_sha,omitempty"`
	Discrepancies   []string          `json:"discrepancies,omitempty"`
	CapturedAt      string            `json:"captured_at"`
}

type RuntimeApplyInput struct {
	ProjectID string `json:"project_id"`
	RecordID  string `json:"record_id"`
	ItemID    string `json:"item_id"`
	Runner    string `json:"runner"`
	JobID     string `json:"job_id"`
	AttemptID string `json:"attempt_id"`
	Path      string `json:"path"`
	RelPath   string `json:"rel_path"`
	Sha256    string `json:"sha256"`
	Kind      string `json:"kind"`
	CreatedAt string `json:"created_at"`
}

type RunTurn struct {
	AttemptID       string `json:"attempt_id"`
	ProjectID       string `json:"project_id"`
	RecordID        string `json:"record_id"`
	TurnID          string `json:"turn_id"`
	TurnIndex       int    `json:"turn_index"`
	SessionRef      string `json:"session_ref"`
	Status          string `json:"status"`
	InputTokens     int    `json:"input_tokens"`
	OutputTokens    int    `json:"output_tokens"`
	TotalTokens     int    `json:"total_tokens"`
	LeaseGeneration int    `json:"lease_generation,omitempty"`
	StartedAt       string `json:"started_at"`
	CompletedAt     string `json:"completed_at"`
	LastEventAt     string `json:"last_event_at"`
	LastError       string `json:"last_error"`
}

type SupervisorDecisionKind string

const (
	SupervisorDecisionContinueThread  SupervisorDecisionKind = "continue_thread"
	SupervisorDecisionContinueAttempt SupervisorDecisionKind = "continue_attempt"
	SupervisorDecisionResumeSession   SupervisorDecisionKind = "resume_session"
	SupervisorDecisionForkThread      SupervisorDecisionKind = "fork_thread"
	SupervisorDecisionNewBranch       SupervisorDecisionKind = "new_branch"
	SupervisorDecisionNewRevision     SupervisorDecisionKind = "new_revision"
	SupervisorDecisionRedrive         SupervisorDecisionKind = "redrive"
	SupervisorDecisionStopForAudit    SupervisorDecisionKind = "stop_for_audit"
	SupervisorDecisionStopForHuman    SupervisorDecisionKind = "stop_for_human"

	supervisorDecisionContinueThread  = string(SupervisorDecisionContinueThread)
	supervisorDecisionContinueAttempt = string(SupervisorDecisionContinueAttempt)
	supervisorDecisionResumeSession   = string(SupervisorDecisionResumeSession)
	supervisorDecisionForkThread      = string(SupervisorDecisionForkThread)
	supervisorDecisionNewBranch       = string(SupervisorDecisionNewBranch)
	supervisorDecisionNewRevision     = string(SupervisorDecisionNewRevision)
	supervisorDecisionRedrive         = string(SupervisorDecisionRedrive)
	supervisorDecisionStopForAudit    = string(SupervisorDecisionStopForAudit)
	supervisorDecisionStopForHuman    = string(SupervisorDecisionStopForHuman)
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

type ExternalLoopEvent struct {
	EventID        string `json:"event_id"`
	ProjectID      string `json:"project_id"`
	RecordID       string `json:"record_id"`
	ItemID         string `json:"item_id"`
	Runner         string `json:"runner"`
	JobID          string `json:"job_id"`
	AttemptID      string `json:"attempt_id"`
	Stage          string `json:"stage"`
	Action         string `json:"action"`
	Status         string `json:"status"`
	Reason         string `json:"reason"`
	PayloadJSON    string `json:"payload_json,omitempty"`
	IdempotencyKey string `json:"idempotency_key"`
	CreatedAt      string `json:"created_at"`
}

type ExternalLoopCaps struct {
	MaxCycles              int `json:"max_cycles"`
	MaxRepairContinuations int `json:"max_repair_continuations"`
	MaxExternalThreads     int `json:"max_external_threads"`
	WallClockTimeoutHours  int `json:"wall_clock_timeout_hours"`
}

type ExternalLoopCounters struct {
	Events              int      `json:"events"`
	Cycles              int      `json:"cycles"`
	RepairContinuations int      `json:"repair_continuations"`
	ExternalThreads     int      `json:"external_threads"`
	DistinctJobIDs      []string `json:"distinct_job_ids"`
}

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
	dbPath := runtimeStoreDBPath(stateRoot)
	db, err := sql.Open("sqlite", runtimeStoreSQLiteDSN(dbPath, runtimeStoreBusyTimeout))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &RuntimeStore{db: db, stateRoot: stateRoot}
	if err := store.Migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := store.ReconcileDuplicateProjects(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := store.EnsureProjectUniqueness(); err != nil {
		_ = db.Close()
		return nil, err
	}
	// Resource leases are daemon-wide rather than project-local lock files.
	// Reclaim only leases whose own expiry policy has elapsed; a caller with a
	// stronger liveness probe may keep an expired-but-live holder fenced.
	if _, err := store.ReconcileExpiredResourceLeases(time.Now().UTC(), nil); err != nil {
		_ = db.Close()
		return nil, err
	}
	// Interrupted departure ref intents are reconciled only after the caller
	// supplies the matching registered project. Ref names are project-local;
	// store-only startup must not classify one against an unrelated repository.
	return store, nil
}

// OpenRuntimeStoreReadOnly opens an existing runtime database without creating
// directories, running migrations, reconciling rows, or taking a write lock.
// Diagnostic commands must use this path so observation cannot change state.
func OpenRuntimeStoreReadOnly(stateRoot string) (*RuntimeStore, error) {
	dbPath := runtimeStoreDBPath(stateRoot)
	if !fileExists(dbPath) {
		return nil, os.ErrNotExist
	}
	u := url.URL{Scheme: "file", Path: dbPath}
	q := u.Query()
	q.Set("mode", "ro")
	q.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", int(runtimeStoreBusyTimeout/time.Millisecond)))
	q.Add("_pragma", "query_only(1)")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &RuntimeStore{db: db, stateRoot: stateRoot}, nil
}

var (
	runtimeStoreBusyTimeout      = 1500 * time.Millisecond
	runtimeStoreBusyRetryLimit   = 5 * time.Second
	runtimeStoreBusyRetryBackoff = []time.Duration{10 * time.Millisecond, 25 * time.Millisecond, 50 * time.Millisecond, 100 * time.Millisecond, 200 * time.Millisecond}
)

const (
	defaultRunHeartbeatInterval = 15 * time.Second
	defaultRunLeaseTTL          = 60 * time.Second
	defaultReconcileTick        = 60 * time.Second
)

func runtimeStoreDBPath(stateRoot string) string {
	return filepath.Join(stateRoot, "daemon.db")
}

func runtimeStoreSQLiteDSN(dbPath string, busyTimeout time.Duration) string {
	if abs, err := filepath.Abs(dbPath); err == nil {
		dbPath = abs
	}
	u := url.URL{Scheme: "file", Path: dbPath}
	q := u.Query()
	q.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", int(busyTimeout/time.Millisecond)))
	u.RawQuery = q.Encode()
	return u.String()
}

func runtimeLeaseHost() string {
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		return "localhost"
	}
	return host
}

func (s *RuntimeStore) exec(query string, args ...any) (sql.Result, error) {
	var result sql.Result
	err := s.withBusyRetry(func() error {
		var err error
		result, err = s.db.Exec(query, args...)
		return err
	})
	return result, err
}

func (s *RuntimeStore) query(query string, args ...any) (*sql.Rows, error) {
	var rows *sql.Rows
	err := s.withBusyRetry(func() error {
		var err error
		rows, err = s.db.Query(query, args...)
		return err
	})
	return rows, err
}

func (s *RuntimeStore) queryRowScan(query string, args []any, dest ...any) error {
	return s.withBusyRetry(func() error {
		return s.db.QueryRow(query, args...).Scan(dest...)
	})
}

func (s *RuntimeStore) withBusyRetry(op func() error) error {
	deadline := time.Now().Add(runtimeStoreBusyRetryLimit)
	var lastErr error
	for attempt := 0; ; attempt++ {
		err := op()
		if !isRuntimeStoreBusy(err) {
			return err
		}
		lastErr = err
		if runtimeStoreBusyRetryLimit <= 0 || !time.Now().Before(deadline) {
			return lastErr
		}
		backoff := runtimeStoreBusyRetryDelay(attempt)
		if remaining := time.Until(deadline); remaining <= 0 {
			return lastErr
		} else if backoff > remaining {
			backoff = remaining
		}
		time.Sleep(backoff)
	}
}

func runtimeStoreBusyRetryDelay(attempt int) time.Duration {
	if len(runtimeStoreBusyRetryBackoff) == 0 {
		return 10 * time.Millisecond
	}
	if attempt < len(runtimeStoreBusyRetryBackoff) {
		return runtimeStoreBusyRetryBackoff[attempt]
	}
	return runtimeStoreBusyRetryBackoff[len(runtimeStoreBusyRetryBackoff)-1]
}

func isRuntimeStoreBusy(err error) bool {
	if err == nil {
		return false
	}
	var sqliteErr *moderncsqlite.Error
	if errors.As(err, &sqliteErr) {
		switch sqliteErr.Code() & 0xff {
		case sqlite3.SQLITE_BUSY, sqlite3.SQLITE_LOCKED:
			return true
		}
	}
	text := err.Error()
	return strings.Contains(text, "SQLITE_BUSY") || strings.Contains(text, "database is locked")
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
			runner_profile TEXT NOT NULL DEFAULT '',
			runner_harness TEXT NOT NULL DEFAULT '',
			runner_model TEXT NOT NULL DEFAULT '',
			runner_effort TEXT NOT NULL DEFAULT '',
			worker_policy_fingerprint TEXT NOT NULL DEFAULT '',
			execute_policy_fingerprint TEXT NOT NULL DEFAULT '',
			lane TEXT NOT NULL DEFAULT '',
			lease_state TEXT NOT NULL DEFAULT 'unclaimed',
			lease_owner TEXT NOT NULL DEFAULT '',
			lease_generation INTEGER NOT NULL DEFAULT 0,
			lease_expires_at TEXT NOT NULL DEFAULT '',
			lease_host TEXT NOT NULL DEFAULT '',
			attempt_outcome TEXT NOT NULL DEFAULT 'none',
			active_attempt_id TEXT NOT NULL DEFAULT '',
			workspace_path TEXT NOT NULL DEFAULT '',
			session_ref TEXT NOT NULL DEFAULT '',
			parent_attempt_id TEXT NOT NULL DEFAULT '',
			child_type TEXT NOT NULL DEFAULT '',
			branch_name TEXT NOT NULL DEFAULT '',
			merge_rule TEXT NOT NULL DEFAULT '',
			fanout_group TEXT NOT NULL DEFAULT '',
			cloud_task_id TEXT NOT NULL DEFAULT '',
			cloud_status TEXT NOT NULL DEFAULT '',
			cloud_environment_id TEXT NOT NULL DEFAULT '',
			cloud_attempt_number INTEGER NOT NULL DEFAULT 0,
			pull_request_url TEXT NOT NULL DEFAULT '',
			apply_ref TEXT NOT NULL DEFAULT '',
			logs_summary TEXT NOT NULL DEFAULT '',
			final_summary TEXT NOT NULL DEFAULT '',
			process_pid INTEGER NOT NULL DEFAULT 0,
			process_pgid INTEGER NOT NULL DEFAULT 0,
			process_started_at TEXT NOT NULL DEFAULT '',
			prompt_path TEXT NOT NULL DEFAULT '',
			event_sink_path TEXT NOT NULL DEFAULT '',
			raw_log_path TEXT NOT NULL DEFAULT '',
			status_path TEXT NOT NULL DEFAULT '',
			work_revision INTEGER NOT NULL DEFAULT 0,
			attempt_count INTEGER NOT NULL DEFAULT 0,
			next_retry_at TEXT NOT NULL DEFAULT '',
			last_error TEXT NOT NULL DEFAULT '',
			infrastructure_json TEXT NOT NULL DEFAULT '',
			last_event_at TEXT NOT NULL DEFAULT '',
			first_event_at TEXT NOT NULL DEFAULT '',
			last_heartbeat_at TEXT NOT NULL DEFAULT '',
			terminal INTEGER NOT NULL DEFAULT 0,
			started_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT '',
			PRIMARY KEY(project_id, record_id)
		);`,
		`CREATE TABLE IF NOT EXISTS run_authorizations (
			project_id TEXT NOT NULL,
			record_id TEXT NOT NULL,
			lease_generation INTEGER NOT NULL,
			source TEXT NOT NULL,
			actor TEXT NOT NULL,
			trigger TEXT NOT NULL DEFAULT '',
			project_automation_enabled INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			PRIMARY KEY(project_id, record_id, lease_generation)
		);`,
		`CREATE TABLE IF NOT EXISTS run_directives (
			project_id TEXT NOT NULL,
			record_id TEXT NOT NULL,
			actor TEXT NOT NULL,
			created_at TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			state TEXT NOT NULL DEFAULT 'queued',
			reason TEXT NOT NULL DEFAULT '',
			PRIMARY KEY(project_id, record_id)
		);`,
		`CREATE TABLE IF NOT EXISTS run_identity_metadata (
			project_id TEXT NOT NULL,
			record_id TEXT NOT NULL,
			repo_root TEXT NOT NULL,
			workspace_path TEXT NOT NULL,
			workspace_mode TEXT NOT NULL,
			runner TEXT NOT NULL,
			branch TEXT NOT NULL DEFAULT '',
			head TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			PRIMARY KEY(project_id, record_id)
		);`,
		`CREATE TABLE IF NOT EXISTS attempts (
			attempt_id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			record_id TEXT NOT NULL,
			item_id TEXT NOT NULL DEFAULT '',
			runner TEXT NOT NULL DEFAULT '',
			lane TEXT NOT NULL DEFAULT '',
			worker_policy_fingerprint TEXT NOT NULL DEFAULT '',
			work_revision INTEGER NOT NULL DEFAULT 0,
			workspace_path TEXT NOT NULL DEFAULT '',
			session_ref TEXT NOT NULL DEFAULT '',
			cloud_task_id TEXT NOT NULL DEFAULT '',
			cloud_status TEXT NOT NULL DEFAULT '',
			cloud_environment_id TEXT NOT NULL DEFAULT '',
			cloud_attempt_number INTEGER NOT NULL DEFAULT 0,
			pull_request_url TEXT NOT NULL DEFAULT '',
			apply_ref TEXT NOT NULL DEFAULT '',
			logs_summary TEXT NOT NULL DEFAULT '',
			final_summary TEXT NOT NULL DEFAULT '',
			end_state_json TEXT NOT NULL DEFAULT '',
			process_pid INTEGER NOT NULL DEFAULT 0,
			outcome TEXT NOT NULL DEFAULT 'none',
			exit_code INTEGER NOT NULL DEFAULT 0,
			turns_used INTEGER NOT NULL DEFAULT 0,
			prompt_path TEXT NOT NULL DEFAULT '',
			event_sink_path TEXT NOT NULL DEFAULT '',
			raw_log_path TEXT NOT NULL DEFAULT '',
			status_path TEXT NOT NULL DEFAULT '',
			last_error TEXT NOT NULL DEFAULT '',
			started_at TEXT NOT NULL DEFAULT '',
			finished_at TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE TABLE IF NOT EXISTS review_results (project_id TEXT NOT NULL, task_id TEXT NOT NULL, work_revision INTEGER NOT NULL, attempt_id TEXT NOT NULL, result_json TEXT NOT NULL, PRIMARY KEY(project_id, task_id, work_revision, attempt_id));`,
		// Completion transactions are intentionally independent of mutable task
		// branches.  The identity is a frozen review handoff plus the exact
		// integration tip from which it was staged; replay resumes this row.
		`CREATE TABLE IF NOT EXISTS completion_transactions (transaction_id TEXT PRIMARY KEY, project_id TEXT NOT NULL, task_id TEXT NOT NULL, result_revision TEXT NOT NULL, phase TEXT NOT NULL, transaction_json TEXT NOT NULL, updated_at TEXT NOT NULL);`,
		// Completion issuances deliberately preserve only a public verifier and
		// signed context. The private Ed25519 capability stays in the resident
		// daemon process; a worker which can write Git refs cannot mint a close.
		`CREATE TABLE IF NOT EXISTS completion_authority_issuances (
			authority_id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			store_identity TEXT NOT NULL,
			repo_identity TEXT NOT NULL,
			transaction_id TEXT NOT NULL,
			context_json TEXT NOT NULL,
			public_key BLOB NOT NULL,
			issued_at TEXT NOT NULL,
			bound_at TEXT NOT NULL DEFAULT '',
			consumed_at TEXT NOT NULL DEFAULT '',
			revoked_at TEXT NOT NULL DEFAULT '',
			UNIQUE(project_id, transaction_id)
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
		`CREATE TABLE IF NOT EXISTS apply_inputs (
			project_id TEXT NOT NULL,
			record_id TEXT NOT NULL,
			item_id TEXT NOT NULL DEFAULT '',
			runner TEXT NOT NULL DEFAULT '',
			job_id TEXT NOT NULL DEFAULT '',
			attempt_id TEXT NOT NULL DEFAULT '',
			path TEXT NOT NULL,
			rel_path TEXT NOT NULL DEFAULT '',
			sha256 TEXT NOT NULL DEFAULT '',
			kind TEXT NOT NULL DEFAULT 'patch',
			created_at TEXT NOT NULL DEFAULT '',
			PRIMARY KEY(project_id, record_id, sha256, path)
		);`,
		`CREATE TABLE IF NOT EXISTS daemon_settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE TABLE IF NOT EXISTS gate_ledger (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			tree_hash TEXT NOT NULL,
			command TEXT NOT NULL,
			profile TEXT NOT NULL DEFAULT '',
			toolchain TEXT NOT NULL DEFAULT '',
			host TEXT NOT NULL DEFAULT '',
			duration_ms INTEGER NOT NULL DEFAULT 0,
			passed_at TEXT NOT NULL,
			provider_receipt_json TEXT NOT NULL DEFAULT '',
			UNIQUE(project_id, tree_hash, command, profile, toolchain)
		);`,
		`CREATE TABLE IF NOT EXISTS batch_gate_runs (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			tree_hash TEXT NOT NULL DEFAULT '',
			profile TEXT NOT NULL DEFAULT '',
			commands_json TEXT NOT NULL DEFAULT '[]',
			status TEXT NOT NULL,
			started_at TEXT NOT NULL,
			finished_at TEXT NOT NULL DEFAULT '',
			first_failure TEXT NOT NULL DEFAULT ''
		);`,
		// resource_leases is intentionally global: resource_name is the primary
		// key, so two registered projects contend through this one daemon DB.
		// Rows are retained after release for generation fencing and recovery
		// audit; resource_lease_events records every attributable takeover.
		`CREATE TABLE IF NOT EXISTS resource_leases (
			resource_name TEXT PRIMARY KEY,
			owner TEXT NOT NULL DEFAULT '',
			purpose TEXT NOT NULL DEFAULT '',
			project_id TEXT NOT NULL DEFAULT '',
			departure_id TEXT NOT NULL DEFAULT '',
			heartbeat_at TEXT NOT NULL DEFAULT '',
			expires_at TEXT NOT NULL DEFAULT '',
			generation INTEGER NOT NULL DEFAULT 0,
			state TEXT NOT NULL DEFAULT 'released',
			released_at TEXT NOT NULL DEFAULT '',
			release_reason TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE TABLE IF NOT EXISTS resource_lease_events (
			event_id TEXT PRIMARY KEY,
			resource_name TEXT NOT NULL,
			event_type TEXT NOT NULL,
			owner TEXT NOT NULL DEFAULT '',
			generation INTEGER NOT NULL DEFAULT 0,
			previous_owner TEXT NOT NULL DEFAULT '',
			previous_generation INTEGER NOT NULL DEFAULT 0,
			reason TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS resource_lease_events_resource
			ON resource_lease_events(resource_name, created_at);`,
		// departure_runs deliberately keeps structured operational facts in JSON
		// columns rather than copying raw command output into the runtime DB. A
		// departure is system work, not a task attempt: its natural idempotency key
		// is the policy window, not a task record id.
		`CREATE TABLE IF NOT EXISTS departure_runs (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			policy_id TEXT NOT NULL,
			scheduled_window TEXT NOT NULL,
			state TEXT NOT NULL,
			state_revision INTEGER NOT NULL DEFAULT 1,
			candidate_json TEXT NOT NULL DEFAULT '{}',
			gate_json TEXT NOT NULL DEFAULT '{}',
			promotion_json TEXT NOT NULL DEFAULT '{}',
			release_json TEXT NOT NULL DEFAULT '{}',
			skip_reason TEXT NOT NULL DEFAULT '',
			block_reason TEXT NOT NULL DEFAULT '',
			execution_last_error TEXT NOT NULL DEFAULT '',
			execution_last_error_at TEXT NOT NULL DEFAULT '',
			execution_error_count INTEGER NOT NULL DEFAULT 0,
			model_invocation_count INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(project_id, policy_id, scheduled_window)
		);`,
		// Landing authority issuances deliberately retain public verification
		// material only. The matching private capability exists solely in the
		// resident daemon's memory and is never serialised, inherited, or exposed
		// through a CLI/API. A gate child therefore cannot mint a valid receipt by
		// editing the cache or this database.
		`CREATE TABLE IF NOT EXISTS landing_authority_issuances (
			authority_id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			repo_identity TEXT NOT NULL,
			departure_id TEXT NOT NULL,
			policy_id TEXT NOT NULL,
			scheduled_window TEXT NOT NULL,
			session_id TEXT NOT NULL,
			host_identity TEXT NOT NULL,
			process_identity TEXT NOT NULL,
			generation INTEGER NOT NULL,
			context_json TEXT NOT NULL,
			public_key BLOB NOT NULL,
			issued_at TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			revoked_at TEXT NOT NULL DEFAULT '',
			consumed_at TEXT NOT NULL DEFAULT '',
			UNIQUE(project_id, departure_id, generation)
		);`,
		`CREATE TABLE IF NOT EXISTS external_loop_events (
			event_id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			record_id TEXT NOT NULL,
			item_id TEXT NOT NULL DEFAULT '',
			runner TEXT NOT NULL DEFAULT '',
			job_id TEXT NOT NULL DEFAULT '',
			attempt_id TEXT NOT NULL DEFAULT '',
			stage TEXT NOT NULL DEFAULT '',
			action TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT '',
			reason TEXT NOT NULL DEFAULT '',
			payload_json TEXT NOT NULL DEFAULT '',
			idempotency_key TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS external_loop_events_idempotency
			ON external_loop_events(project_id, record_id, idempotency_key);`,
	}
	for _, stmt := range statements {
		if _, err := s.exec(stmt); err != nil {
			return err
		}
	}
	for _, column := range []struct {
		name string
		stmt string
	}{
		{"execution_last_error", `ALTER TABLE departure_runs ADD COLUMN execution_last_error TEXT NOT NULL DEFAULT ''`},
		{"execution_last_error_at", `ALTER TABLE departure_runs ADD COLUMN execution_last_error_at TEXT NOT NULL DEFAULT ''`},
		{"execution_error_count", `ALTER TABLE departure_runs ADD COLUMN execution_error_count INTEGER NOT NULL DEFAULT 0`},
	} {
		if err := s.ensureColumn("departure_runs", column.name, column.stmt); err != nil {
			return err
		}
	}
	if err := s.migrateGateLedgerToolchain(); err != nil {
		return err
	}
	if err := s.ensureColumn("completion_authority_issuances", "store_identity", `ALTER TABLE completion_authority_issuances ADD COLUMN store_identity TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := s.ensureColumn("gate_ledger", "provider_receipt_json", `ALTER TABLE gate_ledger ADD COLUMN provider_receipt_json TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := s.ensureColumn("runs", "work_revision", `ALTER TABLE runs ADD COLUMN work_revision INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	if err := s.ensureColumn("runs", "lane", `ALTER TABLE runs ADD COLUMN lane TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	for _, column := range []struct {
		name string
		stmt string
	}{
		{"runner_profile", `ALTER TABLE runs ADD COLUMN runner_profile TEXT NOT NULL DEFAULT ''`},
		{"runner_harness", `ALTER TABLE runs ADD COLUMN runner_harness TEXT NOT NULL DEFAULT ''`},
		{"runner_model", `ALTER TABLE runs ADD COLUMN runner_model TEXT NOT NULL DEFAULT ''`},
		{"runner_effort", `ALTER TABLE runs ADD COLUMN runner_effort TEXT NOT NULL DEFAULT ''`},
		{"worker_policy_fingerprint", `ALTER TABLE runs ADD COLUMN worker_policy_fingerprint TEXT NOT NULL DEFAULT ''`},
		{"execute_policy_fingerprint", `ALTER TABLE runs ADD COLUMN execute_policy_fingerprint TEXT NOT NULL DEFAULT ''`},
	} {
		if err := s.ensureColumn("runs", column.name, column.stmt); err != nil {
			return err
		}
	}
	if err := s.ensureColumn("runs", "active_attempt_id", `ALTER TABLE runs ADD COLUMN active_attempt_id TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := s.ensureColumn("runs", "process_pid", `ALTER TABLE runs ADD COLUMN process_pid INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	for _, column := range []struct {
		name string
		stmt string
	}{
		{"cloud_task_id", `ALTER TABLE runs ADD COLUMN cloud_task_id TEXT NOT NULL DEFAULT ''`},
		{"cloud_status", `ALTER TABLE runs ADD COLUMN cloud_status TEXT NOT NULL DEFAULT ''`},
		{"cloud_environment_id", `ALTER TABLE runs ADD COLUMN cloud_environment_id TEXT NOT NULL DEFAULT ''`},
		{"cloud_attempt_number", `ALTER TABLE runs ADD COLUMN cloud_attempt_number INTEGER NOT NULL DEFAULT 0`},
		{"pull_request_url", `ALTER TABLE runs ADD COLUMN pull_request_url TEXT NOT NULL DEFAULT ''`},
		{"apply_ref", `ALTER TABLE runs ADD COLUMN apply_ref TEXT NOT NULL DEFAULT ''`},
		{"logs_summary", `ALTER TABLE runs ADD COLUMN logs_summary TEXT NOT NULL DEFAULT ''`},
		{"final_summary", `ALTER TABLE runs ADD COLUMN final_summary TEXT NOT NULL DEFAULT ''`},
	} {
		if err := s.ensureColumn("runs", column.name, column.stmt); err != nil {
			return err
		}
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
	if err := s.ensureColumn("runs", "infrastructure_json", `ALTER TABLE runs ADD COLUMN infrastructure_json TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	for _, column := range []struct {
		name string
		stmt string
	}{
		{"lease_owner", `ALTER TABLE runs ADD COLUMN lease_owner TEXT NOT NULL DEFAULT ''`},
		{"lease_generation", `ALTER TABLE runs ADD COLUMN lease_generation INTEGER NOT NULL DEFAULT 0`},
		{"lease_expires_at", `ALTER TABLE runs ADD COLUMN lease_expires_at TEXT NOT NULL DEFAULT ''`},
		{"lease_host", `ALTER TABLE runs ADD COLUMN lease_host TEXT NOT NULL DEFAULT ''`},
	} {
		if err := s.ensureColumn("runs", column.name, column.stmt); err != nil {
			return err
		}
	}
	if err := s.ensureColumn("runs", "last_event_at", `ALTER TABLE runs ADD COLUMN last_event_at TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	for _, column := range []struct {
		name string
		stmt string
	}{
		{"process_pgid", `ALTER TABLE runs ADD COLUMN process_pgid INTEGER NOT NULL DEFAULT 0`},
		{"process_started_at", `ALTER TABLE runs ADD COLUMN process_started_at TEXT NOT NULL DEFAULT ''`},
		{"first_event_at", `ALTER TABLE runs ADD COLUMN first_event_at TEXT NOT NULL DEFAULT ''`},
		{"last_heartbeat_at", `ALTER TABLE runs ADD COLUMN last_heartbeat_at TEXT NOT NULL DEFAULT ''`},
		{"terminal", `ALTER TABLE runs ADD COLUMN terminal INTEGER NOT NULL DEFAULT 0`},
	} {
		if err := s.ensureColumn("runs", column.name, column.stmt); err != nil {
			return err
		}
	}
	// hand_run is deliberately nullable with no default: rows claimed before this
	// column existed scan back as NULL (origin unknown -> render falls back to the
	// task-keyed marker), while every claim under ClaimRunLease stamps an explicit
	// 0/1 that renders authoritatively without touching the filesystem.
	if err := s.ensureColumn("runs", "hand_run", `ALTER TABLE runs ADD COLUMN hand_run INTEGER`); err != nil {
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
	if err := s.ensureColumn("attempts", "turns_used", `ALTER TABLE attempts ADD COLUMN turns_used INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	if err := s.ensureColumn("attempts", "worker_policy_fingerprint", `ALTER TABLE attempts ADD COLUMN worker_policy_fingerprint TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	for _, column := range []struct {
		name string
		stmt string
	}{
		{"parent_attempt_id", `ALTER TABLE attempts ADD COLUMN parent_attempt_id TEXT NOT NULL DEFAULT ''`},
		{"child_type", `ALTER TABLE attempts ADD COLUMN child_type TEXT NOT NULL DEFAULT ''`},
		{"branch_name", `ALTER TABLE attempts ADD COLUMN branch_name TEXT NOT NULL DEFAULT ''`},
		{"merge_rule", `ALTER TABLE attempts ADD COLUMN merge_rule TEXT NOT NULL DEFAULT ''`},
		{"fanout_group", `ALTER TABLE attempts ADD COLUMN fanout_group TEXT NOT NULL DEFAULT ''`},
	} {
		if err := s.ensureColumn("attempts", column.name, column.stmt); err != nil {
			return err
		}
	}
	for _, column := range []struct {
		name string
		stmt string
	}{
		{"cloud_task_id", `ALTER TABLE attempts ADD COLUMN cloud_task_id TEXT NOT NULL DEFAULT ''`},
		{"cloud_status", `ALTER TABLE attempts ADD COLUMN cloud_status TEXT NOT NULL DEFAULT ''`},
		{"cloud_environment_id", `ALTER TABLE attempts ADD COLUMN cloud_environment_id TEXT NOT NULL DEFAULT ''`},
		{"cloud_attempt_number", `ALTER TABLE attempts ADD COLUMN cloud_attempt_number INTEGER NOT NULL DEFAULT 0`},
		{"pull_request_url", `ALTER TABLE attempts ADD COLUMN pull_request_url TEXT NOT NULL DEFAULT ''`},
		{"apply_ref", `ALTER TABLE attempts ADD COLUMN apply_ref TEXT NOT NULL DEFAULT ''`},
		{"logs_summary", `ALTER TABLE attempts ADD COLUMN logs_summary TEXT NOT NULL DEFAULT ''`},
		{"final_summary", `ALTER TABLE attempts ADD COLUMN final_summary TEXT NOT NULL DEFAULT ''`},
		{"end_state_json", `ALTER TABLE attempts ADD COLUMN end_state_json TEXT NOT NULL DEFAULT ''`},
	} {
		if err := s.ensureColumn("attempts", column.name, column.stmt); err != nil {
			return err
		}
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

// migrateGateLedgerToolchain rebuilds the one early ledger table whose old
// inline UNIQUE constraint omitted toolchain. ALTER TABLE can add the column,
// but cannot replace that autoindex; leaving it would silently overwrite a
// green proof from another compiler/runtime. Legacy rows are retained with an
// empty toolchain and are intentionally not reusable by the new lookup API.
func (s *RuntimeStore) migrateGateLedgerToolchain() error {
	rows, err := s.query(`PRAGMA table_info(gate_ledger)`)
	if err != nil {
		return err
	}
	hasToolchain := false
	for rows.Next() {
		var cid, notNull, pk int
		var name, kind string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &pk); err != nil {
			_ = rows.Close()
			return err
		}
		hasToolchain = hasToolchain || name == "toolchain"
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if hasToolchain {
		return nil
	}
	return s.withBusyRetry(func() error {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		if _, err := tx.Exec(`ALTER TABLE gate_ledger RENAME TO gate_ledger_legacy_toolchain`); err != nil {
			return err
		}
		if _, err := tx.Exec(`CREATE TABLE gate_ledger (
			id TEXT PRIMARY KEY, project_id TEXT NOT NULL, tree_hash TEXT NOT NULL,
			command TEXT NOT NULL, profile TEXT NOT NULL DEFAULT '',
			toolchain TEXT NOT NULL DEFAULT '', host TEXT NOT NULL DEFAULT '',
			duration_ms INTEGER NOT NULL DEFAULT 0, passed_at TEXT NOT NULL,
			provider_receipt_json TEXT NOT NULL DEFAULT '',
			UNIQUE(project_id, tree_hash, command, profile, toolchain)
		)`); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO gate_ledger (id, project_id, tree_hash, command, profile, toolchain, host, duration_ms, passed_at)
			SELECT id, project_id, tree_hash, command, profile, '', host, duration_ms, passed_at FROM gate_ledger_legacy_toolchain`); err != nil {
			return err
		}
		if _, err := tx.Exec(`DROP TABLE gate_ledger_legacy_toolchain`); err != nil {
			return err
		}
		return tx.Commit()
	})
}

func (s *RuntimeStore) ensureColumn(tableName, columnName, stmt string) error {
	rows, err := s.query(`PRAGMA table_info(` + tableName + `)`)
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
	_, err = s.exec(stmt)
	return err
}

func (s *RuntimeStore) UpsertProject(project RegisteredProject) error {
	if project.Health == "" {
		project.Health = projectHealthHealthy
	}
	_, err := s.exec(`INSERT INTO projects (
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

// RegisterProject is the idempotent registration boundary. UpsertProject stays
// available for restoring a known project ID and for fixtures, but user-facing
// registration must not mint a second identity for the same checkout.
func (s *RuntimeStore) RegisterProject(project RegisteredProject) (RegisteredProject, bool, error) {
	project.RepoRoot = canonicalProjectPath(project.RepoRoot)
	project.VaultRoot = canonicalProjectPath(project.VaultRoot)
	project.WorkflowPath = workflowPath(project.VaultRoot)
	projects, err := s.ListProjects()
	if err != nil {
		return RegisteredProject{}, false, err
	}
	for _, existing := range projects {
		if sameCanonicalProjectPath(existing.RepoRoot, project.RepoRoot) || sameCanonicalProjectPath(existing.VaultRoot, project.VaultRoot) {
			return existing, false, nil
		}
	}
	if err := s.UpsertProject(project); err != nil {
		return RegisteredProject{}, false, err
	}
	return project, true, nil
}

func (s *RuntimeStore) EnsureProjectUniqueness() error {
	if _, err := s.exec(`CREATE UNIQUE INDEX IF NOT EXISTS projects_repo_root_unique ON projects(repo_root)`); err != nil {
		return err
	}
	_, err := s.exec(`CREATE UNIQUE INDEX IF NOT EXISTS projects_vault_root_unique ON projects(vault_root)`)
	return err
}

// ReconcileDuplicateProjects removes duplicate discovery metadata left by old
// registration paths. Runtime rows are deliberately retained under their
// original project IDs: cleanup must never destroy execution history merely to
// tidy the sidebar.
func (s *RuntimeStore) ReconcileDuplicateProjects() ([]string, error) {
	projects, err := s.ListProjects()
	if err != nil {
		return nil, err
	}
	removed := make([]string, 0)
	for i := 0; i < len(projects); i++ {
		keeper := projects[i]
		if keeper.ProjectID == "" {
			continue
		}
		for j := i + 1; j < len(projects); j++ {
			candidate := projects[j]
			if candidate.ProjectID == "" || (!sameCanonicalProjectPath(keeper.RepoRoot, candidate.RepoRoot) && !sameCanonicalProjectPath(keeper.VaultRoot, candidate.VaultRoot)) {
				continue
			}
			if preferRegisteredProject(candidate, keeper) {
				keeper, candidate = candidate, keeper
				projects[i] = keeper
			}
			if err := s.UnregisterProject(candidate.ProjectID); err != nil {
				return removed, err
			}
			removed = append(removed, candidate.ProjectID)
			projects[j].ProjectID = ""
		}
	}
	return removed, nil
}

func preferRegisteredProject(left, right RegisteredProject) bool {
	if left.Enabled != right.Enabled {
		return left.Enabled
	}
	leftHealthy := left.Health == projectHealthHealthy
	rightHealthy := right.Health == projectHealthHealthy
	if leftHealthy != rightHealthy {
		return leftHealthy
	}
	if (left.LastPollAt != "") != (right.LastPollAt != "") {
		return left.LastPollAt != ""
	}
	if left.LastPollAt != right.LastPollAt {
		return left.LastPollAt > right.LastPollAt
	}
	return left.ProjectID < right.ProjectID
}

func sameCanonicalProjectPath(left, right string) bool {
	left = canonicalProjectPath(left)
	right = canonicalProjectPath(right)
	return left != "" && right != "" && left == right
}

func canonicalProjectPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err == nil {
		path = abs
	}
	path = filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = filepath.Clean(resolved)
	}
	return path
}

func (s *RuntimeStore) RemoveProject(projectID string) error {
	if _, err := s.exec(`DELETE FROM turns WHERE project_id = ?`, projectID); err != nil {
		return err
	}
	if _, err := s.exec(`DELETE FROM external_loop_events WHERE project_id = ?`, projectID); err != nil {
		return err
	}
	if _, err := s.exec(`DELETE FROM supervisor_decisions WHERE project_id = ?`, projectID); err != nil {
		return err
	}
	if _, err := s.exec(`DELETE FROM attempts WHERE project_id = ?`, projectID); err != nil {
		return err
	}
	if _, err := s.exec(`DELETE FROM sessions WHERE project_id = ?`, projectID); err != nil {
		return err
	}
	if _, err := s.exec(`DELETE FROM runs WHERE project_id = ?`, projectID); err != nil {
		return err
	}
	_, err := s.exec(`DELETE FROM projects WHERE project_id = ?`, projectID)
	return err
}

// UnregisterProject removes only daemon discovery metadata. Historical runs,
// attempts, sessions, decisions, and turns remain available for inspection.
// Explicit `projects remove` retains the stronger RemoveProject purge behavior.
func (s *RuntimeStore) UnregisterProject(projectID string) error {
	_, err := s.exec(`DELETE FROM projects WHERE project_id = ?`, projectID)
	return err
}

func (s *RuntimeStore) SetProjectEnabled(projectID string, enabled bool) error {
	health := projectHealthDisabled
	if enabled {
		health = projectHealthHealthy
	}
	result, err := s.exec(`UPDATE projects
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
	err := s.queryRowScan(`SELECT COUNT(*)
		FROM runs
		WHERE project_id = ? AND terminal = 0 AND lease_state IN ('claimed', 'running')`, []any{projectID}, &count)
	return count, err
}

func (s *RuntimeStore) ListProjects() ([]RegisteredProject, error) {
	rows, err := s.query(`SELECT project_id, project_key, name, repo_root, vault_root, workflow_path, enabled, health, last_poll_at, last_error FROM projects ORDER BY name, repo_root`)
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
	rows, err := s.query(`SELECT project_id, record_id, item_id, runner, runner_profile, runner_harness, runner_model, runner_effort, worker_policy_fingerprint, execute_policy_fingerprint, lane, lease_state, lease_owner, lease_generation, lease_expires_at, lease_host, attempt_outcome, active_attempt_id, workspace_path, session_ref, cloud_task_id, cloud_status, cloud_environment_id, cloud_attempt_number, pull_request_url, apply_ref, logs_summary, final_summary, process_pid, process_pgid, process_started_at, prompt_path, event_sink_path, raw_log_path, status_path, work_revision, attempt_count, next_retry_at, last_error, infrastructure_json, last_event_at, first_event_at, last_heartbeat_at, terminal, started_at, updated_at, hand_run FROM runs ORDER BY updated_at DESC, project_id, item_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RunStatus
	for rows.Next() {
		var run RunStatus
		var terminal int
		var handRun sql.NullInt64
		var infrastructureJSON string
		if err := rows.Scan(&run.ProjectID, &run.RecordID, &run.ItemID, &run.Runner, &run.RunnerProfile, &run.RunnerHarness, &run.RunnerModel, &run.RunnerEffort, &run.WorkerPolicyFP, &run.ExecutePolicyFP, &run.Lane, &run.LeaseState, &run.LeaseOwner, &run.LeaseGeneration, &run.LeaseExpiresAt, &run.LeaseHost, &run.AttemptOutcome, &run.ActiveAttemptID, &run.WorkspacePath, &run.SessionRef, &run.CloudTaskID, &run.CloudStatus, &run.CloudEnvironmentID, &run.CloudAttemptNumber, &run.PullRequestURL, &run.ApplyRef, &run.LogsSummary, &run.FinalSummary, &run.ProcessPID, &run.ProcessPGID, &run.ProcessStartedAt, &run.PromptPath, &run.EventSinkPath, &run.RawLogPath, &run.StatusPath, &run.WorkRevision, &run.AttemptCount, &run.NextRetryAt, &run.LastError, &infrastructureJSON, &run.LastEventAt, &run.FirstEventAt, &run.LastHeartbeatAt, &terminal, &run.StartedAt, &run.UpdatedAt, &handRun); err != nil {
			return nil, err
		}
		if strings.TrimSpace(infrastructureJSON) != "" {
			var block RunnerInfrastructureBlock
			if err := json.Unmarshal([]byte(infrastructureJSON), &block); err != nil {
				return nil, fmt.Errorf("decode run infrastructure block: %w", err)
			}
			run.Infrastructure = &block
		}
		run.Terminal = terminal != 0
		run.HandRun = handRun.Valid && handRun.Int64 != 0
		run.HandRunStamped = handRun.Valid
		out = append(out, run)
	}
	return out, rows.Err()
}

func (s *RuntimeStore) QueueRunDirective(directive RunDirective) (bool, error) {
	if strings.TrimSpace(directive.ProjectID) == "" || strings.TrimSpace(directive.RecordID) == "" || strings.TrimSpace(directive.Actor) == "" {
		return false, tuskerError(errorInvalidArg, "run directive requires project_id, record_id, and actor")
	}
	if strings.TrimSpace(directive.CreatedAt) == "" {
		directive.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if strings.TrimSpace(directive.ExpiresAt) == "" {
		return false, tuskerError(errorInvalidArg, "run directive requires an expiry")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, directive.CreatedAt)
	if err != nil {
		return false, tuskerError(errorInvalidArg, "run directive has an invalid created_at")
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, directive.ExpiresAt)
	if err != nil {
		return false, tuskerError(errorInvalidArg, "run directive has an invalid expires_at")
	}
	if !expiresAt.After(createdAt) {
		return false, tuskerError(errorInvalidArg, "run directive expiry must be after creation")
	}
	directive.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
	directive.ExpiresAt = expiresAt.UTC().Format(time.RFC3339Nano)
	directive.State = "queued"
	directive.Reason = ""
	result, err := s.exec(`INSERT INTO run_directives (project_id, record_id, actor, created_at, expires_at, state, reason)
		SELECT ?, ?, ?, ?, ?, ?, ?
		WHERE NOT EXISTS (
			SELECT 1 FROM runs WHERE project_id = ? AND record_id = ? AND lease_state IN ('claimed', 'running')
		)
		ON CONFLICT(project_id, record_id) DO UPDATE SET actor=excluded.actor, created_at=excluded.created_at, expires_at=excluded.expires_at, state=excluded.state, reason=excluded.reason
		WHERE run_directives.state != 'queued'`,
		directive.ProjectID, directive.RecordID, directive.Actor, directive.CreatedAt, directive.ExpiresAt, directive.State, directive.Reason,
		directive.ProjectID, directive.RecordID)
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	return changed > 0, err
}

func (s *RuntimeStore) RunDirective(projectID, recordID string) (*RunDirective, error) {
	row := s.db.QueryRow(`SELECT project_id, record_id, actor, created_at, expires_at, state, reason FROM run_directives WHERE project_id = ? AND record_id = ?`, projectID, recordID)
	var directive RunDirective
	if err := row.Scan(&directive.ProjectID, &directive.RecordID, &directive.Actor, &directive.CreatedAt, &directive.ExpiresAt, &directive.State, &directive.Reason); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if directive.State == "queued" && !runDirectiveActive(&directive, time.Now().UTC()) {
		if err := s.ExpireRunDirectives(time.Now().UTC()); err != nil {
			return nil, err
		}
		directive.State = "lapsed"
		directive.Reason = "daemon did not claim this one-shot run before it expired"
	}
	return &directive, nil
}

func (s *RuntimeStore) ExpireRunDirectives(now time.Time) error {
	_, err := s.exec(`UPDATE run_directives SET state = 'lapsed', reason = 'daemon did not claim this one-shot run before it expired' WHERE state = 'queued' AND julianday(expires_at) <= julianday(?)`, now.UTC().Format(time.RFC3339Nano))
	return err
}

func runDirectiveActive(directive *RunDirective, now time.Time) bool {
	if directive == nil || directive.State != "queued" {
		return false
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, directive.ExpiresAt)
	return err == nil && expiresAt.After(now)
}

// requeueUnstartedDirectedClaim recovers the narrow post-commit/pre-spawn
// window. The caller must first prove that no wrapper spawn event or terminal
// status exists. This transaction removes only the placeholder attempt intent
// and restores the consumed authorization for another daemon poll.
func (s *RuntimeStore) requeueUnstartedDirectedClaim(run RunStatus, now time.Time) (bool, error) {
	if run.ProjectID == "" || run.RecordID == "" || run.ActiveAttemptID == "" || run.LeaseGeneration <= 0 || run.ProcessPID != 0 {
		return false, nil
	}
	now = now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	recovered := false
	err := s.withBusyRetry(func() error {
		recovered = false
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		var source, actor string
		if err := tx.QueryRow(`SELECT source, actor FROM run_authorizations WHERE project_id = ? AND record_id = ? AND lease_generation = ?`, run.ProjectID, run.RecordID, run.LeaseGeneration).Scan(&source, &actor); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return err
		}
		if source != "human_run_directive" {
			return nil
		}
		var attemptPID int
		if err := tx.QueryRow(`SELECT process_pid FROM attempts WHERE attempt_id = ? AND project_id = ? AND record_id = ?`, run.ActiveAttemptID, run.ProjectID, run.RecordID).Scan(&attemptPID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return err
		}
		if attemptPID != 0 {
			return nil
		}
		result, err := tx.Exec(`UPDATE runs SET lease_state = 'unclaimed', lease_owner = '', lease_expires_at = '', lease_host = '',
			active_attempt_id = '', workspace_path = '', session_ref = '', process_pid = 0, process_pgid = 0,
			process_started_at = '', prompt_path = '', event_sink_path = '', raw_log_path = '', status_path = '',
			first_event_at = '', last_heartbeat_at = '', next_retry_at = '', terminal = 0,
			attempt_count = CASE WHEN attempt_count > 0 THEN attempt_count - 1 ELSE 0 END,
			last_error = 'recovered unstarted one-shot run; queued for daemon dispatch', updated_at = ?
			WHERE project_id = ? AND record_id = ? AND lease_state = 'claimed' AND lease_owner = ?
				AND lease_generation = ? AND process_pid = 0 AND active_attempt_id = ?`,
			now.Format(time.RFC3339Nano), run.ProjectID, run.RecordID, run.ActiveAttemptID, run.LeaseGeneration, run.ActiveAttemptID)
		if err != nil {
			return err
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if rows == 0 {
			return nil
		}
		if _, err := tx.Exec(`DELETE FROM attempts WHERE attempt_id = ? AND project_id = ? AND record_id = ? AND process_pid = 0`, run.ActiveAttemptID, run.ProjectID, run.RecordID); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM run_authorizations WHERE project_id = ? AND record_id = ? AND lease_generation = ?`, run.ProjectID, run.RecordID, run.LeaseGeneration); err != nil {
			return err
		}
		if directiveResult, err := tx.Exec(`UPDATE run_directives SET state = 'queued', actor = ?, created_at = ?, expires_at = ?, reason = 'recovered before runner spawn'
			WHERE project_id = ? AND record_id = ? AND state = 'consumed'`, actor, now.Format(time.RFC3339Nano), now.Add(10*time.Minute).Format(time.RFC3339Nano), run.ProjectID, run.RecordID); err != nil {
			return err
		} else if directiveRows, err := directiveResult.RowsAffected(); err != nil {
			return err
		} else if directiveRows == 0 {
			return nil
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		recovered = true
		return nil
	})
	return recovered, err
}

// registerRunnerWrapperSpawn is the durable handoff from a committed attempt
// intent to a real wrapper process. It races recovery inside SQLite: if
// recovery removed the placeholder first, registration returns false and the
// wrapper must exit before launching its child; if registration wins, the
// non-zero attempt PID makes recovery ineligible.
func (s *RuntimeStore) registerRunnerWrapperSpawn(req StartRequest, pid, pgid int, processStartedAt string) (bool, error) {
	if req.ProjectID == "" || req.RecordID == "" || req.AttemptID == "" || req.LeaseGeneration <= 0 || pid <= 0 {
		return false, tuskerError(errorInvalidArg, "wrapper spawn registration requires project, task, attempt, lease generation, and pid")
	}
	registered := false
	err := s.withBusyRetry(func() error {
		registered = false
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		// process_pid = pid (not just 0) accepts the benign write race where the
		// daemon persisted the wrapper's own PID before the wrapper registered it:
		// re-registering the identical PID is idempotent. A different PID or a
		// recovered/replaced claim (stale lease generation, mismatched attempt
		// identity) still fails these predicates and returns unregistered.
		attemptResult, err := tx.Exec(`UPDATE attempts SET process_pid = ?
			WHERE attempt_id = ? AND project_id = ? AND record_id = ? AND (process_pid = 0 OR process_pid = ?)
				AND EXISTS (SELECT 1 FROM runs WHERE project_id = ? AND record_id = ?
					AND active_attempt_id = ? AND lease_owner = ? AND lease_generation = ?
					AND lease_state IN ('claimed','running') AND (process_pid = 0 OR process_pid = ?))`,
			pid, req.AttemptID, req.ProjectID, req.RecordID, pid,
			req.ProjectID, req.RecordID, req.AttemptID, req.AttemptID, req.LeaseGeneration, pid)
		if err != nil {
			return err
		}
		attemptRows, err := attemptResult.RowsAffected()
		if err != nil {
			return err
		}
		if attemptRows == 0 {
			return nil
		}
		runResult, err := tx.Exec(`UPDATE runs SET process_pid = ?, process_pgid = ?, process_started_at = ?, updated_at = ?
			WHERE project_id = ? AND record_id = ? AND active_attempt_id = ? AND lease_owner = ?
				AND lease_generation = ? AND lease_state IN ('claimed','running') AND (process_pid = 0 OR process_pid = ?)`,
			pid, pgid, processStartedAt, time.Now().UTC().Format(time.RFC3339Nano),
			req.ProjectID, req.RecordID, req.AttemptID, req.AttemptID, req.LeaseGeneration, pid)
		if err != nil {
			return err
		}
		runRows, err := runResult.RowsAffected()
		if err != nil {
			return err
		}
		if runRows == 0 {
			return nil
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		registered = true
		return nil
	})
	return registered, err
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
	_, err := s.exec(`INSERT INTO runs (
		project_id, record_id, item_id, runner, runner_profile, runner_harness, runner_model, runner_effort, worker_policy_fingerprint, execute_policy_fingerprint, lane, lease_state, lease_owner, lease_generation, lease_expires_at, lease_host, attempt_outcome, active_attempt_id, workspace_path, session_ref, cloud_task_id, cloud_status, cloud_environment_id, cloud_attempt_number, pull_request_url, apply_ref, logs_summary, final_summary, process_pid, process_pgid, process_started_at, prompt_path, event_sink_path, raw_log_path, status_path, work_revision, attempt_count, next_retry_at, last_error, infrastructure_json, last_event_at, first_event_at, last_heartbeat_at, terminal, started_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(project_id, record_id) DO UPDATE SET
		item_id=excluded.item_id,
		runner=excluded.runner,
		runner_profile=excluded.runner_profile,
		runner_harness=excluded.runner_harness,
		runner_model=excluded.runner_model,
		runner_effort=excluded.runner_effort,
		worker_policy_fingerprint=excluded.worker_policy_fingerprint,
		execute_policy_fingerprint=excluded.execute_policy_fingerprint,
		lane=excluded.lane,
		lease_state=excluded.lease_state,
		lease_owner=excluded.lease_owner,
		lease_generation=excluded.lease_generation,
		lease_expires_at=excluded.lease_expires_at,
		lease_host=excluded.lease_host,
		attempt_outcome=excluded.attempt_outcome,
		active_attempt_id=excluded.active_attempt_id,
		workspace_path=excluded.workspace_path,
		session_ref=excluded.session_ref,
		cloud_task_id=excluded.cloud_task_id,
		cloud_status=excluded.cloud_status,
		cloud_environment_id=excluded.cloud_environment_id,
		cloud_attempt_number=excluded.cloud_attempt_number,
		pull_request_url=excluded.pull_request_url,
		apply_ref=excluded.apply_ref,
		logs_summary=excluded.logs_summary,
		final_summary=excluded.final_summary,
		process_pid=excluded.process_pid,
		process_pgid=excluded.process_pgid,
		process_started_at=excluded.process_started_at,
		prompt_path=excluded.prompt_path,
		event_sink_path=excluded.event_sink_path,
		raw_log_path=excluded.raw_log_path,
		status_path=excluded.status_path,
		work_revision=excluded.work_revision,
		attempt_count=excluded.attempt_count,
		next_retry_at=excluded.next_retry_at,
		last_error=excluded.last_error,
		infrastructure_json=excluded.infrastructure_json,
		last_event_at=excluded.last_event_at,
		first_event_at=excluded.first_event_at,
		last_heartbeat_at=excluded.last_heartbeat_at,
		terminal=excluded.terminal,
		started_at=excluded.started_at,
		updated_at=excluded.updated_at`,
		run.ProjectID, run.RecordID, run.ItemID, run.Runner, run.RunnerProfile, run.RunnerHarness, run.RunnerModel, run.RunnerEffort, run.WorkerPolicyFP, run.ExecutePolicyFP, run.Lane, run.LeaseState, run.LeaseOwner, run.LeaseGeneration, run.LeaseExpiresAt, run.LeaseHost, run.AttemptOutcome, run.ActiveAttemptID, run.WorkspacePath, run.SessionRef, run.CloudTaskID, run.CloudStatus, run.CloudEnvironmentID, run.CloudAttemptNumber, run.PullRequestURL, run.ApplyRef, run.LogsSummary, run.FinalSummary, run.ProcessPID, run.ProcessPGID, run.ProcessStartedAt, run.PromptPath, run.EventSinkPath, run.RawLogPath, run.StatusPath, run.WorkRevision, run.AttemptCount, run.NextRetryAt, run.LastError, marshalRunInfrastructure(run.Infrastructure), run.LastEventAt, run.FirstEventAt, run.LastHeartbeatAt, boolToInt(run.Terminal), run.StartedAt, run.UpdatedAt)
	if err != nil || runConsumesDispatchCapacity(run) {
		return err
	}
	return s.releaseInactiveTaskResourceLeases(run.ProjectID, run.RecordID, "run no longer owns dispatch capacity", time.Now().UTC())
}

var runtimeRunMutableColumns = []string{
	"item_id", "runner", "runner_profile", "runner_harness", "runner_model", "runner_effort",
	"worker_policy_fingerprint", "execute_policy_fingerprint",
	"lane", "lease_state", "lease_owner", "lease_generation",
	"lease_expires_at", "lease_host", "attempt_outcome", "active_attempt_id",
	"workspace_path", "session_ref", "cloud_task_id", "cloud_status",
	"cloud_environment_id", "cloud_attempt_number", "pull_request_url", "apply_ref",
	"logs_summary", "final_summary", "process_pid", "process_pgid", "process_started_at",
	"prompt_path", "event_sink_path", "raw_log_path", "status_path", "work_revision",
	"attempt_count", "next_retry_at", "last_error", "infrastructure_json", "last_event_at", "first_event_at",
	"last_heartbeat_at", "terminal", "started_at", "updated_at",
}

func normalizeRuntimeRunWrite(run RunStatus) RunStatus {
	if run.LeaseState == "" {
		run.LeaseState = string(LeaseStateUnclaimed)
	}
	if run.AttemptOutcome == "" {
		run.AttemptOutcome = string(AttemptOutcomeNone)
	}
	if strings.TrimSpace(run.UpdatedAt) == "" {
		run.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	return run
}

func runtimeRunMutableValues(run RunStatus) []any {
	return []any{
		run.ItemID, run.Runner, run.RunnerProfile, run.RunnerHarness, run.RunnerModel, run.RunnerEffort,
		run.WorkerPolicyFP, run.ExecutePolicyFP,
		run.Lane, run.LeaseState, run.LeaseOwner, run.LeaseGeneration,
		run.LeaseExpiresAt, run.LeaseHost, run.AttemptOutcome, run.ActiveAttemptID,
		run.WorkspacePath, run.SessionRef, run.CloudTaskID, run.CloudStatus,
		run.CloudEnvironmentID, run.CloudAttemptNumber, run.PullRequestURL, run.ApplyRef,
		run.LogsSummary, run.FinalSummary, run.ProcessPID, run.ProcessPGID, run.ProcessStartedAt,
		run.PromptPath, run.EventSinkPath, run.RawLogPath, run.StatusPath, run.WorkRevision,
		run.AttemptCount, run.NextRetryAt, run.LastError, marshalRunInfrastructure(run.Infrastructure), run.LastEventAt, run.FirstEventAt,
		run.LastHeartbeatAt, boolToInt(run.Terminal), run.StartedAt, run.UpdatedAt,
	}
}

func runtimeRunChangedColumns(expected, actual RunStatus) []string {
	expectedValues := runtimeRunMutableValues(expected)
	actualValues := runtimeRunMutableValues(actual)
	changed := make([]string, 0)
	for index, column := range runtimeRunMutableColumns {
		if !reflect.DeepEqual(expectedValues[index], actualValues[index]) {
			changed = append(changed, column)
		}
	}
	return changed
}

func runtimeRunChangesCompatible(before, intended, current RunStatus) bool {
	beforeValues := runtimeRunMutableValues(before)
	intendedValues := runtimeRunMutableValues(intended)
	currentValues := runtimeRunMutableValues(current)
	changed := false
	for index := range runtimeRunMutableColumns {
		if reflect.DeepEqual(beforeValues[index], currentValues[index]) {
			continue
		}
		changed = true
		if !reflect.DeepEqual(intendedValues[index], currentValues[index]) {
			return false
		}
	}
	return changed
}

func runtimeRunSnapshotUpdate(expected, updated RunStatus) (string, []any) {
	assignments := make([]string, 0, len(runtimeRunMutableColumns))
	predicates := make([]string, 0, len(runtimeRunMutableColumns))
	for _, column := range runtimeRunMutableColumns {
		assignments = append(assignments, column+" = ?")
		predicates = append(predicates, column+" = ?")
	}
	query := `UPDATE runs SET ` + strings.Join(assignments, ", ") +
		` WHERE project_id = ? AND record_id = ? AND ` + strings.Join(predicates, " AND ")
	args := runtimeRunMutableValues(updated)
	args = append(args, expected.ProjectID, expected.RecordID)
	args = append(args, runtimeRunMutableValues(expected)...)
	return query, args
}

func runtimeRunInsert(run RunStatus) (string, []any) {
	columns := append([]string{"project_id", "record_id"}, runtimeRunMutableColumns...)
	placeholders := make([]string, len(columns))
	for i := range placeholders {
		placeholders[i] = "?"
	}
	query := `INSERT INTO runs (` + strings.Join(columns, ", ") + `) VALUES (` +
		strings.Join(placeholders, ", ") + `) ON CONFLICT(project_id, record_id) DO NOTHING`
	args := []any{run.ProjectID, run.RecordID}
	args = append(args, runtimeRunMutableValues(run)...)
	return query, args
}

// UpsertRunIfSnapshot is the daemon-poll write path. Existing rows are updated
// only when every stored field still matches the row PollOnce read; absent rows
// are inserted only if no concurrent writer created them first.
func (s *RuntimeStore) UpsertRunIfSnapshot(expected, updated RunStatus) (bool, error) {
	updated = normalizeRuntimeRunWrite(updated)
	if strings.TrimSpace(updated.ProjectID) == "" || strings.TrimSpace(updated.RecordID) == "" {
		return false, tuskerError(errorInvalidArg, "conditional run update requires project_id and record_id")
	}
	var query string
	var args []any
	if strings.TrimSpace(expected.ProjectID) == "" && strings.TrimSpace(expected.RecordID) == "" {
		query, args = runtimeRunInsert(updated)
	} else {
		query, args = runtimeRunSnapshotUpdate(expected, updated)
	}
	result, err := s.exec(query, args...)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected > 0 && !runConsumesDispatchCapacity(updated) {
		if err := s.releaseInactiveTaskResourceLeases(updated.ProjectID, updated.RecordID, "run reconciliation released dispatch capacity", time.Now().UTC()); err != nil {
			return false, err
		}
	}
	return affected > 0, nil
}

func (s *RuntimeStore) RunMatchesSnapshot(run RunStatus) (bool, error) {
	predicates := make([]string, 0, len(runtimeRunMutableColumns))
	for _, column := range runtimeRunMutableColumns {
		predicates = append(predicates, column+" = ?")
	}
	query := `SELECT COUNT(1) FROM runs WHERE project_id = ? AND record_id = ? AND ` + strings.Join(predicates, " AND ")
	args := []any{run.ProjectID, run.RecordID}
	args = append(args, runtimeRunMutableValues(run)...)
	var count int
	if err := s.queryRowScan(query, args, &count); err != nil {
		return false, err
	}
	return count == 1, nil
}

// RedriveRunIfSnapshot commits the queued run transition and budget-window
// reset in one transaction. A concurrent claim makes the snapshot update miss,
// and neither half of the redrive is persisted.
func (s *RuntimeStore) RedriveRunIfSnapshot(expected, redriven RunStatus, reset BudgetRedriveRecord) (bool, error) {
	if strings.TrimSpace(expected.ProjectID) == "" || strings.TrimSpace(expected.RecordID) == "" {
		return false, tuskerError(errorInvalidArg, "conditional redrive requires project_id and record_id")
	}
	redriven = normalizeRuntimeRunWrite(redriven)
	raw, err := json.Marshal(reset)
	if err != nil {
		return false, err
	}
	query, args := runtimeRunSnapshotUpdate(expected, redriven)
	matched := false
	err = s.withBusyRetry(func() error {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		result, err := tx.Exec(query, args...)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			matched = false
			return nil
		}
		if _, err := tx.Exec(`INSERT INTO daemon_settings (key, value) VALUES (?, ?)
			ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
			budgetRedriveSettingKey(expected.ProjectID, expected.RecordID), string(raw)); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		matched = true
		return nil
	})
	return matched, err
}

// UpsertRunPreservingLease inserts the run row if it is absent (same columns and
// defaults as UpsertRun) but, on conflict, updates ONLY the dispatch-intent
// columns (item_id, runner, lane, work_revision, updated_at). It deliberately
// leaves the live lease/process/session/outcome/cloud/summary columns of the
// stored row untouched so a pre-dispatch publish of a (possibly stale) prepped
// run cannot clobber a concurrent operator stop or a bumped lease generation,
// keeping dispatchRun's ClaimRunLease CAS honest against the true stored lease.
func (s *RuntimeStore) UpsertRunPreservingLease(run RunStatus) error {
	if run.LeaseState == "" {
		run.LeaseState = string(LeaseStateUnclaimed)
	}
	if run.AttemptOutcome == "" {
		run.AttemptOutcome = string(AttemptOutcomeNone)
	}
	if strings.TrimSpace(run.UpdatedAt) == "" {
		run.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	_, err := s.exec(`INSERT INTO runs (
		project_id, record_id, item_id, runner, runner_profile, runner_harness, runner_model, runner_effort, worker_policy_fingerprint, execute_policy_fingerprint, lane, lease_state, lease_owner, lease_generation, lease_expires_at, lease_host, attempt_outcome, active_attempt_id, workspace_path, session_ref, cloud_task_id, cloud_status, cloud_environment_id, cloud_attempt_number, pull_request_url, apply_ref, logs_summary, final_summary, process_pid, process_pgid, process_started_at, prompt_path, event_sink_path, raw_log_path, status_path, work_revision, attempt_count, next_retry_at, last_error, infrastructure_json, last_event_at, first_event_at, last_heartbeat_at, terminal, started_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(project_id, record_id) DO UPDATE SET
		item_id=excluded.item_id,
		runner=excluded.runner,
		runner_profile=excluded.runner_profile,
		runner_harness=excluded.runner_harness,
		runner_model=excluded.runner_model,
		runner_effort=excluded.runner_effort,
		lane=excluded.lane,
		work_revision=excluded.work_revision,
		updated_at=excluded.updated_at`,
		run.ProjectID, run.RecordID, run.ItemID, run.Runner, run.RunnerProfile, run.RunnerHarness, run.RunnerModel, run.RunnerEffort, run.WorkerPolicyFP, run.ExecutePolicyFP, run.Lane, run.LeaseState, run.LeaseOwner, run.LeaseGeneration, run.LeaseExpiresAt, run.LeaseHost, run.AttemptOutcome, run.ActiveAttemptID, run.WorkspacePath, run.SessionRef, run.CloudTaskID, run.CloudStatus, run.CloudEnvironmentID, run.CloudAttemptNumber, run.PullRequestURL, run.ApplyRef, run.LogsSummary, run.FinalSummary, run.ProcessPID, run.ProcessPGID, run.ProcessStartedAt, run.PromptPath, run.EventSinkPath, run.RawLogPath, run.StatusPath, run.WorkRevision, run.AttemptCount, run.NextRetryAt, run.LastError, marshalRunInfrastructure(run.Infrastructure), run.LastEventAt, run.FirstEventAt, run.LastHeartbeatAt, boolToInt(run.Terminal), run.StartedAt, run.UpdatedAt)
	return err
}

func marshalRunInfrastructure(block *RunnerInfrastructureBlock) string {
	if block == nil {
		return ""
	}
	raw, err := json.Marshal(block)
	if err != nil {
		return ""
	}
	return string(raw)
}

type RuntimeLeaseRenewal struct {
	ProjectID      string
	RecordID       string
	Owner          string
	Generation     int
	TTL            time.Duration
	Now            time.Time
	Dispatchable   bool
	ProcessPID     int
	ProcessPGID    int
	ProcessStarted string
}

type RuntimeLeaseClaimPrecondition struct {
	ExpectedLeaseState      LeaseState
	ExpectedOwner           string
	ExpectedLeaseGeneration int
	ExpectedWorkRevision    int
	ProjectConcurrencyLimit int
}

func (s *RuntimeStore) ClaimRunLease(projectID, recordID, owner string, generation int, ttl time.Duration, now time.Time, dispatchable bool, handRun bool, precondition RuntimeLeaseClaimPrecondition) (bool, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(recordID) == "" {
		return false, tuskerError(errorInvalidArg, "lease claim requires project_id and record_id")
	}
	if strings.TrimSpace(owner) == "" {
		return false, tuskerError(errorInvalidArg, "lease claim requires owner")
	}
	if generation <= 0 {
		return false, tuskerError(errorInvalidArg, "lease claim requires generation")
	}
	if !dispatchable {
		return false, nil
	}
	expectedLeaseState := strings.TrimSpace(string(precondition.ExpectedLeaseState))
	if expectedLeaseState == "" {
		return false, tuskerError(errorInvalidArg, "lease claim requires expected lease state")
	}
	if ttl <= 0 {
		ttl = defaultRunLeaseTTL
	}
	now = now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	query := `UPDATE runs
		SET lease_state = 'claimed',
			lease_owner = ?,
			lease_generation = ?,
			lease_expires_at = ?,
			lease_host = ?,
			last_heartbeat_at = ?,
			updated_at = ?,
			hand_run = ?
		WHERE project_id = ? AND record_id = ?
			AND lease_state NOT IN ('claimed', 'running')
			AND lease_state = ?
			AND lease_owner = ?
			AND lease_generation = ?
			AND work_revision = ?
			AND (? <= 0 OR (SELECT COUNT(1) FROM runs active WHERE active.project_id = ? AND active.lease_state IN ('claimed','running')) < ?)`
	params := []any{
		owner, generation, now.Add(ttl).Format(time.RFC3339), runtimeLeaseHost(), now.Format(time.RFC3339), now.Format(time.RFC3339), boolToInt(handRun),
		projectID, recordID, expectedLeaseState, precondition.ExpectedOwner,
		precondition.ExpectedLeaseGeneration, precondition.ExpectedWorkRevision,
		precondition.ProjectConcurrencyLimit, projectID, precondition.ProjectConcurrencyLimit,
	}
	result, err := s.exec(query, params...)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

// claimRunLeaseWithDirectiveAttempt is the directed-dispatch commit boundary.
// A one-shot authorization must never be consumed unless the matching lease,
// authorization trail, and durable attempt intent all exist. Keeping those
// writes in one SQLite transaction lets normal stale-lease recovery handle a
// crash after the claim without losing the operator's exactly-once request.
func (s *RuntimeStore) claimRunLeaseWithDirectiveAttempt(run RunStatus, owner string, generation int, ttl time.Duration, now time.Time, precondition RuntimeLeaseClaimPrecondition, auth RunAuthorization, attempt RunAttempt) (bool, error) {
	if strings.TrimSpace(run.ProjectID) == "" || strings.TrimSpace(run.RecordID) == "" || strings.TrimSpace(owner) == "" || generation <= 0 {
		return false, tuskerError(errorInvalidArg, "directed lease claim requires project, task, owner, and generation")
	}
	if strings.TrimSpace(attempt.AttemptID) == "" || attempt.AttemptID != owner {
		return false, tuskerError(errorInvalidArg, "directed lease claim requires an attempt matching the lease owner")
	}
	expectedLeaseState := strings.TrimSpace(string(precondition.ExpectedLeaseState))
	if expectedLeaseState == "" {
		return false, tuskerError(errorInvalidArg, "directed lease claim requires expected lease state")
	}
	if ttl <= 0 {
		ttl = defaultRunLeaseTTL
	}
	now = now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	createdAt := now.Format(time.RFC3339Nano)
	auth.ProjectID, auth.RecordID, auth.LeaseGeneration = run.ProjectID, run.RecordID, generation
	auth.Actor = firstNonEmpty(strings.TrimSpace(auth.Actor), owner)
	auth.Source = firstNonEmpty(strings.TrimSpace(auth.Source), "human_run_directive")
	auth.CreatedAt = createdAt
	attempt.ProjectID, attempt.RecordID = run.ProjectID, run.RecordID
	attempt.AttemptID = owner
	attempt.ItemID = firstNonEmpty(attempt.ItemID, run.ItemID)
	attempt.Runner = firstNonEmpty(attempt.Runner, run.Runner)
	attempt.Lane = firstNonEmpty(attempt.Lane, run.Lane, runLaneExecute)
	attempt.WorkRevision = run.WorkRevision
	attempt.Outcome = firstNonEmpty(attempt.Outcome, string(AttemptOutcomeNone))
	attempt.StartedAt = firstNonEmpty(attempt.StartedAt, createdAt)

	claimed := false
	err := s.withBusyRetry(func() error {
		claimed = false
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		directiveResult, err := tx.Exec(`UPDATE run_directives SET state = 'consumed', reason = ''
			WHERE project_id = ? AND record_id = ? AND state = 'queued' AND julianday(expires_at) > julianday(?)`,
			run.ProjectID, run.RecordID, createdAt)
		if err != nil {
			return err
		}
		directiveRows, err := directiveResult.RowsAffected()
		if err != nil {
			return err
		}
		if directiveRows == 0 {
			return nil
		}

		claimResult, err := tx.Exec(`UPDATE runs
			SET lease_state = 'claimed', lease_owner = ?, lease_generation = ?, lease_expires_at = ?, lease_host = ?,
				last_heartbeat_at = ?, updated_at = ?, hand_run = 0, attempt_count = ?, active_attempt_id = ?,
				runner = ?, runner_profile = ?, runner_harness = ?, runner_model = ?, runner_effort = ?,
				worker_policy_fingerprint = ?, execute_policy_fingerprint = ?, lane = ?,
				started_at = CASE WHEN started_at = '' THEN ? ELSE started_at END, last_event_at = ?
			WHERE project_id = ? AND record_id = ?
				AND lease_state NOT IN ('claimed', 'running') AND lease_state = ? AND lease_owner = ?
				AND lease_generation = ? AND work_revision = ?
				AND (? <= 0 OR (SELECT COUNT(1) FROM runs active WHERE active.project_id = ? AND active.lease_state IN ('claimed','running')) < ?)`,
			owner, generation, now.Add(ttl).Format(time.RFC3339Nano), runtimeLeaseHost(), createdAt, createdAt,
			run.AttemptCount+1, owner, attempt.Runner, run.RunnerProfile, run.RunnerHarness, run.RunnerModel, run.RunnerEffort,
			attempt.WorkerPolicyFP, run.ExecutePolicyFP, attempt.Lane, attempt.StartedAt, createdAt,
			run.ProjectID, run.RecordID, expectedLeaseState, precondition.ExpectedOwner,
			precondition.ExpectedLeaseGeneration, precondition.ExpectedWorkRevision,
			precondition.ProjectConcurrencyLimit, run.ProjectID, precondition.ProjectConcurrencyLimit)
		if err != nil {
			return err
		}
		claimRows, err := claimResult.RowsAffected()
		if err != nil {
			return err
		}
		if claimRows == 0 {
			return nil
		}
		if _, err := tx.Exec(`INSERT INTO run_authorizations(project_id, record_id, lease_generation, source, actor, trigger, project_automation_enabled, created_at)
			VALUES(?,?,?,?,?,?,?,?)`, auth.ProjectID, auth.RecordID, auth.LeaseGeneration, auth.Source, auth.Actor, auth.Trigger, boolToInt(auth.ProjectAutomationEnabled), auth.CreatedAt); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO attempts(attempt_id, project_id, record_id, item_id, runner, lane, worker_policy_fingerprint, work_revision, workspace_path, branch_name, outcome, started_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, attempt.AttemptID, attempt.ProjectID, attempt.RecordID, attempt.ItemID, attempt.Runner, attempt.Lane, attempt.WorkerPolicyFP, attempt.WorkRevision, attempt.WorkspacePath, attempt.BranchName, attempt.Outcome, attempt.StartedAt); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		claimed = true
		return nil
	})
	return claimed, err
}

// claimRunLeaseWithWorkSessionAttempt is the interactive counterpart to the
// directive claim transaction.  It intentionally does not read or consume a
// directive: a user-directed session authorizes exactly this lease and no
// daemon work.  Lease, authorization, and attempt intent either commit
// together or do not exist at all.
func (s *RuntimeStore) claimRunLeaseWithWorkSessionAttempt(run RunStatus, owner string, generation int, ttl time.Duration, now time.Time, precondition RuntimeLeaseClaimPrecondition, auth RunAuthorization, attempt RunAttempt, identity RunIdentityMetadata) (bool, error) {
	if strings.TrimSpace(run.ProjectID) == "" || strings.TrimSpace(run.RecordID) == "" || strings.TrimSpace(owner) == "" || generation <= 0 || strings.TrimSpace(attempt.AttemptID) == "" {
		return false, tuskerError(errorInvalidArg, "work-session claim requires project, task, owner, generation, and attempt")
	}
	if identity.ProjectID != run.ProjectID || identity.RecordID != run.RecordID || identity.RepoRoot == "" || identity.WorkspacePath == "" || identity.WorkspaceMode == "" || identity.Runner == "" {
		return false, tuskerError(errorInvalidArg, "work-session claim requires an exact repository, workspace, branch, and source identity")
	}
	expectedLeaseState := strings.TrimSpace(string(precondition.ExpectedLeaseState))
	if expectedLeaseState == "" {
		return false, tuskerError(errorInvalidArg, "work-session claim requires expected lease state")
	}
	if ttl <= 0 {
		ttl = defaultRunLeaseTTL
	}
	now = now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	createdAt := now.Format(time.RFC3339Nano)
	auth.ProjectID, auth.RecordID, auth.LeaseGeneration = run.ProjectID, run.RecordID, generation
	auth.Actor, auth.Source, auth.CreatedAt = firstNonEmpty(strings.TrimSpace(auth.Actor), owner), firstNonEmpty(strings.TrimSpace(auth.Source), "tusker_cli"), createdAt
	attempt.ProjectID, attempt.RecordID = run.ProjectID, run.RecordID
	attempt.ItemID = firstNonEmpty(attempt.ItemID, run.ItemID)
	attempt.Runner, attempt.Lane = firstNonEmpty(attempt.Runner, run.Runner), firstNonEmpty(attempt.Lane, run.Lane, runLaneExecute)
	attempt.WorkRevision, attempt.Outcome, attempt.StartedAt = run.WorkRevision, firstNonEmpty(attempt.Outcome, string(AttemptOutcomeNone)), firstNonEmpty(attempt.StartedAt, createdAt)
	identity.CreatedAt = firstNonEmpty(identity.CreatedAt, createdAt)
	claimed := false
	err := s.withBusyRetry(func() error {
		claimed = false
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		claimResult, err := tx.Exec(`UPDATE runs
			SET lease_state = 'claimed', lease_owner = ?, lease_generation = ?, lease_expires_at = ?, lease_host = ?,
				last_heartbeat_at = ?, updated_at = ?, hand_run = 1, attempt_count = ?, active_attempt_id = ?,
				runner = ?, lane = ?, started_at = CASE WHEN started_at = '' THEN ? ELSE started_at END, last_event_at = ?
			WHERE project_id = ? AND record_id = ?
				AND lease_state NOT IN ('claimed', 'running') AND lease_state = ? AND lease_owner = ?
				AND lease_generation = ? AND work_revision = ?
				AND (? <= 0 OR (SELECT COUNT(1) FROM runs active WHERE active.project_id = ? AND active.lease_state IN ('claimed','running')) < ?)`,
			owner, generation, now.Add(ttl).Format(time.RFC3339Nano), runtimeLeaseHost(), createdAt, createdAt,
			run.AttemptCount+1, attempt.AttemptID, attempt.Runner, attempt.Lane, attempt.StartedAt, createdAt,
			run.ProjectID, run.RecordID, expectedLeaseState, precondition.ExpectedOwner, precondition.ExpectedLeaseGeneration, precondition.ExpectedWorkRevision,
			precondition.ProjectConcurrencyLimit, run.ProjectID, precondition.ProjectConcurrencyLimit)
		if err != nil {
			return err
		}
		rows, err := claimResult.RowsAffected()
		if err != nil {
			return err
		}
		if rows == 0 {
			return nil
		}
		if _, err := tx.Exec(`INSERT INTO run_authorizations(project_id, record_id, lease_generation, source, actor, trigger, project_automation_enabled, created_at) VALUES(?,?,?,?,?,?,?,?)`, auth.ProjectID, auth.RecordID, auth.LeaseGeneration, auth.Source, auth.Actor, auth.Trigger, boolToInt(auth.ProjectAutomationEnabled), auth.CreatedAt); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO attempts(attempt_id, project_id, record_id, item_id, runner, lane, worker_policy_fingerprint, work_revision, workspace_path, branch_name, outcome, started_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, attempt.AttemptID, attempt.ProjectID, attempt.RecordID, attempt.ItemID, attempt.Runner, attempt.Lane, attempt.WorkerPolicyFP, attempt.WorkRevision, attempt.WorkspacePath, attempt.BranchName, attempt.Outcome, attempt.StartedAt); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO run_identity_metadata(project_id,record_id,repo_root,workspace_path,workspace_mode,runner,branch,head,created_at)
			VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(project_id,record_id) DO UPDATE SET repo_root=excluded.repo_root, workspace_path=excluded.workspace_path, workspace_mode=excluded.workspace_mode, runner=excluded.runner, branch=excluded.branch, head=excluded.head`,
			identity.ProjectID, identity.RecordID, identity.RepoRoot, identity.WorkspacePath, identity.WorkspaceMode, identity.Runner, identity.Branch, identity.Head, identity.CreatedAt); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		claimed = true
		return nil
	})
	return claimed, err
}

func (s *RuntimeStore) RunLeaseMatches(projectID, recordID, owner string, generation int) (bool, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(recordID) == "" || strings.TrimSpace(owner) == "" || generation <= 0 {
		return false, nil
	}
	var count int
	if err := s.queryRowScan(`SELECT COUNT(1) FROM runs WHERE project_id = ? AND record_id = ? AND lease_owner = ? AND lease_generation = ?`, []any{projectID, recordID, owner, generation}, &count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *RuntimeStore) SaveAttemptIfRunLease(attempt RunAttempt, owner string, generation int) (bool, error) {
	matched, err := s.RunLeaseMatches(attempt.ProjectID, attempt.RecordID, owner, generation)
	if err != nil || !matched {
		return false, err
	}
	if err := s.SaveAttempt(attempt); err != nil {
		return false, err
	}
	return s.RunLeaseMatches(attempt.ProjectID, attempt.RecordID, owner, generation)
}

func (s *RuntimeStore) UpdateRunIfLease(run RunStatus, owner string, generation int) (bool, error) {
	if strings.TrimSpace(run.ProjectID) == "" || strings.TrimSpace(run.RecordID) == "" {
		return false, tuskerError(errorInvalidArg, "conditional run update requires project_id and record_id")
	}
	if strings.TrimSpace(owner) == "" || generation <= 0 {
		return false, tuskerError(errorInvalidArg, "conditional run update requires lease owner and generation")
	}
	if run.LeaseState == "" {
		run.LeaseState = string(LeaseStateUnclaimed)
	}
	if run.AttemptOutcome == "" {
		run.AttemptOutcome = string(AttemptOutcomeNone)
	}
	if strings.TrimSpace(run.UpdatedAt) == "" {
		run.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	result, err := s.exec(`UPDATE runs SET
		item_id = ?,
		runner = ?,
		runner_profile = ?,
		runner_harness = ?,
		runner_model = ?,
		runner_effort = ?,
		worker_policy_fingerprint = ?,
		execute_policy_fingerprint = ?,
		lane = ?,
		lease_state = ?,
		lease_owner = ?,
		lease_generation = ?,
		lease_expires_at = ?,
		lease_host = ?,
		attempt_outcome = ?,
		active_attempt_id = ?,
		workspace_path = ?,
		session_ref = ?,
		cloud_task_id = ?,
		cloud_status = ?,
		cloud_environment_id = ?,
		cloud_attempt_number = ?,
		pull_request_url = ?,
		apply_ref = ?,
		logs_summary = ?,
		final_summary = ?,
		process_pid = ?,
		process_pgid = ?,
		process_started_at = ?,
		prompt_path = ?,
		event_sink_path = ?,
		raw_log_path = ?,
		status_path = ?,
		work_revision = ?,
		attempt_count = ?,
		next_retry_at = ?,
		last_error = ?,
		last_event_at = ?,
		first_event_at = ?,
		last_heartbeat_at = ?,
		terminal = ?,
		started_at = ?,
		updated_at = ?
		WHERE project_id = ? AND record_id = ? AND lease_owner = ? AND lease_generation = ?`,
		run.ItemID, run.Runner, run.RunnerProfile, run.RunnerHarness, run.RunnerModel, run.RunnerEffort, run.WorkerPolicyFP, run.ExecutePolicyFP, run.Lane, run.LeaseState, run.LeaseOwner, run.LeaseGeneration, run.LeaseExpiresAt, run.LeaseHost, run.AttemptOutcome, run.ActiveAttemptID, run.WorkspacePath, run.SessionRef, run.CloudTaskID, run.CloudStatus, run.CloudEnvironmentID, run.CloudAttemptNumber, run.PullRequestURL, run.ApplyRef, run.LogsSummary, run.FinalSummary, run.ProcessPID, run.ProcessPGID, run.ProcessStartedAt, run.PromptPath, run.EventSinkPath, run.RawLogPath, run.StatusPath, run.WorkRevision, run.AttemptCount, run.NextRetryAt, run.LastError, run.LastEventAt, run.FirstEventAt, run.LastHeartbeatAt, boolToInt(run.Terminal), run.StartedAt, run.UpdatedAt, run.ProjectID, run.RecordID, owner, generation)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected > 0 && !runConsumesDispatchCapacity(run) {
		if err := s.releaseInactiveTaskResourceLeases(run.ProjectID, run.RecordID, "run completion released dispatch capacity", time.Now().UTC()); err != nil {
			return false, err
		}
	}
	return affected > 0, nil
}

func (s *RuntimeStore) InterruptRunIfSnapshot(expected, interrupted RunStatus) (bool, error) {
	if strings.TrimSpace(expected.ProjectID) == "" || strings.TrimSpace(expected.RecordID) == "" {
		return false, tuskerError(errorInvalidArg, "conditional interrupt requires project_id and record_id")
	}
	result, err := s.exec(`UPDATE runs SET
		lease_state = ?,
		lease_owner = ?,
		lease_expires_at = ?,
		attempt_outcome = ?,
		active_attempt_id = ?,
		process_pid = ?,
		process_pgid = ?,
		process_started_at = ?,
		prompt_path = ?,
		event_sink_path = ?,
		raw_log_path = ?,
		status_path = ?,
		next_retry_at = ?,
		last_error = ?,
		terminal = ?,
		updated_at = ?
		WHERE project_id = ? AND record_id = ?
			AND lease_state = ?
			AND lease_owner = ?
			AND lease_generation = ?
			AND active_attempt_id = ?
			AND work_revision = ?
			AND process_pid = ?
			AND process_pgid = ?
			AND process_started_at = ?`,
		interrupted.LeaseState, interrupted.LeaseOwner, interrupted.LeaseExpiresAt,
		interrupted.AttemptOutcome, interrupted.ActiveAttemptID,
		interrupted.ProcessPID, interrupted.ProcessPGID, interrupted.ProcessStartedAt,
		interrupted.PromptPath, interrupted.EventSinkPath, interrupted.RawLogPath, interrupted.StatusPath,
		interrupted.NextRetryAt, interrupted.LastError, boolToInt(interrupted.Terminal), interrupted.UpdatedAt,
		expected.ProjectID, expected.RecordID,
		expected.LeaseState, expected.LeaseOwner, expected.LeaseGeneration,
		expected.ActiveAttemptID, expected.WorkRevision,
		expected.ProcessPID, expected.ProcessPGID, expected.ProcessStartedAt)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected > 0 && !runConsumesDispatchCapacity(interrupted) {
		if err := s.releaseInactiveTaskResourceLeases(interrupted.ProjectID, interrupted.RecordID, "run interruption released dispatch capacity", time.Now().UTC()); err != nil {
			return false, err
		}
	}
	return affected > 0, nil
}

func (s *RuntimeStore) RenewRunLease(input RuntimeLeaseRenewal) (bool, error) {
	if strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.RecordID) == "" {
		return false, tuskerError(errorInvalidArg, "lease renewal requires project_id and record_id")
	}
	if strings.TrimSpace(input.Owner) == "" {
		return false, tuskerError(errorInvalidArg, "lease renewal requires owner")
	}
	if input.Generation <= 0 {
		return false, tuskerError(errorInvalidArg, "lease renewal requires generation")
	}
	if !input.Dispatchable {
		return false, nil
	}
	now := input.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	ttl := input.TTL
	if ttl <= 0 {
		ttl = defaultRunLeaseTTL
	}
	host := runtimeLeaseHost()
	result, err := s.exec(`UPDATE runs
		SET lease_state = CASE WHEN lease_state = 'claimed' THEN 'running' ELSE lease_state END,
			lease_expires_at = ?,
			lease_host = ?,
			last_heartbeat_at = ?,
			process_pid = CASE WHEN ? > 0 THEN ? ELSE process_pid END,
			process_pgid = CASE WHEN ? > 0 THEN ? ELSE process_pgid END,
			process_started_at = CASE WHEN ? != '' THEN ? ELSE process_started_at END,
			updated_at = ?
		WHERE project_id = ? AND record_id = ?
			AND lease_owner = ? AND lease_generation = ?
			AND lease_state IN ('claimed', 'running')`,
		now.Add(ttl).Format(time.RFC3339), host, now.Format(time.RFC3339),
		input.ProcessPID, input.ProcessPID, input.ProcessPGID, input.ProcessPGID, input.ProcessStarted, input.ProcessStarted, now.Format(time.RFC3339),
		input.ProjectID, input.RecordID, input.Owner, input.Generation)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

func (s *RuntimeStore) ReclaimExpiredRunLease(projectID, recordID string, now time.Time, ttl time.Duration, reason string) (bool, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(recordID) == "" {
		return false, tuskerError(errorInvalidArg, "lease reclaim requires project_id and record_id")
	}
	if ttl <= 0 {
		ttl = defaultRunLeaseTTL
	}
	now = now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	cutoff := now.Add(-2 * ttl).Format(time.RFC3339)
	if strings.TrimSpace(reason) == "" {
		reason = "lease expired past reclaim grace"
	}
	result, err := s.exec(`UPDATE runs
		SET lease_state = 'interrupted',
			attempt_outcome = 'interrupted',
			next_retry_at = '',
			last_error = ?,
			process_pid = 0,
			process_pgid = 0,
			process_started_at = '',
			lease_owner = '',
			lease_expires_at = '',
			updated_at = ?
		WHERE project_id = ? AND record_id = ?
			AND lease_state IN ('claimed', 'running')
			AND lease_expires_at != ''
			AND lease_expires_at < ?`,
		reason, now.Format(time.RFC3339), projectID, recordID, cutoff)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected > 0 {
		if err := s.releaseInactiveTaskResourceLeases(projectID, recordID, "expired run reclaim released dispatch capacity", now); err != nil {
			return false, err
		}
	}
	return affected > 0, nil
}

func (s *RuntimeStore) CheckRunLeaseGeneration(projectID, recordID string, generation int) error {
	if generation <= 0 {
		return nil
	}
	var current int
	err := s.queryRowScan(`SELECT lease_generation FROM runs WHERE project_id = ? AND record_id = ?`, []any{projectID, recordID}, &current)
	if errors.Is(err, sql.ErrNoRows) {
		return tuskerError(errorNotFound, "run not found: "+recordID)
	}
	if err != nil {
		return err
	}
	if current != generation {
		return tuskerError(errorInvalidTransition, fmt.Sprintf("stale lease generation: got %d, current %d", generation, current))
	}
	return nil
}

func (s *RuntimeStore) SaveAttempt(attempt RunAttempt) error {
	if attempt.EndStateJSON == "" && attempt.EndState.Schema != "" {
		if encoded, err := json.Marshal(attempt.EndState); err == nil {
			attempt.EndStateJSON = string(encoded)
		}
	}
	_, err := s.exec(`INSERT INTO attempts (
		attempt_id, project_id, record_id, item_id, runner, lane, worker_policy_fingerprint, work_revision, workspace_path, session_ref, parent_attempt_id, child_type, branch_name, merge_rule, fanout_group, cloud_task_id, cloud_status, cloud_environment_id, cloud_attempt_number, pull_request_url, apply_ref, logs_summary, final_summary, end_state_json, process_pid, outcome, exit_code, turns_used, prompt_path, event_sink_path, raw_log_path, status_path, last_error, started_at, finished_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(attempt_id) DO UPDATE SET
		project_id=excluded.project_id,
		record_id=excluded.record_id,
		item_id=excluded.item_id,
		runner=excluded.runner,
		lane=excluded.lane,
		worker_policy_fingerprint=excluded.worker_policy_fingerprint,
		work_revision=excluded.work_revision,
		workspace_path=excluded.workspace_path,
		session_ref=excluded.session_ref,
		parent_attempt_id=excluded.parent_attempt_id,
		child_type=excluded.child_type,
		branch_name=excluded.branch_name,
		merge_rule=excluded.merge_rule,
		fanout_group=excluded.fanout_group,
		cloud_task_id=excluded.cloud_task_id,
		cloud_status=excluded.cloud_status,
		cloud_environment_id=excluded.cloud_environment_id,
		cloud_attempt_number=excluded.cloud_attempt_number,
		pull_request_url=excluded.pull_request_url,
		apply_ref=excluded.apply_ref,
		logs_summary=excluded.logs_summary,
		final_summary=excluded.final_summary,
		end_state_json=excluded.end_state_json,
		process_pid=excluded.process_pid,
		outcome=excluded.outcome,
		exit_code=excluded.exit_code,
		turns_used=excluded.turns_used,
		prompt_path=excluded.prompt_path,
		event_sink_path=excluded.event_sink_path,
		raw_log_path=excluded.raw_log_path,
		status_path=excluded.status_path,
		last_error=excluded.last_error,
		started_at=excluded.started_at,
		finished_at=excluded.finished_at`,
		attempt.AttemptID, attempt.ProjectID, attempt.RecordID, attempt.ItemID, attempt.Runner, attempt.Lane, attempt.WorkerPolicyFP, attempt.WorkRevision, attempt.WorkspacePath, attempt.SessionRef, attempt.ParentAttemptID, attempt.ChildType, attempt.BranchName, attempt.MergeRule, attempt.FanoutGroup, attempt.CloudTaskID, attempt.CloudStatus, attempt.CloudEnvironmentID, attempt.CloudAttemptNumber, attempt.PullRequestURL, attempt.ApplyRef, attempt.LogsSummary, attempt.FinalSummary, attempt.EndStateJSON, attempt.ProcessPID, attempt.Outcome, attempt.ExitCode, attempt.TurnsUsed, attempt.PromptPath, attempt.EventSinkPath, attempt.RawLogPath, attempt.StatusPath, attempt.LastError, attempt.StartedAt, attempt.FinishedAt)
	return err
}

func (s *RuntimeStore) SaveTurn(turn RunTurn) error {
	if strings.TrimSpace(turn.TurnID) == "" {
		return tuskerError(errorInvalidArg, "turn_id is required")
	}
	if strings.TrimSpace(turn.AttemptID) == "" {
		return tuskerError(errorInvalidArg, "attempt_id is required")
	}
	if turn.LeaseGeneration > 0 {
		if err := s.CheckRunLeaseGeneration(turn.ProjectID, turn.RecordID, turn.LeaseGeneration); err != nil {
			return err
		}
	}
	if strings.TrimSpace(turn.LastEventAt) == "" {
		turn.LastEventAt = time.Now().UTC().Format(time.RFC3339)
	}
	_, err := s.exec(`INSERT INTO turns (
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
	err := s.queryRowScan(`SELECT COALESCE(MAX(turn_index) + 1, 0)
		FROM turns
		WHERE project_id = ? AND record_id = ? AND attempt_id = ?`, []any{projectID, recordID, attemptID}, &index)
	return index, err
}

func (s *RuntimeStore) ListTurnsForRun(projectID, recordID string) ([]RunTurn, error) {
	rows, err := s.query(`SELECT attempt_id, project_id, record_id, turn_id, turn_index, session_ref, status, input_tokens, output_tokens, total_tokens, started_at, completed_at, last_event_at, last_error
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
	rows, err := s.query(`SELECT attempt_id, project_id, record_id, turn_id, turn_index, session_ref, status, input_tokens, output_tokens, total_tokens, started_at, completed_at, last_event_at, last_error
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
	_, err := s.exec(`INSERT INTO supervisor_decisions (
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
	rows, err := s.query(`SELECT decision_id, project_id, record_id, item_id, runner, work_revision, attempt_id, parent_attempt_id, session_ref, parent_session_ref, target_attempt_id, target_session_ref, kind, reason, branch_name, workspace_path, validation_delta, merge_rule, lease_state, context_signal, input_tokens, output_tokens, total_tokens, context_window_tokens, created_at
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
	rows, err := s.query(`SELECT decision_id, project_id, record_id, item_id, runner, work_revision, attempt_id, parent_attempt_id, session_ref, parent_session_ref, target_attempt_id, target_session_ref, kind, reason, branch_name, workspace_path, validation_delta, merge_rule, lease_state, context_signal, input_tokens, output_tokens, total_tokens, context_window_tokens, created_at
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

func (s *RuntimeStore) UpsertApplyInput(input RuntimeApplyInput) (RuntimeApplyInput, error) {
	if strings.TrimSpace(input.ProjectID) == "" {
		return input, tuskerError(errorInvalidArg, "apply input project_id is required")
	}
	if strings.TrimSpace(input.RecordID) == "" {
		return input, tuskerError(errorInvalidArg, "apply input record_id is required")
	}
	if strings.TrimSpace(input.Path) == "" {
		return input, tuskerError(errorInvalidArg, "apply input path is required")
	}
	if strings.TrimSpace(input.Sha256) == "" {
		return input, tuskerError(errorInvalidArg, "apply input sha256 is required")
	}
	if strings.TrimSpace(input.Kind) == "" {
		input.Kind = "patch"
	}
	if strings.TrimSpace(input.CreatedAt) == "" {
		input.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	_, err := s.exec(`INSERT INTO apply_inputs (
		project_id, record_id, item_id, runner, job_id, attempt_id, path, rel_path, sha256, kind, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(project_id, record_id, sha256, path) DO UPDATE SET
		item_id=excluded.item_id,
		runner=excluded.runner,
		job_id=excluded.job_id,
		attempt_id=excluded.attempt_id,
		rel_path=excluded.rel_path,
		kind=excluded.kind`,
		input.ProjectID, input.RecordID, input.ItemID, input.Runner, input.JobID, input.AttemptID, input.Path, input.RelPath, input.Sha256, input.Kind, input.CreatedAt)
	return input, err
}

func (s *RuntimeStore) ListApplyInputsForRun(projectID, recordID string) ([]RuntimeApplyInput, error) {
	rows, err := s.query(`SELECT project_id, record_id, item_id, runner, job_id, attempt_id, path, rel_path, sha256, kind, created_at
		FROM apply_inputs
		WHERE project_id = ? AND record_id = ?
		ORDER BY created_at ASC, rel_path ASC, path ASC`, projectID, recordID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RuntimeApplyInput
	for rows.Next() {
		var input RuntimeApplyInput
		if err := rows.Scan(&input.ProjectID, &input.RecordID, &input.ItemID, &input.Runner, &input.JobID, &input.AttemptID, &input.Path, &input.RelPath, &input.Sha256, &input.Kind, &input.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, input)
	}
	return out, rows.Err()
}

func (s *RuntimeStore) SaveExternalLoopEvent(event ExternalLoopEvent) (ExternalLoopEvent, bool, error) {
	if strings.TrimSpace(event.ProjectID) == "" {
		return event, false, tuskerError(errorInvalidArg, "project_id is required")
	}
	if strings.TrimSpace(event.RecordID) == "" {
		return event, false, tuskerError(errorInvalidArg, "record_id is required")
	}
	if strings.TrimSpace(event.Stage) == "" {
		return event, false, tuskerError(errorInvalidArg, "external loop stage is required")
	}
	if strings.TrimSpace(event.Action) == "" {
		return event, false, tuskerError(errorInvalidArg, "external loop action is required")
	}
	if strings.TrimSpace(event.IdempotencyKey) == "" {
		event.IdempotencyKey = strings.Join([]string{event.Stage, event.Action, event.JobID, event.AttemptID, event.Reason}, "|")
	}
	if existing, err := s.FindExternalLoopEventByKey(event.ProjectID, event.RecordID, event.IdempotencyKey); err != nil {
		return event, false, err
	} else if existing != nil {
		return *existing, false, nil
	}
	if strings.TrimSpace(event.EventID) == "" {
		event.EventID = newRecordID()
	}
	if strings.TrimSpace(event.CreatedAt) == "" {
		event.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	_, err := s.exec(`INSERT INTO external_loop_events (
		event_id, project_id, record_id, item_id, runner, job_id, attempt_id, stage, action, status, reason, payload_json, idempotency_key, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.EventID, event.ProjectID, event.RecordID, event.ItemID, event.Runner, event.JobID, event.AttemptID, event.Stage, event.Action, event.Status, event.Reason, event.PayloadJSON, event.IdempotencyKey, event.CreatedAt)
	return event, true, err
}

func (s *RuntimeStore) FindExternalLoopEventByKey(projectID, recordID, idempotencyKey string) (*ExternalLoopEvent, error) {
	projectID = strings.TrimSpace(projectID)
	recordID = strings.TrimSpace(recordID)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if projectID == "" || recordID == "" || idempotencyKey == "" {
		return nil, nil
	}
	var event ExternalLoopEvent
	err := s.queryRowScan(`SELECT event_id, project_id, record_id, item_id, runner, job_id, attempt_id, stage, action, status, reason, payload_json, idempotency_key, created_at
		FROM external_loop_events
		WHERE project_id = ? AND record_id = ? AND idempotency_key = ?`, []any{projectID, recordID, idempotencyKey}, &event.EventID, &event.ProjectID, &event.RecordID, &event.ItemID, &event.Runner, &event.JobID, &event.AttemptID, &event.Stage, &event.Action, &event.Status, &event.Reason, &event.PayloadJSON, &event.IdempotencyKey, &event.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &event, nil
}

func (s *RuntimeStore) ListExternalLoopEvents(projectID, recordID string) ([]ExternalLoopEvent, error) {
	rows, err := s.query(`SELECT event_id, project_id, record_id, item_id, runner, job_id, attempt_id, stage, action, status, reason, payload_json, idempotency_key, created_at
		FROM external_loop_events
		WHERE project_id = ? AND record_id = ?
		ORDER BY created_at ASC, event_id ASC`, projectID, recordID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ExternalLoopEvent
	for rows.Next() {
		var event ExternalLoopEvent
		if err := rows.Scan(&event.EventID, &event.ProjectID, &event.RecordID, &event.ItemID, &event.Runner, &event.JobID, &event.AttemptID, &event.Stage, &event.Action, &event.Status, &event.Reason, &event.PayloadJSON, &event.IdempotencyKey, &event.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

func (s *RuntimeStore) SaveSession(session RunnerSession) error {
	_, err := s.exec(`INSERT INTO sessions (
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
	var session RunnerSession
	var resumable int
	err := s.queryRowScan(`SELECT project_id, record_id, runner, session_ref, last_message_ref, workspace_path, current_item_id, work_revision, last_attempt_id, state, resumable, started_at, last_seen_at, ended_at, last_error
		FROM sessions
		WHERE project_id = ? AND record_id = ? AND runner = ?
		ORDER BY last_seen_at DESC, started_at DESC
		LIMIT 1`, []any{projectID, recordID, runner}, &session.ProjectID, &session.RecordID, &session.Runner, &session.SessionRef, &session.LastMessageRef, &session.WorkspacePath, &session.CurrentItemID, &session.WorkRevision, &session.LastAttemptID, &session.State, &resumable, &session.StartedAt, &session.LastSeenAt, &session.EndedAt, &session.LastError)
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
	rows, err := s.query(query, args...)
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
	_, err := s.exec(`UPDATE sessions
		SET state = ?, ended_at = ?, last_error = ?, resumable = ?, last_seen_at = ?
		WHERE project_id = ? AND session_ref = ?`,
		state, endedAt, lastError, boolToInt(resumable), time.Now().UTC().Format(time.RFC3339), projectID, sessionRef)
	return err
}

func (s *RuntimeStore) ForceRetryNow(identity string) (bool, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := s.exec(`UPDATE runs SET lease_state = ?, next_retry_at = ?, updated_at = ? WHERE item_id = ? OR record_id = ?`, string(LeaseStateRetryQueued), now, now, identity, identity)
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
	err := s.queryRowScan(`SELECT value FROM daemon_settings WHERE key = ?`, []any{key}, &value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return value, err
}

func (s *RuntimeStore) SetSetting(key, value string) error {
	_, err := s.exec(`INSERT INTO daemon_settings (key, value)
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
	rows, err := s.query(`SELECT record_id FROM runs WHERE project_id = ?`, projectID)
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
		if _, err := s.exec(`DELETE FROM external_loop_events WHERE project_id = ? AND record_id = ?`, projectID, recordID); err != nil {
			return err
		}
		if _, err := s.exec(`DELETE FROM supervisor_decisions WHERE project_id = ? AND record_id = ?`, projectID, recordID); err != nil {
			return err
		}
		if _, err := s.exec(`DELETE FROM runs WHERE project_id = ? AND record_id = ?`, projectID, recordID); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (s *RuntimeStore) DaemonStatus() (map[string]any, error) {
	liveness := readDaemonLiveness(s.stateRoot, time.Now().UTC())
	var projectCount int
	if err := s.queryRowScan(`SELECT COUNT(*) FROM projects`, nil, &projectCount); err != nil {
		return nil, err
	}
	projects, err := s.ListProjects()
	if err != nil {
		return nil, err
	}
	var runCount int
	if err := s.queryRowScan(`SELECT COUNT(*) FROM runs WHERE terminal = 0 AND lease_state IN ('claimed', 'running')`, nil, &runCount); err != nil {
		return nil, err
	}
	var parkedRunCount int
	if err := s.queryRowScan(`SELECT COUNT(*) FROM runs WHERE lease_state = ?`, []any{string(LeaseStateParkedNoProgress)}, &parkedRunCount); err != nil {
		return nil, err
	}
	var parkedNoProgressCount int
	if err := s.queryRowScan(`SELECT COUNT(*) FROM runs WHERE lease_state = 'parked_no_progress'`, nil, &parkedNoProgressCount); err != nil {
		return nil, err
	}
	globalReport, err := configResolveForRepo("", false, "runtime.max_active_runs")
	if err != nil {
		return nil, err
	}
	globalLimit := intFromAny(globalReport.Value)
	lastPollAt, err := s.GetSetting("daemon_last_poll_at")
	if err != nil {
		return nil, err
	}
	watchdogBeatAt, err := s.GetSetting("daemon_watchdog_beat_at")
	if err != nil {
		return nil, err
	}
	invariantCircuit, err := s.ReadInvariantCircuitStatus()
	if err != nil {
		return nil, err
	}
	crashLoop, err := s.ReadCrashLoopStatus()
	if err != nil {
		return nil, err
	}
	lastRestartCause, err := s.GetSetting(daemonLastRestartCauseKey)
	if err != nil {
		return nil, err
	}
	launchdInstalled, launchdPlistPath := false, ""
	if installed, path, err := daemonLaunchdInstalled(); err == nil {
		launchdInstalled = installed
		launchdPlistPath = path
	}
	invariantCircuitReason := ""
	if invariantCircuit.Open {
		invariantCircuitReason = invariantCircuitSummary(invariantCircuit)
	}
	diskPressure, err := s.DiskPressureStatus()
	if err != nil {
		return nil, err
	}
	source := "daemon.db"
	if globalLimit <= 0 {
		globalLimit = 2
		source = configSourceBuiltIn
	}
	daemonRunMode := "manual"
	if liveness.ManagedByLaunchd {
		daemonRunMode = "launchd"
	}
	return map[string]any{
		"state_root":                s.stateRoot,
		"runtime_store_path":        liveness.RuntimeStorePath,
		"daemon_alive":              liveness.Alive,
		"daemon_pid":                liveness.PID,
		"daemon_started_at":         liveness.StartedAt,
		"daemon_uptime_seconds":     liveness.UptimeSeconds,
		"daemon_serve_enabled":      liveness.ServeEnabled,
		"daemon_serve_addr":         liveness.ServeAddr,
		"daemon_serve_url":          daemonServeURL(liveness.ServeAddr),
		"daemon_managed_by_launchd": liveness.ManagedByLaunchd,
		"daemon_run_mode":           daemonRunMode,
		"daemon_last_restart_cause": lastRestartCause,
		"launchd_installed":         launchdInstalled,
		"launchd_plist_path":        launchdPlistPath,
		"daemon_last_poll_at":       lastPollAt,
		"daemon_watchdog_beat_at":   watchdogBeatAt,
		"projects":                  projectCount,
		"activeRuns":                runCount,
		"parkedNoProgressRuns":      parkedNoProgressCount,
		"max_active_runs":           globalLimit,
		"limit_source":              source,
		"default_limit_value":       2,
		"project_health":            projects,
		"crashLoop":                 crashLoop,
		"invariantCircuit":          invariantCircuit,
		"invariant_circuit_open":    invariantCircuit.Open,
		"invariant_circuit_reason":  invariantCircuitReason,
		"disk_pressure":             diskPressure,
		"disk_pressure_state":       diskPressure.State,
		"disk_pressure_paused":      diskPressure.DispatchPaused,
	}, nil
}

func (s *RuntimeStore) TouchProjectPoll(projectID string) error {
	_, err := s.exec(`UPDATE projects SET last_poll_at = ?, health = ?, last_error = '' WHERE project_id = ?`, time.Now().UTC().Format(time.RFC3339), string(projectHealthHealthy), projectID)
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
	repoRoot = canonicalProjectPath(repoRoot)
	vaultRoot = canonicalProjectPath(vaultRoot)
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

func (s *RuntimeStore) SaveRunAuthorization(auth RunAuthorization) error {
	if auth.ProjectID == "" || auth.RecordID == "" || auth.LeaseGeneration <= 0 || auth.Source == "" || auth.Actor == "" {
		return tuskerError(errorInvalidArg, "run authorization requires project, task, generation, source, and actor")
	}
	if auth.CreatedAt == "" {
		auth.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	_, err := s.exec(`INSERT INTO run_authorizations(project_id, record_id, lease_generation, source, actor, trigger, project_automation_enabled, created_at)
		VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(project_id, record_id, lease_generation) DO UPDATE SET source=excluded.source, actor=excluded.actor, trigger=excluded.trigger, project_automation_enabled=excluded.project_automation_enabled, created_at=excluded.created_at`,
		auth.ProjectID, auth.RecordID, auth.LeaseGeneration, auth.Source, auth.Actor, auth.Trigger, boolToInt(auth.ProjectAutomationEnabled), auth.CreatedAt)
	return err
}

func (s *RuntimeStore) LatestRunAuthorization(projectID, recordID string) (*RunAuthorization, error) {
	var auth RunAuthorization
	var enabled int
	err := s.queryRowScan(`SELECT project_id, record_id, lease_generation, source, actor, trigger, project_automation_enabled, created_at
		FROM run_authorizations WHERE project_id=? AND record_id=? ORDER BY lease_generation DESC LIMIT 1`, []any{projectID, recordID},
		&auth.ProjectID, &auth.RecordID, &auth.LeaseGeneration, &auth.Source, &auth.Actor, &auth.Trigger, &enabled, &auth.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	auth.ProjectAutomationEnabled = enabled != 0
	return &auth, nil
}

func (s *RuntimeStore) SaveRunIdentity(identity RunIdentityMetadata) error {
	if identity.ProjectID == "" || identity.RecordID == "" || identity.RepoRoot == "" || identity.WorkspacePath == "" || identity.WorkspaceMode == "" || identity.Runner == "" {
		return tuskerError(errorInvalidArg, "run identity requires project, task, repository, workspace, mode, and runner")
	}
	if identity.CreatedAt == "" {
		identity.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	_, err := s.exec(`INSERT INTO run_identity_metadata(project_id,record_id,repo_root,workspace_path,workspace_mode,runner,branch,head,created_at)
		VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(project_id,record_id) DO UPDATE SET repo_root=excluded.repo_root, workspace_path=excluded.workspace_path, workspace_mode=excluded.workspace_mode, runner=excluded.runner, branch=excluded.branch, head=excluded.head`,
		identity.ProjectID, identity.RecordID, identity.RepoRoot, identity.WorkspacePath, identity.WorkspaceMode, identity.Runner, identity.Branch, identity.Head, identity.CreatedAt)
	return err
}

func (s *RuntimeStore) RunIdentity(projectID, recordID string) (*RunIdentityMetadata, error) {
	var identity RunIdentityMetadata
	err := s.queryRowScan(`SELECT project_id,record_id,repo_root,workspace_path,workspace_mode,runner,branch,head,created_at FROM run_identity_metadata WHERE project_id=? AND record_id=?`, []any{projectID, recordID},
		&identity.ProjectID, &identity.RecordID, &identity.RepoRoot, &identity.WorkspacePath, &identity.WorkspaceMode, &identity.Runner, &identity.Branch, &identity.Head, &identity.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &identity, nil
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
