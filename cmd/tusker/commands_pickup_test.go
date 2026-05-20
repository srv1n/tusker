package main

import (
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
