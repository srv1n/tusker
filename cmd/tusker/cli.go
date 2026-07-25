package main

import (
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	tuskerPointerBegin    = "<!-- tusker:epic-index:begin -->"
	tuskerPointerEnd      = "<!-- tusker:epic-index:end -->"
	readmeOverviewBegin   = "<!-- tusker:overview:begin -->"
	readmeOverviewEnd     = "<!-- tusker:overview:end -->"
	dashboardRunsBegin    = "<!-- tusker:live-runs:begin -->"
	dashboardRunsEnd      = "<!-- tusker:live-runs:end -->"
	readmeDefaultOverview = "_Describe this project in 1-3 paragraphs: what it is, who uses it, and what's in scope. Everything between the overview markers is preserved across `tusker reindex` — only the epic roster below is regenerated._"
	defaultConfigYAML     = `# Tusker per-vault config. Policy surface for the dispatcher + validator.
# The dispatcher re-reads this on every tick; no restart needed.

version: 1

agents:
  # Names that may appear in task.assignee. "sarav" (or any human name) is fine;
  # only agents listed here are dispatchable.
  enabled:
    - sarav
    - claude-code
    - codex
    - gemini
  # Hard cap on concurrent active runs per agent. 0 means never dispatch (human-only).
  concurrency:
    claude-code: 2
    codex: 1
    gemini: 1
    sarav: 0

poll:
  interval_seconds: 60

# Hook commands run by the dispatcher at lifecycle events. Each entry is a shell
# command string; env vars include TUSKER_VAULT, TUSKER_ID, TUSKER_EVENT,
# TUSKER_ACTOR, TUSKER_DISPATCH_STATE. Non-zero exit = hook failure.
hooks:
  pre_claim: []
  post_claim: []
  pre_release: []
  on_fail: []
hook_timeout_seconds: 120

retry:
  max_attempts: 3
  backoff_seconds: [30, 120, 600]

budget:
  monthly_usd_ceiling: null
  daily_usd_ceiling: null

workspace:
  # Relative to the vault. Each run gets a subdirectory under this root.
  root: _system/workspaces
  # "worktree" creates a git worktree per run; "copy" does a plain copy.
  isolation: worktree

definition_of_done:
  require_code_complete: true
  require_user_verified_for_ui: true
`
)

type Args map[string]string

type Config struct {
	Version int `yaml:"version" json:"version"`
	Agents  struct {
		Enabled     []string       `yaml:"enabled" json:"enabled"`
		Concurrency map[string]int `yaml:"concurrency" json:"concurrency"`
	} `yaml:"agents" json:"agents"`
	Poll struct {
		IntervalSeconds int `yaml:"interval_seconds" json:"interval_seconds"`
	} `yaml:"poll" json:"poll"`
	Hooks struct {
		PreClaim   []string `yaml:"pre_claim" json:"pre_claim"`
		PostClaim  []string `yaml:"post_claim" json:"post_claim"`
		PreRelease []string `yaml:"pre_release" json:"pre_release"`
		OnFail     []string `yaml:"on_fail" json:"on_fail"`
	} `yaml:"hooks" json:"hooks"`
	HookTimeoutSeconds int `yaml:"hook_timeout_seconds" json:"hook_timeout_seconds"`
	Retry              struct {
		MaxAttempts    int   `yaml:"max_attempts" json:"max_attempts"`
		BackoffSeconds []int `yaml:"backoff_seconds" json:"backoff_seconds"`
	} `yaml:"retry" json:"retry"`
	Budget struct {
		MonthlyUSDCeiling any `yaml:"monthly_usd_ceiling" json:"monthly_usd_ceiling"`
		DailyUSDCeiling   any `yaml:"daily_usd_ceiling" json:"daily_usd_ceiling"`
	} `yaml:"budget" json:"budget"`
	Workspace struct {
		Root      string `yaml:"root" json:"root"`
		Isolation string `yaml:"isolation" json:"isolation"`
	} `yaml:"workspace" json:"workspace"`
	DefinitionOfDone struct {
		RequireCodeComplete      bool `yaml:"require_code_complete" json:"require_code_complete"`
		RequireUserVerifiedForUI bool `yaml:"require_user_verified_for_ui" json:"require_user_verified_for_ui"`
	} `yaml:"definition_of_done" json:"definition_of_done"`
}

func parseCLI(argv []string) (string, Args) {
	var command string
	if len(argv) > 1 {
		command = argv[1]
		if command == "help" && len(argv) > 2 && !isCLIFlag(argv[2]) {
			command = "help " + argv[2]
			if len(argv) > 3 && !isCLIFlag(argv[3]) && commandTakesSubcommand(argv[2]) {
				command = command + " " + argv[3]
				return command, parseArgs(argv[4:])
			}
			return command, parseArgs(argv[3:])
		}
		if command == "legacy" && len(argv) > 2 && !isCLIFlag(argv[2]) {
			legacyCommand := argv[2]
			command = "legacy " + legacyCommand
			if len(argv) > 3 && !isCLIFlag(argv[3]) && commandTakesSubcommand(legacyCommand) {
				command = command + " " + argv[3]
				return command, parseArgs(argv[4:])
			}
			return command, parseArgs(argv[3:])
		}
		if len(argv) > 2 && !isCLIFlag(argv[2]) && commandTakesSubcommand(command) {
			command = command + " " + argv[2]
			return command, parseArgs(argv[3:])
		}
	}
	if len(argv) <= 2 {
		return command, parseArgs(nil)
	}
	return command, parseArgs(argv[2:])
}

func isCLIFlag(value string) bool {
	return strings.HasPrefix(value, "-")
}

func commandTakesSubcommand(command string) bool {
	switch command {
	case "docs", "domain", "knowledge", "publish", "skill", "setup", "new", "vault", "daemon", "automation", "projects", "runs", "gate-ledger", "context", "migrate", "hook", "legacy", "feedback", "improve", "wave", "delivery", "trace", "escalate", "departure":
		return true
	default:
		return false
	}
}

func parseArgs(argv []string) Args {
	args := Args{}
	var positionals []string
	for i := 0; i < len(argv); i++ {
		current := argv[i]
		if !strings.HasPrefix(current, "--") {
			positionals = append(positionals, current)
			continue
		}
		key := strings.TrimPrefix(current, "--")
		if i+1 >= len(argv) || strings.HasPrefix(argv[i+1], "--") {
			args[key] = "true"
			continue
		}
		args[key] = argv[i+1]
		i++
	}
	if len(positionals) > 0 {
		args["_pos"] = strings.Join(positionals, "\n")
		for i, value := range positionals {
			args[fmt.Sprintf("_pos%d", i)] = value
		}
	}
	return args
}

func (a Args) String(key string) string {
	return a[key]
}

func (a Args) Bool(key string) bool {
	value, ok := a[key]
	if !ok {
		return false
	}
	if value == "" {
		return true
	}
	parsed, err := parseBooleanArg(value, true)
	if err != nil {
		return true
	}
	return parsed
}

func run(command string, args Args) (int, error) {
	beginCLIVaultMutationTracking()
	code, err := runInner(command, args)
	vaults := finishCLIVaultMutationTracking()
	if code == 0 && err == nil {
		for _, vault := range vaults {
			notifyDaemonForVaultPath(vault)
		}
		if len(vaults) == 0 && cliCommandMutatesVault(command) {
			notifyDaemonForVault(args)
		}
		if cliCommandMutatesProjectRegistry(command) {
			_ = sendDaemonControlOneWay(DefaultStateRoot(), daemonControlRequest{Command: "reconcile_registry"}, 250*time.Millisecond)
		}
	}
	return code, err
}

func cliCommandMutatesVault(command string) bool {
	switch command {
	case "status", "discard", "verify add", "evidence add", "gate new", "gate satisfy", "gate waive", "new task", "new epic", "new decision", "delivery import", "reconcile", "finish", "close", "accept", "handoff":
		return true
	default:
		return false
	}
}

func notifyDaemonForVault(args Args) {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return
	}
	notifyDaemonForVaultPath(vaultPath)
}

