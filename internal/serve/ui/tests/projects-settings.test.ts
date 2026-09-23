import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { parseExecutionConcurrency } from "@/features/product/OperationsScreens";

const source = (path: string) => readFileSync(path, "utf8");

test("project registration is available in the sidebar and defaults automation off", () => {
  const sidebar = source("src/components/Sidebar.tsx");
  const strip = source("src/features/workbench/navigation/ProjectStrip.tsx");
  const api = source("src/lib/api.ts");

  expect(strip).toContain("Add project");
  expect(sidebar).toContain("data-add-project-form");
  expect(sidebar).toContain("Registers only. Daemon automation stays off.");
  expect(sidebar).toContain("register.mutateAsync");
  expect(strip).toContain("useProjectRefresh");
  expect(strip).toContain("refresh.mutate()");
  expect(strip).toContain("Refresh failed — check this project’s source.");
  expect(api).toContain('post("/projects", body)');
});

test("project registration offers native folder browsing without pretending browsers reveal paths", () => {
  const sidebar = source("src/components/Sidebar.tsx");
  const panel = source("src/features/panel/Panel.tsx");

  expect(sidebar).toContain('aria-label="Browse repository folder"');
  expect(sidebar).toContain('aria-label="Browse vault folder"');
  expect(sidebar).toContain("const path = await pickFolder()");
  expect(sidebar).toContain("if (path) setValue(path);");
  expect(sidebar).toContain("enter the absolute path manually");
  expect(panel).toContain("pickFolder?: () => Promise<string | undefined>");
});

test("project settings own the explicit daemon automation choice", () => {
  const settings = source("src/features/product/OperationsScreens.tsx");
  const api = source("src/lib/api.ts");
  const controls = source("src/components/ui/controls.tsx");

  expect(settings).toContain("<Toggle");
  expect(settings).toContain("project.automationEnabled");
  expect(settings).toContain("ariaLabel={`Background work");
  expect(settings).toContain("automationPending.current");
  expect(settings).toContain("Run queued work automatically while Tusker is open.");
  expect(settings).toContain("<ActionResultLine pending={automation.isPending}");
  expect(settings).not.toContain("<ActionResultLine pending={automation.isPending} error={automation.error} result={automation.data} />\n          <Button variant=\"primary\"");
  expect(controls).toContain('role="switch"');
  expect(controls).toContain("aria-label={ariaLabel}");
  expect(controls).toContain("aria-checked={checked}");
  expect(controls).toContain("focus-visible:ring-2");
  expect(api).toContain("post(`/projects/${projectId}/automation`, { enabled })");
});

test("production diagnostics project live execution from run APIs, never static runtime fixtures", () => {
  const settings = source("src/features/product/OperationsScreens.tsx");
  expect(settings).toContain("useRuns(projectId)");
  expect(settings).toContain("run.workspacePath");
  expect(settings).toContain("run.workspaceMode");
  expect(settings).not.toMatch(/import\s*\{[^}]*\bworktrees\b[^}]*\}\s*from/s);
  expect(settings).not.toContain("AGX-T-0003");
  expect(settings).not.toContain("CLN-T-0007");
  expect(settings).toContain("No active workspace leases.");
});

test("workspace mode and concurrency persist through project settings API", () => {
  const settings = source("src/features/product/OperationsScreens.tsx");
  const api = source("src/lib/api.ts");
  expect(settings).toContain('aria-label="Workspace mode"');
  expect(settings).toContain('aria-label="Project concurrent tasks"');
  expect(api).toContain("post(`/projects/${projectId}/settings`, body)");
});

test("execution concurrency accepts only blank or positive integers", () => {
  expect(parseExecutionConcurrency("")).toEqual({});
  expect(parseExecutionConcurrency("   ")).toEqual({});
  expect(parseExecutionConcurrency("3")).toEqual({ value: 3 });
  expect(parseExecutionConcurrency("  4  ")).toEqual({ value: 4 });
  for (const invalid of ["1.5", "0", "-2", "lots", "3.0", "0x10", "2e2"]) {
    const checked = parseExecutionConcurrency(invalid);
    expect(checked.value).toBeUndefined();
    expect(checked.error).toContain("positive whole number");
  }
});

