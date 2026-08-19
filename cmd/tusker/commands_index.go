package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"tusker/internal/docgraph"
)

func reindex(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	v7Vault := isV7VaultLayout(vaultPath)
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
	openTasksByEpic := map[string][]map[string]any{}
	for _, task := range tasks {
		epic := stringValue(task["epic"])
		if epic == "" || !isOpenWorkStatus(stringValue(task["status"])) {
			continue
		}
		openTasksByEpic[epic] = append(openTasksByEpic[epic], task)
	}
	for _, epicNote := range epicMap {
		acronym := stringField(epicNote.Data, "id")
		openTasks := openTasksByEpic[acronym]
		sortTaskMapsForProgressiveList(openTasks)
		list := fmt.Sprintf("_No open tasks. Use `tusker list --epic %s --type task --status done` for closed history._", acronym)
		if len(openTasks) > 0 {
			lines := []string{
				fmt.Sprintf("_Open tasks only. Closed/cancelled work is intentionally omitted; use `tusker list --epic %s --type task --status done` for closed history._", acronym),
				"",
			}
			for _, task := range openTasks {
				meta := compactTaskMeta(task)
				lines = append(lines, fmt.Sprintf("- [[%s]] — %s%s", stringValue(task["id"]), stringValue(task["title"]), meta))
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
	var readyQueue, blockedQueue, verificationReviewQueue, reviewQueue, reworkQueue []map[string]any
	for _, item := range work {
		switch stringValue(item["status"]) {
		case "ready":
			readyQueue = append(readyQueue, item)
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
			"openWork":          len(readyQueue) + len(blockedQueue) + len(verificationReviewQueue) + len(reviewQueue) + len(reworkQueue),
			"inReview":          countStatus(work, "review"),
			"verificationQueue": len(verificationQueue),
			"publicationQueue":  countNotPublished(publicationQueue),
			"ready":             len(readyQueue),
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
			"ready":               readyQueue,
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
	generatedDir := filepath.Join(vaultPath, "_generated", "indexes")
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
	if err := writeV6GeneratedIndexes(vaultPath, true); err != nil {
		return err
	}
	if err := writeVaultReadme(vaultPath, epics, tasks, docs, generatedAt); err != nil {
		return err
	}
	if !v7Vault {
		if err := writeDashboardNote(vaultPath, runtimeSection, docsCatalog, generatedAt); err != nil {
			return err
		}
	}
	if err := writeDocsCatalogNote(vaultPath, docsCatalog, generatedAt); err != nil {
		return err
	}
	if v7Vault {
		idx, err := loadV7Index(vaultPath)
		if err != nil {
			return err
		}
		if err := buildV7Dashboards(vaultPath, idx); err != nil {
			return err
		}
		leases, err := loadV7Leases(vaultPath)
		if err != nil {
			return err
		}
		summary = v7SummaryIndexData(idx, v7DashboardIndexData(idx, leases))
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
		fmt.Printf("Tracker queues - ready: %d, blocked: %d, verification: %d, review: %d, rework: %d\n", len(readyQueue), len(blockedQueue), len(verificationReviewQueue), len(reviewQueue), len(reworkQueue))
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
	if args.Bool("branch-policy-only") {
		errs, warns := validateV7BranchPolicy(vaultPath, args)
		if args.Bool("json") {
			emitJSON(map[string]any{
				"ok":       len(errs) == 0,
				"counts":   map[string]any{"errors": len(errs), "warnings": len(warns)},
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
			fmt.Println("Branch policy validation passed.")
		}
		return 0, nil
	}
	notes, err := listAllNotes(vaultPath)
	if err != nil {
		return 0, err
	}
	docGraphIssues, err := docgraph.ValidateRepository(v7RepoRoot(vaultPath))
	if err != nil {
		return 0, err
	}
	docsMapIssues, err := docgraph.CheckDocsMapFresh(v7RepoRoot(vaultPath))
	if err != nil {
		return 0, err
	}
	docGraphIssues = append(docGraphIssues, docsMapIssues...)
	docsMap, err := loadDocsMap(vaultPath)
	if err != nil {
		return 0, err
	}
	epicAcronyms := map[string]struct{}{}
	noteIDs := map[string]struct{}{}
	idToRecordID := map[string]string{}
	recordIDs := map[string]struct{}{}
	v6Domains := map[string]bool{}
	v6KnowledgeNodes := map[string]bool{}
	v6LinkTargets := map[string]bool{}
	v6Freshness := map[string]v6FreshnessRecord{}
	var v6Index v6KnowledgeIndex
	hasV6Index := false
	if hasV6Vault(vaultPath) {
		v6Index, err = v6IndexVault(vaultPath)
		if err != nil {
			return 0, err
		}
		hasV6Index = true
		for _, domain := range v6Index.Domains {
			addV6ValidationLinkTarget(v6LinkTargets, domain.ID)
			addV6ValidationLinkTarget(v6LinkTargets, domain.ID+"/INDEX")
			addV6ValidationLinkTarget(v6LinkTargets, domain.ID+"/CANON")
		}
		for _, node := range v6Index.KnowledgeNodes {
			v6KnowledgeNodes[node.Node] = true
			addV6ValidationLinkTarget(v6LinkTargets, node.Node)
			addV6ValidationLinkTarget(v6LinkTargets, node.Node+".md")
			for _, alias := range node.Aliases {
				addV6ValidationLinkTarget(v6LinkTargets, alias)
			}
		}
		for _, freshness := range v6Index.Freshness {
			v6Freshness[freshness.Node] = freshness
		}
	}
	for _, note := range notes {
		if stringField(note.Data, "type") == "epic" {
			epicAcronyms[stringField(note.Data, "id")] = struct{}{}
		}
		if stringField(note.Data, "schema") == "tusker.epic/v6" {
			epicAcronyms[stringField(note.Data, "id")] = struct{}{}
		}
		if id := stringField(note.Data, "id"); id != "" {
			noteIDs[id] = struct{}{}
			addV6ValidationLinkTarget(v6LinkTargets, id)
		}
		if id := stringField(note.Data, "id"); id != "" && stringField(note.Data, "record_id") != "" {
			idToRecordID[id] = stringField(note.Data, "record_id")
		}
		if recordID := stringField(note.Data, "record_id"); recordID != "" {
			recordIDs[recordID] = struct{}{}
		}
		switch stringField(note.Data, "schema") {
		case "tusker.domain/v6":
			v6Domains[stringField(note.Data, "id")] = true
			addV6ValidationLinkTarget(v6LinkTargets, stringField(note.Data, "id"))
		case "tusker.knowledge/v6":
			v6KnowledgeNodes[stringField(note.Data, "node")] = true
			addV6ValidationLinkTarget(v6LinkTargets, stringField(note.Data, "node"))
		}
	}
	var errs, warns []Issue
	for _, current := range docGraphIssues {
		errs = append(errs, issue(current.Code, current.Message, current.Path, "", nil))
	}
	if docOpeningIssues, err := validateCanonicalDocOpenings(v7RepoRoot(vaultPath)); err != nil {
		return 0, err
	} else {
		warns = append(warns, docOpeningIssues...)
	}
	if specUpdateIssues, err := validateLockedSpecUpdates(v7RepoRoot(vaultPath), vaultPath, notes); err != nil {
		return 0, err
	} else {
		classifyLockedSpecUpdateIssues(specUpdateIssues, &errs, &warns)
	}
	errs = append(errs, validateDocsMapConfig(docsMap)...)
	if hasV6Index {
		v6Errs, v6Warns := validateV6Vault(vaultPath, v6Index)
		errs = append(errs, v6Errs...)
		warns = append(warns, v6Warns...)
	}
	v7SkillErrs, v7SkillWarns := validateV7SkillKnowledge(vaultPath)
	errs = append(errs, v7SkillErrs...)
	warns = append(warns, v7SkillWarns...)
	warns = append(warns, validateV7FeedbackNotes(vaultPath)...)
	warns = append(warns, validateV7FeedbackSignals(vaultPath)...)
	errs = append(errs, validateCollisionProneNamespaces(vaultPath)...)
	idToPaths := map[string][]string{}
	idCollisionLabels := map[string]string{}
	publishPathToPaths := map[string][]string{}
	for _, note := range notes {
		id := stringField(note.Data, "id")
		if id != "" {
			collisionKey := validationIDCollisionKey(note)
			idToPaths[collisionKey] = append(idToPaths[collisionKey], note.RelativePath)
			idCollisionLabels[collisionKey] = id
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
			label := firstNonEmpty(idCollisionLabels[id], id)
			hint := "rename one file or change one id; ids must be unique within their active schema namespace"
			if v7MixedLayoutTaskIDCollision(paths) {
				hint = fmt.Sprintf("mixed V5/V7 task collision: keep exactly one %s; move or rename the legacy tusker/epics/** task, or rename .tusker/work/tasks/%s.md and update links before rerunning `tusker validate`", label, label)
			}
			errs = append(errs, issue(errorIDCollision, fmt.Sprintf(`id "%s" declared in %d files: %s`, label, len(paths), strings.Join(paths, ", ")), paths[0], hint, map[string]any{"id": label, "paths": paths}))
		}
	}
	for publishPath, paths := range publishPathToPaths {
		if len(paths) > 1 {
			errs = append(errs, issue(errorPublishPathCollision, fmt.Sprintf(`publish_path "%s" declared in %d files: %s`, publishPath, len(paths), strings.Join(paths, ", ")), paths[0], "published doc routes must be unique vault-wide", map[string]any{"publish_path": publishPath, "paths": paths}))
		}
	}
	for _, note := range notes {
		noteErrs, noteWarns := validateNote(note, validationContext{
			RelativePath:     note.RelativePath,
			Basename:         filepath.Base(note.AbsolutePath),
			VaultPath:        vaultPath,
			EpicAcronyms:     epicAcronyms,
			NoteIDs:          noteIDs,
			IDToRecordID:     idToRecordID,
			RecordIDs:        recordIDs,
			DocsMap:          docsMap,
			V6Domains:        v6Domains,
			V6KnowledgeNodes: v6KnowledgeNodes,
			V6LinkTargets:    v6LinkTargets,
			V6Freshness:      v6Freshness,
		})
		errs = append(errs, noteErrs...)
		warns = append(warns, noteWarns...)
	}
	warns = append(warns, validateV7SpecTraceability(vaultPath, notes)...)
	eventErrs, eventWarns, eventCount := validateV7Events(vaultPath)
	errs = append(errs, eventErrs...)
	warns = append(warns, eventWarns...)
	generatedErrs, generatedWarns := validateV7GeneratedDashboards(vaultPath)
	errs = append(errs, generatedErrs...)
	warns = append(warns, generatedWarns...)
	attachmentErrs, attachmentWarns := validateV7AttachmentsPolicy(vaultPath)
	errs = append(errs, attachmentErrs...)
	warns = append(warns, attachmentWarns...)
	if args.Bool("branch-policy") {
		branchErrs, branchWarns := validateV7BranchPolicy(vaultPath, args)
		errs = append(errs, branchErrs...)
		warns = append(warns, branchWarns...)
	}
	if args.Bool("dispatchable") {
		errs = append(errs, validateV7DispatchableTasks(vaultPath)...)
	}
	docsErrs, docsWarns := validateDocsPublicationState(vaultPath, notes)
	errs = append(errs, docsErrs...)
	warns = append(warns, docsWarns...)
	specErrs, specWarns := validateSpecCapsules(vaultPath)
	errs = append(errs, specErrs...)
	warns = append(warns, specWarns...)
	if hasV7ProjectSkill(vaultPath) && hasV7KnowledgeDomains(vaultPath) {
		errs, warns = fenceLegacyV6DocsImpactForV7(errs, warns)
	}
	if args.Bool("json") {
		emitJSON(map[string]any{
			"ok":       len(errs) == 0,
			"counts":   map[string]any{"notes": len(notes), "events": eventCount, "errors": len(errs), "warnings": len(warns)},
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
		fmt.Printf("Validation passed for %d notes and %d events.\n", len(notes), eventCount)
	}
	return 0, nil
}

func classifyLockedSpecUpdateIssues(issues []Issue, errs, warns *[]Issue) {
	for _, current := range issues {
		if current.Code == "SPEC_UPDATES_PENDING" {
			// A linked doc-update task is an explicit plan, so keep the
			// unfinished work visible without failing the whole validation.
			*warns = append(*warns, current)
			continue
		}
		// A missing target or an unlanded target without a linked task
		// violates the locked contract; validation must fail closed.
		*errs = append(*errs, current)
	}
}

func validationIDCollisionKey(note Note) string {
	id := stringField(note.Data, "id")
	switch stringField(note.Data, "schema") {
	case "tusker.domain/v6":
		return "legacy-domain:" + id
	case "tusker.domain/v7":
		return "v7-domain:" + id
	default:
		return id
	}
}

func v7MixedLayoutTaskIDCollision(paths []string) bool {
	hasV7Task := false
	hasLegacyTask := false
	for _, path := range paths {
		normalized := filepath.ToSlash(path)
		if strings.Contains(normalized, "work/tasks/") {
			hasV7Task = true
		}
		if strings.Contains(normalized, "epics/") && !strings.Contains(normalized, "work/epics/") {
			hasLegacyTask = true
		}
	}
	return hasV7Task && hasLegacyTask
}

func fenceLegacyV6DocsImpactForV7(errs, warns []Issue) ([]Issue, []Issue) {
	var remaining []Issue
	for _, current := range errs {
		if current.Code == errorDocsImpactUnresolved && strings.Contains(strings.ToLower(current.Message), "v6 knowledge_nodes") {
			current.Code = "LEGACY_V6_DOCS_IMPACT_FENCED"
			current.Hint = "legacy V6 knowledge freshness is fenced from V7 release validation; V7 project truth is " + defaultRepoVaultDir + "/SKILL.md plus " + defaultRepoVaultDir + "/knowledge/domains/**"
			warns = append(warns, current)
			continue
		}
		remaining = append(remaining, current)
	}
	return remaining, warns
}

func validateSpecCapsules(vaultPath string) ([]Issue, []Issue) {
	repoRoot := filepath.Dir(vaultPath)
	specsDir := filepath.Join(repoRoot, "docs", "specs")
	entries, err := os.ReadDir(specsDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return []Issue{issue(errorInvalidField, "could not read specs for capsule validation: "+err.Error(), filepath.ToSlash(filepath.Join("docs", "specs")), "", nil)}, nil
	}
	var errors, warnings []Issue
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(specsDir, entry.Name())
		rel, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)
		if !capsuleRequiredForSpecPath(rel) {
			continue
		}
		raw, err := readText(path)
		if err != nil {
			errors = append(errors, issue(errorInvalidField, "could not read spec for capsule validation: "+err.Error(), rel, "", nil))
			continue
		}
		data, body, err := parseFrontmatter(raw)
		if err != nil {
			errors = append(errors, issue(errorInvalidField, "could not parse spec frontmatter: "+err.Error(), rel, "", nil))
			continue
		}
		validateCapsule(Note{AbsolutePath: path, RelativePath: rel, Data: data, Body: body}, vaultPath, rel, true, &errors, &warnings)
	}
	return errors, warnings
}

type listRecord struct {
	Note        Note
	VaultPath   string
	Project     string
	ActiveLease *v7LeaseRecord
}

func listCmd(args Args) error {
	if args.String("project") != "" {
		args["all-projects"] = "true"
	}
	records, err := collectListRecords(args)
	if err != nil {
		return err
	}
	noteType := strings.TrimSpace(args.String("type"))
	readyOnly := args.Bool("ready")
	runningOnly := args.Bool("running")
	reviewOnly := args.Bool("review")
	mineOnly := args.Bool("mine")
	epic := strings.ToUpper(args.String("epic"))
	positionalEpic := strings.ToUpper(strings.TrimSpace(args.String("_pos0")))
	if positionalEpic != "" {
		if strings.Contains(args.String("_pos"), "\n") {
			return tuskerError(errorInvalidArg, "tusker list accepts at most one positional epic")
		}
		if epic != "" && epic != positionalEpic {
			return tuskerError(errorInvalidArg, fmt.Sprintf("positional epic %s conflicts with --epic %s", positionalEpic, epic))
		}
		epic = positionalEpic
	}
	status := args.String("status")
	if reviewOnly {
		if status != "" && status != "review" {
			return tuskerError(errorInvalidArg, "--review cannot be combined with --status "+status)
		}
		status = "review"
	}
	waveID := strings.ToUpper(strings.TrimSpace(args.String("wave")))
	if waveID != "" && !v7WaveIDPattern.MatchString(waveID) {
		return tuskerError(errorInvalidArg, "invalid V7 wave id: "+waveID)
	}
	assignee := args.String("assignee")
	openOnly := args.Bool("open")
	closedOnly := args.Bool("closed")
	runnableOnly := args.Bool("runnable") || readyOnly
	format := strings.ToLower(strings.TrimSpace(args.String("format")))
	if format == "" {
		format = "table"
	}
	if format != "table" && format != "ids" {
		return tuskerError(errorInvalidArg, `--format must be "table" or "ids"`, withContext(map[string]any{"arg": "--format", "value": format}))
	}
	if positionalEpic != "" {
		if noteType == "" {
			noteType = "task"
		}
		if noteType == "task" && !openOnly && !closedOnly && status == "" {
			openOnly = true
		}
	}
	if noteType == "" {
		if readyOnly || runningOnly || reviewOnly || mineOnly || runnableOnly || openOnly || closedOnly || status != "" || assignee != "" || waveID != "" {
			noteType = "task"
		} else if !args.Bool("json") && format == "table" {
			noteType = "epic"
		}
	}
	if noteType != "" {
		if _, ok := makeSet("epic", "task", "wave", "doc", "note")[noteType]; !ok {
			return tuskerError(errorInvalidArg, fmt.Sprintf(`--type must be one of epic, task, wave, doc, note; got "%s"`, noteType), withContext(map[string]any{"arg": "--type", "value": noteType}))
		}
	}
	limit := atoiSafe(args.String("limit"))
	if limit < 0 {
		limit = 0
	}
	if openOnly && closedOnly {
		return tuskerError(errorInvalidArg, "--open and --closed cannot be combined")
	}
	if args.String("status") != "" && (openOnly || closedOnly) {
		return tuskerError(errorInvalidArg, "--status cannot be combined with --open or --closed")
	}
	taskCounts := taskCountsByScopedRecords(records)
	waveMembers := listWaveMemberKeys(records, waveID)
	var rows []listRecord
	for _, record := range records {
		note := record.Note
		currentType := noteListKind(note.Data)
		if noteType != "" && currentType != noteType {
			continue
		}
		if epic != "" {
			e := stringField(note.Data, "id")
			if currentType != "epic" {
				e = wikiTarget(note.Data["epic"])
			}
			if e != epic {
				continue
			}
			if noteType == "" && (openOnly || closedOnly) && currentType == "epic" {
				continue
			}
		}
		if status != "" && stringField(note.Data, "status") != status {
			continue
		}
		if waveID != "" {
			if currentType != "task" {
				continue
			}
			taskID := stringField(note.Data, "id")
			if stringField(note.Data, "wave") != waveID && !waveMembers[scopedTaskKey(record.Project, taskID)] {
				continue
			}
		}
		if openOnly && !isOpenWorkStatus(stringField(note.Data, "status")) {
			continue
		}
		if closedOnly && isOpenWorkStatus(stringField(note.Data, "status")) {
			continue
		}
		if runnableOnly && !isV7RunnableAgentTask(note) {
			continue
		}
		if runningOnly && record.ActiveLease == nil {
			continue
		}
		if mineOnly && !listRecordMatchesMine(record) {
			continue
		}
		if assignee != "" && stringField(note.Data, "assignee") != assignee {
			continue
		}
		rows = append(rows, record)
	}
	sortListRecords(rows)
	totalRows := len(rows)
	truncated := 0
	if limit > 0 && len(rows) > limit {
		truncated = len(rows) - limit
		rows = rows[:limit]
	}
	if args.Bool("json") {
		items := make([]map[string]any, 0, len(rows))
		for _, row := range rows {
			note := row.Note
			currentType := noteListKind(note.Data)
			item := map[string]any{
				"id":            stringField(note.Data, "id"),
				"title":         stringField(note.Data, "title"),
				"type":          currentType,
				"status":        stringField(note.Data, "status"),
				"readiness":     stringField(note.Data, "readiness"),
				"next_owner":    stringField(note.Data, "next_owner"),
				"record_id":     stringField(note.Data, "record_id"),
				"wave":          stringField(note.Data, "wave"),
				"work_revision": intField(note.Data, "work_revision"),
				"epic": func() string {
					if currentType == "epic" {
						return stringField(note.Data, "id")
					}
					return wikiTarget(note.Data["epic"])
				}(),
				"assignee": stringField(note.Data, "assignee"),
				"risk":     stringField(note.Data, "risk"),
				"priority": stringField(note.Data, "priority"),
				"path":     note.RelativePath,
				"updated":  stringField(note.Data, "updated"),
				"project":  row.Project,
				"capsule":  capsulePayload(note),
			}
			if row.ActiveLease != nil {
				item["running"] = true
				item["lease_owner"] = row.ActiveLease.Owner
				item["lease_id"] = row.ActiveLease.ID
			}
			if currentType == "epic" {
				id := stringField(note.Data, "id")
				item["summary"] = listEpicSummary(note)
				item["counts"] = epicTaskCount(taskCounts, scopedEpicKey(row.Project, id))
			}
			if capsule := v7CapsuleMap(note); len(capsule) > 0 {
				item["capsule"] = capsule
			}
			items = append(items, item)
		}
		emitJSON(map[string]any{"ok": true, "count": len(items), "total": totalRows, "truncated": truncated, "items": items})
		return nil
	}
	if format == "ids" {
		for _, row := range rows {
			fmt.Println(stringField(row.Note.Data, "id"))
		}
		return nil
	}
	if len(rows) > 0 {
		fmt.Print(renderListTable(rows, taskCounts, args))
	}
	if len(rows) == 0 && !args.Bool("quiet") {
		fmt.Println("(no matches)")
	}
	if truncated > 0 && !args.Bool("quiet") {
		fmt.Printf("(...and %d more; use a narrower filter or a higher --limit)\n", truncated)
	}
	return nil
}

type listTableColumn struct {
	Header       string
	Min          int
	Max          int
	Shrink       bool
	DropPriority int
}

func renderListTable(rows []listRecord, taskCounts map[string]map[string]int, args Args) string {
	if len(rows) == 0 {
		return ""
	}
	if allListRowsAreKind(rows, "epic") {
		return renderEpicListTable(rows, taskCounts, args)
	}
	return renderWorkListTable(rows, args)
}

func renderEpicListTable(rows []listRecord, taskCounts map[string]map[string]int, args Args) string {
	showProject := args.Bool("all-projects")
	columns := []listTableColumn{}
	if showProject {
		columns = append(columns, listTableColumn{Header: "Project", Min: 7, Max: 14, Shrink: true, DropPriority: 60})
	}
	columns = append(columns,
		listTableColumn{Header: "ID", Min: 4, Max: 8},
		listTableColumn{Header: "Status", Min: 6, Max: 10},
		listTableColumn{Header: "Open", Min: 4, Max: 5},
		listTableColumn{Header: "Done", Min: 4, Max: 5, DropPriority: 70},
		listTableColumn{Header: "Title", Min: 16, Max: 34, Shrink: true},
		listTableColumn{Header: "Summary", Min: 20, Max: 64, Shrink: true, DropPriority: 100},
	)
	tableRows := make([][]string, 0, len(rows))
	for _, row := range rows {
		note := row.Note
		id := stringField(note.Data, "id")
		counts := epicTaskCount(taskCounts, scopedEpicKey(row.Project, id))
		cells := []string{}
		if showProject {
			cells = append(cells, row.Project)
		}
		cells = append(cells,
			id,
			fallback(stringField(note.Data, "status"), "-"),
			strconv.Itoa(counts["open"]),
			strconv.Itoa(counts["done"]),
			stringField(note.Data, "title"),
			listEpicSummary(note),
		)
		tableRows = append(tableRows, cells)
	}
	return renderASCIIListTable(columns, tableRows, terminalOutputWidth(args))
}

func listEpicSummary(note Note) string {
	if summary := strings.TrimSpace(stringField(note.Data, "summary")); summary != "" {
		return summary
	}
	return firstListParagraph(sectionContent(note.Body, "## Thesis"))
}

func firstListParagraph(value string) string {
	var lines []string
	for _, line := range strings.Split(value, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			if len(lines) > 0 {
				break
			}
			continue
		}
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, "<!--") {
			continue
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, " ")
}

func renderWorkListTable(rows []listRecord, args Args) string {
	showProject := args.Bool("all-projects")
	columns := []listTableColumn{}
	if showProject {
		columns = append(columns, listTableColumn{Header: "Project", Min: 7, Max: 14, Shrink: true, DropPriority: 60})
	}
	columns = append(columns,
		listTableColumn{Header: "ID", Min: 10, Max: 14, Shrink: true},
		listTableColumn{Header: "Type", Min: 4, Max: 6, DropPriority: 90},
		listTableColumn{Header: "Status", Min: 6, Max: 10},
		listTableColumn{Header: "Ready", Min: 5, Max: 16, Shrink: true, DropPriority: 70},
		listTableColumn{Header: "Owner", Min: 8, Max: 18, Shrink: true, DropPriority: 80},
		listTableColumn{Header: "Run", Min: 3, Max: 16, Shrink: true, DropPriority: 100},
		listTableColumn{Header: "Title", Min: 16, Max: 64, Shrink: true},
	)
	tableRows := make([][]string, 0, len(rows))
	for _, row := range rows {
		note := row.Note
		cells := []string{}
		if showProject {
			cells = append(cells, row.Project)
		}
		cells = append(cells,
			stringField(note.Data, "id"),
			noteListKind(note.Data),
			fallback(stringField(note.Data, "status"), "-"),
			listReadinessLabel(note.Data),
			listOwnerLabel(note.Data),
			listRunningLabel(row),
			stringField(note.Data, "title"),
		)
		tableRows = append(tableRows, cells)
	}
	return renderASCIIListTable(columns, tableRows, terminalOutputWidth(args))
}

func allListRowsAreKind(rows []listRecord, kind string) bool {
	for _, row := range rows {
		if noteListKind(row.Note.Data) != kind {
			return false
		}
	}
	return true
}

func listReadinessLabel(data map[string]any) string {
	switch value := strings.TrimSpace(stringField(data, "readiness")); value {
	case "":
		return "-"
	case "waiting_on_review":
		return "review"
	case "waiting_on_human":
		return "human"
	case "waiting_on_dependency":
		return "blocked"
	default:
		return value
	}
}

func listOwnerLabel(data map[string]any) string {
	return fallback(firstNonEmpty(stringField(data, "next_owner"), stringField(data, "assignee")), "-")
}

func listRunningLabel(row listRecord) string {
	if row.ActiveLease == nil {
		return "-"
	}
	return fallback(row.ActiveLease.Owner, "active")
}

func renderASCIIListTable(columns []listTableColumn, rows [][]string, maxWidth int) string {
	maxWidth = clampTerminalWidth(maxWidth)
	cleanRows := make([][]string, len(rows))
	for rowIndex, row := range rows {
		cleanRows[rowIndex] = make([]string, len(row))
		for cellIndex, cell := range row {
			cleanRows[rowIndex][cellIndex] = compactListCell(cell)
		}
	}
	columns, cleanRows = fitListTableColumns(columns, cleanRows, maxWidth)
	widths := listTableWidths(columns, cleanRows, maxWidth)
	var out []string
	out = append(out, renderListTableLine(listTableHeaders(columns), widths))
	out = append(out, renderListTableDivider(widths))
	for _, row := range cleanRows {
		out = append(out, renderListTableLine(row, widths))
	}
	return strings.Join(out, "\n") + "\n"
}

func fitListTableColumns(columns []listTableColumn, rows [][]string, maxWidth int) ([]listTableColumn, [][]string) {
	for listTableMinimumTotalWidth(columns) > maxWidth {
		dropIndex := -1
		for i, column := range columns {
			if column.DropPriority <= 0 {
				continue
			}
			if dropIndex < 0 || column.DropPriority > columns[dropIndex].DropPriority {
				dropIndex = i
			}
		}
		if dropIndex < 0 {
			break
		}
		columns = append(append([]listTableColumn{}, columns[:dropIndex]...), columns[dropIndex+1:]...)
		for i, row := range rows {
			if dropIndex >= len(row) {
				continue
			}
			rows[i] = append(append([]string{}, row[:dropIndex]...), row[dropIndex+1:]...)
		}
	}
	return columns, rows
}

func listTableMinimumTotalWidth(columns []listTableColumn) int {
	widths := make([]int, 0, len(columns))
	for _, column := range columns {
		widths = append(widths, clampInt(listCellWidth(column.Header), column.Min, column.Max))
	}
	return listTableTotalWidth(widths)
}

func listTableHeaders(columns []listTableColumn) []string {
	headers := make([]string, 0, len(columns))
	for _, column := range columns {
		headers = append(headers, column.Header)
	}
	return headers
}

func listTableWidths(columns []listTableColumn, rows [][]string, maxWidth int) []int {
	widths := make([]int, len(columns))
	for i, column := range columns {
		widths[i] = clampInt(listCellWidth(column.Header), column.Min, column.Max)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i >= len(widths) {
				continue
			}
			widths[i] = clampInt(maxInt(widths[i], listCellWidth(cell)), columns[i].Min, columns[i].Max)
		}
	}
	total := listTableTotalWidth(widths)
	for total > maxWidth {
		changed := false
		for i := len(widths) - 1; i >= 0 && total > maxWidth; i-- {
			if !columns[i].Shrink || widths[i] <= columns[i].Min {
				continue
			}
			delta := minInt(widths[i]-columns[i].Min, total-maxWidth)
			widths[i] -= delta
			total -= delta
			changed = true
		}
		if changed {
			continue
		}
		for i := len(widths) - 1; i >= 0 && total > maxWidth; i-- {
			if widths[i] <= columns[i].Min {
				continue
			}
			delta := minInt(widths[i]-columns[i].Min, total-maxWidth)
			widths[i] -= delta
			total -= delta
			changed = true
		}
		if !changed {
			break
		}
	}
	return widths
}

func listTableTotalWidth(widths []int) int {
	total := 0
	for _, width := range widths {
		total += width
	}
	if len(widths) > 1 {
		total += (len(widths) - 1) * 2
	}
	return total
}

func renderListTableDivider(widths []int) string {
	cells := make([]string, 0, len(widths))
	for _, width := range widths {
		cells = append(cells, strings.Repeat("-", width))
	}
	return strings.TrimRight(strings.Join(cells, "  "), " ")
}

func renderListTableLine(cells []string, widths []int) string {
	out := make([]string, 0, len(widths))
	for i, width := range widths {
		cell := ""
		if i < len(cells) {
			cell = cells[i]
		}
		out = append(out, padListCell(truncateListCell(cell, width), width))
	}
	return strings.TrimRight(strings.Join(out, "  "), " ")
}

func compactListCell(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func truncateListCell(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if listCellWidth(value) <= width {
		return value
	}
	marker := "..."
	markerWidth := listCellWidth(marker)
	if width <= markerWidth {
		return strings.Repeat(".", width)
	}
	return truncateListCellToWidth(value, width-markerWidth) + marker
}

func truncateListCellToWidth(value string, width int) string {
	if width <= 0 {
		return ""
	}
	var out strings.Builder
	used := 0
	for _, r := range value {
		part := string(r)
		partWidth := listCellWidth(part)
		if used+partWidth > width {
			break
		}
		out.WriteRune(r)
		used += partWidth
	}
	return out.String()
}

func padListCell(value string, width int) string {
	padding := width - listCellWidth(value)
	if padding <= 0 {
		return value
	}
	return value + strings.Repeat(" ", padding)
}

func listCellWidth(value string) int {
	return displayCellWidth(value)
}

func clampInt(value, min, max int) int {
	if max < min {
		max = min
	}
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func collectListRecords(args Args) ([]listRecord, error) {
	if args.Bool("all-projects") {
		projects, err := registeredProjectLoads(args.String("project"), registeredProjectLoadOptions{Notes: true})
		if err != nil {
			return nil, err
		}
		var out []listRecord
		for _, project := range projects {
			activeLeases := activeV7LeasesByTask(project.Project.VaultRoot)
			label := registeredProjectLabel(project.Project)
			for _, note := range project.Notes {
				record := listRecord{Note: note, VaultPath: project.Project.VaultRoot, Project: label}
				if lease, ok := activeLeases[stringField(note.Data, "id")]; ok {
					record.ActiveLease = &lease
				}
				out = append(out, record)
			}
		}
		return out, nil
	}
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return nil, err
	}
	notes, err := listAllNotes(vaultPath)
	if err != nil {
		return nil, err
	}
	activeLeases := activeV7LeasesByTask(vaultPath)
	project := filepath.Base(filepath.Dir(vaultPath))
	out := make([]listRecord, 0, len(notes))
	for _, note := range notes {
		record := listRecord{Note: note, VaultPath: vaultPath, Project: project}
		if lease, ok := activeLeases[stringField(note.Data, "id")]; ok {
			record.ActiveLease = &lease
		}
		out = append(out, record)
	}
	return out, nil
}

func activeV7LeasesByTask(vaultPath string) map[string]v7LeaseRecord {
	out := map[string]v7LeaseRecord{}
	leases, err := loadV7Leases(vaultPath)
	if err != nil {
		return out
	}
	now := time.Now().UTC()
	for _, lease := range leases {
		if lease.Status != "active" || v7LeaseExpired(lease, now) {
			continue
		}
		out[lease.Task] = lease
	}
	return out
}

func listRecordMatchesMine(record listRecord) bool {
	actor := defaultActorName()
	agentOwner := "agent:" + actor
	data := record.Note.Data
	for _, value := range []string{
		stringField(data, "next_owner"),
		stringField(data, "assignee"),
	} {
		if value == actor || value == agentOwner {
			return true
		}
	}
	if record.ActiveLease != nil && (record.ActiveLease.Owner == actor || record.ActiveLease.Owner == agentOwner) {
		return true
	}
	return false
}

func sortListRecords(rows []listRecord) {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Project != rows[j].Project {
			return rows[i].Project < rows[j].Project
		}
		left := rows[i].Note
		right := rows[j].Note
		leftType := noteListKind(left.Data)
		rightType := noteListKind(right.Data)
		if leftType != rightType {
			return listTypeRank(leftType) < listTypeRank(rightType)
		}
		if leftType == "task" {
			leftStatus := listStatusRank(stringField(left.Data, "status"))
			rightStatus := listStatusRank(stringField(right.Data, "status"))
			if leftStatus != rightStatus {
				return leftStatus < rightStatus
			}
			leftPriority := priorityRank(stringField(left.Data, "priority"))
			rightPriority := priorityRank(stringField(right.Data, "priority"))
			if leftPriority != rightPriority {
				return leftPriority < rightPriority
			}
		}
		return stringField(left.Data, "id") < stringField(right.Data, "id")
	})
}

func epicTaskCount(counts map[string]map[string]int, epic string) map[string]int {
	if counts[epic] != nil {
		return counts[epic]
	}
	return map[string]int{"open": 0, "done": 0, "closed": 0, "total": 0}
}

func taskCountsByScopedRecords(records []listRecord) map[string]map[string]int {
	counts := map[string]map[string]int{}
	for _, record := range records {
		note := record.Note
		if noteListKind(note.Data) != "task" {
			continue
		}
		epic := wikiTarget(note.Data["epic"])
		if epic == "" {
			epic = stringField(note.Data, "epic")
		}
		if epic == "" {
			continue
		}
		key := scopedEpicKey(record.Project, epic)
		if counts[key] == nil {
			counts[key] = map[string]int{"open": 0, "done": 0, "closed": 0, "total": 0}
		}
		counts[key]["total"]++
		if isOpenWorkStatus(stringField(note.Data, "status")) {
			counts[key]["open"]++
		} else {
			counts[key]["closed"]++
		}
		if stringField(note.Data, "status") == "done" {
			counts[key]["done"]++
		}
	}
	return counts
}

func listWaveMemberKeys(records []listRecord, waveID string) map[string]bool {
	out := map[string]bool{}
	if waveID == "" {
		return out
	}
	for _, record := range records {
		if noteListKind(record.Note.Data) != "wave" || stringField(record.Note.Data, "id") != waveID {
			continue
		}
		for _, taskID := range normalizeList(record.Note.Data["members"]) {
			out[scopedTaskKey(record.Project, taskID)] = true
		}
	}
	return out
}

func scopedEpicKey(project, epic string) string {
	return project + "\x00" + epic
}

func scopedTaskKey(project, taskID string) string {
	return project + "\x00" + taskID
}

func taskCountsByEpic(notes []Note) map[string]map[string]int {
	counts := map[string]map[string]int{}
	for _, note := range notes {
		if noteListKind(note.Data) != "task" {
			continue
		}
		epic := wikiTarget(note.Data["epic"])
		if epic == "" {
			epic = stringField(note.Data, "epic")
		}
		if epic == "" {
			continue
		}
		if counts[epic] == nil {
			counts[epic] = map[string]int{"open": 0, "done": 0, "closed": 0, "total": 0}
		}
		counts[epic]["total"]++
		status := stringField(note.Data, "status")
		if strings.EqualFold(status, "done") {
			counts[epic]["done"]++
		} else if isOpenWorkStatus(status) {
			counts[epic]["open"]++
		} else {
			counts[epic]["closed"]++
		}
	}
	return counts
}

func sortListRows(rows []Note) {
	sort.SliceStable(rows, func(i, j int) bool {
		leftType := noteListKind(rows[i].Data)
		rightType := noteListKind(rows[j].Data)
		if leftType != rightType {
			return listTypeRank(leftType) < listTypeRank(rightType)
		}
		leftStatus := listStatusRank(stringField(rows[i].Data, "status"))
		rightStatus := listStatusRank(stringField(rows[j].Data, "status"))
		if leftStatus != rightStatus {
			return leftStatus < rightStatus
		}
		leftPriority := priorityRank(stringField(rows[i].Data, "priority"))
		rightPriority := priorityRank(stringField(rows[j].Data, "priority"))
		if leftPriority != rightPriority {
			return leftPriority < rightPriority
		}
		return stringField(rows[i].Data, "id") < stringField(rows[j].Data, "id")
	})
}

func noteListKind(data map[string]any) string {
	if legacyType := stringField(data, "type"); legacyType != "" {
		return legacyType
	}
	return effectiveV7Kind(data)
}

func sortTaskMapsForProgressiveList(tasks []map[string]any) {
	sort.SliceStable(tasks, func(i, j int) bool {
		leftStatus := listStatusRank(stringValue(tasks[i]["status"]))
		rightStatus := listStatusRank(stringValue(tasks[j]["status"]))
		if leftStatus != rightStatus {
			return leftStatus < rightStatus
		}
		leftPriority := priorityRank(stringValue(tasks[i]["priority"]))
		rightPriority := priorityRank(stringValue(tasks[j]["priority"]))
		if leftPriority != rightPriority {
			return leftPriority < rightPriority
		}
		return stringValue(tasks[i]["id"]) < stringValue(tasks[j]["id"])
	})
}

func listTypeRank(noteType string) int {
	switch noteType {
	case "epic":
		return 0
	case "wave":
		return 1
	case "task":
		return 2
	case "doc":
		return 3
	default:
		return 4
	}
}

func listStatusRank(status string) int {
	order := []string{"active", "review", "rework", "ready", "blocked", "backlog", "draft", "done", "cancelled", "archived", "superseded", "deprecated"}
	for i, value := range order {
		if strings.EqualFold(status, value) {
			return i
		}
	}
	return len(order)
}

func isOpenWorkStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "done", "cancelled", "archived", "superseded", "deprecated":
		return false
	default:
		return true
	}
}

func compactTaskMeta(task map[string]any) string {
	var parts []string
	for _, key := range []string{"status", "priority", "risk"} {
		if value := stringValue(task[key]); value != "" {
			parts = append(parts, value)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, ", ") + ")"
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
		fmt.Sprintf("_Auto-generated %s. This top-level roster intentionally shows epics only. Run `tusker list --type epic` for the live terminal view, then drill into one epic with `tusker list --epic <ACR> --type task --open`._", generatedAt),
		"",
		`Agents: use this page only to choose the right epic. Do not read every task file. Pick the epic whose summary best matches; if nothing fits and the work will outlive one task, propose a new epic with `+"`tusker new epic --acronym <ACR> --title \"<name>\" --summary \"...\"`"+`.`,
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
				fmt.Sprintf("**Drill down:** `tusker list --epic %s --type task --open`.", stringValue(epic["id"])),
				"",
			)
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
	projects, err := loadRegisteredProjects(store, registeredProjectLoadOptions{})
	if err != nil {
		return section
	}
	absVault, err := filepath.Abs(vaultPath)
	if err != nil {
		absVault = vaultPath
	}
	projectIDs := map[string]struct{}{}
	for _, project := range projects {
		projectVault, err := filepath.Abs(project.Project.VaultRoot)
		if err != nil {
			projectVault = project.Project.VaultRoot
		}
		if projectVault == absVault {
			projectIDs[project.Project.ProjectID] = struct{}{}
			section["project_id"] = project.Project.ProjectID
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