func runInner(command string, args Args) (int, error) {
	if args.Bool("help") && command != "" {
		if printCommandHelp(command) {
			return 0, nil
		}
	}
	if command == "legacy" || strings.HasPrefix(command, "legacy ") {
		if args.Bool("json") {
			emitJSON(map[string]any{"ok": false, "error": errorToIssue(tuskerError(errorInvalidArg, "legacy commands are disabled in the V7-only CLI surface"))})
		} else {
			printLegacyHelp()
		}
		return 1, nil
	}
	switch command {
	case "runner-wrapper":
		return 0, runnerWrapperCmd(args)
	case "new epic":
		return 0, newV7Epic(args)
	case "new task":
		return 0, newV7Task(args)
	case "new bug":
		return legacyOnlyCommand("new bug", "legacy new bug")
	case "new doc":
		return legacyOnlyCommand("new doc", "legacy new doc")
	case "new gate":
		return 0, newV7Gate(args)
	case "new decision":
		return 0, newV7Decision(args)
	case "status":
		return 0, statusCmd(args)
	case "discard":
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
		return 0, discardV7Cmd(args)
	case "next":
		return 0, nextCmd(args)
	case "work start", "work status", "work heartbeat", "work submit", "work fail", "work release":
		return 0, workSessionCmd(args, strings.TrimPrefix(command, "work "))
	case "claim":
		return 0, claimCmd(args)
	case "heartbeat":
		return 0, heartbeatV7Cmd(args)
	case "release":
		return 0, releaseV7Cmd(args)
	case "handoff":
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
		return 0, attemptV7HandoffCmd(args)
	case "finish":
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
		return 0, finishV7Cmd(args)
	case "gate":
		return 0, gateV7Cmd(args)
	case "wave":
		return 0, waveV7Cmd(args)
	case "wave create":
		return 0, waveV7CreateCmd(args)
	case "wave add":
		return 0, waveV7AddCmd(args)
	case "wave remove":
		return 0, waveV7RemoveCmd(args)
	case "wave show":
		return 0, waveV7ShowCmd(args)
	case "wave brief":
		return 0, waveV7BriefCmd(args)
	case "wave preflight":
		return 0, waveV7PreflightCmd(args)
	case "wave arm":
		return 0, waveV7ArmCmd(args)
	case "wave pause":
		return 0, waveV7PauseCmd(args)
	case "wave resume":
		return 0, waveV7ResumeCmd(args)
	case "wave disarm":
		return 0, waveV7DisarmCmd(args)
	case "delivery plan":
		return 0, deliveryPlanCmd(args)
	case "delivery import":
		return 0, deliveryImportCmd(args)
	case "delivery doctor":
		return 0, deliveryDoctorCmd(args)
	case "delivery rollout":
		return 0, deliveryRolloutCmd(args)
	case "escalate":
		return 0, escalationV7Cmd(args)
	case "escalate ack":
		return 0, escalationV7AckCmd(args)
	case "digest":
		return 0, digestCmd(args)
	case "logbook":
		return 0, logbookCmd(args)
	case "trace":
		return 0, traceV7Cmd(args)
	case "trace list":
		return 0, traceListCmd(args)
	case "trace show":
		return 0, traceShowCmd(args)
	case "trace replay":
		return 0, traceReplayCmd(args)
	case "land":
		return 0, landV7Cmd(args)
	case "departure":
		printDepartureHelp()
		return 0, nil
	case "departure check":
		return 0, departureCheckCmd(args)
	case "departure status":
		return 0, departureStatusCmd(args)
	case "departure history":
		return 0, departureHistoryCmd(args)
	case "departure hold":
		return 0, departureHoldCmd(args)
	case "departure resume":
		return 0, departureResumeCmd(args)
	case "proof":
		return 0, proofV7Cmd(args)
	case "feedback":
		return 0, feedbackV7Cmd(args)
	case "feedback add":
		args["_pos0"] = "add"
		return 0, feedbackV7Cmd(args)
	case "feedback digest":
		args["_pos0"] = "digest"
		return 0, feedbackV7Cmd(args)
	case "feedback ingest":
		args["_pos0"] = "ingest"
		return 0, feedbackV7Cmd(args)
	case "feedback signals":
		args["_pos0"] = "signals"
		return 0, feedbackV7Cmd(args)
	case "feedback review":
		args["_pos0"] = "review"
		return 0, feedbackV7Cmd(args)
	case "feedback promote":
		if ref := strings.TrimSpace(args.String("_pos0")); ref != "" {
			args["_pos1"] = ref
		}
		args["_pos0"] = "promote"
		return 0, feedbackV7Cmd(args)
	case "improve":
		return 0, improveV7Cmd(args)
	case "improve scan":
		args["_pos0"] = "scan"
		return 0, improveV7Cmd(args)
	case "xcode doctor":
		return 0, xcodeDoctorCmd(args)
	case "xcode":
		printXcodeHelp()
		return 0, nil
	case "closeout", "closeout status":
		return 0, closeoutV7Cmd(args)
	case "evidence":
		return 0, evidenceCmd(args)
	case "attachments":
		return 0, attachmentsV7Cmd(args)
	case "attempt":
		return 0, attemptV7Cmd(args)
	case "proposal", "propose":
		return 0, proposalV7Cmd(args)
	case "redact":
		return 0, redactV7Cmd(args)
	case "verify":
		if strings.ToLower(args.String("_pos0")) == "add" {
			return 0, verifyV7AddCmd(args)
		}
		if strings.ToLower(args.String("_pos0")) == "recipe" {
			return 0, verifyV7RecipeCmd(args)
		}
		return legacyOnlyCommand("verify", "legacy verify")
	case "close":
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
		return 0, closeV7Cmd(args)
	case "accept":
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
		return 0, acceptV7Cmd(args)
	case "redrive":
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
		return 0, redriveCmd(args)
	case "legacy new epic":
		return legacyOnlyCommand("legacy new epic", "")
	case "legacy new task":
		return legacyOnlyCommand("legacy new task", "")
	case "legacy new bug":
		return legacyOnlyCommand("legacy new bug", "")
	case "legacy new doc":
		return legacyOnlyCommand("legacy new doc", "")
	case "legacy next":
		return legacyOnlyCommand("legacy next", "")
	case "legacy verify":
		return legacyOnlyCommand("legacy verify", "")
	case "legacy close":
		return legacyOnlyCommand("legacy close", "")
	case "legacy":
		printLegacyHelp()
		return 0, nil
	case "legacy init":
		return legacyOnlyCommand("legacy init", "")
	case "reconcile":
		return 0, reconcileV7Cmd(args)
	case "brief":
		return 0, briefV7Cmd(args)
	case "packet":
		return 0, packetV7Cmd(args)
	case "dashboard":
		return 0, dashboardV7Cmd(args)
	case "state":
		return 0, stateV7Cmd(args)
	case "hook install":
		return 0, hookInstallCmd(args)
	case "hook":
		printV7Help()
		return 0, nil
	case "migrate v7":
		return legacyOnlyCommand("migrate v7", "legacy migrate v7")
	case "migrate gates":
		return legacyOnlyCommand("migrate gates", "legacy migrate gates")
	case "migrate evidence-policy":
		return 0, migrateV7EvidencePolicyCmd(args)
	case "migrate close-policy":
		return 0, migrateClosePolicyCmd(args)
	case "migrate vault-root":
		return 0, migrateVaultRootCmd(args)
	case "legacy migrate v7":
		return legacyOnlyCommand("legacy migrate v7", "")
	case "legacy migrate gates":
		return legacyOnlyCommand("legacy migrate gates", "")
	case "reindex":
		return 0, reindex(args)
	case "validate":
		return validateCmd(args)
	case "purge":
		return 0, tuskerPurgeCmd(args)
	case "list":
		return 0, listCmd(args)
	case "search":
		return 0, searchCmd(args)
	case "show":
		return 0, showCmd(args)
	case "print":
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
		return 0, printCmd(args)
	case "open":
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
		return 0, openCmd(args)
	case "compact":
		return 0, compactCmd(args)
	case "context audit":
		return 0, contextAuditCmd(args)
	case "docs find":
		return 0, docsCmd("find", args)
	case "docs new":
		return 0, docsCmd("new", args)
	case "docs init":
		return legacyOnlyCommand("docs init", "legacy docs init")
	case "docs model":
		return legacyOnlyCommand("docs model", "legacy docs model")
	case "docs map":
		return 0, docsCmd("map", args)
	case "docs catalog":
		return legacyOnlyCommand("docs catalog", "legacy docs catalog")
	case "docs freshness":
		return legacyOnlyCommand("docs freshness", "legacy docs freshness")
	case "docs check":
		return legacyOnlyCommand("docs check", "legacy docs check")
	case "docs apply":
		return legacyOnlyCommand("docs apply", "legacy docs apply")
	case "docs noop":
		return legacyOnlyCommand("docs noop", "legacy docs noop")
	case "docs waive":
		return legacyOnlyCommand("docs waive", "legacy docs waive")
	case "docs export":
		return legacyOnlyCommand("docs export", "legacy docs export")
	case "docs dev":
		return legacyOnlyCommand("docs dev", "legacy docs dev")
	case "docs build":
		return legacyOnlyCommand("docs build", "legacy docs build")
	case "docs":
		return legacyOnlyCommand("docs", "legacy docs")
	case "legacy docs init":
		return legacyOnlyCommand("legacy docs init", "")
	case "legacy docs model":
		return legacyOnlyCommand("legacy docs model", "")
	case "legacy docs map":
		return legacyOnlyCommand("legacy docs map", "")
	case "legacy docs catalog":
		return legacyOnlyCommand("legacy docs catalog", "")
	case "legacy docs freshness":
		return legacyOnlyCommand("legacy docs freshness", "")
	case "legacy docs check":
		return legacyOnlyCommand("legacy docs check", "")
	case "legacy docs apply":
		return legacyOnlyCommand("legacy docs apply", "")
	case "legacy docs noop":
		return legacyOnlyCommand("legacy docs noop", "")
	case "legacy docs waive":
		return legacyOnlyCommand("legacy docs waive", "")
	case "legacy docs export":
		return legacyOnlyCommand("legacy docs export", "")
	case "legacy docs dev":
		return legacyOnlyCommand("legacy docs dev", "")
	case "legacy docs build":
		return legacyOnlyCommand("legacy docs build", "")
	case "legacy docs":
		return legacyOnlyCommand("legacy docs", "")
	case "domain list":
		return 0, domainV7ListCmd(args)
	case "domain show":
		return 0, domainV7ShowCmd(args)
	case "domain new":
		return 0, newV7Domain(args)
	case "domain canon":
		return 0, domainV7CanonCmd(args)
	case "domain graph":
		return legacyOnlyCommand("domain graph", "")
	case "domain":
		printDomainHelp()
		return 0, nil
	case "legacy domain list":
		return legacyOnlyCommand("legacy domain list", "")
	case "legacy domain show":
		return legacyOnlyCommand("legacy domain show", "")
	case "legacy domain new":
		return legacyOnlyCommand("legacy domain new", "")
	case "legacy domain canon":
		return legacyOnlyCommand("legacy domain canon", "")
	case "legacy domain graph":
		return legacyOnlyCommand("legacy domain graph", "")
	case "legacy domain":
		return legacyOnlyCommand("legacy domain", "")
	case "knowledge map":
		return legacyOnlyCommand("knowledge map", "legacy knowledge map")
	case "knowledge list":
		return legacyOnlyCommand("knowledge list", "legacy knowledge list")
	case "knowledge show":
		return legacyOnlyCommand("knowledge show", "legacy knowledge show")
	case "knowledge route":
		return legacyOnlyCommand("knowledge route", "legacy knowledge route")
	case "knowledge freshness":
		return legacyOnlyCommand("knowledge freshness", "legacy knowledge freshness")
	case "knowledge check":
		return legacyOnlyCommand("knowledge check", "legacy knowledge check")
	case "knowledge apply":
		return legacyOnlyCommand("knowledge apply", "legacy knowledge apply")
	case "knowledge noop":
		return legacyOnlyCommand("knowledge noop", "legacy knowledge noop")
	case "knowledge waive":
		return legacyOnlyCommand("knowledge waive", "legacy knowledge waive")
	case "knowledge new":
		return 0, knowledgeV7NewCmd(args)
	case "skill doctor":
		return skillV7DoctorCmd(args)
	case "skill route":
		return 0, skillV7RouteCmd(args)
	case "skill pack":
		return 0, skillV7PackCmd(args)
	case "skill sync":
		return 0, skillSyncCmd(args)
	case "skill bundle":
		return 0, skillBundleCmd(args)
	case "skill audit-agent-guidance":
		return skillV7AuditAgentGuidanceCmd(args)
	case "setup doctor":
		return 0, setupDoctorCmd(args, false)
	case "setup repair":
		return 0, setupDoctorCmd(args, true)
	case "setup":
		printSetupHelp()
		return 0, nil
	case "skill":
		printSkillHelp()
		return 0, nil
	case "knowledge":
		printKnowledgeHelp()
		return 0, nil
	case "legacy knowledge map":
		return legacyOnlyCommand("legacy knowledge map", "")
	case "legacy knowledge list":
		return legacyOnlyCommand("legacy knowledge list", "")
	case "legacy knowledge show":
		return legacyOnlyCommand("legacy knowledge show", "")
	case "legacy knowledge route":
		return legacyOnlyCommand("legacy knowledge route", "")
	case "legacy knowledge freshness":
		return legacyOnlyCommand("legacy knowledge freshness", "")
	case "legacy knowledge check":
		return legacyOnlyCommand("legacy knowledge check", "")
	case "legacy knowledge apply":
		return legacyOnlyCommand("legacy knowledge apply", "")
	case "legacy knowledge noop":
		return legacyOnlyCommand("legacy knowledge noop", "")
	case "legacy knowledge waive":
		return legacyOnlyCommand("legacy knowledge waive", "")
	case "legacy knowledge new":
		return legacyOnlyCommand("legacy knowledge new", "")
	case "legacy knowledge":
		return legacyOnlyCommand("legacy knowledge", "")
	case "publish export":
		return legacyOnlyCommand("publish export", "legacy publish export")
	case "publish build":
		return legacyOnlyCommand("publish build", "legacy publish build")
	case "publish dev":
		return legacyOnlyCommand("publish dev", "legacy publish dev")
	case "publish llms":
		return legacyOnlyCommand("publish llms", "legacy publish llms")
	case "publish skill":
		return 0, publishSkillCmd(args)
	case "publish":
		printPublishHelp()
		return 0, nil
	case "legacy publish export":
		return legacyOnlyCommand("legacy publish export", "")
	case "legacy publish build":
		return legacyOnlyCommand("legacy publish build", "")
	case "legacy publish dev":
		return legacyOnlyCommand("legacy publish dev", "")
	case "legacy publish llms":
		return legacyOnlyCommand("legacy publish llms", "")
	case "legacy publish skill":
		return legacyOnlyCommand("legacy publish skill", "")
	case "legacy publish":
		return legacyOnlyCommand("legacy publish", "")
	case "vault set":
		return 0, vaultSetCmd(args)
	case "vault status":
		return 0, vaultStatusCmd(args)
	case "vault mount":
		return 0, vaultMountCmd(args)
	case "vault unmount":
		return 0, vaultUnmountCmd(args)
	case "vault repair":
		return 0, vaultRepairCmd(args)
	case "vault move":
		return 0, vaultMoveCmd(args)
	case "vault":
		printVaultHelp()
		return 0, nil
	case "daemon run":
		return 0, daemonRunCmd(args)
	case "daemon install":
		args["_pos0"] = "install"
		return 0, daemonServiceCmd(args)
	case "daemon uninstall":
		args["_pos0"] = "uninstall"
		return 0, daemonServiceCmd(args)
	case "daemon status":
		return 0, daemonStatusCmd(args)
	case "daemon stop":
		return 0, daemonStopCmd(args)
	case "daemon limits":
		return 0, daemonLimitsCmd(args)
	case "daemon resume":
		return 0, daemonResumeCmd(args)
	case "daemon service":
		return 0, daemonServiceCmd(args)
	case "daemon":
		printDaemonHelp()
		return 0, nil
	case "config resolve":
		args["key"] = firstNonEmpty(args.String("key"), args.String("_pos0"))
		return 0, configResolveCmd(args)
	case "config":
		printConfigHelp()
		return 0, nil
	case "automation status":
		return 0, automationStatusCmd(args)
	case "automation queue":
		return 0, automationQueueCmd(args)
	case "automation explain":
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
		return 0, automationExplainCmd(args)
	case "automation plan":
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
		return 0, automationPlanCmd(args)
	case "automation dispatch":
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
		return 0, automationDispatchCmd(args)
	case "automation collect-external":
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
		return 0, automationCollectExternalCmd(args)
	case "automation external-loop":
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
		return 0, automationExternalLoopCmd(args)
	case "automation advance-external":
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
		return 0, automationAdvanceExternalCmd(args)
	case "automation":
		printAutomationHelp()
		return 0, nil
	case "projects add":
		return 0, projectsAddCmd(args)
	case "projects list":
		return 0, projectsListCmd(args)
	case "projects limits":
		return 0, projectsLimitsCmd(args)
	case "projects enable":
		return 0, projectsEnableCmd(args)
	case "projects disable":
		return 0, projectsDisableCmd(args)
	case "projects remove":
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
		return 0, projectsRemoveCmd(args)
	case "projects prune":
		return 0, projectsPruneCmd(args)
	case "projects":
		printProjectsHelp()
		return 0, nil
	case "runs inspect":
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
		return 0, runsInspectCmd(args)
	case "streams":
		return 0, streamsCmd(args)
	// gate-run is deliberately not "gate run": "gate" is the human-gate
	// namespace and must keep its own subcommand parsing.
	case "gate-run":
		return gateRunCmd(args)
	case "gate-ledger check", "gate-ledger record":
		return 0, gateLedgerCmd(args, strings.TrimPrefix(command, "gate-ledger "))
	case "runs claim":
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
		return 0, runsClaimCmd(args)
	case "runs start", "runs heartbeat", "runs submit", "runs fail", "runs reclaim":
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
		return 0, runsLifecycleCmd(args, strings.TrimPrefix(command, "runs "))
	case "runs logs":
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
		return 0, runsLogsCmd(args)
	case "runs events":
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
		return 0, runsEventsCmd(args)
	case "runs interrupt":
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
		return 0, runsInterruptCmd(args)
	case "runs release":
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
		return 0, runsReleaseCmd(args)
	case "runs retire":
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
		return 0, runsRetireCmd(args)
	case "runs redrive":
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
		return 0, redriveCmd(args)
	case "runs":
		printRunsHelp()
		return 0, nil
	case "serve":
		return 0, serveCmd(args)
	case "graph":
		return legacyOnlyCommand("graph", "legacy graph")
	case "legacy graph":
		return legacyOnlyCommand("legacy graph", "")
	case "refresh":
		return 0, refreshCmd(args)
	case "install":
		return 0, installCmd(args)
	case "update":
		return 0, updateCmd(args)
	case "sync-repo-contract":
		return 0, syncRepoContract(args)
	case "init":
		return 0, initCmd(args)
	case "help new", "help new epic", "help new task", "help new bug", "help new doc", "help new gate", "help new decision":
		printNewHelp()
		return 0, nil
	case "help status":
		printStatusHelp()
		return 0, nil
	case "help discard":
		printDiscardHelp()
		return 0, nil
	case "help next":
		printNextHelp()
		return 0, nil
	case "help claim":
		printClaimHelp()
		return 0, nil
	case "help evidence":
		printEvidenceHelp()
		return 0, nil
	case "help migrate vault-root":
		printMigrateVaultRootHelp()
		return 0, nil
	case "help handoff", "help gate", "help wave", "help wave create", "help wave add", "help wave remove", "help wave show", "help wave brief", "help land", "help attempt", "help proposal", "help propose", "help brief", "help packet", "help closeout", "help closeout status", "help dashboard", "help reconcile", "help state", "help hook", "help hook install", "help migrate", "help migrate v7", "help migrate gates":
		printV7Help()
		return 0, nil
	case "help feedback":
		printFeedbackHelp()
		return 0, nil
	case "help improve", "help improve scan":
		printImproveHelp()
		return 0, nil
	case "help xcode", "help xcode doctor":
		printXcodeHelp()
		return 0, nil
	case "help verify":
		printVerifyHelp()
		return 0, nil
	case "help close":
		printCloseHelp()
		return 0, nil
	case "help list":
		printListHelp()
		return 0, nil
	case "help search":
		printSearchHelp()
		return 0, nil
	case "help show":
		printShowHelp()
		return 0, nil
	case "help print":
		printPrintHelp()
		return 0, nil
	case "help open":
		printOpenHelp()
		return 0, nil
	case "help compact":
		printCompactHelp()
		return 0, nil
	case "help context", "help context audit":
		printContextHelp()
		return 0, nil
	case "help validate":
		printValidateHelp()
		return 0, nil
	case "help reindex":
		printReindexHelp()
		return 0, nil
	case "help docs", "help docs init", "help docs model", "help docs map", "help docs catalog", "help docs freshness", "help docs check", "help docs apply", "help docs noop", "help docs waive", "help docs export", "help docs dev", "help docs build":
		printDocsHelp()
		return 0, nil
	case "help domain", "help domain list", "help domain show", "help domain new", "help domain canon", "help domain graph":
		printDomainHelp()
		return 0, nil
	case "help knowledge", "help knowledge map", "help knowledge list", "help knowledge show", "help knowledge route", "help knowledge freshness", "help knowledge check", "help knowledge apply", "help knowledge noop", "help knowledge waive", "help knowledge new":
		printKnowledgeHelp()
		return 0, nil
	case "help skill", "help skill doctor", "help skill route", "help skill pack":
		printSkillHelp()
		return 0, nil
	case "help publish", "help publish export", "help publish build", "help publish dev", "help publish llms", "help publish skill":
		printPublishHelp()
		return 0, nil
	case "help graph":
		printGraphHelp()
		return 0, nil
	case "help vault", "help vault set", "help vault status", "help vault mount", "help vault unmount", "help vault repair", "help vault move":
		printVaultHelp()
		return 0, nil
	case "help daemon", "help daemon run", "help daemon status", "help daemon limits", "help daemon resume", "help daemon stop", "help daemon service":
		printDaemonHelp()
		return 0, nil
	case "help automation", "help automation status", "help automation queue", "help automation explain", "help automation plan", "help automation dispatch", "help automation collect-external", "help automation external-loop", "help automation advance-external":
		printAutomationHelp()
		return 0, nil
	case "help config", "help config resolve":
		printConfigHelp()
		return 0, nil
	case "help projects", "help projects add", "help projects list", "help projects limits", "help projects enable", "help projects disable", "help projects remove", "help projects prune":
		printProjectsHelp()
		return 0, nil
	case "help runs", "help runs inspect", "help runs logs", "help runs events", "help runs interrupt", "help runs release", "help runs retire", "help runs redrive", "help redrive":
		printRunsHelp()
		return 0, nil
	case "help serve":
		printServeHelp()
		return 0, nil
	case "help refresh":
		printRefreshHelp()
		return 0, nil
	case "help install":
		printInstallHelp()
		return 0, nil
	case "help update":
		printUpdateHelp()
		return 0, nil
	case "help purge":
		printPurgeHelp()
		return 0, nil
	case "help sync-repo-contract":
		printSyncRepoContractHelp()
		return 0, nil
	case "help init":
		printInitHelp()
		return 0, nil
	case "help", "--help", "-h", "":
		printHelp()
		return 0, nil
	default:
		if args.Bool("json") {
			emitJSON(map[string]any{"ok": false, "error": errorToIssue(tuskerError(errorInvalidArg, "unknown command: "+command))})
		} else {
			fmt.Fprintf(os.Stderr, "Unknown command: %s\n", command)
			printHelp()
		}
		return 1, nil
	}
}

