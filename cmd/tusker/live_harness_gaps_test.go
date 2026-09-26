package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// G2: per-task profile pins are accepted on create and editable through the
// CAS task update path; unknown names are refused.
func TestTaskProfilePinsCreateAndUpdate(t *testing.T) {
	global := filepath.Join(t.TempDir(), "config.yaml")
	profile := "      harness: codex_exec\n      model: gpt-6-luna\n      effort: medium\n      permission_preset: read-only\n      sandbox:\n        mode: read-only\n        network: false\n"
	if err := os.WriteFile(global, []byte("automation:\n  profiles:\n    pin-a:\n"+profile+"    pin-b:\n"+profile), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TUSKER_CONFIG", global)
	vault := v7DirectTestVault(t)
	body := directAuthoringBodyPath(t, vault, "task.md", "# Pinned task\n\nSubstantive body.\n")
	if err := newAuthoredV7Task(Args{"vault": vault, "quiet": "true", "title": "Bad pin", "work-level": "light", "body-file": body, "execute-profile": "no-such-profile"}); err == nil {
		t.Fatal("unknown execute profile accepted")
	}
	if err := newAuthoredV7Task(Args{"vault": vault, "quiet": "true", "title": "Pinned", "work-level": "light", "body-file": body, "execute-profile": "pin-a", "review-profile": "pin-b"}); err != nil {
		t.Fatalf("new task with profiles: %v", err)
	}
	read := func() map[string]any {
		note, err := resolveV7Note(vault, "TSK-T-0001", "task")
		if err != nil {
			t.Fatal(err)
		}
		return note.Data
	}
	data := read()
	if stringField(data, "execute_profile") != "pin-a" || stringField(data, "review_profile") != "pin-b" {
		t.Fatalf("create did not persist pins: %v", data)
	}
	update := func(extra Args) error {
		args := Args{"vault": vault, "quiet": "true", "_pos0": "TSK-T-0001", "if-revision": stringField(read(), "state_rev"), "by": "agent:builder"}
		for k, v := range extra {
			args[k] = v
		}
		return updateV7TaskCmd(args)
	}
	if err := update(Args{"review-profile": "no-such-profile"}); err == nil {
		t.Fatal("unknown review profile accepted on update")
	}
	if err := update(Args{"review-profile": "pin-a", "clear-execute-profile": "true"}); err != nil {
		t.Fatalf("update pins: %v", err)
	}
	data = read()
	if _, ok := data["execute_profile"]; ok || stringField(data, "review_profile") != "pin-a" {
		t.Fatalf("update did not apply pins: %v", data)
	}
	if stringField(data, "status") == "rework" {
		t.Fatal("profile pin change must not rework the contract")
	}
}

// G3: execution commands resolve the runtime-registered project (ULID) for
// the vault, not the config project_id, and accept --project.
func TestExecutionRegisterResolvesRuntimeProject(t *testing.T) {
	vault := v7DirectTestVault(t)
	if err := writeText(managedTuskerConfigPath(vault), "schema: tusker.config/v1\nproject_id: dir-name\nmutation_mode: local\n"); err != nil {
		t.Fatal(err)
	}
	stateRoot := filepath.Join(t.TempDir(), "state")
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	const ulid = "01M0Q4C79K5R8NY8H57AJC2GB5"
	project := newRegisteredProject(v7RepoRoot(vault), vault)
	project.ProjectID = ulid
	project.Enabled = true
	if _, _, err := store.RegisterProject(project); err != nil {
		t.Fatal(err)
	}
	if err := executionCmd(Args{"vault": vault, "state-root": stateRoot, "quiet": "true"}, "register"); err != nil {
		t.Fatalf("register by vault: %v", err)
	}
	if err := executionCmd(Args{"project": ulid, "state-root": stateRoot, "quiet": "true"}, "register"); err != nil {
		t.Fatalf("register by --project: %v", err)
	}
	if err := executionCmd(Args{"project": "missing-project", "state-root": stateRoot, "quiet": "true"}, "register"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unknown --project error=%v", err)
	}
	var runtimeRecords, localRecords int
	if err := store.queryRowScan(`SELECT COUNT(*) FROM execution_records WHERE project_id=?`, []any{ulid}, &runtimeRecords); err != nil || runtimeRecords != 2 {
		t.Fatalf("executions under runtime project: %d err=%v", runtimeRecords, err)
	}
	if err := store.queryRowScan(`SELECT COUNT(*) FROM execution_records WHERE project_id='dir-name'`, nil, &localRecords); err != nil || localRecords != 0 {
		t.Fatalf("execution written under config project_id: %d err=%v", localRecords, err)
	}

	// --contact-role accepts a subject whose note carries the config
	// project_id even though the registry keys the project by ULID.
	body := directAuthoringBodyPath(t, vault, "contact-task.md", "# Contact subject\n\nSubstantive body.\n")
	mustWave(t, Args{"vault": vault, "quiet": "true", "title": "Subject", "work-level": "light", "body-file": body}, newAuthoredV7Task)
	mustWave(t, Args{"vault": vault, "quiet": "true", "_pos0": "Contact wave", "_pos1": "TSK-T-0001"}, waveV7CreateCmd)
	if err := executionCmd(Args{"vault": vault, "state-root": stateRoot, "quiet": "true",
		"contact-role": "architect", "wave": "W-0001",
		"harness": "claude-code", "provider": "anthropic", "connection-id": "local",
		"source": "direct_claude", "conversation-id": "conv-1",
		"by": "human:tester", "if-generation": "0"}, "register"); err != nil {
		t.Fatalf("register --contact-role by vault: %v", err)
	}
}
