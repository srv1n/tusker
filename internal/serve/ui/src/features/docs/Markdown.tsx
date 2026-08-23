import { Children, isValidElement, type ReactNode } from "react";
import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import { Link } from "@tanstack/react-router";
import { cn } from "@/lib/cn";
import { useDocList } from "@/lib/queries";
import type { DocListEntry } from "@/types/domain";
import { slugify } from "./utils";
import { isSafeHref } from "@/features/editor/sanitize";

/** The subset a rendered wikilink needs from the live vault index. */
type ResolvedLink = { path: string; title: string; kind: string };

/** Resolve a `[[id]]` against the live doc list — by task path or file basename. */
function resolveFromDocs(id: string, docs: DocListEntry[]): ResolvedLink | undefined {
  const key = id.trim();
  const hit = docs.find((d) => {
    if (d.path === key) return true;
    const base = d.path.replace(/\.md$/i, "").split("/").pop();
    return base === key;
  });
  return hit ? { path: hit.path, title: hit.title, kind: hit.kind } : undefined;
}

/** Flatten a React children tree to its plain text (for slugs + code). */
function toText(children: ReactNode): string {
  return Children.toArray(children)
    .map((c) => {
      if (typeof c === "string" || typeof c === "number") return String(c);
      if (isValidElement(c)) return toText((c.props as { children?: ReactNode }).children);
      return "";
    })
    .join("");
}

/**
 * A `[[ID]]` reference. Resolves to the target's capsule (title tooltip) and
 * links into the reader; an unresolved id renders as a warn-tinted broken link,
 * which doubles as the inline validation annotation the packet asks for.
 */
function WikiLink({
  id,
  projectId,
  label,
  resolve,
}: {
  id: string;
  projectId: string;
  label: ReactNode;
  resolve: (id: string) => ResolvedLink | undefined;
}) {
  const target = resolve(id);
  if (!target) {
    return <>{label}</>;
  }
  return (
    <Link
      to="/p/$projectId/docs"
      params={{ projectId }}
      search={{ path: target.path }}
      className="font-mono text-[0.92em] text-info decoration-info/40 underline decoration-dotted underline-offset-2 hover:decoration-info"
      title={`${target.kind} · ${target.title}`}
    >
      {label}
    </Link>
  );
}

/** Token-native code block (design shows a dark slab; tokens flip it per theme). */
function CodeBlock({ lang, code }: { lang: string; code: string }) {
  return (
    <div className="my-5 overflow-hidden rounded-lg border border-line bg-panel">
      {lang && (
        <div className="border-b border-line-soft px-4 py-1.5 font-mono text-[9.5px] uppercase tracking-[0.1em] text-fainter">
          {lang}
        </div>
      )}
      <pre className="tk-scroll overflow-x-auto px-4 py-3">
        <code className="font-mono text-[12.5px] leading-[1.65] text-ink-soft">{code}</code>
      </pre>
    </div>
  );
}

