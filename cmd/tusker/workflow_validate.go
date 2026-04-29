package main

import (
	"fmt"
	"regexp"
	"strings"
)

var extensionNamePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[._:-][a-z0-9]+)*$`)

func validateWorkflowFile(wf WorkflowFile) error {
	return validateWorkflow(wf.Data, wf.Path, wf.Body)
}

func validateWorkflow(wf Workflow, filePath, body string) error {
	if wf.WorkflowVersion != 1 {
		return tuskerError(errorConfigInvalid, "workflow_version must be 1", withPath(filePath))
	}
	if wf.Tracker.Kind != "tusker_vault" {
		return tuskerError(errorConfigInvalid, "tracker.kind must be tusker_vault", withPath(filePath))
	}
	if wf.TrackerSchemaVersion != 2 {
		return tuskerError(errorConfigInvalid, "tracker_schema_version must be 2", withPath(filePath))
	}
	if len(wf.Tracker.ActiveStates) == 0 {
		return tuskerError(errorConfigInvalid, "tracker.active_states must not be empty", withPath(filePath))
	}
	if len(wf.Agents.Enabled) == 0 {
		return tuskerError(errorConfigInvalid, "agents.enabled must not be empty", withPath(filePath))
	}
	knownStates := map[string]struct{}{}
	for _, state := range append(append([]string{}, wf.Tracker.ActiveStates...), append(wf.Tracker.ReviewStates, wf.Tracker.TerminalStates...)...) {
		knownStates[strings.TrimSpace(state)] = struct{}{}
	}
	for state := range wf.Agents.MaxConcurrentAgentsByState {
		if _, ok := knownStates[strings.TrimSpace(state)]; !ok {
			return tuskerError(errorConfigInvalid, "agents.max_concurrent_agents_by_state references unknown tracker state "+state, withPath(filePath))
		}
	}
	if wf.Runtime.PollIntervalMS <= 0 || wf.Runtime.LeaseTTLMS <= 0 {
		return tuskerError(errorConfigInvalid, "runtime.poll_interval_ms and runtime.lease_ttl_ms must be > 0", withPath(filePath))
	}
	if wf.Runtime.MaxActiveRunsPerProject <= 0 {
		return tuskerError(errorConfigInvalid, "runtime.max_active_runs_per_project must be > 0", withPath(filePath))
	}
	if wf.Workspace.Root == "" || wf.Workspace.Strategy == "" {
		return tuskerError(errorConfigInvalid, "workspace.root and workspace.strategy are required", withPath(filePath))
	}
	if wf.Retry.MaxAttempts <= 0 || len(wf.Retry.BackoffMS) == 0 {
		return tuskerError(errorConfigInvalid, "retry.max_attempts and retry.backoff_ms are required", withPath(filePath))
	}
	if strings.TrimSpace(wf.Codex.Command) == "" {
		return tuskerError(errorConfigInvalid, "codex.command is required", withPath(filePath))
	}
	if !validCodexApprovalPolicy(wf.Codex.ApprovalPolicy) {
		return tuskerError(errorConfigInvalid, "codex.approval_policy must be one of untrusted, on-request, on-failure, never", withPath(filePath))
	}
	if !validCodexSandbox(wf.Codex.ThreadSandbox) {
		return tuskerError(errorConfigInvalid, "codex.thread_sandbox must be one of read-only, workspace-write, danger-full-access", withPath(filePath))
	}
	if !validCodexSandbox(wf.Codex.TurnSandboxPolicy) {
		return tuskerError(errorConfigInvalid, "codex.turn_sandbox_policy must be one of read-only, workspace-write, danger-full-access", withPath(filePath))
	}
	if wf.Codex.TurnTimeoutMS <= 0 || wf.Codex.ReadTimeoutMS <= 0 || wf.Codex.StallTimeoutMS <= 0 || wf.Codex.MaxTurns <= 0 {
		return tuskerError(errorConfigInvalid, "codex turn/read/stall timeouts and max_turns must be > 0", withPath(filePath))
	}
	if err := validateExtensionPolicy(wf.Extensions, filePath); err != nil {
		return err
	}
	for _, section := range []string{"## Routing", "## Prompt", "## Retry policy", "## Human override policy"} {
		if findHeading(body, section) == nil {
			return tuskerError(errorConfigInvalid, fmt.Sprintf("WORKFLOW.md missing required section %q", section), withPath(filePath))
		}
	}
	if strings.TrimSpace(wf.Agents.Default) == "" {
		return tuskerError(errorConfigInvalid, "agents.default is required", withPath(filePath))
	}
	return nil
}

func validateExtensionPolicy(policy ExtensionPolicy, filePath string) error {
	if err := validateExtensionNames("extensions.allowed_tools", policy.AllowedTools, filePath); err != nil {
		return err
	}
	if err := validateExtensionNames("extensions.allowed_mcps", policy.AllowedMCPs, filePath); err != nil {
		return err
	}
	if policy.Enabled && policy.AllowTuskerReadTools && !extensionListAllows(policy.AllowedTools, "tusker.show_current") {
		return tuskerError(errorConfigInvalid, "extensions.allow_tusker_read_tools requires extensions.allowed_tools to explicitly include tusker.show_current or *", withPath(filePath))
	}
	return nil
}

func validateExtensionNames(field string, values []string, filePath string) error {
	seen := map[string]struct{}{}
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			return tuskerError(errorConfigInvalid, field+" contains an empty name", withPath(filePath))
		}
		if _, ok := seen[value]; ok {
			return tuskerError(errorConfigInvalid, field+" contains duplicate name "+value, withPath(filePath))
		}
		seen[value] = struct{}{}
		if value == "*" {
			// Wildcard extension grants are allowed only as this explicit whole-list entry.
			if len(values) != 1 {
				return tuskerError(errorConfigInvalid, field+" wildcard must be the only entry when used", withPath(filePath))
			}
			continue
		}
		if strings.Contains(value, "*") {
			return tuskerError(errorConfigInvalid, field+" wildcard must be exactly *; partial wildcards are not supported", withPath(filePath))
		}
		if len(value) > 128 || !extensionNamePattern.MatchString(value) {
			return tuskerError(errorConfigInvalid, field+" contains invalid name "+value+"; use lowercase letters, digits, dot, underscore, colon, or dash", withPath(filePath))
		}
	}
	return nil
}

func extensionListAllows(values []string, name string) bool {
	for _, value := range values {
		switch strings.TrimSpace(value) {
		case name, "*":
			return true
		}
	}
	return false
}

func validCodexApprovalPolicy(value string) bool {
	switch strings.TrimSpace(value) {
	case "untrusted", "on-request", "on-failure", "never":
		return true
	default:
		return false
	}
}

func validCodexSandbox(value string) bool {
	switch strings.TrimSpace(value) {
	case "read-only", "workspace-write", "danger-full-access":
		return true
	default:
		return false
	}
}