func legacyOnlyCommand(command, _ string) (int, error) {
	return 1, removedSurfaceError(command)
}

func printHelp() {
	fmt.Println(`Tusker - V7 repo-local work tracking

Vault discovery: if [--vault] is omitted, tusker walks up from the current
working directory looking for a repo-local .tusker/ vault. V7 markers include
tusker.yaml, .tusker/work, and .tusker/knowledge/domains.

Start here:
  tusker init --yes
  tusker new epic --vault ./.tusker --acronym APP --title "App foundation"
  tusker new task --vault ./.tusker --epic APP --title "Implement auth" --size m --risk medium

Commands:
  init                initialize or refresh a repo vault
  new                 create V7 epics, tasks, gates, and decisions
  list                list work records
  search              search tracker notes without generated files or attachments
  show                show a bounded note capsule or selected section
  print               render a Tusker note as terminal-friendly Markdown
  open                open a Tusker note in the OS, editor, or Obsidian
  compact             remove empty optional metadata and disposable note scaffolding
  context             audit Codex JSONL context and tool-output bloat
  status              move a V7 task through its workflow
  discard             abandon work safely while preserving its history
  next                show the next pickable V7 task
  work                atomically own and retire one interactive work session
  claim               compatibility alias for work start
  evidence            add V7 evidence records
  gate                list/satisfy/waive/obsolete V7 gates
  wave                create, edit, and show named task batches
  land                run the serialized wave merge lane
  feedback            add agent feedback notes and generate digests
  logbook             render a plain-language daily digest for a product reader
  improve             opt-in scans for repeated work worth packaging
  xcode               diagnose Xcode generated build-state failures
  attempt             start or hand off V7 attempts
  handoff             hand off the latest V7 attempt for a task
  brief               print V7 human briefs
  packet              generate V7 agent/reviewer packets
  closeout            emit or inspect terminal human-wait checkpoints
  dashboard           build/open V7 generated dashboards
  reconcile           recompute V7 readiness and next-action projections
  state               sync/import/export V7 runtime state branch files
  hook                install optional local Git hooks
  vault               symlink repo trackers into a shared Obsidian vault
  daemon              operator loop for registered local projects
  config              inspect resolved Tusker configuration with provenance
  automation          plan, inspect, and manually dispatch daemon automation work
  projects            register repositories for daemon pickup
  runs                inspect, tail, interrupt, release, and retire daemon runs
  streams             show the generated live/landed orchestration lane board
  gate-run            run gate-tier proof in harvest mode behind preflight refusal
  gate-ledger         check or record tree-keyed gate results
  serve               serve the read-only localhost control room
  refresh             run one daemon poll tick
  install             install binary and skill bundles
  purge               dry-run or remove generated Tusker repo state
  close               close a V7 task after gates and evidence pass
  accept              accept, confirm proof, and close a green task in one step
  validate            check vault invariants
  reindex             rebuild generated indexes
  update              refresh the installed binary link and skill bundle
  skill               doctor, route, and pack V7 project skills
  setup               diagnose and repair local onboarding drift

Help:
  tusker new --help
  tusker discard --help
  tusker vault --help
  tusker daemon --help
  tusker config --help
  tusker automation --help
  tusker automation plan <task> --json
  tusker runs --help
  tusker serve --help
  tusker search --help
  tusker show --help
  tusker print --help
  tusker open --help
  tusker compact --help
  tusker context --help
  tusker status --help
  tusker install --help
  tusker purge --help
  tusker gate --help
  tusker feedback --help
  tusker improve --help
  tusker xcode --help
  tusker packet --help
  tusker skill --help

Global flags:
  --json           Emit machine-readable output on success and error.`)
}

