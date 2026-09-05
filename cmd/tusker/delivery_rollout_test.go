package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDeliveryRolloutMigratesLegacyRunnerToACPAndKeepsExplicitDirectProfile(t *testing.T) {
	data := map[string]any{
		"agents": map[string]any{"default": "codex_app_server", "enabled": []any{"codex_app_server"}},
		"automation": map[string]any{"profiles": map[string]any{
			"safe":      map[string]any{"harness": "codex_app_server"},
			"emergency": map[string]any{"harness": "codex_exec", "permission_preset": "danger-full-access"},
		}},
	}
	if !migrateKnownRunnerPolicy(data) {
		t.Fatal("legacy runner policy was not marked changed")
	}
	if got := data["agents"].(map[string]any)["default"]; got != string(RunnerCodexACP) {
		t.Fatalf("default runner=%v, want %s", got, RunnerCodexACP)
	}
	profiles := data["automation"].(map[string]any)["profiles"].(map[string]any)
	if got := profiles["safe"].(map[string]any)["harness"]; got != string(RunnerCodexACP) {
		t.Fatalf("safe profile harness=%v, want %s", got, RunnerCodexACP)
	}
	if got := profiles["emergency"].(map[string]any)["harness"]; got != string(RunnerCodexExec) {
		t.Fatalf("emergency profile harness=%v, want %s", got, RunnerCodexExec)
	}
}

