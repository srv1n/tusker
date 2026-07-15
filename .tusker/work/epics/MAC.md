---
schema: "tusker.epic/v7"
kind: "epic"
id: "MAC"
project: "tusker"
title: "Tusker Mac menu bar shell"
status: "ready"
owner: "human:sarav"
priority: "p2"
domains: []
next_task_number: 1
next_gate_number: 1
next_decision_number: 1
created_at: "2026-07-10T13:18:03Z"
updated_at: "2026-07-15T10:38:07Z"
state_rev: "sha256:b41f54aa9ef9b7e85fbf1d3c4c9ed77622e7487fe166ae60623d7565b43bf626"
---

# MAC · Tusker Mac menu bar shell

## Thesis

Thin native macOS shell around the serve UI: menu bar presence, floating WKWebView panel, global hotkey, OS notifications, deep links. All state stays in the daemon; the shell is chrome.

## Success criteria

- [ ] Define success criteria.

## Current decision

TBD.

## Open gates

<!-- tusker:generated open-gates -->

| Gate | Owner | Blocks | Action |
|---|---|---|---|
| [[MAC-G-0004]] | human:sarav | [[MAC-T-0003]] | Drive an attention event through the signed bundle; verify notification click-through, badge sync, and reconnect behavior. |
| [[MAC-G-0005]] | human:sarav | [[MAC-T-0004]] | Use tusker:// links and exercise the documented window.tuskerShell API from Web Inspector. |
| [[MAC-G-0006]] | human:sarav | [[MAC-T-0005]] | Run all three intents from Shortcuts and invoke the App Shortcut phrase from Spotlight. |
| [[MAC-G-0007]] | human:sarav | [[MAC-T-0008]] | Open Spotlight, search a project name and a task ID/title, and select each result to verify the native Tusker window lands on the exact project or task. |

## Active work

<!-- tusker:generated active-work -->

| Task | Status | Next owner | Next action |
|---|---|---|---|
| [[MAC-T-0001]] | backlog | agent | Wait for dependency MAC-T-0006 to reach review with satisfied proof or done. |
| [[MAC-T-0002]] | review | blocked_dependency | Wait for dependency MAC-T-0001 to reach review with satisfied proof or done. |
| [[MAC-T-0003]] | backlog | human:sarav | Accept, waive, or return rework for MAC-G-0004. |
| [[MAC-T-0004]] | backlog | human:sarav | Accept, waive, or return rework for MAC-G-0005. |
| [[MAC-T-0005]] | backlog | human:sarav | Accept, waive, or return rework for MAC-G-0006. |
| [[MAC-T-0006]] | review | reviewer | Review evidence and close or return to rework. |
| [[MAC-T-0008]] | ready | human:sarav | Accept, waive, or return rework for MAC-G-0007. |

## Recently completed

<!-- tusker:generated recently-completed -->

| Task | Accepted by | Closed at |
|---|---|---|
| [[MAC-T-0007]] | reviewer:codex | 2026-07-12T01:00:28Z |
| [[MAC-T-0009]] | reviewer:codex | 2026-07-15T06:12:05Z |
| [[MAC-T-0010]] | reviewer:codex | 2026-07-15T10:38:07Z |
