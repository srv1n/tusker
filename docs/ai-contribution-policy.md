# AI contribution policy

AI-assisted work is allowed. The configured Git user owns the change.

## Repository rule

Do not add AI attribution to commits, pull requests, source files, or generated
metadata. Do not add agent co-author trailers or generated-by lines.

The contributor must understand the final behavior. The contributor must check
the result with the same commands that a non-AI change needs.

## Evidence

Keep proof about the code, not about the tool that wrote it. Record:

- the requested outcome;
- the changed paths;
- exact checks and results;
- known limits; and
- the reviewer focus.

Raw transcripts are not proof. A Tusker packet is context, not approval.

## Scope

Do not make unrelated cleanup. Do not overwrite user changes. Keep a dirty
worktree intact unless the task owns the changed paths.

## Sources

- `AGENTS.md`
- `CLAUDE.md`
- `.tusker/WORKFLOW.md`
- `skills/tusker/SKILL.md`
