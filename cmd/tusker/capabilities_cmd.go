package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"runtime/debug"
	"sort"
)

const (
	capabilitiesSchema             = "tusker.capabilities/v1"
	errorCapabilityContractInvalid = "CAPABILITY_CONTRACT_INVALID"
)

// capabilitiesManifest is the installed-binary contract for an orchestrator.
// Keep it static: host discovery belongs to runner catalog, and project policy
// belongs to the vault. Neither can make an installed CLI capability appear.
type capabilitiesManifest struct {
	Schema               string                   `json:"schema"`
	Binary               VersionProjection        `json:"binary"`
	Commands             []capabilityCommand      `json:"commands"`
	Schemas              capabilitySchemas        `json:"schemas"`
	RunnerAdapters       []string                 `json:"runner_adapters"`
	RunnerCatalogSchema  string                   `json:"runner_catalog_schema"`
	OptionalCapabilities []capabilityAvailability `json:"optional_capabilities"`
	Deprecations         []capabilityDeprecation  `json:"deprecations"`
	Compatibility        capabilityCompatibility  `json:"compatibility"`
}

type capabilityCommand struct {
	Command   string `json:"command"`
	Group     string `json:"group"`
	Purpose   string `json:"purpose"`
	UseWhen   string `json:"use_when,omitempty"`
	AvoidWhen string `json:"avoid_when,omitempty"`
}

type capabilitySchemas struct {
	Task       []string `json:"task"`
	Review     []string `json:"review"`
	Completion []string `json:"completion"`
	Receipt    []string `json:"receipt"`
}

type capabilityAvailability struct {
	Capability string `json:"capability"`
	Available  bool   `json:"available"`
}

type capabilityDeprecation struct {
	Command     string `json:"command"`
	Replacement string `json:"replacement"`
}

type capabilityCompatibility struct {
	Schema                   string                      `json:"schema"`
	Fingerprint              string                      `json:"fingerprint"`
	WorkflowMin              int                         `json:"workflow_min"`
	WorkflowMax              int                         `json:"workflow_max"`
	TrackerSchemaVersions    []int                       `json:"tracker_schema_versions"`
	WaveAuthorizationSchemas []string                    `json:"wave_authorization_schemas"`
	AuthoringContract        authoringContractProvenance `json:"authoring_contract"`
	CanonicalSkillSource     string                      `json:"canonical_skill_source"`
	CanonicalPayloadFP       string                      `json:"canonical_payload_fingerprint"`
	MaterializationSchema    string                      `json:"materialization_schema"`
	ProvenanceManifest       string                      `json:"provenance_manifest"`
	PrimaryGuides            []string                    `json:"primary_guides"`
}

type capabilityCompatibilityMaterial struct {
	Schema               string                   `json:"schema"`
	Commands             []capabilityCommand      `json:"commands"`
	Schemas              capabilitySchemas        `json:"schemas"`
	RunnerAdapters       []string                 `json:"runner_adapters"`
	RunnerCatalogSchema  string                   `json:"runner_catalog_schema"`
	OptionalCapabilities []capabilityAvailability `json:"optional_capabilities"`
	Deprecations         []capabilityDeprecation  `json:"deprecations"`
	Contract             capabilityCompatibility  `json:"contract"`
}

func capabilitiesCmd(args Args) error {
	if len(args) != 1 || !args.Bool("json") {
		return tuskerError(errorInvalidArg, "capabilities is read-only and requires --json")
	}
	info, _ := debugReadBuildInfo()
	executable, _ := executablePath()
	manifest, err := buildCapabilitiesManifest(info, executable)
	if err != nil {
		return tuskerError(errorCapabilityContractInvalid, "capabilities compatibility is unavailable: "+err.Error())
	}
	emitJSON(manifest)
	return nil
}

// Variables keep the command independently testable without relying on the
// test binary's build metadata or location.
var debugReadBuildInfo = debug.ReadBuildInfo
var executablePath = os.Executable
var loadEmbeddedSkillCompatibility = embeddedSkillCompatibilityContract
var loadEmbeddedSkillPayloadFingerprint = embeddedSkillPayloadFingerprint

