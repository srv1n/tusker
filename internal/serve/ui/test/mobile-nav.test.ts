import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";

const source = (path: string) => readFileSync(new URL(`../${path}`, import.meta.url), "utf8");

test("phone layout: rail hidden below lg, bottom bar reuses desktop sections and opens the project drawer", () => {
  const root = source("src/routes/__root.tsx");
  const nav = source("src/components/MobileNav.tsx");
  const strip = source("src/features/workbench/navigation/ProjectStrip.tsx");

  expect(strip).toContain("export const PROJECT_SECTIONS");
  expect(root).toContain('"hidden lg:flex"');
  expect(root).toContain("<MobileNav");
  expect(nav).toContain("PROJECT_SECTIONS.map");
  expect(nav).toContain("lg:hidden");
  expect(nav).toContain('aria-current={active ? "page" : undefined}');
  expect(nav).toContain("need you");
  expect(nav).toContain("aria-expanded={projectsOpen}");
});
