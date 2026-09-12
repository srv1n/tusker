---
subject: agent-access
title: "Agent access: simple profiles, honest permissions"
keywords: [permissions, sandbox, profiles, native Muse, Claude, Codex, ACP, private folders, approvals]
part_of: runners-and-acp
describes: [cmd/tusker, internal/runner, internal/serve/ui]
status: canonical
created: 2026-09-11
read_when: "Building profile access settings, native harness mappings, exceptional approvals or a new agent adapter."
skip_when: "Looking up shipped runner behavior; read runners-and-acp and the installed-route qualification report."
sources:
  - .tusker/specs/decisions/2026-09-11-agent-access-grill.md
  - .tusker/specs/model-level-configuration.md
  - .tusker/specs/runner-execution-boundary.md
  - docs/reports/agent-profiles/capabilities.md
  - docs/reports/agent-coordination/provider-research.md
  - skills/apple-design/references/hig/settings.md
  - skills/apple-design/references/hig/entering-data.md
  - skills/apple-design/references/hig/alerts.md
  - skills/apple-design/references/hig/generative-ai.md
  - https://code.claude.com/docs/en/permissions
  - https://www.anthropic.com/engineering/claude-code-auto-mode
  - https://learn.chatgpt.com/docs/permissions
  - https://agentclientprotocol.com/protocol/v1/tool-calls
updates:
  - docs/system/runners-and-acp.md
  - docs/system/serve-ui.md
  - docs/system/cli.md
decisions_locked: false
capsule:
  what: "Simple profile access, native control mappings and exceptional approvals."
  use_when: "Building or onboarding installed agents with honest permissions and good defaults."
  skip_when: "Looking up already shipped behavior or treating this spec as qualification evidence."
---

# Agent access

## Why

An operator should be able to choose an installed coding agent, let it work in a project with internet access, and reserve interruptions for exceptional actions. Adding another agent should mean adapting its native controls and running the same qualification cases, without redesigning settings or execution.

This is a build specification. It does not claim these controls are already shipped. The operator confirmed the customer goals and asked us to author the interface, schema, defaults and stories. Exact labels, fields and decomposition below are design recommendations within that request, not answers to questions we never asked. `decisions_locked: false` keeps future behavior from being treated as an already-landed system-doc update; it does not prevent implementation of these stories.

## Product contract

Profiles remain the place where people choose an agent, model and access. The ordinary choice is **Work in projects**: routine code changes, commands and internet access proceed automatically. New profiles block recognized destructive operations without prompting; Advanced can explicitly change this to Ask before running. **Review only** is the other ordinary choice. Reference projects start read only. Protected folders are visible in the main form and use the existing private-folder settings.

The operator's later request to choose the defaults and prepare a junior-agent handoff supersedes the original ask-by-default recommendation for newly created profiles. Existing profiles keep their authored values. The current editor design is specified in [Simple profile editor](#simple-profile-editor); the earlier multi-card access dashboard is not the target.

The client is trusted to implement its published controls. Tusker supplies accident guardrails through native settings, hooks and supported approval protocols. It does not isolate a malicious client from the operator's account. Rules for a shell tool also do not make arbitrary Python, build scripts or third-party tools safe. The interface states the actual scope once in Access details, and surfaces a specific missing control when it prevents a run.

Existing profiles keep their current behavior until the operator changes access. Opening settings, saving a model change, installing an agent or upgrading Tusker must never broaden access or switch the execution route.

## Requirements

| ID | Result |
| --- | --- |
| X1 | One profile access contract covers CLI and ACP without treating transport as a permission mode. |
| X2 | New profiles default to project work, internet on, automatic routine work, a private run temporary directory and blocking recognized destructive actions without prompts; asking is an explicit Advanced option. |
| X3 | Project references default to read only; user-selected private folders take precedence; unavailable restrictions cannot silently become broader grants. |
| X4 | The UI shows the effective controls and gaps for the exact installed route, without implying malicious-client isolation or universal script inspection. |
| X5 | Existing profiles, tiers, overrides and execution routes migrate without permission widening or automatic starts. |
| X6 | Exceptional approvals bind to one exact request, survive appropriate lifecycle events, and cannot authorize another attempt or changed command. |
| X7 | Native Muse CLI is a distinct executable route; the existing Muse-through-Codex route remains addressable and unchanged. |
| X8 | Settings and task screens use existing components, good defaults, progressive disclosure and accessible states. |
| X9 | A new harness is onboarded by a bounded capability declaration, adapter and shared contract tests; unsupported features are explicit. |
| X10 | Offline, installed-client and live qualification are separate, with user documentation updated when behavior ships. |

## Defaults and scope

