package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestModelsSetImplicitAndExplicitRevision(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("TUSKER_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))
	t.Setenv("TUSKER_STATE_ROOT", t.TempDir())
	before, err := modelLevelsRead("")
	if err != nil {
		t.Fatal(err)
	}
	args := Args{"scope": "global", "name": "revision-test", "eligible-tiers": "standard", "harness": "codex_exec", "model": "gpt-test", "effort": "high", "preset": "workspace-write-offline", "_no-output": "true"}
	if err := modelsProfileSetCmd(args); err != nil {
		t.Fatal(err)
	}
	after, err := modelLevelsRead("")
	if err != nil || after.Revision == before.Revision || after.Profiles["revision-test"].Model != "gpt-test" {
		t.Fatalf("implicit revision write: %#v, %v", after, err)
	}
	args["if-revision"] = before.Revision
	if err := modelsProfileSetCmd(args); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("stale explicit revision: %v", err)
	}
	args["if-revision"] = after.Revision
	args["model"] = "gpt-test-v2"
	if err := modelsProfileSetCmd(args); err != nil {
		t.Fatal(err)
	}
	final, err := modelLevelsRead("")
	if err != nil || final.Profiles["revision-test"].Model != "gpt-test-v2" {
		t.Fatalf("explicit revision write: %#v, %v", final.Profiles["revision-test"], err)
	}
}
