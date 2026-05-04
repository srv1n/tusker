# Dispatcher Pseudocode

Runtime dispatch reads task-native generated indexes. It does not mutate legacy runtime fields in markdown.

```text
poll:
  reindex vault
  read _system/generated/tasks.index.json
  for each task where status in ["active", "rework"]:
    skip if blocked_by has unfinished tasks
    start or resume an attempt in runtime storage
    write readable evidence packet when the attempt completes
    move task to review only when implementation proof exists
```

The markdown task is the durable human workflow. The runtime store is process bookkeeping.
