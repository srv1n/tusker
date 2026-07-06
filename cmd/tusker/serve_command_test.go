package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func TestServeReadOnlyAndLocalhost(t *testing.T) {
	addr, err := serveBindAddr(Args{})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, defaultServeAddr, addr, "default serve addr")
	if _, err := serveBindAddr(Args{"addr": "0.0.0.0:7420"}); err == nil {
		t.Fatal("expected non-loopback bind to be rejected")
	}

	server := newServeFixture(t)
	req := httptest.NewRequest(http.MethodPost, "/api/tasks", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	assertEqual(t, http.StatusMethodNotAllowed, rec.Code, "mutating API route status")

	req = httptest.NewRequest(http.MethodGet, "/work/app", nil)
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	assertEqual(t, http.StatusOK, rec.Code, "SPA fallback status")
	if !strings.Contains(rec.Body.String(), "serve fixture") {
		t.Fatalf("expected embedded index fallback, got %q", rec.Body.String())
	}
}

func TestServeNeedsSignals(t *testing.T) {
	server := newServeFixture(t)
	var needs []map[string]any
	serveDecode(t, server, "/api/needs", &needs)

	byID := map[string]map[string]any{}
	for _, need := range needs {
		byID[need["id"].(string)] = need
	}
	for _, id := range []string{
		"need-review-APP-T-0001",
		"need-gate-APP-G-0001",
		"need-review-APP-T-0004",
		"need-failed-APP-T-0007",
	} {
		if _, ok := byID[id]; !ok {
			t.Fatalf("expected need %s in %#v", id, needs)
		}
	}
	if _, ok := byID["need-review-APP-T-0002"]; ok {
		t.Fatal("low-risk review task must not enter needs")
	}
	if _, ok := byID["need-failed-APP-T-0008"]; ok {
		t.Fatal("retry-queued failure must not enter needs")
	}
	assertEqual(t, "p0", byID["need-review-APP-T-0001"]["priority"], "critical review priority")
	var criticalTask serveTaskDetail
	serveDecode(t, server, "/api/tasks/APP-T-0001", &criticalTask)
	assertEqual(t, "critical", criticalTask.Risk, "critical risk passthrough")
	assertEqual(t, "provision", byID["need-gate-APP-G-0001"]["kind"], "gate need kind")
	assertEqual(t, float64(2), byID["need-review-APP-T-0004"]["reworkCount"], "rework bounce count")
}

func TestServeNeedsEmptyReturnsArray(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	req := httptest.NewRequest(http.MethodGet, "/api/needs", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/needs returned %d: %s", rec.Code, rec.Body.String())
	}
	if body := strings.TrimSpace(rec.Body.String()); body != "[]" {
		t.Fatalf("expected empty needs to marshal as [], got %q", body)
	}
	var needs []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &needs); err != nil {
		t.Fatalf("decode /api/needs: %v\n%s", err, rec.Body.String())
	}
	if needs == nil || len(needs) != 0 {
		t.Fatalf("expected decoded empty needs slice, got %#v", needs)
	}
}

