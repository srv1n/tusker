package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	// These limits are conservative enough for unattended local operation while
	// still allowing an agent to complete a meaningful implementation attempt.
	defaultBudgetPerAttemptInputTokens  = 2_000_000
	defaultBudgetPerAttemptOutputTokens = 100_000
	defaultBudgetPerTaskMultiplier      = 3
	defaultBudgetDailyInputTokens       = 20_000_000
	defaultBudgetDailyOutputTokens      = 1_000_000

	budgetCircuitSettingKey = "budget_circuit_status"
)

type RuntimeBudgetConfig struct {
	Enabled                bool `yaml:"enabled" json:"enabled"`
	PerAttemptInputTokens  int  `yaml:"per_attempt_input_tokens" json:"per_attempt_input_tokens"`
	PerAttemptOutputTokens int  `yaml:"per_attempt_output_tokens" json:"per_attempt_output_tokens"`
	PerTaskInputTokens     int  `yaml:"per_task_input_tokens" json:"per_task_input_tokens"`
	PerTaskOutputTokens    int  `yaml:"per_task_output_tokens" json:"per_task_output_tokens"`
	DailyInputTokens       int  `yaml:"daily_input_tokens" json:"daily_input_tokens"`
	DailyOutputTokens      int  `yaml:"daily_output_tokens" json:"daily_output_tokens"`
}

type runtimeBudgetTotals struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type BudgetRedriveRecord struct {
	Actor   string `json:"actor"`
	Reason  string `json:"reason"`
	ResetAt string `json:"reset_at"`
}

type budgetCircuitStatus struct {
	Open              bool                `json:"open"`
	Reason            string              `json:"reason,omitempty"`
	ResetAt           string              `json:"reset_at"`
	InputTokens       int                 `json:"input_tokens"`
	OutputTokens      int                 `json:"output_tokens"`
	InputTokenLimit   int                 `json:"input_token_limit"`
	OutputTokenLimit  int                 `json:"output_token_limit"`
	WindowStartedAt   string              `json:"window_started_at"`
	WindowElapsedSecs int                 `json:"window_elapsed_secs"`
	Budget            RuntimeBudgetConfig `json:"budget"`
}

func defaultRuntimeBudgetConfig() RuntimeBudgetConfig {
	return RuntimeBudgetConfig{
		// Token telemetry is diagnostic-only until runners can report
		// non-cumulative, billable usage. Keep the historical shape so old
		// workflow files still load, but never enable enforcement by default.
		Enabled:                false,
		PerAttemptInputTokens:  defaultBudgetPerAttemptInputTokens,
		PerAttemptOutputTokens: defaultBudgetPerAttemptOutputTokens,
		PerTaskInputTokens:     defaultBudgetPerAttemptInputTokens * defaultBudgetPerTaskMultiplier,
		PerTaskOutputTokens:    defaultBudgetPerAttemptOutputTokens * defaultBudgetPerTaskMultiplier,
		DailyInputTokens:       defaultBudgetDailyInputTokens,
		DailyOutputTokens:      defaultBudgetDailyOutputTokens,
	}
}

func withDefaultRuntimeBudgetConfig(budget RuntimeBudgetConfig) RuntimeBudgetConfig {
	defaults := defaultRuntimeBudgetConfig()
	if budget.PerAttemptInputTokens <= 0 {
		budget.PerAttemptInputTokens = defaults.PerAttemptInputTokens
	}
	if budget.PerAttemptOutputTokens <= 0 {
		budget.PerAttemptOutputTokens = defaults.PerAttemptOutputTokens
	}
	if budget.PerTaskInputTokens <= 0 {
		budget.PerTaskInputTokens = budget.PerAttemptInputTokens * defaultBudgetPerTaskMultiplier
	}
	if budget.PerTaskOutputTokens <= 0 {
		budget.PerTaskOutputTokens = budget.PerAttemptOutputTokens * defaultBudgetPerTaskMultiplier
	}
	if budget.DailyInputTokens <= 0 {
		budget.DailyInputTokens = defaults.DailyInputTokens
	}
	if budget.DailyOutputTokens <= 0 {
		budget.DailyOutputTokens = defaults.DailyOutputTokens
	}
	// A legacy enabled value must migrate to the disabled behavior. Do not
	// rewrite workflow/task data or delete turn history: callers may still use
	// it for forensic inspection once accounting is redesigned.
	budget.Enabled = false
	return budget
}

