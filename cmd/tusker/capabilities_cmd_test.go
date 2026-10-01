package main

import (
	"encoding/json"
	"errors"
	"runtime/debug"
	"strings"
	"testing"
)

func TestInstalledCapabilityManifest(t *testing.T) {
	t.Parallel()
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
	if manifest.Schema != capabilitiesSchema {
		t.Fatalf("manifest contract = %#v", manifest)
	}
	if manifest.Binary.Version != "v1.2.3-test" || manifest.Binary.BinarySHA256 == "" {
		t.Fatalf("binary provenance = %#v", manifest.Binary)
	}
	assertSortedCapabilities(t, manifest)
	for _, command := range []string{"automation plan", "capabilities", "daemon status", "docs find", "projects add", "reindex", "runs inspect", "verify", "wave start", "work start"} {
		if !capabilitiesContainCommand(manifest.Commands, command) {
			t.Fatalf("manifest omitted command family %q", command)
		}
	}
	for _, command := range manifest.Commands {
		if command.Group == "" || command.Purpose == "" || command.UseWhen == "" || command.AvoidWhen == "" {
			t.Fatalf("capability lacks group, purpose, or use/avoid guidance: %#v", command)
		}
	}
	if capabilitiesContainCommand(manifest.Commands, "hook") {
		t.Fatal("Tusker must not advertise repository hook installation")
	}
	if manifest.RunnerCatalogSchema != "tusker.runner-catalog/v1" {
		t.Fatalf("runner catalog schema = %q", manifest.RunnerCatalogSchema)
	}
	if !containsString(manifest.RunnerAdapters, string(RunnerCodexACP)) {
		t.Fatalf("manifest omitted primary local runner %q: %#v", RunnerCodexACP, manifest.RunnerAdapters)
	}
	if !containsString(manifest.Schemas.Task, "tusker.task/v7") || !containsString(manifest.Schemas.Task, "tusker.wave/v7") || !containsString(manifest.Schemas.Review, reviewResultSchema) || !containsString(manifest.Schemas.Completion, completionTransactionSchema) || !containsString(manifest.Schemas.Receipt, v7LandingReceiptSchema) {
		t.Fatalf("manifest omitted core schemas: %#v", manifest.Schemas)
	}
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

func capabilitiesContainCommand(commands []capabilityCommand, name string) bool {
	for _, command := range commands {
		if command.Command == name {
			return true
		}
	}
	return false
}

func TestCapabilitiesCommandRoutesJSON(t *testing.T) {
	command, args := parseCLI([]string{"tusker", "capabilities", "--json"})
	if command != "capabilities" || !args.Bool("json") {
		t.Fatalf("parseCLI = %q %#v", command, args)
	}
	output := captureStdout(t, func() {
		if _, err := runInner(command, args); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(output, `"schema":"`+capabilitiesSchema+`"`) || strings.Contains(output, `"read_only"`) {
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
	if !capabilityStringsSorted(manifest.Schemas.Task) || !capabilityStringsSorted(manifest.Schemas.Review) || !capabilityStringsSorted(manifest.Schemas.Completion) || !capabilityStringsSorted(manifest.Schemas.Receipt) || !capabilityStringsSorted(manifest.RunnerAdapters) {
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
