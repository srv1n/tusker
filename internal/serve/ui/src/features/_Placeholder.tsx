import { PageHeader, PageScroll, SectionLabel } from "@/components/ui/page";
import { EmptyState } from "@/components/ui/states";

/**
 * Temporary scaffold for screens still being built out in the fan-out. Each
 * feature replaces its own file with the real, design-faithful screen.
 */
export function ScreenScaffold({ eyebrow, title }: { eyebrow: string; title: string }) {
  return (
    <PageScroll>
      <PageHeader eyebrow={<SectionLabel>{eyebrow}</SectionLabel>} title={title} />
      <EmptyState title="Screen under construction" hint="Being built in the current pass." />
    </PageScroll>
  );
}
