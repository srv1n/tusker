import { afterEach, describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { api, resetServeCapabilityCache } from "@/lib/api";
import { isTaskConflict, taskDraft, taskEditPatch } from "@/features/product/taskAuthoring";
import type { TaskDetail } from "@/types/domain";

const originalFetch = globalThis.fetch;
afterEach(() => {
  globalThis.fetch = originalFetch;
  resetServeCapabilityCache();
});

const json = (status: number, body: unknown) => new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });

const loaded = {
  id: "APP-T-0001", title: "Old title", body: "## Intent\n\nOld.\n", stateRevision: "sha256:loaded",
  authoredWorkLevel: "standard", authoredExecuteProfile: "codex-sol", authoredReviewProfile: undefined,
} as unknown as TaskDetail;

describe("task contract edit save", () => {
  test("sends only changed fields with the revision loaded when editing began", () => {
    expect(taskEditPatch(loaded, taskDraft(loaded))).toBeNull();
    const draft = { ...taskDraft(loaded), title: " New title ", workLevel: "demanding", executeProfile: "" };
    expect(taskEditPatch(loaded, draft)).toEqual({ revision: "sha256:loaded", title: "New title", workLevel: "demanding", executeProfile: null });
  });

  test.serial("posts to the edit route and recognises a revision conflict", async () => {
    const calls: Array<{ url: string; body: unknown }> = [];
    globalThis.fetch = (async (input, init) => {
      if (String(input) === "/api/capability") return json(200, { capability: "token", operatorActor: "human:owner" });
      calls.push({ url: String(input), body: JSON.parse(String(init?.body)) });
      return json(409, { ok: false, refused: true, reason: "V7 object changed since it was loaded", issue: { code: "CAS_CONFLICT" } });
    }) as typeof fetch;
    const error = await api.editTask("APP-T-0001", { revision: "sha256:loaded", title: "New" }, "app").catch((err: unknown) => err);
    expect(calls).toEqual([{ url: "/api/tasks/APP-T-0001/edit?project=app", body: { revision: "sha256:loaded", title: "New" } }]);
    expect(isTaskConflict(error)).toBe(true);
  });

  test.serial("an ordinary refusal is not reported as a conflict", async () => {
    globalThis.fetch = (async (input) => String(input) === "/api/capability"
      ? json(200, { capability: "token", operatorActor: "human:owner" })
      : json(200, { ok: false, refused: true, reason: "--work-level must be light, standard, or demanding", issue: { code: "INVALID_ARG" } })) as typeof fetch;
    const error = await api.editTask("APP-T-0001", { revision: "r", workLevel: "x" }).catch((err: unknown) => err);
    expect(isTaskConflict(error)).toBe(false);
  });

  test("the editor blocks re-saving a stale draft and offers Reload", () => {
    const source = readFileSync(new URL("../src/features/product/TaskScreens.tsx", import.meta.url), "utf8");
    expect(source).toContain("This task changed since you started editing.");
    expect(source).toContain("edit.isPending || conflict}");
    expect(source).toContain("onClick={begin}>Reload</ProductButton>");
    expect(source).toContain("taskEditPatch(base, draft)");
  });
});