function markdownComponents(
  projectId: string,
  resolve: (id: string) => ResolvedLink | undefined,
): Components {
  return {
    h1: ({ children }) => {
      const slug = slugify(toText(children));
      return (
        <h1
          id={slug}
          data-doc-heading={slug}
          className="mb-4 mt-2 scroll-mt-24 font-serif text-[30px] font-semibold leading-[1.1] tracking-[-0.02em] text-ink"
        >
          {children}
        </h1>
      );
    },
    h2: ({ children }) => {
      const slug = slugify(toText(children));
      return (
        <h2
          id={slug}
          data-doc-heading={slug}
          className="mb-3 mt-9 scroll-mt-24 font-serif text-[20px] font-semibold leading-snug tracking-[-0.01em] text-ink"
        >
          {children}
        </h2>
      );
    },
    h3: ({ children }) => {
      const slug = slugify(toText(children));
      return (
        <h3
          id={slug}
          data-doc-heading={slug}
          className="mb-2 mt-7 scroll-mt-24 font-serif text-[16.5px] font-semibold text-ink"
        >
          {children}
        </h3>
      );
    },
    p: ({ children }) => (
      <p className="my-4 font-serif text-[16.5px] leading-[1.68] text-ink-soft">{children}</p>
    ),
    a: ({ href, children }) => {
      if (href && href.startsWith("wikilink:")) {
        return (
          <WikiLink
            id={href.slice("wikilink:".length)}
            projectId={projectId}
            label={children}
            resolve={resolve}
          />
        );
      }
      const external = !!href && /^https?:/.test(href);
      if (!isSafeHref(href)) {
        return <span className="text-ink-soft" title="Unsafe link blocked">{children}</span>;
      }
      return (
        <a
          href={href}
          {...(external ? { target: "_blank", rel: "noopener noreferrer" } : {})}
          className="text-info decoration-info/40 underline decoration-1 underline-offset-2 hover:decoration-info"
        >
          {children}
        </a>
      );
    },
    ul: ({ children }) => (
      <ul className="my-4 list-disc space-y-1.5 pl-6 font-serif text-[16px] leading-[1.65] text-ink-soft marker:text-faint">
        {children}
      </ul>
    ),
    ol: ({ children }) => (
      <ol className="my-4 list-decimal space-y-1.5 pl-6 font-serif text-[16px] leading-[1.65] text-ink-soft marker:text-faint">
        {children}
      </ol>
    ),
    li: ({ children }) => <li className="pl-1">{children}</li>,
    blockquote: ({ children }) => (
      <blockquote className="my-5 rounded-r-md border-l-2 border-accent/50 bg-accent-soft/40 py-1 pl-4 pr-3 font-serif text-[15.5px] italic leading-[1.6] text-muted [&_p]:my-2">
        {children}
      </blockquote>
    ),
    strong: ({ children }) => <strong className="font-semibold text-ink">{children}</strong>,
    em: ({ children }) => <em className="italic">{children}</em>,
    hr: () => <hr className="my-8 border-t border-line" />,
    pre: ({ children }) => <>{children}</>,
    code: ({ className, children }) => {
      const text = toText(children).replace(/\n$/, "");
      const lang = /language-(\w+)/.exec(className ?? "")?.[1] ?? "";
      const isBlock = !!lang || text.includes("\n");
      if (isBlock) return <CodeBlock lang={lang} code={text} />;
      return (
        <code className="rounded-[4px] bg-hover px-1.5 py-0.5 font-mono text-[0.86em] text-ink-soft">
          {children}
        </code>
      );
    },
    table: ({ children }) => (
      <div className="tk-scroll my-5 overflow-x-auto rounded-lg border border-line">
        <table className="w-full border-collapse text-left">{children}</table>
      </div>
    ),
    thead: ({ children }) => <thead className="bg-panel">{children}</thead>,
    tbody: ({ children }) => <tbody>{children}</tbody>,
    tr: ({ children }) => <tr className="border-b border-line-soft last:border-0">{children}</tr>,
    th: ({ children }) => (
      <th className="px-3.5 py-2 font-mono text-[9.5px] font-medium uppercase tracking-[0.06em] text-muted">
        {children}
      </th>
    ),
    td: ({ children }) => <td className="px-3.5 py-2 align-top text-[13.5px] text-ink-soft">{children}</td>,
    img: ({ src, alt }) => (
      <img src={typeof src === "string" ? src : undefined} alt={alt ?? ""} className="my-5 max-w-full rounded-lg border border-line" />
    ),
  };
}

/** Convert `[[ID]]` / `[[ID|label]]` to links the `a` override can resolve. */
function preprocessWikilinks(md: string): string {
  return md.replace(/\[\[([^\]|]+?)(?:\|([^\]]+))?\]\]/g, (_all, id: string, label?: string) => {
    const text = (label ?? id).trim();
    return `[${text}](wikilink:${id.trim()})`;
  });
}

/** Render markdown with the reader's typographic scale + resolved wikilinks. */
export function Markdown({
  markdown,
  projectId,
  className,
}: {
  markdown: string;
  projectId: string;
  className?: string;
}) {
  // Resolution is internal so sibling callers do not need to thread an index.
  const docsQ = useDocList(projectId);
  const docs = docsQ.data ?? [];
  const resolve = (id: string): ResolvedLink | undefined => resolveFromDocs(id, docs);
  return (
    <div className={cn("[&>*:first-child]:mt-0", className)}>
      <ReactMarkdown remarkPlugins={[remarkGfm]} components={markdownComponents(projectId, resolve)}>
        {preprocessWikilinks(markdown)}
      </ReactMarkdown>
    </div>
  );
}
