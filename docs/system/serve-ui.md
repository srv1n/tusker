---
title: Serve UI and local control plane
subject: serve-ui
part_of: overview
status: canonical
read_when:
  - operating the local Tusker Serve UI or TuskerBar
  - wiring a Serve endpoint or UI mutation
---

# Serve UI and local control plane

`tusker serve` is a privileged, localhost-only JSON API plus embedded SPA. It
is not a passive dashboard: task lifecycle, daemon, gate, evidence, project,
execution, delivery, and document actions can change durable state. The daemon
and vault are authoritative; the browser is a client and must verify mutation
readback.

## Security boundary

The server binds loopback and admits mutations only with same-origin/loopback
checks, JSON content type, and the per-process `X-Tusker-Capability` returned by
`GET /api/capability`. This reduces drive-by cross-site requests but is not
authentication. A local process that can read the endpoint can use the control
plane. Responses include CSP, frame denial, MIME sniffing, and referrer-policy
headers.

`GET /api/capabilities` is the machine-readable compatibility registry. It
classifies each surface as `authoritative_mutable`, `authoritative_read_only`,
`cached_projection`, `local_preference`, or `unavailable`; the UI uses it to
avoid presenting unsupported controls as live functionality.

## Truth and freshness

Reads are snapshots/projections of the vault and runtime store. Stream events
are hints that invalidate queries, not a replayable audit log. During disconnect,
the UI falls back to bounded polling and must show disconnected/freshness state;
an event that was not observed is unknown, not proof of absence.

Mutation responses have four distinct outcomes:

1. accepted — the daemon reports success; the UI invalidates and reads back;
2. refused/validation — often a 2xx `{refused:true}` or `{ok:false}` with a
   reason; the UI shows the reason and must not mark the action complete;
3. conflict — HTTP 409/422 typed payloads such as CAS document conflicts;
4. transport — network, timeout, or 5xx failure; retry only after showing the
   operator that no durable outcome is known.

## Execution Operations

Execution views expose bounded projections of runner identity, attempt state,
lease ownership, process status, and redacted logs. They are diagnostic
surfaces, not an alternate authority: operators must reconcile any action or
status with the daemon/runtime read APIs, and an absent event or stale snapshot
is never proof that execution stopped or completed.

## Native shell

TuskerBar supervises the bundled daemon for the default local endpoint, exposes
the same-origin UI in WebKit, and listens to SSE for notifications/badge refresh.
Custom non-local URLs are external mode. Closing the main window leaves the
menu-bar resident; quitting exits the shell. Native notifications and Spotlight
are advisory projections and must be reconciled with `/api/summary`/read APIs.

## Unsupported/reference-only surfaces

Legacy document editing and several app settings still have no durable Serve
contract. They are intentionally disabled or labeled reference/local. Do not
describe them as persisted until an endpoint and readback test exist. See
`internal/serve/ui/BACKEND-GAPS.md` and `GET /api/capabilities` for the current
registry and remaining work.
