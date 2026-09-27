---
title: "Landing and completion"
subject: landing-and-completion
part_of: overview
status: canonical
read_when: "Changing how reviewed work lands, how a task closes, or what receipts record."
skip_when: "You need authoring or review rules rather than the final merge and close transaction."
---

# Landing and completion

Landing integrates a reviewed change. Completion closes the work only after
the bound authority checks pass.

## Landing checks

The landing path checks the task, wave, source revision, task revision,
integration base, review result, proof fingerprint, gate fingerprint, and
landing authorization. A mismatch stops the operation.

Landing is serialized for an integration target. A merge conflict stays
visible. The system must not discard a user change to make the merge pass.

## Completion

The completion transaction records its phases in the runtime store. The final
close authority binds the review result and the reviewed work. The task close
event also carries the authority marker.

A zero process exit code is not completion authority. A passing review result
also does not close a task by itself.

## Receipts

Completion and landing receipts store the task, revisions, fingerprints,
actors, and result. They support later validation of the decision. They do not
replace Git history or repository task state.

## Code sources

- `cmd/tusker/v7_land_cmd.go`
- `cmd/tusker/v7_close_authority.go`
- `cmd/tusker/v7_completion_receipt.go`
- `cmd/tusker/completion_reactor.go`
- `cmd/tusker/runtime_store.go`
