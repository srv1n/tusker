# DAG leases evidence

- Revision: `03201019`; host: `Saravanans-MacBook-Pro.local`.
- Executed: `GOMAXPROCS=2 scripts/with-validation-lock.sh -- go test -p=1 -parallel=1 ./cmd/tusker -run '^TestTrustDagLeases$' -count=1 -v` — PASS.
- [TestTrustDagLeases](../../../cmd/tusker/trust_dag_leases_test.go) exercised an armed branching frontier, one-winner SQLite resource claim race, expiry reassignment, stale-holder fence/release rejection, and conflicts on owned paths, generated outputs, and migration keys.

| Acceptance | Evidence | Status |
| --- | --- | --- |
| A1 | Incremental armed frontier keeps independent tasks eligible and blocks a hard dependent. | Offline PASS |
| A2 | Concurrent SQLite resource acquisition has one owner; the expired owner cannot match or release the new generation. | Offline PASS |
| A3 | `run_ownership.go` projects all three authored collision fields through the existing guarded claim path; scheduler named-resource reservations use durable leases. | Offline PASS |

This is offline deterministic evidence. It does not dispatch a resident daemon.
