# Agent Feedback

- context: Supervised dogfood: dispatched SRV-T-0001 via tusker automation dispatch with no daemon running
- friction: Dispatch spawned codex app-server as a CLI child; CLI exited and the runner died the same second. Lease stayed running until a manual tusker refresh marked it retry_queued (session resumable). A second refresh did not re-dispatch, and manual dispatch was blocked by the zombie run holding the per-project slot (1/1).
- product-idea: Dispatch should detach the runner (setsid/daemonize) or refuse to dispatch without a resident daemon and say so; refresh should re-dispatch queued retries once backoff elapses.
- impact: First real dispatch silently produced zero work; operator believed a run was active for 17 minutes.
- related: SRV-T-0001, RUN-T-0001
