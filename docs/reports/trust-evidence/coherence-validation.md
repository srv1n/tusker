---
title: "Current-source coherence validation"
status: "compile-only and candidate build passed"
---

# Current-source coherence validation

Revision: `03201019308fbc533e6aeace9f8c612e8b2237aa` plus dirty source tree.
Host: local `Darwin arm64`.

## Compile only

```sh
GOMAXPROCS=2 scripts/with-validation-lock.sh -- \
  go test -run '^$' -p=1 -parallel=1 ./...
```

The final retry passed in 26.255s. It compiled every Go package and ran no
tests. The first attempt exposed two compile errors, both repaired by their
owners before this one permitted retry: an unused test fixture variable in
`delivery_amendment_test.go`, and a missing `v7schema` reference in the
docgraph resolver test.

## Candidate

```sh
GOMAXPROCS=2 scripts/with-validation-lock.sh -- \
  go build -p=1 -o /tmp/tusker-trust-current ./cmd/tusker
```

Result: PASS in 17.077s.

| Field | Value |
| --- | --- |
| Binary | `/tmp/tusker-trust-current` |
| SHA-256 | `9f4748ada8b8f717d59b48e38e946740d57cae40ee81b88d8b4c5c1a63890cc9` |
| Version | `tusker v0.0.0-20260903052830-03201019308f+dirty (03201019308f+dirty) go1.26.5 darwin/arm64` |

## Dirty-source identity

Tracked source diff SHA-256, excluding only trust-evidence reports:
`579bc7a480adab6cb4fc6dee9efcf296464b56c664d083926e1fde575bf24485`.

The exact untracked Go source manifest SHA-256 is
`470bd8fa242982d849cc3d4ac8e63e724f29813c91e0aa978bf63a4f94876e1c`:
[coherence-untracked-go.manifest](coherence-untracked-go.manifest).

## Held validation

No focused, grouped, or full functional test suite was run after the final
source freeze. Earlier grouped failures had source repairs awaiting rerun,
including compact CLI output, guide word budget and scope text, held-wave
amendment semantics, context reuse, review fixture setup, and journey fixture
ordering. This report is compile/build evidence only; it does not claim those
repairs pass.
