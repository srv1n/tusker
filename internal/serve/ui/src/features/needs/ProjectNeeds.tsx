import { NeedCard, rankNeeds } from "@/components/needs/NeedCard";
import { useRovingNeedFocus } from "@/features/inbox/GlobalInbox";
import { QueryBoundary } from "@/components/ui/states";
import { useNeeds } from "@/lib/queries";

/**
 * Project attention queue — the same actionable NeedCards as the global inbox,
 * scoped to one project. Absorbed into the Overview (SRV-T-0003); the old
 * standalone /needs route now redirects to the Overview, which renders this list
 * as its "attention first" section. J/K roving focus still works when it renders.
 */
export function NeedsList({ projectId }: { projectId: string }) {
  const needsQ = useNeeds(projectId);
  const listRef = useRovingNeedFocus();

  return (
    <QueryBoundary q={needsQ}>
      {(needs) =>
        needs.length === 0 ? (
          <div className="rounded-xl border border-dashed border-line bg-panel px-4 py-8 text-center text-[13px] text-muted">
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
  );
}
