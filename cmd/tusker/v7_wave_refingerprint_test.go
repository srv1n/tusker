package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestWaveRefingerprintDryRunAndConfirmed(t *testing.T) {
	vault := deliveryTestVault(t)
	planPath := writeDeliveryV2TestPlan(t, vault, validDeliveryPlanV2())
	if err := deliveryImportCmd(Args{"vault": vault, "plan": planPath, "wave": "Imported", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	writeStaleWaveFactoryContract(t, vault, "sha256:old-wave-contract")
	before := mustReadIndexTest(t, filepath.Join(vault, "work", "waves", "W-0001.md"))
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	initial := buildArmedWaveSnapshot(vault, idx, idx.Waves["W-0001"], nil, deliveryImportNow().UTC())
	for _, member := range initial.Members {
		if member.State != armedWaveDisarmed {
			t.Fatalf("never-armed member state = %#v, want disarmed", member)
		}
	}

	output := captureStdout(t, func() {
		if err := waveV7RefingerprintCmd(Args{"vault": vault, "_pos0": "W-0001", "dry-run": "true", "json": "true", "by": "human:test"}); err != nil {
			t.Fatal(err)
		}
	})
	var payload struct {
		Refingerprint waveRefingerprintReport `json:"refingerprint"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Refingerprint.ReadOnly || !payload.Refingerprint.Changed || payload.Refingerprint.NextFingerprint == "" {
		t.Fatalf("unexpected dry-run report: %#v", payload.Refingerprint)
	}
	if after := mustReadIndexTest(t, filepath.Join(vault, "work", "waves", "W-0001.md")); after != before {
		t.Fatal("wave re-fingerprint dry-run mutated the wave")
	}

	if err := waveV7RefingerprintCmd(Args{
		"vault": vault, "_pos0": "W-0001", "confirm": payload.Refingerprint.NextFingerprint,
		"force": "true", "quiet": "true", "by": "human:test",
	}); err != nil {
		t.Fatal(err)
	}
	idx, err = loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	wave := idx.Waves["W-0001"]
	want, err := embeddedFactoryIntakeContractProvenance()
	if err != nil {
		t.Fatal(err)
	}
	gotContract := factoryIntakeContractProvenance{
		Schema:      stringField(wave.Data, "factory_intake_contract_schema"),
		Version:     stringField(wave.Data, "factory_intake_contract_version"),
		Fingerprint: stringField(wave.Data, "factory_intake_contract_fingerprint"),
	}
	if gotContract != want {
		t.Fatalf("wave contract was not refreshed: got=%#v want=%#v", gotContract, want)
	}
	if stringField(wave.Data, "authorization") != "disarmed" || stringField(wave.Data, "authorization_fingerprint") != "" {
		t.Fatalf("re-fingerprint changed authorization: %#v", wave.Data)
	}
	if got, issues := waveMaterialFingerprint(vault, idx, wave); len(issues) != 0 || got != payload.Refingerprint.NextFingerprint {
		t.Fatalf("re-fingerprint did not converge material: got=%s issues=%v want=%s", got, issues, payload.Refingerprint.NextFingerprint)
	}
	if _, _, armed := armedWaveForTask(vault, idx.Tasks["VTP-T-0001"]); armed {
		t.Fatal("re-fingerprint authorized a task in the disarmed wave")
	}
	if blockers := waveFactoryIntakeContractBlockers(vault, wave); len(blockers) != 0 {
		t.Fatalf("refreshed factory contract still blocked preflight: %v", blockers)
	}
}

func TestWaveRefingerprintRefusesArmedWave(t *testing.T) {
	vault := deliveryTestVault(t)
	planPath := writeDeliveryV2TestPlan(t, vault, validDeliveryPlanV2())
	if err := deliveryImportCmd(Args{"vault": vault, "plan": planPath, "wave": "Imported", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	writeStaleWaveFactoryContract(t, vault, "sha256:old-wave-contract")
	setWaveAuthorizationForTest(t, vault, "armed", "sha256:old-authorization")
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	report, err := buildWaveRefingerprintReport(vault, idx, idx.Waves["W-0001"])
	if err != nil {
		t.Fatal(err)
	}
	err = waveV7RefingerprintCmd(Args{"vault": vault, "_pos0": "W-0001", "confirm": report.NextFingerprint, "force": "true", "by": "human:test"})
	if err == nil || !strings.Contains(err.Error(), "requires a disarmed wave") {
		t.Fatalf("armed wave re-fingerprint error = %v", err)
	}
	idx, err = loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	if stringField(idx.Waves["W-0001"].Data, "authorization") != "armed" {
		t.Fatal("refingerprint changed an armed authorization")
	}
}

func writeStaleWaveFactoryContract(t *testing.T, vault, fingerprint string) {
	t.Helper()
	path := filepath.Join(vault, "work", "waves", "W-0001.md")
	data, body, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	data["factory_intake_contract_schema"] = factoryIntakeContractSchema
	data["factory_intake_contract_version"] = "0.0.0"
	data["factory_intake_contract_fingerprint"] = fingerprint
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["wave"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(path, content); err != nil {
		t.Fatal(err)
	}
}

func setWaveAuthorizationForTest(t *testing.T, vault, authorization, fingerprint string) {
	t.Helper()
	path := filepath.Join(vault, "work", "waves", "W-0001.md")
	data, body, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	data["authorization"] = authorization
	if fingerprint == "" {
		delete(data, "authorization_fingerprint")
	} else {
		data["authorization_fingerprint"] = fingerprint
	}
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["wave"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(path, content); err != nil {
		t.Fatal(err)
	}
}
