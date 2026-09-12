import { expect, test } from "bun:test";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { createServer } from "vite";

test("existing Full access edits through the main Access section", async () => {
  const playwright =
    await import("/Users/sarav/.bun/install/global/node_modules/playwright/index.mjs").catch(
      () => undefined,
    );
  if (!playwright) return;
  const root = resolve(import.meta.dir, "..");
  const fixture = mkdtempSync(resolve(root, ".access-journey-browser-"));
  const legacy = {
    display_name: "Legacy Full access",
    harness: "codex_exec",
    model: "gpt-5.6-luna",
    effort: "medium",
    permission_preset: "danger-full-access",
    eligible_tiers: ["light", "standard"],
  };
  const unavailable = {
    display_name: "Claude private route",
    harness: "claude-code",
    model: "claude-opus",
    effort: "high",
    access: {
      schema: "tusker.agent-access/v1",
      mode: "work_in_projects",
      network: true,
      destructive_actions: "ask",
      folders: [],
      private_folders: ["/tmp/private"],
    },
    eligible_tiers: [],
  };
  const initialReport = {
    schema: "tusker.model-levels/v1",
    revision: "r1",
    profiles: { legacy: legacy, unavailable: unavailable },
    profile_states: {
      legacy: "configured_unverified",
      unavailable: "unavailable",
    },
    profile_references: { legacy: ["model_levels.standard.execute"] },
    reference_check_complete: true,
    private_folders: [],
    levels: ["light", "standard", "demanding"].map((level) => ({
      level,
      execute: {
        profiles: level === "light" || level === "standard" ? ["legacy"] : [],
        source: "global",
        overridden: false,
      },
      review: { profiles: [], source: "global", overridden: false },
    })),
  };
  const catalog = {
    schema: "tusker.runner-catalog/v1",
    observed_at: "2026-09-11T00:00:00Z",
    harnesses: [
      {
        harness: "codex_exec",
        display_name: "Codex",
        group: "supported",
        transports: ["cli"],
        source: "fixture",
        available: true,
        discovery_state: "available",
        discovery_source: "fixture:model/list",
        manual_entry: true,
        models: [
          {
            model: "gpt-5.6-luna",
            display_name: "Luna",
            efforts: ["medium"],
            default: true,
            default_known: true,
            default_effort: "medium",
            visibility: "visible",
            hidden: false,
          },
        ],
        options: [
          {
            id: "permission_preset",
            label: "Access",
            kind: "enum",
            values: ["workspace-write-offline", "workspace-write-network"],
          },
        ],
        access_controls: [
          {
            control: "workspace_write",
            mechanism: "native_setting",
            coverage: "fixture workspace",
            evidence: ["fixture"],
          },
          {
            control: "network",
            mechanism: "native_setting",
            coverage: "fixture network",
            evidence: ["fixture"],
          },
          {
            control: "destructive_approval",
            mechanism: "native_setting",
            coverage: "fixture approvals",
            evidence: ["fixture"],
          },
          {
            control: "private_read_deny",
            mechanism: "native_hook",
            coverage: "fixture private folders",
            evidence: ["fixture"],
          },
          {
            control: "private_write_deny",
            mechanism: "native_hook",
            coverage: "fixture private folders",
            evidence: ["fixture"],
          },
        ],
      },
      {
        harness: "claude-code",
        display_name: "Claude Code",
        group: "future",
        transports: ["cli"],
        source: "fixture",
        available: false,
        discovery_state: "unsupported",
        discovery_source: "fixture:unsupported",
        manual_entry: true,
        models: [
          {
            model: "claude-opus",
            display_name: "Claude Opus",
            efforts: ["high"],
            default: true,
            default_known: true,
            default_effort: "high",
            visibility: "manual",
            hidden: false,
          },
        ],
        options: [
          {
            id: "permission_preset",
            label: "Access",
            kind: "enum",
            values: ["read-only"],
          },
        ],
      },
    ],
  };
  writeFileSync(
    resolve(fixture, "index.html"),
    '<main id="root"></main><script type="module" src="/entry.tsx"></script>',
  );
  writeFileSync(
    resolve(fixture, "fixture.css"),
    `@import "${resolve(root, "src/styles/app.css")}";\n@source "${resolve(root, "src")}";`,
  );
  writeFileSync(
    resolve(fixture, "entry.tsx"),
    `
import React from "react";
import { createRoot } from "react-dom/client";
import { AgentsSection } from "@/features/settings/app/AgentsSection";
import "./fixture.css";
let report = ${JSON.stringify(initialReport)};
const catalog = ${JSON.stringify(catalog)};
(window as any).__profileWrites = [];
window.fetch = async (url, init = {}) => {
  const path = String(url);
  if (path.includes("/capability")) return new Response(JSON.stringify({ capability: "fixture", operatorActor: "human:fixture" }));
  if (path.includes("/models/catalog")) return new Response(JSON.stringify(catalog));
  if (path.includes("/projects")) return new Response(JSON.stringify([]));
  if (path.includes("/runner/conformance")) return new Response(JSON.stringify({ schema: "tusker.runner-conformance/v1", ready: false, live: false, cases: [], access: { requested: {}, effective: {}, controls: [], issues: [], state: "ready", fingerprint: "fixture" } }));
  if (path.includes("/models") && (!init.method || init.method === "GET")) return new Response(JSON.stringify(report));
  if (path.includes("/models") && init.method === "POST") {
    const body = JSON.parse(String(init.body || "{}"));
    (window as any).__profileWrites.push(body);
    if (body.action === "profile-set") {
      report = { ...report, revision: "r2", profiles: { ...report.profiles, legacy: { display_name: body.displayName, harness: body.harness, model: body.model, effort: body.effort, access: body.access, eligible_tiers: body.eligibleTiers } }, profile_states: { ...report.profile_states, legacy: "configured_unverified" } };
    }
    return new Response(JSON.stringify(report));
  }
  return new Response(JSON.stringify({}), { status: 404 });
};
createRoot(document.getElementById("root")).render(<AgentsSection />);
`,
  );
  const server = await createServer({
    root: fixture,
    configFile: false,
    plugins: [react(), tailwindcss()],
    resolve: { alias: { "@": resolve(root, "src") } },
    server: { host: "127.0.0.1", port: 43133, fs: { allow: [root, fixture] } },
  });
  let browser:
    Awaited<ReturnType<typeof playwright.chromium.launch>> | undefined;
  try {
    await server.listen();
    const address = server.httpServer?.address();
    if (!address || typeof address === "string")
      throw new Error("Vite did not expose a TCP address");
    browser = await playwright.chromium.launch({
      channel: "chrome",
      headless: true,
    });
    const page = await browser.newPage({
      viewport: { width: 1280, height: 900 },
    });
    await page.goto(`http://127.0.0.1:${address.port}/`);

    const legacyCard = page
      .locator("article")
      .filter({ hasText: "Legacy Full access" });
    await legacyCard
      .getByRole("heading", { name: "Legacy Full access" })
      .click();
    const editor = page.getByRole("form", { name: "Edit profile" });
    await editor.waitFor();
    expect(await editor.getByText("Access", { exact: true }).count()).toBe(1);
    expect(await editor.getByText(/Automatic work:/).count()).toBe(1);
    expect(
      await editor.getByLabel("Access mode", { exact: true }).inputValue(),
    ).toBe("full_access");
    expect(await editor.getByLabel("Access mode").isEnabled()).toBe(true);
    await page.getByRole("button", { name: "Cancel", exact: true }).click();
    expect(
      await page.evaluate(() => (window as any).__profileWrites.length),
    ).toBe(0);

    await legacyCard
      .getByRole("heading", { name: "Legacy Full access" })
      .click();
    const reopened = page.getByRole("form", { name: "Edit profile" });
    await reopened.getByLabel("Access mode").selectOption("work_in_projects");
    expect(await page.getByLabel("Model", { exact: true }).inputValue()).toBe(
      "gpt-5.6-luna",
    );
    await reopened.getByText("Advanced", { exact: true }).click();
    expect(
      await page.getByLabel("Reasoning", { exact: true }).inputValue(),
    ).toBe("medium");
    expect(
      await page.getByLabel("Access mode", { exact: true }).inputValue(),
    ).toBe("work_in_projects");
    const tiers = page
      .getByRole("form", { name: "Edit profile" })
      .locator('input[type="checkbox"]');
    expect(
      await tiers.evaluateAll(
        (inputs) =>
          inputs.filter((input) => (input as HTMLInputElement).checked).length,
      ),
    ).toBe(2);

    await reopened
      .getByLabel("Profile private folder path")
      .fill("/tmp/private");
    await reopened
      .getByRole("button", { name: "Add folder", exact: true })
      .click();
    await reopened
      .getByRole("button", { name: "Save profile", exact: true })
      .click();
    await page.waitForFunction(
      () => (window as any).__profileWrites.length === 1,
    );
    await page
      .getByText("Legacy Full access", { exact: true })
      .first()
      .waitFor();
    const write = await page.evaluate(() => (window as any).__profileWrites[0]);
    expect(write).toMatchObject({
      action: "profile-set",
      name: "legacy",
      model: "gpt-5.6-luna",
      effort: "medium",
      eligibleTiers: ["light", "standard"],
      access: {
        schema: "tusker.agent-access/v1",
        mode: "work_in_projects",
        network: true,
        destructive_actions: "ask",
        folders: [],
        private_folders: ["/tmp/private"],
      },
    });
    expect(write.preset).toBeUndefined();

    expect(
      await page.getByText(/Work in projects/, { exact: false }).count(),
    ).toBeGreaterThan(0);
    expect(
      await page.getByText("Unavailable", { exact: true }).count(),
    ).toBeGreaterThan(0);
  } finally {
    await browser?.close();
    await server.close();
    rmSync(fixture, { recursive: true, force: true });
  }
}, 30_000);
