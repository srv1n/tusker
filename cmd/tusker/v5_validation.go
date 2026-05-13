package main

import (
	"fmt"
	"regexp"
	"strings"
)

func validateV5Note(note Note, ctx validationContext, where string) ([]Issue, []Issue) {
	var errors []Issue
	var warnings []Issue
	data := note.Data
	body := note.Body
	noteType := stringField(data, "type")
	id := stringField(data, "id")

	for _, field := range []string{"schema", "id", "title", "type", "created", "updated"} {
		if stringField(data, field) == "" {
			errors = append(errors, issue(errorMissingField, fmt.Sprintf(`missing required frontmatter "%s"`, field), where, "", map[string]any{"field": field}))
		}
	}

	parsed := parseID(id)
	if noteType == "doc" {
		if stringField(data, "node") == "" {
			errors = append(errors, issue(errorMissingField, `v5 doc missing "node"`, where, "", map[string]any{"field": "node"}))
		}
		if stringField(data, "canonical_status") != "" {
			if _, ok := canonicalStatuses[stringField(data, "canonical_status")]; !ok {
				errors = append(errors, issue(errorInvalidField, fmt.Sprintf(`invalid canonical_status "%s"`, stringField(data, "canonical_status")), where, "", nil))
			}
		}
		if stringField(data, "audience") != "" {
			if _, ok := docAudiences[stringField(data, "audience")]; !ok {
				errors = append(errors, issue(errorInvalidField, fmt.Sprintf(`invalid audience "%s"`, stringField(data, "audience")), where, "", map[string]any{"field": "audience"}))
			}
		}
		if stringField(data, "mode") != "" {
			if _, ok := docModes[stringField(data, "mode")]; !ok {
				errors = append(errors, issue(errorInvalidField, fmt.Sprintf(`invalid mode "%s"`, stringField(data, "mode")), where, "", map[string]any{"field": "mode"}))
			}
		}
		if stringField(data, "agent_layer") != "" {
			if _, ok := docAgentLayers[stringField(data, "agent_layer")]; !ok {
				errors = append(errors, issue(errorInvalidField, fmt.Sprintf(`invalid agent_layer "%s"`, stringField(data, "agent_layer")), where, "", map[string]any{"field": "agent_layer"}))
			}
		}
		for _, domain := range normalizeList(data["domains"]) {
			if !ctx.DocsMap.HasDomain(domain) {
				errors = append(errors, issue(errorUnknownDomain, fmt.Sprintf(`unknown domain "%s"`, domain), where, "add it to _config/docs-map.yaml or fix the note", map[string]any{"domain": domain}))
			}
		}
		if boolField(data, "publish") {
			if strings.TrimSpace(stringField(data, "publish_path")) == "" {
				errors = append(errors, issue(errorPublishPathMissing, `publish: true requires publish_path`, where, "", nil))
			}
			if strings.TrimSpace(stringField(data, "publish_description")) == "" {
				errors = append(errors, issue(errorPublishDescriptionMissing, `publish: true requires publish_description`, where, "", nil))
			}
		}
		return errors, warnings
	}

	if parsed == nil {
		errors = append(errors, issue(errorIDScheme, fmt.Sprintf(`id "%s" does not match the v5 scheme`, id), where, `task ids look like ABC-T-0001; epic ids are 3 uppercase letters`, map[string]any{"id": id}))
	} else if noteType == "task" && parsed.Kind != "task" {
		errors = append(errors, issue(errorIDKindMismatch, fmt.Sprintf(`id kind "%s" does not match type "task"`, parsed.Kind), where, "", map[string]any{"id": id}))
	} else if noteType == "epic" && parsed.Kind != "epic" {
		errors = append(errors, issue(errorIDKindMismatch, fmt.Sprintf(`id kind "%s" does not match type "epic"`, parsed.Kind), where, "", map[string]any{"id": id}))
	}

	if parsed != nil && ctx.RelativePath != "" {
		expected := fmt.Sprintf("epics/%s/%s.md", parsed.Acronym, parsed.Acronym)
		if parsed.Kind == "task" {
			expected = fmt.Sprintf("epics/%s/%s.md", parsed.Acronym, id)
		}
		if parsed.Kind != "doc" && ctx.RelativePath != expected {
			errors = append(errors, issue(errorPathMismatch, fmt.Sprintf(`file at "%s" declares id "%s" but v5 path is "%s"`, ctx.RelativePath, id, expected), where, "move the file to the V5 path", map[string]any{"expected": expected, "actual": ctx.RelativePath}))
		}
	}

	switch noteType {
	case "epic":
		if _, ok := taskStatuses[stringField(data, "status")]; !ok {
			errors = append(errors, issue(errorInvalidField, fmt.Sprintf(`invalid epic status "%s"`, stringField(data, "status")), where, "", map[string]any{"field": "status"}))
		}
		for _, section := range []string{"## Thesis", "## Scope", "## Success metrics", "## Canon", "## Task stack"} {
			if findHeading(body, section) == nil {
				warnings = append(warnings, issue(errorMissingSection, fmt.Sprintf(`epic missing v5 section "%s"`, section), where, "", map[string]any{"section": section}))
			}
		}
	case "task":
		if _, ok := taskStatuses[stringField(data, "status")]; !ok {
			errors = append(errors, issue(errorInvalidField, fmt.Sprintf(`invalid task status "%s"`, stringField(data, "status")), where, "", map[string]any{"field": "status"}))
		}
		if stringField(data, "status") == "blocked" && len(normalizeList(data["blocked_by"])) == 0 && strings.TrimSpace(stringField(data, "block_reason")) == "" {
			errors = append(errors, issue(errorMissingField, `blocked task requires blocked_by or block_reason`, where, "set blocked_by for Tusker dependencies or block_reason for an external blocker", map[string]any{"field": "block_reason"}))
		}
		if _, ok := changeTypes[stringField(data, "kind")]; !ok {
			errors = append(errors, issue(errorInvalidField, fmt.Sprintf(`invalid or missing kind "%s"`, stringField(data, "kind")), where, "", map[string]any{"field": "kind"}))
		}
		if _, ok := risks[stringField(data, "risk")]; !ok {
			errors = append(errors, issue(errorInvalidField, fmt.Sprintf(`invalid or missing risk "%s"`, stringField(data, "risk")), where, "", map[string]any{"field": "risk"}))
		}
		if _, ok := sizes[stringField(data, "size")]; !ok {
			errors = append(errors, issue(errorInvalidField, fmt.Sprintf(`invalid or missing size "%s"`, stringField(data, "size")), where, "", map[string]any{"field": "size"}))
		}
		if _, ok := priorities[stringField(data, "priority")]; !ok {
			errors = append(errors, issue(errorInvalidField, fmt.Sprintf(`invalid or missing priority "%s"`, stringField(data, "priority")), where, "", map[string]any{"field": "priority"}))
		}
		if value := stringField(data, "delegation"); value != "" {
			if _, ok := delegations[value]; !ok {
				errors = append(errors, issue(errorInvalidField, fmt.Sprintf(`invalid delegation "%s"`, value), where, "", map[string]any{"field": "delegation"}))
			}
		}
		if value := stringField(data, "ai_assistance"); value != "" {
			if _, ok := aiAssistanceLevels[value]; !ok {
				errors = append(errors, issue(errorInvalidField, fmt.Sprintf(`invalid ai_assistance "%s"`, value), where, "", map[string]any{"field": "ai_assistance"}))
			}
		}
		epicRef := wikiTarget(data["epic"])
		if epicRef == "" {
			errors = append(errors, issue(errorOrphanWork, "task has no epic", where, "", map[string]any{"field": "epic"}))
		} else if _, ok := ctx.EpicAcronyms[epicRef]; !ok {
			errors = append(errors, issue(errorUnknownEpic, fmt.Sprintf(`epic "%s" does not exist`, epicRef), where, "", map[string]any{"epic": epicRef}))
		}
		for _, domain := range normalizeList(data["domains"]) {
			if !ctx.DocsMap.HasDomain(domain) {
				errors = append(errors, issue(errorUnknownDomain, fmt.Sprintf(`unknown domain "%s"`, domain), where, "add it to _config/docs-map.yaml or fix the task", map[string]any{"domain": domain}))
			}
		}
		for _, node := range normalizeList(data["doc_nodes"]) {
			if _, ok := ctx.DocsMap.Node(node); !ok {
				errors = append(errors, issue(errorUnknownDocNode, fmt.Sprintf(`unknown doc_node "%s"`, node), where, "add it to _config/docs-map.yaml or fix the task", map[string]any{"doc_node": node}))
			}
		}
		for _, node := range normalizeList(data["knowledge_nodes"]) {
			if len(ctx.V6KnowledgeNodes) > 0 && !ctx.V6KnowledgeNodes[node] {
				errors = append(errors, issue(errorUnknownDocNode, fmt.Sprintf(`unknown V6 knowledge_node "%s"`, node), where, "run `tusker knowledge list` and fix knowledge_nodes", map[string]any{"knowledge_node": node}))
			}
		}
		for _, row := range parseKnowledgeDeltaRows(body) {
			for _, node := range row.DocNodes {
				if _, ok := ctx.DocsMap.Node(node); !ok {
					errors = append(errors, issue(errorUnknownDocNode, fmt.Sprintf(`unknown Knowledge delta doc_node "%s"`, node), where, "add it to _config/docs-map.yaml or fix the Knowledge delta row", map[string]any{"doc_node": node}))
				}
			}
			if row.Mode != "" {
				if _, ok := docModes[row.Mode]; !ok {
					errors = append(errors, issue(errorInvalidField, fmt.Sprintf(`invalid Knowledge delta mode "%s"`, row.Mode), where, "", map[string]any{"field": "mode"}))
				}
			}
			if row.Audience != "" {
				if _, ok := docAudiences[row.Audience]; !ok {
					errors = append(errors, issue(errorInvalidField, fmt.Sprintf(`invalid Knowledge delta audience "%s"`, row.Audience), where, "", map[string]any{"field": "audience"}))
				}
			}
		}
		validateV5TaskSections(body, stringField(data, "risk"), where, &errors, &warnings)
		if riskNeedsKnowledgeDelta(stringField(data, "risk")) && !hasValidKnowledgeDelta(body) {
			errors = append(errors, issue(errorMissingKnowledgeDelta, `risk high/critical task requires a non-tautological Knowledge delta row`, where, "fill the Knowledge delta table with a real before -> after reader-facing change", nil))
		}
		if stringField(data, "status") == "done" && len(normalizeList(data["doc_nodes"])) > 0 && !docsImpactResolved(data) {
			errors = append(errors, issue(errorDocsImpactUnresolved, `task has doc_nodes but docs impact is unresolved`, where, "run `tusker docs check`, then `tusker docs apply` or `tusker docs waive` for each node", map[string]any{"doc_nodes": normalizeList(data["doc_nodes"])}))
		}
		if stringField(data, "status") == "done" && len(normalizeList(data["knowledge_nodes"])) > 0 {
			if len(ctx.V6Freshness) > 0 {
				if issues := knowledgeImpactFreshnessIssues(data, ctx.V6Freshness); len(issues) > 0 {
					errors = append(errors, issue(errorDocsImpactUnresolved, `task has stale or unresolved V6 knowledge_nodes`, where, "run `tusker knowledge check`, then apply/noop/waive each node with current sources", map[string]any{"knowledge_nodes": normalizeList(data["knowledge_nodes"]), "issues": issues}))
				}
			} else if !knowledgeImpactResolved(data) {
				errors = append(errors, issue(errorDocsImpactUnresolved, `task has unresolved V6 knowledge_nodes`, where, "run `tusker knowledge check`, then apply/noop/waive each node", map[string]any{"knowledge_nodes": normalizeList(data["knowledge_nodes"])}))
			}
		}
	}
	return errors, warnings
}

