package main

import (
	"path/filepath"
	"testing"
)

func TestTaskAuthoringMetadataNewTaskOmitsEmptyOptionalFields(t *testing.T) {
	vault := pickupV7TestVault(t)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Minimal task"}, newV7Task)

	path := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	data, body, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"architect", "origin", "runner_profile", "concurrency_group", "peer_contacts", "domains", "gates", "dependencies", "evidence_required", "knowledge_nodes", "owned_paths",
	} {
		if _, ok := data[key]; ok {
			t.Fatalf("minimal task should omit empty %s: %#v", key, data[key])
		}
	}
	for _, key := range []string{"risk", "priority", "size"} {
		if _, ok := data[key]; ok {
			t.Fatalf("minimal task should omit unset %s: %#v", key, data[key])
		}
	}
	assertEqual(t, "standard", stringField(data, "work_level"), "default authored work level")
	assertEqual(t, "inline", stringField(data, "proof_mode"), "default proof mode")
	assertEqual(t, []string{"focused_test", "broad_test"}, normalizeList(data["proof_required"]), "default proof requirements")
	assertEqual(t, 0, intField(data, "evidence_budget"), "zero evidence budget")
	assertEqual(t, false, boolField(data, "raw_artifacts_allowed"), "default raw artifact policy")
	assertEqual(t, "task", stringField(data, "next_source"), "default next source")
	assertEqual(t, "APP-T-0001", stringField(data, "next_ref"), "default next ref")
	if !v7StateRevMatches(data, body, stringField(data, "state_rev")) {
		t.Fatalf("new task state_rev does not match trimmed content")
	}
}

func TestTaskAuthoringMetadataInfersExplicitWorkLevelFromComplexity(t *testing.T) {
	for complexity, want := range map[string]string{"routine": "light", "standard": "standard", "complex": "demanding", "frontier": "demanding"} {
		if got := taskWorkLevel("", complexity); got != want {
			t.Fatalf("complexity %s work level=%s want %s", complexity, got, want)
		}
	}
	if got := taskWorkLevel("light", "frontier"); got != "light" {
		t.Fatalf("explicit work level lost: %s", got)
	}
}

func TestTaskAuthoringMetadataPreservesSuppliedFieldsAndCAS(t *testing.T) {
	vault := pickupV7TestVault(t)
	mustV7Proof(t, Args{
		"vault": vault, "quiet": "true", "epic": "APP", "id": "APP-T-0001", "title": "Configured task",
		"architect": "execution:architect", "origin": "task:APP-T-0009", "peers": "reviewer=task:APP-T-0002",
		"domains": "project,cli", "gates": "APP-G-0001", "dependencies": "APP-T-0009:hard",
		"evidence-required": "automated_test", "owned-paths": "cmd/tusker/commands_v7.go",
		"raw-artifacts-allowed": "false", "evidence-budget": "0", "next-source": "custom", "next-ref": "custom-ref",
	}, newV7Task)

	path := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	data, body, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "execution:architect", stringField(data, "architect"), "architect")
	assertEqual(t, "task:APP-T-0009", stringField(data, "origin"), "origin")
	assertEqual(t, map[string]string{"reviewer": "task:APP-T-0002"}, normalizeStringMap(data["peer_contacts"]), "peer contacts")
	assertEqual(t, []string{"project", "cli"}, normalizeList(data["domains"]), "domains")
	assertEqual(t, []string{"APP-G-0001"}, normalizeList(data["gates"]), "gates")
	assertEqual(t, []string{"APP-T-0009:hard"}, normalizeList(data["dependencies"]), "dependencies")
	assertEqual(t, []string{"automated_test"}, normalizeList(data["evidence_required"]), "required evidence")
	assertEqual(t, []string{"cmd/tusker/commands_v7.go"}, normalizeList(data["owned_paths"]), "owned paths")
	assertEqual(t, false, boolField(data, "raw_artifacts_allowed"), "explicit false raw artifact policy")
	assertEqual(t, 0, intField(data, "evidence_budget"), "explicit zero evidence budget")
	assertEqual(t, "custom", stringField(data, "next_source"), "explicit next source")
	assertEqual(t, "custom-ref", stringField(data, "next_ref"), "explicit next ref")

	baseRev := stringField(data, "state_rev")
	updated := cloneMap(data)
	updated["next_action"] = "Continue the configured task."
	nextRev, err := saveV7DocumentCAS(path, updated, body, v7FrontmatterOrder["task"], baseRev)
	if err != nil {
		t.Fatal(err)
	}
	if nextRev == baseRev {
		t.Fatalf("CAS write did not advance state_rev: %q", nextRev)
	}
	if _, err := saveV7DocumentCAS(path, data, body, v7FrontmatterOrder["task"], baseRev); err == nil {
		t.Fatal("stale CAS write unexpectedly succeeded")
	}
	after, afterBody, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 0, intField(after, "evidence_budget"), "CAS-preserved zero evidence budget")
	if !v7StateRevMatches(after, afterBody, stringField(after, "state_rev")) {
		t.Fatalf("CAS-written task state_rev does not match content")
	}
}

func TestTaskAuthoringMetadataWaveTaskOmitsEmptyFields(t *testing.T) {
	vault := pickupV7TestVault(t)
	path := writeDirectIntakeRequest(t, vault, map[string]any{
		"schema":      "tusker.wave-authoring/v1",
		"request_key": "metadata",
		"title":       "Metadata",
		"outcome":     "Direct wave task omits empty optional fields.",
		"tasks": []map[string]any{
			{"key": "only", "title": "Only", "work_level": "standard", "epic": "APP", "body": "# Only\n\nDo it.\n"},
		},
	})
	if err := waveV7CreateCmd(Args{"vault": vault, "file": path, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}

	taskPath := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	data, body, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"architect", "origin", "runner_profile", "concurrency_group", "peer_contacts", "domains", "gates", "dependencies", "knowledge_nodes", "owned_paths", "evidence_required",
	} {
		if _, ok := data[key]; ok {
			t.Fatalf("new authored task should omit empty %s: %#v", key, data[key])
		}
	}
	assertEqual(t, false, boolField(data, "raw_artifacts_allowed"), "authored raw artifact policy")
	assertEqual(t, 0, intField(data, "evidence_budget"), "authored evidence budget")
	assertEqual(t, "task", stringField(data, "next_source"), "authored next source")
	assertEqual(t, "APP-T-0001", stringField(data, "next_ref"), "authored next ref")
	if !v7StateRevMatches(data, body, stringField(data, "state_rev")) {
		t.Fatalf("new authored task state_rev does not match trimmed content")
	}
}
