# Tusker — dossier
Last updated: 2026-08-18 (update this line every edit)

## One paragraph
Tusker is a repo-local work protocol and tracker for software work in which humans write requirements and agents execute them. It keeps task contracts, evidence, gates, and project knowledge as versioned Markdown in each repository; a local daemon and SQLite runtime coordinate attempts, reviews, and delivery; users operate it through a CLI, a loopback web control room, or the macOS TuskerBar app.

## Status
Active private dogfood. Sarav is the only user; external users, customers, and revenue are zero. Task tracking, CLI-enforced contracts and gates, waves, the knowledge base, and TipTap document editing are in use. Agent dispatch and ACP orchestration are not yet stable enough to ship and are the next dogfood focus. The public GitHub repository has no releases, no semantic-version release tag is present locally, and the public installer refuses to run until its production Minisign key is provisioned. The installed CLI and user-wide skills trail the current source. Nothing is deployed or store-distributed.

## What it does (features, user-facing)
- Create and manage epics, tasks, decisions, dependencies, status, and acceptance contracts inside a repository.
- Attach verification rows, evidence, reviews, and explicit human or external gates to work.
- Group work into authorized waves; preflight, pause, resume, review, and land completed work.
- Review and import delivery plans without dispatching them until an exact human-confirmed start.
- Run agent work through configured Codex, Codex ACP, Codex Cloud, or Claude adapters with leases, isolated workspaces, permissions, attempts, and recovery controls.
- Inspect projects, tasks, live runs, evidence, gates, documents, and execution lineage in the CLI or local Serve UI.
- Use TuskerBar on macOS for a full window, menu-bar triage panel, notifications, deep links, and Spotlight search.
- Install or sync Codex/Claude agent skills, validate project knowledge, diagnose setup, and remove generated or machine-wide Tusker state through guarded commands.

## Who it's for
Solo software builders and very small technical teams managing agent-heavy projects in Git-tracked repositories. Collaboration is commit/push/pull rather than real-time synchronization.

## Numbers that are true
- GitHub stars: 1 on 2026-08-17. Reproduce from `https://github.com/srv1n/tusker` or `gh api repos/srv1n/tusker --jq .stargazers_count`.
- Public releases: 0 on 2026-08-17. Reproduce with `gh api repos/srv1n/tusker/releases --jq length`.
- Current source command manifest: 84 top-level command entries and 6 runner adapters. Reproduce with `go run ./cmd/tusker capabilities --json | jq '.commands|length, .runner_adapters|length'`.
- Release target matrix: 4 pairs (`darwin/arm64`, `darwin/amd64`, `linux/arm64`, `linux/amd64`). Reproduce from `RELEASE_MATRIX` in `Makefile`.
- External users: 0; total users: 1 (Sarav). Source: owner confirmation on 2026-08-18; the product has no user registry or telemetry.
- Revenue and customers: 0. Source: owner confirmation on 2026-08-18; no billing path exists in the product.
- Public release downloads: 0 because there are no GitHub releases. Reproduce with `gh api repos/srv1n/tusker/releases --jq length`; source-checkout clones and personal installs are not measured.
- Current benchmark or production reliability claim: none. The audit found only dated hardening reports; no current benchmark was rerun.
- Loss cases: Windows product surfaces are unsupported; Linux lacks launchd/TuskerBar and the full-gate provider; the public installer deliberately fails before download while its trust key is unprovisioned.

## Tech shape (short)
Go 1.26.5 CLI/daemon/API with Markdown+YAML project truth and a machine-local SQLite runtime.
React 19, TypeScript, Vite 8, TanStack, and Bun 1.3.14 power an embedded loopback Serve UI.
Swift/AppKit supplies the macOS TuskerBar shell and supervises the bundled daemon.
Runner adapters cover Codex ACP, Codex exec/app-server/cloud, and Claude Code.
Authority is fail-closed: CAS revisions, leases, explicit gates, typed proof, capability-gated local mutations, and fingerprint-bound delivery starts.
Automation is opt-in; the resident daemon is the only dispatch authority.
Canonical task state stays repo-local; optional `codex_cloud` execution is remote.

