# Software-factory packet fidelity

## Scope

FLW-T-0051 qualifies the existing direct-authoring path without adding
mandatory task metadata, a packet registry, or another delivery mechanism.
The test starts at the supported CLI entry points, reads the durable V7 task,
renders the existing worker and reviewer packets, and reads the existing Serve
task projection consumed by the UI. File and stdin authoring use a compiled
local Tusker binary in a subprocess; packet rendering is source-level through
the existing renderer, and the API assertion uses the existing `httptest`
Serve boundary. This is not browser automation.

## Preserved specimen

The file-input specimen preserves this exact body material:

```markdown
Preserve `literal | pipe` and \*escaped\* markdown.
```

Its verification row preserves this exact command in the durable body and
worker/reviewer packets:

```text
command: go test ./cmd/tusker -run 'TestSoftwareFactoryPacket' -count=1 \| tee packet.log
```

The Serve projection exposes the same command semantically as:

```text
command: go test ./cmd/tusker -run 'TestSoftwareFactoryPacket' -count=1 | tee packet.log
```

The escaped Markdown table delimiter is storage syntax; the API projection
returns the command value after table parsing. The body itself remains
durable, including the original escape.

The stdin specimen also retains its interface literal:

```text
The stdin body keeps a `consumer -> provider` interface literal.
```

## Qualification results

Command:

```text
GOCACHE=/private/tmp/tusker-w0027-packet-exact2-gocache TUSKER_VALIDATION_LOCK_DIR=/private/tmp/tusker-w0027-packet-exact2-locks go test ./cmd/tusker -run '^TestSoftwareFactoryPacket' -count=1 -v
```

Result: **PASS — 1 focused root matched, 4 substantive subtests passed, 0
failed, 25.709s.**

The cases cover:

- file and stdin authoring through the compiled local CLI process;
- exact durable body, command, and stdin literals retained in both worker and
  reviewer packets;
- acceptance, intent, and verification projection through the existing API
  task detail boundary consumed by the UI;
- unresolved spec references rejected before publication;
- valid wave/task `spec_refs` persisted and read back as
  `.tusker/specs/delivery.md`;
- invalid structured acceptance mapping rejected by the supported `verify add`
  surface before task/event mutation;
- invalid direct-wave `human_actions.covers` rejected before any task, wave,
  gate, or event write;
- request-key replay conflict with no task, wave, receipt, or duplicate event;
- injected multi-record write failure after the staged human-action gate write,
  using the existing deterministic `fail-after-write-count` hook, with no
  task, wave, gate, receipt, or event residue; and
- help, parser/dispatch, and capability-manifest parity exercised through real
  command behavior;
- a cold-reader semantic specimen containing explicit cases, a provider /
  consumer boundary, locked versus proposed decisions, exact shared context,
  and an exact check.

## Limits

This is local/source and disposable-fixture evidence. The compiled subprocess
is built from the current checkout; it is not an installed-binary qualification.
The Serve assertion is `httptest`, not a browser-rendering or installed-UI
claim. The suite does not qualify provider dispatch, receiver execution,
daemon pickup, or human acceptance. A packet or queued request proves neither
Save-triggered execution nor downstream integration. Unknown evidence remains
pending; no live endpoint, profile, provider budget, lifecycle state, or
mandatory metadata was added or changed.
