import type { ReactNode } from "react";
import { Dot } from "@/components/ui/primitives";
import { SectionLabel } from "@/components/ui/page";
import { EmptyState, QueryBoundary } from "@/components/ui/states";
import { useRuns } from "@/lib/queries";
import { partitionRuns } from "@/features/runs/board/helpers";
import {
  ActiveRunRow,
  RecentRunRow,
  RunsBoardSkeleton,
  RunsTableHeader,
} from "@/features/runs/board/rows";

/** Legend tying the liveness hues to the words the board reads by (§4.2). */
export function LivenessLegend() {
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
  return <div className="overflow-hidden rounded-[10px] border border-line">{children}</div>;
}

function EmptyBoard({ children }: { children: ReactNode }) {
  return (
    <div className="rounded-[10px] border border-line bg-panel px-4 py-8 text-center text-[13px] text-muted">
      {children}
    </div>
  );
}

/**
 * Project runs board (packet §4.2), absorbed into the Overview (SRV-T-0003). A
 * live table of agent runs — active first, sorted so a silently-dead process
 * floats to the top — with finished runs below. The old standalone /runs route
 * redirects to the Overview, which renders this below its attention section.
 */
export function RunsBoard({ projectId }: { projectId: string }) {
  const runsQ = useRuns(projectId);

  return (
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
  );
}
