import { getRouteApi } from "@tanstack/react-router";
import { NeedCard, rankNeeds } from "@/components/needs/NeedCard";
import { useRovingNeedFocus } from "@/features/inbox/GlobalInbox";
import { QueryBoundary } from "@/components/ui/states";
import { Kbd } from "@/components/ui/primitives";
import { useNeeds, useProjects } from "@/lib/queries";

const route = getRouteApi("/p/$projectId/needs");

/** Project-scoped needs-me queue. Same cards as the global inbox, filtered. */
export function ProjectNeeds() {
  const { projectId } = route.useParams();
  const needsQ = useNeeds(projectId);
  const projects = useProjects();
  const projectName = projects.data?.find((p) => p.id === projectId)?.name ?? projectId;
  const listRef = useRovingNeedFocus();

  return (
    <div className="tk-scroll h-full overflow-y-auto">
      <div className="mx-auto w-full max-w-[900px] px-4 pb-20 pt-[30px] sm:px-11">
        <div className="mb-1.5 font-mono text-[11px] text-faint">◇ {projectName}</div>
        <h1 className="font-serif text-[30px] font-semibold tracking-[-0.02em] text-ink">Needs me</h1>
        <p className="mb-5 mt-1.5 flex items-center gap-1.5 font-mono text-[11px] text-fainter">
          <Kbd>J</Kbd> jump to next · <Kbd>K</Kbd> previous
        </p>

        <QueryBoundary q={needsQ}>
          {(needs) =>
            needs.length === 0 ? (
              <div className="rounded-xl border border-line bg-panel p-12 text-center text-[14px] text-muted">
                Nothing needs you on this project.
              </div>
            ) : (
              <div ref={listRef} className="border-t border-line-soft">
                {rankNeeds(needs).map((need) => (
                  <NeedCard key={need.id} need={need} />
                ))}
              </div>
            )
          }
        </QueryBoundary>
      </div>
    </div>
  );
}
