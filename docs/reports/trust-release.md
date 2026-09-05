# Trust release acceptance

The release path has two separate evidence layers.

`TestTrustFullJourney` is the deterministic offline route: a temporary Git
repository generates an exact V2 planning context and imports a two-node DAG;
an implementation session fails, recovers in its retained workspace, submits
material, receives a separately owned native review packet, records a pass,
and closes. It demonstrates state transitions and snapshot binding without a
resident daemon or provider.

`e2e/agent_journey/` is the fresh-worker route. `prepare-v2.sh` generates the
context fingerprint from the pinned candidate and then imports the contract.
The fresh implementer and independent reviewer prompts use only shipped
guidance and the copied candidate. The direct status-ready command remains an
interactive action; it does not arm a wave or enable a daemon.

Release remains blocked until the final candidate is pinned into the temporary
fixture, `prepare-v2.sh` succeeds, the focused source checks pass in the
coordinated validation batch, and separate fresh implementer/reviewer receipts
are collected. Provider usage, daemon health, and human acceptance have no
substitute in the offline scenario.
