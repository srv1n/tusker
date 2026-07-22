import { Children, isValidElement, useMemo, type ReactNode } from "react";
import ReactMarkdown, { defaultUrlTransform, type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import { Link } from "@tanstack/react-router";
import { cn } from "@/lib/cn";
import type { DocLinkRef } from "./types";

/** Flatten a React children tree to plain text (for slugs + code). */
function toText(children: ReactNode): string {
  return Children.toArray(children)
    .map((c) => {
      if (typeof c === "string" || typeof c === "number") return String(c);
      if (isValidElement(c)) return toText((c.props as { children?: ReactNode }).children);
      return "";
    })
    .join("");
}

function slugify(text: string): string {
  return text
    .toLowerCase()
    .replace(/[^\w\s-]/g, "")
    .trim()
    .replace(/\s+/g, "-");
}

const WIKI_SCHEME = "doc:";

/** `[[ref]]` / `[[ref|label]]` → a `doc:`-scheme link the `a` override resolves. */
function preprocessWikilinks(md: string): string {
  return md.replace(/\[\[([^\]|]+?)(?:\|([^\]]+))?\]\]/g, (_all, ref: string, label?: string) => {
    const text = (label ?? ref).trim();
    return `[${text}](${WIKI_SCHEME}${encodeURIComponent(ref.trim())})`;
  });
}

// react-markdown's default transform strips unknown schemes (including our
// sentinel) to "" before the `a` override runs; only `doc:` may bypass it, so
// javascript:/data: hrefs in doc bodies stay neutralized.
function wikiUrlTransform(url: string): string {
  return url.startsWith(WIKI_SCHEME) ? url : defaultUrlTransform(url);
}

/**
 * A `[[ref]]` reference. Resolution comes straight from the API `links` array:
 * a resolved ref links into the referenced doc's reader; anything the corpus
 * could not resolve is marked visibly (muted, dotted underline, tooltip) so a
 * dangling reference is never silent.
 */
function WikiLink({
  refText,
  projectId,
  label,
  link,
}: {
  refText: string;
  projectId: string;
  label: ReactNode;
  link: DocLinkRef | undefined;
}) {
  if (!link || !link.resolved) {
    return (
      <span
        className="cursor-help font-mono text-[0.92em] text-muted decoration-muted/50 underline decoration-dotted underline-offset-2"
        title="No such document — this reference does not resolve in the corpus"
      >
        {label ?? refText}
      </span>
    );
  }
  return (
    <Link
      to="/p/$projectId/knowledge/$subject"
      params={{ projectId, subject: link.subject }}
      className="font-mono text-[0.92em] text-info decoration-info/40 underline decoration-dotted underline-offset-2 hover:decoration-info"
      title={link.path}
    >
      {label ?? refText}
    </Link>
  );
}

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
  linkFor: (ref: string) => DocLinkRef | undefined,
): Components {
  return {
    h1: ({ children }) => (
      <h1 id={slugify(toText(children))} className="mb-4 mt-2 scroll-mt-24 font-serif text-[30px] font-semibold leading-[1.1] tracking-[-0.02em] text-ink">
        {children}
      </h1>
    ),
    h2: ({ children }) => (
      <h2 id={slugify(toText(children))} className="mb-3 mt-9 scroll-mt-24 font-serif text-[20px] font-semibold leading-snug tracking-[-0.01em] text-ink">
        {children}
      </h2>
    ),
    h3: ({ children }) => (
      <h3 id={slugify(toText(children))} className="mb-2 mt-7 scroll-mt-24 font-serif text-[16.5px] font-semibold text-ink">
        {children}
      </h3>
    ),
    p: ({ children }) => (
      <p className="my-4 font-serif text-[16.5px] leading-[1.68] text-ink-soft">{children}</p>
    ),
    a: ({ href, children }) => {
      if (href && href.startsWith(WIKI_SCHEME)) {
        const refText = decodeURIComponent(href.slice(WIKI_SCHEME.length));
        return (
          <WikiLink refText={refText} projectId={projectId} label={children} link={linkFor(refText)} />
        );
      }
      const external = !!href && /^https?:/.test(href);
      return (
        <a
          href={href}
          {...(external ? { target: "_blank", rel: "noreferrer" } : {})}
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

export function KnowledgeMarkdown({
  body,
  projectId,
  links,
  className,
}: {
  body: string;
  projectId: string;
  links: DocLinkRef[];
  className?: string;
}) {
  const linkMap = useMemo(() => new Map(links.map((l) => [l.ref, l])), [links]);
  const components = useMemo(
    () => markdownComponents(projectId, (ref) => linkMap.get(ref)),
    [projectId, linkMap],
  );
  return (
    <div className={cn("[&>*:first-child]:mt-0", className)}>
      <ReactMarkdown remarkPlugins={[remarkGfm]} urlTransform={wikiUrlTransform} components={components}>
        {preprocessWikilinks(body)}
      </ReactMarkdown>
    </div>
  );
}