func printCommandHelp(command string) bool {
	switch command {
	case "init":
		printInitHelp()
	case "new", "new epic", "new task", "new bug", "new doc", "new gate", "new decision":
		printNewHelp()
	case "status":
		printStatusHelp()
	case "discard":
		printDiscardHelp()
	case "next":
		printNextHelp()
	case "work", "work start", "work status", "work heartbeat", "work submit", "work fail", "work release":
		printWorkSessionHelp()
	case "claim":
		printClaimHelp()
	case "evidence":
		printEvidenceHelp()
	case "migrate vault-root":
		printMigrateVaultRootHelp()
	case "handoff", "finish", "gate", "wave", "wave create", "wave add", "wave remove", "wave show", "wave brief", "wave preflight", "wave arm", "wave pause", "wave resume", "wave disarm", "delivery", "delivery plan", "delivery import", "delivery rollout", "trace", "trace list", "trace show", "trace replay", "land", "proof", "attempt", "proposal", "propose", "redact", "brief", "packet", "closeout", "closeout status", "dashboard", "reconcile", "state", "hook", "hook install", "attachments", "migrate", "migrate v7", "migrate gates", "migrate evidence-policy", "migrate close-policy":
		printV7Help()
	case "feedback", "feedback add", "feedback digest", "feedback ingest", "feedback signals", "feedback review", "feedback promote":
		printFeedbackHelp()
	case "improve", "improve scan":
		printImproveHelp()
	case "xcode", "xcode doctor":
		printXcodeHelp()
	case "verify":
		printVerifyHelp()
	case "close":
		printCloseHelp()
	case "list":
		printListHelp()
	case "search":
		printSearchHelp()
	case "show":
		printShowHelp()
	case "print":
		printPrintHelp()
	case "open":
		printOpenHelp()
	case "compact":
		printCompactHelp()
	case "context", "context audit":
		printContextHelp()
	case "validate":
		printValidateHelp()
	case "purge":
		printPurgeHelp()
	case "reindex":
		printReindexHelp()
	case "docs", "docs init", "docs model", "docs map", "docs catalog", "docs freshness", "docs check", "docs apply", "docs noop", "docs waive", "docs export", "docs dev", "docs build":
		printLegacyRedirectHelp("docs", "legacy docs")
	case "domain", "domain list", "domain show", "domain new", "domain canon", "domain graph":
		printDomainHelp()
	case "knowledge", "knowledge map", "knowledge list", "knowledge show", "knowledge route", "knowledge freshness", "knowledge check", "knowledge apply", "knowledge noop", "knowledge waive", "knowledge new":
		printKnowledgeHelp()
	case "skill", "skill doctor", "skill route", "skill pack", "skill sync", "skill bundle", "skill audit-agent-guidance":
		printSkillHelp()
	case "setup", "setup doctor", "setup repair":
		printSetupHelp()
	case "publish", "publish export", "publish build", "publish dev", "publish llms", "publish skill":
		printPublishHelp()
	case "graph":
		printGraphHelp()
	case "vault", "vault set", "vault status", "vault mount", "vault unmount", "vault repair", "vault move":
		printVaultHelp()
	case "daemon", "daemon run", "daemon status", "daemon limits", "daemon resume", "daemon stop", "daemon service":
		printDaemonHelp()
	case "config", "config resolve":
		printConfigHelp()
	case "automation", "automation status", "automation queue", "automation explain", "automation plan", "automation dispatch", "automation collect-external", "automation external-loop", "automation advance-external":
		printAutomationHelp()
	case "projects", "projects add", "projects list", "projects limits", "projects enable", "projects disable", "projects remove", "projects prune":
		printProjectsHelp()
	case "runs", "runs claim", "runs start", "runs heartbeat", "runs submit", "runs fail", "runs reclaim", "runs inspect", "runs logs", "runs events", "runs interrupt", "runs release", "runs retire", "runs redrive", "redrive":
		printRunsHelp()
	case "serve":
		printServeHelp()
	case "refresh":
		printRefreshHelp()
	case "install":
		printInstallHelp()
	case "update":
		printUpdateHelp()
	case "sync-repo-contract":
		printSyncRepoContractHelp()
	case "legacy", "legacy init", "legacy new", "legacy docs", "legacy domain", "legacy knowledge", "legacy publish", "legacy migrate":
		printLegacyHelp()
	default:
		return false
	}
	return true
}