## Recent changes (rolling, newest first, keep last ~10)
- 2026-08-18: Rebuilt the operator skill around adoption tiers (25 reference files to 6), defined the repo-knowledge read-when/skip-when routing contract, and made tier-1 close ceremony-free to match the tier contract.
- 2026-08-18: Made Serve project settings real: layered config table with provenance (tier, enabled runners editable), setup doctor/repair panel, and registration removal; deleted inert budget-enforcement, intake-router, and spend-invariant machinery.
- 2026-08-18: Fixed all nine adversarial-review findings on the opt-in wave (including an uninstall --state env-poisoning guard) and made run directives — the Serve play button — bypass the automation opt-in as deliberate human authority.
- 2026-08-17: Made installation and automation opt-in, added global uninstall, disabled new project polling by default, and expanded guarded Serve setup controls.
- 2026-08-17: Tightened proof command parsing, transactional evidence writes, gate refusals, and delivery issue typing.
- 2026-08-17: Added adoption tiers and hardened gate, delivery, execution, and reconciliation integrity.
- 2026-08-11: Made Codex ACP the primary local runner.
- 2026-08-11: Added verified local Codex ACP adapter installation and capability checks.
- 2026-08-10: Added fenced ACP attempt execution and a fail-closed permission broker.
- 2026-07-29: Added targeted, revision-safe reconciliation and execution observability.

## Deliberate exclusions
- Legacy V5/V6 execution and documentation commands are outside the default V7 surface; migration is out-of-band.
- Planning, review, and import do not dispatch work; a fingerprint-bound human start is separate.
- Automation and project polling default off; enabling them is explicit.
- The runner does not silently fall back after a prompt may have been delivered, avoiding duplicate execution.
- Codex ACP full-access mode is refused until permission parity exists; Claude extension tools are unsupported.
- Windows CLI, daemon, installer, and release artifacts are not supported; Linux uses its host service manager.
- Public installation and release fail closed until signing trust is provisioned.
- Hosted or cloud-owned canonical project state, web SaaS, and mobile clients are intentionally out of scope; project truth stays local and Git-tracked.
- Real-time collaborative editing and large-team project management are intentionally out of scope; small teams synchronize through Git.
- General-purpose, non-code task tracking is intentionally out of scope.
- Universal coding-agent harness support is intentionally out of scope; Tusker supports a bounded set.
- Fully autonomous agent authority without explicit gates is intentionally out of scope.
- These product exclusions are expected to remain long-term, though Sarav has not made an irrevocable permanence promise.

## Open questions / embarrassments
- The current checkout is ahead of public `origin/main`; the installed CLI, `dist/tusker`, and user-wide skills are from older source, so source behavior is not installed/released proof.
- `make install` and related docs claim to install the CLI, but the Makefile omits the now-required `--bin`; the command currently refreshes skills without creating the PATH link.
- CLI help calls Serve read-only, but its capability-gated API includes authoritative mutations for projects, tasks, runs, gates, evidence, and delivery.
- Serve app-level settings remain reference-only: runner profile persistence, density/defaults, permissions, and notification settings are local mocks. Project-level settings (config table, tier, enabled runners, setup doctor, removal) are wired as of 2026-08-18; durable task-note editing and some redrive/readback paths are still unavailable. The embedded dist/ build predates the new settings UI.
- Root `tusker.yaml` selects `codex_acp`, while `.tusker/WORKFLOW.md` still selects `codex_exec`; UI mock profile values also trail backend defaults.
- `internal/serve/ui/README.md` still describes a mock-first frontend although production mode uses the daemon API.
- Historical V5 docs claim an 11-command public CLI; that claim does not describe the current 84-entry V7 manifest.
- A system overview says there is “no cloud service,” but the product includes an optional `codex_cloud` remote execution adapter; repo-local state, not all execution, is local.
- Dated hardening reports contain green test counts, but this audit did not rerun them and found no clean release, artifact publication, canary, restore, or soak proof.
- Harness orchestration went through several redesigns before settling on ACP; ACP has not yet received enough real dogfood use to justify release confidence.
