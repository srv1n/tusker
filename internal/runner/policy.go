package runner

import (
	"fmt"
	"strings"
)

func compilePolicy(d HarnessDefinition, input RunInput) (EffectivePolicy, []string, error) {
	policy := EffectivePolicy{Preset: input.Preset, Approvals: "deny", Workspace: strings.TrimSpace(input.Workspace)}
	switch input.Preset {
	case PresetReadOnly:
		policy.Filesystem, policy.Network = "read-only", false
	case PresetWorkspaceOffline:
		policy.Filesystem, policy.Network = "workspace-write", false
	case PresetWorkspaceNetwork:
		policy.Filesystem, policy.Network = "workspace-write", true
	case PresetDangerFullAccess:
		policy.Filesystem, policy.Network, policy.Approvals = "unrestricted", true, "bypass"
	}
	if input.ResolvedAccess != nil {
		if input.ResolvedAccess.State != "ready" {
			return policy, nil, admission(d, "policy_unenforceable", "access", "resolved access is not ready for this route")
		}
		policy = input.ResolvedAccess.Effective
	}
	if d.Transport == TransportACP {
		if d.Provider == "devin" {
			return compileDevinACPArgs(d, input, policy)
		}
		if input.Preset == PresetDangerFullAccess {
			return policy, nil, admission(d, "policy_unenforceable", "policy", "ACP full access is not admitted")
		}
		// ACP tool callbacks are fail-closed, but they do not contain arbitrary
		// adapter subprocess access. Bounded ACP requires a separately verified
		// native boundary, which this V1 definition intentionally cannot assert.
		if !d.NativeContainment {
			return policy, nil, admission(d, "policy_unenforceable", "policy", "ACP bounded containment requires a verified native process boundary")
		}
		return policy, append([]string(nil), d.Args...), nil
	}
	if hasForbiddenPolicyArg(d.Args) {
		return policy, nil, admission(d, "policy_conflict", "policy", "configured arguments contain permission, sandbox, settings, tool, or directory overrides")
	}
	args := append([]string(nil), d.Args...)
	switch d.Dialect {
	case "codex":
		args = compileCodexArgs(d, input, args, policy)
	case "claude":
		var err error
		args, err = compileClaudeArgs(d, input, args, policy)
		if err != nil {
			return policy, nil, err
		}
	case "muse":
		var err error
		args, err = compileMuseArgs(d, input, args, policy)
		if err != nil {
			return policy, nil, err
		}
	default:
		return policy, nil, admission(d, "unsupported_dialect", "dialect", "unknown CLI dialect")
	}
	return policy, args, nil
}

func compileDevinACPArgs(d HarnessDefinition, input RunInput, policy EffectivePolicy) (EffectivePolicy, []string, error) {
	if input.Preset != PresetWorkspaceNetwork || policy.Filesystem != "workspace-write" || !policy.Network {
		return policy, nil, admission(d, "policy_unenforceable", "policy", "Devin ACP currently supports only sandboxed workspace-write with network enabled")
	}
	if policy.Approvals != "deny" {
		return policy, nil, admission(d, "policy_unenforceable", "policy", "Devin ACP currently supports destructive actions blocked, not operator approval")
	}
	if len(d.Args) != 1 || d.Args[0] != "acp" {
		return policy, nil, admission(d, "policy_conflict", "policy", "Devin ACP command must be exactly 'devin acp'; Tusker compiles sandbox and model controls")
	}
	if strings.TrimSpace(input.Model) == "" {
		return policy, nil, admission(d, "invalid_configuration", "model", "Devin ACP requires an exact discovered model")
	}
	return policy, []string{"--sandbox", "acp", "--model", input.Model}, nil
}

func compileCodexArgs(d HarnessDefinition, input RunInput, base []string, policy EffectivePolicy) []string {
	args := append([]string(nil), base...)
	execAt := indexOf(args, "exec")
	if execAt < 0 {
		args = append(args, "exec")
		execAt = len(args) - 1
	}
	compiled := []string{"--ignore-user-config", "--strict-config"}
	if input.Preset == PresetDangerFullAccess {
		compiled = append(compiled, "--dangerously-bypass-approvals-and-sandbox")
	} else {
		approval := "never"
		if policy.Approvals == "ask" {
			approval = "on-request"
		}
		compiled = append(compiled, "--sandbox", policy.Filesystem, "-c", fmt.Sprintf(`approval_policy="%s"`, approval))
		if policy.Filesystem == "workspace-write" {
			compiled = append(compiled, "-c", fmt.Sprintf("sandbox_workspace_write.network_access=%t", policy.Network))
		}
	}
	if input.ResolvedAccess != nil {
		for _, folder := range input.ResolvedAccess.Folders {
			if folder.Access == "write" && folder.Path != "" && folder.Path != policy.Workspace {
				compiled = append(compiled, "--add-dir", folder.Path)
			}
		}
	}
	if input.Model != "" {
		compiled = append(compiled, "--model", input.Model)
	}
	if input.Effort != "" {
		compiled = append(compiled, "-c", fmt.Sprintf(`model_reasoning_effort="%s"`, input.Effort))
	}
	if !contains(args, "--json") {
		compiled = append(compiled, "--json")
	}
	if input.ResumeSession != "" {
		args = append(args[:execAt+1], append([]string{"resume"}, args[execAt+1:]...)...)
		compiled = append(compiled, input.ResumeSession, "-")
	} else if len(args) == execAt+1 || args[len(args)-1] != "-" {
		compiled = append(compiled, "-")
	}
	return append(args[:execAt+1], append(compiled, args[execAt+1:]...)...)
}

