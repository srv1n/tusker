package main

import (
	"fmt"
	"os"
	"strings"
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
		if command == "help" && len(argv) > 2 && !strings.HasPrefix(argv[2], "--") {
			command = "help " + argv[2]
			if len(argv) > 3 && !strings.HasPrefix(argv[3], "--") && commandTakesSubcommand(argv[2]) {
				command = command + " " + argv[3]
				return command, parseArgs(argv[4:])
			}
			return command, parseArgs(argv[3:])
		}
		if command == "legacy" && len(argv) > 2 && !strings.HasPrefix(argv[2], "--") {
			legacyCommand := argv[2]
			command = "legacy " + legacyCommand
			if len(argv) > 3 && !strings.HasPrefix(argv[3], "--") && commandTakesSubcommand(legacyCommand) {
				command = command + " " + argv[3]
				return command, parseArgs(argv[4:])
			}
			return command, parseArgs(argv[3:])
		}
		if len(argv) > 2 && !strings.HasPrefix(argv[2], "--") && commandTakesSubcommand(command) {
			command = command + " " + argv[2]
			return command, parseArgs(argv[3:])
		}
	}
	if len(argv) <= 2 {
		return command, parseArgs(nil)
	}
	return command, parseArgs(argv[2:])
}

func commandTakesSubcommand(command string) bool {
	switch command {
	case "docs", "domain", "knowledge", "publish", "skill", "new", "vault", "daemon", "projects", "runs", "context", "migrate", "hook", "legacy":
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
	if args.Bool("help") && command != "" {
		if printCommandHelp(command) {
			return 0, nil
		}
	}
	switch command {
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
	case "next":
		return 0, nextCmd(args)
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
	case "proof":
		return 0, proofV7Cmd(args)
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
		return legacyOnlyCommand("verify", "legacy verify")
	case "close":
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
		return 0, closeV7Cmd(args)
	case "legacy new epic":
		return 0, newV5Epic(args)
	case "legacy new task":
		return 0, newV5Task(args, "feature")
	case "legacy new bug":
		return 0, newV5Task(args, "bug")
	case "legacy new doc":
		return 0, newV5Doc(args)
	case "legacy next":
		return 0, nextV5Cmd(args)
	case "legacy verify":
		return 0, verifyCmd(args)
	case "legacy close":
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
		return 0, closeV5Cmd(args)
	case "legacy":
		printLegacyHelp()
		return 0, nil
	case "legacy init":
		args["legacy"] = "true"
		return 0, initCmd(args)
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
	case "legacy migrate v7":
		return 0, migrateV7Cmd(args)
	case "legacy migrate gates":
		return 0, migrateV7GatesCmd(args)
	case "reindex":
		return 0, reindex(args)
	case "validate":
		return validateCmd(args)
	case "list":
		return 0, listCmd(args)
	case "search":
		return 0, searchCmd(args)
	case "show":
		return 0, showCmd(args)
	case "compact":
		return 0, compactCmd(args)
	case "context audit":
		return 0, contextAuditCmd(args)
	case "docs init":
		return 0, docsInitCmd(args)
	case "docs model":
		return 0, docsModelCmd(args)
	case "docs map":
		return 0, docsMapCmd(args)
	case "docs catalog":
		return 0, docsCatalogCmd(args)
	case "docs freshness":
		return 0, docsFreshnessCmd(args)
	case "docs check":
		return 0, docsImpactCheckCmd(args)
	case "docs apply":
		return 0, docsImpactApplyCmd(args)
	case "docs noop":
		return 0, docsImpactNoopCmd(args)
	case "docs waive":
		return 0, docsImpactWaiveCmd(args)
	case "docs export":
		return 0, docsExportCmd(args)
	case "docs dev":
		return 0, docsDevCmd(args)
	case "docs build":
		return 0, docsBuildCmd(args)
	case "docs":
		return legacyOnlyCommand("docs", "legacy docs")
	case "legacy docs init":
		return 0, docsInitCmd(args)
	case "legacy docs model":
		return 0, docsModelCmd(args)
	case "legacy docs map":
		return 0, docsMapCmd(args)
	case "legacy docs catalog":
		return 0, docsCatalogCmd(args)
	case "legacy docs freshness":
		return 0, docsFreshnessCmd(args)
	case "legacy docs check":
		return 0, docsImpactCheckCmd(args)
	case "legacy docs apply":
		return 0, docsImpactApplyCmd(args)
	case "legacy docs noop":
		return 0, docsImpactNoopCmd(args)
	case "legacy docs waive":
		return 0, docsImpactWaiveCmd(args)
	case "legacy docs export":
		return 0, docsExportCmd(args)
	case "legacy docs dev":
		return 0, docsDevCmd(args)
	case "legacy docs build":
		return 0, docsBuildCmd(args)
	case "legacy docs":
		printDocsHelp()
		return 0, nil
	case "domain list":
		return 0, domainListCmd(args)
	case "domain show":
		return 0, domainShowCmd(args)
	case "domain new":
		return 0, domainNewCmd(args)
	case "domain canon":
		return 0, domainCanonCmd(args)
	case "domain graph":
		return 0, domainGraphCmd(args)
	case "domain":
		return legacyOnlyCommand("domain", "legacy domain")
	case "legacy domain list":
		return 0, domainListCmd(args)
	case "legacy domain show":
		return 0, domainShowCmd(args)
	case "legacy domain new":
		return 0, domainNewCmd(args)
	case "legacy domain canon":
		return 0, domainCanonCmd(args)
	case "legacy domain graph":
		return 0, domainGraphCmd(args)
	case "legacy domain":
		printDomainHelp()
		return 0, nil
	case "knowledge map":
		return 0, knowledgeMapCmd(args)
	case "knowledge list":
		return 0, knowledgeListCmd(args)
	case "knowledge show":
		return 0, knowledgeShowCmd(args)
	case "knowledge route":
		return 0, knowledgeRouteCmd(args)
	case "knowledge freshness":
		return 0, knowledgeFreshnessCmd(args)
	case "knowledge check":
		return 0, knowledgeCheckCmd(args)
	case "knowledge apply":
		return 0, knowledgeApplyCmd(args)
	case "knowledge noop":
		return 0, knowledgeNoopCmd(args)
	case "knowledge waive":
		return 0, knowledgeWaiveCmd(args)
	case "knowledge new":
		return 0, knowledgeNewCmd(args)
	case "skill doctor":
		return skillV7DoctorCmd(args)
	case "skill route":
		return 0, skillV7RouteCmd(args)
	case "skill pack":
		return 0, skillV7PackCmd(args)
	case "skill":
		printSkillHelp()
		return 0, nil
	case "knowledge":
		return legacyOnlyCommand("knowledge", "legacy knowledge")
	case "legacy knowledge map":
		return 0, knowledgeMapCmd(args)
	case "legacy knowledge list":
		return 0, knowledgeListCmd(args)
	case "legacy knowledge show":
		return 0, knowledgeShowCmd(args)
	case "legacy knowledge route":
		return 0, knowledgeRouteCmd(args)
	case "legacy knowledge freshness":
		return 0, knowledgeFreshnessCmd(args)
	case "legacy knowledge check":
		return 0, knowledgeCheckCmd(args)
	case "legacy knowledge apply":
		return 0, knowledgeApplyCmd(args)
	case "legacy knowledge noop":
		return 0, knowledgeNoopCmd(args)
	case "legacy knowledge waive":
		return 0, knowledgeWaiveCmd(args)
	case "legacy knowledge new":
		return 0, knowledgeNewCmd(args)
	case "legacy knowledge":
		printKnowledgeHelp()
		return 0, nil
	case "publish export":
		return 0, publishExportCmd(args)
	case "publish build":
		return 0, publishBuildCmd(args)
	case "publish dev":
		return 0, publishDevCmd(args)
	case "publish llms":
		return 0, publishLLMSCmd(args)
	case "publish skill":
		return 0, publishSkillCmd(args)
	case "publish":
		return legacyOnlyCommand("publish", "legacy publish")
	case "legacy publish export":
		return 0, publishExportCmd(args)
	case "legacy publish build":
		return 0, publishBuildCmd(args)
	case "legacy publish dev":
		return 0, publishDevCmd(args)
	case "legacy publish llms":
		return 0, publishLLMSCmd(args)
	case "legacy publish skill":
		return 0, publishSkillCmd(args)
	case "legacy publish":
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
	case "daemon status":
		return 0, daemonStatusCmd(args)
	case "daemon limits":
		return 0, daemonLimitsCmd(args)
	case "daemon":
		printDaemonHelp()
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
	case "projects":
		printProjectsHelp()
		return 0, nil
	case "runs inspect":
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
		return 0, runsInspectCmd(args)
	case "runs logs":
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
		return 0, runsLogsCmd(args)
	case "runs events":
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
		return 0, runsEventsCmd(args)
	case "runs interrupt":
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
		return 0, runsInterruptCmd(args)
	case "runs":
		printRunsHelp()
		return 0, nil
	case "graph":
		return legacyOnlyCommand("graph", "legacy graph")
	case "legacy graph":
		return 0, graphCmd(args)
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
	case "help next":
		printNextHelp()
		return 0, nil
	case "help claim":
		printClaimHelp()
		return 0, nil
	case "help evidence":
		printEvidenceHelp()
		return 0, nil
	case "help handoff", "help gate", "help attempt", "help proposal", "help propose", "help brief", "help packet", "help closeout", "help closeout status", "help dashboard", "help reconcile", "help state", "help hook", "help hook install", "help migrate", "help migrate v7", "help migrate gates":
		printV7Help()
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
	case "help daemon", "help daemon run", "help daemon status", "help daemon limits":
		printDaemonHelp()
		return 0, nil
	case "help projects", "help projects add", "help projects list", "help projects limits", "help projects enable", "help projects disable", "help projects remove":
		printProjectsHelp()
		return 0, nil
	case "help runs", "help runs inspect", "help runs logs", "help runs events", "help runs interrupt":
		printRunsHelp()
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
	case "help sync-repo-contract":
		printSyncRepoContractHelp()
		return 0, nil
	case "help init":
		printInitHelp()
		return 0, nil
	case "help", "--help", "-h", "":
		printHelp()
		return 0, nil
	case "help legacy", "help legacy init", "help legacy new", "help legacy docs", "help legacy domain", "help legacy knowledge", "help legacy publish", "help legacy migrate":
		printLegacyHelp()
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

func legacyOnlyCommand(command, replacement string) (int, error) {
	return 1, tuskerError(errorInvalidArg, fmt.Sprintf("%s is legacy-only; use `tusker %s`", command, replacement), withHint("V7 is the default top-level CLI surface."))
}

func printHelp() {
	fmt.Println(`Tusker - V7 repo-local work tracking

Vault discovery: if [--vault] is omitted, tusker walks up from the current
working directory looking for a repo-local tusker/ vault. V7 markers include
tusker.yaml, tusker/work, and tusker/knowledge/domains.

Start here:
  tusker init --yes
  tusker new epic --vault ./tusker --acronym APP --title "App foundation"
  tusker new task --vault ./tusker --epic APP --title "Implement auth" --size m --risk medium

Commands:
  init                initialize or refresh a repo vault
  new                 create V7 epics, tasks, gates, and decisions
  list                list work records
  search              search tracker notes without generated files or attachments
  show                show a bounded note capsule or selected section
  compact             remove empty optional metadata and disposable note scaffolding
  context             audit Codex JSONL context and tool-output bloat
  status              move a V7 task through its workflow
  next                show the next pickable V7 task
  claim               create a V7 local lease
  evidence            add V7 evidence records
  gate                list/satisfy/waive/obsolete V7 gates
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
  projects            register repositories for daemon pickup
  runs                inspect, tail, and interrupt daemon runs
  refresh             run one daemon poll tick
  install             install binary and skill bundles
  close               close a V7 task after gates and evidence pass
  validate            check vault invariants
  reindex             rebuild generated indexes
  update              refresh the installed binary link and skill bundle
  skill               doctor, route, and pack V7 project skills
  legacy              explicit access to old tracker, knowledge, docs, and migration commands

Help:
  tusker new --help
  tusker vault --help
  tusker daemon --help
  tusker runs --help
  tusker search --help
  tusker show --help
  tusker compact --help
  tusker context --help
  tusker status --help
  tusker install --help
  tusker gate --help
  tusker packet --help
  tusker skill --help
  tusker legacy --help

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
	case "next":
		printNextHelp()
	case "claim":
		printClaimHelp()
	case "evidence":
		printEvidenceHelp()
	case "handoff", "finish", "gate", "proof", "attempt", "proposal", "propose", "redact", "brief", "packet", "closeout", "closeout status", "dashboard", "reconcile", "state", "hook", "hook install", "attachments", "migrate", "migrate v7", "migrate gates", "migrate evidence-policy":
		printV7Help()
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
	case "compact":
		printCompactHelp()
	case "context", "context audit":
		printContextHelp()
	case "validate":
		printValidateHelp()
	case "reindex":
		printReindexHelp()
	case "docs", "docs init", "docs model", "docs map", "docs catalog", "docs freshness", "docs check", "docs apply", "docs noop", "docs waive", "docs export", "docs dev", "docs build":
		printLegacyRedirectHelp("docs", "legacy docs")
	case "domain", "domain list", "domain show", "domain new", "domain canon", "domain graph":
		printDomainHelp()
	case "knowledge", "knowledge map", "knowledge list", "knowledge show", "knowledge route", "knowledge freshness", "knowledge check", "knowledge apply", "knowledge noop", "knowledge waive", "knowledge new":
		printKnowledgeHelp()
	case "skill", "skill doctor", "skill route", "skill pack":
		printSkillHelp()
	case "publish", "publish export", "publish build", "publish dev", "publish llms", "publish skill":
		printPublishHelp()
	case "graph":
		printGraphHelp()
	case "vault", "vault set", "vault status", "vault mount", "vault unmount", "vault repair", "vault move":
		printVaultHelp()
	case "daemon", "daemon run", "daemon status", "daemon limits":
		printDaemonHelp()
	case "projects", "projects add", "projects list", "projects limits", "projects enable", "projects disable", "projects remove":
		printProjectsHelp()
	case "runs", "runs inspect", "runs logs", "runs events", "runs interrupt":
		printRunsHelp()
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
	fmt.Println(`Usage:
  tusker legacy new epic --acronym <ACR> --title <title>
  tusker legacy new task --epic <ACR> --title <title>
  tusker legacy new bug --epic <ACR> --title <title>
  tusker legacy new doc --node <route> --title <title>
  tusker legacy init [--vault <path>] [--yes] [--fresh]
  tusker legacy next [--epic <ACR>] [--json]
  tusker legacy verify <id> [--by <name>]
  tusker legacy close <id> [--by <name>]
  tusker legacy docs <subcommand>
  tusker legacy domain <subcommand>
  tusker legacy knowledge <subcommand>
  tusker legacy publish <subcommand>
  tusker legacy migrate v7|gates
  tusker legacy graph <node-or-task-or-domain>

Purpose:
  Explicit access to the V5 tracker, V6 knowledge graph, docs-map/docs
  publishing, and migration surfaces. Top-level commands are V7 defaults.`)
}

func printLegacyRedirectHelp(command, replacement string) {
	fmt.Printf("`tusker %s` is legacy-only. Use `tusker %s`.\n", command, replacement)
}

func printDaemonHelp() {
	fmt.Println(`Usage:
  tusker daemon run [--once]
  tusker daemon status [--json]
  tusker daemon limits [--max-active-runs <n>] [--json]

Purpose:
  Operator/internal runtime loop for registered local projects. The normal
  workflow remains task-centric: edit a task, move it to active or rework, then
  verify and close after the daemon hands it to review.

Behavior:
  - daemon run polls registered projects and dispatches active/rework tasks
  - --once performs one poll tick and exits
  - daemon status reports state-root, project count, and active run count
  - daemon limits reads or updates the global active-run cap

Examples:
  tusker daemon status
  tusker daemon run --once
  tusker daemon limits --max-active-runs 1`)
}

func printV7Help() {
	fmt.Println(`Usage:
  tusker new epic --acronym HSP --title "First-class harness provider setup"
  tusker new task --epic HSP --title "Add provider smoke harness"
  tusker new gate --blocks HSP-T-0001 --kind auth --owner human:sarav
  tusker new decision --epic HSP --title "Use repo-local branch-safe tracker"

  tusker gate list --open [--owner human:sarav]
  tusker gate satisfy HSP-G-0001 --evidence "Provider endpoint returned ready."
  tusker gate waive HSP-G-0002 --reason "Live smoke deferred."
  tusker gate obsolete HSP-G-0003 --reason "Task superseded."

  tusker claim HSP-T-0001 --owner agent:codex
  tusker heartbeat HSP-T-0001
  tusker release HSP-T-0001
  tusker attempt start HSP-T-0001
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

  tusker brief HSP-T-0001
  tusker packet HSP-T-0001 --for agent [--write]
  tusker packet HSP-T-0001 --for reviewer [--write]
  tusker closeout HSP-T-0001 --emit-packet --validate "go test ./..." --json
  tusker closeout status HSP-T-0001 --json
  tusker dashboard build
  tusker reconcile
  tusker state sync [--branch tusker/state] [--push] [--remote origin]
  tusker state import [--branch tusker/state] [--fetch] [--remote origin]
  tusker state export [--dir .tusker-runtime/state]
  tusker hook install pre-commit [--force]
  tusker validate --branch-policy [--staged]
  tusker migrate v7 --dry-run [--json]
  tusker migrate gates --from-blocked-reason [--write] [--json]
  tusker migrate evidence-policy [--write] [--json]

Purpose:
  V7 repo-local, markdown-backed work records with first-class proof, gates, evidence,
  attempts, event-per-file history, generated briefs/packets/dashboards, and
  branch guards for protected task/gate state.`)
}

func printProjectsHelp() {
	fmt.Println(`Usage:
  tusker projects add [--repo <path>] [--vault <path>] [--json]
  tusker projects list [--json]
  tusker projects limits [--id <project-id>|--repo <path>|--vault <path>] [--max-active-runs <n>] [--json]
  tusker projects enable [--id <project-id>|--repo <path>|--vault <path>] [--json]
  tusker projects disable [--id <project-id>|--repo <path>|--vault <path>] [--json]
  tusker projects remove <project-id> [--json]

Purpose:
  Register repo-local Tusker vaults for daemon pickup. Obsidian remains the
  editing surface; project registration only tells the local runtime what to
  poll.

Examples:
  tusker projects add --repo . --vault ./tusker
  tusker projects list
  tusker projects disable --repo .`)
}

func printRunsHelp() {
	fmt.Println(`Usage:
  tusker runs inspect <task-id-or-record-id> [--json]
  tusker runs logs <task-id-or-record-id> [--lines <n>] [--follow] [--json]
  tusker runs events <task-id-or-record-id> [--lines <n>] [--follow] [--json]
  tusker runs interrupt <task-id-or-record-id> [--json]

Purpose:
  Inspect and control daemon runtime state for a task. These commands expose
  attempts, turns, sessions, event tails, logs, and interrupts without making
  runtime state part of task frontmatter.

Examples:
  tusker runs inspect ORC-T-0018 --json
  tusker runs events ORC-T-0018 --lines 20
  tusker runs interrupt ORC-T-0018`)
}

func printRefreshHelp() {
	fmt.Println(`Usage:
  tusker refresh [--quiet] [--json]

Purpose:
  Run one daemon poll tick for registered projects. This is the easiest local
  smoke path for checking whether active/rework tasks are picked up.`)
}

func printDomainHelp() {
	fmt.Println(`Usage:
  tusker domain list [--v7] [--json]
  tusker domain show <domain-id> [--v7] [--capsule|--full|--json]
  tusker domain new <domain-id> --title "..." [--summary "..."]
  tusker domain new <domain-id> --v7 --title "..." [--summary "..."]
  tusker domain canon <domain-id> [--v7] [--full]
  tusker domain graph <domain-id> [--depth 1] [--json]

Purpose:
  Inspect V6 source-truth domains, or create V7 domain canon folders under
  tusker/knowledge/domains/** with --v7.

Examples:
  tusker domain list
  tusker domain show runtime --capsule
  tusker domain new billing --title "Billing" --summary "Plans and invoices."
  tusker domain new providers --v7 --title "Providers"`)
}

func printKnowledgeHelp() {
	fmt.Println(`Usage:
  tusker knowledge map [--json]
  tusker knowledge list [--domain <id>] [--json]
  tusker knowledge new <node> --title "..." [--kind reference] [--source <paths>]
  tusker knowledge new <domain>/<folder>/<slug> --v7 --kind runbook|decision|invariant|interface|glossary|source --title "..."
  tusker knowledge show <node> [--capsule|--full|--section <name>|--json]
  tusker knowledge route "<intent>" [--limit <n>] [--json]
  tusker knowledge freshness [--stale] [--json]
  tusker knowledge check <TASK-ID> [--json]
  tusker knowledge apply <TASK-ID> --node <node> --reason "..."
  tusker knowledge noop <TASK-ID> --node <node> --reason "..."
  tusker knowledge waive <TASK-ID> --node <node> --reason "..."

Purpose:
  Operate the legacy V6 knowledge graph, or use --v7 with knowledge new to
  create V7 leaf nodes under tusker/knowledge/domains/**. Prefer V7 leaf
  generation for runbooks, interfaces, invariants, decisions, glossary entries,
  and raw source attribution under sources/.

Examples:
  tusker knowledge route "reviewer lane auto close"
  tusker knowledge show runtime/reference/reviewer-lane --capsule
  tusker knowledge freshness --stale
  tusker knowledge new providers/runbooks/oauth-refresh --v7 --kind runbook --title "OAuth refresh"`)
}

func printSkillHelp() {
	fmt.Println(`Usage:
  tusker skill doctor [--strict] [--json]
  tusker skill doctor --package <path> [--strict] [--json]
  tusker skill route "<intent>" [--json]
  tusker skill pack <TASK-ID> --budget <n> --for agent

Purpose:
  Validate and route V7 project skills. skill doctor checks the repo skill
  package, domain routes, docs publication sources, forbidden source-truth
  paths, local absolute paths, and task-domain coverage. skill route returns the
  smallest ordered read set for an intent. skill pack is an explicit wrapper
  over tusker packet for bounded task context.

Examples:
  tusker skill doctor --strict --json
  tusker skill route "change provider auth refresh logic" --json
  tusker skill pack VSD-T-0010 --budget 6000 --for agent`)
}

func printPublishHelp() {
	fmt.Println(`Usage:
  tusker publish export [--site ./site]
  tusker publish build [--site ./site] [--quiet]
  tusker publish dev [--site ./site] [--host 127.0.0.1] [--port 4321]
  tusker publish llms [--site ./site]
  tusker publish skill [--v7] [--out ./dist/project-skill]

Purpose:
  Generate V6 projections from tusker/domains/** or V7 project-skill bundles
  from tusker/knowledge/domains/**. Source truth stays in the vault; site, LLM
  lanes, and project-skill packages are disposable output.`)
}

func printGraphHelp() {
	fmt.Println(`Usage:
  tusker graph <node-or-task-or-domain> [--depth 1] [--json]

Purpose:
  Inspect a bounded V6 graph neighborhood across domains, knowledge nodes,
  tasks, and epics.`)
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
  tusker vault mount --repo . --vault ./tusker --name my-app
  tusker vault status`)
}

func printInitHelp() {
	fmt.Println(`Usage:
  tusker init [--vault <path>] [--yes] [--fresh]
  tusker init --profile generic|library|app|cli|infra|tusker|v7 [--yes] [--fresh]
  tusker legacy init [--vault <path>] [--yes] [--fresh]
  tusker init --migrate-v5 [--vault <path>] [--yes] [--vault-only]
  tusker init --migrate-v6 --dry-run [--vault <path>]

What it does:
  1. initializes a fresh vault if needed
  2. writes WORKFLOW.md
  3. injects Tusker pointers into AGENTS.md / CLAUDE.md
  4. installs repo-contract helper docs
  5. reindexes the vault

Default init creates a V7 repo-local work tracker with tusker/SKILL.md,
tusker/work/**, tusker/knowledge/domains/**, evidence, attempts, events,
dashboards, and generated packet/index folders.

tusker legacy init creates the old V5 docs/template scaffold.

With --migrate-v5 it initializes the legacy scaffold, then converts legacy
story/bug notes into V5 tasks, renames epic index files to V5 paths, adds
schemas, refreshes templates/views, and creates a side-by-side backup before
writing.

With --profile generic|library|app|cli|infra|tusker it creates a V6
knowledge-graph vault under tusker/domains/**. With --profile v7 or --v7 it
creates V7 with the default V7 profile.

With --migrate-v6 --dry-run it reports the clean-break V5-to-V6 path and field
rewrites. Automated V6 apply is intentionally not hidden behind compatibility aliases.

Flags:
  --vault <path>    target vault path (default: ./tusker)
  --yes             accept defaults without prompts
  --fresh           move an existing target vault aside and recreate it cleanly
  --profile <name>  create V6 layout for named profiles, or V7 when <name> is v7
  --v7              explicit alias for default V7 init
  --legacy          create the legacy V5 docs/template scaffold
  --migrate-v5      repair an existing legacy vault in place
  --migrate-v6      report explicit V5-to-V6 clean-break migration plan
  --dry-run         show the migration plan without writing
  --vault-only      update only the vault; skip AGENTS/CLAUDE and repo-contract files
  --no-backup       skip the migration backup
  --no-pointers     skip AGENTS.md / CLAUDE.md pointer injection
  --no-contract     skip repo-contract helper docs

Examples:
  tusker init --yes
  tusker init --profile generic --yes --fresh
  tusker init --profile v7 --yes --fresh
  tusker legacy init --yes --fresh
  tusker init --migrate-v5 --yes --vault-only
  tusker init --migrate-v5 --dry-run --vault ./tusker
  tusker init --migrate-v6 --dry-run --vault ./tusker`)
}

func printDocsHelp() {
	fmt.Println(`Usage:
  tusker legacy docs init [--site <path>] [--force]
  tusker legacy docs model [--json]
  tusker legacy docs map [<doc-node>] [--vault <path>] [--json]
  tusker legacy docs catalog [--vault <path>] [--json]
  tusker legacy docs freshness [--vault <path>] [--stale] [--json]
  tusker legacy docs check <id> [--vault <path>] [--json]
  tusker legacy docs apply <id> --node <doc-node> [--by <name>] [--reason <text>]
  tusker legacy docs noop <id> --node <doc-node> [--by <name>] [--reason <text>]
  tusker legacy docs waive <id> <doc-node> [--by <name>] --reason <text>
  tusker legacy docs export [--vault <path>] [--site <path>] [--clean] [--public-only] [--json]
  tusker legacy docs dev [--vault <path>] [--site <path>] [--watch] [--port <n>] [--host <host>]
  tusker legacy docs build [--vault <path>] [--site <path>] [--public-only] [--quiet] [--json]

Purpose:
  Own the documentation system from the CLI: explain the model, inspect the
  docs-map, read the generated catalog, check freshness, resolve task docs
  impact, and export/build the site.

What it means:
  _config/docs-map.yaml is the access layer. It maps exact doc_nodes to source
  pages, domains, Diátaxis mode, audience, agent layer, source-of-truth files,
  and stale_when triggers. Tasks name doc_nodes when work changes durable
  understanding. Close requires each targeted doc node to be applied, verified
  as no-op, or waived with a reason.
  docs build suppresses Astro output when --quiet or --json is set; failures
  include the final non-empty log tail.

Diátaxis modes:
  tutorial     learn by doing       -> Start here
  how-to       complete a task      -> Guides
  reference    look up facts        -> Reference
  explanation  understand why       -> Concepts

Agent layer:
  none         human-facing doc only
  capsule      human-facing doc with a compact agent note
  standalone   agent-facing runbook or recipe

Notes:
  docs model explains the philosophy without needing a vault.
  docs map lists controlled domains and doc_nodes.
  docs catalog shows reader-facing IA generated from docs-map.
  docs freshness shows stale/missing/verified docs and linked task evidence.
  docs check is a dry-run inspection for a task's doc_nodes.
  docs apply records that a target doc node was updated.
  docs noop records that a target doc node was checked and already correct.
  docs waive records an explicit no-change decision for a target doc node.
  The site lives under ./site by default. Repo-doc publishing is configured
  through docs/publication.yaml. Vault docs are included only when a vault is
  discoverable or --vault is provided.
  --watch re-exports when vault markdown, attachments, or registered repo docs
  change while Astro serves the generated tree.
  Export emits llms.txt, content-manifest.json, and canon-manifest.json so
  agents can tell published canon from stale source files.`)
}

func printNewHelp() {
	fmt.Println(`Usage:
  tusker new epic [--vault <path>] --acronym <ACR> --title <title> [--summary <text>] [--owner <name>]
  tusker new task [--vault <path>] --epic <ACR> --title <title> [--status ready|backlog|review|rework] [--priority p0|p1|p2|p3] [--size s|m|l|xl] [--risk low|medium|high|critical] [--evidence-required automated_test]
  tusker new gate --blocks <TASK-ID> --kind <gate-kind> --owner <owner>
  tusker new decision --epic <ACR> --title <title>

Purpose:
  Create V7 work objects. V5 task/doc creation is available through
  tusker legacy new ... only.

Examples:
  tusker new epic --vault ./tusker --acronym APP --title "App foundation"
  tusker new task --vault ./tusker --epic APP --title "Implement auth" --risk medium --size m
  tusker new gate --vault ./tusker --blocks APP-T-0001 --kind auth --owner human:sarav`)
}

func printStatusHelp() {
	fmt.Println(`Usage:
  tusker status <id> <status> [--vault <path>] [--actor <name>] [--reason <text>]
  tusker status --id <id> --status <status> [--vault <path>] [--actor <name>] [--reason <text>]

Statuses:
  draft, backlog, ready, active, blocked, review, rework, done, cancelled
  V7 task statuses: idea, backlog, ready, review, rework, done, cancelled, superseded

Purpose:
  Move a V5 task/epic/doc or V7 task through its durable workflow.

Notes:
  blocked requires either --blocked-by <TASK-ID[,TASK-ID]> or --block-reason <text>. Use
  backlog for shaped future work that should not be picked up in the current release.`)
}

func printNextHelp() {
	fmt.Println(`Usage:
  tusker next [--vault <path>] [--epic <ACR>] [--owner <name>] [--json]
  tusker next --claim --as <agent-or-person> [--vault <path>] [--epic <ACR>] [--json]

Purpose:
  Return the next pickable V7 task. Pickable means status ready or rework,
  readiness ready, and next_owner matching --owner when provided.

Ranking:
  priority first (p0 before p1), then risk (critical before high), then task id.
  --claim uses the same rules, then writes a V7 local lease.`)
}

func printClaimHelp() {
	fmt.Println(`Usage:
  tusker claim <id> --as <agent-or-person> [--vault <path>] [--reason <text>] [--json]

Purpose:
  Assign one ready/rework task and move it to active. Claim rejects draft,
  backlog, blocked, active, review, done, cancelled, and tasks with unresolved
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
  Attach proof to a V5 task or add/promote/prune V7 evidence records. For V7 inline proof, prefer tusker verify add.`)
}

func printVerifyHelp() {
	fmt.Println(`Usage:
  tusker verify add <task-id> --covers A1,A2 --check <command-or-manual-check> --result pass [--note <text>]
  tusker verify <id> [--vault <path>] [--by <name>] [--summary <text>]
  tusker verify --id <id> [--vault <path>] [--by <name>] [--summary <text>]

Purpose:
  Add inline V7 verification rows, or record legacy V5 verification on a task in review status.`)
}

func printCloseHelp() {
	fmt.Println(`Usage:
  tusker close <id> [--vault <path>] [--by <name>] [--reason <text>]
  tusker close --id <id> [--vault <path>] [--by <name>] [--reason <text>]

Purpose:
  Close a V7 task after required evidence is present, close policy passes, and
  no blocking gates remain open. V5 close is tusker legacy close.`)
}

func printListHelp() {
	fmt.Println(`Usage:
  tusker list [--vault <path>] [--json] [--type epic|task|doc] [--status <status>] [--epic <ACR>] [--open|--closed] [--limit <n>]

Purpose:
  Query V5 epics, tasks, and docs from the vault without reading note bodies.
  Start with epics, then drill into one epic's open tasks.

Examples:
  tusker list --vault ./tusker
  tusker list --vault ./tusker --type epic
  tusker list --vault ./tusker --epic ORC --type task --open
  tusker list --vault ./tusker --epic ORC --type task --open --limit 10
  tusker list --vault ./tusker --type task --status active
  tusker list --vault ./tusker --type doc --json`)
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

func printCompactHelp() {
	fmt.Println(`Usage:
  tusker compact <ID> [--vault <path>] [--write] [--json]
  tusker compact --all [--vault <path>] [--archive-logs] [--write] [--json] [--verbose]

Purpose:
  Dry-run or apply safe note compaction: remove empty optional frontmatter and
  disposable placeholder sections such as empty Execution plan and Work log.
  Substantive sections are preserved unless --archive-logs is set, which
  extracts V5 Work log sections into V7 attempt records and Verification log
  sections into V7 evidence before removing them.
  With --all, unchanged notes are hidden unless --verbose is set.

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
