---
title: "FLW-T-0028 full-journey evidence"
status: "source scenario prepared; focused validation pending coordinated build"
---

# Full-journey evidence

`TestTrustFullJourney` creates a temporary Git repository and invokes the
production CLI parser/router for `init`, V2 delivery import, work
start/fail/start/submit, native `work review`,
review submit, and close. Its source scenario proves the intended lifecycle:
a declared local spec imports into a two-node hard dependency DAG; the first
attempt fails without a launchable lease; recovery creates a new attempt in
the retained workspace; a committed owned-path implementation submits; a
different reviewer receives the immutable implementation workspace and all
snapshot fingerprints; command proof runs at pass; then the reviewed task
closes. The base checkout never receives the isolated implementation file.

The test is offline and uses a temporary runtime state root. It neither starts
a daemon nor dispatches automation or a provider. It does not establish that a
resident daemon, an external model, or a human acceptance completed the
journey.

Both source scenarios generate their exact V2 planning-context fingerprint in
their temporary repository. Do not treat the source test as a release pass
until the coordinated build and focused validation land.

Planned focused validation:

```sh
GOMAXPROCS=2 scripts/with-validation-lock.sh -- \
  go test -p=1 -parallel=1 ./cmd/tusker \
  -run '^TestTrustFullJourney$' -count=1 -v
```