test("execution settings form rejects invalid concurrency visibly without submission", async () => {
  // A real input-and-submit interaction against the shipped SettingsBasic
  // form. jsdom stands in for the browser because this sandbox denies the
  // TCP bind a dev-server browser test would need; the input events, React
  // state, validation message, and mutation payload are all exercised.
  const { JSDOM } = await import(
    "/Users/sarav/.bun/install/global/node_modules/jsdom/lib/api.js"
  );
  const dom = new JSDOM(
    '<!doctype html><html><body><main id="root"></main></body></html>',
    { url: "http://localhost/", pretendToBeVisual: true },
  );
  const win = dom.window as unknown as Record<string, any>;
  const saved = new Map<string, unknown>();
  for (const key of [
    "window",
    "document",
    "navigator",
    "HTMLElement",
    "HTMLInputElement",
    "HTMLSelectElement",
    "HTMLButtonElement",
    "Event",
    "CustomEvent",
    "Node",
    "Element",
    "DocumentFragment",
    "MutationObserver",
    "localStorage",
  ]) {
    saved.set(key, (globalThis as Record<string, unknown>)[key]);
    (globalThis as Record<string, unknown>)[key] = win[key];
  }
  for (const key of [
    "getComputedStyle",
    "requestAnimationFrame",
    "cancelAnimationFrame",
  ]) {
    saved.set(key, (globalThis as Record<string, unknown>)[key]);
    (globalThis as Record<string, unknown>)[key] =
      typeof win[key] === "function" ? win[key].bind(win) : win[key];
  }
  saved.set("IS_REACT_ACT_ENVIRONMENT", (globalThis as Record<string, unknown>).IS_REACT_ACT_ENVIRONMENT);
  (globalThis as Record<string, unknown>).IS_REACT_ACT_ENVIRONMENT = true;
  const realFetch = (globalThis as Record<string, unknown>).fetch;
  (globalThis as Record<string, unknown>).fetch = async () =>
    new Response(null, { status: 404 });
  try {
    const React = await import("react");
    const { act } = React;
    const { createRoot } = await import("react-dom/client");
    const { QueryClient, QueryClientProvider } = await import("@tanstack/react-query");
    const { SettingsBasic } = await import(
      "@/features/product/OperationsScreens"
    );
    const writes: Array<Record<string, unknown>> = [];
    const settings = {
      mutate: (body: Record<string, unknown>) => {
        writes.push(body);
      },
      isPending: false,
      error: null,
      data: null,
    };
    const automation = {
      mutate: () => {},
      isPending: false,
      error: null,
      data: null,
    };
    const project = {
      id: "proj-1",
      logicalId: "proj-1",
      health: "ok",
      automationEnabled: false,
      automationSource: "Project",
      workspaceMode: "shared",
      workspaceSource: "Project",
      maxActiveRunsPerProject: 2,
      concurrencySource: "Project",
    };
    let root: { unmount: () => void } | undefined;
    await act(async () => {
      root = createRoot(win.document.getElementById("root")!);
      // c5a5da52 added the automation scope query to SettingsBasic.
      root.render(React.createElement(QueryClientProvider, { client: new QueryClient() },
        React.createElement(SettingsBasic, {
          project,
          projectIds: ["proj-1"],
          operations: undefined,
          automation,
          settings,
          onOpenAdvanced: () => {},
        }),
      ));
    });
    const doc = win.document;
    const input = doc.querySelector(
      'input[aria-label="Project concurrent tasks"]',
    ) as HTMLInputElement;
    expect(input.value).toBe("2");
    const save = Array.from(doc.querySelectorAll("button")).find(
      (button) => button.textContent === "Save execution settings",
    ) as HTMLButtonElement;
    const setInput = async (value: string) => {
      await act(async () => {
        const setter = Object.getOwnPropertyDescriptor(
          win.HTMLInputElement.prototype,
          "value",
        )!.set!;
        setter.call(input, value);
        input.dispatchEvent(new win.Event("input", { bubbles: true }));
      });
    };
    const clickSave = async () => {
      await act(async () => {
        save.dispatchEvent(new win.MouseEvent("click", { bubbles: true }));
      });
    };
    const alertText = () =>
      doc.querySelector('[role="alert"]')?.textContent ?? "";

    // Fractional input is rejected visibly and never submitted.
    await setInput("1.5");
    await clickSave();
    expect(alertText()).toContain("positive whole number");
    expect(writes).toEqual([]);

    // Zero is rejected the same way, with still no submission.
    await setInput("0");
    await clickSave();
    expect(alertText()).toContain("positive whole number");
    expect(writes).toEqual([]);

    // Blank keeps the explicit unset semantics: only the workspace mode is sent.
    await setInput("");
    await clickSave();
    expect(writes).toEqual([{ workspaceMode: "shared" }]);

    // A valid positive integer saves both fields together.
    const mode = doc.querySelector(
      'select[aria-label="Workspace mode"]',
    ) as HTMLSelectElement;
    await act(async () => {
      const setter = Object.getOwnPropertyDescriptor(
        win.HTMLSelectElement.prototype,
        "value",
      )!.set!;
      setter.call(mode, "worktree");
      mode.dispatchEvent(new win.Event("change", { bubbles: true }));
    });
    await setInput("3");
    await clickSave();
    expect(writes).toEqual([
      { workspaceMode: "shared" },
      { workspaceMode: "worktree", maxActiveRunsPerProject: 3 },
    ]);
    expect(doc.querySelector('[role="alert"]')).toBeNull();
    await act(async () => {
      root!.unmount();
    });
  } finally {
    (globalThis as Record<string, unknown>).fetch = realFetch;
    for (const [key, value] of saved) {
      (globalThis as Record<string, unknown>)[key] = value;
    }
    dom.window.close();
  }
}, 60_000);

