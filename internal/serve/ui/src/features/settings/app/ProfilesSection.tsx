/*
  Runner profiles tab — a profile is the named bundle the daemon uses to launch
  an agent (harness · model · effort · permission preset · subagent policy).
  Built-ins are editable-by-copy: Duplicate leaves the original intact.
*/

import { Copy, Pencil, Plus } from "lucide-react";
import { DashedButton, HarnessChip } from "./parts";
import { runnerProfiles, type RunnerProfile } from "./mock";

function ProfileFact({ k, v }: { k: string; v: string }) {
  return (
    <div className="flex items-center justify-between">
      <span className="text-faint">{k}</span>
      <span className="text-ink-soft">{v}</span>
    </div>
  );
}

function ProfileCard({ p }: { p: RunnerProfile }) {
  return (
    <div className="rounded-[10px] border border-line px-4 py-[15px]">
      <div className="mb-3 flex items-center gap-[9px]">
        <span className="font-serif text-[16px] font-semibold text-ink">{p.name}</span>
        <HarnessChip harness={p.harness} />
      </div>

      <div className="flex flex-col gap-1.5 font-mono text-[11px]">
        <ProfileFact k="model" v={p.model} />
        <ProfileFact k="effort" v={p.effort} />
        <ProfileFact k="permission" v={p.preset} />
        <ProfileFact k="subagents" v={p.subagents} />
      </div>

      <div className="mt-3 flex items-center justify-between border-t border-line-soft pt-[11px]">
        <span className="font-mono text-[10px] text-fainter">
          {p.builtin ? "built-in · copy to edit" : "custom"}
        </span>
        <span className="flex items-center gap-3">
          {/* TODO(api): clone / edit a profile through the settings API */}
          <button
            type="button"
            className="inline-flex items-center gap-1 text-[11.5px] text-muted transition-colors hover:text-ink"
          >
            <Copy size={12} strokeWidth={2} /> Duplicate
          </button>
          <button
            type="button"
            className="inline-flex items-center gap-1 text-[11.5px] text-muted transition-colors hover:text-ink"
          >
            <Pencil size={12} strokeWidth={2} /> Edit
          </button>
        </span>
      </div>
    </div>
  );
}

export function ProfilesSection() {
  return (
    <div className="animate-rise">
      <p className="mb-4 max-w-[64ch] text-[13px] leading-relaxed text-muted">
        A profile is the bundle the daemon uses to launch an agent. Built-ins are editable by copy —
        duplicating one leaves the original intact.
      </p>

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        {runnerProfiles.map((p) => (
          <ProfileCard key={p.name} p={p} />
        ))}
      </div>

      {/* TODO(api): create a new profile */}
      <DashedButton className="mt-3.5 inline-flex items-center gap-1">
        <Plus size={13} strokeWidth={2} /> New profile
      </DashedButton>
    </div>
  );
}
