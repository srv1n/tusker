---
title: "FLW-T-0014 artifact contract evidence"
status: "focused regression passed"
---

# Artifact contract evidence

The close-time matcher requires accepted evidence of the contract's mapped
kind, full contract acceptance coverage, a task-scoped copied artifact, and a
matching SHA-256 fingerprint. If a task declares `source_sha` or
`source_commit`, evidence must carry that exact source revision.

`TestTrustArtifactContract` exercises a valid screenshot contract, then
rejects the wrong evidence kind, wrong acceptance coverage, missing legacy
identity, and replaced artifact bytes. Those are direct calls through the
same proof report used by finish and close.

Revision: `03201019308fbc533e6aeace9f8c612e8b2237aa` plus the dirty proof patch
Host: local `Darwin arm64`

Executed:

```sh
GOMAXPROCS=2 scripts/with-validation-lock.sh -- \
  go test -timeout=5m -p=1 -parallel=1 ./cmd/tusker \
  -run '^TestTrustArtifactContract$' -count=1 -v
```

Result: PASS, `ok tusker/cmd/tusker 1.636s`.

Material SHA-256: `v7_proof_cmd.go`
`c896e273808db204232cec77fd55184951a871605c46984f4a27258a3f31d989`;
`v7_evidence_attempt_cmd.go`
`948aec815a351e2620b2a3213153112392761dda55dc7c155733181103bc2512`;
`trust_proof_contract_test.go`
`2bf380ed0a35f2bb6e2e7bda7e8110d7eba47c6b7964a26c7bfdd1a557279624`.

External links and legacy artifacts remain readable historical records, but
they do not satisfy a current artifact contract. Installed-runtime and human
acceptance are outside this focused source check.
