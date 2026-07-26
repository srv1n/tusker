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
	if wf.TrackerSchemaVersion != 7 {
		return tuskerError(errorConfigInvalid, "tracker_schema_version must be 7", withPath(filePath))
	}
	if err := validateScheduledPromotionPolicy(wf.ScheduledPromotion, filePath); err != nil {
		return err
	}
	if len(wf.Tracker.ActiveStates) == 0 {
		return tuskerError(errorConfigInvalid, "tracker.dispatch_states must not be empty; usually ready,rework", withPath(filePath))
	}
	for _, state := range wf.Tracker.ActiveStates {
		if strings.TrimSpace(state) == "active" {
			return tuskerError(errorConfigInvalid, "V7 tracker.dispatch_states must not include durable active", withPath(filePath), withHint("use ready,rework; running work belongs to runtime leases"))
		}
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
	if _, err := serveNormalizeAddr(firstNonEmpty(strings.TrimSpace(wf.Runtime.Serve.Addr), defaultServeAddr)); err != nil {
		return tuskerError(errorConfigInvalid, "runtime.serve.addr must be a localhost address", withPath(filePath), withContext(map[string]any{"addr": wf.Runtime.Serve.Addr}))
	}
	if strings.TrimSpace(wf.Workspace.Strategy) == "" {
		return tuskerError(errorConfigInvalid, "workspace.strategy is required", withPath(filePath))
	}
	if !validWorkspaceStrategy(wf.Workspace.Strategy) {
		return tuskerError(errorConfigInvalid, "workspace.strategy must be one of in_place, worktree, clone, copy", withPath(filePath))
	}
	if workspaceStrategyFromWorkflow(wf.Workspace.Strategy) != WorkspaceStrategyShared && strings.TrimSpace(wf.Workspace.Root) == "" {
		return tuskerError(errorConfigInvalid, "workspace.root is required unless workspace.strategy is in_place", withPath(filePath))
	}
	if workspaceStrategyFromWorkflow(wf.Workspace.Strategy) != WorkspaceStrategyShared && !validSharedWorkspaceRootConfig(wf.Workspace.Root) {
		return tuskerError(errorConfigInvalid, "workspace.root must be workspaces or a relative subdirectory under workspaces", withPath(filePath))
	}
	if wf.Retry.MaxAttempts <= 0 || len(wf.Retry.BackoffMS) == 0 {
		return tuskerError(errorConfigInvalid, "retry.max_attempts and retry.backoff_ms are required", withPath(filePath))
	}
	if err := validateReviewerPolicy(wf.Reviewer, wf.Agents.Enabled, filePath); err != nil {
		return err
	}
	if err := validateRunnerDefinitions(wf, filePath); err != nil {
		return err
	}
	if err := validateWorkflowRunnerProfiles(wf, filePath); err != nil {
		return err
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
	if err := validateCodexCloudWorkflow(wf.CodexCloud, wf, filePath); err != nil {
		return err
	}
	if err := validateExtensionPolicy(wf.Extensions, filePath); err != nil {
		return err
	}
	if err := validateFanoutPolicy(wf.Fanout, filePath); err != nil {
		return err
	}
	if len(wf.Orchestration.BatchGate.Windows) > 0 {
		if _, err := parseMergeWindows(wf.Orchestration.BatchGate.Windows); err != nil {
			return tuskerError(errorConfigInvalid, err.Error(), withPath(filePath))
		}
	}
	if err := validateGateScopes(wf.Orchestration.Gate.Scopes, filePath); err != nil {
		return err
	}
	if err := validateGateIsolationProvider(wf.Orchestration.Gate.IsolationProvider, filePath); err != nil {
		return err
	}
	if action := strings.TrimSpace(wf.Orchestration.Gate.FlakeFailureAction); action != "" && action != "quarantine" && action != "rerun" {
		return tuskerError(errorConfigInvalid, "orchestration.gate.flake_failure_action must be quarantine or rerun", withPath(filePath))
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

func validateGateIsolationProvider(provider string, filePath string) error {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return nil
	}
	if !extensionNamePattern.MatchString(provider) {
		return tuskerError(errorConfigInvalid, "orchestration.gate.isolation_provider must be a trusted provider profile name", withPath(filePath))
	}
	return nil
}

func validateScheduledPromotionPolicy(policy ScheduledPromotionPolicy, filePath string) error {
	if policy.Version != scheduledPromotionPolicyVersion {
		return tuskerError(errorConfigInvalid, "scheduled_promotion.version must be 1", withPath(filePath), withHint("set scheduled_promotion.version: 1 and mode: disabled, shadow, stage, or promote"))
	}
	switch policy.Mode {
	case scheduledPromotionDisabled, scheduledPromotionShadow, scheduledPromotionStage, scheduledPromotionPromote:
	default:
		return tuskerError(errorConfigInvalid, "scheduled_promotion.mode must be disabled, shadow, stage, or promote", withPath(filePath), withHint("set scheduled_promotion.mode: disabled to preserve existing behavior"))
	}
	profile := strings.TrimSpace(policy.Release.Profile)
	if (profile == "") != !policy.Release.Authorized {
		return tuskerError(errorConfigInvalid, "scheduled_promotion.release.profile and scheduled_promotion.release.authorized must be set together", withPath(filePath), withHint("set a named release profile with authorized: true, or remove both release fields"))
	}
	if policy.Mode != scheduledPromotionPromote && (profile != "" || policy.ModelTriage.Authorized) {
		return tuskerError(errorConfigInvalid, "scheduled_promotion release and paid model triage require mode promote", withPath(filePath), withHint("use mode: promote for separately authorized release/model triage, or remove those authorities"))
	}
	return nil
}

func validateWorkflowRunnerProfiles(wf Workflow, filePath string) error {
	for name, profile := range wf.RunnerProfiles {
		if err := validateRunnerProfileDefinition(strings.TrimSpace(name), profile, filePath); err != nil {
			return err
		}
	}
	if strings.TrimSpace(wf.RunnerDefaultProfile) != "" {
		if _, ok := wf.RunnerProfiles[strings.TrimSpace(wf.RunnerDefaultProfile)]; !ok {
			return tuskerError(errorConfigInvalid, "runner_default_profile references unknown profile "+wf.RunnerDefaultProfile, withPath(filePath))
		}
	}
	for lane, profile := range wf.RunnerLaneProfiles {
		if _, ok := wf.RunnerProfiles[strings.TrimSpace(profile)]; !ok {
			return tuskerError(errorConfigInvalid, "runner_lane_profiles."+lane+" references unknown profile "+profile, withPath(filePath))
		}
	}
	for _, rule := range wf.RunnerRouting {
		if strings.TrimSpace(rule.Profile) == "" {
			return tuskerError(errorConfigInvalid, "runner_routing rule "+rule.Name+" is missing profile", withPath(filePath))
		}
		if _, ok := wf.RunnerProfiles[strings.TrimSpace(rule.Profile)]; !ok {
			return tuskerError(errorConfigInvalid, "runner_routing rule "+rule.Name+" references unknown profile "+rule.Profile, withPath(filePath))
		}
	}
	return nil
}

func validateRunnerDefinitions(wf Workflow, filePath string) error {
	for name, definition := range wf.Runners {
		name = strings.TrimSpace(name)
		if name == "" {
			return tuskerError(errorConfigInvalid, "runners contains an empty runner name", withPath(filePath))
		}
		kind := RunnerName(firstNonEmpty(strings.TrimSpace(definition.Kind), name))
		switch kind {
		case RunnerCodex, RunnerCodexAppServer, RunnerCodexExec, RunnerCodexCloud, RunnerClaude:
		default:
			return tuskerError(errorConfigInvalid, "runner "+name+" has unsupported kind "+string(kind), withPath(filePath))
		}
		if kind == RunnerCodexExec && strings.Contains(definition.Command, "app-server") {
			return tuskerError(errorConfigInvalid, "runner "+name+" is codex_exec but uses an app-server command", withPath(filePath))
		}
		if kind == RunnerCodexAppServer && strings.TrimSpace(definition.Command) != "" && !strings.Contains(definition.Command, "app-server") {
			return tuskerError(errorConfigInvalid, "runner "+name+" is codex_app_server but command is not app-server", withPath(filePath))
		}
	}
	return nil
}

// validateGateScopes enforces that each selective-gate scope is well formed: a
// scope with no name, no paths, or no commands cannot be selected or run, so it
// is a configuration error rather than a silently inert entry.
func validateGateScopes(scopes []GateScope, filePath string) error {
	seen := map[string]bool{}
	for i, scope := range scopes {
		name := strings.TrimSpace(scope.Name)
		if name == "" {
			return tuskerError(errorConfigInvalid, fmt.Sprintf("orchestration.gate.scopes[%d] is missing name", i), withPath(filePath))
		}
		if seen[name] {
			return tuskerError(errorConfigInvalid, "orchestration.gate.scopes has duplicate scope name "+name, withPath(filePath))
		}
		seen[name] = true
		if len(nonEmptyTrimmed(scope.Paths)) == 0 {
			return tuskerError(errorConfigInvalid, "orchestration.gate.scope "+name+" requires at least one path", withPath(filePath))
		}
		if len(nonEmptyTrimmed(scope.Commands)) == 0 {
			return tuskerError(errorConfigInvalid, "orchestration.gate.scope "+name+" requires at least one command", withPath(filePath))
		}
	}
	return nil
}

func nonEmptyTrimmed(values []string) []string {
	var out []string
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, value)
		}
	}
	return out
}

