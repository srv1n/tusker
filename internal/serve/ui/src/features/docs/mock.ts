/*
  Screen-local mock — everything the Document / Library screen needs that the
  shared fixtures don't carry yet. Each export names the real endpoint that must
  replace it. Types come from the shared domain model so shapes stay honest.

  TODO(api): all of the below is a stand-in for daemon/vault responses:
    - localDocContents  → GET /api/docs/*path        (bodies fixtures omit)
    - localDocList      → GET /api/docs               (task contracts + demo doc)
    - localTaskDetails  → GET /api/tasks/:id          (review-lane contracts)
    - wikilinkTargets   → GET /api/vault/resolve?ref  (wikilink → capsule preview)
    - mockValidate      → POST /api/docs/*path/validate
    - conflictFor       → 409 CAS payload on POST /api/docs/*path (save)
    - mergeChecksFor    → GET /api/tasks/:id/closeout (merge-readiness rollup)
    - approvalContextFor→ GET /api/needs?kind=approve-spec (blocked count)
*/

import type {
  DocContent,
  DocListEntry,
  RunSummary,
  TaskDetail,
} from "@/types/domain";
import type {
  ApprovalContext,
  ConflictDiff,
  MergeCheck,
  ValidationIssue,
  WikilinkTarget,
} from "./types";

/** Task ids look like `AGX-T-0003`; decisions/specs are file paths. */
export function isTaskId(path: string): boolean {
  return /^[A-Z]{2,4}-T-\d{3,}$/.test(path.trim());
}

/** Stable heading slug — mirrors the outline slugs the API emits. */
export function slugify(text: string): string {
  return text
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/(^-|-$)/g, "");
}

// ----------------------------------------------------------------------------
// Wikilink resolution (hover preview + validation vocabulary)
// ----------------------------------------------------------------------------

export const wikilinkTargets: Record<string, WikilinkTarget> = {
  "AGX-D-0002": {
    id: "AGX-D-0002",
    title: "Leases renew on event emission",
    kind: "decision",
    path: ".tusker/decisions/AGX-D-0002.md",
  },
  "CLN-D-0004": {
    id: "CLN-D-0004",
    title: "Worktree retention after lease",
    kind: "decision",
    path: ".tusker/decisions/CLN-D-0004.md",
  },
  "SRV-SPEC-10": {
    id: "SRV-SPEC-10",
    title: "10 · Tusker Serve",
    kind: "spec",
    path: "docs/specs/10-tusker-serve.md",
  },
  "AGX-T-0003": {
    id: "AGX-T-0003",
    title: "Workflow runner lease + liveness protocol",
    kind: "task",
    path: "AGX-T-0003",
  },
  "AGX-T-0001": {
    id: "AGX-T-0001",
    title: "Daemon registry heartbeat",
    kind: "task",
    path: "AGX-T-0001",
  },
  "CLN-T-0007": {
    id: "CLN-T-0007",
    title: "Vault CAS write-path guard",
    kind: "task",
    path: "CLN-T-0007",
  },
};

export function resolveWikilink(id: string): WikilinkTarget | undefined {
  return wikilinkTargets[id.trim()];
}

// ----------------------------------------------------------------------------
// Doc bodies fixtures omit (so every library link resolves)
// ----------------------------------------------------------------------------

const retentionMd = [
  "# CLN-D-0004 · Worktree retention after lease",
  "",
  "> Decision record. State fields are managed by tusker; edit the body only.",
  "",
  "## Context",
  "",
  "Every dispatched runner executes in an isolated git worktree. When a lease",
  "ends we must decide the worktree's fate. Keeping every worktree forever",
  "exhausts disk; deleting instantly destroys forensics for a failed run.",
  "",
  "## Retention",
  "",
  "Retention: worktrees are deleted immediately after lease. Evidence is copied",
  "into the run's `attempts/` directory before teardown, so nothing an operator",
  "needs is lost. See the acceptance rule in [[SRV-D-0009]] for the guarantee we",
  "must preserve.",
  "",
  "## Decision",
  "",
  "- Copy `evidence/` and the final diff into the run record first.",
  "- Then remove the worktree in the same transaction as lease release.",
  "- A `--keep-worktree` escape hatch stays for local debugging.",
  "",
  "## Rollout",
  "",
].join("\n");

