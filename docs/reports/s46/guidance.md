# S46 guidance walkthrough (TSK-T-0060)

Cold-reader walkthrough exercised in a disposable project at `/tmp/s46g-walk`
(fresh `tusker init --vault ./.tusker --yes --fresh --with-pointers
--with-contract --no-mount`). All commands below ran with
`--vault ./.tusker`; without a vault this host reports `CONFIG_INVALID`
(runtime registry unavailable), which is environment setup, not guidance.

## One folder tree

```text
docs/system/00-overview.md                 entry point
docs/system/domains/<domain>/00-index.md   domain purpose, reading order, chapters
docs/system/proposals/<subject>.md         one change specification
docs/system/decisions/<subject>.md         one durable product decision
.tusker/                                   tracker state and thin pointers only
```

## Four cold-reader questions and exact destinations

| Question | Command | Destination |
| --- | --- | --- |
| Where do I document current behavior? | `tusker docs new billing --domain billing --index` then `tusker docs new billing-overview --kind doc --domain billing` | `docs/system/domains/billing/billing-overview.md` (`kind: doc`, `status: current`) |
| Where do I propose a change? | `tusker docs new billing-cap --kind proposal` | `docs/system/proposals/billing-cap.md` (`status: proposed`, `updates: []`, `decisions_locked: false`) |
| Where do I record a decision? | `tusker docs new billing-model --kind decision --decides-for billing-cap` | `docs/system/decisions/billing-model.md` (`decides_for: billing-cap`) |
| Where do I migrate old knowledge? | `tusker docs adopt --migration --dry-run` (preview; then apply an approved, fingerprinted table) | Explicit per-row targets; originals stay in the `.tusker/_generated/docs` recovery journal |

After all four writes, `tusker docs check --json` reported
`{"valid":true,"document_count":5,"issues":[]}`. The migration preview
returned `ok:true` with a fingerprinted, read-only proposal table
(`applied:false`).

## Ambiguities found

1. A domain chapter requires its index first. Without
   `docs/system/domains/billing/00-index.md`, `docs new --kind doc --domain
   billing` refuses with `INVALID_FIELD` and names the exact recovery:
   `tusker docs new <subject> --domain billing --index`. Reusing the index
   subject for a chapter is refused with `ALREADY_EXISTS`; chapters need
   distinct subjects. Skill onboarding now states the index-first order.
2. Scaffolds carry front matter and a title but no body links. Plain-Markdown
   navigation without Tusker depends on author-maintained links in
   `00-overview.md` and each `00-index.md`; generated `INDEX.md`/`graph.json`
   are build outputs and must remain useful when absent.
3. `tusker docs check --json` on the Tusker repository itself currently
   reports two legacy lifecycle issues (`DOC_LIFECYCLE_INVALID` for
   `.tusker/specs/spec-to-proof.md` with `draft` and
   `.tusker/specs/tusker-trust-and-efficiency.md` with `planned`). These are
   pre-existing migration/adoption inventory, not guidance defects; the
   guidance contract test therefore validates the portable-tree fixture plus
   the shipped wording, not a green check of the unmigrated repository.
4. `--kind spec` remains accepted as the spelling for proposal; guidance and
   onboarding teach `--kind proposal` and name the compatibility spelling
   once.
