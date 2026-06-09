package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

type externalLoopAdvancePayload struct {
	OK           bool                      `json:"ok"`
	ExternalLoop externalLoopAdvanceResult `json:"external_loop"`
}

type externalLoopStatusPayload struct {
	OK           bool                     `json:"ok"`
	ExternalLoop externalLoopStatusReport `json:"external_loop"`
}

func TestAutomationAdvanceExternalCollectsRecordsPolicyAndIsIdempotent(t *testing.T) {
	vault := automationTestVault(t)
	repoRoot := filepath.Dir(vault)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Advance external", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	registerAutomationTestProject(t, vault)

	jobID := "cgpt_policy_patch"
	fetchCommand := seedExternalFetchFixture(t, repoRoot, jobID, map[string]string{
		"fix.patch": "diff --git a/README.md b/README.md\n--- a/README.md\n+++ b/README.md\n@@ -1 +1 @@\n-old\n+new\n",
		"notes.md":  "# Review\n\nPatch is scoped.\n",
	})

	first := runAdvanceExternalForTest(t, vault, Args{
		"id":            "APP-T-0001",
		"job":           jobID,
		"runner":        "chatgpt-browser",
		"fetch-command": fetchCommand,
		"json":          "true",
	})
	assertEqual(t, true, first.OK, "json ok")
	assertEqual(t, externalLoopActionApplyPatch, first.ExternalLoop.NextAction, "next action")
	assertEqual(t, true, first.ExternalLoop.Dispatchable, "dispatchable")
	assertEqual(t, true, first.ExternalLoop.EventCreated, "event created")
	assertEqual(t, 0, first.ExternalLoop.Counters.Cycles, "initial cycles")
	assertEqual(t, 1, first.ExternalLoop.ProjectedCounters.Cycles, "projected cycles")
	assertEqual(t, 1, first.ExternalLoop.ProjectedCounters.ExternalThreads, "projected threads")
	if len(first.ExternalLoop.DispatchCommand) == 0 {
		t.Fatalf("expected dispatch command, got %#v", first.ExternalLoop)
	}
	if first.ExternalLoop.Collect == nil || len(first.ExternalLoop.Collect.Patches) != 1 {
		t.Fatalf("expected embedded collect result with one patch, got %#v", first.ExternalLoop.Collect)
	}

	second := runAdvanceExternalForTest(t, vault, Args{
		"id":            "APP-T-0001",
		"job":           jobID,
		"runner":        "chatgpt-browser",
		"fetch-command": fetchCommand,
		"json":          "true",
	})
	assertEqual(t, false, second.ExternalLoop.EventCreated, "idempotent event")
	assertEqual(t, 1, second.ExternalLoop.Counters.Cycles, "cycle count after prior collect")
	assertEqual(t, 1, second.ExternalLoop.ProjectedCounters.Cycles, "idempotent projected cycles")

	status := runExternalLoopStatusForTest(t, vault, "APP-T-0001")
	assertEqual(t, true, status.OK, "status ok")
	assertEqual(t, 1, status.ExternalLoop.Counters.Events, "status events")
	assertEqual(t, []string{jobID}, status.ExternalLoop.Counters.DistinctJobIDs, "distinct jobs")
}

func TestAutomationAdvanceExternalRepairContinuationCapEscalates(t *testing.T) {
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Repair cap", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	registerAutomationTestProject(t, vault)

	first := runAdvanceExternalForTest(t, vault, Args{"id": "APP-T-0001", "event": "apply_failed", "job": "cgpt-repair", "attempt-id": "apply-1", "reason": "first conflict", "json": "true"})
	assertEqual(t, true, first.OK, "first repair ok")
	assertEqual(t, externalLoopActionContinueThreadOnFailure, first.ExternalLoop.NextAction, "first action")

	second := runAdvanceExternalForTest(t, vault, Args{"id": "APP-T-0001", "event": "apply_failed", "job": "cgpt-repair", "attempt-id": "apply-2", "reason": "second conflict", "json": "true"})
	assertEqual(t, true, second.OK, "second repair ok")
	assertEqual(t, externalLoopActionContinueThreadOnFailure, second.ExternalLoop.NextAction, "second action")

	third := runAdvanceExternalForTest(t, vault, Args{"id": "APP-T-0001", "event": "apply_failed", "job": "cgpt-repair", "attempt-id": "apply-3", "reason": "third conflict", "json": "true"})
	assertEqual(t, false, third.OK, "third repair escalates")
	assertEqual(t, externalLoopActionEscalateHuman, third.ExternalLoop.NextAction, "third action")
	if !containsSubstring(third.ExternalLoop.Blockers, "repair continuation cap") {
		t.Fatalf("expected repair cap blocker, got %#v", third.ExternalLoop.Blockers)
	}
}

