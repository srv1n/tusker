# Fresh implementer prompt

You are operating in a new temporary Git repository. Use only the installed
`tusker` candidate and the repository's shipped Tusker guide. Do not inspect
Tusker source, use private/internal commands, start a daemon, dispatch
automation, or claim authority you were not given. Never change `HOME`,
`TUSKER_STATE_ROOT`, sandbox settings, permissions, or security configuration.
If any command reports a sandbox or registry write refusal, stop and return
that exact refusal.

1. Read `.tusker/specs/fresh-agent.md` and `delivery.yaml`.
2. The release operator has already initialized and imported the pinned V2
   plan. Read its capsule and use the exact task ID it reports. Register this
   temporary project with `tusker projects add --repo . --vault ./.tusker` only
   if it is not already registered; do not enable automation.
3. Report the imported DAG and the exact task ID for `greeting`. Ensure the
   task declares `owned/greeting.txt`; do not edit the sibling task. Make the
   ordinary direct task ready with `tusker status <TASK-ID> ready --by
   agent:fresh-muse --reason "Ready for direct interactive work"`. This does
   not arm a delivery wave or authorize a daemon.
4. Start the greeting through `tusker work start <TASK-ID> --by
   agent:fresh-muse --source codex`. Record its JSON packet. Deliberately fail
   that first session with `tusker work fail <TASK-ID> --by agent:fresh-muse
   --reason "fixture interruption"` before writing any implementation.
5. Recover only by starting a fresh work session for the same task. In the
   returned workspace write `owned/greeting.txt` with exactly `hello from a
   fresh agent` and a newline. This fixture uses its checked-out shared
   workspace; keep the scoped material there and do not create a branch or
   commit just for this exercise. Submit with `tusker work submit` and the task ID, owner, concise
   deliverable, verification, and `A1=pass` gate verdict.
6. Return the task ID, recovered submit packet/output, workspace path, current
   Git head, and a concise account of the failed and recovered attempts. Stop
   before review or close.

If a command refuses an action, return the exact refusal and leave the tracker
unchanged. Do not forge a review, approval, proof, or daemon receipt.
