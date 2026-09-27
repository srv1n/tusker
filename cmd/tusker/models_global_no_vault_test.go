package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestModelsGlobalWithoutVault(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("TUSKER_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))
	t.Setenv("TUSKER_STATE_ROOT", t.TempDir())
	report, err := modelLevelsRead("")
	if err != nil || len(report.Levels) != 3 || len(report.Profiles) == 0 {
		t.Fatalf("global report: %#v, %v", report, err)
	}
	if err := modelsCmd(Args{"json": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := runnerCatalogCmd(Args{"json": "true", "bundled": "true"}); err != nil {
		t.Fatal(err)
	}
	args := Args{"scope": "global", "name": "no-vault", "eligible-tiers": "standard", "harness": "codex_exec", "model": "gpt-test", "effort": "high", "preset": "workspace-write-offline", "if-revision": report.Revision, "_no-output": "true"}
	if err := modelsProfileSetCmd(args); err != nil {
		t.Fatal(err)
	}
	report, err = modelLevelsRead("")
	if err != nil || report.Profiles["no-vault"].Model != "gpt-test" {
		t.Fatalf("profile set: %#v, %v", report.Profiles["no-vault"], err)
	}
	set := Args{"scope": "global", "level": "standard", "lane": "execute", "profiles": "no-vault", "if-revision": report.Revision, "_no-output": "true"}
	if err := modelsSetCmd(set); err != nil {
		t.Fatal(err)
	}
	report, err = modelLevelsRead("")
	if err != nil || strings.Join(report.Levels[1].Execute.Profiles, ",") != "no-vault" {
		t.Fatalf("global level: %#v, %v", report.Levels, err)
	}
	reset := Args{"scope": "global", "level": "standard", "lane": "execute", "if-revision": report.Revision, "_no-output": "true"}
	if err := modelsResetCmd(reset); err != nil {
		t.Fatal(err)
	}
	report, err = modelLevelsRead("")
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"profile-disable", "profile-enable", "profile-remove"} {
		if err := modelsProfileLifecycleCmd(Args{"scope": "global", "name": "no-vault", "if-revision": report.Revision, "_no-output": "true"}, action); err != nil {
			t.Fatalf("%s: %v", action, err)
		}
		report, err = modelLevelsRead("")
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, ok := report.Profiles["no-vault"]; ok {
		t.Fatal("profile was not removed")
	}
	for _, command := range []func(Args) error{modelsSetCmd, modelsResetCmd} {
		if err := command(Args{"scope": "project"}); err == nil || !strings.Contains(err.Error(), "requires a Tusker vault") {
			t.Fatalf("project scope: %v", err)
		}
	}
}
