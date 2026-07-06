# Agent Feedback

- context: Codex run executed repo-contract/skill sync from a temp worktree (/tmp/tuskerwf.*)
- friction: The sync rewrote the real repo's AGENTS.md/CLAUDE.md bootstrap pointers to absolute temp paths, recreating the exact dead-pointer bug CLN-T-0001 fixes
- product-idea: Pointer generation must always emit repo-relative paths regardless of invocation cwd, with a regression test
- impact: Agents bootstrapping from the polluted pointers read a nonexistent vault; silent trust break
- related: CLN-T-0001
