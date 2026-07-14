# Daemon Launchd Supervision

Tusker's resident daemon can run as a per-user macOS LaunchAgent:

```bash
tusker daemon install
tusker daemon status
tusker daemon uninstall
```

`daemon install` is the short form of `daemon service install`. It atomically copies the current executable to the canonical Application Support state root, writes `~/Library/LaunchAgents/com.tusker.daemon.plist`, and starts it with `launchctl bootstrap`. Re-running install unloads and replaces the service in place. The plist runs `tusker daemon run`, sets `TUSKER_LAUNCHD=1`, uses `KeepAlive` only for abnormal exits, throttles restarts to at least 10 seconds, and writes stdout/stderr to `<state-root>/logs/daemon.log`. `daemon uninstall` is idempotent and removes the plist without touching task or runtime history.

`daemon status` reports whether the plist is installed, whether the live daemon was launched by launchd or manually, the last restart cause, and crash-loop circuit state.

The reconcile tick feeds the watchdog. A beat older than three configured ticks exits with an abnormal status, allowing launchd to restart the daemon; merely having a live pid is not health. SIGKILL is recognized from the stale daemon pid record, while an orderly error path persists one pending restart cause. Each failed generation is therefore counted once.

If more than five managed abnormal starts happen within ten minutes, the sixth replacement process opens the `crash_loop` circuit. That process stays up and serves reads, but implementation and review dispatch are blocked. CLI/JSON status and the control-room red banner expose the cause until an operator repairs it and runs:

```bash
tusker daemon resume
```

Manual development remains unchanged: `tusker daemon run`, `tusker daemon run --once`, and `tusker refresh` do not require launchd and do not consume the managed-service restart budget.
