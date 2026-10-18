import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { parseSilenceMinutes } from "@/features/product/OperationsScreens";
import { processStateLabel, processStateTone } from "@/features/executions/ProcessesSection";
import { invalidateStreamEvent } from "@/lib/stream";

const read = (path: string) => readFileSync(new URL(path, import.meta.url), "utf8");

test("processes section sits on Execution Operations with the spec columns and an inline stop confirm", () => {
  const screen = read("../src/features/executions/ExecutionOperations.tsx");
  const section = read("../src/features/executions/ProcessesSection.tsx");
  expect(screen).toContain("<ProcessesSection projectId={projectId} />");
  for (const column of ["What", "State", "Runtime · last sign of life", "Owner", "PID · group"]) expect(section).toContain(column);
  expect(section).toContain("Confirm");
  expect(section).not.toMatch(/\b(window\.)?(confirm|alert)\(/);
});

test("a long-running process is not shown as a problem; silent is", () => {
  expect(processStateTone("long_running")).not.toBe("warn");
  expect(processStateTone("long_running")).not.toBe("fail");
  expect(processStateTone("silent")).toBe("warn");
  expect(processStateLabel("long_running")).toBe("long-running");
});

test("the process list refreshes from the change stream, not its own poll", () => {
  for (const key of ["runs:APP-T-0001", "daemon", "executions"]) {
    const invalidated: unknown[] = [];
    invalidateStreamEvent({ invalidateQueries: (filters?: { queryKey?: unknown }) => { invalidated.push(filters?.queryKey); return Promise.resolve(); } } as never, { kind: "changed", keys: [key], project: "app" });
    expect(invalidated).toContainEqual(["processes"]);
  }
});

test("silence limits accept whole minutes from 1 to 1440 only", () => {
  expect(parseSilenceMinutes("15")).toEqual({ value: 15 });
  for (const bad of ["0", "1441", "2.5", "", "ten"]) expect(parseSilenceMinutes(bad).error).toBeDefined();
});
