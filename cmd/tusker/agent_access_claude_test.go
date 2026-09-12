package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestClaudePrivateFolderToolBoundary(t *testing.T) {
	workspace := t.TempDir()
	private := filepath.Join(workspace, "private")
	if err := os.Mkdir(private, 0o700); err != nil {
		t.Fatal(err)
	}
	handle := &claudeLiveHandle{cmd: &exec.Cmd{Dir: workspace}, privateFolders: []string{private}}

	denied := handle.evaluateToolApproval(map[string]any{
		"name":  "Read",
		"input": map[string]any{"file_path": filepath.Join(private, "secret.txt")},
	})
	if denied.Decision != "reject" || denied.Reason == "" {
		t.Fatalf("private descendant was accepted: %#v", denied)
	}
	relativeDenied := handle.evaluateToolApproval(map[string]any{
		"name":  "Read",
		"input": map[string]any{"file_path": filepath.Join("private", "secret.txt"), "cwd": workspace},
	})
	if relativeDenied.Decision != "reject" {
		t.Fatalf("relative private path was accepted: %#v", relativeDenied)
	}

	// A private descendant must not make ordinary workspace paths unusable.
	allowed := handle.evaluateToolApproval(map[string]any{
		"name":  "Read",
		"input": map[string]any{"file_path": filepath.Join(workspace, "main.go")},
	})
	if allowed.Decision != "accept" {
		t.Fatalf("workspace path was rejected because of private descendant: %#v", allowed)
	}

	commandDenied := handle.evaluateToolApproval(map[string]any{
		"name":  "Bash",
		"input": map[string]any{"command": "cat " + filepath.Join(private, "secret.txt")},
	})
	if commandDenied.Decision != "reject" || commandDenied.Reason == "" {
		t.Fatalf("private command path was accepted: %#v", commandDenied)
	}

	hookDenied := handle.evaluateToolApproval(map[string]any{
		"tool_name":  "Read",
		"tool_input": map[string]any{"file_path": filepath.Join(private, "secret.txt")},
	})
	if hookDenied.Decision != "reject" {
		t.Fatalf("private hook path was accepted: %#v", hookDenied)
	}
}
