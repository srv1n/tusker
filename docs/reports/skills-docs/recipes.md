# Worker verification and knowledge recipes

This exercise checks that a fresh reader can choose a bounded verification
route and answer three knowledge conflicts without inventing authority.

## Runnable verification recipe

Prerequisites: repository checkout, current `tusker` binary, Go toolchain, and
no edits outside the task's declared paths. This is a local source/test check;
it does not start automation, a daemon, a provider, or an installed app.

1. Read `tusker show FLW-T-0034 --capsule` and the acceptance table supplied in
   the task contract. If the packet is unavailable because the task is held,
   record that fact and continue only with explicit interactive assignment.
2. Read `skills/tusker/SKILL.md`, then only its `RUN.md`, `KNOWLEDGE.md`,
   `SPECS.md`, and `TRACK.md` routes required by this documentation task.
3. Confirm commands with `tusker capabilities --json` and targeted `--help`.
4. Run:

   ```sh
   go test ./cmd/tusker -run 'TestSkillTaskManagementProtocol|TestTuskerSkillProgressiveDisclosure' -count=1 -v
   ```

   Expected state: both named tests run and pass. Zero matched tests is not a
   pass. Preserve the exact command, exit code, test names, first actionable
   error, and checkout revision in the review record. This proves the focused
   skill protocol and progressive-disclosure regression checks only; prose
   meaning still needs review.
5. Inspect the diff for only:
   `skills/tusker/references/RUN.md`,
   `skills/tusker/references/KNOWLEDGE.md`, and this file. Record unrelated
   dirty paths without changing them.

Cleanup: none for the focused Go test. Go's temporary test directories are
test-owned. Do not remove caches, shared evidence, or concurrent edits.

## Fresh-reader knowledge cases

For each case, record every file and section read plus the answer and its
authority label.

### Stale history

Given an older discussion that conflicts with a current `docs/system/`
section, answer from the current section and label the discussion historical.
Use bounded decision/history reads only if the question asks why. Expected:
no old proposal is promoted to current behavior.

### Code/document disagreement

Given a current doc and its owning source with different behavior, report
`code/doc drift`. Describe the code as **inspected source**, not executed
proof. Update only an owned doc; otherwise return the defect to its owner.
Expected: no claim that inspection proves runtime behavior.

### Missing rationale

Search the exact subject with `tusker docs find <query>`, read the relevant
current section, then the matching decision section or backlinks if supported.
If no rationale is recorded, answer `rationale not in canon` and keep any
explanation inferred from code explicitly labeled as inference. Expected: no
invented decision or unnecessary full-history scan.

## Independent review

From a fresh bounded packet, follow the verification recipe and all three
cases. Check each command against current help/source, confirm the required
task conditions survived, and verify the references preserve the interactive
session versus resident-daemon boundary. A structural or test pass alone is
not approval of the writing.