func validateFanoutPolicy(policy FanoutPolicy, filePath string) error {
	if !policy.Enabled {
		return nil
	}
	if policy.MaxChildren <= 0 {
		return tuskerError(errorConfigInvalid, "fanout.max_children must be > 0 when fanout is enabled", withPath(filePath))
	}
	if len(policy.AllowedChildTypes) == 0 {
		return tuskerError(errorConfigInvalid, "fanout.allowed_child_types is required when fanout is enabled", withPath(filePath))
	}
	if strings.TrimSpace(policy.MergeRule) == "" {
		return tuskerError(errorConfigInvalid, "fanout.merge_rule is required when fanout is enabled", withPath(filePath))
	}
	return nil
}

func validateCodexCloudWorkflow(config CodexCloudConfig, wf Workflow, filePath string) error {
	used := workflowUsesRunner(wf, RunnerCodexCloud)
	configured := codexCloudConfigPresent(config)
	if !used && !configured {
		return nil
	}
	for field, value := range map[string]bool{
		"approval_policy":     strings.TrimSpace(config.ApprovalPolicy) != "",
		"thread_sandbox":      strings.TrimSpace(config.ThreadSandbox) != "",
		"turn_sandbox_policy": strings.TrimSpace(config.TurnSandboxPolicy) != "",
		"turn_timeout_ms":     config.TurnTimeoutMS != 0,
		"read_timeout_ms":     config.ReadTimeoutMS != 0,
		"stall_timeout_ms":    config.StallTimeoutMS != 0,
		"max_turns":           config.MaxTurns != 0,
	} {
		if value {
			return tuskerError(errorConfigInvalid, "codex_cloud."+field+" is local app-server-only; use codex."+field+" for "+string(RunnerCodex), withPath(filePath))
		}
	}
	if strings.TrimSpace(config.EnvironmentID) == "" {
		return tuskerError(errorConfigInvalid, "codex_cloud.environment_id is required when codex_cloud is configured or enabled", withPath(filePath))
	}
	if !validCodexCloudApplyMode(config.ApplyMode) {
		return tuskerError(errorConfigInvalid, "codex_cloud.apply_mode must be one of manual, pull_request", withPath(filePath))
	}
	if !validCodexCloudPRMode(config.PRMode) {
		return tuskerError(errorConfigInvalid, "codex_cloud.pr_mode must be one of none, draft, ready", withPath(filePath))
	}
	if strings.TrimSpace(config.ApplyMode) == "pull_request" && strings.TrimSpace(config.PRMode) == "none" {
		return tuskerError(errorConfigInvalid, "codex_cloud.pr_mode must be draft or ready when apply_mode is pull_request", withPath(filePath))
	}
	return nil
}

