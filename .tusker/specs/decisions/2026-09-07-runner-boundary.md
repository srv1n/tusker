---
title: "Runner boundary decisions — 7 September 2026"
subject: runner-boundary-decisions
part_of: runner-execution-boundary
decides_for: runner-execution-boundary
status: canonical
read_when: "Understanding why harness onboarding and conformance have these boundaries."
skip_when: "Implementing the locked contract; read runner-execution-boundary."
sources: [.tusker/specs/runner-execution-boundary.md]
---

# Runner boundary decisions

The operator requested specifications and an amended Tusker task for a team to
implement; no product implementation or agent execution is authorized by this session.
The preceding recommendation supplied the boundary and the operator asked to spec it
out. No additional interview or approval round was required for that authorized work.

| Decision | Alternatives and recommendation | Operator evidence | Recorded contract |
| --- | --- | --- | --- |
| Deliverable | Spec and task handoff, or implement immediately; choose spec and handoff. | "I don't want you to build it" and "write the spec on the Tusker task". | Amend FLW-T-0030 and its canonical spec; keep W-0012 disarmed. |
| Reuse | Internal module or separate platform; recommend internal module first. | Wants a reusable module as part of the bigger spec and a first-class capability. | Execution module separated from task state; no separately distributed SDK in this delivery. |
| Onboarding | Promise arbitrary CLI interoperability or require an explicit supported dialect; recommend explicit dialect. | Wants onboarding to need almost no work for ACP or CLI. | Compatible ACP/existing CLI dialects use configuration only. New CLI semantics require an isolated adapter and shared tests. This is the technical limit of the requested end state, not a claim the operator asked for an adapter framework. |
| Done | Agent says done, or repeatable conformance and task proof; recommend the latter. | Wants clear tests and quick confidence when onboarding each harness. | Same conformance report/service in CLI and app, per host/preset; installed canary and verification required. |
| Containment | Approximate provider mode or fail closed; choose exact enforcement. | Supplied ruling explicitly rejects weakened sandbox/network/approval policy. | Unsupported combinations remain unavailable; handshake and tool approval alone are not containment proof. |
| Execution ownership | Worker closes task or Tusker closes task; choose Tusker. | Supplied ruling says workers must not modify Tusker state. | Runner returns execution facts; Tusker verifies and closes with crash recovery/fencing. |

The numerical defaults (15-second probe, 30-minute attempt, 2-second cleanup grace,
1-MiB frame, 10-MiB diagnostics, 24-hour live report validity) are specification defaults
chosen during drafting, not separately stated operator preferences. They are explicit
and testable; changing them must preserve the acceptance behavior and update this spec.
Mac-first certification follows the existing installed-app acceptance. Other hosts are
unverified until exercised. No test results or implementation completion are asserted.
