---
title: Skill packaging and distribution
subject: skills
keywords: [skills, install, provenance, compatibility, fingerprint, symlink, bundle, AGENTS.md, setup doctor]
part_of: overview
status: canonical
read_when: "You are installing, syncing, bundling, diagnosing, or repairing the Tusker operator skill, or you need to know which skill paths are editable source versus generated output and how staleness is detected."
skip_when: "You want task lifecycle, proof, or gate mechanics (read the installed `tusker` skill and [[tasks-and-proof]]), or the intake routing table ([[factory-intake]])."
sources:
  - skills/tusker/bundle.go
  - cmd/tusker/install.go
  - cmd/tusker/skill_compatibility.go
  - cmd/tusker/skill_provenance.go
  - cmd/tusker/skill_install_safety.go
  - cmd/tusker/v7_skill_cmd.go
  - cmd/tusker/v7_skill_guidance_cmd.go
  - cmd/tusker/setup_doctor.go
  - cmd/tusker/capabilities_cmd.go
  - skills/tusker/assets/compatibility.yaml
---

# Skill packaging and distribution

One canonical source tree (`skills/tusker/`) is embedded in the binary and
materialized into the directories agents read. Edit the source; never edit an
install target.

## Canonical source tree

`skills/tusker/bundle.go` embeds `SKILL.md LICENSE references docs agents assets`
via `go:embed`. `testdata/`, `README.md`, and `bundle.go` itself are **not**
embedded; only a copy install from an explicit `--source` checkout carries them.

| Path | Contents |
| --- | --- |
| `SKILL.md` | Router. Frontmatter is exactly two keys (`name: tusker`, `description`); enforced by `validateTuskerSkillPackageShape` in `cmd/tusker/skill_provenance.go`. |
| `references/*.md` | 24 guides. Seven are live routes (below); the rest are one-hop or legacy targets. |
| `docs/*.md` | 5 files: orchestration runbook, failure classes, operator intervention, dispatcher pseudocode, README. |
| `agents/openai.yaml` | Codex-side agent manifest. |
| `assets/compatibility.yaml` | `tusker.skill-compatibility/v1` contract. |
| `assets/factory-intake-contract.yaml` | See [[factory-intake]]. |
| `assets/repo-contract/` | Exactly three files: `AGENTS.workflow-snippet.md`, `docs/agent-workflow.md`, `docs/ai-contribution-policy.md`. |
| `assets/snippets/{AGENTS,CLAUDE}.md.snippet`, `assets/templates/*.md`, `assets/icons/*.svg`, `assets/README.md` | Snippets, note templates, branding. |
| `testdata/progressive-disclosure-budget.json` | Test fixture only (not embedded). |

`skills/tusker/assets/gitignore.recommended` no longer exists.

## Install destinations and modes

`cmd/tusker/install.go` resolves destinations then a per-destination mode.

| Flag | Destination |
| --- | --- |
| `--codex-user` | `~/.agents/skills/tusker` |
| `--claude-user` | `~/.claude/skills/tusker` |
| `--repo <path>` | `<repo>/.agents/skills/tusker` **and** `<repo>/.claude/skills/tusker` |
| (no `--repo`, or `--refresh-existing-user-skills`) | refreshes whichever of `~/.agents`, `~/.codex`, `~/.claude` `skills/tusker` already exist (`existingUserSkillDestinations`) |

`~/.codex/skills/tusker` is refreshed if present but is never created: only
`~/.agents` is a `--codex-user` target (`defaultCodexUserSkillDestination`).

Mode (`skillInstallModeForDestination`): explicit `--skill-mode`/`--mode`
(`copy`|`symlink`|`link`) wins; otherwise repo-local destinations get
`symlink` and everything else gets `copy`. Both repo-local destinations are
symlinks — the `.claude` target is not a copy.

`skillSymlinkTarget` writes a **relative** target when the destination is
repo-local to the source's own repo, so worktrees and fresh clones resolve;
otherwise the absolute source path is kept.

Copy mode reads the embedded payload unless `--source`/`TUSKER_SKILL_SOURCE`
names a checkout. Symlink mode always needs a source: `canonicalSkillSourceDir`
accepts a skill dir, a checkout (`<root>/skills/tusker` or `<root>/skill`), or
discovers the repo root from cwd; with none it errors and suggests
`--skill-mode copy`.

Other flags: `--bin-dir` (first writable of `~/.local/bin`, `/opt/homebrew/bin`,
`/usr/local/bin` that is on `PATH`, else `~/.local/bin`), `--no-bin`, `--force`
(accepted, unused), `--json`, `--repo-only` (update only).

