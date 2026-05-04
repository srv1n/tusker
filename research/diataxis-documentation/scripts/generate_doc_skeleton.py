#!/usr/bin/env python3
"""Generate a starter Markdown skeleton for a Diátaxis document type."""
from __future__ import annotations

import argparse
from pathlib import Path

TEMPLATES = {
"tutorial": """---
title: "{title}"
mode: tutorial
audience: "{audience}"
reader_state: acquisition
reader_need: learning
source_of_truth: ""
stale_when: []
---

# {title}

## What we will make

In this tutorial, we will create <concrete outcome>. By the end, we will have <visible result>.

## Before we start

You need:

- <requirement>

## Step 1: <first concrete action>

```bash
<command>
```

You should see:

```text
<expected output>
```

Notice <thing>.

## Step 2: <next concrete action>

```bash
<command>
```

## What happened

<Minimal explanation. Link to deeper explanation.>

## Next steps

- <link>
""",
"how-to": """---
title: "{title}"
mode: how-to
audience: "{audience}"
reader_state: application
reader_need: goal
source_of_truth: ""
stale_when: []
---

# {title}

## When to use this guide

Use this guide when <situation>.

## Before you begin

You need:

- <precondition>

## Steps

### 1. <prepare>

```bash
<command>
```

### 2. <do the thing>

```bash
<command>
```

### 3. Verify the result

```bash
<command>
```

## Variants

### If <condition>

Do <action>.

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| <symptom> | <cause> | <fix> |
""",
"reference": """---
title: "{title}"
mode: reference
audience: "{audience}"
reader_state: application
reader_need: information
source_of_truth: ""
stale_when: []
---

# {title}

## Summary

<Precise description.>

## Syntax / shape

```text
<syntax/schema/endpoint/command>
```

## Parameters / fields / options

| Name | Type | Required | Default | Description |
|---|---:|---:|---:|---|
| <name> | <type> | <yes/no> | <default> | <description> |

## Behavior

- <fact>

## Errors

| Error | Meaning | User action |
|---|---|---|
| <error> | <meaning> | <action> |

## Examples

```bash
<example>
```
""",
"explanation": """---
title: "{title}"
mode: explanation
audience: "{audience}"
reader_state: acquisition
reader_need: understanding
source_of_truth: ""
stale_when: []
---

# {title}

## The problem this topic addresses

<Frame the topic.>

## The mental model

<Explain how to think about it.>

## Why it works this way

<Rationale, constraints, history, or design decisions.>

## Trade-offs and alternatives

| Approach | Works well when | Costs / risks |
|---|---|---|
| <approach> | <condition> | <cost> |

## Common misunderstandings

- <misunderstanding>: <correction>
""",
"agent-reference": """---
title: "{title}"
mode: reference
reader: agent
audience: "{audience}"
source_of_truth: ""
version: ""
stale_when: []
---

# {title}

## Scope

<Scope and exclusions.>

## Source of truth

- <path or URL>

## Inputs

| Name | Type | Required | Default | Constraints |
|---|---|---:|---|---|
| <name> | <type> | <yes/no> | <default> | <constraints> |

## Outputs

| Name | Type | Meaning |
|---|---|---|
| <name> | <type> | <meaning> |

## Commands / API calls

```bash
<command>
```

## Error handling

| Error | Meaning | Agent action |
|---|---|---|
| <error> | <meaning> | <action> |

## Stale triggers

- <trigger>
""",
"agent-runbook": """---
title: "{title}"
mode: how-to
reader: agent
audience: "{audience}"
source_of_truth: ""
requires_confirmation: false
stale_when: []
---

# {title}

## Scope

Use this runbook to <task>. Do not use it for <excluded task>.

## Preconditions

- <precondition>

## Inputs

| Name | Type | Required | Validation |
|---|---|---:|---|
| <name> | <type> | <yes/no> | <validation> |

## Procedure

### 1. Validate starting state

```bash
<command>
```

### 2. Execute change

```bash
<command>
```

### 3. Validate final state

```bash
<command>
```

## Failure modes

| Failure | Detection | Agent action |
|---|---|---|
| <failure> | <check> | <action> |
""",
}


def main() -> None:
    parser = argparse.ArgumentParser(description="Generate a Diátaxis document skeleton.")
    parser.add_argument("--type", required=True, choices=sorted(TEMPLATES), help="Document type")
    parser.add_argument("--title", required=True, help="Document title")
    parser.add_argument("--audience", default="", help="Reader/audience")
    parser.add_argument("--output", type=Path, help="Output path. Defaults to stdout.")
    args = parser.parse_args()

    text = TEMPLATES[args.type].format(title=args.title, audience=args.audience)
    if args.output:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(text, encoding="utf-8")
        print(f"Wrote {args.output}")
    else:
        print(text)


if __name__ == "__main__":
    main()
