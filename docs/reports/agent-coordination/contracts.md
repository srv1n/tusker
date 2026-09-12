# Agent coordination contract

Tusker stores logical contacts separately from dependency edges. Contacts are project-scoped `architect`, `origin`, or named `peer` references targeting a task owner or exact execution. Replacements use an expected generation and retain a predecessor ID; stale replacement is refused.

Messages have immutable IDs, caller idempotency keys, typed recipients, origin/revision fields, correlation, reply/yield intent, and separate transport, consumed, answered, and applied facts. A transport receipt never means read or applied. Task addresses may remain queued without a current owner; exact execution addresses never fall back to a fresh conversation.

Public surfaces: `tusker message send|ask|reply|list|show|consume|apply` and `/api/messages`. Bodies are structured arguments/JSON and capped at 32 KiB.

Verification: `go test ./cmd/tusker ./internal/runner -run 'TestAgentContacts|TestAgentMessages|TestAgentTransport' -count=1` — PASS.
