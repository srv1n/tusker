# Signs

- Gates over records: write only artifacts a gate consumes (verify rows
  covering acceptance) or a human decision. No progress logs, no narrative
  evidence, no transcripts. See .tusker/specs/decisions/2026-07-22-gates-over-records.md.
- Smallest proof that covers the contract. One command row is a complete
  proof for a small task. Do not pad verification tables.
- Guard refused you with a no-decision remedy (open attempt, use proposal)?
  Apply the remedy, continue, report one line. Do not discuss the guard.
- Verification is command + PASS/FAIL + first actionable failure. Noisy logs
  go to .tusker/scratch/<TASK-ID>/, never into task markdown. Scratch is
  deleted when the task closes and swept after 14 days regardless; promote
  anything worth keeping to evidence first.
- Proof commands must run the package that owns the tests (cmd/tusker, not
  embed-only internal/serve). A green run of zero tests is not proof.
- Raw `|` inside a markdown table cell splits the row and breaks the
  covers-parser. Use pipe-free -run regexes in verify rows.
