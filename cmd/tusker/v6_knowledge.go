package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	v6KnowledgeMapRelative  = "_system/generated/knowledge-map.json"
	v6RouteIndexRelative    = "_system/generated/route-index.json"
	v6GraphIndexRelative    = "_system/generated/graph.index.json"
	v6BacklinksRelative     = "_system/generated/backlinks.index.json"
	v6FreshnessRelative     = "_system/generated/freshness.index.json"
	v6PublicationRelative   = "_system/generated/publication.index.json"
	v6DomainsBlockBegin     = "<!-- tusker:domains:begin -->"
	v6DomainsBlockEnd       = "<!-- tusker:domains:end -->"
	v6BackrefsBlockBegin    = "<!-- tusker:backrefs:begin -->"
	v6BackrefsBlockEnd      = "<!-- tusker:backrefs:end -->"
	v6CurrentWorkBlockBegin = "<!-- tusker:current-work:begin -->"
	v6CurrentWorkBlockEnd   = "<!-- tusker:current-work:end -->"
)

var (
	v6DomainStatuses     = makeSet("current", "draft", "archived")
	v6KnowledgeKinds     = makeSet("canon", "index", "architecture", "reference", "how-to", "troubleshooting", "decision", "glossary", "runbook", "asset", "feature", "support", "release")
	v6CanonicalStatuses  = makeSet("draft", "approved", "deprecated", "historical", "superseded", "archived")
	v6HistoricalStatuses = makeSet("deprecated", "historical", "superseded", "archived")
	v6ProfileDomains     = map[string][]string{
		"generic": {"codebase", "product"},
		"library": {"codebase", "api", "usage"},
		"app":     {"codebase", "product", "auth", "operations"},
		"cli":     {"codebase", "cli", "workflow"},
		"infra":   {"codebase", "deployments", "operations", "security"},
		"tusker":  {"codebase", "cli", "runtime", "workflow", "schema", "knowledge-system", "skill", "obsidian", "adoption"},
	}
	v6FrontmatterOrder = map[string][]string{
		"project-skill": {"schema", "name", "description"},
		"domain":        {"schema", "id", "title", "status", "owner", "summary", "required", "primary_epics", "knowledge_nodes", "code_anchors", "source_of_truth", "tags"},
		"knowledge":     {"schema", "node", "title", "domain", "kind", "audience", "agent_layer", "canonical_status", "summary", "aliases", "source_of_truth", "stale_when", "related_nodes", "related_epics", "publish", "created_at", "updated_at", "tags"},
		"task":          {"schema", "id", "title", "epic", "kind", "status", "risk", "size", "priority", "primary_domain", "domains", "knowledge_change", "knowledge_nodes", "knowledge_resolution", "ai_assistance", "ai_tools", "created_at", "updated_at", "verified_by", "verified_at", "verification_summary", "closed_by", "closed_at", "close_summary", "tags"},
		"epic":          {"schema", "id", "title", "status", "primary_domains", "knowledge_nodes", "owner", "created_at", "updated_at", "tags"},
	}
)

type v6KnowledgeIndex struct {
	GeneratedAt    string               `json:"generated_at"`
	Documents      []v6IndexedMarkdown  `json:"documents,omitempty"`
	Domains        []v6DomainRecord     `json:"domains"`
	KnowledgeNodes []v6KnowledgeRecord  `json:"knowledge_nodes"`
	Tasks          []v6TaskRecord       `json:"tasks"`
	Epics          []v6EpicRecord       `json:"epics"`
	Freshness      []v6FreshnessRecord  `json:"freshness,omitempty"`
	Graph          v6GraphIndex         `json:"graph,omitempty"`
	Backlinks      []v6BacklinkRecord   `json:"backlinks,omitempty"`
	Publication    []v6PublicationEntry `json:"publication,omitempty"`
}

type v6IndexedMarkdown struct {
	Path          string                         `json:"path"`
	Schema        string                         `json:"schema"`
	Kind          string                         `json:"kind"`
	Frontmatter   map[string]any                 `json:"frontmatter,omitempty"`
	Title         string                         `json:"title,omitempty"`
	Headings      []v6Heading                    `json:"headings,omitempty"`
	Sections      map[string]v6Section           `json:"sections,omitempty"`
	WikiLinks     []v6WikiLink                   `json:"wiki_links,omitempty"`
	MarkdownLinks []v6MarkdownLink               `json:"markdown_links,omitempty"`
	ManagedBlocks []v6ManagedBlock               `json:"managed_blocks,omitempty"`
	Tables        map[string]v6MarkdownTable     `json:"tables,omitempty"`
	KnowledgeRows []knowledgeDeltaRow            `json:"knowledge_delta,omitempty"`
	RawBody       string                         `json:"-"`
	AbsolutePath  string                         `json:"-"`
	SectionByName map[string]string              `json:"-"`
	FrontmatterBy map[string]map[string]struct{} `json:"-"`
}

type v6Heading struct {
	Level int    `json:"level"`
	Title string `json:"title"`
	Slug  string `json:"slug"`
	Line  int    `json:"line"`
}

type v6Section struct {
	Heading string `json:"heading"`
	Slug    string `json:"slug"`
	Level   int    `json:"level"`
	Body    string `json:"body"`
}

type v6WikiLink struct {
	Target string `json:"target"`
	Anchor string `json:"anchor,omitempty"`
	Label  string `json:"label,omitempty"`
	Raw    string `json:"raw"`
}

type v6MarkdownLink struct {
	Text   string `json:"text"`
	Target string `json:"target"`
	Anchor string `json:"anchor,omitempty"`
	Raw    string `json:"raw"`
}

type v6ManagedBlock struct {
	Name  string `json:"name"`
	Start int    `json:"start_line"`
	End   int    `json:"end_line"`
}

type v6MarkdownTable struct {
	Headers []string   `json:"headers"`
	Rows    [][]string `json:"rows"`
}

type v6DomainRecord struct {
	ID             string   `json:"id"`
	Path           string   `json:"path"`
	Title          string   `json:"title"`
	Status         string   `json:"status"`
	Owner          string   `json:"owner,omitempty"`
	Summary        string   `json:"summary,omitempty"`
	Required       bool     `json:"required,omitempty"`
	PrimaryEpics   []string `json:"primary_epics,omitempty"`
	KnowledgeNodes []string `json:"knowledge_nodes,omitempty"`
	CodeAnchors    []string `json:"code_anchors,omitempty"`
	SourceOfTruth  []string `json:"source_of_truth,omitempty"`
	ReadWhen       string   `json:"read_when,omitempty"`
	DoNotReadWhen  string   `json:"do_not_read_when,omitempty"`
	RelatedDomains []string `json:"related_domains,omitempty"`
}

type v6KnowledgeRecord struct {
	Node            string         `json:"node"`
	Path            string         `json:"path"`
	Title           string         `json:"title"`
	Domain          string         `json:"domain"`
	Kind            string         `json:"kind"`
	Audience        string         `json:"audience"`
	AgentLayer      string         `json:"agent_layer"`
	CanonicalStatus string         `json:"canonical_status"`
	Summary         string         `json:"summary,omitempty"`
	Aliases         []string       `json:"aliases,omitempty"`
	ReadWhen        string         `json:"read_when,omitempty"`
	DoNotReadWhen   string         `json:"do_not_read_when,omitempty"`
	SourceOfTruth   []string       `json:"source_of_truth,omitempty"`
	StaleWhen       map[string]any `json:"stale_when,omitempty"`
	RelatedNodes    []string       `json:"related_nodes,omitempty"`
	RelatedEpics    []string       `json:"related_epics,omitempty"`
	LinksOut        []string       `json:"links_out,omitempty"`
	Backlinks       []string       `json:"backlinks,omitempty"`
	RecentTasks     []string       `json:"recent_tasks,omitempty"`
	Freshness       string         `json:"freshness,omitempty"`
	Publish         map[string]any `json:"publish,omitempty"`
}

type v6TaskRecord struct {
	ID                  string   `json:"id"`
	Path                string   `json:"path"`
	Title               string   `json:"title"`
	Epic                string   `json:"epic"`
	Status              string   `json:"status"`
	Kind                string   `json:"kind"`
	Risk                string   `json:"risk"`
	Priority            string   `json:"priority"`
	PrimaryDomain       string   `json:"primary_domain,omitempty"`
	Domains             []string `json:"domains,omitempty"`
	KnowledgeNodes      []string `json:"knowledge_nodes,omitempty"`
	KnowledgeResolution []any    `json:"knowledge_resolution,omitempty"`
}

type v6EpicRecord struct {
	ID              string   `json:"id"`
	Path            string   `json:"path"`
	Title           string   `json:"title"`
	Status          string   `json:"status"`
	Owner           string   `json:"owner,omitempty"`
	PrimaryDomains  []string `json:"primary_domains,omitempty"`
	KnowledgeNodes  []string `json:"knowledge_nodes,omitempty"`
	LegacyDocNodes  []string `json:"doc_nodes,omitempty"`
	LegacyV5Summary string   `json:"summary,omitempty"`
}

type v6FreshnessRecord struct {
	Node              string   `json:"node"`
	State             string   `json:"state"`
	Fingerprint       string   `json:"fingerprint,omitempty"`
	Recorded          string   `json:"recorded_fingerprint,omitempty"`
	LastTask          string   `json:"last_task,omitempty"`
	LastStatus        string   `json:"last_status,omitempty"`
	LastAt            string   `json:"last_at,omitempty"`
	Missing           []string `json:"missing,omitempty"`
	MatchedSourcePath []string `json:"matched_source_paths,omitempty"`
}

type v6GraphIndex struct {
	Schema      string        `json:"schema"`
	GeneratedAt string        `json:"generated_at"`
	Nodes       []v6GraphNode `json:"nodes"`
	Edges       []v6GraphEdge `json:"edges"`
}

type v6GraphNode struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Title  string `json:"title,omitempty"`
	Path   string `json:"path,omitempty"`
	Domain string `json:"domain,omitempty"`
}

type v6GraphEdge struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Relation string `json:"relation"`
}

type v6BacklinkRecord struct {
	Node  string   `json:"node"`
	From  []string `json:"from,omitempty"`
	Tasks []string `json:"tasks,omitempty"`
}

type v6PublicationEntry struct {
	Node            string `json:"node"`
	Title           string `json:"title"`
	Path            string `json:"path"`
	Route           string `json:"route"`
	Lane            string `json:"lane"`
	Audience        string `json:"audience"`
	Kind            string `json:"kind"`
	CanonicalStatus string `json:"canonical_status"`
	IncludeInLLMS   bool   `json:"include_in_llms"`
}

func isV6Schema(data map[string]any) bool {
	return strings.HasSuffix(stringField(data, "schema"), "/v6")
}

func effectiveNoteKind(data map[string]any) string {
	schema := stringField(data, "schema")
	switch schema {
	case "tusker.domain/v6":
		return "domain"
	case "tusker.knowledge/v6":
		return "knowledge"
	case "tusker.task/v6":
		return "task"
	case "tusker.epic/v6":
		return "epic"
	case "tusker.project-skill/v6":
		return "project-skill"
	default:
		return stringField(data, "type")
	}
}

