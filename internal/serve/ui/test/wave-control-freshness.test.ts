import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { invalidateWaveControlQueries, qk } from "../src/lib/queries";

test("wave controls invalidate the current wave, run, and task query families", async () => {
  const invalidations: unknown[] = [];
  await invalidateWaveControlQueries(
    {
      invalidateQueries: (filters: unknown) => {
        invalidations.push(filters);
        return Promise.resolve();
      },
    },
    "tusker",
    "W-0025",
  );

  expect(invalidations).toEqual(expect.arrayContaining([
    { queryKey: qk.waveReview("tusker", "W-0025") },
    { queryKey: ["waves"] },
    { queryKey: ["runs"] },
    { queryKey: ["run"] },
    { queryKey: ["tasks"] },
    { queryKey: ["task"] },
    { queryKey: ["needs"] },
  ]));
});

test("start, pause, and resume share the freshness invalidation path", () => {
  const source = readFileSync(new URL("../src/lib/queries.ts", import.meta.url), "utf8");
  expect(source).toContain('useMutation<DirectStartResult, unknown, "start" | "pause" | "resume">');
  expect(source).toContain("onSettled: () => void invalidateWaveControlQueries(qc, projectId, waveId)");
});
