package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestOnboardingPlanV2CanonicalArtifacts(t *testing.T) {
	root := filepath.Join("..", "..")
	artifacts := map[string]string{}
	for _, rel := range []string{
		"skills/tusker/assets/templates/onboard-prompt.md",
		"skills/tusker/references/REPO_ONBOARDING.md",
		"docs/specs/10-repo-bootstrap-and-existing-repo-onboarding.md",
	} {
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		artifacts[rel] = string(raw)
	}

	for rel, text := range artifacts {
		for _, required := range []string{
			"tusker.onboard_plan/v2",
			"tusker.delivery-plan/v2",
			"source_key",
			"held",
			"disarmed",
			"doctor",
			"product review",
			"fingerprint-bound Start",
			"legacy",
			"migration",
			"non-executable",
		} {
			if !strings.Contains(text, required) {
				t.Fatalf("%s missing V2 onboarding guardrail %q", rel, required)
			}
		}
		for _, forbidden := range []string{
			"\n  tasks.yaml",
			"## tasks.yaml",
			"status: backlog",
			"readiness: held",
			"human confirms command",
			"check: \"<exact command if known",
		} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("%s retained executable or fake-proof onboarding content %q", rel, forbidden)
			}
		}
	}
}

func TestOnboardingPlanV2SkeletonUsesSourceKeysAndExactProof(t *testing.T) {
	const skeleton = `schema: tusker.delivery-plan/v2
scope: packet-backed-example/v1
title: Packet-backed example
spec_refs: [docs/specs/example.md]
context_fingerprint: sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
epic_contract:
  source_key: packet-backed-example
  acronym_hint: PBE
  title: Packet-backed example
requirements:
  - id: R1
    outcome: The exact packet-evidenced command is documented.
tasks:
  - source_key: document-command
    requirement_refs: [R1]
    title: Document command
    outcome: Documentation contains the packet-evidenced command.
    acceptance:
      - id: A1
        outcome: The command matches the CI evidence exactly.
    verification:
      - covers: A1
        check: "command: go test ./example -run '^TestExample$' -count=1"
    artifact:
      kind: documentation
      path: README.md
      summary: Documents the exact command.
      acceptance_ids: [A1]
    dependencies: []
`
	var plan map[string]any
	if err := yaml.Unmarshal([]byte(skeleton), &plan); err != nil {
		t.Fatal(err)
	}
	if plan["schema"] != deliveryPlanV2Schema {
		t.Fatalf("schema = %v, want %s", plan["schema"], deliveryPlanV2Schema)
	}
	contextFingerprint, ok := plan["context_fingerprint"].(string)
	if !ok || !deliveryContextFingerprintValid(contextFingerprint) {
		t.Fatalf("context_fingerprint = %v, want sha256:<64 lowercase hex>", plan["context_fingerprint"])
	}
	if _, ok := plan["epic_contract"].(map[string]any)["source_key"]; !ok {
		t.Fatal("V2 skeleton has no epic source_key")
	}
	tasks := plan["tasks"].([]any)
	task := tasks[0].(map[string]any)
	for _, field := range []string{"source_key", "requirement_refs", "acceptance", "verification", "artifact", "dependencies"} {
		if _, ok := task[field]; !ok {
			t.Fatalf("V2 skeleton task missing %s", field)
		}
	}
	if strings.Contains(skeleton, "-T-") || strings.Contains(skeleton, "state_rev:") || strings.Contains(skeleton, "readiness:") {
		t.Fatal("V2 skeleton contains a final identity or lifecycle field")
	}
}
