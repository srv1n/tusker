---
title: "FLW-T-0026 fresh-agent evidence"
status: "fixture prepared; focused validation pending coordinated build"
---

# Fresh-agent evidence

The isolated fixture is `e2e/agent_journey/`. Its current contract is V2 and
declares two non-overlapping owned paths, a locked local spec, requirements,
and exact command-proof rows. The release operator generates the V2 planning
context fingerprint in the pinned temporary repository, then imports it before
launching a worker. That generated fingerprint is an authored candidate fact;
the fresh worker must not invent or repair it.

The implementer prompt requires a direct interactive status transition with an
`agent:` identity, an intentionally failed first work session, a fresh recovery
session, and a submit from the recovered workspace. It forbids resident daemon
startup and automation dispatch. The independent reviewer uses `work review`
and executes its packet `next` command verbatim after supplying only verdict,
coverage, and summary placeholders. The packet binds task/work/source/proof,
gate, and material fingerprints server-side.

The shared-checkout preflight is a real runtime condition: the fixture writes
the current project-local `automation.workspace.strategy: shared` policy before
context generation. Direct work keeps the declared owned-path lease, but its
workspace check requires a clean Git tree outside `.tusker`. A release operator
must therefore baseline the `docs/system` and shipped-skill files created by
`init` before launch; this is setup, not a worker commit or a bypass of the
workspace check. The first R3 live attempt remains blocked setup friction,
because its worktree policy required a Git-ref write unavailable to the worker
sandbox. It created no implementation session.

`TestTrustFreshAgent` checks the V2 fixture shape and runs a temporary
repository through the production CLI router for init, V2 import, project
registration, and refusal of an imported held task before the direct status
release. It uses no worker, provider, daemon, or installed binary. The
companion full-journey test carries the recovered implementation and review
receipt behavior.

Validation to run once the coordinated candidate rebuild is released:

```sh
GOMAXPROCS=2 scripts/with-validation-lock.sh -- \
  go test -p=1 -parallel=1 ./cmd/tusker \
  -run '^TestTrustFreshAgent$' -count=1 -v
```

Current live gate: root must repin the fixture to the final candidate after
the direct status-release fix, then launch a fresh Muse implementer and a
separate reviewer from the copied fixture. No live run, token comparison, or
human acceptance is claimed in this report.
