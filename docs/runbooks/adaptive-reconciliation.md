# Adaptive reconciliation

Tusker is event-driven during ordinary use. Successful canonical CLI writes
notify the resident daemon with the registered runtime identity of the affected
project. The daemon debounces bursts for 350 ms and reconciles only that
project. Serve refresh and active project attention use the same targeted path.

Timed reconciliation is a bounded fallback for raw editor writes, missed
notifications, process recovery, and registry drift. Each enabled project owns
an independent in-memory schedule:

| Tier | Idle/activity state | Safety cadence |
|---|---|---:|
| `live` | Runnable, claimed, running, retry-queued, or interrupted runtime work | 5 seconds |
| `hot` | First minute after daemon start, CLI mutation, UI attention, or manual refresh | 60 seconds |
| `warm` | At least one minute idle | 5 minutes |
| `cool` | At least five minutes idle | 10 minutes |
| `cold` | At least ten minutes idle | 30 minutes |

The global `TUSKER_POLL_INTERVAL_MS` override changes the hot-tier cadence and
retains a five-second floor. It does not force unrelated projects to share one
project's workflow interval.

Serve attention remains a separate 20-second path. The five-second live tier
applies only while runtime work needs lease, process, retry, or capacity
supervision; it is not a global scan cadence.

The scheduler wakes only at the nearest project due time. A due project loads
its workflow and operational records under `.tusker/work/**`; it does not walk
project knowledge or documentation. The mtime/size note cache makes an
unchanged operational pass stat-only with zero Markdown reads or YAML parses.
Serve and documentation commands retain the full-vault loader because those
surfaces actually need knowledge and docs.

Adaptive state is deliberately in memory. A daemon restart initializes every
enabled project as hot, which is safer than trusting stale activity metadata.
The Serve projects response exposes tier, cadence, last activity, last poll,
and next due time for diagnosis.

Projects may be registered for observation while dispatch remains off:

```yaml
automation:
  enabled: false
```

Registry enablement means “observe this project.” `automation.enabled` means
“allow eligible work to dispatch.” Fresh repositories default automation to
false.
