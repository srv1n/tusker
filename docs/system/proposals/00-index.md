---
kind: doc
subject: proposals-index
keywords: [proposals, index, change specifications]
part_of: overview
status: current
created: 2026-09-27
read_when: "Finding which change proposal covers a topic, or where new change specifications live."
skip_when: "You need current shipped behavior; read the system chapters instead."
---

# Proposals

This folder holds change specifications for the tracked system. Each file
proposes one bounded change; its `updates` list names the system chapters it
will change once implemented. Current behavior stays in `docs/system/`.

| File | Covers |
| --- | --- |
| [real-testing-campaign.md](real-testing-campaign.md) | The owner's statement of what Tusker is for, and the running work list for the September 2026 live test campaign. |
| [task-states.md](task-states.md) | One visible state per task, shared by the UI, CLI, and API. |