| Setting | New profile default | Change location |
| --- | --- | --- |
| Access | Work in projects | Profile editor |
| Working files | Read/write in the actual execution workspace | Derived from the existing workspace resolver |
| Internet | On | Access details |
| Routine edits, builds and tests | Automatic | Consequence of the selected access mode |
| Recognized destructive project operations | Block without prompting | Advanced may explicitly change to Ask before running |
| Reference folders | None; additions start Read only | Access details |
| Temporary files | A unique subdirectory of the host temporary directory per attempt | Derived; not a normal preference |
| Private folders | Inherit the explicit shared list; no guessed personal paths | Protected folders in the main editor; shared-list management remains under Profiles |
| Runtime/tool support paths | Existing native defaults plus qualified, narrowly enumerated paths | Derived, inspectable in technical details |
| Unknown or missing required control | Profile can be saved; affected execution is unavailable | Inline reason and repair action |
| New provider/model test | Never automatic | Explicit Run test button |

Do not guess which of Documents, Desktop or Downloads is private: this repository itself lives under Downloads. Start with an honest empty list, show “No private folders added”, and offer Add folder in the first profile's details. Never claim personal content is excluded before it has been configured. Preserve the harness's built-in credential protections. Do not read credentials or copy the whole home directory to discover settings. Supplying the installed client its authentication is separate from granting the model access to credential files; qualify any distinction the adapter claims.

Work in projects is an access intent, not an OS boundary. Writable scope is the execution workspace and explicitly writable references. Read scope includes those locations, read-only references, and required native runtime/tool support. Other project references are requested through the supported native mechanism or need a profile change; a working directory alone is not evidence of a read restriction. Report native rules that cover only direct file tools as such.

Review only disallows project writes and destructive actions. It may allow network and necessary temporary runtime writes. Never label a route Review only if its unrestricted shell can freely modify the project with no operative native restriction. If it can only instruct the model not to write, that mode is unavailable for the route.

The current unrestricted/full-access preset remains available under **Advanced → Native permissions**, with its current admission rules and an explicit effective summary. Its current selection remains visible in the main Access control. This release does not invent a new universal full-access contract or automatically convert old full-access profiles to Work in projects. Existing permissions cannot be combined with the new access object. Private-folder restrictions that cannot operate under an existing bypass mode are visibly “Not applied”; an operator must explicitly retain that mode or choose a supported restricted profile. A newly saved private-folder exclusion is not advertised as covering bypass profiles that do not enforce it.

## Simple profile editor

### Purpose and settled defaults

The single job is: choose an agent, let it do ordinary project work without approval interruptions, protect selected folders, and save reusable settings. The user explicitly delegated the remaining default choices on 11 September 2026 while stepping away. These are authored defaults, not an answer to the earlier optional question.

Keep the existing `tusker.agent-access/v1` schema. New ordinary profiles use `mode: work_in_projects`, `network: true`, `destructive_actions: deny`, no added references, no per-profile private paths, and the existing shared private list. This changes creation defaults only. Explicit saved `ask` values and profiles without `access` round-trip unchanged. There is no `yolo` field, generic rule language or new policy store.

“No prompts” means permitted work proceeds automatically and blocked requests return a denial through the native interface. The agent may continue other permitted work. If a blocked operation is necessary, report needs attention through the existing task surface; never spin, wait for an approval that cannot arrive, or claim the task completed. This is not automatic authorization to delete project folders or force-push. Advanced offers Ask before running for recognized destructive work; automatic destructive execution is outside this revision. Private paths and catastrophic targets cannot be approved away.

### Main form

Use Settings → Agents → Profiles and the existing editor, not a new screen, wizard or modal. At normal desktop scale the default editor should fit inside a 760px-tall content viewport without expanding Advanced or scrolling through diagnostics to reach Save. This is a layout target for the empty/default form, not a fixed height or clipping rule. Long folder lists and large text may scroll normally.

```text
Add profile                                      Cancel

Agent       [Codex                   v]
Model       [Discovered default      v]

Access      [Work in projects        v]
Edits, commands and internet run automatically.
Destructive actions are blocked.

Protected folders                        Add folder
No protected folders configured.

Checking compatibility…

> Advanced

                                    [Save profile]
```

At 390px, preserve the same order and one column. Buttons and path controls wrap; do not create a different settings flow:

```text
Add profile                       Cancel
Agent  [Codex                          v]
Model  [Discovered default             v]
Access [Work in projects               v]
Edits, commands and internet run
 automatically. Destructive actions
 are blocked.
Protected folders          Add folder
No protected folders configured.
Checking compatibility…
> Advanced
                         [Save profile]
```

The main form has only Agent, Model, Access, Protected folders, one compatibility status and Save/Cancel. Use the installed catalog's explicit model and reasoning defaults; do not pin today's model names. If no model is marked default, keep selection empty with Choose model rather than silently choosing the first or most expensive entry. Existing selected models survive discovery refresh. Reasoning remains editable under Advanced and uses the catalog's explicit default when known; unknown required choices get one inline action to that field.