func resolveRunBudget(wf Workflow, note Note) RuntimeBudgetConfig {
	base := withDefaultRuntimeBudgetConfig(wf.Runtime.Budget)
	budget := base
	if raw, ok := note.Data["budget"]; ok {
		overrides := budgetOverrideMap(raw)
		if len(overrides) > 0 {
			if enabled, ok := budgetBoolOverride(overrides["enabled"]); ok {
				budget.Enabled = enabled
			}
			applyCappedBudgetOverride(overrides, "per_attempt_input_tokens", &budget.PerAttemptInputTokens, base.PerAttemptInputTokens)
			applyCappedBudgetOverride(overrides, "per_attempt_output_tokens", &budget.PerAttemptOutputTokens, base.PerAttemptOutputTokens)
			applyCappedBudgetOverride(overrides, "per_task_input_tokens", &budget.PerTaskInputTokens, base.PerTaskInputTokens)
			applyCappedBudgetOverride(overrides, "per_task_output_tokens", &budget.PerTaskOutputTokens, base.PerTaskOutputTokens)
			applyCappedBudgetOverride(overrides, "daily_input_tokens", &budget.DailyInputTokens, base.DailyInputTokens)
			applyCappedBudgetOverride(overrides, "daily_output_tokens", &budget.DailyOutputTokens, base.DailyOutputTokens)
		}
	}
	return withDefaultRuntimeBudgetConfig(budget)
}

func budgetOverrideMap(value any) map[string]any {
	out := map[string]any{}
	switch v := value.(type) {
	case map[string]any:
		for key, raw := range v {
			out[strings.TrimSpace(key)] = raw
		}
	case map[any]any:
		for key, raw := range v {
			out[strings.TrimSpace(toString(key))] = raw
		}
	}
	return out
}

func budgetBoolOverride(value any) (bool, bool) {
	switch v := value.(type) {
	case bool:
		return v, true
	case string:
		parsed, err := parseBooleanArg(v, false)
		return parsed, err == nil
	default:
		return false, false
	}
}

func applyCappedBudgetOverride(overrides map[string]any, key string, target *int, defaultValue int) {
	raw, ok := overrides[key]
	if !ok {
		return
	}
	value := intValue(raw)
	if value <= 0 {
		return
	}
	capValue := defaultValue * 4
	if capValue > 0 && value > capValue {
		value = capValue
	}
	*target = value
}

func (s *RuntimeStore) SumRunTokens(projectID, recordID, since string) (runtimeBudgetTotals, error) {
	var totals runtimeBudgetTotals
	query := `SELECT COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0), COALESCE(SUM(total_tokens), 0)
		FROM turns WHERE project_id = ? AND record_id = ?`
	args := []any{projectID, recordID}
	if strings.TrimSpace(since) != "" {
		query += ` AND last_event_at >= ?`
		args = append(args, since)
	}
	err := s.queryRowScan(query, args, &totals.InputTokens, &totals.OutputTokens, &totals.TotalTokens)
	return totals, err
}

func (s *RuntimeStore) SumAttemptTokens(attemptID string) (runtimeBudgetTotals, error) {
	var totals runtimeBudgetTotals
	if strings.TrimSpace(attemptID) == "" {
		return totals, nil
	}
	err := s.queryRowScan(`SELECT COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0), COALESCE(SUM(total_tokens), 0)
		FROM turns WHERE attempt_id = ?`, []any{attemptID}, &totals.InputTokens, &totals.OutputTokens, &totals.TotalTokens)
	return totals, err
}

func (s *RuntimeStore) SumTokensSince(since string) (runtimeBudgetTotals, error) {
	var totals runtimeBudgetTotals
	query := `SELECT COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0), COALESCE(SUM(total_tokens), 0) FROM turns`
	var args []any
	if strings.TrimSpace(since) != "" {
		query += ` WHERE last_event_at >= ?`
		args = append(args, since)
	}
	err := s.queryRowScan(query, args, &totals.InputTokens, &totals.OutputTokens, &totals.TotalTokens)
	return totals, err
}

func budgetRedriveSettingKey(projectID, recordID string) string {
	return "budget_redrive:" + strings.TrimSpace(projectID) + ":" + strings.TrimSpace(recordID)
}

func (s *RuntimeStore) BudgetWindowStart(projectID, recordID string) (BudgetRedriveRecord, error) {
	raw, err := s.GetSetting(budgetRedriveSettingKey(projectID, recordID))
	if err != nil || strings.TrimSpace(raw) == "" {
		return BudgetRedriveRecord{}, err
	}
	var record BudgetRedriveRecord
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		return BudgetRedriveRecord{}, err
	}
	return record, nil
}

