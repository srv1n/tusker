---
title: "Delivery and waves"
subject: delivery-and-waves
part_of: overview
status: canonical
---

# Delivery and waves

A delivery plan describes a set of tasks. A wave is the repository record that
groups the approved tasks and their landing data.

## Plan phases

1. Build or receive a plan file.
2. Validate the plan schema and source context.
3. Review the tasks, dependencies, gates, checks, and artifacts.
4. Import the plan into a held state.
5. Confirm the current plan identity before start.
6. Authorize the wave when its dispatch checks pass.

Plan, review, import, start, and authorization are separate operations. A plan
or a review does not dispatch work.

## Wave authority

A wave stores its members, source references, delivery fingerprint,
integration base, concurrency rules, shared resources, unresolved decisions,
and authorization fingerprint. A task, dependency, source, or policy change
can make the authorization stale.

The daemon uses the current project dispatch scope. With `armed_waves`, only an
authorized wave can supply unattended work.

## Landing

Each landing binds the reviewed task, source revision, integration base, and
proof facts. One wave landing must not silently overwrite another landing or a
user change.

## Code sources

- `cmd/tusker/delivery_v2.go`
- `cmd/tusker/delivery_review_cmd.go`
- `cmd/tusker/delivery_cmd.go`
- `cmd/tusker/v7_wave_authorization.go`
- `cmd/tusker/v7_land_cmd.go`
- `internal/v7schema/schema.go`
