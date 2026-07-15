package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDeliveryRolloutDoctor(t *testing.T) {
	fixture := newDeliveryRolloutFixture(t)
	stale := fixture.addProject("stale", true)
	workflow := mustReadIndexTest(t, workflowPath(filepath.Join(stale, ".tusker")))
	workflow = strings.Replace(workflow, `approval_policy: on-request`, `approval_policy: never`, 1)
	if err := writeText(workflowPath(filepath.Join(stale, ".tusker")), workflow); err != nil {
		t.Fatal(err)
	}
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
	if err := writeText(filepath.Join(repo, "tusker.yaml"), config); err != nil {
		t.Fatal(err)
	}
	first, err := runDeliveryRollout(fixture.input(), true)
	if err != nil {
		t.Fatal(err)
	}
	project := rolloutProjectByID(t, first, "stale")
	if project.Status != "repaired" || !deliveryRolloutChanged(project.Findings) {
		t.Fatalf("repair did not report changes: %#v", project)
	}
	for _, text := range []string{mustReadIndexTest(t, wfPath), mustReadIndexTest(t, filepath.Join(repo, "tusker.yaml"))} {
		if strings.Contains(text, "codex_app_server") || strings.Contains(text, "required_acceptor: human") || strings.Contains(text, "approval_policy: on-request") || strings.Contains(text, "approval_policy: untrusted") {
			t.Fatalf("legacy unattended policy survived repair:\n%s", text)
		}
	}
	afterFirst := snapshotDeliveryRolloutPaths(t, []string{repo})
	second, err := runDeliveryRollout(fixture.input(), true)
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
	fixture := newDeliveryRolloutFixture(t)
	good := fixture.addProject("repairable", true)
	bad := fixture.addProject("old-schema", true)
	path := workflowPath(filepath.Join(bad, ".tusker"))
	if err := writeText(path, strings.Replace(mustReadIndexTest(t, path), "tracker_schema_version: 7", "tracker_schema_version: 6", 1)); err != nil {
		t.Fatal(err)
	}
	opaque := fixture.addProject("opaque-runner", true)
	opaqueConfig := "schema: tusker.config/v1\nproject_id: opaque-runner\nautomation:\n  runners:\n    codex:\n      kind: codex_exec\n      command: custom-human-approval-wrapper\n      approval_policy: on-request\n"
	if err := writeText(filepath.Join(opaque, "tusker.yaml"), opaqueConfig); err != nil {
		t.Fatal(err)
	}
	report, err := runDeliveryRollout(fixture.input(), true)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"missing", "old-schema", "opaque-runner"} {
		project := rolloutProjectByID(t, report, id)
		if project.Status != "quarantined" || project.Action == "" {
			t.Fatalf("bad quarantine for %s: %#v", id, project)
		}
	}
	if !dirExists(good) {
		t.Fatal("compatible sibling was damaged")
	}
	projects, _ := fixture.store.ListProjects()
	for _, project := range projects {
		if project.ProjectID == "missing" || project.ProjectID == "old-schema" || project.ProjectID == "opaque-runner" {
			if project.Health != projectHealthError || !strings.HasPrefix(project.LastError, "delivery rollout quarantine:") {
				t.Fatalf("quarantine not durable: %#v", project)
			}
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
	f.addProject("healthy", true)
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
	if err := writeText(filepath.Join(repo, "tusker.yaml"), "schema: tusker.config/v1\nproject_id: "+id+"\n"); err != nil {
		f.t.Fatal(err)
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
