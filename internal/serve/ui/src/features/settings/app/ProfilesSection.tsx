import { useEffect, useMemo, useRef, useState } from "react";
import { Check, CircleAlert, FolderPlus, Plus, Trash2 } from "lucide-react";
import { api, ApiError } from "@/lib/api";
import { Button, Select, TextInput, Toggle } from "@/components/ui/controls";
import { Card, Chip, Dot } from "@/components/ui/primitives";
import { CommandPolicyDetails } from "./CommandPolicyDetails";
import type {
  AgentAccessFolder,
  AgentAccessV1,
  ModelLevelProfile,
  ModelLevelsReport,
  RunnerCatalog,
  RunnerCatalogHarness,
  RunnerConformanceReport,
} from "@/types/domain";

type Scope = "global" | "project";
type Tier = "light" | "standard" | "demanding";
type Draft = {
  name: string;
  displayName: string;
  harness: string;
  model: string;
  effort: string;
  preset: string;
  access?: AgentAccessV1;
  eligibleTiers: Tier[];
};

const TIERS: Array<{ id: Tier; label: string }> = [
  { id: "light", label: "Tier 1 · Light" },
  { id: "standard", label: "Tier 2 · Standard" },
  { id: "demanding", label: "Tier 3 · Demanding" },
];

const EMPTY_ACCESS: AgentAccessV1 = {
  schema: "tusker.agent-access/v1",
  mode: "work_in_projects",
  network: true,
  destructive_actions: "deny",
  folders: [],
  private_folders: [],
};

const EMPTY_DRAFT: Draft = {
  name: "",
  displayName: "",
  harness: "codex_exec",
  model: "",
  effort: "",
  preset: "",
  access: EMPTY_ACCESS,
  eligibleTiers: [],
};

function cleanLabel(value: string): string {
  return value
    .replace(/^gpt[-_]?/i, "")
    .replace(/[-_]+/g, " ")
    .replace(/\b\w/g, (letter) => letter.toUpperCase());
}

function modelLabel(model: string): string {
  return /^gpt-\d+\.x$/i.test(model)
    ? "Model needs verification"
    : model || "Manual model";
}

function permissionPresetLabel(preset: string): string {
  return (
    (
      {
        "read-only": "Read-only",
        "workspace-write-offline": "Workspace only",
        "workspace-write-network": "Workspace + network",
        "danger-full-access": "Full access",
      } as Record<string, string>
    )[preset] ||
    preset ||
    "Access unavailable"
  );
}

function accessLabel(
  access: AgentAccessV1 | undefined,
  preset: string,
): string {
  if (!access) return permissionPresetLabel(preset);
  return access.mode === "review_only" ? "Review only" : "Work in projects";
}

function accessFromPreset(preset: string): AgentAccessV1 {
  return {
    ...defaultAccess(),
    mode: preset === "read-only" ? "review_only" : "work_in_projects",
    network:
      preset === "workspace-write-network" || preset === "danger-full-access",
    destructive_actions: preset === "read-only" ? "deny" : "ask",
  };
}

function presetMode(preset: string): "full_access" | AgentAccessV1["mode"] {
  if (preset === "danger-full-access") return "full_access";
  return preset === "read-only" ? "review_only" : "work_in_projects";
}

function accessPreset(
  access: AgentAccessV1 | undefined,
  preset: string,
): string {
  if (!access) return preset || "workspace-write-offline";
  if (access.mode === "review_only") return "read-only";
  return access.network ? "workspace-write-network" : "workspace-write-offline";
}

function cloneAccess(access: AgentAccessV1): AgentAccessV1 {
  return {
    ...access,
    folders: access.folders.map((folder) => ({ ...folder })),
    private_folders: [...access.private_folders],
  };
}

function defaultAccess(): AgentAccessV1 {
  return cloneAccess(EMPTY_ACCESS);
}

function defaultProfileName(
  harness: RunnerCatalogHarness | undefined,
  model: string,
): string {
  return model.trim()
    ? `${harness?.display_name || cleanLabel(harness?.harness || "Agent")} · ${cleanLabel(model)}`
    : harness?.display_name || "New profile";
}

function profileId(harness: string, model: string): string {
  const suffix = model
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/(^-|-$)/g, "");
  return `${harness}-${suffix || "profile"}`;
}

export function availableProfileId(
  harness: string,
  model: string,
  profiles: Record<string, unknown>,
): string {
  const base = profileId(harness, model);
  if (!profiles[base]) return base;
  for (let suffix = 2; ; suffix += 1) {
    const candidate = `${base}-${suffix}`;
    if (!profiles[candidate]) return candidate;
  }
}

function profileDraft(name: string, profile: ModelLevelProfile): Draft {
  return {
    name,
    displayName: profile.display_name || "",
    harness: profile.harness,
    model: profile.model,
    effort: profile.effort,
    preset: profile.permission_preset || "",
    access: profile.access ? cloneAccess(profile.access) : undefined,
    eligibleTiers: profile.eligible_tiers || [],
  };
}

function testState(
  report: RunnerConformanceReport | undefined,
  state: string | undefined,
  disabled: boolean,
  unavailable: boolean,
) {
  if (disabled) return { label: "Disabled", tone: "warn" as const };
  if (unavailable) return { label: "Unavailable", tone: "warn" as const };
  if (!report)
    return state === "tested"
      ? { label: "Test passed", tone: "pass" as const }
      : { label: "Needs test", tone: "neutral" as const };
  if (report.ready) return { label: "Test passed", tone: "pass" as const };
  return report.live
    ? { label: "Test failed", tone: "fail" as const }
    : { label: "Setup checked", tone: "info" as const };
}

function catalogHarnesses(
  catalog: RunnerCatalog | undefined,
): RunnerCatalogHarness[] {
  return (catalog?.harnesses || []).filter((harness) => harness.manual_entry);
}

function defaultModel(harness: RunnerCatalogHarness | undefined) {
  return (harness?.models || []).find((item) => !item.hidden && item.default);
}

function defaultEffort(
  model: { efforts: string[]; default_effort?: string } | undefined,
): string {
  return model?.default_effort && model.efforts.includes(model.default_effort)
    ? model.default_effort
    : model?.efforts[0] || "";
}

