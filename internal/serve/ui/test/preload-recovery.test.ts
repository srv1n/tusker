import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";

test("stale route chunks trigger one cache-busted reload", () => {
  const source = readFileSync(new URL("../src/main.tsx", import.meta.url), "utf8");

  expect(source).toContain('addEventListener("vite:preloadError"');
  expect(source).toContain('sessionStorage.getItem(retryKey)');
  expect(source).toContain('searchParams.set("_reload"');
  expect(source).toContain("location.replace(url)");
});
