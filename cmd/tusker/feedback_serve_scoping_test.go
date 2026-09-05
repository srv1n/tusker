package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Bug 1: a coincidental normalized title must not mark feedback as already-linked
// when the existing note is of a kind this promotion could never target.
func TestFeedbackPromoteDuplicateTitleRequiresMatchingKind(t *testing.T) {
	source := feedbackPromoteSource{
		Kind:        "feedback_signal",
		ID:          "sig-1",
		Title:       "Retry backoff is too aggressive",
		Severity:    "P1",
		RepeatCount: 2,
	}
	if kind := feedbackPromoteTargetKind(normalizeFeedbackPromoteSource(source)); kind != "task" {
		t.Fatalf("fixture drifted: expected target kind task, got %q", kind)
	}

	unrelated := []feedbackPromoteExistingWork{{
		ID: "APP-R-0001", Kind: "runbook", Title: "Retry backoff is too aggressive", Path: "docs/runbooks/retry.md",
	}}
	if match, ok := matchFeedbackPromoteDuplicate(source, unrelated); ok {
		t.Fatalf("title-only match against unrelated kind should not count as duplicate: %#v", match)
	}

	sameKind := []feedbackPromoteExistingWork{{
		ID: "APP-T-0001", Kind: "task", Title: "retry backoff is TOO aggressive", Path: "work/tasks/APP-T-0001.md",
	}}
	match, ok := matchFeedbackPromoteDuplicate(source, sameKind)
	if !ok || match.ID != "APP-T-0001" || match.Field != "title" {
		t.Fatalf("same-kind title match should still dedupe, got ok=%v match=%#v", ok, match)
	}
}

// Bug 2: the validation-issue path must be the vault-relative form of the real
// write path, otherwise issues point at files that do not exist.
func TestFeedbackSignalRelativePathMatchesWritePath(t *testing.T) {
	vault := t.TempDir()
	signal := completeFeedbackSignal(feedbackSignal{
		Project:       "APP",
		Date:          "2026-08-17",
		Source:        "reducer",
		Category:      "cli_friction",
		Severity:      "P1",
		DedupeKey:     "retry-backoff-too-aggressive",
		Summary:       "Retry backoff is too aggressive.",
		ObservedFacts: map[string]any{"runs": 3},
	})

	written, err := writeFeedbackSignal(vault, signal)
	if err != nil {
		t.Fatal(err)
	}
	rel := feedbackSignalRelativePath(signal)
	if got := filepath.Join(vault, filepath.FromSlash(rel)); got != written {
		t.Fatalf("relative path %q resolves to %q but signal was written to %q", rel, got, written)
	}
	if _, err := os.Stat(filepath.Join(vault, filepath.FromSlash(rel))); err != nil {
		t.Fatalf("relative signal path does not exist on disk: %v", err)
	}
}

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
