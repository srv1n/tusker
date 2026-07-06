import { ChevronDown, Lock } from "lucide-react";
import type { DocMeta } from "@/types/domain";

type Frontmatter = DocMeta["frontmatter"];

/**
 * The locked property panel (packet §4.6). Frontmatter is shown as controls,
 * never raw YAML; state fields (`status`, `state_rev`, …) are visibly
 * read-only. Free fields carry an affordance but editing them is a follow-up.
 */
export function PropertyPanel({ frontmatter }: { frontmatter: Frontmatter }) {
  if (frontmatter.length === 0) return null;
  return (
    <div className="mb-7 rounded-lg border border-line bg-panel px-4 py-3">
      <div className="mb-2.5 font-mono text-[9px] uppercase tracking-[0.1em] text-faint">
        Properties <span className="text-fainter">· state fields locked</span>
      </div>
      <div className="flex flex-wrap gap-x-4 gap-y-2">
        {frontmatter.map((p) => (
          <div key={p.key} className="flex items-center gap-1.5">
            <span className="font-mono text-[10.5px] text-faint">{p.key}</span>
            {p.locked ? (
              <span
                className="inline-flex items-center gap-1 rounded-md bg-hover px-2 py-[3px] font-mono text-[11px] text-muted"
                title="Read-only state field — managed by tusker"
              >
                {p.value}
                <Lock size={9} strokeWidth={2.25} className="text-fainter" />
              </span>
            ) : (
              <button
                type="button"
                // TODO(api): free-field editor (title/priority) via structured control.
                className="inline-flex items-center gap-1 rounded-md border border-line bg-raised px-2 py-[3px] font-mono text-[11px] text-ink-soft transition-colors hover:border-line-soft hover:bg-hover"
                title="Editable field"
              >
                {p.value}
                <ChevronDown size={11} strokeWidth={2} className="text-faint" />
              </button>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}
