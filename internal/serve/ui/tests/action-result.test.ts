import { afterEach, describe, expect, test } from "bun:test";
import { ActionRefusalError, ApiError, api, requireAccepted, resetServeCapabilityCache } from "@/lib/api";

const originalFetch = globalThis.fetch;

afterEach(() => {
  globalThis.fetch = originalFetch;
  resetServeCapabilityCache();
});

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });
}

describe("mutation result contract", () => {
  test("accepts an explicit successful result", () => {
    const result = { ok: true, reason: "queued" };
    expect(requireAccepted(result)).toBe(result);
  });

  test("turns an in-band refusal into a typed failure", () => {
    expect(() => requireAccepted({ ok: false, refused: true, reason: "gate is open" })).toThrow(ActionRefusalError);
    try {
      requireAccepted({ ok: false, refused: true, reason: "gate is open" });
    } catch (error) {
      expect(error).toBeInstanceOf(ActionRefusalError);
      expect((error as ActionRefusalError<{ reason: string }>).result.reason).toBe("gate is open");
      expect((error as ActionRefusalError<{ reason: string }>).kind).toBe("refused");
    }
  });

  test("treats a false ok without refused as refusal too", () => {
    expect(() => requireAccepted({ ok: false, reason: "validation failed", issue: { code: "invalid_arg" } })).toThrow("validation failed");
    try {
      requireAccepted({ ok: false, reason: "validation failed", issue: { code: "invalid_arg" } });
    } catch (error) {
      expect((error as ActionRefusalError<{ issue: { code: string } }>).kind).toBe("validation");
    }
  });
});

describe("transport error normalization", () => {
  test.serial("turns a non-JSON delivery failure into ApiError", async () => {
    globalThis.fetch = (async () => new Response("upstream gateway error", { status: 502 })) as typeof fetch;
    try {
      await api.deliveryReview("plan.yaml");
      throw new Error("expected delivery failure");
    } catch (error) {
      expect(error).toBeInstanceOf(ApiError);
      expect((error as ApiError).status).toBe(502);
      expect((error as ApiError).message).toContain("non-JSON");
    }
  });
});

