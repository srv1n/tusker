# Agent Feedback

- context: OPS-T-0005 live shakedown: first exact run was tusker land TRC-T-0002 from the source repo.
- friction: The command aborts with 'TRC-T-0002 is not in a wave; merge-lane land requires wave membership' and does not suggest the recovery command or whether creating a wave is the intended operator step.
- product-idea: Have tusker land print an actionable next step for non-wave tasks, or provide an explicit --create-wave/--wave option for single-task landing shakedowns.
- impact: A reviewed, proof-satisfied task cannot be landed by the new command without out-of-band knowledge of wave setup, so the first live drain stalls before the merge lane is exercised.
- related: OPS-T-0005, TRC-T-0002, tusker land, waves
- dedupe-key: OPS-T-0005-land-non-wave-recovery
