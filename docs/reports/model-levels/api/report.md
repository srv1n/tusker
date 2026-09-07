# Model settings API acceptance

`GET /api/models` and `tusker models show --json` use the same resolver and
versioned `tusker.model-levels/v1` document. `POST /api/models` delegates `set`,
`reset`, and `profile-set` to the CLI command functions, including shared
validation, atomic writes, and revision conflict checks. `GET
/api/models/catalog` returns the bounded installed-harness observation.

| Case | Result |
| --- | --- |
| CLI/API read agreement | PASS |
| Project write and provenance | PASS |
| Stale revision refusal | PASS |
| Profile write through Serve | PASS; returned profile remains visibly `configured_unverified` |
| Capability registry | PASS; `profiles` is `authoritative_mutable` |
| Run identity | PASS; actual profile/harness/model/effort and fallback reason serialize when recorded, while absent identity remains absent |

Measured with the same focused Go command recorded in the routing report: 8
tests passed. No live provider call was used as API proof.

