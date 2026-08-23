import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import {
  frontmatterFieldDefinition,
  patchDocFrontmatter,
  patchTaskFrontmatter,
  validateFrontmatterValue,
} from "../src/lib/frontmatter";
import type { DocContent, TaskDetail } from "../src/types/domain";

const task = {
  id: "SRV-T-0009",
  title: "Frontmatter controls",
  epicId: "SRV",
  epicTitle: "Serve",
  status: "ready",
  rawStatus: "ready",
  readiness: "ready",
  rawReadiness: "ready",
  priority: "p1",
  risk: "low",
  hasGate: false,
  updatedAt: "2026-07-09T12:00:00Z",
  intent: "Inline controls only.",
  acceptance: [],
  nonGoals: [],
  verification: [],
  evidence: [],
  deps: [],
  gates: [],
  runHistory: [],
} satisfies TaskDetail;

const doc = {
  path: "docs/system/serve-ui.md",
  title: "Serve spec",
  kind: "spec",
  updatedAt: "2026-07-09T12:00:00Z",
  rev: "sha256:test",
  frontmatter: [
    { key: "id", value: "SRV-SPEC-10", locked: true },
    { key: "owner", value: "human:sarav", locked: false },
    { key: "accepted_at", value: "2026-07-09", locked: false },
  ],
  markdown: "# Serve spec",
  outline: [],
} satisfies DocContent;

describe("frontmatter field schema", () => {
  test("uses typed controls by field kind", () => {
    expect(frontmatterFieldDefinition("status").kind).toBe("enum");
    expect(frontmatterFieldDefinition("priority").options).toEqual(["p0", "p1", "p2", "p3"]);
    expect(frontmatterFieldDefinition("accepted_at").kind).toBe("date");
    expect(frontmatterFieldDefinition("title").kind).toBe("text");
  });

  test("enum fields reject values outside the V7 schema", () => {
    expect(frontmatterFieldDefinition("status").options).not.toContain("active");
    expect(validateFrontmatterValue("status", "rework")).toEqual({ ok: true, value: "rework" });
    expect(validateFrontmatterValue("status", "active").ok).toBe(false);
    expect(validateFrontmatterValue("risk", "urgent").ok).toBe(false);
  });

  test("date and text fields validate without exposing raw YAML", () => {
    expect(validateFrontmatterValue("accepted_at", "2026-07-09")).toEqual({
      ok: true,
      value: "2026-07-09",
    });
    expect(validateFrontmatterValue("accepted_at", "July 9").ok).toBe(false);
    expect(validateFrontmatterValue("title", "").ok).toBe(false);
  });
});

describe("frontmatter cache patches", () => {
  test("task patches update the structured task source used by panel and rail", () => {
    const patched = patchTaskFrontmatter(task, "priority", "p0");

    expect(patched.priority).toBe("p0");
    expect(task.priority).toBe("p1");
  });

  test("canonical status edits keep raw status distinct from display projection", () => {
    const patched = patchTaskFrontmatter(task, "status", "rework");

    expect(patched.rawStatus).toBe("rework");
    expect(patched.status).toBe("ready");
  });

  test("doc patches update frontmatter without touching markdown source", () => {
    const patched = patchDocFrontmatter(doc, "owner", "human:ops");

    expect(patched.frontmatter.find((field) => field.key === "owner")?.value).toBe("human:ops");
    expect(patched.markdown).toBe(doc.markdown);
  });
});

describe("frontmatter edit surface", () => {
  const propertyPanel = readFileSync("src/features/docs/PropertyPanel.tsx", "utf8");
  const taskContract = readFileSync("src/features/docs/TaskContract.tsx", "utf8");
  const queries = readFileSync("src/lib/queries.ts", "utf8");
  const api = readFileSync("src/lib/api.ts", "utf8");

  test("PropertyPanel renders select/date/text controls and no textarea", () => {
    expect(propertyPanel).toContain("<Select");
    expect(propertyPanel).toContain('type={def.kind === "date" ? "date" : "text"}');
    expect(propertyPanel).toContain("validateFrontmatterValue");
    expect(propertyPanel).not.toContain("<textarea");
  });

  test("locked fields explain their lock reason on click", () => {
    expect(propertyPanel).toContain("lockedFrontmatterReason");
    expect(propertyPanel).toContain("onLockedClick");
    expect(propertyPanel).toContain('role="status"');
  });

  test("TaskContract uses one frontmatter source for panel and right-rail chips", () => {
    expect(taskContract).toContain("frontmatterByKey.status");
    expect(taskContract).toContain("<EditableFact");
    expect(taskContract).toContain("<PropertyPanel");
    expect(taskContract).toContain("readOnly");
    expect(taskContract).not.toContain("useFrontmatterUpdate");
  });

  test("the UI does not expose a fake frontmatter mutation", () => {
    expect(api).not.toContain("updateFrontmatter");
    expect(queries).not.toContain("useFrontmatterUpdate");
  });
});
