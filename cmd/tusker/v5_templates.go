package main

import (
	"os"
	"path/filepath"
	"strings"
)

func defaultV5WorkflowMarkdown(date string) string {
	return defaultWorkflowMarkdown()
}

func writeDefaultV5Docs(vaultPath, date string) error {
	docs := []struct {
		Rel        string
		Title      string
		Audience   string
		Mode       string
		AgentLayer string
		Kind       string
		Domain     string
		Node       string
	}{
		{"docs/spec/v5-overview.md", "Tusker v5 implementation spec", "developer", "explanation", "capsule", "reference", "adoption", ""},
		{"docs/spec/v5-adoption.md", "Tusker v5 adoption guide", "developer", "how-to", "none", "guide", "adoption", ""},
		{"docs/reference/cli.md", "Tusker v5 CLI surface", "developer", "reference", "capsule", "reference", "cli", ""},
		{"docs/reference/docs-pipeline.md", "Docs routing, impact hook, and publication model", "developer", "explanation", "capsule", "reference", "docs-system", "tusker/docs-system"},
		{"docs/reference/obsidian.md", "Obsidian and view requirements", "developer", "how-to", "none", "guide", "obsidian", ""},
		{"docs/reference/runtime.md", "Runtime state, events, and generated caches", "developer", "reference", "capsule", "reference", "runtime", ""},
		{"docs/reference/skill.md", "Skill and AGENTS guidance", "developer", "reference", "capsule", "reference", "skill", ""},
		{"docs/reference/templates.md", "Template contract", "developer", "reference", "capsule", "reference", "schema", ""},
		{"docs/reference/validator.md", "Validator rollout and failure codes", "developer", "reference", "capsule", "reference", "schema", ""},
		{"docs/agents/use-tusker.md", "Agent recipe: using Tusker", "agent", "how-to", "standalone", "runbook", "skill", "agents/tusker-skill"},
	}
	for _, doc := range docs {
		path := filepath.Join(vaultPath, filepath.FromSlash(doc.Rel))
		if fileExists(path) {
			continue
		}
		node := doc.Rel[len("docs/"):]
		node = node[:len(node)-len(".md")]
		if doc.Node != "" {
			node = doc.Node
		}
		content := replaceTemplateTokens(defaultV5DocTemplate(), map[string]string{
			"{{node}}":                node,
			"{{title}}":               doc.Title,
			"{{publish_path}}":        node,
			"{{publish_description}}": doc.Title + ".",
			"{{date}}":                date,
		})
		if doc.Audience == "agent" || doc.AgentLayer == "standalone" {
			content = replaceTemplateTokens(defaultV5AgentDocTemplate(), map[string]string{
				"{{node}}":                node,
				"{{title}}":               doc.Title,
				"{{publish_path}}":        node,
				"{{publish_description}}": doc.Title + ".",
				"{{date}}":                date,
			})
		} else if doc.AgentLayer == "capsule" {
			content = strings.Replace(content, "\n## Content\n", "\n## Agent capsule\n\n- Agent-facing notes, caveats, and automation cues for this page.\n\n## Content\n", 1)
		}
		data, body, err := parseFrontmatter(content)
		if err != nil {
			return err
		}
		data["audience"] = doc.Audience
		data["mode"] = doc.Mode
		data["agent_layer"] = doc.AgentLayer
		data["kind"] = doc.Kind
		data["domains"] = []string{doc.Domain}
		data["source_of_truth"] = []string{"_config/docs-map.yaml"}
		data["stale_when_paths"] = []string{"_config/docs-map.yaml"}
		if bodyOverride := defaultV5DocBody(doc.Rel); bodyOverride != "" {
			body = bodyOverride
		}
		content, err = serializeDocument(data, body, frontmatterOrderForType("doc"))
		if err != nil {
			return err
		}
		if err := writeText(path, content); err != nil {
			return err
		}
	}
	return nil
}

