import { getRouteApi } from "@tanstack/react-router";
import { LibraryList } from "./LibraryList";
import { TaskContract } from "./TaskContract";
import { DocReader } from "./DocReader";
import { DocSourceView } from "./DocSourceView";
import { isTaskId } from "./mock";

const route = getRouteApi("/p/$projectId/docs");

/**
 * Document / Library (packet §4.5 + §4.6). One route, three surfaces keyed by
 * the `path` search param:
 *   - absent            → the library listing, grouped by kind
 *   - a task id         → the rendered task contract + evidence + closeout
 *   - any vault path    → the document reader/editor, or raw source when asked
 */
export function DocumentView() {
  const { projectId } = route.useParams();
  const { path, view } = route.useSearch();

  if (!path) return <LibraryList projectId={projectId} />;
  if (isTaskId(path)) return <TaskContract projectId={projectId} taskId={path} />;
  if (view === "source") return <DocSourceView projectId={projectId} path={path} />;
  return <DocReader projectId={projectId} path={path} />;
}
