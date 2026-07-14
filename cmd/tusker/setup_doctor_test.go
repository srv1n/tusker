package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSetupDoctorRepairsStaleKurpodVaultRootIdempotently(t *testing.T) {
	stateRoot := t.TempDir()
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	repo := filepath.Join(t.TempDir(), "kurpod")
	vault := filepath.Join(repo, ".tusker")
	if err := writeText(workflowPath(vault), defaultWorkflowMarkdown()); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(repo, "tusker")
	project := RegisteredProject{ProjectID: "kurpod", ProjectKey: "kurpod", Name: "kurpod", RepoRoot: repo, VaultRoot: stale, WorkflowPath: workflowPath(stale), Enabled: true}
	if err := store.UpsertProject(project); err != nil {
		t.Fatal(err)
	}
	if _, err := store.exec(`UPDATE projects SET workflow_path = ? WHERE project_id = ?`, project.WorkflowPath, project.ProjectID); err != nil {
		t.Fatal(err)
	}

	dry, err := runSetupDoctor(setupDoctorInput{RepoRoot: repo, Store: store, Source: repo}, false)
	if err != nil {
		t.Fatal(err)
	}
	assertSetupFinding(t, dry, "stale_vault_root", false)
	projects, _ := store.ListProjects()
	assertEqual(t, stale, projects[0].VaultRoot, "doctor must not mutate registration")

	repaired, err := runSetupDoctor(setupDoctorInput{RepoRoot: repo, Store: store, Source: repo}, true)
	if err != nil {
		t.Fatal(err)
	}
	assertSetupFinding(t, repaired, "stale_vault_root", true)
	projects, _ = store.ListProjects()
	assertEqual(t, vault, projects[0].VaultRoot, "repair canonical vault root")
	assertEqual(t, workflowPath(vault), projects[0].WorkflowPath, "repair workflow path")

	again, err := runSetupDoctor(setupDoctorInput{RepoRoot: repo, Store: store, Source: repo}, true)
	if err != nil {
		t.Fatal(err)
	}
	if findingByCode(again, "stale_vault_root") != nil {
		t.Fatalf("second repair retained stale vault finding: %#v", again.Findings)
	}
}

