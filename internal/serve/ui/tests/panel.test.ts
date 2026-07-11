import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";

const panel = readFileSync(new URL("../src/features/panel/Panel.tsx", import.meta.url), "utf8");
const root = readFileSync(new URL("../src/routes/__root.tsx", import.meta.url), "utf8");

test("panel row navigation prefers the optional shell bridge and has a router fallback", () => {
  expect(panel).toContain("window.tuskerShell?.openFull");
  expect(panel).toContain("navigate({ to: row.path as \"/\" })");
});

test("panel shell mode persists the query flag and recognizes the embedded user agent", () => {
  expect(root).toContain('get("shell") === "1"');
  expect(root).toContain('navigator.userAgent.includes("TuskerShell/")');
  expect(root).toContain('location.pathname === "/panel"');
});

test("panel registers the optional in-page deep-link hook for the native shell", () => {
  expect(panel).toContain("shell.onNavigate = onNavigate");
  expect(panel).toContain("navigate({ to: path as \"/\" })");
});

test("panel header opens the native desktop window with a browser fallback", () => {
  expect(panel).toContain('aria-label="Open the main Tusker window"');
  expect(panel).toContain('bridge("/")');
  expect(panel).toContain('navigate({ to: "/" })');
});
