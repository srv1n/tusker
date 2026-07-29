# Agent Feedback

- context: Interactive work start and broad validation for SRV-T-0007 on current main
- friction: tusker work start refused a ready task with identity_or_workspace_binding after preparing no usable workspace; the same binding refusal breaks daemon/crash-recovery fixtures. tusker reconcile also rewrote unrelated ready projections while repairing one new task revision.
- product-idea: Make work-start identity validation report the missing field before mutating workspace state, and scope reconcile repair to the requested object unless an explicit all-record repair is requested.
- impact: Interactive ownership could not open, and the repository-wide gate failed four unrelated tests despite focused product suites passing.
- related: SRV-T-0007; TestDaemonPollDispatchesReleasedReviewHandoffInsideArmedWave; TestServeConfigDisableAndLocalhostDefaults; TestDaemonKillNineAdoptsSurvivingWrapper; TestArmedWaveCrashRestartConverges
- dedupe-key: work-start-identity-binding-and-broad-reconcile