func validateV5TaskSections(body, risk, where string, errors, warnings *[]Issue) {
	required := []string{"## Intent", "## Acceptance contract", "## Evidence"}
	switch strings.ToLower(risk) {
	case "medium":
		required = []string{"## Intent", "## Scope", "## Acceptance contract", "## Deliverables", "## Verification plan", "## Evidence"}
	case "high":
		required = []string{"## Intent", "## Scope", "## Acceptance contract", "## Canon", "## Code/system anchors", "## Constraints", "## Deliverables", "## Verification plan", "## Knowledge delta", "## Evidence", "## Verification log"}
	case "critical":
		required = []string{"## Intent", "## Scope", "## Acceptance contract", "## Canon", "## Code/system anchors", "## Constraints", "## Deliverables", "## Verification plan", "## Knowledge delta", "## Rollback", "## Evidence", "## Verification log"}
	}
	for _, section := range required {
		if findHeading(body, section) == nil {
			*warnings = append(*warnings, issue(errorMissingSection, fmt.Sprintf(`task missing v5 section "%s"`, section), where, "", map[string]any{"section": section}))
		}
	}
}

func riskNeedsKnowledgeDelta(risk string) bool {
	return strings.EqualFold(risk, "high") || strings.EqualFold(risk, "critical")
}

