---
kind: decision
subject: event-driven-status-refresh
keywords: [resource utilization, polling, reconciliation]
part_of: orchestration
describes: [cmd/tusker/daemon.go, cmd/tusker/adaptive_reconcile.go, cmd/tusker/serve_runtime_cache.go, internal/serve/ui/src/lib/stream.ts, apps/mac/TuskerBar/Sources/TuskerBar/SSE.swift]
status: accepted
decides_for: orchestration
code_conformance: unverified
created: 2026-10-15
read_when: "Changing status notifications, fallback scans, or client refresh behavior."
skip_when: "You only need task authoring or proof mechanics."
---

# CLI-driven status updates with periodic reconciliation

CLI mutations are the normal source of status changes. They wake the daemon
immediately, coalescing duplicate project notifications without losing a change
that arrives during processing. A shallow notification supersedes precise
record hints and requests a full refresh.

Full project reconciliation remains a recovery mechanism for missed events and
external edits, with a five-minute default. Frequent notifications must not
postpone that deadline indefinitely. Startup and explicit refreshes read the
full current state. Active-run health checks remain at five seconds, with their
existing watchdog and ownership guarantees.

Runtime-only events update runs, questions, and needs without rebuilding notes
and documentation. Clients coalesce refresh work, pause hidden-view fetching,
and reconcile on resume or stream replay misses. External edits can appear at
the next fallback; mutation and dispatch authorization always revalidate current
material. A cached control projection is never mutation authority.

Log processing and hashing use bounded reads and incremental state. Caches have
defined operation, request, attempt, or project lifetimes. Replacement,
truncation, restart, and partial write failures must preserve evidence integrity.
Truncated authoritative runner output cannot certify success. An arbitrary
shell command cannot reuse another task's pass based only on a repository hash,
because its inputs can include tracker state and external files.
