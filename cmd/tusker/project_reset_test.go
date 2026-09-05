package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResetRequiresExplicitYes(t *testing.T) {
	repo := t.TempDir()
	runGitDir(t, repo, "init", "-b", "main")
	err := resetCmd(Args{"repo": repo})
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("reset without --yes error = %v", err)
	}
	outside := t.TempDir()
	if err := resetCmd(Args{"repo": outside, "yes": "true"}); err == nil || !strings.Contains(err.Error(), "Tusker project") {
		t.Fatalf("reset outside a project error = %v", err)
	}
}

func TestResetPreservesSpecsAndRelaunchesV7Vault(t *testing.T) {
	repo := t.TempDir()
	runGitDir(t, repo, "init", "-b", "main")
	vault := filepath.Join(repo, defaultRepoVaultDir)
	keep := filepath.Join(vault, "specs", "keep.md")
	writeFileForResetTest(t, keep, "# Keep this spec\n")
	writeFileForResetTest(t, filepath.Join(vault, "work", "tasks", "APP-T-0001.md"), "stale task\n")
	writeFileForResetTest(t, filepath.Join(vault, "scratch", "stale.log"), "throwaway\n")
	writeFileForResetTest(t, filepath.Join(vault, "evidence", "APP-T-0001", "proof.md"), "old proof\n")
	source := filepath.Join(repo, "src", "keep.go")
	writeFileForResetTest(t, source, "package src\n")
	docSpec := filepath.Join(repo, "docs", "specs", "keep.md")
	writeFileForResetTest(t, docSpec, "# External spec\n")

	if err := resetCmd(Args{"repo": repo, "yes": "true"}); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(keep); err != nil || string(got) != "# Keep this spec\n" {
		t.Fatalf(".tusker/specs was not preserved: %q %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(vault, "work", "tasks", "APP-T-0001.md")); !os.IsNotExist(err) {
		t.Fatalf("stale task survived reset: %v", err)
	}
	if _, err := os.Stat(filepath.Join(vault, "scratch", "stale.log")); !os.IsNotExist(err) {
		t.Fatalf("scratch survived reset: %v", err)
	}
	if _, err := os.Stat(filepath.Join(vault, "evidence", "APP-T-0001", "proof.md")); !os.IsNotExist(err) {
		t.Fatalf("evidence survived reset: %v", err)
	}
	for _, relative := range []string{"WORKFLOW.md", "SKILL.md", "knowledge/domains/project/CANON.md"} {
		if _, err := os.Stat(filepath.Join(vault, relative)); err != nil {
			t.Fatalf("fresh V7 vault missing %s: %v", relative, err)
		}
	}
	if got, err := os.ReadFile(source); err != nil || string(got) != "package src\n" {
		t.Fatalf("source changed: %q %v", got, err)
	}
	if got, err := os.ReadFile(docSpec); err != nil || string(got) != "# External spec\n" {
		t.Fatalf("docs/specs changed: %q %v", got, err)
	}
	wf, err := loadWorkflow(vault)
	if err != nil {
		t.Fatalf("load reset workflow: %v", err)
	}
	if wf.Data.Agents.Default != string(RunnerCodexExec) || wf.Data.Reviewer.Runner != string(RunnerCodexExec) {
		t.Fatalf("reset seeded unavailable default runner: agents=%q reviewer=%q", wf.Data.Agents.Default, wf.Data.Reviewer.Runner)
	}
	if containsString(wf.Data.Agents.Enabled, string(RunnerCodexACP)) {
		t.Fatalf("reset enabled unconfigured ACP: %v", wf.Data.Agents.Enabled)
	}
	for name, profile := range wf.Data.RunnerProfiles {
		if profile.Harness == string(RunnerCodexACP) {
			t.Fatalf("reset seeded ACP profile %s without ACP setup", name)
		}
	}
	if _, _, err := runnerForName(string(RunnerCodexExec), wf.Data); err != nil {
		t.Fatalf("reset did not seed an available default runner: %v", err)
	}
}

func TestResetRefusesSymlinkedSpecsBeforeDeletion(t *testing.T) {
	repo := t.TempDir()
	runGitDir(t, repo, "init", "-b", "main")
	vault := filepath.Join(repo, defaultRepoVaultDir)
	if err := os.MkdirAll(vault, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(vault, "specs")); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(vault, "stale.txt")
	writeFileForResetTest(t, marker, "must remain after refusal\n")
	if err := resetCmd(Args{"repo": repo, "yes": "true"}); err == nil || !strings.Contains(err.Error(), "not a real directory") {
		t.Fatalf("symlinked specs reset error = %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("reset deleted state after refusing symlinked specs: %v", err)
	}
}

func writeFileForResetTest(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
