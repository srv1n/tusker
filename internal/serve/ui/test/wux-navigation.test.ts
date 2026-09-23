import { expect, test } from "bun:test";
import {
  emptyNavigationState,
  moveProject,
  orderProjects,
  projectWorkPath,
  readNavigationState,
  recordProjectVisit,
  reorderProject,
  resolveNavigationTarget,
  sanitizeNavigationState,
  setExpandedProjects,
  setProjectIcon,
  waveSectionKind,
  writeNavigationState,
  type StorageLike,
} from "../src/features/workbench/navigation/navigationState";

function memoryStorage(initial: Record<string, string> = {}): StorageLike {
  const store = new Map(Object.entries(initial));
  return {
    getItem: (key) => store.get(key) ?? null,
    setItem: (key, value) => {
      store.set(key, value);
    },
    removeItem: (key) => {
      store.delete(key);
    },
  };
}

const PROJECTS = ["p-alpha", "p-beta", "p-gamma"];

test("navigation order survives reload", () => {
  const storage = memoryStorage();
  // Drag p-gamma (index 2) to the top and expand two projects.
  let state = readNavigationState(storage, PROJECTS);
  const moved = reorderProject(state, PROJECTS, 2, 0);
  expect(moved.movedId).toBe("p-gamma");
  expect(moved.position).toBe(1);
  state = setExpandedProjects(moved.state, PROJECTS, ["p-alpha", "p-beta"]);
  state = recordProjectVisit(state, PROJECTS, "p-beta", "/p/p-beta/waves/W-1");
  expect(writeNavigationState(storage, state)).toBe(true);

  // Reload with the same projects: order, expansion and last screen restore.
  const reloaded = readNavigationState(storage, PROJECTS);
  expect(reloaded.orderedProjectIds).toEqual(["p-gamma", "p-alpha", "p-beta"]);
  expect(reloaded.expandedProjectIds).toEqual(["p-alpha", "p-beta"]);
  expect(reloaded.lastPathByProject["p-beta"]).toBe("/p/p-beta/waves/W-1");

  // Keyboard reorder shares the identical ordering path as drag.
  const viaKeyboard = moveProject(reloaded, PROJECTS, "p-beta", -1);
  const viaDrag = reorderProject(reloaded, PROJECTS, 2, 1);
  expect(viaKeyboard.state.orderedProjectIds).toEqual(viaDrag.state.orderedProjectIds);
  expect(viaKeyboard.position).toBe(2);

  // New projects append without reshuffling; removed ones disappear cleanly.
  const withNew = readNavigationState(storage, [...PROJECTS, "p-delta"]);
  expect(withNew.orderedProjectIds).toEqual(["p-gamma", "p-alpha", "p-beta", "p-delta"]);
  const trimmed = sanitizeNavigationState(withNew, ["p-alpha", "p-beta"]);
  expect(trimmed.orderedProjectIds).toEqual(["p-alpha", "p-beta"]);
  expect(trimmed.expandedProjectIds).toEqual(["p-alpha", "p-beta"]);
  expect(trimmed.activeProjectId === null || trimmed.activeProjectId === "p-alpha" || trimmed.activeProjectId === "p-beta").toBe(true);

  // Malformed or blocked storage never prevents navigation.
  const corrupt = memoryStorage({
    "tusker.wux.navigation.v1": "{not json",
  });
  expect(readNavigationState(corrupt, PROJECTS).orderedProjectIds).toEqual(PROJECTS);
  expect(readNavigationState(null, PROJECTS).orderedProjectIds).toEqual(PROJECTS);
  expect(writeNavigationState(null, emptyNavigationState())).toBe(false);
  const ordered = orderProjects(
    PROJECTS.map((id) => ({ id })),
    reloaded,
  ).map((project) => project.id);
  expect(ordered).toEqual(["p-gamma", "p-alpha", "p-beta"]);
});

