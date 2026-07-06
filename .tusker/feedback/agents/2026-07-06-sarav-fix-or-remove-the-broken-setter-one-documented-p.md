# Agent Feedback

- context: Owner asked to raise per-project concurrency from 1 to 4 on the tusker project (2026-07-06).
- friction: 'tusker projects limits --max-active-runs 4' returns ok:true but read-back stays 1 — silent no-op (no projects-table column, no daemon_settings write). Effective limit actually resolves from repo tusker.yaml automation.concurrency.max_active_runs_per_project, silently overriding .tusker/WORKFLOW.md runtime.max_active_runs_per_project (workflow.go:317); editing WORKFLOW.md alone does nothing and nothing says which source won. A daemon restart was also required to pick up the change.
- product-idea: Fix or remove the broken setter; one documented precedence chain with resolve-style provenance (RUN-T-0002); hot-reload concurrency limits on poll.
- impact: Operator burned ~15 minutes and two daemon restarts on a one-line config change; silent ok:true erodes trust in the CLI.
- related: RUN-T-0002
