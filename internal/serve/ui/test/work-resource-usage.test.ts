import { expect, test } from "bun:test";
import type { TaskDetail, WaveReview } from "../src/types/domain";

test("Tasks defers member requests and pan/zoom does not traverse or refetch the graph", async () => {
  const { JSDOM } = await import("jsdom");
  const dom = new JSDOM('<!doctype html><div id="root"></div>', { url: "http://localhost/", pretendToBeVisual: true });
  const win = dom.window as unknown as Record<string, any>;
  const globals = globalThis as Record<string, any>;
  const saved = new Map<string, unknown>();
  for (const key of ["window", "document", "navigator", "HTMLElement", "Node", "Element", "Event", "requestAnimationFrame", "cancelAnimationFrame", "localStorage"]) {
    saved.set(key, globals[key]);
    globals[key] = typeof win[key] === "function" && key.includes("AnimationFrame") ? win[key].bind(win) : win[key];
  }
  saved.set("IS_REACT_ACT_ENVIRONMENT", globals.IS_REACT_ACT_ENVIRONMENT);
  globals.IS_REACT_ACT_ENVIRONMENT = true;
  saved.set("scrollTo", globals.scrollTo);
  globals.scrollTo = () => {};
  const savedFetch = globalThis.fetch;
  let root: { unmount(): void } | undefined;
  let client: import("@tanstack/react-query").QueryClient | undefined;
  try {
    const React = await import("react");
    const { act, createElement } = React;
    const { createRoot } = await import("react-dom/client");
    const { QueryClient, QueryClientProvider } = await import("@tanstack/react-query");
    const { createMemoryHistory, createRootRoute, createRoute, createRouter, RouterProvider } = await import("@tanstack/react-router");
    const { ConfirmProvider } = await import("../src/components/ui/action-feedback");
    const { WorkWave } = await import("../src/features/workbench/integration/WorkExperience");
    const { qk } = await import("../src/lib/queries");
    const { makeWave, sampleState } = await import("../src/features/workbench/overview/previewFixtures");
    const { readyTask } = await import("../previews/wux/inspector/fixtures");
    const ids = ["T-1", "T-2", "T-3"];
    const tasks: TaskDetail[] = ids.map((id) => ({ ...readyTask, id, deps: [], state: sampleState("planned") }));
    const requests: string[] = [];
    globalThis.fetch = (async (input: RequestInfo | URL) => {
      const path = new URL(String(input), "http://localhost").pathname;
      requests.push(path);
      const task = tasks.find((item) => path === `/api/tasks/${item.id}`);
      return task ? new Response(JSON.stringify(task), { headers: { "content-type": "application/json" } }) : new Response(null, { status: 404 });
    }) as typeof fetch;
    client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity, structuralSharing: false } } });
    client.setQueryData(qk.wave("app", "W-1"), makeWave({
      id: "W-1", title: "Resource usage", memberIds: ids,
      members: ids.map((id) => ({ id, title: id, group: "", status: "ready", state: sampleState("planned"), proof: "" })),
    }));
    client.setQueryData(qk.waveReview("app", "W-1"), {
      schema: "tusker.wave-review/v1", waveId: "W-1", title: "Resource usage", state: "Planned", authorization: "inert",
      materialFingerprint: "test", outcome: "", members: ids.map((taskId) => ({ taskId, title: taskId, state: sampleState("planned") })),
      frontiers: [ids], blockers: [], controls: [],
    } satisfies WaveReview);
    client.setQueryData(qk.runs("app"), []);
    const route = createRootRoute();
    const waveRoute = createRoute({ getParentRoute: () => route, path: "/p/$projectId/waves/$waveId", component: WorkWave });
    const router = createRouter({ routeTree: route.addChildren([waveRoute]), history: createMemoryHistory({ initialEntries: ["/p/app/waves/W-1?view=work"] }) });
    await router.load();
    await act(async () => {
      root = createRoot(win.document.getElementById("root"));
      (root as ReturnType<typeof createRoot>).render(createElement(QueryClientProvider, { client: client! }, createElement(ConfirmProvider, null, createElement(RouterProvider, { router }))));
      await Bun.sleep(20);
    });
    expect(requests).toEqual([]);
    const tab = (text: string) => [...win.document.querySelectorAll('nav[aria-label="Wave views"] button')].find((button: HTMLButtonElement) => button.textContent === text)!;
    await act(async () => {
      tab("Graph").click();
      await Bun.sleep(30);
    });
    await act(async () => { await Bun.sleep(20); });
    expect(requests.sort()).toEqual(ids.map((id) => `/api/tasks/${id}`));
    expect(win.document.querySelector('[aria-label="Wave dependency graph"]')).not.toBeNull();
    let dependencyReads = 0;
    const cached = client.getQueryData<TaskDetail>(qk.task("T-1", "app"))!;
    const deps = cached.deps;
    Object.defineProperty(cached, "deps", { configurable: true, get: () => { dependencyReads++; return deps; } });
    const before = requests.length;
    await act(async () => {
      for (let index = 0; index < 10; index++) win.document.querySelector('button[aria-label="Zoom in"]').click();
    });
    expect(dependencyReads).toBe(0);
    expect(requests).toHaveLength(before);
    const canvas = () => win.document.querySelector('[aria-label="Wave dependency graph"] [style*="translate"]') as HTMLElement;
    const zoomed = canvas().style.transform;
    expect(zoomed).not.toContain("scale(1)");
    await act(async () => { tab("Tasks").click(); });
    await act(async () => { tab("Graph").click(); });
    expect(canvas().style.transform).toBe(zoomed);
    expect(requests).toHaveLength(before);
    await act(async () => root!.unmount());
    root = undefined;
  } finally {
    if (root) {
      const { act } = await import("react");
      await act(async () => root!.unmount());
    }
    client?.clear();
    globalThis.fetch = savedFetch;
    for (const [key, value] of saved) globals[key] = value;
    dom.window.close();
  }
}, 30_000);
