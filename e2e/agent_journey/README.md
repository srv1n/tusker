# Fresh-agent journey fixture

This directory is a disposable, offline acceptance fixture. Copy `fixture/`
to a new temporary Git repository before each run. Use the installed Tusker
candidate supplied by the release operator; do not inspect Tusker source or
reuse a prior task's state. Run `prepare-v2.sh` from the copied root before
launching a worker; it derives the one valid context fingerprint from the
pinned candidate and repository state.

The fixture sets the existing project-local `automation.workspace.strategy`
to `shared` before it derives the planning context. This is for one direct
interactive task with declared non-overlapping owned paths; it is not a daemon
or wave setting. `work start` requires a clean Git tree outside `.tusker`.
Because `tusker init` writes the shipped skills and `docs/system` into the
fixture repository, establish those generated files and the pinned context as
the Git baseline before launching the implementer. The worker then leaves its
scoped implementation material uncommitted for the submit protocol.

The fixture has a locked spec and delivery plan with two independent owned
paths. The implementer prompt deliberately injects a failed first work session
before recovery. The reviewer prompt requires the native `work review` packet
and executes its `next` command verbatim after filling only its verdict,
coverage, and summary placeholders.

The release operator owns all human approval and external-worker launch. This
fixture must not start `tusker daemon run` or `tusker automation dispatch`.
