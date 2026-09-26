import { Link, useLocation, useParams } from "@tanstack/react-router";
import { FolderOpen } from "lucide-react";
import { cn } from "@/lib/cn";
import { useProjects } from "@/lib/queries";
import { projectContainsCheckout } from "@/types/domain";
import { PROJECT_SECTIONS } from "@/features/workbench/navigation/ProjectStrip";

const ITEM = "flex min-h-11 min-w-0 flex-1 flex-col items-center justify-center gap-0.5 rounded-md text-[11px] outline-none focus-visible:ring-2 focus-visible:ring-info";

/** <lg only: project picker trigger plus the same project sections as the desktop rail. */
export function MobileNav({ projectsOpen, onProjects }: { projectsOpen: boolean; onProjects: () => void }) {
  const location = useLocation();
  const { projectId } = useParams({ strict: false }) as { projectId?: string };
  const project = useProjects().data?.find((item) => projectContainsCheckout(item, projectId ?? ""));
  const sectionRest = location.pathname.replace(/^\/p\/[^/]+/, "").replace(/\/$/, "");

  return (
    <nav aria-label="Section navigation" className="flex flex-none items-stretch gap-1 border-t border-line-soft bg-panel px-2 pt-1 pb-[max(0.25rem,env(safe-area-inset-bottom))] lg:hidden">
      <button type="button" onClick={onProjects} aria-expanded={projectsOpen} aria-label={project ? `Projects, current: ${project.name}` : "Projects"} className={cn(ITEM, "text-muted hover:text-ink")}>
        <FolderOpen size={18} aria-hidden="true" />
        <span className="max-w-full truncate px-1">{project?.name ?? "Projects"}</span>
      </button>
      {projectId && PROJECT_SECTIONS.map(({ label, to, icon: Icon, match }) => {
        const active = match(sectionRest);
        const badge = label === "Inbox" ? project?.needsCount ?? 0 : 0;
        return (
          <Link key={label} to={to} params={{ projectId }} aria-current={active ? "page" : undefined} aria-label={badge ? `${label}, ${badge} need you` : label} className={cn(ITEM, "relative", active ? "font-semibold text-ink" : "text-muted hover:text-ink")}>
            <Icon size={18} aria-hidden="true" />
            <span>{label}</span>
            {badge > 0 && <span aria-hidden="true" className="absolute right-[calc(50%-20px)] top-0.5 rounded-full bg-fail px-1 font-mono text-[9px] font-semibold text-white">{badge}</span>}
          </Link>
        );
      })}
    </nav>
  );
}
