package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

func reindex(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	notes, err := listAllNotes(vaultPath)
	if err != nil {
		return err
	}
	docsMap, err := loadDocsMap(vaultPath)
	if err != nil {
		return err
	}
	fixedLinks := 0
	if args.Bool("fix-links") {
		fixedLinks, err = repairRecordIDMirrors(notes)
		if err != nil {
			return err
		}
		if fixedLinks > 0 {
			notes, err = listAllNotes(vaultPath)
			if err != nil {
				return err
			}
		}
	}
	var epics, tasks, docs, links []map[string]any
	var records []map[string]any
	epicMap := map[string]Note{}
	for _, note := range notes {
		if stringField(note.Data, "type") == "epic" {
			epicMap[stringField(note.Data, "id")] = note
		}
	}
	for _, note := range notes {
		base := baseIndexShape(note)
		records = append(records, base)
		noteType := stringField(note.Data, "type")
		switch noteType {
		case "epic":
			base["owner"] = stringField(note.Data, "owner")
			base["summary"] = stringField(note.Data, "summary")
			base["spec_source"] = stringField(note.Data, "spec_source")
			base["target_release"] = stringField(note.Data, "target_release")
			base["docs"] = normalizeList(note.Data["docs"])
			epics = append(epics, base)
		case "task":
			epicRef := wikiTarget(note.Data["epic"])
			if epicRef == "" {
				epicRef = stringField(note.Data, "epic")
			}
			base["change_type"] = stringField(note.Data, "change_type")
			base["kind"] = firstNonEmpty(stringField(note.Data, "kind"), stringField(note.Data, "change_type"))
			base["size"] = stringField(note.Data, "size")
			base["risk"] = stringField(note.Data, "risk")
			base["priority"] = stringField(note.Data, "priority")
			base["domains"] = normalizeList(note.Data["domains"])
			base["doc_nodes"] = normalizeList(note.Data["doc_nodes"])
			base["delegation"] = stringField(note.Data, "delegation")
			base["surfaces"] = normalizeList(note.Data["surfaces"])
			base["assignee"] = stringField(note.Data, "assignee")
			base["requester"] = stringField(note.Data, "requester")
			base["ai_assistance"] = stringField(note.Data, "ai_assistance")
			base["ai_tools"] = normalizeList(note.Data["ai_tools"])
			base["docs_resolution"] = anySlice(note.Data["docs_resolution"])
			base["verified_at"] = stringField(note.Data, "verified_at")
			base["closed_at"] = stringField(note.Data, "closed_at")
			base["work_revision"] = intField(note.Data, "work_revision")
			base["dod_code_complete"] = boolField(note.Data, "dod_code_complete")
			base["dod_user_verified"] = boolField(note.Data, "dod_user_verified")
			base["due"] = stringField(note.Data, "due")
			base["epic"] = epicRef
			base["epic_record_id"] = stringField(note.Data, "epic_record_id")
			base["epicTitle"] = stringField(epicMap[epicRef].Data, "title")
			tasks = append(tasks, base)
			links = append(links, collectLinks(note)...)
		case "doc":
			nodeID := stringField(note.Data, "node")
			mappedNode, hasMappedNode := DocsMapNode{}, false
			if docsMap != nil {
				mappedNode, hasMappedNode = docsMap.Node(nodeID)
			}
			epicRef := wikiTarget(note.Data["epic"])
			base["epic"] = epicRef
			base["epic_record_id"] = stringField(note.Data, "epic_record_id")
			base["epicTitle"] = stringField(epicMap[epicRef].Data, "title")
			base["audience"] = firstNonEmpty(stringField(note.Data, "audience"), mappedNode.Audience)
			base["node"] = nodeID
			base["kind"] = stringField(note.Data, "kind")
			base["domains"] = normalizeList(note.Data["domains"])
			if len(normalizeList(note.Data["domains"])) == 0 && mappedNode.Domain != "" {
				base["domains"] = []string{mappedNode.Domain}
			}
			base["mode"] = firstNonEmpty(stringField(note.Data, "mode"), mappedNode.EffectiveMode())
			base["agent_layer"] = firstNonEmpty(stringField(note.Data, "agent_layer"), mappedNode.EffectiveAgentLayer())
			base["source_of_truth"] = firstNonEmptyList(normalizeList(note.Data["source_of_truth"]), mappedNode.SourceOfTruth)
			base["stale_when_paths"] = firstNonEmptyList(normalizeList(note.Data["stale_when_paths"]), mappedNode.StaleWhen.Paths)
			base["docs_map_path"] = mappedNode.SourcePath()
			base["docs_map_role"] = mappedNode.Role
			base["docs_map_evals"] = mappedNode.Evals
			base["docs_map_node"] = hasMappedNode
			base["doc_intent"] = stringField(note.Data, "doc_intent")
			base["owner_epic"] = wikiTarget(note.Data["owner_epic"])
			base["canon_for"] = wikiTarget(note.Data["canon_for"])
			base["canonical"] = boolField(note.Data, "canonical")
			base["canonical_status"] = stringField(note.Data, "canonical_status")
			base["verified_at"] = firstNonEmpty(stringField(note.Data, "last_verified_at"), stringField(note.Data, "verified_at"))
			base["deprecated"] = boolField(note.Data, "deprecated")
			base["superseded_by"] = stringField(note.Data, "superseded_by")
			base["redirect_from"] = normalizeList(note.Data["redirect_from"])
			base["publish"] = boolField(note.Data, "publish")
			base["publish_lane"] = firstNonEmpty(stringField(note.Data, "publish_lane"), mappedNode.PublishLane)
			base["publish_path"] = firstNonEmpty(stringField(note.Data, "publish_path"), mappedNode.PublishPath)
			base["publish_description"] = firstNonEmpty(stringField(note.Data, "publish_description"), mappedNode.PublishDescription)
			base["publish_order"] = optionalIntValue(note.Data["publish_order"])
			base["publish_section_title"] = stringField(note.Data, "publish_section_title")
			base["publish_url"] = stringField(note.Data, "publish_url")
			base["published_at"] = stringField(note.Data, "published_at")
			docs = append(docs, base)
			links = append(links, collectLinks(note)...)
		}
	}
	for _, collection := range [][]map[string]any{epics, tasks, docs} {
		sortByUpdatedDesc(collection)
	}
	counts := map[string]map[string]int{}
	for _, epic := range epics {
		counts[stringValue(epic["id"])] = map[string]int{"tasks": 0, "bug_tasks": 0, "docs": 0, "open": 0, "done": 0}
	}
	for _, task := range tasks {
		c := counts[stringValue(task["epic"])]
		if c == nil {
			continue
		}
		c["tasks"]++
		if stringValue(task["kind"]) == "bug" {
			c["bug_tasks"]++
		}
		status := stringValue(task["status"])
		if status == "done" {
			c["done"]++
		} else if status != "cancelled" {
			c["open"]++
		}
	}
	for _, doc := range docs {
		c := counts[stringValue(doc["epic"])]
		if c == nil {
			continue
		}
		c["docs"]++
	}
	for _, epic := range epics {
		epic["counts"] = counts[stringValue(epic["id"])]
	}
	for _, epicNote := range epicMap {
		acronym := stringField(epicNote.Data, "id")
		var children []map[string]any
		for _, collection := range [][]map[string]any{tasks, docs} {
			for _, item := range collection {
				if stringValue(item["epic"]) == acronym {
					children = append(children, item)
				}
			}
		}
		list := "_No tasks or docs yet._"
		if len(children) > 0 {
			var lines []string
			for _, child := range children {
				lines = append(lines, fmt.Sprintf("- [[%s]] — %s (%s)", stringValue(child["id"]), stringValue(child["title"]), stringValue(child["status"])))
			}
			list = strings.Join(lines, "\n")
		}
		data, body, err := parseFrontmatterMustRead(epicNote.AbsolutePath)
		if err != nil {
			return err
		}
		newBody := replaceSection(body, "## Task stack", list)
		if newBody == body {
			newBody = replaceSection(body, "## Stories", list)
		}
		if newBody != body {
			content, err := serializeDocument(data, newBody, frontmatterOrder["epic"])
			if err != nil {
				return err
			}
			if err := writeText(epicNote.AbsolutePath, content); err != nil {
				return err
			}
		}
	}
	verificationQueue := []map[string]any{}
	for _, item := range tasks {
		if stringValue(item["status"]) != "review" || stringValue(item["verified_at"]) != "" {
			continue
		}
		verificationQueue = append(verificationQueue, map[string]any{
			"id":       item["id"],
			"title":    item["title"],
			"type":     item["type"],
			"kind":     item["kind"],
			"epic":     item["epic"],
			"risk":     item["risk"],
			"priority": item["priority"],
			"path":     item["path"],
		})
	}
	publicationQueue := []map[string]any{}
	publicationManifestDocs := []map[string]any{}
	for _, doc := range docs {
		if !boolValue(doc["publish"]) {
			continue
		}
		entry := map[string]any{"kind": "doc"}
		for key, value := range doc {
			entry[key] = value
		}
		publicationQueue = append(publicationQueue, entry)
		if publicationManifestReady(doc) {
			publicationManifestDocs = append(publicationManifestDocs, publicationManifestItem(doc))
		}
	}
	sortByUpdatedDesc(publicationQueue)
	sortByUpdatedDesc(publicationManifestDocs)
	work := append([]map[string]any{}, tasks...)
	var activeQueue, blockedQueue, verificationReviewQueue, reviewQueue, reworkQueue []map[string]any
	for _, item := range work {
		switch stringValue(item["status"]) {
		case "active":
			activeQueue = append(activeQueue, item)
		case "blocked":
			blockedQueue = append(blockedQueue, item)
		case "review":
			if stringValue(item["verified_at"]) == "" {
				verificationReviewQueue = append(verificationReviewQueue, item)
			} else {
				reviewQueue = append(reviewQueue, item)
			}
		case "rework":
			reworkQueue = append(reworkQueue, item)
		}
	}
	var configData *Config
	config, _, _, loadErr := loadConfig(vaultPath)
	if loadErr == nil {
		configData = &config
	}
	generatedAt := time.Now().UTC().Format(time.RFC3339)
	summary := map[string]any{
		"generatedAt": generatedAt,
		"counts": map[string]any{
			"epics":             len(epics),
			"tasks":             len(tasks),
			"bugTasks":          countKind(work, "bug"),
			"docs":              len(docs),
			"openWork":          len(activeQueue) + len(blockedQueue) + len(verificationReviewQueue) + len(reviewQueue) + len(reworkQueue),
			"inReview":          countStatus(work, "review"),
			"verificationQueue": len(verificationQueue),
			"publicationQueue":  countNotPublished(publicationQueue),
			"active":            len(activeQueue),
			"blocked":           len(blockedQueue),
			"verification":      len(verificationReviewQueue),
			"rework":            len(reworkQueue),
		},
	}
	docsCatalog := buildDocsCatalog(docsMap, docs, tasks)
	dashboard := map[string]any{
		"generatedAt": generatedAt,
		"counts":      summary["counts"],
		"queues": map[string]any{
			"active":              activeQueue,
			"blocked":             blockedQueue,
			"verification":        verificationReviewQueue,
			"review":              reviewQueue,
			"rework":              reworkQueue,
			"verification_review": verificationQueue,
			"publication":         publicationQueue,
		},
		"docs_catalog": docsCatalog,
		"agents":       buildAgentsSection(configData, map[string]int{}),
	}
	runtimeSection := loadDashboardRuntime(vaultPath)
	dashboard["runtime"] = runtimeSection
	if counts, ok := dashboard["counts"].(map[string]any); ok {
		counts["running"] = len(anySlice(runtimeSection["active_runs"]))
	}
	if configData != nil {
		dashboard["config"] = map[string]any{
			"poll_interval_seconds": configData.Poll.IntervalSeconds,
			"hook_timeout_seconds":  configData.HookTimeoutSeconds,
			"retry":                 configData.Retry,
			"workspace_root":        configData.Workspace.Root,
		}
	} else {
		dashboard["config"] = nil
	}
	publicationManifest := map[string]any{
		"generatedAt": generatedAt,
		"counts": map[string]any{
			"vault_docs": len(publicationManifestDocs),
		},
		"items": publicationManifestDocs,
		"sources": map[string]any{
			"vault_docs": publicationManifestDocs,
		},
	}
	generatedDir := filepath.Join(vaultPath, "_system", "generated")
	if err := ensureDir(generatedDir); err != nil {
		return err
	}
	for _, target := range []struct {
		Name string
		Data any
	}{
		{"epics.index.json", map[string]any{"generatedAt": generatedAt, "items": epics}},
		{"tasks.index.json", map[string]any{"generatedAt": generatedAt, "items": tasks}},
		{"docs.index.json", map[string]any{"generatedAt": generatedAt, "items": docs, "catalog": docsCatalog}},
		{"records.index.json", map[string]any{"generatedAt": generatedAt, "items": records}},
		{"links.index.json", map[string]any{"generatedAt": generatedAt, "items": links}},
		{"verification.index.json", map[string]any{"generatedAt": generatedAt, "items": verificationQueue}},
		{"publication.index.json", publicationManifest},
		{"summary.json", summary},
		{"dashboard.json", dashboard},
	} {
		if err := writeJSON(filepath.Join(generatedDir, target.Name), target.Data); err != nil {
			return err
		}
	}
	for _, stale := range []string{"stories.index.json", "bugs.index.json", "attestation.index.json"} {
		_ = os.Remove(filepath.Join(generatedDir, stale))
	}
	if err := writeVaultReadme(vaultPath, epics, tasks, docs, generatedAt); err != nil {
		return err
	}
	if err := writeDashboardNote(vaultPath, runtimeSection, docsCatalog, generatedAt); err != nil {
		return err
	}
	if err := writeDocsCatalogNote(vaultPath, docsCatalog, generatedAt); err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "summary": summary, "fixed_links": fixedLinks})
		return nil
	}
	if !args.Bool("quiet") {
		if args.Bool("fix-links") {
			fmt.Printf("Repaired record-id mirrors in %d note%s.\n", fixedLinks, plural(fixedLinks))
		}
		fmt.Printf("Indexed %d epics, %d tasks, %d docs.\n", len(epics), len(tasks), len(docs))
		fmt.Printf("Tracker queues - active: %d, blocked: %d, verification: %d, review: %d, rework: %d\n", len(activeQueue), len(blockedQueue), len(verificationReviewQueue), len(reviewQueue), len(reworkQueue))
	}
	return nil
}