func TestAutomationAdvanceExternalExternalThreadCapEscalates(t *testing.T) {
	vault := automationTestVault(t)
	repoRoot := filepath.Dir(vault)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Thread cap", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	registerAutomationTestProject(t, vault)

	firstFetch := seedExternalFetchFixture(t, repoRoot, "cgpt-thread-1", map[string]string{"thread-1.patch": "diff --git a/a b/a\n"})
	first := runAdvanceExternalForTest(t, vault, Args{"id": "APP-T-0001", "job": "cgpt-thread-1", "runner": "chatgpt-browser", "fetch-command": firstFetch, "max-external-threads": "1", "json": "true"})
	assertEqual(t, true, first.OK, "first thread ok")

	secondFetch := seedExternalFetchFixture(t, repoRoot, "cgpt-thread-2", map[string]string{"thread-2.patch": "diff --git a/b b/b\n"})
	second := runAdvanceExternalForTest(t, vault, Args{"id": "APP-T-0001", "job": "cgpt-thread-2", "runner": "chatgpt-browser", "fetch-command": secondFetch, "max-external-threads": "1", "json": "true"})
	assertEqual(t, false, second.OK, "second thread escalates")
	assertEqual(t, externalLoopActionEscalateHuman, second.ExternalLoop.NextAction, "second action")
	if !containsSubstring(second.ExternalLoop.Blockers, "external thread cap") {
		t.Fatalf("expected thread cap blocker, got %#v", second.ExternalLoop.Blockers)
	}
}

func runAdvanceExternalForTest(t *testing.T, vault string, args Args) externalLoopAdvancePayload {
	t.Helper()
	args["vault"] = vault
	args["json"] = "true"
	output := captureStdout(t, func() {
		if err := automationAdvanceExternalCmd(args); err != nil {
			t.Fatal(err)
		}
	})
	var payload externalLoopAdvancePayload
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatalf("parse advance output: %v\n%s", err, output)
	}
	return payload
}

func runExternalLoopStatusForTest(t *testing.T, vault, taskID string) externalLoopStatusPayload {
	t.Helper()
	output := captureStdout(t, func() {
		if err := automationExternalLoopCmd(Args{"vault": vault, "id": taskID, "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var payload externalLoopStatusPayload
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatalf("parse status output: %v\n%s", err, output)
	}
	return payload
}

func seedExternalFetchFixture(t *testing.T, repoRoot, jobID string, files map[string]string) string {
	t.Helper()
	artifactDir := writeExternalFetchFiles(t, files)
	var paths []string
	for name := range files {
		paths = append(paths, filepath.Join(artifactDir, filepath.FromSlash(name)))
	}
	sort.Strings(paths)
	payload, err := json.Marshal(map[string]any{
		"job_id":       jobID,
		"artifact_dir": artifactDir,
		"files":        paths,
	})
	if err != nil {
		t.Fatal(err)
	}
	payloadPath := filepath.Join(t.TempDir(), "external-fetch.json")
	if err := os.WriteFile(payloadPath, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	return "cat " + strconv.Quote(payloadPath)
}

func containsSubstring(items []string, needle string) bool {
	for _, item := range items {
		if strings.Contains(item, needle) {
			return true
		}
	}
	return false
}
