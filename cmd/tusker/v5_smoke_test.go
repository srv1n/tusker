package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestV5GreenfieldTaskDocsGate(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")

	must := func(args Args, fn func(Args) error) {
		t.Helper()
		if err := fn(args); err != nil {
			t.Fatal(err)
		}
	}

	must(Args{"vault": vault, "quiet": "true"}, bootstrap)
	assertExists(t, filepath.Join(vault, "_config", "docs-map.yaml"))
	assertExists(t, filepath.Join(vault, "WORKFLOW.md"))
	assertExists(t, filepath.Join(vault, "docs", "reference", "cli.md"))

	must(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App foundation", "summary": "Build the v5 app foundation."}, newV5Epic)
	must(Args{
		"vault":     vault,
		"quiet":     "true",
		"epic":      "APP",
		"title":     "Wire v5 task path",
		"risk":      "medium",
		"size":      "m",
		"domains":   "cli,docs",
		"doc-nodes": "reference/cli",
	}, func(args Args) error { return newV5Task(args, "feature") })

	taskPath := filepath.Join(vault, "epics", "APP", "APP-T-0001.md")
	assertExists(t, taskPath)
	if code, err := validateCmd(Args{"vault": vault, "json": "true"}); err != nil || code != 0 {
		t.Fatalf("validate failed: code=%d err=%v", code, err)
	}

	if err := verifyV5Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "by": "agent"}); err == nil {
		t.Fatal("expected verify to fail before review status")
	}
	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "status": "review", "actor": "agent"}, setStatus)
	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "by": "agent"}, verifyV5Cmd)
	if err := closeV5Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "by": "agent"}); err == nil {
		t.Fatal("expected close to fail before docs impact resolution")
	}
	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "node": "reference/cli", "by": "agent", "reason": "CLI doc unchanged for smoke."}, docsImpactWaiveCmd)
	must(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "by": "agent"}, closeV5Cmd)
	if code, err := validateCmd(Args{"vault": vault, "json": "true"}); err != nil || code != 0 {
		t.Fatalf("validate after close failed: code=%d err=%v", code, err)
	}
}

func TestV5RejectsUnsafeDocNode(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	err := newV5Doc(Args{"vault": vault, "title": "Escape", "node": "../../outside", "quiet": "true"})
	if err == nil {
		t.Fatal("expected unsafe doc node to be rejected")
	}
}

func TestV5BaseViewsScopeToEmbeddingFolder(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"Epics.base", "Tasks.base", "BugTasks.base", "Docs.base"} {
		content, err := readText(filepath.Join(vault, "_system", "views", name))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(content, `'this.file.folder == "" || file.inFolder(this.file.folder)'`) {
			t.Fatalf("%s is missing shared-vault folder scope filter", name)
		}
	}
}

func TestV5EpicDoneRejectsUnfinishedTasks(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV5Epic(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "App work."}); err != nil {
		t.Fatal(err)
	}
	if err := newV5Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Open task", "risk": "low", "size": "s"}, "feature"); err != nil {
		t.Fatal(err)
	}
	err := setStatus(Args{"vault": vault, "quiet": "true", "id": "APP", "status": "done", "actor": "agent"})
	if err == nil {
		t.Fatal("expected epic done to reject unfinished v5 task")
	}
}

func TestV5DocsCheckRequiresDocsMap(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV5Epic(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "App work."}); err != nil {
		t.Fatal(err)
	}
	if err := newV5Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Docs task", "risk": "low", "size": "s", "doc-nodes": "reference/cli"}, "feature"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(vault, "_config", "docs-map.yaml")); err != nil {
		t.Fatal(err)
	}
	err := docsImpactCheckCmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001"})
	if err == nil {
		t.Fatal("expected docs check to require docs-map")
	}
}

