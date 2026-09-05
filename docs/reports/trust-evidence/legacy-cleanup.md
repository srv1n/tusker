# Legacy cleanup evidence

## Batch A

`cmd/tusker/removed_surfaces.go` no longer carries V5/V6 command fences,
migration report shapes, or schema-validation no-ops. The retained V7
`publish skill`, `status`, and `evidence add|promote|prune` paths remain; an
unknown evidence action now returns `evidence requires add, promote, or
prune`.

`cmd/tusker/commands_index.go` now calls `v7DashboardLandingNote` directly.
The deleted V5-named wrapper had that single caller and only delegated to the
same function.

## Batch B

The old vault docs-map and V6 validation/index layers were unconditionally
inert: `loadDocsMap` always returned `nil` and `hasV6Vault` always returned
`false`. Their reindex and validation branches, generated Docs catalog, and
V5/V6 collision exceptions are gone. The current `internal/docgraph` map is
separate and remains in its active docs command and Serve paths.

Current domain and knowledge tests now call their V7 handlers directly rather
than preserve local aliases just for test compilation.

## Batch C

Project configuration now has one read path: `.tusker/config.yaml`, followed
by the optional `.tusker/config.local.yaml`. The resolver, initialization,
vault discovery, feedback lookup, delivery-context provenance, and mutation
notification no longer inspect root `tusker.yaml`, `tusker.local.yaml`, or a
`tusker/` vault directory.

The obsolete `migrate vault-root` implementation and its migration-only tests
were deleted. Current configuration fixtures retain their profile and policy
settings in the managed config path. The tracked root `tusker.yaml` was then
deleted after its replacement was validated; no compatibility copy remains.

Before disabling the root-file read path, the root and managed config were
decoded as YAML and compared by scalar path. The managed config contained all
115 root-config scalar paths with the same values and adds current-only
settings. The unavailable `codex_acp` runner was then replaced by the existing
`codex_exec` runner in the default runner, enabled-runner list, and 11 runnable
profiles. Every other root-config setting is preserved in the managed config.

The lifecycle steward validated the managed config and `WORKFLOW.md` after the
runner correction: `codex_exec` is selected, route blockers are empty, and
automation is disabled. This cleared deletion of the obsolete root file.

## Fresh setup correction

Reset exposed a separate current-path defect: fresh config and workflow
defaults named `codex_acp` even though no ACP runner definition existed.
Bootstrap now uses the available `codex_exec` runner for workflow defaults,
built-in profiles, generated config, and catalog-derived profiles. ACP remains
available only through its explicit setup path, which writes the admitted
machine-local adapter configuration.

Active close-policy normalization remains. It now receives the managed config
path directly, pending its current callers' replacement work.

## Verification

Passed after the cleanup:

```sh
go test ./cmd/tusker -run '^(TestV7EvidenceAddUpdatesProofStatusAndTaskEvidenceSection|TestV7ReindexRefreshesDashboardBasesAndSummary|TestV7DashboardBuildIsDeterministicAndValidateRejectsStaleProjection|TestV7DomainNewCreatesKnowledgeDomainLayoutAndValidates|TestCapsuleTemplatesCreateScaffolds)$' -count=1
```

Five focused tests passed. They cover the retained V7 evidence, reindex,
dashboard, domain, and capsule paths; they are not full-suite proof.

A later direct CLI check could not compile the concurrently edited package:
`cmd/tusker/human_control_receipt.go` referenced an undefined
`v7HumanControlRequired`. That file is outside this cleanup and was absent
from the audit baseline.

Batch C source checks are staged but Go tests are intentionally held until the
concurrent contract and proof changes compile as one package.
