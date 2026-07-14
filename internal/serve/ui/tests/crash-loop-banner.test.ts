import { describe, expect, test } from "bun:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { CrashLoopCircuitBanner } from "../src/components/CrashLoopCircuitBanner";

describe("crash-loop circuit banner", () => {
  test.each([
    [false, "browser"],
    [true, "embedded tray"],
  ])("renders loud recovery instructions in the %s shell", (embedded) => {
    const html = renderToStaticMarkup(createElement(CrashLoopCircuitBanner, {
      embedded,
      circuit: { open: true, summary: "six abnormal starts" },
    }));
    expect(html).toContain("Daemon crash loop");
    expect(html).toContain("six abnormal starts");
    expect(html).toContain("tusker daemon resume");
  });

  test("stays absent while the circuit is closed", () => {
    const html = renderToStaticMarkup(createElement(CrashLoopCircuitBanner, {
      embedded: true,
      circuit: { open: false },
    }));
    expect(html).toBe("");
  });
});
