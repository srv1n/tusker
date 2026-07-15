# V7 feedback implementation review handoff

Date: 2026-05-15

## Review objective

Review whether the V7 feedback patch fully fixes the lifecycle, legacy-docs
leakage, evidence/proposal policy, bootstrap split, and shipped skill/test
strategy problems raised in the previous static review.

## What changed

| Original finding | Implementation area | Primary files |
|---|---|---|
| P0-1 build completeness | Added fresh-clone baseline tests for module files, embedded skill assets, CLI help, and V7 init | `cmd/tusker/fresh_clone_baseline_test.go` |
| P0-2/P0-3 legacy docs leakage and mixed bootstrap | Default init/bootstrap is V7-only; legacy bootstrap is explicit; top-level docs help redirects to legacy; public/site publication narrowed to V7-safe docs | `cmd/tusker/commands_create.go`, `cmd/tusker/install.go`, `cmd/tusker/cli.go`, `docs/publication.yaml`, `README.md` |
| P0-4 finish lifecycle | Added `tusker finish`; `attempt handoff` now requests review unless explicitly bypassed | `cmd/tusker/commands_v7.go`, `skills/tusker/SKILL.md`, `skills/tusker/references/WORKFLOW.md`, `tusker/docs/agents/use-tusker.md` |
| P0-5 close bypass | `close` now requires task status `review` unless forced; smoke flow updated | `cmd/tusker/v7_control_cmd.go`, `cmd/tusker/v7_smoke_test.go` |
| P0-6 guardrails | Broadened guardrails for finish contract, top-level help, bootstrap leakage, and active shipped surfaces | `cmd/tusker/v7_guardrail_test.go` |
| P1-1 gate evidence | Blocking gate satisfaction requires durable evidence unless forced; gate records satisfaction evidence fields | `cmd/tusker/v7_control_cmd.go`, `internal/v7schema/schema.go` |
| P1-2 bad status proposal | `propose status --status done` is rejected; close proposals remain the done path | `cmd/tusker/v7_proposal_cmd.go` |
| P1-3 proposal reviewer policy | Accept/reject defaults to `human:<default>` and blocks self-accept without explicit override | `cmd/tusker/v7_proposal_cmd.go` |
| P1-4 proposal apply consistency | Proposal apply now writes `applying_*` transaction metadata before target mutation and records applied metadata afterward | `cmd/tusker/v7_proposal_cmd.go`, `internal/v7schema/schema.go` |
| P1-5 source-of-truth clarification | V7 spec now defines `source_of_truth` as repo-local canonical inputs, not old publication freshness | `tusker/docs/spec/tusker-v7-repo-local-work-tracker-spec.md` |
| P1-6 evidence acceptance | Review-heavy evidence kinds default to pending review; accepted manual/video/security/release/human/perf evidence requires human/reviewer acceptance | `cmd/tusker/v7_evidence_attempt_cmd.go`, `cmd/tusker/v7_validation.go` |
| P2 dashboards/tests | Dashboards get generated headers; skill now includes acceptance-linked proof, test class selection, and retry/output discipline | `cmd/tusker/v7_state_runtime.go`, `skills/tusker/references/RISK_AND_EVIDENCE.md` |

## Legacy quarantine

- V7 default bootstrap no longer creates `_config/docs-map.yaml`, V5 docs paths,
  V5 templates, or V5 Obsidian views.
- `tusker legacy init` and `tusker legacy docs ...` are the explicit legacy path.
- Public site export no longer publishes the old documentation model, historical
  specs, or broad legacy skill references.
- Old research packs were moved under `research/legacy/`.
- Repo-local installed skill copies were refreshed with
  `tusker update --repo . --repo-only --no-bin`.

## Tests and checks run

```sh
rtk go test ./cmd/tusker -run 'TestV7|TestFreshClone' -count=1
rtk go test ./cmd/tusker -count=1
rtk go test ./...
rtk go run ./cmd/tusker legacy docs export --vault ./tusker --site ./site --clean --quiet
rtk go run ./cmd/tusker legacy docs build --vault ./tusker --site ./site --quiet
rtk go run ./cmd/tusker validate --json
```

Current results:

- `go test ./...`: 216 passed across 4 packages.
- docs build: succeeded from 14 routes.
- `tusker validate --json`: zero errors, one warning.

Known validation warning:

```text
PATH_MISMATCH: docs/agents/use-tusker.md carries V7 knowledge frontmatter but
still lives under tusker/docs/agents/ for compatibility with existing guardrails
and published agent-doc path.
```

## Suggested review focus

1. State-machine correctness: `finish`, `attempt handoff`, `close`, status
   proposals, gates, and evidence coverage should make it impossible to hide
   completed implementation outside review.
2. Branch-safety: ensure direct protected-state mutations still go only through
   control commands or proposals.
3. Proposal apply recovery: the new `applying_*` marker is a near-term
   transactional guard, not a full rollback system. Review idempotence and
   reconcile behavior carefully.
4. Legacy quarantine: verify public/help/default-init surfaces are V7-first and
   legacy docs-map behavior is reachable only through explicit legacy paths.
5. Evidence policy: confirm the acceptance rules are strict enough without
   making automated test evidence painful.
6. Publication pruning: confirm removing old specs/internal pages from the site
   is the desired product choice, not just a test-passing cleanup.
7. Dirty tree hygiene: this repo was already heavily modified before this work;
   review the diff by topic, not by one giant file list.
