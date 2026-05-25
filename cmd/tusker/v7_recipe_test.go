package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestV7ProofRecipeSuggestsScopedCommandsFromConfig(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Recipe policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "CLI proof recipe", "risk": "medium", "priority": "p2", "domains": "cli", "v7": "true"}, newV7Task)
	writeRecipeConfig(t, vault, `
recipes:
  - id: cli-owned-go
    title: Owned CLI Go files
    domains: [cli]
    risks: [low, medium, high]
    file_globs: ["cmd/tusker/*.go"]
    ownership_scope: owned_files
    package_scope: ./cmd/tusker
    commands:
      - go test ./cmd/tusker -run TestV7Proof -count=1
    expected_noise:
      - full ./... may include unrelated worker edits
  - id: docs-only
    domains: [docs]
    commands:
      - npm run docs:check
`)

	output := captureStdout(t, func() {
		if err := proofV7RecipeCmd(Args{"vault": vault, "id": "APP-T-0001", "files": "cmd/tusker/v7_proof_cmd.go", "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var report v7VerificationRecipeReport
	if err := json.Unmarshal([]byte(output), &report); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "APP-T-0001", report.TaskID, "task id")
	assertEqual(t, []string{"cmd/tusker/v7_proof_cmd.go"}, report.TouchedFiles, "touched files")
	if len(report.Recipes) != 1 {
		t.Fatalf("expected one matching recipe, got %#v", report.Recipes)
	}
	assertEqual(t, "cli-owned-go", report.Recipes[0].ID, "recipe id")
	assertEqual(t, "owned_files", report.Recipes[0].OwnershipScope, "ownership scope")
	assertEqual(t, []string{"go test ./cmd/tusker -run TestV7Proof -count=1"}, report.Recipes[0].Commands, "commands")
	if !strings.Contains(report.Policy, "Scoped recipes are acceptable") {
		t.Fatalf("policy should explain scoped recipe use, got %q", report.Policy)
	}
}

func TestV7ProofRecipeTextIncludesExpectedNoise(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Recipe policy.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Schema proof recipe", "risk": "low", "priority": "p2", "domains": "schema", "v7": "true"}, newV7Task)
	writeRecipeConfig(t, vault, `
recipes:
  - id: schema-unit
    domains: [schema]
    file_globs: ["internal/v7schema/*.go"]
    ownership_scope: package
    package_scope: ./internal/v7schema
    commands:
      - go test ./internal/v7schema -count=1
    expected_noise:
      - broad CLI tests may fail on unrelated command work
`)

	output := captureStdout(t, func() {
		if err := proofV7RecipeCmd(Args{"vault": vault, "id": "APP-T-0001", "files": "internal/v7schema/schema.go"}); err != nil {
			t.Fatal(err)
		}
	})
	for _, want := range []string{
		"Verification recipes for APP-T-0001",
		"schema-unit",
		"scope: package",
		"package: ./internal/v7schema",
		"expected noise: broad CLI tests may fail on unrelated command work",
		"$ go test ./internal/v7schema -count=1",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("recipe output missing %q:\n%s", want, output)
		}
	}
}

func writeRecipeConfig(t *testing.T, vault, body string) {
	t.Helper()
	if err := writeText(filepath.Join(vault, "verification-recipes.yaml"), strings.TrimSpace(body)+"\n"); err != nil {
		t.Fatal(err)
	}
}