describe("capability rotation", () => {
  test.serial("threads the configured operator actor into human mutations", async () => {
    const bodies: unknown[] = [];
    globalThis.fetch = (async (input, init) => {
      if (String(input) === "/api/capability") return jsonResponse(200, { capability: "current-token", operatorActor: "human:operator" });
      if (init?.body) bodies.push(JSON.parse(String(init.body)));
      return jsonResponse(200, { ok: true, reason: "accepted" });
    }) as typeof fetch;
    await api.runTask("APP-T-0001", "app");
    await api.deliveryStart({ plan: "plan.yaml", confirm: "fp", planIdentity: "id" }, "app");
    expect(bodies).toEqual([
      { actor: "human:operator" },
      { plan: "plan.yaml", confirm: "fp", planIdentity: "id", actor: "human:operator" },
    ]);
    resetServeCapabilityCache();
  });

  test.serial("threads the configured actor through every durable Serve mutation", async () => {
    const bodies: unknown[] = [];
    globalThis.fetch = (async (input, init) => {
      if (String(input) === "/api/capability") return jsonResponse(200, { capability: "current-token", operatorActor: "human:operator" });
      if (init?.body) bodies.push(JSON.parse(String(init.body)));
      return jsonResponse(200, { ok: true, reason: "accepted" });
    }) as typeof fetch;

    await api.taskStatus("APP-T-1", { status: "rework" }, "app");
    await api.discardTask("APP-T-1", { reason: "obsolete" }, "app");
    await api.closeTask("APP-T-1", {}, "app");
    await api.landTask("APP-T-1", {}, "app");
    await api.landWave("W-1", "app");
    await api.gateAction("APP-G-1", "satisfy", { evidence: "checked" }, "app");
    await api.addEvidence({ taskId: "APP-T-1", kind: "automated_test", covers: "A1" }, "app");
    await api.redrive("APP-T-1", "app");
    await api.acknowledgeRun("APP-T-1", "app");

    expect(bodies).toEqual([
      { status: "rework", actor: "human:operator" },
      { reason: "obsolete", actor: "human:operator" },
      { actor: "human:operator" },
      { actor: "human:operator" },
      { actor: "human:operator" },
      { evidence: "checked", actor: "human:operator" },
      { taskId: "APP-T-1", kind: "automated_test", covers: "A1", actor: "human:operator" },
      { actor: "human:operator" },
      { actor: "human:operator" },
    ]);
  });

  test.serial("re-bootstraps once after a stale capability 403", async () => {
    const calls: Array<{ url: string; token: string | null }> = [];
    globalThis.fetch = (async (input, init) => {
      const url = String(input);
      const token = new Headers(init?.headers).get("X-Tusker-Capability");
      calls.push({ url, token });
      if (url === "/api/capability") {
        const bootstrapCount = calls.filter((call) => call.url === url).length;
        return jsonResponse(200, { capability: bootstrapCount === 1 ? "stale-token" : "fresh-token", operatorActor: "human:test" });
      }
      if (token === "stale-token") {
        return jsonResponse(403, { ok: false, refused: true, reason: "refused mutation without serve capability" });
      }
      return jsonResponse(200, { ok: true, reason: "started" });
    }) as typeof fetch;

    await expect(api.daemonAction("start")).resolves.toMatchObject({ ok: true, reason: "started" });
    expect(calls).toEqual([
      { url: "/api/capability", token: null },
      { url: "/api/daemon/start", token: "stale-token" },
      { url: "/api/capability", token: null },
      { url: "/api/daemon/start", token: "fresh-token" },
    ]);
  });

  test.serial("fails after the single retry when the refreshed capability is refused", async () => {
    let bootstraps = 0;
    let mutations = 0;
    globalThis.fetch = (async (input, init) => {
      if (String(input) === "/api/capability") {
        bootstraps += 1;
        return jsonResponse(200, { capability: `token-${bootstraps}`, operatorActor: "human:test" });
      }
      mutations += 1;
      expect(new Headers(init?.headers).get("X-Tusker-Capability")).toBe(`token-${mutations}`);
      return jsonResponse(403, { ok: false, refused: true, reason: "refused mutation without serve capability" });
    }) as typeof fetch;

    try {
      await api.daemonAction("start");
      throw new Error("expected capability rejection");
    } catch (error) {
      expect(error).toBeInstanceOf(ApiError);
      expect((error as ApiError).status).toBe(403);
    }
    expect(bootstraps).toBe(2);
    expect(mutations).toBe(2);
  });

  test.serial("does not refresh or retry an ordinary in-band action refusal", async () => {
    let bootstraps = 0;
    let mutations = 0;
    globalThis.fetch = (async (input) => {
      if (String(input) === "/api/capability") {
        bootstraps += 1;
        return jsonResponse(200, { capability: "current-token", operatorActor: "human:test" });
      }
      mutations += 1;
      return jsonResponse(200, { ok: false, refused: true, reason: "gate is still open" });
    }) as typeof fetch;

    await expect(api.daemonAction("start")).rejects.toBeInstanceOf(ActionRefusalError);
    expect(bootstraps).toBe(1);
    expect(mutations).toBe(1);
  });

  test.serial("preserves typed refusal for an ordinary non-2xx action response", async () => {
    let mutations = 0;
    globalThis.fetch = (async (input) => {
      if (String(input) === "/api/capability") return jsonResponse(200, { capability: "current-token", operatorActor: "human:test" });
      mutations += 1;
      return jsonResponse(409, { ok: false, refused: true, reason: "run is still active" });
    }) as typeof fetch;

    try {
      await api.acknowledgeRun("APP-T-1");
      throw new Error("expected action refusal");
    } catch (error) {
      expect(error).toBeInstanceOf(ActionRefusalError);
      expect((error as ActionRefusalError<{ reason: string }>).status).toBe(409);
    }
    expect(mutations).toBe(1);
  });

  test.serial("uses the same single refresh path for delivery POST and docgraph PUT", async () => {
    const exercise = async (run: () => Promise<unknown>, expectedMethod: "POST" | "PUT") => {
      let bootstraps = 0;
      let mutations = 0;
      globalThis.fetch = (async (input, init) => {
        if (String(input) === "/api/capability") {
          bootstraps += 1;
          return jsonResponse(200, { capability: bootstraps === 1 ? "stale" : "fresh", operatorActor: "human:test" });
        }
        mutations += 1;
        expect(init?.method).toBe(expectedMethod);
        if (mutations === 1) {
          return jsonResponse(403, { ok: false, refused: true, reason: "refused mutation without serve capability" });
        }
        return jsonResponse(200, expectedMethod === "POST"
          ? { schema: "tusker.delivery-start/v1", waveId: "W-1" }
          : { subject: "doc", body: "saved", rev: "2", warnings: [] });
      }) as typeof fetch;
      await expect(run()).resolves.toBeDefined();
      expect(bootstraps).toBe(2);
      expect(mutations).toBe(2);
      resetServeCapabilityCache();
    };

    await exercise(
      () => api.deliveryStart({ plan: "plan.yaml", confirm: "fp", planIdentity: "id" }),
      "POST",
    );
    await exercise(
      () => api.saveDocgraphDoc("project", "doc", { base_rev: "1", body: "saved", frontmatter: {} }),
      "PUT",
    );
  });
});
