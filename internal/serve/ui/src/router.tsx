import {
  createRootRoute,
  createRoute,
  createRouter,
  lazyRouteComponent,
  redirect,
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
    '/p/$projectId/needs'        → redirects to overview (absorbed)
    '/p/$projectId/runs'         → redirects to overview (absorbed)
    '/p/$projectId/runs/$taskId' Run detail          (params: projectId, taskId)
    '/p/$projectId/work'         Project work
    '/p/$projectId/delivery'     Review and start a planned delivery
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
    () => import("@/features/v2/TodayScreens"),
    "GlobalTodayV2",
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
    () => import("@/features/v2/TodayScreens"),
    "ProjectTodayV2",
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
    () => import("@/features/v2/DeliveryScreens"),
    "EpicsV2",
  ),
});

const wavesRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "waves",
  component: lazyRouteComponent(
    () => import("@/features/v2/DeliveryScreens"),
    "WavesV2",
  ),
});

const waveDetailRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "waves/$waveId",
  component: lazyRouteComponent(
    () => import("@/features/v2/DeliveryScreens"),
    "WaveDetailV2",
  ),
});

const tasksRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "tasks",
  component: lazyRouteComponent(
    () => import("@/features/v2/TaskScreens"),
    "TasksV2",
  ),
});

const taskDetailRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "tasks/$taskId",
  component: lazyRouteComponent(
    () => import("@/features/v2/TaskScreens"),
    "TaskDetailV2",
  ),
});

const trainsRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "trains",
  component: lazyRouteComponent(
    () => import("@/features/v2/DeliveryScreens"),
    "TrainsV2",
  ),
});

const diagnosticsRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "diagnostics",
  component: lazyRouteComponent(
    () => import("@/features/v2/OperationsScreens"),
    "DiagnosticsV2",
  ),
});

const executionsRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "diagnostics/executions",
  component: lazyRouteComponent(() => import("@/features/executions/ExecutionOperations"), "ExecutionOperations"),
});

// Needs-me and Runs are absorbed into the Overview (SRV-T-0003). Their old
// URLs redirect to the Overview so bookmarks and in-app links never break; the
// run-detail route below is kept (it is just no longer a sidebar item).
const needsRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "needs",
  beforeLoad: ({ params }) => {
    throw redirect({ to: "/p/$projectId", params: { projectId: params.projectId } });
  },
});

const runsRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "runs",
  beforeLoad: ({ params }) => {
    throw redirect({ to: "/p/$projectId", params: { projectId: params.projectId } });
  },
});

const runDetailRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "runs/$taskId",
  component: lazyRouteComponent(
    () => import("@/features/runs/RunDetail"),
    "RunDetail",
  ),
});

const workRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "work",
  beforeLoad: ({ params }) => {
    throw redirect({ to: "/p/$projectId/tasks", params: { projectId: params.projectId } });
  },
});

const deliveryRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "delivery",
  beforeLoad: ({ params }) => {
    throw redirect({ to: "/p/$projectId/plan", params: { projectId: params.projectId } });
  },
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
    () => import("@/features/v2/OperationsScreens"),
    "SettingsV2",
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
    needsRoute,
    runsRoute,
    runDetailRoute,
    workRoute,
    deliveryRoute,
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