func TestServeFieldsRosterAndEpics(t *testing.T) {
	server := newServeFixture(t)

	var task serveTaskDetail
	serveDecode(t, server, "/api/tasks/APP-T-0003", &task)
	assertEqual(t, "app", task.ProjectID, "task project id")
	if len(task.Gates) != 1 {
		t.Fatalf("expected one gate, got %#v", task.Gates)
	}
	assertEqual(t, false, task.Gates[0].Satisfied, "gate satisfied flag")
	assertEqual(t, "provision", task.Gates[0].Kind, "gate kind payload")
	assertEqual(t, "Provision test credentials.", task.Gates[0].Ask, "gate ask")

	var runs []map[string]any
	serveDecode(t, server, "/api/runs", &runs)
	var failed map[string]any
	for _, run := range runs {
		if run["taskId"] == "APP-T-0007" {
			failed = run
			break
		}
	}
	if failed == nil {
		t.Fatal("expected APP-T-0007 run")
	}
	assertEqual(t, "boom", failed["error"], "terminal error text")
	if failed["terminal"] != nil || failed["lastHeartbeatAt"] != nil {
		t.Fatalf("daemon-unlanded fields must be explicit nulls: %#v", failed)
	}
	var parked map[string]any
	for _, run := range runs {
		if run["taskId"] == "APP-T-0009" {
			parked = run
			break
		}
	}
	if parked == nil {
		t.Fatal("expected APP-T-0009 run")
	}
	assertEqual(t, "parked-no-progress", parked["outcome"], "parked run outcome")
	assertEqual(t, "parked_no_progress", parked["leaseStateRaw"], "parked raw lease")

	var epics []serveEpicSummary
	serveDecode(t, server, "/api/epics", &epics)
	seenTRC := false
	for _, epic := range epics {
		if epic.ID == "TRC" {
			seenTRC = true
			if epic.Counts["ready"] == 0 {
				t.Fatalf("expected TRC ready count from vault tasks: %#v", epic)
			}
		}
	}
	if !seenTRC {
		t.Fatalf("expected TRC epic derived from vault, got %#v", epics)
	}

	var roster map[string]any
	serveDecode(t, server, "/api/roster", &roster)
	rows := roster["rows"].([]any)
	if len(rows) == 0 {
		t.Fatal("expected roster rows from runs")
	}
	row := rows[0].(map[string]any)
	for _, key := range []string{"workingOn", "blockedOn", "handingOffTo"} {
		if _, ok := row[key]; !ok {
			t.Fatalf("roster row missing %s: %#v", key, row)
		}
	}
}