Access ordinarily offers Work in projects and Review only. Existing native profiles show their actual current preset as an additional current-value choice. Full access is visibly identified when active, with one short sentence: “Protected folders do not apply in this mode.” The native preset selector remains in Advanced and keeps existing route admission. Do not silently enroll a native profile by opening it, changing its model, changing internet or adding a folder. Offer an explicit Use project access action adjacent to the incompatible control; only that action changes the draft representation. Provide a draft-only way back to the original native settings until Save. Cancellation changes nothing.

The summary is conditional: default project work says the two sentences above; `ask` says “Edits, commands and internet run automatically. Ask before destructive actions.” Offline replaces the internet claim with “Internet off.” Review only says “Read project files; project writes are blocked,” with the actual supported internet choice. An old native workspace preset must not be labeled as though new folder restrictions are active. Do not derive effective guarantees from a preset label.

### Protected folders and explicit saving

“Protected folders” is the user-facing label for existing `private_folders`, meaning no agent reads or writes through the qualified native tool surface. Keep it visible because it is the operator's main concern. Display full selectable paths, a short final-component label if useful, and text indicating Inherited or This profile. Do not guess Documents, Desktop or Downloads; the active repo itself is under Downloads. An empty list must remain explicitly empty.

Add folder reuses an existing picker if available; otherwise reveal one labeled absolute-path field inline with Add and Cancel. Do not build a native picker bridge. Validate on entry without scanning contents: absolute path, canonical/symlink-aware overlap, duplicates, workspace/private-ancestor conflicts. A private descendant of the workspace is valid. Invalid input remains editable and is not discarded.

Edits in this form change only the profile draft. Add/remove of a profile-specific path is staged until Save. Inherited shared exclusions cannot be removed here; show their existing shared-management destination as “Manage shared folders.” That remains a separate, explicit shared-settings save and names the affected profiles. Do not auto-save a global restriction from the profile editor or build a second shared-list authority.

Save uses the existing expected-revision API and preserves profile ID, model, reasoning, tier references and per-project override semantics. Successful saves close the editor and update the profile card. Save errors preserve every field; revision conflict offers reload/reapply instead of overwriting another editor. Cancel discards draft additions/removals, including a native-to-project transition. Saved changes apply to the next launch; running attempts retain their pinned policy. Saving never starts an agent or a paid test.

### Advanced and defaults recovery

One closed-by-default Advanced disclosure holds editable Internet On/Off; Destructive actions Block without prompting / Ask before running; Reference folders, additions default Read only with explicit Read and write; Reasoning; Display name; Eligible tiers; Native permissions for existing routes; and Check setup / Run test plus collapsed Technical details. Use existing controls and settings storage. A native preset is mutually exclusive with the access object; no “Full access plus protected folders” combination is allowed without an independently supported native mapping.

The destructive selector is disabled in Review only with a concise explanation. Do not offer an Automatic option with a no-op handler. Mode changes must preserve user-authored values for switching back within the draft; Review only tightens the effective policy rather than destroying the saved preference. Do not silently force internet off while displaying On. If the route cannot satisfy the selected combination, report that combination as unsupported.

Offer Use recommended defaults inside Advanced. It stages project mode, internet on and destructive Block; preserve protected folders and reference scope. It must not erase security choices, profile identity or tier assignment. Show what changed and let Cancel abandon it. Existing user values are never reset by refresh, model changes or application upgrade.

Remove the five summary cards, duplicated internet control, behavior badges that repeat their labels, always-expanded Agent adapter section, routine “draft” explanations and repeated migration warnings. Technical details may show rule examples, native mechanisms, exact route/executable/version, checking time and qualification limits. No color-heavy warning card for normal Review only, empty private list or untouched defaults.

### Compatibility and effective state

Use the same existing resolver/result DTO as launch and explicit Check setup. Run a bounded, provider-free local setup check when the editor opens with sufficient data and after relevant draft changes settle. Debounce edits, key results to the whole draft plus shared-private revision, route/executable identity and version, and ignore superseded responses. Never reread credentials, spawn a coding agent, run conformance canaries, write test sentinels or make a model request automatically. Reuse the existing setup-only endpoint after verifying it has those bounds; do not substitute full conformance if it does not.

Persist the returned access report in editor state. Distinguish setup results from explicit live-test results. Changing folders, mode, internet, destructive policy, agent/model or shared revision immediately invalidates the relevant result; a late response for the old draft cannot restore it. No indefinite “Needs route check” placeholder after a successful setup check. A checked profile whose project is chosen later says “Compatible; project folders checked at launch,” not “This project is protected.”

