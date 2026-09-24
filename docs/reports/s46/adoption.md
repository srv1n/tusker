# S46 adoption receipt (TSK-T-0061) — BLOCKED, no migration applied

Outcome: the Tusker repository was re-inventoried and the migration story's
preview was reviewed read-only. No file was moved, no tracker YAML was edited,
and no approval was recorded. Apply is blocked on the owner/operator actions
listed below. This receipt is the task's blocking report, not proof of a
completed migration.

## Input inventory (2026-09-23)

- `.tusker/specs/`: 35 tracked Markdown files + 4 PNG assets
  (`git ls-files .tusker/specs | wc -l` = 39). At authoring: 32 tracked
  spec/decision files plus untracked `ui-reset.md`; `ui-reset.md` is now
  tracked, no untracked files remain under `.tusker/specs/`.
- `.tusker/specs/decisions/`: 6 grill/decision files.
- `.tusker/knowledge/domains/project/`: 3 files (`INDEX.md`, `CANON.md`,
  `glossary.md`).
- Governing spec `.tusker/specs/portable-project-documentation.md`
  sha256 `4e119cfbb2bdd745a048f9d8412f3ee8a145f7a595ae1e0295b96e25e232c92c`,
  subject `portable-project-documentation` (stable across migration).
- `.tusker/SKILL.md` sha256
  `c1023e94611a75ff791393823d567464a030255f88edf05d120b109402a3e71e`.
- Legacy references still resolve: `docs/system/INDEX.md` maps ~30 subjects
  to `.tusker/specs/` paths; `docs/system/00-overview.md`, `cli.md`,
  `knowledge-and-feedback.md`, `skills.md` document the transition; many
  active task `spec_refs` point at `.tusker/specs/` paths (material bindings
  changeable only through revision-aware CLI rebinding, never by hand-edit).

## Migration preview (read-only, reviewed)

- Command: `tusker docs adopt --migration --dry-run --json` (never writes).
- Table fingerprint: `sha256:19b739603bf32e14c98a68918e4a6ab33d1f739ca44927e6650de5ed9fe40079`,
  `ok:true`, 882 rows: 511 leave (policy/README), 278 promote (legacy
  Markdown), 57 leave (duplicate subject, manual merge required), 35 promote
  with explicit no-prior-approval lifecycle dispositions, 1 selected-current.
- Governing-spec row: `promote .tusker/specs/portable-project-documentation.md
  -> docs/system/proposals/portable-project-documentation.md`
  ("legacy canonical migration carries no prior approval; selected proposed").
- Decisions rows target `docs/system/decisions/`; spec rows target
  `docs/system/proposals/`; `glossary.md -> docs/system/domains/project/glossary.md`;
  `CANON.md`/`INDEX.md` are `leave` (duplicate subject, manual merge required).
- The 882-row scope covers the whole working tree, far beyond this task's
  bounded corpus; an apply must use an explicitly reviewed row subset, not the
  raw table.

## Output inventory

- No document moved. `docs/system/proposals/` and `docs/system/decisions/`
  were not created. No forwarding stubs written. No skill pointers changed.
- `tusker docs check --json`: `valid:false` (50 docs; 2 owned-file lifecycle
  issues: `draft` in `.tusker/specs/spec-to-proof.md`, `planned` in
  `.tusker/specs/tusker-trust-and-efficiency.md`).
- `tusker validate --json`: 289 errors (includes the 2 above plus unrelated
  pre-existing errors, stale generated map, V7 domain layout mismatches).
- This receipt is the only new file: `docs/reports/s46/adoption.md`.

## Blocked apply — exact owner/operator actions

1. Hard dependency unmet: `TSK-T-0060` (guidance) is `backlog`/`held`, as are
   `TSK-T-0055`–`TSK-T-0059`. Operator: complete guidance first, then
   re-authorize this task through the normal task entry.
2. Dirty owned files (concurrent uncommitted work, owner unknown — coordinate
   the live author before touching): `docs/system/00-overview.md`,
   `docs/system/cli.md`, `docs/system/knowledge-and-feedback.md`,
   `docs/system/skills.md`, `skills/tusker/assets/templates/*` (6 files),
   `skills/tusker/references/KNOWLEDGE.md`, `OPERATE.md`,
   `REPO_ONBOARDING.md`, `SPECS.md`, `cmd/tusker/docs_adopt_cmd.go`,
   `cmd/tusker/docs_adopt_cmd_test.go`; plus uncommitted
   `cmd/tusker/docs_s46_migration_test.go`,
   `cmd/tusker/s46_guidance_contract_test.go`, `docs/reports/s46/guidance.md`.
   Operator: land or shelve that work, then re-run the preview.
3. No human approval exists for table fingerprint `sha256:19b7…fe40`.
   Operator: review the preview table and approve via
   `tusker docs adopt --table <file> --approve --by human:<name>`.
4. Wave `W-0040` authorization is `disarmed` and this task is `backlog`/`held`;
   approval path (3) additionally requires wave/task execution authority.
   Operator: authorize through `tusker wave start` / normal task entry.

## Redirects, history, unresolved exceptions

- Redirects: none created (no moves applied). Legacy `.tusker/specs/` paths
  still resolve through the compatibility reader.
- History preserved: no tracker file hand-edited (contract YAML, CAS bindings
  and proof untouched); no evidence or events rewritten.
- Unresolved: duplicate-subject merges (`CANON.md`/`INDEX.md` vs portable
  tree; 57 duplicate-subject rows repo-wide); `draft`/`planned` lifecycle
  dispositions for `spec-to-proof.md` and `tusker-trust-and-efficiency.md`;
  full `docs/system/INDEX.md` re-pointing after a future apply; skill-pointer
  updates deferred to supported authoring routes after guidance settles.
- 5. Adopt focused-test suite does not complete in this environment (killed
  after 11m0s, twice, on the dirty tree); re-run the single relevant test
  binary with a longer budget on a clean tree after actions 1–4 close.

## Focused-test note

No product code was changed by this receipt-only step, so no new focused test
was added. The existing adopt-suite run (`go test ./cmd/tusker -run
'TestDocsAdopt|TestAdopt' -count=1`) was attempted twice against the dirty
working tree: the suite binary was killed after 11m0s (`Test killed with
quit: ran too long`), package `tusker/cmd/tusker` FAIL after 660s. This is an
environment/suite-duration finding, not an adoption regression — no code was
changed here. See unresolved item 5.

## Repeat-run outcome

- Preview re-run returns the identical fingerprint
  `sha256:19b739603bf32e14c98a68918e4a6ab33d1f739ca44927e6650de5ed9fe40079`:
  read-only step is idempotent. Apply remains blocked until actions 1–4 close.
