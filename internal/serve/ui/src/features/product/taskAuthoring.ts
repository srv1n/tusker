import { ActionRefusalError } from "@/lib/api";
import type { TaskDetail } from "@/types/domain";

/** Body for POST /api/tasks/:id/edit (`tusker task update`). Omitted keys stay unchanged; a null pin clears it. */
export interface TaskEditBody {
  revision: string;
  title?: string;
  body?: string;
  workLevel?: string;
  executeProfile?: string | null;
  reviewProfile?: string | null;
}

/** Body for POST /api/tasks (`tusker new task`, then `wave add` when a wave is named). */
export interface TaskCreateBody {
  title: string;
  body: string;
  workLevel: string;
  /** Comma-separated folders or files the task may change. Required. */
  ownedPaths: string;
  wave?: string;
}

export interface TaskDraft {
  title: string;
  body: string;
  workLevel: string;
  executeProfile: string;
  reviewProfile: string;
}

export const NEW_TASK_BODY = "## Intent\n\n\n\n## Acceptance\n\n| ID | Outcome | Proof |\n|---|---|---|\n| A1 |  |  |\n";

export function taskDraft(detail: TaskDetail): TaskDraft {
  return {
    title: detail.title,
    body: detail.body ?? "",
    workLevel: detail.authoredWorkLevel ?? "",
    executeProfile: detail.authoredExecuteProfile ?? "",
    reviewProfile: detail.authoredReviewProfile ?? "",
  };
}

/** Only changed fields are sent: `task update` refuses a no-op, and unchanged fields must not race other writers. */
export function taskEditPatch(detail: TaskDetail, draft: TaskDraft): TaskEditBody | null {
  const base = taskDraft(detail);
  const patch: TaskEditBody = { revision: detail.stateRevision ?? "" };
  if (draft.title.trim() !== base.title) patch.title = draft.title.trim();
  if (draft.body !== base.body) patch.body = draft.body;
  if (draft.workLevel !== base.workLevel) patch.workLevel = draft.workLevel;
  if (draft.executeProfile !== base.executeProfile) patch.executeProfile = draft.executeProfile || null;
  if (draft.reviewProfile !== base.reviewProfile) patch.reviewProfile = draft.reviewProfile || null;
  return Object.keys(patch).length > 1 ? patch : null;
}

/** A compare-and-swap refusal: someone else changed the task since this screen loaded it. */
export function isTaskConflict(error: unknown): boolean {
  // Status is not enough: requireAccepted reports every in-band refusal as 409.
  return error instanceof ActionRefusalError && error.result.issue?.code === "CAS_CONFLICT";
}
