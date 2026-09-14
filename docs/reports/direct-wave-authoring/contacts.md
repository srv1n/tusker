# External agent contacts and connector capabilities

`tusker execution register --contact-role` publishes a pre-existing external
agent conversation as an `architect`, `origin`, or named `peer` contact on a
durable task or wave:

```bash
tusker execution register --task TSK-T-0001 --contact-role architect \
  --harness codex --provider openai \
  --conversation-id <native-id> --connection-id <host-or-connection-id> \
  --source direct_codex --if-generation 0 --by operator:sarah --json
```

Exactly one of `--task` / `--wave` is required and must resolve in the
current project before any write. `peer` requires `--contact-name`; other
roles reject it. The execution record keeps `provider = harness`,
`provider_session_id = conversation_id`, and `session_ref = connection_id`;
the contact endpoint stores exactly `harness`, `provider`,
`conversation_id`, `connection_id`. Authority is never parsed from a display
label. `--if-generation 0` creates; replacement requires the exact current
generation and preserves the predecessor execution. Registration requires a
`human:` or `operator:` actor.

Task contacts override inherited wave contacts: when a task has no row for a
role/name, the resolver returns the row registered under its durable wave ID
with an explicit `inherited_from` fact. Authored task frontmatter stays a
display-only fallback and is never upgraded into a registration.

## Inspected capability matrix

| Capability | Codex | Claude Code | Devin |
|---|---|---|---|
| `messageWhileRunning` | yes — live `codexLiveHandle` steers the exact active turn via `turn/steer` with the `expectedTurnId` fence | no implemented adapter | no implemented adapter |
| `continueWhileIdle` | no | no | no |
| `retrieveResponse` | no — no adapter retrieves correlated replies from the provider | no | no |
| `attachmentValid` | yes only while the admitted attempt is live | no — stored registration is asserted, not provider-verified | no — stored registration is asserted, not provider-verified |
| Pre-existing external conversation delivery | `unsupported` — no adapter delivers into a conversation Tusker did not start | `unsupported` | `unsupported` |

`busy` = a live admitted endpoint with no active steerable turn.
`stale` = a replaced generation or an attachment that no longer matches the
registered endpoint. `unbound` = missing or unverifiable route.
`unsupported` = registered identity preserved, but no installed connector can
deliver; the reason names the harness and the operator remedy.

## Proof status

- Protocol fixtures (registration transaction, generation fencing,
  inheritance, busy/stale/unsupported projection, idempotent wakeups):
  covered by `TestExternalArchitectRouting*` tests.
- Provider-live send/reply against a real external endpoint: **NOT RUN** —
  this environment has no supplied authorized external endpoint.
