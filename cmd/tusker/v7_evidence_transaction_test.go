package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestV7EvidenceAddFailureBeforeTaskCommitLeavesNoDanglingReference(t *testing.T) {
	vault := pickupV7TestVault(t)
	mustRunPickupTest(t, Args{
		"vault":    vault,
		"quiet":    "true",
		"epic":     "APP",
		"title":    "Evidence transaction",
		"risk":     "low",
		"priority": "p2",
		"v7":       "true",
	}, newV7Task)

	taskPath := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")
	beforeData, beforeBody, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeRev := stringField(beforeData, "state_rev")
	forced := errors.New("forced evidence task commit failure")
	v7EvidenceBeforeTaskCommitHook = func(string, string) error { return forced }
	t.Cleanup(func() { v7EvidenceBeforeTaskCommitHook = nil })

	args := Args{
		"vault":       vault,
		"quiet":       "true",
		"id":          "APP-T-0001",
		"evidence-id": "APP-T-0001-E-0001",
		"kind":        "automated_test",
		"covers":      "A1",
		"summary":     "Crash-safe evidence.",
	}
	if err := evidenceV7AddCmd(args); !errors.Is(err, forced) {
		t.Fatalf("expected injected task commit failure, got %v", err)
	}

	evidencePath := filepath.Join(vault, "evidence", "APP-T-0001", "APP-T-0001-E-0001.md")
	evidenceData, evidenceBody, err := parseFrontmatterMustRead(evidencePath)
	if err != nil {
		t.Fatal(err)
	}
	if !v7StateRevMatches(evidenceData, evidenceBody, stringField(evidenceData, "state_rev")) {
		t.Fatal("published evidence card has an invalid state_rev")
	}
	afterData, afterBody, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	if stringField(afterData, "state_rev") != beforeRev || afterBody != beforeBody || strings.Contains(afterBody, "[[APP-T-0001-E-0001]]") {
		t.Fatal("failed evidence commit left a dangling task reference or mutated task")
	}

	v7EvidenceBeforeTaskCommitHook = nil
	if err := evidenceV7AddCmd(args); err != nil {
		t.Fatalf("orphaned evidence retry failed: %v", err)
	}
	finalData, finalBody, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(finalBody, "[[APP-T-0001-E-0001]] automated_test") {
		t.Fatal("retry did not commit the evidence reference")
	}
	if !v7StateRevMatches(finalData, finalBody, stringField(finalData, "state_rev")) {
		t.Fatal("task state_rev does not match its committed evidence reference")
	}
}

func TestV7EvidenceArtifactsAreScopedByEvidenceID(t *testing.T) {
	previousWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousWD); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})
	vault := filepath.Join(repo, "vault")
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "v7": "true"}, newV7Epic)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Scoped evidence", "risk": "low", "priority": "p2", "v7": "true"}, newV7Task)
	if err := writeText("proof.txt", "first evidence\n"); err != nil {
		t.Fatal(err)
	}
	baseArgs := Args{
		"vault": vault, "quiet": "true", "id": "APP-T-0001", "kind": "automated_test", "status": "accepted",
		"covers": "A1", "path": "proof.txt", "summary": "Scoped artifact.",
	}
	baseArgs["evidence-id"] = "APP-T-0001-E-0001"
	if err := evidenceV7AddCmd(baseArgs); err != nil {
		t.Fatal(err)
	}
	if err := writeText("proof.txt", "second evidence\n"); err != nil {
		t.Fatal(err)
	}
	baseArgs["evidence-id"] = "APP-T-0001-E-0002"
	if err := evidenceV7AddCmd(baseArgs); err != nil {
		t.Fatal(err)
	}

	firstPath := filepath.Join(vault, "evidence", "APP-T-0001", "APP-T-0001-E-0001.md")
	secondPath := filepath.Join(vault, "evidence", "APP-T-0001", "APP-T-0001-E-0002.md")
	firstData, _, err := parseFrontmatterMustRead(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	secondData, _, err := parseFrontmatterMustRead(secondPath)
	if err != nil {
		t.Fatal(err)
	}
	firstArtifact := filepath.Join(vault, "evidence", "APP-T-0001", "artifacts", "APP-T-0001-E-0001", "proof.txt")
	secondArtifact := filepath.Join(vault, "evidence", "APP-T-0001", "artifacts", "APP-T-0001-E-0002", "proof.txt")
	assertEqual(t, "evidence/APP-T-0001/artifacts/APP-T-0001-E-0001/proof.txt", normalizeList(firstData["artifact_paths"])[0], "first artifact path")
	assertEqual(t, "evidence/APP-T-0001/artifacts/APP-T-0001-E-0002/proof.txt", normalizeList(secondData["artifact_paths"])[0], "second artifact path")
	firstBytes, err := os.ReadFile(firstArtifact)
	if err != nil {
		t.Fatal(err)
	}
	secondBytes, err := os.ReadFile(secondArtifact)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "first evidence\n", string(firstBytes), "first artifact bytes")
	assertEqual(t, "second evidence\n", string(secondBytes), "second artifact bytes")
}

