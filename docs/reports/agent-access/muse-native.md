# Direct Muse CLI route

The `muse_cli` route invokes the installed `muse` executable directly. The
existing `muse` route remains `codex --profile muse exec ...`; it is not an
alias and no fallback occurs between the two routes.

## Native invocation

Direct execution compiles the selected policy into structured argv:

```text
muse exec --json --workspace <workspace> \
  --approval-mode <never|on-request> \
  --sandbox-network <restricted|enabled>
```

Read-only adds `--disable-write --disable-shell`; network-off adds
`--disable-web-tools`; model and effort use Muse's `--model` and
`--reasoning-effort`. Full legacy access uses Muse's explicit `--yolo` only
when that legacy mode was selected.

`muse exec --json` emits a structured JSONL envelope with schema version,
stream/session identity, sequence, record type/payload type, and terminal
records. The adapter normalizes completed, failed, cancelled, missing-terminal
and session identity outcomes. Unknown records are ignored; a missing terminal
record fails closed.

## Qualification boundary

Installed version observed locally: `Muse Code 1.1.1 (1.1.1-R2514.1)`.
Provider-free echo execution and argv fixtures were run against disposable
paths. Approval callback behavior was NOT RUN; headless routes do not invent a
human approval response. Muse `serve`/MSP schema is discovery/control-plane
evidence only and is not used as the execution transport.
