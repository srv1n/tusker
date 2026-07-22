package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestRunnerPreflightDetectsBrokenCodexWrapper(t *testing.T) {
	brokenCodex := writeRunnerPreflightScript(t, t.TempDir(), "codex", `#!/bin/sh
echo "missing vendor binary" >&2
exit 127
`)

	blocker := runnerCommandPreflightBlocker(RunnerCodexExec, brokenCodex+" exec --json --skip-git-repo-check -")
	if !strings.Contains(blocker, "runner preflight blocked") ||
		!strings.Contains(blocker, "failed health check") ||
		!strings.Contains(blocker, "missing vendor binary") {
		t.Fatalf("expected broken wrapper health-check blocker, got %q", blocker)
	}
}

func TestRunnerPreflightPrefersRunnerPathPrefix(t *testing.T) {
	badDir := t.TempDir()
	goodDir := t.TempDir()
	writeRunnerPreflightScript(t, badDir, "codex", `#!/bin/sh
echo "bad codex" >&2
exit 127
`)
	writeRunnerPreflightScript(t, goodDir, "codex", `#!/bin/sh
echo "codex-test 1.0"
exit 0
`)
	t.Setenv("PATH", badDir)
	t.Setenv(runnerPathPrefixEnv, goodDir)

	if blocker := runnerCommandPreflightBlocker(RunnerCodexExec, "codex exec --json --skip-git-repo-check -"); blocker != "" {
		t.Fatalf("expected runner path prefix to avoid broken PATH entry, got %q", blocker)
	}
	pathValue := envValueForPreflightTest(runnerBaseEnv(), "PATH")
	first := filepath.SplitList(pathValue)[0]
	assertEqual(t, goodDir, first, "runner path prefix")
	if !containsString(filepath.SplitList(pathValue), badDir) {
		t.Fatalf("expected original PATH to be preserved in %q", pathValue)
	}
}

func TestRunnerResolutionFallsBackPastBrokenBinary(t *testing.T) {
	badDir := t.TempDir()
	goodDir := t.TempDir()
	writeRunnerPreflightScript(t, badDir, "codex", "#!/bin/sh\necho stale wrapper >&2\nexit 127\n")
	writeRunnerPreflightScript(t, goodDir, "codex", "#!/bin/sh\necho codex-test 1.0\n")
	t.Setenv("PATH", badDir+string(os.PathListSeparator)+goodDir)
	t.Setenv(runnerPathPrefixEnv, "")
	previousLoginPath := runnerLoginShellPath
	runnerLoginShellPath = func() string { return "" }
	t.Cleanup(func() { runnerLoginShellPath = previousLoginPath })

	result, blocker := runnerCommandPreflight(RunnerCodexExec, "codex exec --json --skip-git-repo-check -")
	if blocker != "" {
		t.Fatalf("expected healthy PATH fallback, got %q", blocker)
	}
	assertEqual(t, filepath.Join(goodDir, "codex"), result.ResolvedExecutable, "resolved executable")
	assertEqual(t, goodDir, result.RunnerPathPrefix, "launch PATH prefix")
	launchPath := envValueForPreflightTest(runnerEnv(runnerLaunchEnv{RunnerPathPrefix: result.RunnerPathPrefix}), "PATH")
	assertEqual(t, goodDir, filepath.SplitList(launchPath)[0], "healthy executable wins at launch")
}

func TestRunnerPreferredPathDirsFindsEachSupportedAppBundle(t *testing.T) {
	standalone := t.TempDir()
	chatGPT := t.TempDir()
	writeRunnerPreflightScript(t, standalone, "codex", "#!/bin/sh\nexit 0\n")
	writeRunnerPreflightScript(t, chatGPT, "codex", "#!/bin/sh\nexit 0\n")

	dirs := runnerPreferredPathDirsFrom([]string{standalone, chatGPT, t.TempDir()})
	assertEqual(t, []string{standalone, chatGPT}, dirs, "supported Codex app resource directories")
}

