---
title: "Runners and ACP"
subject: runners-and-acp
part_of: overview
status: canonical
---

# Runners and ACP

A runner executes one claimed task. The runtime records the runner profile,
attempt, workspace, heartbeat, session, and result.

## Runner kinds

The current runner profile code accepts:

- `codex_acp` for local Codex through the ACP adapter;
- `codex_exec` for an explicit direct Codex profile;
- `claude-code` for the Claude Code stream protocol; and
- `codex_cloud` for a configured remote Codex environment.

This repository enables `codex_acp`, `codex_exec`, and `claude-code`. Its
default is `codex_acp`. `codex_cloud` needs explicit commands and an environment
ID before it can run.

## ACP setup

Run `tusker acp setup --vault ./.tusker --json` to install and seal the adapter.
Run `tusker acp doctor --json` to check the adapter, manifest, authentication,
and bundle digest.

The ACP run uses the admitted adapter path and exact runner profile. It does
not choose a new adapter after a prompt starts.

## Failure rules

A live attempt needs an identity and a fresh heartbeat. A launch with no
session identity fails or interrupts. Retry limits and stall limits come from
the resolved project policy.

## Code sources

- `cmd/tusker/runner_profiles.go`
- `cmd/tusker/runner_acp_codex.go`
- `cmd/tusker/runner_acp_codex_live.go`
- `cmd/tusker/runner_claude_live.go`
- `cmd/tusker/runner_codex_cloud.go`
- `internal/acp/`
- `.tusker/WORKFLOW.md`
