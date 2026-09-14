# Recovered partial work for W-0023

The architect prematurely implemented these bounded changes, then reverted them after a scope correction. The user asked that completed effort be preserved for the assigned executors. These patches recover that prior work; they are not applied to the active product and are not accepted completion.

| Patch | Owner | What was already written | Still required |
|---|---|---|---|
| [admission.patch](admission.patch) | FLW-T-0037 | Per-member execute/review inspection in wave preflight; reuse of the existing Markdown cell parser; two regression cases. | Full start-path parity, route failure matrix, malformed-workflow diagnostics, independent review. |
| [metadata.patch](metadata.patch) | FLW-T-0038 | Empty optional fields omitted for new CLI/import records only; three regression cases with explicit zero-cap presence assertions. | Full intake classification, human gates, governing anchors, wave brief, help and compatibility acceptance. |
| [guidance.patch](guidance.patch) | FLW-T-0038 | Prior proposed HANDOFF/TRACK guidance for tiers, contacts, self claims, route checks and batch waves. | Reconcile other skill owners; distinguish supported behavior from planned behavior; verify canonical and shipped guidance together. |

## Provenance and validation limits

Admission was recovered by its original author from the preceding work. Metadata source changes were recovered from the exact reviewed diff; test content came from retained command output, including the root's three explicit zero-cap assertions. Guidance is the complete retained Git diff. No new product behavior was developed during recovery.

Before the reversal, the combined focused selection reported 25 passing tests and the candidate binary built. That is historical evidence for the then-current dirty checkout, not a fresh validation of applying these files elsewhere and not proof of full ticket acceptance. The two admission regressions and eight existing wave-preflight cases passed in that work. The production checkout still had malformed/incompatible workflow configuration: per-task route inspection could not be populated and only generic setup blockers were returned there. The admission owner must cover that limitation.

Inspect the patches and the current shared checkout first. `git apply --check <patch>` is a non-mutating applicability check; its success is not a correctness check. Apply through the executor's normal owned workspace, rebase as needed, and run the ticket's substantive checks. Do not overwrite another agent's changes or close a task merely because a recovered patch applies. The prior candidate binary is a local scratch build, not an installed or releasable artifact.
