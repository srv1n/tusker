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
		if len(argv) > 2 && !strings.HasPrefix(argv[2], "--") && commandTakesSubcommand(command) {
			command = command + " " + argv[2]
			return command, parseArgs(argv[3:])
		}
	}
	return command, parseArgs(argv[2:])
}

func commandTakesSubcommand(command string) bool {
	switch command {
	case "docs", "new", "vault", "daemon", "projects", "runs":
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
		return 0, newV5Epic(args)
	case "new task":
		return 0, newV5Task(args, "feature")
	case "new bug":
		return 0, newV5Task(args, "bug")
	case "new doc":
		return 0, newV5Doc(args)
	case "status":
		return 0, statusCmd(args)
	case "next":
		return 0, nextCmd(args)
	case "claim":
		return 0, claimCmd(args)
	case "evidence":
		return 0, evidenceCmd(args)
	case "verify":
		return 0, verifyCmd(args)
	case "close":
		return 0, closeV5Cmd(args)
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
		printDocsHelp()
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
	case "refresh":
		return 0, refreshCmd(args)
	case "update":
		return 0, updateCmd(args)
	case "init":
		return 0, initCmd(args)
	case "help new", "help new epic", "help new task", "help new bug", "help new doc":
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
	case "help validate":
		printValidateHelp()
		return 0, nil
	case "help reindex":
		printReindexHelp()
		return 0, nil
	case "help docs", "help docs init", "help docs model", "help docs map", "help docs catalog", "help docs freshness", "help docs check", "help docs apply", "help docs noop", "help docs waive", "help docs export", "help docs dev", "help docs build":
		printDocsHelp()
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
	fmt.Println(`Tusker - V5 markdown task and docs tracking

Vault discovery: if [--vault] is omitted, tusker walks up from the current
working directory looking for a folder named tusker/ (or a vault-shaped dir
with WORKFLOW.md or _system/config.yaml). Pass --vault <path> only to override.

Start here:
  tusker init --yes
  tusker new epic --vault ./tusker --acronym APP --title "App foundation"
  tusker new task --vault ./tusker --epic APP --title "Implement auth" --size m --risk medium

Commands:
  init                initialize or refresh a repo vault
  new                 create epic, task, bug task, or doc nodes
  list                list V5 epics, tasks, and docs
  search              search tracker notes without generated files or attachments
  show                show a bounded note capsule or selected section
  compact             remove empty optional metadata and disposable note scaffolding
  status              move a V5 task or epic through its workflow
  next                show the next pickable task
  claim               assign a ready/rework task and move it active
  evidence            attach proof to a V5 task
  docs                check, apply, waive, export, build, or preview docs
  vault               symlink repo trackers into a shared Obsidian vault
  daemon              operator loop for registered local projects
  projects            register repositories for daemon pickup
  runs                inspect, tail, and interrupt daemon runs
  refresh             run one daemon poll tick
  verify              record task verification
  close               close a verified task after docs impact is resolved
  validate            check vault invariants
  reindex             rebuild generated indexes
  update              refresh the installed binary link and skill bundle

Help:
  tusker new --help
  tusker docs --help
  tusker vault --help
  tusker daemon --help
  tusker runs --help
  tusker search --help
  tusker show --help
  tusker compact --help
  tusker status --help

Global flags:
  --json           Emit machine-readable output on success and error.`)
}

func printCommandHelp(command string) bool {
	switch command {
	case "init":
		printInitHelp()
	case "new", "new epic", "new task", "new bug", "new doc":
		printNewHelp()
	case "status":
		printStatusHelp()
	case "next":
		printNextHelp()
	case "claim":
		printClaimHelp()
	case "evidence":
		printEvidenceHelp()
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
	case "validate":
		printValidateHelp()
	case "reindex":
		printReindexHelp()
	case "docs", "docs init", "docs model", "docs map", "docs catalog", "docs freshness", "docs check", "docs apply", "docs noop", "docs waive", "docs export", "docs dev", "docs build":
		printDocsHelp()
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
	case "update":
		printUpdateHelp()
	default:
		return false
	}
	return true
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
  tusker init --migrate-v5 [--vault <path>] [--yes] [--vault-only]

What it does:
  1. initializes a fresh vault if needed
  2. writes WORKFLOW.md
  3. injects Tusker pointers into AGENTS.md / CLAUDE.md
  4. installs repo-contract helper docs
  5. reindexes the vault

With --migrate-v5 it also converts legacy story/bug notes into V5 tasks,
renames epic index files to V5 paths, adds schemas, refreshes templates/views,
and creates a side-by-side backup before writing.

Flags:
  --vault <path>    target vault path (default: ./tusker)
  --yes             accept defaults without prompts
  --fresh           move an existing target vault aside and recreate it cleanly
  --migrate-v5      repair an existing legacy vault in place
  --dry-run         show the migration plan without writing
  --vault-only      update only the vault; skip AGENTS/CLAUDE and repo-contract files
  --no-backup       skip the migration backup
  --no-pointers     skip AGENTS.md / CLAUDE.md pointer injection
  --no-contract     skip repo-contract helper docs

Examples:
  tusker init --yes
  tusker init --migrate-v5 --yes --vault-only
  tusker init --migrate-v5 --dry-run --vault ./tusker`)
}

func printDocsHelp() {
	fmt.Println(`Usage:
  tusker docs init [--site <path>] [--force]
  tusker docs model [--json]
  tusker docs map [<doc-node>] [--vault <path>] [--json]
  tusker docs catalog [--vault <path>] [--json]
  tusker docs freshness [--vault <path>] [--stale] [--json]
  tusker docs check <id> [--vault <path>] [--json]
  tusker docs apply <id> --node <doc-node> [--by <name>] [--reason <text>]
  tusker docs noop <id> --node <doc-node> [--by <name>] [--reason <text>]
  tusker docs waive <id> <doc-node> [--by <name>] --reason <text>
  tusker docs export [--vault <path>] [--site <path>] [--clean] [--public-only] [--json]
  tusker docs dev [--vault <path>] [--site <path>] [--watch] [--port <n>] [--host <host>]
  tusker docs build [--vault <path>] [--site <path>] [--public-only] [--json]

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
  tusker new task [--vault <path>] --epic <ACR> --title <title> [--kind <type>] [--status draft|backlog|ready|blocked] [--priority p0|p1|p2|p3] [--size s|m|l|xl] [--risk low|medium|high|critical]
  tusker new bug  [--vault <path>] --epic <ACR> --title <title> [--status draft|backlog|ready|blocked] [--priority p0|p1|p2|p3] [--size s|m|l|xl] [--risk low|medium|high|critical]
  tusker new doc  [--vault <path>] --node <route> --title <title> [--publish-lane <lane>] [--no-publish]

Purpose:
  Create V5 epics, tasks, bug tasks, and doc nodes.

Examples:
  tusker new epic --vault ./tusker --acronym APP --title "App foundation"
  tusker new task --vault ./tusker --epic APP --title "Implement auth" --kind feature --risk medium --size m
  tusker new bug --vault ./tusker --epic APP --title "Fix login redirect" --risk high
  tusker new doc --vault ./tusker --node developer/auth --title "Auth guide" --publish-lane developer`)
}

func printStatusHelp() {
	fmt.Println(`Usage:
  tusker status <id> <status> [--vault <path>] [--actor <name>] [--reason <text>]
  tusker status --id <id> --status <status> [--vault <path>] [--actor <name>] [--reason <text>]

Statuses:
  draft, backlog, ready, active, blocked, review, rework, done, cancelled

Purpose:
  Move a V5 task or epic through its durable workflow.

Notes:
  blocked requires either --blocked-by <TASK-ID[,TASK-ID]> or --block-reason <text>. Use
  backlog for shaped future work that should not be picked up in the current release.`)
}

func printNextHelp() {
	fmt.Println(`Usage:
  tusker next [--vault <path>] [--epic <ACR>] [--owner <name>] [--json]
  tusker next --claim --as <agent-or-person> [--vault <path>] [--epic <ACR>] [--json]

Purpose:
  Return the next pickable task. Pickable means status ready or rework, no
  unresolved blockers, and not already assigned to another owner.

Ranking:
  priority first (p0 before p1), then risk (critical before high), then oldest
  created date. --claim uses the same rules, then moves the selected task to active.`)
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

Purpose:
  Attach proof to a V5 task before verification and close.`)
}

func printVerifyHelp() {
	fmt.Println(`Usage:
  tusker verify <id> [--vault <path>] [--by <name>] [--summary <text>]
  tusker verify --id <id> [--vault <path>] [--by <name>] [--summary <text>]

Purpose:
  Record verification on a V5 task in review status.`)
}

func printCloseHelp() {
	fmt.Println(`Usage:
  tusker close <id> [--vault <path>] [--by <name>] [--reason <text>]
  tusker close --id <id> [--vault <path>] [--by <name>] [--reason <text>]

Purpose:
  Close a verified V5 task after required docs impact is applied or waived.`)
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
  tusker compact --all [--vault <path>] [--write] [--json]

Purpose:
  Dry-run or apply safe note compaction: remove empty optional frontmatter and
  disposable placeholder sections such as empty Execution plan and Work log.
  Substantive sections are preserved.

Examples:
  tusker compact ORC-T-0019
  tusker compact ORC-T-0019 --write
  tusker compact --all --json`)
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