func buildCapabilitiesManifest(info *debug.BuildInfo, executable string) (capabilitiesManifest, error) {
	manifest := capabilitiesManifest{
		Schema:   capabilitiesSchema,
		Binary:   buildVersionProjection(info, executable),
		Commands: installedCapabilityCommands(),
		Schemas: capabilitySchemas{
			Task:       []string{"tusker.task/v7", "tusker.epic/v7", "tusker.gate/v1", "tusker.evidence/v1", "tusker.wave/v7"},
			Review:     []string{reviewResultSchema, reviewProposalSchema},
			Completion: []string{completionTransactionSchema, completionReceiptSchema},
			Receipt:    []string{v7LandingReceiptSchema},
		},
		RunnerAdapters: []string{
			string(RunnerClaude),
			string(RunnerCodex),
			string(RunnerCodexACP),
			string(RunnerCodexAppServer),
			string(RunnerCodexCloud),
			string(RunnerCodexExec),
			string(RunnerDevin),
			string(RunnerMuse),
		},
		RunnerCatalogSchema: "tusker.runner-catalog/v1",
		Deprecations:        []capabilityDeprecation{},
	}
	sortCapabilitiesManifest(&manifest)
	compatibility, err := buildCapabilityCompatibility(manifest)
	if err != nil {
		return capabilitiesManifest{}, err
	}
	manifest.Compatibility = compatibility
	return manifest, nil
}

func buildCapabilityCompatibility(manifest capabilitiesManifest) (capabilityCompatibility, error) {
	contract, err := loadEmbeddedSkillCompatibility()
	if err != nil {
		return capabilityCompatibility{}, fmt.Errorf("load embedded compatibility contract: %w", err)
	}
	payloadFingerprint, err := loadEmbeddedSkillPayloadFingerprint()
	if err != nil {
		return capabilityCompatibility{}, fmt.Errorf("fingerprint embedded skill payload: %w", err)
	}
	projection := capabilityCompatibility{
		Schema: skillCompatibilitySchema, WorkflowMin: contract.WorkflowMin, WorkflowMax: contract.WorkflowMax,
		TrackerSchemaVersions:    append([]int(nil), contract.TrackerSchemaVersions...),
		WaveAuthorizationSchemas: append([]string(nil), contract.WaveAuthorizationSchemas...),
		AuthoringContract:        contract.AuthoringContract,
		CanonicalSkillSource:     contract.CanonicalSource, CanonicalPayloadFP: payloadFingerprint,
		MaterializationSchema: contract.MaterializationSchema, ProvenanceManifest: skillProvenanceFilename,
		PrimaryGuides: append([]string(nil), contract.PrimaryGuides...),
	}
	sort.Ints(projection.TrackerSchemaVersions)
	sort.Strings(projection.WaveAuthorizationSchemas)
	sort.Strings(projection.PrimaryGuides)
	material := capabilityCompatibilityMaterial{
		Schema:   manifest.Schema,
		Commands: manifest.Commands, Schemas: manifest.Schemas,
		RunnerAdapters: manifest.RunnerAdapters, RunnerCatalogSchema: manifest.RunnerCatalogSchema,
		OptionalCapabilities: manifest.OptionalCapabilities, Deprecations: manifest.Deprecations,
		Contract: projection,
	}
	raw, err := json.Marshal(material)
	if err != nil {
		return capabilityCompatibility{}, fmt.Errorf("encode compatibility material: %w", err)
	}
	sum := sha256.Sum256(raw)
	projection.Fingerprint = "sha256:" + hex.EncodeToString(sum[:])
	return projection, nil
}

// installedCapabilityCommands lists every visible command in the command
// table; internal plumbing stays out of the public inventory.
func installedCapabilityCommands() []capabilityCommand {
	var out []capabilityCommand
	for _, c := range cliCommands {
		if c.Hidden {
			continue
		}
		out = append(out, capabilityCommand{Command: c.Name, Group: c.Group, Purpose: c.Summary, UseWhen: c.Use, AvoidWhen: c.Avoid})
	}
	return out
}

func sortCapabilitiesManifest(manifest *capabilitiesManifest) {
	sort.Slice(manifest.Commands, func(i, j int) bool { return manifest.Commands[i].Command < manifest.Commands[j].Command })
	sort.Strings(manifest.Schemas.Task)
	sort.Strings(manifest.Schemas.Review)
	sort.Strings(manifest.Schemas.Completion)
	sort.Strings(manifest.Schemas.Receipt)
	sort.Strings(manifest.RunnerAdapters)
	sort.Slice(manifest.OptionalCapabilities, func(i, j int) bool {
		return manifest.OptionalCapabilities[i].Capability < manifest.OptionalCapabilities[j].Capability
	})
	sort.Slice(manifest.Deprecations, func(i, j int) bool { return manifest.Deprecations[i].Command < manifest.Deprecations[j].Command })
}

func printCapabilitiesHelp() {
	fmt.Println("Usage: tusker capabilities --json\n\nPrint the versioned installed-binary capability manifest. This query does not mutate state.")
}
