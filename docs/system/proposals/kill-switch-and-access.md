---
kind: proposal
subject: kill-switch-and-access
keywords: [kill switch, automation off, access, deny list, protected folders, sandbox-exec, yolo]
part_of: real-testing-campaign
status: accepted
code_conformance: unverified
created: 2026-09-27
read_when: "Building the global automation switch, or changing what folders and commands an agent may touch."
skip_when: "Changing per-task routing or profile tiers that do not affect access."
updates: [orchestration, runners-and-acp]
sources: [real-testing-campaign.md]
decisions_locked: true
---

# Global kill switch and agent access

Two owner decisions from 2026-09-27: one switch stops all automation, and
agents run with full access except for a short deny list the owner controls.

## Global automation switch (F11)

Automation has three levels. Each one can be off.

| Level | Command | Off means |
| --- | --- | --- |
| Global | `tusker automation off` / `on` | Nothing dispatches in any project. |
| Project | `tusker projects disable` / `enable` | Nothing dispatches in that project. |
| Wave | `tusker wave pause` / `resume` | Nothing dispatches in that wave. |

Rules:

1. `automation off` stops new dispatch **and interrupts every running
   worker**. Each interrupted run keeps its native session ID, so `automation
   on` followed by Continue resumes it. Interrupted runs show Blocked with
   reason `paused`.
2. The daemon and Serve stay up while automation is off. `daemon stop` is no
   longer the only global stop.
3. The switch is owner-only: an agent session is always refused, whatever
   `agents.act_as_owner` says.
4. Serve and TuskerBar show the switch state at the top of every screen, and
   let the owner flip it.
5. The switch is stored in the daemon database with an audit row (who, when,
   before, after), like project enable.

## Agent access (D1)

Agents run with full access, like the owner's own YOLO sessions. A deny list
blocks the few things that are catastrophic or not the agent's business.

### Default deny list

| Group | Blocked |
| --- | --- |
| Secrets | `~/.ssh`, `~/.aws`, `~/.gnupg`, `~/Library/Keychains` (write only; harness logins read it), `.env` files outside the task's worktree |
| Tusker state | `~/.config/tusker`, `~/Library/Application Support/tusker` |
| Destructive git | `git push --force`, `git reset --hard` on the default branch, deleting remote branches |
| Deletes | recursive delete of anything outside the task's worktree |

### Owner folders

The owner adds folders to protect in the global config, for example
`~/Documents`:

```yaml
access:
  protected_paths:
    - ~/Documents
    - ~/Desktop
```

Protected paths are denied for read and write. The default groups above are
always on; `protected_paths` adds to them.

### How each harness gets the list

Tusker writes the list once. Each harness adapter translates it:

| Harness | Mechanism |
| --- | --- |
| Claude Code | `permissions.deny` rules in the settings passed to `claude -p` |
| Codex | a `sandbox-exec` profile wrapping `codex exec` |
| Muse | a `sandbox-exec` profile wrapping `muse exec` |
| Devin | its permission mode plus a `sandbox-exec` wrapper |

A denied action reaches the task as Blocked with reason `not_allowed`, naming
the path or command.

## Dependencies

- The completion step must accept every harness under this model (campaign
  items F14, F15). Done in Phase 2 (S4): the Codex-sandbox-only rule is gone.
- F11 touches `daemon.go`, so it starts after the F17 lane lands.
- D1 touches the runner adapters, so it starts after the F21 lane lands.

## Acceptance

| ID | Outcome | Proof |
| --- | --- | --- |
| K1 | `automation off` interrupts a running worker and blocks new dispatch; `on` plus Continue resumes the same session. | focused Go test plus a live run in the test repo |
| K2 | An agent session running `automation off` is refused. | focused Go test |
| K3 | Serve shows the switch and flips it. | screenshot |
| A1 | Each harness is refused a write to `~/.ssh` and to a configured `protected_paths` entry, and the task shows Blocked `not_allowed`. | one live run per harness |
| A2 | `git push --force` from a worker is refused. | live run |
