import { expect, test } from "bun:test";
import {
  emptyNavigationState,
  movePinnedProject,
  orderProjects,
  readNavigationState,
  recordProjectVisit,
  resolveNavigationTarget,
  setProjectPinned,
  toggleProjectPinned,
  writeNavigationState,
  type StorageLike,
} from "../src/features/workbench/navigation/navigationState";

function storage(initial: Record<string, string> = {}): StorageLike {
  const values = new Map(Object.entries(initial));
  return {
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => values.set(key, value),
  };
}

const projects = ["alpha", "beta", "gamma", "delta"];

test("project strip state migrates old records without losing routes or opaque view state", () => {
  const old = {
    version: 1,
    orderedProjectIds: ["gamma", "alpha", "gamma", "unknown"],
    expandedProjectIds: ["alpha", "removed"],
    activeProjectId: "alpha",
    lastPathByProject: { alpha: "/p/alpha/waves/W-1", removed: "/p/removed" },
    viewStateByProject: { alpha: { filter: "needs-you", graph: { x: 12 } } },
  };
  const state = readNavigationState(storage({ "tusker.wux.navigation.v1": JSON.stringify(old) }), projects);

  expect(state.orderedProjectIds).toEqual(["gamma", "alpha", "beta", "delta"]);
  expect(state.pinnedProjectIds).toEqual([]);
  expect(state.recentProjectIds).toEqual([]);
  expect(state.lastPathByProject).toEqual({ alpha: "/p/alpha/waves/W-1" });
  expect(state.viewStateByProject).toEqual(old.viewStateByProject);
  expect(state.expandedProjectIds).toEqual(["alpha"]);
});

test("pins order the rail and visits never reorder it", () => {
  let state = emptyNavigationState();
  state = recordProjectVisit(state, projects, "beta", "/p/beta/waves");
  state = recordProjectVisit(state, projects, "gamma", "/p/gamma/knowledge");
  const mounted = orderProjects(projects.map((id) => ({ id })), state).map((project) => project.id);
  expect(mounted).toEqual(projects);

  const sameProjectRoute = recordProjectVisit(state, projects, "gamma", "/p/gamma/tasks");
  expect(sameProjectRoute.recentProjectIds).toEqual(["gamma", "beta"]);
  expect(sameProjectRoute.lastPathByProject.gamma).toBe("/p/gamma/tasks");

  state = setProjectPinned(sameProjectRoute, projects, "alpha", true);
  state = setProjectPinned(state, projects, "gamma", true);
  expect(state.pinnedProjectIds).toEqual(["alpha", "gamma"]);
  expect(orderProjects(projects.map((id) => ({ id })), state).map((project) => project.id)).toEqual(["alpha", "gamma", "beta", "delta"]);

  const moved = movePinnedProject(state, projects, "gamma", -1);
  expect(moved.state.pinnedProjectIds).toEqual(["gamma", "alpha"]);
  const unpinned = toggleProjectPinned(moved.state, projects, "gamma");
  expect(unpinned.pinnedProjectIds).toEqual(["alpha"]);
});

test("malformed storage and unsafe or cross-project routes fall back safely", () => {
  expect(readNavigationState(storage({ "tusker.wux.navigation.v1": "{" }), projects).orderedProjectIds).toEqual(projects);
  expect(writeNavigationState(null, emptyNavigationState())).toBe(false);

  const state = recordProjectVisit(emptyNavigationState(), projects, "alpha", "/p/alpha/waves/W-1");
  expect(resolveNavigationTarget({ ...state, lastPathByProject: { alpha: "/p/beta/tasks" } }, projects).path).toBe("/p/alpha/waves");
  expect(resolveNavigationTarget({ ...state, lastPathByProject: { alpha: "https://example.com/p/alpha" } }, projects).path).toBe("/p/alpha/waves");
  expect(resolveNavigationTarget(state, projects, { deepLink: "/p/checkout-alpha/tasks", routeOwners: { "checkout-alpha": "alpha" } })).toEqual({ projectId: "alpha", path: "/p/checkout-alpha/tasks" });
  expect(resolveNavigationTarget(state, projects, { deepLink: "/p/checkout-beta/tasks", routeOwners: { "checkout-beta": "beta" } }).projectId).toBe("beta");
  expect(resolveNavigationTarget(state, ["alpha"], { deepLink: "/p/removed/tasks" }).path).toBe("/p/alpha/waves");
  expect(resolveNavigationTarget({ ...state, activeProjectId: "removed" }, ["beta"]).path).toBe("/p/beta/waves");
});
