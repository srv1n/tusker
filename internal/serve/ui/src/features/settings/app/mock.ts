/*
  App-settings mock data. Most of these values are served by the settings API
  (they are NOT in the shared fixtures), so they live here as typed local mock
  and are marked `// TODO(api)` where a real endpoint must supply / persist them.

  Provenance (§1.3 of the settings UX addendum): every row carries a source —
  `default` | `global` | `project` | `local` (machine-only). Editing an app-level
  row writes to the user's global config; the chip also tells the operator whether
  teammates will see the value. Source → tone maps 1:1 onto the shared tone system.
*/

import type { Tone } from "@/components/ui/tone";

export type SettingSource = "default" | "global" | "project" | "local";

/** Source → tone. default=neutral, global=info(blue), project=accent(violet), local=warn(amber). */
export const sourceTone: Record<SettingSource, Tone> = {
  default: "neutral",
  global: "info",
  project: "accent",
  local: "warn",
};

export type Harness = "codex" | "claude-code";
export type Density = "Comfortable" | "Compact";
export type Delivery = "macOS" | "In-app" | "Both";

export interface SelectRow {
  key: string;
  value: string;
  options: string[];
  source: SettingSource;
  /** Overridden away from the inherited value → shows a "reset to inherited" affordance. */
  overridden?: boolean;
}

export interface ReadonlyRow {
  key: string;
  value: string;
  source: SettingSource;
}

export interface ToggleRow {
  key: string;
  on: boolean;
  source: SettingSource;
}

export interface RunnerProfile {
  name: string;
  harness: Harness;
  model: string;
  effort: string;
  preset: string;
  subagents: string;
  builtin: boolean;
}

export interface PermissionPreset {
  key: "full" | "guarded" | "workspace";
  label: string;
  desc: string;
}

export interface DenylistEntry {
  pattern: string;
  builtin: boolean;
}

// ---- Option lists ----------------------------------------------------------
// TODO(api): harness / model / effort option lists come from the harness registry.
export const runnerOptions = ["codex", "claude-code"];
export const modelOptions = ["gpt-5-codex", "sonnet-4.6", "opus-4.6"];
export const effortOptions = ["minimal", "low", "medium", "high"];
export const densityOptions: Density[] = ["Comfortable", "Compact"];
export const deliveryOptions: Delivery[] = ["macOS", "In-app", "Both"];

// ---- General → Defaults ----------------------------------------------------
// TODO(api): read/write through the settings API (writes land in the global config).
export const defaultRows: SelectRow[] = [
  { key: "Default runner", value: "codex", options: runnerOptions, source: "global" },
  { key: "Default model", value: "gpt-5-codex", options: modelOptions, source: "global" },
  { key: "Reasoning effort", value: "medium", options: effortOptions, source: "default" },
];

// ---- General → Daemon ------------------------------------------------------
// Read-only / machine-derived here. `Port` is wired to the live daemon in the
// section; the rest are // TODO(api): served by the daemon-config endpoint.
export const daemonRows: ReadonlyRow[] = [
  { key: "Global concurrency", value: "8", source: "global" },
  { key: "Vault root", value: "~/code", source: "local" },
  { key: "Event retention", value: "7 days", source: "default" },
];

// ---- Runner profiles -------------------------------------------------------
// TODO(api): profiles are stored config; built-ins ship with the daemon.
export const runnerProfiles: RunnerProfile[] = [
  { name: "default", harness: "codex", model: "gpt-5-codex", effort: "medium", preset: "Full access", subagents: "up to 3", builtin: true },
  { name: "docs-fast", harness: "claude-code", model: "sonnet-4.6", effort: "low", preset: "Workspace-only", subagents: "none", builtin: true },
  { name: "review-frontier", harness: "claude-code", model: "opus-4.6", effort: "high", preset: "Guarded full access", subagents: "up to 2", builtin: true },
  { name: "guarded-yolo", harness: "codex", model: "gpt-5-codex", effort: "high", preset: "Guarded full access", subagents: "up to 5", builtin: true },
];

// ---- Permission presets ----------------------------------------------------
export const permissionPresets: PermissionPreset[] = [
  { key: "full", label: "Full access", desc: "No sandbox, no approvals — the operator’s usual mode." },
  { key: "guarded", label: "Guarded full access", desc: "Full filesystem and network, but a denylist blocks destructive commands." },
  { key: "workspace", label: "Workspace-only", desc: "Writes confined to the workspace; a separate toggle controls network access." },
];

// ---- Denylist --------------------------------------------------------------
// Built-ins are non-deletable; operator entries append below and can be removed.
// TODO(api): persist the operator-added patterns through the settings API.
export const initialDenylist: DenylistEntry[] = [
  { pattern: "git push --force", builtin: true },
  { pattern: "rm -rf outside the workspace", builtin: true },
  { pattern: "git reset --hard", builtin: true },
  { pattern: "writes to *.env / credential files", builtin: true },
  { pattern: "curl … | sh", builtin: false },
];

// ---- Notifications ---------------------------------------------------------
// TODO(api): persist through the settings API (global config).
export const notifyRows: ToggleRow[] = [
  { key: "Human gate", on: true, source: "global" },
  { key: "Stale run", on: true, source: "global" },
];
