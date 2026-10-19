package main

import (
	"fmt"
	"strings"
)

// commandRows is the whole Tusker CLI. See cliCommand in commands.go. Rows are
// listed by group, in the order help prints them. Handlers reach back into the
// table (capabilities, install), so it is built in init, not as a var literal.
func commandRows() []cliCommand {
	return []cliCommand{
		{Name: "batch execute", Use: "Use only from the batch controller with inherited resource locks and a compiled launch.", Avoid: "Never invoke this internal helper by hand.", Group: "internal", Hidden: true, Summary: "Execute one compiled batch launch with inherited resource locks.", Run: run0(coordinatorBatchExecuteCmd)},
		{Name: "audit", Use: "Use when checking report coverage, commits, ownership, attribution, or repository audit hooks.", Avoid: "For human acceptance, review the changes and use close with a verdict.", Group: "maintain", Summary: "Check batch reports, commits, ownership, gates and repo audit hooks.", Usage: "tusker audit <task|batch> [--json]", Run: run0(coordinatorAuditCmd)},
		{Name: "ledger", Use: "Use when inspecting box costs or recording creation and deletion facts.", Avoid: "This command does not provision or delete cloud machines.", Group: "find", Summary: "Inspect box costs or record box creation and deletion.", Usage: "tusker ledger [--json]\ntusker ledger box add --id <id> --type <type> --eur-per-hour <rate> --expires <RFC3339> [--batch <batch>] [--epic <ACR>]\ntusker ledger box delete --id <id>", Run: run0(coordinatorLedgerCmd)},
		{Name: "triage", Use: "Use when previewing open tasks with staleness, dead references, or duplicate titles.", Avoid: "For applying reviewed actions, use triage apply.", Group: "maintain", Summary: "Preview stale open tasks; never discards automatically.", Usage: "tusker triage [--epic <ACR>] [--older-than 14d] [--output <new-path>] [--json]", Run: run0(coordinatorTriageCmd)},
		{Name: "triage apply", Use: "Use when a reviewed triage table should update tasks without deleting their history.", Avoid: "For automatic age-based deletion, no such action is provided.", Group: "maintain", Summary: "Apply a reviewed task/revision/action/reason JSON table.", Usage: "tusker triage apply <file> [--by <actor>] [--json]", Run: run0(coordinatorTriageApplyCmd), Mutates: true},

		// setup
		{Name: "init", Group: "setup",
			Summary: "Create or refresh the repo-local .tusker vault and register the project.",
			Use:     "Use when a repo has no .tusker vault yet, or a vault needs its workflow, config, starter SKILL.md and indexes rewritten. Run `tusker init --yes`.",
			Avoid:   "Not for wiping state and starting over — use `tusker reset --yes`. Not for installing agent skills — use `tusker install --repo .`.",
			Ref:     printInitHelp, Run: run0(initCmd)},
		{Name: "install", Group: "setup",
			Summary: "Install or refresh Tusker skills and pointers; --bin links the binary on PATH.",
			Use:     "Use when setting up skills (--codex-user, --claude-user, --repo .) or after rebuilding (--bin); --repo-only touches only the repo; --skills-only regenerates repo .agents/.claude packages without pointer edits; --mode copy|symlink.",
			Avoid:   "Not for a portable copy — use `tusker skill bundle`. Not for repo-contract files — use `tusker init --yes --with-contract`.",
			Ref:     printUpdateHelp, Run: run0(updateCmd)},
		{Name: "check", Group: "setup",
			Summary: "Run every health check: setup, mounts, vault, skills, docs; --fix repairs.",
			Use:     "Use when setup, the vault, skills or docs may be broken, before commit, or as a CI gate. --only setup,validate,skills,docs,acp narrows; --fix runs setup/mount repair, reconcile, reindex and docs map first; --since <ref> adds the docs touch rule; --strict fails on skill warnings and stale docs.",
			Avoid:   "Not for a stuck task or wave — use `tusker show <ID> --why`. Credentials and project routing stay manual.",
			Ref:     printCheckHelp, Run: checkCmd, Mutates: true},
		{Name: "config", Group: "setup",
			Summary: "Show a config key's effective value and source provenance.",
			Use:     "Use when you need to know which config layer sets a key: runner profiles, routing, denylist, concurrency limits.",
			Avoid:   "Not for model level lanes — use `tusker models`. Not for which profile a task gets — use `tusker show <TASK-ID> --route execute`.",
			Usage:   "tusker config <key> [--json]",
			Run:     run0(configResolveCmd)},
		{Name: "vault", Group: "setup",
			Summary: "Show, set or move the shared Obsidian vault, and mount or unmount repo trackers.",
			Use:     "Use when one Obsidian vault should show several repos' trackers. --path sets the vault, or moves the old folder there when the new path does not exist yet.",
			Avoid:   "Not for the repo's own .tusker vault — use `--vault <path>` or `tusker init`. Not for broken mounts — use `tusker check --fix`.",
			Usage:   "tusker vault [--json]\ntusker vault --path <obsidian-vault> [--force] [--json]\ntusker vault --mount [--repo <path>] [--vault <tracker>] [--name <folder>] [--force] [--json]\ntusker vault --unmount <folder> [--json]",
			Run:     run0(vaultCmd)},
		{Name: "version", Group: "setup",
			Summary: "Print the Tusker version, revision, Go version, and binary hash.",
			Use:     "Use when checking which tusker binary is on PATH, or (with --json) which commands and features it supports.",
			Avoid:   "Not for setup health — use `tusker check`.",
			Usage:   "tusker version [--json]   (--json includes the capability manifest under \"capabilities\")",
			Run:     run0(versionCmd)},
		{Name: "reset", Group: "setup",
			Summary: "Delete repo Tusker state, then initialize a clean vault (--no-init: delete only).",
			Use:     "Use when a repo's .tusker state is corrupt or obsolete. Default keeps docs/system and .tusker/specs and re-inits; --no-init removes all repo Tusker state (previews unless --yes). Preview with --dry-run.",
			Avoid:   "Not for machine cleanup — use `tusker uninstall`. Not for scratch — use `tusker gc`.",
			Ref:     printResetHelp, Run: run0(resetCmd)},
		{Name: "uninstall", Group: "setup",
			Summary: "Preview or remove machine-level Tusker install and config.",
			Use:     "Use when removing Tusker from this machine: PATH binary, user skills in ~/.agents ~/.codex ~/.claude, global config, launchd service, ACP adapters, logs. --state also deletes runtime history.",
			Avoid:   "Not for one repo's .tusker state — use `tusker purge --repo <path> --only-tusker-state`. Not for refreshing skills — use `tusker install`.",
			Run:     run0(tuskerGlobalUninstallCmd)},

		// find
		{Name: "digest", Group: "find",
			Summary: "Print operator digest: escalations, landings, parked runs, pending gates.",
			Use:     "Use when an operator wants what changed recently: open escalations, landed tasks, red-parked runs, pending hard gates, armed waves; default window is the last 24 hours.",
			Avoid:   "Not for one task's state — use `tusker brief <TASK-ID>`. Not for feedback notes — use `tusker feedback digest`. Note: it updates the digest watermark and stale escalation severity.",
			Usage:   "tusker digest [--since last|<RFC3339|YYYY-MM-DD> | --all] [--epic <ACR>] [--coordinator] [--json]",
			Run:     run0(digestCmd)},

		// author
		{Name: "evidence", Group: "author",
			Summary: "Record, promote from scratch, or audit durable proof artifacts.",
			Use:     "Use when proof is an artifact (test log, screenshot, CI run, trace) tied to acceptance items, when keeping a scratch file before close, or when evidence exceeds budget.",
			Avoid:   "Not for inline command or manual check rows — use `tusker edit <TASK-ID> --verify`. Not for coverage gaps — use `tusker show <TASK-ID> --proof`.",
			Usage:   "tusker evidence add <TASK-ID> --covers A1[,A2] [--kind <kind>] [--summary <text>] [--command <cmd>] [--result <text>] [--path <file>|--artifact-paths <csv>|--external-url <url>] [--link-only] [--status accepted|pending_review] [--evidence-id <ID>] [--accepted-by <actor>] [--checked-by <actor>] [--redacted --redaction-note <text>]\ntusker evidence promote <TASK-ID> --from .tusker/scratch/<TASK-ID>/<file> [--kind log_excerpt] --covers A1 [--force]\ntusker evidence prune <TASK-ID> [--json]   (dry-run classification only)\nKinds: automated_test unit_test integration_test e2e_test verification_summary screenshot video trace log_excerpt manual_smoke physical_smoke ci_run provider_probe benchmark review_packet security_review privacy_review accessibility_review performance_profile release_smoke human_review",
			Ref:     printEvidenceHelp, Run: run0(evidenceCmd), Mutates: true},

		// start

		// work
		{Name: "show", Group: "find",
			Summary: "Show any record by ID: task, wave, batch, execution, message, escalation, or doc.",
			Use:     "Use when you need one record. A task prints its capsule; flags pick a view: --packet, --why (diagnosis, readiness, dispatch plan; exit 0 healthy, 1 fault, 2 unavailable), --progress, --lease, --proof, --route, --run, --logs, --events, --traces, --closeout, --styled. A wave shows members, start readiness, and results.",
			Avoid:   "Not for many records; use `tusker list`. Not for vault-wide health; use `tusker check`.",
			Usage:   "tusker show <ID> [--capsule|--acceptance|--evidence|--verification [--lines <n>]|--full|--section <heading>] [--json]\ntusker show <TASK-ID> --styled [--plain] | --packet [worker|agent|reviewer|integrator|explainer] [--routing] [--write] | --why [--output <new-path>] | --progress | --lease | --proof [--verbose] | --route [execute|review] | --run | --logs [--follow] [--lines <n>] | --events [--follow] | --traces | --trace <TRACE-ID> | --closeout\ntusker show <W-ID> [--check] [--why] [--json]\ntusker show <B-…|exec_…|msg-…|ESC-…> [--json]\ntusker show <doc-subject|docs/path.md> [--section <heading>] [--backlinks] [--current]",
			Run:     showRun},
		{Name: "list", Group: "find",
			Summary: "List records of one kind, with filters, text search, and the next pickable task.",
			Use:     "Use when you need many records or want to search. Kinds: tasks, epics, waves, docs, runs, executions, messages, approvals, forks, flaky, projects, queue, escalations. --grep searches text; --next shows the top pickable task.",
			Avoid:   "Not for one record; use `tusker show <ID>`.",
			Usage:   "tusker list [tasks|epics|waves|docs|runs|executions|messages|approvals|forks|flaky|projects|queue|escalations] [--json]\ntusker list [<EPIC>] [--status s] [--epic ACR] [--wave W-0001] [--open|--closed|--all] [--ready|--running|--review|--mine] [--owner <who>] [--format table|ids] [--limit n]\ntusker list [tasks|epics] --grep <text>\ntusker list --next [--owner o] [--epic ACR] [--lane l] [--explain]\ntusker list docs [--grep <text>|--dir <path>|--stale [--strict]]\ntusker list runs [--active]\ntusker list executions [--unbound]\ntusker list messages [--for <addr>] [--pending]\ntusker list forks --epic <ACR>\ntusker list escalations [--open]",
			Run:     run0(listCmd)},
		{Name: "new", Group: "author",
			Summary: "Create a record: epic, task, gate, decision, wave, batch, fork, flaky, or doc.",
			Use:     "Use when authoring work or knowledge. The kind comes first; its flags follow. A task needs a body file with Acceptance and Verification tables.",
			Avoid:   "Not for changing an existing record; use `tusker edit <ID>`.",
			Usage:   "tusker new epic --acronym <ACR> --title <title> [--summary <text>] [--spec-refs <csv>]\ntusker new task --title <title> --work-level light|standard|demanding --body-file <path|-> [--epic <ACR>] [--owned-paths <csv>] [--spec-refs <csv>] [--priority p0-p3] [--risk low|medium|high|critical]\ntusker new gate --blocks <TASK-ID> --kind <kind> --owner <owner> --action <text> --verification <proof>\ntusker new decision --epic <ACR> --title <title>\ntusker new wave <TASK-ID>... [--outcome <text>] | --file <path>\ntusker new batch --tasks <ids> [--max-parallel N]\ntusker new fork --epic <ACR> --id <slug> --question <text> --ruling <text> --by <actor> [--supersedes <old-slug>]\ntusker new flaky <test> --reason <text> --until <date|task-id>\ntusker new doc <kind> ...",
			Ref:     printNewHelp, Run: run0(newCmd), Mutates: true},
		{Name: "edit", Group: "author",
			Summary: "Change a task, wave, or gate: contract fields, proof rows, membership, or status.",
			Use:     "Use when an existing record must change. A task takes contract flags, --verify rows, --verify-remove, --proof-mode, and --status. A wave takes --add, --remove, and --outcome. A gate takes --status satisfied|waived|obsolete.",
			Avoid:   "Not for finishing work; use `tusker done`. Not for cancelling; use `tusker stop <ID> --cancel`.",
			Usage:   "tusker edit <TASK-ID> [--title <t>] [--body-file <path|->] [--owned-paths <csv>] [--dependencies <csv>] [--work-level <l>] [--if-revision <rev>] --by <actor>\ntusker edit <TASK-ID> --verify \"A1|command: go test ./pkg|pending|note\" [--verify-remove <row>] [--proof-mode none|inline|card|artifact|audit]\ntusker edit <TASK-ID> --status idea|backlog|ready|review|rework|superseded [--reason <text>]\ntusker edit <W-ID> [--add <TASK-IDs>] [--remove <TASK-IDs>] [--outcome <text>]\ntusker edit <GATE-ID> --status satisfied --evidence <summary> | --status waived|obsolete --reason <text>",
			Ref:     printVerifyHelp, Run: run0(editCmd), Mutates: true},
		{Name: "start", Group: "work",
			Summary: "Start a task in your session, or queue a task, wave, or batch for agents.",
			Use:     "Use when work should begin. A task starts in your own session here (rerunning renews your lease); --background queues it for the daemon. A W- wave is authorized and queued, or resumed if paused. A B- batch runs in the foreground.",
			Avoid:   "Not for recording finished work; use `tusker done`. Not for a review; use `tusker review`.",
			Usage:   "tusker start <TASK-ID> --by <actor> [--current-workspace] [--json]\ntusker start <TASK-ID> --background [--by human:<name>]\ntusker start <W-ID> [--by human:<name>]\ntusker start <B-ID> [--max-parallel N]",
			Ref:     printWorkSessionHelp, Run: run0(startCmd), Mutates: true},
		{Name: "done", Group: "work",
			Summary: "Record finished work: Tusker reruns the proof, then moves tasks to review or done.",
			Use:     "Use whenever work is finished, however it was done: your own session, parallel agents, another harness, or hand edits. Select by IDs, --wave <W-ID>, or --since <git-ref> (open tasks whose owned paths changed). An agent lands tasks in review; a person closes them, including tasks already in review.",
			Avoid:   "Not for giving up; use `tusker stop`. Not for a reviewer verdict; use `tusker review`. Never claim a task afterwards just to record it.",
			Usage:   "tusker done <TASK-ID> [<TASK-ID> ...] --by <actor> [--summary <text>] [--json]\ntusker done --wave <W-ID> --by <actor>\ntusker done --since <git-ref> --by <actor>",
			Run:     run0(doneCmd), Mutates: true},
		{Name: "review", Group: "work",
			Summary: "Claim a task's review, or record the reviewer's verdict.",
			Use:     "Use when you are the task's reviewer. Without --verdict it claims the review session and prints the review packet. With --verdict it records pass, changes_requested, or blocked against the exact revision; batch tasks take accept, accept-with-followups, or reject.",
			Avoid:   "Not for the implementer's handback; use `tusker done`. Not for checking whether a wave can start; use `tusker show <W-ID>`.",
			Usage:   "tusker review <TASK-ID> --by reviewer:<name>\ntusker review <TASK-ID> --verdict pass|changes_requested|blocked --attempt <ATTEMPT-ID> --covers A1[,A2] --task-rev <rev> --source-sha <sha> --work-rev <n> [--summary <text>] [--finding <json>] [--blocker machine|infrastructure|human] [--by reviewer:<name>]\ntusker review <TASK-ID> --verdict accept|accept-with-followups|reject [--reason <text>]",
			Ref:     printReviewHelp, Run: run0(reviewCmd), Mutates: true},
		{Name: "stop", Group: "work",
			Summary: "Stop work without finishing: release, fail, or cancel a task; pause a wave; stop a batch.",
			Use:     "Use when work should end unfinished. On a task you hold it releases the session, or records failure with --failed; --cancel cancels the task for good; on a daemon run you do not own it stops the run. A W- wave stops admitting new work; a B- batch stops its workers (--drain lets them finish). --all stops the daemon and every process Tusker started (agent sessions and checks).",
			Avoid:   "Not for finished work; use `tusker done`.",
			Usage:   "tusker stop <TASK-ID> --by <actor> --reason <text> [--failed|--cancel [--dependents detach|discard]]\ntusker stop <W-ID> [--by human:<name>]\ntusker stop <B-ID> [--drain]\ntusker stop --all [--json]",
			Run:     run0(stopCmd), Mutates: true},
		{Name: "work wait", Group: "work",
			Summary: "Wait until a task's run releases, up to 600 seconds.",
			Use:     "Use when you must block until a task's current attempt ends (lease released). Exits nonzero on timeout and never reports a terminal state that did not happen.",
			Avoid:   "Not for a one-time status check; use `tusker work progress`. Not for streaming run output; use `tusker runs events --follow`.",
			Ref:     printWorkSessionHelp, Run: workLifecycle("wait")},
		{Name: "daemon", Group: "daemon",
			Summary: "Report daemon liveness and runs; --limit sets the global cap; --resume closes tripped circuits.",
			Use:     "Use when checking whether the resident daemon runs, changing the global active-run cap or disk-pressure guard, or reopening dispatch after fixing a tripped invariant/crash-loop circuit (owner only).",
			Avoid:   "Not for the automation switch; use `tusker automation`. Not for one project's cap; use `tusker projects set --limit`.",
			Usage:   "tusker daemon [status] [--json]\ntusker daemon status --limit <n> [--disk-pressure-enabled true|false] [--disk-pressure-min-free-bytes <n>] [--disk-pressure-min-free-percent <p>] [--json]\ntusker daemon status --resume [--json]",
			Run:     run0(daemonCmd)},
		{Name: "daemon run", Group: "daemon",
			Summary: "Run the resident daemon loop; --once does one poll without dispatching.",
			Use:     "Use when an operator starts the runtime by hand (launchd runs it otherwise), or with --once to check what would be picked up; --once works while a daemon is running.",
			Avoid:   "Refused from agent sessions (except --once). For a persistent service, use `tusker daemon install`.",
			Usage:   "tusker daemon run [--once [--quiet] [--json]]",
			Run:     run0(daemonRunRoute)},
		{Name: "daemon stop", Group: "daemon",
			Summary: "Ask the running daemon to exit; `--drain` waits for wrappers.",
			Use:     "Use when the resident daemon process must shut down. Detached run wrappers stay alive unless `--drain` waits for them.",
			Avoid:   "To halt dispatch but keep the daemon, use `tusker automation off`. To remove the launchd service, use `tusker daemon install --remove`.",
			Usage:   "tusker daemon stop [--drain] [--json]",
			Run:     run0(daemonStopCmd)},
		{Name: "daemon install", Group: "daemon",
			Summary: "Install and start the launchd daemon service (macOS); --remove uninstalls it.",
			Use:     "Use when the daemon should run persistently on macOS, or --remove to stop and delete the service. Refused from agent sessions.",
			Avoid:   "Not for a foreground loop; use `tusker daemon run`. Not for enabling a project; use `tusker projects set <id> --on`.",
			Usage:   "tusker daemon install [--allow-protected-projects] [--json]\ntusker daemon install --remove [--json]",
			Run:     run0(daemonInstallRoute)},
		{Name: "projects add", Group: "daemon",
			Summary: "Register a repo vault (disabled), or --rebind a moved project to a new path.",
			Use:     "Use when a repo with a .tusker vault should get a project ID, or a registered (disabled, idle) project's repo moved: --rebind <project-id> --repo --vault [--allow-dirty] [--dry-run].",
			Avoid:   "Not for turning on background work; use `tusker projects set <id> --on`.",
			Usage:   "tusker projects add [--repo <path>] [--vault <path>] [--json]\ntusker projects add --rebind <project-id> --repo <path> --vault <path> [--allow-dirty] [--dry-run] [--json]",
			Run:     run0(projectsAddRoute), Mutates: true},
		{Name: "projects set", Group: "daemon",
			Summary: "Turn a project's background work on/off or set its run cap; --dry-run previews.",
			Use:     "Use when one project should start or stop dispatch (audited; --off interrupts nothing) or cap its active runs. --dry-run shows the resume scope and toggle history and changes nothing.",
			Avoid:   "Not the global switch; use `tusker automation on|off`. Not for a running task; use `tusker runs stop`.",
			Usage:   "tusker projects set [<project-id>|--repo <path>|--vault <path>] --on|--off|--limit <n> [--dry-run] [--json]",
			Run:     run0(projectsSetCmd)},
		{Name: "projects remove", Group: "daemon",
			Summary: "Unregister a project and delete its runtime history; --missing for dead registrations.",
			Use:     "Use when a project ID should no longer be polled (deletes its runs, attempts, sessions; repo untouched), or --missing to preview (--apply removes) registrations whose directories are gone.",
			Avoid:   "To keep history, use `tusker projects set <id> --off`. For a moved repo, use `tusker projects add --rebind`.",
			Usage:   "tusker projects remove <project-id> [--json]\ntusker projects remove --missing [--apply] [--json]",
			Run:     run0(projectsRemoveRoute)},
		{Name: "automation", Group: "daemon",
			Summary: "Show or flip the global automation switch.",
			Use:     "Use when checking whether dispatch is on everywhere, or `on`/`off` (owner only): off stops dispatch in every project and interrupts running workers.",
			Avoid:   "Not for one project; use `tusker projects set`. Not for the daemon process; use `tusker daemon stop`.",
			Usage:   "tusker automation [status|on|off] [--json]",
			Run:     run0(automationCmd)},
		{Name: "models", Group: "models",
			Summary: "Show model levels mapped to execute and review profiles.",
			Use:     "Use when checking which profiles each level (light, standard, demanding) uses per lane, and the config revision.",
			Avoid:   "Not for installed harnesses; use `tusker runner catalog`.",
			Usage:   "tusker models [show] [--json] [--compact]",
			Run:     run0(modelsRoute)},
		{Name: "models set", Group: "models",
			Summary: "Set or --clear the ordered profile list for one level and lane.",
			Use:     "Use when changing which profiles run a level/lane for this project (default) or globally.",
			Avoid:   "Not for defining a profile; use `tusker models profile`.",
			Usage:   "tusker models set <level> <lane> <profile,...> [--scope global|project] [--if-revision <sha>] [--json]\ntusker models set <level> <lane> --clear [--scope global|project] [--json]",
			Run:     run0(modelsSetRoute)},
		{Name: "models profile", Group: "models",
			Summary: "Create, update, disable, enable, or remove a global runner profile; --defaults generates them.",
			Use:     "Use when defining a named profile (harness, model, effort, preset, tiers), changing its lifecycle, or previewing/--write generated defaults from installed harnesses.",
			Avoid:   "Not for assigning profiles to levels; use `tusker models set`.",
			Usage:   "tusker models profile <name> [--set] --harness <h> --model <id> --effort <e> [--preset <p>] [--display-name <n>] [--eligible-tiers <csv>] [--command <path>] [--if-revision <sha>] [--json]\ntusker models profile <name> --disable|--enable|--remove --if-revision <sha> [--json]\ntusker models profile --defaults [--bundled] [--write] [--json]",
			Run:     run0(modelsProfileRoute)},
		{Name: "runs say", Group: "runs",
			Summary: "Deliver an operator message into a run's agent session.",
			Use:     "Use when a daemon worker needs an instruction mid-run; delivered softly if supported, otherwise interrupt and resume.",
			Avoid:   "Not for a settled run; use `tusker runs restart --message`.",
			Usage:   "tusker runs say <task-id> (--message <text>|--message-file <path|->) --by <actor> [--key <k>] [--json]",
			Run:     runID(runsSayCmd)},
		{Name: "runs restart", Group: "runs",
			Summary: "Restart a settled run: same session, --fresh session, or --requeue with a budget reset.",
			Use:     "Use when a stopped/failed run should resume its native session (optionally --message), restart clean (--fresh), or a budget-parked/failed run should be requeued (--requeue).",
			Avoid:   "Not for a live run; use `tusker runs say` or `tusker runs stop --soft` first.",
			Usage:   "tusker runs restart <task-id> --by <actor> [--message <text>|--message-file <path|->] [--json]\ntusker runs restart <task-id> --fresh --by <actor> [--reason <text>] [--json]\ntusker runs restart <task-id> --requeue [--reason <text>] [--by <actor>] [--json]",
			Run:     runID(runsRestartCmd)},
		{Name: "runs stop", Group: "runs",
			Summary: "Stop a daemon run with recorded intent; --soft interrupts only the current turn.",
			Use:     "Use when a background run must end and stay stopped (same as Serve Stop), or --soft to interrupt now and keep the session for restart.",
			Avoid:   "Not for a whole wave; use `tusker stop <W-ID>`. Not for your own session; use `tusker stop`.",
			Usage:   "tusker runs stop <task-id> --by <actor> [--reason <text>] [--vault <path>|--project <id>] [--json]\ntusker runs stop <task-id> --soft [--json]",
			Run:     runID(runsStopRoute)},
		{Name: "runs retire", Group: "runs",
			Summary: "Retire a broken run record for good; --release frees a dead run's lease instead.",
			Use:     "Use when a run row keeps tripping daemon circuits (needs --reason, --force overrides a fresh heartbeat), or --release when its process is gone and it should run again.",
			Avoid:   "Afterwards, close tripped circuits with `tusker daemon status --resume`.",
			Usage:   "tusker runs retire <task-id> --reason <text> [--by <actor>] [--force] [--json]\ntusker runs retire <task-id> --release [--break-glass --by human:<n> --reason <text>] [--json]",
			Run:     runID(runsRetireRoute)},
		{Name: "execution register", Group: "runs",
			Summary: "Allocate a direct-execution ID, or attach/bind an existing one.",
			Use:     "Use when a direct Codex/Claude/cloud session should appear in the execution graph; later `register <ID> --attach` records its provider session; --bind <TASK> binds it; --contact-role makes it an architect/origin/peer contact.",
			Avoid:   "Does not claim a task; use `tusker start`.",
			Usage:   "tusker execution register [<EXECUTION-ID>] [--name <n>] [--source direct|direct_codex|direct_claude|codex_cloud] [--provider <p>] [--agent-type <t>] [--attach (--provider-session-id <id>|--cloud-task-id <id>) [--session-ref <r>]] [--bind <TASK-ID>] [--by <actor>] [--project <id>|--vault <path>] [--json]\ntusker execution register (--task <TASK-ID>|--wave <WAVE-ID>) --contact-role architect|origin|peer [--contact-name <n>] --harness codex|claude-code|devin --source direct_codex|direct_claude|direct_devin --provider <p> --conversation-id <id> [--connection-id <host>] --if-generation <n> --by human:<name>|operator:<name> [--json]",
			Run:     execution("register")},
		{Name: "execution bind", Group: "runs",
			Summary: "Bind an execution to a task's wave (rebinds if bound); --none detaches.",
			Use:     "Use when a registered direct execution belongs to a task, moved to another task, or no longer belongs to one.",
			Avoid:   "Does not claim the task; use `tusker start`.",
			Usage:   "tusker execution bind <EXECUTION-ID> <TASK-ID>|--task <TASK-ID> [--wave <WAVE-ID>] [--by <actor>] [--json]\ntusker execution bind <EXECUTION-ID> --none [--by <actor>] [--json]",
			Run:     execution("bind")},
		{Name: "message ask", Group: "work",
			Summary: "Ask a question, send a --notify notice, or raise a --severity escalation.",
			Use:     "Use when a worker needs a decision (--yield ends your turn until answered), any sender must notify an address (--notify), or a runner hits a system error/security concern/conflict/stuck loop (--severity P0-P2).",
			Avoid:   "Not for answering; use `tusker message reply`. Not for a daemon run's live session; use `tusker runs say`.",
			Usage:   "see `tusker help message`",
			Ref:     printMessageHelp, Run: run0(messageAskCmd), Mutates: true},
		{Name: "message mark", Group: "work",
			Summary: "Mark a message read (--consumed) or acted on (--applied); --applied acknowledges an ESC- escalation.",
			Use:     "Use when a recipient has read or carried out a message, or an operator has seen an escalation.",
			Avoid:   "Not for replying; use `tusker message reply`.",
			Usage:   "tusker message mark <message-id> --consumed|--applied --project <id>\ntusker message mark <ESC-ID> --applied [--by <actor>]",
			Ref:     printMessageHelp, Run: run0(messageMarkCmd), Mutates: true},
		{Name: "message reply", Group: "work",
			Summary: "Answer a message; it goes to the original sender by default.",
			Use:     "Use when responding to a message ID with `--reply-to`. The answer goes back to the asker and can wake a worker that yielded.",
			Avoid:   "Not for new threads; use `tusker message send` or `tusker message ask`. Not for tool-permission requests; use `tusker approvals respond`.",
			Ref:     printMessageHelp, Run: messageCmd("message reply"), Mutates: true},
		{Name: "message inbox", Group: "work", Hidden: true,
			Summary: "Print pending messages for a run, an address, or a hook session.",
			Use:     "Use when a worker or session needs its unread mail (`--run <attempt-id>` or `--for task:|execution:|operator`). `--format hook` reads harness hook JSON from stdin.",
			Avoid:   "Not for all messages; use `tusker message list`. Not for unbound executions; use `tusker execution inbox`. Not for tool approvals; use `tusker approvals list`.",
			Ref:     printMessageHelp, Run: run0(agentMessageInboxCmd)},

		// runs
		{Name: "execution cancel", Group: "runs",
			Summary: "Request cancellation of an execution if its provider supports it.",
			Use:     "Use when a direct execution should be cancelled and its provider has a proven cancel capability. Records settlement evidence; otherwise reports that cancel is unavailable.",
			Avoid:   "Not for daemon runs; use `tusker runs stop <task-id>`. Not for releasing a task lease; use `tusker work release`.",
			Usage:   "tusker execution cancel <EXECUTION-ID> [--request-key <key>] [--by <actor>]",
			Ref:     printExecutionHelp, Run: execution("cancel")},
		{Name: "approvals respond", Group: "runs",
			Summary: "Allow once or block a pending tool-access request.",
			Use:     "Use when a human owner decides a worker's tool-access request with `--allow-once` or `--block`. The actor must be human:<name>. Owner session only.",
			Avoid:   "Not for answering agent questions; use `tusker message reply`. Not for satisfying task gates; use `tusker gate`.",
			Run:     run0(approvalsRespondCmd)},

		// deliver
		{Name: "land", Group: "deliver",
			Summary: "Merge task branches into their wave branch, or a wave to main.",
			Use:     "Use when done tasks must merge into the wave integration branch, or every wave member is done and the wave should merge to the default branch; serialized by a landing lock.",
			Avoid:   "Not for closing a task — use `tusker close <TASK-ID>`. Not for tasks still in review — finish review and close them first.",
			Usage:   "tusker land <TASK-ID> [<TASK-ID>...] [--branch <branch>] [--from <ref>] [--trust-from] [--by <actor>] [--json]\ntusker land <WAVE-ID> [--json]   (requires every member task done)",
			Run:     run0(landV7Cmd)},
		{Name: "closeout", Group: "deliver",
			Summary: "Write or inspect a checkpoint when only human action remains.",
			Use:     "Use when machine work on a task is complete but it waits on a human gate or review: record a validation snapshot and reviewer packet, then stop.",
			Avoid:   "Not for closing the task — use `tusker close <TASK-ID>`. Not for a task summary — use `tusker brief <TASK-ID>`.",
			Usage:   "tusker closeout <TASK-ID> --validate <command> --emit-packet [--by <actor>] [--json]\ntusker closeout status <TASK-ID> [--json]",
			Run:     run0(closeoutV7Cmd)},

		// docs
		{Name: "docs verify", Group: "docs",
			Summary: "Stamp a doc's last_verified with today's date and commit.",
			Use:     "Use when you have checked a doc against the current code and want to record it as verified.",
			Avoid:   "Not for checking structure or links — use `tusker docs check`. Not for task verification — use `tusker verify` or `tusker evidence add`.",
			Ref:     printDocsHelp, Run: docs("verify")},
		{Name: "docs adopt", Group: "docs",
			Summary: "Preview or apply a reviewed table bringing docs under management.",
			Use:     "Use when moving unmanaged docs or legacy .tusker/specs, decisions, and knowledge domains (--migration) into docs/system. Preview, edit table, then --table --approve --by.",
			Avoid:   "Not for creating one new doc — use `tusker docs new`. Not for rebuilding the map afterwards — use `tusker docs map`.",
			Ref:     printDocsHelp, Run: docs("adopt")},
		{Name: "skill route", Group: "docs",
			Summary: "Print the smallest ordered read set for an intent.",
			Use:     "Use when starting a change and you need which project skill/domain files to read for an intent, e.g. `tusker skill route \"change auth refresh\" --json`.",
			Avoid:   "Not for searching documentation by keyword — use `tusker docs find <query>`. Not for task context — use `tusker packet <TASK-ID> --for agent`.",
			Ref:     printSkillHelp, Run: run0(skillV7RouteCmd)},
		{Name: "skill bundle", Group: "docs",
			Summary: "Write a portable, copied Tusker skill bundle for cloud or handoff.",
			Use:     "Use when a cloud agent or ChatGPT handoff needs real files, not symlinks. Default output .tusker/_generated/skill-bundle.",
			Avoid:   "Not for exporting project knowledge (SKILL.md plus domains) — use `tusker publish skill`. Not for refreshing installed skills — use `tusker skill sync` or `tusker install`.",
			Ref:     printSkillHelp, Run: run0(skillBundleCmd)},

		// models
		{Name: "runner catalog", Group: "models",
			Summary: "List installed runner harnesses and models without launching them.",
			Use:     "Use when you need which agent CLIs (codex, claude-code, etc.), versions, transports, and models exist on this machine. --refresh bypasses cache.",
			Avoid:   "Not for level-to-profile mapping — use `tusker models show`. Not for proving a harness works — use `tusker runner test <profile>`.",
			Ref:     printRunnerHelp, Run: run0(runnerCatalogCmd)},
		{Name: "runner test", Group: "models",
			Summary: "Run conformance checks against a harness or profile.",
			Use:     "Use when verifying a runner harness or profile behaves correctly before routing work to it. No model launch unless --live.",
			Avoid:   "Not for listing harnesses — use `tusker runner catalog`. Not for task routing — use `tusker runner route`.",
			Ref:     printRunnerHelp, Run: runnerConformanceCmd},

		// daemon
		{Name: "automation plan", Group: "daemon", Hidden: true,
			Summary: "Print the daemon's dispatch decision for a task. Read-only.",
			Use:     "Use when an agent or operator needs the structured decision the daemon would make for a task: runner, workspace, and eligibility. It does not authorize dispatch.",
			Avoid:   "Runs nothing. Interactive agents must not follow it with `tusker automation dispatch`; implement the task yourself with `tusker work start`.",
			Ref:     printAutomationHelp, Run: runID(automationPlanCmd)},
		{Name: "automation dispatch", Group: "daemon", Hidden: true,
			Summary: "Dispatch a task now if it passes the daemon's checks.",
			Use:     "Use only as an operator outside agent sessions, to skip polling for a task that already passes every dispatch check. Refused inside agent sessions.",
			Avoid:   "Not for interactive agents; implement with `tusker work start`. Not for whole waves; use `tusker wave start`. Not for parked runs; use `tusker redrive`.",
			Ref:     printAutomationHelp, Run: runID(automationDispatchCmd)},
		{Name: "serve", Group: "daemon",
			Summary: "Serve the localhost operator UI and JSON API.",
			Use:     "Use when a human needs the control room (needs-you items, runs, waves, tasks, gates, executions) or a tool needs the localhost JSON API. Requires `--by human:<name>`.",
			Avoid:   "Not the daemon; serve dispatches nothing. Start background work with `tusker daemon install` and `tusker wave start`.",
			Ref:     printServeHelp, Run: run0(serveCmd)},

		// maintain
		{Name: "gc", Group: "maintain",
			Summary: "Sweep stale entries from .tusker/scratch; dry-run by default.",
			Use:     "Use when .tusker/scratch is large or over its 200MiB budget, or to pin a task's scratch with --keep. Pass --yes to delete.",
			Avoid:   "Not for deleting the vault or tracker state — use `tusker purge` or `tusker reset`. Not for evidence pruning — use `tusker evidence prune`.",
			Ref:     printGCHelp, Run: run0(scratchGCCmd)},
		{Name: "redact", Group: "maintain",
			Summary: "Append a redaction event for a record without rewriting history.",
			Use:     "Use when a human must mark sensitive content in a task, gate, evidence, or other record as redacted, with a reason and optional replacement text.",
			Avoid:   "Not for fixing a wrong actor on an event — use `tusker actor correction plan`. Not for deleting work — use `tusker discard`.",
			Usage:   "tusker redact <ID> --reason <text> --by human:<name> [--object-kind <kind>] [--target <field>] [--replacement <text>]",
			Run:     run0(redactV7Cmd)},
		{Name: "feedback add", Group: "maintain",
			Summary: "Write a short structured product feedback note.",
			Use:     "Use when an agent or human hits friction with Tusker or the project and wants to record context, friction, product idea, impact, and related refs.",
			Avoid:   "Not for runner failures needing operator action — use `tusker message ask --severity`. Not for tracked work — use `tusker new task`.",
			Ref:     printFeedbackHelp, Run: feedback("add")},
		{Name: "feedback digest", Group: "maintain",
			Summary: "Summarize feedback notes since a date across repos.",
			Use:     "Use when reviewing accumulated feedback notes for one or more repos since a date, optionally writing the digest file with --write.",
			Avoid:   "Not for the operator state digest of landings, escalations, and gates — use `tusker digest`.",
			Ref:     printFeedbackHelp, Run: feedback("digest")},
		{Name: "demo seed", Group: "maintain", Hidden: true,
			Summary: "Create a disposable demo project with a smoke task and three waves.",
			Use:     "Use when you need a deterministic demo repo to exercise Tusker; seeding is inert and leaves waves unstarted.",
			Avoid:   "Not for real projects — use `tusker init`. Not for executing — use `tusker demo run`.",
			Ref:     printDemoHelp, Run: demoSeedCmd, Mutates: true},
		{Name: "demo status", Group: "maintain", Hidden: true,
			Summary: "Show the state of a seeded demo project.",
			Use:     "Use when checking a demo repo's waves, tasks, and runs.",
			Avoid:   "Not for real projects — use `tusker list` or `tusker status`.",
			Ref:     printDemoHelp, Run: demoStatusCmd},
		{Name: "demo run", Group: "maintain", Hidden: true,
			Summary: "Authorize and execute named demo waves, offline or with a real harness.",
			Use:     "Use when driving demo waves through the CLI: offline timer executor by default, or --mode real --require-harness to test a real runner.",
			Avoid:   "Not for real work — use `tusker run <TASK-ID|WAVE-ID>`. Not for runner conformance — use `tusker runner test`.",
			Ref:     printDemoHelp, Run: demoRunCmd, Mutates: true},
		{Name: "demo session", Group: "maintain", Hidden: true,
			Summary: "Run one scripted interactive-session scenario against a harness.",
			Use:     "Use when testing harness session control in a demo: restart, kill-worker, say, ask, stop-continue, permission-deny.",
			Avoid:   "Not for real run control — use `tusker runs say|stop|continue <run-id>`.",
			Ref:     printDemoHelp, Run: demoSessionCmd, Mutates: true},
		{Name: "demo wait", Group: "maintain", Hidden: true,
			Summary: "Wait until the demo reaches a terminal state or times out.",
			Use:     "Use when a script must block until demo runs finish: `--until terminal --timeout 120s`.",
			Avoid:   "Not for real work — use `tusker work wait`.",
			Ref:     printDemoHelp, Run: demoWaitCmd},
		{Name: "demo check", Group: "maintain", Hidden: true,
			Summary: "Assert the demo's final state matches expectations.",
			Use:     "Use when verifying a demo run produced the expected attempts, evidence, and closes.",
			Avoid:   "Not for real vault invariants — use `tusker validate`.",
			Ref:     printDemoHelp, Run: demoCheckCmd},
		{Name: "demo reset", Group: "maintain", Hidden: true,
			Summary: "Preview or delete demo-owned state in a demo repo.",
			Use:     "Use when a demo repo should be returned to empty; previews by default, refuses active runs.",
			Avoid:   "Not for real repos — use `tusker reset` or `tusker purge`.",
			Ref:     printDemoHelp, Run: demoResetCmd, Mutates: true},

		// internal: plumbing the binary calls on itself, and pipelines being retired
		{Name: "mcp serve", Group: "internal", Hidden: true,
			Summary: "Serve worker ask, update, and inbox tools over stdio MCP.",
			Use:     "Use when configuring an agent harness to reach Tusker message tools as an MCP server.",
			Avoid:   "Not for the web control room — use `tusker serve`. Not for direct messages from a shell — use `tusker message send|ask`.",
			Run:     run0(mcpServeCmd)},
		{Name: "runner-wrapper", Group: "internal", Hidden: true,
			Summary: "Internal: process wrapper the daemon spawns around a runner.",
			Use:     "Use when never by hand; the daemon spawns it with a one-shot --request file to contain a harness process.",
			Avoid:   "Not for running work — use `tusker run <TASK-ID>`. Not for testing a harness — use `tusker runner test`.",
			Usage:   "tusker runner-wrapper --request <request-file>   # internal, spawned by the daemon",
			Run:     run0(runnerWrapperCmd)},
		{Name: "gate-run", Group: "internal", Hidden: true,
			Summary: "Run configured gate-tier proof commands; JSON output, exit 1 on fail.",
			Use:     "Use when proving the project gate on the current tree; reuses gate-ledger hits; --changed runs only touched scopes from orchestration.gate.scopes.",
			Avoid:   "Not for human gates — use `tusker gate`. Not for only checking a cached pass — use `tusker gate-ledger check --command <cmd>`.",
			Run:     gateRunCmd},
	}
}