export function ProfilesSection({
  projectId,
  scope = "global",
}: {
  projectId?: string;
  scope?: Scope;
}) {
  const [levels, setLevels] = useState<ModelLevelsReport>();
  const [catalog, setCatalog] = useState<RunnerCatalog>();
  const [draft, setDraft] = useState<Draft>();
  const [reports, setReports] = useState<
    Record<string, RunnerConformanceReport>
  >({});
  const [setupStates, setSetupStates] = useState<
    Record<string, { tone: "pass" | "warn" | "checking"; message: string }>
  >({});
  const [error, setError] = useState("");
  const [running, setRunning] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [privateFolder, setPrivateFolder] = useState("");
  const [privateSaving, setPrivateSaving] = useState(false);
  const [privateNotice, setPrivateNotice] = useState("");
  const menuRoot = useRef<HTMLDivElement>(null);
  const setupRequest = useRef(0);
  const previousDraft = useRef("");
  const latestDraft = useRef("");

  const refreshLevels = async () => {
    const next = await api.modelLevels(projectId, scope);
    setLevels(next);
    return next;
  };
  const refreshCatalog = async (refresh = false) => {
    const next = await api.modelCatalog(refresh);
    setCatalog(next);
    return next;
  };

  useEffect(() => {
    setError("");
    void Promise.all([refreshLevels(), refreshCatalog()]).catch((cause) =>
      setError(cause instanceof ApiError ? cause.message : String(cause)),
    );
  }, [projectId, scope]);

  useEffect(() => {
    const closeMenus = (event: PointerEvent) => {
      if (menuRoot.current?.contains(event.target as Node)) return;
      menuRoot.current
        ?.querySelectorAll<HTMLDetailsElement>("details[open]")
        .forEach((menu) => menu.removeAttribute("open"));
    };
    document.addEventListener("pointerdown", closeMenus);
    return () => document.removeEventListener("pointerdown", closeMenus);
  }, []);

  const harnesses = useMemo(() => catalogHarnesses(catalog), [catalog]);
  const selectableHarnesses = useMemo(
    () => harnesses.filter((harness) => harness.group === "supported"),
    [harnesses],
  );
  const currentHarness = useMemo(
    () => harnesses.find((harness) => harness.harness === draft?.harness),
    [draft?.harness, harnesses],
  );
  const model = useMemo(
    () => currentHarness?.models?.find((item) => item.model === draft?.model),
    [currentHarness, draft?.model],
  );
  const allowedPresets =
    currentHarness?.options?.find((option) => option.id === "permission_preset")
      ?.values || [];
  const editing = Boolean(draft && levels?.profiles[draft.name]);
  const profileEntries = Object.entries(levels?.profiles || {});
  const displayCounts = useMemo(
    () =>
      profileEntries.reduce<Record<string, number>>((counts, [, profile]) => {
        const label =
          profile.display_name ||
          defaultProfileName(
            harnesses.find((item) => item.harness === profile.harness),
            profile.model,
          );
        counts[label] = (counts[label] || 0) + 1;
        return counts;
      }, {}),
    [levels?.profiles, harnesses],
  );
  const sharedPrivateFolders = levels?.private_folders || [];
  const draftKey = draft
    ? JSON.stringify({
        draft,
        sharedRevision: levels?.revision,
        routeVersion: currentHarness?.version || "",
        executableDetected: currentHarness?.executable_detected ?? false,
      })
    : "";
  latestDraft.current = draftKey;

  useEffect(() => {
    if (previousDraft.current !== draftKey && previousDraft.current !== "") {
      setupRequest.current += 1;
      setReports({});
      setSetupStates({});
    }
    previousDraft.current = draftKey;
  }, [draftKey]);

  useEffect(() => {
    if (!draft?.model.trim() || !draft.effort.trim()) return;
    const key = draftKey;
    const timer = window.setTimeout(() => {
      if (key === draftKey) void checkSetupDraft();
    }, 350);
    return () => window.clearTimeout(timer);
  }, [draftKey, levels?.revision]);

  function openNew() {
    const harness = selectableHarnesses[0] || harnesses[0];
    const model = defaultModel(harness);
    const harnessID = harness?.harness || "codex_exec";
    setError("");
    setSetupStates({});
    setDraft({
      ...EMPTY_DRAFT,
      name: availableProfileId(
        harnessID,
        model?.model || "",
        levels?.profiles || {},
      ),
      harness: harnessID,
      model: model?.model || "",
      effort: defaultEffort(model),
      access: defaultAccess(),
    });
  }

  function updateDraft(next: Partial<Draft>) {
    setDraft((old) => {
      if (!old) return old;
      const updated = { ...old, ...next };
      if (next.harness !== undefined && next.harness !== old.harness) {
        const harness = harnesses.find((item) => item.harness === next.harness);
        const nextModel = defaultModel(harness);
        updated.model = nextModel?.model || "";
        updated.effort = defaultEffort(nextModel);
        // Agent changes recompute support, but never discard authored access.
      }
      if (next.model !== undefined && next.model !== old.model)
        updated.effort = defaultEffort(
          harnesses
            .find((item) => item.harness === updated.harness)
            ?.models?.find((item) => item.model === next.model),
        );
      if (
        !levels?.profiles[old.name] &&
        (next.harness !== undefined || next.model !== undefined)
      )
        updated.name = availableProfileId(
          updated.harness,
          updated.model,
          levels?.profiles || {},
        );
      return updated;
    });
  }

  async function save() {
    if (!levels || !draft) return;
    const name = draft.name.trim();
    if (!name || !draft.model.trim() || !draft.effort.trim()) {
      setError("Choose a model and reasoning level before saving.");
      return;
    }
    if (!editing && levels.profiles[name]) {
      setError(
        "That stable profile ID already exists. Change the model or open the existing profile to edit it.",
      );
      return;
    }
    if (
      !draft.access &&
      allowedPresets.length > 0 &&
      !allowedPresets.includes(draft.preset)
    ) {
      setError(
        "That existing access option is not supported by the selected agent.",
      );
      return;
    }
    setError("");
    try {
      const saved = await api.modelProfileSet(
        {
          name,
          displayName: draft.displayName || undefined,
          eligibleTiers: draft.eligibleTiers,
          harness: draft.harness,
          model: draft.model,
          effort: draft.effort,
          ...(draft.access
            ? { access: cloneAccess(draft.access) }
            : { preset: draft.preset }),
        },
        levels.revision,
        scope,
        projectId,
      );
      setLevels(saved);
      setReports((old) => {
        const next = { ...old };
        delete next[name];
        return next;
      });
      setDraft(undefined);
    } catch (cause) {
      setError(cause instanceof ApiError ? cause.message : String(cause));
    } finally {
      setRunning(false);
    }
  }

  async function testDraft() {
    if (!draft || !draft.model.trim() || !draft.effort.trim()) {
      setError("Choose a model and reasoning level before testing.");
      return;
    }
    const request = ++setupRequest.current;
    const expectedDraft = draftKey;
    if (
      !draft.access &&
      allowedPresets.length > 0 &&
      !allowedPresets.includes(draft.preset)
    ) {
      setError(
        "That existing access option is not supported by the selected agent.",
      );
      return;
    }
    setRunning(true);
    setError("");
    try {
      const report = await api.runnerConformance(
        draft.harness,
        accessPreset(draft.access, draft.preset),
        true,
        "print",
        projectId,
        {
          id: draft.name || profileId(draft.harness, draft.model),
          model: draft.model,
          effort: draft.effort,
          ...(draft.access ? { access: cloneAccess(draft.access) } : {}),
        },
      );
      if (
        request !== setupRequest.current ||
        latestDraft.current !== expectedDraft
      )
        return;
      setReports((old) => ({ ...old, [draft.name]: report }));
    } catch (cause) {
      setError(cause instanceof ApiError ? cause.message : String(cause));
    } finally {
      setRunning(false);
    }
  }

  async function checkSetupDraft() {
    if (!draft) return;
    const request = ++setupRequest.current;
    const expectedDraft = draftKey;
    setSetupStates((old) => ({
      ...old,
      [draft.name]: { tone: "checking", message: "Checking compatibility…" },
    }));
    setError("");
    try {
      const harness = currentHarness;
      const report = await api.runnerConformance(
        draft.harness,
        accessPreset(draft.access, draft.preset),
        false,
        "",
        projectId,
        {
          id: draft.name || profileId(draft.harness, draft.model),
          model: draft.model,
          effort: draft.effort,
          ...(draft.access ? { access: cloneAccess(draft.access) } : {}),
        },
        true,
      );
      if (
        request !== setupRequest.current ||
        latestDraft.current !== expectedDraft
      )
        return;
      const ready = Boolean(
        harness?.available &&
        harness.discovery_state === "available" &&
        (!report.access || report.access.state === "ready"),
      );
      const issue = report.access?.issues?.[0]?.message;
      setSetupStates((old) => ({
        ...old,
        [draft.name]: {
          tone: ready ? "pass" : "warn",
          message: ready
            ? "Settings supported"
            : issue ||
              `${harness?.display_name || draft.harness} is not ready for this route. Refresh discovery or choose a supported agent.`,
        },
      }));
    } catch (cause) {
      if (
        request !== setupRequest.current ||
        latestDraft.current !== expectedDraft
      )
        return;
      setSetupStates((old) => ({
        ...old,
        [draft.name]: {
          tone: "warn",
          message:
            "Local setup check could not complete; the draft was preserved.",
        },
      }));
      setError(cause instanceof ApiError ? cause.message : String(cause));
    } finally {
      // Compatibility checks are provider-free and must not lock the draft.
    }
  }

  async function checkProfile(
    name: string,
    profile: ModelLevelProfile,
    live: boolean,
  ) {
    setRunning(true);
    setError("");
    try {
      const report = await api.runnerConformance(
        name,
        accessPreset(
          profile.access,
          profile.permission_preset || "workspace-write-offline",
        ),
        live,
        live ? "print" : "",
        projectId,
      );
      setReports((old) => ({ ...old, [name]: report }));
    } catch (cause) {
      setError(cause instanceof ApiError ? cause.message : String(cause));
    } finally {
      setRunning(false);
    }
  }

  async function lifecycle(
    action: "profile-disable" | "profile-enable" | "profile-remove",
    name: string,
  ) {
    if (!levels) return;
    setRunning(true);
    setError("");
    try {
      setLevels(
        await api.modelProfileLifecycle(
          action,
          name,
          levels.revision,
          scope,
          projectId,
        ),
      );
      setReports((old) => {
        const next = { ...old };
        delete next[name];
        return next;
      });
    } catch (cause) {
      setError(cause instanceof ApiError ? cause.message : String(cause));
    } finally {
      setRunning(false);
    }
  }

  async function refreshModels() {
    setRefreshing(true);
    setError("");
    try {
      const next = await refreshCatalog(true);
      setDraft((old) => {
        if (!old || old.model) return old;
        const nextModel = defaultModel(
          catalogHarnesses(next).find((item) => item.harness === old.harness),
        );
        return nextModel
          ? {
              ...old,
              model: nextModel.model,
              effort: defaultEffort(nextModel),
              name:
                old.name ||
                availableProfileId(
                  old.harness,
                  nextModel.model,
                  levels?.profiles || {},
                ),
            }
          : old;
      });
    } catch (cause) {
      setError(cause instanceof ApiError ? cause.message : String(cause));
    } finally {
      setRefreshing(false);
    }
  }

  async function savePrivateFolders(next: string[]) {
    if (!levels) return;
    setPrivateSaving(true);
    setError("");
    setPrivateNotice("");
    try {
      setLevels(
        await api.modelPrivateFoldersSet(
          next,
          levels.revision,
          scope,
          projectId,
        ),
      );
      setPrivateFolder("");
      setPrivateNotice(
        "Saved. Existing attempts retain their launch policy; the next launch uses the new private-folder revision.",
      );
    } catch (cause) {
      setError(cause instanceof ApiError ? cause.message : String(cause));
    } finally {
      setPrivateSaving(false);
    }
  }

  function addPrivateFolder() {
    const path = privateFolder.trim();
    if (!path.startsWith("/")) {
      setError("Private folders must be absolute host paths.");
      return;
    }
    if (sharedPrivateFolders.includes(path)) {
      setError("That private folder is already listed.");
      return;
    }
    void savePrivateFolders([...sharedPrivateFolders, path]);
  }

  return (
    <section
      className="profiles-access animate-rise"
      aria-labelledby="profiles-title"
    >
      <div className="mb-5 flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2
            id="profiles-title"
            className="font-serif text-[20px] font-semibold text-ink"
          >
            Profiles
          </h2>
          <p className="mt-1 max-w-[62ch] text-[14px] leading-relaxed text-muted">
            Choose the installed agent, model, reasoning level and access used
            for future runs.
          </p>
        </div>
        <Button
          type="button"
          variant="primary"
          onClick={openNew}
          disabled={!levels || running}
        >
          <Plus size={14} />
          Add profile
        </Button>
      </div>
      {error && (
        <p
          role="alert"
          className="mb-4 rounded-lg border border-fail/25 bg-fail-soft px-3 py-2 text-[12px] text-fail"
        >
          {error}
        </p>
      )}
      {!levels && !error && (
        <p className="text-[14px] text-muted">Loading profiles…</p>
      )}
      {levels && (
        <section
          aria-labelledby="private-folders-title"
          className="mb-5 rounded-xl border border-line bg-surface px-4 py-4"
        >
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div>
              <h3
                id="private-folders-title"
                className="text-[14px] font-semibold text-ink"
              >
                Private folders
              </h3>
              <p className="mt-1 max-w-[58ch] text-[12px] leading-relaxed text-muted">
                These folders are excluded from project-access profiles. Choose
                personal folders deliberately; changes apply to future runs.
              </p>
            </div>
            <Chip tone="info" variant="soft" className="rounded-md">
              Shared settings
            </Chip>
          </div>
          <div className="mt-3 grid gap-2">
            {sharedPrivateFolders.map((path) => (
              <div
                key={path}
                className="flex min-w-0 items-center gap-2 rounded-lg border border-line bg-panel px-3 py-2"
              >
                <code className="min-w-0 flex-1 break-all text-[12px] text-ink-soft">
                  {path}
                </code>
                <button
                  type="button"
                  aria-label={`Remove private folder ${path}`}
                  disabled={privateSaving}
                  onClick={() =>
                    void savePrivateFolders(
                      sharedPrivateFolders.filter((item) => item !== path),
                    )
                  }
                  className="inline-flex h-8 w-8 flex-none items-center justify-center rounded-md text-muted hover:bg-hover hover:text-danger focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/40 disabled:opacity-40"
                >
                  <Trash2 size={14} />
                </button>
              </div>
            ))}
            {sharedPrivateFolders.length === 0 && (
              <p className="rounded-lg border border-dashed border-line px-3 py-3 text-[12px] text-muted">
                No private folders added
              </p>
            )}
          </div>
          <div className="mt-3 flex flex-wrap items-end gap-2">
            <label className="grid min-w-0 flex-1 gap-1 text-[12px] font-medium text-muted">
              Absolute folder path
              <TextInput
                aria-label="Private folder path"
                value={privateFolder}
                onChange={(event) => setPrivateFolder(event.target.value)}
                placeholder="/Users/you/Private"
                className="w-full"
              />
            </label>
            <Button
              type="button"
              size="sm"
              onClick={addPrivateFolder}
              disabled={
                privateSaving ||
                !privateFolder.trim() ||
                !privateFolder.trim().startsWith("/")
              }
            >
              <FolderPlus size={14} />
              Add folder
            </Button>
          </div>
          {privateNotice && (
            <p role="status" className="mt-2 text-[12px] text-pass">
              {privateNotice}
            </p>
          )}
        </section>
      )}
      {draft && (
        <ProfileEditor
          draft={draft}
          harness={currentHarness}
          harnesses={harnesses}
          sharedPrivateFolders={sharedPrivateFolders}
          modelEfforts={model?.efforts || []}
          allowedPresets={allowedPresets}
          editing={editing}
          running={running}
          refreshing={refreshing}
          setupState={setupStates[draft.name]}
          report={reports[draft.name]}
          onChange={updateDraft}
          onClose={() => setDraft(undefined)}
          onRefresh={() => void refreshModels()}
          onSave={() => void save()}
          onCheckSetup={() => void checkSetupDraft()}
          onTest={() => void testDraft()}
        />
      )}
      {levels && profileEntries.length === 0 && !draft && (
        <p className="rounded-lg border border-line bg-surface px-4 py-5 text-[14px] text-muted">
          No profiles yet. Add an access profile for routine project work.
        </p>
      )}
      <div
        ref={menuRoot}
        onClickCapture={(event) => {
          const active = (event.target as HTMLElement).closest("details");
          menuRoot.current
            ?.querySelectorAll<HTMLDetailsElement>("details[open]")
            .forEach((menu) => {
              if (menu !== active) menu.removeAttribute("open");
            });
        }}
        className="grid gap-2"
      >
        {levels &&
          profileEntries.map(([name, profile]) => {
            const disabled =
              levels.profile_states[name] === "disabled" ||
              Boolean(profile.disabled);
            const unavailable = levels.profile_states[name] === "unavailable";
            const references = levels.profile_references[name] || [];
            const baseName =
              profile.display_name ||
              defaultProfileName(
                harnesses.find((item) => item.harness === profile.harness),
                profile.model,
              );
            const displayName =
              displayCounts[baseName] > 1
                ? `${baseName} · ${profile.effort || "custom"} · ${cleanLabel(name)}`
                : baseName;
            const status = testState(
              reports[name],
              levels.profile_states[name],
              disabled,
              unavailable,
            );
            return (
              <article
                key={name}
                tabIndex={0}
                aria-label={`Edit ${displayName}`}
                onClick={(event) => {
                  if ((event.target as HTMLElement).closest("details")) return;
                  setDraft(profileDraft(name, profile));
                }}
                onKeyDown={(event) => {
                  if (
                    event.target !== event.currentTarget ||
                    (event.key !== "Enter" && event.key !== " ")
                  )
                    return;
                  event.preventDefault();
                  setDraft(profileDraft(name, profile));
                }}
                className="flex cursor-pointer flex-wrap items-center gap-x-4 gap-y-2 rounded-xl border border-line bg-raised px-4 py-3.5 transition-[border-color,background-color] hover:border-accent/45 hover:bg-hover/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/30"
              >
                <div className="min-w-0 flex-1 sm:min-w-[11rem]">
                  <div className="flex items-center gap-2">
                    <Dot
                      tone={status.tone}
                      pulse={running && draft?.name === name}
                      size={7}
                    />
                    <h3 className="text-[14px] font-semibold text-ink">
                      {displayName}
                    </h3>
                  </div>
                  <p className="ml-[15px] mt-1 break-words text-[12px] text-muted">
                    {modelLabel(profile.model)} ·{" "}
                    {profile.effort || "Reasoning unavailable"} ·{" "}
                    {accessLabel(
                      profile.access,
                      profile.permission_preset || "",
                    )}
                  </p>
                </div>
                <div className="flex flex-wrap gap-1">
                  {profile.eligible_tiers.map((tier) => (
                    <Chip
                      key={tier}
                      tone="accent"
                      variant="soft"
                      className="rounded-md text-[10px]"
                    >
                      {TIERS.find((item) => item.id === tier)?.label}
                    </Chip>
                  ))}
                  {profile.eligible_tiers.length === 0 && (
                    <span className="text-[12px] text-muted">No tiers</span>
                  )}
                </div>
                <Chip tone={status.tone} variant="soft" className="rounded-md">
                  <Dot tone={status.tone} size={6} />
                  {status.label}
                </Chip>
                <details className="relative">
                  <summary
                    className="cursor-pointer list-none rounded p-2 text-[15px] text-muted hover:bg-hover focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/40"
                    aria-label={`Actions for ${displayName}`}
                  >
                    •••
                  </summary>
                  <div
                    onClick={(event) => {
                      if ((event.target as HTMLElement).closest("button"))
                        event.currentTarget.parentElement?.removeAttribute(
                          "open",
                        );
                    }}
                    className="absolute right-0 z-10 mt-1 w-44 rounded-md border border-line bg-raised p-1 text-[12px] shadow-lg"
                  >
                    <button
                      type="button"
                      onClick={() => setDraft(profileDraft(name, profile))}
                      className="w-full rounded px-2 py-2 text-left hover:bg-hover"
                    >
                      Edit
                    </button>
                    <button
                      type="button"
                      disabled={running || unavailable}
                      onClick={() => void checkProfile(name, profile, false)}
                      className="w-full rounded px-2 py-2 text-left hover:bg-hover disabled:opacity-40"
                    >
                      Check setup
                    </button>
                    <button
                      type="button"
                      disabled={running || disabled || unavailable}
                      onClick={() =>
                        void lifecycle(
                          disabled ? "profile-enable" : "profile-disable",
                          name,
                        )
                      }
                      className="w-full rounded px-2 py-2 text-left hover:bg-hover disabled:opacity-40"
                    >
                      {disabled ? "Enable" : "Disable"}
                    </button>
                    <button
                      type="button"
                      disabled={running}
                      title="Remove profile"
                      onClick={() => void lifecycle("profile-remove", name)}
                      className="w-full rounded px-2 py-2 text-left text-danger hover:bg-hover disabled:opacity-40"
                    >
                      Remove
                    </button>
                  </div>
                </details>
                {references.length > 0 && (
                  <p className="w-full text-[12px] text-muted">
                    Referenced by {references.join(", ")}.
                  </p>
                )}
              </article>
            );
          })}
      </div>
    </section>
  );
}