func repairRecordIDMirrors(notes []Note) (int, error) {
	idToRecordID := map[string]string{}
	for _, note := range notes {
		id := stringField(note.Data, "id")
		recordID := stringField(note.Data, "record_id")
		if id != "" && recordID != "" {
			idToRecordID[id] = recordID
		}
	}

	changed := 0
	for _, note := range notes {
		noteType := stringField(note.Data, "type")
		if !managedNoteType(noteType) {
			continue
		}
		data, body, err := parseFrontmatterMustRead(note.AbsolutePath)
		if err != nil {
			return changed, err
		}
		didChange := false

		setScalarMirror := func(linkField, mirrorField string) {
			target := wikiTarget(data[linkField])
			if target == "" {
				if _, ok := data[mirrorField]; ok && stringField(data, mirrorField) != "" {
					data[mirrorField] = ""
					didChange = true
				}
				return
			}
			recordID := idToRecordID[target]
			if recordID != "" && stringField(data, mirrorField) != recordID {
				data[mirrorField] = recordID
				didChange = true
			}
		}
		setListMirror := func(linkField, mirrorField string) {
			recordIDs := resolveRecordIDsFromMap(idToRecordID, data[linkField])
			if !stringSlicesEqual(normalizeList(data[mirrorField]), recordIDs) {
				data[mirrorField] = recordIDs
				didChange = true
			}
		}

		switch noteType {
		case "epic":
			setListMirror("docs", "docs_record_ids")
		case "task":
			setScalarMirror("epic", "epic_record_id")
			setListMirror("related", "related_record_ids")
			setListMirror("blocks", "blocks_record_ids")
			setListMirror("blocked_by", "blocked_by_record_ids")
		case "doc":
			setScalarMirror("epic", "epic_record_id")
		}

		if !didChange {
			continue
		}
		content, err := serializeDocument(data, body, frontmatterOrderForType(noteType))
		if err != nil {
			return changed, err
		}
		if err := writeText(note.AbsolutePath, content); err != nil {
			return changed, err
		}
		changed++
	}
	return changed, nil
}

