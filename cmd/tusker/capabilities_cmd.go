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
	ReadOnly             bool                     `json:"read_only"`
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
	Command     string   `json:"command"`
	Subcommands []string `json:"subcommands,omitempty"`
	Flags       []string `json:"flags,omitempty"`
}

type capabilitySchemas struct {
	Task       []string `json:"task"`
	Delivery   []string `json:"delivery_plan"`
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
	Schema                   string                          `json:"schema"`
	Fingerprint              string                          `json:"fingerprint"`
	WorkflowMin              int                             `json:"workflow_min"`
	WorkflowMax              int                             `json:"workflow_max"`
	TrackerSchemaVersions    []int                           `json:"tracker_schema_versions"`
	WaveAuthorizationSchemas []string                        `json:"wave_authorization_schemas"`
	FactoryIntakeContract    factoryIntakeContractProvenance `json:"factory_intake_contract"`
	CanonicalSkillSource     string                          `json:"canonical_skill_source"`
	CanonicalPayloadFP       string                          `json:"canonical_payload_fingerprint"`
	MaterializationSchema    string                          `json:"materialization_schema"`
	ProvenanceManifest       string                          `json:"provenance_manifest"`
	PrimaryGuides            []string                        `json:"primary_guides"`
}

type capabilityCompatibilityMaterial struct {
	Schema               string                   `json:"schema"`
	ReadOnly             bool                     `json:"read_only"`
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
		ReadOnly: true,
		Binary:   buildVersionProjection(info, executable),
		Commands: installedCapabilityCommands(),
		Schemas: capabilitySchemas{
			Task:       []string{"tusker.task/v7", "tusker.epic/v7", "tusker.gate/v1", "tusker.evidence/v1", "tusker.wave/v7"},
			Delivery:   []string{deliveryPlanV2Schema, deliveryPlanningContextSchema, deliveryReviewSchema, deliveryStartSchema},
			Review:     []string{reviewResultSchema, reviewProposalSchema},
			Completion: []string{completionTransactionSchema, completionReceiptSchema},
			Receipt:    []string{v7LandingReceiptSchema},
		},
		RunnerAdapters: []string{
			string(RunnerClaude),
			string(RunnerCodex),
			string(RunnerCodexAppServer),
			string(RunnerCodexCloud),
			string(RunnerCodexExec),
		},
		RunnerCatalogSchema: "tusker.runner-catalog/v1",
		OptionalCapabilities: []capabilityAvailability{
			{Capability: strictV2ProofAuthorityCapability, Available: deliveryCapabilityAvailable(strictV2ProofAuthorityCapability)},
		},
		Deprecations: []capabilityDeprecation{
			{Command: "docs", Replacement: "docs find|new|map"},
			{Command: "docs apply", Replacement: "edit canonical docs and record task proof"},
			{Command: "docs build", Replacement: "use the repository publication pipeline"},
			{Command: "docs catalog", Replacement: "docs map"},
			{Command: "docs check", Replacement: "validate"},
			{Command: "docs dev", Replacement: "use the repository publication pipeline"},
			{Command: "docs export", Replacement: "use the repository publication pipeline"},
			{Command: "docs freshness", Replacement: "docs map"},
			{Command: "docs init", Replacement: "docs new"},
			{Command: "docs model", Replacement: "docs map"},
			{Command: "docs noop", Replacement: "record task proof"},
			{Command: "docs waive", Replacement: "record a task decision or gate"},
			{Command: "domain graph", Replacement: "domain list|show"},
			{Command: "graph", Replacement: "domain list|show"},
			{Command: "knowledge apply", Replacement: "edit project canon through its owning workflow"},
			{Command: "knowledge check", Replacement: "validate"},
			{Command: "knowledge freshness", Replacement: "show --capsule"},
			{Command: "knowledge list", Replacement: "domain list"},
			{Command: "knowledge map", Replacement: "docs map"},
			{Command: "knowledge noop", Replacement: "record task proof"},
			{Command: "knowledge route", Replacement: "skill route"},
			{Command: "knowledge show", Replacement: "domain show"},
			{Command: "knowledge waive", Replacement: "record a task decision or gate"},
			{Command: "legacy", Replacement: "V7 commands listed by tusker capabilities --json"},
			{Command: "migrate gates", Replacement: "migrate evidence-policy|close-policy|vault-root"},
			{Command: "migrate v7", Replacement: "V7 is the only supported tracker"},
			{Command: "new bug", Replacement: "new task"},
			{Command: "new doc", Replacement: "docs new"},
			{Command: "propose", Replacement: "proposal"},
			{Command: "publish build", Replacement: "use the repository publication pipeline"},
			{Command: "publish dev", Replacement: "use the repository publication pipeline"},
			{Command: "publish export", Replacement: "use the repository publication pipeline"},
			{Command: "publish llms", Replacement: "use the repository publication pipeline"},
			{Command: "verify", Replacement: "verify add|recipe"},
		},
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
		FactoryIntakeContract:    contract.FactoryIntakeContract,
		CanonicalSkillSource:     contract.CanonicalSource, CanonicalPayloadFP: payloadFingerprint,
		MaterializationSchema: contract.MaterializationSchema, ProvenanceManifest: skillProvenanceFilename,
		PrimaryGuides: append([]string(nil), contract.PrimaryGuides...),
	}
	sort.Ints(projection.TrackerSchemaVersions)
	sort.Strings(projection.WaveAuthorizationSchemas)
	sort.Strings(projection.PrimaryGuides)
	material := capabilityCompatibilityMaterial{
		Schema: manifest.Schema, ReadOnly: manifest.ReadOnly,
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

// installedCapabilityCommands is intentionally a complete public CLI inventory,
// rather than a short list of the commands this feature happens to need. Keep
// new command families here when they are added to runInner.
func installedCapabilityCommands() []capabilityCommand {
	return []capabilityCommand{
		{Command: "acp", Subcommands: []string{"doctor", "install"}},
		{Command: "acp doctor", Flags: []string{"--auth-source", "--bundle-digest", "--json"}},
		{Command: "acp install", Flags: []string{"--artifact", "--artifact-sha256", "--json", "--provider", "--publisher", "--source-url", "--version"}},
		{Command: "accept"}, {Command: "attachments"}, {Command: "attempt"},
		{Command: "automation", Subcommands: []string{"advance-external", "collect-external", "dispatch", "explain", "external-loop", "plan", "queue", "status"}, Flags: []string{"--json"}},
		{Command: "brief"}, {Command: "capabilities", Flags: []string{"--json"}}, {Command: "claim"}, {Command: "close"},
		{Command: "closeout", Subcommands: []string{"status"}}, {Command: "compact"}, {Command: "config", Subcommands: []string{"resolve"}},
		{Command: "context", Subcommands: []string{"audit"}}, {Command: "daemon", Subcommands: []string{"install", "limits", "resume", "run", "service", "status", "stop", "uninstall"}},
		{Command: "dashboard"}, {Command: "delivery", Subcommands: []string{"context", "doctor", "import", "plan", "review", "rollout", "start"}, Flags: []string{"--by", "--confirm", "--json", "--plan", "--scope"}},
		{Command: "departure", Subcommands: []string{"check", "history", "hold", "resume", "status"}}, {Command: "digest"}, {Command: "discard"},
		{Command: "docs", Subcommands: []string{"find", "map", "new"}},
		{Command: "domain", Subcommands: []string{"canon", "list", "new", "show"}},
		{Command: "execution", Subcommands: []string{"attach", "bind", "cancel", "detach", "inbox", "launch", "list", "rebind", "register", "rename", "show"}, Flags: []string{"--json"}},
		{Command: "evidence"}, {Command: "escalate", Subcommands: []string{"ack"}}, {Command: "factory", Subcommands: []string{"operations"}},
		{Command: "feedback", Subcommands: []string{"add", "digest", "ingest", "promote", "review", "signals"}}, {Command: "finish"},
		{Command: "gate"}, {Command: "gate-ledger", Subcommands: []string{"check", "record"}}, {Command: "gate-run"}, {Command: "gc", Flags: []string{"--json", "--ttl", "--vault", "--yes"}},
		{Command: "handoff"}, {Command: "heartbeat"}, {Command: "help"}, {Command: "hook", Subcommands: []string{"install"}}, {Command: "improve", Subcommands: []string{"scan"}},
		{Command: "init"}, {Command: "install"}, {Command: "land"}, {Command: "list"}, {Command: "logbook"},
		{Command: "knowledge", Subcommands: []string{"new"}},
		{Command: "migrate", Subcommands: []string{"close-policy", "evidence-policy", "vault-root"}},
		{Command: "new", Subcommands: []string{"decision", "epic", "gate", "task"}, Flags: []string{"--vault"}}, {Command: "next"}, {Command: "open"}, {Command: "packet"}, {Command: "print"},
		{Command: "projects", Subcommands: []string{"add", "disable", "enable", "limits", "list", "prune", "rebind", "remove"}}, {Command: "proof"}, {Command: "proposal"}, {Command: "publish", Subcommands: []string{"skill"}}, {Command: "purge"},
		{Command: "reconcile", Flags: []string{"--dry-run", "--id", "--json"}}, {Command: "redact"}, {Command: "redrive"}, {Command: "refresh"}, {Command: "reindex"}, {Command: "release"},
		{Command: "review", Subcommands: []string{"submit"}, Flags: []string{"--attempt", "--covers", "--gate-fingerprint", "--proof-fingerprint", "--source-sha", "--task-rev", "--verdict", "--work-rev"}},
		{Command: "runner", Subcommands: []string{"catalog", "profiles", "route"}, Flags: []string{"--bundled", "--json", "--lane", "--write"}}, {Command: "runner-wrapper"},
		{Command: "runs", Subcommands: []string{"claim", "events", "fail", "heartbeat", "inspect", "interrupt", "logs", "reclaim", "redrive", "release", "retire", "start", "submit"}},
		{Command: "search"}, {Command: "serve"}, {Command: "setup", Subcommands: []string{"doctor", "repair"}}, {Command: "show"},
		{Command: "skill", Subcommands: []string{"audit-agent-guidance", "bundle", "doctor", "pack", "route", "sync"}}, {Command: "state"}, {Command: "status"}, {Command: "streams"},
		{Command: "sync-repo-contract"}, {Command: "trace", Subcommands: []string{"list", "replay", "show"}}, {Command: "update"}, {Command: "validate"},
		{Command: "verify", Subcommands: []string{"add", "recipe"}},
		{Command: "vault", Subcommands: []string{"mount", "move", "repair", "set", "status", "unmount"}}, {Command: "version", Flags: []string{"--json"}},
		{Command: "wave", Subcommands: []string{"add", "arm", "brief", "create", "disarm", "pause", "preflight", "remove", "resume", "show"}},
		{Command: "work", Subcommands: []string{"fail", "heartbeat", "release", "start", "status", "submit"}, Flags: []string{"--json", "--vault"}}, {Command: "xcode", Subcommands: []string{"doctor"}},
	}
}

func sortCapabilitiesManifest(manifest *capabilitiesManifest) {
	sort.Slice(manifest.Commands, func(i, j int) bool { return manifest.Commands[i].Command < manifest.Commands[j].Command })
	for i := range manifest.Commands {
		sort.Strings(manifest.Commands[i].Subcommands)
		sort.Strings(manifest.Commands[i].Flags)
	}
	sort.Strings(manifest.Schemas.Task)
	sort.Strings(manifest.Schemas.Delivery)
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
	fmt.Println("Usage: tusker capabilities --json\n\nPrint the versioned, read-only installed-binary capability manifest.")
}
