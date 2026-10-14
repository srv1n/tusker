# Coordinator batch mode

Use this route only when the user authorizes a coordinator batch. A batch
worker follows its assigned packet; it never starts another batch or daemon.
Ordinary interactive implementation still uses `tusker start` and `tusker done`.

1. Record the agreed spec and each ruling. Use `tusker new fork --epic <ACR>
   --id <slug> --question <text> --ruling <text> --by coordinator [--doc <ref>]`.
2. Create tasks with acceptance IDs, exact check commands, owned paths,
   dependencies, an explicit `--runner-profile <name>` (or execute profile),
   `--resources <name=units,...>` and `--budget <duration>`. The global profile
   must pin harness, model and effort. Batch mode bypasses work/review levels
   as execution routing; those fields remain available for ordinary work.
3. Inspect each `tusker show <TASK-ID> --packet worker`. Repair dead references
   before creation/start. Use `--routing` only when extra context is needed.
4. Create a batch with `tusker new batch --name <name> --epic <ACR>
   --tasks <ids> [--max-parallel N]`. This reserves tasks without executing them.
5. Start `tusker start <name>` through the harness's background-process
   facility. Use process completion as the notification, or `--notify <cmd>`
   as the alternative. The managed runner owns scheduling and mechanical
   recovery. Do not launch ad hoc `codex exec` or `claude -p` workers yourself.
6. On completion, read the digest. Review the named branches and evidence;
   `review` means report and audit passed, not human acceptance. Close with
   `tusker review <TASK-ID> --verdict accept|accept-with-followups|reject --note
   <review>`. A reasoned `--override-audit` cannot override an explicit human
   gate. Integration into the original checkout and pushing remain separate.
7. For a novel question, add the ruling and run the batch again. Completed work
   is retained. At session start, use `tusker digest --since last`.

`tusker show <name> --json` and `tusker audit <task|batch>` inspect evidence.
`tusker stop <name> --drain` stops admission;
without drain it also stops active workers. Never remove live locks or edit
batch state to make a retry happen. Contract/profile drift requires a new batch.

Workers write versioned REPORT.json and REPORT.md under TUSKER_BATCH_RUN_DIR,
never in committed scratch. A valid remaining item must include title, why and
evidence; the runner harvests it into backlog even when work parks or fails.
Only exact active fork matches and unexpired flaky entries authorize mechanical
recovery. A checkpoint never releases downstream work.

For the report schema, resource capacity, runtime layout, ledger and reviewed
triage formats, read the repository's `docs/system/coordinator-batches.md`.