func printLegacyHelp() {
	fmt.Println(`Legacy V5/V6 documentation commands were removed from this build.

Use the V7 surfaces instead:
  tusker init --yes
  tusker new epic --acronym APP --title "App foundation"
  tusker new task --epic APP --title "Implement auth"
  tusker domain list
  tusker skill doctor --strict
  tusker automation plan <TASK-ID> --json

Migration work should happen out-of-band, then import only current V7 work, gates, evidence, and knowledge canon into .tusker/.`)
}

func printLegacyRedirectHelp(command, _ string) {
	fmt.Printf("`tusker %s` was removed from the V7-only build. Use V7 work/domain/skill commands instead.\n", command)
}

func printDaemonHelp() {
	fmt.Println(`Usage:
  tusker daemon run [--once]
  tusker daemon status [--json]
  tusker daemon stop [--json]
  tusker daemon limits [--max-active-runs <n>] [--json]
  tusker daemon resume [--json]
  tusker daemon stop [--drain] [--json]
  tusker daemon service install|start [--allow-protected-projects] [--json]
  tusker daemon service stop|status|uninstall [--json]

Purpose:
  Operator/internal runtime loop for registered local projects. The normal
  workflow remains task-centric: edit a task, move it to ready or rework, then
  verify and close after the daemon hands it to review.

Behavior:
  - daemon run polls registered projects and dispatches ready/rework tasks
  - --once performs one poll tick and exits
  - daemon stop asks the incumbent daemon to exit and waits briefly
  - daemon status reports state-root, project count, and active run count
  - daemon install writes and starts a per-user launchd LaunchAgent
  - daemon uninstall stops and removes the per-user launchd LaunchAgent
  - daemon limits reads or updates the global active-run cap
  - daemon resume closes invariant/crash-loop circuits after operator repair
  - daemon stop asks the resident daemon to shut down and leaves detached wrappers alive
  - daemon stop --drain waits bounded for detached wrappers to finish
  - daemon service manages the macOS per-user launchd agent for daemon run
  - service install/start blocks before launch when enabled projects are under
    macOS-protected folders; --allow-protected-projects is the explicit override
    after Full Disk Access has been granted
  - shared daemon runtime lives under Application Support; each project keeps
    its WORKFLOW.md, tasks, knowledge, and source inside the repository

Examples:
  tusker daemon status
  tusker daemon install
  tusker daemon run --once
  tusker daemon limits --max-active-runs 1
  tusker daemon resume
  tusker daemon stop --drain
  tusker daemon service install
  tusker daemon service install --allow-protected-projects
  tusker daemon service status`)
}

func printAutomationHelp() {
	fmt.Println(`Usage:
  tusker automation status [--json]
  tusker automation queue [--project <id>|--repo <path>|--vault <path>] [--json]
  tusker automation explain <task> [--project <id>|--repo <path>|--vault <path>] [--json]
  tusker automation plan <task> [--project <id>|--repo <path>|--vault <path>] [--json]
  tusker automation dispatch <task> [--project <id>|--repo <path>|--vault <path>] [--json]
  tusker automation collect-external <task> --runner chatgpt-browser --job <job-id> [--covers A1,A2] [--json]
  tusker automation external-loop <task> [--json]
  tusker automation advance-external <task> --job <job-id> [--dispatch] [--json]
  tusker automation advance-external <task> --event apply_failed|apply_succeeded|review_succeeded [--reason <text>] [--json]

Purpose:
  Operator surface for daemon automation. Plan, explain, and queue are read-only.
  Dispatch bypasses polling only after the same eligibility checks pass.

Behavior:
  - status summarizes registered projects and runtime run counts
  - queue splits dispatchable and blocked task candidates
  - explain shows concrete blockers, selected runner, workspace, and approvals
  - plan is the canonical dispatch decision used by agents and operators
  - dispatch routes through the daemon dispatch helper and existing gates
  - collect-external fetches ChatGPT/browser artifacts, records review packets, and stores patches as apply inputs
  - external-loop shows durable external cycle counters and policy events
  - advance-external records the next loop decision, enforces caps, and can dispatch the Codex apply path

Examples:
  tusker automation queue --json
  tusker automation explain APP-T-0001
  tusker automation plan APP-T-0001 --json
  tusker automation dispatch APP-T-0001 --json
  tusker automation collect-external APP-T-0001 --runner chatgpt-browser --job cgpt_xxx --json
  tusker automation advance-external APP-T-0001 --job cgpt_xxx --dispatch --json
  tusker automation advance-external APP-T-0001 --event apply_failed --reason "git apply conflict" --json`)
}

