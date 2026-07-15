# Canon locations

Tusker supports three legitimate canon patterns. Pick one per epic, make it explicit, then create the task stack that executes it.

## Rule

```text
Every current epic declares canon and has at least one executable task.
```

If the epic is current and the task stack does not exist yet, scoping is incomplete.

## Canon patterns

| Canon lives in | Use when | What to set |
|---|---|---|
| epic `## Design` | the workstream is still evolving | make `## Design` substantive |
| durable docs page under `tusker/docs/**` | the spec should be durable, reviewable, or publishable | create a doc with `kind: canon` and a stable `node` |
| repo file cited by a durable doc | the contract ships with code or external consumers read it | create a canon doc and put repo paths in `source_of_truth` |

## Pick the epic body when

- product and technical decisions will keep moving during execution
- you want one living epic page that readers open first
- the canon is more roadmap than frozen spec

## Pick a durable canon doc when

- multiple tasks cite a shared decision set
- the doc should outlive day-to-day epic edits
- the doc should publish or appear in agent-readable manifests

```bash
tusker new doc --title "<Spec title>" \
  --node spec/<slug> \
  --kind canon \
  --audience developer \
  --domains schema,workflow
```

Task frontmatter should name the affected docs target:

```yaml
domains: [schema, workflow]
doc_nodes: [spec/<slug>]
```

## Pick a repo file when

- the spec is part of a shipped artifact
- external consumers read it from the repository
- the file belongs beside code generation or protocol assets

Create a durable canon doc and cite the shipped file:

```yaml
kind: canon
source_of_truth: [docs/specs/my_protocol.md]
```

## Companion docs are not canon by default

Use a companion doc when content supports execution but is not the primary contract:

- design deep-dive
- alternatives analysis
- user guide
- release note
- research memo
- rollout note

```bash
tusker new doc --title "<Doc title>" \
  --node reference/<slug> \
  --kind companion \
  --audience developer \
  --domains runtime
```

Link it from the task body and include its node in `doc_nodes` if close should check it.

## What not to do

- do not create a beautiful canonical spec with zero tasks
- do not leave canon implicit
- do not make every developer doc canonical by accident
- do not copy spec prose into task bodies

## Task citation

Task `## Canon` should point at the chosen source in read order:

```markdown
## Canon

- Epic: [[<ACR>]]
- Spec: [[spec/<slug>]]
- Contract: `docs/specs/my_protocol.md`
```

Durable docs live under `tusker/docs/**`; do not put new doc nodes under epic folders.
