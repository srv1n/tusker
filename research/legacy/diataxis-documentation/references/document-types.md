# Document type rules

## Tutorial

### Use when

The reader is acquiring skill. They need a guided, safe, concrete learning experience.

### The document must

- Give the learner a clear picture of what they will build or accomplish.
- Keep them on one reliable path.
- Produce visible results early and often.
- Say what the reader should notice after each meaningful action.
- Minimize explanation. Link to explanation instead.
- Remove choices unless the choice itself is the thing being learned.
- Be testable end-to-end by someone close to the target reader.

### Typical structure

```text
# Build <small meaningful thing>

## What we will make
## Before we start
## Step 1: create the starting point
## Step 2: make the first visible change
## Step 3: connect the pieces
## Step 4: run or inspect the result
## What happened
## Clean up or repeat
## Next steps
```

### Language

Use “we” when walking with the learner. Use concrete verbs. Give expected output. Avoid “you will learn”; say what will be made or experienced.

### Failure modes

- Too much theory.
- Too many options.
- Fragile environment setup.
- Steps that do not show results.
- Hidden assumptions about user competence.

## How-to guide

### Use when

The reader is applying existing skill to accomplish a real goal.

### The document must

- Be named after the user's goal, not the system's feature.
- Assume the user is competent enough to operate in the domain.
- Start and end at practical boundaries.
- Give steps, decisions, and checks in a sequence that fits the user's work.
- Include branches for real-world cases.
- Omit background, theory, and exhaustive reference data. Link to them.

### Typical structure

```text
# How to <achieve goal>

## When to use this guide
## Before you begin
## Steps
### 1. Prepare <thing>
### 2. Configure <thing>
### 3. Verify <result>
## Variants
## Troubleshooting
## Related reference
```

### Language

Use direct imperatives. Mention decision points explicitly. Keep pace. Do not pause the work to lecture.

### Failure modes

- Telling the user what obvious UI controls do.
- Teaching beginner concepts.
- Defining every term inline.
- Pretending a messy real-world task is always linear.
- Starting from system operations instead of user purpose.

## Reference

### Use when

The reader needs facts, specifications, options, commands, schemas, fields, constraints, behavior, or error meanings while working.

### The document must

- Be neutral.
- Be accurate and complete within a stated scope.
- Mirror the structure of the system, API, command, configuration, or domain object.
- Use standard patterns consistently.
- Include examples only when they clarify use of the described thing.
- Include warnings where incorrect use is dangerous or irreversible.

### Typical structure

```text
# <Object/API/Command/Config> reference

## Summary
## Syntax or shape
## Parameters / fields / options
## Behavior
## Return values / outputs
## Errors
## Examples
## Version notes
## Related how-to guides
```

### Language

State facts. Use tables and precise labels. Avoid storytelling. Avoid persuasion.

### Failure modes

- Explaining why the feature exists.
- Hiding facts in prose.
- Omitting constraints or defaults.
- Using inconsistent field names.
- Organizing by marketing concepts rather than system shape.

## Explanation

### Use when

The reader needs understanding, context, rationale, implications, history, trade-offs, or a mental model.

### The document must

- Be about a bounded topic.
- Make connections between concepts.
- Explain why something exists or behaves as it does.
- Discuss alternatives and trade-offs when relevant.
- Admit opinion and perspective where the domain requires judgment.
- Avoid turning into instructions or reference tables.

### Typical structure

```text
# About <topic>

## The problem this topic addresses
## The mental model
## Why it works this way
## Trade-offs and alternatives
## Common misunderstandings
## Implications for practice
## Related tutorials, how-to guides, and reference
```

### Language

Use explanatory prose. Define the frame. Make relationships explicit. The reader should finish with better judgment, not just more facts.

### Failure modes

- Endless scope creep.
- Procedure creep.
- Reference creep.
- Vague “overview” pages that do not answer a real why-question.

## Universal document metadata

Use this lightweight metadata when the environment supports frontmatter:

```yaml
title: ""
mode: tutorial | how-to | reference | explanation
audience: ""
reader_state: acquisition | application
reader_need: learning | goal | information | understanding
source_of_truth: ""
owner: ""
review_interval: ""
stale_when:
  - ""
related:
  tutorials: []
  how_to_guides: []
  reference: []
  explanation: []
agent_layer: none | capsule | standalone
```
