package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaimMovesReadyTaskToActive(t *testing.T) {
	vault := pickupTestVault(t)
	if err := newV5Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Ready work", "risk": "low", "size": "s", "status": "ready"}, "feature"); err != nil {
		t.Fatal(err)
	}

	if err := claimCmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "as": "codex"}); err != nil {
		t.Fatal(err)
	}
	data, _, err := parseFrontmatterMustRead(filepath.Join(vault, "epics", "APP", "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "active", data["status"], "claimed status")
	assertEqual(t, "codex", data["assignee"], "claimed assignee")
	if stringField(data, "started") == "" {
		t.Fatal("expected claim to set started")
	}
}

func TestClaimRejectsBacklogAndUnresolvedBlockers(t *testing.T) {
	vault := pickupTestVault(t)
	if err := newV5Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Blocker", "risk": "low", "size": "s", "status": "ready"}, "feature"); err != nil {
		t.Fatal(err)
	}
	if err := newV5Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Dependent", "risk": "low", "size": "s", "status": "ready", "blocked-by": "APP-T-0001"}, "feature"); err != nil {
		t.Fatal(err)
	}

	if err := claimCmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0002", "as": "codex"}); err == nil {
		t.Fatal("expected unresolved blocker to reject claim")
	}
	if err := setStatus(Args{"vault": vault, "quiet": "true", "id": "APP-T-0002", "status": "backlog", "actor": "planner"}); err != nil {
		t.Fatal(err)
	}
	if err := claimCmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0002", "as": "codex"}); err == nil {
		t.Fatal("expected backlog to reject claim")
	}
}

