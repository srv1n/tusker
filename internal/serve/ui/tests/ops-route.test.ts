import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";

const router = readFileSync(new URL("../src/router.tsx", import.meta.url), "utf8");
const ops = readFileSync(new URL("../src/features/ops/ProjectOps.tsx", import.meta.url), "utf8");

test("ops is a live route for factory operations and wave deep links", () => {
  const route = router.slice(router.indexOf("const opsRoute"), router.indexOf("const docsRoute"));
  expect(route).toContain('path: "ops"');
  expect(route).toContain('import("@/features/ops/ProjectOps")');
  expect(route).toContain('"ProjectOps"');
  expect(route).not.toContain("beforeLoad");
  expect(ops).toContain('id={`wave-${wave.id}`}');
  expect(ops).toContain("<FactoryOperationsSurface projection={projection} />");
});
