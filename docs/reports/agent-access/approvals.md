# Agent-access approval lifecycle

Status: implemented backend contract for AAC-T-0004.

Tusker stores one approval row for one native callback. The identity includes
the project/task, attempt, execution, provider session, native request ID,
route, policy fingerprint, tool, exact argument digest, working directory,
targets, and the offered native option. The raw argument payload is not
stored. The Serve projection carries only redactedArguments and argsDigest,
so the response cannot be replayed from the database.

## State and binding

New callbacks use CreateAgentAccessApproval. The request is pending at
revision 1. Re-delivery with the same request ID and binding is idempotent;
the same ID with changed arguments, route, policy, option, or lifecycle
identity is rejected. SettleAgentAccessApproval accepts only allow_once or
deny, requires the expected revision, and requires an explicit human:<name>
actor. A worker identity cannot settle a request. Successful settlement
increments the revision and records the actor and time.

Repeated delivery of the same terminal decision returns the durable terminal
row; a conflicting decision fails. The native option is intentionally
persisted as an exact option ID and kind. There is no allow-all or
session-wide grant. The runtime store does not retain or execute a shell
command.

## Recovery matrix

| Event | Durable result | Reuse |
| --- | --- | --- |
| Duplicate callback, same binding | One pending row | Existing request ID |
| Changed args/identity/policy under same ID | Rejected | None |
| Matching human allow-once | allowed, revision +1 | One native callback |
| Human block/deny | denied, revision +1 | Native protocol may continue |
| Deadline elapsed | expired, revision +1 | New native request only |
| Provider cancellation | cancelled, revision +1 | New native request only |
| Tusker restart without proof of live callback | expired, revision +1 | New attempt/request only |
| Stale or conflicting response | Rejected | Refresh projection |
| Worker response | Rejected | Human operator required |
| Unsupported/headless callback | Native adapter must deny | No synthetic approval |

Opening the runtime store does not imply a restart and leaves pending requests
unchanged. The resident daemon calls `ReconcileAgentAccessApprovals` at its
process-start boundary and checks the in-process live-handle registry; an
absent or mismatched attempt handle fails closed by expiring the request. No
stored command is replayed after restart or resume.

## Authenticated Serve API

The existing loopback capability and configured Serve operator protect the
mutation. Reads use the existing task/request projections:

    GET /api/approvals?project=tusker&task=AAC-T-0004
    GET /api/approvals/<request-id>?project=tusker

The response is:

    {
      "approvals": [{
        "requestId": "approval-1",
        "attemptId": "attempt-1",
        "sessionId": "session-1",
        "nativeRequestId": "native-approval-1",
        "route": "codex_cli",
        "policyFingerprint": "sha256:...",
        "tool": "shell",
        "argsDigest": "sha256:...",
        "redactedArguments": {"command": "git clean -fd"},
        "workingDirectory": "/workspace",
        "targets": ["/workspace"],
        "nativeOptionId": "allow-once-option",
        "nativeOptionKind": "allow_once",
        "state": "pending",
        "stateRevision": 1,
        "expiresAt": "2026-09-11T07:00:00Z"
      }]
    }

An operator responds through either equivalent mutation:

    POST /api/approvals/<request-id>/allow-once
    POST /api/approvals/<request-id>/deny

    {"projectId":"tusker","expectedRevision":1,"actor":"human:operator"}

The response returns the settled projection. A stale revision, expired
request, wrong project, conflicting decision, or unauthenticated actor is a
failed-closed response and leaves no broader permission.

## Verification

    go test ./cmd/tusker -run 'TestAgentAccessApproval|TestDaemonStartupReconciliation' -count=1

The focused tests cover duplicate callback idempotency, changed payload
rejection, worker rejection, exact allow-once/deny behavior, stale/conflicting
responses, secondary-store preservation, authoritative daemon-start expiry,
live-handle preservation, and fresh-attempt isolation.