func TestSetupDoctorRepairsGeneratedSkillInstallsFromCanonicalSource(t *testing.T) {
	sourceRoot := t.TempDir()
	if err := writeText(filepath.Join(sourceRoot, "skill", "SKILL.md"), "canonical\n"); err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	if err := writeText(filepath.Join(repo, ".agents", "skills", "tusker", "SKILL.md"), "stale copy\n"); err != nil {
		t.Fatal(err)
	}

	dry, err := runSetupDoctor(setupDoctorInput{RepoRoot: repo, Source: sourceRoot}, false)
	if err != nil {
		t.Fatal(err)
	}
	assertSetupFinding(t, dry, "skill_install_generated_copy", false)
	assertSetupFinding(t, dry, "skill_install_missing", false)

	first, err := runSetupDoctor(setupDoctorInput{RepoRoot: repo, Source: sourceRoot}, true)
	if err != nil {
		t.Fatal(err)
	}
	assertSetupFinding(t, first, "skill_install_generated_copy", true)
	assertSetupFinding(t, first, "skill_install_missing", true)
	for _, path := range []string{filepath.Join(repo, ".agents", "skills", "tusker"), filepath.Join(repo, ".claude", "skills", "tusker")} {
		assertIsSymlink(t, path)
	}

	second, err := runSetupDoctor(setupDoctorInput{RepoRoot: repo, Source: sourceRoot}, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range second.Findings {
		if finding.Code == "skill_install_generated_copy" || finding.Code == "skill_install_missing" || finding.Code == "skill_install_stale" {
			t.Fatalf("idempotent repair retained skill drift: %#v", finding)
		}
	}
}

func TestSkillSyncSourceClassificationRejectsGeneratedAndInvalidSources(t *testing.T) {
	canonical := t.TempDir()
	if err := writeText(filepath.Join(canonical, "skill", "SKILL.md"), "canonical\n"); err != nil {
		t.Fatal(err)
	}
	generated := filepath.Join(t.TempDir(), ".agents", "skills", "tusker")
	if err := writeText(filepath.Join(generated, "SKILL.md"), "generated\n"); err != nil {
		t.Fatal(err)
	}
	invalid := t.TempDir()

	assertEqual(t, "canonical", classifySkillSyncSource(canonical, "").Kind, "canonical source kind")
	assertEqual(t, "generated", classifySkillSyncSource(generated, "").Kind, "generated source kind")
	assertEqual(t, "invalid", classifySkillSyncSource(invalid, "").Kind, "invalid source kind")

	repo := t.TempDir()
	for _, source := range []string{generated, invalid} {
		if err := skillSyncCmd(Args{"repo": repo, "source": source, "quiet": "true"}); err == nil {
			t.Fatalf("skill sync accepted non-canonical source %s", source)
		}
	}

	output := captureStdout(t, func() {
		if err := skillSyncCmd(Args{"repo": repo, "source": canonical, "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var payload map[string]any
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "canonical", mapString(payload, "skill_source_kind"), "reported skill source kind")
	assertEqual(t, filepath.Join(canonical, "skill"), mapString(payload, "skill_source"), "reported canonical source")
}

func TestSkillSyncCopyUsesEmbeddedCanonicalPayloadOutsideCheckout(t *testing.T) {
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Chdir(outside); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
	repo := t.TempDir()
	output := captureStdout(t, func() {
		if err := skillSyncCmd(Args{"repo": repo, "mode": "copy", "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var payload map[string]any
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "embedded", mapString(payload, "skill_source_kind"), "embedded copy provenance")
	for _, path := range []string{filepath.Join(repo, ".agents", "skills", "tusker"), filepath.Join(repo, ".claude", "skills", "tusker")} {
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			t.Fatalf("portable copy unexpectedly symlinked: %s", path)
		}
	}
}

func TestSetupDoctorDiagnosesOfflineHandoffAndRepairsZipPatternOnly(t *testing.T) {
	repo := t.TempDir()
	configPath := filepath.Join(repo, ".chatgpt-handoff.json")
	config := map[string]any{
		"profile":                           "architect",
		"project_id":                        "project-123",
		"browser_workflow_version":          "v1",
		"required_browser_workflow_version": "v2",
		"preserve_me":                       "yes",
	}
	if err := writeHandoffConfig(configPath, config); err != nil {
		t.Fatal(err)
	}

	dry, err := runSetupDoctor(setupDoctorInput{RepoRoot: repo}, false)
	if err != nil {
		t.Fatal(err)
	}
	assertSetupFinding(t, dry, "handoff_zip_pattern_missing", false)
	stale := findingByCode(dry, "handoff_browser_workflow_stale")
	if stale == nil || stale.Repairable || stale.Action == "" {
		t.Fatalf("stale browser workflow must be actionable but not forged as repaired: %#v", stale)
	}

	first, err := runSetupDoctor(setupDoctorInput{RepoRoot: repo}, true)
	if err != nil {
		t.Fatal(err)
	}
	assertSetupFinding(t, first, "handoff_zip_pattern_missing", true)
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var repaired map[string]any
	if err := json.Unmarshal(raw, &repaired); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "yes", mapString(repaired, "preserve_me"), "unknown handoff config preserved")
	assertEqual(t, "*.zip", handoffAttachmentPattern(repaired), "safe zip default")

	second, err := runSetupDoctor(setupDoctorInput{RepoRoot: repo}, true)
	if err != nil {
		t.Fatal(err)
	}
	if findingByCode(second, "handoff_zip_pattern_missing") != nil {
		t.Fatalf("second repair rewrote zip pattern: %#v", second.Findings)
	}
}

func TestSetupDoctorRejectsInvalidZipAttachmentPatternOffline(t *testing.T) {
	repo := t.TempDir()
	config := map[string]any{
		"profile":    "architect",
		"project_id": "project-123",
		"attachments": map[string]any{
			"zip_pattern": "*.patch",
		},
	}
	if err := writeHandoffConfig(filepath.Join(repo, ".chatgpt-handoff.json"), config); err != nil {
		t.Fatal(err)
	}
	report, err := runSetupDoctor(setupDoctorInput{RepoRoot: repo}, false)
	if err != nil {
		t.Fatal(err)
	}
	assertSetupFinding(t, report, "handoff_zip_pattern_invalid", false)
}

func TestSetupDoctorReportsWorkflowAndBinaryMismatch(t *testing.T) {
	stateRoot := t.TempDir()
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	repo := t.TempDir()
	vault := filepath.Join(repo, ".tusker")
	if err := writeText(workflowPath(vault), defaultWorkflowMarkdown()); err != nil {
		t.Fatal(err)
	}
	project := RegisteredProject{ProjectID: "app", RepoRoot: repo, VaultRoot: vault, WorkflowPath: filepath.Join(vault, "old-WORKFLOW.md"), Enabled: true}
	if err := store.UpsertProject(project); err != nil {
		t.Fatal(err)
	}
	if _, err := store.exec(`UPDATE projects SET workflow_path = ? WHERE project_id = ?`, project.WorkflowPath, project.ProjectID); err != nil {
		t.Fatal(err)
	}
	binA := filepath.Join(t.TempDir(), "tusker-a")
	binB := filepath.Join(t.TempDir(), "tusker-b")
	if err := writeText(binA, "a"); err != nil {
		t.Fatal(err)
	}
	if err := writeText(binB, "b"); err != nil {
		t.Fatal(err)
	}

	report, err := runSetupDoctor(setupDoctorInput{RepoRoot: repo, Store: store, ExecutablePath: binA, InstalledPath: binB}, false)
	if err != nil {
		t.Fatal(err)
	}
	assertSetupFinding(t, report, "workflow_path_mismatch", false)
	assertSetupFinding(t, report, "binary_version_mismatch", false)
}

func assertSetupFinding(t *testing.T, report setupDoctorReport, code string, changed bool) {
	t.Helper()
	finding := findingByCode(report, code)
	if finding == nil {
		t.Fatalf("missing setup finding %s in %#v", code, report.Findings)
	}
	if finding.Changed != changed {
		t.Fatalf("finding %s changed=%v, want %v", code, finding.Changed, changed)
	}
}

func findingByCode(report setupDoctorReport, code string) *setupFinding {
	for i := range report.Findings {
		if report.Findings[i].Code == code {
			return &report.Findings[i]
		}
	}
	return nil
}
