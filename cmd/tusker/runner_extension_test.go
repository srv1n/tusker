package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodexExtensionToolDeniedByDefault(t *testing.T) {
	dispatch := dispatchCodexExtensionToolRequest(
		"item/tool/call",
		json.RawMessage(`{"name":"tusker.show_current","arguments":{}}`),
		ExtensionPolicy{},
		filepath.Join(t.TempDir(), "missing.md"),
	)
	if !dispatch.Handled {
		t.Fatal("expected recognizable tool request to be handled")
	}
	if !dispatch.Denied {
		t.Fatal("expected default extension policy to deny tool request")
	}
	if !strings.Contains(dispatch.Error, "extensions are disabled") {
		t.Fatalf("expected disabled denial, got %q", dispatch.Error)
	}
}

func TestCodexExtensionToolShowCurrentReturnsReadOnlyNoteSummary(t *testing.T) {
	tempRoot := t.TempDir()
	notePath := filepath.Join(tempRoot, "note.md")
	if err := writeText(notePath, "---\nschema: tusker.task/v5\nid: ORC-T-0015\nrecord_id: rec-0015\ntype: task\nkind: feature\nepic: ORC\ntitle: Extension bridge\nstatus: review\nwork_revision: 3\nsummary: Read-only extension bridge slice.\n---\n\n## Workpad\n\nDo not mutate this note.\n"); err != nil {
		t.Fatal(err)
	}

	dispatch := dispatchCodexExtensionToolRequest(
		"item/tool/call",
		json.RawMessage(`{"tool":{"name":"tusker.show_current"},"arguments":{}}`),
		ExtensionPolicy{
			Enabled:              true,
			AllowedTools:         []string{"tusker.show_current"},
			AllowTuskerReadTools: true,
		},
		notePath,
	)
	if !dispatch.Handled || dispatch.Denied || dispatch.Error != "" {
		t.Fatalf("expected allowed tool result, got handled=%t denied=%t err=%q", dispatch.Handled, dispatch.Denied, dispatch.Error)
	}
	result, ok := dispatch.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", dispatch.Result)
	}
	output, ok := result["output"].(map[string]any)
	if !ok {
		t.Fatalf("expected output map, got %T", result["output"])
	}
	assertEqual(t, "ORC-T-0015", output["id"], "tool note id")
	assertEqual(t, "rec-0015", output["record_id"], "tool record id")
	assertEqual(t, "Extension bridge", output["title"], "tool title")
	assertEqual(t, "review", output["status"], "tool status")
	assertEqual(t, 3, output["work_revision"], "tool work revision")
	assertEqual(t, "Read-only extension bridge slice.", output["summary"], "tool summary")
}

func TestClaudeExtensionBridgeIsExplicitlyUnsupported(t *testing.T) {
	if extensionPolicyRequestsNativeBridge(ExtensionPolicy{}) {
		t.Fatal("empty policy should not request a native extension bridge")
	}
	if extensionPolicyRequestsNativeBridge(ExtensionPolicy{Enabled: true}) {
		t.Fatal("enabled policy without tools should not request a native extension bridge")
	}
	if !extensionPolicyRequestsNativeBridge(ExtensionPolicy{Enabled: true, AllowedTools: []string{"tusker.show_current"}}) {
		t.Fatal("allowed tool should request a native extension bridge")
	}
	if !extensionPolicyRequestsNativeBridge(ExtensionPolicy{Enabled: true, AllowedMCPs: []string{"browser"}}) {
		t.Fatal("allowed MCP should request a native extension bridge")
	}
	if !extensionPolicyRequestsNativeBridge(ExtensionPolicy{Enabled: true, AllowTuskerReadTools: true}) {
		t.Fatal("read tool flag should request a native extension bridge")
	}
}
