import { useState } from "react";
import { RotateCcw } from "lucide-react";
import type { TaskCapsule } from "@/types/domain";
import { cn } from "@/lib/cn";
import { TaskStateBadge } from "@/components/ui/chips";
import { availableTags, boardGroupFor, boardGroups, filterBoardTasks } from "./boardModel";

export interface TaskBoardProps {
  tasks: TaskCapsule[];
  mode: "board" | "list";
  onModeChange: (value: "board" | "list") => void;
  onSelectTask: (id: string) => void;
  taskIds?: string[];
  tagsByTaskId?: Record<string, string[]>;
  selectedTags: string[];
  onSelectedTagsChange: (tags: string[]) => void;
  tagsAvailable: boolean;
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

function TaskCard({ task, tagsByTaskId, tagsVisible, onSelectTask }: {
  task: TaskCapsule;
  tagsByTaskId?: Record<string, string[]>;
  tagsVisible: boolean;
  onSelectTask: (id: string) => void;
}) {
  return (
    <button
      type="button"
      aria-label={`Open task ${task.id}: ${task.title}`}
      onClick={() => onSelectTask(task.id)}
      className="group w-full rounded-lg border border-line bg-raised px-3 py-2.5 text-left shadow-2xs transition hover:border-accent/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/40"
    >
      <div className="flex items-center justify-between gap-2">
        <span className="font-mono text-[10.5px] text-faint">{task.id}</span>
      </div>
      <div className="mt-1 text-[13px] font-medium leading-snug text-ink group-hover:text-accent">{task.title}</div>
      {task.state.reason ? <div className="mt-1 line-clamp-2 text-[11.5px] leading-4 text-muted">{task.state.reason}</div> : null}
      <TaskTags task={task} tagsByTaskId={tagsByTaskId} visible={tagsVisible} />
    </button>
  );
}

export function TaskBoard({
  tasks,
  mode,
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

  const [expanded, setExpanded] = useState<Record<string, boolean>>({});

  return (
    <section aria-label="Task board" className="flex min-h-0 flex-1 flex-col" data-testid="task-board">
      {taskIds || (tagsAvailable && tags.length > 0) ? (
        <div className="flex flex-none flex-wrap items-center gap-2 border-b border-line px-4 py-2 text-[12px]" aria-label="Task tag filters">
          {taskIds ? <span className="font-medium text-ink">Unassigned tasks</span> : null}
          {tagsAvailable ? tags.map((tag) => (
            <button
              key={tag}
              type="button"
              aria-pressed={selectedTags.includes(tag)}
              onClick={() => toggleTag(tag)}
              className={cn(
                "rounded-full border px-2.5 py-0.5 text-[11px] font-medium focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/40",
                selectedTags.includes(tag) ? "border-ink bg-ink text-surface" : "border-line bg-raised text-muted hover:border-ink/50",
              )}
            >
              {tag}
            </button>
          )) : null}
          {selectedTags.length > 0 && (
            <button type="button" onClick={() => onSelectedTagsChange([])} className="ml-auto inline-flex items-center gap-1 text-[11px] text-muted underline underline-offset-4 hover:text-ink">
              <RotateCcw size={12} /> Clear
            </button>
          )}
        </div>
      ) : null}

      {visibleTasks.length === 0 ? (
        <p className="p-5 text-[13px] text-muted">{scopedTasks.length ? "No tasks match these filters." : "No tasks yet."}</p>
      ) : mode === "list" ? (
        <div className="min-h-0 flex-1 overflow-y-auto p-4">
          <ul aria-label="Task list" className="mx-auto max-w-[1180px] divide-y divide-line overflow-hidden rounded-lg border border-line bg-raised">
            {visibleTasks.map((task) => (
              <li key={task.id}>
                <button
                  type="button"
                  onClick={() => onSelectTask(task.id)}
                  className="grid h-11 w-full grid-cols-[6.5rem_minmax(0,1fr)_auto] items-center gap-3 px-3 text-left hover:bg-hover focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-accent/40"
                >
                  <span className="truncate font-mono text-[11px] text-faint">{task.id}</span>
                  <span className="truncate text-[13px] text-ink">{task.title}</span>
                  <TaskStateBadge state={task.state} />
                </button>
              </li>
            ))}
          </ul>
        </div>
      ) : (
        <div className="flex min-h-0 flex-1 items-stretch gap-3 overflow-x-auto p-4">
          {boardGroups.map((group) => {
            const groupTasks = visibleTasks.filter((task) => boardGroupFor(task) === group.key);
            if (groupTasks.length === 0) return null;
            const label = groupTasks[0].state.label;
            const open = expanded[group.key] ?? !group.collapsed;
            if (!open) return (
              <button key={group.key} type="button" aria-expanded={false} aria-label={`Show ${label} tasks (${groupTasks.length})`} onClick={() => setExpanded({ ...expanded, [group.key]: true })} className="flex w-10 flex-none flex-col items-center gap-2 rounded-lg border border-line bg-panel/40 py-3 text-[12px] text-muted hover:text-ink">
                <span className="font-mono text-[11px]">{groupTasks.length}</span>
                <span className="[writing-mode:vertical-rl]">{label}</span>
              </button>
            );
            return (
              <section key={group.key} aria-label={`${label} tasks`} className="flex min-h-0 w-72 flex-none flex-col rounded-lg bg-panel/40">
                <button type="button" aria-expanded onClick={() => setExpanded({ ...expanded, [group.key]: false })} className="flex flex-none items-center justify-between gap-2 px-3 py-2 text-left">
                  <h2 className="text-[12.5px] font-semibold text-ink">{label}</h2>
                  <span className="font-mono text-[11px] text-faint">{groupTasks.length}</span>
                </button>
                <div className="min-h-0 flex-1 space-y-2 overflow-y-auto px-2 pb-2">
                  {groupTasks.map((task) => <TaskCard key={task.id} task={task} tagsByTaskId={tagsByTaskId} tagsVisible={tagsAvailable} onSelectTask={onSelectTask} />)}
                </div>
              </section>
            );
          })}
        </div>
      )}
    </section>
  );
}
