package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
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
	writeCanonicalTuskerSkillFixture(t, sourceRoot)
	repo := t.TempDir()
	if err := writeText(filepath.Join(repo, ".agents", "skills", "tusker", "SKILL.md"), "stale copy\n"); err != nil {
		t.Fatal(err)
	}

	dry, err := runSetupDoctor(setupDoctorInput{RepoRoot: repo, Source: sourceRoot}, false)
	if err != nil {
		t.Fatal(err)
	}
	assertSetupFinding(t, dry, "skill_install_missing_provenance", false)
	assertSetupFinding(t, dry, "skill_install_missing", false)

	first, err := runSetupDoctor(setupDoctorInput{RepoRoot: repo, Source: sourceRoot}, true)
	if err != nil {
		t.Fatal(err)
	}
	assertSetupFinding(t, first, "skill_install_missing_provenance", true)
	assertSetupFinding(t, first, "skill_install_missing", true)
	for _, code := range []string{"skill_install_missing_provenance", "skill_install_missing"} {
		finding := findingByCode(first, code)
		if finding.Provenance == nil || finding.Provenance.Status != "current" {
			t.Fatalf("repair %s did not report re-inspected current provenance: %#v", code, finding)
		}
	}
	for _, path := range []string{filepath.Join(repo, ".agents", "skills", "tusker"), filepath.Join(repo, ".claude", "skills", "tusker")} {
		assertIsSymlink(t, path)
	}

	second, err := runSetupDoctor(setupDoctorInput{RepoRoot: repo, Source: sourceRoot}, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range second.Findings {
		if finding.Code == "skill_install_missing_provenance" || finding.Code == "skill_install_missing" || finding.Code == "skill_install_stale" {
			t.Fatalf("idempotent repair retained skill drift: %#v", finding)
		}
	}
}

func TestSetupDoctorRepairsLocallyModifiedSkillInstall(t *testing.T) {
	sourceRoot := t.TempDir()
	writeCanonicalTuskerSkillFixture(t, sourceRoot)
	repo := t.TempDir()
	destination := filepath.Join(repo, ".agents", "skills", "tusker")
	if err := installSkillPayloadCopy(destination); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(destination, "SKILL.md"), "local operator edit\n"); err != nil {
		t.Fatal(err)
	}
	if got := inspectSkillMaterialization(destination); got.Status != "locally_modified" {
		t.Fatalf("fixture status = %#v", got)
	}

	dry, err := runSetupDoctor(setupDoctorInput{RepoRoot: repo, Source: sourceRoot}, false)
	if err != nil {
		t.Fatal(err)
	}
	assertSetupFinding(t, dry, "skill_install_locally_modified", false)

	repaired, err := runSetupDoctor(setupDoctorInput{RepoRoot: repo, Source: sourceRoot}, true)
	if err != nil {
		t.Fatal(err)
	}
	finding := findingByCode(repaired, "skill_install_locally_modified")
	if finding == nil || !finding.Changed || finding.Provenance == nil || finding.Provenance.Status != "current" {
		t.Fatalf("locally modified repair = %#v in %#v", finding, repaired)
	}
	assertIsSymlink(t, destination)
}

