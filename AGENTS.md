<!-- tusker:epic-index:begin -->
## Progressive Tusker context

Start with `tusker list --type epic` to see the short epic roster. Use `tusker/README.md` only when the project overview is needed; it intentionally omits task lists from the top-level roster.

Progressive drill-down: `tusker list --epic <ACR> --type task --open` for one epic's open tasks, then `tusker show <ID> --capsule` for the selected task. Open the full task file only when the capsule is insufficient. Use `tusker search "<term>" --type task` before creating possible duplicates. Use `tusker compact <ID>` as a dry-run before reading or editing old noisy notes.

When logging work: pick the epic whose summary best matches, and announce the ID **plus a one-line rationale for the epic choice**. If nothing fits and the work will outlive one task, create a new epic with `tusker new epic --acronym <ACR> --title "<name>" --summary "..."`.
<!-- tusker:epic-index:end -->
