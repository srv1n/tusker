# Dispatcher pseudocode

Reference algorithm for a cron-driven binary that turns the vault's `dashboard.json` into agent runs. This is the contract the Tusker CLI exposes; the dispatcher does not hand-parse markdown.

## Tick loop

```text
function tick(vault_path):
    # 1. Reindex — cheap enough to run every tick; skips if nothing changed.
    run("tusker reindex --vault <vault_path>")

    # 2. Read the authoritative snapshot.
    dashboard = read_json(vault_path + "/_system/generated/dashboard.json")
    config    = load_yaml(vault_path + "/_system/config.yaml")

    # 3. Reconcile runs we started in prior ticks.
    for row in dashboard.queues.claimed + dashboard.queues.running:
        reconcile_run(vault_path, row, config)

    # 4. Claim new work.
    for row in dashboard.queues.unclaimed:
        if row.status != "active":
            continue
        agent = row.assignee
        if agent not in config.agents.enabled:
            continue
        if dashboard.agents[agent].active >= (config.agents.concurrency[agent] or 0):
            continue
        # pickup is atomic — if two dispatchers race, the second gets ALREADY_CLAIMED.
        result = run_json("tusker pickup --vault <vault_path> --id " + row.id + " --by " + agent)
        if not result.ok:
            log(result.error)
            continue
        dispatch_agent(vault_path, row, agent, config)
```

## dispatch_agent

```text
function dispatch_agent(vault_path, row, agent, config):
    workspace = prepare_workspace(vault_path, row.id, config)
    prompt    = build_prompt(vault_path, row.id)

    run_json("tusker release --vault <vault_path> --id " + row.id +
             " --to running --by dispatcher")

    # Fork the agent CLI. stdout/stderr stream to Attachments/<id>/session-<n>.log.
    pid = spawn_cli(agent, prompt, workspace,
                    log_path = vault_path + "/Attachments/" + row.id + "/session-" + row.run_attempts + ".log",
                    heartbeat_path = workspace + "/.tusker-heartbeat")

    persist({id: row.id, pid: pid, started_at: now(), workspace: workspace,
             heartbeat_path: workspace + "/.tusker-heartbeat"},
            vault_path + "/_system/logs/runs.json")
```

## reconcile_run

```text
function reconcile_run(vault_path, row, config):
    record = lookup_run(row.id, vault_path + "/_system/logs/runs.json")
    if not record:
        # We rebooted and lost the PID table. Trust the vault's state.
        return

    if pid_alive(record.pid):
        if heartbeat_stale(record.heartbeat_path, config):
            kill_tree(record.pid)
            run_json("tusker release --vault <vault_path> --id " + row.id +
                     " --to stalled --failure-class stuck")
        return

    exit_code = reap(record.pid)
    if exit_code == 0:
        run_json("tusker release --vault <vault_path> --id " + row.id + " --to done")
    else:
        klass = classify_exit(exit_code, record)
        run_json("tusker release --vault <vault_path> --id " + row.id +
                 " --to failed --failure-class " + klass)
        if klass == "transient" and row.run_attempts < config.retry.max_attempts:
            schedule_retry(row.id, config.retry.backoff_seconds[min(row.run_attempts, len(config.retry.backoff_seconds) - 1)])
```

## Atomicity guarantees provided by the CLI

- `pickup` races are resolved by filesystem write semantics plus the `dispatch_state` precondition. Two concurrent pickups against the same id: the loser gets `ALREADY_CLAIMED`.
- `release` only writes the terminal state; it never clears `claimed_by` / `claimed_at`, so the audit trail survives.
- `reindex` is idempotent — running it while the dispatcher is in the middle of a tick is safe.

## Notes

- The dispatcher owns the PID table (`_system/logs/runs.json`). The vault records are source of truth for policy; the PID table is a process liveness cache.
- iCloud sync latency (5-30s) is not a correctness issue: the CLI always reads fresh state on every call, and pickup is atomic on the local filesystem. Two machines running dispatchers simultaneously is the one failure mode — enforce single-dispatcher via a lock file under `_system/logs/dispatcher.lock`.
- Heartbeats are a cooperative protocol: agents are expected to `touch` the heartbeat file every N seconds (default 30). No heartbeat for `heartbeat_timeout_seconds` (default 180) → stuck.

## Shipping the CLI as a single binary

The dispatcher is going to want a statically-locatable `tusker` it can shell out to from a launchd/cron context. Build it with:

```sh
go build -o dist/tusker ./cmd/tusker
```

All templates, bases, gitignore, and repo-contract files are embedded in the Go binary, so it has no runtime dependency on the source checkout. Drop it at `~/bin/tusker` (or anywhere on `$PATH`) and the dispatcher can invoke it uniformly across the laptop and the iCloud-synced phone bridge.
