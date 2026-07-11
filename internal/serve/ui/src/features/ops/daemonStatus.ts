import type { DiskPressureStatus } from "@/types/domain";

export type DiskPressureTone = "muted" | "warn" | "fail" | "pass";

export interface DiskPressurePresentation {
  label: string;
  reason?: string;
  tone: DiskPressureTone;
}

export function diskPressurePresentation(status: DiskPressureStatus | undefined): DiskPressurePresentation | undefined {
  if (!status) return undefined;

  const reason = status.reason?.trim() || undefined;
  const eligibility = status.dispatch_paused ? "new dispatch paused" : "new dispatch eligible";

  switch (status.state) {
    case "paused":
      return { label: "Disk pressure paused new dispatch", reason, tone: "fail" };
    case "warning":
      return { label: `Disk pressure warning; ${eligibility}`, reason, tone: "warn" };
    case "recovered":
      return { label: `Disk pressure recovered; ${eligibility}`, reason, tone: "pass" };
    case "disabled":
      return { label: "Disk pressure guard disabled", reason, tone: "muted" };
    default:
      return { label: `Disk pressure: ${status.state || "unknown"}`, reason, tone: "muted" };
  }
}
