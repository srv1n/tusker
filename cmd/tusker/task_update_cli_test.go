package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestTaskUpdateCLIImplicitAndExplicitRevision(t *testing.T) {
	vault := v7DirectTestVault(t)
	body := directAuthoringBodyPath(t, vault, "body.md", "# Body\n")
	if err := newAuthoredV7Task(Args{"owned-paths": "cmd/tusker", "vault": vault, "quiet": "true", "title": "Before", "work-level": "standard", "body-file": body, "spec-refs": ".tusker/specs/delivery.md"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(vault, "work", "tasks", "TSK-T-0001.md")
	before, _, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	rev := stringField(before, "state_rev")
	if err := taskUpdateCLICmd(Args{"vault": vault, "quiet": "true", "_pos0": "TSK-T-0001", "title": "After", "by": "agent:builder"}); err != nil {
		t.Fatal(err)
	}
	if err := taskUpdateCLICmd(Args{"vault": vault, "quiet": "true", "_pos0": "TSK-T-0001", "if-revision": rev, "title": "Stale", "by": "agent:builder"}); err == nil || !strings.Contains(err.Error(), "changed since it was loaded") {
		t.Fatalf("stale explicit revision: %v", err)
	}
	after, _, err := parseFrontmatterMustRead(path)
	if err != nil || stringField(after, "title") != "After" || stringField(after, "state_rev") == rev {
		t.Fatalf("task after update: %#v, %v", after, err)
	}
	if err := taskUpdateCLICmd(Args{"vault": vault, "quiet": "true", "_pos0": "TSK-T-0001", "if-revision": stringField(after, "state_rev"), "title": "Explicit", "by": "agent:builder"}); err != nil {
		t.Fatal(err)
	}
	final, _, err := parseFrontmatterMustRead(path)
	if err != nil || stringField(final, "title") != "Explicit" {
		t.Fatalf("explicit update: %#v, %v", final, err)
	}
}
