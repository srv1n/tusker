# Agent Feedback

- context: Two tusker daemon processes ran concurrently against the same daemon.db after an operator started a second instance (first appeared dead due to a misleading process check)
- friction: The daemons contended on SQLite and the older one exited fatally with 'database is locked (5) (SQLITE_BUSY)'; nothing prevents double-starting a daemon and a transient lock is treated as fatal
- product-idea: Single-instance guard (pidfile/exclusive lock with a clear 'daemon already running' error on second start) plus busy_timeout/retry-with-backoff on SQLITE_BUSY instead of fatal exit; expose daemon pid/uptime in tusker automation status
- impact: A double-start silently kills the incumbent supervisor instead of refusing to start; any transient db contention can take the poller down
- related: RUN-T-0007
