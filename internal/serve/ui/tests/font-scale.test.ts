import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";

const css = readFileSync(new URL("../src/styles/app.css", import.meta.url), "utf8");
const scale = readFileSync(new URL("../src/lib/font-scale.tsx", import.meta.url), "utf8");
const settings = readFileSync(new URL("../src/features/settings/app/GeneralSection.tsx", import.meta.url), "utf8");

test("one persisted font setting drives the full app and TipTap surface", () => {
  expect(css).toContain("--tk-font-family");
  expect(css).toContain("--tk-prose-body-size: 13px");
  expect(css).toContain("--tk-prose-h1-size: 26px");
  expect(css).toContain("--tk-prose-h2-size: 22px");
  expect(css).toContain("--tk-prose-h3-size: 17px");
  expect(css).toContain("font-size: var(--tk-prose-body-size)");
  expect(css).toContain("zoom: var(--tk-font-scale)");
  expect(css).toContain("height: calc(100dvh / var(--tk-font-scale))");
  expect(scale).toContain('const STORAGE_KEY = "tusker-font-scale"');
  expect(scale).toContain('const FONT_FAMILY_STORAGE_KEY = "tusker-font-family"');
  expect(scale).toContain('"Iowan Old Style"');
  expect(scale).toContain('"JetBrains Mono"');
  expect(settings).toContain('label="Text size"');
  expect(settings).toContain('label="Font"');
});