func resolveRecordIDsFromMap(idToRecordID map[string]string, value any) []string {
	var out []string
	for _, link := range normalizeList(value) {
		target := wikiTarget(link)
		out = append(out, idToRecordID[target])
	}
	return out
}

func stringSlicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func publicationManifestReady(doc map[string]any) bool {
	if !boolValue(doc["publish"]) {
		return false
	}
	if strings.TrimSpace(stringValue(doc["publish_path"])) == "" {
		return false
	}
	if strings.TrimSpace(stringValue(doc["publish_description"])) == "" {
		return false
	}
	return true
}

func publicationManifestItem(doc map[string]any) map[string]any {
	return map[string]any{
		"source_kind":           "vault_doc",
		"id":                    firstNonEmpty(stringValue(doc["node"]), stringValue(doc["id"])),
		"title":                 stringValue(doc["title"]),
		"path":                  stringValue(doc["path"]),
		"node":                  stringValue(doc["node"]),
		"epic":                  stringValue(doc["epic"]),
		"audience":              stringValue(doc["audience"]),
		"mode":                  stringValue(doc["mode"]),
		"agent_layer":           stringValue(doc["agent_layer"]),
		"source_of_truth":       normalizeList(doc["source_of_truth"]),
		"stale_when_paths":      normalizeList(doc["stale_when_paths"]),
		"doc_intent":            stringValue(doc["doc_intent"]),
		"owner_epic":            stringValue(doc["owner_epic"]),
		"canon_for":             stringValue(doc["canon_for"]),
		"canonical":             boolValue(doc["canonical"]),
		"canonical_status":      stringValue(doc["canonical_status"]),
		"verified_at":           firstNonEmpty(stringValue(doc["last_verified_at"]), stringValue(doc["verified_at"])),
		"deprecated":            boolValue(doc["deprecated"]),
		"superseded_by":         stringValue(doc["superseded_by"]),
		"redirect_from":         normalizeList(doc["redirect_from"]),
		"status":                firstNonEmpty(stringValue(doc["status"]), stringValue(doc["canonical_status"])),
		"publish":               boolValue(doc["publish"]),
		"publish_lane":          stringValue(doc["publish_lane"]),
		"publish_path":          stringValue(doc["publish_path"]),
		"publish_description":   stringValue(doc["publish_description"]),
		"publish_order":         optionalIntValue(doc["publish_order"]),
		"publish_section_title": stringValue(doc["publish_section_title"]),
		"tags":                  normalizeList(doc["tags"]),
		"updated":               stringValue(doc["updated"]),
	}
}

