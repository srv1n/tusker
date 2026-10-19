package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// wmlWideningFixture builds a git-backed vault with APP-T-0001 in review that
// owns src/ but whose submission also changed lib/caller.go, and returns the
// reviewer's proof-scope finding asking to widen owned_paths to that file.
func wmlWideningFixture(t *testing.T) (vault, finding string) {
	t.Helper()
	vault = automationTestVault(t)
	repo := filepath.Dir(vault)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Capacity", "risk": "low", "priority": "p0", "owned-paths": "src", "v7": "true"}, newV7Task)
	writeDocsFixture(t, repo, "src/capacity.go", "package src\n")
	writeDocsFixture(t, repo, "lib/caller.go", "package lib\n")
	runDocsGit(t, repo, "init", "-b", "main")
	runDocsGit(t, repo, "config", "user.email", "test@example.com")
	runDocsGit(t, repo, "config", "user.name", "Test")
	runDocsGit(t, repo, "add", ".")
	runDocsGit(t, repo, "commit", "-m", "base")
	base := strings.TrimSpace(docsGitOutput(t, repo, "rev-parse", "HEAD"))
	writeDocsFixture(t, repo, "src/capacity.go", "package src\n\nvar Cap = 2\n")
	writeDocsFixture(t, repo, "lib/caller.go", "package lib\n\nvar Uses = 2\n")
	runDocsGit(t, repo, "add", "src", "lib")
	runDocsGit(t, repo, "commit", "-m", "work")
	head := strings.TrimSpace(docsGitOutput(t, repo, "rev-parse", "HEAD"))

	path := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	data, body, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	data["status"], data["readiness"], data["next_owner"] = "review", "waiting_on_review", "reviewer"
	data["source_sha"], data["base_sha"] = head, base
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(path, content); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(reviewerFindingRecord{
		Schema: reviewerFindingSchema, ID: "F-001", Kind: "blocking", Acceptance: []string{"A1"},
		Evidence:         []string{"caller.go"},
		Consequence:      "The submission edits a file outside owned_paths.",
		ClosureCondition: "Architect widens owned_paths to include caller.go, then review is re-requested.",
		RepairScope:      reviewerFindingRepairScopeProof,
	})
	return vault, string(raw)
}

func wmlTask(t *testing.T, vault, id string) Note {
	t.Helper()
	note, err := resolveV7Note(vault, id, "task")
	if err != nil {
		t.Fatal(err)
	}
	return note
}

func TestReviewWidensUnclaimedOwnedPath(t *testing.T) {
	vault, finding := wmlWideningFixture(t)
	project := registerAutomationTestProject(t, vault)
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.UpsertRun(RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", Lane: runLaneReview, LeaseState: string(LeaseStateReleased)}); err != nil {
		t.Fatal(err)
	}

	if err := returnReviewerFindingToImplementer(vault, "APP-T-0001", finding, "daemon:review-pass"); err != nil {
		t.Fatal(err)
	}
	task := wmlTask(t, vault, "APP-T-0001")
	if got := strings.Join(normalizeList(task.Data["owned_paths"]), ","); got != "lib/caller.go,src" {
		t.Fatalf("owned_paths = %q, want widened to lib/caller.go,src", got)
	}
	if status := stringField(task.Data, "status"); status != "review" {
		t.Fatalf("status = %q, want review re-requested", status)
	}
	if !strings.Contains(task.Body, ownedPathWideningLogPrefix+"lib/caller.go") || strings.Contains(task.Body, reviewerFindingGeneratedMarker) {
		t.Fatalf("expected a widening work-log entry and no bounce:\n%s", task.Body)
	}
	if window, err := store.BudgetWindowStart(project.ProjectID, "APP-T-0001"); err != nil || window.ResetAt == "" || window.Actor != ownedPathWideningEditActor {
		t.Fatalf("review-cycle window not reset: %#v err=%v", window, err)
	}

	// The reactor sees the same stale result until the fresh review runs: it
	// must neither bounce the task nor widen again.
	rev := stringField(task.Data, "state_rev")
	if err := returnReviewerFindingToImplementer(vault, "APP-T-0001", finding, "daemon:review-pass"); err != nil {
		t.Fatal(err)
	}
	if again := wmlTask(t, vault, "APP-T-0001"); stringField(again.Data, "state_rev") != rev {
		t.Fatalf("stale result rewrote the task: status=%s", stringField(again.Data, "status"))
	}

	// A material finding is not a widening request: ordinary bounce.
	material := strings.Replace(finding, `"repair_scope":"proof"`, `"repair_scope":"material"`, 1)
	if handled, err := widenOwnedPathsForReview(vault, "APP-T-0001", material); handled || err != nil {
		t.Fatalf("material finding handled=%v err=%v", handled, err)
	}
}

func TestReviewWideningRefusesClaimedPath(t *testing.T) {
	t.Run("another active task owns the file", func(t *testing.T) {
		vault, finding := wmlWideningFixture(t)
		mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Caller owner", "risk": "low", "priority": "p1", "owned-paths": "lib", "v7": "true"}, newV7Task)
		wmlAssertRoutedToOperator(t, vault, finding, "APP-T-0002")
	})
	t.Run("WORKFLOW.md do_not_touch", func(t *testing.T) {
		vault, finding := wmlWideningFixture(t)
		wf := workflowPath(vault)
		text, err := os.ReadFile(wf)
		if err != nil {
			t.Fatal(err)
		}
		if err := writeText(wf, strings.Replace(string(text), "---\n", "---\ndo_not_touch:\n  - lib\n", 1)); err != nil {
			t.Fatal(err)
		}
		wmlAssertRoutedToOperator(t, vault, finding, "WORKFLOW.md do_not_touch")
	})
}

func wmlAssertRoutedToOperator(t *testing.T, vault, finding, reason string) {
	t.Helper()
	for range 2 {
		if err := returnReviewerFindingToImplementer(vault, "APP-T-0001", finding, "daemon:review-pass"); err != nil {
			t.Fatal(err)
		}
	}
	task := wmlTask(t, vault, "APP-T-0001")
	if got := strings.Join(normalizeList(task.Data["owned_paths"]), ","); got != "src" {
		t.Fatalf("owned_paths = %q, want unchanged", got)
	}
	if status := stringField(task.Data, "status"); status != "review" {
		t.Fatalf("status = %q, want review held for the operator", status)
	}
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	gates := 0
	for _, gate := range idx.Gates {
		action := stringField(gate.Data, "action")
		if !v7GateTouchesTask(gate, "APP-T-0001") || !strings.Contains(action, ownedPathWideningGateMarker) {
			continue
		}
		gates++
		if stringField(gate.Data, "owner") != "human:operator" || stringField(gate.Data, "status") != "open" || !strings.Contains(action, reason) || !strings.Contains(action, "lib/caller.go") {
			t.Fatalf("attention gate = %#v", gate.Data)
		}
	}
	if gates != 1 {
		t.Fatalf("want exactly one operator gate, got %d", gates)
	}
}
