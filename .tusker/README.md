---
title: "Overview"
type: "note"
created: "2026-08-23"
updated: "2026-08-23"
tags: ["tusker-generated"]
---

# Project overview

<!-- tusker:overview:begin -->

Tusker is a Go work tracker for this repository. It stores task contracts,
proof, gates, and project canon in `.tusker/`.

The CLI owns tracker state. The daemon is optional. Serve and TuskerBar show
repository and machine state. Source code is the behavior authority.
`docs/system/` explains that behavior.

<!-- tusker:overview:end -->

---

# Epic roster

_Auto-generated 2026-08-23T10:56:31Z. This top-level roster intentionally shows epics only. Run `tusker list --type epic` for the live terminal view, then drill into one epic with `tusker list --epic <ACR> --type task --open`._

Agents: use this page only to choose the right epic. Do not read every task file. Pick the epic whose summary best matches; if nothing fits and the work will outlive one task, propose a new epic with `tusker new epic --acronym <ACR> --title "<name>" --summary "..."`.

_(no epics yet — create one with `tusker new epic --acronym <ACR> --title <title> --summary "..."`)_
