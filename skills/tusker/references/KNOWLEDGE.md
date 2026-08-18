# Knowledge

Repo knowledge lives in `.tusker/knowledge/domains/<domain>/`. Docs are canonical: an answer comes from a doc read this run, or it is "not in canon" plus an offer to write it. The routing below exists so one question costs one or two file reads, not a scan.

## Answer a question

1. `.tusker/SKILL.md` — pick the narrowest domain whose read-when line matches the question.
2. That domain's `CANON.md` — domain truth, plus a Files table routing deeper.
3. Read only the leaf file(s) whose read-when line matches; answer citing paths.

The read-when lines are the filter: a file whose line rules the question out stays closed. If canon contradicts the code, current behavior belongs to the code — report the drift and fix the doc in the same change.

## The machinery owns the skeleton

`.tusker/SKILL.md` and each domain's `INDEX.md` are generated, schema'd notes — `tusker domain new` regenerates them, and a hand edit is clobbered or corrupts CAS. Create and register domains only through the CLI; the `--summary` becomes the domain's read-when line in the router:

```bash
tusker domain new auth --title "Auth" --summary "token refresh, sessions, OAuth, 401 handling"
```

Hand-maintained surfaces are exactly two: `CANON.md` and the leaf docs beside it.

## CANON.md

The domain's single source of truth: dense prose stating current behavior and why — file paths, commands, invariants, the gotcha no config confesses. What one file or `--help` lookup already shows stays out; it goes stale here and never does there. When the domain grows leaf docs, CANON.md carries a Files table:

```markdown
| File | Read when |
|---|---|
| token-refresh.md | token refresh, expiry clocks, 401 retry |
| oauth-callback.md | OAuth callback, provider redirect, state param |
```

A leaf absent from this table is unreachable — adding, renaming, or resharpening a doc updates its row in the same change.

## Leaf docs

Plain `.md` beside CANON.md, one subject each. Frontmatter is the file's context pointer — one dense line each, trigger phrases first, `->` pointing rejections at the right home:

```markdown
---
read_when: token refresh, session expiry, 401 retry, OAuth callback wiring
skip_when: login UI styling; profile data -> profile/
---
# Token refresh
```

## Write or update

- New durable truth goes to the narrowest owning domain's CANON.md; a stable runbook, interface, or decision splits into a leaf when CANON outgrows one subject.
- Behavior changed means the owning doc changes in the same task.
- Raw external input belongs in the domain's `sources/`; task records and runtime state never enter knowledge.
- Run `tusker validate --json` after knowledge changes.
