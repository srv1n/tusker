# Obsidian Bases

Obsidian Bases are generated views over typed markdown contracts. They are not source of truth.

Useful generated views:

```text
Ready Queue      status in [ready,rework], readiness=ready, next_owner=agent*
Needs Shaping    status in [idea,backlog] and missing acceptance/proof
In Review        status=review
Human Gates      readiness=waiting_on_human
Blocked          readiness starts with blocked/waiting_on_ci
Recently Done    status=done
```

Humans may edit task contracts. Generated tags, dashboards, Bases, packets, and indexes can be rebuilt.
