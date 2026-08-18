package main

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestCanonicalSkillCompatibilityMatchesFactoryIntakeContract(t *testing.T) {
	root := filepath.Join("..", "..", "skills", "tusker")
	compatibility, err := readSkillCompatibilityContract(root)
	if err != nil {
		t.Fatal(err)
	}
	want, err := embeddedFactoryIntakeContractProvenance()
	if err != nil {
		t.Fatal(err)
	}
	got := compatibility.FactoryIntakeContract
	if got != want {
		t.Fatalf("canonical skill compatibility contract = %#v, want %#v", got, want)
	}
	raw, err := os.ReadFile(filepath.Join(root, "assets", "factory-intake-contract.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if got := factoryIntakeContractFingerprint(raw); got != want.Fingerprint {
		t.Fatalf("contract content fingerprint = %s, want %s", got, want.Fingerprint)
	}
}

func canonicalFactoryIntakeContractForTest(t *testing.T) factoryIntakeContract {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "skills", "tusker", "assets", "factory-intake-contract.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var contract factoryIntakeContract
	if err := yaml.Unmarshal(raw, &contract); err != nil {
		t.Fatal(err)
	}
	return contract
}

func TestFactoryIntakeContractRejectsIncompleteDecisionTable(t *testing.T) {
	contract := canonicalFactoryIntakeContractForTest(t)
	if contract.ContractVersion != "1.1.0" {
		t.Fatalf("contract version = %q, want 1.1.0", contract.ContractVersion)
	}
	contract.DecisionTable = contract.DecisionTable[:len(contract.DecisionTable)-1]
	if err := validateFactoryIntakeContract(contract); err == nil {
		t.Fatal("expected incomplete contract to fail validation")
	}
}

func TestFactoryIntakeContractRejectsMissingStartFingerprintGuardrail(t *testing.T) {
	contract := canonicalFactoryIntakeContractForTest(t)
	contract.Guardrails = contract.Guardrails[:len(contract.Guardrails)-1]
	if err := validateFactoryIntakeContract(contract); err == nil {
		t.Fatal("expected missing start fingerprint guardrail to fail validation")
	}
}
