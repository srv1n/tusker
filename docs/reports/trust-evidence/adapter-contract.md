# Adapter contract evidence

- Revision: `03201019`; host: `Saravanans-MacBook-Pro.local`.
- Executed: `GOMAXPROCS=2 scripts/with-validation-lock.sh -- go test -p=1 -parallel=1 ./cmd/tusker -run '^TestTrustAdapterContract$' -count=1 -v` — PASS.
- [TestTrustAdapterContract](../../../cmd/tusker/trust_adapter_contract_test.go) verifies generic ACP reports only its implemented transport operations, refuses resume explicitly, abandons lost sessions instead of inventing resume, and keeps cloud/ACP unavailable usage and resume flags false.

| Acceptance | Evidence | Status |
| --- | --- | --- |
| A1 | Generic ACP returns explicit unavailable resume; its capability flags match implemented transport operations. | Offline PASS |
| A2 | Lost generic ACP sessions are abandoned rather than resumed. Disconnect, malformed-output, cancellation, and duplicate-final fake-provider matrices remain separate focused coverage. | Partial |
| A3 | ACP and cloud do not advertise unavailable usage/resume behavior. Live credentials, billable usage, and policy reachability were not assumed. | Offline PASS; live gate remains |

Release-pilot harnesses are the installed local `codex exec` and `claude` runners; no credentials or live provider calls were assumed here.
