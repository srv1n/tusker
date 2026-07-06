import { CheckCircle2 } from "lucide-react";
import { NeedCard, rankNeeds } from "@/components/needs/NeedCard";
import { QueryBoundary } from "@/components/ui/states";
import { Kbd } from "@/components/ui/primitives";
import { useNeeds, useProjects } from "@/lib/queries";

/**
 * Global inbox — every human gate across all projects, ranked by how much work
 * it blocks. The default landing view: "what needs me?" (packet §4.1, extended
 * to multi-project).
 */
export function GlobalInbox() {
  const needsQ = useNeeds();
  const projects = useProjects();
  const projectCount = projects.data?.length ?? 0;

  return (
    <div className="tk-scroll h-full overflow-y-auto">
      <div className="mx-auto w-full max-w-[960px] px-11 pb-20 pt-[34px]">
        <header className="mb-6">
          <h1 className="font-serif text-[34px] font-semibold leading-none tracking-[-0.02em] text-ink">
            Needs me
          </h1>
          <p className="mt-2.5 text-[13.5px] text-muted">
            {needsQ.data
              ? needsQ.data.length === 0
                ? "Nothing waiting."
                : `${needsQ.data.length} item${needsQ.data.length === 1 ? "" : "s"} across ${projectCount} projects.`
              : "Loading…"}
          </p>
          <p className="mt-2 flex items-center gap-1.5 font-mono text-[11px] text-fainter">
            <Kbd>J</Kbd> jump to next · <Kbd>K</Kbd> previous
          </p>
        </header>

        <QueryBoundary q={needsQ}>
          {(needs) =>
            needs.length === 0 ? (
              <div className="animate-rise rounded-xl border border-line bg-panel p-14 text-center">
                <div className="mx-auto mb-4 flex h-8 w-8 items-center justify-center rounded-full border-[1.5px] border-pass text-pass">
                  <CheckCircle2 size={18} />
                </div>
                <h2 className="font-serif text-[22px] font-semibold text-ink">Nothing needs you</h2>
                <p className="mt-2.5 text-[14px] text-muted">
                  Across all {projectCount} projects. The machine is running clean.
                </p>
              </div>
            ) : (
              <div className="border-t border-line-soft">
                {rankNeeds(needs).map((need) => (
                  <NeedCard key={need.id} need={need} showProject />
                ))}
              </div>
            )
          }
        </QueryBoundary>
      </div>
    </div>
  );
}
