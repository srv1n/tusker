import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { diskPressurePresentation } from "../src/features/ops/daemonStatus";
import type { DiskPressureStatus } from "../src/types/domain";

function diskPressure(state: string, dispatchPaused = false): DiskPressureStatus {
  return {
    state,
    enabled: state !== "disabled",
    dispatch_paused: dispatchPaused,
    warning: state === "warning",
    recovered: state === "recovered",
    min_free_bytes: 2 << 30,
    min_free_percent: 1,
    effective_threshold_bytes: 2 << 30,
    warning_threshold_bytes: 4 << 30,
    filesystems: [],
    config: { enabled: state !== "disabled", min_free_bytes: 2 << 30, min_free_percent: 1, source: "runtime" },
  };
}

test("disk-pressure daemon projection keeps dispatch state honest", () => {
  expect(diskPressurePresentation(diskPressure("warning"))).toMatchObject({
    label: "Disk pressure warning; new dispatch eligible",
    tone: "warn",
  });
  expect(diskPressurePresentation(diskPressure("paused", true))).toMatchObject({
    label: "Disk pressure paused new dispatch",
    tone: "fail",
  });
  expect(diskPressurePresentation(diskPressure("recovered"))).toMatchObject({
    label: "Disk pressure recovered; new dispatch eligible",
    tone: "pass",
  });
  expect(diskPressurePresentation(diskPressure("disabled"))).toMatchObject({
    label: "Disk pressure guard disabled",
    tone: "muted",
  });
});

test("the daemon query type and Ops panel retain the disk-pressure projection", () => {
  const types = readFileSync("src/types/domain.ts", "utf8");
  const api = readFileSync("src/lib/api.ts", "utf8");
  const panel = readFileSync("src/features/ops/ProjectOps.tsx", "utf8");

  expect(types).toContain("diskPressure?: DiskPressureStatus");
  expect(api).toContain("daemon: (): Promise<DaemonStatus>");
  expect(panel).toContain("diskPressurePresentation(status.diskPressure)");
  expect(panel).toContain("data-daemon-disk-pressure");
});
