import type { DocContent, DocMeta, TaskCapsule } from "@/types/domain";

type FrontmatterField = DocMeta["frontmatter"][number];

export type FrontmatterControlKind = "enum" | "date" | "text";

export interface FrontmatterFieldDefinition {
  kind: FrontmatterControlKind;
  options?: readonly string[];
  labels?: Record<string, string>;
}

export type FrontmatterTarget =
  | { kind: "task"; id: string }
  | { kind: "doc"; path: string };

export interface FrontmatterUpdateInput {
  target: FrontmatterTarget;
  key: string;
  value: string;
}

export const TASK_STATUS_VALUES = [
  "idea",
  "backlog",
  "ready",
  "review",
  "rework",
  "done",
  "cancelled",
  "superseded",
] as const;

export const TASK_READINESS_VALUES = [
  "ready",
  "blocked_by_gate",
  "blocked_by_dependency",
  "waiting_on_review",
  "waiting_on_human",
  "waiting_on_ci",
  "held",
  "done",
  "cancelled",
  "superseded",
] as const;

export const TASK_PRIORITY_VALUES = ["p0", "p1", "p2", "p3"] as const;
export const TASK_RISK_VALUES = ["low", "medium", "high", "critical"] as const;

const STATUS_LABELS: Record<string, string> = {
  idea: "Idea",
  backlog: "Backlog",
  ready: "Ready",
  review: "Review",
  rework: "Rework",
  done: "Done",
  cancelled: "Cancelled",
  superseded: "Superseded",
};

const READINESS_LABELS: Record<string, string> = {
  ready: "Ready",
  blocked_by_gate: "Blocked by gate",
  blocked_by_dependency: "Blocked by dependency",
  waiting_on_review: "Waiting on review",
  waiting_on_human: "Waiting on human",
  waiting_on_ci: "Waiting on CI",
  held: "Held",
  done: "Done",
  cancelled: "Cancelled",
  superseded: "Superseded",
};

function normalizedKey(key: string): string {
  return key.trim().toLowerCase();
}

function isDateKey(key: string): boolean {
  const k = normalizedKey(key);
  return (
    k.endsWith("_at") ||
    k.endsWith("_date") ||
    k === "date" ||
    k === "created" ||
    k === "updated" ||
    k === "started" ||
    k === "completed"
  );
}

export function frontmatterFieldDefinition(key: string): FrontmatterFieldDefinition {
  switch (normalizedKey(key)) {
    case "status":
      return { kind: "enum", options: TASK_STATUS_VALUES, labels: STATUS_LABELS };
    case "readiness":
      return { kind: "enum", options: TASK_READINESS_VALUES, labels: READINESS_LABELS };
    case "priority":
      return { kind: "enum", options: TASK_PRIORITY_VALUES };
    case "risk":
      return { kind: "enum", options: TASK_RISK_VALUES };
    default:
      return isDateKey(key) ? { kind: "date" } : { kind: "text" };
  }
}

export function frontmatterControlValue(key: string, value: string): string {
  if (frontmatterFieldDefinition(key).kind !== "date") return value;
  const match = value.trim().match(/^\d{4}-\d{2}-\d{2}/);
  return match?.[0] ?? "";
}

export function validateFrontmatterValue(
  key: string,
  rawValue: string,
): { ok: true; value: string } | { ok: false; reason: string } {
  const def = frontmatterFieldDefinition(key);
  const value = def.kind === "text" ? rawValue.trim() : rawValue.trim();

  if (normalizedKey(key) === "title" && value === "") {
    return { ok: false, reason: "Title cannot be empty." };
  }
  if (def.kind === "enum" && def.options && !def.options.includes(value)) {
    return { ok: false, reason: `${key} must be one of: ${def.options.join(", ")}.` };
  }
  if (def.kind === "date" && value !== "" && !/^\d{4}-\d{2}-\d{2}$/.test(value)) {
    return { ok: false, reason: `${key} must be a YYYY-MM-DD date.` };
  }
  return { ok: true, value };
}

export function lockedFrontmatterReason(field: FrontmatterField): string {
  return (
    field.lockReason ??
    (normalizedKey(field.key) === "state_rev"
      ? "state_rev is the CAS token and is updated only by successful structured writes."
      : `${field.key} is managed by Tusker and is not editable from this chip.`)
  );
}

export function patchFrontmatterList(
  frontmatter: DocMeta["frontmatter"],
  key: string,
  value: string,
): DocMeta["frontmatter"] {
  return frontmatter.map((field) =>
    field.key === key ? { ...field, value } : field,
  );
}

export function displayStatusFromFrontmatter(value: string, fallback: string): string {
  switch (normalizedKey(value)) {
    case "done":
      return "done";
    case "review":
      return "review";
    case "ready":
    case "rework":
      return "ready";
    case "idea":
    case "backlog":
    case "cancelled":
    case "superseded":
      return "backlog";
    default:
      return fallback;
  }
}

export function displayReadinessFromFrontmatter(value: string, fallback: string): string {
  const raw = normalizedKey(value);
  if (raw.includes("dep")) return "blocked_dependency";
  if (raw.includes("gate") || raw.includes("human") || raw.includes("review")) return "blocked_gate";
  if (raw.includes("draft") || raw === "held") return "draft";
  if (raw === "cancelled" || raw === "superseded") return "draft";
  return raw === "ready" || raw === "done" ? "ready" : fallback;
}

export function patchTaskFrontmatter<T extends TaskCapsule>(
  task: T,
  key: string,
  value: string,
): T {
  const next = { ...task } as T & { rawStatus?: string; rawReadiness?: string };

  switch (normalizedKey(key)) {
    case "title":
      next.title = value;
      break;
    case "status":
      next.rawStatus = value;
      next.status = displayStatusFromFrontmatter(value, next.status) as T["status"];
      break;
    case "readiness":
      next.rawReadiness = value;
      next.readiness = displayReadinessFromFrontmatter(value, next.readiness) as T["readiness"];
      break;
    case "priority":
      next.priority = value as T["priority"];
      break;
    case "risk":
      next.risk = value as T["risk"];
      break;
    case "updated":
    case "updated_at":
      next.updatedAt = value;
      break;
  }

  return next;
}

export function patchDocFrontmatter<T extends DocContent>(
  doc: T,
  key: string,
  value: string,
): T {
  return {
    ...doc,
    title: normalizedKey(key) === "title" ? value : doc.title,
    frontmatter: patchFrontmatterList(doc.frontmatter, key, value),
  };
}