func defaultV5DocBody(rel string) string {
	switch rel {
	case "docs/spec/v5-adoption.md":
		return `# Tusker v5 adoption guide

## Goal

Move an existing Tusker vault onto V5 without hand-renaming notes or leaving old story/bug concepts behind.

## Existing repo repair

Run this from the repo root after installing or rebuilding the current Tusker binary:

` + "```bash" + `
tusker init --migrate-v5 --dry-run --vault ./tusker
tusker init --migrate-v5 --yes --vault-only --no-mount --vault ./tusker
tusker validate --vault ./tusker
tusker docs export --vault ./tusker --site ./site
tusker docs build --vault ./tusker --site ./site
tusker update --repo . --repo-only --no-bin
` + "```" + `

Use ` + "`--vault-only`" + ` when the repo already has its own ` + "`AGENTS.md`" + `, ` + "`CLAUDE.md`" + `, or project contract files and the goal is only to repair the Tusker vault.

## What migration changes

| Legacy shape | V5 shape |
|---|---|
| ` + "`type: story`" + ` | ` + "`type: task`, `kind: feature`" + ` |
| ` + "`type: bug`" + ` | ` + "`type: task`, `kind: bug`" + ` |
| ` + "`ABC-S-0001.md`" + ` | ` + "`ABC-T-0001.md`" + ` |
| ` + "`ABC-B-0001.md`" + ` | next non-conflicting ` + "`ABC-T-NNNN.md`" + ` |
| ` + "`epics/ABC/index.md`" + ` | ` + "`epics/ABC/ABC.md`" + ` |
| old wikilinks | rewritten to the new task IDs |
| missing docs map entries | added for published docs |

## Acceptance

- ` + "`tusker validate --vault ./tusker`" + ` exits with zero errors.
- No ` + "`*-S-NNNN.md`" + `, ` + "`*-B-NNNN.md`" + `, ` + "`Stories.base`" + `, ` + "`Bugs.base`" + `, or ` + "`story.md`" + ` files remain in the vault.
- ` + "`tusker list --vault ./tusker --type epic`" + ` shows the expected epic roster.
- ` + "`tusker docs build --vault ./tusker --site ./site`" + ` completes.

Warnings about missing V5 sections in old notes are migration debt, not a broken repo. Fix them when touching the note for real work.

## Rollback

By default migration creates ` + "`tusker.backup-v5-YYYYMMDD-HHMMSS`" + `. If a repo has a watcher that restores or deletes sidecar backups, rerun with ` + "`--no-backup`" + ` only after making your own git or filesystem checkpoint.
`
	case "docs/reference/cli.md":
		return `# Tusker v5 CLI surface

## Shape

The public CLI is small on purpose:

| Area | Commands |
|---|---|
| Setup | ` + "`init`, `update`" + ` |
| Work items | ` + "`new`, `list`, `show`, `search`, `compact`, `next`, `claim`, `status`, `evidence`, `verify`, `close`" + ` |
| Docs | ` + "`docs model`, `docs map`, `docs catalog`, `docs freshness`, `docs check`, `docs apply`, `docs noop`, `docs waive`, `docs export`, `docs dev`, `docs build`" + ` |
| Shared vaults | ` + "`vault set`, `vault status`, `vault mount`, `vault unmount`, `vault repair`, `vault move`" + ` |
| Health | ` + "`validate`, `reindex`" + ` |

## Existing repo migration

` + "```bash" + `
tusker init --migrate-v5 --dry-run --vault ./tusker
tusker init --migrate-v5 --yes --vault-only --no-mount --vault ./tusker
` + "```" + `

` + "`--migrate-v5`" + ` converts old stories and bugs into tasks, updates IDs and wikilinks, renames epic index files, installs V5 templates/views, and adds missing docs-map nodes for published docs.

## Work pickup

` + "```bash" + `
tusker next --vault ./tusker
tusker claim APP-T-0001 --as codex --vault ./tusker
tusker next --claim --as codex --vault ./tusker
` + "```" + `

` + "`next`" + ` returns only pickable work: ` + "`ready`" + ` or ` + "`rework`" + ` tasks with no unresolved ` + "`blocked_by`" + ` dependencies. ` + "`claim`" + ` assigns the task and moves it to ` + "`active`" + `. ` + "`draft`" + ` and ` + "`backlog`" + ` are intentionally not pickable.

## Note reading

Use ` + "`show`" + ` before opening a note:

` + "```bash" + `
tusker show ORC-T-0019
tusker show ORC-T-0019 --acceptance
tusker show ORC-T-0019 --evidence
` + "```" + `

` + "`show`" + ` defaults to the agent capsule: the first-screen summary, state, verification/close summaries, and next anchors. ` + "`--verification`" + ` shows verification frontmatter plus a small log tail; use ` + "`--section \"Verification log\"`" + ` only when the full log is needed. Use ` + "`--full`" + ` only when the capsule and exact sections are insufficient.

Use ` + "`compact`" + ` to trim old notes before they become model context:

` + "```bash" + `
tusker compact ORC-T-0019
tusker compact ORC-T-0019 --write
tusker compact --all --json
` + "```" + `

` + "`compact`" + ` dry-runs by default. It removes empty optional frontmatter and disposable placeholder sections such as empty ` + "`Execution plan`" + ` and creation-only ` + "`Work log`" + `; substantive decisions and evidence are preserved.

## Repo-local skill refresh

` + "```bash" + `
tusker update --repo . --repo-only --no-bin
` + "```" + `

Use this after pulling or rebuilding Tusker when the repository should carry the current agent skill bundle under ` + "`.agents/skills/tusker`" + ` and ` + "`.claude/skills/tusker`" + `.

## Docs pipeline

` + "```bash" + `
tusker docs map --json
tusker docs freshness --stale
tusker docs export --vault ./tusker --site ./site
tusker docs build --vault ./tusker --site ./site
` + "```" + `

The site output is generated. Author source docs in ` + "`tusker/docs/**`" + ` or registered repo docs, not in ` + "`site/src/content/docs/**`" + `.

## Shared Obsidian vault

` + "```bash" + `
tusker vault set --path /path/to/shared-obsidian-vault
tusker vault mount --repo /path/to/repo --vault /path/to/repo/tusker --name repo-name
tusker vault status
` + "```" + `

` + "`vault mount`" + ` creates a symlink at ` + "`<shared-vault>/<name>`" + ` that points to the repo-local Tusker tracker. Use this when one Obsidian workspace should monitor multiple project trackers.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | success |
| 1 | user input or command error |
| 2 | validation failure |
| 3 | filesystem or I/O failure |
`
	default:
		return ""
	}
}

