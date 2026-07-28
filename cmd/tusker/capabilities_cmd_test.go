package main

import (
	"encoding/json"
	"os"
	"runtime/debug"
	"strings"
	"testing"
)

func TestInstalledCapabilityManifest(t *testing.T) {
	executable := writeTempExecutable(t, "tusker-capabilities-test-binary")
	info := &debug.BuildInfo{Main: debug.Module{Version: "v1.2.3-test"}}
	first, err := json.Marshal(buildCapabilitiesManifest(info, executable))
	if err != nil {
		t.Fatal(err)
	}
	second, err := json.Marshal(buildCapabilitiesManifest(info, executable))
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("manifest is nondeterministic:\nfirst:  %s\nsecond: %s", first, second)
	}
	var manifest capabilitiesManifest
	if err := json.Unmarshal(first, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Schema != capabilitiesSchema || !manifest.ReadOnly {
		t.Fatalf("manifest contract = %#v", manifest)
	}
	if manifest.Binary.Version != "v1.2.3-test" || manifest.Binary.BinarySHA256 == "" {
		t.Fatalf("binary provenance = %#v", manifest.Binary)
	}
	assertSortedCapabilities(t, manifest)
	for _, command := range []string{"automation", "capabilities", "daemon", "delivery", "projects", "runs", "wave", "work"} {
		if !capabilitiesContainCommand(manifest.Commands, command) {
			t.Fatalf("manifest omitted command family %q", command)
		}
	}
	if manifest.RunnerCatalogSchema != "tusker.runner-catalog/v1" {
		t.Fatalf("runner catalog schema = %q", manifest.RunnerCatalogSchema)
	}
	if !containsString(manifest.Schemas.Delivery, deliveryPlanV2Schema) || !containsString(manifest.Schemas.Review, reviewResultSchema) || !containsString(manifest.Schemas.Completion, completionTransactionSchema) || !containsString(manifest.Schemas.Receipt, v7LandingReceiptSchema) {
		t.Fatalf("manifest omitted core schemas: %#v", manifest.Schemas)
	}
}

// TestDispatchCapabilitySkewRefusal binds the manifest's unavailable state to
// the actual Start gate. It proves that a caller cannot turn stale capability
// knowledge into a claim, imported task, or armed wave.
func TestDispatchCapabilitySkewRefusal(t *testing.T) {
	manifest := buildCapabilitiesManifest(&debug.BuildInfo{}, writeTempExecutable(t, "tusker-capability-skew"))
	if len(manifest.OptionalCapabilities) != 1 || manifest.OptionalCapabilities[0].Capability != strictV2ProofAuthorityCapability || manifest.OptionalCapabilities[0].Available {
		t.Fatalf("manifest does not report unavailable strict capability: %#v", manifest.OptionalCapabilities)
	}

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
	issue := errorToIssue(err)
	context, ok := issue.Context.(unavailableCapabilityContext)
	if err == nil || issue.Code != errorInvalidArg || !strings.Contains(issue.Message, strictV2ProofAuthorityCapability) || !ok {
		t.Fatalf("machine-readable Start refusal = %#v, err=%v", issue, err)
	}
	assertEqual(t, []string{strictV2ProofAuthorityCapability}, context.MissingCapabilities, "machine-readable missing capability")
	if context.Installed.Schema != versionSchema || context.Installed.Version == "" || context.Remedy != unavailableCapabilityRemedy || issue.Hint != unavailableCapabilityRemedy {
		t.Fatalf("machine-readable refusal context = %#v, issue = %#v", context, issue)
	}
	if locked {
		t.Fatal("Start acquired the material epoch before capability refusal")
	}
	assertEqual(t, before, snapshotDeliveryV2Records(t, vault), "capability skew refusal is preclaim/prewrite")
}

func capabilitiesContainCommand(commands []capabilityCommand, name string) bool {
	for _, command := range commands {
		if command.Command == name {
			return true
		}
	}
	return false
}

func TestCapabilitiesCommandRoutesOnlyReadOnlyJSON(t *testing.T) {
	command, args := parseCLI([]string{"tusker", "capabilities", "--json"})
	if command != "capabilities" || !args.Bool("json") {
		t.Fatalf("parseCLI = %q %#v", command, args)
	}
	output := captureStdout(t, func() {
		if _, err := runInner(command, args); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(output, `"schema":"`+capabilitiesSchema+`"`) || !strings.Contains(output, `"read_only":true`) {
		t.Fatalf("capabilities output = %s", output)
	}
	if cliCommandMutatesVault("capabilities") {
		t.Fatal("capabilities was accidentally classified as mutating")
	}
	for _, invalid := range []Args{{}, {"vault": ".tusker", "json": "true"}, {"json": "false"}, {"write": "true", "json": "true"}} {
		if err := capabilitiesCmd(invalid); err == nil || errorToIssue(err).Code != errorInvalidArg {
			t.Fatalf("args %#v did not refuse as read-only: %v", invalid, err)
		}
	}
}

func assertSortedCapabilities(t *testing.T, manifest capabilitiesManifest) {
	t.Helper()
	for i := 1; i < len(manifest.Commands); i++ {
		if manifest.Commands[i-1].Command >= manifest.Commands[i].Command {
			t.Fatalf("commands are not sorted: %#v", manifest.Commands)
		}
	}
	for _, command := range manifest.Commands {
		if !capabilityStringsSorted(command.Subcommands) || !capabilityStringsSorted(command.Flags) {
			t.Fatalf("command is not sorted: %#v", command)
		}
	}
	if !capabilityStringsSorted(manifest.Schemas.Task) || !capabilityStringsSorted(manifest.Schemas.Delivery) || !capabilityStringsSorted(manifest.Schemas.Review) || !capabilityStringsSorted(manifest.Schemas.Completion) || !capabilityStringsSorted(manifest.Schemas.Receipt) || !capabilityStringsSorted(manifest.RunnerAdapters) {
		t.Fatalf("manifest collections are not sorted: %#v", manifest)
	}
}

func capabilityStringsSorted(values []string) bool {
	for i := 1; i < len(values); i++ {
		if values[i-1] >= values[i] {
			return false
		}
	}
	return true
}
