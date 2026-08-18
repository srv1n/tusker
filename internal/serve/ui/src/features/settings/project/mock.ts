/*
  Project Settings / Details — screen-local mock.

  The authoritative repository/config surfaces live in project/sections.tsx.
  This module keeps the remaining routing, workspace, and landing fixtures
  screen-local until their endpoints exist.

  Provenance model (addendum §1.3): every settings row carries a `source` chip —
  `default` (built-in), `global` (your config), `project` (committed, shared) or
  `local` (machine-only override). Editing a `project`/`global`/`default` row
  never rewrites committed config: it writes a `local` override, which is what
  the write-path in ProjectSettings.tsx models and what "reset to inherited"
  undoes.
*/

export type SettingSource = "default" | "global" | "project" | "local";

export interface SelectOption {
  value: string;
  label: string;
}

/** How a row is edited. `readonly` rows are derived facts, never committed. */
export type SettingControl =
  | { kind: "readonly" }
  | { kind: "toggle" }
  | { kind: "select"; options: SelectOption[] }
  | { kind: "segment"; options: SelectOption[] };

export interface SettingRowData {
  key: string;
  label: string;
  desc?: string;
  control: SettingControl;
  /** Current effective value. Booleans for toggles, strings otherwise. */
  value: string | boolean;
  source: SettingSource;
  /** True when a local override shadows the inherited value. */
  overridden: boolean;
  /** What "reset to inherited" restores. */
  inherited: { value: string | boolean; source: SettingSource };
}

/** Chip presentation + one-line consequence, shown on every settings row. */
export const sourceMeta: Record<
  SettingSource,
  { label: string; cls: string; hint: string }
> = {
  default: {
    label: "default",
    cls: "bg-hover text-faint",
    hint: "Built-in default.",
  },
  global: {
    label: "global",
    cls: "bg-info-soft text-info",
    hint: "Your global config — applies across your projects; teammates don’t see it.",
  },
  project: {
    label: "project",
    cls: "bg-accent-soft text-accent",
    hint: "Committed project config — shared with teammates.",
  },
  local: {
    label: "local",
    cls: "bg-warn-soft text-warn",
    hint: "Machine-only override — saved outside committed config; not shared.",
  },
};

// ---- Row factory -----------------------------------------------------------

function mk(
  key: string,
  label: string,
  control: SettingControl,
  value: string | boolean,
  source: SettingSource,
  opts?: { desc?: string; inherited?: { value: string | boolean; source: SettingSource } },
): SettingRowData {
  return {
    key,
    label,
    control,
    value,
    source,
    desc: opts?.desc,
    overridden: source === "local",
    inherited: opts?.inherited ?? { value, source },
  };
}

// ---- Option sets -----------------------------------------------------------

const concurrencyOptions: SelectOption[] = Array.from({ length: 8 }, (_, i) => ({
  value: String(i + 1),
  label: String(i + 1),
}));

const afterLandingOptions: SelectOption[] = [
  { value: "keep", label: "Keep branch" },
  { value: "delete", label: "Delete branch" },
];

// ---- Details → Worktrees (live, read-only) --------------------------------

export interface WorktreeInfo {
  task: string;
  path: string;
  lease: string;
}

// TODO(api): live worktree leases stream from the daemon; this is a snapshot.
export const worktrees: WorktreeInfo[] = [
  { task: "AGX-T-0003", path: "~/.tusker/workspaces/AGX-T-0003-a1c9", lease: "held · 2m 14s" },
  { task: "CLN-T-0007", path: "~/.tusker/workspaces/CLN-T-0007-3f0b", lease: "held · 43s" },
];

// ---- Routing (project-level) ----------------------------------------------

export interface RoutingRule {
  id: string;
  match: string;
  profile: string;
}

// TODO(api): ordered routing table; reorder + edits persist via the settings API.
export const routingRules: RoutingRule[] = [
  { id: "rr-1", match: "epic = SRV", profile: "docs-fast" },
  { id: "rr-2", match: "risk = high", profile: "review-frontier" },
  { id: "rr-3", match: "keywords: migration, schema, drop", profile: "guarded-yolo" },
];

export const routingFallthrough = "lane mapping → project default";

// ---- Workspace lifecycle (project-level) ----------------------------------

export interface WorkspaceScript {
  key: string;
  label: string;
  desc: string;
  value: string;
}

// TODO(api): scripts + copy globs are stored config; a failed setup fails dispatch.
export const workspaceScripts: WorkspaceScript[] = [
  {
    key: "setup",
    label: "Setup script",
    desc: "Runs once when a workspace is created — install deps, copy env. A non-zero exit fails the dispatch and shows the output tail.",
    value: "bun install --frozen-lockfile\ncp ../../.env .env",
  },
  {
    key: "copy",
    label: "Files to copy",
    desc: "Glob patterns for local-only files each new workspace needs. One pattern per line.",
    value: ".env*\n.dev.vars\n.tusker/local/*",
  },
  {
    key: "archive",
    label: "Archive script",
    desc: "Cleanup before a workspace is removed.",
    value: "rm -rf node_modules/.cache dist",
  },
];

export const portRange = "10 ports from a base (3100, 3110, 3120, …)";

// ---- Landing & parallelism (project-level) --------------------------------
// TODO(api): editable project config; writes land as `local` overrides.

export const landingRows: SettingRowData[] = [
  mk("land.concurrency", "Max concurrent runs", { kind: "select", options: concurrencyOptions }, "4", "local", {
    desc: "How many worktrees may run in parallel for this project.",
    inherited: { value: "2", source: "project" },
  }),
  mk("land.autoMerge", "Auto-merge on green", { kind: "toggle" }, true, "project", {
    desc: "Land a finished branch automatically once every check passes.",
  }),
  mk("land.conflictAssist", "Conflict assist", { kind: "toggle" }, false, "default", {
    desc: "Let an agent attempt conflict resolution before asking you.",
  }),
  mk("land.afterLanding", "After landing", { kind: "segment", options: afterLandingOptions }, "delete", "global", {
    desc: "What to do with the task branch once it lands.",
  }),
];

export const overlapNote =
  "Overlapping-work protection is automatic: if a task’s files overlap a running task it’s queued, and the reason shows on the task card — nothing to configure here.";

// ---- Header meta -----------------------------------------------------------

// TODO(api): path + current branch come from the daemon's project registry.
export const projectMeta: { path: string; branch: string } = {
  path: "~/Downloads/side/tusker",
  branch: "main",
};

// ---- Tabs ------------------------------------------------------------------

export type SettingsTab = "details" | "routing" | "workspace" | "landing";

export const settingsTabs: { key: SettingsTab; label: string }[] = [
  { key: "details", label: "Details" },
  { key: "routing", label: "Routing" },
  { key: "workspace", label: "Workspace" },
  { key: "landing", label: "Landing" },
];
