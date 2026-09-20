import { expect, test } from "bun:test";
import { initialWaveView, settleEnteredWaveView } from "../src/features/workbench/integration/integrationModel";
import { makeWave } from "../src/features/workbench/overview/previewFixtures";

test("Dependencies is the fresh default for every wave lifecycle", () => {
  for (const status of ["open", "running", "blocked", "completed", "closed"]) {
    const wave = makeWave({ status });
    expect(initialWaveView(wave)).toBe("flow");
    expect(settleEnteredWaveView(null, wave, false)).toBe("flow");
  }
  const wave = makeWave({ status: "completed" });
  expect(initialWaveView(wave, "work")).toBe("work");
  expect(initialWaveView(wave, "results")).toBe("results");
  expect(settleEnteredWaveView("work", wave, false)).toBe("work");
});
