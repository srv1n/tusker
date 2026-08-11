package main

import (
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"runtime/debug"
	"strings"
	"testing"
)

func TestInstalledCapabilityManifest(t *testing.T) {
	executable := writeTempExecutable(t, "tusker-capabilities-test-binary")
	info := &debug.BuildInfo{Main: debug.Module{Version: "v1.2.3-test"}}
	firstManifest, err := buildCapabilitiesManifest(info, executable)
	if err != nil {
		t.Fatal(err)
	}
	first, err := json.Marshal(firstManifest)
	if err != nil {
		t.Fatal(err)
	}
	secondManifest, err := buildCapabilitiesManifest(info, executable)
	if err != nil {
		t.Fatal(err)
	}
	second, err := json.Marshal(secondManifest)
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
	for _, command := range []string{"automation", "capabilities", "daemon", "delivery", "docs", "projects", "reindex", "runs", "verify", "wave", "work"} {
		if !capabilitiesContainCommand(manifest.Commands, command) {
			t.Fatalf("manifest omitted command family %q", command)
		}
	}
	if manifest.RunnerCatalogSchema != "tusker.runner-catalog/v1" {
		t.Fatalf("runner catalog schema = %q", manifest.RunnerCatalogSchema)
	}
	if !containsString(manifest.RunnerAdapters, string(RunnerCodexACP)) {
		t.Fatalf("manifest omitted primary local runner %q: %#v", RunnerCodexACP, manifest.RunnerAdapters)
	}
	if !containsString(manifest.Schemas.Delivery, deliveryPlanV2Schema) || !containsString(manifest.Schemas.Review, reviewResultSchema) || !containsString(manifest.Schemas.Completion, completionTransactionSchema) || !containsString(manifest.Schemas.Receipt, v7LandingReceiptSchema) {
		t.Fatalf("manifest omitted core schemas: %#v", manifest.Schemas)
	}
}

func TestCapabilityInventoryCoversDispatcher(t *testing.T) {
	raw, err := os.ReadFile("cli.go")
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(string(raw), "func runInner(")
	end := strings.Index(string(raw), "\nfunc legacyOnlyCommand(")
	if start < 0 || end <= start {
		t.Fatal("cannot isolate runInner dispatcher")
	}
	manifest, err := buildCapabilitiesManifest(nil, "")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(raw)[start:end], "\n")
	quoted := regexp.MustCompile(`"([^"]+)"`)
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "case ") || !strings.HasSuffix(line, ":") {
			continue
		}
		caseExpression := strings.TrimSuffix(strings.TrimPrefix(line, "case "), ":")
		j := i + 1
		for ; j < len(lines); j++ {
			next := strings.TrimSpace(lines[j])
			if strings.HasPrefix(next, "case ") || strings.HasPrefix(next, "default:") {
				break
			}
		}
		refuses := strings.Contains(strings.Join(lines[i+1:j], "\n"), "legacyOnlyCommand(")
		for _, match := range quoted.FindAllStringSubmatch(caseExpression, -1) {
			caseName := strings.TrimSpace(match[1])
			parts := strings.Fields(caseName)
			if len(parts) == 0 {
				continue
			}
			command := parts[0]
			if command == "legacy" || command == "help" || strings.HasPrefix(command, "-") {
				continue
			}
			deprecated := capabilityDeprecationNamed(manifest.Deprecations, caseName)
			if refuses {
				if !deprecated {
					t.Errorf("refusal route %q is missing a typed deprecation", caseName)
				}
				if capabilityAdvertisesCase(manifest.Commands, caseName) {
					t.Errorf("refusal route %q is advertised as live", caseName)
				}
				continue
			}
			if deprecated {
				continue
			}
			capability, ok := capabilityCommandNamed(manifest.Commands, command)
			if !ok {
				t.Errorf("runInner public command %q is absent from capabilities", command)
				continue
			}
			if len(parts) > 1 && !containsString(capability.Subcommands, parts[1]) {
				t.Errorf("runInner public command %q subcommand %q is absent from capabilities", command, parts[1])
			}
		}
	}
}

func capabilityAdvertisesCase(commands []capabilityCommand, caseName string) bool {
	parts := strings.Fields(caseName)
	if len(parts) == 0 {
		return false
	}
	command, ok := capabilityCommandNamed(commands, parts[0])
	if !ok {
		return false
	}
	if len(parts) == 1 {
		return len(command.Subcommands) == 0
	}
	return containsString(command.Subcommands, parts[1])
}

func capabilityDeprecationNamed(deprecations []capabilityDeprecation, name string) bool {
	for _, deprecation := range deprecations {
		if deprecation.Command == name {
			return true
		}
	}
	return false
}

func capabilityCommandNamed(commands []capabilityCommand, name string) (capabilityCommand, bool) {
	for _, command := range commands {
		if command.Command == name {
			return command, true
		}
	}
	return capabilityCommand{}, false
}

func TestCapabilityCompatibilityFailsClosed(t *testing.T) {
	previousContract := loadEmbeddedSkillCompatibility
	previousPayload := loadEmbeddedSkillPayloadFingerprint
	t.Cleanup(func() {
		loadEmbeddedSkillCompatibility = previousContract
		loadEmbeddedSkillPayloadFingerprint = previousPayload
	})

	loadEmbeddedSkillCompatibility = func() (skillCompatibilityContract, error) {
		return skillCompatibilityContract{}, errors.New("contract fixture failed")
	}
	if _, err := buildCapabilitiesManifest(nil, ""); err == nil || !strings.Contains(err.Error(), "contract fixture failed") {
		t.Fatalf("contract failure was not propagated: %v", err)
	}
	if err := capabilitiesCmd(Args{"json": "true"}); err == nil || errorToIssue(err).Code != errorCapabilityContractInvalid {
		t.Fatalf("capabilities command did not fail closed: %v", err)
	}

	loadEmbeddedSkillCompatibility = previousContract
	loadEmbeddedSkillPayloadFingerprint = func() (string, error) {
		return "", errors.New("payload fixture failed")
	}
	if _, err := buildCapabilitiesManifest(nil, ""); err == nil || !strings.Contains(err.Error(), "payload fixture failed") {
		t.Fatalf("payload failure was not propagated: %v", err)
	}
}

// TestDispatchCapabilitySkewRefusal binds the manifest's unavailable state to
// the actual Start gate. It proves that a caller cannot turn stale capability
// knowledge into a claim, imported task, or armed wave.
func TestDispatchCapabilitySkewRefusal(t *testing.T) {
	manifest, err := buildCapabilitiesManifest(&debug.BuildInfo{}, writeTempExecutable(t, "tusker-capability-skew"))
	if err != nil {
		t.Fatal(err)
	}
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