func TestAsymmetricManagedSkillMetadataBlocksClaimedWaveAndSetupRepairs(t *testing.T) {
	tests := []struct {
		name string
		old  string
		next string
	}{
		{name: "wave authorization schema", old: "wave_authorization_schemas: [tusker.wave-authorization/v1]", next: "wave_authorization_schemas: [tusker.wave-authorization/v0]"},
		{name: "workflow version", old: "workflow_min: 1", next: "workflow_min: 2"},
		{name: "tracker schema version", old: "tracker_schema_versions: [7]", next: "tracker_schema_versions: [6]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			goodRoot := t.TempDir()
			badRoot := t.TempDir()
			writeCanonicalTuskerSkillFixture(t, goodRoot)
			writeCanonicalTuskerSkillFixture(t, badRoot)
			goodPackage := filepath.Join(goodRoot, "skills", "tusker")
			badPackage := filepath.Join(badRoot, "skills", "tusker")
			compatibilityPath := filepath.Join(badPackage, "assets", skillCompatibilityFilename)
			raw, err := os.ReadFile(compatibilityPath)
			if err != nil {
				t.Fatal(err)
			}
			changed := strings.Replace(string(raw), tt.old, tt.next, 1)
			if changed == string(raw) {
				t.Fatalf("fixture did not contain %q", tt.old)
			}
			if err := writeText(compatibilityPath, changed); err != nil {
				t.Fatal(err)
			}

			repo := t.TempDir()
			agents := filepath.Join(repo, ".agents", "skills", "tusker")
			claude := filepath.Join(repo, ".claude", "skills", "tusker")
			for destination, source := range map[string]string{agents: badPackage, claude: goodPackage} {
				if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(source, destination); err != nil {
					t.Fatal(err)
				}
			}
			if state := inspectSkillMaterialization(agents); state.Status != "incompatible" {
				t.Fatalf("invalid .agents compatibility metadata = %#v", state)
			}
			if state := inspectSkillMaterialization(claude); state.Status != "current" {
				t.Fatalf("healthy .claude package = %#v", state)
			}

			contract, err := embeddedFactoryIntakeContractProvenance()
			if err != nil {
				t.Fatal(err)
			}
			claimed := Note{Data: map[string]any{
				"factory_intake_contract_schema":      contract.Schema,
				"factory_intake_contract_version":     contract.Version,
				"factory_intake_contract_fingerprint": contract.Fingerprint,
			}}
			blockers := strings.Join(waveFactoryIntakeContractBlockers(filepath.Join(repo, ".tusker"), claimed), "\n")
			if !strings.Contains(blockers, ".agents skill is incompatible") || strings.Contains(blockers, ".claude skill is") {
				t.Fatalf("asymmetric claimed-wave blockers = %q", blockers)
			}

			dry, err := runSetupDoctor(setupDoctorInput{RepoRoot: repo, Source: goodRoot}, false)
			if err != nil {
				t.Fatal(err)
			}
			finding := findingByCode(dry, "skill_install_incompatible")
			if dry.OK || finding == nil || finding.Path != agents || !finding.Repairable || finding.Changed {
				t.Fatalf("asymmetric setup finding = %#v in %#v", finding, dry)
			}
			repaired, err := runSetupDoctor(setupDoctorInput{RepoRoot: repo, Source: goodRoot}, true)
			if err != nil {
				t.Fatal(err)
			}
			finding = findingByCode(repaired, "skill_install_incompatible")
			if !repaired.OK || finding == nil || !finding.Changed || finding.Provenance == nil || finding.Provenance.Status != "current" {
				t.Fatalf("asymmetric setup repair = %#v in %#v", finding, repaired)
			}
			if got := waveFactoryIntakeContractBlockers(filepath.Join(repo, ".tusker"), claimed); len(got) != 0 {
				t.Fatalf("claimed-wave blockers remained after repair: %#v", got)
			}
		})
	}
}

func TestSkillSyncSourceClassificationRejectsGeneratedAndInvalidSources(t *testing.T) {
	canonical := t.TempDir()
	writeCanonicalTuskerSkillFixture(t, canonical)
	generated := filepath.Join(t.TempDir(), ".agents", "skills", "tusker")
	writeCanonicalTuskerSkillPackage(t, generated)
	invalid := t.TempDir()
	if err := writeText(filepath.Join(invalid, "SKILL.md"), "---\nname: chatgpt-handoff\n---\n# ChatGPT handoff\n"); err != nil {
		t.Fatal(err)
	}
	arbitrary := t.TempDir()
	spoofed := `---
name: another-skill
---
# Tusker Operator Skill
<!-- name: tusker; wave_authorization_schema: "tusker.wave-authorization/v1"; workflow_version: 1; tracker_schema_version: 7 -->
`
	if err := writeText(filepath.Join(arbitrary, "SKILL.md"), spoofed); err != nil {
		t.Fatal(err)
	}

	assertEqual(t, "canonical", classifySkillSyncSource(canonical, "").Kind, "canonical source kind")
	assertEqual(t, "generated", classifySkillSyncSource(generated, "").Kind, "generated source kind")
	assertEqual(t, "invalid", classifySkillSyncSource(invalid, "").Kind, "invalid source kind")
	assertEqual(t, "invalid", classifySkillSyncSource(arbitrary, "").Kind, "arbitrary SKILL.md source kind")

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
	assertEqual(t, filepath.Join(canonical, "skills", "tusker"), mapString(payload, "skill_source"), "reported canonical source")
}

