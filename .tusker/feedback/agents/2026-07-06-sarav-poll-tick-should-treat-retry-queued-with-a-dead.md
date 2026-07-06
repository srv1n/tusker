# Agent Feedback

- context: Recovering the dead SRV-T-0001 run while the daemon was live
- friction: A retry_queued lease with a dead process is a permanent zombie: the poll loop never re-dispatches it (next_retry_at stays empty), runs interrupt fails with HOOK_FAILED live runner handle not found, and the lease still counts against global and per-project active-run limits. Had to reset lease_state to unclaimed via sqlite3 on daemon.db by hand.
- product-idea: Poll tick should treat retry_queued with a dead pid as dispatchable (resume if session resumable); interrupt should clear dead leases instead of requiring a live handle; add tusker runs release <id> as the sanctioned unstick command.
- impact: One dead run deadlocks the whole project queue; operator needed raw DB surgery.
- related: SRV-T-0001, RUN-T-0001