func writeDefaultV5VaultTemplates(vaultPath, date string) error {
	replacements := map[string]string{
		"{{date}}": date,
	}
	templates := map[string]string{
		"epic.md":       defaultV5EpicTemplate(),
		"task.md":       defaultV5TaskTemplate(),
		"bug.md":        defaultV5BugTemplate(),
		"doc.md":        defaultV5DocTemplate(),
		"agent-doc.md":  defaultV5AgentDocTemplate(),
		"dashboard.md":  defaultV5DashboardNote(date),
		"cheatsheet.md": defaultV5CheatsheetNote(date),
	}
	for name, content := range templates {
		for key, value := range replacements {
			content = replaceTemplateTokens(content, map[string]string{key: value})
		}
		if err := writeText(filepath.Join(vaultPath, "_system", "templates", name), content); err != nil {
			return err
		}
	}
	_ = os.Remove(filepath.Join(vaultPath, "_system", "templates", "story.md"))
	return nil
}

func writeDefaultV5VaultViews(vaultPath string) error {
	views := map[string]string{
		"Epics.base":    defaultV5EpicsBase(),
		"Tasks.base":    defaultV5TasksBase(false),
		"BugTasks.base": defaultV5TasksBase(true),
		"Docs.base":     defaultV5DocsBase(),
	}
	viewDir := filepath.Join(vaultPath, "_system", "views")
	for name, content := range views {
		if err := writeText(filepath.Join(viewDir, name), content); err != nil {
			return err
		}
	}
	for _, stale := range []string{"Stories.base", "Bugs.base", "Attestation.base", "Verification.base", "Orchestration.base"} {
		_ = os.Remove(filepath.Join(viewDir, stale))
	}
	return nil
}

func defaultV5DashboardNote(date string) string {
	return `---
title: "Vault Home"
type: "note"
created: "` + date + `"
updated: "` + date + `"
tags: ["dashboard"]
---

# Vault Home

## Workflow status

| Surface | Current setting |
|---|---|
| Worker dispatch | ` + "`active`, `rework`" + ` |
| Review checkpoint | ` + "`review`" + ` |
| Default runner | ` + "`codex`" + ` |
| Reviewer lane | enabled |
| Reviewer actor | ` + "`agent-reviewer`" + ` |
| Reviewer auto-close | ` + "`low`, `medium`" + ` |
| Human gate | ` + "`high`, `critical`" + ` |

## Active epics

![[_system/views/Epics.base#Active]]

## Task board

![[_system/views/Tasks.base#Board]]

## Ready

![[_system/views/Tasks.base#Ready]]

## Blocked

![[_system/views/Tasks.base#Blocked]]

## Backlog

![[_system/views/Tasks.base#Backlog]]

## Bug board

![[_system/views/BugTasks.base#Board]]

## Live runs

` + dashboardRunsBegin + `
_Auto-generated by ` + "`tusker reindex`" + `._
` + dashboardRunsEnd + `

## Docs pipeline

![[_system/views/Docs.base#Pipeline]]
`
}

