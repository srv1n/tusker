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
