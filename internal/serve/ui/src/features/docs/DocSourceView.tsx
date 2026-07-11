import { Link } from "@tanstack/react-router";
import { FileText } from "lucide-react";
import { Mono } from "@/components/ui/primitives";
import { ErrorState, Skeleton } from "@/components/ui/states";
import { useDoc } from "@/lib/queries";
import { DocShell } from "./DocShell";
import { taskIdFromDocPath } from "./taskMarkdown";

export function DocSourceView({ projectId, path }: { projectId: string; path: string }) {
  const q = useDoc(path, projectId);
  // Live mode never falls back to a fixture body: an absent doc is an error/empty
  // state (below), never fabricated source.
  const doc = q.data;
  const taskId = taskIdFromDocPath(path);

  if (!doc) {
    if (q.isLoading) return <SourceSkeleton projectId={projectId} path={path} />;
    return (
      <div className="p-8">
        <ErrorState error={q.error} onRetry={() => q.refetch()} />
      </div>
    );
  }

  return (
    <DocShell
      projectId={projectId}
      path={doc.path}
      actions={
        taskId ? (
          <Link
            to="/p/$projectId/docs"
            params={{ projectId }}
            search={{ path: taskId }}
            className="flex items-center gap-1.5 rounded-lg border border-line bg-raised px-3.5 py-1.5 text-[12.5px] font-semibold text-ink-soft transition-colors hover:border-line-soft hover:bg-hover"
          >
            <FileText size={13} strokeWidth={2} />
            Task view
          </Link>
        ) : undefined
      }
    >
      <div className="mx-auto w-full max-w-[980px] px-4 pb-24 pt-7 sm:px-6 lg:px-11">
        <div className="mb-2 flex items-center justify-between">
          <Mono className="text-[9.5px] uppercase tracking-[0.12em] text-fainter">
            Source
          </Mono>
          <Mono className="text-[10px] text-faint">read-only</Mono>
        </div>
        <pre className="tk-scroll overflow-auto rounded-lg border border-line bg-panel p-4 font-mono text-[12px] leading-[1.65] text-ink-soft">
          {doc.markdown}
        </pre>
      </div>
    </DocShell>
  );
}

function SourceSkeleton({ projectId, path }: { projectId: string; path: string }) {
  return (
    <DocShell projectId={projectId} path={path}>
      <div className="mx-auto w-full max-w-[980px] px-4 pt-7 sm:px-6 lg:px-11">
        <Skeleton className="mb-3 h-3 w-24" />
        <Skeleton className="h-96 w-full" />
      </div>
    </DocShell>
  );
}
