# Acceptance criteria vs deliverables

These are not the same thing.

## Acceptance criteria

Acceptance criteria are statements about the finished world.

Good:
- When autocomplete times out, the user can still submit an address manually.
- A new user can complete setup in under 5 minutes following the quickstart.

Bad:
- Add timeout handling.
- Write tests.
- Update the docs.

Those are actions, not verdicts.

## Deliverables

Deliverables are the proof package required to justify the claim.

Examples:
- demo video
- screenshots
- unit/integration/e2e test output
- logs or traces
- rollout note
- doc patch

## Verification

Verification maps evidence to the criteria.

```text
AC: user can complete setup in under 5 minutes
Proof: demo video + clean setup transcript
Verification: replay in clean environment
```

## Rule of thumb

```text
Acceptance criteria = what must be true
Deliverables        = what must be shown
Verification        = how truth is checked
Docs impact         = what durable knowledge must change
```
