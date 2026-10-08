package main

import (
	"fmt"
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

// parseCLI joins the first two words into one command when the command table
// has that subcommand; otherwise the second word stays a positional argument.
func parseCLI(argv []string) (string, Args) {
	if len(argv) < 2 {
		return "", parseArgs(nil)
	}
	command, rest := argv[1], argv[2:]
	if len(rest) > 0 && !isCLIFlag(rest[0]) && lookupCommand(command+" "+rest[0]) != nil {
		command, rest = command+" "+rest[0], rest[1:]
	}
	return command, parseArgs(rest)
}

func isCLIFlag(value string) bool {
	return strings.HasPrefix(value, "-")
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
		value := argv[i+1]
		if (key == "task" || key == "waive") && args[key] != "" {
			args[key] += "\n" + value
		} else {
			args[key] = value
		}
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

func notifyDaemonForVault(args Args) {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return
	}
	notifyDaemonForVaultPath(vaultPath)
}

// printWaveCreateHelp documents the wave authoring request schema.
func printWaveCreateHelp() {
	fmt.Println(`Usage:
  tusker wave create "<title>" <TASK-ID>...
  tusker wave create --file <path|-> --request-key <stable-key> [--json]

Purpose:
  Create a named, task-backed delivery wave. The --file mode atomically
  authors a complete tusker.wave-authoring/v1 request (temporary task keys,
  bodies, dependencies, and human actions) into durable TSK or epic task
  records, gates, and an inert wave. An identical request_key and
  fingerprint returns the existing records; a changed request under the same
  key conflicts. Nothing is claimed or dispatched.

Example request (tusker.wave-authoring/v1):
  schema: tusker.wave-authoring/v1      # required, exactly this value
  request_key: auth-wave-v1             # required (or pass --request-key), string
  title: Auth hardening                 # required, string
  outcome: Sessions expire server-side. # required, string
  concurrency: 2                        # optional, int
  spec_refs: [docs/system/auth.md]      # optional, list of governing spec/proposal/decision refs
  shared_context: Shared notes.         # optional, string (lives once in the wave brief)
  tasks:                                # required, at least one
    - key: expiry                       # required, unique temporary key
      title: Enforce session expiry     # required, string
      work_level: standard              # required, light|standard|demanding
      review_level: demanding           # optional, light|standard|demanding
      review_reason: touches auth       # optional, required when review_level differs from work_level
      epic: AUT                         # optional, existing three-letter epic (default TSK)
      spec_refs: [docs/system/auth.md]  # optional, list
      dependencies:                     # optional, list
        - task: other-key               # required, key of another task in this request
          kind: hard                    # optional, hard|soft (default hard)
      owned_paths: [internal/auth/]     # optional, list of paths
      generated_outputs: []             # optional, list of paths
      implementation_notes: Notes.      # optional, string
      body: |                           # required, non-empty task markdown
        ## Acceptance
        | ID | Outcome | Proof |
        | --- | --- | --- |
        | A1 | Expired sessions are rejected. | Verification A1 |
        ## Verification
        | Covers | Check | Result | Notes |
        | --- | --- | --- | --- |
        | A1 | command: go test ./internal/auth -count=1 | pending | |
  human_actions:                        # optional, list
    - key: rotate-keys                  # required, unique key
      task: expiry                      # required, task key
      owner: human:alice                # required, human:<name>
      action: rotate the signing key    # required, string
      verification: new key is live     # required, string
      why_agent_cannot: needs prod KMS  # required, string
      covers: [A1]                      # optional, acceptance ids of that task

  Verification Check cells must start with command:, manual proof:,
  ledger:, or proof: (manual: is not accepted). Acceptance ids covered by
  a human action's covers also count as planned proof. After create,
  contracts that wave review --check would refuse are printed as warnings.`)
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
  tusker automation on|off [--json]
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
  - status summarizes the global switch, registered projects, and runtime run counts
  - off stops dispatch in every project and interrupts running workers (owner-only); on resumes dispatch
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

func printProjectsHelp() {
	fmt.Println(`Usage:
  tusker projects add [--repo <path>] [--vault <path>] [--json]
  tusker projects list [--json]
  tusker projects limits [--id <project-id>|--repo <path>|--vault <path>] [--max-active-runs <n>] [--json]
  tusker projects enable [--id <project-id>|--repo <path>|--vault <path>] [--dry-run] [--json]
  tusker projects disable [--id <project-id>|--repo <path>|--vault <path>] [--json]
  tusker projects automation-scope [--id <project-id>|--repo <path>|--vault <path>] [--json]
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
  - enable --dry-run and automation-scope preview the exact resume scope
    (armed waves, eligible tasks, active directives, excluded waves) and the
    toggle audit trail without arming, enabling, or mutating anything
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
  tusker config resolve automation.concurrency.weight_budget
  tusker config resolve automation.concurrency.max_load_per_cpu
  tusker config resolve automation.profiles --json`)
}

func printRunsHelp() {
	fmt.Println(`Usage:
  tusker runs list [--project <id>] [--active] [--limit <n>] [--json]
  tusker runs inspect <task-id-or-record-id> [--json]
  tusker runs logs <task-id-or-record-id> [--lines <n>] [--follow] [--json]
  tusker runs events <task-id-or-record-id> [--lines <n>] [--follow] [--json]
  tusker runs interrupt <task-id-or-record-id> [--json]
  tusker runs say <task-id> (--message <text> | --message-file <path|->) --by <actor> [--key <idempotency-key>] [--json]
  tusker runs continue <task-id> [--message <text> | --message-file <path|->] --by <actor> [--json]
  tusker runs fresh <task-id> --by <actor> [--reason <text>] [--vault <path>|--project <id>] [--json]
  tusker runs stop <task-id> --by <actor> [--reason <text>] [--vault <path>|--project <id>] [--json]
  tusker runs pause <task-id> --by <actor> [--vault <path>|--project <id>] [--json]
  tusker runs release <task-id-or-record-id> [--json]
  tusker runs retire <task-id-or-record-id> --reason <text> [--by <actor>] [--force] [--json]
  tusker redrive <task-id-or-record-id> --reason <text> [--by <actor>] [--json]

Purpose:
  Inspect and control daemon runtime state for a task. These commands expose
  attempts, turns, sessions, event tails, logs, and interrupts without making
  runtime state part of task frontmatter. Stop, pause, and fresh share the
  Serve run-control transitions, including the durable operator intent;
  pause currently refuses because no runner negotiates a pause capability.
  Redrive resets the budget window for parked budget runs and queues them for
  the resident daemon. Retire is the terminal operator path for stale, broken
  runtime records that must stop tripping daemon invariant circuits.

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
  tusker work start <task-id> --by <agent> [--source codex|claude|tusker_cli] [--current-workspace] [--json]
  tusker work status <task-id> [--json]
  tusker work heartbeat <task-id> --by <agent> [--json]
  tusker work submit <task-id> --by <agent> --deliverable <summary> --verification <summary> --gate-verdicts <A1=pass> [--json]
  tusker work fail <task-id> --by <agent> --reason <text> [--json]
  tusker work release <task-id> --by <agent> --reason <text> [--json]
  tusker work review <task-id> --by reviewer:<name> [--source codex|claude|tusker_cli] [--json]
  tusker work readiness <task-id> [--lane execute|review] [--json]
  tusker work progress <task-id> [--json]
  tusker work wait <task-id> [--timeout 60] [--json]
  tusker work reconcile <task-id> --by <agent> [--json]
  tusker work retry <task-id> --by <agent> [--json]
  tusker work profile <task-id> [--lane execute|review] [--json]
  tusker work recover <task-id> --action rerun_checks --by human:<name>|operator:<name> --vault <path> [--json]
  tusker work recover <task-id> --action adopt_completed --by <actor> --vault <path> [--json]

Purpose:
  The canonical runtime ownership protocol for interactive tracked work.
  --current-workspace is an explicit Codex/Claude self-implementation claim;
  it validates the current repository and serializes ownership of that checkout.
  The default work start keeps its configured isolated-workspace behavior.
  It never enables automation, arms a wave, starts a daemon, or launches a
  worker.
  Supported lifecycle: ready -> work start -> work reconcile ->
  work submit -> work review -> review submit -> close.
  tusker finish <TASK-ID> --request-review is the shared-checkout handback.`)
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
  Operate domain knowledge routed to docs/system/domains/<domain>/00-index.md.
  Legacy .tusker/knowledge/domains/**/INDEX.md and CANON.md records remain as
  compatibility pointers; reads, packets, and skill routes prefer the portable
  index when present.

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
  tusker skill sync [--repo <path>] [--mode symlink|copy] [--source <checkout>] [--json]
  tusker skill bundle [--repo <path>] [--out <path>] [--dereference-symlinks] [--json]

Purpose:
  Validate and route project skills. skill doctor checks the repo skill
  package, domain routes, forbidden source-truth paths, local absolute paths,
  and task-domain coverage. skill route returns the
  smallest ordered read set for an intent. skill sync refreshes repo-local
  generated skill installs, defaulting to symlink mode. Pass --source when
  running from outside the Tusker checkout. skill bundle creates a portable
  materialized copy for cloud/handoff contexts. audit-agent-guidance finds
  non-managed AGENTS.md/CLAUDE.md guidance, detects stale/missing Tusker
  bootstrap blocks, and --write refreshes managed bootstrap guidance.

Examples:
  tusker skill doctor --strict --json
  tusker skill audit-agent-guidance --write
  tusker skill route "change provider auth refresh logic" --json
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
  5. registers the project for Tusker app discovery without enabling automation
  6. optionally injects pointers, installs repo-contract files, and mounts the
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
  --no-register     skip Tusker app project discovery

Examples:
  tusker init --yes
  tusker init --yes --with-pointers --with-contract
  tusker init --yes --fresh --purge-state`)
}

func printDocsHelp() {
	fmt.Println(`Documentation graph commands:
  tusker docs find <query>
  tusker docs browse [<managed-directory>] [--limit <n>] [--json]
  tusker docs read <subject-or-path> [--section <heading>] [--current] [--json]
  tusker docs backlinks <subject-or-path> [--limit <n>] [--json]
  tusker docs check [<subject-or-path>] [--json]
  tusker docs touch --since <ref> [--waive <subject>]... [--json]
  tusker docs new <subject> --read-when <line> --skip-when <line> [--kind doc|proposal|decision|spec] [--domain <name>] [--index] [--decides-for <subject>] [--supersedes <subject>]
  tusker docs new <subject> --read-when <line> --skip-when <line> [--kind doc|proposal|decision|spec] --print [--json]
  tusker docs map
  tusker docs status [--stale [--strict]] [--json]
  tusker docs verify <subject>
  tusker docs adopt [--dry-run] [--json]
  tusker docs adopt --migration [--dry-run] [--json]
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
present in the approved table; no disposition deletes a file. --migration
extends the preview to legacy .tusker/specs, decisions, and knowledge domains
with explicit destinations, kinds, and lifecycle dispositions, plus each
source's relative links/assets and active task references. Legacy vault
sources with stub_source become subject-less forwarding stubs once their
content has a canonical owner; every mutated path's original bytes are kept
in the .tusker/_generated/docs recovery journal, so an interrupted apply
resumes and a repeated apply is idempotent. Dirty owned inputs, overlapping
active work, duplicate subjects, target collisions, path escapes, and symlink
escapes are refused with the exact conflicting paths before any damaging
write. Generated map artifacts are left untouched; run tusker docs map after
review. --apply and --yes are not accepted aliases.

Placement: current chapters live in docs/system (per domain under
docs/system/domains/<domain>), change specifications in docs/system/proposals,
and product decisions in docs/system/decisions. --kind spec stays accepted as
the spelling for proposal. Lifecycle and code conformance are independent:
lifecycle (current/proposed/accepted/implemented/superseded, kind-specific)
says where a document stands; code_conformance (unverified/matches/drift)
says whether the code was checked against it, and matches needs a
last_verified date, commit, and describes scope. Acceptance records intent,
not implementation proof.`)
}

func printNewHelp() {
	fmt.Println("Batch task options: --runner-profile <name> --resources <cargo:mac=1,box:host=1> --budget <2h> [--workspace-path <absolute-path>]")
	fmt.Println(`Usage:
  tusker new epic [--vault <path>] --acronym <ACR> --title <title> [--summary <text>] [--owner <name>] [--spec-refs <csv>]
  tusker new task [--vault <path>] --title <title> --work-level light|standard|demanding --body-file <path|-> [--epic <ACR>] [--status ready|backlog|review|rework] [--priority p0|p1|p2|p3] [--size s|m|l|xl] [--risk low|medium|high|critical] [--review-level light|standard|demanding] [--review-reason <reason>] [--execute-profile <name>] [--review-profile <name>] [--spec-refs <csv>] [--owned-paths <csv>] [--weight <1-8>] [--generated-outputs <csv>] [--evidence-required automated_test] [--architect <address>] [--origin <address>] [--peers <name=address,...>]
  tusker task update <TASK-ID> [--if-revision <state_rev>] [--body-file <path|->] [--title <title>] [--work-level <level>] [--review-level <level> --review-reason <reason>] [--spec-refs <csv>] [--dependencies <csv>] [--rebind-contract] [--rebind-dependency-contracts] [--owned-paths <csv>] [--weight <1-8>] [--generated-outputs <csv>] [--execute-profile <name>|--clear-execute-profile] [--review-profile <name>|--clear-review-profile] --by <actor> [--json]
  tusker new gate --blocks <TASK-ID> --kind <gate-kind> --owner <owner> --action <text> --verification <proof>
  tusker new decision --epic <ACR> --title <title>

Purpose:
  Create current work objects.

Notes:
  --architect names the address (operator, task:<ID> or execution:<ID>) that
  receives the wave's wave_result when the wave completes or stalls.
  Task IDs are allocated only after a successful create. For ordered batches,
  pass explicit --id values; a refused create does not reserve an ID.
  Without --epic a task is allocated in the standalone TSK-T-0001 namespace and
  no epic is recorded. --body-file - reads the exact task body from stdin.
  Every supplied spec_refs path and section anchor must resolve.
  --execute-profile/--review-profile pin one lane to a named runner profile
  from the global config; the name must resolve or the command is refused.

Examples:
  tusker new epic --vault ./.tusker --acronym APP --title "App foundation"
  tusker new task --vault ./.tusker --title "Implement auth" --work-level standard --body-file task-body.md \
    --spec-refs .tusker/specs/auth.md --owned-paths cmd/auth.go,internal/auth --generated-outputs internal/auth/openapi.gen.go
  tusker task update TSK-T-0001 [--if-revision sha256:<rev>] --title "Implement auth v2" --by agent:builder
  tusker new gate --vault ./.tusker --blocks APP-T-0001 --kind auth --owner human:sarav \
    --action "Provision staging OAuth credentials." \
    --verification "Provider ready check passes." \
    --why-agent-cannot "Human account access is required."`)
}

func printStatusHelp() {
	fmt.Println(`Usage:
  tusker status <id> <status> [--vault <path>] [--actor <name>] [--reason <text>]
  tusker status --id <id> --status <status> [--vault <path>] [--actor <name>] [--reason <text>]
  tusker status [--vault <path>]

  Bare tusker status (no id) prints a vault summary instead of changing
  a task.

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

func printReviewHelp() {
	fmt.Println(`Usage:
  tusker review submit <TASK-ID> --attempt <ATTEMPT-ID> --task-rev <REV> --source-sha <SHA> --work-rev <N> --proof-fingerprint <FP> --gate-fingerprint <FP> --material-fingerprint <FP> --verdict pass|changes_requested|blocked --covers <A1,A2> --summary "<summary>"

Structured findings:
  ` + reviewerFindingFlagExample("<material-fingerprint>") + `

Verified closures on a later pass:
  ` + reviewerClosureFlagExample("<repaired-material-fingerprint>") + `

Use one JSON array for all findings or closures. Never repeat --finding or
--closure; the array preserves every record. Blocking findings require stable
id, acceptance, evidence, consequence, closure_condition, repair_scope, and
exact material. Use repair_scope=material for source/implementation/artifact
repair and repair_scope=proof when only verification or the task ledger must
change; omitted scope is material. A later independent review must bind the
exact current material; same material is valid only for proof-scoped findings.`)
}

func printVerifyHelp() {
	fmt.Println(`Usage:
  tusker verify add <task-id> --covers A1,A2 --check <command-or-manual-check> --result pending|blocked|skipped|waived [--note <text>]
  tusker verify add <task-id> --rows "A1|command: go test ./pkg|pending|note"
  tusker verify add <task-id> --batch-file <path>
  tusker verify remove <task-id> --index <one-based-row>
  tusker verify recipe <task-id> [--files <path[,path...]>]
  tusker verify <id> [--vault <path>] [--by <name>] [--summary <text>]
  tusker verify --id <id> [--vault <path>] [--by <name>] [--summary <text>]

Purpose:
  Add inline verification rows or suggest scoped verification recipes. Command
  rows start pending; the verification executor records their pass or fail.

Options:
  --blocked-by <path|task|owner>  Required with --result blocked; attributes external/shared blockers.
  --rows <rows>                   Newline rows shaped covers|check|result|note|blocked_by.
  --batch-file <path>             Read rows from a file using the same format.`)
}

func printCloseHelp() {
	fmt.Println(`Usage:
  tusker close <id> --verdict accept|accept-with-followups|reject [--note <text>] [--override-audit <reason>]
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