export const localDocContents: Record<string, DocContent> = {
  "docs/specs/05-runner-and-session-protocol.md": {
    path: "docs/specs/05-runner-and-session-protocol.md",
    title: "05 · Runner & Session Protocol",
    kind: "spec",
    updatedAt: "2026-07-05T22:14:00Z",
    rev: "sha256:8b21…d0",
    frontmatter: [
      { key: "id", value: "AGX-SPEC-05", locked: true },
      { key: "status", value: "accepted", locked: true },
      { key: "owner", value: "human:sarav", locked: false },
      { key: "epic", value: "AGX", locked: true },
      { key: "state_rev", value: "31", locked: true },
    ],
    outline: [
      { level: 2, text: "Protocol events", slug: "protocol-events" },
      { level: 2, text: "Lease lifecycle", slug: "lease-lifecycle" },
      { level: 3, text: "Renewal", slug: "renewal" },
      { level: 2, text: "Session handoff", slug: "session-handoff" },
    ],
    markdown: [
      "# 05 · Runner & Session Protocol",
      "",
      "The contract every runner speaks: a stream of protocol events the daemon",
      "consumes to track liveness, tokens, and outcome. See [[AGX-D-0002]] for why",
      "leases renew on these events rather than a separate heartbeat.",
      "",
      "## Protocol events",
      "",
      "| kind | when | carries |",
      "| --- | --- | --- |",
      "| `session.start` | runner attaches | runner, model, lane |",
      "| `tool.call` | each tool use | summary line |",
      "| `turn.complete` | end of a turn | token deltas |",
      "| `lease.expired` | ttl elapsed | last event age |",
      "",
      "## Lease lifecycle",
      "",
      "A lease is acquired before `session.start` with a 120s TTL. Any emitted",
      "event renews it. A process that stops emitting loses its lease and the run",
      "is surfaced as stale.",
      "",
      "### Renewal",
      "",
      "```go",
      "func (l *Lease) Renew(now time.Time) {",
      "\tl.deadline = now.Add(l.ttl)",
      "}",
      "```",
      "",
      "## Session handoff",
      "",
      "On interrupt the runner flushes a final `turn.complete`, releases the lease,",
      "and the reviewer lane may pick up the workspace unchanged.",
    ].join("\n"),
  },

  "docs/design/tusker-serve-ux-packet.md": {
    path: "docs/design/tusker-serve-ux-packet.md",
    title: "Tusker Serve — UX Packet",
    kind: "knowledge",
    updatedAt: "2026-07-06T09:23:00Z",
    rev: "sha256:44af…19",
    frontmatter: [
      { key: "id", value: "SRV-UX-PACKET", locked: true },
      { key: "status", value: "reference", locked: true },
      { key: "owner", value: "human:sarav", locked: false },
    ],
    outline: [
      { level: 2, text: "Product context", slug: "product-context" },
      { level: 2, text: "The organizing principle", slug: "the-organizing-principle" },
      { level: 2, text: "Reader / editor", slug: "reader-editor" },
    ],
    markdown: [
      "# Tusker Serve — UX Packet",
      "",
      "The design brief for the `tusker serve` control room. Where this packet and",
      "the engineering spec [[SRV-SPEC-10]] disagree on UX intent, this wins.",
      "",
      "## Product context",
      "",
      "One operator opens one surface to answer: *what needs me, and what is the",
      "machine doing?* Agents outnumber the human ~10:1 in activity.",
      "",
      "## The organizing principle",
      "",
      "> Attention routing, not chronology. The to-do list first, live machine",
      "> status second, browse/read/edit third.",
      "",
      "## Reader / editor",
      "",
      "Reading is first-class: a comfortable measure, rendered tables, resolved",
      "wikilinks, a left-hand outline. Editing is guard-railed — locked frontmatter,",
      "CAS conflict handling, and inline validation.",
    ].join("\n"),
  },

  ".tusker/decisions/AGX-D-0002.md": {
    path: ".tusker/decisions/AGX-D-0002.md",
    title: "AGX-D-0002 · Leases renew on event",
    kind: "decision",
    updatedAt: "2026-07-06T18:24:00Z",
    rev: "sha256:0c7e…b5",
    frontmatter: [
      { key: "id", value: "AGX-D-0002", locked: true },
      { key: "status", value: "accepted", locked: true },
      { key: "owner", value: "human:sarav", locked: false },
      { key: "epic", value: "AGX", locked: true },
      { key: "state_rev", value: "12", locked: true },
    ],
    outline: [
      { level: 2, text: "Decision", slug: "decision" },
      { level: 2, text: "Consequences", slug: "consequences" },
    ],
    markdown: [
      "# AGX-D-0002 · Leases renew on event",
      "",
      "## Decision",
      "",
      "Leases renew on protocol-event emission, **not** a separate heartbeat timer.",
      "One clock, not two: a runner that is doing work is a runner that is alive.",
      "",
      "## Consequences",
      "",
      "- No background heartbeat goroutine to leak or desync.",
      "- A silent process is indistinguishable from a dead one — which is exactly",
      "  the stall `tusker serve` must surface (see [[SRV-SPEC-10]]).",
    ].join("\n"),
  },

  ".tusker/work/epics/SRV.md": {
    path: ".tusker/work/epics/SRV.md",
    title: "SRV · Tusker Serve",
    kind: "epic",
    updatedAt: "2026-07-06T05:13:44Z",
    rev: "sha256:9d10…7c",
    frontmatter: [
      { key: "id", value: "SRV", locked: true },
      { key: "status", value: "active", locked: true },
      { key: "owner", value: "human:sarav", locked: false },
    ],
    outline: [
      { level: 2, text: "Goal", slug: "goal" },
      { level: 2, text: "Tasks", slug: "tasks" },
    ],
    markdown: [
      "# SRV · Tusker Serve",
      "",
      "## Goal",
      "",
      "Ship the local control-room UI: the single surface the operator uses to",
      "unblock agents, watch runs, and read/edit the vault.",
      "",
      "## Tasks",
      "",
      "- [[AGX-T-0003]] — the liveness protocol this UI renders.",
      "- [[CLN-T-0007]] — the CAS write-path guard behind conflict-safe saves.",
    ].join("\n"),
  },

  ".tusker/decisions/CLN-D-0004.md": {
    path: ".tusker/decisions/CLN-D-0004.md",
    title: "CLN-D-0004 · Worktree retention after lease",
    kind: "decision",
    updatedAt: "2026-07-06T18:02:00Z",
    rev: "sha256:1a44…18",
    frontmatter: [
      { key: "id", value: "CLN-D-0004", locked: true },
      { key: "status", value: "proposed", locked: true },
      { key: "owner", value: "human:sarav", locked: false },
      { key: "epic", value: "CLN", locked: true },
      { key: "state_rev", value: "18", locked: true },
    ],
    outline: [
      { level: 2, text: "Context", slug: "context" },
      { level: 2, text: "Retention", slug: "retention" },
      { level: 2, text: "Decision", slug: "decision" },
      { level: 2, text: "Rollout", slug: "rollout" },
    ],
    markdown: retentionMd,
  },

  // Task contracts, round-tripped to markdown (target of "Open markdown").
  ".tusker/work/tasks/AGX-T-0003.md": {
    path: ".tusker/work/tasks/AGX-T-0003.md",
    title: "AGX-T-0003 · Workflow runner lease + liveness protocol",
    kind: "task",
    updatedAt: "2026-07-06T18:29:53Z",
    rev: "sha256:7c2a…f1",
    frontmatter: [
      { key: "id", value: "AGX-T-0003", locked: true },
      { key: "status", value: "in_progress", locked: true },
      { key: "priority", value: "p1", locked: true },
      { key: "risk", value: "high", locked: true },
      { key: "epic", value: "AGX", locked: true },
      { key: "state_rev", value: "24", locked: true },
      { key: "owner", value: "human:sarav", locked: false },
    ],
    outline: [
      { level: 2, text: "Intent", slug: "intent" },
      { level: 2, text: "Acceptance", slug: "acceptance" },
      { level: 2, text: "Verification", slug: "verification" },
    ],
    markdown: [
      "# AGX-T-0003 · Workflow runner lease + liveness protocol",
      "",
      "## Intent",
      "",
      "Give every dispatched runner a **lease** it must renew by emitting protocol",
      "events. A silent death expires the lease and surfaces the run as stale within",
      "a minute — see [[AGX-D-0002]].",
      "",
      "## Acceptance",
      "",
      "| criterion | proof |",
      "| --- | --- |",
      "| Lease acquired before `session.start`, TTL 120s | pass |",
      "| Renewal piggybacks on any protocol event | pass |",
      "| Expired lease with live process routes to a failed gate | pending |",
      "",
      "## Verification",
      "",
      "```bash",
      "go test ./internal/agent/... -run Lease",
      "```",
    ].join("\n"),
  },

  ".tusker/work/tasks/CLN-T-0007.md": {
    path: ".tusker/work/tasks/CLN-T-0007.md",
    title: "CLN-T-0007 · Vault CAS write-path guard",
    kind: "task",
    updatedAt: "2026-07-06T16:55:00Z",
    rev: "sha256:5e90…07",
    frontmatter: [
      { key: "id", value: "CLN-T-0007", locked: true },
      { key: "status", value: "review", locked: true },
      { key: "priority", value: "p1", locked: true },
      { key: "risk", value: "high", locked: true },
      { key: "epic", value: "CLN", locked: true },
      { key: "state_rev", value: "31", locked: true },
      { key: "owner", value: "human:sarav", locked: false },
    ],
    outline: [
      { level: 2, text: "Intent", slug: "intent" },
      { level: 2, text: "Acceptance", slug: "acceptance" },
    ],
    markdown: [
      "# CLN-T-0007 · Vault CAS write-path guard",
      "",
      "## Intent",
      "",
      "Every vault write is compare-and-swap guarded: a save carrying a stale",
      "`state_rev` is rejected, never silently overwriting a concurrent edit. This",
      "is the invariant behind the reader/editor conflict UX — see [[CLN-D-0004]].",
      "",
      "## Acceptance",
      "",
      "- Runner policy matches hardened `tusker.yaml`.",
      "- A stale-rev save returns 409 with the winning revision.",
    ].join("\n"),
  },
};

