import { expect, test } from "bun:test";
import {
  emptyNavigationState,
  getWorkspaceViewState,
  readNavigationState,
  recordWorkspaceDocumentVisit,
  recordWorkspaceWaveVisit,
  updateWorkspaceViewState,
  type StorageLike,
} from "../src/features/workbench/navigation/navigationState";

const PROJECTS = ["alpha", "beta"];
const DOC_PATH = "/p/alpha/knowledge/overview";

function storage(values: Record<string, string>): StorageLike {
  return {
    getItem: (key) => values[key] ?? null,
    setItem: (key, value) => {
      values[key] = value;
    },
  };
}

test("workspace state keeps legacy records and survives malformed or blocked storage", () => {
  const old = {
    version: 1,
    orderedProjectIds: ["alpha", "alpha", "missing", "beta"],
    pinnedIds: ["alpha"],
    activeProjectId: "alpha",
    lastPathByProject: { alpha: "/p/alpha/waves", beta: "https://example.invalid" },
    viewStateByProject: {
      alpha: { legacy: { filter: "needs-you" }, workspace: { docs: { contextOpen: false } } },
      "checkout-alpha": { workspace: { board: { mode: "list" } } },
    },
  };
  const state = readNavigationState(storage({ "tusker.wux.navigation.v1": JSON.stringify(old) }), PROJECTS);
  expect(state.orderedProjectIds).toEqual(PROJECTS);
  expect(state.pinnedProjectIds).toEqual(["alpha"]);
  expect(state.viewStateByProject.alpha).toEqual(old.viewStateByProject.alpha);
  expect(getWorkspaceViewState(state, "alpha").docs?.contextOpen).toBe(false);
  expect(getWorkspaceViewState(state, "checkout-alpha").board?.mode).toBe("list");
  expect(getWorkspaceViewState(state, "beta").docs).toBeUndefined();

  const corrupt = readNavigationState(storage({ "tusker.wux.navigation.v1": "{" }), PROJECTS);
  expect(corrupt.orderedProjectIds).toEqual(PROJECTS);
  expect(readNavigationState({ getItem: () => { throw new Error("blocked"); }, setItem: () => {} }, PROJECTS).orderedProjectIds).toEqual(PROJECTS);
  expect(readNavigationState(null, PROJECTS).orderedProjectIds).toEqual(PROJECTS);
});

test("workspace updates merge by checkout and reject invalid values without losing opaque state", () => {
  const initial = readNavigationState(
    storage({
      "tusker.wux.navigation.v1": JSON.stringify({
        viewStateByProject: {
          alpha: {
            legacy: { graph: { x: 12 } },
            workspace: { docs: { contextOpen: true }, waves: { byId: { "wave-1": { view: "flow", scrollTop: 12 } } } },
          },
          beta: { legacy: "untouched" },
        },
      }),
    }),
    PROJECTS,
  );
  const next = updateWorkspaceViewState(initial, "alpha", {
    lastDocsPath: DOC_PATH,
    docs: { contextOpen: false, contextWidth: 240 },
    waves: { overviewQuery: "alpha", overviewFilter: "running" },
    board: { mode: "list", selectedTaskId: "stale-task", selectedTags: ["urgent", "urgent", ""] },
  });
  expect(getWorkspaceViewState(next, "alpha")).toMatchObject({
    lastDocsPath: DOC_PATH,
    docs: { contextOpen: false, contextWidth: 240 },
    waves: { overviewQuery: "alpha", overviewFilter: "running" },
    board: { mode: "list", selectedTaskId: "stale-task", selectedTags: ["urgent"] },
  });
  expect(next.viewStateByProject.alpha).toMatchObject({ legacy: { graph: { x: 12 } } });
  expect(getWorkspaceViewState(next, "beta")).toEqual({});

  const invalid = updateWorkspaceViewState(next, "alpha", {
    lastDocsPath: "/p/beta/knowledge/wrong-project",
    docs: {
      contextWidth: 199,
      overviewScrollTop: Number.NaN,
      scrollBySubject: {
        bad: { top: Number.POSITIVE_INFINITY, left: -1 },
      },
    } as never,
    waves: { byId: { "wave-1": { view: "invalid" as never, scrollTop: -1, selectedTaskId: "" } } },
    board: { mode: "invalid" as never, scrollTop: -1, scrollLeft: Number.POSITIVE_INFINITY },
  });
  expect(getWorkspaceViewState(invalid, "alpha")).toMatchObject({
    lastDocsPath: DOC_PATH,
    docs: { contextOpen: false, contextWidth: 240 },
    board: { mode: "list", selectedTaskId: "stale-task" },
  });
  expect(getWorkspaceViewState(invalid, "alpha").docs?.scrollBySubject).toBeUndefined();
  expect(getWorkspaceViewState(invalid, "alpha").waves?.byId?.["wave-1"]).toEqual({ view: "flow", scrollTop: 12 });
});

test("document and wave visit maps keep the last 20 entries in visit order", () => {
  let state = emptyNavigationState();
  for (let index = 0; index < 25; index += 1) {
    state = recordWorkspaceDocumentVisit(state, "alpha", `doc-${index}`, { top: index, left: 0 });
    state = recordWorkspaceWaveVisit(state, "alpha", `wave-${index}`, { view: index % 2 ? "flow" : "results" });
  }

  let workspace = getWorkspaceViewState(state, "alpha");
  expect(Object.keys(workspace.docs?.scrollBySubject ?? {})).toEqual(
    Array.from({ length: 20 }, (_, index) => `doc-${index + 5}`),
  );
  expect(Object.keys(workspace.waves?.byId ?? {})).toEqual(
    Array.from({ length: 20 }, (_, index) => `wave-${index + 5}`),
  );

  state = recordWorkspaceDocumentVisit(state, "alpha", "doc-5");
  state = recordWorkspaceWaveVisit(state, "alpha", "wave-5", { selectedTaskId: "stale-task" });
  workspace = getWorkspaceViewState(state, "alpha");
  expect(Object.keys(workspace.docs?.scrollBySubject ?? {}).slice(-1)).toEqual(["doc-5"]);
  expect(Object.keys(workspace.waves?.byId ?? {}).slice(-1)).toEqual(["wave-5"]);
  expect(workspace.waves?.byId?.["wave-5"]?.selectedTaskId).toBe("stale-task");
});
