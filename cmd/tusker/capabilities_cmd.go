package main

import (
	"fmt"
	"os"
	"runtime/debug"
	"sort"
)

const capabilitiesSchema = "tusker.capabilities/v1"

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

func capabilitiesCmd(args Args) error {
	if len(args) != 1 || !args.Bool("json") {
		return tuskerError(errorInvalidArg, "capabilities is read-only and requires --json")
	}
	info, _ := debugReadBuildInfo()
	executable, _ := executablePath()
	emitJSON(buildCapabilitiesManifest(info, executable))
	return nil
}

// Variables keep the command independently testable without relying on the
// test binary's build metadata or location.
var debugReadBuildInfo = debug.ReadBuildInfo
var executablePath = os.Executable

func buildCapabilitiesManifest(info *debug.BuildInfo, executable string) capabilitiesManifest {
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
			{Command: "legacy", Replacement: "V7 commands listed by tusker capabilities --json"},
		},
	}
	sortCapabilitiesManifest(&manifest)
	return manifest
}

// installedCapabilityCommands is intentionally a complete public CLI inventory,
// rather than a short list of the commands this feature happens to need. Keep
// new command families here when they are added to runInner.
func installedCapabilityCommands() []capabilityCommand {
	return []capabilityCommand{
		{Command: "accept"}, {Command: "attachments"}, {Command: "attempt"},
		{Command: "automation", Subcommands: []string{"advance-external", "collect-external", "dispatch", "explain", "external-loop", "plan", "queue", "status"}, Flags: []string{"--json"}},
		{Command: "brief"}, {Command: "capabilities", Flags: []string{"--json"}}, {Command: "claim"}, {Command: "close"},
		{Command: "closeout", Subcommands: []string{"status"}}, {Command: "compact"}, {Command: "config", Subcommands: []string{"resolve"}},
		{Command: "context", Subcommands: []string{"audit"}}, {Command: "daemon", Subcommands: []string{"install", "limits", "resume", "run", "service", "status", "stop"}},
		{Command: "dashboard"}, {Command: "delivery", Subcommands: []string{"context", "doctor", "import", "plan", "review", "rollout", "start"}, Flags: []string{"--by", "--confirm", "--json", "--plan", "--scope"}},
		{Command: "departure", Subcommands: []string{"check", "history", "hold", "resume", "status"}}, {Command: "digest"}, {Command: "discard"},
		{Command: "evidence"}, {Command: "escalate", Subcommands: []string{"ack"}}, {Command: "factory", Subcommands: []string{"operations"}},
		{Command: "feedback", Subcommands: []string{"add", "digest", "ingest", "promote", "review", "signals"}}, {Command: "finish"},
		{Command: "gate"}, {Command: "gate-ledger", Subcommands: []string{"check", "record"}}, {Command: "gate-run"}, {Command: "graph"},
		{Command: "handoff"}, {Command: "heartbeat"}, {Command: "hook", Subcommands: []string{"install"}}, {Command: "improve", Subcommands: []string{"scan"}},
		{Command: "init"}, {Command: "install"}, {Command: "land"}, {Command: "list"}, {Command: "logbook"},
		{Command: "migrate", Subcommands: []string{"close-policy", "evidence-policy", "gates", "v7", "vault-root"}},
		{Command: "new", Subcommands: []string{"decision", "epic", "gate", "task"}, Flags: []string{"--vault"}}, {Command: "next"}, {Command: "open"}, {Command: "packet"}, {Command: "print"},
		{Command: "projects", Subcommands: []string{"add", "disable", "enable", "limits", "list", "prune", "remove"}}, {Command: "proof"}, {Command: "proposal"}, {Command: "purge"},
		{Command: "reconcile"}, {Command: "redact"}, {Command: "redrive"}, {Command: "refresh"}, {Command: "release"},
		{Command: "review", Subcommands: []string{"submit"}, Flags: []string{"--attempt", "--covers", "--gate-fingerprint", "--proof-fingerprint", "--source-sha", "--task-rev", "--verdict", "--work-rev"}},
		{Command: "runner", Subcommands: []string{"catalog", "profiles", "route"}, Flags: []string{"--bundled", "--json", "--lane", "--write"}}, {Command: "runner-wrapper"},
		{Command: "runs", Subcommands: []string{"claim", "events", "fail", "heartbeat", "inspect", "interrupt", "logs", "reclaim", "redrive", "release", "retire", "start", "submit"}},
		{Command: "search"}, {Command: "serve"}, {Command: "setup", Subcommands: []string{"doctor", "repair"}}, {Command: "show"},
		{Command: "skill", Subcommands: []string{"audit-agent-guidance", "bundle", "doctor", "pack", "route", "sync"}}, {Command: "state"}, {Command: "status"}, {Command: "streams"},
		{Command: "sync-repo-contract"}, {Command: "trace", Subcommands: []string{"list", "replay", "show"}}, {Command: "update"}, {Command: "validate"},
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
