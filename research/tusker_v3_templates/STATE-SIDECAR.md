# Sidecar state

Use a hidden machine-state file for runtime and audit metadata.

```text
.tusker/state/<ID>.json
```

Example:

```json
{
  "id": "MEM-T-0012",
  "run_state": "running",
  "tool": "codex",
  "session_id": "abc123",
  "prs": ["https://github.com/org/repo/pull/42"],
  "evidence": [
    {"kind": "demo", "path": "evidence/MEM-T-0012/demo.mp4"},
    {"kind": "test-report", "path": "evidence/MEM-T-0012/tests.txt"}
  ],
  "transitions": [
    {"from": "draft", "to": "active", "at": "2026-04-29T10:00:00Z"}
  ],
  "attestation": {
    "by": "sarav",
    "role": "human",
    "at": "2026-04-29T12:00:00Z"
  }
}
```

## What belongs here

- daemon/runtime state
- PRs and run IDs
- evidence manifest
- retries and interrupts
- timestamps and transitions
- attestation/signoff bookkeeping

## What does not belong here

- problem statement
- acceptance criteria
- canon
- plan
- docs impact
- human checkpoints

Those belong in the markdown contract.
