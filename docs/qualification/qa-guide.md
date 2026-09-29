# QA guide: first end-to-end pass

This guide takes a tester from install to one task landed on `main`, then
lists the failure cases worth trying. The per-harness deep checks (Say, ask,
interrupt, continue, forced failures) are in
[live-harness/README.md](live-harness/README.md); run that after this guide
passes.

Run every command from a normal terminal. Never start the daemon from inside
an agent session.

## 1. Install

```sh
git clone <tusker repo> && cd tusker
git checkout <qa tag>
make install          # macOS: CLI, skills, TuskerBar. Linux: CLI and skills (needs Go and Bun 1.3.14)
tusker version        # record the version and sha256 in your report
tusker runner catalog --refresh --json   # each harness you plan to use: available and authenticated
```

Log in to at least one harness CLI (Claude Code or Codex) before you start.

## 2. Make a scratch project

Use a throwaway repository with a few files and one test command that passes.

```sh
mkdir /tmp/qa-project && cd /tmp/qa-project && git init -q
# add a small source file and a test, then commit them
tusker init --yes
tusker projects add --repo . --vault ./.tusker
```

Set the landing gate to the project's test command. `tusker init` writes
none, and without one the gate falls back to `go build ./...` and every
landing of a non-Go project fails. In `.tusker/config.yaml`, under
`automation:`, add:

```yaml
  validation:
    commands:
      - python3 -m unittest
```

Registration does not start anything. The project is off until step 4.

## 3. Create one task and one wave

The task body needs an acceptance table and a verification table whose
command Tusker recognizes as a test: `pytest`, `python3 -m unittest`,
`go test`, `npm test`, `cargo test`, `make test`, or `test ...`. Running a
script directly (`python3 test_app.py`) does not count, and `new task` warns
that the review will be refused.

```sh
cat > /tmp/body.md <<'EOF'
# Add greet

## Outcome

`greet(name)` in `app.py` returns `hello, <name>`, with a test in `test_app.py`.

## Acceptance

| ID | Outcome | Proof |
| --- | --- | --- |
| A1 | The greet test passes. | command: python3 -m unittest test_app |

## Verification

| Covers | Check | Result |
| --- | --- | --- |
| A1 | command: python3 -m unittest test_app | pending |
EOF
tusker new task --title "Add greet" --work-level light --status ready \
  --owned-paths app.py,test_app.py --body-file /tmp/body.md
tusker wave create "QA wave 1" <TASK-ID>
git add .tusker && git commit -qm "Add QA task"   # arming cuts the wave branch from main
```

Pass: `new task` prints no warning, and `tusker show <TASK-ID>` shows status
`ready`.

## 4. Start the daemon, enable the project, arm the wave

```sh
tusker daemon service start          # or: tusker daemon run   (in its own terminal)
tusker projects enable --repo . --dry-run --json
tusker projects enable --repo . --json
tusker automation explain <TASK-ID>  # only the wave authorization should block
tusker wave start <WAVE-ID> --mode background --by human:<you> --json
```

Open Serve at <http://127.0.0.1:7420> (TuskerBar opens it on macOS).

## 5. Watch the task go through

| Stage | What you should see | Where |
| --- | --- | --- |
| Queued, then Working | One run with a runner, model and session ID | Serve run page, `tusker runs inspect <TASK-ID> --json` |
| Submitted | One commit for the task on the checked-out branch | `git log` |
| Review | A second agent reviews in a separate worktree, then accepts or sends rework | Serve run page |
| Done | Task closed with proof recorded | `tusker show <TASK-ID>` |
| On `main` | The wave lands at the next promotion window, or when you run `tusker land <WAVE-ID>` | `git log main` |

Pass: every stage happens without you touching files, and Serve and the CLI
agree on the state at each stage.

## 6. Failure cases to try

Try each one on a fresh task. Record what Tusker showed you and whether it
told you what to do next.

1. **Failing proof.** Write a task whose acceptance test cannot pass. Expect
   the review to send it back to rework, not close it.
2. **Your own edits in the checkout.** While a task runs, edit an unrelated
   file in the same repository. Expect the landing to leave your edit alone.
3. **Editing a file the task owns.** Change one of the task's owned files
   after it submits. Expect the landing to refuse and name the file.
4. **Kill the daemon mid-run.** Stop it (`tusker daemon stop`, or kill the
   process) while a task is Working, then start it again. Expect the run to
   recover or show a clear reason, never to stay Working forever.
5. **Low disk.** Run with a nearly full disk. Expect `tusker automation
   explain` to report disk pressure instead of silently not starting work.
6. **Agent asks a question.** Put "ask the operator which word to use before
   you start" in a task body. Expect it under Needs you in Serve; your reply
   should reach the same session.

## 7. Report

For every problem, send:

- the exact command and its full output (or a Serve screenshot);
- `tusker runs inspect <TASK-ID> --json` for the task involved;
- `tusker version`;
- what you expected instead.

## Known limits in this build

- After a crash in the middle of a landing, Git's `.git/index.lock` can be
  left behind. Tusker refuses the retry and names the file; remove it only if
  no Git command is running, then retry.
- `tusker init` does not set a landing gate; see step 2.
- On Linux the daemon runs tasks but refuses to land them: scheduled
  landing runs gates in macOS `sandbox-exec` and fails closed with
  "host cannot isolate gate execution". Run the end-to-end pass on macOS.
- Windows is not supported for the daemon or TuskerBar.
- The Serve UI is the review surface; some actions exist only in the CLI.
