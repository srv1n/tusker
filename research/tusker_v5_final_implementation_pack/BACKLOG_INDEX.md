# Tusker v5 backlog index

Total tasks: 64

| ID | Epic | Title | Kind | Risk | Priority | Depends on | Doc nodes |
|---|---|---|---|---|---|---|---|
| ARC-T-0001 | ARC | Rename story to task across core model | migration | high | p0 | — | tusker/model, tusker/tasks |
| ARC-T-0002 | ARC | Collapse bug into task kind bug | migration | high | p0 | ARC-T-0001 | tusker/tasks |
| ARC-T-0003 | ARC | Trim task frontmatter and remove docs enum | feature | high | p0 | ARC-T-0001, ARC-T-0002 | tusker/tasks, tusker/docs-system |
| ARC-T-0004 | ARC | Define final task body contract | feature | high | p0 | ARC-T-0003 | tusker/tasks |
| ARC-T-0005 | ARC | Move durable docs out of epic folders while keeping type doc | migration | high | p0 | ARC-T-0003 | tusker/docs-system |
| ARC-T-0006 | ARC | Define events runs generated state split | feature | high | p0 | ARC-T-0003 | tusker/model, tusker/daemon |
| ARC-T-0007 | ARC | Implement risk-tiered required sections | feature | medium | p1 | ARC-T-0004 | tusker/tasks |
| MIG-T-0001 | MIG | Build v5 migration dry-run and report | migration | high | p0 | — | tusker/migration |
| MIG-T-0002 | MIG | Migrate story files and references to task files | migration | high | p0 | MIG-T-0001 | tusker/migration |
| MIG-T-0003 | MIG | Migrate bug files to task kind bug | migration | high | p0 | MIG-T-0001, ARC-T-0002 | tusker/migration |
| MIG-T-0004 | MIG | Migrate D-notes to docs pages and docs-map nodes | migration | critical | p0 | ARC-T-0005, DOC-T-0001, MIG-T-0001 | tusker/migration |
| MIG-T-0005 | MIG | Clean legacy frontmatter fields during migration | migration | medium | p1 | ARC-T-0003, MIG-T-0001 | tusker/migration |
| MIG-T-0006 | MIG | Update sample vault and fixtures to v5 | feature | medium | p1 | MIG-T-0002, MIG-T-0003, MIG-T-0004 | tusker/migration |
| MIG-T-0007 | MIG | Add migration rollback and safety report | feature | high | p1 | MIG-T-0001, MIG-T-0002, MIG-T-0003, MIG-T-0004, MIG-T-0005 | tusker/migration |
| DOC-T-0001 | DOC | Introduce docs-map v5 with Diátaxis fields | feature | high | p0 | ARC-T-0003, ARC-T-0005 | tusker/docs-system |
| DOC-T-0002 | DOC | Add Diátaxis-aware doc template | feature | medium | p1 | DOC-T-0001 | tusker/docs-system |
| DOC-T-0003 | DOC | Add first-class agent docs templates | feature | high | p1 | DOC-T-0001, DOC-T-0002 | tusker/docs-system |
| DOC-T-0004 | DOC | Implement docs impact hook with dry-run apply waive | feature | critical | p0 | DOC-T-0001, DOC-T-0005, VAL-T-0002 | tusker/docs-system |
| DOC-T-0005 | DOC | Parse structured knowledge delta and route to doc nodes | feature | high | p0 | ARC-T-0004, DOC-T-0001 | tusker/docs-system |
| DOC-T-0006 | DOC | Generate docs catalog and reader-facing IA | feature | medium | p2 | DOC-T-0001 | tusker/docs-system |
| DOC-T-0007 | DOC | Generate llms.txt and llms-full.txt from docs-map | feature | medium | p2 | DOC-T-0001, DOC-T-0006 | tusker/docs-system |
| DOC-T-0008 | DOC | Add docs freshness index and Obsidian views | feature | medium | p2 | DOC-T-0001, DOC-T-0004 | tusker/docs-system |
| MRK-T-0001 | MRK | Spike restricted Markdoc publish layer | research | medium | p2 | DOC-T-0001, DOC-T-0002 | tusker/markdoc |
| MRK-T-0002 | MRK | Implement Markdoc node-based rendering first | feature | medium | p2 | MRK-T-0001 | tusker/markdoc |
| MRK-T-0003 | MRK | Add restricted published-doc Markdoc tags | feature | medium | p2 | MRK-T-0002, MED-T-0001 | tusker/markdoc |
| MRK-T-0004 | MRK | Add Markdoc validation gate | feature | medium | p2 | MRK-T-0003 | tusker/markdoc |
| MRK-T-0005 | MRK | Export clean plain Markdown from published docs | feature | medium | p2 | MRK-T-0003, DOC-T-0007 | tusker/markdoc |
| MED-T-0001 | MED | Introduce media-map and media asset schema | feature | high | p1 | DOC-T-0001 | tusker/media |
| MED-T-0002 | MED | Add video transcript companion template | feature | medium | p1 | MED-T-0001 | tusker/media |
| MED-T-0003 | MED | Add evidence-to-doc-media promotion workflow | feature | medium | p2 | MED-T-0001, MED-T-0002, DOC-T-0004 | tusker/media |
| MED-T-0004 | MED | Add promo claim ledger and validation | feature | high | p1 | MED-T-0001 | tusker/media |
| MED-T-0005 | MED | Add media evidence attachment indexing | feature | medium | p2 | MED-T-0001, CLI-T-0002 | tusker/media |
| MED-T-0006 | MED | Add media Obsidian views | feature | low | p3 | MED-T-0001, OBS-T-0001 | tusker/media |
| EVAL-T-0001 | EVAL | Define docs eval schema | feature | high | p1 | DOC-T-0001 | tusker/evals |
| EVAL-T-0002 | EVAL | Implement docs eval runner skeleton | feature | medium | p2 | EVAL-T-0001, CLI-T-0001 | tusker/evals |
| EVAL-T-0003 | EVAL | Add agent recipe execution eval | feature | medium | p2 | EVAL-T-0001, DOC-T-0003 | tusker/evals |
| EVAL-T-0004 | EVAL | Add retrieval reference staleness media promo evals | feature | medium | p2 | EVAL-T-0001, MED-T-0001, DOC-T-0007 | tusker/evals |
| EVAL-T-0005 | EVAL | Wire docs evals into close gate by risk and doc type | feature | high | p1 | EVAL-T-0002, DOC-T-0004, VAL-T-0002 | tusker/evals |
| CLI-T-0001 | CLI | Simplify CLI to stable primitive commands | migration | high | p0 | ARC-T-0001, ARC-T-0003 | tusker/cli |
| CLI-T-0002 | CLI | Update task lifecycle and evidence commands | feature | high | p0 | CLI-T-0001, ARC-T-0006 | tusker/cli |
| CLI-T-0003 | CLI | Implement docs check apply waive commands | feature | critical | p0 | CLI-T-0001, DOC-T-0004 | tusker/cli |
| CLI-T-0004 | CLI | Update reindex and generated indexes for v5 | feature | medium | p1 | ARC-T-0006, DOC-T-0001, MED-T-0001, EVAL-T-0001 | tusker/cli |
| CLI-T-0005 | CLI | Update help output aliases and command docs | docs | low | p2 | CLI-T-0001, CLI-T-0002, CLI-T-0003 | tusker/cli |
| VAL-T-0001 | VAL | Enforce UNKNOWN_DOMAIN and UNKNOWN_DOC_NODE | feature | high | p0 | DOC-T-0001 | tusker/tasks |
| VAL-T-0002 | VAL | Enforce docs impact unresolved at close | feature | critical | p0 | DOC-T-0004 | tusker/tasks |
| VAL-T-0003 | VAL | Enforce structured knowledge delta | feature | high | p0 | DOC-T-0005 | tusker/tasks |
| VAL-T-0004 | VAL | Validate Diátaxis mode audience and agent_layer | feature | medium | p1 | DOC-T-0001, DOC-T-0002 | tusker/tasks |
| VAL-T-0005 | VAL | Validate video transcripts and promo claim evidence | feature | medium | p1 | MED-T-0001, MED-T-0002, MED-T-0004 | tusker/tasks |
| VAL-T-0006 | VAL | Validate risk-tier required sections | feature | medium | p1 | ARC-T-0007 | tusker/tasks |
| VAL-T-0007 | VAL | Make validator output agent-actionable | feature | low | p2 | VAL-T-0001 | tusker/tasks |
| SKL-T-0001 | SKL | Rewrite SKILL.md for task doc_nodes v5 model | docs | high | p0 | ARC-T-0001, ARC-T-0003, DOC-T-0001 | agents/tusker-skill |
| SKL-T-0002 | SKL | Update skill reference docs | docs | high | p0 | SKL-T-0001, DOC-T-0001, CLI-T-0001 | agents/tusker-skill |
| SKL-T-0003 | SKL | Replace templates with v5 templates | feature | high | p0 | ARC-T-0004, DOC-T-0002, MED-T-0002, EVAL-T-0001 | agents/tusker-skill |
| SKL-T-0004 | SKL | Integrate Diátaxis documentation skill guidance | docs | medium | p1 | DOC-T-0002, DOC-T-0003 | agents/tusker-skill |
| SKL-T-0005 | SKL | Update AGENTS.md and WORKFLOW.md installers | docs | medium | p1 | SKL-T-0001, SKL-T-0002 | agents/tusker-skill |
| ORC-T-0001 | ORC | Rename daemon scheduling from stories to tasks | migration | high | p1 | ARC-T-0001, CLI-T-0002 | tusker/daemon |
| ORC-T-0002 | ORC | Use frontmatter status plus events audit in orchestration | feature | high | p1 | ARC-T-0006, ORC-T-0001 | tusker/daemon |
| ORC-T-0003 | ORC | Add doc_node locks for concurrent docs updates | feature | medium | p2 | DOC-T-0004 | tusker/daemon |
| ORC-T-0004 | ORC | Write evidence and run records from daemon attempts | feature | medium | p2 | ORC-T-0002, MED-T-0005 | tusker/daemon |
| ORC-T-0005 | ORC | Respect docs close gate before daemon auto-close | feature | critical | p1 | ORC-T-0001, DOC-T-0004, VAL-T-0002 | tusker/daemon |
| OBS-T-0001 | OBS | Update Obsidian dashboards for v5 tasks | feature | medium | p1 | ARC-T-0001, ARC-T-0003, CLI-T-0004 | tusker/obsidian |
| OBS-T-0002 | OBS | Add docs media eval views | feature | low | p2 | DOC-T-0008, MED-T-0006, EVAL-T-0001 | tusker/obsidian |
| OBS-T-0003 | OBS | Ensure Obsidian renders source docs without Markdoc syntax | feature | medium | p2 | MRK-T-0001, MRK-T-0003 | tusker/obsidian |
| OBS-T-0004 | OBS | Add migration note and sample screens | docs | low | p3 | OBS-T-0001, MIG-T-0006 | tusker/obsidian |

## First batch

```text
ARC-T-0001
ARC-T-0002
ARC-T-0003
ARC-T-0004
ARC-T-0005
DOC-T-0001
VAL-T-0001
```

## Second batch

```text
MIG-T-0001
MIG-T-0002
MIG-T-0003
MIG-T-0004
CLI-T-0001
CLI-T-0002
DOC-T-0005
VAL-T-0002
VAL-T-0003
```

## Later

```text
Markdoc publishing
Media/promo
Docs eval automation
Daemon/orchestration
```