func buildDocsCatalog(docsMap *DocsMap, docs, tasks []map[string]any) []map[string]any {
	if docsMap == nil {
		return nil
	}
	docsByNode := map[string]map[string]any{}
	docsByMapPath := map[string]map[string]any{}
	for _, doc := range docs {
		if node := stringValue(doc["node"]); node != "" {
			docsByNode[node] = doc
		}
		if path := docsNormalizePath(stringValue(doc["path"])); path != "" {
			docsByMapPath[path] = doc
		}
	}
	var catalog []map[string]any
	for _, node := range docsMap.Nodes {
		path := docsNormalizePath(node.SourcePath())
		doc := docsByNode[node.ID]
		if doc == nil && path != "" {
			doc = docsByMapPath[path]
		}
		freshness := "missing"
		updated := ""
		verifiedAt := ""
		publishPath := node.PublishPath
		linkedTasks, waivers, lastEvent := docsCatalogTaskState(tasks, node.ID)
		if doc != nil {
			updated = stringValue(doc["updated"])
			verifiedAt = firstNonEmpty(stringValue(doc["last_verified_at"]), stringValue(doc["verified_at"]))
			publishPath = firstNonEmpty(stringValue(doc["publish_path"]), publishPath)
			if verifiedAt != "" {
				freshness = "verified"
			} else {
				freshness = "needs_verification"
			}
		}
		if lastEvent != nil {
			switch stringValue(lastEvent["status"]) {
			case "applied", "verified_noop":
				freshness = "verified_by_task"
			case "waived":
				freshness = "waived"
			}
			verifiedAt = firstNonEmpty(verifiedAt, stringValue(lastEvent["date"]))
		}
		staleDueTo := []string{}
		if freshness == "missing" || freshness == "needs_verification" {
			staleDueTo = append(staleDueTo, node.StaleWhen.Paths...)
		}
		catalog = append(catalog, map[string]any{
			"doc_node":            node.ID,
			"title":               node.CatalogTitle(),
			"section":             docsCatalogSection(node),
			"audience":            node.Audience,
			"mode":                node.EffectiveMode(),
			"agent_layer":         node.EffectiveAgentLayer(),
			"domain":              node.Domain,
			"path":                path,
			"publish_path":        publishPath,
			"freshness":           freshness,
			"updated":             updated,
			"verified_at":         verifiedAt,
			"last_verified_event": lastEvent,
			"linked_tasks":        linkedTasks,
			"waivers":             waivers,
			"stale_due_to":        staleDueTo,
			"source_of_truth":     append([]string{}, node.SourceOfTruth...),
			"stale_when":          append([]string{}, node.StaleWhen.Paths...),
			"evals":               append([]string{}, node.Evals...),
		})
	}
	sort.SliceStable(catalog, func(i, j int) bool {
		left, right := catalog[i], catalog[j]
		if docsCatalogSectionRank(stringValue(left["section"])) != docsCatalogSectionRank(stringValue(right["section"])) {
			return docsCatalogSectionRank(stringValue(left["section"])) < docsCatalogSectionRank(stringValue(right["section"]))
		}
		return stringValue(left["doc_node"]) < stringValue(right["doc_node"])
	})
	return catalog
}

func docsCatalogTaskState(tasks []map[string]any, node string) ([]string, []map[string]any, map[string]any) {
	linked := []string{}
	waivers := []map[string]any{}
	var lastEvent map[string]any
	for _, task := range tasks {
		if !containsString(normalizeList(task["doc_nodes"]), node) {
			continue
		}
		taskID := stringValue(task["id"])
		linked = append(linked, taskID)
		for _, raw := range anySlice(task["docs_resolution"]) {
			row, ok := raw.(map[string]any)
			if !ok || stringValue(row["node"]) != node {
				continue
			}
			event := map[string]any{
				"task":   taskID,
				"status": stringValue(row["status"]),
				"actor":  stringValue(row["actor"]),
				"date":   stringValue(row["date"]),
				"reason": stringValue(row["reason"]),
			}
			if stringValue(row["status"]) == "waived" {
				waivers = append(waivers, event)
			}
			if lastEvent == nil || stringValue(event["date"]) >= stringValue(lastEvent["date"]) {
				lastEvent = event
			}
		}
	}
	sort.Strings(linked)
	return linked, waivers, lastEvent
}

