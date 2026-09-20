import { LayoutGrid, List, RotateCcw, Tag } from "lucide-react";
import type { RunSummary, TaskCapsule } from "@/types/domain";
import { cn } from "@/lib/cn";
import { ProductButton, ProductLabel, ProductStatus, phaseTone } from "@/features/product/shared";
import { availableTags, boardGroupFor, boardGroups, filterBoardTasks, isLiveTask, statusLabel } from "./boardModel";

export interface TaskBoardProps {
  tasks: TaskCapsule[];
  runs: RunSummary[];
  mode: "board" | "list";
  onModeChange: (value: "board" | "list") => void;
  onSelectTask: (id: string) => void;
  taskIds?: string[];
  tagsByTaskId?: Record<string, string[]>;
  selectedTags: string[];
  onSelectedTagsChange: (tags: string[]) => void;
  tagsAvailable: boolean;
}

function TaskState({ task, runs }: { task: TaskCapsule; runs: RunSummary[] }) {
  const label = statusLabel(task, runs);
  return <ProductStatus tone={phaseTone(label)}>{label}</ProductStatus>;
}

function TaskTags({ task, tagsByTaskId, visible }: { task: TaskCapsule; tagsByTaskId?: Record<string, string[]>; visible: boolean }) {
  if (!visible) return null;
  return (
    <div className="mt-3 flex flex-wrap gap-1.5">
      {(tagsByTaskId?.[task.id] ?? []).map((tag) => (
        <span key={tag} className="rounded-full border border-line bg-panel px-2 py-0.5 text-[10px] text-muted">{tag}</span>
      ))}
    </div>
  );
}

function TaskCard({ task, runs, tagsByTaskId, tagsVisible, onSelectTask }: {
  task: TaskCapsule;
  runs: RunSummary[];
  tagsByTaskId?: Record<string, string[]>;
  tagsVisible: boolean;
  onSelectTask: (id: string) => void;
}) {
  return (
    <button
      type="button"
      aria-label={`Open task ${task.id}: ${task.title}`}
      onClick={() => onSelectTask(task.id)}
      className="group w-full rounded-xl border border-line bg-raised p-3.5 text-left shadow-2xs transition hover:border-accent/50 hover:shadow-xs focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/40"
    >
      <div className="flex items-start justify-between gap-3">
        <span className="font-mono text-[10px] text-faint">{task.id}</span>
        <TaskState task={task} runs={runs} />
      </div>
      <div className="mt-2 text-[13.5px] font-semibold leading-snug text-ink group-hover:text-accent">{task.title}</div>
      {isLiveTask(task, runs) && <div className="mt-2 font-mono text-[10px] uppercase tracking-[0.12em] text-info">Live execution</div>}
      <TaskTags task={task} tagsByTaskId={tagsByTaskId} visible={tagsVisible} />
    </button>
  );
}