func hasValidKnowledgeDelta(body string) bool {
	rows := parseKnowledgeDeltaRows(body)
	content := sectionContent(body, "## Knowledge delta")
	if content == "" {
		return false
	}
	tautology := regexp.MustCompile(`(?i)\b(updated|changed|implemented|fixed)\b`)
	for _, row := range rows {
		if row.Before == "" || row.After == "" || row.Before == row.After {
			continue
		}
		if tautology.MatchString(row.Before) && tautology.MatchString(row.After) && len(row.Before) < 32 && len(row.After) < 32 {
			continue
		}
		return true
	}
	return false
}

type knowledgeDeltaRow struct {
	ChangeType string
	Topic      string
	Before     string
	After      string
	Audience   string
	DocNodes   []string
	Mode       string
	Status     string
}

func parseKnowledgeDeltaRows(body string) []knowledgeDeltaRow {
	content := sectionContent(body, "## Knowledge delta")
	if content == "" {
		return nil
	}
	var headers []string
	var rows []knowledgeDeltaRow
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") || strings.Contains(trimmed, "---") {
			continue
		}
		cells := splitMarkdownTableRow(trimmed)
		if len(cells) == 0 {
			continue
		}
		if headers == nil {
			headers = make([]string, len(cells))
			for i, cell := range cells {
				headers[i] = knowledgeDeltaColumn(cell)
			}
			continue
		}
		row := knowledgeDeltaRow{}
		for i, cell := range cells {
			if i >= len(headers) {
				continue
			}
			value := strings.TrimSpace(cell)
			switch headers[i] {
			case "change_type":
				row.ChangeType = value
			case "topic":
				row.Topic = value
			case "before":
				row.Before = value
			case "after":
				row.After = value
			case "audience":
				row.Audience = strings.ToLower(value)
			case "doc_nodes":
				row.DocNodes = parseKnowledgeDeltaDocNodes(value)
			case "mode":
				row.Mode = strings.ToLower(value)
			case "status":
				row.Status = strings.ToLower(value)
			}
		}
		if row.Topic != "" || row.Before != "" || row.After != "" || len(row.DocNodes) > 0 {
			rows = append(rows, row)
		}
	}
	return rows
}