/** Task contracts + the editing-demo doc, merged into the library listing. */
export const localDocList: DocListEntry[] = [
  { path: ".tusker/decisions/CLN-D-0004.md", title: "CLN-D-0004 · Worktree retention after lease", kind: "decision", updatedAt: "2026-07-06T18:02:00Z" },
  { path: "AGX-T-0003", title: "Workflow runner lease + liveness protocol", kind: "task", updatedAt: "2026-07-06T18:29:53Z" },
  { path: "CLN-T-0007", title: "Vault CAS write-path guard", kind: "task", updatedAt: "2026-07-06T16:55:00Z" },
];

// ----------------------------------------------------------------------------
// Task contracts fixtures omit (review-lane closeout demo)
// ----------------------------------------------------------------------------

const clnReviewRun: RunSummary = {
  taskId: "CLN-T-0007",
  taskTitle: "Vault CAS write-path guard",
  projectId: "tusker",
  runner: "claude",
  model: "claude-opus-4-8",
  lane: "review",
  leaseState: "held",
  outcome: "running",
  elapsedSec: 143,
  sinceLastEventSec: 96,
  liveness: "stale",
  tokens: { input: 96010, output: 8820 },
  attemptCount: 1,
};

export const localTaskDetails: Record<string, TaskDetail> = {
  "CLN-T-0007": {
    id: "CLN-T-0007",
    title: "Vault CAS write-path guard",
    epicId: "CLN",
    epicTitle: "Cleanup & store hygiene",
    status: "review",
    readiness: "blocked_gate",
    priority: "p1",
    risk: "high",
    hasGate: true,
    updatedAt: "2026-07-06T16:55:00Z",
    intent:
      "Guarantee every vault write is **compare-and-swap** guarded: a save carrying a stale `state_rev` is rejected, never silently overwriting an agent's concurrent edit. This is the invariant that makes the reader/editor's conflict UX honest — see [[CLN-D-0004]].",
    acceptance: [
      { id: "a1", text: "Runner policy matches hardened tusker.yaml", proof: "pass" },
      { id: "a2", text: "Lease renewal cancels on process exit", proof: "pass" },
      { id: "a3", text: "Stale-run detector fires within 60s", proof: "fail" },
    ],
    nonGoals: [
      "No three-way auto-merge — conflicts are surfaced to the human, not resolved silently.",
      "No optimistic UI that assumes a save succeeded before the CAS check returns.",
    ],
    verification: [
      { id: "v1", command: "go test ./internal/store/... -run CAS", result: "pass" },
      { id: "v2", command: "go test ./internal/store/... -run StaleDetector", result: "fail", detail: "detector fires at 74s, budget is 60s" },
    ],
    evidence: [
      { id: "e1", label: "cas_guard.go diff", kind: "diff", ref: "attempts/2/cas_guard.go.patch" },
      { id: "e2", label: "stale-detector trace", kind: "log", ref: "attempts/2/stale-detector.log" },
    ],
    knowledgeDelta:
      "Confirms [[CLN-D-0004]] — evidence is copied before worktree teardown, so a rejected save loses nothing.",
    deps: [{ id: "AGX-T-0003", title: "Workflow runner lease + liveness protocol", status: "in_progress" }],
    gates: [{ id: "CLN-G-0003", kind: "review", owner: "human:sarav" }],
    runHistory: [clnReviewRun],
  },
};