func printV7Help() {
	fmt.Println(`Usage:
  tusker new epic --acronym HSP --title "First-class harness provider setup"
  tusker new task --epic HSP --title "Add provider smoke harness"
  tusker new gate --blocks HSP-T-0001 --kind auth --owner human:sarav \
    --action "Provision staging OAuth credentials." \
    --verification "Provider endpoint returned ready." \
    --why-agent-cannot "Human account access is required."
  tusker new decision --epic HSP --title "Use repo-local branch-safe tracker"

  tusker delivery plan --spec docs/specs/example.md --out .tusker/scratch/delivery-plan.yaml
  tusker delivery import --plan .tusker/scratch/delivery-plan.yaml --wave "Example delivery" --dry-run
  tusker delivery import --plan .tusker/scratch/delivery-plan.yaml --wave "Example delivery"
  tusker delivery rollout doctor --json
  tusker delivery rollout repair --dry-run --json

  tusker gate list --open [--owner human:sarav]
  tusker gate satisfy HSP-G-0001 --evidence "Provider endpoint returned ready."
  tusker gate waive HSP-G-0002 --reason "Live smoke deferred."
  tusker gate obsolete HSP-G-0003 --reason "Task superseded."

  tusker claim HSP-T-0001 --owner agent:codex
  tusker heartbeat HSP-T-0001
  tusker release HSP-T-0001
  tusker attempt start HSP-T-0001
  tusker land HSP-T-0001
  tusker land W-0001
  tusker wave preflight W-0001 --json
  tusker wave brief W-0001 [--json]
  tusker wave arm W-0001 --by human:sarav
  tusker wave pause W-0001 --reason "operator-requested pause"
  tusker wave resume W-0001 --by human:sarav
  tusker wave disarm W-0001 --reason "scope withdrawn"
  tusker verify add HSP-T-0001 --covers A1,A2 --check "go test ./cmd/tusker -count=1" --result pass
  tusker proof status HSP-T-0001
  tusker proof set-mode HSP-T-0001 inline
  tusker finish HSP-T-0001 --summary "Implemented; proof is satisfied." --request-review
  tusker attempt handoff HSP-T-0001 --summary "Implemented; proof is satisfied." [--no-review-proposal]
  tusker handoff HSP-T-0001 --summary ./summary.md
  tusker evidence add HSP-T-0001 --kind automated_test --covers A1,A2 --summary "Focused tests passed."
  tusker evidence promote HSP-T-0001 --from .tusker/scratch/HSP-T-0001/smoke.mov --kind video --covers A1-A3
  tusker evidence prune HSP-T-0001 --dry-run
  tusker attachments migrate --dry-run
  tusker propose close HSP-T-0001 --reason "Implementation branch is ready."
  tusker propose status HSP-T-0001 --status review
  tusker proposal list [--target HSP-T-0001] [--status proposed]
  tusker proposal accept HSP-P-0001 --by human:sarav
  tusker proposal apply HSP-P-0001 --by human:sarav
  tusker proposal reject HSP-P-0002 --reason "Superseded."
  tusker redact HSP-T-0001 --reason "Removed leaked token from evidence." --replacement "Redacted summary retained."
  tusker escalate -s P1 --task HSP-T-0001 --reason system_error "Runner is stuck in a no-progress loop."
  tusker escalate ack ESC-0001 --by human:sarav
  tusker digest [--since 2026-07-07T00:00:00Z] [--json]
  tusker logbook [--date 2026-07-20] [--write] [--json]   # --date is host-local; default is today

  tusker brief HSP-T-0001
  tusker packet HSP-T-0001 --for agent [--write]
  tusker packet HSP-T-0001 --for reviewer [--write]
  tusker packet HSP-T-0001 --for explainer [--write]
  tusker closeout HSP-T-0001 --emit-packet --validate "go test ./..." --json
  tusker closeout status HSP-T-0001 --json
  tusker dashboard build
  tusker reconcile
  tusker state sync [--branch tusker/state] [--push] [--remote origin]
  tusker state import [--branch tusker/state] [--fetch] [--remote origin]
  tusker state export [--dir .tusker-runtime/state]
  tusker hook install pre-commit [--force]
  tusker hook install pre-push [--force]
  tusker validate --branch-policy [--staged]
  tusker migrate v7 --dry-run [--json]
  tusker migrate gates --from-blocked-reason [--write] [--json]
  tusker migrate evidence-policy [--write] [--json]
  tusker migrate close-policy [--write] [--json]
  tusker migrate vault-root --to .tusker [--dry-run] [--json]

Purpose:
  V7 repo-local, markdown-backed work records with first-class proof, gates, evidence,
  attempts, event-per-file history, generated briefs/packets/dashboards, and
  branch guards for protected task/gate state. Delivery planning and import are inert:
  models propose source-keyed plans, while Tusker validates and atomically owns final
  task IDs, revisions, relations, and wave records; neither command dispatches work.
  Wave preflight is read-only. Arm records exact batch authorization and atomically
  promotes held members; pause/resume/disarm only control future claims.`)
}

func printMigrateVaultRootHelp() {
	fmt.Println(`Usage:
  tusker migrate vault-root --to .tusker [--vault <path>] [--dry-run] [--json]

Purpose:
  Explicitly move a repo-local Tusker vault, update tusker.yaml storage roots,
  and refresh managed AGENTS.md / CLAUDE.md pointer blocks.

Behavior:
  - never runs as part of normal commands or init
  - warns when the Git worktree is dirty
  - errors if the destination already exists

Examples:
  tusker migrate vault-root --to .tusker --dry-run
  tusker migrate vault-root --to .tusker`)
}

func printProjectsHelp() {
	fmt.Println(`Usage:
  tusker projects add [--repo <path>] [--vault <path>] [--json]
  tusker projects list [--json]
  tusker projects limits [--id <project-id>|--repo <path>|--vault <path>] [--max-active-runs <n>] [--json]
  tusker projects enable [--id <project-id>|--repo <path>|--vault <path>] [--json]
  tusker projects disable [--id <project-id>|--repo <path>|--vault <path>] [--json]
  tusker projects remove <project-id> [--json]
  tusker projects prune [--apply] [--dry-run] [--json]

Purpose:
  Register repo-local Tusker vaults for daemon pickup. Obsidian remains the
  editing surface; project registration only tells the local runtime what to
  poll.

Behavior:
  - project WORKFLOW.md, tasks, knowledge, and source remain repo-local
  - shared daemon state, logs, limits, and runtime metadata live outside repos
  - prune previews registrations whose tracker roots no longer exist and their
    matching dangling Obsidian-vault symlinks; --apply performs the removal
  - on macOS, projects under Desktop, Documents, Downloads, or iCloud Drive
    receive a launchd access warning during add/enable

Examples:
  tusker projects add --repo . --vault ./.tusker
  tusker projects list
  tusker projects disable --repo .
  tusker projects prune
  tusker projects prune --apply`)
}

func printConfigHelp() {
	fmt.Println(`Usage:
  tusker config resolve <key> [--vault <path>] [--json]

Purpose:
  Show the effective value for a Tusker config key, the winning source, and
  each lower-precedence source value. Supported resolver keys include runner
  profiles, routing, denylist, and runtime concurrency limits.

Examples:
  tusker config resolve runtime.max_active_runs_per_project
  tusker config resolve automation.profiles --json`)
}

func printRunsHelp() {
	fmt.Println(`Usage:
	  tusker runs claim <task-id> --owner <actor> [--project <id>] [--json]
	  tusker runs start <task-id> --owner <actor> [--session <id>] [--pid <n>] [--pgid <n>] [--json]
	  tusker runs heartbeat <task-id> --owner <actor> [--json]
	  tusker runs submit <task-id> --owner <actor> --deliverable <summary> --verification <summary> --gate-verdicts <A1=pass,A2=pass> [--branch <name>] [--head-sha <sha>] [--json]
	  tusker runs fail <task-id> --owner <actor> --reason <text> [--json]
	  tusker runs reclaim <task-id> --owner <actor> --reason <text> [--json]
  tusker runs inspect <task-id-or-record-id> [--json]
  tusker runs logs <task-id-or-record-id> [--lines <n>] [--follow] [--json]
  tusker runs events <task-id-or-record-id> [--lines <n>] [--follow] [--json]
  tusker runs interrupt <task-id-or-record-id> [--json]
  tusker runs release <task-id-or-record-id> [--json]
  tusker runs retire <task-id-or-record-id> --reason <text> [--by <actor>] [--force] [--json]
  tusker redrive <task-id-or-record-id> --reason <text> [--by <actor>] [--json]

Purpose:
  Inspect and control daemon runtime state for a task. These commands expose
  attempts, turns, sessions, event tails, logs, and interrupts without making
  runtime state part of task frontmatter. Redrive resets the budget window for
  parked budget runs and queues them for the resident daemon. Retire is the
  terminal operator path for stale, broken runtime records that must stop
  tripping daemon invariant circuits.

Examples:
  tusker runs inspect ORC-T-0018 --json
  tusker runs events ORC-T-0018 --lines 20
  tusker runs interrupt ORC-T-0018
  tusker runs release ORC-T-0018 --json
  tusker runs retire ORC-T-0018 --reason "legacy over retry cap" --json
  tusker redrive ORC-T-0018 --reason "operator reviewed spend" --json`)
}

func printWorkSessionHelp() {
	fmt.Println(`Usage:
  tusker work start <task-id> --by <agent> [--source codex|claude|tusker_cli] [--json]
  tusker work status <task-id> [--json]
  tusker work heartbeat <task-id> --by <agent> [--json]
  tusker work submit <task-id> --by <agent> --deliverable <summary> --verification <summary> --gate-verdicts <A1=pass> [--json]
  tusker work fail <task-id> --by <agent> --reason <text> [--json]
  tusker work release <task-id> --by <agent> --reason <text> [--json]

Purpose:
  The canonical runtime ownership protocol for interactive tracked work.
  It never enables automation, arms a wave, starts a daemon, or launches a
  worker. Legacy claim remains a compatibility alias for work start.`)
}