func TestV5DocsNoopResolvesDocsGate(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV5Epic(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "App work."}); err != nil {
		t.Fatal(err)
	}
	if err := newV5Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Docs noop task", "risk": "low", "size": "s", "doc-nodes": "reference/cli"}, "feature"); err != nil {
		t.Fatal(err)
	}
	if err := setStatus(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "status": "review", "actor": "agent"}); err != nil {
		t.Fatal(err)
	}
	if err := verifyV5Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "by": "agent"}); err != nil {
		t.Fatal(err)
	}
	if err := docsImpactNoopCmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "node": "reference/cli", "by": "agent", "reason": "Already current."}); err != nil {
		t.Fatal(err)
	}
	if err := closeV5Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "by": "agent"}); err != nil {
		t.Fatal(err)
	}
	data, _, err := parseFrontmatterMustRead(filepath.Join(vault, "epics", "APP", "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	resolutions := anySlice(data["docs_resolution"])
	if len(resolutions) != 1 {
		t.Fatalf("expected one docs resolution, got %#v", resolutions)
	}
	assertEqual(t, "verified_noop", stringValue(resolutions[0].(map[string]any)["status"]), "docs resolution status")
}

func TestV5CreatesSequentialTaskIDsAndReviewStatus(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	must := func(args Args, fn func(Args) error) {
		t.Helper()
		if err := fn(args); err != nil {
			t.Fatal(err)
		}
	}

	must(Args{"vault": vault, "quiet": "true"}, bootstrap)
	must(Args{"vault": vault, "quiet": "true", "acronym": "MEM", "title": "Memory", "summary": "Build memory features."}, newV5Epic)
	must(Args{"vault": vault, "quiet": "true", "epic": "MEM", "title": "Add memory", "risk": "low", "size": "s"}, func(args Args) error {
		return newV5Task(args, "feature")
	})
	must(Args{"vault": vault, "quiet": "true", "epic": "MEM", "title": "Document memory", "kind": "docs", "risk": "low", "size": "s"}, func(args Args) error {
		return newV5Task(args, "feature")
	})
	must(Args{"vault": vault, "quiet": "true", "id": "MEM-T-0002", "status": "review", "actor": "agent"}, setStatus)

	assertExists(t, filepath.Join(vault, "epics", "MEM", "MEM.md"))
	assertExists(t, filepath.Join(vault, "epics", "MEM", "MEM-T-0001.md"))
	assertExists(t, filepath.Join(vault, "epics", "MEM", "MEM-T-0002.md"))

	data, _, err := parseFrontmatterMustRead(filepath.Join(vault, "epics", "MEM", "MEM-T-0002.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "tusker.task/v5", data["schema"], "task schema")
	assertEqual(t, "task", data["type"], "task type")
	assertEqual(t, "MEM-T-0002", data["id"], "task id")
	assertEqual(t, "docs", data["kind"], "task kind")
	assertEqual(t, "review", data["status"], "task status")

	if code, err := validateCmd(Args{"vault": vault, "json": "true"}); err != nil || code != 0 {
		t.Fatalf("validate v5 vault failed: code=%d err=%v", code, err)
	}
}

func TestV5RejectsDuplicateDocNode(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV5Doc(Args{"vault": vault, "quiet": "true", "title": "Memory guide", "node": "guides/memory", "publish-description": "Memory guide."}); err != nil {
		t.Fatal(err)
	}
	if err := newV5Doc(Args{"vault": vault, "quiet": "true", "title": "Duplicate memory guide", "node": "guides/memory", "publish-description": "Duplicate memory guide."}); err == nil {
		t.Fatal("expected duplicate doc node to be rejected")
	}
	assertExists(t, filepath.Join(vault, "docs", "guides", "memory.md"))
}

func TestV5NewDocDefaultsPublishDescription(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV5Doc(Args{"vault": vault, "quiet": "true", "title": "Scratch guide", "node": "developer/scratch"}); err != nil {
		t.Fatal(err)
	}
	data, _, err := parseFrontmatterMustRead(filepath.Join(vault, "docs", "developer", "scratch.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "Scratch guide.", stringField(data, "publish_description"), "publish description")
	if code, err := validateCmd(Args{"vault": vault, "json": "true"}); err != nil || code != 0 {
		t.Fatalf("validate failed after new doc: code=%d err=%v", code, err)
	}
}
