package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestV7DocOpeningLintUsesPlainShapeHeuristic(t *testing.T) {
	body := "# CLI\n\nThis document explains cmd/tusker/serve.go and the ServeRouter.\n\n## Details\n\nMore.\n"
	offenders := v7DocOpeningCodeWords(body)
	if !containsString(offenders, "cmd/tusker/serve.go") {
		t.Fatalf("expected opening code words, got %#v", offenders)
	}
	clean := "# CLI\n\nThis document explains how an operator uses the service and what to expect.\n"
	if got := v7DocOpeningCodeWords(clean); len(got) != 0 {
		t.Fatalf("plain opening should pass, got %#v", got)
	}
}

func TestLockedSpecUpdateSeverityMatchesLockedContract(t *testing.T) {
	issues := []Issue{
		issue("SPEC_UPDATES_UNLANDED", "unplanned update", ".tusker/specs/locked.md", "", nil),
		issue("SPEC_UPDATES_PENDING", "planned update", ".tusker/specs/locked.md", "", nil),
	}
	var errs, warns []Issue
	classifyLockedSpecUpdateIssues(issues, &errs, &warns)
	if !issuesContainCode(errs, "SPEC_UPDATES_UNLANDED") || issuesContainCode(errs, "SPEC_UPDATES_PENDING") {
		t.Fatalf("locked unlanded update must fail while pending task only warns: errs=%#v", errs)
	}
	if !issuesContainCode(warns, "SPEC_UPDATES_PENDING") || issuesContainCode(warns, "SPEC_UPDATES_UNLANDED") {
		t.Fatalf("pending task must warn while unlanded update must fail: warns=%#v", warns)
	}
}

func TestValidationIssueScoping(t *testing.T) {
	issues := []Issue{
		{Code: "ONE", Path: "work/tasks/FLW-T-0001.md", Message: "FLW-T-0001 failed"},
		{Code: "TWO", Path: "work/tasks/OLD-T-0001.md", Message: "old failure"},
	}
	if got := filterValidationIssuesByPath(issues, "work/tasks"); len(got) != 2 {
		t.Fatalf("path scope = %#v", got)
	}
	if got := filterValidationIssuesByEpic(issues, "FLW"); len(got) != 1 || got[0].Code != "ONE" {
		t.Fatalf("epic scope = %#v", got)
	}
}