The binary symlink is staged then `rename`d (`replaceBinarySymlinkAtomically`)
and refuses to run when source and target resolve to the same file — a
release-installed binary must be reinstalled by the release script.

Make targets: `install-user` (binary + both user skills), `install-repo
REPO=/abs/path`, `install-bin`, `install` (alias for `install-user`),
`mac-preview` (`install-user` + TuskerBar).

## Commands

| Command | Behavior |
| --- | --- |
| `tusker install` / `tusker update` | Install/refresh skills, binary symlink, and the managed `AGENTS.md`/`CLAUDE.md` block; `update` only touches destinations that already exist plus `--repo`. |
| `tusker skill sync [--repo .] [--mode symlink\|copy] [--source <checkout>]` | Repair path. Rewrites only the two repo-local `skills/tusker` packages. Never touches pointers, project knowledge, secrets, or other skills. Rejects a source classified `generated` or `invalid`. |
| `tusker skill bundle [--repo .] [--out …]` | Portable copy-mode bundle at `.tusker/_generated/skill-bundle` by default, containing `.agents/` and `.claude/` trees plus a `.tusker-skill-bundle` marker. Refuses an explicit non-default `--out` lacking that marker. |
| `tusker skill doctor [--package <path>] [--strict] [--json]` | Without `--package`, validates the vault's V7 project skill; with `--package`, validates an Agent-Skills package and reports provenance (`not_tusker` unless the directory is named `tusker`). |
| `tusker skill route "<intent>" [--json]` | Scores `.tusker/knowledge/domains/*` by id/title/summary/body token overlap and returns `SKILL.md` plus up to three domains' `INDEX.md`+`CANON.md`. |
| `tusker skill pack <TASK-ID> --for agent [--budget n]` | Thin wrapper over `tusker packet`. |
| `tusker skill audit-agent-guidance [--repo] [--write\|--draft] [--target feedback\|knowledge]` | Finds non-managed `AGENTS.md`/`CLAUDE.md` content, classifies it, warns on a missing/stale managed block, optionally upserts pointers and writes a migration draft. Exits 1 on any finding; warnings alone exit 1 only under `--json`. |
| `tusker sync-repo-contract --repo <path> [--vault] [--force]` | Writes `assets/repo-contract/**` into the repo (skipping existing files unless `--force`), ensures the feedback README, and upserts the pointer block. No `.github/` templates ship today. |
| `tusker setup doctor` / `tusker setup repair [--dry-run] [--source]` | See below. |

Fresh `tusker init` materializes the `/spec` skill in both repo-local agent
directories and preserves any existing copies.

`skill doctor --package` on an Agent-Skills package enforces: `name` matches
`^[a-z0-9]+(-[a-z0-9]+)*$`, ≤64 chars, equal to the directory name;
`description` 1–1024 chars; `compatibility` ≤500 chars; string-only `metadata`;
resolvable `references|scripts|assets` links; ≤500-line body (warning, error
under `--strict`); and no `work/ epics/ evidence/ attempts/ events/ _generated/
_system/ dashboards/ Attachments/` paths or local absolute paths.

## Compatibility and provenance

`assets/compatibility.yaml` (`tusker.skill-compatibility/v1`) declares `schema`,
`version`, `workflow_min`/`workflow_max`, `tracker_schema_versions`,
`wave_authorization_schemas`, the factory-intake provenance triple,
`canonical_source`, `materialization_schema`, and `primary_guides`. It is
decoded with `KnownFields(true)` — an unknown key is a hard parse error.

`tusker capabilities --json` (`cmd/tusker/capabilities_cmd.go`) projects that
contract plus `canonical_payload_fingerprint` (SHA-256 over the embedded
payload, excluding `.tusker-skill-provenance.yaml`) and hashes the whole
material — commands, schemas, runner adapters, optional capabilities,
deprecations, contract — into one `compatibility.fingerprint`. Adding a command
or flipping an optional capability changes it.

Materialized copies carry `.tusker-skill-provenance.yaml`
(`tusker.skill-materialization/v1`) with source kind/identity
(`embedded://tusker/skills/tusker` or `canonical://skills/tusker`), the
compatibility schema + fingerprint, the canonical payload fingerprint, the
factory-contract triple, and a `payload_fingerprint` computed over the
destination excluding the manifest itself and `.DS_Store`.

`inspectSkillMaterialization` classifies:

| Status | Meaning |
| --- | --- |
| `current` | Manifest, payload, compatibility, and contract all match the running binary. |
| `missing` | Destination absent, or symlink target unresolvable. |
| `missing_provenance` | Copy with no manifest. |
| `locally_modified` | Payload hash differs from the recorded one. |
| `stale` | Compatibility fingerprint or contract version/fingerprint predates the binary; also legacy `SKILL.md` frontmatter metadata instead of `assets/compatibility.yaml`. |
| `incompatible` | Schema mismatch, unreadable manifest, manifest contradicting the packaged contract, or a compatibility range not covering this binary. |

