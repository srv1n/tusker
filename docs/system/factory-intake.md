---
title: "Factory intake"
subject: factory-intake
part_of: overview
status: canonical
---

# Factory intake

Factory intake converts a reviewed request into a delivery plan candidate.

## Required facts

The intake must name:

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

## Candidate and authority

The generated plan is a candidate. Validation checks its fields and graph.
Review checks its meaning. Import writes task and wave records only after the
plan passes the import checks. A failed import must leave the tracker
unchanged.

The operator still needs to confirm start and wave authorization. Factory
intake alone cannot enable a project or dispatch a runner.

## Code sources

- `cmd/tusker/delivery_context_cmd.go`
- `cmd/tusker/delivery_cmd.go`
- `cmd/tusker/delivery_v2.go`
- `cmd/tusker/delivery_review_cmd.go`
- `cmd/tusker/serve_delivery.go`
