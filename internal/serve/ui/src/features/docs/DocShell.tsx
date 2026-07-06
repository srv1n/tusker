import type { ReactNode } from "react";
import { ArrowLeft } from "lucide-react";
import { Link } from "@tanstack/react-router";
import { Mono } from "@/components/ui/primitives";
import { useProjects } from "@/lib/queries";

/**
 * The reader/editor frame: a sticky context bar (back to overview + doc path +
 * right-aligned actions) over a single scrolling body. Body owns its own grid.
 */
export function DocShell({
  projectId,
  path,
  actions,
  children,
}: {
  projectId: string;
  path: string;
  actions?: ReactNode;
  children: ReactNode;
}) {
  const projects = useProjects();
  const projectName = projects.data?.find((p) => p.id === projectId)?.name ?? projectId;

  return (
    <div className="flex h-full flex-col">
      <header className="sticky top-0 z-20 flex items-center gap-3 border-b border-line bg-surface/85 px-11 py-3 backdrop-blur-md">
        <Link
          to="/p/$projectId"
          params={{ projectId }}
          className="flex items-center gap-1.5 font-mono text-[11.5px] text-faint transition-colors hover:text-ink"
        >
          <ArrowLeft size={13} strokeWidth={2} />
          {projectName}
        </Link>
        <Mono className="truncate text-[11px] text-fainter">/ {path}</Mono>
        {actions && <div className="ml-auto flex flex-none items-center gap-2.5">{actions}</div>}
      </header>
      <div className="tk-scroll flex-1 overflow-y-auto">{children}</div>
    </div>
  );
}
