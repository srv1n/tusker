import { expect, test } from "bun:test";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { createServer } from "vite";

test("command policy details expose behavior, coverage, and limits accessibly", async () => {
  const playwright = await import("/Users/sarav/.bun/install/global/node_modules/playwright/index.mjs").catch(() => undefined);
  if (!playwright) return;
  const root = resolve(import.meta.dir, "..");
  const fixture = mkdtempSync(resolve(root, ".command-policy-browser-"));
  const artifactDir = resolve(root, "../../../docs/reports/agent-access-profiles");
  mkdirSync(artifactDir, { recursive: true });
  writeFileSync(resolve(fixture, "index.html"), '<main id="root"></main><script type="module" src="/entry.tsx"></script>');
  writeFileSync(resolve(fixture, "fixture.css"), `@import "${resolve(root, "src/styles/app.css")}";\n@source "${resolve(root, "src")}";`);
  writeFileSync(resolve(fixture, "entry.tsx"), `
import React from "react";
import { createRoot } from "react-dom/client";
import { CommandPolicyDetails } from "@/features/settings/app/CommandPolicyDetails";
import "./fixture.css";
const policy = { routine: "automatic", destructive: "ask", catastrophic: "block", private: "block", outside_workspace: "block", review_write: "block", rules: [
  { id: "routine", behavior: "automatic", examples: ["project edits", "git status", "git add", "git commit"], coverage: "Codex native command requests" },
  { id: "destructive", behavior: "ask", examples: ["git reset --hard", "destructive git clean", "force push"], coverage: "Codex native command requests" },
  { id: "catastrophic", behavior: "block", examples: ["filesystem root", "raw-device and format operations"], coverage: "Codex native command requests" },
  { id: "private", behavior: "block", examples: ["configured private folders"], coverage: "resolved private-folder paths" },
  { id: "outside", behavior: "block", examples: ["outside writable roots"], coverage: "resolved command targets" },
  { id: "review", behavior: "block", examples: ["all project writes in Review only"], coverage: "native tool requests" },
] };
const controls = [{ control: "workspace_write", mechanism: "native_setting", coverage: "Codex project workspace tools", evidence: ["fixture"] }, { control: "private_read_deny", mechanism: "native_hook", coverage: "Codex private-path callback", evidence: ["fixture"] }];
const access = { schema: "tusker.agent-access/v1", mode: "work_in_projects", network: true, destructive_actions: "ask", folders: [], private_folders: [] };
createRoot(document.getElementById("root")!).render(<div><CommandPolicyDetails access={access} policy={policy} controls={controls} state="ready" /><CommandPolicyDetails access={{ ...access, mode: "review_only" }} policy={{ ...policy, destructive: "block" }} controls={controls} state="ready" /><CommandPolicyDetails access={access} policy={policy} controls={[...controls, { control: "private_write_deny", mechanism: "unsupported", coverage: "not exposed by provider", evidence: [] }]} state="unsupported" issues={[{ code: "missing", field: "private_folders", message: "Private-folder denial is not supported.", remedy: "Choose another provider." }]} /></div>);
`);
  const server = await createServer({ root: fixture, configFile: false, plugins: [react(), tailwindcss()], resolve: { alias: { "@": resolve(root, "src") } }, server: { host: "127.0.0.1", port: 0, fs: { allow: [root, fixture] } } });
  let browser: Awaited<ReturnType<typeof playwright.chromium.launch>> | undefined;
  try {
    await server.listen();
    const address = server.httpServer?.address();
    if (!address || typeof address === "string") throw new Error("Vite did not expose a TCP address");
    browser = await playwright.chromium.launch({ channel: "chrome", headless: true });
    const page = await browser.newPage({ viewport: { width: 820, height: 900 } });
    await page.goto(`http://127.0.0.1:${address.port}/`);
    expect(await page.getByText("Automatic", { exact: true }).count()).toBeGreaterThanOrEqual(3);
    expect(await page.getByText("Ask each time", { exact: true }).count()).toBeGreaterThanOrEqual(3);
    expect(await page.getByText("Block", { exact: true }).count()).toBeGreaterThanOrEqual(3);
    expect(await page.getByText("Codex project workspace tools", { exact: true }).count()).toBe(3);
    expect(await page.getByText("Review only blocks project writes.", { exact: false }).count()).toBe(1);
    expect(await page.getByRole("alert").count()).toBe(1);
    expect(await page.getByText("Required access restriction is unavailable for this provider.", { exact: true }).count()).toBe(1);
    await page.screenshot({ path: resolve(artifactDir, "command-policy-details.png"), fullPage: true });
  } finally {
    await browser?.close();
    await server.close();
    rmSync(fixture, { recursive: true, force: true });
  }
}, 30_000);