func workflowUsesRunner(wf Workflow, runner RunnerName) bool {
	target := strings.TrimSpace(string(runner))
	if strings.TrimSpace(wf.Agents.Default) == target {
		return true
	}
	if strings.TrimSpace(wf.Reviewer.Runner) == target {
		return true
	}
	return stringListContainsFold(wf.Agents.Enabled, target)
}

func codexCloudConfigPresent(config CodexCloudConfig) bool {
	return strings.TrimSpace(config.Command) != "" ||
		strings.TrimSpace(config.StatusCommand) != "" ||
		strings.TrimSpace(config.CollectCommand) != "" ||
		strings.TrimSpace(config.EnvironmentID) != "" ||
		strings.TrimSpace(config.ApplyMode) != "" ||
		strings.TrimSpace(config.PRMode) != "" ||
		strings.TrimSpace(config.ApprovalPolicy) != "" ||
		strings.TrimSpace(config.ThreadSandbox) != "" ||
		strings.TrimSpace(config.TurnSandboxPolicy) != "" ||
		config.TurnTimeoutMS != 0 ||
		config.ReadTimeoutMS != 0 ||
		config.StallTimeoutMS != 0 ||
		config.MaxTurns != 0
}

func validCodexCloudApplyMode(value string) bool {
	switch strings.TrimSpace(value) {
	case "manual", "pull_request":
		return true
	default:
		return false
	}
}

