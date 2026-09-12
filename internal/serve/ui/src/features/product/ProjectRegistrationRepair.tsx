import { useEffect, useState } from "react";
import { Button, TextInput } from "@/components/ui/controls";
import { ActionResultLine, useConfirm } from "@/components/ui/action-feedback";
import { Chip, Mono } from "@/components/ui/primitives";
import { SectionLabel } from "@/components/ui/page";
import { useProjectRebind } from "@/lib/queries";
import { cn } from "@/lib/cn";
import type { ProjectSummary } from "@/types/domain";

export function ProjectRegistrationRepair({ project, needsAttention }: { project: ProjectSummary; needsAttention: boolean }) {
  const [repoRoot, setRepoRoot] = useState(project.repoRoot);
  const [vaultRoot, setVaultRoot] = useState(project.vaultRoot);
  const [allowDirty, setAllowDirty] = useState(false);
  const [previewSelection, setPreviewSelection] = useState<{ repoRoot: string; vaultRoot: string; allowDirty: boolean } | null>(null);
  const [previewResult, setPreviewResult] = useState<ReturnType<typeof useProjectRebind>["data"] | null>(null);
  const [browsing, setBrowsing] = useState(false);
  const [browseHint, setBrowseHint] = useState<string | null>(null);
  const rebind = useProjectRebind(project.id);
  const confirm = useConfirm();
  const canBrowseFolders = typeof window.tuskerShell?.pickFolder === "function";

  useEffect(() => {
    setRepoRoot(project.repoRoot);
    setVaultRoot(project.vaultRoot);
    setAllowDirty(false);
    setPreviewSelection(null);
    setPreviewResult(null);
  }, [project.id, project.repoRoot, project.vaultRoot]);

  const selection = () => ({ repoRoot: repoRoot.trim(), vaultRoot: vaultRoot.trim(), allowDirty });
  const previewIsCurrent = previewSelection !== null
    && previewSelection.repoRoot === repoRoot.trim()
    && previewSelection.vaultRoot === vaultRoot.trim()
    && previewSelection.allowDirty === allowDirty
    && previewResult?.ok === true;
  const dirtyConfirmation = async () => {
    if (!allowDirty) return "";
    const confirmed = await confirm({
      title: "Allow repair on a dirty checkout?",
      body: "This updates only the project registration. It leaves working-tree changes and task state untouched.",
      confirmLabel: "Allow dirty repair",
      tone: "danger",
      typeToConfirm: "ALLOW DIRTY",
    });
    return confirmed ? "ALLOW DIRTY" : null;
  };

  const browseForFolder = async (setValue: (path: string) => void) => {
    const pickFolder = window.tuskerShell?.pickFolder;
    if (!pickFolder) {
      setBrowseHint("Browse is available in the Tusker macOS app. In a browser, enter the absolute path manually.");
      return;
    }
    setBrowseHint(null);
    setBrowsing(true);
    try {
      const path = await pickFolder();
      if (path) setValue(path);
    } finally {
      setBrowsing(false);
    }
  };

  const repositoryVault = (path: string) => `${path.trim().replace(/\/+$/, "")}/.tusker`;
  const useRepositoryVault = () => {
    if (repoRoot.trim()) {
      setVaultRoot(repositoryVault(repoRoot));
      setPreviewSelection(null);
      setPreviewResult(null);
    }
  };

  const runRepair = async (dryRun: boolean) => {
    if (!dryRun && (project.automationEnabled || !previewIsCurrent)) return;
    const confirmation = allowDirty
      ? await dirtyConfirmation()
      : !dryRun
        ? (await confirm({
          title: "Rebind project registration?",
          body: "This preserves the project ID and task history while changing only the repository and vault registration.",
          confirmLabel: "Apply repair",
        }) ? "" : null)
        : "";
    if (confirmation === null) return;
    const current = selection();
    let result: Awaited<ReturnType<typeof rebind.mutateAsync>>;
    try {
      result = await rebind.mutateAsync({
        repoRoot: current.repoRoot,
        ...(current.vaultRoot ? { vaultRoot: current.vaultRoot } : {}),
        ...(current.allowDirty ? { allowDirty: true, confirm: confirmation ?? "ALLOW DIRTY" } : {}),
        ...(dryRun ? { dryRun: true } : {}),
      });
    } catch {
      if (dryRun) {
        setPreviewSelection(null);
        setPreviewResult(null);
      }
      return;
    }
    if (dryRun) {
      setPreviewSelection(current);
      setPreviewResult(result);
    } else {
      setPreviewSelection(null);
      setPreviewResult(null);
    }
  };

  const submit = (event: React.FormEvent) => {
    event.preventDefault();
    void runRepair(false);
  };

  return (
    <section className={cn("rounded-xl border p-4", needsAttention ? "border-warn/40 bg-warn-soft/40" : "border-line bg-panel")} aria-labelledby="registration-repair-title">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <SectionLabel className={needsAttention ? "text-warn" : "text-ink"}>Registration repair</SectionLabel>
          <h2 id="registration-repair-title" className="mt-1 text-[15px] font-semibold text-ink">{needsAttention ? "Serve is pointing at an unhealthy checkout" : "Rebind this project registration"}</h2>
          <p className="mt-1 max-w-[720px] text-[12.5px] leading-relaxed text-muted">{project.lastError ?? "Choose the canonical repository and vault. This changes registration only; it does not initialize a new vault or alter task history."}</p>
        </div>
        <Chip tone={needsAttention ? "fail" : "neutral"} mono>health: {project.health || "unknown"}</Chip>
      </div>
      <div className="mt-4 grid gap-3 rounded-lg border border-line bg-raised p-3 sm:grid-cols-2">
        <div className="min-w-0"><SectionLabel>Current repository</SectionLabel><Mono className="mt-1 block break-all text-[11px] text-ink-soft">{project.repoRoot || "Not served"}</Mono></div>
        <div className="min-w-0"><SectionLabel>Current vault</SectionLabel><Mono className="mt-1 block break-all text-[11px] text-ink-soft">{project.vaultRoot || "Not served"}</Mono></div>
      </div>
      <form onSubmit={submit} className="mt-4 space-y-3">
        <div className="grid gap-3 sm:grid-cols-2">
          <label className="block text-[12px] font-medium text-ink-soft">
            Repository path
            <div className="mt-1 flex">
              <TextInput required aria-label="Repair repository path" value={repoRoot} onChange={(event) => { setRepoRoot(event.target.value); setPreviewSelection(null); setPreviewResult(null); }} className="min-w-0 flex-1 rounded-r-none font-mono text-[11px]" />
              <button type="button" onClick={() => void browseForFolder((path) => { setRepoRoot(path); setVaultRoot(repositoryVault(path)); setPreviewSelection(null); setPreviewResult(null); })} disabled={browsing} aria-label="Browse repository folder" className="rounded-r-lg border border-l-0 border-line px-2 text-[10px] text-muted hover:bg-hover">Browse</button>
            </div>
          </label>
          <label className="block text-[12px] font-medium text-ink-soft">
            Vault path
            <div className="mt-1 flex">
              <TextInput aria-label="Repair vault path" value={vaultRoot} onChange={(event) => { setVaultRoot(event.target.value); setPreviewSelection(null); setPreviewResult(null); }} className="min-w-0 flex-1 rounded-r-none font-mono text-[11px]" />
              <button type="button" onClick={() => void browseForFolder(setVaultRoot)} disabled={browsing} aria-label="Browse vault folder" className="rounded-r-lg border border-l-0 border-line px-2 text-[10px] text-muted hover:bg-hover">Browse</button>
            </div>
            <button type="button" onClick={useRepositoryVault} disabled={!repoRoot.trim()} className="mt-1 text-[10px] text-muted underline decoration-line/70 underline-offset-2 hover:text-ink disabled:cursor-not-allowed disabled:text-faint">Use repository/.tusker</button>
          </label>
        </div>
        {(!canBrowseFolders || browseHint) ? <p className="text-[10px] leading-4 text-faint">{browseHint ?? "Browse is available in the Tusker macOS app. In a browser, enter the absolute path manually."}</p> : null}
        <p className="text-[11px] leading-4 text-muted">Rebinding requires background work to be off. {project.automationEnabled ? "Disable it in Basic settings, then return here." : "Background work is off, so this repair is available."}</p>
        <label className="flex items-start gap-2 text-[12px] leading-5 text-muted">
          <input type="checkbox" checked={allowDirty} onChange={(event) => { setAllowDirty(event.target.checked); setPreviewSelection(null); setPreviewResult(null); }} className="mt-1 accent-ink" />
          <span><strong className="text-ink-soft">Allow dirty checkout</strong><span className="block text-[11px] text-faint">Only enable this when you have reviewed uncommitted changes. The action still changes registration only.</span></span>
        </label>
        {previewIsCurrent && previewResult?.rebind ? <p role="status" className="text-[11px] leading-4 text-pass">Check complete; no registration changed.{typeof previewResult.rebind.retained_queued_count === "number" ? ` ${previewResult.rebind.retained_queued_count} queued run(s) stay attached.` : ""}</p> : null}
        <div className="flex flex-wrap items-center gap-3">
          <Button type="button" variant="default" disabled={!repoRoot.trim() || project.automationEnabled || rebind.isPending} onClick={() => void runRepair(true)}>{rebind.isPending ? "Checking…" : "Check repair"}</Button>
          <Button type="submit" variant="primary" aria-label="Repair registration" disabled={!previewIsCurrent || project.automationEnabled || rebind.isPending}>{rebind.isPending ? "Applying…" : "Apply repair"}</Button>
          <ActionResultLine pending={rebind.isPending} error={rebind.error} result={rebind.data} />
        </div>
      </form>
    </section>
  );
}
