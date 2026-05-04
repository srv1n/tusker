package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const docsMapRelative = "_config/docs-map.yaml"

type DocsMap struct {
	Schema    string                   `yaml:"schema" json:"schema"`
	Generated string                   `yaml:"generated" json:"generated"`
	Domains   map[string]DocsMapDomain `yaml:"domains" json:"domains"`
	Nodes     []DocsMapNode            `yaml:"nodes" json:"nodes"`
}

type DocsMapDomain struct {
	Label       string `yaml:"label" json:"label"`
	OwnerEpic   string `yaml:"owner_epic" json:"owner_epic,omitempty"`
	Description string `yaml:"description" json:"description"`
}

type DocsMapNode struct {
	ID                 string           `yaml:"id" json:"id"`
	Title              string           `yaml:"title" json:"title"`
	Page               string           `yaml:"page" json:"page"`
	Path               string           `yaml:"path" json:"path"`
	Domain             string           `yaml:"domain" json:"domain"`
	Mode               string           `yaml:"mode" json:"mode"`
	AgentLayer         string           `yaml:"agent_layer" json:"agent_layer"`
	Role               string           `yaml:"role" json:"role"`
	Audience           string           `yaml:"audience" json:"audience"`
	Kind               string           `yaml:"kind" json:"kind"`
	Covers             []string         `yaml:"covers" json:"covers,omitempty"`
	SourceOfTruth      []string         `yaml:"source_of_truth" json:"source_of_truth,omitempty"`
	StaleWhen          DocsMapStaleWhen `yaml:"stale_when" json:"stale_when,omitempty"`
	Evals              []string         `yaml:"evals" json:"evals,omitempty"`
	PublishLane        string           `yaml:"publish_lane" json:"publish_lane"`
	PublishPath        string           `yaml:"publish_path" json:"publish_path"`
	PublishDescription string           `yaml:"publish_description" json:"publish_description"`
}

type DocsMapStaleWhen struct {
	Paths []string `yaml:"paths" json:"paths,omitempty"`
}

func (m *DocsMap) UnmarshalYAML(value *yaml.Node) error {
	type docsMapRaw struct {
		Schema    string                   `yaml:"schema"`
		Generated string                   `yaml:"generated"`
		Domains   map[string]DocsMapDomain `yaml:"domains"`
		Nodes     yaml.Node                `yaml:"nodes"`
	}
	var raw docsMapRaw
	if err := value.Decode(&raw); err != nil {
		return err
	}
	m.Schema = raw.Schema
	m.Generated = raw.Generated
	m.Domains = raw.Domains
	if m.Domains == nil {
		m.Domains = map[string]DocsMapDomain{}
	}
	switch raw.Nodes.Kind {
	case 0:
		m.Nodes = nil
	case yaml.SequenceNode:
		var nodes []DocsMapNode
		if err := raw.Nodes.Decode(&nodes); err != nil {
			return err
		}
		m.Nodes = nodes
	case yaml.MappingNode:
		nodes := make([]DocsMapNode, 0, len(raw.Nodes.Content)/2)
		for i := 0; i+1 < len(raw.Nodes.Content); i += 2 {
			key := strings.TrimSpace(raw.Nodes.Content[i].Value)
			var node DocsMapNode
			if err := raw.Nodes.Content[i+1].Decode(&node); err != nil {
				return err
			}
			if node.ID == "" {
				node.ID = key
			}
			nodes = append(nodes, node)
		}
		m.Nodes = nodes
	default:
		return fmt.Errorf("docs-map nodes must be a list or mapping")
	}
	return nil
}

func loadDocsMap(vaultPath string) (*DocsMap, error) {
	mapPath := filepath.Join(vaultPath, docsMapRelative)
	if !fileExists(mapPath) {
		return nil, nil
	}
	raw, err := os.ReadFile(mapPath)
	if err != nil {
		return nil, err
	}
	var docsMap DocsMap
	if err := yaml.Unmarshal(raw, &docsMap); err != nil {
		return nil, err
	}
	if docsMap.Domains == nil {
		docsMap.Domains = map[string]DocsMapDomain{}
	}
	return &docsMap, nil
}