Show one status: Checking compatibility; Settings supported; Needs setup; or the first actionable blocking sentence, with additional details collapsed. Use Settings supported only for required selected controls that the exact route can affirmatively provide; do not infer live qualification from generated arguments. Unsupported optional capabilities do not make the profile unavailable. A required unsupported/advisory/missing control does. Never silently drop exclusions, substitute writable references, switch provider, change network, or use bypass permissions to clear a blocker.

Example: “This Muse route cannot exclude these folders. Saved profile will be unavailable for runs.” Keep Save available for valid configuration and show Saved · Needs setup afterward. Invalid fields prevent Save. Removing an exclusion is a deliberate user edit, not an automatic repair or recommended downgrade. The app must never present a generic “Protected” badge while compatibility is unknown.

### Common across agents; precise about limits

All providers use the same authored settings, labels, defaults and Save semantics. CLI and ACP are transports, not permission levels. The adapter translates the shared policy into the exact installed route's native settings/hooks and reports supported/unsupported controls. The shared command policy determines automatic/ask/block precedence; provider parsing and operative interception coverage remain explicit. Native support varies with executable, version and host OS. No separate Codex/Claude/Muse settings form and no common claim beyond the verified route.

Source inspection found native mappings that declare Codex/Muse private exclusions unsupported and a Claude workspace compiler that still rejects project presets. Do not paper over these limits in this UI task. Successful saving and fixture rendering do not prove the operator's automatic-work-plus-private-folders use case is executable. Existing native-mapping and qualification work (AAC-T-0002, AAC-T-0003, AAC-T-0007) owns that proof. The completion report must say which exact routes can actually run this combination and which remain unavailable/NOT RUN. Supporting no route cannot be called end-to-end product completion.

This is trusted-harness protection through qualified native controls, not isolation of a malicious installed client or arbitrary script internals. Keep that limit once in Technical details; show a particular unsupported protection prominently when it prevents execution.

### Visual and implementation constraints

Reuse current React controls and `src/styles/app.css`; no global palette, typography, navigation or font-scale redesign. Desktop body 13px minimum, labels 12–13px, heading 17–18px; important explanatory content uses `ink-soft`, not faint text. Use the existing system font. Group with spacing and one divider instead of nested cards. The signature is the editable Protected folders list, not an adapter diagram. No added motion; preserve reduced-motion preferences.

Reference token pairs from the inspected stylesheet: light raised #ffffff / ink-soft #30343b; dark raised #161718 / ink-soft #d0d6e0; light accent #5558e0 / accent-ink #ffffff; dark accent #e4f222 / accent-ink #08090a. Measure contrast in the built UI; token values are not rendered proof. Maintain text contrast at least 4.5:1, meaningful control/focus contrast at least 3:1, visible keyboard focus, accessible names and text statuses. Keep desktop targets at least 28px high and narrow controls at least 44px. At 390px and 200% zoom, no horizontal document overflow, clipped paths, overlapping buttons or unreachable Save; vertical scrolling is allowed.

HIG rationale: settings.md › Best practices says “Minimize the number of settings you offer”; disclosure-controls.md › Best practices says “Use a disclosure control to hide details until they’re relevant”; entering-data.md › Best practices calls for dynamic validation. These support this small form. The exact layout and default block policy are product judgments under the operator's delegated design choice.

### Junior implementation handoff and proof

Implement this as one bounded refinement of the existing profile editor and its setup/default handling. Do not rewrite the runner architecture or reopen the original seven-story implementation. Read this section, Defaults and scope, and the decision log D8 first. Existing flow: ProfilesSection.openNew/profileDraft → editor draft → modelProfileSet / setup-only runnerConformance → serve_model_levels / serve_runner_conformance → resolveAccess / runner preparation. Downstream profile cards and task route previews must retain the same authored values and eligibility.

Acceptance and exact verification scenarios are in the follow-up Tusker task. Required proof covers new defaults; existing ask/full-access/model-only round-trip; explicit transition/cancel/save/reopen; profile private-folder add/remove and inherited restrictions; revision failure; automatic setup without paid calls; stale-response races; unsupported optional versus required controls; review-only internet truth; blocked requests creating no approval; supported fixture and unsupported provider states; keyboard, 390px and 200% zoom. Update current serve-ui/runner docs only for implemented behavior and keep native/live qualification separate. Use the existing browser harness and sentinel fixtures; no new test platform.

## Task execution journeys

### 3. Start work

The task's existing profile selector shows a short summary: “Daily coding · Work in projects · Internet on”. Details are available without leaving the task. Derive the actual working directory from the selected execution workspace, including a worktree; never mistakenly grant write access to the entire project-home parent. Additional write roots remain explicit.

