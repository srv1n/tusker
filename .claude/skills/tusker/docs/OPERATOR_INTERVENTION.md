# Operator Intervention

Use the public V5 task commands for manual intervention.

## Reset A Task

```bash
tusker status <TASK-ID> active --actor <name> --reason "<why work can resume>"
```

Use this when a task was blocked or rework is complete.

## Cancel A Task

```bash
tusker status <TASK-ID> cancelled --actor <name> --reason "<why cancelled>"
```

Cancellation is terminal. Create a new task if the work needs to continue under different scope.

## Verify And Close

```bash
tusker evidence <TASK-ID> log <file-or-url> --note "<what this proves>"
tusker docs check <TASK-ID>
tusker status <TASK-ID> review --actor <name>
tusker verify <TASK-ID> --by <verifier>
tusker close <TASK-ID> --by <reviewer>
tusker validate
```

If the reviewer lane is enabled, the configured `reviewer.actor` can perform the verify/close steps for low/medium tasks after review passes. For high/critical tasks, use the reviewer output as advisory evidence and leave final verify/close to a human.

Never delete prior evidence or work-log history. Append the new truth.
