import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";

const css = readFileSync(new URL("../src/styles/app.css", import.meta.url), "utf8");
const main = readFileSync(new URL("../src/main.tsx", import.meta.url), "utf8");

function block(source: string, start: string): string {
  const begin = source.indexOf(start);
  expect(begin).toBeGreaterThanOrEqual(0);
  const open = source.indexOf("{", begin);
  let depth = 0;
  for (let i = open; i < source.length; i++) {
    if (source[i] === "{") depth++;
    if (source[i] === "}") {
      depth--;
      if (depth === 0) return source.slice(open + 1, i);
    }
  }
  throw new Error(`unclosed block starting at ${start}`);
}

function tokens(blockText: string): Map<string, string> {
  const out = new Map<string, string>();
  for (const match of blockText.matchAll(/--(k-[a-z-]+):\s*(#[0-9a-fA-F]{6})/g)) {
    out.set(match[1]!, match[2]!.toLowerCase());
  }
  return out;
}

const light = tokens(block(css, ":root {"));
const dark = tokens(block(css, ':root[data-theme="dark"]'));
const systemDark = tokens(block(css, ":root:not([data-theme=\"light\"])"));

function luminance(hex: string): number {
  const c = [0, 2, 4].map((i) => parseInt(hex.slice(i + 1, i + 3), 16) / 255);
  const f = (v: number) => (v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4);
  return 0.2126 * f(c[0]!) + 0.7152 * f(c[1]!) + 0.0722 * f(c[2]!);
}

function contrast(a: string, b: string): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
}

// HIG Accessibility › Vision: text up to 17pt needs 4.5:1 (WCAG AA).
const MINIMUM = 4.5;

test("explicit dark theme and system-following dark stay in sync", () => {
  for (const [name, value] of dark) {
    expect(systemDark.get(name)).toBe(value);
  }
});

test("light tokens meet 4.5:1 for every text/background pairing", () => {
  const pairs: Array<[string, string]> = [
    ["k-accent", "k-raised"],
    ["k-raised", "k-accent"],
    ["k-accent", "k-accent-soft"],
    ["k-pass", "k-raised"],
    ["k-pass", "k-pass-soft"],
    ["k-warn", "k-raised"],
    ["k-warn", "k-warn-soft"],
    ["k-info", "k-raised"],
    ["k-info", "k-info-soft"],
    ["k-fail", "k-raised"],
    ["k-fail", "k-fail-soft"],
    ["k-muted", "k-surface"],
    ["k-ink", "k-surface"],
    ["k-ink-soft", "k-raised"],
  ];
  for (const [fg, bg] of pairs) {
    const ratio = contrast(light.get(fg)!, light.get(bg)!);
    expect(ratio).toBeGreaterThanOrEqual(MINIMUM);
  }
});

test("dark tokens meet 4.5:1 for every text/background pairing", () => {
  const pairs: Array<[string, string]> = [
    ["k-accent", "k-surface"],
    ["k-accent", "k-accent-soft"],
    ["k-pass", "k-pass-soft"],
    ["k-warn", "k-warn-soft"],
    ["k-info", "k-info-soft"],
    ["k-fail", "k-fail-soft"],
    ["k-muted", "k-surface"],
    ["k-ink", "k-surface"],
    ["k-ink-soft", "k-raised"],
  ];
  for (const [fg, bg] of pairs) {
    const ratio = contrast(dark.get(fg)!, dark.get(bg)!);
    expect(ratio).toBeGreaterThanOrEqual(MINIMUM);
  }
});

test("interface type is the platform system face, not a bundled geometric sans", () => {
  expect(css).toContain("-apple-system");
  expect(main).not.toContain("@fontsource/archivo");
});

test("decorative motion switches off under Reduce Motion", () => {
  expect(css).toContain("prefers-reduced-motion");
});
