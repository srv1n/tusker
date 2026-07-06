import { useEffect, useState } from "react";
import { cn } from "@/lib/cn";
import type { DocOutlineEntry } from "@/types/domain";

/**
 * Left-hand outline (packet §4.6). Tracks the heading nearest the top via an
 * IntersectionObserver so the active section stays lit as the reader scrolls.
 */
export function Outline({ entries }: { entries: DocOutlineEntry[] }) {
  const [active, setActive] = useState<string | null>(entries[0]?.slug ?? null);

  useEffect(() => {
    const els = Array.from(document.querySelectorAll<HTMLElement>("[data-doc-heading]"));
    if (els.length === 0) return;
    const io = new IntersectionObserver(
      (obs) => {
        const top = obs
          .filter((e) => e.isIntersecting)
          .sort((a, b) => a.boundingClientRect.top - b.boundingClientRect.top)[0];
        if (top) setActive(top.target.getAttribute("data-doc-heading"));
      },
      { rootMargin: "-88px 0px -68% 0px", threshold: 0 },
    );
    els.forEach((el) => io.observe(el));
    return () => io.disconnect();
  }, [entries]);

  if (entries.length === 0) return null;

  return (
    <nav className="sticky top-6 flex flex-col gap-0.5">
      <div className="mb-2 pl-3 font-mono text-[9px] uppercase tracking-[0.14em] text-fainter">
        On this page
      </div>
      {entries.map((e) => {
        const isActive = e.slug === active;
        return (
          <button
            key={e.slug}
            onClick={() =>
              document.getElementById(e.slug)?.scrollIntoView({ behavior: "smooth", block: "start" })
            }
            className={cn(
              "border-l-2 py-1 pr-2 text-left text-[12.5px] leading-snug transition-colors",
              e.level === 3 ? "pl-6" : "pl-3",
              isActive
                ? "border-ink font-medium text-ink"
                : "border-transparent text-faint hover:border-line hover:text-ink-soft",
            )}
          >
            {e.text}
          </button>
        );
      })}
    </nav>
  );
}
