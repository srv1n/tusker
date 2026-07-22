/*
  The Docs index route (SRV-T-0004, A6).

  There is no bespoke landing page: the Docs section is the familiar two-pane
  notes layout, and the index simply opens that layout on the corpus root
  document (the overview — the parentless canonical doc, falling back to the
  first doc). While the corpus is still loading, or if it is empty, the rail and
  a slim toolbar still show.
*/

import { useMemo } from "react";
import { getRouteApi } from "@tanstack/react-router";
import { Network } from "lucide-react";
import { EmptyState } from "@/components/ui/states";
import { useDocgraph } from "@/lib/queries";
import type { DocgraphDoc } from "./types";
import { KnowledgeShell, SectionToolbar, ViewSwitch } from "./KnowledgeShell";
import { DocSection } from "./KnowledgeReader";

const route = getRouteApi("/p/$projectId/knowledge");

/** The corpus root: the overview, else a parentless canonical doc, else first. */
function resolveRootSubject(docs: DocgraphDoc[] | undefined): string | undefined {
  if (!docs || docs.length === 0) return undefined;
  const parentless = docs.filter((d) => !d.part_of);
  const root =
    parentless.find((d) => d.subject === "overview") ??
    parentless.find((d) => d.kind === "canonical") ??
    parentless[0] ??
    docs[0];
  return root?.subject;
}

export function KnowledgeList() {
  const { projectId } = route.useParams();
  const q = useDocgraph(projectId);
  const rootSubject = useMemo(() => resolveRootSubject(q.data?.docs), [q.data]);

  // Once the corpus is known, the Docs index IS the two-pane doc view opened on
  // the root document — no separate card-gallery landing page.
  if (rootSubject) return <DocSection projectId={projectId} subject={rootSubject} />;

  return (
    <KnowledgeShell projectId={projectId}>
      <div className="flex h-full flex-col">
        <SectionToolbar right={<ViewSwitch projectId={projectId} active="files" />} />
        <div className="flex flex-1 items-center justify-center p-8">
          {!q.isLoading && (
            <EmptyState
              icon={<Network size={22} strokeWidth={1.5} />}
              title="No documentation corpus yet"
              hint="Canonical system docs, specs, and decision logs with doc-graph headers will appear here once the vault has them."
            />
          )}
        </div>
      </div>
    </KnowledgeShell>
  );
}