func TestLockedSpecUpdatesRequireLandingOrDocTask(t *testing.T) {
	repo := t.TempDir()
	writeTestDoc(t, repo, "docs/system/00-overview.md", "---\nsubject: overview\n---\n# Overview\n")
	writeTestDoc(t, repo, ".tusker/specs/locked.md", "---\nsubject: locked\npart_of: overview\ndecisions_locked: true\nupdates: [docs/system/cli.md]\n---\n# Locked\n")
	writeTestDoc(t, repo, "docs/system/cli.md", "---\nsubject: cli\npart_of: overview\n---\n# CLI\n")
	if err := os.Chtimes(filepath.Join(repo, ".tusker/specs/locked.md"), unixTime(2), unixTime(2)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(repo, "docs/system/cli.md"), unixTime(1), unixTime(1)); err != nil {
		t.Fatal(err)
	}
	issues, err := validateLockedSpecUpdates(repo, filepath.Join(repo, ".tusker"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !issuesContainCode(issues, "SPEC_UPDATES_UNLANDED") {
		t.Fatalf("expected unlanded locked update, got %#v", issues)
	}
}

func TestLockedSpecUpdatesAcceptRelatedDocTask(t *testing.T) {
	repo := t.TempDir()
	vault := filepath.Join(repo, ".tusker")
	writeTestDoc(t, repo, "docs/system/00-overview.md", "---\nsubject: overview\n---\n# Overview\n")
	writeTestDoc(t, repo, ".tusker/specs/locked.md", "---\nsubject: locked\npart_of: overview\ndecisions_locked: true\nupdates: [docs/system/cli.md]\n---\n# Locked\n")
	writeTestDoc(t, repo, "docs/system/cli.md", "---\nsubject: cli\npart_of: overview\n---\n# CLI\n")
	if err := os.Chtimes(filepath.Join(repo, ".tusker/specs/locked.md"), unixTime(2), unixTime(2)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(repo, "docs/system/cli.md"), unixTime(1), unixTime(1)); err != nil {
		t.Fatal(err)
	}
	notes := []Note{
		{Data: map[string]any{"kind": "epic", "id": "APP", "spec_refs": []any{".tusker/specs/locked.md"}}},
		{Data: map[string]any{"kind": "task", "epic": "APP", "spec_refs": []any{".tusker/specs/locked.md"}, "title": "Routine implementation"}, Body: "No documentation work here."},
		{Data: map[string]any{"kind": "epic", "id": "OPS", "spec_refs": []any{".tusker/specs/locked.md"}}},
		{Data: map[string]any{"kind": "task", "epic": "OPS", "spec_refs": []any{".tusker/specs/locked.md"}, "title": "Update canonical documentation"}, Body: "Refresh the canonical doc."},
	}
	issues, err := validateLockedSpecUpdates(repo, vault, notes)
	if err != nil {
		t.Fatal(err)
	}
	if issuesContainCode(issues, "SPEC_UPDATES_UNLANDED") {
		t.Fatalf("related doc task should suppress hard failure: %#v", issues)
	}
	if !issuesContainCode(issues, "SPEC_UPDATES_PENDING") {
		t.Fatalf("expected pending warning, got %#v", issues)
	}
}

func TestLockedSpecUpdatesAcceptCanonicalDeliveryAliasWithoutEpicRef(t *testing.T) {
	repo := t.TempDir()
	vault := filepath.Join(repo, ".tusker")
	writeTestDoc(t, repo, "docs/system/00-overview.md", "---\nsubject: overview\n---\n# Overview\n")
	writeTestDoc(t, repo, ".tusker/specs/execution-observability.md", "---\nsubject: execution-observability\npart_of: overview\ndecisions_locked: true\nupdates:\n  - docs/system/cli.md\n  - docs/system/orchestration.md\n  - docs/system/serve-ui.md\n---\n# Execution observability\n")
	writeTestDoc(t, repo, "docs/specs/24-execution-observability.md", "---\nsubject: execution-observability-intake\npart_of: overview\n---\n# Execution observability\n\nSee [the canonical execution-observability spec](../../.tusker/specs/execution-observability.md).\n")
	for _, path := range []string{"docs/system/cli.md", "docs/system/orchestration.md", "docs/system/serve-ui.md"} {
		writeTestDoc(t, repo, path, "---\nsubject: "+filepath.Base(path)+"\npart_of: overview\n---\n# Documentation\n")
		if err := os.Chtimes(filepath.Join(repo, filepath.FromSlash(path)), unixTime(1), unixTime(1)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(filepath.Join(repo, ".tusker/specs/execution-observability.md"), unixTime(2), unixTime(2)); err != nil {
		t.Fatal(err)
	}
	notes := []Note{
		{Data: map[string]any{"kind": "epic", "id": "ORC"}},
		{Data: map[string]any{
			"kind":      "task",
			"id":        "ORC-T-0076",
			"epic":      "ORC",
			"spec_refs": []any{"docs/specs/24-execution-observability.md"},
			"title":     "Publish execution observability guidance and rollout boundaries",
		}, Body: "Publish canonical documentation updates."},
	}
	issues, err := validateLockedSpecUpdates(repo, vault, notes)
	if err != nil {
		t.Fatal(err)
	}
	if issuesContainCode(issues, "SPEC_UPDATES_UNLANDED") {
		t.Fatalf("canonical delivery alias task should suppress hard failure: %#v", issues)
	}
	if !issuesContainCode(issues, "SPEC_UPDATES_PENDING") {
		t.Fatalf("expected pending warnings until the follow-up docs land: %#v", issues)
	}
}

func TestLockedSpecUpdatesIgnoreUnrelatedLaterGitEdit(t *testing.T) {
	repo := t.TempDir()
	vault := filepath.Join(repo, ".tusker")
	initDocsValidationGitRepo(t, repo)
	writeTestDoc(t, repo, "docs/system/00-overview.md", "---\nsubject: overview\n---\n# Overview\n")
	writeTestDoc(t, repo, "docs/system/cli.md", "---\nsubject: cli\npart_of: overview\n---\n# CLI\nOriginal.\n")
	runGitDir(t, repo, "add", ".")
	runGitDir(t, repo, "commit", "-m", "seed docs")

	writeTestDoc(t, repo, ".tusker/specs/locked.md", "---\nsubject: locked\npart_of: overview\ndecisions_locked: true\nupdates: [docs/system/cli.md]\n---\n# Locked\n")
	runGitDir(t, repo, "add", ".tusker/specs/locked.md")
	runGitDir(t, repo, "commit", "-m", "lock spec")

	writeTestDoc(t, repo, "docs/system/cli.md", "---\nsubject: cli\npart_of: overview\n---\n# CLI\nUnrelated typo fix.\n")
	runGitDir(t, repo, "add", "docs/system/cli.md")
	runGitDir(t, repo, "commit", "-m", "fix typo")

	issues, err := validateLockedSpecUpdates(repo, vault, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !issuesContainCode(issues, "SPEC_UPDATES_UNLANDED") {
		t.Fatalf("unrelated later target edit must not satisfy the locked update: %#v", issues)
	}
}

func TestLockedSpecUpdatesAcceptTargetInSpecGitChange(t *testing.T) {
	repo := t.TempDir()
	vault := filepath.Join(repo, ".tusker")
	initDocsValidationGitRepo(t, repo)
	writeTestDoc(t, repo, "docs/system/00-overview.md", "---\nsubject: overview\n---\n# Overview\n")
	writeTestDoc(t, repo, "docs/system/cli.md", "---\nsubject: cli\npart_of: overview\n---\n# CLI\nUpdated by the locked change.\n")
	writeTestDoc(t, repo, ".tusker/specs/locked.md", "---\nsubject: locked\npart_of: overview\ndecisions_locked: true\nupdates: [docs/system/cli.md]\n---\n# Locked\n")
	runGitDir(t, repo, "add", ".")
	runGitDir(t, repo, "commit", "-m", "lock spec and land docs")

	issues, err := validateLockedSpecUpdates(repo, vault, nil)
	if err != nil {
		t.Fatal(err)
	}
	if issuesContainCode(issues, "SPEC_UPDATES_UNLANDED") || issuesContainCode(issues, "SPEC_UPDATES_PENDING") {
		t.Fatalf("target changed in the locked commit should satisfy the update: %#v", issues)
	}
}

func TestLockedSpecUpdatesAnchorToChangedAuthorityFields(t *testing.T) {
	repo := t.TempDir()
	vault := filepath.Join(repo, ".tusker")
	initDocsValidationGitRepo(t, repo)
	writeTestDoc(t, repo, "docs/system/00-overview.md", "---\nsubject: overview\n---\n# Overview\n")
	writeTestDoc(t, repo, "docs/system/cli.md", "---\nsubject: cli\npart_of: overview\n---\n# CLI\nOriginal.\n")
	writeTestDoc(t, repo, ".tusker/specs/locked.md", "---\nsubject: locked\npart_of: overview\ndecisions_locked: false\n---\n# Locked\n")
	runGitDir(t, repo, "add", ".")
	runGitDir(t, repo, "commit", "-m", "seed unlocked spec")

	writeTestDoc(t, repo, ".tusker/specs/locked.md", "---\nsubject: locked\npart_of: overview\ndecisions_locked: true\nupdates: [docs/system/cli.md]\n---\n# Locked\n")
	writeTestDoc(t, repo, "docs/system/cli.md", "---\nsubject: cli\npart_of: overview\n---\n# CLI\nUpdated with the newly locked decision.\n")
	runGitDir(t, repo, "add", ".tusker/specs/locked.md", "docs/system/cli.md")
	runGitDir(t, repo, "commit", "-m", "lock decision and land docs")

	issues, err := validateLockedSpecUpdates(repo, vault, nil)
	if err != nil {
		t.Fatal(err)
	}
	if issuesContainCode(issues, "SPEC_UPDATES_UNLANDED") || issuesContainCode(issues, "SPEC_UPDATES_PENDING") {
		t.Fatalf("target changed with the authority fields should satisfy the update: %#v", issues)
	}
}

func TestLockedSpecUpdatesDoNotTreatDirtyPairAsLanded(t *testing.T) {
	repo := t.TempDir()
	vault := filepath.Join(repo, ".tusker")
	initDocsValidationGitRepo(t, repo)
	writeTestDoc(t, repo, "docs/system/00-overview.md", "---\nsubject: overview\n---\n# Overview\n")
	writeTestDoc(t, repo, "docs/system/cli.md", "---\nsubject: cli\npart_of: overview\n---\n# CLI\nOriginal.\n")
	writeTestDoc(t, repo, ".tusker/specs/locked.md", "---\nsubject: locked\npart_of: overview\ndecisions_locked: false\n---\n# Locked\n")
	runGitDir(t, repo, "add", ".")
	runGitDir(t, repo, "commit", "-m", "seed unlocked spec")

	writeTestDoc(t, repo, ".tusker/specs/locked.md", "---\nsubject: locked\npart_of: overview\ndecisions_locked: true\nupdates: [docs/system/cli.md]\n---\n# Locked\n")
	writeTestDoc(t, repo, "docs/system/cli.md", "---\nsubject: cli\npart_of: overview\n---\n# CLI\nDirty work in progress.\n")

	issues, err := validateLockedSpecUpdates(repo, vault, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !issuesContainCode(issues, "SPEC_UPDATES_UNLANDED") {
		t.Fatalf("dirty spec and target must remain unlanded until committed: %#v", issues)
	}
}

func TestLockedSpecUpdatesAcceptTaskBackedLaterGitEdit(t *testing.T) {
	repo := t.TempDir()
	vault := filepath.Join(repo, ".tusker")
	initDocsValidationGitRepo(t, repo)
	writeTestDoc(t, repo, "docs/system/00-overview.md", "---\nsubject: overview\n---\n# Overview\n")
	writeTestDoc(t, repo, "docs/system/cli.md", "---\nsubject: cli\npart_of: overview\n---\n# CLI\nOriginal.\n")
	writeTestDoc(t, repo, ".tusker/specs/locked.md", "---\nsubject: locked\npart_of: overview\ndecisions_locked: true\nupdates: [docs/system/cli.md]\n---\n# Locked\n")
	runGitDir(t, repo, "add", ".")
	runGitDir(t, repo, "commit", "-m", "lock spec")

	writeTestDoc(t, repo, "docs/system/cli.md", "---\nsubject: cli\npart_of: overview\n---\n# CLI\nUpdated by the follow-up task.\n")
	runGitDir(t, repo, "add", "docs/system/cli.md")
	runGitDir(t, repo, "commit", "-m", "land doc update task")

	notes := []Note{
		{Data: map[string]any{"kind": "epic", "id": "OPS", "spec_refs": []any{".tusker/specs/locked.md"}}},
		{Data: map[string]any{"kind": "task", "epic": "OPS", "spec_refs": []any{".tusker/specs/locked.md"}, "title": "Update canonical documentation"}, Body: "Refresh the canonical doc."},
	}
	issues, err := validateLockedSpecUpdates(repo, vault, notes)
	if err != nil {
		t.Fatal(err)
	}
	if issuesContainCode(issues, "SPEC_UPDATES_UNLANDED") || issuesContainCode(issues, "SPEC_UPDATES_PENDING") {
		t.Fatalf("task-backed later target edit should satisfy the update: %#v", issues)
	}
}

func initDocsValidationGitRepo(t *testing.T, repo string) {
	t.Helper()
	runGitDir(t, repo, "init", "-b", "main")
	runGitDir(t, repo, "config", "user.email", "test@example.com")
	runGitDir(t, repo, "config", "user.name", "Tusker Test")
}

func unixTime(seconds int64) time.Time { return time.Unix(seconds, 0) }

func writeTestDoc(t *testing.T, repo, relative, content string) {
	t.Helper()
	path := filepath.Join(repo, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