func (n DocsMapNode) CatalogTitle() string {
	if title := strings.TrimSpace(n.Title); title != "" {
		return title
	}
	return docsTitleizeSegment(filepath.Base(n.ID))
}

func docsCatalogSection(node DocsMapNode) string {
	path := strings.ToLower(node.SourcePath())
	if node.Audience == "agent" || node.EffectiveAgentLayer() == "standalone" {
		return "For agents"
	}
	if strings.Contains(path, "troubleshooting") {
		return "Troubleshooting"
	}
	if strings.Contains(path, "example") {
		return "Examples"
	}
	if strings.Contains(path, "media") {
		return "Media"
	}
	switch node.EffectiveMode() {
	case "tutorial":
		return "Start here"
	case "how-to":
		return "Guides"
	case "explanation":
		return "Concepts"
	default:
		return "Reference"
	}
}

func docsCatalogSectionRank(section string) int {
	order := []string{"Start here", "Guides", "Reference", "Concepts", "Troubleshooting", "Examples", "For agents", "Media"}
	for i, value := range order {
		if section == value {
			return i
		}
	}
	return len(order)
}

func writeDocsCatalogNote(vaultPath string, catalog []map[string]any, generatedAt string) error {
	if len(catalog) == 0 {
		return nil
	}
	date := generatedAt
	if len(date) >= 10 {
		date = date[:10]
	}
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("title: \"Docs Catalog\"\n")
	b.WriteString("type: \"note\"\n")
	b.WriteString(fmt.Sprintf("created: \"%s\"\n", date))
	b.WriteString(fmt.Sprintf("updated: \"%s\"\n", date))
	b.WriteString("tags: [\"docs-catalog\", \"tusker-generated\"]\n")
	b.WriteString("---\n\n")
	b.WriteString("# Docs Catalog\n\n")
	b.WriteString(fmt.Sprintf("_Auto-generated %s from `_config/docs-map.yaml`. Diátaxis mode is metadata; navigation is grouped by reader intent._\n\n", generatedAt))
	sections := []string{"Start here", "Guides", "Reference", "Concepts", "Troubleshooting", "Examples", "For agents", "Media"}
	for _, section := range sections {
		var rows []map[string]any
		for _, item := range catalog {
			if stringValue(item["section"]) == section {
				rows = append(rows, item)
			}
		}
		if len(rows) == 0 {
			continue
		}
		b.WriteString("## " + section + "\n\n")
		for _, item := range rows {
			path := strings.TrimSuffix(stringValue(item["path"]), ".md")
			link := stringValue(item["title"])
			if path != "" {
				link = fmt.Sprintf("[[%s|%s]]", path, stringValue(item["title"]))
			}
			b.WriteString(fmt.Sprintf("- %s — `%s` · %s · %s · %s\n", link, stringValue(item["doc_node"]), stringValue(item["mode"]), stringValue(item["audience"]), stringValue(item["freshness"])))
		}
		b.WriteByte('\n')
	}
	return writeText(filepath.Join(vaultPath, "Docs.md"), b.String())
}

func optionalIntValue(value any) any {
	switch current := value.(type) {
	case nil:
		return nil
	case int:
		return current
	case int64:
		return int(current)
	case int32:
		return int(current)
	case float64:
		if current == float64(int(current)) {
			return int(current)
		}
	case string:
		trimmed := strings.TrimSpace(current)
		if trimmed == "" {
			return nil
		}
		if parsed, err := strconv.Atoi(trimmed); err == nil {
			return parsed
		}
	}
	return nil
}