func hasV6Vault(vaultPath string) bool {
	return fileExists(filepath.Join(vaultPath, "SKILL.md")) || fileExists(filepath.Join(vaultPath, "domains"))
}

func bootstrapV6(args Args) error {
	vaultPath, err := resolveVaultPath(args, true)
	if err != nil {
		return err
	}
	profile := strings.ToLower(strings.TrimSpace(firstNonEmpty(args.String("profile"), "generic")))
	domains, ok := v6ProfileDomains[profile]
	if !ok {
		return tuskerError(errorInvalidArg, fmt.Sprintf(`unknown V6 profile "%s"`, profile), withHint("use generic, library, app, cli, infra, or tusker"))
	}
	date := todayISO()
	for _, relative := range []string{
		"",
		"domains",
		"epics",
		"_config",
		"_system/generated",
		"_system/templates",
		"_system/views",
		"_system/runs",
		"_system/events",
		"_system/snippets",
		"_system/archive",
		"_system/workspaces",
		"_system/logs",
		"Attachments",
	} {
		if err := ensureDir(filepath.Join(vaultPath, relative)); err != nil {
			return err
		}
	}
	if err := writeDefaultConfig(vaultPath); err != nil {
		return err
	}
	if err := writeDefaultV6KnowledgePolicy(vaultPath); err != nil {
		return err
	}
	if err := writeDefaultV6ProjectSkill(vaultPath, domains); err != nil {
		return err
	}
	if err := writeText(filepath.Join(vaultPath, "WORKFLOW.md"), strings.Replace(defaultWorkflowMarkdown(), "tracker_schema_version: 5", "tracker_schema_version: 6", 1)); err != nil {
		return err
	}
	if !fileExists(filepath.Join(vaultPath, "README.md")) {
		if err := writeText(filepath.Join(vaultPath, "README.md"), defaultV6Readme(date, profile)); err != nil {
			return err
		}
	}
	for _, domain := range domains {
		if err := writeV6DomainStarter(vaultPath, domain, date); err != nil {
			return err
		}
	}
	if err := writeDefaultV6VaultTemplates(vaultPath, date); err != nil {
		return err
	}
	if err := upsertGitignore(vaultPath); err != nil {
		return err
	}
	if err := writeV6GeneratedIndexes(vaultPath, true); err != nil {
		return err
	}
	if !args.Bool("quiet") {
		fmt.Printf("Tusker V6 vault initialized at %s using profile %s\n", vaultPath, profile)
	}
	return nil
}

func writeDefaultV6KnowledgePolicy(vaultPath string) error {
	path := filepath.Join(vaultPath, "_config", "knowledge-policy.yaml")
	if fileExists(path) {
		return nil
	}
	return writeText(path, `schema: tusker.knowledge-policy/v6

required_domain_files:
  - INDEX.md
  - CANON.md

allowed_kinds:
  - canon
  - index
  - architecture
  - reference
  - how-to
  - troubleshooting
  - decision
  - glossary
  - runbook
  - asset
  - feature
  - support
  - release

allowed_audiences:
  - user
  - developer
  - operator
  - support
  - release
  - agent
  - internal

allowed_agent_layers:
  - none
  - capsule
  - standalone

required_sections:
  knowledge:
    - Read this when
    - Do not read this when
    - Source of truth
    - Related
  domain:
    - Read this when
    - Do not read this when
    - Current canon
    - Start here
    - Main knowledge nodes
    - Source of truth
    - Related domains
    - Current work

publication:
  default_llms_statuses:
    - approved
    - draft
  default_llms_excluded_audiences:
    - internal
  historical_statuses:
    - deprecated
    - historical
    - superseded
    - archived
`)
}

func writeDefaultV6ProjectSkill(vaultPath string, domains []string) error {
	path := filepath.Join(vaultPath, "SKILL.md")
	if fileExists(path) {
		return nil
	}
	body := `# Project knowledge skill

Use this file to route through this repository's Tusker knowledge graph.
Use the Tusker operator skill for task mechanics and CLI workflow.

## Routing rule

Start with the narrowest domain INDEX. Read CANON before task history.
Read task files only for proof, evidence, or implementation history.

## Answering rules

1. Prefer domain CANON.md over task history.
2. Prefer source code or API schemas over prose when exact behavior conflicts.
3. When code and canon disagree, trust code, mark canon stale, and report the conflict.
4. Do not read generated output by default.
5. Do not load full files when a capsule or section read is enough.
6. When suggesting a code change, include verification.
7. When production impact is possible, include rollback or safe-change checks.

## Domains

` + v6DomainsBlockBegin + `
` + renderV6ProjectSkillDomainTableFromIDs(domains) + `
` + v6DomainsBlockEnd + `
`
	data := map[string]any{
		"schema":      "tusker.project-skill/v6",
		"name":        "project-knowledge",
		"description": "Understand, modify, explain, or verify this repository using its domain canon, codebase map, task proof, and knowledge graph.",
	}
	content, err := serializeDocument(data, body, v6FrontmatterOrder["project-skill"])
	if err != nil {
		return err
	}
	return writeText(path, content)
}

func renderV6ProjectSkillDomainTableFromIDs(domains []string) string {
	rows := []string{"| Intent | Read first | Canon | Notes |", "|---|---|---|---|"}
	for _, id := range domains {
		rows = append(rows, fmt.Sprintf("| %s | [[%s/INDEX]] | [[%s/CANON]] | %s domain knowledge. |", v6DomainIntent(id), id, id, strings.Title(strings.ReplaceAll(id, "-", " "))))
	}
	return strings.Join(rows, "\n")
}

func v6DomainIntent(id string) string {
	switch id {
	case "cli":
		return "Change or inspect CLI behavior"
	case "runtime":
		return "Understand daemon, runner, leases, or reviewer lane"
	case "workflow":
		return "Change lifecycle or close policy"
	case "schema":
		return "Change frontmatter, validation, or migration"
	case "knowledge-system":
		return "Change knowledge graph, freshness, or publish"
	case "skill":
		return "Change operator or project skill guidance"
	case "obsidian":
		return "Change vault navigation or Obsidian views"
	case "adoption":
		return "Install, migrate, or roll out Tusker"
	case "codebase":
		return "Change repository code safely"
	default:
		return "Understand " + strings.ReplaceAll(id, "-", " ")
	}
}

func defaultV6Readme(date, profile string) string {
	return `---
title: "Tusker V6 vault"
type: "note"
created: "` + date + `"
updated: "` + date + `"
tags: ["v6", "knowledge-graph"]
---

# Tusker V6 vault

` + readmeOverviewBegin + `

This vault uses the V6 product knowledge graph layout.

Start with ` + "`SKILL.md`" + ` for routing, then read domain ` + "`INDEX.md`" + ` and ` + "`CANON.md`" + ` files before task history.

Profile: ` + profile + `

` + readmeOverviewEnd + `
`
}