func (m *DocsMap) HasDomain(domain string) bool {
	if m == nil {
		return true
	}
	_, ok := m.Domains[strings.TrimSpace(domain)]
	return ok
}

func (m *DocsMap) Node(id string) (DocsMapNode, bool) {
	if m == nil {
		return DocsMapNode{}, true
	}
	id = strings.TrimSpace(id)
	for _, node := range m.Nodes {
		if node.ID == id {
			return node, true
		}
	}
	return DocsMapNode{}, false
}

func (n DocsMapNode) SourcePath() string {
	return firstNonEmpty(n.Page, n.Path)
}

func (n DocsMapNode) EffectiveMode() string {
	return strings.TrimSpace(n.Mode)
}

func (n DocsMapNode) EffectiveAgentLayer() string {
	return strings.TrimSpace(n.AgentLayer)
}

func firstNonEmptyList(values ...[]string) []string {
	for _, value := range values {
		if len(value) > 0 {
			return value
		}
	}
	return nil
}

func validateDocsMapConfig(docsMap *DocsMap) []Issue {
	if docsMap == nil {
		return nil
	}
	var issues []Issue
	where := docsMapRelative
	if strings.TrimSpace(docsMap.Schema) != "tusker.docs-map/v5" {
		issues = append(issues, issue(errorConfigInvalid, `docs-map schema must be "tusker.docs-map/v5"`, where, "", map[string]any{"field": "schema", "value": docsMap.Schema}))
	}
	seen := map[string]struct{}{}
	for _, node := range docsMap.Nodes {
		id := strings.TrimSpace(node.ID)
		if id == "" {
			issues = append(issues, issue(errorConfigInvalid, "docs-map node is missing id", where, "", nil))
			continue
		}
		if _, ok := seen[id]; ok {
			issues = append(issues, issue(errorConfigInvalid, fmt.Sprintf(`duplicate docs-map node "%s"`, id), where, "", map[string]any{"node": id}))
		}
		seen[id] = struct{}{}
		if reason := validateDocNodePath(id); reason != "" {
			issues = append(issues, issue(errorConfigInvalid, fmt.Sprintf(`invalid docs-map node id "%s": %s`, id, reason), where, "", map[string]any{"node": id}))
		}
		path := node.SourcePath()
		if path == "" {
			issues = append(issues, issue(errorConfigInvalid, fmt.Sprintf(`docs-map node "%s" is missing path`, id), where, "", map[string]any{"node": id, "field": "path"}))
		} else if reason := validateDocNodePath(strings.TrimSuffix(strings.TrimPrefix(path, "docs/"), ".md")); reason != "" {
			issues = append(issues, issue(errorConfigInvalid, fmt.Sprintf(`docs-map node "%s" has invalid path "%s": %s`, id, path, reason), where, "", map[string]any{"node": id, "field": "path"}))
		}
		if node.Domain == "" {
			issues = append(issues, issue(errorMissingField, fmt.Sprintf(`docs-map node "%s" is missing domain`, id), where, "", map[string]any{"node": id, "field": "domain"}))
		} else if _, ok := docsMap.Domains[node.Domain]; !ok {
			issues = append(issues, issue(errorUnknownDomain, fmt.Sprintf(`docs-map node "%s" uses unknown domain "%s"`, id, node.Domain), where, "add the domain or fix the node", map[string]any{"node": id, "domain": node.Domain}))
		}
		if node.Mode == "" {
			issues = append(issues, issue(errorMissingField, fmt.Sprintf(`docs-map node "%s" is missing mode`, id), where, "", map[string]any{"node": id, "field": "mode"}))
		} else if _, ok := docModes[node.EffectiveMode()]; !ok {
			issues = append(issues, issue(errorInvalidField, fmt.Sprintf(`docs-map node "%s" has invalid mode "%s"`, id, node.EffectiveMode()), where, "", map[string]any{"node": id, "field": "mode"}))
		}
		if _, ok := docAudiences[strings.TrimSpace(node.Audience)]; !ok {
			issues = append(issues, issue(errorInvalidField, fmt.Sprintf(`docs-map node "%s" has invalid audience "%s"`, id, node.Audience), where, "", map[string]any{"node": id, "field": "audience"}))
		}
		if node.AgentLayer == "" {
			issues = append(issues, issue(errorMissingField, fmt.Sprintf(`docs-map node "%s" is missing agent_layer`, id), where, "", map[string]any{"node": id, "field": "agent_layer"}))
		} else if _, ok := docAgentLayers[node.EffectiveAgentLayer()]; !ok {
			issues = append(issues, issue(errorInvalidField, fmt.Sprintf(`docs-map node "%s" has invalid agent_layer "%s"`, id, node.EffectiveAgentLayer()), where, "", map[string]any{"node": id, "field": "agent_layer"}))
		}
		if len(node.SourceOfTruth) == 0 {
			issues = append(issues, issue(errorMissingField, fmt.Sprintf(`docs-map node "%s" is missing source_of_truth`, id), where, "", map[string]any{"node": id, "field": "source_of_truth"}))
		}
		if len(node.StaleWhen.Paths) == 0 {
			issues = append(issues, issue(errorMissingField, fmt.Sprintf(`docs-map node "%s" is missing stale_when.paths`, id), where, "", map[string]any{"node": id, "field": "stale_when.paths"}))
		}
	}
	return issues
}