func validateCmd(args Args) (int, error) {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return 0, err
	}
	notes, err := listAllNotes(vaultPath)
	if err != nil {
		return 0, err
	}
	docsMap, err := loadDocsMap(vaultPath)
	if err != nil {
		return 0, err
	}
	epicAcronyms := map[string]struct{}{}
	noteIDs := map[string]struct{}{}
	idToRecordID := map[string]string{}
	recordIDs := map[string]struct{}{}
	for _, note := range notes {
		if stringField(note.Data, "type") == "epic" {
			epicAcronyms[stringField(note.Data, "id")] = struct{}{}
		}
		if id := stringField(note.Data, "id"); id != "" {
			noteIDs[id] = struct{}{}
		}
		if id := stringField(note.Data, "id"); id != "" && stringField(note.Data, "record_id") != "" {
			idToRecordID[id] = stringField(note.Data, "record_id")
		}
		if recordID := stringField(note.Data, "record_id"); recordID != "" {
			recordIDs[recordID] = struct{}{}
		}
	}
	var errs, warns []Issue
	errs = append(errs, validateDocsMapConfig(docsMap)...)
	idToPaths := map[string][]string{}
	publishPathToPaths := map[string][]string{}
	for _, note := range notes {
		id := stringField(note.Data, "id")
		if id != "" {
			idToPaths[id] = append(idToPaths[id], note.RelativePath)
		}
		if stringField(note.Data, "type") == "doc" && boolField(note.Data, "publish") {
			publishPath := strings.TrimSpace(stringField(note.Data, "publish_path"))
			if publishPath != "" {
				publishPathToPaths[publishPath] = append(publishPathToPaths[publishPath], note.RelativePath)
			}
		}
		abs, _ := filepath.Abs(note.AbsolutePath)
		root, _ := filepath.Abs(vaultPath)
		if abs != root && !strings.HasPrefix(abs, root+string(filepath.Separator)) {
			errs = append(errs, issue(errorPathEscape, fmt.Sprintf(`file "%s" resolves outside vault root`, note.RelativePath), note.RelativePath, "", map[string]any{"absolute": abs, "root": root}))
		}
	}
	for id, paths := range idToPaths {
		if len(paths) > 1 {
			errs = append(errs, issue(errorIDCollision, fmt.Sprintf(`id "%s" declared in %d files: %s`, id, len(paths), strings.Join(paths, ", ")), paths[0], "rename one file or change one id; ids must be unique vault-wide", map[string]any{"id": id, "paths": paths}))
		}
	}
	for publishPath, paths := range publishPathToPaths {
		if len(paths) > 1 {
			errs = append(errs, issue(errorPublishPathCollision, fmt.Sprintf(`publish_path "%s" declared in %d files: %s`, publishPath, len(paths), strings.Join(paths, ", ")), paths[0], "published doc routes must be unique vault-wide", map[string]any{"publish_path": publishPath, "paths": paths}))
		}
	}
	for _, note := range notes {
		noteErrs, noteWarns := validateNote(note, validationContext{
			RelativePath: note.RelativePath,
			Basename:     filepath.Base(note.AbsolutePath),
			EpicAcronyms: epicAcronyms,
			NoteIDs:      noteIDs,
			IDToRecordID: idToRecordID,
			RecordIDs:    recordIDs,
			DocsMap:      docsMap,
		})
		errs = append(errs, noteErrs...)
		warns = append(warns, noteWarns...)
	}
	docsErrs, docsWarns := validateDocsPublicationState(vaultPath, notes)
	errs = append(errs, docsErrs...)
	warns = append(warns, docsWarns...)
	if args.Bool("json") {
		emitJSON(map[string]any{
			"ok":       len(errs) == 0,
			"counts":   map[string]any{"notes": len(notes), "errors": len(errs), "warnings": len(warns)},
			"errors":   errs,
			"warnings": warns,
		})
		if len(errs) > 0 {
			return 1, nil
		}
		return 0, nil
	}
	if len(warns) > 0 {
		fmt.Println("Warnings:")
		for _, warning := range warns {
			fmt.Printf("- %s\n", formatIssue(warning))
		}
	}
	if len(errs) > 0 {
		fmt.Println("Errors:")
		for _, current := range errs {
			fmt.Printf("- %s\n", formatIssue(current))
		}
		return 1, nil
	}
	if !args.Bool("quiet") {
		fmt.Printf("Validation passed for %d notes.\n", len(notes))
	}
	return 0, nil
}

func listCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	notes, err := listAllNotes(vaultPath)
	if err != nil {
		return err
	}
	noteType := args.String("type")
	if noteType != "" {
		if _, ok := makeSet("epic", "task", "doc", "note")[noteType]; !ok {
			return tuskerError(errorInvalidArg, fmt.Sprintf(`--type must be one of epic, task, doc, note; got "%s"`, noteType), withContext(map[string]any{"arg": "--type", "value": noteType}))
		}
	}
	epic := strings.ToUpper(args.String("epic"))
	status := args.String("status")
	assignee := args.String("assignee")
	var rows []Note
	for _, note := range notes {
		if noteType != "" && stringField(note.Data, "type") != noteType {
			continue
		}
		if epic != "" {
			e := stringField(note.Data, "id")
			if stringField(note.Data, "type") != "epic" {
				e = wikiTarget(note.Data["epic"])
			}
			if e != epic {
				continue
			}
		}
		if status != "" && stringField(note.Data, "status") != status {
			continue
		}
		if assignee != "" && stringField(note.Data, "assignee") != assignee {
			continue
		}
		rows = append(rows, note)
	}
	if args.Bool("json") {
		items := make([]map[string]any, 0, len(rows))
		for _, note := range rows {
			items = append(items, map[string]any{
				"id":            stringField(note.Data, "id"),
				"title":         stringField(note.Data, "title"),
				"type":          stringField(note.Data, "type"),
				"status":        stringField(note.Data, "status"),
				"record_id":     stringField(note.Data, "record_id"),
				"work_revision": intField(note.Data, "work_revision"),
				"epic": func() string {
					if stringField(note.Data, "type") == "epic" {
						return stringField(note.Data, "id")
					}
					return wikiTarget(note.Data["epic"])
				}(),
				"assignee": stringField(note.Data, "assignee"),
				"risk":     stringField(note.Data, "risk"),
				"priority": stringField(note.Data, "priority"),
				"path":     note.RelativePath,
				"updated":  stringField(note.Data, "updated"),
			})
		}
		emitJSON(map[string]any{"ok": true, "count": len(items), "items": items})
		return nil
	}
	for _, note := range rows {
		fmt.Printf("%-14s  %-6s  %-10s  %s\n", stringField(note.Data, "id"), stringField(note.Data, "type"), stringField(note.Data, "status"), stringField(note.Data, "title"))
	}
	if len(rows) == 0 && !args.Bool("quiet") {
		fmt.Println("(no matches)")
	}
	return nil
}

