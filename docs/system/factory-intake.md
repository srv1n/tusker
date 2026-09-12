---
title: "Factory intake"
subject: factory-intake
part_of: overview
status: canonical
---

# Factory intake

Use factory intake to turn a reviewed request into work that another person or
agent can inspect before anyone starts it. Intake prepares a delivery plan. It
does not execute tasks.

## Prepare the request

Name these facts before you create the plan:

- the product outcome;
- the source requirements;
- allowed and excluded paths;
- acceptance row IDs;
- exact verification commands;
- task dependencies;
- shared resources and path overlaps; and
- decisions that still need a person.

Do not invent a command, owner, or decision. Keep an unknown fact visible.
Use a gate only when an agent cannot supply the fact.

The result is a delivery plan candidate. Check the candidate against the
request, then follow [Delivery and waves](delivery-and-waves.md) to validate,
review, import, start, and authorize it.

## Know what the result means

Validation checks the candidate's fields and dependency graph. Review checks
that its tasks preserve the request. Import writes held task and wave records
after the import checks pass. A failed import leaves the tracker unchanged.

Preparation stops here. The operator must still confirm the current plan
identity and authorize its wave. Intake, validation, review, and import do not
enable automation, start a task, or dispatch a runner.

If a required fact is missing, keep it unknown or add the gate that owns it.
Do not guess and continue. The supported next action is to obtain the fact,
then validate the same candidate again.

## Code sources

- `cmd/tusker/delivery_context_cmd.go`
- `cmd/tusker/delivery_cmd.go`
- `cmd/tusker/delivery_v2.go`
- `cmd/tusker/delivery_review_cmd.go`
- `cmd/tusker/serve_delivery.go`
