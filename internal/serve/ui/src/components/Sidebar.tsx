import { useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { cn } from "@/lib/cn";
import { useRegisterProject } from "@/lib/queries";

export function AddProjectForm({ onDone }: { onDone: () => void }) {
  const [repoRoot, setRepoRoot] = useState("");
  const [vaultRoot, setVaultRoot] = useState("");
  const [browsing, setBrowsing] = useState(false);
  const [browseHint, setBrowseHint] = useState<string | null>(null);
  const register = useRegisterProject();
  const navigate = useNavigate();
  const canBrowseFolders = typeof window.tuskerShell?.pickFolder === "function";
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
  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    const result = await register.mutateAsync({
      repoRoot: repoRoot.trim(),
      ...(vaultRoot.trim() ? { vaultRoot: vaultRoot.trim() } : {}),
    });
    if (result.ok && result.projectId) {
      onDone();
      await navigate({ to: "/p/$projectId", params: { projectId: result.projectId } });
    }
  };
  return (
    <form onSubmit={submit} className="mx-1 mt-2 rounded-xl border border-line bg-raised p-3 shadow-2xs" data-add-project-form>
      <label className="font-mono text-[9.5px] uppercase tracking-[0.12em] text-faint">
        Repository path
        <div className="mt-1 flex">
          <input
            required
            autoFocus
            value={repoRoot}
            onChange={(event) => setRepoRoot(event.target.value)}
            placeholder="/Users/me/code/project"
            className="min-w-0 flex-1 rounded-l-md border border-line bg-surface px-2.5 py-1.5 font-mono text-[11px] normal-case tracking-normal text-ink outline-none focus:border-info"
          />
          <button type="button" onClick={() => void browseForFolder(setRepoRoot)} disabled={browsing} aria-label="Browse repository folder" className="rounded-r-md border border-l-0 border-line bg-panel px-2.5 text-[10px] font-medium normal-case tracking-normal text-muted hover:bg-hover hover:text-ink">
            Browse
          </button>
        </div>
      </label>
      <label className="mt-2.5 block font-mono text-[9.5px] uppercase tracking-[0.12em] text-faint">
        Vault path
        <div className="mt-1 flex">
          <input
            value={vaultRoot}
            onChange={(event) => setVaultRoot(event.target.value)}
            placeholder="defaults to .tusker"
            className="min-w-0 flex-1 rounded-l-md border border-line bg-surface px-2.5 py-1.5 font-mono text-[11px] normal-case tracking-normal text-ink outline-none focus:border-info"
          />
          <button type="button" onClick={() => void browseForFolder(setVaultRoot)} disabled={browsing} aria-label="Browse vault folder" className="rounded-r-md border border-l-0 border-line bg-panel px-2.5 text-[10px] font-medium normal-case tracking-normal text-muted hover:bg-hover hover:text-ink">
            Browse
          </button>
        </div>
      </label>
      {(!canBrowseFolders || browseHint) && <p className="mt-2 text-[10.5px] leading-4 text-faint">{browseHint ?? "Browse is available in the Tusker macOS app. In a browser, enter the absolute path manually."}</p>}
      <p className="mt-2 text-[10.5px] leading-4 text-faint">Registers only. Daemon automation stays off.</p>
      {register.data?.reason && <p className={cn("mt-2 text-[10.5px]", register.data.ok ? "text-pass" : "text-fail")}>{register.data.reason}</p>}
      <button
        type="submit"
        disabled={!repoRoot.trim() || register.isPending}
        className="mt-3 w-full rounded-lg bg-ink px-3 py-2 text-[11.5px] font-semibold text-surface shadow-2xs hover:opacity-90 disabled:opacity-40 transition-opacity"
      >
        {register.isPending ? "Registering…" : "Register project"}
      </button>
    </form>
  );
}