func compileClaudeArgs(d HarnessDefinition, input RunInput, base []string, policy EffectivePolicy) ([]string, error) {
	if input.Preset == PresetWorkspaceNetwork || input.Preset == PresetWorkspaceOffline {
		return nil, admission(d, "policy_unenforceable", "policy", "Claude Code does not expose a verified workspace-write sandbox boundary")
	}
	args := append([]string(nil), base...)
	if !contains(args, "-p") && !contains(args, "--print") {
		args = append(args, "-p")
	}
	args = append(args, "--output-format", "stream-json", "--input-format", "stream-json", "--verbose", "--permission-prompts", "none", "--strict-mcp-config", "--disable-slash-commands")
	if input.Preset == PresetDangerFullAccess {
		args = append(args, "--dangerously-skip-permissions")
	} else {
		args = append(args, "--permission-mode", "plan", "--tools", "Read,Glob,Grep")
	}
	if input.Model != "" {
		args = append(args, "--model", input.Model)
	}
	if input.Effort != "" {
		args = append(args, "--effort", input.Effort)
	}
	if input.ResumeSession != "" {
		args = append(args, "--resume", input.ResumeSession)
	}
	return args, nil
}

// compileMuseArgs is deliberately narrow: direct Muse is a separate dialect
// from the existing Codex profile route. Native flags are compiled here so a
// configured shell command cannot widen the request.
func compileMuseArgs(d HarnessDefinition, input RunInput, base []string, policy EffectivePolicy) ([]string, error) {
	args := append([]string(nil), base...)
	execAt := indexOf(args, "exec")
	if execAt < 0 {
		args = append(args, "exec")
		execAt = len(args) - 1
	}
	compiled := []string{}
	add := func(flag string, values ...string) {
		if !contains(args, flag) {
			compiled = append(compiled, flag)
			compiled = append(compiled, values...)
		}
	}
	if policy.Workspace != "" {
		add("--workspace", policy.Workspace)
	}
	if input.Preset == PresetDangerFullAccess {
		add("--yolo")
	} else {
		approval := "never"
		if policy.Approvals == "ask" {
			approval = "on-request"
		}
		add("--approval-mode", approval)
		network := "restricted"
		if policy.Network {
			network = "enabled"
		}
		add("--sandbox-network", network)
		if policy.Filesystem == "read-only" {
			add("--disable-write")
			add("--disable-shell")
		}
		if !policy.Network {
			add("--disable-web-tools")
		}
	}
	if input.Model != "" {
		add("--model", input.Model)
	}
	if input.Effort != "" {
		add("--reasoning-effort", input.Effort)
	}
	add("--json")
	if input.ResumeSession != "" {
		if !contains(args, "resume") {
			args = append(args[:execAt+1], append([]string{"resume"}, args[execAt+1:]...)...)
		}
		compiled = append(compiled, input.ResumeSession)
	}
	if input.ResumeSession == "" && !contains(args, "-") {
		compiled = append(compiled, "-")
	}
	return append(args[:execAt+1], append(compiled, args[execAt+1:]...)...), nil
}

func hasForbiddenPolicyArg(args []string) bool {
	for _, arg := range args {
		lower := strings.ToLower(strings.TrimSpace(arg))
		for _, prefix := range []string{"--sandbox", "--sandbox-network", "-s=", "--dangerously", "--approve-for-me", "--permission-mode", "--permission-prompts", "--allowedtools", "--allowed-tools", "--disallowedtools", "--disallowed-tools", "--settings", "--mcp-config", "--tools", "--add-dir", "-c", "--config", "--yolo", "--workspace", "--approval-mode"} {
			if lower == prefix || strings.HasPrefix(lower, prefix+"=") {
				return true
			}
		}
	}
	return false
}

func indexOf(values []string, want string) int {
	for i, value := range values {
		if value == want {
			return i
		}
	}
	return -1
}
func contains(values []string, want string) bool { return indexOf(values, want) >= 0 }