func validateDocNodePath(node string) string {
	node = strings.TrimSpace(node)
	if node == "" {
		return "must not be empty"
	}
	if filepath.IsAbs(node) || strings.HasPrefix(node, "/") {
		return "must be relative"
	}
	if strings.Contains(node, "\\") {
		return "must use forward slashes"
	}
	if strings.HasSuffix(node, "/") {
		return "must not end with /"
	}
	segments := strings.Split(node, "/")
	for _, segment := range segments {
		if segment == "" {
			return "must not contain empty segments"
		}
		if segment == "." || segment == ".." {
			return `must not contain "." or ".." segments`
		}
	}
	return ""
}

func defaultDocsMapYAML(date string) string {
	return `schema: tusker.docs-map/v5
generated: "` + date + `"

domains:
  schema:
    label: Schema
    description: Note types, frontmatter, IDs, parser rules.
  workflow:
    label: Workflow
    description: Lifecycle, close flow, review gates, docs gates.
  docs:
    label: Docs
    description: Durable documentation pages, publication pipeline, docs impact.
  cli:
    label: CLI
    description: User and agent command surface.
  runtime:
    label: Runtime
    description: Daemon behavior, runs, events, generated caches.
  obsidian:
    label: Obsidian
    description: Views, dashboard, vault readability.
  adoption:
    label: Adoption
    description: V5 setup, operator rollout, and local conventions.
  skill:
    label: Skill
    description: SKILL.md, AGENTS guidance, repo-local agent instructions.
  docs-system:
    label: Documentation system
    description: Diátaxis catalog, docs freshness, doc nodes, and agent docs.

nodes:
  - id: spec/v5-overview
    title: Tusker v5 overview
    page: docs/spec/v5-overview.md
    domain: adoption
    mode: explanation
    audience: developer
    agent_layer: capsule
    kind: canon
    source_of_truth: [research/tusker_v5_implementation_spec.md]
    stale_when:
      paths: [cmd/tusker/**, skill/**, tusker/_config/docs-map.yaml]
    publish_lane: internal
    publish_path: spec/v5-overview
    publish_description: Implementation spec for Tusker v5.
  - id: spec/v5-adoption
    title: Tusker v5 adoption guide
    page: docs/spec/v5-adoption.md
    domain: adoption
    mode: how-to
    audience: developer
    agent_layer: none
    kind: guide
    source_of_truth: [research/tusker_v5_implementation_spec.md, AGENTS.md]
    stale_when:
      paths: [cmd/tusker/**, skill/**, tusker/WORKFLOW.md]
    publish_lane: internal
    publish_path: spec/v5-adoption
    publish_description: Adoption guide for Tusker v5.
  - id: reference/cli
    title: Tusker CLI reference
    page: docs/reference/cli.md
    domain: cli
    mode: reference
    audience: developer
    agent_layer: capsule
    kind: reference
    source_of_truth: [cmd/tusker/cli.go, cmd/tusker/commands_v5.go]
    stale_when:
      paths: [cmd/tusker/**, skill/references/COMMANDS.md]
    publish_lane: internal
    publish_path: reference/cli
    publish_description: Primary v5 CLI surface.
  - id: reference/validator
    title: Tusker validator reference
    page: docs/reference/validator.md
    domain: schema
    mode: reference
    audience: developer
    agent_layer: capsule
    kind: reference
    source_of_truth: [cmd/tusker/schema.go, cmd/tusker/v5_validation.go]
    stale_when:
      paths: [cmd/tusker/schema.go, cmd/tusker/v5_validation.go, skill/references/SCHEMA.md]
    publish_lane: internal
    publish_path: reference/validator
    publish_description: Validator rollout, failure codes, and section requirements.
  - id: reference/templates
    title: Template contract
    page: docs/reference/templates.md
    domain: schema
    mode: reference
    audience: developer
    agent_layer: capsule
    kind: reference
    source_of_truth: [cmd/tusker/v5_templates.go, skill/assets/templates]
    stale_when:
      paths: [cmd/tusker/v5_templates.go, skill/assets/templates/**]
    publish_lane: internal
    publish_path: reference/templates
    publish_description: Epic, task, bug-task, and doc template contract.
  - id: tusker/docs-system
    title: Documentation freshness system
    page: docs/reference/docs-pipeline.md
    domain: docs-system
    mode: explanation
    audience: developer
    agent_layer: capsule
    kind: canon
    source_of_truth: [cmd/tusker/docs_map.go, cmd/tusker/docs_impact.go, cmd/tusker/docs_export.go]
    stale_when:
      paths: [cmd/tusker/docs_*.go, cmd/tusker/docs_map.go, skill/references/DOCS_PUBLICATION.md]
    publish_lane: internal
    publish_path: reference/docs-pipeline
    publish_description: Docs routing, impact hook, waiver flow, and publication rules.
  - id: reference/runtime
    title: Runtime state and events
    page: docs/reference/runtime.md
    domain: runtime
    mode: reference
    audience: developer
    agent_layer: capsule
    kind: canon
    source_of_truth: [cmd/tusker/daemon.go, cmd/tusker/runtime_store.go]
    stale_when:
      paths: [cmd/tusker/daemon.go, cmd/tusker/runtime_store.go, skill/docs/ORCHESTRATION_RUNBOOK.md]
    publish_lane: internal
    publish_path: reference/runtime
    publish_description: Current-state frontmatter, audit events, runs, and generated caches.
  - id: reference/obsidian
    title: Obsidian vault guide
    page: docs/reference/obsidian.md
    domain: obsidian
    mode: how-to
    audience: developer
    agent_layer: none
    kind: guide
    source_of_truth: [cmd/tusker/v5_templates.go, skill/references/BASES.md]
    stale_when:
      paths: [tusker/_system/views/**, skill/assets/bases/**, skill/references/BASES.md]
    publish_lane: internal
    publish_path: reference/obsidian
    publish_description: Dashboard, views, and vault readability requirements.
  - id: reference/skill
    title: Skill and agent workflow
    page: docs/reference/skill.md
    domain: skill
    mode: reference
    audience: developer
    agent_layer: capsule
    kind: canon
    source_of_truth: [skill/SKILL.md, skill/references/**, AGENTS.md]
    stale_when:
      paths: [skill/**, AGENTS.md, CLAUDE.md]
    publish_lane: internal
    publish_path: reference/skill
    publish_description: SKILL.md and AGENTS progressive-disclosure guidance.
  - id: agents/tusker-skill
    title: "Agent recipe: using Tusker"
    page: docs/agents/use-tusker.md
    domain: skill
    mode: how-to
    audience: agent
    agent_layer: standalone
    kind: runbook
    source_of_truth: [skill/SKILL.md, AGENTS.md]
    stale_when:
      paths: [skill/**, AGENTS.md, CLAUDE.md, cmd/tusker/cli.go]
    publish_lane: internal
    publish_path: agents/use-tusker
    publish_description: Agent-facing procedure for using Tusker safely.
`
}
