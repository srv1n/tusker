# Agent Improvement Scan Product Story - 2026-05-25

Source:

- User request on 2026-05-25 adapting an OpenAI-style prompt for mining recent work history and creating the smallest useful skill, subagent, or automation.

## Story AFS-014 - Add Opt-In Agent Improvement Scans

Priority: P1

As a Tusker user who repeatedly works with Codex, Claude Code, and repo-local task history, I need an opt-in improvement scan that finds repeated manual workflows and proposes or creates the smallest useful reusable asset, so Tusker gets better with use without turning every run into an expensive prompt archaeology job.

Acceptance criteria:

- The scan is opt-in and bounded by a lookback window, defaulting to the last 30 days or all available history if shorter.
- Evidence priority is Tusker-first: recent task summaries, attempts, proof, feedback notes/digests, existing skills, agent docs, and automations before optional external sources.
- Optional sources such as Codex sessions, Claude Code transcripts, Memories, Chronicle, or equivalent provider history require explicit enablement and are used for discovery only unless the relevant source system can confirm details.
- The scan emits a compact shortlist with repeated workflow, evidence and dates, frequency/confidence, recommended form, and packaging rationale.
- Candidate rules are strict: repeated at least twice or clearly costly to repeat; stable inputs; repeatable procedure; clear output or stopping condition; material improvement; not already adequately covered.
- Recommended forms are constrained to skill/playbook, custom subagent, automation, extend existing, or skip.
- Apply mode creates only high-confidence missing items and prefers extending existing assets over duplicates.
- Users can select a runner/profile for the scan. Cheap discovery profiles should be supported, with stronger models reserved for apply or review when configured.
- The final report lists created or extended assets, deliberately skipped candidates, and candidates needing more evidence.

Implementation notes:

- The likely product shape is `tusker improve scan` or `tusker feedback improve`, with `--dry-run` as the default and `--apply` as an explicit step.
- Do not store provider names, model choices, or token-heavy transcript excerpts in task files. Keep those in runtime/config surfaces and record only concise evidence.
- The first useful slice can be Tusker-only: mine task titles, attempts, feedback notes, verification rows, and installed skill/doc inventory before adding Codex/Claude/Chronicle adapters.

Tracked by:

- `VSD-T-0032`
