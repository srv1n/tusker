# W-0027 team handoff

Created September 14 through the repository-local `./tusker` CLI. Inert;
no work started, profiles changed, installs performed or provider budget spent.
Canonical contract: `.tusker/specs/direct-wave-authoring.md`, software-factory
acceptance extension. Read current task packets, not the scratch batch input;
subsequent CLI amendments corrected verification commands and added dependencies.

| Task | Work / review | Depends on |
|---|---|---|
| FLW-T-0051 Packet fidelity and guidance | Standard / Demanding | FLW-T-0049 |
| FLW-T-0052 Exact-material proof and finding closure | Demanding / Demanding | FLW-T-0049 |
| FLW-T-0053 Bounded repair and restart recovery | Demanding / Demanding | FLW-T-0052 |
| FLW-T-0054 Installed real-loop qualification | Standard / Demanding | FLW-T-0051, FLW-T-0053; FLW-G-0002 |

The current W-0025 team retains its final independent review, corrections and
honest lifecycle closeout. Its final-handoff.md reports all eight implemented,
but local/fixture verification is not installed/provider qualification. Do not
re-run migration or restore deleted delivery-plan paths. New tasks consume the
accepted result. Proof owner publishes shared review/receipt changes before
runtime consumer starts. Check actual shared-file ownership before parallel work.

## Blocking authoring defects to return to the finishing team

1. `wave create --file` rejects an existing durable task ID in dependencies as
   DEPENDENCY_DANGLING; current decoder accepts only batch-local keys.
2. Supported `task update --dependencies FLW-T-0049:hard` succeeds but wave
   review then rejects it: cross-wave dependency requires a dependency_contract
   entry. Roots 0051/0052 currently have these real edges but lack that binding.
   Supply a supported CLI operation to bind the producer contract; preserve
   the edges and do not hand-edit tracker metadata or remove readiness checks.
3. Literal regex alternation in a Markdown verification cell was truncated at
   the pipe in wave projection. New task commands now use single-prefix regexes
   and read back correctly. Preserve this as a packet-fidelity regression
   specimen; investigate supported escaping before calling it a parser defect.

These findings came from actual authoring/readback, not a new code review.
No product fixes were applied. The existing edge-binding gap prevents calling
this wave runnable even after the producer closes. Return it with the current
team's cutover fixes; do not weaken dependency integrity to unblock execution.

## Other launch prerequisites

Wave review reports empty Standard/Demanding execute mappings in this context.
Coordinator must inspect effective routes; no route or provider was selected
here. Installed PATH CLI remains older than repository-local binary; use the
verified local CLI for inspection until an authorized installation occurs.

FLW-G-0002 requires Sarav's explicit pilot authorization with exact routes,
disposable workspace, independently running runtime owner, permitted mutations
and cumulative attempt/time/spending bounds. Earlier tasks need no live pilot.
No daemon may be launched from an interactive agent session.

## Coordinator prompt

Pick up W-0027 from current packets and this handoff after W-0025's final
independent review and closeout. First resolve the supported cross-wave binding
gap with the finishing owner, without deleting dependencies or fabricating
proof. Then assign 0051 and 0052 independently, 0053 after 0052, and 0054 after
both branches and its explicit campaign gate. Preserve shared edits, configured
routes and exact proof boundaries. Record existing passing behavior rather than
rebuild it. Return accepted results or precise unresolved findings; never turn
a missing live capability into PASS.