func validCodexCloudPRMode(value string) bool {
	switch strings.TrimSpace(value) {
	case "none", "draft", "ready":
		return true
	default:
		return false
	}
}

func validateReviewerPolicy(policy ReviewerPolicy, enabledAgents []string, filePath string) error {
	if !policy.Enabled {
		return nil
	}
	if strings.TrimSpace(policy.Runner) == "" {
		return tuskerError(errorConfigInvalid, "reviewer.runner is required when reviewer.enabled is true", withPath(filePath))
	}
	if !stringListContainsFold(enabledAgents, policy.Runner) {
		return tuskerError(errorConfigInvalid, "reviewer.runner must be listed in agents.enabled", withPath(filePath))
	}
	if strings.TrimSpace(policy.Actor) == "" {
		return tuskerError(errorConfigInvalid, "reviewer.actor is required when reviewer.enabled is true", withPath(filePath))
	}
	if policy.MaxCycles < 1 {
		return tuskerError(errorConfigInvalid, "reviewer.max_cycles must be at least 1", withPath(filePath))
	}
	if len(policy.HumanRequiredRisks) > 0 {
		return tuskerError(errorConfigInvalid, "legacy reviewer.human_required_risks treats risk as human authority", withPath(filePath), withHint("run `tusker migrate close-policy --write`; use explicit gates for capability, external authority, unresolved intent, or subjective acceptance"))
	}
	if len(policy.AutoCloseRisks) == 0 {
		return tuskerError(errorConfigInvalid, "reviewer.auto_close_risks must list every objective risk tier", withPath(filePath))
	}
	seen := map[string]string{}
	for field, values := range map[string][]string{
		"reviewer.auto_close_risks": policy.AutoCloseRisks,
	} {
		for _, raw := range values {
			value := strings.ToLower(strings.TrimSpace(raw))
			if _, ok := risks[value]; !ok {
				return tuskerError(errorConfigInvalid, field+" contains invalid risk "+raw, withPath(filePath))
			}
			if previous := seen[value]; previous != "" {
				return tuskerError(errorConfigInvalid, "reviewer risk "+value+" appears in both "+previous+" and "+field, withPath(filePath))
			}
			seen[value] = field
		}
	}
	for risk := range risks {
		if !stringListContainsFold(policy.AutoCloseRisks, risk) {
			return tuskerError(errorConfigInvalid, "reviewer.auto_close_risks missing objective close tier "+risk, withPath(filePath), withHint("list low, medium, high, critical; explicit gates—not risk—reserve human authority"))
		}
	}
	if strings.TrimSpace(policy.Prompt) == "" {
		return tuskerError(errorConfigInvalid, "reviewer.prompt is required when reviewer.enabled is true", withPath(filePath))
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

func validWorkspaceStrategy(value string) bool {
	switch strings.TrimSpace(value) {
	case string(WorkspaceStrategyShared), string(WorkspaceStrategyInPlace), string(WorkspaceStrategyWorktree), string(WorkspaceStrategyClone), string(WorkspaceStrategyCopy):
		return true
	default:
		return false
	}
}