func printRefreshHelp() {
	fmt.Println(`Usage:
  tusker refresh [--quiet] [--json]

Purpose:
  Run one daemon poll tick for registered projects. This is the easiest local
  smoke path for checking whether ready/rework tasks are picked up.`)
}

func printDomainHelp() {
	fmt.Println(`Usage:
  tusker domain list [--json]
  tusker domain show <domain-id> [--capsule|--full|--json]
  tusker domain new <domain-id> --title "..." [--summary "..."]
  tusker domain canon <domain-id> [--full]

Purpose:
  Operate V7 domain canon under .tusker/knowledge/domains/**.

Examples:
  tusker domain list
  tusker domain show runtime --capsule
  tusker domain new providers --title "Providers" --summary "External API integrations."`)
}

func printKnowledgeHelp() {
	fmt.Println(`Usage:
  tusker knowledge new <domain>/<folder>/<slug> --kind runbook|decision|invariant|interface|glossary|source --title "..."

Purpose:
  Create V7 leaf nodes under .tusker/knowledge/domains/**. Routing and reading
  should happen through ` + "`" + `tusker skill route` + "`" + ` and the project SKILL.md.

Examples:
  tusker knowledge new runtime/runbooks/dispatch-loop --kind runbook --title "Dispatch loop"
  tusker knowledge new providers/decisions/oauth-refresh --kind decision --title "OAuth refresh policy"`)
}

func printSkillHelp() {
	fmt.Println(`Usage:
  tusker skill doctor [--strict] [--json]
  tusker skill doctor --package <path> [--strict] [--json]
  tusker skill audit-agent-guidance [--repo <path>] [--write|--draft] [--target feedback|knowledge] [--json]
  tusker skill route "<intent>" [--json]
  tusker skill pack <TASK-ID> --budget <n> --for agent
  tusker skill sync [--repo <path>] [--mode symlink|copy] [--source <checkout>] [--json]
  tusker skill bundle [--repo <path>] [--out <path>] [--dereference-symlinks] [--json]

Purpose:
  Validate and route V7 project skills. skill doctor checks the repo skill
  package, domain routes, forbidden source-truth paths, local absolute paths,
  and task-domain coverage. skill route returns the
  smallest ordered read set for an intent. skill pack is an explicit wrapper
  over tusker packet for bounded task context. skill sync refreshes repo-local
  generated skill installs, defaulting to symlink mode. Pass --source when
  running from outside the Tusker checkout. skill bundle creates a portable
  materialized copy for cloud/handoff contexts. audit-agent-guidance finds
  non-managed AGENTS.md/CLAUDE.md guidance, detects stale/missing Tusker
  bootstrap blocks, and --write refreshes managed bootstrap guidance.

Examples:
  tusker skill doctor --strict --json
  tusker skill audit-agent-guidance --write
  tusker skill route "change provider auth refresh logic" --json
  tusker skill pack VSD-T-0010 --budget 6000 --for agent
  tusker skill sync --repo .
  tusker skill sync --repo . --source ~/src/tusker
  tusker skill bundle --repo . --out .tusker/_generated/skill-bundle`)
}

func printPublishHelp() {
	fmt.Println(`Usage:
  tusker publish skill --v7 [--out ./dist/project-skill]

Purpose:
  Export the V7 project skill package from .tusker/SKILL.md and
  .tusker/knowledge/domains/**. Site export/build/dev/llms were removed.`)
}

func printGraphHelp() {
	fmt.Println(`Graph inspection belonged to the removed V6 knowledge graph. Use ` + "`" + `tusker skill route` + "`" + ` or domain show for V7 routing.`)
}

func printVaultHelp() {
	fmt.Println(`Usage:
  tusker vault set --path <obsidian-vault>
  tusker vault status [--json]
  tusker vault mount [--repo <path>] [--vault <path>] [--name <folder>] [--force] [--json]
  tusker vault unmount [--name <folder>|--repo <path>|--vault <path>] [--json]
  tusker vault repair [--force] [--json]
  tusker vault move --to <obsidian-vault> [--force] [--json]

Purpose:
  Link repo-local Tusker trackers into one shared Obsidian vault so multiple
  projects can be monitored from one place.

Behavior:
  - vault set stores the shared Obsidian vault path in Tusker workspace config
  - vault mount creates a symlink from <obsidian-vault>/<name> to the repo tracker
  - vault status shows every configured mount and whether its symlink is healthy
  - vault repair recreates missing mount symlinks from saved metadata
  - vault move updates the shared vault path and repairs saved mounts

Examples:
  tusker vault set --path ~/Obsidian/Tusker
  tusker vault mount --repo . --vault ./.tusker --name my-app
  tusker vault status`)
}

func printInitHelp() {
	fmt.Println(`Usage:
  tusker init [--vault <path>] [--yes] [--fresh] [--purge-state]
  tusker init --profile v7 [--yes] [--fresh] [--purge-state]

What it does:
  1. initializes a V7 repo-local vault under .tusker by default
  2. writes WORKFLOW.md with tracker_schema_version: 7
  3. writes .tusker/SKILL.md and knowledge/domains/** starter canon
  4. injects Tusker pointers into AGENTS.md / CLAUDE.md unless disabled
  5. reindexes the vault and refreshes V7 dashboards/Bases

Removed:
  V5 bootstrap, V6 knowledge-graph bootstrap, migration scaffolds, and site publishing
  were removed from the default codebase. Keep conversions outside current
  repo state and import only current V7 records.

Flags:
  --vault <path>    target vault path (default: ./.tusker)
  --yes             accept defaults without prompts
  --fresh           move an existing target vault aside and recreate it cleanly
  --purge-state     delete generated Tusker state first; implies the same safe
                    scope as 'tusker purge --only-tusker-state --yes'
  --profile v7      explicit V7 profile; any other profile is rejected
  --v7              explicit alias for default V7 init
  --vault-only      update only the vault; skip AGENTS/CLAUDE and repo-contract files
  --no-pointers     skip AGENTS.md / CLAUDE.md pointer injection
  --no-contract     skip repo-contract helper docs

Examples:
  tusker init --yes
  tusker init --profile v7 --yes --fresh
  tusker init --yes --fresh --purge-state`)
}

func printDocsHelp() {
	fmt.Println(`The V5/V6 documentation publishing system was removed.

Use V7 project knowledge instead:
  tusker skill route "<intent>" --json
  tusker domain list
  tusker domain show <domain-id>
  tusker knowledge new <domain>/<path>/<slug> --kind runbook --title "..."`)
}

func printNewHelp() {
	fmt.Println(`Usage:
  tusker new epic [--vault <path>] --acronym <ACR> --title <title> [--summary <text>] [--owner <name>] [--spec-refs <csv>]
  tusker new task [--vault <path>] --epic <ACR> --title <title> [--status ready|backlog|review|rework] [--priority p0|p1|p2|p3] [--size s|m|l|xl] [--risk low|medium|high|critical] [--spec-refs <csv>] [--evidence-required automated_test]
  tusker new gate --blocks <TASK-ID> --kind <gate-kind> --owner <owner> --action <text> --verification <proof>
  tusker new decision --epic <ACR> --title <title>

Purpose:
  Create V7 work objects only. V5 task/doc creation was removed.

Examples:
  tusker new epic --vault ./.tusker --acronym APP --title "App foundation"
  tusker new task --vault ./.tusker --epic APP --title "Implement auth" --risk medium --size m
  tusker new gate --vault ./.tusker --blocks APP-T-0001 --kind auth --owner human:sarav \
    --action "Provision staging OAuth credentials." \
    --verification "Provider ready check passes." \
    --why-agent-cannot "Human account access is required."`)
}

func printStatusHelp() {
	fmt.Println(`Usage:
  tusker status <id> <status> [--vault <path>] [--actor <name>] [--reason <text>]
  tusker status --id <id> --status <status> [--vault <path>] [--actor <name>] [--reason <text>]

Statuses:
  idea, backlog, ready, review, rework, superseded
  Use tusker close for done and tusker discard for cancelled.

Purpose:
  Move a V7 task through its durable workflow. Runtime activity is represented
  by leases/runs, not by a durable active task status.

Notes:
	Use backlog for shaped future work that should not be picked up in the current release.
	Use tusker discard instead of setting cancelled directly so dependencies,
	gates, runtime rows, and discard metadata are handled together.`)
}

func printDiscardHelp() {
	fmt.Println(`Usage:
  tusker discard <task-id> --reason <text> [--dependents detach|discard] [--by <actor>] [--json]
  tusker discard <task-id> --dry-run [--json]

Purpose:
  Remove abandoned work from active views without deleting its durable task,
  attempt, evidence, or event history.

Dependency policy:
  Discard refuses when active downstream tasks depend on the target unless
  --dependents explicitly chooses detach (remove the target edge) or discard
  (cancel the complete downstream dependency closure). Use --dry-run first to
  inspect the exact impact. No dependency edge is removed silently.`)
}