A qualified, supported profile starts without an access confirmation dialog. Missing controls appear in the existing needs-attention/route-preview surface, with a precise repair link to the profile. Persist the requested and effective policy with the attempt. Task packets may explain the rules, but packet text is not enforcement. All start/test/resume paths use the same resolver; no CLI-only or UI-only shortcut.

### 4. Handle one exceptional action

Reuse the existing human-action card and request feed. Do not create a separate approval inbox.

```text
Approval needed                              Daily coding · Claude
Discard uncommitted changes
Project: Tusker
Command: git reset --hard HEAD
Folder:  /…/tusker-worktree
Reason: This can discard work that is not committed.

[Block action]                                [Allow once]
```

Show the exact immutable command/tool arguments, working directory, resolved targets when available, requesting agent and relevant consequence. Do not manufacture a target when the adapter cannot resolve it. Long content expands; it must not hide flags that change meaning. Use action labels, never Yes/No. Neither Enter in the surrounding page nor dismissing the card grants permission. Escape/dismiss leaves a live request pending; Block action denies it. No session-wide Allow all or always-approve checkbox in this release.

At most one actionable card per request. Repeat delivery updates the existing card. The live request follows the provider's lifecycle; when the process/request expires, the card becomes informational with Retry through the existing run action. An approval for an expired process does not replay a command. The agent may continue after a denial if its native protocol supports that. A terminal denial becomes needs attention, not an endless restart loop.

## HIG application and accessibility

This is the existing served desktop UI, not a macOS Settings redesign. Apply the local Apple design guidance with the existing semantic tokens and controls:

| Guidance | Decision in this feature |
| --- | --- |
| Settings → Best practices: “Minimize the number of settings you offer.” | Two ordinary modes; derive workspace and temporary paths; hide technical mappings. |
| Entering data → Best practices: “When possible, offer choices instead of requiring text entry.” | Choose a registered project and a read/write option; typed paths are the fallback. |
| Alerts → Best practices: “Use alerts sparingly.” | Inline setup errors; exceptional irreversible actions use the existing approval card. |
| Generative AI → Introduction: “Set clear expectations about what your AI-powered feature can and can’t do.” | Show unsupported controls and native-rule scope; never label advisory rules as isolation. |
| Layout → Visual hierarchy | Group access settings; progressively disclose detail; make Save the form's primary action. |
| Accessibility → keyboard and visual presentation | Real labels, logical focus order, visible focus, text status alongside color, announced errors. |

Use existing typography and colors; supporting permission text must remain readable rather than the current tiny technical-label style. Design targets are 14 px body and at least 12 px supporting text, 32 px desktop controls and 44 px touch targets where applicable; these are this product's choices, not claims of a universal Apple web requirement. Meet 4.5:1 normal text contrast and 3:1 meaningful control boundaries/focus. Verify at desktop width, 390 px width and 200% zoom with no clipped actions or horizontal form scrolling. Preserve keyboard navigation and return focus after dialogs/disclosures. Do not add animation; honor reduced motion for reused components. Screenshots alone are insufficient accessibility proof.

Required UI states: empty list, existing legacy profile, default new profile, inherited/private-folder edit, read-only reference, route change with missing control, checking, ready, stale qualification, test running/success/failure, unavailable credentials, request pending/allowed/denied/expired, stale-save conflict and narrow layout.

## Developer contract: one small schema

Extend the existing profile type and settings authority. Do not add a reusable policy-object database, policy marketplace, generic capability-driven form builder or a second runner registry. A profile has either the new `access` object or the legacy `permission_preset`, never both as active authorities.

Proposed versioned authored shape (field names are implementation recommendations):

```yaml
# In the existing settings document, not a new store.
private_folders: []

profiles:
  daily:
    harness: claude
    model: chosen-model
    access:
      schema: tusker.agent-access/v1
      mode: work_in_projects             # work_in_projects | review_only
      network: true
      destructive_actions: deny         # new default; ask | deny; review_only resolves deny
      folders:
        - path: /absolute/path/to/library
          access: read                  # read | write
      private_folders: []                # additions to the shared list
```

Missing `access` on an existing record means legacy behavior, not default enrollment. Only creation of a new ordinary profile applies the new defaults. Keep the existing global/project-profile precedence, identity and revision conflict checks; project overrides can add restrictions, and any widening of a profile's folder grants is an explicit authored change with an effective summary. Do not add task-level arbitrary permission overrides in this release. Unknown versions/fields/access values fail validation. Runtime-derived workspace, temporary directory and required support paths never get copied into reusable profile defaults.

