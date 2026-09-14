import { expect, test } from "bun:test";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { createServer } from "vite";
import { availableProfileId } from "@/features/settings/app/ProfilesSection";

test("new profiles get distinct stable IDs for the same model", () => {
  const profiles = {
    "codex_exec-gpt-5-6-sol": {},
    "codex_exec-gpt-5-6-sol-2": {},
  };
  expect(availableProfileId("codex_exec", "gpt-5.6-sol", profiles)).toBe(
    "codex_exec-gpt-5-6-sol-3",
  );
});

test("Profiles add and edit flow keeps access in one understandable section", async () => {
  const playwright =
    await import("/Users/sarav/.bun/install/global/node_modules/playwright/index.mjs").catch(
      () => undefined,
    );
  if (!playwright) return;
  const root = resolve(import.meta.dir, "..");
  const fixture = mkdtempSync(resolve(root, ".profiles-browser-"));
  const report = {
    schema: "tusker.model-levels/v1",
    revision: "r1",
    profiles: {
      "codex_exec-gpt-5-6-luna": {
        display_name: "Existing Luna profile",
        harness: "codex_exec",
        model: "gpt-5.6-luna",
        effort: "low",
        permission_preset: "workspace-write-offline",
        eligible_tiers: ["light"],
      },
      seeded: {
        display_name: "Seeded reviewer",
        harness: "claude-code",
        model: "claude-opus",
        effort: "high",
        permission_preset: "read-only",
        eligible_tiers: ["light"],
      },
      legacyFull: {
        display_name: "Legacy full",
        harness: "codex_exec",
        model: "gpt-5.6-luna",
        effort: "medium",
        permission_preset: "danger-full-access",
        eligible_tiers: ["standard", "demanding"],
      },
    },
    profile_states: {
      seeded: "unavailable",
      legacyFull: "configured_unverified",
    },
    profile_references: {
      seeded: ["model_levels.light.review"],
      legacyFull: [
        "model_levels.standard.execute",
        "model_levels.demanding.review",
      ],
    },
    reference_check_complete: false,
    levels: ["light", "standard", "demanding"].map((level) => ({
      level,
      execute: { profiles: [], source: "global", overridden: false },
      review: { profiles: [], source: "global", overridden: false },
    })),
  };
  const catalog = {
    schema: "tusker.runner-catalog/v1",
    observed_at: "2026-09-09T00:00:00Z",
    harnesses: [
      {
        harness: "codex_exec",
        display_name: "Codex",
        group: "supported",
        transports: ["cli"],
        source: "live",
        available: true,
        discovery_state: "available",
        discovery_source: "app_server:model/list",
        last_checked: "2026-09-09T00:00:00Z",
        manual_entry: true,
        models: [
          {
            model: "gpt-5.6-luna",
            display_name: "Luna",
            efforts: ["low", "medium", "high"],
            default: true,
            default_known: true,
            default_effort: "medium",
            visibility: "visible",
            hidden: false,
          },
          {
            model: "gpt-5.6-sol",
            display_name: "Sol",
            efforts: ["low"],
            default: false,
            default_known: true,
            default_effort: "low",
            visibility: "visible",
            hidden: false,
          },
        ],
        options: [
          {
            id: "permission_preset",
            label: "Access",
            kind: "enum",
            values: ["read-only", "workspace-write-offline"],
            default: "workspace-write-offline",
          },
        ],
      },
      {
        harness: "muse",
        display_name: "Muse",
        group: "supported",
        transports: ["cli"],
        source: "live",
        available: true,
        discovery_state: "available",
        discovery_source:
          "muse_server:model/list + muse_profile:provider/models",
        manual_entry: true,
        models: [
          {
            model: "muse-spark-1.3",
            display_name: "Muse Spark 1.3",
            efforts: [
              "none",
              "minimal",
              "low",
              "medium",
              "high",
              "xhigh",
              "max",
              "ultra",
            ],
            default: true,
            default_known: true,
            default_effort: "high",
            visibility: "visible",
            hidden: false,
          },
          {
            model: "muse-spark-1.3-contributor",
            display_name: "Muse Spark 1.3 Contributor",
            efforts: [
              "none",
              "minimal",
              "low",
              "medium",
              "high",
              "xhigh",
              "max",
              "ultra",
            ],
            default: false,
            default_known: true,
            default_effort: "high",
            visibility: "visible",
            hidden: false,
          },
          {
            model: "muse-spark-1.2",
            display_name: "Muse Spark 1.2",
            efforts: [
              "none",
              "minimal",
              "low",
              "medium",
              "high",
              "xhigh",
              "max",
              "ultra",
            ],
            default: false,
            default_known: true,
            default_effort: "high",
            visibility: "visible",
            hidden: false,
          },
          {
            model: "muse-spark-1.2-contributor",
            display_name: "Muse Spark 1.2 Contributor",
            efforts: [
              "none",
              "minimal",
              "low",
              "medium",
              "high",
              "xhigh",
              "max",
              "ultra",
            ],
            default: false,
            default_known: true,
            default_effort: "high",
            visibility: "visible",
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
  // The fixture root is temporary, so Tailwind cannot discover the imported
  // feature tree unless we explicitly include the real source directory.
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
let report = ${JSON.stringify(report)};
const catalog = ${JSON.stringify(catalog)};
(window as any).__profileWrites = 0;
window.fetch = async (url, init = {}) => {
  const path = String(url);
  if (path.includes("/models/catalog")) return new Response(JSON.stringify(catalog));
  if (path.includes("/runner/conformance")) { const body = JSON.parse(String(init.body || "{}")); (window as any).__conformanceWrites = ((window as any).__conformanceWrites || 0) + 1; if (body.model === "gpt-5.6-sol") await new Promise((resolve) => setTimeout(resolve, 200)); (window as any).__lastConformanceRequest = body; return new Response(JSON.stringify({ schema: "tusker.runner-conformance/v1", ready: !body.setup, live: !body.setup, cases: [], ...(body.access ? { access: { requested: body.access, effective: {}, controls: [], issues: [], state: "ready", fingerprint: "fixture" } } : {}) })); }
  if (path.includes("/models") && (!init.method || init.method === "GET")) return new Response(JSON.stringify(report));
  if (path.includes("/models") && (window as any).__failNextProfileWrite) { (window as any).__failNextProfileWrite = false; return new Response(JSON.stringify({ error: "revision conflict" }), { status: 409 }); }
  if (path.includes("/models")) { (window as any).__profileWrites++; const body = JSON.parse(init.body); (window as any).__lastProfileWrite = body; if (body.action === "profile-remove") { const profiles = { ...report.profiles }; delete profiles[body.name]; const profile_states = { ...report.profile_states }; delete profile_states[body.name]; report = { ...report, revision: "r2", profiles, profile_states }; } else { report = { ...report, revision: "r2", profiles: { ...report.profiles, [body.name]: { display_name: body.displayName, harness: body.harness, model: body.model, effort: body.effort, ...(body.access ? { access: body.access } : { permission_preset: body.preset }), eligible_tiers: body.eligibleTiers } }, profile_states: { ...report.profile_states, [body.name]: "configured_unverified" } }; } return new Response(JSON.stringify(report)); }
  if (path.includes("/capability")) return new Response(JSON.stringify({ capability: "fixture" }));
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
    server: { host: "127.0.0.1", port: 43129, fs: { allow: [root, fixture] } },
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
      viewport: { width: 390, height: 844 },
    });
    await page.goto(`http://127.0.0.1:${address.port}/`);
    const legacy = page.locator("article").filter({ hasText: "Legacy full" });
    expect(await legacy.innerText()).toContain("Full access");
    await legacy.getByRole("heading", { name: "Legacy full" }).click();
    const legacyForm = page.getByRole("form", { name: "Edit profile" });
    await legacyForm.waitFor();
    expect(await legacyForm.getByText("Access", { exact: true }).count()).toBe(
      1,
    );
    expect(await legacyForm.getByText(/Automatic work:/).count()).toBe(1);
    expect(
      await legacyForm.getByLabel("Access mode", { exact: true }).inputValue(),
    ).toBe("full_access");
    expect(await legacyForm.getByLabel("Access mode").isEnabled()).toBe(true);
    await legacyForm.screenshot({
      path: resolve(
        root,
        "../../../docs/reports/agent-access-profiles/legacy-full-existing.png",
      ),
      animations: "disabled",
    });
    await legacyForm
      .getByLabel("Access mode")
      .selectOption("work_in_projects");
    expect(
      await legacyForm.getByLabel("Access mode", { exact: true }).inputValue(),
    ).toBe("work_in_projects");
    expect(await page.getByLabel("Model", { exact: true }).inputValue()).toBe(
      "gpt-5.6-luna",
    );
    await legacyForm
      .getByRole("button", { name: "Rediscover models" })
      .waitFor();
    await page.getByRole("button", { name: "Cancel", exact: true }).click();
    await legacy.getByRole("heading", { name: "Legacy full" }).click();
    await page.getByRole("form", { name: "Edit profile" }).waitFor();
    expect(
      await page.getByLabel("Access mode", { exact: true }).inputValue(),
    ).toBe("full_access");
    await page
      .getByLabel("Access mode")
      .selectOption("work_in_projects");
    await page.waitForTimeout(100);
    await page.getByRole("form", { name: "Edit profile" }).screenshot({
      path: resolve(
        root,
        "../../../docs/reports/agent-access-profiles/legacy-full-project-access.png",
      ),
      animations: "disabled",
    });
    await page.evaluate(() => document.querySelector("form")?.requestSubmit());
    await page.getByText("Legacy full", { exact: true }).waitFor();
    expect(
      await page.evaluate(() => (window as any).__lastProfileWrite),
    ).toMatchObject({
      name: "legacyFull",
      model: "gpt-5.6-luna",
      effort: "medium",
      eligibleTiers: ["standard", "demanding"],
      access: {
        schema: "tusker.agent-access/v1",
        mode: "work_in_projects",
        network: true,
        destructive_actions: "ask",
        folders: [],
        private_folders: [],
      },
    });
    expect(
      await page
        .locator("article")
        .filter({ hasText: "Legacy full" })
        .innerText(),
    ).toContain("Work in projects");
    await page.evaluate(() => {
      (window as any).__profileWrites = 0;
    });
    const seeded = page
      .locator("article")
      .filter({ hasText: "Seeded reviewer" });
    await seeded.getByRole("heading", { name: "Seeded reviewer" }).click();
    await page.getByRole("form", { name: "Edit profile" }).waitFor();
    await page.getByRole("button", { name: "Cancel", exact: true }).click();
    await seeded.press("Enter");
    await page.getByRole("form", { name: "Edit profile" }).waitFor();
    await page.getByRole("button", { name: "Cancel", exact: true }).click();
    await page.getByRole("button", { name: "Add profile" }).click();
    await page.getByRole("form", { name: "Add profile" }).waitFor();
    expect(await page.getByLabel("Model", { exact: true }).inputValue()).toBe(
      "gpt-5.6-luna",
    );
    await page.getByText("Advanced", { exact: true }).click();
    expect(
      await page.getByLabel("Reasoning", { exact: true }).inputValue(),
    ).toBe("medium");
    expect(
      await page.getByLabel("Access mode", { exact: true }).inputValue(),
    ).toBe("work_in_projects");
    const artifactDir = resolve(
      root,
      "../../../docs/reports/agent-access-profiles/automatic-defaults",
    );
    mkdirSync(artifactDir, { recursive: true });
    await page.screenshot({
      path: resolve(artifactDir, "390-add-profile.png"),
      fullPage: true,
    });
    expect(
      new Set(
        await page
          .getByRole("form", { name: "Add profile" })
          .locator("select:visible")
          .evaluateAll((controls) =>
            controls.map((control) => control.getBoundingClientRect().height),
          ),
      ).size,
    ).toBe(1);
    const addForm = page.getByRole("form", { name: "Add profile" });
    await addForm
      .getByLabel("Profile private folder path")
      .fill("/tmp/private");
    await addForm
      .getByRole("button", { name: "Add folder", exact: true })
      .click();
    await addForm
      .getByRole("switch")
      .evaluate((element) => (element as HTMLElement).click());
    await addForm
      .getByLabel("Destructive actions", { exact: true })
      .selectOption("deny");
    await page.getByRole("button", { name: "Check setup", exact: true }).click();
    await page.getByText("Settings supported", { exact: true }).waitFor();
    expect(await page.evaluate(() => (window as any).__profileWrites)).toBe(0);
    expect(
      await page.evaluate(() => (window as any).__lastConformanceRequest),
    ).toMatchObject({
      draft: true,
      setup: true,
      access: {
        network: false,
        destructive_actions: "deny",
        folders: [],
        private_folders: ["/tmp/private"],
      },
    });
    await page.getByLabel("Model", { exact: true }).selectOption("gpt-5.6-sol");
    expect(
      await page.getByLabel("Reasoning", { exact: true }).inputValue(),
    ).toBe("low");
    await page.getByRole("button", { name: "Run test", exact: true }).click();
    await page
      .getByLabel("Model", { exact: true })
      .selectOption("gpt-5.6-luna");
    await page.waitForTimeout(250);
    expect(await page.getByText("Test passed", { exact: true }).count()).toBe(0);
    expect(
      await page.getByLabel("Reasoning", { exact: true }).inputValue(),
    ).toBe("medium");
    await page.getByLabel("Display name").fill("Codex · Luna");
    await page.getByRole("button", { name: "Check setup", exact: true }).click();
    await page.getByText("Settings supported", { exact: true }).waitFor();
    expect(
      await page.evaluate(() => (window as any).__lastConformanceRequest),
    ).toMatchObject({
      draft: true,
      setup: true,
      model: "gpt-5.6-luna",
      effort: "medium",
      access: {
        network: false,
        destructive_actions: "deny",
        folders: [],
        private_folders: ["/tmp/private"],
      },
    });
    await page.evaluate(() => { (window as any).__failNextProfileWrite = true; });
    await page
      .getByRole("button", { name: "Save profile", exact: true })
      .click();
    await page.getByText("revision conflict", { exact: false }).waitFor();
    expect(await addForm.getByText("/tmp/private", { exact: false }).count()).toBeGreaterThan(0);
    await page.getByRole("button", { name: "Save profile", exact: true }).click();
    await page.getByText("Codex · Luna", { exact: true }).waitFor();
    expect(
      await page.evaluate(() => (window as any).__lastProfileWrite),
    ).toMatchObject({
      name: "codex_exec-gpt-5-6-luna-2",
      access: {
        mode: "work_in_projects",
        network: false,
        destructive_actions: "deny",
        folders: [],
        private_folders: ["/tmp/private"],
      },
    });
    await page.getByRole("button", { name: "Add profile" }).click();
    await page.setViewportSize({ width: 1280, height: 900 });
    await page.screenshot({
      path: resolve(artifactDir, "1280-add-profile.png"),
      fullPage: true,
    });
    await page.setViewportSize({ width: 390, height: 844 });
    await page.evaluate(() => {
      document.documentElement.style.zoom = "2";
    });
    const zoomMetrics = await page.evaluate(() => {
      const form = document.querySelector<HTMLFormElement>("form");
      return {
        scrollWidth: form?.scrollWidth ?? 0,
        clientWidth: form?.clientWidth ?? 0,
        saveWidth:
          form
            ?.querySelector<HTMLButtonElement>('button[type="submit"]')
            ?.getBoundingClientRect().width ?? 0,
        buttons: [
          ...(form?.querySelectorAll<HTMLButtonElement>("button") ?? []),
        ]
          .map((button) => button.getBoundingClientRect().height)
          .filter(Boolean),
      };
    });
    expect(zoomMetrics.scrollWidth).toBeLessThanOrEqual(
      zoomMetrics.clientWidth + 1,
    );
    expect(zoomMetrics.saveWidth).toBeGreaterThan(0);
    expect(Math.min(...zoomMetrics.buttons)).toBeGreaterThanOrEqual(44);
    // Keep this viewport-only: a full-page capture at 2x can be taller than
    // the browser's raster budget while adding no layout evidence.
    await page.screenshot({
      path: resolve(artifactDir, "390-add-profile-200-percent.png"),
      fullPage: false,
    });
    await page.evaluate(() =>
      document
        .querySelector<HTMLButtonElement>('button[type="submit"]')
        ?.scrollIntoView({ block: "center" }),
    );
    await page.screenshot({
      path: resolve(artifactDir, "390-add-profile-200-percent-actions.png"),
      fullPage: false,
    });
    await page.evaluate(() => {
      document.documentElement.style.zoom = "1";
    });
    await page.getByLabel("Model", { exact: true }).focus();
    expect(
      await page.evaluate(() => {
        const active = document.activeElement as HTMLElement | null;
        if (!active) return false;
        const style = getComputedStyle(active);
        return style.outlineStyle !== "none" || style.boxShadow !== "none";
      }),
    ).toBe(true);
    await page.getByLabel("Coding agent").selectOption("muse");
    expect(await page.getByLabel("Model", { exact: true }).inputValue()).toBe(
      "muse-spark-1.3",
    );
    expect(
      await page.getByLabel("Reasoning", { exact: true }).inputValue(),
    ).toBe("high");
    expect(
      await page
        .getByLabel("Model", { exact: true })
        .locator("option")
        .allTextContents(),
    ).toEqual([
      "Muse Spark 1.3",
      "Muse Spark 1.3 Contributor",
      "Muse Spark 1.2",
      "Muse Spark 1.2 Contributor",
    ]);
    await page
      .getByLabel("Model", { exact: true })
      .selectOption("muse-spark-1.2-contributor");
    expect(
      await page
        .getByLabel("Reasoning", { exact: true })
        .locator("option")
        .allTextContents(),
    ).toEqual([
      "none",
      "minimal",
      "low",
      "medium",
      "high",
      "xhigh",
      "max",
      "ultra",
    ]);
    await page
      .getByRole("button", { name: "Save profile", exact: true })
      .click();
    await page
      .getByText("Muse · Muse Spark 1.2 Contributor", { exact: true })
      .waitFor();
    const actions = page.locator("details").last().locator("summary");
    await actions.click();
    const remove = page.getByRole("button", { name: "Remove", exact: true });
    await page.mouse.click(20, 20);
    expect(await remove.isVisible()).toBe(false);
    await actions.click();
    await remove.click();
    await page.waitForFunction(
      () =>
        !document.body.innerText.includes("Muse · Muse Spark 1.2 Contributor"),
    );
    expect(
      await page
        .getByText("Muse · Muse Spark 1.2 Contributor", { exact: true })
        .count(),
    ).toBe(0);
    await seeded.locator("summary").click();
    const seededRemove = seeded.getByRole("button", {
      name: "Remove",
      exact: true,
    });
    expect(await seededRemove.isEnabled()).toBe(true);
  } finally {
    await browser?.close();
    await server.close();
    rmSync(fixture, { recursive: true, force: true });
  }
}, 30_000);
