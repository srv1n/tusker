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
updated_at: "2026-07-11T01:03:02Z"
state_rev: "sha256:0748f65e6b48dc6093eda5cc0bba152b50a6d449aee4cfa85df0c3ad76aa82a5"
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
| [[MAC-G-0001]] | human:sarav | [[MAC-T-0006]] | Launch .build/TuskerBar.app, verify the normal window and Dock icon, then exercise Open Tusker Window, Enter Full Screen, Open in Browser, and Quit from the status menu. |
| [[MAC-G-0002]] | human:sarav | [[MAC-T-0001]] | Exercise 100% full-window content coverage, the panel's Open Tusker shortcut, panel toggle, all-Spaces/full-screen behavior, persistence, and terminal-free daemon startup/reuse. |
| [[MAC-G-0003]] | human:sarav | [[MAC-T-0002]] | Test global hotkey, rebinding, URL override, SMAppService registration, and Dock toggle. |
| [[MAC-G-0004]] | human:sarav | [[MAC-T-0003]] | Drive an attention event through the signed bundle; verify notification click-through, badge sync, and reconnect behavior. |
| [[MAC-G-0005]] | human:sarav | [[MAC-T-0004]] | Use tusker:// links and exercise the documented window.tuskerShell API from Web Inspector. |
| [[MAC-G-0006]] | human:sarav | [[MAC-T-0005]] | Run all three intents from Shortcuts and invoke the App Shortcut phrase from Spotlight. |

## Active work

<!-- tusker:generated active-work -->

| Task | Status | Next owner | Next action |
|---|---|---|---|
| [[MAC-T-0001]] | backlog | human:sarav | Accept, waive, or return rework for MAC-G-0002. |
| [[MAC-T-0002]] | backlog | human:sarav | Accept, waive, or return rework for MAC-G-0003. |
| [[MAC-T-0003]] | backlog | human:sarav | Accept, waive, or return rework for MAC-G-0004. |
| [[MAC-T-0004]] | backlog | human:sarav | Accept, waive, or return rework for MAC-G-0005. |
| [[MAC-T-0005]] | backlog | human:sarav | Accept, waive, or return rework for MAC-G-0006. |
| [[MAC-T-0006]] | ready | human:sarav | Accept, waive, or return rework for MAC-G-0001. |

## Recently completed

<!-- tusker:generated recently-completed -->

| Task | Accepted by | Closed at |
|---|---|---|
| _None._ |  | |