func printNextHelp() {
	fmt.Println(`Usage:
  tusker next [--vault <path>] [--epic <ACR>] [--owner <name>] [--domain <name>] [--lane <name>] [--explain] [--json]
  tusker next --claim --as <agent-or-person> [--vault <path>] [--epic <ACR>] [--domain <name>] [--lane <name>] [--json]

Purpose:
  Return the next pickable V7 task. Pickable means status ready or rework,
  readiness ready, next_owner agent or agent:*, and exact --owner match when provided.
  --domain matches the task domains list. --lane matches lane or lanes frontmatter.

Ranking:
  priority first (p0 before p1), then risk (critical before high), then task id.
  --explain prints the selected task plus skipped candidates and reasons.
  --claim uses the same rules, then writes a V7 local lease.`)
}

func printClaimHelp() {
	fmt.Println(`Usage:
  tusker claim <id> --as <agent-or-person> [--vault <path>] [--reason <text>] [--json]

Purpose:
  Assign one ready/rework task by creating a runtime lease. Claim rejects idea,
  backlog, review, done, cancelled, superseded, and tasks with unresolved
  blocked_by dependencies.`)
}

func printEvidenceHelp() {
	fmt.Println(`Usage:
  tusker evidence <id> <kind> <path-or-url> [--vault <path>] [--note <text>]
  tusker evidence --id <id> --kind <kind> --path <path-or-url> [--vault <path>] [--note <text>]
  tusker evidence add <task-id> --kind automated_test --covers A1,A2 --summary <text>
  tusker evidence promote <task-id> --from .tusker/scratch/<task-id>/<artifact> --kind screenshot --covers A1
  tusker evidence prune <task-id> --dry-run

Purpose:
  Add, promote, or prune V7 evidence records. For inline proof, prefer tusker verify add.`)
}

func printVerifyHelp() {
	fmt.Println(`Usage:
  tusker verify add <task-id> --covers A1,A2 --check <command-or-manual-check> --result pass|fail|blocked|skipped|waived [--note <text>]
  tusker verify add <task-id> --rows "A1|go test ./pkg|pass|note"
  tusker verify add <task-id> --batch-file <path>
  tusker verify recipe <task-id> [--files <path[,path...]>]
  tusker verify <id> [--vault <path>] [--by <name>] [--summary <text>]
  tusker verify --id <id> [--vault <path>] [--by <name>] [--summary <text>]

Purpose:
  Add inline V7 verification rows or suggest scoped verification recipes.

Options:
  --blocked-by <path|task|owner>  Required with --result blocked; attributes external/shared blockers.
  --rows <rows>                   Newline rows shaped covers|check|result|note|blocked_by.
  --batch-file <path>             Read rows from a file using the same format.`)
}

func printCloseHelp() {
	fmt.Println(`Usage:
  tusker close <id> [--vault <path>] [--by <name>] [--reason <text>]
  tusker close --id <id> [--vault <path>] [--by <name>] [--reason <text>]

Purpose:
  Close a V7 task after required evidence is present, close policy passes, and
  no blocking gates remain open.`)
}

func printListHelp() {
	fmt.Println(`Usage:
  tusker list [<EPIC>] [--vault <path>] [--json] [--type epic|task|wave|doc] [--status <status>] [--epic <ACR>] [--wave <W-0001>] [--open|--closed] [--ready|--running|--review|--mine] [--format table|ids] [--limit <n>] [--width <cols>]

Purpose:
  Query epics, tasks, and docs from the vault without dumping note bodies.
  Bare tusker list prints a compact epic table. Task-scoped filters such as
  --open, --ready, --running, --review, and --mine print compact task tables.
  A positional epic, like tusker list VSD, drills into that epic's open tasks.
  Table output sizes itself to the terminal width and drops low-value columns
  in cramped terminals. Use --width when a terminal reports bad dimensions.
  Use --runnable for V7 agent-runnable tasks only: task kind, status ready/rework,
  readiness ready, and next_owner agent or agent:*. --ready is the
  human-friendly alias. --running shows active local leases. --all-projects
  queries registered projects from tusker projects add.

Examples:
  tusker list --vault ./.tusker
  tusker list VSD
  tusker list --vault ./.tusker --type epic
  tusker list --vault ./.tusker --ready
  tusker list --vault ./.tusker --running
  tusker list --vault ./.tusker --epic ORC --type task --open
  tusker list --vault ./.tusker --wave W-0001 --type task
  tusker list --all-projects --open --format ids
  tusker list --vault ./.tusker --epic ORC --type task --open --limit 10
  tusker list --vault ./.tusker --type task --status ready
  tusker list --vault ./.tusker --type doc --json`)
}

func printSearchHelp() {
	fmt.Println(`Usage:
  tusker search <text> [--vault <path>] [--type epic|task|doc] [--epic <ACR>] [--status <status>] [--limit <n>] [--json]
  tusker search --query <text> [--json]

Purpose:
  Search first-party tracker notes without reading generated indexes,
  Attachments, runtime state, build logs, or arbitrary repository files.

Examples:
  tusker search braindump_id --type task
  tusker search "docs close gate" --epic DOC --limit 10
  tusker search reviewer --status review --json`)
}

func printShowHelp() {
	fmt.Println(`Usage:
  tusker show <ID> [--vault <path>] [--capsule|--acceptance|--evidence|--verification|--full] [--lines <n>]
  tusker show <ID> --section "<heading>"

Purpose:
  Read the smallest useful slice of a Tusker note. Defaults to --capsule.
  --verification shows summaries plus a small log tail; use --section for the full log.

Examples:
  tusker show ORC-T-0019
  tusker show ORC-T-0019 --acceptance
  tusker show ORC-T-0019 --evidence
  tusker show ORC-T-0019 --full`)
}

func printPrintHelp() {
	fmt.Println(`Usage:
  tusker print <ID> [--vault <path>] [--project <id|key|name>] [--capsule|--acceptance|--evidence|--verification|--full] [--section <heading>]
  tusker print <ID> [--plain] [--style dark|light|notty|<path>] [--width <n>] [--json]

Purpose:
  Render a Tusker Markdown note for human terminal reading. Defaults to the note
  body with Glamour Markdown styling. Use --plain for raw Markdown output.

Examples:
  tusker print ING-T-0016
  tusker print ING-T-0016 --capsule
  tusker print ING-T-0016 --acceptance --plain
  tusker print ING-T-0016 --project mobile --style light`)
}

func printOpenHelp() {
	fmt.Println(`Usage:
  tusker open <ID> [--vault <path>] [--project <id|key|name>] [--path|--json]
  tusker open <ID> [--editor|--obsidian|--app <name>]

Purpose:
  Resolve a Tusker record and open the underlying Markdown note. Current-vault
  lookup wins; outside a repo, registered projects from tusker projects add are searched.

Examples:
  tusker open ING-T-0016
  tusker open ING-T-0016 --path
  tusker open ING-T-0016 --editor
  tusker open ING-T-0016 --obsidian
  tusker open ING-T-0016 --project mobile --json`)
}

func printCompactHelp() {
	fmt.Println(`Usage:
  tusker compact <ID> [--vault <path>] [--write] [--json]
  tusker compact --all [--vault <path>] [--archive-logs] [--write] [--json] [--verbose]

Purpose:
  Dry-run or apply safe note compaction: remove empty optional frontmatter and
  disposable placeholder sections such as empty Execution plan and Work log.
  Substantive sections are preserved unless --archive-logs is set, which moves
  bulky historical log material into bounded attempt/evidence records before
  removing it from task bodies. With --all, unchanged notes are hidden unless
  --verbose is set.

Examples:
  tusker compact ORC-T-0019
  tusker compact ORC-T-0019 --write
  tusker compact --all --archive-logs --write
  tusker compact --all --json`)
}

func printContextHelp() {
	fmt.Println(`Usage:
  tusker context audit --file <codex-session.jsonl> [--top <n>] [--json]

Purpose:
  Summarize a Codex JSONL transcript without dumping raw session content.
  Reports output categories, largest tool outputs, token totals, and concrete
  context-reduction recommendations.

Examples:
  tusker context audit --file ~/.codex/sessions/2026/05/09/session.jsonl
  tusker context audit --file ./thread.jsonl --top 20 --json`)
}

func printValidateHelp() {
	fmt.Println(`Usage:
  tusker validate [--vault <path>] [--json]
  tusker validate --branch-policy [--base origin/main] [--json]
  tusker validate --branch-policy-only [--base origin/main] [--json]
  tusker validate --staged --branch-policy [--json]

Purpose:
  Check the vault against Tusker schema and workflow invariants.

Options:
  --branch-policy       Include protected V7 state-field diff checks.
  --branch-policy-only  Run only protected V7 state-field diff checks.
  --staged              Check staged changes instead of branch diff.`)
}

func printReindexHelp() {
	fmt.Println(`Usage:
  tusker reindex [--vault <path>] [--json] [--fix-links]

Purpose:
  Rebuild generated indexes, dashboard JSON, and epic roster output.

Options:
  --fix-links  Refresh record-id mirror fields from human-authored wikilinks before rebuilding indexes.`)
}
