package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// Bug 3: /api/digest must honour ?project= instead of always using the launch vault.
func TestServeDigestHonoursProjectParam(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	addServeProjectFixture(t, server, "backend", "BCK-T-0001", "Backend task")

	var digest tuskerDigest
	serveDecode(t, server, "/api/digest?project=backend", &digest)
	if digest.ProjectID != "backend" {
		t.Fatalf("digest ignored project param: %#v", digest.ProjectID)
	}

	var launch tuskerDigest
	serveDecode(t, server, "/api/digest", &launch)
	if launch.ProjectID != "app" {
		t.Fatalf("digest without project should fall back to launch project, got %q", launch.ProjectID)
	}
}

// Bug 4: a disabled first project must not stop serve from picking the next
// enabled+loadable project.
func TestDaemonServeTargetSkipsDisabledProject(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	register := func(projectID string, enabled bool) {
		root := t.TempDir()
		vault := filepath.Join(root, ".tusker")
		if err := ensureDir(filepath.Join(vault, "work", "tasks")); err != nil {
			t.Fatal(err)
		}
		if err := writeText(managedTuskerConfigPath(filepath.Join(root, defaultRepoVaultDir)), "schema: tusker.config/v1\nproject_id: "+projectID+"\nstorage:\n  root: .tusker\n"); err != nil {
			t.Fatal(err)
		}
		writeDaemonServeWorkflow(t, vault, true, "127.0.0.1:0")
		if err := store.UpsertProject(RegisteredProject{
			ProjectID: projectID, ProjectKey: projectID, Name: projectID,
			RepoRoot: root, VaultRoot: vault, WorkflowPath: workflowPath(vault),
			Enabled: enabled, Health: projectHealthHealthy,
		}); err != nil {
			t.Fatal(err)
		}
	}
	register("aaa-disabled", false)
	register("bbb-enabled", true)

	d := &Daemon{stateRoot: stateRoot, store: store}
	target, enabled, err := d.serveTarget()
	if err != nil {
		t.Fatal(err)
	}
	if !enabled {
		t.Fatal("a disabled first project must not disable serve globally")
	}
	if target.project.ProjectID != "bbb-enabled" {
		t.Fatalf("serve target should be the first enabled+loadable project, got %q", target.project.ProjectID)
	}
}

// Bug 5: interrupt is mutating and must not fall through to a same-ID run in
// another project when ?project= is missing.
func TestServeRunInterruptRequiresProjectParam(t *testing.T) {
	server := &serveServer{}
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7420/api/runs/APP-T-0001/interrupt", nil)
	rec := httptest.NewRecorder()
	server.handleRunInterrupt(rec, req, "APP-T-0001")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 without project param, got %d: %s", rec.Code, rec.Body.String())
	}
	var result serveInterruptResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Refused || result.Reason == "" {
		t.Fatalf("expected a refusal with a clear reason, got %#v", result)
	}
}

// Bug 6: a daemon start that fails after the 250ms "start requested" response
// must still surface in daemon status.
func TestDaemonStartErrorSurfacesInDaemonStatus(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	t.Cleanup(func() { setLastDaemonStartError(nil) })

	setLastDaemonStartError(nil)
	snap, err := server.loadSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	status := server.daemonStatusFromSnapshot(snap)
	if status.DaemonAlive {
		t.Skip("a live daemon is running in this environment")
	}
	baseline := stringValue(status.DaemonDownReason)

	setLastDaemonStartError(errors.New("serve addr already bound"))
	status = server.daemonStatusFromSnapshot(snap)
	reason := stringValue(status.DaemonDownReason)
	if reason == baseline {
		t.Fatalf("daemon status did not surface the start failure: %q", reason)
	}
	if !strings.Contains(reason, "serve addr already bound") {
		t.Fatalf("daemon down reason should name the start failure, got %q", reason)
	}
}
