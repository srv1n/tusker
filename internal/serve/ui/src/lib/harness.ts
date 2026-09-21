export function harnessLabel(harness?: string): string {
  return harness === "codex_exec" ? "Codex" : harness || "";
}
