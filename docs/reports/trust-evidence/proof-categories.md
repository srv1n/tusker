---
title: "FLW-T-0015 proof category evidence"
status: "focused regression passed"
---

# Proof category evidence

The existing evidence record now carries a compact category and facts map.
No second evidence hierarchy is introduced.

`TestTrustProofCategories` has one complete and one materially misleading
fixture for each category:

- visual: a before/after pair passes; one view fails;
- performance: equal before/after workloads pass; a changed workload fails;
- backend: observable behavior plus a negative case passes; no negative case
  fails; and
- migration: preservation, interruption, and recovery pass; missing
  preservation fails.

The structural matcher checks declared provenance only. It does not claim
visual quality or human acceptance.

Revision: `03201019308fbc533e6aeace9f8c612e8b2237aa` plus the dirty proof patch
Host: local `Darwin arm64`

Executed:

```sh
GOMAXPROCS=2 scripts/with-validation-lock.sh -- \
  go test -timeout=5m -p=1 -parallel=1 ./cmd/tusker \
  -run '^TestTrustProofCategories$' -count=1 -v
```

Result: PASS, `ok tusker/cmd/tusker 1.636s`.

Material SHA-256: `v7_proof_cmd.go`
`c896e273808db204232cec77fd55184951a871605c46984f4a27258a3f31d989`;
`v7_evidence_attempt_cmd.go`
`948aec815a351e2620b2a3213153112392761dda55dc7c155733181103bc2512`;
`trust_proof_contract_test.go`
`2bf380ed0a35f2bb6e2e7bda7e8110d7eba47c6b7964a26c7bfdd1a557279624`.