Path validation uses absolute host paths, canonical existing ancestors, symlink-aware overlap checks and platform case behavior. Never expand arbitrary shell expressions. Private-folder denial wins over read/write grants, including a private descendant inside a project. If a private folder contains the execution workspace, show a configuration conflict before launch. An exception must be implemented by an operative native exclusion, not subtracting it from a UI list. Revalidate at launch and resume; symlink/reparse and runtime races remain scoped to the native enforcement claim. Do not promise race-free access control from string validation.

Resolve policy in this order:

1. Existing settings precedence and explicit profile selection.
2. Shared private list plus profile additions, execution workspace, explicit references and a fresh run temporary directory.
3. Mode and network intent; review-only tightens writes/destructive actions.
4. Exact harness, executable identity/version, transport and installed capability evidence.
5. Native compilation, required-control comparison, then an immutable effective report.

Private denial and catastrophic-target denial beat destructive approval, which beats routine allowance. Disabling internet must be enforceable across the claimed tool surface; blocking only a web-fetch tool while leaving unrestricted shell egress is not “Internet off”. Run temporary access is scoped to its own canonical directory, not all of /tmp. Cleanup uses Tusker-owned directory identity and an anchored operation; an untrusted supplied path can never become a recursive cleanup target.

## Capability and effective-result contract

Extend the existing typed runner catalog and PreparedLaunch/EffectivePolicy. Keep the number of controls fixed to the shipped policy fields. The catalog is not a bag of arbitrary UI widgets.

```typescript
type AccessControl =
  | 'workspace_write' | 'reference_read' | 'reference_write'
  | 'private_read_deny' | 'private_write_deny' | 'network'
  | 'destructive_approval' | 'review_only';

type ControlSupport = {
  control: AccessControl;
  mechanism: 'native_setting' | 'native_hook' | 'advisory' | 'unsupported';
  coverage: string;             // bounded factual description of tools covered
  evidence: string[];           // qualification case IDs / report references
};

type ResolvedAccess = {
  requested: AgentAccessV1 | LegacyPermissionPreset;
  effective: EffectivePolicy;   // extend existing type; canonical resolved paths
  controls: ControlSupport[];
  state: 'ready' | 'needs_setup' | 'unsupported' | 'stale';
  issues: { code: string; field: string; message: string; remedy: string }[];
  fingerprint: string;          // effective intent + route + relevant revisions
};
```

Catalog evidence is keyed by harness ID, transport, executable identity/version and relevant configuration; include qualification time/case result using existing discovery/conformance provenance. A handwritten `native_containment: true` or a help-page claim is not a passing result. Reuse existing cache invalidation; no new periodically polling service. A binary/config change marks relevant conformance stale and prevents using stale evidence to claim required support; local checks remain free. Do not require a fresh paid model test on every run.

`ready` means requested controls are operative for their explicitly documented native coverage. It never means the client is contained or every interpreter/script is inspected. Support statuses remain distinct: native setting, native hook, advisory, unsupported. A required setting implemented only as advisory/unsupported makes that new profile unavailable until changed. The report is the same source for UI, CLI, runtime events and stored attempt evidence.

Reuse the existing profile read/save, catalog, route preview and conformance APIs in `serve_model_levels.go`, `serve_runner_conformance.go` and `runner_route_preview.go`. Extend their typed DTOs with authored access, resolved report, shared-private revision and stable issue codes. Keep expected-revision writes, authentication/host authorization and existing errors. Check setup uses discovery/resolve only; Run test uses the existing explicit conformance path. Story 1 must publish exact final request/response examples in its contract artifact before dependent implementation; do not fork separate schemas per UI and CLI.

## Native mappings and destructive actions

Reuse `internal/runner` prepare/policy/conformance/event interfaces and existing native hooks. Do not write a universal command firewall. Adapters compile the same intent into supported flags/config/hooks and return unrepresented requirements as issues. Unsupported syntax or ambiguous targets in a request being classified as destructive must never produce an accidental allow.

| Route | Integration requirement |
| --- | --- |
| Codex CLI | Qualify the installed version's supported sandbox/permissions configuration and approval path. Never mix incompatible legacy sandbox flags and newer permission-profile settings. Full-access native bypass cannot stand in for project mode. |
| Claude CLI | Reuse native permission rules and the existing PreToolUse hook seam. An additional directory that grants writes cannot represent a read-only reference by itself. Resolve conflicts with native deny/ask/allow precedence and report tool coverage. |
| Existing `muse` | Preserve the existing Codex-profile route, configuration and saved references. Label it “Muse via Codex” where disambiguation matters. |
| New `muse_cli` | Execute the installed Muse CLI directly through a separate dialect/adapter. Qualify approval mode, permission profile, shell/network/write scope and event handling independently. Model discovery via Muse serve does not prove execution readiness. |
| ACP | Reuse the same resolved policy and normalized permission request event. Qualify which operations the agent delegates and whether it can await/respond. ACP transport alone grants no interception or confinement guarantee. |
| Future Devin, Grok, OpenCode, Hermes, others | Add only when selected for onboarding. Supply the bounded declaration, native mapping, output/event adapter and common tests. Remote routes must declare host-side path mapping; local folder strings cannot imply remote filesystem control. |

