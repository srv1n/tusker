package runner

import (
	"fmt"
	"strings"
)

func compilePolicy(d HarnessDefinition, input RunInput) (EffectivePolicy, []string, error) {
	policy := EffectivePolicy{Preset: input.Preset, Approvals: "deny"}
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
	if d.Transport == TransportACP {
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
	default:
		return policy, nil, admission(d, "unsupported_dialect", "dialect", "unknown CLI dialect")
	}
	return policy, args, nil
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
		compiled = append(compiled, "--sandbox", policy.Filesystem, "-c", `approval_policy="never"`)
		if policy.Filesystem == "workspace-write" {
			compiled = append(compiled, "-c", fmt.Sprintf("sandbox_workspace_write.network_access=%t", policy.Network))
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

func hasForbiddenPolicyArg(args []string) bool {
	for _, arg := range args {
		lower := strings.ToLower(strings.TrimSpace(arg))
		for _, prefix := range []string{"--sandbox", "-s=", "--dangerously", "--approve-for-me", "--permission-mode", "--permission-prompts", "--allowedtools", "--allowed-tools", "--disallowedtools", "--disallowed-tools", "--settings", "--tools", "--add-dir", "-c", "--config"} {
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
