import {
  createRootRoute,
  createRoute,
  createRouter,
  lazyRouteComponent,
} from "@tanstack/react-router";
import { ProjectLayout, RootLayout } from "@/routes/__root";
import { RouteFallback } from "@/components/RouteFallback";

// Feature screens are code-split: each `component` is wired with
// `lazyRouteComponent(() => import(...), "<NamedExport>")` so its chunk — and
// the heavy libs it pulls (the TipTap editor and mermaid under the docs route) —
// loads on demand instead of in the initial bundle. The always-present shell
// (RootLayout / ProjectLayout) stays eager. `defaultPreload: "intent"` warms a
// screen's chunk on link hover; `defaultPendingComponent` covers the fetch gap.

/*
  Code-based route tree. Route ids screens use with getRouteApi(...):
    '/'                          Global Today
    '/settings'                  App settings
    '/p/$projectId/'             Project Today
    '/p/$projectId/plan'         Plan inbox / review
    '/p/$projectId/epics'        Epic portfolio
    '/p/$projectId/waves'        Delivery waves
    '/p/$projectId/waves/$waveId' Wave detail
    '/p/$projectId/tasks'        Task board/list
    '/p/$projectId/tasks/$taskId' Task product detail
    '/p/$projectId/trains'       Promotion trains
    '/p/$projectId/diagnostics'  Runtime diagnostics
    '/p/$projectId/diagnostics/executions' Execution graph beneath Operations
    '/p/$projectId/runs/$taskId' Run detail          (params: projectId, taskId)
    '/p/$projectId/ops'          Operator controls
    '/p/$projectId/docs'         Library / document  (search: { path?: string })
    '/p/$projectId/knowledge'            Docs corpus list
    '/p/$projectId/knowledge/graph'      Doc-graph view
    '/p/$projectId/knowledge/$subject'   Doc reader        (params: projectId, subject)
    '/p/$projectId/settings'     Project settings
*/

const rootRoute = createRootRoute({ component: RootLayout });

const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  component: lazyRouteComponent(
    () => import("@/features/product/TodayScreens"),
    "GlobalToday",
  ),
});

const appSettingsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "settings",
  component: lazyRouteComponent(
    () => import("@/features/settings/AppSettings"),
    "AppSettings",
  ),
});

const panelRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "panel",
  component: lazyRouteComponent(() => import("@/features/panel/Panel"), "Panel"),
});

const projectRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "p/$projectId",
  component: ProjectLayout,
});

const overviewRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "/",
  component: lazyRouteComponent(
    () => import("@/features/product/TodayScreens"),
    "ProjectToday",
  ),
});

const planRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "plan",
  component: lazyRouteComponent(
    () => import("@/features/delivery/DeliveryReview"),
    "DeliveryReviewPage",
  ),
});

const epicsRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "epics",
  component: lazyRouteComponent(
    () => import("@/features/product/DeliveryScreens"),
    "Epics",
  ),
});

const wavesRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "waves",
  component: lazyRouteComponent(
    () => import("@/features/product/DeliveryScreens"),
    "Waves",
  ),
});

const waveDetailRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "waves/$waveId",
  component: lazyRouteComponent(
    () => import("@/features/product/DeliveryScreens"),
    "WaveDetail",
  ),
});

const tasksRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "tasks",
  component: lazyRouteComponent(
    () => import("@/features/product/TaskScreens"),
    "Tasks",
  ),
});

const taskDetailRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "tasks/$taskId",
  component: lazyRouteComponent(
    () => import("@/features/product/TaskScreens"),
    "TaskDetail",
  ),
});

const trainsRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "trains",
  component: lazyRouteComponent(
    () => import("@/features/product/DeliveryScreens"),
    "Trains",
  ),
});

const diagnosticsRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "diagnostics",
  component: lazyRouteComponent(
    () => import("@/features/product/OperationsScreens"),
    "Diagnostics",
  ),
});

const executionsRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "diagnostics/executions",
  component: lazyRouteComponent(() => import("@/features/executions/ExecutionOperations"), "ExecutionOperations"),
});

const runDetailRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "runs/$taskId",
  component: lazyRouteComponent(
    () => import("@/features/runs/RunDetail"),
    "RunDetail",
  ),
});

const opsRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "ops",
  component: lazyRouteComponent(
    () => import("@/features/ops/ProjectOps"),
    "ProjectOps",
  ),
});

const docsRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "docs",
  validateSearch: (search: Record<string, unknown>): { path?: string; view?: "source"; gate?: string } => ({
    path: typeof search.path === "string" ? search.path : undefined,
    view: search.view === "source" ? "source" : undefined,
    gate: typeof search.gate === "string" ? search.gate : undefined,
  }),
  component: lazyRouteComponent(
    () => import("@/features/docs/DocumentView"),
    "DocumentView",
  ),
});

const knowledgeRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "knowledge",
  component: lazyRouteComponent(
    () => import("@/features/knowledge/KnowledgeList"),
    "KnowledgeList",
  ),
});

const knowledgeGraphRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "knowledge/graph",
  component: lazyRouteComponent(
    () => import("@/features/knowledge/KnowledgeGraph"),
    "KnowledgeGraph",
  ),
});

const knowledgeReaderRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "knowledge/$subject",
  component: lazyRouteComponent(
    () => import("@/features/knowledge/KnowledgeReader"),
    "KnowledgeReader",
  ),
});

const projectSettingsRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "settings",
  component: lazyRouteComponent(
    () => import("@/features/product/OperationsScreens"),
    "Settings",
  ),
});

const routeTree = rootRoute.addChildren([
  indexRoute,
  appSettingsRoute,
  panelRoute,
  projectRoute.addChildren([
    overviewRoute,
    planRoute,
    epicsRoute,
    wavesRoute,
    waveDetailRoute,
    tasksRoute,
    taskDetailRoute,
    trainsRoute,
  diagnosticsRoute,
  executionsRoute,
    runDetailRoute,
    opsRoute,
    docsRoute,
    knowledgeRoute,
    knowledgeGraphRoute,
    knowledgeReaderRoute,
    projectSettingsRoute,
  ]),
]);

export const router = createRouter({
  routeTree,
  defaultPreload: "intent",
  defaultPendingComponent: RouteFallback,
  scrollRestoration: true,
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