func knowledgeDeltaColumn(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.Trim(value, "`")
	value = strings.ReplaceAll(value, "-", "_")
	value = strings.ReplaceAll(value, " ", "_")
	switch value {
	case "change", "change_type", "type":
		return "change_type"
	case "doc_node", "doc_nodes", "knowledge_node", "knowledge_nodes", "target_doc_nodes", "target_docs", "target_knowledge_nodes", "target_knowledge", "targets":
		return "doc_nodes"
	default:
		return value
	}
}

func parseKnowledgeDeltaDocNodes(value string) []string {
	value = strings.NewReplacer(";", ",", "<br>", ",", "<br/>", ",").Replace(value)
	var nodes []string
	for _, node := range splitCSV(value) {
		node = strings.TrimSpace(node)
		node = strings.Trim(node, "`")
		node = strings.TrimPrefix(strings.TrimSuffix(node, "]]"), "[[")
		node = strings.TrimSpace(node)
		switch strings.ToLower(node) {
		case "", "-", "none", "n/a", "na":
			continue
		}
		if node != "" {
			nodes = append(nodes, node)
		}
	}
	return nodes
}

func splitMarkdownTableRow(row string) []string {
	row = strings.Trim(row, "|")
	parts := strings.Split(row, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func docsImpactResolved(data map[string]any) bool {
	required := map[string]struct{}{}
	for _, node := range normalizeList(data["doc_nodes"]) {
		required[node] = struct{}{}
	}
	for _, raw := range anySlice(data["docs_resolution"]) {
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