test("advanced settings expose bounded registration repair without reset controls", () => {
  const settings = source("src/features/product/OperationsScreens.tsx");
  const repair = source("src/features/product/ProjectRegistrationRepair.tsx");
  const api = source("src/lib/api.ts");
  const queries = source("src/lib/queries.ts");
  const sidebar = source("src/components/Sidebar.tsx");

  expect(settings).toContain('project.health === "error"');
  expect(settings).toContain("Open registration repair");
  expect(settings).toContain("onOpenAdvanced={() => setTab(\"advanced\")}");
  expect(settings).toContain("<ProjectRegistrationRepair project={project} needsAttention={needsRegistrationRepair} />");
  expect(repair).toContain("Rebinding requires background work to be off.");
  expect(repair).toContain("project.automationEnabled || rebind.isPending");
  expect(repair).toContain('aria-label="Repair repository path"');
  expect(repair).toContain('aria-label="Repair vault path"');
  expect(repair).toContain('aria-label="Browse repository folder"');
  expect(repair).toContain('aria-label="Browse vault folder"');
  expect(repair).toContain('replace(/\\/+$/, "")');
  expect(repair).toContain("setVaultRoot(repositoryVault(path))");
  expect(repair).toContain("Use repository/.tusker");
  expect(repair).toContain('type="checkbox" checked={allowDirty}');
  expect(repair).toContain('typeToConfirm: "ALLOW DIRTY"');
  expect(repair).toContain('title: "Rebind project registration?"');
  expect(repair).toContain("Check repair");
  expect(repair).toContain("Apply repair");
  expect(repair).toContain('dryRun: true');
  expect(repair).toContain("setPreviewSelection(null)");
  expect(repair).toContain("retained_queued_count");
  expect(settings).not.toMatch(/<Button[^>]*(Reset|Retire)/i);
  expect(api).toContain("/rebind");
  expect(api).toContain("allowDirty?: boolean");
  expect(api).toContain("confirm?: string");
  expect(api).toContain("dryRun?: boolean");
  expect(queries).toContain("useProjectRebind");
  expect(queries).toContain("query.queryKey.some((part) => part === projectId)");
  expect(sidebar).not.toContain("Repair in Settings");
  expect(sidebar).not.toContain("Troubleshooting in Settings");
});