func TestDaemonParksRunnerPreflightFailureWithoutRetryChurn(t *testing.T) {
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Broken runner", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")

	brokenCodex := writeRunnerPreflightScript(t, t.TempDir(), "codex", `#!/bin/sh
echo "arm64 vendor binary missing" >&2
exit 127
`)
	wfFile, err := loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	wf := wfFile.Data
	command := brokenCodex + " exec --json --skip-git-repo-check -"
	wf.Codex.Command = command
	wf.Runners[string(RunnerCodexExec)] = RunnerDefinition{Kind: string(RunnerCodexExec), Command: command}
	writeWorkflowForPreflightTest(t, vault, wf, wfFile.Body)
	writeTuskerYamlRunnerCommandForPreflightTest(t, vault, command)

	project := registerAutomationTestProject(t, vault)
	daemon, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()

	for i := 0; i < 2; i++ {
		if err := daemon.PollOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
	}

	run := latestRunForRecord(t, daemon.store, project.ProjectID, "APP-T-0001")
	assertEqual(t, string(LeaseStateParkedNoProgress), run.LeaseState, "preflight lease state")
	assertEqual(t, string(AttemptOutcomeBlocked), run.AttemptOutcome, "preflight outcome")
	assertEqual(t, true, run.Terminal, "preflight terminal")
	assertEqual(t, 0, run.AttemptCount, "preflight attempt count")
	assertEqual(t, "", run.ActiveAttemptID, "preflight active attempt")
	assertEqual(t, 0, run.ProcessPID, "preflight process pid")
	if !strings.Contains(run.LastError, "runner preflight blocked") ||
		!strings.Contains(run.LastError, "failed health check") ||
		!strings.Contains(run.LastError, "arm64 vendor binary missing") {
		t.Fatalf("expected clear preflight blocker, got %#v", run)
	}
	attempts, err := daemon.store.ListAttemptsForRun(project.ProjectID, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 0 {
		t.Fatalf("expected no attempts after repeated polls, got %#v", attempts)
	}
	if blocker := automationRunBlocker(run, time.Now().UTC()); !strings.Contains(blocker, "runner preflight") || !strings.Contains(blocker, "redrive") {
		t.Fatalf("expected queue blocker to explain runner preflight/redrive, got %q", blocker)
	}
}

func TestDaemonSkipsAutomationDisabledProject(t *testing.T) {
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Automation off", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	project := registerAutomationTestProject(t, vault)
	if _, err := setProjectLocalConfigWithReadback(vault, "automation.enabled", false); err != nil {
		t.Fatal(err)
	}
	daemon, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()

	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	run := latestRunForRecord(t, daemon.store, project.ProjectID, "APP-T-0001")
	assertEqual(t, string(LeaseStateUnclaimed), run.LeaseState, "automation-disabled lease state")
	if !strings.Contains(run.LastError, "automation is disabled in its configuration") {
		t.Fatalf("expected explicit automation-disabled reason, got %#v", run)
	}
	attempts, err := daemon.store.ListAttemptsForRun(project.ProjectID, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 0, len(attempts), "automation-disabled attempt count")
}

func TestInteractiveClaimRegistersRunWithAutomationOff(t *testing.T) {
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Manual automation off", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	initializeOrchestrationGitRepo(t, filepath.Dir(vault))
	project := registerAutomationTestProject(t, vault)
	if _, err := setProjectLocalConfigWithReadback(vault, "automation.enabled", false); err != nil {
		t.Fatal(err)
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	project.Enabled = false
	project.Health = projectHealthDisabled
	if err := store.UpsertProject(project); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	captureStdout(t, func() {
		if err := runsClaimCmd(Args{"vault": vault, "id": "APP-T-0001", "owner": "agent:codex", "source": "codex"}); err != nil {
			t.Fatal(err)
		}
	})
	captureStdout(t, func() {
		if err := runsLifecycleCmd(Args{"id": "APP-T-0001", "owner": "agent:codex"}, "start"); err != nil {
			t.Fatal(err)
		}
		if err := runsLifecycleCmd(Args{"id": "APP-T-0001", "owner": "agent:codex"}, "heartbeat"); err != nil {
			t.Fatal(err)
		}
	})
	store, err = OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run, err := store.FindRun("APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if run == nil {
		t.Fatal("interactive claim did not register a run")
	}
	assertEqual(t, string(LeaseStateRunning), run.LeaseState, "interactive automation-off run state")
	assertEqual(t, true, run.HandRun, "interactive automation-off origin")
	if strings.TrimSpace(run.LastHeartbeatAt) == "" {
		t.Fatalf("interactive automation-off run did not record a heartbeat: %#v", run)
	}
	captureStdout(t, func() {
		if err := runsLifecycleCmd(Args{"id": "APP-T-0001", "owner": "agent:codex", "gate-verdicts": "A1=pass", "deliverable": "manual run submitted", "verification": "A1=pass"}, "submit"); err != nil {
			t.Fatal(err)
		}
	})
	noteData, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "review", stringField(noteData, "status"), "interactive automation-off submit status")
}

func writeRunnerPreflightScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeWorkflowForPreflightTest(t *testing.T, vault string, wf Workflow, body string) {
	t.Helper()
	raw, err := yaml.Marshal(wf)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(workflowPath(vault), "---\n"+strings.TrimSpace(string(raw))+"\n---\n"+body); err != nil {
		t.Fatal(err)
	}
}

func writeTuskerYamlRunnerCommandForPreflightTest(t *testing.T, vault, command string) {
	t.Helper()
	configPath := filepath.Join(filepath.Dir(vault), "tusker.yaml")
	text, err := readText(configPath)
	if err != nil {
		t.Fatal(err)
	}
	const defaultLine = "command: codex exec --json --skip-git-repo-check -"
	if !strings.Contains(text, defaultLine) {
		t.Fatalf("expected default runner command in %s", configPath)
	}
	text = strings.Replace(text, defaultLine, "command: "+command, 1)
	if err := writeText(configPath, text); err != nil {
		t.Fatal(err)
	}
}

func envValueForPreflightTest(env []string, key string) string {
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	return ""
}