func TestSkillSyncRejectsCanonicalLookalikeWithoutExactFactoryContract(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(string) error
	}{
		{name: "missing", mutate: os.Remove},
		{name: "malformed", mutate: func(path string) error {
			return writeText(path, "schema: [unterminated\n")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sourceRoot := t.TempDir()
			writeCanonicalTuskerSkillFixture(t, sourceRoot)
			source := filepath.Join(sourceRoot, "skills", "tusker")
			if err := tt.mutate(filepath.Join(source, "assets", "factory-intake-contract.yaml")); err != nil {
				t.Fatal(err)
			}
			if got := classifySkillSyncSource(sourceRoot, "").Kind; got != "invalid" {
				t.Fatalf("lookalike source kind = %q, want invalid", got)
			}

			repo := t.TempDir()
			for _, destination := range []string{filepath.Join(repo, ".agents", "skills", "tusker"), filepath.Join(repo, ".claude", "skills", "tusker")} {
				if err := writeText(filepath.Join(destination, "SKILL.md"), "preserve existing install\n"); err != nil {
					t.Fatal(err)
				}
			}
			if err := writeText(filepath.Join(repo, "AGENTS.md"), "preserve repo instructions\n"); err != nil {
				t.Fatal(err)
			}
			if err := skillSyncCmd(Args{"repo": repo, "source": sourceRoot, "quiet": "true"}); err == nil {
				t.Fatal("skill sync accepted a canonical lookalike without an exact factory contract")
			}
			for _, destination := range []string{filepath.Join(repo, ".agents", "skills", "tusker"), filepath.Join(repo, ".claude", "skills", "tusker")} {
				info, err := os.Lstat(destination)
				if err != nil || !info.IsDir() {
					t.Fatalf("managed destination was replaced: %s (%v)", destination, err)
				}
				raw, err := os.ReadFile(filepath.Join(destination, "SKILL.md"))
				if err != nil || string(raw) != "preserve existing install\n" {
					t.Fatalf("managed destination changed: %s = %q (%v)", destination, raw, err)
				}
			}
			raw, err := os.ReadFile(filepath.Join(repo, "AGENTS.md"))
			if err != nil || string(raw) != "preserve repo instructions\n" {
				t.Fatalf("skill sync escaped managed packages: %q (%v)", raw, err)
			}
		})
	}
}

func TestSetupRepairRejectsCanonicalLookalikeWithoutFalseSuccess(t *testing.T) {
	sourceRoot := t.TempDir()
	writeCanonicalTuskerSkillFixture(t, sourceRoot)
	if err := writeText(filepath.Join(sourceRoot, "skills", "tusker", "assets", "factory-intake-contract.yaml"), "schema: [unterminated\n"); err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	existing := filepath.Join(repo, ".agents", "skills", "tusker", "SKILL.md")
	if err := writeText(existing, "preserve stale install\n"); err != nil {
		t.Fatal(err)
	}

	report, err := runSetupDoctor(setupDoctorInput{RepoRoot: repo, Source: sourceRoot}, true)
	if err != nil {
		t.Fatal(err)
	}
	if report.OK {
		t.Fatalf("setup repair falsely reported OK: %#v", report)
	}
	for _, code := range []string{"skill_install_missing_provenance", "skill_install_missing"} {
		finding := findingByCode(report, code)
		if finding == nil || finding.Changed || finding.Repairable {
			t.Fatalf("lookalike setup finding %s = %#v", code, finding)
		}
	}
	raw, err := os.ReadFile(existing)
	if err != nil || string(raw) != "preserve stale install\n" {
		t.Fatalf("setup repair changed existing destination: %q (%v)", raw, err)
	}
	if pathExistsIncludingSymlink(filepath.Join(repo, ".claude", "skills", "tusker")) {
		t.Fatal("setup repair created a destination from the invalid source")
	}
}