func TestDeliveryRolloutDoctor(t *testing.T) {
	fixture := newDeliveryRolloutFixture(t)
	fixture.addProject("stale", true)
	before := snapshotDeliveryRolloutPaths(t, fixture.roots)
	projectsBefore, _ := fixture.store.ListProjects()
	report, err := runDeliveryRollout(fixture.input(), false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Schema != deliveryRolloutSchema || !report.DryRun || len(report.Projects) != 3 || report.Projects[0].ProjectID != "healthy" || report.Projects[1].ProjectID != "missing" || report.Projects[2].ProjectID != "stale" {
		t.Fatalf("unexpected doctor matrix: %#v", report)
	}
	if report.Projects[1].Status != "quarantined" || report.Projects[1].Action == "" || report.Projects[2].Status != "needs_repair" {
		t.Fatalf("doctor classification is not actionable: %#v", report.Projects)
	}
	if got := snapshotDeliveryRolloutPaths(t, fixture.roots); !reflect.DeepEqual(before, got) {
		t.Fatal("doctor mutated fixture repositories")
	}
	projectsAfter, _ := fixture.store.ListProjects()
	if !reflect.DeepEqual(projectsBefore, projectsAfter) {
		t.Fatal("doctor mutated project registry")
	}
}

func TestDeliveryRolloutRepair(t *testing.T) {
	fixture := newDeliveryRolloutFixture(t)
	repo := fixture.addProject("stale", true)
	wfPath := workflowPath(filepath.Join(repo, ".tusker"))
	wf := mustReadIndexTest(t, wfPath)
	wf = strings.Replace(wf, `default: codex_exec`, `default: codex_app_server`, 1)
	wf = strings.Replace(wf, `approval_policy: on-request`, `approval_policy: untrusted`, 1)
	wf = strings.Replace(wf, `auto_close_risks:\n    - low\n    - medium\n    - high\n    - critical`, `auto_close_risks:\n    - low\n  human_required_risks:\n    - high`, 1)
	if err := writeText(wfPath, wf); err != nil {
		t.Fatal(err)
	}
	config := "schema: tusker.config/v1\nproject_id: stale\nautomation:\n  default_runner: codex_app_server\n  runners:\n    codex:\n      kind: codex_app_server\n      approval_policy: on-request\nclose_policy:\n  high:\n    required_acceptor: human\n    required_gates: [signoff]\nunknown_top: keep\n"
	if err := writeText(managedTuskerConfigPath(filepath.Join(repo, defaultRepoVaultDir)), config); err != nil {
		t.Fatal(err)
	}
	first, err := runDeliveryRolloutScoped(fixture.input(), deliveryRolloutRepairAutomation, true)
	if err != nil {
		t.Fatal(err)
	}
	project := rolloutProjectByID(t, first, "stale")
	if project.Status != "repaired" || !deliveryRolloutChanged(project.Findings) {
		t.Fatalf("repair did not report changes: %#v", project)
	}
	for _, text := range []string{mustReadIndexTest(t, wfPath), mustReadIndexTest(t, managedTuskerConfigPath(filepath.Join(repo, defaultRepoVaultDir)))} {
		if strings.Contains(text, "codex_app_server") || strings.Contains(text, "required_acceptor: human") || strings.Contains(text, "approval_policy: on-request") || strings.Contains(text, "approval_policy: untrusted") {
			t.Fatalf("legacy unattended policy survived repair:\n%s", text)
		}
	}
	afterFirst := snapshotDeliveryRolloutPaths(t, []string{repo})
	second, err := runDeliveryRolloutScoped(fixture.input(), deliveryRolloutRepairAutomation, true)
	if err != nil {
		t.Fatal(err)
	}
	if deliveryRolloutChanged(rolloutProjectByID(t, second, "stale").Findings) {
		t.Fatal("second repair was not idempotent")
	}
	if !reflect.DeepEqual(afterFirst, snapshotDeliveryRolloutPaths(t, []string{repo})) {
		t.Fatal("second repair changed repository bytes")
	}
}

func TestDeliveryRolloutPreservation(t *testing.T) {
	t.Parallel()
	fixture := newDeliveryRolloutFixture(t)
	repo := fixture.addProject("preserve", false)
	sentinels := map[string]string{
		filepath.Join(repo, ".tusker", "knowledge", "domain.md"):         "knowledge\n",
		filepath.Join(repo, ".tusker", "work", "tasks", "APP-T-0001.md"): "task contract\n",
		filepath.Join(repo, ".env"):                                      "SECRET=untouched\n",
		filepath.Join(repo, "AGENTS.md"):                                 "repo instructions\n",
	}
	for path, body := range sentinels {
		if err := writeText(path, body); err != nil {
			t.Fatal(err)
		}
	}
	wfPath := workflowPath(filepath.Join(repo, ".tusker"))
	wf := mustReadIndexTest(t, wfPath)
	wf = strings.Replace(wf, "extensions:\n", "custom_project_key: preserve-me\nextensions:\n", 1) + "\n## Project-only body\n\nNever rewrite me.\n"
	if err := writeText(wfPath, wf); err != nil {
		t.Fatal(err)
	}
	before := snapshotDeliveryRolloutPaths(t, []string{repo})
	if _, err := runDeliveryRollout(fixture.input(), true); err != nil {
		t.Fatal(err)
	}
	after := snapshotDeliveryRolloutPaths(t, []string{repo})
	for path, body := range before {
		if strings.Contains(path, "/.agents/skills/") || strings.Contains(path, "/.claude/skills/") || path == wfPath {
			continue
		}
		if body != after[path] {
			t.Fatalf("rollout rewrote preserved path %s", path)
		}
	}
	text := mustReadIndexTest(t, wfPath)
	if !strings.Contains(text, "custom_project_key:") || !strings.Contains(text, "preserve-me") || !strings.Contains(text, "Never rewrite me.") {
		t.Fatalf("unknown workflow content was not preserved:\n%s", text)
	}
	projects, _ := fixture.store.ListProjects()
	for _, project := range projects {
		if project.ProjectID == "preserve" && project.Enabled {
			t.Fatal("rollout enabled a disabled project")
		}
	}
}

func TestDeliveryRolloutQuarantine(t *testing.T) {
	t.Parallel()
	fixture := newDeliveryRolloutFixture(t)
	good := fixture.addProject("repairable", true)
	bad := fixture.addProject("old-schema", true)
	path := workflowPath(filepath.Join(bad, ".tusker"))
	if err := writeText(path, strings.Replace(mustReadIndexTest(t, path), "tracker_schema_version: 7", "tracker_schema_version: 6", 1)); err != nil {
		t.Fatal(err)
	}
	opaque := fixture.addProject("opaque-runner", true)
	opaqueConfig := "schema: tusker.config/v1\nproject_id: opaque-runner\nautomation:\n  runners:\n    codex:\n      kind: codex_exec\n      command: custom-human-approval-wrapper\n      approval_policy: on-request\n"
	if err := writeText(managedTuskerConfigPath(filepath.Join(opaque, defaultRepoVaultDir)), opaqueConfig); err != nil {
		t.Fatal(err)
	}
	report, err := runDeliveryRollout(fixture.input(), true)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"missing", "old-schema"} {
		project := rolloutProjectByID(t, report, id)
		if project.Status != "quarantined" || project.Action == "" {
			t.Fatalf("bad quarantine for %s: %#v", id, project)
		}
	}
	opaqueProject := rolloutProjectByID(t, report, "opaque-runner")
	if opaqueProject.Status != "needs_repair" || opaqueProject.Readiness.Dimensions.Contract.State != ReadinessStateReady || opaqueProject.Readiness.Dimensions.Interactive.State != ReadinessStateReady || opaqueProject.Readiness.Dimensions.Automation.State != ReadinessStateBlocked {
		t.Fatalf("opaque unattended runner should stay local to automation: %#v", opaqueProject)
	}
	if !dirExists(good) {
		t.Fatal("compatible sibling was damaged")
	}
	projects, _ := fixture.store.ListProjects()
	for _, project := range projects {
		if project.ProjectID == "missing" || project.ProjectID == "old-schema" {
			if project.Health != projectHealthError || !strings.HasPrefix(project.LastError, "delivery rollout quarantine:") {
				t.Fatalf("quarantine not durable: %#v", project)
			}
		}
		if project.ProjectID == "opaque-runner" && project.Health == projectHealthError {
			t.Fatalf("automation-only failure quarantined core project state: %#v", project)
		}
	}
	if err := writeText(path, strings.Replace(mustReadIndexTest(t, path), "tracker_schema_version: 6", "tracker_schema_version: 7", 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := runDeliveryRollout(fixture.input(), true); err != nil {
		t.Fatal(err)
	}
	projects, _ = fixture.store.ListProjects()
	for _, project := range projects {
		if project.ProjectID == "old-schema" && (project.Health != projectHealthHealthy || project.LastError != "") {
			t.Fatalf("restored project retained rollout quarantine: %#v", project)
		}
	}
}

func TestScopedFleetRepair(t *testing.T) {
	t.Parallel()
	fixture := newDeliveryRolloutFixture(t)
	repo := fixture.addProject("optional", true)
	stabilizeFleetReadinessFixture(t, fixture)
	if err := writeText(filepath.Join(repo, ".chatgpt-handoff.json"), "{not json"); err != nil {
		t.Fatal(err)
	}
	workflowPath := workflowPath(filepath.Join(repo, ".tusker"))
	workflowBefore := mustReadIndexTest(t, workflowPath)
	serviceCalls := 0
	input := fixture.input()
	input.ServiceCheck = func(apply bool) ([]setupFinding, error) {
		if apply {
			serviceCalls++
		}
		return []setupFinding{{Code: "daemon_service_stale", Status: "error", Message: "stale fixture service", Action: "separate explicit service operation", Repairable: true}}, nil
	}

	core, err := runDeliveryRollout(input, true)
	if err != nil {
		t.Fatal(err)
	}
	project := rolloutProjectByID(t, core, "optional")
	if project.Status != "healthy" || project.Readiness.Dimensions.Contract.State != ReadinessStateReady || project.Readiness.Dimensions.Interactive.State != ReadinessStateReady || project.Readiness.Dimensions.OptionalIntegration.State != ReadinessStateBlocked {
		t.Fatalf("optional integration drift escaped its dimension: %#v", project)
	}
	if core.RepairScope != deliveryRolloutRepairCore || core.ServiceReadiness.Dimensions.Runtime.State != ReadinessStateUnavailable || len(core.ServiceReadiness.Blockers) != 1 {
		t.Fatalf("fleet service readiness was not projected once and independently: %#v", core)
	}
	if mustReadIndexTest(t, workflowPath) != workflowBefore || serviceCalls != 0 {
		t.Fatal("default core repair changed automation or service state")
	}
	projects, _ := fixture.store.ListProjects()
	for _, registered := range projects {
		if registered.ProjectID == "optional" && registered.Health == projectHealthError {
			t.Fatalf("optional integration drift quarantined registration: %#v", registered)
		}
	}
	if _, err := runDeliveryRolloutScoped(input, deliveryRolloutRepairIntegrations, true); err != nil {
		t.Fatal(err)
	}
	if serviceCalls != 0 {
		t.Fatal("integration repair touched managed service")
	}
}

func TestFleetHealthDimensions(t *testing.T) {
	t.Parallel()
	fixture := newDeliveryRolloutFixture(t)
	disabled := fixture.addProject("disabled", false)
	degraded := fixture.addProject("degraded", true)
	stale := fixture.addProject("stale-runtime", true)
	stabilizeFleetReadinessFixture(t, fixture)
	projects, err := fixture.store.ListProjects()
	if err != nil {
		t.Fatal(err)
	}
	for _, project := range projects {
		switch project.ProjectID {
		case "degraded":
			project.Health, project.LastError = projectHealthDegraded, "fixture runtime degraded"
		case "stale-runtime":
			project.LastPollAt = time.Now().UTC().Add(-2 * daemonHeartbeatDeadThreshold).Format(time.RFC3339)
		}
		if err := fixture.store.UpsertProject(project); err != nil {
			t.Fatal(err)
		}
	}
	report, err := runDeliveryRollout(fixture.input(), false)
	if err != nil {
		t.Fatal(err)
	}
	if got := rolloutProjectByID(t, report, "healthy").Readiness.Dimensions.Runtime.State; got != ReadinessStateReady {
		t.Fatalf("healthy runtime = %q", got)
	}
	if project := rolloutProjectByID(t, report, "disabled"); project.Status != "disabled" || project.Readiness.Dimensions.Runtime.State != ReadinessStateNotApplicable {
		t.Fatalf("disabled runtime should be inapplicable: %#v", project)
	}
	for _, id := range []string{"degraded", "stale-runtime"} {
		project := rolloutProjectByID(t, report, id)
		if project.Readiness.Dimensions.Runtime.State != ReadinessStateUnavailable || len(project.Readiness.ReadinessBlockerIDs(ReadinessDimensionRuntime)) == 0 || project.Status == "quarantined" {
			t.Fatalf("runtime failure escaped its project dimension for %s: %#v", id, project)
		}
	}
	if !dirExists(disabled) || !dirExists(degraded) || !dirExists(stale) {
		t.Fatal("runtime diagnostics damaged a sibling fixture")
	}
}

func TestMixedFleetCoreRepairPreservesOtherScopes(t *testing.T) {
	t.Parallel()
	fixture := newDeliveryRolloutFixture(t)
	missingSkill := fixture.addProject("missing-skill", true)
	legacyRunner := fixture.addProject("legacy-runner", true)
	optionalProvider := fixture.addProject("optional-provider", true)
	disabled := fixture.addProject("disabled", false)
	stabilizeFleetReadinessFixture(t, fixture)

	for _, destination := range []string{
		filepath.Join(missingSkill, ".agents", "skills", "tusker"),
		filepath.Join(missingSkill, ".claude", "skills", "tusker"),
	} {
		if err := os.RemoveAll(destination); err != nil {
			t.Fatal(err)
		}
	}
	legacyConfig := "schema: tusker.config/v1\nproject_id: legacy-runner\nautomation:\n  runners:\n    codex:\n      kind: codex_exec\n      command: custom-human-approval-wrapper\n      approval_policy: on-request\n"
	legacyConfigPath := managedTuskerConfigPath(filepath.Join(legacyRunner, defaultRepoVaultDir))
	if err := writeText(legacyConfigPath, legacyConfig); err != nil {
		t.Fatal(err)
	}
	optionalConfigPath := filepath.Join(optionalProvider, ".chatgpt-handoff.json")
	if err := writeText(optionalConfigPath, "{not json"); err != nil {
		t.Fatal(err)
	}
	legacyBefore := mustReadIndexTest(t, legacyConfigPath)
	optionalBefore := mustReadIndexTest(t, optionalConfigPath)
	serviceCalls := 0
	input := fixture.input()
	input.ServiceCheck = func(apply bool) ([]setupFinding, error) {
		if apply {
			serviceCalls++
		}
		return []setupFinding{{Code: "daemon_service_stale", Status: "error", Message: "stale fixture service", Action: "repair service definition explicitly", Repairable: true}}, nil
	}

	report, err := runDeliveryRolloutScoped(input, deliveryRolloutRepairCore, true)
	if err != nil {
		t.Fatal(err)
	}
	if project := rolloutProjectByID(t, report, "missing"); project.Status != "quarantined" {
		t.Fatalf("missing vault did not remain quarantined: %#v", project)
	}
	if project := rolloutProjectByID(t, report, "missing-skill"); project.Status != "repaired" || !deliveryRolloutChanged(project.Findings) {
		t.Fatalf("missing skill did not converge through core repair: %#v", project)
	}
	for _, destination := range []string{
		filepath.Join(missingSkill, ".agents", "skills", "tusker"),
		filepath.Join(missingSkill, ".claude", "skills", "tusker"),
	} {
		if got := inspectSkillMaterialization(destination); got.Status != "current" {
			t.Fatalf("core repair left %s at %#v", destination, got)
		}
	}
	legacy := rolloutProjectByID(t, report, "legacy-runner")
	if legacy.Readiness.Dimensions.Automation.State != ReadinessStateBlocked || legacy.Status == "quarantined" || mustReadIndexTest(t, legacyConfigPath) != legacyBefore {
		t.Fatalf("core repair changed or misclassified legacy runner: %#v", legacy)
	}
	optional := rolloutProjectByID(t, report, "optional-provider")
	if optional.Readiness.Dimensions.OptionalIntegration.State != ReadinessStateBlocked || optional.Status == "quarantined" || mustReadIndexTest(t, optionalConfigPath) != optionalBefore {
		t.Fatalf("core repair changed or misclassified optional provider: %#v", optional)
	}
	disabledProject := rolloutProjectByID(t, report, "disabled")
	if disabledProject.Status != "disabled" || !dirExists(disabled) {
		t.Fatalf("core repair changed disabled project: %#v", disabledProject)
	}
	registered, err := fixture.store.ListProjects()
	if err != nil {
		t.Fatal(err)
	}
	for _, project := range registered {
		if project.ProjectID == "disabled" && project.Enabled {
			t.Fatal("core repair enabled disabled project")
		}
	}
	if serviceCalls != 0 || report.ServiceReadiness.Dimensions.Runtime.State != ReadinessStateUnavailable {
		t.Fatalf("core repair touched or hid stale service: calls=%d readiness=%#v", serviceCalls, report.ServiceReadiness)
	}
}

func stabilizeFleetReadinessFixture(t *testing.T, fixture *deliveryRolloutFixture) {
	t.Helper()
	for _, scope := range []deliveryRolloutRepairScope{deliveryRolloutRepairCore, deliveryRolloutRepairAutomation} {
		if _, err := runDeliveryRolloutScoped(fixture.input(), scope, true); err != nil {
			t.Fatalf("stabilize %s fixture: %v", scope, err)
		}
	}
}

type deliveryRolloutFixture struct {
	t      *testing.T
	store  *RuntimeStore
	source string
	roots  []string
}

func newDeliveryRolloutFixture(t *testing.T) *deliveryRolloutFixture {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	source := t.TempDir()
	writeCanonicalTuskerSkillFixture(t, source)
	f := &deliveryRolloutFixture{t: t, store: store, source: source, roots: []string{}}
	healthy := f.addProject("healthy", true)
	healthyWorkflow := mustReadIndexTest(t, workflowPath(filepath.Join(healthy, ".tusker")))
	healthyWorkflow = strings.Replace(healthyWorkflow, `approval_policy: on-request`, `approval_policy: never`, 1)
	if err := writeText(workflowPath(filepath.Join(healthy, ".tusker")), healthyWorkflow); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "gone")
	if err := store.UpsertProject(RegisteredProject{ProjectID: "missing", ProjectKey: "missing", Name: "missing", RepoRoot: missing, VaultRoot: filepath.Join(missing, ".tusker"), WorkflowPath: filepath.Join(missing, ".tusker", "WORKFLOW.md"), Enabled: true, Health: projectHealthHealthy}); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *deliveryRolloutFixture) addProject(id string, enabled bool) string {
	repo := filepath.Join(f.t.TempDir(), id)
	vault := filepath.Join(repo, ".tusker")
	if err := writeText(workflowPath(vault), defaultWorkflowMarkdown()); err != nil {
		f.t.Fatal(err)
	}
	if err := writeText(managedTuskerConfigPath(filepath.Join(repo, defaultRepoVaultDir)), "schema: tusker.config/v1\nproject_id: "+id+"\n"); err != nil {
		f.t.Fatal(err)
	}
	canonicalSkill := filepath.Join(f.source, "skills", "tusker")
	for _, install := range []string{filepath.Join(repo, ".agents", "skills", "tusker"), filepath.Join(repo, ".claude", "skills", "tusker")} {
		if err := ensureDir(filepath.Dir(install)); err != nil {
			f.t.Fatal(err)
		}
		if err := os.Symlink(canonicalSkill, install); err != nil {
			f.t.Fatal(err)
		}
	}
	project := RegisteredProject{ProjectID: id, ProjectKey: id, Name: id, RepoRoot: repo, VaultRoot: vault, WorkflowPath: workflowPath(vault), Enabled: enabled, Health: projectHealthHealthy}
	if !enabled {
		project.Health = projectHealthDisabled
	}
	if err := f.store.UpsertProject(project); err != nil {
		f.t.Fatal(err)
	}
	f.roots = append(f.roots, repo)
	return repo
}

func (f *deliveryRolloutFixture) input() deliveryRolloutInput {
	return deliveryRolloutInput{Store: f.store, Source: f.source, WorkflowInspect: func(_, _ string) ([]byte, error) { return nil, os.ErrNotExist }, ServiceCheck: func(apply bool) ([]setupFinding, error) {
		finding := setupFinding{Code: "daemon_service_stale", Status: "error", Message: "fixture managed service is stale", Action: "refresh fixture service", Repairable: true, Changed: apply}
		if !apply {
			return []setupFinding{finding}, nil
		}
		return []setupFinding{finding}, nil
	}}
}

func rolloutProjectByID(t *testing.T, report deliveryRolloutReport, id string) deliveryRolloutProject {
	t.Helper()
	for _, p := range report.Projects {
		if p.ProjectID == id {
			return p
		}
	}
	t.Fatalf("project %s missing", id)
	return deliveryRolloutProject{}
}

func snapshotDeliveryRolloutPaths(t *testing.T, roots []string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, root := range roots {
		_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() || strings.Contains(path, string(filepath.Separator)+".git"+string(filepath.Separator)) {
				return nil
			}
			raw, readErr := os.ReadFile(path)
			if readErr == nil {
				out[path] = string(raw)
			}
			return nil
		})
	}
	return out
}