func writeV6DomainStarter(vaultPath, domain, date string) error {
	domainDir := filepath.Join(vaultPath, "domains", domain)
	if err := ensureDir(domainDir); err != nil {
		return err
	}
	title := strings.Title(strings.ReplaceAll(domain, "-", " "))
	indexPath := filepath.Join(domainDir, "INDEX.md")
	if !fileExists(indexPath) {
		data := map[string]any{
			"schema":          "tusker.domain/v6",
			"id":              domain,
			"title":           title,
			"status":          "current",
			"owner":           "sarav",
			"summary":         v6DefaultDomainSummary(domain),
			"required":        domain == "codebase",
			"knowledge_nodes": []string{domain + "/canon"},
			"source_of_truth": []string{"tusker/SKILL.md"},
			"tags":            []string{domain},
		}
		body := `# ` + title + `

## Read this when

Read this when work touches ` + strings.ReplaceAll(domain, "-", " ") + ` behavior, implementation, policy, or current product knowledge.

## Do not read this when

Do not read this for unrelated domains or task proof history unless this index routes you there.

## Current canon

- [[` + domain + `/CANON]]

## Start here

Read [[` + domain + `/CANON]] first, then the narrowest reference node.

## Main knowledge nodes

- [[` + domain + `/CANON]]

## Source of truth

- ` + "`tusker/SKILL.md`" + `

## Related domains

- [[codebase/INDEX]]

## Current work

` + v6CurrentWorkBlockBegin + `
_Run ` + "`tusker reindex`" + ` to refresh current work._
` + v6CurrentWorkBlockEnd + `
`
		content, err := serializeDocument(data, body, v6FrontmatterOrder["domain"])
		if err != nil {
			return err
		}
		if err := writeText(indexPath, content); err != nil {
			return err
		}
	}
	canonPath := filepath.Join(domainDir, "CANON.md")
	if !fileExists(canonPath) {
		data := map[string]any{
			"schema":           "tusker.knowledge/v6",
			"node":             domain + "/canon",
			"title":            title + " canon",
			"domain":           domain,
			"kind":             "canon",
			"audience":         "developer",
			"agent_layer":      "capsule",
			"canonical_status": "draft",
			"summary":          v6DefaultDomainSummary(domain),
			"aliases":          []string{domain + " canon", strings.ReplaceAll(domain, "-", " ")},
			"source_of_truth":  []string{"tusker/SKILL.md"},
			"stale_when":       map[string]any{"paths": []string{"tusker/SKILL.md"}},
			"publish":          map[string]any{"lane": "internal", "path": domain + "/canon", "include_in_llms": true},
			"created_at":       date,
			"updated_at":       date,
			"tags":             []string{domain},
		}
		body := `# ` + title + ` canon

## Read this when

Read this for the current model, invariants, defaults, and boundaries for ` + strings.ReplaceAll(domain, "-", " ") + `.

## Do not read this when

Do not use this as task proof; open linked tasks only when implementation history or evidence matters.

## Current model

This domain records current durable truth for ` + strings.ReplaceAll(domain, "-", " ") + `.

## Invariants

- Keep current truth in domain knowledge pages.
- Keep task proof in ` + "`tusker/epics/**`" + `.
- Prefer source code over prose when exact behavior conflicts.

## Current defaults

- New knowledge starts as draft canon until verified.
- Route through this canon before opening historical tasks.

## Deprecated behavior

- Do not treat task files as canonical documentation.

## Source of truth

- ` + "`tusker/SKILL.md`" + `

## Open questions

- Add domain-specific open questions here as the implementation matures.

## Related

- [[` + domain + `/INDEX]]

## Recent changes

` + v6BackrefsBlockBegin + `
_Run ` + "`tusker reindex`" + ` to refresh recent task proof._
` + v6BackrefsBlockEnd + `
`
		content, err := serializeDocument(data, body, v6FrontmatterOrder["knowledge"])
		if err != nil {
			return err
		}
		if err := writeText(canonPath, content); err != nil {
			return err
		}
	}
	if domain == "codebase" {
		for _, starter := range []struct {
			file  string
			node  string
			title string
		}{
			{"repo-map.md", "codebase/repo-map", "Repository map"},
			{"testing.md", "codebase/testing", "Testing"},
			{"safe-change-rules.md", "codebase/safe-change-rules", "Safe change rules"},
		} {
			if err := writeV6KnowledgeStarter(vaultPath, domain, starter.file, starter.node, starter.title, "reference", date); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeV6KnowledgeStarter(vaultPath, domain, fileName, node, title, kind, date string) error {
	path := filepath.Join(vaultPath, "domains", domain, fileName)
	if fileExists(path) {
		return nil
	}
	data := map[string]any{
		"schema":           "tusker.knowledge/v6",
		"node":             node,
		"title":            title,
		"domain":           domain,
		"kind":             kind,
		"audience":         "developer",
		"agent_layer":      "capsule",
		"canonical_status": "draft",
		"summary":          title + " for the " + strings.ReplaceAll(domain, "-", " ") + " domain.",
		"aliases":          []string{strings.ToLower(title)},
		"source_of_truth":  []string{"tusker/SKILL.md"},
		"stale_when":       map[string]any{"paths": []string{"tusker/SKILL.md"}},
		"publish":          map[string]any{"lane": "internal", "path": node, "include_in_llms": true},
		"created_at":       date,
		"updated_at":       date,
	}
	body := `# ` + title + `

## Read this when

Read this when you need ` + strings.ToLower(title) + ` for this repository.

## Do not read this when

Do not read this for unrelated product behavior or task proof history.

## Source of truth

- ` + "`tusker/SKILL.md`" + `

## Related

- [[` + domain + `/CANON]]

## Recent changes

` + v6BackrefsBlockBegin + `
_Run ` + "`tusker reindex`" + ` to refresh recent task proof._
` + v6BackrefsBlockEnd + `
`
	content, err := serializeDocument(data, body, v6FrontmatterOrder["knowledge"])
	if err != nil {
		return err
	}
	return writeText(path, content)
}

func v6DefaultDomainSummary(domain string) string {
	switch domain {
	case "cli":
		return "Command surface, flags, help text, routing, and user-visible terminal behavior."
	case "runtime":
		return "Daemon dispatch, runner state, review lane, leases, attempts, sessions, and logs."
	case "workflow":
		return "Task lifecycle, close gates, verification, evidence, and review policy."
	case "schema":
		return "Note schemas, frontmatter, validation, templates, and migrations."
	case "knowledge-system":
		return "Domain knowledge graph, indexer, freshness, routing, backrefs, and publication."
	case "skill":
		return "Operator skill, project skill router, agent instructions, and bundled guidance."
	case "obsidian":
		return "Vault layout, wikilinks, managed blocks, Bases views, and graph navigation."
	case "adoption":
		return "Install, migration, rollout, and consumer repo adoption."
	case "codebase":
		return "Repository layout, implementation anchors, testing, and safe change rules."
	default:
		return "Current durable truth for " + strings.ReplaceAll(domain, "-", " ") + "."
	}
}

func writeDefaultV6VaultTemplates(vaultPath, date string) error {
	replacements := map[string]string{"{{date}}": date}
	templates := map[string]string{
		"domain-index.md":  defaultV6DomainIndexTemplate(),
		"domain-canon.md":  defaultV6DomainCanonTemplate(),
		"knowledge.md":     defaultV6KnowledgeTemplate(),
		"task-v6.md":       defaultV6TaskTemplate(),
		"epic-v6.md":       defaultV6EpicTemplate(),
		"project-skill.md": defaultV6ProjectSkillTemplate(),
	}
	for name, content := range templates {
		content = replaceTemplateTokens(content, replacements)
		if err := writeText(filepath.Join(vaultPath, "_system", "templates", name), content); err != nil {
			return err
		}
	}
	return nil
}

func defaultV6DomainIndexTemplate() string {
	return `---
schema: "tusker.domain/v6"
id: "{{domain}}"
title: "{{title}}"
status: "current"
owner: "sarav"
summary: "{{summary}}"
knowledge_nodes:
  - "{{domain}}/canon"
source_of_truth:
  - "tusker/SKILL.md"
tags:
  - "{{domain}}"
---

# {{title}}

## Read this when

Use this domain index to route work for {{title}}.

## Do not read this when

Skip this when another domain is narrower.

## Current canon

- [[{{domain}}/CANON]]

## Start here

Read [[{{domain}}/CANON]] first.

## Main knowledge nodes

- [[{{domain}}/CANON]]

## Source of truth

- ` + "`tusker/SKILL.md`" + `

## Related domains

- [[codebase/INDEX]]

## Current work

` + v6CurrentWorkBlockBegin + `
_Run ` + "`tusker reindex`" + ` to refresh current work._
` + v6CurrentWorkBlockEnd + `
`
}

func defaultV6DomainCanonTemplate() string {
	return `---
schema: "tusker.knowledge/v6"
node: "{{domain}}/canon"
title: "{{title}} canon"
domain: "{{domain}}"
kind: "canon"
audience: "developer"
agent_layer: "capsule"
canonical_status: "draft"
summary: "{{summary}}"
source_of_truth:
  - "tusker/SKILL.md"
stale_when:
  paths:
    - "tusker/SKILL.md"
publish:
  lane: "internal"
  path: "{{domain}}/canon"
  include_in_llms: true
created_at: "{{date}}"
updated_at: "{{date}}"
---

# {{title}} canon

## Read this when

Read this for the current {{title}} model.

## Do not read this when

Do not use this as task proof.

## Current model

Current model goes here.

## Invariants

- Keep current truth here.

## Current defaults

- Defaults go here.

## Deprecated behavior

- Deprecated behavior goes here.

## Source of truth

- ` + "`tusker/SKILL.md`" + `

## Open questions

- None yet.

## Related

- [[{{domain}}/INDEX]]

## Recent changes

` + v6BackrefsBlockBegin + `
_Run ` + "`tusker reindex`" + ` to refresh recent task proof._
` + v6BackrefsBlockEnd + `
`
}

func defaultV6KnowledgeTemplate() string {
	return `---
schema: "tusker.knowledge/v6"
node: "{{node}}"
title: "{{title}}"
domain: "{{domain}}"
kind: "reference"
audience: "developer"
agent_layer: "capsule"
canonical_status: "draft"
summary: "{{summary}}"
aliases: []
source_of_truth:
  - "tusker/SKILL.md"
stale_when:
  paths:
    - "tusker/SKILL.md"
related_nodes: []
related_epics: []
publish:
  lane: "internal"
  path: "{{node}}"
  include_in_llms: true
created_at: "{{date}}"
updated_at: "{{date}}"
---

# {{title}}

## Read this when

Read this when this exact knowledge node answers the user's intent.

## Do not read this when

Do not read this for unrelated domains or historical proof.

## Source of truth

- ` + "`tusker/SKILL.md`" + `

## Related

- [[{{domain}}/CANON]]

## Recent changes

` + v6BackrefsBlockBegin + `
_Run ` + "`tusker reindex`" + ` to refresh recent task proof._
` + v6BackrefsBlockEnd + `
`
}

func defaultV6TaskTemplate() string {
	return `---
schema: "tusker.task/v6"
id: "{{id}}"
title: "{{title}}"
epic: "{{epic}}"
kind: "feature"
status: "ready"
risk: "medium"
size: "m"
priority: "p1"
primary_domain: "{{domain}}"
domains:
  - "{{domain}}"
knowledge_change: false
knowledge_nodes: []
knowledge_resolution: []
ai_assistance: "assisted"
ai_tools: []
created_at: "{{date}}"
updated_at: "{{date}}"
---

# {{id}} - {{title}}

## Intent

State the change.

## Read this when

Read this for task proof or implementation context.

## Acceptance

- Outcome and proof.

## Verification plan

- Command or review path.

## Verification log

- Not verified yet.

## Evidence

- Not started.

## Knowledge delta

| Topic | Before | After | Audience | Target knowledge nodes |
|---|---|---|---|---|
| _none_ | _none_ | _none_ | developer | none |
`
}

func defaultV6EpicTemplate() string {
	return `---
schema: "tusker.epic/v6"
id: "{{id}}"
title: "{{title}}"
status: "ready"
primary_domains: []
knowledge_nodes: []
owner: "sarav"
created_at: "{{date}}"
updated_at: "{{date}}"
---

# {{id}} - {{title}}

## Thesis

Describe the initiative.

## Scope

In and out of scope.

## Success metrics

- Metric.

## Canon

Current truth lives in domain CANON files. This epic records workstream proof.

## Task stack

_No open tasks._
`
}

func defaultV6ProjectSkillTemplate() string {
	return `---
schema: "tusker.project-skill/v6"
name: "project-knowledge"
description: "Route through this repository using domain canon, codebase map, task proof, and knowledge graph."
---

# Project knowledge skill

## Routing rule

Start with the narrowest domain INDEX. Read CANON before task history.

## Answering rules

1. Prefer domain canon over task history.
2. Trust source code over prose when they conflict.
3. Do not read generated output by default.

## Domains

` + v6DomainsBlockBegin + `
| Intent | Read first | Canon | Notes |
|---|---|---|---|
` + v6DomainsBlockEnd + `
`
}

func v6IndexVault(vaultPath string) (v6KnowledgeIndex, error) {
	generatedAt := time.Now().UTC().Format(time.RFC3339)
	docs, err := v6IndexedMarkdownFiles(vaultPath)
	if err != nil {
		return v6KnowledgeIndex{}, err
	}
	index := v6KnowledgeIndex{GeneratedAt: generatedAt, Documents: docs}
	knowledgeByNode := map[string]*v6KnowledgeRecord{}
	for _, doc := range docs {
		switch doc.Kind {
		case "domain":
			record := v6DomainRecord{
				ID:             stringField(doc.Frontmatter, "id"),
				Path:           doc.Path,
				Title:          stringField(doc.Frontmatter, "title"),
				Status:         stringField(doc.Frontmatter, "status"),
				Owner:          stringField(doc.Frontmatter, "owner"),
				Summary:        stringField(doc.Frontmatter, "summary"),
				Required:       boolField(doc.Frontmatter, "required"),
				PrimaryEpics:   normalizeList(doc.Frontmatter["primary_epics"]),
				KnowledgeNodes: normalizeList(doc.Frontmatter["knowledge_nodes"]),
				CodeAnchors:    normalizeList(doc.Frontmatter["code_anchors"]),
				SourceOfTruth:  normalizeList(doc.Frontmatter["source_of_truth"]),
				ReadWhen:       v6SectionBody(doc, "read this when"),
				DoNotReadWhen:  v6SectionBody(doc, "do not read this when"),
				RelatedDomains: v6ExtractWikiTargets(v6SectionBody(doc, "related domains")),
			}
			index.Domains = append(index.Domains, record)
		case "knowledge":
			record := v6KnowledgeRecord{
				Node:            stringField(doc.Frontmatter, "node"),
				Path:            doc.Path,
				Title:           stringField(doc.Frontmatter, "title"),
				Domain:          stringField(doc.Frontmatter, "domain"),
				Kind:            stringField(doc.Frontmatter, "kind"),
				Audience:        stringField(doc.Frontmatter, "audience"),
				AgentLayer:      stringField(doc.Frontmatter, "agent_layer"),
				CanonicalStatus: stringField(doc.Frontmatter, "canonical_status"),
				Summary:         stringField(doc.Frontmatter, "summary"),
				Aliases:         normalizeList(doc.Frontmatter["aliases"]),
				ReadWhen:        v6SectionBody(doc, "read this when"),
				DoNotReadWhen:   v6SectionBody(doc, "do not read this when"),
				SourceOfTruth:   normalizeList(doc.Frontmatter["source_of_truth"]),
				StaleWhen:       mapField(doc.Frontmatter, "stale_when"),
				RelatedNodes:    normalizeList(doc.Frontmatter["related_nodes"]),
				RelatedEpics:    normalizeList(doc.Frontmatter["related_epics"]),
				LinksOut:        v6LinksOut(doc),
				Publish:         mapField(doc.Frontmatter, "publish"),
			}
			index.KnowledgeNodes = append(index.KnowledgeNodes, record)
			copyRecord := record
			knowledgeByNode[record.Node] = &copyRecord
		case "task":
			record := v6TaskRecord{
				ID:                  stringField(doc.Frontmatter, "id"),
				Path:                doc.Path,
				Title:               stringField(doc.Frontmatter, "title"),
				Epic:                wikiTarget(doc.Frontmatter["epic"]),
				Status:              stringField(doc.Frontmatter, "status"),
				Kind:                stringField(doc.Frontmatter, "kind"),
				Risk:                stringField(doc.Frontmatter, "risk"),
				Priority:            stringField(doc.Frontmatter, "priority"),
				PrimaryDomain:       stringField(doc.Frontmatter, "primary_domain"),
				Domains:             normalizeList(doc.Frontmatter["domains"]),
				KnowledgeNodes:      normalizeList(doc.Frontmatter["knowledge_nodes"]),
				KnowledgeResolution: anySlice(doc.Frontmatter["knowledge_resolution"]),
			}
			if record.Epic == "" {
				record.Epic = stringField(doc.Frontmatter, "epic")
			}
			index.Tasks = append(index.Tasks, record)
		case "epic":
			record := v6EpicRecord{
				ID:              stringField(doc.Frontmatter, "id"),
				Path:            doc.Path,
				Title:           stringField(doc.Frontmatter, "title"),
				Status:          stringField(doc.Frontmatter, "status"),
				Owner:           stringField(doc.Frontmatter, "owner"),
				PrimaryDomains:  normalizeList(doc.Frontmatter["primary_domains"]),
				KnowledgeNodes:  normalizeList(doc.Frontmatter["knowledge_nodes"]),
				LegacyDocNodes:  normalizeList(doc.Frontmatter["doc_nodes"]),
				LegacyV5Summary: stringField(doc.Frontmatter, "summary"),
			}
			index.Epics = append(index.Epics, record)
		}
	}
	index.Backlinks = v6BuildBacklinks(index.KnowledgeNodes, index.Tasks)
	index.Graph = v6BuildGraph(generatedAt, index)
	index.Freshness = v6BuildFreshness(vaultPath, index.KnowledgeNodes, index.Tasks)
	freshnessByNode := map[string]string{}
	for _, item := range index.Freshness {
		freshnessByNode[item.Node] = item.State
	}
	backlinksByNode := map[string]v6BacklinkRecord{}
	for _, item := range index.Backlinks {
		backlinksByNode[item.Node] = item
	}
	for i := range index.KnowledgeNodes {
		node := index.KnowledgeNodes[i].Node
		index.KnowledgeNodes[i].Freshness = freshnessByNode[node]
		index.KnowledgeNodes[i].Backlinks = backlinksByNode[node].From
		index.KnowledgeNodes[i].RecentTasks = backlinksByNode[node].Tasks
	}
	index.Publication = v6BuildPublication(index.KnowledgeNodes)
	sortV6Index(&index)
	return index, nil
}

func v6IndexedMarkdownFiles(vaultPath string) ([]v6IndexedMarkdown, error) {
	var paths []string
	if err := walkDirUnsorted(vaultPath, func(current string, entry fs.DirEntry) error {
		rel, err := filepath.Rel(vaultPath, current)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			if rel == "_system" || strings.HasPrefix(rel, "_system/") || rel == "_config" || strings.HasPrefix(rel, "_config/") || rel == "Attachments" || strings.HasPrefix(rel, "Attachments/") {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(rel, ".md") {
			paths = append(paths, current)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	sort.Strings(paths)
	var out []v6IndexedMarkdown
	for _, path := range paths {
		doc, err := v6IndexMarkdownFile(vaultPath, path)
		if err != nil {
			return nil, err
		}
		if doc.Schema == "" && doc.Kind == "" {
			continue
		}
		out = append(out, doc)
	}
	return out, nil
}

func v6IndexMarkdownFile(vaultPath, path string) (v6IndexedMarkdown, error) {
	text, err := readText(path)
	if err != nil {
		return v6IndexedMarkdown{}, err
	}
	data, body, err := parseFrontmatter(text)
	if err != nil {
		return v6IndexedMarkdown{}, err
	}
	rel, err := filepath.Rel(vaultPath, path)
	if err != nil {
		return v6IndexedMarkdown{}, err
	}
	doc := v6IndexedMarkdown{
		Path:         filepath.ToSlash(rel),
		AbsolutePath: path,
		Schema:       stringField(data, "schema"),
		Kind:         effectiveNoteKind(data),
		Frontmatter:  data,
		Title:        firstNonEmpty(stringField(data, "title"), docsFirstHeading(body)),
		RawBody:      body,
	}
	doc.Headings, doc.Sections = v6ExtractSections(body)
	doc.WikiLinks = v6ExtractWikiLinks(body)
	doc.MarkdownLinks = v6ExtractMarkdownLinks(body)
	doc.ManagedBlocks = v6ExtractManagedBlocks(body)
	doc.Tables = v6ExtractTables(doc.Sections)
	if doc.Kind == "task" {
		doc.KnowledgeRows = parseKnowledgeDeltaRows(body)
	}
	return doc, nil
}

func v6ExtractSections(body string) ([]v6Heading, map[string]v6Section) {
	lines := strings.Split(body, "\n")
	headingPattern := regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*$`)
	type headingLine struct {
		heading v6Heading
		index   int
	}
	var headings []headingLine
	for i, line := range lines {
		match := headingPattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		title := strings.TrimSpace(match[2])
		headings = append(headings, headingLine{
			heading: v6Heading{Level: len(match[1]), Title: title, Slug: normalizeV6Heading(title), Line: i + 1},
			index:   i,
		})
	}
	outHeadings := make([]v6Heading, 0, len(headings))
	sections := map[string]v6Section{}
	for i, current := range headings {
		outHeadings = append(outHeadings, current.heading)
		end := len(lines)
		for j := i + 1; j < len(headings); j++ {
			if headings[j].heading.Level <= current.heading.Level {
				end = headings[j].index
				break
			}
		}
		body := strings.TrimSpace(stripHTMLComments(strings.Join(lines[current.index+1:end], "\n")))
		sections[current.heading.Slug] = v6Section{Heading: current.heading.Title, Slug: current.heading.Slug, Level: current.heading.Level, Body: body}
	}
	return outHeadings, sections
}

func normalizeV6Heading(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = regexp.MustCompile(`[^\p{L}\p{N}\s-]+`).ReplaceAllString(value, "")
	value = regexp.MustCompile(`[\s-]+`).ReplaceAllString(value, "-")
	return strings.Trim(value, "-")
}

func v6SectionBody(doc v6IndexedMarkdown, name string) string {
	if doc.Sections == nil {
		return ""
	}
	if section, ok := doc.Sections[normalizeV6Heading(name)]; ok {
		return section.Body
	}
	return ""
}

func v6ExtractWikiLinks(body string) []v6WikiLink {
	pattern := regexp.MustCompile(`\[\[([^\]]+)\]\]`)
	var out []v6WikiLink
	for _, match := range pattern.FindAllStringSubmatch(body, -1) {
		inner := strings.TrimSpace(match[1])
		label := ""
		if parts := strings.SplitN(inner, "|", 2); len(parts) == 2 {
			inner = strings.TrimSpace(parts[0])
			label = strings.TrimSpace(parts[1])
		}
		target := inner
		anchor := ""
		if parts := strings.SplitN(inner, "#", 2); len(parts) == 2 {
			target = strings.TrimSpace(parts[0])
			anchor = strings.TrimSpace(parts[1])
		}
		out = append(out, v6WikiLink{Target: target, Anchor: anchor, Label: label, Raw: match[0]})
	}
	return out
}

func v6ExtractMarkdownLinks(body string) []v6MarkdownLink {
	pattern := regexp.MustCompile(`!?\[([^\]]*)\]\(([^)]+)\)`)
	var out []v6MarkdownLink
	for _, match := range pattern.FindAllStringSubmatch(body, -1) {
		target := strings.TrimSpace(match[2])
		href, _, anchor := docsSplitHref(target)
		if !strings.HasSuffix(strings.ToLower(href), ".md") {
			continue
		}
		out = append(out, v6MarkdownLink{Text: strings.TrimSpace(match[1]), Target: href, Anchor: anchor, Raw: match[0]})
	}
	return out
}

func v6ExtractManagedBlocks(body string) []v6ManagedBlock {
	lines := strings.Split(body, "\n")
	beginPattern := regexp.MustCompile(`<!--\s*tusker:([a-z0-9_-]+):begin\s*-->`)
	endPattern := regexp.MustCompile(`<!--\s*tusker:([a-z0-9_-]+):end\s*-->`)
	open := map[string]int{}
	var out []v6ManagedBlock
	for i, line := range lines {
		if match := beginPattern.FindStringSubmatch(line); match != nil {
			open[match[1]] = i + 1
			continue
		}
		if match := endPattern.FindStringSubmatch(line); match != nil {
			if start := open[match[1]]; start > 0 {
				out = append(out, v6ManagedBlock{Name: match[1], Start: start, End: i + 1})
			}
		}
	}
	return out
}

func v6ExtractTables(sections map[string]v6Section) map[string]v6MarkdownTable {
	out := map[string]v6MarkdownTable{}
	for slug, section := range sections {
		var headers []string
		var rows [][]string
		for _, line := range strings.Split(section.Body, "\n") {
			trimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(trimmed, "|") || !strings.HasSuffix(trimmed, "|") {
				continue
			}
			if strings.Contains(trimmed, "---") {
				continue
			}
			cells := splitMarkdownTableRow(trimmed)
			if headers == nil {
				headers = cells
				continue
			}
			rows = append(rows, cells)
		}
		if len(headers) > 0 {
			out[slug] = v6MarkdownTable{Headers: headers, Rows: rows}
		}
	}
	return out
}

func v6LinksOut(doc v6IndexedMarkdown) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, link := range doc.WikiLinks {
		target := v6NormalizeLinkTarget(link.Target)
		if target == "" {
			continue
		}
		if _, ok := seen[target]; ok {
			continue
		}
		seen[target] = struct{}{}
		out = append(out, target)
	}
	for _, link := range doc.MarkdownLinks {
		target := strings.TrimSuffix(strings.TrimPrefix(filepath.ToSlash(link.Target), "domains/"), ".md")
		target = strings.TrimSuffix(target, "/CANON")
		target = strings.TrimSuffix(target, "/INDEX")
		if target == "" {
			continue
		}
		if _, ok := seen[target]; ok {
			continue
		}
		seen[target] = struct{}{}
		out = append(out, target)
	}
	sort.Strings(out)
	return out
}

func v6NormalizeLinkTarget(target string) string {
	target = strings.TrimSpace(target)
	target = strings.TrimPrefix(target, "domains/")
	target = strings.TrimSuffix(target, ".md")
	target = strings.TrimSuffix(target, "/CANON")
	target = strings.TrimSuffix(target, "/INDEX")
	if strings.HasSuffix(target, "/canon") {
		return target
	}
	if strings.HasSuffix(target, "/Canon") {
		return strings.TrimSuffix(target, "/Canon") + "/canon"
	}
	if strings.HasSuffix(target, "/CANON") {
		return strings.TrimSuffix(target, "/CANON") + "/canon"
	}
	return target
}

func v6ExtractWikiTargets(body string) []string {
	links := v6ExtractWikiLinks(body)
	seen := map[string]struct{}{}
	var out []string
	for _, link := range links {
		target := v6NormalizeLinkTarget(link.Target)
		if target == "" {
			continue
		}
		if _, ok := seen[target]; ok {
			continue
		}
		seen[target] = struct{}{}
		out = append(out, target)
	}
	sort.Strings(out)
	return out
}

func mapField(data map[string]any, key string) map[string]any {
	switch v := data[key].(type) {
	case map[string]any:
		return v
	case map[any]any:
		out := map[string]any{}
		for key, value := range v {
			out[toString(key)] = value
		}
		return out
	default:
		return nil
	}
}

func v6BuildBacklinks(nodes []v6KnowledgeRecord, tasks []v6TaskRecord) []v6BacklinkRecord {
	byNode := map[string]*v6BacklinkRecord{}
	ensure := func(node string) *v6BacklinkRecord {
		if byNode[node] == nil {
			byNode[node] = &v6BacklinkRecord{Node: node}
		}
		return byNode[node]
	}
	for _, node := range nodes {
		ensure(node.Node)
	}
	for _, node := range nodes {
		for _, target := range node.LinksOut {
			record := ensure(target)
			if !containsString(record.From, node.Node) {
				record.From = append(record.From, node.Node)
			}
		}
	}
	for _, task := range tasks {
		for _, node := range task.KnowledgeNodes {
			record := ensure(node)
			if !containsString(record.Tasks, task.ID) {
				record.Tasks = append(record.Tasks, task.ID)
			}
		}
		for _, raw := range task.KnowledgeResolution {
			row, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			node := stringValue(row["node"])
			if node == "" {
				continue
			}
			record := ensure(node)
			if !containsString(record.Tasks, task.ID) {
				record.Tasks = append(record.Tasks, task.ID)
			}
		}
	}
	var out []v6BacklinkRecord
	for _, record := range byNode {
		sort.Strings(record.From)
		sort.Strings(record.Tasks)
		out = append(out, *record)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Node < out[j].Node })
	return out
}

func v6BuildGraph(generatedAt string, index v6KnowledgeIndex) v6GraphIndex {
	graph := v6GraphIndex{Schema: "tusker.graph/v6", GeneratedAt: generatedAt}
	seenNodes := map[string]struct{}{}
	addNode := func(node v6GraphNode) {
		if node.ID == "" {
			return
		}
		if _, ok := seenNodes[node.ID]; ok {
			return
		}
		seenNodes[node.ID] = struct{}{}
		graph.Nodes = append(graph.Nodes, node)
	}
	addEdge := func(from, to, relation string) {
		if from == "" || to == "" {
			return
		}
		graph.Edges = append(graph.Edges, v6GraphEdge{From: from, To: to, Relation: relation})
	}
	for _, domain := range index.Domains {
		addNode(v6GraphNode{ID: domain.ID, Kind: "domain", Title: domain.Title, Path: domain.Path, Domain: domain.ID})
		for _, node := range domain.KnowledgeNodes {
			addEdge(domain.ID, node, "contains")
		}
		for _, epic := range domain.PrimaryEpics {
			addEdge(domain.ID, epic, "primary_epic")
		}
	}
	for _, node := range index.KnowledgeNodes {
		addNode(v6GraphNode{ID: node.Node, Kind: "knowledge", Title: node.Title, Path: node.Path, Domain: node.Domain})
		addEdge(node.Domain, node.Node, "domain")
		for _, target := range node.RelatedNodes {
			addEdge(node.Node, target, "related")
		}
		for _, target := range node.LinksOut {
			addEdge(node.Node, target, "links")
		}
	}
	for _, task := range index.Tasks {
		addNode(v6GraphNode{ID: task.ID, Kind: "task", Title: task.Title, Path: task.Path, Domain: task.PrimaryDomain})
		addEdge(task.ID, task.Epic, "epic")
		for _, domain := range task.Domains {
			addEdge(task.ID, domain, "touches_domain")
		}
		for _, node := range task.KnowledgeNodes {
			addEdge(task.ID, node, "touches_knowledge")
		}
	}
	for _, epic := range index.Epics {
		addNode(v6GraphNode{ID: epic.ID, Kind: "epic", Title: epic.Title, Path: epic.Path})
		for _, domain := range epic.PrimaryDomains {
			addEdge(epic.ID, domain, "primary_domain")
		}
		for _, node := range epic.KnowledgeNodes {
			addEdge(epic.ID, node, "knowledge")
		}
	}
	sort.Slice(graph.Nodes, func(i, j int) bool { return graph.Nodes[i].ID < graph.Nodes[j].ID })
	sort.Slice(graph.Edges, func(i, j int) bool {
		if graph.Edges[i].From != graph.Edges[j].From {
			return graph.Edges[i].From < graph.Edges[j].From
		}
		if graph.Edges[i].To != graph.Edges[j].To {
			return graph.Edges[i].To < graph.Edges[j].To
		}
		return graph.Edges[i].Relation < graph.Edges[j].Relation
	})
	return graph
}

func v6BuildFreshness(vaultPath string, nodes []v6KnowledgeRecord, tasks []v6TaskRecord) []v6FreshnessRecord {
	latest := map[string]map[string]any{}
	latestTask := map[string]string{}
	for _, task := range tasks {
		for _, raw := range task.KnowledgeResolution {
			row, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			node := stringValue(row["node"])
			if node == "" {
				continue
			}
			currentAt := firstNonEmpty(stringValue(row["at"]), stringValue(row["date"]))
			previousAt := ""
			if latest[node] != nil {
				previousAt = firstNonEmpty(stringValue(latest[node]["at"]), stringValue(latest[node]["date"]))
			}
			if latest[node] == nil || currentAt >= previousAt {
				latest[node] = row
				latestTask[node] = task.ID
			}
		}
	}
	var out []v6FreshnessRecord
	for _, node := range nodes {
		fingerprint, matched, missing := v6SourceFingerprint(vaultPath, node.SourceOfTruth, v6StaleWhenPaths(node))
		state := "unknown"
		if _, ok := v6HistoricalStatuses[node.CanonicalStatus]; ok {
			state = "historical"
		}
		row := latest[node.Node]
		recorded := ""
		lastStatus := ""
		lastAt := ""
		if row != nil {
			recorded = stringValue(row["source_fingerprint"])
			lastStatus = stringValue(row["status"])
			lastAt = firstNonEmpty(stringValue(row["at"]), stringValue(row["date"]))
			switch lastStatus {
			case "waived":
				state = "waived"
			case "applied", "verified_noop":
				if fingerprint != "" && recorded == fingerprint {
					state = "current"
				} else if fingerprint != "" {
					state = "stale"
				}
			}
		}
		if len(missing) > 0 && state != "historical" {
			state = "missing"
		} else if fingerprint != "" && row == nil && state != "historical" {
			state = "unknown"
		}
		out = append(out, v6FreshnessRecord{
			Node:              node.Node,
			State:             state,
			Fingerprint:       fingerprint,
			Recorded:          recorded,
			LastTask:          latestTask[node.Node],
			LastStatus:        lastStatus,
			LastAt:            lastAt,
			Missing:           missing,
			MatchedSourcePath: matched,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Node < out[j].Node })
	return out
}

func v6StaleWhenPaths(node v6KnowledgeRecord) []string {
	if node.StaleWhen == nil {
		return nil
	}
	return normalizeList(node.StaleWhen["paths"])
}

func v6SourceFingerprint(vaultPath string, sourceOfTruth, staleWhen []string) (string, []string, []string) {
	patterns := append([]string{}, sourceOfTruth...)
	patterns = append(patterns, staleWhen...)
	patterns = uniqueStrings(patterns)
	if len(patterns) == 0 {
		return "", nil, nil
	}
	repoRoot := v6RepoRoot(vaultPath)
	var matched []string
	var missing []string
	for _, pattern := range patterns {
		paths := v6ResolvePattern(repoRoot, pattern)
		if len(paths) == 0 {
			missing = append(missing, pattern)
			continue
		}
		matched = append(matched, paths...)
	}
	matched = uniqueStrings(matched)
	sort.Strings(matched)
	if len(matched) == 0 {
		return "", nil, missing
	}
	hash := sha256.New()
	for _, rel := range matched {
		abs := filepath.Join(repoRoot, filepath.FromSlash(rel))
		raw, err := os.ReadFile(abs)
		if err != nil {
			missing = append(missing, rel)
			continue
		}
		hash.Write([]byte(rel))
		hash.Write([]byte{0})
		hash.Write(raw)
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil)), matched, uniqueStrings(missing)
}

func v6RepoRoot(vaultPath string) string {
	clean := filepath.Clean(vaultPath)
	if filepath.Base(clean) == "tusker" {
		return filepath.Dir(clean)
	}
	return clean
}

func v6ResolvePattern(repoRoot, pattern string) []string {
	pattern = filepath.ToSlash(strings.TrimSpace(pattern))
	if pattern == "" || strings.HasPrefix(pattern, "external:") || strings.HasPrefix(pattern, "http://") || strings.HasPrefix(pattern, "https://") {
		return nil
	}
	if strings.Contains(pattern, "*") {
		return v6ResolveGlob(repoRoot, pattern)
	}
	abs := filepath.Join(repoRoot, filepath.FromSlash(pattern))
	if fileExists(abs) {
		return []string{pattern}
	}
	if strings.HasPrefix(pattern, "tusker/") {
		trimmed := strings.TrimPrefix(pattern, "tusker/")
		if fileExists(filepath.Join(repoRoot, filepath.FromSlash(trimmed))) {
			return []string{trimmed}
		}
	}
	return nil
}

func v6ResolveGlob(repoRoot, pattern string) []string {
	prefix := pattern
	if idx := strings.Index(prefix, "*"); idx >= 0 {
		prefix = prefix[:idx]
	}
	prefix = strings.TrimSuffix(prefix, "/")
	walkRoot := repoRoot
	if prefix != "" {
		walkRoot = filepath.Join(repoRoot, filepath.FromSlash(prefix))
		if info, err := os.Stat(walkRoot); err != nil || !info.IsDir() {
			walkRoot = filepath.Dir(walkRoot)
		}
	}
	regex := regexp.MustCompile("^" + regexp.QuoteMeta(pattern) + "$")
	regexText := regexp.QuoteMeta(pattern)
	regexText = strings.ReplaceAll(regexText, `\*\*`, `.*`)
	regexText = strings.ReplaceAll(regexText, `\*`, `[^/]*`)
	regex = regexp.MustCompile("^" + regexText + "$")
	var out []string
	_ = filepath.WalkDir(walkRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if regex.MatchString(rel) {
			out = append(out, rel)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func v6BuildPublication(nodes []v6KnowledgeRecord) []v6PublicationEntry {
	var out []v6PublicationEntry
	for _, node := range nodes {
		publish := node.Publish
		if publish == nil {
			continue
		}
		include := true
		if raw, ok := publish["include_in_llms"]; ok {
			include = boolValue(raw)
		}
		lane := firstNonEmpty(stringValue(publish["lane"]), node.Audience, "internal")
		route := firstNonEmpty(stringValue(publish["path"]), node.Node)
		out = append(out, v6PublicationEntry{
			Node:            node.Node,
			Title:           node.Title,
			Path:            node.Path,
			Route:           route,
			Lane:            lane,
			Audience:        node.Audience,
			Kind:            node.Kind,
			CanonicalStatus: node.CanonicalStatus,
			IncludeInLLMS:   include,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Node < out[j].Node })
	return out
}

func sortV6Index(index *v6KnowledgeIndex) {
	sort.Slice(index.Domains, func(i, j int) bool { return index.Domains[i].ID < index.Domains[j].ID })
	sort.Slice(index.KnowledgeNodes, func(i, j int) bool { return index.KnowledgeNodes[i].Node < index.KnowledgeNodes[j].Node })
	sort.Slice(index.Tasks, func(i, j int) bool { return index.Tasks[i].ID < index.Tasks[j].ID })
	sort.Slice(index.Epics, func(i, j int) bool { return index.Epics[i].ID < index.Epics[j].ID })
}

func writeV6GeneratedIndexes(vaultPath string, updateManaged bool) error {
	if !hasV6Vault(vaultPath) {
		return nil
	}
	index, err := v6IndexVault(vaultPath)
	if err != nil {
		return err
	}
	generatedDir := filepath.Join(vaultPath, "_system", "generated")
	if err := ensureDir(generatedDir); err != nil {
		return err
	}
	knowledgeMap := map[string]any{
		"schema":          "tusker.knowledge-map/v6",
		"generated_at":    index.GeneratedAt,
		"domains":         index.Domains,
		"knowledge_nodes": index.KnowledgeNodes,
		"tasks":           index.Tasks,
		"epics":           index.Epics,
	}
	if err := writeJSON(filepath.Join(vaultPath, v6KnowledgeMapRelative), knowledgeMap); err != nil {
		return err
	}
	routeIndex := map[string]any{"schema": "tusker.route-index/v6", "generated_at": index.GeneratedAt, "items": index.KnowledgeNodes}
	if err := writeJSON(filepath.Join(vaultPath, v6RouteIndexRelative), routeIndex); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(vaultPath, v6GraphIndexRelative), index.Graph); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(vaultPath, v6BacklinksRelative), map[string]any{"schema": "tusker.backlinks/v6", "generated_at": index.GeneratedAt, "items": index.Backlinks}); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(vaultPath, v6FreshnessRelative), map[string]any{"schema": "tusker.freshness/v6", "generated_at": index.GeneratedAt, "items": index.Freshness}); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(vaultPath, v6PublicationRelative), map[string]any{"schema": "tusker.publication/v6", "generated_at": index.GeneratedAt, "items": index.Publication}); err != nil {
		return err
	}
	if updateManaged {
		if err := updateV6ManagedBlocks(vaultPath, index); err != nil {
			return err
		}
	}
	return nil
}

func updateV6ManagedBlocks(vaultPath string, index v6KnowledgeIndex) error {
	if err := updateV6ProjectSkillDomains(vaultPath, index.Domains); err != nil {
		return err
	}
	backlinksByNode := map[string]v6BacklinkRecord{}
	for _, record := range index.Backlinks {
		backlinksByNode[record.Node] = record
	}
	for _, node := range index.KnowledgeNodes {
		if err := updateV6KnowledgeBackrefs(vaultPath, node, backlinksByNode[node.Node]); err != nil {
			return err
		}
	}
	tasksByDomain := v6OpenTasksByDomain(index.Tasks)
	for _, domain := range index.Domains {
		if err := updateV6DomainCurrentWork(vaultPath, domain, tasksByDomain[domain.ID]); err != nil {
			return err
		}
	}
	return nil
}

func updateV6ProjectSkillDomains(vaultPath string, domains []v6DomainRecord) error {
	path := filepath.Join(vaultPath, "SKILL.md")
	if !fileExists(path) {
		return nil
	}
	content, err := readText(path)
	if err != nil {
		return err
	}
	var lines []string
	lines = append(lines, "| Intent | Read first | Canon | Notes |", "|---|---|---|---|")
	for _, domain := range domains {
		lines = append(lines, fmt.Sprintf("| %s | [[%s/INDEX]] | [[%s/CANON]] | %s |", v6DomainIntent(domain.ID), domain.ID, domain.ID, fallback(domain.Summary, domain.Title)))
	}
	next := replaceManagedBlock(content, v6DomainsBlockBegin, v6DomainsBlockEnd, strings.Join(lines, "\n"))
	if next == content {
		return nil
	}
	return writeText(path, next)
}

func updateV6KnowledgeBackrefs(vaultPath string, node v6KnowledgeRecord, backlink v6BacklinkRecord) error {
	path := filepath.Join(vaultPath, filepath.FromSlash(node.Path))
	content, err := readText(path)
	if err != nil {
		return err
	}
	var lines []string
	if len(backlink.Tasks) == 0 {
		lines = append(lines, "_No task proof recorded yet._")
	} else {
		for _, task := range backlink.Tasks {
			lines = append(lines, "- [["+task+"]] touched this knowledge node.")
		}
	}
	next := replaceManagedBlock(content, v6BackrefsBlockBegin, v6BackrefsBlockEnd, strings.Join(lines, "\n"))
	if next == content {
		return nil
	}
	return writeText(path, next)
}

func updateV6DomainCurrentWork(vaultPath string, domain v6DomainRecord, tasks []v6TaskRecord) error {
	path := filepath.Join(vaultPath, filepath.FromSlash(domain.Path))
	content, err := readText(path)
	if err != nil {
		return err
	}
	var lines []string
	if len(tasks) == 0 {
		lines = append(lines, "_No open task currently targets this domain._")
	} else {
		for _, task := range tasks {
			lines = append(lines, fmt.Sprintf("- [[%s]] - %s (%s)", task.ID, task.Title, task.Status))
		}
	}
	next := replaceManagedBlock(content, v6CurrentWorkBlockBegin, v6CurrentWorkBlockEnd, strings.Join(lines, "\n"))
	if next == content {
		return nil
	}
	return writeText(path, next)
}

func replaceManagedBlock(content, begin, end, replacement string) string {
	start := strings.Index(content, begin)
	stop := strings.Index(content, end)
	if start == -1 || stop == -1 || stop < start {
		return content
	}
	prefix := content[:start+len(begin)]
	suffix := content[stop:]
	return strings.TrimRight(prefix, "\n") + "\n" + strings.TrimSpace(replacement) + "\n" + suffix
}

func v6OpenTasksByDomain(tasks []v6TaskRecord) map[string][]v6TaskRecord {
	out := map[string][]v6TaskRecord{}
	for _, task := range tasks {
		if !isOpenWorkStatus(task.Status) {
			continue
		}
		domains := append([]string{}, task.Domains...)
		if task.PrimaryDomain != "" && !containsString(domains, task.PrimaryDomain) {
			domains = append(domains, task.PrimaryDomain)
		}
		for _, domain := range domains {
			out[domain] = append(out[domain], task)
		}
	}
	for domain := range out {
		sort.Slice(out[domain], func(i, j int) bool { return out[domain][i].ID < out[domain][j].ID })
	}
	return out
}

func loadV6KnowledgeMap(vaultPath string) (v6KnowledgeIndex, error) {
	if err := writeV6GeneratedIndexes(vaultPath, false); err != nil {
		return v6KnowledgeIndex{}, err
	}
	raw, err := os.ReadFile(filepath.Join(vaultPath, v6KnowledgeMapRelative))
	if err != nil {
		return v6KnowledgeIndex{}, err
	}
	var payload struct {
		GeneratedAt    string              `json:"generated_at"`
		Domains        []v6DomainRecord    `json:"domains"`
		KnowledgeNodes []v6KnowledgeRecord `json:"knowledge_nodes"`
		Tasks          []v6TaskRecord      `json:"tasks"`
		Epics          []v6EpicRecord      `json:"epics"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return v6KnowledgeIndex{}, err
	}
	index, err := v6IndexVault(vaultPath)
	if err != nil {
		return v6KnowledgeIndex{}, err
	}
	index.GeneratedAt = payload.GeneratedAt
	return index, nil
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func validateV6Vault(vaultPath string, index v6KnowledgeIndex) ([]Issue, []Issue) {
	if !hasV6Vault(vaultPath) {
		return nil, nil
	}
	var errs, warns []Issue
	if !fileExists(filepath.Join(vaultPath, "SKILL.md")) {
		errs = append(errs, issue(errorMissingField, "V6 vault is missing tusker/SKILL.md", "SKILL.md", "", nil))
	}
	if !fileExists(filepath.Join(vaultPath, "domains", "codebase", "INDEX.md")) {
		errs = append(errs, issue(errorMissingField, "V6 vault is missing domains/codebase/INDEX.md", "domains/codebase/INDEX.md", "", nil))
	}
	if !fileExists(filepath.Join(vaultPath, "domains", "codebase", "CANON.md")) {
		errs = append(errs, issue(errorMissingField, "V6 vault is missing domains/codebase/CANON.md", "domains/codebase/CANON.md", "", nil))
	}
	aliasOwners := map[string][]string{}
	for _, node := range index.KnowledgeNodes {
		for _, alias := range node.Aliases {
			key := strings.ToLower(strings.TrimSpace(alias))
			if key != "" {
				aliasOwners[key] = append(aliasOwners[key], node.Node)
			}
		}
	}
	for alias, owners := range aliasOwners {
		owners = uniqueStrings(owners)
		if len(owners) > 1 {
			errs = append(errs, issue(errorInvalidField, fmt.Sprintf(`ambiguous V6 alias "%s" used by %s`, alias, strings.Join(owners, ", ")), "domains", "aliases must route to exactly one knowledge node", map[string]any{"alias": alias, "nodes": owners}))
		}
	}
	return errs, warns
}

func addV6ValidationLinkTarget(targets map[string]bool, target string) {
	normalized := v6NormalizeLinkTarget(target)
	if normalized == "" {
		return
	}
	targets[normalized] = true
	targets[strings.ToLower(normalized)] = true
}

func v6ValidationLinkTargetKnown(ctx validationContext, target string) bool {
	normalized := v6NormalizeLinkTarget(target)
	if normalized == "" {
		return true
	}
	if ctx.V6LinkTargets[normalized] {
		return true
	}
	return ctx.V6LinkTargets[strings.ToLower(normalized)]
}

func validateV6DocumentLinks(note Note, ctx validationContext, where string) []Issue {
	if ctx.VaultPath == "" || note.AbsolutePath == "" {
		return nil
	}
	doc, err := v6IndexMarkdownFile(ctx.VaultPath, note.AbsolutePath)
	if err != nil {
		return []Issue{issue(errorInvalidField, fmt.Sprintf("cannot index V6 links: %v", err), where, "", nil)}
	}
	var errors []Issue
	for _, link := range doc.WikiLinks {
		target := v6NormalizeLinkTarget(link.Target)
		if target == "" {
			continue
		}
		if !v6ValidationLinkTargetKnown(ctx, target) {
			errors = append(errors, issue(errorUnknownDocNode, fmt.Sprintf(`unresolved V6 wikilink "%s"`, link.Raw), where, "fix the link target or create the target note", map[string]any{"target": link.Target}))
		}
	}
	for _, link := range doc.MarkdownLinks {
		if docsIsExternalHref(link.Target) {
			continue
		}
		target := filepath.FromSlash(link.Target)
		var abs string
		if filepath.IsAbs(target) {
			abs = filepath.Join(ctx.VaultPath, strings.TrimPrefix(filepath.ToSlash(link.Target), "/"))
		} else {
			abs = filepath.Join(filepath.Dir(note.AbsolutePath), target)
		}
		abs = filepath.Clean(abs)
		if !fileExists(abs) {
			errors = append(errors, issue(errorUnknownDocNode, fmt.Sprintf(`unresolved V6 markdown link "%s"`, link.Raw), where, "fix the relative .md link or create the target file", map[string]any{"target": link.Target}))
		}
	}
	return errors
}

func validateV6Note(note Note, ctx validationContext, where string) ([]Issue, []Issue) {
	var errors []Issue
	var warnings []Issue
	data := note.Data
	body := note.Body
	kind := effectiveNoteKind(data)
	schema := stringField(data, "schema")
	for _, field := range []string{"schema", "title"} {
		if kind == "project-skill" && field == "title" {
			continue
		}
		if stringField(data, field) == "" {
			errors = append(errors, issue(errorMissingField, fmt.Sprintf(`missing required frontmatter "%s"`, field), where, "", map[string]any{"field": field}))
		}
	}
	switch kind {
	case "project-skill":
		if schema != "tusker.project-skill/v6" {
			errors = append(errors, issue(errorInvalidField, `project skill must use schema "tusker.project-skill/v6"`, where, "", nil))
		}
		if note.RelativePath != "SKILL.md" {
			errors = append(errors, issue(errorPathMismatch, `project skill must live at SKILL.md`, where, "", map[string]any{"expected": "SKILL.md", "actual": note.RelativePath}))
		}
		if stringField(data, "name") == "" {
			errors = append(errors, issue(errorMissingField, `project skill missing "name"`, where, "", map[string]any{"field": "name"}))
		}
	case "domain":
		errors = append(errors, validateV6DomainNote(note, ctx, where)...)
	case "knowledge":
		errors = append(errors, validateV6KnowledgeNote(note, ctx, where)...)
	case "task":
		errors = append(errors, validateV6TaskNote(note, ctx, where)...)
	case "epic":
		errors = append(errors, validateV6EpicNote(note, ctx, where)...)
	default:
		errors = append(errors, issue(errorUnknownType, fmt.Sprintf(`unknown V6 schema "%s"`, schema), where, "", map[string]any{"schema": schema}))
	}
	if kind == "knowledge" || kind == "domain" {
		for _, section := range []string{"## Read this when", "## Do not read this when"} {
			if findHeading(body, section) == nil {
				errors = append(errors, issue(errorMissingSection, fmt.Sprintf(`V6 %s missing section "%s"`, kind, section), where, "", map[string]any{"section": section}))
			}
		}
	}
	errors = append(errors, validateV6DocumentLinks(note, ctx, where)...)
	return errors, warnings
}

func validateV6DomainNote(note Note, ctx validationContext, where string) []Issue {
	var errors []Issue
	data := note.Data
	id := stringField(data, "id")
	if id == "" {
		errors = append(errors, issue(errorMissingField, `V6 domain missing "id"`, where, "", map[string]any{"field": "id"}))
	}
	if stringField(data, "status") == "" {
		errors = append(errors, issue(errorMissingField, `V6 domain missing "status"`, where, "", map[string]any{"field": "status"}))
	} else if _, ok := v6DomainStatuses[stringField(data, "status")]; !ok {
		errors = append(errors, issue(errorInvalidField, fmt.Sprintf(`invalid V6 domain status "%s"`, stringField(data, "status")), where, "", map[string]any{"field": "status"}))
	}
	expected := filepath.ToSlash(filepath.Join("domains", id, "INDEX.md"))
	if id != "" && note.RelativePath != expected {
		errors = append(errors, issue(errorPathMismatch, fmt.Sprintf(`V6 domain "%s" must live at "%s"`, id, expected), where, "", map[string]any{"expected": expected, "actual": note.RelativePath}))
	}
	if id != "" && !fileExists(filepath.Join(filepath.Dir(note.AbsolutePath), "CANON.md")) {
		errors = append(errors, issue(errorMissingField, fmt.Sprintf(`V6 domain "%s" is missing CANON.md`, id), where, "", nil))
	}
	for _, section := range []string{"## Current canon", "## Start here", "## Main knowledge nodes", "## Source of truth", "## Related domains", "## Current work"} {
		if findHeading(note.Body, section) == nil {
			errors = append(errors, issue(errorMissingSection, fmt.Sprintf(`V6 domain missing section "%s"`, section), where, "", map[string]any{"section": section}))
		}
	}
	for _, node := range normalizeList(data["knowledge_nodes"]) {
		if !ctx.V6KnowledgeNodes[node] {
			errors = append(errors, issue(errorUnknownDocNode, fmt.Sprintf(`unknown V6 knowledge node "%s"`, node), where, "run `tusker knowledge list` and fix knowledge_nodes", map[string]any{"node": node}))
		}
	}
	return errors
}

func validateV6KnowledgeNote(note Note, ctx validationContext, where string) []Issue {
	var errors []Issue
	data := note.Data
	node := stringField(data, "node")
	domain := stringField(data, "domain")
	if node == "" {
		errors = append(errors, issue(errorMissingField, `V6 knowledge missing "node"`, where, "", map[string]any{"field": "node"}))
	}
	if domain == "" {
		errors = append(errors, issue(errorMissingField, `V6 knowledge missing "domain"`, where, "", map[string]any{"field": "domain"}))
	} else if !ctx.V6Domains[domain] {
		errors = append(errors, issue(errorUnknownDomain, fmt.Sprintf(`unknown V6 domain "%s"`, domain), where, "create the domain or fix the knowledge page", map[string]any{"domain": domain}))
	}
	if node != "" && domain != "" && !strings.HasPrefix(node, domain+"/") {
		errors = append(errors, issue(errorInvalidField, fmt.Sprintf(`V6 knowledge node "%s" must start with domain "%s/"`, node, domain), where, "", map[string]any{"field": "node"}))
	}
	if node != "" {
		expectedPrefix := filepath.ToSlash(filepath.Join("domains", strings.Split(node, "/")[0]))
		if !strings.HasPrefix(note.RelativePath, expectedPrefix+"/") {
			errors = append(errors, issue(errorPathMismatch, fmt.Sprintf(`V6 knowledge node "%s" must live under "%s"`, node, expectedPrefix), where, "", nil))
		}
	}
	if kind := stringField(data, "kind"); kind == "" {
		errors = append(errors, issue(errorMissingField, `V6 knowledge missing "kind"`, where, "", map[string]any{"field": "kind"}))
	} else if _, ok := v6KnowledgeKinds[kind]; !ok {
		errors = append(errors, issue(errorInvalidField, fmt.Sprintf(`invalid V6 knowledge kind "%s"`, kind), where, "", map[string]any{"field": "kind"}))
	}
	if audience := stringField(data, "audience"); audience == "" {
		errors = append(errors, issue(errorMissingField, `V6 knowledge missing "audience"`, where, "", map[string]any{"field": "audience"}))
	} else if _, ok := docAudiences[audience]; !ok {
		errors = append(errors, issue(errorInvalidField, fmt.Sprintf(`invalid V6 knowledge audience "%s"`, audience), where, "", map[string]any{"field": "audience"}))
	}
	if layer := stringField(data, "agent_layer"); layer == "" {
		errors = append(errors, issue(errorMissingField, `V6 knowledge missing "agent_layer"`, where, "", map[string]any{"field": "agent_layer"}))
	} else if _, ok := docAgentLayers[layer]; !ok {
		errors = append(errors, issue(errorInvalidField, fmt.Sprintf(`invalid V6 knowledge agent_layer "%s"`, layer), where, "", map[string]any{"field": "agent_layer"}))
	}
	if status := stringField(data, "canonical_status"); status == "" {
		errors = append(errors, issue(errorMissingField, `V6 knowledge missing "canonical_status"`, where, "", map[string]any{"field": "canonical_status"}))
	} else if _, ok := v6CanonicalStatuses[status]; !ok {
		errors = append(errors, issue(errorInvalidField, fmt.Sprintf(`invalid V6 canonical_status "%s"`, status), where, "", map[string]any{"field": "canonical_status"}))
	}
	if len(normalizeList(data["source_of_truth"])) == 0 {
		errors = append(errors, issue(errorMissingField, `V6 knowledge missing source_of_truth`, where, "", map[string]any{"field": "source_of_truth"}))
	}
	for _, section := range []string{"## Source of truth", "## Related"} {
		if findHeading(note.Body, section) == nil {
			errors = append(errors, issue(errorMissingSection, fmt.Sprintf(`V6 knowledge missing section "%s"`, section), where, "", map[string]any{"section": section}))
		}
	}
	if stringField(data, "kind") == "canon" {
		for _, section := range []string{"## Current model", "## Invariants", "## Current defaults", "## Deprecated behavior", "## Source of truth", "## Open questions", "## Related"} {
			if findHeading(note.Body, section) == nil {
				errors = append(errors, issue(errorMissingSection, fmt.Sprintf(`V6 canon missing section "%s"`, section), where, "", map[string]any{"section": section}))
			}
		}
	}
	for _, related := range normalizeList(data["related_nodes"]) {
		if !ctx.V6KnowledgeNodes[related] {
			errors = append(errors, issue(errorUnknownDocNode, fmt.Sprintf(`unknown related V6 knowledge node "%s"`, related), where, "", map[string]any{"node": related}))
		}
	}
	return errors
}

func validateV6TaskNote(note Note, ctx validationContext, where string) []Issue {
	var errors []Issue
	data := note.Data
	body := note.Body
	id := stringField(data, "id")
	parsed := parseID(id)
	if parsed == nil || parsed.Kind != "task" {
		errors = append(errors, issue(errorIDScheme, fmt.Sprintf(`V6 task id "%s" does not match ABC-T-0001`, id), where, "", map[string]any{"id": id}))
	}
	if parsed != nil {
		expected := fmt.Sprintf("epics/%s/%s.md", parsed.Acronym, id)
		if note.RelativePath != expected {
			errors = append(errors, issue(errorPathMismatch, fmt.Sprintf(`V6 task "%s" must live at "%s"`, id, expected), where, "", nil))
		}
	}
	for _, field := range []string{"epic", "kind", "status", "risk", "size", "priority"} {
		if stringField(data, field) == "" {
			errors = append(errors, issue(errorMissingField, fmt.Sprintf(`V6 task missing "%s"`, field), where, "", map[string]any{"field": field}))
		}
	}
	if epic := stringField(data, "epic"); epic != "" {
		if _, ok := ctx.EpicAcronyms[epic]; !ok {
			errors = append(errors, issue(errorUnknownEpic, fmt.Sprintf(`unknown V6 task epic "%s"`, epic), where, "create the epic or fix the task frontmatter", map[string]any{"epic": epic}))
		}
		if parsed != nil && parsed.Acronym != epic {
			errors = append(errors, issue(errorEpicAcronymMismatch, fmt.Sprintf(`task "%s" belongs to epic "%s" but id acronym is "%s"`, id, epic, parsed.Acronym), where, "", map[string]any{"id": id, "epic": epic}))
		}
	}
	if kind := stringField(data, "kind"); kind != "" {
		if _, ok := changeTypes[kind]; !ok {
			errors = append(errors, issue(errorInvalidField, fmt.Sprintf(`invalid V6 task kind "%s"`, kind), where, "", map[string]any{"field": "kind"}))
		}
	}
	if status := stringField(data, "status"); status != "" {
		if _, ok := taskStatuses[status]; !ok {
			errors = append(errors, issue(errorInvalidField, fmt.Sprintf(`invalid V6 task status "%s"`, status), where, "", map[string]any{"field": "status"}))
		}
	}
	if risk := stringField(data, "risk"); risk != "" {
		if _, ok := risks[risk]; !ok {
			errors = append(errors, issue(errorInvalidField, fmt.Sprintf(`invalid V6 task risk "%s"`, risk), where, "", map[string]any{"field": "risk"}))
		}
	}
	if size := stringField(data, "size"); size != "" {
		if _, ok := sizes[size]; !ok {
			errors = append(errors, issue(errorInvalidField, fmt.Sprintf(`invalid V6 task size "%s"`, size), where, "", map[string]any{"field": "size"}))
		}
	}
	if priority := stringField(data, "priority"); priority != "" {
		if _, ok := priorities[priority]; !ok {
			errors = append(errors, issue(errorInvalidField, fmt.Sprintf(`invalid V6 task priority "%s"`, priority), where, "", map[string]any{"field": "priority"}))
		}
	}
	if domain := stringField(data, "primary_domain"); domain != "" && !ctx.V6Domains[domain] {
		errors = append(errors, issue(errorUnknownDomain, fmt.Sprintf(`unknown V6 primary_domain "%s"`, domain), where, "", map[string]any{"domain": domain}))
	}
	for _, domain := range normalizeList(data["domains"]) {
		if !ctx.V6Domains[domain] {
			errors = append(errors, issue(errorUnknownDomain, fmt.Sprintf(`unknown V6 domain "%s"`, domain), where, "", map[string]any{"domain": domain}))
		}
	}
	for _, node := range normalizeList(data["knowledge_nodes"]) {
		if !ctx.V6KnowledgeNodes[node] {
			errors = append(errors, issue(errorUnknownDocNode, fmt.Sprintf(`unknown V6 knowledge node "%s"`, node), where, "", map[string]any{"node": node}))
		}
	}
	for _, section := range []string{"## Intent", "## Read this when", "## Acceptance", "## Verification plan", "## Verification log", "## Evidence"} {
		if findHeading(body, section) == nil {
			errors = append(errors, issue(errorMissingSection, fmt.Sprintf(`V6 task missing section "%s"`, section), where, "", map[string]any{"section": section}))
		}
	}
	if (len(normalizeList(data["knowledge_nodes"])) > 0 || boolField(data, "knowledge_change")) && findHeading(body, "## Knowledge delta") == nil {
		errors = append(errors, issue(errorMissingKnowledgeDelta, `V6 task with knowledge_nodes requires Knowledge delta`, where, "", nil))
	}
	if stringField(data, "status") == "done" {
		for _, proofIssue := range v6TaskProofIssues(data, body) {
			errors = append(errors, issue(errorEvidenceGate, proofIssue, where, "done V6 tasks must carry acceptance, evidence, and verification proof", map[string]any{"id": id}))
		}
		if len(normalizeList(data["knowledge_nodes"])) > 0 {
			if issues := knowledgeImpactFreshnessIssues(data, ctx.V6Freshness); len(issues) > 0 {
				errors = append(errors, issue(errorDocsImpactUnresolved, `V6 task has stale or unresolved knowledge_nodes`, where, "run `tusker knowledge check`, then apply/noop/waive each node with current sources", map[string]any{"knowledge_nodes": normalizeList(data["knowledge_nodes"]), "issues": issues}))
			}
		}
	}
	return errors
}

func validateV6EpicNote(note Note, ctx validationContext, where string) []Issue {
	var errors []Issue
	data := note.Data
	id := stringField(data, "id")
	parsed := parseID(id)
	if parsed == nil || parsed.Kind != "epic" {
		errors = append(errors, issue(errorIDScheme, fmt.Sprintf(`V6 epic id "%s" must be a 3-letter acronym`, id), where, "", map[string]any{"id": id}))
	} else {
		expected := fmt.Sprintf("epics/%s/%s.md", id, id)
		if note.RelativePath != expected {
			errors = append(errors, issue(errorPathMismatch, fmt.Sprintf(`V6 epic "%s" must live at "%s"`, id, expected), where, "", nil))
		}
	}
	for _, domain := range normalizeList(data["primary_domains"]) {
		if !ctx.V6Domains[domain] {
			errors = append(errors, issue(errorUnknownDomain, fmt.Sprintf(`unknown V6 primary_domain "%s"`, domain), where, "", map[string]any{"domain": domain}))
		}
	}
	for _, node := range normalizeList(data["knowledge_nodes"]) {
		if !ctx.V6KnowledgeNodes[node] {
			errors = append(errors, issue(errorUnknownDocNode, fmt.Sprintf(`unknown V6 knowledge node "%s"`, node), where, "", map[string]any{"node": node}))
		}
	}
	return errors
}

func knowledgeImpactResolved(data map[string]any) bool {
	required := map[string]struct{}{}
	for _, node := range normalizeList(data["knowledge_nodes"]) {
		required[node] = struct{}{}
	}
	for _, raw := range anySlice(data["knowledge_resolution"]) {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		node := stringValue(row["node"])
		status := stringValue(row["status"])
		if status == "applied" || status == "verified_noop" || status == "waived" {
			delete(required, node)
		}
	}
	return len(required) == 0
}

func v6TaskProofIssues(data map[string]any, body string) []string {
	id := stringField(data, "id")
	var issues []string
	if stringField(data, "verified_at") == "" {
		issues = append(issues, id+": done requires verified_at")
	}
	if !sectionHasSubstance(body, "## Acceptance") {
		issues = append(issues, id+`: done requires substantive "## Acceptance" proof`)
	}
	if !sectionHasSubstance(body, "## Evidence") {
		issues = append(issues, id+`: done requires substantive "## Evidence" proof`)
	}
	if strings.TrimSpace(stringField(data, "verification_summary")) == "" && !sectionHasSubstance(body, "## Verification log") {
		issues = append(issues, id+`: done requires verification_summary or substantive "## Verification log" proof`)
	}
	return issues
}

func v6FreshnessByNode(index v6KnowledgeIndex) map[string]v6FreshnessRecord {
	out := map[string]v6FreshnessRecord{}
	for _, freshness := range index.Freshness {
		out[freshness.Node] = freshness
	}
	return out
}

func v6ResolutionRowsByNode(data map[string]any) map[string]map[string]any {
	rows := map[string]map[string]any{}
	atByNode := map[string]string{}
	for _, raw := range anySlice(data["knowledge_resolution"]) {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		node := stringValue(row["node"])
		if node == "" {
			continue
		}
		at := firstNonEmpty(stringValue(row["at"]), stringValue(row["date"]))
		if _, ok := rows[node]; !ok || at >= atByNode[node] {
			rows[node] = row
			atByNode[node] = at
		}
	}
	return rows
}

func knowledgeImpactFreshnessIssues(data map[string]any, freshnessByNode map[string]v6FreshnessRecord) []string {
	required := normalizeList(data["knowledge_nodes"])
	if len(required) == 0 {
		return nil
	}
	rows := v6ResolutionRowsByNode(data)
	var issues []string
	for _, node := range required {
		row := rows[node]
		if row == nil {
			issues = append(issues, fmt.Sprintf("%s has no knowledge_resolution row", node))
			continue
		}
		status := stringValue(row["status"])
		switch status {
		case "waived":
			if strings.TrimSpace(stringValue(row["reason"])) == "" {
				issues = append(issues, fmt.Sprintf("%s waiver is missing a reason", node))
			}
			continue
		case "applied", "verified_noop":
		default:
			issues = append(issues, fmt.Sprintf("%s resolution status %q is not closed", node, status))
			continue
		}
		recorded := strings.TrimSpace(stringValue(row["source_fingerprint"]))
		if recorded == "" {
			issues = append(issues, fmt.Sprintf("%s resolution is missing source_fingerprint", node))
			continue
		}
		freshness, ok := freshnessByNode[node]
		if !ok {
			issues = append(issues, fmt.Sprintf("%s has no freshness record", node))
			continue
		}
		if len(freshness.Missing) > 0 || freshness.State == "missing" {
			issues = append(issues, fmt.Sprintf("%s source paths are missing", node))
			continue
		}
		if strings.TrimSpace(freshness.Fingerprint) == "" {
			issues = append(issues, fmt.Sprintf("%s current source fingerprint is unknown", node))
			continue
		}
		if recorded != freshness.Fingerprint {
			issues = append(issues, fmt.Sprintf("%s source fingerprint changed since %s", node, firstNonEmpty(stringValue(row["at"]), "resolution")))
		}
	}
	return issues
}