func workSession(action string) func(Args) (int, error) {
	return func(a Args) (int, error) { return 0, workSessionCmd(a, action) }
}

func workLifecycle(action string) func(Args) (int, error) {
	return func(a Args) (int, error) { return 0, workRealLifecycleCmd(a, action) }
}

func messageCmd(command string) func(Args) (int, error) {
	return func(a Args) (int, error) { return 0, agentMessageCmd(command, a) }
}

func execution(action string) func(Args) (int, error) {
	return func(a Args) (int, error) { return 0, executionCmd(a, action) }
}

func docs(action string) func(Args) (int, error) {
	return func(a Args) (int, error) { return 0, docsCmd(action, a) }
}

func modelsProfileLifecycle(command string) func(Args) (int, error) {
	return func(a Args) (int, error) { return 0, modelsProfileLifecycleCmd(a, command) }
}

func daemonService(action string) func(Args) (int, error) {
	return func(a Args) (int, error) {
		a["_pos0"] = action
		return 0, daemonServiceCmd(a)
	}
}

func feedback(action string) func(Args) (int, error) {
	return func(a Args) (int, error) {
		a["_pos0"] = action
		return 0, feedbackV7Cmd(a)
	}
}

// verifyCmd routes the verify subcommands that share one positional parser.
func verifyCmd(a Args) error {
	switch strings.ToLower(a.String("_pos0")) {
	case "add":
		return verifyV7AddCmd(a)
	case "remove":
		return verifyV7RemoveCmd(a)
	case "recipe":
		return verifyV7RecipeCmd(a)
	}
	printVerifyHelp()
	return nil
}

func printModelsHelp() {
	fmt.Println("Usage:\n  tusker models show [--json] [--compact]\n  tusker models profile-set [--scope global] --name <stable-id> [--display-name <name>] [--eligible-tiers <light,standard,demanding>] --harness <harness> --model <id> --effort <effort> --preset <preset> [--command <path>] [--if-revision <sha256>] [--json]\n  tusker models profile-disable|profile-enable|profile-remove [--scope global] --name <name> [--if-revision <sha256>] [--json]\n  tusker models set --scope global|project --level light|standard|demanding --lane execute|review --profiles <ordered,csv> [--if-revision <sha256>] [--json]\n  tusker models reset --scope global|project --level <level> --lane execute|review [--if-revision <sha256>] [--json]\n\nProfiles are defined only in the global config; a project selects them with models set/reset.")
}
