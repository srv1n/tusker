import type { ComponentType } from "react";
import { BookOpen, FileText, GitBranch } from "lucide-react";
import { cn } from "@/lib/cn";
import { Chip } from "@/components/ui/primitives";
import { tone, statusLabelOf, statusToneOf, type Tone } from "@/components/ui/tone";
import type { BacklinkVia, DocgraphKind } from "./types";

interface KindMeta {
  /** Short badge label. */
  label: string;
  /** Plural section header for the grouped list. */
  group: string;
  tone: Tone;
  Icon: ComponentType<{ size?: number; strokeWidth?: number; className?: string }>;
  /** CSS custom property the SVG graph paints nodes/legend with. */
  cssVar: string;
}

/** One hue + glyph per corpus kind. Three visually distinct theme colors. */
export const kindMeta: Record<DocgraphKind, KindMeta> = {
  canonical: { label: "System doc", group: "System docs", tone: "info", Icon: BookOpen, cssVar: "--k-info" },
  spec: { label: "Spec", group: "Specs", tone: "accent", Icon: FileText, cssVar: "--k-accent" },
  decision: { label: "Decision", group: "Decision logs", tone: "pass", Icon: GitBranch, cssVar: "--k-pass" },
};

export const KIND_ORDER: DocgraphKind[] = ["canonical", "spec", "decision"];

/** Kind glyph in its tone — leads each list row. */
export function KindGlyph({ kind, size = 15 }: { kind: DocgraphKind; size?: number }) {
  const m = kindMeta[kind];
  return (
    <span className={cn("inline-flex flex-none", tone[m.tone].text)}>
      <m.Icon size={size} strokeWidth={1.75} />
    </span>
  );
}

/** Soft kind pill for the reader header + backlinks. */
export function KindBadge({ kind }: { kind: DocgraphKind }) {
  const m = kindMeta[kind];
  return (
    <Chip tone={m.tone} variant="soft">
      <m.Icon size={12} strokeWidth={2} />
      {m.label}
    </Chip>
  );
}

/** Status chip — superseded is flagged loudly (warn), everything else stays quiet. */
export function DocStatusChip({ status }: { status: string }) {
  const superseded = status === "superseded";
  return (
    <Chip tone={superseded ? "warn" : statusToneOf(status)} variant={superseded ? "outline" : "soft"}>
      {statusLabelOf(status)}
    </Chip>
  );
}

const viaLabel: Record<BacklinkVia, string> = {
  wiki: "links",
  part_of: "part of",
  updates: "updates",
  decides_for: "decides for",
  superseded_by: "supersedes",
};

const viaTone: Record<BacklinkVia, Tone> = {
  wiki: "info",
  part_of: "muted",
  updates: "accent",
  decides_for: "pass",
  superseded_by: "warn",
};

export function ViaChip({ via }: { via: BacklinkVia }) {
  return (
    <Chip tone={viaTone[via]} variant="outline" mono>
      {viaLabel[via]}
    </Chip>
  );
}