// ----------------------------------------------------------------------------
// Merge-readiness closeout (Conductor "Checks" pattern — packet §9)
// ----------------------------------------------------------------------------

export function mergeChecksFor(taskId: string): MergeCheck[] {
  if (taskId === "CLN-T-0007") {
    return [
      { id: "c1", label: "Git status", detail: "worktree clean · 4 files", state: "pass" },
      { id: "c2", label: "Acceptance", detail: "2 of 3 criteria passing", state: "fail" },
      { id: "c3", label: "Verification", detail: "1 of 2 commands green", state: "fail" },
      { id: "c4", label: "Review threads", detail: "no open threads", state: "pass" },
      { id: "c5", label: "Open gates", detail: "1 review gate held by you", state: "pending" },
    ];
  }
  return [];
}

// ----------------------------------------------------------------------------
// Approve-spec context (packet §5 moment 5)
// ----------------------------------------------------------------------------

export function approvalContextFor(path: string, status: string): ApprovalContext | null {
  // A draft spec awaiting the human gate blocks downstream work.
  if (status === "draft" && path.endsWith("10-tusker-serve.md")) {
    return { blocked: 6 };
  }
  return null;
}

// ----------------------------------------------------------------------------
// CAS conflict (packet §4.6 — the critical moment)
// ----------------------------------------------------------------------------

