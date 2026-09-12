import { expect, test } from "bun:test";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { createServer } from "vite";

test("agent access approvals show immutable states and only settle a live request", async () => {
  const playwright = await import("/Users/sarav/.bun/install/global/node_modules/playwright/index.mjs").catch(() => undefined);
  if (!playwright) return;
  const root = resolve(import.meta.dir, "..");
  const fixture = mkdtempSync(resolve(root, ".approval-browser-"));
  const now = Date.now();
  const approval = (overrides: Record<string, unknown> = {}) => ({
    requestId: "request-live",
    projectId: "fixture-project",
    taskId: "fixture-task",
    attemptId: "attempt-1",
    executionId: "execution-1",
    sessionId: "session-1",
    nativeRequestId: "native-1",
    route: "codex_exec / work_in_projects",
    policyFingerprint: "sha256:policy",
    tool: "shell",
    argsDigest: "sha256:args",
    redactedArguments: { command: "git push", token: "[redacted]" },
    workingDirectory: "/tmp/fixture-worktree",
    targets: ["origin/main"],
    reason: "Force-push changes a remote branch.",
    nativeOptionId: "allow-once-option",
    nativeOptionKind: "allow_once",
    state: "pending",
    stateRevision: 1,
    expiresAt: new Date(now + 120_000).toISOString(),
    liveUntil: new Date(now + 120_000).toISOString(),
    createdAt: new Date(now).toISOString(),
    updatedAt: new Date(now).toISOString(),
    ...overrides,
  });
  const approvals = [
    approval(),
    approval(),
    approval({ requestId: "request-no-callback", nativeOptionKind: "unsupported", nativeOptionId: "", liveUntil: "" }),
    approval({ requestId: "request-denied", state: "denied", decision: "deny", terminalReason: "Operator blocked this request." }),
    approval({ requestId: "request-expired", state: "expired", expiresAt: new Date(now - 1000).toISOString(), liveUntil: new Date(now - 1000).toISOString(), terminalReason: "Native request is no longer live." }),
  ];
  writeFileSync(resolve(fixture, "index.html"), '<main id="root"></main><script type="module" src="/entry.tsx"></script>');
  writeFileSync(resolve(fixture, "entry.tsx"), `
import React from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AgentAccessApprovalList } from "@/features/human-action/HumanActionCard";
import "@/styles/app.css";
const initial = ${JSON.stringify(approvals)};
let current = initial;
(window as any).__approvalPosts = [];
(window as any).__retries = 0;
window.fetch = async (url, init = {}) => {
  const path = String(url);
  if (path.includes("/capability")) return new Response(JSON.stringify({ capability: "fixture", operatorActor: "human:fixture" }));
  if (path.includes("/approvals/") && init.method === "POST") {
    const body = JSON.parse(String(init.body));
    (window as any).__approvalPosts.push(body);
    const item = current.find((candidate: any) => candidate.requestId === body.requestId);
    if (body.requestId === "request-no-callback") return new Response(JSON.stringify({ ok: false, refused: true, reason: "native request is unavailable" }), { status: 409 });
    const settled = { ...item, state: body.decision === "allow_once" ? "allowed" : "denied", decision: body.decision, stateRevision: body.expectedRevision + 1 };
    current = current.map((candidate: any) => candidate.requestId === body.requestId ? settled : candidate);
    return new Response(JSON.stringify({ ok: true, approval: settled }));
  }
  if (path.includes("/approvals")) return new Response(JSON.stringify({ approvals: current }));
  return new Response(JSON.stringify({}), { status: 404 });
};
createRoot(document.getElementById("root")).render(<QueryClientProvider client={new QueryClient()}><AgentAccessApprovalList projectId="fixture-project" taskId="fixture-task" approvals={initial} onRetry={() => (window as any).__retries++} /></QueryClientProvider>);
`);
  const server = await createServer({ root: fixture, configFile: false, plugins: [react(), tailwindcss()], resolve: { alias: { "@": resolve(root, "src") } }, server: { host: "127.0.0.1", port: 43131, fs: { allow: [root, fixture] } } });
  let browser: Awaited<ReturnType<typeof playwright.chromium.launch>> | undefined;
  try {
    await server.listen();
    const address = server.httpServer?.address();
    if (!address || typeof address === "string") throw new Error("Vite did not expose a TCP address");
    browser = await playwright.chromium.launch({ channel: "chrome", headless: true });
    const page = await browser.newPage({ viewport: { width: 390, height: 844 } });
    await page.goto(`http://127.0.0.1:${address.port}/`);
    const cards = page.locator("[data-agent-access-approvals] article");
    await cards.first().waitFor();
    expect(await page.locator('[data-testid="agent-access-approval-request-live"]').count()).toBe(1);
    expect(await page.locator("pre").filter({ hasText: '"token": "[redacted]"' }).count()).toBeGreaterThan(0);
    expect(await page.getByText("secret", { exact: true }).count()).toBe(0);
    expect(await page.getByRole("button", { name: "Allow once", exact: true }).count()).toBe(1);
    expect(await page.getByRole("button", { name: "Block", exact: true }).count()).toBe(1);
    expect(await page.getByText("Denied", { exact: true }).count()).toBe(1);
    expect(await page.getByText("Expired", { exact: true }).count()).toBe(1);
    const liveCard = page.locator('[data-testid="agent-access-approval-request-live"]');
    await liveCard.focus();
    await liveCard.press("Enter");
    expect(await page.evaluate(() => (window as any).__approvalPosts.length)).toBe(0);
    await page.getByRole("button", { name: "Allow once", exact: true }).click();
    await page.waitForFunction(() => document.querySelector('[data-testid="agent-access-approval-request-live"]')?.getAttribute("data-approval-state") === "allowed");
    expect(await liveCard.getAttribute("data-approval-state")).toBe("allowed");
    expect(await page.evaluate(() => (window as any).__approvalPosts[0])).toMatchObject({ requestId: "request-live", expectedRevision: 1, decision: "allow_once" });
    expect(await page.getByRole("button", { name: "Allow once", exact: true }).count()).toBe(0);
    const deadCard = page.locator('[data-testid="agent-access-approval-request-no-callback"]');
    expect(await deadCard.getByRole("button", { name: "Allow once", exact: true }).count()).toBe(0);
    expect(await deadCard.getByRole("button", { name: "Block", exact: true }).count()).toBe(0);
    await deadCard.getByRole("button", { name: "Retry", exact: true }).click();
    expect(await page.evaluate(() => (window as any).__retries)).toBe(1);
    const artifactDir = resolve(root, "../../../docs/reports/agent-access/approval-ui");
    mkdirSync(artifactDir, { recursive: true });
    await page.screenshot({ path: resolve(artifactDir, "390-approval-states.png"), fullPage: true });
  } finally {
    await browser?.close();
    await server.close();
    rmSync(fixture, { recursive: true, force: true });
  }
}, 30_000);
