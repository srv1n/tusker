# Diátaxis model for agents

## The compass

Classify every documentation request with two binary choices:

```text
Question 1: What does the content primarily inform?
- Action: the reader does something.
- Cognition: the reader knows, checks, reasons, or understands something.

Question 2: What is the reader doing with the craft?
- Acquisition: the reader is learning or building competence.
- Application: the reader is already competent enough to work.
```

Result:

```text
Action + Acquisition      -> Tutorial
Action + Application      -> How-to guide
Cognition + Application   -> Reference
Cognition + Acquisition   -> Explanation
```

## The map

```text
                             acquisition / study
                    +--------------------------------+
                    |                                |
                    |  Tutorial       Explanation    |
                    |  guided action  understanding  |
                    |                                |
action / doing      +--------------------------------+ cognition / knowing
                    |                                |
                    |  How-to guide   Reference      |
                    |  work action    facts/spec     |
                    |                                |
                    +--------------------------------+
                              application / work
```

## Decision table

| User asks | They need | Write |
|---|---|---|
| “Teach me to…” | Safe learning experience | Tutorial |
| “How do I…?” | Goal-oriented practical direction | How-to guide |
| “What is this option/endpoint/error?” | Accurate facts | Reference |
| “Why does this work this way?” | Context and mental model | Explanation |
| “Can you organize all our docs?” | A needs-based map and incremental backlog | Documentation program |
| “Make this usable by agents.” | Contracts, constraints, examples, failure modes | Agent-facing reference/runbook, mapped through Diátaxis |

## Boundary rules

### Tutorial vs how-to guide

Tutorials are for learners. They use a controlled, safe path. The writer owns the learner's success.

How-to guides are for competent users at work. They solve a real problem and may branch because real work is messy. The user owns their work; the guide must be practical and adaptable.

Red flag: A page titled “Tutorial” that says “choose the option that fits your environment” is probably a how-to guide. A page titled “How to build your first app” that carefully controls every step for a beginner is probably a tutorial.

### Reference vs explanation

Reference is consulted. It is neutral, structured, complete, and boring in a good way. It follows the structure of the thing documented.

Explanation is read. It discusses context, rationale, trade-offs, history, and relationships. It may include opinion and perspective.

Red flag: A reference page with long “why” sections is probably mixing explanation. An explanation page full of option tables is probably hiding reference.

## Handling mixed content

When a document mixes modes:

1. Identify the dominant user need.
2. Extract unrelated material into linked adjacent documents.
3. Keep only the minimal cross-mode content required for the reader to continue.
4. Add links with explicit labels: “For details, see Reference”; “For background, see Explanation”; “For a complete walkthrough, see Tutorial.”

## Complex hierarchies

Do not force every documentation system into four top-level folders. Use four modes as the organizing principle, not as a dumb folder mandate.

Choose the outer hierarchy by what the user experiences as the product:

```text
Small product:
Home
├── Tutorials
├── How-to guides
├── Reference
└── Explanation
```

```text
Complex product with distinct user worlds:
Home
├── For operators
│   ├── Tutorials
│   ├── How-to guides
│   ├── Reference
│   └── Explanation
├── For developers
│   ├── Tutorials
│   ├── How-to guides
│   ├── Reference
│   └── Explanation
└── For contributors
    ├── How-to guides
    ├── Reference
    └── Explanation
```

The correct structure is the one that lets a user find the right mode for their actual work or study. Complexity is acceptable when it reflects real user needs. Fake simplicity that hides needs is worse.
