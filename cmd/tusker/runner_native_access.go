package main

import (
	"path/filepath"
	"strings"

	runnercore "tusker/internal/runner"
)

// nativeAccessControls is the closed support declaration consumed by the
// shared resolver. It describes operative provider controls, not prompt text
// or a handwritten containment claim.
func nativeAccessControls(definition runnercore.HarnessDefinition, access *AgentAccessV1) []ControlSupport {
	if definition.Provider == "devin" {
		return devinAccessControls(access)
	}
	controls := []ControlSupport{
		{Control: runnercore.AccessWorkspaceWrite, Mechanism: runnercore.AccessNativeSetting, Coverage: "prepared execution workspace", Evidence: []string{"native.argv"}},
		{Control: runnercore.AccessNetwork, Mechanism: runnercore.AccessNativeSetting, Coverage: "provider network setting across the selected CLI route", Evidence: []string{"native.argv"}},
		{Control: runnercore.AccessDestructiveApproval, Mechanism: runnercore.AccessNativeSetting, Coverage: "provider approval mode; exact destructive target classification remains adapter-scoped", Evidence: []string{"native.argv"}},
		{Control: runnercore.AccessReviewOnly, Mechanism: runnercore.AccessNativeSetting, Coverage: "read-only route flags", Evidence: []string{"native.argv"}},
	}
	if definition.Dialect == "claude" {
		for i := range controls {
			switch controls[i].Control {
			case runnercore.AccessWorkspaceWrite, runnercore.AccessPrivateReadDeny, runnercore.AccessPrivateWriteDeny, runnercore.AccessDestructiveApproval:
				controls[i].Mechanism = runnercore.AccessNativeHook
				controls[i].Coverage = "Claude PreToolUse native hook for supported tool requests"
				controls[i].Evidence = []string{"claude.pre_tool_use"}
			}
		}
	}
	// Neither Codex CLI nor direct Muse exposes a read-only grant for an
	// arbitrary external folder. Do not turn a read reference into a write
	// grant through --add-dir or --workspace.
	for _, control := range []runnercore.AccessControl{runnercore.AccessReferenceRead, runnercore.AccessReferenceWrite, runnercore.AccessPrivateReadDeny, runnercore.AccessPrivateWriteDeny} {
		mechanism := runnercore.AccessUnsupported
		coverage := "route has no qualified native path exclusion or read-only external-root control"
		if definition.Dialect == "claude" && (control == runnercore.AccessPrivateReadDeny || control == runnercore.AccessPrivateWriteDeny) {
			mechanism = runnercore.AccessNativeHook
			coverage = "Claude PreToolUse native hook rejects supported private-path requests"
		}
		controls = append(controls, ControlSupport{Control: control, Mechanism: mechanism, Coverage: coverage, Evidence: []string{"native.gap"}})
	}
	_ = access
	return controls
}

func devinAccessControls(access *AgentAccessV1) []ControlSupport {
	controls := []ControlSupport{
		{Control: runnercore.AccessWorkspaceWrite, Mechanism: runnercore.AccessNativeSetting, Coverage: "Devin --sandbox grants writes only to the workspace", Evidence: []string{"native.argv.--sandbox"}},
		{Control: runnercore.AccessNetwork, Mechanism: runnercore.AccessUnsupported, Coverage: "Devin sandbox network filtering is not qualified for offline execution", Evidence: []string{"native.gap"}},
		{Control: runnercore.AccessDestructiveApproval, Mechanism: runnercore.AccessUnsupported, Coverage: "Devin operator approval is unavailable to a headless Tusker attempt", Evidence: []string{"native.gap"}},
		{Control: runnercore.AccessReviewOnly, Mechanism: runnercore.AccessUnsupported, Coverage: "Devin ACP has no qualified read-only filesystem boundary", Evidence: []string{"native.gap"}},
	}
	if access != nil && access.Network {
		controls[1] = ControlSupport{Control: runnercore.AccessNetwork, Mechanism: runnercore.AccessNativeSetting, Coverage: "Devin sandbox with its default full network mode", Evidence: []string{"native.argv.--sandbox"}}
	}
	if access != nil && access.DestructiveActions == "deny" {
		controls[2] = ControlSupport{Control: runnercore.AccessDestructiveApproval, Mechanism: runnercore.AccessNativeHook, Coverage: "Devin smart mode prompts for high-risk commands and Tusker rejects unresolved ACP permission requests", Evidence: []string{"acp.session.mode=smart", "acp.permission.reject"}}
	}
	for _, control := range []runnercore.AccessControl{runnercore.AccessReferenceRead, runnercore.AccessReferenceWrite, runnercore.AccessPrivateReadDeny, runnercore.AccessPrivateWriteDeny} {
		controls = append(controls, ControlSupport{Control: control, Mechanism: runnercore.AccessUnsupported, Coverage: "Devin route has no per-attempt qualified external-root or private-path compiler", Evidence: []string{"native.gap"}})
	}
	return controls
}

func accessReferencesForProfile(access *AgentAccessV1, workspace string) []string {
	if access == nil {
		return nil
	}
	workspace = filepath.Clean(workspace)
	var references []string
	for _, folder := range access.Folders {
		if strings.TrimSpace(folder.Access) != "read" {
			continue
		}
		path := filepath.Clean(strings.TrimSpace(folder.Path))
		if path != "" && path != workspace {
			references = append(references, path)
		}
	}
	return references
}
