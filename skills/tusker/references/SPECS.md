# Documentation authoring

Use a supplied canonical document directly, or `tusker docs find <query>` to
locate its owner. Update the existing subject in place.
The corpus serves humans and agents under one placement rule: current
chapters in `docs/system/` (per domain under `docs/system/domains/<domain>/`),
change specifications in `docs/system/proposals/`, and product decisions in
`docs/system/decisions/`. `.tusker/` holds tracker state and thin pointers
only. Legacy `.tusker/specs/` paths still resolve during
transition; all new knowledge writes use `docs/system/`.
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

Create through `tusker docs new <subject> --kind doc|proposal|decision`
(`--kind spec` stays accepted as the spelling for proposal). Its template is
front-matter authority; fill subject, discovery keywords, parent (`part_of`),
read/skip conditions, described paths, `updates` targets for proposals, and
`decides_for` for decisions. New documents declare
`kind: doc | proposal | decision`; kind is independent of location. A `doc`
without `--domain` defaults to the system root; `--domain <name>` targets
`docs/system/domains/<name>/`. New domain indexes use `00-index.md`.
Declare kind-specific lifecycle (`current|superseded` for docs,
`proposed|accepted|implemented|superseded` for proposals,
`proposed|accepted|superseded` for decisions) separately from
`code_conformance` (`unverified|matches|drift|not_applicable`); `matches`
needs a `last_verified` date, commit, and stated scope, and partial coverage
is `drift` with the gap named in prose. Mark verification dates only after
checking behavior. Keep metadata out of the reading prose. Use a domain
`00-index.md` for purpose, reading order and links to children; preserve
normal file/folder navigation. Reviewed migration instructions live with
`tusker docs adopt --migration`: preview first, then apply an approved,
fingerprinted table; originals stay recoverable until completion.

Keep executable details near their owning subject. Specs identify `updates:`
targets; changed behavior lands with its documentation or an explicit follow-up
task. Preserve decision links instead of copying their contents.

Check changed docs with `tusker docs check <subject-or-path>`. Refresh
`tusker docs map` when graph metadata, paths, or links change; generated maps
are outputs. Run `tusker validate` for project-canon or task-contract changes.
Use `tusker docs adopt` for requested brownfield adoption: review the proposal
and preserve its approval boundary. Supersede explicitly so old links resolve
forward. Raw logs belong in scratch under configured retention.
