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
	Command     string   `json:"command"`
	Subcommands []string `json:"subcommands,omitempty"`
	Flags       []string `json:"flags,omitempty"`
	Purpose     string   `json:"purpose,omitempty"`
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
		Deprecations:        []capabilityDeprecation{{Command: "propose", Replacement: "proposal"}},
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

// installedCapabilityCommands is intentionally a complete public CLI inventory,
// rather than a short list of the commands this feature happens to need. Keep
// new command families here when they are added to runInner.
func installedCapabilityCommands() []capabilityCommand {
	return []capabilityCommand{
		{Command: "acp", Subcommands: []string{"doctor"}},
		{Command: "actor", Subcommands: []string{"correction"}, Flags: []string{"--by", "--corrected-actor", "--event-id", "--gate", "--json", "--original-sha256", "--receipt"}},
		{Command: "acp doctor", Flags: []string{"--auth-source", "--bundle-digest", "--json"}},
		{Command: "accept"}, {Command: "attachments"}, {Command: "attempt"},
		{Command: "automation", Subcommands: []string{"advance-external", "collect-external", "dispatch", "explain", "external-loop", "plan", "queue", "status"}, Flags: []string{"--json"}},
		{Command: "brief"}, {Command: "capabilities", Flags: []string{"--json"}}, {Command: "claim"}, {Command: "close"},
		{Command: "closeout", Subcommands: []string{"status"}}, {Command: "config", Subcommands: []string{"resolve"}},
		{Command: "context", Subcommands: []string{"audit"}}, {Command: "daemon", Subcommands: []string{"install", "limits", "resume", "run", "service", "status", "stop", "uninstall"}},
		{Command: "dashboard"}, {Command: "demo", Subcommands: []string{"check", "reset", "run", "seed", "status", "wait"}, Flags: []string{"--fail-once", "--fast", "--json", "--mode", "--profile", "--reject-once", "--repo", "--require-harness", "--scenario", "--timeout", "--until", "--waves", "--with-human-gate", "--with-second-project", "--yes"}, Purpose: "Seed and drive a disposable deterministic demo (one standalone task plus three waves, thirteen tasks) through the native CLI; seed is inert and reset previews by default. Offline timer lane is the default; --mode real resolves configured profiles and never substitutes another harness."},
		{Command: "departure", Subcommands: []string{"check", "history", "hold", "resume", "status"}}, {Command: "digest"}, {Command: "discard"},
		{Command: "docs", Subcommands: []string{"adopt", "backlinks", "browse", "check", "find", "map", "new", "read", "status", "verify"}, Flags: []string{"--approval-token", "--approve", "--by", "--dry-run", "--json", "--limit", "--section", "--table"}},
		{Command: "domain", Subcommands: []string{"canon", "list", "new", "show"}},
		{Command: "execution", Subcommands: []string{"attach", "bind", "cancel", "detach", "inbox", "launch", "list", "rebind", "register", "rename", "show"}, Flags: []string{"--json"}},
		{Command: "execution register", Flags: []string{"--by", "--connection-id", "--contact-name", "--contact-role", "--conversation-id", "--harness", "--if-generation", "--json", "--provider", "--source", "--task", "--wave"}, Purpose: "Allocate a direct-execution ID or, with --contact-role, atomically register a pre-existing external agent conversation as a role contact on a durable task or wave."},
		{Command: "evidence"}, {Command: "escalate", Subcommands: []string{"ack"}}, {Command: "factory", Subcommands: []string{"operations"}},
		{Command: "feedback", Subcommands: []string{"add", "digest", "ingest", "promote", "review", "signals"}}, {Command: "finish"},
		{Command: "gate"}, {Command: "gate-ledger", Subcommands: []string{"check", "record"}}, {Command: "gate-run"}, {Command: "gc", Flags: []string{"--json", "--ttl", "--vault", "--yes"}},
		{Command: "handoff"}, {Command: "heartbeat"}, {Command: "help"}, {Command: "improve", Subcommands: []string{"scan"}},
		{Command: "init", Flags: []string{"--isolated-vault", "--vault", "--yes"}}, {Command: "install"}, {Command: "land"}, {Command: "list"}, {Command: "logbook"},
		{Command: "knowledge", Subcommands: []string{"new"}},
		{Command: "migrate", Subcommands: []string{"evidence-policy", "vault-root"}},
		{Command: "message", Subcommands: []string{"apply", "ask", "consume", "list", "reply", "send", "show"}, Flags: []string{"--body", "--id", "--json", "--key", "--project", "--recipient", "--recipient-kind", "--reply-to", "--sender", "--yield"}, Purpose: "Persist and inspect correlated task or execution messages; transport and wakeup remain capability-gated."},
		{Command: "models", Subcommands: []string{"catalog", "profile-disable", "profile-enable", "profile-remove", "profile-set", "reset", "set", "show"}, Flags: []string{"--command", "--compact", "--display-name", "--effort", "--eligible-tiers", "--harness", "--if-revision", "--json", "--lane", "--level", "--model", "--name", "--preset", "--profiles", "--scope"}},
		{Command: "new", Subcommands: []string{"decision", "epic", "gate", "task"}, Flags: []string{"--architect", "--body-file", "--dependencies", "--domains", "--epic", "--evidence-budget", "--evidence-required", "--execute-profile", "--gates", "--generated-outputs", "--id", "--origin", "--owned-paths", "--peers", "--review-level", "--review-profile", "--review-reason", "--spec-refs", "--title", "--vault", "--work-level"}}, {Command: "next"}, {Command: "open"}, {Command: "packet"}, {Command: "print"},
		{Command: "projects", Subcommands: []string{"add", "disable", "enable", "limits", "list", "prune", "rebind", "remove"}, Flags: []string{"--allow-dirty", "--dry-run", "--id", "--json", "--repo", "--vault"}}, {Command: "proof"}, {Command: "proposal"}, {Command: "publish", Subcommands: []string{"skill"}}, {Command: "purge"},
		{Command: "reconcile", Flags: []string{"--dry-run", "--id", "--json"}}, {Command: "redact"}, {Command: "redrive"}, {Command: "refresh"}, {Command: "reindex"}, {Command: "release"}, {Command: "relaunch", Flags: []string{"--dry-run", "--json", "--repo", "--yes"}}, {Command: "reset", Flags: []string{"--dry-run", "--json", "--repo", "--yes"}},
		{Command: "review", Subcommands: []string{"submit"}, Flags: []string{"--attempt", "--covers", "--gate-fingerprint", "--proof-fingerprint", "--source-sha", "--task-rev", "--verdict", "--work-rev"}},
		{Command: "runner", Subcommands: []string{"catalog", "conformance", "profiles", "route", "test"}, Flags: []string{"--bundled", "--refresh", "--json", "--lane", "--write"}},
		{Command: "runner conformance", Flags: []string{"--exercise", "--external-containment", "--harness", "--json", "--live", "--preset", "--script", "--workspace"}, Purpose: "Probe and exercise an operator-installed CLI or ACP harness without claiming work."}, {Command: "runner-wrapper"},
		{Command: "runner test", Flags: []string{"--exercise", "--external-containment", "--harness", "--json", "--live", "--preset", "--quiet", "--script", "--workspace"}, Purpose: "Short agent-friendly alias for runner conformance; accepts the harness as the first positional argument."},
		{Command: "runs", Subcommands: []string{"claim", "events", "fail", "heartbeat", "inspect", "interrupt", "logs", "reclaim", "redrive", "release", "retire", "start", "submit"}},
		{Command: "search"}, {Command: "serve"}, {Command: "setup", Subcommands: []string{"doctor", "repair"}}, {Command: "show"},
		{Command: "skill", Subcommands: []string{"audit-agent-guidance", "bundle", "doctor", "pack", "route", "sync"}}, {Command: "state"}, {Command: "status"}, {Command: "streams"},
		{Command: "sync-repo-contract"}, {Command: "task", Subcommands: []string{"start", "update"}},
		{Command: "task update", Flags: []string{"--body-file", "--by", "--dependencies", "--generated-outputs", "--id", "--if-revision", "--json", "--owned-paths", "--rebind-contract", "--rebind-dependency-contracts", "--review-level", "--review-reason", "--spec-refs", "--title", "--work-level"}, Purpose: "CAS-mutate an existing task contract's mutable authoring fields or explicitly rebind its stored contract fingerprint; identity, history, and proof are preserved."},
		{Command: "task start", Flags: []string{"--by", "--current-workspace", "--json", "--mode"}, Purpose: "Authorize and claim one task: interactive claims in the current workspace through work start; background persists a task-scoped run directive for the runtime. Inside a paused wave the directive stays task-scoped and the wave remains paused."},
		{Command: "trace", Subcommands: []string{"list", "replay", "show"}}, {Command: "uninstall", Flags: []string{"--force-state", "--state", "--yes"}}, {Command: "update"}, {Command: "validate"},
		{Command: "verify", Subcommands: []string{"add", "recipe", "remove"}},
		{Command: "vault", Subcommands: []string{"mount", "move", "repair", "set", "status", "unmount"}}, {Command: "version", Flags: []string{"--json"}},
		{Command: "wave", Subcommands: []string{"add", "brief", "create", "outcome", "pause", "remove", "resume", "review", "show", "start"}},
		{Command: "wave create", Flags: []string{"--file", "--id", "--json", "--request-key", "--summary", "--title"}, Purpose: "Create a wave from existing task IDs, or atomically author a complete tusker.wave-authoring/v1 request with --file and --request-key; direct authoring is inert and idempotent."},
		{Command: "wave review", Flags: []string{"--json"}, Purpose: "Read the durable wave/task/gate material projection: state, authorization, members, frontiers, blockers, and controls."},
		{Command: "wave pause", Flags: []string{"--by", "--json"}, Purpose: "Pause new wave-owned admissions while admitted attempts finish; preserves the exact authorization fingerprint, actor, and timestamp."},
		{Command: "wave resume", Flags: []string{"--by", "--json"}, Purpose: "Restore a paused wave to armed only while the current material still matches the stored authorization fingerprint; daemon polling then advances the remaining frontier automatically."},
		{Command: "wave start", Flags: []string{"--by", "--json", "--mode"}, Purpose: "Authorize the exact current wave material and queue eligible roots as durable run directives; daemon polling advances each dependency frontier automatically. An offline daemon leaves an authorized wave Waiting."},
		{Command: "work", Subcommands: []string{"cancel", "fail", "heartbeat", "profile", "progress", "readiness", "reconcile", "release", "retry", "review", "start", "status", "submit", "wait"}, Flags: []string{"--current-workspace", "--json", "--vault"}}, {Command: "xcode", Subcommands: []string{"doctor"}},
	}
}

func sortCapabilitiesManifest(manifest *capabilitiesManifest) {
	sort.Slice(manifest.Commands, func(i, j int) bool { return manifest.Commands[i].Command < manifest.Commands[j].Command })
	for i := range manifest.Commands {
		sort.Strings(manifest.Commands[i].Subcommands)
		sort.Strings(manifest.Commands[i].Flags)
	}
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
