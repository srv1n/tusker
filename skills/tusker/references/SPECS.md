# Documentation authoring

Use a supplied canonical document directly, or `tusker docs find <query>` to
locate its owner. Update the existing subject in place.
The corpus serves humans and agents: current behavior in `docs/system/`,
proposals in `.tusker/specs/`, rationale in `.tusker/specs/decisions/`.
Tusker accepts specifications produced by any planning method.

For unresolved product choices, use an available external design method such
as `grilling`, or the user's chosen method. A supplied adequate spec skips that
discussion. Preserve decisions, constraints, acceptance, and open questions.
Use an available writing skill for prose; otherwise lead with the outcome,
name the actor, and retain exact commands, permission boundaries, uncertainty,
expected results, and failure paths. Missing optional skills do not block this
local procedure or authorize an installation.

When task conversion is requested, use `TRACK.md` and its handoff guidance.
Settle shared interfaces, ownership, proof scenarios, and architect/origin
contacts before handing off. A documentation edit alone needs no task batch,
runner selection, or design interview.

Create through `tusker docs new <subject> --kind doc|spec`. Its template is
front-matter authority; fill subject, discovery keywords, parent (`part_of`),
read/skip conditions, described paths and applicable spec/decision links.
Mark verification dates only after checking behavior. Keep metadata out of the
reading prose. Use a folder introduction/index for purpose, reading order and
links to children; preserve normal file/folder navigation.

Keep executable details near their owning subject. Specs identify `updates:`
targets; changed behavior lands with its documentation or an explicit follow-up
task. Preserve decision links instead of copying their contents.

Check changed docs with `tusker docs check <subject-or-path>`. Refresh
`tusker docs map` when graph metadata, paths, or links change; generated maps
are outputs. Run `tusker validate` for project-canon or task-contract changes.
Use `tusker docs adopt` for requested brownfield adoption: review the proposal
and preserve its approval boundary. Supersede explicitly so old links resolve
forward. Raw logs belong in scratch under configured retention.
