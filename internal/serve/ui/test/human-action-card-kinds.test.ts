import { expect, test } from "bun:test";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";
import react from "@vitejs/plugin-react";
import { createServer } from "vite";

test("a mounted human action card switches gate, question, and permission without hook errors", async () => {
  const playwright = await import("/Users/sarav/.bun/install/global/node_modules/playwright/index.mjs").catch(() => undefined);
  if (!playwright) return;
  const root = resolve(import.meta.dir, "..");
  const fixture = mkdtempSync(resolve(root, ".human-kinds-"));
  writeFileSync(resolve(fixture, "index.html"), '<main id="root"></main><script type="module" src="/entry.tsx"></script>');
  writeFileSync(resolve(fixture, "entry.tsx"), `
import React from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ConfirmProvider } from "@/components/ui/action-feedback";
import { HumanActionCard } from "@/features/human-action/HumanActionCard";
const base = { kind: "decision", rawKind: "decision", gateId: "G-1", title: "Gate", action: "Approve", whyAgentCannot: "Human", completionCondition: "Recorded", materialRevision: "rev", blockedTaskIds: ["T-1"], covers: [], acceptance: [] };
const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
(window as any).__errors = [];
window.addEventListener("error", (event) => (window as any).__errors.push(event.message));
const root = createRoot(document.getElementById("root")!);
(window as any).__show = (kind: string) => root.render(<QueryClientProvider client={client}><ConfirmProvider><HumanActionCard action={{ ...base, kind, title: kind, messageId: "m-1", requestId: "r-1" }} taskId="T-1" taskTitle="Task" projectId="p" approvals={[]} /></ConfirmProvider></QueryClientProvider>);
(window as any).__show("decision");
`);
  const server = await createServer({ root: fixture, configFile: false, plugins: [react()], resolve: { alias: { "@": resolve(root, "src") } }, server: { host: "127.0.0.1", port: 43134, strictPort: true, fs: { allow: [root, fixture] } } });
  let browser: Awaited<ReturnType<typeof playwright.chromium.launch>> | undefined;
  try {
    await server.listen();
    const address = server.httpServer?.address();
    if (!address || typeof address === "string") throw new Error("Vite address unavailable");
    browser = await playwright.chromium.launch({ channel: "chrome", headless: true });
    const page = await browser.newPage();
    await page.goto(`http://127.0.0.1:${address.port}/`);
    for (const kind of ["decision", "question", "permission", "decision"]) {
      await page.evaluate((next) => (window as any).__show(next), kind);
      await page.getByRole("heading", { name: kind === "decision" ? "Your action" : kind }).waitFor();
    }
    expect(await page.evaluate(() => (window as any).__errors)).toEqual([]);
  } finally {
    await browser?.close();
    await server.close();
    rmSync(fixture, { recursive: true, force: true });
  }
}, 20_000);
