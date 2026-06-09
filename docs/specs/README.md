# Tusker V5 Spec Set

This directory is the implementation spec set for Tusker V5.

Tusker has three concerns:

1. the installable skill bundle in `skill/SKILL.md`
2. the V5 markdown tracker stored in the vault
3. internal runtime orchestration on top of V5 tasks

## Spec Map

| File | Purpose | Primary implementation surface |
|---|---|---|
| `00-product-modes.md` | Product shape, packaging, and public CLI boundary | `cli.go`, `install.go` |
| `01-vault-tracker.md` | V5 note model and generated indexes | `schema.go`, `notes.go`, `commands_v5.go`, `commands_index.go` |
| `02-workflow-contract.md` | `WORKFLOW.md` V5 runtime contract | `workflow.go`, `workflow_validate.go` |
| `03-daemon-and-registry.md` | Internal task-native runtime boundary | `daemon.go`, runtime store files |
| `04-workspace-manager.md` | Per-task isolated workspaces | workspace manager and runner integration |
| `05-runner-and-session-protocol.md` | Runner sessions, turns, and evidence | runner adapters |
| `06-review-rework-retry.md` | Verification, rework, close, and retry semantics | `commands_v5.go`, runtime reconcile logic |
| `07-documentation-site-and-publication.md` | Docs export and publication pipeline | docs exporter and site build |
| `08-symphony-alignment-and-orchestration-roadmap.md` | Runtime roadmap on the V5 task model | workflow, daemon, runner, docs |
| `09-chatgpt-pro-handoff-orchestration.md` | ChatGPT Pro artifact handoff and apply-loop orchestration | daemon, runner adapters, external collection |
| `10-repo-bootstrap-and-existing-repo-onboarding.md` | Existing repo setup, curated onboarding packets, and conservative import | `install.go`, `cli.go`, onboarding commands, skill bundle |

## Commitments

- Current work is `type: task`.
- Bug work is `kind: bug`.
- Durable docs live under `tusker/docs/**`.
- Public CLI stays at 11 commands.
- Runtime state never becomes frontmatter.
- Generated indexes are rebuildable caches.
- Close requires verification and docs impact resolution.

## Build Order

1. Public CLI and V5 note creation.
2. V5 validation and reindex.
3. V5 workflow, init refresh, templates, and views.
4. Docs map and docs impact close gate.
5. Runtime dispatch on V5 tasks.
6. Docs export/site generation.
7. Full smoke and release checks.

## Done Means

- Fresh init produces V5 assets only.
- Current validation rejects non-V5 managed notes.
- Public help exposes only V5 commands.
- Runtime sees V5 tasks.
- README, skill docs, source specs, and exported site teach the same model.