func defaultV5CheatsheetNote(date string) string {
	return `---
title: "Cheat Sheet"
type: "note"
created: "` + date + `"
updated: "` + date + `"
tags: ["cheatsheet"]
---

# Tusker Cheat Sheet

## Task flow

` + "```text" + `
draft -> ready -> active -> review -> done
draft -> backlog
ready|active -> blocked -> ready|active
review -> rework -> active
active|review|blocked|rework|backlog -> cancelled
` + "```" + `

` + "`review`" + ` is a checkpoint. With ` + "`reviewer.enabled`" + `, the daemon can launch an independent review lane from ` + "`review`" + `; low/medium work can close as ` + "`agent-reviewer`" + `, and high/critical work stays human-gated.

## Current workflow settings

| Surface | Current setting |
|---|---|
| Worker dispatch | ` + "`active`, `rework`" + ` |
| Review checkpoint | ` + "`review`" + ` |
| Reviewer runner | ` + "`codex`" + ` |
| Reviewer actor | ` + "`agent-reviewer`" + ` |
| Auto-close | ` + "`low`, `medium`" + ` |
| Human close | ` + "`high`, `critical`" + ` |

## Common commands

` + "```bash" + `
tusker validate --vault ./tusker
tusker list --vault ./tusker --type task
tusker next --vault ./tusker
tusker claim --vault ./tusker MEM-T-0001 --as sarav
tusker status --vault ./tusker MEM-T-0001 active --actor sarav
tusker evidence --vault ./tusker MEM-T-0001 pr https://example.com/pr/123
tusker status --vault ./tusker MEM-T-0001 review --actor sarav
tusker verify --vault ./tusker MEM-T-0001 --by verifier
tusker close --vault ./tusker MEM-T-0001 --by sarav
` + "```" + `

## What to open

- ` + "`Dashboard.md`" + ` = landing page
- ` + "`Tasks.base#Board`" + ` = active work, grouped by status
- ` + "`Tasks.base#Ready`" + ` = shaped, unblocked current work, ready to pull
- ` + "`Tasks.base#Blocked`" + ` = current work waiting on blockers
- ` + "`Tasks.base#Backlog`" + ` = shaped future work, not this release
- ` + "`Tasks.base#Needs Attention`" + ` = blocked, review, rework — silent rotters
- ` + "`Tasks.base#Archive`" + ` = done + cancelled
- ` + "`BugTasks.base#Board`" + ` = open bug work, grouped by status

## Files and folders

- ` + "`_system/views/*.base`" + ` = Bases views
- ` + "`_system/generated/dashboard.json`" + ` = derived tracker/runtime snapshot
- ` + "`Attachments/<TASK-ID>/`" + ` = evidence files
`
}

func defaultV5EpicsBase() string {
	return `filters:
  and:
    - 'this.file.folder == "" || file.inFolder(this.file.folder)'
    - 'file.ext == "md"'
    - 'type == "epic"'
properties:
  "file.name":
    displayName: "ID"
  title:
    displayName: "Title"
  status:
    displayName: "Status"
  summary:
    displayName: "Summary"
  updated:
    displayName: "Updated"
views:
  - type: table
    name: "Active"
    filters:
      and:
        - 'status != "done"'
        - 'status != "cancelled"'
    order:
      - "file.name"
      - title
      - status
      - summary
      - updated
  - type: table
    name: "All"
    order:
      - "file.name"
      - title
      - status
      - summary
      - updated
  - type: table
    name: "Done"
    filters:
      and:
        - 'status == "done" || status == "cancelled"'
    order:
      - "file.name"
      - title
      - status
      - summary
      - updated
`
}

