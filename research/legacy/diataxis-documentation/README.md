# diataxis-documentation

An Agent Skill for planning, writing, auditing, and governing documentation using Diátaxis.

This is not a raw scrape of Diátaxis. It is a synthesized, agent-oriented skill derived from the public Diátaxis framework pages and packaged using the Agent Skills folder format.

## Install

Copy this folder into a skills directory recognized by your agent client. Common examples:

```text
.agents/skills/diataxis-documentation/
```

The folder name must match the `name` field in `SKILL.md`.

## What it gives an agent

- A Diátaxis classifier for documentation requests.
- Drafting rules and templates for tutorials, how-to guides, reference, and explanation.
- A method for building documentation architecture and backlog without doing fake top-down architecture.
- Rules for agent-readable documentation: contracts, schemas, constraints, examples, failure modes, and versioning.
- Optional Python scripts for skeleton generation and rough document audits.

## Files

```text
diataxis-documentation/
├── SKILL.md
├── README.md
├── LICENSE.txt
├── references/
│   ├── diataxis-model.md
│   ├── document-types.md
│   ├── documentation-program.md
│   ├── human-and-agent-docs.md
│   ├── quality-gates.md
│   └── source-attribution.md
├── assets/
│   ├── templates/
│   └── checklists/
├── scripts/
│   ├── audit_docset.py
│   ├── classify_doc.py
│   └── generate_doc_skeleton.py
└── evals/
    ├── evals.json
    └── trigger-eval-queries.json
```

## Opinionated defaults

- Start with user need, not site structure.
- Split documents that try to teach, instruct, explain, and specify all at once.
- Publish small improvements. Waiting for the perfect reorg is usually a procrastination tax.
- For agent docs, add parseable contracts and failure modes. Do not just paste prose into an `AGENTS.md` and hope the model behaves.

## Source basis

See `references/source-attribution.md` for source pages and licensing notes.