/** Armed for the retention decision: an agent advanced it while you edited. */
export function conflictFor(path: string): ConflictDiff | null {
  if (path !== ".tusker/decisions/CLN-D-0004.md") return null;
  const theirMarkdown = retentionMd.replace(
    "Retention: worktrees are deleted immediately after lease.",
    "Retention: worktrees are archived one cycle, then deleted after lease.",
  );
  return {
    agent: "codex · API-T-0090",
    agoLabel: "40s ago",
    fromRev: "18",
    toRev: "19",
    hunkLabel: "Retention",
    yours: [
      { text: "Retention: worktrees are " },
      { text: "deleted immediately", mark: "add" },
      { text: " after lease." },
    ],
    theirs: [
      { text: "Retention: worktrees are " },
      { text: "archived one cycle, then deleted", mark: "del" },
      { text: " after lease." },
    ],
    theirMarkdown,
  };
}

// ----------------------------------------------------------------------------
// Note-level validation (packet §4.6 — inline errors/warnings)
// ----------------------------------------------------------------------------

const WIKILINK_RE = /\[\[([^\]|]+?)(?:\|[^\]]*)?\]\]/g;

export function mockValidate(markdown: string): ValidationIssue[] {
  const issues: ValidationIssue[] = [];

  // Broken wikilinks → errors (an invalid save must be rejected).
  for (const m of markdown.matchAll(WIKILINK_RE)) {
    const id = m[1]?.trim() ?? "";
    if (id && !resolveWikilink(id)) {
      issues.push({
        severity: "error",
        anchor: id,
        message: `Acceptance criterion references [[${id}]] — no such decision in the vault.`,
      });
    }
  }

  // Empty headings → warnings (sections should have content before save).
  const lines = markdown.split("\n");
  for (let i = 0; i < lines.length; i++) {
    const h = /^(#{1,6})\s+(.*\S)\s*$/.exec(lines[i] ?? "");
    if (!h) continue;
    let hasBody = false;
    for (let j = i + 1; j < lines.length; j++) {
      const line = (lines[j] ?? "").trim();
      if (/^#{1,6}\s/.test(line)) break;
      if (line.length > 0) {
        hasBody = true;
        break;
      }
    }
    if (!hasBody) {
      const title = (h[2] ?? "").replace(/^\d+\s*·\s*/, "");
      issues.push({
        severity: "warn",
        anchor: slugify(h[2] ?? ""),
        message: `Heading “${title}” is empty — sections should have content before save.`,
      });
    }
  }

  return issues;
}

/** Where a task's markdown contract lives (for "open in editor"). */
export function taskDocPath(taskId: string): string {
  // TODO(api): the daemon exposes the contract's real vault path.
  return `.tusker/work/tasks/${taskId}.md`;
}
