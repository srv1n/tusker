# Daemon Launchd Supervision

Tusker's resident daemon can run as a per-user macOS LaunchAgent:

```bash
tusker daemon install
tusker daemon status
tusker daemon uninstall
```

`daemon install` writes `~/Library/LaunchAgents/com.tusker.daemon.plist`, starts it with `launchctl bootstrap`, and restarts it with `launchctl kickstart -k`. The plist runs `tusker daemon run`, sets `TUSKER_LAUNCHD=1`, preserves the selected `TUSKER_STATE_ROOT`, uses `KeepAlive` for abnormal exits, throttles restarts to at least 10 seconds, and writes stdout/stderr to `<state-root>/daemon.log`.

`daemon status` reports whether the plist is installed, whether the live daemon was launched by launchd or manually, the last restart cause, and crash-loop circuit state.

If more than five abnormal daemon starts happen within ten minutes, the daemon opens the `crash_loop` circuit. In that mode the process stays up and serves reads, but dispatch is blocked until an operator repairs the cause and runs:

```bash
tusker daemon resume
```

Manual development remains unchanged: `tusker daemon run`, `tusker daemon run --once`, and `tusker refresh` do not require launchd.
