import type { ComponentType } from "react";
import { BookOpen, FileText, ScrollText } from "lucide-react";
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

/**
 * One hue + glyph per corpus kind. Portable kinds lead; `canonical`/`spec`
 * are legacy spellings that share their portable family's presentation so
 * old and new documents read as one tree.
 */
export const kindMeta: Record<DocgraphKind, KindMeta> = {
  doc: { label: "Doc", group: "Docs", tone: "info", Icon: BookOpen, cssVar: "--k-info" },
  canonical: { label: "System doc", group: "System docs", tone: "info", Icon: BookOpen, cssVar: "--k-info" },
  proposal: { label: "Proposal", group: "Proposals", tone: "accent", Icon: FileText, cssVar: "--k-accent" },
  spec: { label: "Spec", group: "Specs", tone: "accent", Icon: FileText, cssVar: "--k-accent" },
  decision: { label: "Decision", group: "Decisions", tone: "pass", Icon: ScrollText, cssVar: "--k-pass" },
};

/** Portable legend/group order. Legacy `canonical`/`spec` docs render under their family's entry. */
export const KIND_ORDER: DocgraphKind[] = ["doc", "proposal", "decision"];

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

const conformanceTone: Record<string, Tone> = {
  unverified: "muted",
  matches: "pass",
  drift: "warn",
  not_applicable: "muted",
};

const conformanceLabel: Record<string, string> = {
  unverified: "Unverified",
  matches: "Matches code",
  drift: "Drift",
  not_applicable: "N/A",
};

/**
 * Independent verification chip. Lifecycle (accepted/implemented/…) records
 * intent; this chip records whether anyone checked the code. Acceptance
 * without verification reads as plain "Unverified", never as proof.
 */
export function ConformanceChip({ conformance }: { conformance: string }) {
  const key = conformance === "" ? "unverified" : conformance;
  return (
    <Chip tone={conformanceTone[key] ?? "muted"} variant={key === "drift" ? "outline" : "soft"}>
      {conformanceLabel[key] ?? key}
    </Chip>
  );
}

/**
 * The reader's one-line truthful state: lifecycle intent kept separate from
 * the verification claim. Proposed, accepted-but-unverified,
 * implemented-with-evidence, drift, and superseded each read differently.
 */
export function truthfulState(lifecycle: string, conformance: string): string {
  const life = lifecycle === "" ? "—" : lifecycle;
  const conf = conformance === "" ? "unverified" : conformance;
  if (life === "superseded") return "Superseded";
  if (life === "proposed") return "Proposed";
  if (life === "implemented") return conf === "matches" ? "Implemented · verified" : "Implemented · unverified evidence";
  if (life === "accepted") return conf === "unverified" ? "Accepted · unverified" : `Accepted · ${conf}`;
  if (conf === "drift") return `${life} · drift`;
  if (conf === "matches") return `${life} · verified`;
  return life;
}

const viaLabel: Record<BacklinkVia, string> = {
  wiki: "links",
  part_of: "part of",
  updates: "updates",
  source: "source",
  decides_for: "decides for",
  superseded_by: "supersedes",
  link: "link",
};

const viaTone: Record<BacklinkVia, Tone> = {
  wiki: "info",
  part_of: "muted",
  updates: "accent",
  source: "info",
  decides_for: "pass",
  superseded_by: "warn",
  link: "accent",
};

export function ViaChip({ via }: { via: BacklinkVia }) {
  return (
    <Chip tone={viaTone[via]} variant="outline" mono>
      {viaLabel[via]}
    </Chip>
  );
}