func defaultV5TasksBase(bugOnly bool) string {
	filter := `    - 'type == "task"'`
	if bugOnly {
		filter += "\n    - 'kind == \"bug\"'"
	}
	kindProp := `  kind:
    displayName: "Kind"
`
	kindCol := `      - kind
`
	if bugOnly {
		// Bugs are kind == "bug" by definition; suppress the column.
		kindProp = ""
		kindCol = ""
	}
	return `filters:
  and:
    - 'this.file.folder == "" || file.inFolder(this.file.folder)'
` + filter + `
properties:
  "file.name":
    displayName: "ID"
  title:
    displayName: "Title"
  status:
    displayName: "Status"
` + kindProp + `  risk:
    displayName: "Risk"
  priority:
    displayName: "Priority"
  assignee:
    displayName: "Assignee"
  size:
    displayName: "Size"
  epic:
    displayName: "Epic"
  blocked_by:
    displayName: "Blocked By"
  block_reason:
    displayName: "Block Reason"
  updated:
    displayName: "Updated"
views:
  - type: table
    name: "Board"
    filters:
      and:
        - 'status != "done"'
        - 'status != "cancelled"'
        - 'status != "backlog"'
        - 'status != "draft"'
        - 'status != "ready"'
    groupBy:
      property: note.status
      direction: ASC
    order:
      - "file.name"
      - title
` + kindCol + `      - risk
      - priority
      - assignee
      - size
      - epic
      - blocked_by
      - block_reason
  - type: table
    name: "Ready"
    filters:
      and:
        - 'status == "ready"'
    order:
      - "file.name"
      - title
` + kindCol + `      - risk
      - priority
      - assignee
      - size
      - epic
      - updated
  - type: table
    name: "Blocked"
    filters:
      and:
        - 'status == "blocked"'
    order:
      - "file.name"
      - title
      - blocked_by
      - block_reason
` + kindCol + `      - risk
      - priority
      - assignee
      - epic
  - type: table
    name: "Backlog"
    filters:
      and:
        - 'status == "backlog"'
    order:
      - "file.name"
      - title
` + kindCol + `      - risk
      - priority
      - size
      - epic
      - updated
  - type: table
    name: "Needs Attention"
    filters:
      and:
        - 'status == "draft" || status == "blocked" || status == "review" || status == "rework"'
    order:
      - "file.name"
      - title
      - status
      - risk
      - priority
      - assignee
      - epic
      - blocked_by
      - block_reason
      - updated
  - type: table
    name: "Archive"
    filters:
      and:
        - 'status == "done" || status == "cancelled"'
    groupBy:
      property: note.epic
      direction: ASC
    order:
      - "file.name"
      - title
      - status
` + kindCol + `      - epic
      - updated
`
}

func defaultV5DocsBase() string {
	return `filters:
  and:
    - 'this.file.folder == "" || file.inFolder(this.file.folder)'
    - 'file.ext == "md"'
    - 'type == "doc"'
properties:
  title:
    displayName: "Title"
  node:
    displayName: "Node"
  publish:
    displayName: "Publish"
  publish_lane:
    displayName: "Lane"
  canonical_status:
    displayName: "Canon"
  updated:
    displayName: "Updated"
views:
  - type: table
    name: "Pipeline"
    groupBy:
      property: note.publish_lane
      direction: ASC
    order:
      - title
      - node
      - publish
      - canonical_status
      - updated
`
}

func defaultV5EpicTemplate() string {
	return `---
schema: tusker.epic/v5
id: "{{acronym}}"
title: "{{title}}"
type: epic
status: draft
owner: "{{owner}}"
summary: "{{summary}}"
doc_nodes: []
created: "{{date}}"
updated: "{{date}}"
---

# {{acronym}} · {{title}}

## Thesis

## Scope

In:
-

Out:
-

## Success metrics

-

## Canon

-

## Task stack

-

## Open questions

-
`
}

func defaultV5TaskTemplate() string {
	return defaultV5TaskDocument("{{id}}", "{{title}}", "feature", "{{epic}}", "medium", "m", "p2", "{{date}}")
}

func defaultV5BugTemplate() string {
	return defaultV5TaskDocument("{{id}}", "{{title}}", "bug", "{{epic}}", "medium", "s", "p1", "{{date}}")
}

func defaultV5TaskDocument(id, title, kind, epic, risk, size, priority, date string) string {
	return `---
schema: tusker.task/v5
id: "` + id + `"
title: "` + title + `"
type: task
kind: ` + kind + `
epic: "` + epic + `"
status: draft
priority: ` + priority + `
risk: ` + risk + `
size: ` + size + `
delegation: execute
ai_assistance: heavy
ai_tools: []
domains: []
doc_nodes: []
blocked_by: []
block_reason: ""
created: "` + date + `"
updated: "` + date + `"
---

` + renderV5TaskBody(id, title, kind, risk, date)
}

