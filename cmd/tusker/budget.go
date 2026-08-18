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

type BudgetRedriveRecord struct {
	Actor   string `json:"actor"`
	Reason  string `json:"reason"`
	ResetAt string `json:"reset_at"`
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
