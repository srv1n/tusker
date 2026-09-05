package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func deliveryAmendmentPlan(t *testing.T, vault string) (deliveryPlanV2, string, string) {
	t.Helper()
	if err := writeText(filepath.Join(vault, "specs", "delivery.md"), "---\nsubject: delivery\npart_of: overview\n---\n# Delivery\n\n## Work streams\n"); err != nil {
		t.Fatal(err)
	}
	plan := validDeliveryPlanV2()
	plan.HumanGates = nil
	path := writeDeliveryV2TestPlan(t, vault, plan)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return plan, path, deliveryFingerprint(raw)
}

func deliveryWaveForScope(t *testing.T, vault, scope string) Note {
	t.Helper()
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	for _, wave := range idx.Waves {
		if stringField(wave.Data, "delivery_plan_scope") == scope {
			return wave
		}
	}
	t.Fatalf("no wave found for delivery scope %q", scope)
	return Note{}
}

func deliveryWaveTaskIDsBySource(t *testing.T, vault, waveID string) map[string]string {
	t.Helper()
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for id, task := range idx.Tasks {
		if stringField(task.Data, "wave") == waveID {
			out[stringField(task.Data, "delivery_source_key")] = id
		}
	}
	return out
}

func TestDeliveryImportAllowsHeldWaveAmendmentAfterDefaultAdvance(t *testing.T) {
	repo, vault := newLandTestRepo(t, 1, "true")
	planA, path, _ := deliveryAmendmentPlan(t, vault)
	if err := deliveryImportCmd(Args{"vault": vault, "plan": path, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	waveA := deliveryWaveForScope(t, vault, planA.Scope)
	baseA := stringField(waveA.Data, "integration_base_sha")
	if baseA == "" || fallback(stringField(waveA.Data, "status"), "") != "open" || stringField(waveA.Data, "authorization") != "disarmed" {
		t.Fatalf("initial import did not retain a held base snapshot: %#v", waveA.Data)
	}
	tasksA := deliveryWaveTaskIDsBySource(t, vault, stringField(waveA.Data, "id"))
	if len(tasksA) != len(planA.Tasks) {
		t.Fatalf("initial import has incomplete source-keyed task allocation: %#v", tasksA)
	}

	if err := writeText(filepath.Join(repo, "amendment-base.txt"), "base B\n"); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, repo, "add", "amendment-base.txt")
	runGitDir(t, repo, "commit", "-m", "advance delivery base")

	planB := planA
	planB.ContextFingerprint = ""
	planB.Summary = "An amended held delivery contract remains editable before execution."
	path = writeDeliveryV2TestPlan(t, vault, planB)
	if err := deliveryImportCmd(Args{"vault": vault, "plan": path, "quiet": "true"}); err != nil {
		t.Fatalf("held wave amendment was refused: %v", err)
	}
	waveB := deliveryWaveForScope(t, vault, planB.Scope)
	if waveB.Data["id"] != waveA.Data["id"] || stringField(waveB.Data, "delivery_plan_scope") != planA.Scope {
		t.Fatalf("amendment changed wave identity or scope: before=%#v after=%#v", waveA.Data, waveB.Data)
	}
	if stringField(waveB.Data, "integration_base_sha") != strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "refs/heads/main")) {
		t.Fatalf("held amendment did not select the current default base: %#v", waveB.Data)
	}
	if stringField(waveB.Data, "delivery_plan_fingerprint") == stringField(waveA.Data, "delivery_plan_fingerprint") {
		t.Fatal("held amendment retained the old plan fingerprint")
	}
	if got := deliveryWaveTaskIDsBySource(t, vault, stringField(waveB.Data, "id")); len(got) != len(tasksA) {
		t.Fatalf("amendment changed source-keyed task allocation: before=%#v after=%#v", tasksA, got)
	}
	for source, id := range tasksA {
		if got := deliveryWaveTaskIDsBySource(t, vault, stringField(waveB.Data, "id"))[source]; got != id {
			t.Fatalf("source key %q changed task ID from %s to %s", source, id, got)
		}
	}
}

func TestDeliveryImportRejectsAmendmentAfterWaveStart(t *testing.T) {
	_, vault := newLandTestRepo(t, 1, "true")
	planA, path, confirm := deliveryAmendmentPlan(t, vault)
	if _, err := deliveryStart(Args{"vault": vault, "plan": path, "by": "human:test", "confirm": confirm, "quiet": "true"}, fixedWaveEnvironmentInspector(greenWaveEnvironment())); err != nil {
		t.Fatal(err)
	}
	waveA := deliveryWaveForScope(t, vault, planA.Scope)
	if stringField(waveA.Data, "authorization") != "armed" {
		t.Fatalf("initial Start did not arm wave: %#v", waveA.Data)
	}

	planB := planA
	planB.ContextFingerprint = ""
	planB.Summary = "A changed contract must not rewrite an executed wave."
	path = writeDeliveryV2TestPlan(t, vault, planB)
	if err := deliveryImportCmd(Args{"vault": vault, "plan": path, "quiet": "true"}); err == nil || !strings.Contains(err.Error(), "new plan scope/wave or perform an explicit controlled rebase") {
		t.Fatalf("amendment after Start was not refused: %v", err)
	}
	waveAfter := deliveryWaveForScope(t, vault, planA.Scope)
	if stringField(waveAfter.Data, "delivery_plan_fingerprint") != stringField(waveA.Data, "delivery_plan_fingerprint") || stringField(waveAfter.Data, "authorization") != "armed" {
		t.Fatalf("refused amendment rewrote the armed wave: before=%#v after=%#v", waveA.Data, waveAfter.Data)
	}
}