func writeVaultReadme(vaultPath string, epics, tasks, docs []map[string]any, generatedAt string) error {
	readmePath := filepath.Join(vaultPath, "README.md")
	legacyPath := filepath.Join(vaultPath, "EPICS.md")
	overviewBody := readmeDefaultOverview
	if fileExists(readmePath) {
		existing, err := readText(readmePath)
		if err != nil {
			return err
		}
		begin := strings.Index(existing, readmeOverviewBegin)
		end := strings.Index(existing, readmeOverviewEnd)
		if begin == -1 || end == -1 || end <= begin {
			fmt.Fprintf(os.Stderr, "Skipping vault README regen: %s exists without tusker overview markers. Rename it or add <!-- tusker:overview:begin --> / <!-- tusker:overview:end --> to enable regeneration.\n", readmePath)
			return nil
		}
		extracted := strings.TrimSpace(existing[begin+len(readmeOverviewBegin) : end])
		if extracted != "" {
			overviewBody = extracted
		}
	}
	statusOrder := []string{"active", "blocked", "review", "rework", "ready", "backlog", "draft", "done", "cancelled"}
	groups := map[string][]map[string]any{}
	for _, status := range statusOrder {
		groups[status] = []map[string]any{}
	}
	for _, epic := range epics {
		status := stringValue(epic["status"])
		if _, ok := groups[status]; !ok {
			status = "intake"
		}
		groups[status] = append(groups[status], epic)
	}
	childrenByEpic := map[string][]map[string]any{}
	for _, collection := range [][]map[string]any{tasks, docs} {
		for _, item := range collection {
			epic := stringValue(item["epic"])
			if epic == "" {
				continue
			}
			childrenByEpic[epic] = append(childrenByEpic[epic], item)
		}
	}
	today := generatedAt[:10]
	var lines []string
	lines = append(lines,
		"---",
		`title: "Overview"`,
		`type: "note"`,
		fmt.Sprintf(`created: "%s"`, today),
		fmt.Sprintf(`updated: "%s"`, today),
		`tags: ["tusker-generated"]`,
		"---",
		"",
		"# Project overview",
		"",
		readmeOverviewBegin,
		"",
		overviewBody,
		"",
		readmeOverviewEnd,
		"",
		"---",
		"",
		"# Epic roster",
		"",
		fmt.Sprintf("_Auto-generated %s. Run `tusker list --type epic` for a live terminal view. Everything below this heading is regenerated on every `tusker reindex` — edits here get overwritten._", generatedAt),
		"",
		`Agents: read this file before logging new work. Pick the epic whose summary best matches; if nothing fits and the work will outlive one task, propose a new epic with `+"`tusker new epic --acronym <ACR> --title \"<name>\" --summary \"...\"`"+`.`,
		"",
	)
	anyRendered := false
	for _, status := range statusOrder {
		bucket := groups[status]
		if len(bucket) == 0 {
			continue
		}
		anyRendered = true
		lines = append(lines, "## "+capitalize(status), "")
		sort.Slice(bucket, func(i, j int) bool { return stringValue(bucket[i]["id"]) < stringValue(bucket[j]["id"]) })
		for _, epic := range bucket {
			counts := epic["counts"].(map[string]int)
			summary := strings.TrimSpace(stringValue(epic["summary"]))
			if summary == "" {
				summary = "_(no summary — run `tusker new epic` with --summary or edit the epic note)_"
			}
			lines = append(lines,
				fmt.Sprintf("### [[%s]] — %s", stringValue(epic["id"]), stringValue(epic["title"])),
				"",
				"**Summary:** "+summary,
				"",
				fmt.Sprintf("**Counts:** %d task%s, %d bug task%s, %d doc%s (open: %d, done: %d)", counts["tasks"], plural(counts["tasks"]), counts["bug_tasks"], plural(counts["bug_tasks"]), counts["docs"], plural(counts["docs"]), counts["open"], counts["done"]),
				"",
			)
			var openChildren []map[string]any
			for _, child := range childrenByEpic[stringValue(epic["id"])] {
				status := stringValue(child["status"])
				if status == "done" || status == "cancelled" || status == "archived" || status == "superseded" {
					continue
				}
				openChildren = append(openChildren, child)
			}
			if len(openChildren) > 0 {
				lines = append(lines, "**Open work:**")
				shown := openChildren
				if len(shown) > 5 {
					shown = shown[:5]
				}
				for _, child := range shown {
					lines = append(lines, fmt.Sprintf("- [[%s]] — %s (%s)", stringValue(child["id"]), stringValue(child["title"]), stringValue(child["status"])))
				}
				if len(openChildren) > len(shown) {
					lines = append(lines, fmt.Sprintf("- _...and %d more (run `tusker list --epic %s`)_", len(openChildren)-len(shown), stringValue(epic["id"])))
				}
				lines = append(lines, "")
			}
		}
	}
	if !anyRendered {
		lines = append(lines, "_(no epics yet — create one with `tusker new epic --acronym <ACR> --title <title> --summary \"...\"`)_", "")
	}
	if err := writeText(readmePath, strings.Join(lines, "\n")); err != nil {
		return err
	}
	if fileExists(legacyPath) {
		_ = os.Remove(legacyPath)
	}
	return nil
}

func loadDashboardRuntime(vaultPath string) map[string]any {
	section := map[string]any{
		"state_root":   DefaultStateRoot(),
		"project_id":   "",
		"active_runs":  []map[string]any{},
		"tracked_runs": 0,
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return section
	}
	defer store.Close()
	projects, err := store.ListProjects()
	if err != nil {
		return section
	}
	absVault, err := filepath.Abs(vaultPath)
	if err != nil {
		absVault = vaultPath
	}
	projectIDs := map[string]struct{}{}
	for _, project := range projects {
		projectVault, err := filepath.Abs(project.VaultRoot)
		if err != nil {
			projectVault = project.VaultRoot
		}
		if projectVault == absVault {
			projectIDs[project.ProjectID] = struct{}{}
			section["project_id"] = project.ProjectID
		}
	}
	if len(projectIDs) == 0 {
		return section
	}
	runs, err := store.ListRuns()
	if err != nil {
		return section
	}
	activeRuns := []map[string]any{}
	trackedRuns := 0
	for _, run := range runs {
		if _, ok := projectIDs[run.ProjectID]; !ok {
			continue
		}
		trackedRuns++
		switch LeaseState(run.LeaseState) {
		case LeaseStateClaimed, LeaseStateRunning, LeaseStateRetryQueued, LeaseStateInterrupted:
			activeRuns = append(activeRuns, map[string]any{
				"item_id":        run.ItemID,
				"runner":         run.Runner,
				"lease_state":    run.LeaseState,
				"session_ref":    run.SessionRef,
				"started_at":     run.StartedAt,
				"updated_at":     run.UpdatedAt,
				"workspace_path": run.WorkspacePath,
				"raw_log_path":   run.RawLogPath,
			})
		}
	}
	sort.Slice(activeRuns, func(i, j int) bool {
		return stringValue(activeRuns[i]["item_id"]) < stringValue(activeRuns[j]["item_id"])
	})
	section["active_runs"] = activeRuns
	section["tracked_runs"] = trackedRuns
	return section
}