func TestSymlinkInstallRejectsCanonicalLookalikeBeforeDestinationMutation(t *testing.T) {
	sourceRoot := t.TempDir()
	writeCanonicalTuskerSkillFixture(t, sourceRoot)
	if err := os.Remove(filepath.Join(sourceRoot, "skills", "tusker", "assets", "factory-intake-contract.yaml")); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "managed", "tusker")
	existing := filepath.Join(destination, "SKILL.md")
	if err := writeText(existing, "preserve direct install\n"); err != nil {
		t.Fatal(err)
	}
	if err := installSkillPayloadSymlink(destination, sourceRoot); err == nil {
		t.Fatal("symlink installer accepted a canonical lookalike without its factory contract")
	}
	info, err := os.Lstat(destination)
	if err != nil || !info.IsDir() {
		t.Fatalf("symlink installer replaced destination before validation: %v", err)
	}
	raw, err := os.ReadFile(existing)
	if err != nil || string(raw) != "preserve direct install\n" {
		t.Fatalf("symlink installer changed destination before validation: %q (%v)", raw, err)
	}
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

func TestSetupDoctorUsesActualHandoffSchemaAndRepairsDetectedZipSelector(t *testing.T) {
	repo := t.TempDir()
	writeHandoffProviderFixtures(t, repo)
	if err := writeText(filepath.Join(repo, ".chatgpt-handoff", "profile.md"), "# profile\n"); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(repo, "artifacts", "reviews", "app-source-review-20260714-120000.zip")
	if err := writeText(artifact, "zip"); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(repo, ".chatgpt-handoff.json")
	config := map[string]any{
		"schema":      "rzn.chatgpt_handoff.config/v1",
		"project_id":  "project-123",
		"preserve_me": "yes",
		"zip":         map[string]any{"artifacts_dir": "artifacts", "pattern": "-codebase-", "make_target": "codebasezip"},
		"rzn":         map[string]any{"system": "chatgpt", "send_workflow": "send", "read_workflow": "read", "projects_workflow": "projects", "workflows_dir": filepath.Join(repo, "workflows")},
	}
	if err := writeHandoffConfig(configPath, config); err != nil {
		t.Fatal(err)
	}

	dry, err := runSetupDoctor(setupDoctorInput{RepoRoot: repo}, false)
	if err != nil {
		t.Fatal(err)
	}
	assertSetupFinding(t, dry, "handoff_zip_config_stale", false)

	first, err := runSetupDoctor(setupDoctorInput{RepoRoot: repo}, true)
	if err != nil {
		t.Fatal(err)
	}
	assertSetupFinding(t, first, "handoff_zip_config_stale", true)
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var repaired map[string]any
	if err := json.Unmarshal(raw, &repaired); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "yes", mapString(repaired, "preserve_me"), "unknown handoff config preserved")
	zipConfig := repaired["zip"].(map[string]any)
	assertEqual(t, "artifacts/reviews", mapString(zipConfig, "artifacts_dir"), "detected artifact directory")
	assertEqual(t, "-source-review-", mapString(zipConfig, "pattern"), "detected artifact pattern")

	second, err := runSetupDoctor(setupDoctorInput{RepoRoot: repo}, true)
	if err != nil {
		t.Fatal(err)
	}
	if findingByCode(second, "handoff_zip_config_stale") != nil {
		t.Fatalf("second repair rewrote zip selector: %#v", second.Findings)
	}
}

func TestSetupDoctorDiagnosesActualProviderContractDrift(t *testing.T) {
	repo := t.TempDir()
	if err := writeText(filepath.Join(repo, ".chatgpt-handoff", "profile.md"), "# profile\n"); err != nil {
		t.Fatal(err)
	}
	config := map[string]any{
		"schema":     "rzn.chatgpt_handoff.config/v1",
		"project_id": "project-123",
		"zip":        map[string]any{"make_target": "codebasezip"},
	}
	if err := writeText(filepath.Join(repo, "Makefile"), "codebasezip:\n\t@true\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeHandoffConfig(filepath.Join(repo, ".chatgpt-handoff.json"), config); err != nil {
		t.Fatal(err)
	}
	inspect := func(system, workflow string) ([]byte, error) {
		return []byte(`{"reference":"chatgpt/` + workflow + `","id":"chatgpt/` + workflow + `","system":"chatgpt","capability":"chatgpt.` + workflow + `","inputs":[{"name":"project_id"}]}`), nil
	}
	report, err := runSetupDoctor(setupDoctorInput{RepoRoot: repo, WorkflowInspect: inspect}, false)
	if err != nil {
		t.Fatal(err)
	}
	assertSetupFinding(t, report, "handoff_workflow_send_stale", false)
	assertSetupFinding(t, report, "handoff_workflow_read_stale", false)
	if finding := findingByCode(report, "handoff_workflow_projects_stale"); finding != nil {
		t.Fatalf("projects contract only requires the correct installed identity: %#v", finding)
	}
}

