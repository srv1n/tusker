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
  # Names that may appear in story.assignee. "sarav" (or any human name) is fine;
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
			if len(argv) > 3 && !strings.HasPrefix(argv[3], "--") && (argv[2] == "workflow" || argv[2] == "vault" || argv[2] == "daemon" || argv[2] == "projects" || argv[2] == "review" || argv[2] == "retry" || argv[2] == "runs" || argv[2] == "docs") {
				command = command + " " + argv[3]
				return command, parseArgs(argv[4:])
			}
			return command, parseArgs(argv[3:])
		}
		if len(argv) > 2 && !strings.HasPrefix(argv[2], "--") && (command == "workflow" || command == "vault" || command == "daemon" || command == "projects" || command == "review" || command == "retry" || command == "runs" || command == "docs") {
			command = command + " " + argv[2]
			return command, parseArgs(argv[3:])
		}
	}
	return command, parseArgs(argv[2:])
}

func parseArgs(argv []string) Args {
	args := Args{}
	for i := 0; i < len(argv); i++ {
		current := argv[i]
		if !strings.HasPrefix(current, "--") {
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
	case "bootstrap":
		return 0, bootstrap(args)
	case "new-epic":
		return 0, newEpic(args)
	case "new-story":
		return 0, createWorkItem(args, "story")
	case "new-bug":
		return 0, createWorkItem(args, "bug")
	case "new-doc":
		return 0, newDoc(args)
	case "handoff":
		return 0, handoffCmd(args)
	case "set-status":
		return 0, setStatus(args)
	case "pickup":
		return 0, pickup(args)
	case "release":
		return 0, release(args)
	case "attest":
		return 0, attest(args)
	case "signoff":
		return 0, signoff(args)
	case "attach-evidence":
		return 0, attachEvidence(args)
	case "promote-decision":
		return 0, promoteDecision(args)
	case "reindex":
		return 0, reindex(args)
	case "validate":
		return validateCmd(args)
	case "list":
		return 0, listCmd(args)
	case "epics":
		return 0, epicsCmd(args)
	case "move":
		return 0, moveCmd(args)
	case "workflow init":
		return 0, workflowInitCmd(args)
	case "workflow":
		printWorkflowHelp()
		return 0, nil
	case "vault set":
		return 0, vaultSetCmd(args)
	case "vault status":
		return 0, vaultStatusCmd(args)
	case "vault move":
		return 0, vaultMoveCmd(args)
	case "vault repair":
		return 0, vaultRepairCmd(args)
	case "vault mount":
		return 0, vaultMountCmd(args)
	case "vault unmount":
		return 0, vaultUnmountCmd(args)
	case "vault":
		printVaultHelp()
		return 0, nil
	case "mount":
		return 0, vaultMountCmd(args)
	case "unmount":
		return 0, vaultUnmountCmd(args)
	case "daemon run":
		return 0, daemonRunCmd(args)
	case "daemon status":
		return 0, daemonStatusCmd(args)
	case "daemon limits":
		return 0, daemonLimitsCmd(args)
	case "daemon":
		printDaemonHelp()
		return 0, nil
	case "projects list":
		return 0, projectsListCmd(args)
	case "projects add":
		return 0, projectsAddCmd(args)
	case "projects limits":
		return 0, projectsLimitsCmd(args)
	case "projects enable":
		return 0, projectsEnableCmd(args)
	case "projects disable":
		return 0, projectsDisableCmd(args)
	case "projects remove":
		return 0, projectsRemoveCmd(args)
	case "projects":
		printProjectsHelp()
		return 0, nil
	case "runs":
		return 0, runsCmd(args)
	case "runs inspect":
		return 0, runsInspectCmd(args)
	case "runs logs":
		return 0, runsLogsCmd(args)
	case "runs events":
		return 0, runsEventsCmd(args)
	case "runs interrupt":
		return 0, runsInterruptCmd(args)
	case "refresh":
		return 0, refreshCmd(args)
	case "review approve":
		return 0, reviewApproveCmd(args)
	case "review verify":
		return 0, reviewVerifyCmd(args)
	case "review request-changes":
		return 0, reviewRequestChangesCmd(args)
	case "review comment":
		return 0, reviewCommentCmd(args)
	case "review":
		printReviewHelp()
		return 0, nil
	case "retry now":
		return 0, retryNowCmd(args)
	case "retry":
		printRetryHelp()
		return 0, nil
	case "docs init":
		return 0, docsInitCmd(args)
	case "docs export":
		return 0, docsExportCmd(args)
	case "docs dev":
		return 0, docsDevCmd(args)
	case "docs build":
		return 0, docsBuildCmd(args)
	case "docs":
		printDocsHelp()
		return 0, nil
	case "sync-repo-contract":
		return 0, syncRepoContract(args)
	case "install":
		return 0, installCmd(args)
	case "update":
		return 0, updateCmd(args)
	case "init":
		return 0, initCmd(args)
	case "help bootstrap":
		printBootstrapHelp()
		return 0, nil
	case "help new-epic":
		printNewEpicHelp()
		return 0, nil
	case "help new-story":
		printNewStoryHelp()
		return 0, nil
	case "help new-bug":
		printNewBugHelp()
		return 0, nil
	case "help new-doc":
		printNewDocHelp()
		return 0, nil
	case "help handoff":
		printHandoffHelp()
		return 0, nil
	case "help set-status":
		printSetStatusHelp()
		return 0, nil
	case "help list":
		printListHelp()
		return 0, nil
	case "help validate":
		printValidateHelp()
		return 0, nil
	case "help reindex":
		printReindexHelp()
		return 0, nil
	case "help move":
		printMoveHelp()
		return 0, nil
	case "help sync-repo-contract":
		printSyncRepoContractHelp()
		return 0, nil
	case "help workflow", "help workflow init":
		printWorkflowHelp()
		return 0, nil
	case "help vault", "help vault set", "help vault status", "help vault move", "help vault repair", "help vault mount", "help vault unmount":
		printVaultHelp()
		return 0, nil
	case "help mount", "help unmount":
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
	case "help review", "help review verify", "help review approve", "help review request-changes", "help review comment":
		printReviewHelp()
		return 0, nil
	case "help retry", "help retry now":
		printRetryHelp()
		return 0, nil
	case "help docs", "help docs init", "help docs export", "help docs dev", "help docs build":
		printDocsHelp()
		return 0, nil
	case "help install":
		printInstallHelp()
		return 0, nil
	case "help update":
		printUpdateHelp()
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

func printHelp() {
	fmt.Println(`Tusker - markdown-first task tracking with optional daemon orchestration

Vault discovery: if [--vault] is omitted, tusker walks up from the current
working directory looking for a folder named tusker/ (or a vault-shaped dir
with WORKFLOW.md or _system/config.yaml). Pass --vault <path> only to override.

Start here:
  tusker init --yes
  tusker new-epic --vault ./tusker --acronym APP --title "App foundation"
  tusker new-story --vault ./tusker --epic APP --title "Implement auth" --size m --risk medium

Core:
  init                repo bootstrap: vault + workflow + repo contract
  bootstrap           create a vault skeleton only
  new-epic            create an epic
  new-story           create a story
  new-bug             create a bug
  new-doc             create a doc note
  handoff             render a worker / verifier / reviewer packet
  set-status          move a ticket through its durable workflow
  reindex             rebuild generated indexes
  validate            check vault invariants
  list                list notes and work items
  epics               show the epic roster
  move                move a work item to a different epic
  update              refresh the installed binary link and skill bundle

Namespaces:
  tusker workflow --help
  tusker vault --help
  tusker daemon --help
  tusker projects --help
  tusker runs --help
  tusker review --help
  tusker retry --help
  tusker docs --help
  tusker install --help
  tusker update --help
  tusker init --help

Orchestration quick start:
  tusker workflow init --vault ./tusker
  tusker projects add --repo . --vault ./tusker
  tusker projects limits --max-active-runs 1
  tusker daemon limits --max-active-runs 2
  tusker daemon run --once
  tusker runs --json

Tracker-only quick stop:
  tusker projects disable

Global flags:
  --json           Emit machine-readable output on success and error.`)
}

func printCommandHelp(command string) bool {
	switch command {
	case "init":
		printInitHelp()
	case "bootstrap":
		printBootstrapHelp()
	case "new-epic":
		printNewEpicHelp()
	case "new-story":
		printNewStoryHelp()
	case "new-bug":
		printNewBugHelp()
	case "new-doc":
		printNewDocHelp()
	case "handoff":
		printHandoffHelp()
	case "set-status":
		printSetStatusHelp()
	case "list":
		printListHelp()
	case "validate":
		printValidateHelp()
	case "reindex":
		printReindexHelp()
	case "move":
		printMoveHelp()
	case "sync-repo-contract":
		printSyncRepoContractHelp()
	case "workflow", "workflow init":
		printWorkflowHelp()
	case "vault", "vault set", "vault status", "vault move", "vault repair", "vault mount", "vault unmount", "mount", "unmount":
		printVaultHelp()
	case "daemon", "daemon run", "daemon status", "daemon limits":
		printDaemonHelp()
	case "projects", "projects add", "projects list", "projects limits", "projects enable", "projects disable", "projects remove":
		printProjectsHelp()
	case "runs", "runs inspect", "runs logs", "runs events", "runs interrupt":
		printRunsHelp()
	case "review", "review verify", "review approve", "review request-changes", "review comment":
		printReviewHelp()
	case "retry", "retry now":
		printRetryHelp()
	case "docs", "docs init", "docs export", "docs dev", "docs build":
		printDocsHelp()
	case "install":
		printInstallHelp()
	case "update":
		printUpdateHelp()
	default:
		return false
	}
	return true
}

func printInitHelp() {
	fmt.Println(`Usage:
  tusker init [--vault <path>] [--yes] [--fresh] [--daemon] [--mount|--no-mount] [--mount-name <name>]

What it does:
  1. bootstraps a fresh vault if needed
  2. writes WORKFLOW.md
  3. injects Tusker pointers into AGENTS.md / CLAUDE.md
  4. installs repo-contract helper docs
  5. reindexes the vault
  6. optionally mounts the repo tracker into your configured Obsidian vault
  7. optionally registers the repo with the daemon

Flags:
  --vault <path>    target vault path (default: ./tusker)
  --yes             accept defaults without prompts
  --fresh           move an existing target vault aside and recreate it cleanly
  --daemon          also register this repo with the daemon
  --mount           symlink this repo tracker into the configured Obsidian vault
  --no-mount        skip the Obsidian mount prompt
  --mount-name      folder name to show at the Obsidian vault root

Examples:
  tusker init --yes
  tusker init --fresh --yes
  tusker init --yes --mount
  tusker init --yes --daemon`)
}

func printWorkflowHelp() {
	fmt.Println(`Usage:
  tusker workflow init [--vault <path>]

Purpose:
  WORKFLOW.md is the daemon contract. It defines active states, polling,
  retry behavior, workspace strategy, and runner commands.

Commands:
  workflow init     write the default WORKFLOW.md if it does not exist

Examples:
  tusker workflow init --vault ./tusker
  tusker workflow init`)
}

func printVaultHelp() {
	fmt.Println(`Usage:
  tusker vault set --path <obsidian-vault>
  tusker vault status [--json]
  tusker vault mount [--repo <path>] [--vault <path>] [--name <folder>] [--force] [--json]
  tusker vault unmount [--repo <path> | --name <folder>] [--json]
  tusker vault move --to <obsidian-vault> [--json]
  tusker vault repair [--json]

Aliases:
  tusker mount     same as tusker vault mount
  tusker unmount   same as tusker vault unmount

Purpose:
  Keep each repo's ./tusker tracker git-committable while exposing many project
  trackers as root-level folders in one Obsidian vault.

Examples:
  tusker vault set --path "$HOME/iCloud Drive/Obsidian/Work"
  tusker init --yes --mount
  tusker vault mount --name client-a-mobile
  tusker vault repair`)
}

func printDaemonHelp() {
	fmt.Println(`Usage:
  tusker daemon run [--once]
  tusker daemon status [--json]
  tusker daemon limits [--max-active-runs <N>] [--json]
  tusker refresh [--json]

Purpose:
  The daemon watches registered projects, dispatches active work to runners,
  reconciles retries, and moves successful work into review. Limit changes are
  picked up on the next poll; no restart needed.

Examples:
  tusker daemon run --once
  tusker daemon status --json
  tusker daemon limits --max-active-runs 2
  tusker refresh`)
}

func printProjectsHelp() {
	fmt.Println(`Usage:
  tusker projects add --repo <path> [--vault <path>] [--json]
  tusker projects list [--json]
  tusker projects limits [--id <project-id> | --repo <path> | --vault <path>] [--max-active-runs <N>] [--json]
  tusker projects enable [--id <project-id> | --repo <path> | --vault <path>] [--json]
  tusker projects disable [--id <project-id> | --repo <path> | --vault <path>] [--json]
  tusker projects remove --id <project-id> [--json]

Purpose:
  Project registration tells the daemon which repos and vaults to monitor.
  Disable a project when you want tracker-only mode without removing it from
  the registry. Project limit changes hot-reload from WORKFLOW.md on the next
  poll.

Examples:
  tusker projects limits --max-active-runs 1
  tusker projects disable
  tusker projects disable --repo .
  tusker projects enable --id 01ABC...`)
}

func printRunsHelp() {
	fmt.Println(`Usage:
  tusker runs [--json]
  tusker runs inspect --id <ID> [--json]
  tusker runs logs --id <ID> [--follow] [--lines <N>] [--json]
  tusker runs events --id <ID> [--follow] [--lines <N>] [--json]
  tusker runs interrupt --id <ID> [--json]

Purpose:
  Inspect active and historical daemon work, logs, sessions, and interruptions.`)
}

func printReviewHelp() {
	fmt.Println(`Usage:
  tusker review verify --vault <path> --id <ID> --by <name> [--summary "..."]
  tusker review approve --vault <path> --id <ID> --by <human> [--summary "..."]
  tusker review request-changes --vault <path> --id <ID> --by <human> [--summary "..."]
  tusker review comment --vault <path> --id <ID> --by <human> [--summary "..."]

Purpose:
  Verification checks that the worker's claims match the current tree.
  Human review then decides approve vs changes requested.`)
}

func printRetryHelp() {
	fmt.Println(`Usage:
  tusker retry now --id <ID>

Purpose:
  Force a known run back into the retry queue immediately instead of waiting
  for its backoff timer.`)
}

func printDocsHelp() {
	fmt.Println(`Usage:
  tusker docs init [--site <path>] [--force]
  tusker docs export [--vault <path>] [--site <path>] [--clean] [--public-only] [--json]
  tusker docs dev [--vault <path>] [--site <path>] [--watch] [--port <n>] [--host <host>]
  tusker docs build [--vault <path>] [--site <path>] [--public-only] [--json]

Purpose:
  Export published Tusker docs and explicitly registered repo docs into the
  Astro/Starlight site, then preview or build the static docs output.

Notes:
  The site lives under ./site by default. Repo-doc publishing is configured
  through docs/publication.yaml. Vault docs are included only when a vault is
  discoverable or --vault is provided.
  --watch re-exports when vault markdown, attachments, or registered repo docs
  change while Astro serves the generated tree.
  Export emits llms.txt, content-manifest.json, and canon-manifest.json so
  agents can tell published canon from stale source files.`)
}

func printBootstrapHelp() {
	fmt.Println(`Usage:
  tusker bootstrap [--vault <path>] [--epic <ACR> --title <title>]

Purpose:
  Create a fresh Tusker vault skeleton without touching repo wiring.

Examples:
  tusker bootstrap --vault ./tusker
  tusker bootstrap --vault ./tusker --epic APP --title "App foundation"`)
}

func printNewEpicHelp() {
	fmt.Println(`Usage:
  tusker new-epic [--vault <path>] --acronym <ACR> --title <title> [--summary <text>] [--owner <name>] [--spec-source <path>]

Purpose:
  Create a new epic directory and epic index note.`)
}

func printNewStoryHelp() {
	fmt.Println(`Usage:
  tusker new-story [--vault <path>] --epic <ACR> --title <title> --size s|m|l|xl --risk low|medium|high|critical
                   [--priority p0|p1|p2|p3|icebox] [--assignee <name>] [--requester <name>]
                   [--delegation execute|explore|escalate] [--change-type <type>]
                   [--surfaces <csv>] [--ai-assistance none|light|moderate|heavy]
                   [--ai-tools <csv>] [--ai-session-log <path>] [--due <date>]
                   [--related <links>] [--blocks <links>] [--blocked-by <links>] [--tags <csv>]

Formal intake flags:
  --priority p0|p1|p2|p3|icebox
  --assignee <name>
  --requester <name>
  --delegation execute|explore|escalate
  --change-type feature|refactor|migration|security|docs|chore|research|incident|bug
  --surfaces <csv>
  --ai-assistance none|light|moderate|heavy
  --ai-tools <csv>
  --ai-session-log <path>
  --due <date>
  --related <links>
  --blocks <links>
  --blocked-by <links>
  --tags <csv>

Purpose:
  Create a story note under an existing epic.

Examples:
  tusker new-story --vault ./tusker --epic APP --title "Add login" --size m --risk medium
  tusker new-story --vault ./tusker --epic APP --title "Harden auth" --size s --risk high --priority p1 --assignee codex --requester sarav --change-type security --surfaces api,auth
  tusker new-story --vault ./tusker --epic APP --title "Wire checkout" --size l --risk medium --blocks APP-S-0003 --tags payments,ui --due 2026-05-15`)
}

func printNewBugHelp() {
	fmt.Println(`Usage:
  tusker new-bug [--vault <path>] --epic <ACR> --title <title> --size s|m|l|xl --risk low|medium|high|critical

Purpose:
  Create a bug note under an existing epic.`)
}

func printNewDocHelp() {
	fmt.Println(`Usage:
  tusker new-doc [--vault <path>] --epic <ACR> --title <title>
                 [--audience <developer|user|release|support|internal>]
                 [--canon-for <EPIC> | --companion-to <STORY-ID>]
                 [--status draft|review|approved|published|archived]
                 [--publish true|false] [--publish-path <route>]
                 [--publish-description <text>] [--publish-order <n>]
                 [--publish-section-title <text>] [--publish-url <url>]
                 [--tags <csv>]

Purpose:
  Create a companion or canonical doc note under an existing epic.

Notes:
  Developer docs must declare intent with --canon-for or --companion-to.
  --publish true requires --status approved|published, --publish-path, and
  --publish-description.`)
}

func printHandoffHelp() {
	fmt.Println(`Usage:
  tusker handoff [--vault <path>] --id <ID> --for worker|verifier|reviewer [--json]

Purpose:
  Render a role-specific packet from the ticket so you can hand work to a
  coding agent, verifier, or human reviewer without making them rediscover
  the whole note from scratch.`)
}

func printSetStatusHelp() {
	fmt.Println(`Usage:
  tusker set-status [--vault <path>] --id <ID> --status <status> [--actor <name>] [--reason <text>]

Purpose:
  Update the durable workflow status on a note.

Examples:
  tusker set-status --vault ./tusker --id APP-S-0001 --status active --actor sarav
  tusker set-status --vault ./tusker --id APP-S-0001 --status done --actor sarav`)
}

func printListHelp() {
	fmt.Println(`Usage:
  tusker list [--vault <path>] [--json] [--type epic|story|bug|doc] [--status <status>] [--review-state <state>] [--assignee <name>] [--epic <ACR>]

Purpose:
  Query notes and work items from the vault.

Examples:
  tusker list --vault ./tusker
  tusker list --vault ./tusker --type story --status active
  tusker list --vault ./tusker --review-state requested --json`)
}

func printValidateHelp() {
	fmt.Println(`Usage:
  tusker validate [--vault <path>] [--json]

Purpose:
  Check the vault against Tusker schema and workflow invariants.`)
}

func printReindexHelp() {
	fmt.Println(`Usage:
  tusker reindex [--vault <path>] [--json] [--fix-links]

Purpose:
  Rebuild generated indexes, dashboard JSON, and epic roster output.

Options:
  --fix-links  Refresh record-id mirror fields from human-authored wikilinks before rebuilding indexes.`)
}

func printMoveHelp() {
	fmt.Println(`Usage:
  tusker move [--vault <path>] --id <ID> --to-epic <ACR>

Purpose:
  Move a story, bug, or doc to a different epic and rewrite its derived id/path.`)
}

func printSyncRepoContractHelp() {
	fmt.Println(`Usage:
  tusker sync-repo-contract --repo <path> [--vault <path>] [--force]

Purpose:
  Write helper repo-contract docs and update AGENTS.md / CLAUDE.md Tusker pointers.`)
}
