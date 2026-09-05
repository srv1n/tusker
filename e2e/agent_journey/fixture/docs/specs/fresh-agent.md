---
subject: fresh-agent
part_of: overview
status: canonical
---

# Fresh-agent acceptance fixture

## Locked decision

Create the greeting implementation only in the declared task-owned path. Do
not alter the sibling task's path or Tusker control records.

## Acceptance

- `owned/greeting.txt` contains exactly `hello from a fresh agent` followed by
  one newline.
- The independent sibling remains untouched.
- A failure is recorded before recovery; recovery must use a new work session.
- A different reviewer records the final review from the supplied immutable
  implementation workspace.
