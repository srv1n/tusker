# Tusker Signs

- Use `rtk` for repository commands unless a command must deliberately bypass the wrapper.
- Prefer `tusker packet <TASK-ID> --for agent` over broad vault scans.
- Keep proof compact: command plus PASS/FAIL and the first actionable failure.
- Do not paste raw logs into task markdown; use `.tusker/scratch/<TASK-ID>/` for noisy artifacts.
- Broad validation runs through `make build-go`, `make vet`, `make test`, or `make check`; raw `cmd/tusker` tests also auto-join the same cross-worktree lock. Do not launch parallel Go gates.
