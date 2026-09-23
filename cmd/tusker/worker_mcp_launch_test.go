package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestWorkerMCPLaunchProjection(t *testing.T) {
	state := t.TempDir()
	if err := os.Chmod(state, 0755); err != nil {
		t.Fatal(err)
	}
	status := filepath.Join(state, "status.json")
	first, err := projectWorkerMCP("project with space", "record", "item", "attempt'quoted", 7, 11, filepath.Join(state, "events.jsonl"), status, 900, true)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(state); err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("private dir mode: %v %v", info, err)
	}
	for _, path := range []string{first.claudeConfig, first.claudeSettings} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("%s mode %o", path, info.Mode().Perm())
		}
		if filepath.Dir(path) != state {
			t.Fatalf("projection escaped attempt dir: %s", path)
		}
	}
	var config struct {
		MCPServers map[string]struct {
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Env     map[string]string `json:"env"`
		} `json:"mcpServers"`
	}
	b, _ := os.ReadFile(first.claudeConfig)
	if err := json.Unmarshal(b, &config); err != nil {
		t.Fatal(err)
	}
	if got := config.MCPServers["tusker"]; got.Command != first.command || !reflect.DeepEqual(got.Args, first.args) || got.Env["TUSKER_ATTEMPT_ID"] != "attempt'quoted" || got.Env["TUSKER_LEASE_GENERATION"] != "7" || got.Env["TUSKER_WORK_REVISION"] != "11" {
		t.Fatalf("bad MCP config: %+v", got)
	}
	settings, _ := os.ReadFile(first.claudeSettings)
	if !strings.Contains(string(settings), "mcp__tusker") || !strings.Contains(string(settings), "PostToolUse") || !strings.Contains(string(settings), "--format hook") {
		t.Fatalf("bad Claude settings: %s", settings)
	}
	if err := os.Remove(first.claudeConfig); err != nil {
		t.Fatal(err)
	}
	second, err := projectWorkerMCP("project with space", "record", "item", "attempt'quoted", 7, 11, filepath.Join(state, "events.jsonl"), status, 900, true)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("resume projection changed: %+v %+v", first, second)
	}
	if _, err := os.Stat(first.claudeConfig); err != nil {
		t.Fatal(err)
	}
}

func TestWorkerMCPStateRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "custom state")
	t.Setenv("TUSKER_STATE_ROOT", root)
	status := filepath.Join(t.TempDir(), "status.json")
	p, err := projectWorkerMCP("app", "record", "item", "attempt", 1, 1, filepath.Join(t.TempDir(), "events"), status, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if p.env["TUSKER_STATE_ROOT"] != root {
		t.Fatalf("env=%v", p.env)
	}
	settings, err := os.ReadFile(p.claudeSettings)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(settings), "TUSKER_STATE_ROOT=") || !strings.Contains(string(settings), "custom state") {
		t.Fatal(string(settings))
	}
}

func TestWorkerMCPLaunchCodexResumeOrdering(t *testing.T) {
	p := workerMCPProjection{command: "/tmp/tusker", args: []string{"mcp", "serve", "--max-wait", "900"}, env: map[string]string{"TUSKER_ATTEMPT_ID": "a quoted \" value", "TUSKER_PROJECT_ID": "p"}}
	start := appendCodexMCP([]string{"/tmp/codex", "exec", "--json", "-"}, p)
	resume := appendCodexMCP([]string{"/tmp/codex", "exec", "--json", "resume", "session", "-"}, p)
	if !reflect.DeepEqual(start[2:len(start)-2], resume[2:len(resume)-4]) {
		t.Fatalf("projection flags differ: %v %v", start, resume)
	}
	if resume[len(resume)-3] != "resume" {
		t.Fatalf("flags follow resume: %v", resume)
	}
	if !strings.Contains(strings.Join(resume, " "), `default_tools_approval_mode="approve"`) {
		t.Fatalf("missing approval override: %v", resume)
	}
	if again := appendCodexMCP(resume, p); !reflect.DeepEqual(again, resume) {
		t.Fatalf("resume projection duplicated: %v", again)
	}
}