func (s *RuntimeStore) RecordBudgetRedrive(projectID, recordID, actor, reason string, now time.Time) (BudgetRedriveRecord, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(recordID) == "" {
		return BudgetRedriveRecord{}, tuskerError(errorInvalidArg, "budget redrive requires project_id and record_id")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	record := BudgetRedriveRecord{
		Actor:   firstNonEmpty(strings.TrimSpace(actor), defaultActorName()),
		Reason:  firstNonEmpty(strings.TrimSpace(reason), "budget redrive requested"),
		ResetAt: now.UTC().Format(time.RFC3339),
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return BudgetRedriveRecord{}, err
	}
	return record, s.SetSetting(budgetRedriveSettingKey(projectID, recordID), string(raw))
}

func (s *RuntimeStore) BudgetCircuitStatus(wf Workflow, now time.Time) (budgetCircuitStatus, error) {
	_ = s
	_ = now
	return budgetCircuitStatus{Budget: withDefaultRuntimeBudgetConfig(wf.Runtime.Budget)}, nil
}

func (s *RuntimeStore) SetBudgetCircuitStatus(status budgetCircuitStatus) error {
	raw, err := json.Marshal(status)
	if err != nil {
		return err
	}
	return s.SetSetting(budgetCircuitSettingKey, string(raw))
}

func (s *RuntimeStore) ReadBudgetCircuitStatus() (budgetCircuitStatus, error) {
	// Do not parse a legacy circuit record here. A malformed or stale record is
	// telemetry, not control-plane state, and must never make status/dispatch
	// fail or resurrect a circuit-open banner.
	_ = s
	return budgetCircuitStatus{Budget: defaultRuntimeBudgetConfig()}, nil
}

func budgetLimitBreached(scope string, totals runtimeBudgetTotals, inputLimit, outputLimit int) (bool, string) {
	if inputLimit > 0 && totals.InputTokens > inputLimit {
		return true, fmt.Sprintf("%s input token budget exceeded: %d > %d", scope, totals.InputTokens, inputLimit)
	}
	if outputLimit > 0 && totals.OutputTokens > outputLimit {
		return true, fmt.Sprintf("%s output token budget exceeded: %d > %d", scope, totals.OutputTokens, outputLimit)
	}
	return false, ""
}

func (d *Daemon) refreshBudgetCircuitStatus(wf Workflow, now time.Time) (budgetCircuitStatus, error) {
	if d == nil || d.store == nil {
		return budgetCircuitStatus{}, nil
	}
	status, err := d.store.BudgetCircuitStatus(wf, now)
	if err != nil {
		return status, err
	}
	return status, d.store.SetBudgetCircuitStatus(status)
}

func (d *Daemon) budgetDispatchBlocker(project RegisteredProject, wf Workflow, note Note, run RunStatus, now time.Time) (RunStatus, string, bool, error) {
	_ = d
	_ = project
	_ = wf
	_ = note
	_ = now
	return run, "", false, nil
}

func (d *Daemon) enforceBudgetForRun(ctx context.Context, project RegisteredProject, wf Workflow, note Note, run RunStatus) (RunStatus, bool, error) {
	_ = d
	_ = ctx
	_ = project
	_ = wf
	_ = note
	return run, false, nil
}

func releaseLegacyBudgetPark(run RunStatus, now time.Time) RunStatus {
	run.LeaseState = string(LeaseStateUnclaimed)
	run.AttemptOutcome = string(AttemptOutcomeNone)
	run.NextRetryAt = ""
	run.LastError = "legacy token budget park released: token telemetry is diagnostic-only"
	run.UpdatedAt = now.UTC().Format(time.RFC3339)
	run.Terminal = false
	clearActiveExecution(&run)
	return run
}

func (d *Daemon) enforceTurnCapForRun(ctx context.Context, project RegisteredProject, wf Workflow, run RunStatus) (RunStatus, bool, error) {
	if d == nil || d.store == nil || RunnerName(strings.TrimSpace(run.Runner)) != RunnerCodexExec || !isDispatchingLeaseState(run.LeaseState) {
		return run, false, nil
	}
	maxTurns := codexPolicyForLane(codexPolicyFromWorkflow(wf), run.Lane).MaxTurns
	if maxTurns <= 0 {
		return run, false, nil
	}
	turns, err := d.store.ListTurnsForAttempt(run.ActiveAttemptID)
	if err != nil {
		return run, false, err
	}
	if len(turns) <= maxTurns {
		return run, false, nil
	}
	reason := fmt.Sprintf("turn cap exhausted for attempt %s: observed %d/%d turns", run.ActiveAttemptID, len(turns), maxTurns)
	if _, stopErr := d.stopRunExecution(ctx, run); stopErr != nil {
		reason += ": " + stopErr.Error()
	}
	now := time.Now().UTC().Format(time.RFC3339)
	updateRunAttemptFromRun(d.store, run, AttemptOutcomeTurnCapExhausted, exitCodeForOutcome(AttemptOutcomeTurnCapExhausted), reason, now)
	run = d.scheduleRetry(run, wf, reason)
	if strings.TrimSpace(run.SessionRef) != "" {
		_ = d.store.MarkSessionState(project.ProjectID, run.SessionRef, sessionStateForLeaseState(LeaseState(run.LeaseState)), "", reason, runSessionResumable(wf, run))
	}
	run.UpdatedAt = now
	return run, true, nil
}

func parkBudgetRun(run RunStatus, reason string) RunStatus {
	now := time.Now().UTC()
	run.LeaseState = string(LeaseStateParkedBudget)
	run.AttemptOutcome = string(AttemptOutcomeBudgetExceeded)
	run.LastError = reason
	run.NextRetryAt = ""
	run.UpdatedAt = now.Format(time.RFC3339)
	run.Terminal = true
	clearActiveExecution(&run)
	return run
}