Symlinks are resolved and their live target inspected — no cached manifest can
make a retargeted link look current. The canonical in-repo package is detected
separately (`inspectTuskerSkillPackage`) and reported with `source_kind:
canonical`. Repair for every non-`current` status is
`tusker skill sync --repo . --source <canonical-tusker-checkout>`.

Install writes are transactional (`cmd/tusker/skill_install_safety.go`): a
`.tusker-*-replace.lock` mkdir lock, populate a staging dir, rename the old
destination aside, rename staging in, then delete the backup — with rollback if
activation fails. Every path is confined to a managed boundary derived from the
`.agents`/`.claude`/`.codex` ancestor; the filesystem root is refused, and no
ancestor may be a symlink.

## Managed AGENTS.md / CLAUDE.md block

`renderTuskerPointerBlock` emits a `## Tusker` block between the
`tuskerPointerBegin`/`tuskerPointerEnd` markers, naming the installed skill,
`<vault>/SKILL.md`, `tusker next`, the do-not-read list, compact-proof rules,
scratch non-durability, and the feedback instruction. `upsertTuskerPointer`
replaces the marked region in place, appends it if the markers are absent, and
creates the file if missing. `install --repo`, `update --repo`, `sync-repo-contract`,
`init`, `migrate vault-root`, and `skill audit-agent-guidance --write` all call
it; `skill sync` and `skill bundle`
deliberately do not.

If pointers changed but `<vault>/SKILL.md` is missing and non-managed guidance
exists, install/update print a flattening warning
(`repoAgentGuidanceFlatteningWarning`).

## Setup doctor and repair

`runSetupDoctor` (`cmd/tusker/setup_doctor.go`) emits a
`tusker.setup-doctor/v1` report over: registered-project vault root and
workflow path, legacy dispatch-scope / completion-reactor config warnings,
broken or legacy `.tusker` symlinks, scratch budget, both repo-local skill
installs (`skill_install_<status>`, repairable only when the classified source
is `canonical`), `tusker`-on-`PATH` versus running binary, and ChatGPT-handoff
config/profile/routing/zip/workflow findings. Skill repair reinstalls in
**symlink** mode and re-inspects; a repair that does not reach `current` is
reported, not silently accepted. `tusker delivery rollout doctor` reuses the
same function with a narrowed `RepairScope`.

## Source vs generated

| Location | Kind |
| --- | --- |
| `skills/tusker/**` | Canonical source — edit here. |
| `skills/spec/SKILL.md` | Canonical `/spec` skill source; embedded into the Tusker binary. |
| `~/.agents`, `~/.codex`, `~/.claude` `skills/tusker` | Generated copies. |
| `<repo>/.agents/skills/tusker`, `<repo>/.claude/skills/tusker` | Generated symlinks (copies only with `--skill-mode copy`). |
| `<repo>/.agents/skills/spec`, `<repo>/.claude/skills/spec` | Generated `/spec` skill copies; `tusker init` fills only missing destinations. |
| `.tusker/SKILL.md` + `knowledge/domains/**` | Generated `tusker.project-skill/v7` — regenerate with `tusker publish skill --v7`. |
| `.tusker/_generated/skill-bundle/**` | Generated portable bundle. |
| `<repo>/docs/agent-workflow.md`, `docs/ai-contribution-policy.md`, `AGENTS.workflow-snippet.md` | Generated from `assets/repo-contract/`. |

## Progressive-disclosure budget

`cmd/tusker/skill_progressive_disclosure_test.go` enforces the router contract
against `skills/tusker/testdata/progressive-disclosure-budget.json`
(`tusker.skill-disclosure-budget/v1`): `SKILL.md` body ≤900 words and ≤140
lines, exactly two frontmatter keys, exactly seven `references/*.md` routes
matching a fixed route table, no `references/` mention at all inside
`TRACK.md`/`KNOWLEDGE.md`/`RUN.md`/`OPERATE.md`, and no duplicated paragraphs
across those four plus `SKILL.md`.

Each of the six fixture cases pins router+guide word counts **exactly**
(`loaded_words`) under a ceiling (`max_loaded_words`) and asserts required
safety strings survive. Editing a primary guide's length fails the test until
the fixture is updated. The test also re-derives `skillPayloadFingerprint` for
the repo's own `.agents` and `.claude` installs and requires them to equal the
canonical tree's.