func TestNextRanksPickableTasks(t *testing.T) {
	vault := pickupTestVault(t)
	if err := newV5Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Low risk", "risk": "low", "size": "s", "status": "ready", "priority": "p1"}, "feature"); err != nil {
		t.Fatal(err)
	}
	if err := newV5Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "High risk", "risk": "high", "size": "s", "status": "ready", "priority": "p1"}, "feature"); err != nil {
		t.Fatal(err)
	}
	if err := newV5Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Blocked p0", "risk": "low", "size": "s", "status": "ready", "priority": "p0", "blocked-by": "APP-T-0001"}, "feature"); err != nil {
		t.Fatal(err)
	}

	output := captureStdout(t, func() {
		if err := nextV5Cmd(Args{"vault": vault}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(output, "APP-T-0002") {
		t.Fatalf("expected high-risk unblocked p1 task, got:\n%s", output)
	}
}

func TestNextV7FiltersByDomainAndLane(t *testing.T) {
	vault := pickupV7TestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Frontend lane", "risk": "low", "priority": "p0", "domains": "frontend", "v7": "true"}, newV7Task)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Backend lane", "risk": "low", "priority": "p1", "domains": "backend", "v7": "true"}, newV7Task)
	setPickupV7TaskFields(t, vault, "APP-T-0001", map[string]any{"lane": "implementation"})
	setPickupV7TaskFields(t, vault, "APP-T-0002", map[string]any{"lanes": []string{"implementation", "review"}})

	output := captureStdout(t, func() {
		if err := nextCmd(Args{"vault": vault, "domain": "backend", "lane": "implementation"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(output, "APP-T-0002") {
		t.Fatalf("expected backend implementation task, got:\n%s", output)
	}
	if strings.Contains(output, "APP-T-0001") {
		t.Fatalf("expected filtered output to omit frontend task, got:\n%s", output)
	}
}

func TestNextV7ExplainShowsSelectedAndSkippedReasons(t *testing.T) {
	vault := pickupV7TestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Frontend lane", "risk": "low", "priority": "p0", "domains": "frontend", "v7": "true"}, newV7Task)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Backend lane", "risk": "low", "priority": "p1", "domains": "backend", "v7": "true"}, newV7Task)
	setPickupV7TaskFields(t, vault, "APP-T-0001", map[string]any{"lane": "implementation"})
	setPickupV7TaskFields(t, vault, "APP-T-0002", map[string]any{"lane": "implementation"})

	output := captureStdout(t, func() {
		if err := nextCmd(Args{"vault": vault, "domain": "backend", "lane": "implementation", "explain": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	for _, expected := range []string{"Selected:", "APP-T-0002", "Skipped:", "APP-T-0001", "domain frontend does not match backend"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("explain output missing %q:\n%s", expected, output)
		}
	}
}

func TestNextV7ExplainJSONIncludesItemAndSkipReasons(t *testing.T) {
	vault := pickupV7TestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Wrong lane", "risk": "low", "priority": "p0", "domains": "backend", "v7": "true"}, newV7Task)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Right lane", "risk": "low", "priority": "p1", "domains": "backend", "v7": "true"}, newV7Task)
	setPickupV7TaskFields(t, vault, "APP-T-0001", map[string]any{"lane": "review"})
	setPickupV7TaskFields(t, vault, "APP-T-0002", map[string]any{"lane": "implementation"})

	output := captureStdout(t, func() {
		if err := nextCmd(Args{"vault": vault, "domain": "backend", "lane": "implementation", "explain": "true", "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var payload struct {
		OK      bool           `json:"ok"`
		Item    map[string]any `json:"item"`
		Skipped []struct {
			ID      string   `json:"id"`
			Reasons []string `json:"reasons"`
		} `json:"skipped"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, true, payload.OK, "json ok")
	assertEqual(t, "APP-T-0002", stringField(payload.Item, "id"), "selected item")
	if len(payload.Skipped) != 1 || payload.Skipped[0].ID != "APP-T-0001" || !containsString(payload.Skipped[0].Reasons, "lane review does not match implementation") {
		t.Fatalf("expected skip reason for wrong lane, got: %+v", payload.Skipped)
	}
}

func TestBlockedTaskRequiresReasonOrDependency(t *testing.T) {
	vault := pickupTestVault(t)
	if err := newV5Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "May block", "risk": "low", "size": "s", "status": "ready"}, "feature"); err != nil {
		t.Fatal(err)
	}

	if err := setStatus(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "status": "blocked", "actor": "codex"}); err == nil {
		t.Fatal("expected blocked status without reason/dependency to fail")
	}
	if err := setStatus(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "status": "blocked", "actor": "codex", "block-reason": "waiting for credentials"}); err != nil {
		t.Fatal(err)
	}
	if code, err := validateCmd(Args{"vault": vault, "json": "true"}); err != nil || code != 0 {
		t.Fatalf("validate failed: code=%d err=%v", code, err)
	}
}

func pickupTestVault(t *testing.T) string {
	t.Helper()
	vault := filepath.Join(t.TempDir(), "vault")
	if err := bootstrapLegacy(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV5Epic(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "App work."}); err != nil {
		t.Fatal(err)
	}
	return vault
}

func pickupV7TestVault(t *testing.T) string {
	t.Helper()
	vault := filepath.Join(t.TempDir(), "vault")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV7Epic(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "App work.", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	for _, domain := range []string{"backend", "frontend"} {
		dir := filepath.Join(vault, "knowledge", "domains", domain)
		if err := ensureDir(dir); err != nil {
			t.Fatal(err)
		}
		if err := writeText(filepath.Join(dir, "INDEX.md"), "# "+domain+"\n"); err != nil {
			t.Fatal(err)
		}
		if err := writeText(filepath.Join(dir, "CANON.md"), "# "+domain+" canon\n"); err != nil {
			t.Fatal(err)
		}
	}
	return vault
}

func mustRunPickupTest(t *testing.T, args Args, fn func(Args) error) {
	t.Helper()
	if err := fn(args); err != nil {
		t.Fatal(err)
	}
}

func setPickupV7TaskFields(t *testing.T, vault, id string, fields map[string]any) {
	t.Helper()
	path := filepath.Join(vault, "work", "tasks", id+".md")
	data, body, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range fields {
		data[key] = value
	}
	body = replaceSection(body, "## Acceptance", "| ID | Outcome | Proof |\n|---|---|---|\n| A1 | Route the requested lane correctly. | Inline verification |")
	body = replaceSection(body, "## Verification", "| Covers | Check | Result | Notes |\n|---|---|---|---|\n| A1 | command: go test ./cmd/tusker -run TestNextV7 -count=1 | pending | Focused routing proof. |")
	content, err := serializeDocument(data, body, frontmatterOrderForType("task"))
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(path, content); err != nil {
		t.Fatal(err)
	}
}