function ProfileEditor({
  draft,
  harness,
  harnesses,
  sharedPrivateFolders,
  modelEfforts,
  allowedPresets,
  editing,
  running,
  refreshing,
  setupState,
  report,
  onChange,
  onClose,
  onRefresh,
  onSave,
  onCheckSetup,
  onTest,
}: {
  draft: Draft;
  harness?: RunnerCatalogHarness;
  harnesses: RunnerCatalogHarness[];
  sharedPrivateFolders: string[];
  modelEfforts: string[];
  allowedPresets: string[];
  editing: boolean;
  running: boolean;
  refreshing: boolean;
  setupState?: { tone: "pass" | "warn" | "checking"; message: string };
  report?: RunnerConformanceReport;
  onChange: (next: Partial<Draft>) => void;
  onClose: () => void;
  onRefresh: () => void;
  onSave: () => void;
  onCheckSetup: () => void;
  onTest: () => void;
}) {
  const [path, setPath] = useState("");
  const [privatePath, setPrivatePath] = useState("");
  const models = harness?.models || [];
  const transports = harness?.transports || [];
  const discoveryReady =
    harness?.discovery_state === "available" && models.length > 0;
  const transportOwner =
    harness?.harness === "codex_exec"
      ? "Codex"
      : `${harness?.display_name || "agent"} profile`;
  const modelOptions = models.some((item) => item.model === draft.model)
    ? models
    : draft.model
      ? [
          {
            model: draft.model,
            display_name: `Manual: ${draft.model}`,
            efforts: modelEfforts,
            default: false,
            default_known: false,
            visibility: "manual",
            hidden: false,
          },
          ...models,
        ]
      : models;
  const presetOptions =
    draft.preset && !allowedPresets.includes(draft.preset)
      ? [draft.preset, ...allowedPresets]
      : allowedPresets;
  const accessControls =
    report?.access?.controls || harness?.access_controls || [];
  const resolvedAccess = report?.access;
  const access = draft.access;
  const selectedAccess = access || accessFromPreset(draft.preset);
  const currentMode = access ? access.mode : presetMode(draft.preset);
  const fullAccess = currentMode === "full_access";
  const references = selectedAccess.folders;
  const setupReady =
    harness?.available && harness.discovery_state === "available";

  function updateAccess(next: Partial<AgentAccessV1>) {
    if (!access) return;
    onChange({ access: { ...cloneAccess(selectedAccess), ...next } });
  }
  function updateMode(value: string) {
    if (value === "full_access") {
      onChange({ access: undefined, preset: "danger-full-access" });
      return;
    }
    onChange({
      access: {
        ...cloneAccess(access || accessFromPreset(draft.preset)),
        mode: value as AgentAccessV1["mode"],
      },
    });
  }
  function addFolder(
    folderPath: string,
    folderAccess: AgentAccessFolder["access"] = "read",
  ) {
    const value = folderPath.trim();
    if (
      !value ||
      !value.startsWith("/") ||
      references.some((folder) => folder.path === value)
    )
      return;
    updateAccess({
      folders: [...references, { path: value, access: folderAccess }],
    });
    setPath("");
  }
  function addPrivatePath() {
    const value = privatePath.trim();
    if (
      !value.startsWith("/") ||
      sharedPrivateFolders.includes(value) ||
      selectedAccess.private_folders.includes(value)
    )
      return;
    updateAccess({
      private_folders: [...selectedAccess.private_folders, value],
    });
    setPrivatePath("");
  }

  return (
    <form
      onSubmit={(event) => {
        event.preventDefault();
        onSave();
      }}
      className="mb-5 rounded-xl border border-line bg-raised p-4 shadow-sm sm:p-5"
      aria-label={editing ? "Edit profile" : "Add profile"}
    >
      <div className="mb-5 flex items-center justify-between gap-3">
        <div>
          <h3 className="font-serif text-[18px] font-semibold text-ink">
            {editing ? "Edit profile" : "Add profile"}
          </h3>
          <p className="mt-1 text-[12px] text-muted">
            Choose an agent, add protected folders, and save.
          </p>
        </div>
      </div>
      <div className="grid gap-4 sm:grid-cols-3">
        <label className="grid content-start gap-1.5 text-[12px] font-medium text-muted">
          Coding agent
          <Select
            aria-label="Coding agent"
            value={draft.harness}
            onChange={(event) => onChange({ harness: event.target.value })}
            className="w-full"
          >
            {harness ? null : (
              <option value={draft.harness}>{draft.harness}</option>
            )}
            {harnesses.map((item) => (
              <option key={item.harness} value={item.harness}>
                {item.display_name}
                {item.group !== "supported" ? " · unavailable" : ""}
              </option>
            ))}
          </Select>
        </label>
        {discoveryReady && (
          <div className="grid content-start gap-1.5 text-[12px] font-medium text-muted">
            <span className="flex items-center justify-between gap-2">
              <label htmlFor="profile-model">Model</label>
              <span className="flex items-center gap-1">
                <Chip
                  tone="info"
                  variant="soft"
                  className="rounded-md text-[10px]"
                >
                  Discovered
                </Chip>
                <button
                  type="button"
                  aria-label="Rediscover models"
                  disabled={refreshing}
                  onClick={onRefresh}
                  className="rounded px-1.5 py-1 text-[11px] text-muted underline hover:text-ink focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/40 disabled:opacity-40"
                >
                  {refreshing ? "Refreshing…" : "Refresh"}
                </button>
              </span>
            </span>
            <Select
              id="profile-model"
              aria-label="Model"
              value={draft.model}
              onChange={(event) => onChange({ model: event.target.value })}
              className="w-full"
            >
              {modelOptions.map((item) => (
                <option key={item.model} value={item.model}>
                  {item.display_name || item.model}
                </option>
              ))}
            </Select>
            {harness?.last_checked && (
              <span className="text-[10px] font-normal text-faint">
                Checked {new Date(harness.last_checked).toLocaleString()}
              </span>
            )}
          </div>
        )}
        {discoveryReady && (
          <label className="grid content-start gap-1.5 text-[12px] font-medium text-muted">
            Reasoning
            <Select
              aria-label="Reasoning"
              value={draft.effort}
              onChange={(event) => onChange({ effort: event.target.value })}
              className="w-full"
            >
              {modelEfforts.map((effort) => (
                <option key={effort} value={effort}>
                  {effort}
                </option>
              ))}
            </Select>
          </label>
        )}
        {!discoveryReady && (
          <div
            role="alert"
            className="sm:col-span-2 rounded-lg border border-fail/25 bg-fail-soft px-3 py-3 text-[12px] text-fail"
          >
            <p>
              {harness?.error ||
                `${harness?.display_name || "Agent"} model discovery has not returned choices.`}
            </p>
            <button
              type="button"
              disabled={refreshing}
              onClick={onRefresh}
              className="mt-1 rounded py-1 text-[12px] font-medium underline disabled:opacity-40"
            >
              {refreshing ? "Retrying…" : "Retry discovery"}
            </button>
          </div>
        )}
      </div>
      <section
        aria-labelledby="profile-access-title"
        className="mt-5 min-w-0 max-w-full rounded-xl border border-line bg-surface p-4"
      >
        <div className="flex min-w-0 flex-wrap items-start justify-between gap-3">
          <div className="min-w-0">
            <h4
              id="profile-access-title"
              className="text-[15px] font-semibold text-ink"
            >
              Access
            </h4>
            <p className="mt-1 max-w-[58ch] text-[12px] leading-relaxed text-muted">
              Choose what this agent can touch. Changes are kept in this draft
              until you save.
            </p>
          </div>
          <label className="grid min-w-0 w-full gap-1 text-[11px] font-medium text-muted sm:w-auto sm:min-w-[12rem]">
            Current mode
            <Select
              aria-label="Access mode"
              value={currentMode}
              onChange={(event) => updateMode(event.target.value)}
              className="w-full max-w-full"
            >
              <option value="work_in_projects">Work in projects</option>
              <option value="review_only">Review only</option>
              <option value="full_access">Full access</option>
            </Select>
          </label>
        </div>
        <p className="mt-3 text-[13px] text-ink-soft">
          <strong>Automatic work:</strong>{" "}
          {fullAccess
            ? "Unrestricted files and native actions"
            : selectedAccess.mode === "review_only"
            ? "Read-only review"
            : "Project edits, builds and tests"}{" "}
          · Internet {selectedAccess.network ? "On" : "Off"}
          {!fullAccess && (
            <>
              {" "}· Destructive actions{" "}
              {selectedAccess.destructive_actions === "ask"
                ? "Ask"
                : "Blocked"}
            </>
          )}
        </p>
        <section
          aria-labelledby="protected-folders-title"
          className="mt-4 min-w-0 border-t border-line-soft pt-4"
        >
          <h5 className="py-1 text-[13px] font-medium text-ink">
            <span id="protected-folders-title">Protected folders</span>
          </h5>
          <div className="mt-3 grid min-w-0 gap-4">
            <div className="min-w-0">
              <p className="text-[12px] font-medium text-ink-soft">
                Protected folders
              </p>
              <p className="mt-1 text-[12px] text-muted">
                Shared exclusions apply to project-access profiles; add
                profile-specific exclusions here.
              </p>
              <div className="mt-2 grid min-w-0 gap-2">
                {sharedPrivateFolders.map((folder) => (
                  <code
                    key={folder}
                    className="break-all text-[12px] text-ink-soft"
                  >
                    {folder} · Inherited
                  </code>
                ))}
                {selectedAccess.private_folders.map((folder) => (
                  <div
                    key={folder}
                    className="flex min-w-0 items-center gap-2 rounded-lg border border-line bg-surface px-3 py-2"
                  >
                    <code className="min-w-0 flex-1 break-all text-[12px] text-ink-soft">
                      {folder} · This profile
                    </code>
                    <button
                      type="button"
                      aria-label={`Remove profile private folder ${folder}`}
                      onClick={() =>
                        updateAccess({
                          private_folders:
                            selectedAccess.private_folders.filter(
                              (item) => item !== folder,
                            ),
                        })
                      }
                      className="inline-flex h-8 w-8 flex-none items-center justify-center rounded-md text-muted hover:bg-hover hover:text-danger focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/40"
                    >
                      <Trash2 size={14} />
                    </button>
                  </div>
                ))}
                {fullAccess && (
                  <p className="rounded-lg border border-dashed border-line px-3 py-3 text-[12px] text-muted">
                    Private exclusions are not applied while Full access is
                    selected.
                  </p>
                )}
                {!fullAccess &&
                  sharedPrivateFolders.length === 0 &&
                  selectedAccess.private_folders.length === 0 && (
                    <p className="rounded-lg border border-dashed border-line px-3 py-3 text-[12px] text-muted">
                      No private folders added
                    </p>
                  )}
              </div>
              <div className="mt-2 flex min-w-0 flex-wrap items-end gap-2">
                <label className="grid min-w-0 flex-1 gap-1 text-[12px] font-medium text-muted">
                  Additional absolute folder path
                  <TextInput
                    aria-label="Profile private folder path"
                    value={privatePath}
                    onChange={(event) => setPrivatePath(event.target.value)}
                    placeholder="/Users/you/Private"
                    className="w-full max-w-full"
                  />
                </label>
                <Button
                  type="button"
                  size="sm"
                  onClick={addPrivatePath}
                  disabled={!access || !privatePath.trim()}
                >
                  <FolderPlus size={14} />
                  Add folder
                </Button>
              </div>
            </div>
          </div>
        </section>
      </section>
      <details className="mt-4 min-w-0 rounded-lg bg-panel/55 px-3 py-3">
        <summary className="cursor-pointer py-1 text-[13px] font-medium text-ink focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/40">
          Advanced
        </summary>
        <div className="mt-3 flex flex-wrap gap-2">
          <Button
            type="button"
            size="sm"
            onClick={() =>
              updateAccess({
                mode: "work_in_projects",
                network: true,
                destructive_actions: "deny",
              })
            }
            disabled={!access}
          >
            Use recommended defaults
          </Button>
          <Button
            type="button"
            size="sm"
            onClick={onCheckSetup}
            disabled={running || !draft.harness}
          >
            Check setup
          </Button>
          <Button
            type="button"
            size="sm"
            onClick={onTest}
            disabled={running || !draft.model || !draft.effort}
          >
            {running ? "Running test…" : "Run test"}
          </Button>
        </div>
        <div className="mt-3 grid min-w-0 gap-3 sm:grid-cols-2">
          <label className="flex items-center justify-between gap-3 text-[12px]">
            Internet
            <Toggle
              checked={selectedAccess.network}
              onChange={(network) => updateAccess({ network })}
              label={selectedAccess.network ? "On" : "Off"}
              disabled={!access}
            />
          </label>
          <label className="grid min-w-0 gap-1.5 text-[12px]">
            Destructive actions
            <Select
              aria-label="Destructive actions"
              value={
                selectedAccess.mode === "review_only"
                  ? "deny"
                  : selectedAccess.destructive_actions
              }
              onChange={(event) =>
                updateAccess({
                  destructive_actions: event.target
                    .value as AgentAccessV1["destructive_actions"],
                })
              }
              disabled={!access || selectedAccess.mode === "review_only"}
            >
              <option value="deny">Block without prompting</option>
              <option value="ask">Ask before running</option>
            </Select>
          </label>
          <div className="min-w-0 sm:col-span-2">
            <p className="text-[12px] font-medium">Reference folders</p>
            {references.map((folder) => (
              <div
                key={folder.path}
                className="mt-2 flex min-w-0 items-center gap-2"
              >
                <code className="min-w-0 flex-1 break-all text-[12px]">
                  {folder.path} ·{" "}
                  {folder.access === "read" ? "Read only" : "Read and write"}
                </code>
                <button
                  type="button"
                  aria-label={`Remove reference folder ${folder.path}`}
                  onClick={() =>
                    updateAccess({
                      folders: references.filter(
                        (item) => item.path !== folder.path,
                      ),
                    })
                  }
                >
                  <Trash2 size={14} />
                </button>
              </div>
            ))}
            <div className="mt-2 flex min-w-0 flex-wrap gap-2">
              <TextInput
                aria-label="Reference folder path"
                value={path}
                onChange={(event) => setPath(event.target.value)}
                placeholder="/absolute/reference"
                className="min-w-0 flex-1"
              />
              <Button
                type="button"
                size="sm"
                onClick={() => addFolder(path)}
                disabled={!access || !path.trim()}
              >
                Add reference
              </Button>
            </div>
          </div>
          <fieldset className="sm:col-span-2">
            <legend className="text-[12px] font-medium">Eligible tiers</legend>
            <div className="mt-2 flex flex-wrap gap-2">
              {TIERS.map((tier) => (
                <label
                  key={tier.id}
                  className="rounded-lg border border-line px-3 py-2 text-[12px]"
                >
                  <input
                    type="checkbox"
                    checked={draft.eligibleTiers.includes(tier.id)}
                    onChange={(event) =>
                      onChange({
                        eligibleTiers: event.target.checked
                          ? [...draft.eligibleTiers, tier.id]
                          : draft.eligibleTiers.filter(
                              (item) => item !== tier.id,
                            ),
                      })
                    }
                  />{" "}
                  {tier.label}
                </label>
              ))}
            </div>
          </fieldset>
          {!access && (
            <label className="grid min-w-0 gap-1.5 text-[12px]">
              Permission preset
              <Select
                aria-label="Permission preset"
                value={draft.preset}
                onChange={(event) => onChange({ preset: event.target.value })}
                className="w-full max-w-full"
              >
                {presetOptions.length > 0 ? (
                  presetOptions.map((preset) => (
                    <option key={preset} value={preset}>
                      {permissionPresetLabel(preset)}
                    </option>
                  ))
                ) : (
                  <option value={draft.preset}>
                    {draft.preset || "Unavailable"}
                  </option>
                )}
              </Select>
            </label>
          )}
          {draft.preset && access && (
            <p className="min-w-0 rounded-lg border border-line bg-surface px-3 py-2 text-[12px] text-muted">
              <strong className="text-ink-soft">Permission preset:</strong>{" "}
              {permissionPresetLabel(draft.preset)}.
            </p>
          )}
          <label className="grid min-w-0 gap-1.5 text-[12px]">
            Display name (optional)
            <TextInput
              aria-label="Display name"
              value={draft.displayName}
              onChange={(event) =>
                onChange({ displayName: event.target.value })
              }
              placeholder={defaultProfileName(harness, draft.model)}
              className="w-full max-w-full"
            />
          </label>
          <label className="grid min-w-0 gap-1.5 text-[12px]">
            Transport (managed by {transportOwner})
            <TextInput
              aria-label="Transport"
              value={transports.join(", ") || "Unavailable"}
              readOnly
              className="w-full max-w-full"
            />
          </label>
          <label className="grid min-w-0 gap-1.5 text-[12px]">
            Manual model ID
            <TextInput
              aria-label="Manual model ID"
              value={draft.model}
              onChange={(event) => onChange({ model: event.target.value })}
              className="w-full max-w-full"
            />
          </label>
          <label className="grid min-w-0 gap-1.5 text-[12px]">
            Manual reasoning level
            <TextInput
              aria-label="Manual reasoning level"
              value={draft.effort}
              onChange={(event) => onChange({ effort: event.target.value })}
              className="w-full max-w-full"
            />
          </label>
        </div>
        <p className="mt-3 text-[12px] text-muted">
          Manual model and reasoning values remain unverified until a live test
          succeeds. Stable ID:{" "}
          <code>{draft.name || profileId(draft.harness, draft.model)}</code>.
        </p>
        <details className="mt-3 rounded-lg border border-line px-3 py-2">
          <summary className="cursor-pointer text-[12px] font-medium">
            Technical details
          </summary>
          <div className="mt-2">
            <CommandPolicyDetails
              access={
                fullAccess
                  ? { ...selectedAccess, mode: "work_in_projects" }
                  : selectedAccess
              }
              policy={resolvedAccess?.command_policy}
              controls={accessControls}
              state={resolvedAccess?.state}
              issues={resolvedAccess?.issues}
            />
          </div>
        </details>
      </details>
      <div className="mt-5 flex flex-wrap items-center gap-2">
        <Button
          type="submit"
          variant="primary"
          disabled={running || !draft.model || !draft.effort}
        >
          Save profile
        </Button>
        <Button type="button" onClick={onClose}>
          Cancel
        </Button>
      </div>
      <p className="mt-3 text-[12px] text-muted">
        Save persists this profile only. Setup checks are local; Run test is
        explicit and may use provider usage.
      </p>
      {setupState && (
        <p
          role="status"
          className={`mt-3 rounded-lg border px-3 py-2 text-[12px] ${setupState.tone === "pass" ? "border-pass/25 bg-pass-soft text-pass" : "border-warn/25 bg-warn-soft text-warn"}`}
        >
          {setupState.message}
        </p>
      )}
      {report && (
        <Card
          tone={report.ready ? "pass" : "fail"}
          className={
            report.ready
              ? "mt-3 flex items-start gap-2 bg-pass-soft p-3 text-pass"
              : "mt-3 flex items-start gap-2 bg-fail-soft p-3 text-fail"
          }
        >
          {report.ready ? (
            <Check className="mt-0.5 flex-none" size={16} strokeWidth={2.5} />
          ) : (
            <CircleAlert className="mt-0.5 flex-none" size={16} />
          )}
          <div>
            <p className="text-[13px] font-semibold">
              {report.ready ? "Test passed" : "Test failed"}
            </p>
            <p className="mt-0.5 text-[12px] opacity-80">
              {report.ready
                ? "This exact model and reasoning configuration completed the explicit test."
                : report.next_step ||
                  "The model did not complete the conformance check."}
            </p>
            {report.access && (
              <p className="mt-1 text-[12px] opacity-80">
                Access route: {report.access.state};{" "}
                {report.access.controls.length} control checks reported.
              </p>
            )}
          </div>
        </Card>
      )}
      {!setupReady && (
        <p className="mt-3 flex items-start gap-2 text-[12px] text-warn">
          <CircleAlert size={14} className="mt-0.5 flex-none" />
          This route is not currently available. You can preserve the draft, but
          it will not be eligible for a run until setup is supported.
        </p>
      )}
    </form>
  );
}