func writeDashboardNote(vaultPath string, runtime map[string]any, docsCatalog []map[string]any, generatedAt string) error {
	dashboardPath := filepath.Join(vaultPath, "Dashboard.md")
	body := ""
	if fileExists(dashboardPath) {
		existing, err := readText(dashboardPath)
		if err != nil {
			return err
		}
		body = existing
	} else {
		body = defaultV5DashboardNote(generatedAt[:10])
	}
	if !strings.Contains(body, "## Docs catalog") {
		body += "\n## Docs catalog\n\n![[Docs]]\n"
	}
	if !strings.Contains(body, docsFreshnessBegin) || !strings.Contains(body, docsFreshnessEnd) {
		body += "\n\n## Docs freshness\n\n" + docsFreshnessBegin + "\n" + docsFreshnessEnd + "\n"
	}
	if !strings.Contains(body, dashboardRunsBegin) || !strings.Contains(body, dashboardRunsEnd) {
		body += "\n\n## Live runs\n\n" + dashboardRunsBegin + "\n" + dashboardRunsEnd + "\n"
	}
	freshnessBlock := renderDocsFreshnessBlock(docsCatalog, generatedAt)
	freshBegin := strings.Index(body, docsFreshnessBegin)
	freshEnd := strings.Index(body, docsFreshnessEnd)
	if freshBegin != -1 && freshEnd != -1 && freshEnd > freshBegin {
		body = body[:freshBegin+len(docsFreshnessBegin)] + "\n\n" + freshnessBlock + "\n\n" + body[freshEnd:]
	}
	block := renderDashboardRunsBlock(runtime, generatedAt)
	begin := strings.Index(body, dashboardRunsBegin)
	end := strings.Index(body, dashboardRunsEnd)
	if begin == -1 || end == -1 || end < begin {
		return writeText(dashboardPath, body)
	}
	replaced := body[:begin+len(dashboardRunsBegin)] + "\n\n" + block + "\n\n" + body[end:]
	return writeText(dashboardPath, replaced)
}

const docsFreshnessBegin = "<!-- tusker:docs-freshness:begin -->"
const docsFreshnessEnd = "<!-- tusker:docs-freshness:end -->"

func renderDocsFreshnessBlock(catalog []map[string]any, generatedAt string) string {
	var stale []map[string]any
	for _, item := range catalog {
		freshness := stringValue(item["freshness"])
		if freshness == "verified" || freshness == "verified_by_task" {
			continue
		}
		stale = append(stale, item)
	}
	if len(stale) == 0 {
		return fmt.Sprintf("_Auto-generated %s. No stale docs right now._", generatedAt)
	}
	lines := []string{
		fmt.Sprintf("_Auto-generated %s. Docs needing verification are shown below._", generatedAt),
		"",
		"| Doc node | Freshness | Stale due to |",
		"|---|---|---|",
	}
	for _, item := range stale {
		staleDueTo := strings.Join(normalizeList(item["stale_due_to"]), ", ")
		lines = append(lines, fmt.Sprintf("| `%s` | `%s` | %s |", stringValue(item["doc_node"]), stringValue(item["freshness"]), fallback(staleDueTo, "—")))
	}
	return strings.Join(lines, "\n")
}

func renderDashboardRunsBlock(runtime map[string]any, generatedAt string) string {
	activeRuns := anySlice(runtime["active_runs"])
	if len(activeRuns) == 0 {
		return fmt.Sprintf("_Auto-generated %s. No live runs right now._", generatedAt)
	}
	lines := []string{
		fmt.Sprintf("_Auto-generated %s. Live daemon activity is shown below._", generatedAt),
		"",
		"| Task | Runner | Lease | Session |",
		"|---|---|---|---|",
	}
	for _, row := range activeRuns {
		run, ok := row.(map[string]any)
		if !ok {
			continue
		}
		itemID := stringValue(run["item_id"])
		runner := stringValue(run["runner"])
		lease := stringValue(run["lease_state"])
		session := stringValue(run["session_ref"])
		if session == "" {
			session = "—"
		} else if len(session) > 12 {
			session = session[:12] + "…"
		}
		lines = append(lines, fmt.Sprintf("| [[%s]] | `%s` | `%s` | `%s` |", itemID, runner, lease, session))
	}
	return strings.Join(lines, "\n")
}

func anySlice(value any) []any {
	switch v := value.(type) {
	case []any:
		return v
	case []map[string]any:
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, item)
		}
		return out
	default:
		return nil
	}
}

func autoReindex(vaultPath string) {
	if err := reindex(Args{"vault": vaultPath, "quiet": "true"}); err != nil && os.Getenv("TUSKER_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "[auto-reindex] skipped: %s\n", err.Error())
	}
}

func buildAgentsSection(config *Config, activity map[string]int) map[string]any {
	out := map[string]any{}
	enabled := map[string]struct{}{}
	if config != nil {
		for _, name := range config.Agents.Enabled {
			enabled[name] = struct{}{}
			capValue, ok := config.Agents.Concurrency[name]
			if ok {
				out[name] = map[string]any{"enabled": true, "concurrency_cap": capValue, "active": activity[name]}
			} else {
				out[name] = map[string]any{"enabled": true, "concurrency_cap": nil, "active": activity[name]}
			}
		}
	}
	for name, count := range activity {
		if _, ok := out[name]; ok {
			continue
		}
		if _, ok := enabled[name]; ok {
			continue
		}
		out[name] = map[string]any{"enabled": false, "concurrency_cap": nil, "active": count, "orphaned": true}
	}
	return out
}
