---
subject: 2026-09-23-harness-sessions-grill
title: "Harness session decisions — 23 September 2026"
keywords: [harness, exec, resume, steer, mailbox, MCP, hooks, Claude Code, Codex, Muse, Devin]
part_of: run-session-continuity
status: canonical
created: 2026-09-23
read_when: "Understanding why Tusker drives every harness through headless exec plus a mailbox instead of per-provider control protocols."
skip_when: "Implementing the resulting contract; read run-session-continuity."
decides_for: .tusker/specs/run-session-continuity.md
---

# Harness session decisions

These entries record the 23 September 2026 operator discussion that followed an
audit of W-0039 (run session continuity). Audit and research evidence is
summarized, not copied; exact findings live in the tasks that fix them.

## D1 — Four harnesses first

Operator: start with Claude Code, Codex, Muse (Meta Muse Code) and Devin.
Other harnesses come later through the same contract.

Locked: the contract is harness-neutral; each harness is a driver that declares
its capabilities. Users choose a model/profile, never a transport.

## D2 — Headless exec is the primary route; app-server is not

Response proposed: drive Codex through `codex app-server` because it offers
`turn/steer`, typed status and `thread/resume`.

Operator correction: app-server requires Tusker to drive threads and turns and
maintain protocol plumbing; it cannot share a live thread with the operator's
own ChatGPT/Codex session (separate process). Headless exec is preferred.

Locked:

| Harness | Route |
| --- | --- |
| Claude Code | `claude -p --input-format stream-json --output-format stream-json` |
| Codex | `codex exec --json` |
| Muse | `muse exec --json` |
| Devin | `devin acp` (no exec mode; existing ACP driver) |

Codex app-server and Muse MSP (`muse serve`) remain optional future drivers,
not prerequisites. The existing app-server runner is not removed by this
decision.

## D3 — Interrupt plus resume is the universal steering primitive

Locked: "say to a running agent" has two deliveries.

- Soft: a hook in the worker surfaces pending Tusker messages as added context
  after tool calls. Used only where verified for that harness (Claude
  `PostToolUse` additional context is documented; Codex hook context injection
  in exec mode is unverified at decision time). Claude's open stdin
  stream-json channel is an allowed soft path for Claude.
- Hard: interrupt the process, then resume the same native thread with the
  message as the next prompt. Every harness supports native resume:
  `claude -p --resume <id>`, `codex exec <exec-flags> resume <id> -`,
  `muse exec --session-id <id>`, ACP `session/load`.

Continue after failure uses the same hard path with a typed failure summary.
Start fresh is always explicit.

## D4 — Mailbox for cross-messaging; MCP for agent-initiated questions

Operator requirement: stuck workers ask the architect or human; architects and
humans message running workers; the operator's interactive spec session can be
reached.

Research (2026-09-23): all four harnesses accept MCP servers at launch; Claude
MCP tools may block for long periods with progress notifications; Codex
`tool_timeout_sec` defaults to 60s and is configurable. Claude Code sessions
expose a preview inbox socket and "channels"; neither is stable.

Locked:

- Tusker's existing message store (`tusker message ask|reply|list`) is the
  single mailbox. No relay model.
- Workers get an injected Tusker MCP server exposing ask/post/check tools bound
  to their run identity.
- Interactive architect sessions read the mailbox through a hook
  (`UserPromptSubmit`/`Stop`) that lists pending messages. Preview push
  channels are deferred.
- Attaching to a provider conversation Tusker did not start remains
  unsupported (unchanged from agent-coordination).

## D5 — Subscription auth boundary

Research quotes (code.claude.com legal-and-compliance, 2026-09-23): an end user
may run the unmodified Claude Code binary with their own subscription; third
parties may not offer Claude login or route plan credentials for other users.
`--bare` never reads OAuth credentials. OpenAI docs recommend API keys for
programmatic Codex but do not prohibit ChatGPT login.

Locked: Tusker launches only the user's installed binaries on the user's own
login, never brokers credentials, and never uses `--bare` for Claude.

## D6 — Qualification after implementation

Operator: implement the waves, then return to author end-to-end scenarios in
the existing e2e test repository and test manually before promoting a real
project. Fixture and source proof in these waves do not claim live-provider
qualification.
