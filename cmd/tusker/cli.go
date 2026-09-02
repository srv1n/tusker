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
)

type Args map[string]string

// copyArgsForInternalMutation keeps composite CLI operations from modifying
// their caller's parsed argument map while they add private implementation
// flags for a downstream command.
func copyArgsForInternalMutation(in Args) Args {
	out := make(Args, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

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
	case "acp", "actor", "docs", "domain", "knowledge", "publish", "skill", "setup", "new", "vault", "daemon", "automation", "projects", "runs", "runner", "gate-ledger", "context", "config", "migrate", "feedback", "improve", "wave", "delivery", "review", "trace", "escalate", "departure", "factory", "work", "execution":
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
		if len(vaults) == 0 && cliCommandMutatesVault(command) && !(command == "reconcile" && args.Bool("dry-run")) {
			notifyDaemonForVault(args)
		}
		if cliCommandMutatesProjectRegistry(command, args) {
			_ = sendDaemonControlOneWay(DefaultStateRoot(), daemonControlRequest{Command: "reconcile_registry"}, 250*time.Millisecond)
		}
	}
	return code, err
}

func cliCommandMutatesVault(command string) bool {
	switch command {
	case "status", "discard", "verify add", "verify remove", "evidence add", "gate new", "gate satisfy", "gate waive", "new task", "new epic", "new decision", "delivery import", "delivery start", "wave refingerprint", "wave re-fingerprint", "actor correction", "reconcile", "finish", "close", "accept", "handoff":
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
	switch command {
	case "version", "--version":
		return 0, versionCmd(args)
	case "capabilities":
		return 0, capabilitiesCmd(args)
	case "acp":
		if err := validateACPAdapterCommandArgs(args); err != nil {
			return 0, tuskerError(errorInvalidArg, err.Error())
		}
		printACPAdapterHelp()
		return 0, nil
	case "acp install":
		return 0, acpInstallCommand(args)
	case "acp doctor":
		return 0, acpDoctorCommand(args)
	case "acp setup":
		return 0, acpSetupCommand(args)
	case "runner-wrapper":
		return 0, runnerWrapperCmd(args)
	case "runner":
		printRunnerHelp()
		return 0, nil
	case "runner catalog":
		return 0, runnerCatalogCmd(args)
	case "runner profiles":
		return 0, runnerProfilesBootstrapCmd(args)
	case "runner route":
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
		return 0, runnerRouteCmd(args)
	case "new epic":
		return 0, newV7Epic(args)
	case "new task":
		return 0, newV7Task(args)
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
	case "execution", "execution register", "execution attach", "execution rename", "execution bind", "execution detach", "execution rebind", "execution inbox", "execution list", "execution show", "execution cancel", "execution launch":
		return 0, executionCmd(args, strings.TrimSpace(strings.TrimPrefix(command, "execution")))
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
	case "wave refingerprint", "wave re-fingerprint":
		return 0, waveV7RefingerprintCmd(args)
	case "factory":
		printFactoryOperationsHelp()
		return 0, nil
	case "factory operations":
		return 0, factoryOperationsCmd(args)
	case "delivery plan":
		return 0, deliveryPlanCmd(args)
	case "delivery context":
		return 0, deliveryPlanningContextCmd(args)
	case "delivery import":
		return 0, deliveryImportCmd(args)
	case "delivery review":
		return 0, deliveryReviewCmd(args)
	case "delivery start":
		return 0, deliveryStartCmd(args)
	case "review submit":
		return 0, reviewSubmitCmd(args)
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
	case "actor":
		fmt.Println("Usage: tusker actor correction plan|apply|list ...")
		return 0, nil
	case "actor correction":
		return 0, actorCorrectionV7Cmd(args)
	case "redact":
		return 0, redactV7Cmd(args)
	case "verify":
		if strings.ToLower(args.String("_pos0")) == "add" {
			return 0, verifyV7AddCmd(args)
		}
		if strings.ToLower(args.String("_pos0")) == "remove" {
			return 0, verifyV7RemoveCmd(args)
		}
		if strings.ToLower(args.String("_pos0")) == "recipe" {
			return 0, verifyV7RecipeCmd(args)
		}
		printVerifyHelp()
		return 0, nil
	case "close":
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
		return 0, closeV7Cmd(args)
	case "accept":
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
		return 0, acceptV7Cmd(args)
	case "redrive":
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
		return 0, redriveCmd(args)
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
	case "migrate evidence-policy":
		return 0, migrateV7EvidencePolicyCmd(args)
	case "migrate close-policy":
		return 0, migrateClosePolicyCmd(args)
	case "migrate vault-root":
		return 0, migrateVaultRootCmd(args)
	case "reindex":
		return 0, reindex(args)
	case "reset", "relaunch":
		return 0, resetCmd(args)
	case "validate":
		return validateCmd(args)
	case "purge":
		return 0, tuskerPurgeCmd(args)
	case "uninstall":
		return 0, tuskerGlobalUninstallCmd(args)
	case "gc":
		return 0, scratchGCCmd(args)
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
	case "context audit":
		return 0, contextAuditCmd(args)
	case "docs find":
		return 0, docsCmd("find", args)
	case "docs new":
		return 0, docsCmd("new", args)
	case "docs map":
		return 0, docsCmd("map", args)
	case "docs status":
		return 0, docsCmd("status", args)
	case "docs verify":
		return 0, docsCmd("verify", args)
	case "docs adopt":
		return 0, docsCmd("adopt", args)
	case "docs":
		printDocsHelp()
		return 0, nil
	case "domain list":
		return 0, domainV7ListCmd(args)
	case "domain show":
		return 0, domainV7ShowCmd(args)
	case "domain new":
		return 0, newV7Domain(args)
	case "domain canon":
		return 0, domainV7CanonCmd(args)
	case "domain":
		printDomainHelp()
		return 0, nil
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
	case "publish skill":
		return 0, publishSkillCmd(args)
	case "publish":
		printPublishHelp()
		return 0, nil
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
	case "projects rebind":
		return 0, projectsRebindCmd(args)
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
	case "help actor", "help actor correction":
		fmt.Println("Usage: tusker actor correction plan|apply|list ...\n\nActor corrections are append-only, human-gated metadata projections; original event bytes never change. Apply is unavailable until exact-verification human-control authority is installed.")
		return 0, nil
	case "help migrate vault-root":
		printMigrateVaultRootHelp()
		return 0, nil
	case "help handoff", "help gate", "help wave", "help wave create", "help wave add", "help wave remove", "help wave show", "help wave brief", "help wave preflight", "help wave arm", "help wave pause", "help wave resume", "help wave disarm", "help wave refingerprint", "help wave re-fingerprint", "help land", "help attempt", "help proposal", "help propose", "help brief", "help packet", "help closeout", "help closeout status", "help dashboard", "help reconcile", "help state", "help migrate":
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
	case "help context", "help context audit":
		printContextHelp()
		return 0, nil
	case "help validate":
		printValidateHelp()
		return 0, nil
	case "help reindex":
		printReindexHelp()
		return 0, nil
	case "help docs", "help docs map", "help docs status", "help docs verify", "help docs adopt":
		printDocsHelp()
		return 0, nil
	case "help domain", "help domain list", "help domain show", "help domain new", "help domain canon":
		printDomainHelp()
		return 0, nil
	case "help knowledge", "help knowledge new":
		printKnowledgeHelp()
		return 0, nil
	case "help skill", "help skill doctor", "help skill route", "help skill pack":
		printSkillHelp()
		return 0, nil
	case "help publish", "help publish skill":
		printPublishHelp()
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
	case "help factory", "help factory operations":
		printFactoryOperationsHelp()
		return 0, nil
	case "help config", "help config resolve":
		printConfigHelp()
		return 0, nil
	case "help projects", "help projects add", "help projects list", "help projects limits", "help projects enable", "help projects disable", "help projects rebind", "help projects remove", "help projects prune":
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
	case "help reset", "help relaunch":
		printResetHelp()
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

func printHelp() {
	fmt.Println(`Tusker - repo-local work tracking

Vault discovery: if [--vault] is omitted, tusker walks up from the current
working directory looking for a repo-local .tusker/ vault. Current markers
include .tusker/config.yaml, .tusker/work, and .tusker/knowledge/domains.
For a linked worktree without its own vault, use --use-project-vault to route
to the registered canonical checkout. Creating a separate graph requires the
explicit tusker init --isolated-vault confirmation.

Start here:
  tusker init --yes
  tusker capabilities --json
  tusker new epic --vault ./.tusker --acronym APP --title "App foundation"
  tusker new task --vault ./.tusker --epic APP --title "Implement auth" --size m --risk medium

Commands:
  init                initialize or refresh a repo vault
  reset               delete Tusker state, preserve specs, and relaunch a repo
  relaunch            alias for reset
  capabilities        print the installed-binary capability manifest (JSON only)
  new                 create epics, tasks, gates, and decisions
  list                list work records
  search              search tracker notes without generated files or attachments
  show                show a bounded note capsule or selected section
  print               render a Tusker note as terminal-friendly Markdown
  open                open a Tusker note in the OS, editor, or Obsidian
  context             audit Codex JSONL context and tool-output bloat
  status              move a task through its workflow
  discard             abandon work safely while preserving its history
  next                show the next pickable task
  work                atomically own and retire one interactive work session
  claim               alias for work start
  evidence            add evidence records
  gate                list, satisfy, waive, or obsolete gates
  wave                create, edit, and show named task batches
  land                run the serialized wave merge lane
  digest              render the operator morning digest
  escalate            record or acknowledge runner escalations
  departure           inspect or control scheduled departures
  feedback            add agent feedback notes and generate digests
  logbook             render a plain-language daily digest for a product reader
  improve             opt-in scans for repeated work worth packaging
  xcode               diagnose Xcode generated build-state failures
  attempt             start or hand off attempts
  handoff             hand off the latest attempt for a task
  brief               print human briefs
  packet              generate agent or reviewer packets
  closeout            emit or inspect terminal human-wait checkpoints
  dashboard           build or open generated dashboards
  reconcile           recompute readiness and next-action projections
  state               sync, import, or export runtime state branch files
  vault               symlink repo trackers into a shared Obsidian vault
  daemon              operator loop for registered local projects
  config              inspect resolved Tusker configuration with provenance
  automation          plan, inspect, and manually dispatch daemon automation work
  factory             inspect the read-only factory operations projection
  projects            register repositories for daemon pickup
  runs                inspect, tail, interrupt, release, and retire daemon runs
  streams             show the generated live/landed orchestration lane board
  gate-run            run gate-tier proof in harvest mode behind preflight refusal
  gate-ledger         check or record tree-keyed gate results
  serve               serve the read-only localhost control room
  refresh             run one daemon poll tick
  install             install skill bundles; binary link is opt-in with --bin
  purge               dry-run or remove generated Tusker repo state
  uninstall           dry-run or remove machine-level Tusker state
  gc                  sweep stale entries out of .tusker/scratch
  close               close a task after gates and evidence pass
  accept              accept, confirm proof, and close a green task in one step
  validate            check vault invariants
  reindex             rebuild generated indexes
  update              refresh skill bundles; binary link is opt-in with --bin
  skill               doctor, route, and pack project skills
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
  tusker context --help
  tusker status --help
  tusker install --help
  tusker reset --help
  tusker relaunch --help
  tusker purge --help
  tusker gc --help
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
	case "acp", "acp install", "acp doctor", "acp setup":
		printACPAdapterHelp()
	case "actor", "actor correction":
		fmt.Println("Usage: tusker actor correction plan|apply|list ...\n\nActor corrections are append-only, human-gated metadata projections; original event bytes never change. Apply is unavailable until exact-verification human-control authority is installed.")
	case "capabilities":
		printCapabilitiesHelp()
	case "init":
		printInitHelp()
	case "reset", "relaunch":
		printResetHelp()
	case "runner", "runner catalog", "runner profiles", "runner route":
		printRunnerHelp()
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
	case "execution", "execution register", "execution attach", "execution rename", "execution bind", "execution detach", "execution rebind", "execution inbox", "execution list", "execution show", "execution cancel", "execution launch":
		printExecutionHelp()
	case "claim":
		printClaimHelp()
	case "evidence":
		printEvidenceHelp()
	case "migrate vault-root":
		printMigrateVaultRootHelp()
	case "wave", "wave create", "wave add", "wave remove", "wave show", "wave brief", "wave preflight", "wave arm", "wave pause", "wave resume", "wave disarm", "wave refingerprint", "wave re-fingerprint", "land", "brief", "dashboard", "closeout", "closeout status", "gate-run", "digest", "escalate", "escalate ack", "departure", "departure check", "departure status", "departure history", "departure hold", "departure resume":
		printOperatorCommandHelp(command)
	case "handoff", "finish", "gate", "delivery", "delivery plan", "delivery context", "delivery import", "delivery review", "delivery start", "delivery doctor", "delivery rollout", "trace", "trace list", "trace show", "trace replay", "proof", "attempt", "proposal", "propose", "redact", "packet", "reconcile", "state", "attachments", "migrate", "migrate evidence-policy", "migrate close-policy":
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
	case "context", "context audit":
		printContextHelp()
	case "validate":
		printValidateHelp()
	case "purge":
		printPurgeHelp()
	case "gc":
		printGCHelp()
	case "reindex":
		printReindexHelp()
	case "docs", "docs find", "docs new", "docs map", "docs status", "docs verify", "docs adopt":
		printDocsHelp()
	case "domain", "domain list", "domain show", "domain new", "domain canon":
		printDomainHelp()
	case "knowledge", "knowledge new":
		printKnowledgeHelp()
	case "skill", "skill doctor", "skill route", "skill pack", "skill sync", "skill bundle", "skill audit-agent-guidance":
		printSkillHelp()
	case "setup", "setup doctor", "setup repair":
		printSetupHelp()
	case "publish", "publish skill":
		printPublishHelp()
	case "vault", "vault set", "vault status", "vault mount", "vault unmount", "vault repair", "vault move":
		printVaultHelp()
	case "daemon", "daemon run", "daemon status", "daemon limits", "daemon resume", "daemon stop", "daemon service":
		printDaemonHelp()
	case "config", "config resolve":
		printConfigHelp()
	case "automation", "automation status", "automation queue", "automation explain", "automation plan", "automation dispatch", "automation collect-external", "automation external-loop", "automation advance-external":
		printAutomationHelp()
	case "factory", "factory operations":
		printFactoryOperationsHelp()
	case "projects", "projects add", "projects list", "projects limits", "projects enable", "projects disable", "projects rebind", "projects remove", "projects prune":
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
	default:
		return false
	}
	return true
}

// printOperatorCommandHelp is intentionally side-effect free. Keep these
// short command contracts separate from the broad examples so `--help`
// never falls through to a command that opens a vault or writes state.
func printOperatorCommandHelp(command string) {
	switch {
	case command == "wave":
		fmt.Println(`Usage:
  tusker wave create|add|remove|show|brief|preflight|arm|pause|resume|disarm|refingerprint ...

Purpose:
  Manage a named, task-backed delivery wave.`)
	case command == "wave refingerprint" || command == "wave re-fingerprint":
		fmt.Println(`Usage:
  tusker wave refingerprint <WAVE-ID> --dry-run [--json]
  tusker wave refingerprint <WAVE-ID> --confirm <sha256:fingerprint> [--json]

Purpose:
  Refresh stale imported factory-intake material without reauthoring or
  arming the wave. The dry-run is read-only; confirmation preserves disarmed
  state and cannot dispatch work.`)
	case strings.HasPrefix(command, "wave "):
		fmt.Printf("Usage:\n  tusker %s ...\n\nPurpose:\n  Operate on a named delivery wave.\n", command)
	case command == "land":
		fmt.Println(`Usage:
  tusker land <TASK-ID|WAVE-ID> [--json]

Purpose:
  Run the serialized merge lane for a task or wave.`)
	case command == "brief":
		fmt.Println(`Usage:
  tusker brief [<TASK-ID>] [--owner <owner>] [--json]

Purpose:
  Print a bounded human brief without changing tracker state.`)
	case command == "dashboard":
		fmt.Println(`Usage:
  tusker dashboard build|open <name>

Purpose:
  Build or locate generated dashboards.`)
	case strings.HasPrefix(command, "closeout"):
		fmt.Printf("Usage:\n  tusker %s <TASK-ID> [options]\n\nPurpose:\n  Emit or inspect a terminal human-wait checkpoint.\n", command)
	case command == "gate-run":
		fmt.Println(`Usage:
  tusker gate-run [--changed] [--command <command>] [--profile <name>] [--json]

Purpose:
  Run configured gate-tier proof. Help performs no preflight or execution.`)
	case command == "digest":
		fmt.Println(`Usage:
  tusker digest [--since <RFC3339|YYYY-MM-DD>] [--all] [--json]

Purpose:
  Render the operator digest. Without --since or --all, show the last 24 hours;
  --all explicitly includes all recorded state.`)
	case command == "escalate":
		fmt.Println(`Usage:
  tusker escalate -s <P0|P1|P2> --task <TASK-ID> --reason <reason> <description>
  tusker escalate ack <ESC-ID> --by <actor>

Purpose:
  Record or acknowledge a bounded runner escalation.`)
	case command == "escalate ack":
		fmt.Println(`Usage:
  tusker escalate ack <ESC-ID> --by <actor> [--json]

Purpose:
  Acknowledge one open escalation.`)
	case command == "departure":
		fmt.Println(`Usage:
  tusker departure check|status|history|hold|resume ...

Purpose:
  Inspect or control scheduled promotion departures.`)
	case strings.HasPrefix(command, "departure "):
		fmt.Printf("Usage:\n  tusker %s ...\n\nPurpose:\n  Inspect or control scheduled promotion departures.\n", command)
	}
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
  tusker daemon service stop|refresh|status|uninstall [--json]

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
  - service refresh atomically updates its executable without starting a daemon
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
  tusker daemon service refresh
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
  tusker new epic --acronym APP --title "App foundation"
  tusker new task --epic APP --title "Add login"
  tusker new gate --blocks APP-T-0001 --kind auth --owner human:sarav --action "Provision credentials." --verification "The provider reports ready."
  tusker verify add APP-T-0001 --covers A1 --check "go test ./..." --result pass
  tusker status APP-T-0001 review --reason "Proof is complete."
  tusker review submit APP-T-0001 ...
  tusker close APP-T-0001
  tusker wave preflight W-0001 --json
  tusker wave arm W-0001 --by human:sarav
  tusker delivery plan --spec docs/system/00-overview.md --out .tusker/scratch/delivery-plan.yaml
  tusker delivery doctor --plan .tusker/scratch/delivery-plan.yaml --json
  tusker delivery import --plan .tusker/scratch/delivery-plan.yaml --dry-run
  tusker delivery review --plan .tusker/scratch/delivery-plan.yaml --json
  tusker delivery start --plan .tusker/scratch/delivery-plan.yaml --confirm sha256:<fingerprint> --by human:<name>
  tusker migrate evidence-policy [--write] [--json]
  tusker migrate close-policy [--write] [--json]
  tusker migrate vault-root --to .tusker [--dry-run] [--json]

Purpose:
  Manage repository work records, proof, gates, review, delivery, and closeout.
  A Tusker delivery plan is inert until an authorized final start.
  Planning, review, import, and preflight do not dispatch work.`)
}

func printMigrateVaultRootHelp() {
	fmt.Println(`Usage:
  tusker migrate vault-root --to .tusker [--vault <path>] [--dry-run] [--json]

Purpose:
  Explicitly move a repo-local Tusker vault, update .tusker/config.yaml storage roots,
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
  tusker projects rebind --id <project-id> --repo <canonical-path> --vault <canonical-path> [--allow-dirty] [--dry-run] [--json]
  tusker projects remove <project-id> [--json]
  tusker projects prune [--apply] [--dry-run] [--json]

Purpose:
  Register repo-local Tusker vaults for daemon pickup. Obsidian remains the
  editing surface; project registration only tells the local runtime what to
  poll.

Behavior:
  - project WORKFLOW.md, tasks, knowledge, and source remain repo-local
  - shared daemon state, logs, limits, and runtime metadata live outside repos
  - rebind moves one disabled, quiescent project identity to a clean validated repo/vault without replacing its runtime history
  - --allow-dirty is an explicit opt-in to rebind a Git worktree with uncommitted changes
  - prune previews registrations whose tracker roots no longer exist and their
    matching dangling Obsidian-vault symlinks; --apply performs the removal
  - on macOS, projects under Desktop, Documents, Downloads, or iCloud Drive
    receive a launchd access warning during add/enable

Examples:
  tusker projects add --repo . --vault ./.tusker
  tusker projects list
  tusker projects disable --repo .
  tusker projects rebind --id <project-id> --repo /repos/backend --vault /repos/backend/.tusker --dry-run
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
  tusker runs inspect APP-T-0001 --json
  tusker runs events APP-T-0001 --lines 20
  tusker runs interrupt APP-T-0001
  tusker runs release APP-T-0001 --json
  tusker runs retire APP-T-0001 --reason "over retry cap" --json
  tusker redrive APP-T-0001 --reason "operator reviewed spend" --json`)
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
  worker. Claim is an alias for work start.`)
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
  Operate domain canon under .tusker/knowledge/domains/**.

Examples:
  tusker domain list
  tusker domain show runtime --capsule
  tusker domain new providers --title "Providers" --summary "External API integrations."`)
}

func printKnowledgeHelp() {
	fmt.Println(`Usage:
  tusker knowledge new <domain>/<folder>/<slug> --kind runbook|decision|invariant|interface|glossary|source --title "..."

Purpose:
  Create leaf nodes under .tusker/knowledge/domains/**. Routing and reading
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
  Validate and route project skills. skill doctor checks the repo skill
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
  tusker publish skill [--out ./dist/project-skill]

Purpose:
  Export the current project skill package from .tusker/SKILL.md and
  .tusker/knowledge/domains/**.`)
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
  tusker init [--vault <path>] [--yes] [--fresh] [--purge-state] [--preserve-specs] [--isolated-vault] [--with-pointers] [--with-contract] [--with-mount]

What it does:
  1. initializes a repo-local vault under .tusker by default
  2. writes current workflow and configuration files
  3. writes .tusker/SKILL.md and knowledge/domains/** starter canon
  4. reindexes the vault and refreshes generated views
  5. optionally injects pointers, installs repo-contract files, and mounts the
    tracker when explicitly requested

Flags:
  --vault <path>    target vault path (default: ./.tusker)
  --isolated-vault   explicitly allow a second vault for a registered project
  --yes             accept the safe minimum: vault + reindex only
  --fresh           move an existing target vault aside and recreate it cleanly
  --purge-state     delete generated Tusker state first; implies the same safe
                    scope as 'tusker purge --only-tusker-state --yes'
  --preserve-specs  with --purge-state, retain .tusker/specs/** across the reset
  --vault-only      update only the vault; skip pointers and repo-contract files
  --with-pointers   opt in to AGENTS.md / CLAUDE.md pointer injection
  --with-contract   opt in to repo-contract helper docs
  --with-mount      opt in to mounting the tracker in the configured Obsidian vault
  --no-pointers     skip pointer injection
  --no-contract     skip repo-contract helper docs
  --no-mount        skip Obsidian mounting

Examples:
  tusker init --yes
  tusker init --yes --with-pointers --with-contract
  tusker init --yes --fresh --purge-state`)
}

func printDocsHelp() {
	fmt.Println(`Documentation graph commands:
  tusker docs find <query>
  tusker docs new <subject> [--kind doc|spec]
  tusker docs map
  tusker docs status
  tusker docs verify <subject>
  tusker docs adopt [--dry-run] [--json]
  tusker docs adopt --table <file> --approve --by human:<name> [--json]
  tusker docs adopt --table <file> --approve --by user-session:<id> [--approval-token user-session:<id>@<fingerprint>] [--json]

Adoption is a reviewed batch. The default and --dry-run print a fingerprinted
JSON table and never write. Edit that table, set approved_by, and pass the exact
table with --approve; every row is preflighted before any write. Unattended runs
require --by human:<name>. An interactive agent session may use the explicit
user-session:<id> approval path; --approval-token binds that user receipt to the
exact proposal fingerprint. Every approval, apply, and failure is written to
.tusker/events as an auditable event. Promote and merge preserve their sources.
Tombstone rewrites a source as a superseded signpost only when explicitly
present in the approved table; no disposition deletes a file. Generated map
artifacts are left untouched; run tusker docs map after review. --apply and
--yes are not accepted aliases.`)
}

func printNewHelp() {
	fmt.Println(`Usage:
  tusker new epic [--vault <path>] --acronym <ACR> --title <title> [--summary <text>] [--owner <name>] [--spec-refs <csv>]
  tusker new task [--vault <path>] --epic <ACR> --title <title> [--status ready|backlog|review|rework] [--priority p0|p1|p2|p3] [--size s|m|l|xl] [--risk low|medium|high|critical] [--spec-refs <csv>] [--evidence-required automated_test]
  tusker new gate --blocks <TASK-ID> --kind <gate-kind> --owner <owner> --action <text> --verification <proof>
  tusker new decision --epic <ACR> --title <title>

Purpose:
  Create current work objects.

Notes:
  Task IDs are allocated only after a successful create. For ordered batches,
  pass explicit --id values; a refused create does not reserve an ID.

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
  Move a task through its durable workflow. Runtime activity is represented
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
  Return the next pickable task. Pickable means status ready or rework,
  readiness ready, next_owner agent or agent:*, and exact --owner match when provided.
  --domain matches the task domains list. --lane matches lane or lanes frontmatter.

Ranking:
  priority first (p0 before p1), then risk (critical before high), then task id.
  --explain prints the selected task plus skipped candidates and reasons.
  --claim uses the same rules, then writes a local lease.`)
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
  Add, promote, or prune evidence records. For inline proof, prefer tusker verify add.

Scratch retention:
  .tusker/scratch/ is raw exhaust and is not durable: scratch/<task-id>/ is
  deleted when the task closes, and 'tusker gc' sweeps anything older than 14
  days. Promote anything worth keeping to evidence before close.`)
}

func printVerifyHelp() {
	fmt.Println(`Usage:
  tusker verify add <task-id> --covers A1,A2 --check <command-or-manual-check> --result pass|fail|blocked|skipped|waived [--note <text>]
  tusker verify add <task-id> --rows "A1|go test ./pkg|pass|note"
  tusker verify add <task-id> --batch-file <path>
  tusker verify remove <task-id> --index <one-based-row>
  tusker verify recipe <task-id> [--files <path[,path...]>]
  tusker verify <id> [--vault <path>] [--by <name>] [--summary <text>]
  tusker verify --id <id> [--vault <path>] [--by <name>] [--summary <text>]

Purpose:
  Add inline verification rows or suggest scoped verification recipes.

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
  Close a task after required evidence is present, close policy passes, and
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
  Use --runnable for agent-runnable tasks only: task kind, status ready/rework,
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
  tusker show APP-T-0001
  tusker show APP-T-0001 --acceptance
  tusker show APP-T-0001 --evidence
  tusker show APP-T-0001 --full`)
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
  tusker validate [--path <vault-relative-path> | --epic <ACR>] [--json]
  tusker validate --branch-policy [--base origin/main] [--json]
  tusker validate --branch-policy-only [--base origin/main] [--json]
  tusker validate --staged --branch-policy [--json]

Purpose:
  Check the vault against Tusker schema and workflow invariants.

Options:
  --path                Report findings only under this vault-relative path.
  --epic                Report findings only for records belonging to this epic.
  --branch-policy       Include protected state-field diff checks.
  --branch-policy-only  Run only protected state-field diff checks.
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
