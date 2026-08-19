package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestV7ProposalChangeSpecRefsUsesAllowlistAndEmitsEvent(t *testing.T) {
	repo := t.TempDir()
	vault := filepath.Join(repo, ".tusker")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := ensureDir(filepath.Join(repo, "docs", "specs")); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(repo, "docs", "specs", "example.md"), "# Governing spec\n"); err != nil {
		t.Fatal(err)
	}
	if err := newV7Epic(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV7Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Task", "risk": "low", "priority": "p2", "v7": "true"}); err != nil {
		t.Fatal(err)
	}

	if err := applyV7ChangeProposal(vault, "APP-T-0001", "task", map[string]any{"title": "not allowed"}, "human:sarav", "APP-P-0001"); err == nil || !strings.Contains(err.Error(), "only supports spec_refs") {
		t.Fatalf("expected narrow change allowlist refusal, got %v", err)
	}

	if err := proposalV7NewCmd(Args{
		"vault": vault, "quiet": "true", "action": "change", "target": "APP-T-0001",
		"set": "spec_refs=docs/specs/example.md", "by": "agent:codex",
	}); err != nil {
		t.Fatal(err)
	}
	if err := proposalV7TransitionCmd(Args{"vault": vault, "quiet": "true", "id": "APP-P-0001", "by": "human:sarav"}, "accepted"); err != nil {
		t.Fatal(err)
	}
	if err := proposalV7ApplyCmd(Args{"vault": vault, "quiet": "true", "id": "APP-P-0001", "by": "human:sarav", "local": "true"}); err != nil {
		t.Fatal(err)
	}

	task, err := resolveV7Note(vault, "APP-T-0001", "task")
	if err != nil {
		t.Fatal(err)
	}
	if got := normalizeList(task.Data["spec_refs"]); len(got) != 1 || got[0] != "docs/specs/example.md" {
		t.Fatalf("expected spec_refs mutation, got %#v", task.Data["spec_refs"])
	}
	store := v7MarkdownStore{VaultPath: vault}
	events, err := store.GetEvents(context.Background(), v7EventScope{ObjectID: "APP-T-0001", EventKind: "updated"})
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if stringField(event.Payload, "source") == "proposal:APP-P-0001" {
			return
		}
	}
	t.Fatalf("expected evented proposal change, got %#v", events)
}