func TestV7EvidenceAddDuplicateWithDifferentContentReturnsAlreadyExists(t *testing.T) {
	vault := pickupV7TestVault(t)
	mustRunPickupTest(t, Args{
		"vault": vault, "quiet": "true", "epic": "APP", "title": "Duplicate evidence", "risk": "low", "priority": "p2", "v7": "true",
	}, newV7Task)
	forced := errors.New("leave evidence orphaned")
	v7EvidenceBeforeTaskCommitHook = func(string, string) error { return forced }
	t.Cleanup(func() { v7EvidenceBeforeTaskCommitHook = nil })
	args := Args{
		"vault": vault, "quiet": "true", "id": "APP-T-0001", "evidence-id": "APP-T-0001-E-0001",
		"kind": "automated_test", "covers": "A1", "summary": "Original content.",
	}
	if err := evidenceV7AddCmd(args); !errors.Is(err, forced) {
		t.Fatalf("expected injected task commit failure, got %v", err)
	}
	v7EvidenceBeforeTaskCommitHook = nil

	args["kind"] = "verification_summary"
	err := evidenceV7AddCmd(args)
	typed, ok := err.(*TuskerError)
	if !ok || typed.Code != errorAlreadyExists || !strings.Contains(err.Error(), "different content") {
		t.Fatalf("expected ALREADY_EXISTS different-content error, got %v", err)
	}
	args["kind"] = "automated_test"
	args["covers"] = "A2"
	err = evidenceV7AddCmd(args)
	typed, ok = err.(*TuskerError)
	if !ok || typed.Code != errorAlreadyExists || !strings.Contains(err.Error(), "different content") {
		t.Fatalf("expected covers mismatch ALREADY_EXISTS error, got %v", err)
	}
	args["covers"] = "A1"
	args["summary"] = "Changed summary."
	err = evidenceV7AddCmd(args)
	typed, ok = err.(*TuskerError)
	if !ok || typed.Code != errorAlreadyExists || !strings.Contains(err.Error(), "different content") {
		t.Fatalf("expected summary mismatch ALREADY_EXISTS error, got %v", err)
	}
	args["summary"] = "Original content."
	args["path"] = "different-proof.txt"
	err = evidenceV7AddCmd(args)
	typed, ok = err.(*TuskerError)
	if !ok || typed.Code != errorAlreadyExists || !strings.Contains(err.Error(), "different content") {
		t.Fatalf("expected artifact mismatch ALREADY_EXISTS error, got %v", err)
	}

	evidencePath := filepath.Join(vault, "evidence", "APP-T-0001", "APP-T-0001-E-0001.md")
	content, err := readText(evidencePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(evidencePath, strings.Replace(content, "Original content.", "Tampered content.", 1)); err != nil {
		t.Fatal(err)
	}
	args["path"] = ""
	err = evidenceV7AddCmd(args)
	typed, ok = err.(*TuskerError)
	if !ok || typed.Code != errorAlreadyExists || !strings.Contains(err.Error(), "different content") {
		t.Fatalf("expected state_rev mismatch ALREADY_EXISTS error, got %v", err)
	}
}

func TestWriteNewV7EvidenceDocumentRejectsExistingAndSweepsStaleTemps(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "APP-T-0001-E-0001.md")
	if err := writeText(path, "existing\n"); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(dir, ".APP-T-0001-E-0001.md.tmp-stale")
	if err := writeText(stale, "stale\n"); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}

	err := writeNewV7EvidenceDocument(path, "replacement\n")
	typed, ok := err.(*TuskerError)
	if !ok || typed.Code != errorAlreadyExists || typed.Path != path {
		t.Fatalf("expected ALREADY_EXISTS for %s, got %v", path, err)
	}
	if fileExists(stale) {
		t.Fatal("stale evidence temp was not swept")
	}
	content, err := readText(path)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "existing\n", content, "existing evidence document")
}