func TestSetupDoctorAcceptsCanonicalReadAliasWithResolvedCapability(t *testing.T) {
	repo := t.TempDir()
	if err := writeText(filepath.Join(repo, ".chatgpt-handoff", "profile.md"), "# profile\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(repo, "Makefile"), "codebasezip:\n\t@true\n"); err != nil {
		t.Fatal(err)
	}
	config := map[string]any{
		"schema":     "rzn.chatgpt_handoff.config/v1",
		"project_id": "project-123",
		"rzn": map[string]any{
			"system": "chatgpt", "send_workflow": "send", "read_workflow": "read", "projects_workflow": "projects",
			"include_model_version_param": true, "include_require_exact_model_param": true,
		},
		"zip": map[string]any{"make_target": "codebasezip", "artifacts_dir": "artifacts", "pattern": "-codebase-"},
	}
	if err := writeHandoffConfig(filepath.Join(repo, ".chatgpt-handoff.json"), config); err != nil {
		t.Fatal(err)
	}
	contracts := map[string]map[string]any{
		"send":     workflowInspectFixture("chatgpt/send", "chatgpt/send", "chatgpt.send", []string{"project_id", "message_text", "model_slug", "model_effort", "model_version", "require_exact_model"}),
		"read":     workflowInspectFixture("chatgpt/read", "chatgpt/read_current", "chatgpt.read", []string{"chat_id", "download_attachments", "attachments_scroll", "mode"}),
		"projects": workflowInspectFixture("chatgpt/projects", "chatgpt/projects", "chatgpt.projects", []string{"project_id", "mode"}),
	}
	inspect := func(_ string, workflow string) ([]byte, error) { return json.Marshal(contracts[workflow]) }
	report, err := runSetupDoctor(setupDoctorInput{RepoRoot: repo, WorkflowInspect: inspect}, false)
	if err != nil {
		t.Fatal(err)
	}
	assertNoHandoffWorkflowFinding(t, report)

	for _, forgedID := range []string{"chatgpt/read_obsolete", "chatgpt/read_evil", "chatgpt/projects"} {
		contracts["read"] = workflowInspectFixture("chatgpt/read", forgedID, "chatgpt.read", []string{"chat_id", "download_attachments", "attachments_scroll"})
		report, err = runSetupDoctor(setupDoctorInput{RepoRoot: repo, WorkflowInspect: inspect}, false)
		if err != nil {
			t.Fatal(err)
		}
		assertSetupFinding(t, report, "handoff_workflow_read_stale", false)
	}
}

func TestLiveRZNWorkflowContractsOffline(t *testing.T) {
	if os.Getenv("TUSKER_LIVE_RZN_WORKFLOW_TEST") != "1" {
		t.Skip("set TUSKER_LIVE_RZN_WORKFLOW_TEST=1 to inspect the installed local provider catalog")
	}
	config := map[string]any{
		"rzn": map[string]any{
			"system": "chatgpt", "send_workflow": "send", "read_workflow": "read", "projects_workflow": "projects",
			"include_model_version_param": true, "include_require_exact_model_param": true,
		},
	}
	findings := diagnoseHandoffWorkflows(config, filepath.Join(t.TempDir(), ".chatgpt-handoff.json"), inspectRZNWorkflow)
	if len(findings) != 0 {
		t.Fatalf("installed local rzn workflow contracts drifted: %#v", findings)
	}
}

