# Agent access contract

This document is the shared contract for settings, route preview, conformance,
launch and resume. It is intentionally a small versioned profile object; native
adapters report their bounded support separately.

## Authored shape

```yaml
automation:
  private_folders: []
  profiles:
    daily:
      harness: claude
      model: chosen-model
      access:
        schema: tusker.agent-access/v1
        mode: work_in_projects
        network: true
        destructive_actions: ask
        folders:
          - path: /absolute/library
            access: read
        private_folders: []
```

`access` is optional. A profile with no `access` remains a legacy
`permission_preset` profile, including full-access profiles. The two authorities
are mutually exclusive. Defaults apply only while creating a new ordinary
profile: `work_in_projects`, workspace write, network on, routine destructive
actions ask, empty folder/private lists. Reads never enroll an old profile.

## Effective response

All consumers use the same response shape:

```json
{
  "requested": {"schema":"tusker.agent-access/v1","mode":"work_in_projects","network":true,"destructive_actions":"ask","folders":[],"private_folders":[]},
  "effective": {"preset":"workspace-write-network","filesystem":"workspace-write","network":true,"approvals":"ask","workspace":"/workspace","temporary_directory":"/workspace/.tusker/tmp","access_fingerprint":"sha256:..."},
  "folders": [{"path":"/workspace","access":"write"}],
  "references": [],
  "private_folders": [],
  "controls": [{"control":"workspace_write","mechanism":"native_setting","coverage":"provider workspace tools","evidence":["fixture.workspace-write"]}],
  "state":"ready",
  "issues": [],
  "fingerprint":"sha256:..."
}
```

`state` is `ready`, `needs_setup`, `unsupported`, or `stale`. A required
control backed only by `advisory` or `unsupported` support produces
`access_control_unsupported`; it cannot silently fall back. The fixed controls
are `workspace_write`, `reference_read`, `reference_write`,
`private_read_deny`, `private_write_deny`, `network`, `destructive_approval`,
and `review_only`.

## Command policy authority

The resolved report also carries `command_policy`, a fixed projection shared by
settings, route previews, and native callbacks. It has no authored regex or
general command editor. Routine project edits, builds/tests, `git status`,
`git add`, and `git commit` are `automatic`. Recognized destructive requests
such as bounded recursive deletion, `git reset --hard`, destructive `git clean`,
and force push are `ask` when `destructive_actions: ask`, otherwise `block`.
Catastrophic targets, configured private folders, and targets outside writable
scope are always `block`; Review-only blocks all project writes. Mandatory
blocks are evaluated before an allow-once request and cannot be overridden.

The historical `automation.denylist` remains readable for configuration
round-tripping and provenance, but its regexes are not runtime enforcement.
Native adapters classify their supported requests and delegate behavior to the
fixed command-policy evaluator. This preserves legacy profiles that never had
a denylist consumer while preventing previews from implying that declarations
alone protect execution.

## Resolution and validation

Profile/layer precedence and expected revisions remain the existing settings
authority. Runtime resolution then derives the execution workspace, an
attempt-owned temporary directory, explicit references and provider support
roots. Paths must be absolute and are canonicalized through their longest
existing ancestor. Symlink-aware overlap is used for containment checks; a
private path wins over read/write grants. A private ancestor containing the
execution workspace is a configuration conflict. References are read-only and
review-only forces writes and destructive actions to deny. Runtime-derived
paths are never persisted as profile defaults.

Unknown access fields, versions and values fail closed. Stable issue codes are:

| Code | Meaning |
| --- | --- |
| `access_workspace_invalid` | Workspace is missing, relative, or not a directory. |
| `access_workspace_private_conflict` | Private scope contains the execution workspace. |
| `access_folder_invalid` | Folder grant is not an absolute/canonicalizable path. |
| `access_reference_invalid` | Reference cannot be resolved as an absolute path. |
| `access_private_path_invalid` | Private folder is not an absolute/canonicalizable path. |
| `access_control_unsupported` | Required route control is advisory or unsupported. |
| `access_resolution_required` | Conformance needs the runtime resolver/context. |

## API seams

The existing `/api/models` profile read/save response carries `access` and
`private_folders`; profile writes keep expected-revision checks. The existing
`/api/models/catalog`, `/api/runner/conformance`, and route-preview response
carry `access_controls`, `access`, and `resolved_access` fields. No policy store
or provider-specific UI schema is introduced. `private-folders` writes use the
same revision-checked settings document and are non-destructive.

## Migration matrix

| Input | Result |
| --- | --- |
| Existing profile with `permission_preset` | Round-trips unchanged. |
| Existing profile with no `access` | Remains legacy; no defaults are injected. |
| New profile | Receives the ordinary-profile defaults at creation. |
| Model/display/tier edit of an access profile | Preserves the access object and does not widen scope. |
| Access plus active `permission_preset` | Rejected as a configuration error. |
| Project override | Merges through existing project/local precedence and revision checks. |
| Disabled profile | Remains disabled; access resolution does not auto-start or re-enable it. |
