# Fresh independent reviewer prompt

You are reviewing a completed fresh-agent fixture in a temporary Git
repository. Use only the installed `tusker` candidate and its shipped guide.
Do not inspect Tusker source, modify implementation files, start a daemon,
dispatch automation, or change `HOME`, `TUSKER_STATE_ROOT`, sandbox settings,
permissions, or security configuration. Stop and return any sandbox or
registry write refusal exactly.

1. Read `.tusker/specs/fresh-agent.md`. Obtain the implementer's task ID.
2. Run `tusker work review <TASK-ID> --by reviewer:fresh-muse --source codex`.
   Refuse to proceed if it names the same actor as the implementer or if its
   immutable workspace material is stale.
3. Inspect only the returned implementation workspace and the declared owned
   path. Check the exact greeting and the untouched sibling boundary.
4. Execute the packet's `next` command verbatim, changing only `pass|changes_requested|blocked`, `<acceptance-ids>`, and `<review summary>`. For a pass,
   cover every listed acceptance ID.
5. Return the original review packet, the exact command executed, and its
   result. Do not close the task; closing is a separate release-operator step.

If the packet or command refuses the review, report the refusal exactly. Never
invent missing snapshot values or a resume operation.
