import { useEffect } from "react";
import { createRoot } from "react-dom/client";
import { ProfilesSection } from "@/features/settings/app/ProfilesSection";
import "@/styles/app.css";

const modelLevels = {
  schema: "tusker.model-levels/v1",
  revision: "sha256:preview",
  profiles: {
    "execute-cheap": { harness: "codex_exec", model: "gpt-5.6-luna", effort: "medium", permission_preset: "workspace-write-offline" },
    default: { harness: "codex_exec", model: "gpt-6-astra", effort: "high", permission_preset: "workspace-write-offline" },
    "review-frontier": { harness: "claude-code", model: "opus", effort: "high", permission_preset: "read-only" },
  },
  profile_states: { "execute-cheap": "configured_unverified", default: "configured_unverified", "review-frontier": "configured_unverified" },
  levels: [
    { level: "light", execute: { profiles: ["execute-cheap", "default"], source: "user-global", overridden: false }, review: { profiles: ["review-frontier"], source: "user-global", overridden: false } },
    { level: "standard", execute: { profiles: ["default"], source: "project-local", overridden: true }, review: { profiles: ["review-frontier"], source: "user-global", overridden: false } },
    { level: "demanding", execute: { profiles: ["default"], source: "built-in", overridden: false }, review: { profiles: ["review-frontier"], source: "built-in", overridden: false } },
  ],
};

globalThis.fetch = async (input) => {
  const path = String(input);
  if (path.includes("/api/models")) return new Response(JSON.stringify(modelLevels), { status: 200, headers: { "content-type": "application/json" } });
  if (path.includes("/api/capability")) return new Response(JSON.stringify({ capability: "preview" }), { status: 200, headers: { "content-type": "application/json" } });
  return new Response(JSON.stringify({ error: "preview route unavailable" }), { status: 404, headers: { "content-type": "application/json" } });
};

function Preview() {
  useEffect(() => {
    document.querySelector("details")?.setAttribute("open", "");
  }, []);
  return (
  <div className="min-h-screen bg-surface px-4 py-6 text-ink sm:px-8">
    <div className="mx-auto max-w-[1120px]">
      <p className="font-mono text-[10px] uppercase tracking-[0.14em] text-faint">Settings · Runner profiles</p>
      <h1 className="mb-6 mt-1 font-serif text-[30px] font-semibold">Runner profiles</h1>
      <ProfilesSection />
    </div>
  </div>
  );
}

createRoot(document.getElementById("root")!).render(<Preview />);