func TestSetupDoctorReadOnlyCommandDoesNotChangeRuntimeOrRepoBytes(t *testing.T) {
	stateRoot := t.TempDir()
	repo := t.TempDir()
	canonical := t.TempDir()
	writeCanonicalTuskerSkillFixture(t, canonical)
	installCanonicalSkillLinks(t, repo, filepath.Join(canonical, "skills", "tusker"))
	writeValidHandoffFixture(t, repo)
	vault := filepath.Join(repo, ".tusker")
	if err := writeText(workflowPath(vault), defaultWorkflowMarkdown()); err != nil {
		t.Fatal(err)
	}
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertProject(RegisteredProject{ProjectID: "app", ProjectKey: "app", Name: "app", RepoRoot: repo, VaultRoot: vault, WorkflowPath: workflowPath(vault), Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	beforeState := snapshotTree(t, stateRoot)
	beforeRepo := snapshotTree(t, repo)
	args := Args{"repo": repo, "state-root": stateRoot, "source": canonical, "json": "true"}
	first := captureStdout(t, func() {
		if err := setupDoctorCmd(args, false); err != nil {
			t.Fatal(err)
		}
	})
	second := captureStdout(t, func() {
		if err := setupDoctorCmd(args, false); err != nil {
			t.Fatal(err)
		}
	})
	if first != second {
		t.Fatalf("doctor output changed between identical runs\nfirst=%s\nsecond=%s", first, second)
	}
	assertSnapshotEqual(t, beforeState, snapshotTree(t, stateRoot), "runtime state")
	assertSnapshotEqual(t, beforeRepo, snapshotTree(t, repo), "repository")
}

func TestRuntimeStoreReadOnlyRejectsWrites(t *testing.T) {
	stateRoot := t.TempDir()
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertProject(RegisteredProject{ProjectID: "before", RepoRoot: "/tmp/before", VaultRoot: "/tmp/before/.tusker", WorkflowPath: "/tmp/before/.tusker/WORKFLOW.md", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	before := snapshotTree(t, stateRoot)
	readOnly, err := OpenRuntimeStoreReadOnly(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := readOnly.UpsertProject(RegisteredProject{ProjectID: "forbidden", RepoRoot: "/tmp/forbidden", VaultRoot: "/tmp/forbidden/.tusker", WorkflowPath: "/tmp/forbidden/.tusker/WORKFLOW.md", Enabled: true}); err == nil {
		t.Fatal("read-only runtime store accepted a write")
	}
	if err := readOnly.Close(); err != nil {
		t.Fatal(err)
	}
	assertSnapshotEqual(t, before, snapshotTree(t, stateRoot), "read-only runtime database")
}

func TestSetupRepairConvergesForWrongRootValidStaleSymlinkAndZip(t *testing.T) {
	stateRoot := t.TempDir()
	repo := t.TempDir()
	canonical := t.TempDir()
	writeCanonicalTuskerSkillFixture(t, canonical)
	installCanonicalSkillLinks(t, repo, filepath.Join(canonical, "skills", "tusker"))
	writeValidHandoffFixture(t, repo)
	legacy := filepath.Join(repo, "tusker")
	if err := writeText(workflowPath(legacy), defaultWorkflowMarkdown()); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("tusker", filepath.Join(repo, ".tusker")); err != nil {
		t.Fatal(err)
	}
	config, _, err := readHandoffConfig(filepath.Join(repo, ".chatgpt-handoff.json"))
	if err != nil {
		t.Fatal(err)
	}
	config["zip"] = map[string]any{"artifacts_dir": "artifacts", "pattern": "-codebase-"}
	if err := writeHandoffConfig(filepath.Join(repo, ".chatgpt-handoff.json"), config); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(repo, "artifacts", "review", "app-source-review-20260714-120000.zip"), "zip"); err != nil {
		t.Fatal(err)
	}
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertProject(RegisteredProject{ProjectID: "app", ProjectKey: "app", Name: "app", RepoRoot: repo, VaultRoot: legacy, WorkflowPath: workflowPath(legacy), Enabled: true}); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	args := Args{"repo": repo, "state-root": stateRoot, "source": canonical, "json": "true"}
	first := captureStdout(t, func() { mustSetupCommand(t, args, true) })
	if !bytes.Contains([]byte(first), []byte(`"changed":true`)) {
		t.Fatalf("first repair did not report deterministic changes: %s", first)
	}
	second := captureStdout(t, func() { mustSetupCommand(t, args, true) })
	afterSecondState, afterSecondRepo := snapshotTree(t, stateRoot), snapshotTree(t, repo)
	third := captureStdout(t, func() { mustSetupCommand(t, args, true) })
	if second != third {
		t.Fatalf("second and third repair output differ\nsecond=%s\nthird=%s", second, third)
	}
	// SQLite may advance its WAL/checkpoint page metadata when a read-only
	// doctor opens the database. Compare the logical control-plane rows
	// separately, while keeping byte snapshots for all other state files.
	delete(afterSecondState, "daemon.db")
	currentState := snapshotTree(t, stateRoot)
	delete(currentState, "daemon.db")
	assertSnapshotEqual(t, afterSecondState, currentState, "runtime state after convergence")
	secondStore, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	secondProjects, err := secondStore.ListProjects()
	if err != nil {
		_ = secondStore.Close()
		t.Fatal(err)
	}
	secondRuns, err := secondStore.ListRuns()
	if err != nil {
		_ = secondStore.Close()
		t.Fatal(err)
	}
	_ = secondStore.Close()
	thirdStore, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	thirdProjects, err := thirdStore.ListProjects()
	if err != nil {
		_ = thirdStore.Close()
		t.Fatal(err)
	}
	thirdRuns, err := thirdStore.ListRuns()
	if err != nil {
		_ = thirdStore.Close()
		t.Fatal(err)
	}
	_ = thirdStore.Close()
	if !reflect.DeepEqual(secondProjects, thirdProjects) || !reflect.DeepEqual(secondRuns, thirdRuns) {
		t.Fatalf("logical runtime state changed across converged repair: projects=%v/%v runs=%v/%v", secondProjects, thirdProjects, secondRuns, thirdRuns)
	}
	assertSnapshotEqual(t, afterSecondRepo, snapshotTree(t, repo), "repository after convergence")
	if bytes.Contains([]byte(second), []byte(`"changed":true`)) {
		t.Fatalf("converged repair still reports a change: %s", second)
	}
	var report setupDoctorReport
	if err := json.Unmarshal([]byte(second), &report); err != nil {
		t.Fatal(err)
	}
	assertSetupFinding(t, report, "stale_vault_symlink", false)
	if findingByCode(report, "stale_vault_root") != nil || findingByCode(report, "handoff_zip_config_stale") != nil {
		t.Fatalf("repairable drift survived convergence: %#v", report.Findings)
	}
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

func writeCanonicalTuskerSkillFixture(t *testing.T, root string) {
	t.Helper()
	writeCanonicalTuskerSkillPackage(t, filepath.Join(root, "skills", "tusker"))
}

func writeCanonicalTuskerSkillPackage(t *testing.T, root string) {
	t.Helper()
	body := `---
name: tusker
description: Operate Tusker.
---
# Tusker
`
	if err := writeText(filepath.Join(root, "SKILL.md"), body); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"TRACK.md", "KNOWLEDGE.md", "RUN.md", "OPERATE.md"} {
		if err := writeText(filepath.Join(root, "references", name), "# "+name+"\n"); err != nil {
			t.Fatal(err)
		}
	}
	contract, err := os.ReadFile(filepath.Join("..", "..", "skills", "tusker", "assets", "factory-intake-contract.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(root, "assets", "factory-intake-contract.yaml"), string(contract)); err != nil {
		t.Fatal(err)
	}
	compatibility, err := os.ReadFile(filepath.Join("..", "..", "skills", "tusker", "assets", skillCompatibilityFilename))
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(root, "assets", skillCompatibilityFilename), string(compatibility)); err != nil {
		t.Fatal(err)
	}
}

func installCanonicalSkillLinks(t *testing.T, repo, source string) {
	t.Helper()
	for _, destination := range []string{filepath.Join(repo, ".agents", "skills", "tusker"), filepath.Join(repo, ".claude", "skills", "tusker")} {
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(source, destination); err != nil {
			t.Fatal(err)
		}
	}
}

func writeHandoffProviderFixtures(t *testing.T, repo string) {
	t.Helper()
	contracts := map[string][]string{
		"send":     {"project_id", "message_text", "model_slug", "model_effort", "model_version", "require_exact_model"},
		"read":     {"chat_id", "download_attachments", "attachments_scroll"},
		"projects": {},
	}
	for workflow, names := range contracts {
		properties := map[string]any{}
		for _, name := range names {
			properties[name] = map[string]any{"type": "string"}
		}
		payload := map[string]any{"schema_version": "rzn.workflow_manifest", "id": "chatgpt/" + workflow, "version": "1.0.0", "system": "chatgpt", "capability": "chatgpt." + workflow, "params": map[string]any{"properties": properties}}
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		if err := writeText(filepath.Join(repo, "workflows", "chatgpt", "chatgpt_"+workflow+".json"), string(raw)); err != nil {
			t.Fatal(err)
		}
	}
}

func workflowInspectFixture(reference, id, capability string, names []string) map[string]any {
	inputs := make([]any, 0, len(names))
	for _, name := range names {
		inputs = append(inputs, map[string]any{"name": name, "kind": "string", "required": false})
	}
	return map[string]any{
		"reference":  reference,
		"id":         id,
		"system":     "chatgpt",
		"capability": capability,
		"inputs":     inputs,
	}
}

func assertNoHandoffWorkflowFinding(t *testing.T, report setupDoctorReport) {
	t.Helper()
	for _, finding := range report.Findings {
		if strings.HasPrefix(finding.Code, "handoff_workflow_") {
			t.Fatalf("unexpected handoff workflow finding: %#v", finding)
		}
	}
}

func writeValidHandoffFixture(t *testing.T, repo string) {
	t.Helper()
	writeHandoffProviderFixtures(t, repo)
	if err := writeText(filepath.Join(repo, ".chatgpt-handoff", "profile.md"), "# project profile\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(repo, "Makefile"), "codebasezip:\n\t@true\n"); err != nil {
		t.Fatal(err)
	}
	config := map[string]any{
		"schema":     "rzn.chatgpt_handoff.config/v1",
		"project_id": "project-123",
		"model":      map[string]any{"slug": "GPT-5.6 Sol", "version": "5.6", "effort": "Pro", "fail_on_downgrade": true},
		"rzn": map[string]any{
			"system": "chatgpt", "send_workflow": "send", "read_workflow": "read", "read_fallback_workflow": "read_root", "projects_workflow": "projects",
			"workflows_dir": filepath.Join(repo, "workflows"), "include_model_version_param": true, "include_require_exact_model_param": true,
		},
		"zip": map[string]any{"make_target": "codebasezip", "artifacts_dir": "artifacts", "pattern": "-codebase-", "build_by_default": true},
	}
	if err := writeHandoffConfig(filepath.Join(repo, ".chatgpt-handoff.json"), config); err != nil {
		t.Fatal(err)
	}
}

func mustSetupCommand(t *testing.T, args Args, apply bool) {
	t.Helper()
	if err := setupDoctorCmd(args, apply); err != nil {
		t.Fatal(err)
	}
}

func snapshotTree(t *testing.T, root string) map[string][]byte {
	t.Helper()
	snapshot := map[string][]byte{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			snapshot[filepath.ToSlash(rel)] = []byte("symlink:" + target)
			return nil
		}
		if entry.IsDir() || strings.HasSuffix(path, "-wal") || strings.HasSuffix(path, "-shm") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		// Opening a WAL-mode SQLite database can advance its file-change
		// counter while leaving all logical rows untouched. Exclude that
		// volatile header field from byte-for-byte mutation snapshots.
		if filepath.Base(path) == "daemon.db" && len(raw) >= 28 {
			for i := 24; i < 28; i++ {
				raw[i] = 0
			}
		}
		snapshot[filepath.ToSlash(rel)] = raw
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func assertSnapshotEqual(t *testing.T, want, got map[string][]byte, label string) {
	t.Helper()
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("%s changed\nwant=%v\ngot=%v", label, snapshotKeys(want), snapshotKeys(got))
	}
}

func snapshotKeys(snapshot map[string][]byte) []string {
	keys := make([]string, 0, len(snapshot))
	for key := range snapshot {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
