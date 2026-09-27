package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRunnerAccessPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("TUSKER_CONFIG", "")
	t.Setenv("TUSKER_STATE_ROOT", "")
	config := filepath.Join(home, ".config", "tusker", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(config), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte("access:\n  protected_paths:\n    - ~/Documents\n    - /tmp/owner-private\n"), 0600); err != nil {
		t.Fatal(err)
	}
	paths, err := effectiveRunnerDenyPaths(filepath.Join(home, "worktree"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{filepath.Join(home, ".ssh"), filepath.Join(home, "Documents"), "/tmp/owner-private"} {
		if !containsString(paths.protected, want) {
			t.Errorf("missing protected path %s: %v", want, paths.protected)
		}
	}
	if !containsString(paths.state, filepath.Join(home, ".config", "tusker")) {
		t.Fatalf("missing state path: %v", paths.state)
	}
	if strings.Contains(sandboxExecProfile(paths), DefaultStateRoot()) {
		t.Fatalf("runtime state root must remain writable: %v", paths.state)
	}
}

func TestRunnerAccessOverlappingCustomStateRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TUSKER_STATE_ROOT", root)
	t.Setenv("TUSKER_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))
	worktree := filepath.Join(root, "workspaces", "task")
	paths, err := effectiveRunnerDenyPaths(worktree)
	if err != nil {
		t.Fatal(err)
	}
	if containsString(paths.state, root) {
		t.Fatalf("overlapping custom state root would deny the worktree: root=%s worktree=%s state=%v", root, worktree, paths.state)
	}
}

func TestRunnerAccessClaudeSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"permissions":{"allow":["mcp__tusker"]},"hooks":{"PostToolUse":[]}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := addClaudeAccessSettings(path, runnerAccessPaths{protected: []string{"/tmp/private"}}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"mcp__tusker"`) || !strings.Contains(string(data), `"Read(//tmp/private/**)"`) || !strings.Contains(string(data), `"PostToolUse"`) {
		t.Fatal(string(data))
	}
}

func TestClaudeProfilePolicyAndDenySettings(t *testing.T) {
	workflow := CodexPolicy{ApprovalPolicy: "never", ThreadSandbox: "workspace-write", TurnSandboxPolicy: "workspace-write"}
	full := ResolvedRunnerProfile{Name: "claude-opus-high", Definition: RunnerProfileDefinition{
		Harness: string(RunnerClaude), PermissionPreset: "danger-full-access",
		Sandbox: RunnerSandboxDefinition{Mode: "danger-full-access"},
	}}
	policy := codexPolicyForResolvedProfile(workflow, runLaneExecute, full)
	mode, err := claudePermissionModeForPolicy(policy)
	if err != nil || mode != "bypassPermissions" {
		t.Fatalf("full-access Claude profile resolved to %q, policy=%#v, err=%v", mode, policy, err)
	}

	dir := t.TempDir()
	projection, err := projectWorkerMCP("project", "record", "item", "attempt", 1, 1, filepath.Join(dir, "events"), filepath.Join(dir, "status"), 900, true)
	if err != nil {
		t.Fatal(err)
	}
	paths := runnerAccessPaths{protected: []string{"/tmp/private"}, state: []string{"/tmp/tusker-state"}}
	if err := addClaudeAccessSettings(projection.claudeSettings, paths); err != nil {
		t.Fatal(err)
	}
	argv := appendClaudeMCP([]string{"claude", "--permission-mode", mode}, projection)
	if !containsRunnerArgPair(argv, "--settings", projection.claudeSettings) {
		t.Fatalf("Claude settings missing from argv: %v", argv)
	}
	settings, err := os.ReadFile(projection.claudeSettings)
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range []string{`Read(//tmp/private/**)`, `Edit(//tmp/tusker-state/**)`, `Bash(git reset --hard*)`} {
		if !strings.Contains(string(settings), rule) {
			t.Fatalf("Claude full-access settings missing deny rule %q: %s", rule, settings)
		}
	}

	review := ResolvedRunnerProfile{Name: "claude-review", Definition: RunnerProfileDefinition{
		Harness: string(RunnerClaude), PermissionPreset: "read-only",
		Sandbox: RunnerSandboxDefinition{Mode: "read-only"},
	}}
	policy = codexPolicyForResolvedProfile(workflow, runLaneReview, review)
	mode, err = claudePermissionModeForPolicy(policy)
	if err != nil || mode != "plan" {
		t.Fatalf("read-only Claude review resolved to %q, policy=%#v, err=%v", mode, policy, err)
	}
}

func TestRunnerAccessProfileAndClaudeRules(t *testing.T) {
	paths := runnerAccessPaths{protected: []string{`/tmp/private "folder"`}, state: []string{"/tmp/state"}}
	profile := sandboxExecProfile(paths)
	if !strings.Contains(profile, `(deny file-read* file-write* (subpath "/tmp/private \"folder\""))`) || !strings.Contains(profile, `(deny file-write* (subpath "/tmp/state")`) {
		t.Fatal(profile)
	}
	if strings.Contains(profile, `(deny file-read* file-write* (subpath "/tmp/state"`) {
		t.Fatal("state reads must remain available")
	}
	rules := claudeAccessDenyRules(paths)
	for _, want := range []string{`Read(//tmp/private "folder"/**)`, "Edit(//tmp/state/**)", "Bash(git push --force*)", "Bash(rm -rf /*)"} {
		if !containsString(rules, want) {
			t.Errorf("missing Claude rule %q", want)
		}
	}
	if containsString(rules, "Read(//tmp/state/**)") {
		t.Fatal("state reads must remain available")
	}
}

func TestRunnerAccessArgvWrapping(t *testing.T) {
	for _, harness := range []RunnerName{RunnerCodexExec, RunnerMuse, RunnerDevin} {
		base := []string{"/bin/echo", string(harness)}
		paths := runnerAccessPaths{protected: []string{"/tmp/private"}}
		for _, mode := range []string{"workspace-write", "read-only"} {
			argv := wrapRunnerAccessArgv(base, paths, CodexPolicy{TurnSandboxPolicy: mode})
			if !equalStringSlices(argv, base) {
				t.Fatalf("%s %s should use native sandbox: %v", harness, mode, argv)
			}
		}
		argv := wrapRunnerAccessArgv(base, paths, CodexPolicy{TurnSandboxPolicy: "danger-full-access"})
		if runtime.GOOS == "darwin" {
			if len(argv) != 5 || argv[0] != "/usr/bin/sandbox-exec" || argv[1] != "-p" || argv[3] != "/bin/echo" {
				t.Fatalf("%s: %v", harness, argv)
			}
		} else if len(argv) != 2 || argv[0] != "/bin/echo" {
			t.Fatalf("%s: %v", harness, argv)
		}
	}
}

func TestDevinFullAccessProfileUsesBypassAndDenyWrapper(t *testing.T) {
	profile := ResolvedRunnerProfile{Name: "devin-swe-2-max", Definition: RunnerProfileDefinition{
		Harness: string(RunnerDevin), PermissionPreset: "danger-full-access",
		Sandbox: RunnerSandboxDefinition{Mode: "danger-full-access"},
	}}
	policy := codexPolicyForResolvedProfile(CodexPolicy{}, runLaneExecute, profile)
	mode, err := devinACPModeForPolicy(policy)
	if err != nil || mode != "bypass" {
		t.Fatalf("Devin full-access mode=%q policy=%#v err=%v", mode, policy, err)
	}
	argv := wrapRunnerAccessArgv([]string{"/usr/bin/devin", "acp"}, runnerAccessPaths{protected: []string{"/tmp/private"}}, policy)
	if runtime.GOOS == "darwin" && (len(argv) < 4 || argv[0] != "/usr/bin/sandbox-exec" || !strings.Contains(argv[2], "/tmp/private")) {
		t.Fatalf("Devin full-access deny wrapper missing: %v", argv)
	}
}

func TestRunnerAccessSandboxExecDarwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS sandbox-exec only")
	}
	if _, err := os.Stat("/usr/bin/sandbox-exec"); err != nil {
		t.Skip(err)
	}
	root := t.TempDir()
	protected := filepath.Join(root, "protected")
	allowed := filepath.Join(root, "allowed")
	for _, path := range []string{protected, allowed} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	profile := sandboxExecProfile(runnerAccessPaths{protected: []string{protected}})
	for _, tc := range []struct {
		path   string
		denied bool
	}{{filepath.Join(protected, "f"), true}, {filepath.Join(allowed, "f"), false}} {
		cmd := exec.Command("/usr/bin/sandbox-exec", "-p", profile, "/bin/sh", "-c", `echo x > "$1"`, "sh", tc.path)
		err := cmd.Run()
		if (err != nil) != tc.denied {
			t.Fatalf("write %s: error %v, want denied %v", tc.path, err, tc.denied)
		}
	}
	if err := os.WriteFile(filepath.Join(protected, "existing"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("/usr/bin/sandbox-exec", "-p", profile, "/bin/cat", filepath.Join(protected, "existing")).Run(); err == nil {
		t.Fatal("protected read succeeded")
	}
}