Muse's direct CLI and its `serve` MSP endpoint are different execution/control surfaces. MSP is not ACP. This release adds direct CLI; it does not build an MSP transport merely because it exists. Never call a different agent when a selected executable is absent.

Recognized destructive operations include recursive deletion, discarding uncommitted work (`git reset --hard`, destructive `git clean`, restore/checkout of modified files), force push and supported destructive database commands. Ordinary editor changes and supported ordinary file deletions within the workspace continue automatically. Recursive deletion of a build-output directory is still classified as destructive and follows Block/Ask; do not add a fragile cache-folder exception system. Git backups do not cover all local changes, external databases or published history.

Native rules/hooks deny catastrophic targets (filesystem root, home root, project/worktree root, private folders and recognized disk-format/raw-device operations) rather than offering one-click approval. Destructive operations outside writable roots are denied. A destructive action inside a writable root asks, or is denied when that setting is Block. Exact network-side targets such as force-push destinations must be displayed when available; workspace location is not their impact boundary. If a database command cannot be safely scoped, deny it with a repair explanation; do not claim arbitrary database backup/recovery.

Reuse native parser/rule semantics and existing project helpers where sound. Test quoting, chained commands, relative paths, symlinks, variable/option forms and command substitution for the claimed coverage. Do not advertise the present regex denylist as complete enforcement, or build an open-ended shell interpreter to chase every spelling. It is acceptable to state that `python script.py` is outside command-pattern coverage; it is unacceptable to label that coverage universal or convert an unparseable known destructive request into automatic permission. No model classifier in Tusker.

## Approval and execution lifecycle

One normalized request contains a stable ID, attempt/execution/native-session binding, route identity, policy fingerprint, tool name, exact bounded arguments, working directory, resolved targets when known, reason, offered native response options and expiry/liveness. Reuse the runtime store and human-action records; no second approval service.

A response is allow-once or deny and includes the expected request revision. Resolve the exact native option ID from the request; never invent an ACP option. Validate request ownership, state, payload identity, current attempt/session and policy binding atomically before returning the native decision. Duplicate responses are idempotent; conflicting or stale responses fail visibly. The request cannot mutate into a different command after approval.

When a route can hold a request open, wait through its supported callback without keeping an execution lease falsely active forever. On restart, reconcile process/request liveness; recover only a native pending request that is demonstrably still the same. Otherwise mark it expired and require a new attempt/request. No shell-command replay from a stored approval. New launches and resumed attempts recompute policy and invalidate incompatible outstanding requests; a running attempt otherwise retains its launch snapshot.

For an unattended route without a supported interactive callback, deny the exception through the native mechanism and report needs attention if work cannot continue. Do not scrape a terminal prompt, pipe a speculative “yes”, wait forever or change to bypass mode. A route may qualify deterministic deny-on-exception without qualifying a live Approve button; reflect that difference in the UI and conformance report. Ordinary permitted work still proceeds unattended.

Existing human-action authorization applies. Workers cannot grant their own exceptions or modify global private exclusions. Persist a bounded decision audit with outcome and reason; redact secrets in displayed/stored arguments while preserving a secure payload binding used for response validation. Never write raw authentication tokens into the general event stream.

## Compatibility and source seams

This spec extends the profile-centered refinement in [[model-level-configuration]]. For newly authored `access` profiles, it adds a trusted-native-rules mode alongside the historical execution-boundary contract. It does not reinterpret a legacy preset or weaken existing full-access admission. The qualification/documentation story must explicitly reconcile this scope in [[runner-execution-boundary]] and the current runner docs when implemented.

Source baseline inspected 11 September 2026, with a shared dirty checkout preserved:

- `cmd/tusker/runner_profiles.go`: existing profile/preset configuration and deny-rule declarations; declarations alone do not establish enforcement.
- `cmd/tusker/model_levels.go`, `serve_model_levels.go`: profile identity, tier assignment, precedence and revision contracts.
- `cmd/tusker/runner_catalog.go`, `runner_conformance.go`, `runner_route_preview.go`: installed detection, typed options, Muse discovery, existing Muse-through-Codex route and preview.
- `internal/runner/types.go`, `policy.go`, `prepare.go`, `execute_acp.go`: common types, native compilation and permission-event plumbing. Existing unattended ACP handling denies requests.
- `cmd/tusker/acp_permission.go`, `runner_claude_live.go`, `runtime_store.go`, `serve_actions.go`: existing approval evaluator, native hook, runtime persistence and operator action seams.
- `internal/serve/ui/src/features/settings/app/ProfilesSection.tsx`, `AgentsSection.tsx`, and `features/human-action/HumanActionCard.tsx`: reuse current settings and action surfaces.

