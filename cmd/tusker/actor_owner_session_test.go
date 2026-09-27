package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestActorOwnerSessionSetting(t *testing.T) {
	clearAgentSessionEnvForTest(t)
	vault := filepath.Join(t.TempDir(), ".tusker")
	if err := os.MkdirAll(vault, 0755); err != nil {
		t.Fatal(err)
	}
	args := Args{"vault": vault, "by": "human:sarav"}
	t.Setenv("CLAUDECODE", "1")
	check := func(wantAllowed bool) {
		t.Helper()
		actor, err := resolveV7Actor(args, "gate satisfy", v7ActorPolicy{})
		if wantAllowed {
			if err != nil || actor != "human:sarav" {
				t.Fatalf("human actor = %q, %v", actor, err)
			}
		} else if err == nil || err.Error() != "gate satisfy: this project does not let agents act as the owner (agents.act_as_owner: false); ask the owner or use --by agent:<name>" {
			t.Fatalf("human actor error = %v", err)
		}
	}
	check(true)
	if err := os.WriteFile(filepath.Join(vault, "config.yaml"), []byte("agents:\n  act_as_owner: false\n"), 0644); err != nil {
		t.Fatal(err)
	}
	check(false)
	args["by"] = "agent:codex"
	if actor, err := resolveV7Actor(args, "gate satisfy", v7ActorPolicy{}); err != nil || actor != "agent:codex" {
		t.Fatalf("agent actor = %q, %v", actor, err)
	}
	args["by"] = "human:sarav"
	t.Setenv("CLAUDECODE", "")
	check(true)
	t.Setenv("CLAUDECODE", "1")
	if err := os.WriteFile(filepath.Join(vault, "config.local.yaml"), []byte("agents:\n  act_as_owner: true\n"), 0644); err != nil {
		t.Fatal(err)
	}
	check(true)
	if err := os.WriteFile(filepath.Join(vault, "config.local.yaml"), []byte("agents:\n  act_as_owner: false\n"), 0644); err != nil {
		t.Fatal(err)
	}
	check(false)
}

func TestActorRequireOwnerSession(t *testing.T) {
	clearAgentSessionEnvForTest(t)
	if err := requireOwnerSession("daemon resume"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_SHELL", "1")
	if err := requireOwnerSession("daemon resume"); err == nil || !strings.Contains(err.Error(), "daemon resume is owner-only and cannot run from an agent session") {
		t.Fatalf("owner session error = %v", err)
	}
}
