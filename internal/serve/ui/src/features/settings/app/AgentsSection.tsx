import { useState } from "react";
import { cn } from "@/lib/cn";
import { ProfilesSection } from "./ProfilesSection";
import { TiersSection } from "./TiersSection";

export function AgentsSection() {
  const [tab, setTab] = useState<"profiles" | "tiers">("profiles");
  return <div><div className="mb-5 inline-flex rounded-lg border border-line bg-panel p-0.5"><button type="button" aria-current={tab === "profiles" ? "page" : undefined} onClick={() => setTab("profiles")} className={cn("rounded-md px-3 py-1.5 text-[12px] transition-colors", tab === "profiles" ? "bg-raised font-semibold text-ink shadow-2xs" : "text-muted hover:text-ink")}>Profiles</button><button type="button" aria-current={tab === "tiers" ? "page" : undefined} onClick={() => setTab("tiers")} className={cn("rounded-md px-3 py-1.5 text-[12px] transition-colors", tab === "tiers" ? "bg-raised font-semibold text-ink shadow-2xs" : "text-muted hover:text-ink")}>Tiers</button></div>{tab === "profiles" ? <ProfilesSection scope="global" /> : <TiersSection scope="global" />}</div>;
}
