import { getRouteApi } from "@tanstack/react-router";
import { LibraryList } from "./LibraryList";
import { TaskContract } from "./TaskContract";
import { DocReader } from "./DocReader";
import { isTaskId } from "./mock";

const route = getRouteApi("/p/$projectId/docs");

/**
 * Document / Library (packet §4.5 + §4.6). One route, three surfaces keyed by
 * the `path` search param:
 *   - absent            → the library listing, grouped by kind
 *   - a task id         → the rendered task contract + evidence + closeout
 *   - any vault path    → the markdown reader/editor with guard-railed saves
 */
export function DocumentView() {
  const { projectId } = route.useParams();
  const { path } = route.useSearch();

  if (!path) return <LibraryList projectId={projectId} />;
  if (isTaskId(path)) return <TaskContract projectId={projectId} taskId={path} />;
  return <DocReader projectId={projectId} path={path} />;
}