export function TaskBoard({
  tasks,
  runs,
  mode,
  onModeChange,
  onSelectTask,
  taskIds,
  tagsByTaskId,
  selectedTags,
  onSelectedTagsChange,
  tagsAvailable,
}: TaskBoardProps) {
  const scopedTasks = taskIds ? tasks.filter((task) => taskIds.includes(task.id)) : tasks;
  const visibleTasks = filterBoardTasks({ tasks: scopedTasks, selectedTags, tagsByTaskId, tagsAvailable });
  const tags = availableTags(tagsByTaskId);
  const toggleTag = (tag: string) => {
    onSelectedTagsChange(selectedTags.includes(tag) ? selectedTags.filter((value) => value !== tag) : [...selectedTags, tag]);
  };

  return (
    <section aria-label="Task board" className="space-y-5" data-testid="task-board">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-line pb-4">
        <div>
          <ProductLabel>{taskIds ? "Unassigned tasks" : "Tasks"}</ProductLabel>
          <p className="mt-1 text-[12px] text-muted">{visibleTasks.length} of {scopedTasks.length} tasks</p>
        </div>
        <div className="flex border border-line bg-raised" aria-label="Task view">
          <ProductButton aria-pressed={mode === "board"} tone={mode === "board" ? "primary" : "text"} className="rounded-none border-0" onClick={() => onModeChange("board")}>
            <LayoutGrid size={13} /> Board
          </ProductButton>
          <ProductButton aria-pressed={mode === "list"} tone={mode === "list" ? "primary" : "text"} className="rounded-none border-0" onClick={() => onModeChange("list")}>
            <List size={13} /> List
          </ProductButton>
        </div>
      </div>

      <div className="rounded-xl border border-line bg-panel/45 px-4 py-3.5" aria-label="Task tag filters">
        <div className="flex flex-wrap items-center gap-2">
          <span className="inline-flex items-center gap-1.5 text-[12px] font-medium text-ink"><Tag size={14} /> Topics</span>
          {tagsAvailable ? (
            <>
              {tags.map((tag) => (
                <button
                  key={tag}
                  type="button"
                  aria-pressed={selectedTags.includes(tag)}
                  onClick={() => toggleTag(tag)}
                  className={cn(
                    "rounded-full border px-2.5 py-1 text-[11px] font-medium transition focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/40",
                    selectedTags.includes(tag) ? "border-ink bg-ink text-surface" : "border-line bg-raised text-muted hover:border-ink/50",
                  )}
                >
                  {tag}
                </button>
              ))}
              {selectedTags.length > 0 && (
                <button type="button" onClick={() => onSelectedTagsChange([])} className="ml-auto inline-flex items-center gap-1 text-[11px] font-medium text-muted underline underline-offset-4 hover:text-ink">
                  <RotateCcw size={12} /> Clear all
                </button>
              )}
              {tags.length === 0 && <span className="text-[11px] text-muted">No topics in this view.</span>}
            </>
          ) : (
            <span className="text-[11px] text-muted">Unavailable until the durable tag contract exists.</span>
          )}
        </div>
      </div>

      {visibleTasks.length === 0 ? (
        <div className="rounded-xl border border-dashed border-line px-6 py-12 text-center text-[13px] text-muted">No tasks match these filters.</div>
      ) : mode === "list" ? (
        <ul aria-label="Task list" className="divide-y divide-line-soft rounded-xl border border-line bg-raised">
          {visibleTasks.map((task) => (
            <li key={task.id}>
              <button
                type="button"
                onClick={() => onSelectTask(task.id)}
                className="grid w-full gap-2 px-4 py-3.5 text-left transition hover:bg-hover/60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-accent/40 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center"
              >
                <span className="min-w-0">
                  <span className="block font-mono text-[10px] text-faint">{task.id}</span>
                  <span className="mt-1 block truncate text-[13.5px] font-semibold text-ink">{task.title}</span>
                  <TaskTags task={task} tagsByTaskId={tagsByTaskId} visible={tagsAvailable} />
                </span>
                <TaskState task={task} runs={runs} />
              </button>
            </li>
          ))}
        </ul>
      ) : (
        <div className="grid items-start gap-4 sm:grid-cols-2 xl:grid-cols-3">
          {boardGroups.map((group) => {
            const groupTasks = visibleTasks.filter((task) => boardGroupFor(task, runs) === group.key);
            if (groupTasks.length === 0) return null;
            return (
              <section key={group.key} aria-label={`${group.label} tasks`} className="min-w-0 rounded-xl border border-line bg-panel/35 p-3">
                <div className="mb-3 flex items-center justify-between gap-2">
                  <h2 className="text-[13px] font-semibold text-ink">{group.label}</h2>
                  <span className="font-mono text-[10px] text-faint">{groupTasks.length}</span>
                </div>
                <div className="space-y-2.5">
                  {groupTasks.map((task) => <TaskCard key={task.id} task={task} runs={runs} tagsByTaskId={tagsByTaskId} tagsVisible={tagsAvailable} onSelectTask={onSelectTask} />)}
                </div>
              </section>
            );
          })}
        </div>
      )}
    </section>
  );
}
