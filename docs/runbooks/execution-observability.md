---
title: "Execution observability operations runbook"
subject: execution-observability-runbook
keywords: [executions, migration, restart, cursors, cancellation, provider]
status: canonical
read_when: "You are recovering, migrating, or diagnosing execution observations."
skip_when: "You only need normal task dispatch or generic daemon operation."
---

# Execution observability operations runbook

This runbook recovers observation without granting it delivery authority. Do not solve missing provider visibility by creating leases, changing task state, or binding old history to a task.

## Backfill and compatibility

Open the runtime store with the current Tusker binary; migrations create and validate execution records, typed lineage edges, provider observations, and timeline sources transactionally. Existing attempt lineage is preserved as immutable provenance. A repeated backfill must be equivalent, not silently overwrite conflicting execution, edge, or ownership data.

Before and after an upgrade, run the focused execution migration/ledger tests. If migration reports a conflicting historical row, stop the upgrade and retain the database for diagnosis; do not delete rows or rebuild from provider data. SQLite lease owner/generation and PID/PGID/start-time fences remain the source of ownership truth.

## Restart and cursor recovery

After a daemon, Serve, browser, or subscriber restart, fetch the execution or wave timeline from the last returned cursor. If the response says `reset`, `gap`, or `stale_cursor`, discard that cursor and fetch `direction=tail`. Continue authoritative fetch until the returned checkpoint reaches `committed_tail`. `has_older` and `has_newer` mean there is more history to fetch; neither is permission to infer a missing event.

Provider cursor regression, unavailable replay, or an incomplete child hook is partial visibility. Persist the diagnostic and use an adapter's authoritative enumeration/fetch when available. Do not turn parent completion into child completion: a parent can finish while children remain partial, lost, or still observable.

## Binding conflicts and unbound work

Start with `tusker execution inbox --json` and inspect the execution graph. Rename freely for recognition, but bind only after confirming the intended task and its canonical wave. A bind preview/UI check catches task-wave disagreement and a conflicting live owner. Resolve the real ownership conflict first; never detach a live lease just to make a direct execution fit.

Binding, rebind, and detach create generations. Earlier unbound or prior-bound observations stay in the ledger but cannot become proof, review, landing, close, release, or spending authority.

## Cancellation settlement

Request cancellation through `tusker execution cancel --id exec_… --json` or the Serve control only when it is advertised as available. Record separately:

1. provider acknowledgement (if a target-specific adapter confirms it); and
2. operating-system settlement for local work, fenced by PID/PGID/start time.

An acknowledged provider request is not local process death. A dead local process is not provider acknowledgement. For cloud work, no local process settlement exists; preserve provider evidence and mark process evidence absent.

## Bounded disable/rollback response

If an adapter is emitting malformed, duplicate, or misleading observations, disable **observation ingestion for that adapter/source only** and preserve the durable ledger. Existing claims, leases, task state, and ownership fences remain unchanged. Continue normal task safety through canonical runtime state. Restore ingestion only after bounded replay/idempotency and authoritative-fetch checks pass. Do not delete the execution graph, promote provider facts to ownership, or use rollback to erase an audit trail.

