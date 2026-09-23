package main

import (
	"encoding/json"
	"io"
	"os"
	"strings"
)

// RunFailureReasonCode is the closed set of driver reported terminal reasons.
type RunFailureReasonCode string

const (
	RunFailureUsageLimit       RunFailureReasonCode = "usage_limit"
	RunFailureAuthExpired      RunFailureReasonCode = "auth_expired"
	RunFailurePermissionDenied RunFailureReasonCode = "permission_denied"
	RunFailureSandboxDenied    RunFailureReasonCode = "sandbox_denied"
	RunFailureNetworkDenied    RunFailureReasonCode = "network_denied"
	RunFailureMissingAccess    RunFailureReasonCode = "missing_access"
	RunFailureContextWindow    RunFailureReasonCode = "context_window"
	RunFailureMaxTurns         RunFailureReasonCode = "max_turns"
	RunFailureMaxBudget        RunFailureReasonCode = "max_budget"
	RunFailureConfigInvalid    RunFailureReasonCode = "config_invalid"
	RunFailureProviderError    RunFailureReasonCode = "provider_error"
	RunFailureProcessLost      RunFailureReasonCode = "process_lost"
	RunFailureOutcomeUnknown   RunFailureReasonCode = "outcome_unknown"
	RunFailureCancelled        RunFailureReasonCode = "cancelled"
	RunFailureUnknown          RunFailureReasonCode = "unknown"
)

type runFailureReasonSpec struct {
	Class     string
	Retryable bool
	Outcome   AttemptOutcome
	Guidance  string
}

var runFailureReasons = map[RunFailureReasonCode]runFailureReasonSpec{
	RunFailureUsageLimit:       {"blocked", false, AttemptOutcomeBlocked, "The provider usage limit was reached. Wait for the limit to reset or switch profile, then Continue."},
	RunFailureAuthExpired:      {"blocked", false, AttemptOutcomeBlocked, "Provider authentication expired. Sign in again, then Continue."},
	RunFailurePermissionDenied: {"blocked", false, AttemptOutcomeBlocked, "A required permission was denied. Grant it, then Continue."},
	RunFailureSandboxDenied:    {"blocked", false, AttemptOutcomeBlocked, "The sandbox denied an operation. Adjust the sandbox policy, then Continue."},
	RunFailureNetworkDenied:    {"blocked", false, AttemptOutcomeBlocked, "Network access was denied. Restore access, then Continue."},
	RunFailureMissingAccess:    {"blocked", false, AttemptOutcomeBlocked, "The provider lacks required access. Grant access, then Continue."},
	RunFailureContextWindow:    {"failed", false, AttemptOutcomeBlocked, "The context window is full. Audit the context, then Continue in a fresh thread."},
	RunFailureMaxTurns:         {"failed", true, AttemptOutcomeTurnCapExhausted, "The turn cap was reached. Raise the cap or Continue."},
	RunFailureMaxBudget:        {"blocked", false, AttemptOutcomeBlocked, "The run budget was exhausted. Raise the budget, then Continue."},
	RunFailureConfigInvalid:    {"blocked", false, AttemptOutcomeBlocked, "The runner configuration is invalid. Fix it, then Continue."},
	RunFailureProviderError:    {"failed", true, AttemptOutcomeFailed, "The provider failed. Inspect the last events, then Continue."},
	RunFailureProcessLost:      {"lost", false, AttemptOutcomeUnknown, "The runner process was lost. Inspect the attempt before Continuing."},
	RunFailureOutcomeUnknown:   {"lost", false, AttemptOutcomeUnknown, "The outcome is unknown. Inspect provider state before Continuing."},
	RunFailureCancelled:        {"failed", false, AttemptOutcomeCancelled, "The run was cancelled. Continue if work should resume."},
	RunFailureUnknown:          {"failed", true, AttemptOutcomeFailed, "The failure is unclassified. Inspect the last events, then Continue."},
}

func runFailureReason(code RunFailureReasonCode) (runFailureReasonSpec, bool) {
	spec, ok := runFailureReasons[code]
	return spec, ok
}

func codexExecFailureCode(message string) RunFailureReasonCode {
	text := strings.ToLower(message)
	switch {
	case strings.Contains(text, "usage limit"), strings.Contains(text, "rate limit"), strings.Contains(text, "too many requests"):
		return RunFailureUsageLimit
	case strings.Contains(text, "401"), strings.Contains(text, "unauthorized"), strings.Contains(text, "login"), strings.Contains(text, "sign in"):
		return RunFailureAuthExpired
	case strings.Contains(text, "sandbox"), strings.Contains(text, "permission"):
		return RunFailureSandboxDenied
	default:
		return RunFailureProviderError
	}
}

// Codex exec reports terminal provider failures only in its JSONL stream.
func codexExecFailureFromLog(path string) (RunFailureReasonCode, string) {
	file, err := os.Open(path)
	if err != nil {
		return RunFailureProviderError, ""
	}
	defer file.Close()
	if info, err := file.Stat(); err == nil && info.Size() > 64*1024 {
		_, _ = file.Seek(-64*1024, io.SeekEnd)
	}
	raw, err := io.ReadAll(io.LimitReader(file, 64*1024))
	if err != nil {
		return RunFailureProviderError, ""
	}
	lines := strings.Split(string(raw), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		var event struct {
			Type    string `json:"type"`
			Message string `json:"message"`
			Error   struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal([]byte(lines[i]), &event) != nil || (event.Type != "error" && event.Type != "turn.failed") {
			continue
		}
		message := firstNonEmpty(event.Message, event.Error.Message)
		if message != "" {
			return codexExecFailureCode(message), message
		}
	}
	return RunFailureProviderError, ""
}
