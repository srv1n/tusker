# Documentation authoring

Start with `tusker docs find <query>`; update the existing subject in place.
The corpus serves humans and agents: current behavior in `docs/system/`,
proposals in `.tusker/specs/`, rationale in `.tusker/specs/decisions/`.
Tusker accepts specifications produced by any planning method.

For unresolved choices, use the host's available external design method and
return its settled decisions or open questions here. A supplied adequate spec
skips that discussion: preserve its decision links, then convert it to tasks
using `TRACK.md`'s complete handoff and readiness review. Before conversion,
settle cross-task interfaces, surrounding-work ownership and proof scenarios;
carry the actual architect/origin contact into the handoff. A shorter worker
context must not discard the decisions the planner already resolved.
Tusker does not maintain a second interview protocol. For prose, use an
available writing skill when present; otherwise keep the outcome, actor, exact
commands, permission boundaries, uncertainty, expected result, and failure
path explicit.

Create through `tusker docs new <subject> --kind doc|spec`. Its template is
front-matter authority; fill subject, discovery keywords, parent (`part_of`),
read/skip conditions, described paths and applicable spec/decision links.
Mark verification dates only after checking behavior. Keep metadata out of the
reading prose. Use a folder introduction/index for purpose, reading order and
links to children; preserve normal file/folder navigation.

Write outcome, decisions and rationale, constraints, acceptance, and unresolved
questions in the appropriate canonical document. Keep executable details near
their owning subject. Specs identify `updates:` targets; changed behavior lands
with its documentation or an explicit follow-up task. Avoid parallel copies.

Use installed `docs --help`/capabilities for bounded browse, read, backlinks,
front-matter checking and template preview support; report missing helpers
instead of assuming a newer CLI is installed. Read only the required section
unless the complete contract is needed.

After edits run `tusker docs map`, then `tusker validate`. Generated maps are
outputs. Use `tusker docs adopt` for requested brownfield adoption: review the
whole proposal and preserve its approval boundary. Supersede explicitly so old
links resolve forward. Durable decisions remain documentation; raw logs and
scratch are disposable under configured retention, not mandatory agent journals.
