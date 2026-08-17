# Plan

Use this guide to turn a request into the smallest useful Tusker record.

## Intake

Capture the desired outcome, observable acceptance, important failure cases,
constraints, non-goals, and genuinely unresolved decisions. Do not turn every
conversation into a task. One bounded outcome is one task; use an epic only
when several real tasks share a product outcome.

## Create records through the CLI

```bash
tusker new epic --vault ./.tusker --acronym APP --title "App foundation"
tusker new task --vault ./.tusker --epic APP --title "Implement auth" \
  --status backlog --priority p2 --size m --risk medium
tusker new gate --vault ./.tusker --blocks APP-T-0001 --kind auth \
  --owner human:<name> --action "Provision credentials." \
  --verification "Provider readiness check passes."
```

Use dependencies only when one task truly cannot be accepted before another.
Use gates only for missing human authority, credentials, unresolved product
intent, or contractually subjective acceptance. Risk alone is not a gate.

## Shape the contract

Each task needs a concrete outcome and acceptance that can be observed. Add
the smallest verification that proves each acceptance item. Unknown commands,
ownership, credentials, and product choices remain open questions or gates;
do not fabricate proof or placeholders.

Use `tusker show <TASK-ID> --capsule` and `--acceptance` to verify the result.
If the CLI cannot express the requested record, report the exact mismatch and
stop changing tracker state. Do not hand-edit protected fields.
