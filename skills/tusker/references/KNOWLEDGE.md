# Knowledge

Canonical repo knowledge is one visible corpus, shared by humans and agents — never a separate agents fork:

| Node | Lives in | Answers |
|---|---|---|
| Canonical doc | `docs/system/` | how it works today |
| Spec | `.tusker/specs/` | what is changing and why |
| Decision log | `.tusker/specs/decisions/` | what was said, what got locked |

Docs are canonical: an answer comes from a doc read this run, or it is "not in canon" plus an offer to write it. If a doc contradicts the code, current behavior belongs to the code — report the drift and fix the doc in the same change.

## Answer a question

```bash
tusker docs find <query>
```

Deterministic keyword routing over front-matter and headings, ranked canonical doc → spec → decision log. Read the top hit(s) whose `read_when` matches; answer citing paths. One question costs one or two file reads, not a scan.

## Read efficiently

Use supported `docs read` section selection, `docs browse` for a folder and
`docs backlinks` for callers when available; check targeted help first on an
older installation. Raw file reads remain sufficient when helpers are missing.
Read front matter and the relevant section together. A folder introduction
explains its purpose and reading order; it does not replace its child documents.

For authoring, switch to the documentation route in the entry point. Keep a
single durable account of decisions, current behavior and source references;
execution transcripts and scratch are not a second knowledge corpus.
