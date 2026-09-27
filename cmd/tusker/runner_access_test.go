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
		argv := wrapRunnerAccessArgv([]string{"/bin/echo", string(harness)}, runnerAccessPaths{protected: []string{"/tmp/private"}})
		if runtime.GOOS == "darwin" {
			if len(argv) != 5 || argv[0] != "/usr/bin/sandbox-exec" || argv[1] != "-p" || argv[3] != "/bin/echo" {
				t.Fatalf("%s: %v", harness, argv)
			}
		} else if len(argv) != 2 || argv[0] != "/bin/echo" {
			t.Fatalf("%s: %v", harness, argv)
		}
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
