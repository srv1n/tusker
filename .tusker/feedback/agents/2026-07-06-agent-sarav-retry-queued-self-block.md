# Agent Feedback

- context: AGX-T-0003 hit max_turns=1 turn boundary; supervisor queued continue_thread retry at 2026-07-06T05:57Z with max_active_runs_per_project=1
- friction: retry_queued lease counts toward the project active-run limit, so the run's own continuation is capacity-blocked by itself and never dispatches; separately the resident daemon process had died with no surfaced signal, so nothing was polling
- product-idea: Exempt the run being dispatched from its own capacity check (retry/continuation path); surface daemon liveness (pidfile/health in automation plan output) so a dead poller is visible
- impact: At limit 1 every multi-turn task wedges at its first turn boundary until a manual work_revision bump; second lease dead-end class this week after interrupted
- related: AGX-T-0003
