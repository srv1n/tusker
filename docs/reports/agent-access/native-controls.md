# Agent access native controls

This report records the provider-neutral mappings implemented by the runner
boundary. A setting is listed as operative only for the native tools covered
by that setting; it is not a claim of malicious-client containment.

| Route | Operative native mapping | Explicit gap |
| --- | --- | --- |
| Codex CLI | `--sandbox` plus `approval_policy` and `sandbox_workspace_write.network_access`; extra writable roots use `--add-dir` | Codex CLI has no qualified read-only external-root grant or native private-folder exclusion. Those requests are unavailable rather than widened. |
| Claude CLI | Existing PreToolUse hook remains the native hook seam; read-only uses plan/read tools | Workspace-write and external read-only references remain unavailable where the installed route cannot prove the required boundary. `bypassPermissions` is never used as bounded enforcement. |
| Direct Muse CLI | `muse exec --json`, `--workspace`, `--approval-mode`, `--sandbox-network`, `--disable-write`, `--disable-shell`, and `--disable-web-tools` | Direct Muse has no qualified private-folder exclusion or read-only external-root grant. Headless approval callback support is not claimed. |

The shared resolver rejects a selected required control whose mechanism is
unsupported or advisory. It preserves the legacy permission-preset path when
no `access` object is authored.

## Local qualification

- Codex executable: `/Users/sarav/.bun/bin/codex`, `codex-cli 0.153.4`.
- Claude executable: `/Users/sarav/.local/bin/claude`, `2.1.261 (Claude Code)`.
- Direct Muse executable: `/Users/sarav/.local/bin/muse`, `Muse Code 1.1.1 (1.1.1-R2514.1)`.
- Unit coverage: `TestAgentAccessNative` and `TestAgentAccessDestructive` use
  provider-free argv/receipt fixtures and disposable paths.
- Paid/live model qualification: NOT RUN.