func newServeFixture(t *testing.T) *serveServer {
	t.Helper()
	root := t.TempDir()
	vault := filepath.Join(root, ".tusker")
	stateRoot := filepath.Join(root, "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	for _, dir := range []string{
		filepath.Join(vault, "work", "tasks"),
		filepath.Join(vault, "work", "epics"),
		filepath.Join(vault, "work", "gates"),
	} {
		if err := ensureDir(dir); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeText(filepath.Join(root, "tusker.yaml"), "schema: tusker.config/v1\nproject_id: app\nstorage:\n  root: .tusker\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeText(workflowPath(vault), defaultWorkflowMarkdown()); err != nil {
		t.Fatal(err)
	}
	writeServeEpic(t, vault, "APP", "App")
	writeServeEpic(t, vault, "TRC", "Trace")
	writeServeTask(t, vault, serveTaskSeed{ID: "APP-T-0001", Epic: "APP", Title: "Critical review", Status: "review", Risk: "critical", Priority: "p0"})
	writeServeTask(t, vault, serveTaskSeed{ID: "APP-T-0002", Epic: "APP", Title: "Low review", Status: "review", Risk: "low", Priority: "p2"})
	writeServeTask(t, vault, serveTaskSeed{ID: "APP-T-0003", Epic: "APP", Title: "Human gate", Status: "ready", Risk: "medium", Priority: "p1"})
	writeServeTask(t, vault, serveTaskSeed{ID: "APP-T-0004", Epic: "APP", Title: "Rework loop", Status: "ready", Risk: "medium", Priority: "p1", ReworkTransitions: 2})
	writeServeTask(t, vault, serveTaskSeed{ID: "APP-T-0005", Epic: "APP", Title: "Dependent", Status: "ready", Risk: "medium", Priority: "p2", Dependencies: []string{"APP-T-0001"}})
	writeServeTask(t, vault, serveTaskSeed{ID: "APP-T-0006", Epic: "APP", Title: "Dependency blocked", Status: "ready", Readiness: "blocked_dependency", Risk: "medium", Priority: "p2", Dependencies: []string{"APP-T-0005"}})
	writeServeTask(t, vault, serveTaskSeed{ID: "APP-T-0007", Epic: "APP", Title: "Terminal failure", Status: "ready", Risk: "high", Priority: "p1"})
	writeServeTask(t, vault, serveTaskSeed{ID: "APP-T-0008", Epic: "TRC", Title: "Retrying failure", Status: "ready", Risk: "high", Priority: "p1"})
	writeServeTask(t, vault, serveTaskSeed{ID: "APP-T-0009", Epic: "TRC", Title: "Parked loop", Status: "ready", Risk: "high", Priority: "p1"})
	writeServeGate(t, vault)

	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	project := RegisteredProject{ProjectID: "app", ProjectKey: "app", Name: "app", RepoRoot: root, VaultRoot: vault, WorkflowPath: workflowPath(vault), Enabled: true, Health: projectHealthHealthy, LastPollAt: "2026-07-06T07:00:00Z"}
	if err := store.UpsertProject(project); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(RunStatus{ProjectID: "app", RecordID: "APP-T-0007", ItemID: "APP-T-0007", Runner: string(RunnerCodexAppServer), Lane: runLaneExecute, LeaseState: string(LeaseStateReleased), AttemptOutcome: string(AttemptOutcomeFailed), AttemptCount: 3, LastError: "boom", StartedAt: "2026-07-06T06:00:00Z", UpdatedAt: "2026-07-06T06:10:00Z", LastEventAt: "2026-07-06T06:10:00Z"}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(RunStatus{ProjectID: "app", RecordID: "APP-T-0008", ItemID: "APP-T-0008", Runner: string(RunnerCodexAppServer), Lane: runLaneExecute, LeaseState: string(LeaseStateRetryQueued), AttemptOutcome: string(AttemptOutcomeFailed), AttemptCount: 1, LastError: "retry me", NextRetryAt: "2026-07-06T06:20:00Z", StartedAt: "2026-07-06T06:00:00Z", UpdatedAt: "2026-07-06T06:10:00Z", LastEventAt: "2026-07-06T06:10:00Z"}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(RunStatus{ProjectID: "app", RecordID: "APP-T-0009", ItemID: "APP-T-0009", Runner: string(RunnerCodexAppServer), Lane: runLaneExecute, LeaseState: string(LeaseStateParkedNoProgress), AttemptOutcome: string(AttemptOutcomeBlocked), AttemptCount: 3, LastError: "continuation retry cap reached", StartedAt: "2026-07-06T06:00:00Z", UpdatedAt: "2026-07-06T06:10:00Z", LastEventAt: "2026-07-06T06:10:00Z", Terminal: true}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(RunStatus{ProjectID: "app", RecordID: "APP-T-0003", ItemID: "APP-T-0003", Runner: string(RunnerCodexAppServer), Lane: runLaneExecute, LeaseState: string(LeaseStateRunning), AttemptOutcome: string(AttemptOutcomeNone), AttemptCount: 1, WorkspacePath: "/tmp/app", StartedAt: "2026-07-06T06:00:00Z", UpdatedAt: "2026-07-06T06:10:00Z", LastEventAt: "2026-07-06T06:10:00Z"}); err != nil {
		t.Fatal(err)
	}
	assets := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html>serve fixture</html>"), ModTime: time.Now()}}
	server := newServeServer(vault, root, defaultServeAddr, store, assets)
	server.now = func() time.Time {
		return time.Date(2026, 7, 6, 6, 11, 0, 0, time.UTC)
	}
	return server
}

func newServeEmptyNeedsFixture(t *testing.T) *serveServer {
	t.Helper()
	root := t.TempDir()
	vault := filepath.Join(root, ".tusker")
	stateRoot := filepath.Join(root, "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	for _, dir := range []string{
		filepath.Join(vault, "work", "tasks"),
		filepath.Join(vault, "work", "epics"),
		filepath.Join(vault, "work", "gates"),
	} {
		if err := ensureDir(dir); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeText(filepath.Join(root, "tusker.yaml"), "schema: tusker.config/v1\nproject_id: app\nstorage:\n  root: .tusker\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeText(workflowPath(vault), defaultWorkflowMarkdown()); err != nil {
		t.Fatal(err)
	}
	writeServeEpic(t, vault, "APP", "App")
	writeServeTask(t, vault, serveTaskSeed{ID: "APP-T-0001", Epic: "APP", Title: "Quiet ready task", Status: "ready", Risk: "medium", Priority: "p2"})

	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	project := RegisteredProject{ProjectID: "app", ProjectKey: "app", Name: "app", RepoRoot: root, VaultRoot: vault, WorkflowPath: workflowPath(vault), Enabled: true, Health: projectHealthHealthy, LastPollAt: "2026-07-06T07:00:00Z"}
	if err := store.UpsertProject(project); err != nil {
		t.Fatal(err)
	}
	assets := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html>serve fixture</html>"), ModTime: time.Now()}}
	server := newServeServer(vault, root, defaultServeAddr, store, assets)
	server.now = func() time.Time {
		return time.Date(2026, 7, 6, 6, 11, 0, 0, time.UTC)
	}
	return server
}

type serveTaskSeed struct {
	ID                string
	Epic              string
	Title             string
	Status            string
	Readiness         string
	Risk              string
	Priority          string
	Dependencies      []string
	ReworkTransitions int
}

func writeServeEpic(t *testing.T, vault, id, title string) {
	t.Helper()
	body := "---\nschema: \"tusker.epic/v7\"\nkind: \"epic\"\nid: \"" + id + "\"\nproject: \"app\"\ntitle: \"" + title + "\"\nstatus: \"ready\"\nupdated_at: \"2026-07-06T06:00:00Z\"\n---\n\n# " + id + " - " + title + "\n"
	if err := writeText(filepath.Join(vault, "work", "epics", id+".md"), body); err != nil {
		t.Fatal(err)
	}
}

func writeServeTask(t *testing.T, vault string, seed serveTaskSeed) {
	t.Helper()
	readiness := firstNonEmpty(seed.Readiness, "ready")
	var deps string
	if len(seed.Dependencies) > 0 {
		deps = "dependencies:\n"
		for _, dep := range seed.Dependencies {
			deps += "  - \"" + dep + "\"\n"
		}
	} else {
		deps = "dependencies: []\n"
	}
	transitions := "transitions: []\n"
	if seed.ReworkTransitions > 0 {
		transitions = "transitions:\n"
		for i := 0; i < seed.ReworkTransitions; i++ {
			transitions += fmt.Sprintf("  - at: \"2026-07-06T06:%02d:00Z\"\n    kind: \"status\"\n    from: \"review\"\n    to: \"rework\"\n    actor: \"reviewer:agent\"\n    reason: \"needs changes\"\n", i)
		}
	}
	body := "---\nschema: \"tusker.task/v7\"\nkind: \"task\"\nid: \"" + seed.ID + "\"\nproject: \"app\"\ntitle: \"" + seed.Title + "\"\nepic: \"" + seed.Epic + "\"\nstatus: \"" + seed.Status + "\"\nreadiness: \"" + readiness + "\"\npriority: \"" + seed.Priority + "\"\nrisk: \"" + seed.Risk + "\"\nsize: \"m\"\nproof_mode: \"inline\"\nproof_status: \"pending\"\nproof_required:\n  - \"focused_test\"\n" + deps + "gates: []\ncreated_at: \"2026-07-06T06:00:00Z\"\nupdated_at: \"2026-07-06T06:00:00Z\"\nnext_owner: \"agent\"\nnext_action: \"Execute.\"\n" + transitions + "---\n\n# " + seed.ID + " - " + seed.Title + "\n\n## Intent\n\nDo the work.\n\n## Acceptance\n\n| ID | Outcome | Proof |\n|---|---|---|\n| A1 | Works. | Inline verification |\n\n## Non-goals\n\n- None.\n\n## Verification\n\n| Covers | Check | Result | Notes |\n|---|---|---|---|\n| A1 | go test ./cmd/tusker -run TestServe -count=1 | pending | Focused. |\n\n## Evidence\n\nAccepted:\n- None.\n\nPending:\n- None.\n\n## Knowledge delta\n\nNone expected.\n"
	if err := writeText(filepath.Join(vault, "work", "tasks", seed.ID+".md"), body); err != nil {
		t.Fatal(err)
	}
}

func writeServeGate(t *testing.T, vault string) {
	t.Helper()
	body := "---\nschema: \"tusker.gate/v1\"\nkind: \"gate\"\nid: \"APP-G-0001\"\nproject: \"app\"\ntitle: \"Provision credentials\"\ngate_kind: \"provision\"\nstatus: \"open\"\nowner: \"human:sarav\"\nblocking: true\nblocks:\n  - \"APP-T-0003\"\naction: \"Provision test credentials.\"\n---\n\n# APP-G-0001 - Provision credentials\n\n## Action\n\nProvision test credentials.\n"
	if err := writeText(filepath.Join(vault, "work", "gates", "APP-G-0001.md"), body); err != nil {
		t.Fatal(err)
	}
}

func serveDecode(t *testing.T, server *serveServer, path string, out any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s returned %d: %s", path, rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
		t.Fatalf("decode %s: %v\n%s", path, err, rec.Body.String())
	}
}