Follow current shared ownership before editing these files. File maps in the stories are starting points; trace all callers, especially tests, launch, resume and daemon admission, before claiming common enforcement.

## Qualification and delivery

The emitted plan is inert and uses existing task/review/landing machinery. These are build stories, not authorization to dispatch workers or change project automation.

Shared acceptance cases must cover: defaults; exact legacy round-trip; model-only edits; private precedence; private/workspace conflict; read-only reference with a write-capable-only provider; offline/network control gaps; temp ownership; route/version invalidation; direct Muse versus Codex route; required advisory/unsupported rejection; ordinary automatic action; recognized destructive allow-once/deny; catastrophic target rejection; chained/quoted/symlink examples; unsupported interpreter coverage; no-callback denial; duplicate/stale/expired approval; process death/restart; policy changes between attempts; UI keyboard, narrow layout and async errors.

Run these with local fixture executables and sentinel files first, without touching actual personal folders, executing destructive commands or using paid model calls. Negative tests prove the claimed boundary by attempting operations against disposable fixtures, not by matching configuration strings alone. Adapter-specific native probes and explicitly authorized live model tests have separate rows with exact version/route and PASS, FAIL or NOT RUN. No tests matching zero cases count as proof. A doc claim cannot replace a live result, and an unavailable provider does not prevent provider-free core work.

Final implementation evidence includes a behavior/support matrix, exact native mappings and known gaps, before/after UI screenshots with keyboard findings, integrated fixture results, installed/live qualification status, and a concise new-provider onboarding recipe. Update current system docs only to behavior actually proved. Keep future gaps explicit.

## Deferred (not now)

- Tusker-owned OS sandboxing, containers, virtual machines and malicious-client containment.
- A custom model-based command approval classifier or a general shell interpreter.
- A separate policy store, generic rule editor, policy sharing or an approval inbox.
- Session-wide approvals, inferred auto-approval from prior clicks and live hot-swapping of running process permissions.
- A new universal full-access mode or automatic migration of existing bypass profiles.
- MSP execution, remote path mapping and additional providers beyond the named first adapters; the extension contract exists now, implementations follow demand.

## Build map

The [delivery plan](agent-access.plan.yaml) is imported as [W-0020](../work/waves/W-0020.md), with these stories. The wave remains disarmed; the operator will assign implementation to other agents.

| Story | Deliverable | Prerequisite |
| --- | --- | --- |
| [AAC-T-0001](../work/tasks/AAC-T-0001.md) | Shared contract, API examples and legacy migration | None |
| [AAC-T-0002](../work/tasks/AAC-T-0002.md) | Native Codex and Claude policy mappings | 0001 |
| [AAC-T-0003](../work/tasks/AAC-T-0003.md) | Direct Muse CLI with legacy route preserved | 0002 |
| [AAC-T-0004](../work/tasks/AAC-T-0004.md) | Exact-request approval lifecycle | 0003 |
| [AAC-T-0005](../work/tasks/AAC-T-0005.md) | Profile settings and folder controls | 0003 |
| [AAC-T-0006](../work/tasks/AAC-T-0006.md) | Task access summary and approval cards | 0004, 0005 |
| [AAC-T-0007](../work/tasks/AAC-T-0007.md) | Integrated qualification, onboarding recipe and current docs | 0006 |

Approval runtime and profile UI can be implemented independently after the native contract is settled; shared-file changes remain ordered by the recorded dependencies. The final qualified system is the completion condition, not just a passing schema or a settings screenshot.

<!-- tusker:delivery-import:2768713b91dd5701:begin -->

- `[[AAC-T-0004]]` implements delivery source `approval-lifecycle`.
- `[[AAC-T-0006]]` implements delivery source `approval-ui`.
- `[[AAC-T-0001]]` implements delivery source `contract`.
- `[[AAC-T-0002]]` implements delivery source `native-mappings`.
- `[[AAC-T-0003]]` implements delivery source `native-muse`.
- `[[AAC-T-0005]]` implements delivery source `profile-ui`.
- `[[AAC-T-0007]]` implements delivery source `qualification`.

- `[[W-0020]]` is the imported delivery wave.

<!-- tusker:delivery-import:2768713b91dd5701:end -->

<!-- tusker:delivery-import:c9efe331fb88cb99:begin -->

## Work streams

- `[[AAC-T-0008]]` implements delivery source `automatic-profile-editor`.

- `[[W-0022]]` is the imported delivery wave.

<!-- tusker:delivery-import:c9efe331fb88cb99:end -->
