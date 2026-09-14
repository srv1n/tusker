# Working in Tusker

## Scope and context

Implement the requested outcome directly. Preserve existing user changes and
stay within the requested files and behavior. Read the source and documentation
needed for the task; a small edit does not require a repository-wide audit.

- Skill authoring: edit `skills/tusker/`, the canonical shipped package.
  `.agents/skills/tusker` and `.claude/skills/tusker` point to it.
- Repo knowledge: use an exact governing reference, or `tusker docs find <query>`
  to locate the relevant section. Current behavior lives in `docs/system/`;
  proposals and decisions live in `.tusker/specs/`.
- Tracked work: use the Tusker skill for contracts, dependencies, proof, gates,
  review, and lifecycle. A capsule answers status; implementation needs the
  complete task packet.

## Completion

For documentation or skill wording changes, edit the text and finish. Do not
run tests, builds, installations, or regenerate outputs unless requested.
For code changes, use the affected existing checks and fix failures caused by
the change; broaden verification only for an unresolved concern. Follow the
user's explicit testing instructions.

Continue routine authorized work without repeated permission requests. Pause
only work that depends on missing authority, a material decision, or an explicit
human gate. Report the completed result and any remaining blocker concisely.
Use `rtk` for noisy shell output when available.

## Execution modes

Interactive sessions implement the user's work themselves. Never start
`tusker daemon run`, invoke `tusker automation dispatch`, or launch nested
`codex exec`/`claude -p` workers. Recording tasks is inert; automation settings
change only within the user's request.

Background execution belongs to an independently running resident daemon with
project automation enabled. `tusker automation plan` is read-only. A worker
with `TUSKER_ATTEMPT_ID` follows its existing claim and works only that task;
it does not spawn another runner or daemon.

## Commit authorship

Commit as the configured Git user with local credentials. Never add AI
attribution: no agent co-author trailers, generated-by lines, or agent names in
commit messages, PR bodies, or authorship metadata.
