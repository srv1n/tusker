package main

import (
	"fmt"
	"path/filepath"
	"strings"
)

func docsImpactCheckCmd(args Args) error {
	args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	id, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	note, err := resolveNote(vaultPath, id)
	if err != nil {
		return err
	}
	nodes := normalizeList(note.Data["doc_nodes"])
	deltas := parseKnowledgeDeltaRows(note.Body)
	for _, row := range deltas {
		for _, node := range row.DocNodes {
			if !containsString(nodes, node) {
				nodes = append(nodes, node)
			}
		}
	}
	docsMap, err := loadDocsMap(vaultPath)
	if err != nil {
		return err
	}
	if docsMap == nil {
		return tuskerError(errorNotFound, "_config/docs-map.yaml not found", withHint("run `tusker init --yes` or create the docs map before docs impact checks"))
	}
	var rows []map[string]any
	for _, node := range nodes {
		target, ok := docsMap.Node(node)
		status := "patch_or_waiver_required"
		page := ""
		if ok {
			page = target.SourcePath()
			if fileExists(filepath.Join(vaultPath, filepath.FromSlash(page))) {
				status = "needs_review"
			}
		} else {
			status = "unknown_node"
		}
		rows = append(rows, map[string]any{"node": node, "page": page, "status": status, "deltas": knowledgeDeltasForNode(deltas, node)})
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "id": id, "doc_nodes": rows, "knowledge_delta": deltas})
		return nil
	}
	if len(rows) == 0 {
		fmt.Printf("%s has no doc_nodes; docs impact is not required.\n", id)
		return nil
	}
	fmt.Printf("Docs impact for %s:\n", id)
	for _, row := range rows {
		fmt.Printf("- %s -> %s (%s)\n", row["node"], fallback(stringValue(row["page"]), "(unmapped)"), row["status"])
		for _, raw := range anySlice(row["deltas"]) {
			delta, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			fmt.Printf("  delta: %s — %s -> %s\n", fallback(stringValue(delta["topic"]), "(untitled)"), stringValue(delta["before"]), stringValue(delta["after"]))
		}
	}
	fmt.Println("Resolve each node with `tusker docs apply <id> --node <node>` or `tusker docs waive <id> <node> --reason ...`.")
	return nil
}

func docsImpactApplyCmd(args Args) error {
	return recordDocsImpactResolution(args, "applied")
}

func docsImpactNoopCmd(args Args) error {
	return recordDocsImpactResolution(args, "verified_noop")
}

func docsImpactWaiveCmd(args Args) error {
	return recordDocsImpactResolution(args, "waived")
}

func recordDocsImpactResolution(args Args, status string) error {
	args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
	args["node"] = firstNonEmpty(args.String("node"), args.String("_pos1"))
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	id, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	node, err := requireArg(args, "node")
	if err != nil {
		return err
	}
	note, err := resolveNote(vaultPath, id)
	if err != nil {
		return err
	}
	if stringField(note.Data, "type") != "task" {
		return tuskerError(errorInvalidArg, "docs impact resolution applies to v5 tasks")
	}
	allowedNodes := normalizeList(note.Data["doc_nodes"])
	for _, row := range parseKnowledgeDeltaRows(note.Body) {
		for _, target := range row.DocNodes {
			if !containsString(allowedNodes, target) {
				allowedNodes = append(allowedNodes, target)
			}
		}
	}
	if !containsString(allowedNodes, node) {
		return tuskerError(errorInvalidArg, fmt.Sprintf(`task %s does not target doc_node "%s"`, id, node))
	}
	data, body, err := parseFrontmatterMustRead(note.AbsolutePath)
	if err != nil {
		return err
	}
	date := todayISO()
	actor := fallback(fallback(args.String("actor"), args.String("by")), "automation")
	reason := strings.TrimSpace(args.String("reason"))
	if status == "waived" && reason == "" {
		return tuskerError(errorMissingArg, "docs waiver requires --reason", withContext(map[string]any{"id": id, "node": node}))
	}
	resolutions := anySlice(data["docs_resolution"])
	var next []any
	for _, item := range resolutions {
		row, ok := item.(map[string]any)
		if ok && stringValue(row["node"]) == node {
			continue
		}
		next = append(next, item)
	}
	next = append(next, map[string]any{
		"node":   node,
		"status": status,
		"actor":  actor,
		"date":   date,
		"reason": reason,
	})
	data["docs_resolution"] = next
	data["updated"] = date
	body = appendWorkLogBullet(body, fmt.Sprintf("%s — %s — docs %s for %s%s", date, actor, status, node, suffixReason(reason)))
	content, err := serializeDocument(data, body, frontmatterOrderForType("task"))
	if err != nil {
		return err
	}
	if err := writeText(note.AbsolutePath, content); err != nil {
		return err
	}
	autoReindex(vaultPath)
	if !args.Bool("quiet") {
		fmt.Printf("%s docs %s for %s\n", id, status, node)
	}
	return nil
}

func knowledgeDeltasForNode(rows []knowledgeDeltaRow, node string) []map[string]any {
	var out []map[string]any
	for _, row := range rows {
		if len(row.DocNodes) > 0 && !containsString(row.DocNodes, node) {
			continue
		}
		out = append(out, map[string]any{
			"change_type": row.ChangeType,
			"topic":       row.Topic,
			"before":      row.Before,
			"after":       row.After,
			"audience":    row.Audience,
			"mode":        row.Mode,
			"status":      row.Status,
		})
	}
	return out
}