func renderV5TaskBody(id, title, kind, risk, date string) string {
	var sections []string
	sections = append(sections,
		"# "+id+" · "+title,
		"",
		"## Agent capsule",
		"",
		"- Essence: "+title+".",
		"- Next action: define acceptance, do the smallest scoped change, and attach concise evidence.",
		"- Read next: this note, then only the code/docs anchors named here.",
		"- Avoid: raw logs, full transcripts, generated indexes, and attachments unless doing evidence forensics.",
		"",
		"## Intent",
		"",
		"## Acceptance contract",
		"",
		"| # | Outcome | Proof required | Docs impact |",
		"|---|---|---|---|",
		"| 1 |  |  |  |",
		"",
	)
	if kind == "bug" {
		sections = append(sections,
			"## Symptom",
			"",
			"-",
			"",
			"## Reproduction",
			"",
			"1.",
			"",
			"Expected:",
			"-",
			"",
			"Observed:",
			"-",
			"",
		)
	}
	switch strings.ToLower(risk) {
	case "medium":
		sections = append(sections, mediumTaskSections()...)
	case "high":
		sections = append(sections, mediumTaskSections()...)
		sections = append(sections, highTaskSections(false)...)
	case "critical":
		sections = append(sections, mediumTaskSections()...)
		sections = append(sections, highTaskSections(true)...)
	}
	sections = append(sections,
		"## Evidence",
		"",
		"- _No evidence yet. Attach summaries, PRs, packets, screenshots, or short log tails only._",
		"",
	)
	if strings.EqualFold(risk, "high") || strings.EqualFold(risk, "critical") {
		sections = append(sections,
			"## Verification log",
			"",
			"- _No verification yet._",
			"",
		)
	}
	return strings.Join(sections, "\n")
}

func mediumTaskSections() []string {
	return []string{
		"## Scope",
		"",
		"In:",
		"-",
		"",
		"Out:",
		"-",
		"",
		"## Deliverables",
		"",
		"-",
		"",
		"## Verification plan",
		"",
		"-",
		"",
	}
}

func highTaskSections(includeRollback bool) []string {
	sections := []string{
		"## Canon",
		"",
		"-",
		"",
		"## Code/system anchors",
		"",
		"-",
		"",
		"## Constraints",
		"",
		"-",
		"",
		"## Escalate if",
		"",
		"-",
		"",
		"## Knowledge delta",
		"",
		"| Topic | Before | After | Audience | Target doc nodes |",
		"|---|---|---|---|---|",
		"|  |  |  |  |  |",
		"",
	}
	if includeRollback {
		sections = append(sections,
			"## Rollback",
			"",
			"-",
			"",
		)
	}
	return sections
}

func defaultV5DocTemplate() string {
	return `---
schema: tusker.doc/v5
id: "{{node}}"
title: "{{title}}"
type: doc
node: "{{node}}"
audience: developer
mode: reference
agent_layer: none
kind: reference
domains: []
source_of_truth: []
stale_when_paths: []
canonical_status: draft
publish: true
publish_lane: internal
publish_path: "{{publish_path}}"
publish_description: "{{publish_description}}"
created: "{{date}}"
updated: "{{date}}"
---

# {{title}}

## Summary

## Audience

developer

## Mode

reference

## Source of truth

-

## Stale when

-

## Content

## Verification notes

-
`
}

func defaultV5AgentDocTemplate() string {
	return `---
schema: tusker.doc/v5
id: "{{node}}"
title: "{{title}}"
type: doc
node: "{{node}}"
audience: agent
mode: how-to
agent_layer: standalone
kind: runbook
domains: []
source_of_truth: []
stale_when_paths: []
canonical_status: draft
publish: true
publish_lane: internal
publish_path: "{{publish_path}}"
publish_description: "{{publish_description}}"
created: "{{date}}"
updated: "{{date}}"
---

# {{title}}

## Goal

## Inputs

## Preconditions

## Steps

1.

## Validation

## Failure modes

## Rollback

## Escalate when

## Manual intervention points

## Source of truth

-

## Stale when

-
`
}
