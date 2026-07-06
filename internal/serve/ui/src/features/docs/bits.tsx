import type { ComponentType } from "react";
import {
  BookOpen,
  FileText,
  GitBranch,
  Layers,
  ListChecks,
} from "lucide-react";
import { cn } from "@/lib/cn";
import { Chip } from "@/components/ui/primitives";
import { tone, type Tone } from "@/components/ui/tone";
import type { DocKind, VerificationRow } from "@/types/domain";

interface KindMeta {
  label: string;
  tone: Tone;
  Icon: ComponentType<{ size?: number; strokeWidth?: number; className?: string }>;
}

/** One hue + glyph per doc kind — color carries meaning (packet §6). */
export const kindMeta: Record<DocKind, KindMeta> = {
  spec: { label: "Spec", tone: "info", Icon: FileText },
  decision: { label: "Decision", tone: "accent", Icon: GitBranch },
  knowledge: { label: "Knowledge", tone: "muted", Icon: BookOpen },
  task: { label: "Task", tone: "warn", Icon: ListChecks },
  epic: { label: "Epic", tone: "pass", Icon: Layers },
  dashboard: { label: "Dashboard", tone: "info", Icon: Layers },
};

/** Letter-spaced mono eyebrow, tinted by kind (design: doc.typeLabel). */
export function KindEyebrow({ kind, className }: { kind: DocKind; className?: string }) {
  const m = kindMeta[kind];
  return (
    <div
      className={cn(
        "font-mono text-[10.5px] font-medium uppercase tracking-[0.14em]",
        tone[m.tone].text,
        className,
      )}
    >
      {m.label}
    </div>
  );
}

/** Kind glyph in its tone — used in the library rows. */
export function KindGlyph({ kind, size = 15 }: { kind: DocKind; size?: number }) {
  const m = kindMeta[kind];
  return (
    <span className={cn("inline-flex flex-none", tone[m.tone].text)}>
      <m.Icon size={size} strokeWidth={1.75} />
    </span>
  );
}

const resultTone: Record<VerificationRow["result"], Tone> = {
  pass: "pass",
  fail: "fail",
  pending: "neutral",
};

/** Verification-row outcome chip (there's no shared one). */
export function ResultChip({ result }: { result: VerificationRow["result"] }) {
  return (
    <Chip tone={resultTone[result]} variant="soft" mono>
      {result}
    </Chip>
  );
}
