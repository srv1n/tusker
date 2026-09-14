# Task authoring identity and self implementation

## Contract

Task authoring records one optional `authoring_provenance` object in task data:

```yaml
authoring_provenance:
  source: codex|claude|unknown
  conversation_id: <host supplied id, optional>
  host: <host supplied id, optional>
  captured_at: <RFC3339 timestamp>
  binding_state: unbound|bound
```

`CaptureTaskAuthoringProvenance` is the schema/import hook. It is idempotent
and capture-once: an existing valid object is preserved on retry or import,
even when the importer is a different conversation. A missing host identity is
recorded as `unknown` and `unbound`; Tusker does not derive an identity from a
task or execution ID. `CODEX_SESSION_ID` is preferred, with
`CODEX_THREAD_ID` as the existing host fallback. These environment values are
descriptive host provenance only and do not authorize delivery.

Authoring provenance, contacts and execution identity have separate owners:

| Fact | Authority | Meaning |
| --- | --- | --- |
| Original author conversation/host | Task `authoring_provenance` | Who authored the task, if the host supplied it |
| Architect/origin role and generation | `agent_contacts` | Stable logical role address and replacement CAS |
| Native provider endpoint | `execution_records` plus attachment events | Provider/session identity after supported registration |
| Current owner and actual executor | `runs`, `run_authorizations`, `attempts` | Lease, actor, attempt and review parent |
| Workspace/revision identity | `run_identity_metadata` | Repository, workspace, branch and source head |

Do not add `native_thread_id`, `architect_thread_id` or `is_architect` to each
ticket. A native conversation can author a ticket and later implement it, but
those are separate facts. The self implementation claim is stored in the
existing run authorization `trigger` as `self_implementation;source=...`, with
the actor and attempt still recorded by the run authority.

## Contact binding

`ResolveAgentContactBinding` reads the current role/generation contact and
returns an explicit `bound` or `unbound` projection. A task address is shown as
a logical continuation route only after its target task is verified in the
same registered project; it remains `unbound` until a live session is resolved.
An execution address is bound only when the execution exists in the project,
has an attached provider session, has a live matching task attempt, and uses a
supported Codex route. Missing, stale, replaced, cross-project and unsupported
endpoints remain unbound with a reason. `ExecutionProvider` projects provider
identity from the immutable record or the append-only attachment authority.

`TaskAuthoringIdentityForTask` combines the task provenance and those effective
contact projections for inspectors. It is read-only and does not register,
rewrite or wake an endpoint.

## Explicit current-workspace implementation

`tusker work start <task-id> --current-workspace` is an explicit
Codex/Claude self-implementation claim. It requires the existing interactive
host marker, checks that the process cwd is exactly the registered Git
repository root on a named branch, and serializes the physical checkout with
the existing project ownership fence. It deliberately skips
`WorkspaceManager.Prepare` so the already-open dirty tree is not replaced by a
new empty worktree. The default `work start` path keeps configured workspace
isolation.

The packet exposes `implementation_mode` (`current_conversation` or
`isolated_workspace`), the actual implementation actor and any captured task
provenance. Independent review remains mandatory. When a self-implementation
trigger contains a known native conversation, review from that same native
conversation is refused even if the reviewer changes `--by`; legacy claims
without a known native session retain the existing compatibility behavior.

## Focused verification

Run from the repository checkout:

```sh
go test ./cmd/tusker -run '^TestTaskAuthoringIdentity' -count=1 -v
```

The focused cases cover capture-once preservation across a different importer,
trusted environment selection, explicit same-conversation review refusal,
missing execution contacts remaining unbound, execution provider projection,
and repository-root validation for current-workspace claims. Provider live
delivery and cross-process wake qualification remain runtime evidence gates;
these helpers do not claim that evidence.

