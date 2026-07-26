package main

import (
	"os"
	"strings"
	"testing"
)

func unavailableStrictCapabilityPlan() deliveryPlanV2 {
	plan := validDeliveryPlanV2()
	plan.RequiredCapabilities = []string{strictV2ProofAuthorityCapability}
	return plan
}

func TestDeliveryRequiredCapabilities(t *testing.T) {
	got, err := deliveryRequiredCapabilities([]string{strictV2ProofAuthorityCapability, strictV2ProofAuthorityCapability})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, []string{strictV2ProofAuthorityCapability}, got, "normalized required capabilities")
	if deliveryCapabilityAvailable(strictV2ProofAuthorityCapability) {
		t.Fatal("K0 must not advertise strict authority before the reviewed kernel exists")
	}

	vault := deliveryTestVault(t)
	path := writeDeliveryV2TestPlan(t, vault, unavailableStrictCapabilityPlan())
	before := snapshotDeliveryV2Records(t, vault)
	report, err := deliveryPlanDoctor(vault, path)
	if err != nil {
		t.Fatal(err)
	}
	if report.OK {
		t.Fatalf("doctor accepted unavailable strict capability: %#v", report)
	}
	requireDoctorCodes(t, report, "REQUIRED_CAPABILITY_UNAVAILABLE")
	if err := deliveryV2ImportCmd(vault, path, Args{"vault": vault, "quiet": "true"}); err == nil || !strings.Contains(err.Error(), strictV2ProofAuthorityCapability) {
		t.Fatalf("import error = %v, want exact unavailable strict capability", err)
	}
	assertEqual(t, before, snapshotDeliveryV2Records(t, vault), "unavailable capability import is prewrite")
}

func TestDeliveryStrictBootstrapFence(t *testing.T) {
	vault := deliveryTestVault(t)
	path := writeDeliveryV2TestPlan(t, vault, unavailableStrictCapabilityPlan())
	before := snapshotDeliveryV2Records(t, vault)
	previousObserver := v7MaterialEpochLockObserver
	locked := false
	v7MaterialEpochLockObserver = func() { locked = true }
	t.Cleanup(func() { v7MaterialEpochLockObserver = previousObserver })

	err := deliveryV2ImportCmd(vault, path, Args{"vault": vault, "quiet": "true"})
	if err == nil || !strings.Contains(err.Error(), strictV2ProofAuthorityCapability) {
		t.Fatalf("strict bootstrap import error = %v, want unavailable capability refusal", err)
	}
	if locked {
		t.Fatal("K0 acquired the material epoch before refusing strict import")
	}
	assertEqual(t, before, snapshotDeliveryV2Records(t, vault), "K0 cannot stamp or create a strict task")
}

func TestDeliveryStartRequiredCapabilities(t *testing.T) {
	vault := deliveryTestVault(t)
	path := writeDeliveryV2TestPlan(t, vault, unavailableStrictCapabilityPlan())
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	before := snapshotDeliveryV2Records(t, vault)
	previousObserver := v7MaterialEpochLockObserver
	locked := false
	v7MaterialEpochLockObserver = func() { locked = true }
	t.Cleanup(func() { v7MaterialEpochLockObserver = previousObserver })
	_, err = deliveryStart(Args{
		"vault": vault, "plan": path, "by": "human:fixture", "quiet": "true",
		"confirm": deliveryFingerprint(raw),
	}, fixedWaveEnvironmentInspector(greenWaveEnvironment()))
	if err == nil || !strings.Contains(err.Error(), strictV2ProofAuthorityCapability) {
		t.Fatalf("Start error = %v, want exact unavailable strict capability", err)
	}
	if locked {
		t.Fatal("Start acquired the material epoch before capability refusal")
	}
	assertEqual(t, before, snapshotDeliveryV2Records(t, vault), "Start capability refusal is prewrite")
}
