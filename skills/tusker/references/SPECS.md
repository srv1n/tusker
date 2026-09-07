# Documentation authoring

Start with `tusker docs find <query>`; update the existing subject in place.
The corpus serves humans and agents: current behavior in `docs/system/`,
proposals in `.tusker/specs/`, rationale in `.tusker/specs/decisions/`.
Tusker accepts specifications produced by any planning method.

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
