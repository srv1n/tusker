import type { ReactNode } from "react";
import { getRouteApi } from "@tanstack/react-router";
import { Dot } from "@/components/ui/primitives";
import { SectionLabel } from "@/components/ui/page";
import { EmptyState, QueryBoundary } from "@/components/ui/states";
import { useProjects, useRuns } from "@/lib/queries";
import { partitionRuns } from "@/features/runs/board/helpers";
import {
  ActiveRunRow,
  RecentRunRow,
  RunsBoardSkeleton,
  RunsTableHeader,
} from "@/features/runs/board/rows";

const route = getRouteApi("/p/$projectId/runs");

/** Legend tying the liveness hues to the words the board reads by (§4.2). */
function LivenessLegend() {
  return (
    <div className="flex items-center gap-3.5 font-mono text-[10.5px] text-muted">
      <span className="flex items-center gap-1.5">
        <Dot tone="pass" size={7} />
        live
      </span>
      <span className="flex items-center gap-1.5">
        <Dot tone="warn" size={7} />
        slowing
      </span>
      <span className="flex items-center gap-1.5">
        <Dot tone="fail" size={7} />
        stale
      </span>
    </div>
  );
}

/** Bordered container with rounded corners, used by both run tables. */
function Board({ children }: { children: ReactNode }) {
  return (
    <div className="overflow-hidden rounded-[10px] border border-line">{children}</div>
  );
}

function EmptyBoard({ children }: { children: ReactNode }) {
  return (
    <div className="rounded-[10px] border border-line bg-panel px-4 py-8 text-center text-[13px] text-muted">
      {children}
    </div>
  );
}

/**
 * Project Runs board (packet §4.2). A live table of agent runs — active first,
 * sorted so a silently-dead process floats to the top — with finished runs
 * below. Polls via useRuns; the whole board reads as "what is the machine doing
 * right now, and is anything wrong?".
 */
export function ProjectRuns() {
  const { projectId } = route.useParams();
  const runsQ = useRuns(projectId);
  const projects = useProjects();
  const projectName = projects.data?.find((p) => p.id === projectId)?.name ?? projectId;

  return (
    <div className="tk-scroll h-full overflow-y-auto">
      <div className="mx-auto w-full max-w-[1120px] px-11 pb-20 pt-[30px]">
        <div className="mb-1.5 font-mono text-[11px] text-faint">◇ {projectName}</div>
        <header className="mb-5 flex items-end justify-between gap-4">
          <h1 className="font-serif text-[30px] font-semibold tracking-[-0.02em] text-ink">
            Runs
          </h1>
          <LivenessLegend />
        </header>

        <QueryBoundary
          q={runsQ}
          loading={
            <>
              <SectionLabel className="mb-2">Active</SectionLabel>
              <RunsBoardSkeleton />
            </>
          }
        >
          {(runs) => {
            if (runs.length === 0) {
              return (
                <EmptyState
                  title="No runs yet"
                  hint="Dispatched agents show up here the moment they take a lease."
                />
              );
            }
            const { active, recent } = partitionRuns(runs);
            return (
              <>
                <SectionLabel className="mb-2">Active · live</SectionLabel>
                <div className="mb-6">
                  {active.length === 0 ? (
                    <EmptyBoard>No active runs.</EmptyBoard>
                  ) : (
                    <Board>
                      <RunsTableHeader />
                      {active.map((run) => (
                        <ActiveRunRow key={run.taskId} run={run} />
                      ))}
                    </Board>
                  )}
                </div>

                <SectionLabel className="mb-2">Recent · settled</SectionLabel>
                {recent.length === 0 ? (
                  <EmptyBoard>No finished runs yet.</EmptyBoard>
                ) : (
                  <Board>
                    <RunsTableHeader />
                    {recent.map((run) => (
                      <RecentRunRow key={run.taskId} run={run} />
                    ))}
                  </Board>
                )}
              </>
            );
          }}
        </QueryBoundary>
      </div>
    </div>
  );
}
