# Model settings API acceptance

`GET /api/models` and `tusker models show --json` use the same resolver and
versioned `tusker.model-levels/v1` document. `POST /api/models` delegates `set`,
`reset`, `profile-set`, `profile-disable`, `profile-enable`, and `profile-remove`
to the CLI command functions, including shared
validation, atomic writes, and revision conflict checks. `GET
/api/models/catalog` returns the bounded installed-harness observation.

| Case | Result |
| --- | --- |
| CLI/API read agreement | PASS |
| Project write and provenance | PASS |
| Stale revision refusal | PASS |
| Profile write through Serve | PASS; returned profile remains visibly `configured_unverified` |
| Lifecycle through Serve | PASS; disable returns `disabled`, unreferenced remove succeeds, and stale replay is refused |
| Capability registry | PASS; `profiles` is `authoritative_mutable` |
| Run identity | PASS; actual profile/harness/model/effort and fallback reason serialize when recorded, while absent identity remains absent |

Measured with the same focused Go command recorded in the routing report: 8
tests passed. No live provider call was used as API proof.

Stable packet 03/04 request examples:

```json
{"action":"profile-set","scope":"project","name":"codex-standard","displayName":"Codex Standard","eligibleTiers":["standard","demanding"],"harness":"codex_exec","model":"gpt-6-astra","effort":"high","preset":"workspace-write-offline","revision":"sha256:..."}
{"action":"set","scope":"project","level":"standard","lane":"execute","profiles":["codex-standard","codex-light"],"revision":"sha256:..."}
{"action":"reset","scope":"project","level":"standard","lane":"review","revision":"sha256:..."}
{"action":"profile-disable","scope":"project","name":"codex-standard","revision":"sha256:..."}
{"action":"profile-remove","scope":"project","name":"unused-profile","revision":"sha256:..."}
```

The profile map key is the stable ID; `display_name` is editable and
`eligible_tiers` is the authoritative many-to-many membership. The response
also includes `profile_references` and `reference_check_complete`. A referenced
membership removal or profile removal returns 422 with the affected assignment
references and changes nothing. Every mutation requires the latest `revision`.