test("navigation deep link wins", () => {
  const state = recordProjectVisit(
    setExpandedProjects(emptyNavigationState(), PROJECTS, ["p-alpha"]),
    PROJECTS,
    "p-alpha",
    "/p/p-alpha/waves/W-9",
  );

  // An explicit deep link beats the saved last screen and switches project.
  const deep = resolveNavigationTarget(state, PROJECTS, { deepLink: "/p/p-beta/waves/W-2" });
  expect(deep.projectId).toBe("p-beta");
  expect(deep.path).toBe("/p/p-beta/waves/W-2");
  expect(deep.notice).toBeUndefined();

  // Without a deep link the saved screen restores.
  const restored = resolveNavigationTarget(
    { ...state, activeProjectId: "p-alpha" },
    PROJECTS,
  );
  expect(restored).toEqual({ projectId: "p-alpha", path: "/p/p-alpha/waves/W-9" });

  // External URLs never restore — the active project's Work view wins instead.
  const external = resolveNavigationTarget(
    { ...state, activeProjectId: "p-alpha" },
    PROJECTS,
    { deepLink: "https://example.com/p/p-beta" },
  );
  expect(external.path).toBe("/p/p-alpha/waves/W-9");

  // A deep link into a removed project falls back once, without loops.
  const gone = resolveNavigationTarget(state, ["p-alpha"], { deepLink: "/p/p-beta/waves/W-2" });
  expect(gone.projectId).toBe("p-alpha");
  expect(gone.path).toBe(projectWorkPath("p-alpha"));
  expect(typeof gone.notice).toBe("string");
});

test("project icon choice persists locally and auto restores discovery", () => {
  const storage = memoryStorage();
  const state = setProjectIcon(emptyNavigationState(), PROJECTS, "p-alpha", "audio");
  expect(writeNavigationState(storage, state)).toBe(true);
  expect(readNavigationState(storage, PROJECTS).projectIconById).toEqual({ "p-alpha": "audio" });

  const automatic = setProjectIcon(state, PROJECTS, "p-alpha", "auto");
  expect(automatic.projectIconById).toEqual({});
});

test("navigation missing project fallback", () => {
  const state = {
    ...emptyNavigationState(),
    orderedProjectIds: [...PROJECTS],
    activeProjectId: "p-gone",
    lastPathByProject: { "p-gone": "/p/p-gone/waves/W-1" },
  };

  // Removed active project: first available project, with an explanation.
  const fallback = resolveNavigationTarget(state, ["p-alpha", "p-beta"]);
  expect(fallback.projectId).toBe("p-alpha");
  expect(fallback.path).toBe(projectWorkPath("p-alpha"));
  expect(typeof fallback.notice).toBe("string");

  // Saved route pointing at a removed project: that project's Work view.
  const staleRoute = resolveNavigationTarget(
    {
      ...emptyNavigationState(),
      activeProjectId: "p-alpha",
      lastPathByProject: { "p-alpha": "/p/p-gone/waves/W-1" },
    },
    ["p-alpha", "p-beta"],
  );
  expect(staleRoute.path).toBe(projectWorkPath("p-alpha"));

  // No projects at all: the Add-project empty state, never a guessed name.
  const empty = resolveNavigationTarget(state, []);
  expect(empty.projectId).toBeNull();
  expect(empty.path).toBe("/");

  // Unavailable storage still yields a usable first-project target.
  const noStorage = readNavigationState(null, ["p-alpha"]);
  const target = resolveNavigationTarget(noStorage, ["p-alpha"]);
  expect(target).toEqual({ projectId: "p-alpha", path: projectWorkPath("p-alpha") });
});

test("navigation wave loading differs from empty", () => {
  expect(waveSectionKind(undefined, 0)).toBe("loading");
  expect(waveSectionKind({ state: "loading" }, 0)).toBe("loading");
  // A missing link list is not proof of an empty project while loading.
  expect(waveSectionKind({ state: "loading" }, 3)).toBe("loading");
  expect(waveSectionKind({ state: "ready" }, 0)).toBe("empty");
  expect(waveSectionKind({ state: "ready" }, 2)).toBe("ready");
  expect(waveSectionKind({ state: "error", error: "boom" }, 0)).toBe("error");
  expect(waveSectionKind({ state: "error" }, 4)).toBe("error");
});
