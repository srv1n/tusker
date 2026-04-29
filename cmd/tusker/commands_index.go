package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	skillbundle "tusker/skill"
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
	var epics, stories, bugs, docs, links []map[string]any
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
		case "story", "bug":
			epicRef := wikiTarget(note.Data["epic"])
			base["change_type"] = stringField(note.Data, "change_type")
			base["size"] = stringField(note.Data, "size")
			base["risk"] = stringField(note.Data, "risk")
			base["priority"] = stringField(note.Data, "priority")
			base["delegation"] = stringField(note.Data, "delegation")
			base["surfaces"] = normalizeList(note.Data["surfaces"])
			base["assignee"] = stringField(note.Data, "assignee")
			base["requester"] = stringField(note.Data, "requester")
			base["ai_assistance"] = stringField(note.Data, "ai_assistance")
			base["ai_tools"] = normalizeList(note.Data["ai_tools"])
			base["review_state"] = stringField(note.Data, "review_state")
			base["work_revision"] = intField(note.Data, "work_revision")
			base["attested_by"] = stringField(note.Data, "attested_by")
			base["attested_role"] = stringField(note.Data, "attested_role")
			base["signoff_by"] = stringField(note.Data, "signoff_by")
			base["dod_code_complete"] = boolField(note.Data, "dod_code_complete")
			base["dod_user_verified"] = boolField(note.Data, "dod_user_verified")
			base["due"] = stringField(note.Data, "due")
			base["epic"] = epicRef
			base["epic_record_id"] = stringField(note.Data, "epic_record_id")
			base["epicTitle"] = stringField(epicMap[epicRef].Data, "title")
			if noteType == "story" {
				stories = append(stories, base)
			} else {
				bugs = append(bugs, base)
			}
			links = append(links, collectLinks(note)...)
		case "doc":
			epicRef := wikiTarget(note.Data["epic"])
			base["epic"] = epicRef
			base["epic_record_id"] = stringField(note.Data, "epic_record_id")
			base["epicTitle"] = stringField(epicMap[epicRef].Data, "title")
			base["story"] = wikiTarget(note.Data["story"])
			base["story_record_id"] = stringField(note.Data, "story_record_id")
			base["audience"] = stringField(note.Data, "audience")
			base["doc_intent"] = stringField(note.Data, "doc_intent")
			base["owner_epic"] = wikiTarget(note.Data["owner_epic"])
			base["canon_for"] = wikiTarget(note.Data["canon_for"])
			base["canonical"] = boolField(note.Data, "canonical")
			base["canonical_status"] = stringField(note.Data, "canonical_status")
			base["verified_at"] = stringField(note.Data, "verified_at")
			base["deprecated"] = boolField(note.Data, "deprecated")
			base["superseded_by"] = stringField(note.Data, "superseded_by")
			base["redirect_from"] = normalizeList(note.Data["redirect_from"])
			base["publish"] = boolField(note.Data, "publish")
			base["publish_path"] = stringField(note.Data, "publish_path")
			base["publish_description"] = stringField(note.Data, "publish_description")
			base["publish_order"] = optionalIntValue(note.Data["publish_order"])
			base["publish_section_title"] = stringField(note.Data, "publish_section_title")
			base["publish_url"] = stringField(note.Data, "publish_url")
			base["published_at"] = stringField(note.Data, "published_at")
			docs = append(docs, base)
			links = append(links, collectLinks(note)...)
		}
	}
	for _, collection := range [][]map[string]any{epics, stories, bugs, docs} {
		sortByUpdatedDesc(collection)
	}
	counts := map[string]map[string]int{}
	for _, epic := range epics {
		counts[stringValue(epic["id"])] = map[string]int{"stories": 0, "bugs": 0, "docs": 0, "open": 0, "done": 0}
	}
	for _, story := range stories {
		c := counts[stringValue(story["epic"])]
		if c == nil {
			continue
		}
		c["stories"]++
		status := stringValue(story["status"])
		if status == "done" {
			c["done"]++
		} else if status != "cancelled" {
			c["open"]++
		}
	}
	for _, bug := range bugs {
		c := counts[stringValue(bug["epic"])]
		if c == nil {
			continue
		}
		c["bugs"]++
		status := stringValue(bug["status"])
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
		for _, collection := range [][]map[string]any{stories, bugs, docs} {
			for _, item := range collection {
				if stringValue(item["epic"]) == acronym {
					children = append(children, item)
				}
			}
		}
		list := "_No stories, bugs, or docs yet._"
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
		newBody := replaceSection(body, "## Stories", list)
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
	attestationQueue := []map[string]any{}
	for _, item := range append(append([]map[string]any{}, stories...), bugs...) {
		if stringValue(item["status"]) != "in_review" {
			continue
		}
		if stringValue(item["review_state"]) == "verification_requested" {
			verificationQueue = append(verificationQueue, map[string]any{
				"id":            item["id"],
				"title":         item["title"],
				"type":          item["type"],
				"epic":          item["epic"],
				"risk":          item["risk"],
				"delegation":    item["delegation"],
				"ai_assistance": item["ai_assistance"],
				"path":          item["path"],
			})
			continue
		}
		if stringValue(item["review_state"]) != "requested" && stringValue(item["review_state"]) != "approved" {
			continue
		}
		attestationQueue = append(attestationQueue, map[string]any{
			"id":               item["id"],
			"title":            item["title"],
			"type":             item["type"],
			"epic":             item["epic"],
			"risk":             item["risk"],
			"delegation":       item["delegation"],
			"ai_assistance":    item["ai_assistance"],
			"attestation_rule": attestationRequirement(stringValue(item["risk"])),
			"path":             item["path"],
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
	work := append(append([]map[string]any{}, stories...), bugs...)
	var activeQueue, blockedQueue, verificationReviewQueue, reviewQueue, reworkQueue, mergingQueue []map[string]any
	for _, item := range work {
		switch stringValue(item["status"]) {
		case "active":
			activeQueue = append(activeQueue, item)
		case "blocked":
			blockedQueue = append(blockedQueue, item)
		case "in_review":
			if stringValue(item["review_state"]) == "verification_requested" {
				verificationReviewQueue = append(verificationReviewQueue, item)
			} else {
				reviewQueue = append(reviewQueue, item)
			}
		case "rework":
			reworkQueue = append(reworkQueue, item)
		case "merging":
			mergingQueue = append(mergingQueue, item)
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
			"stories":           len(stories),
			"bugs":              len(bugs),
			"docs":              len(docs),
			"openWork":          len(activeQueue) + len(blockedQueue) + len(verificationReviewQueue) + len(reviewQueue) + len(reworkQueue) + len(mergingQueue),
			"inReview":          countStatus(work, "in_review"),
			"verificationQueue": len(verificationQueue),
			"attestationQueue":  len(attestationQueue),
			"publicationQueue":  countNotPublished(publicationQueue),
			"active":            len(activeQueue),
			"blocked":           len(blockedQueue),
			"verification":      len(verificationReviewQueue),
			"rework":            len(reworkQueue),
			"merging":           len(mergingQueue),
		},
	}
	dashboard := map[string]any{
		"generatedAt": generatedAt,
		"counts":      summary["counts"],
		"queues": map[string]any{
			"active":              activeQueue,
			"blocked":             blockedQueue,
			"verification":        verificationReviewQueue,
			"in_review":           reviewQueue,
			"rework":              reworkQueue,
			"merging":             mergingQueue,
			"verification_review": verificationQueue,
			"attestation":         attestationQueue,
			"publication":         publicationQueue,
		},
		"agents": buildAgentsSection(configData, map[string]int{}),
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
		{"stories.index.json", map[string]any{"generatedAt": generatedAt, "items": stories}},
		{"bugs.index.json", map[string]any{"generatedAt": generatedAt, "items": bugs}},
		{"docs.index.json", map[string]any{"generatedAt": generatedAt, "items": docs}},
		{"records.index.json", map[string]any{"generatedAt": generatedAt, "items": records}},
		{"links.index.json", map[string]any{"generatedAt": generatedAt, "items": links}},
		{"verification.index.json", map[string]any{"generatedAt": generatedAt, "items": verificationQueue}},
		{"attestation.index.json", map[string]any{"generatedAt": generatedAt, "items": attestationQueue}},
		{"publication.index.json", publicationManifest},
		{"summary.json", summary},
		{"dashboard.json", dashboard},
	} {
		if err := writeJSON(filepath.Join(generatedDir, target.Name), target.Data); err != nil {
			return err
		}
	}
	if err := writeVaultReadme(vaultPath, epics, stories, bugs, docs, generatedAt); err != nil {
		return err
	}
	if err := writeDashboardNote(vaultPath, runtimeSection, generatedAt); err != nil {
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
		fmt.Printf("Indexed %d epics, %d stories, %d bugs, %d docs.\n", len(epics), len(stories), len(bugs), len(docs))
		fmt.Printf("Tracker queues - active: %d, blocked: %d, verification: %d, in_review: %d, rework: %d, merging: %d\n", len(activeQueue), len(blockedQueue), len(verificationReviewQueue), len(reviewQueue), len(reworkQueue), len(mergingQueue))
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
		case "story", "bug":
			setScalarMirror("epic", "epic_record_id")
			setListMirror("related", "related_record_ids")
			setListMirror("blocks", "blocks_record_ids")
			setListMirror("blocked_by", "blocked_by_record_ids")
		case "doc":
			setScalarMirror("epic", "epic_record_id")
			setScalarMirror("story", "story_record_id")
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
	status := stringValue(doc["status"])
	if status != "approved" && status != "published" {
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
		"id":                    stringValue(doc["id"]),
		"title":                 stringValue(doc["title"]),
		"path":                  stringValue(doc["path"]),
		"epic":                  stringValue(doc["epic"]),
		"story":                 stringValue(doc["story"]),
		"audience":              stringValue(doc["audience"]),
		"doc_intent":            stringValue(doc["doc_intent"]),
		"owner_epic":            stringValue(doc["owner_epic"]),
		"canon_for":             stringValue(doc["canon_for"]),
		"canonical":             boolValue(doc["canonical"]),
		"canonical_status":      stringValue(doc["canonical_status"]),
		"verified_at":           stringValue(doc["verified_at"]),
		"deprecated":            boolValue(doc["deprecated"]),
		"superseded_by":         stringValue(doc["superseded_by"]),
		"redirect_from":         normalizeList(doc["redirect_from"]),
		"status":                stringValue(doc["status"]),
		"publish":               boolValue(doc["publish"]),
		"publish_path":          stringValue(doc["publish_path"]),
		"publish_description":   stringValue(doc["publish_description"]),
		"publish_order":         optionalIntValue(doc["publish_order"]),
		"publish_section_title": stringValue(doc["publish_section_title"]),
		"tags":                  normalizeList(doc["tags"]),
		"updated":               stringValue(doc["updated"]),
	}
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
	epicAcronyms := map[string]struct{}{}
	storyCounts := map[string]int{}
	noteIDs := map[string]struct{}{}
	idToRecordID := map[string]string{}
	recordIDs := map[string]struct{}{}
	for _, note := range notes {
		if stringField(note.Data, "type") == "epic" {
			epicAcronyms[stringField(note.Data, "id")] = struct{}{}
		}
		if stringField(note.Data, "type") == "story" {
			epic := wikiTarget(note.Data["epic"])
			if epic != "" {
				storyCounts[epic]++
			}
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
			StoryCounts:  storyCounts,
			NoteIDs:      noteIDs,
			IDToRecordID: idToRecordID,
			RecordIDs:    recordIDs,
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
	epic := strings.ToUpper(args.String("epic"))
	status := args.String("status")
	assignee := args.String("assignee")
	reviewState := args.String("review-state")
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
		if reviewState != "" && stringField(note.Data, "review_state") != reviewState {
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
				"review_state":  stringField(note.Data, "review_state"),
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

func epicsCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	notes, err := listAllNotes(vaultPath)
	if err != nil {
		return err
	}
	var epics []Note
	counts := map[string]map[string]int{}
	for _, note := range notes {
		if stringField(note.Data, "type") == "epic" {
			epics = append(epics, note)
		}
	}
	for _, note := range notes {
		t := stringField(note.Data, "type")
		if t != "story" && t != "bug" && t != "doc" {
			continue
		}
		epic := wikiTarget(note.Data["epic"])
		if epic == "" {
			continue
		}
		if counts[epic] == nil {
			counts[epic] = map[string]int{}
		}
		counts[epic][t]++
	}
	rows := make([]map[string]any, 0, len(epics))
	for _, note := range epics {
		id := stringField(note.Data, "id")
		row := map[string]any{
			"id":      id,
			"title":   stringField(note.Data, "title"),
			"status":  stringField(note.Data, "status"),
			"summary": strings.TrimSpace(stringField(note.Data, "summary")),
			"stories": counts[id]["story"],
			"bugs":    counts[id]["bug"],
			"docs":    counts[id]["doc"],
		}
		rows = append(rows, row)
	}
	statusOrder := map[string]int{"active": 0, "intake": 1, "blocked": 2, "done": 3, "cancelled": 4}
	sort.Slice(rows, func(i, j int) bool {
		sa := statusOrder[stringValue(rows[i]["status"])]
		sb := statusOrder[stringValue(rows[j]["status"])]
		if sa != sb {
			return sa < sb
		}
		return stringValue(rows[i]["id"]) < stringValue(rows[j]["id"])
	})
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "count": len(rows), "epics": rows})
		return nil
	}
	if len(rows) == 0 {
		if !args.Bool("quiet") {
			fmt.Println("(no epics yet)")
		}
		return nil
	}
	for _, row := range rows {
		countsText := fmt.Sprintf("%ds/%db/%dd", intValue(row["stories"]), intValue(row["bugs"]), intValue(row["docs"]))
		summary := stringValue(row["summary"])
		if summary == "" {
			summary = "(no summary)"
		}
		fmt.Printf("%-5s %-10s %-10s %s - %s\n", stringValue(row["id"]), stringValue(row["status"]), countsText, stringValue(row["title"]), summary)
	}
	return nil
}

func moveCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	id, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	targetEpic, err := requireArg(args, "to-epic")
	if err != nil {
		return err
	}
	targetEpic = strings.ToUpper(targetEpic)
	actor := fallback(fallback(args.String("actor"), args.String("by")), defaultActorName())
	reason := args.String("reason")
	if !epicAcronymPattern.MatchString(targetEpic) {
		return tuskerError(errorInvalidArg, fmt.Sprintf(`--to-epic must be 3 uppercase letters, got "%s"`, args.String("to-epic")), withContext(map[string]any{"arg": "--to-epic", "value": args.String("to-epic")}))
	}
	note, err := resolveNote(vaultPath, id)
	if err != nil {
		return err
	}
	noteType := stringField(note.Data, "type")
	if noteType != "story" && noteType != "bug" && noteType != "doc" {
		return tuskerError(errorInvalidArg, fmt.Sprintf("move only supports story/bug/doc notes, got %s", noteType), withContext(map[string]any{"id": id, "type": noteType}))
	}
	parsed := parseID(stringField(note.Data, "id"))
	if parsed == nil {
		return tuskerError(errorIDScheme, fmt.Sprintf(`cannot parse id "%s"`, stringField(note.Data, "id")), withPath(note.RelativePath))
	}
	if parsed.Acronym == targetEpic {
		if !args.Bool("quiet") {
			fmt.Printf("%s is already in epic %s\n", stringField(note.Data, "id"), targetEpic)
		}
		return nil
	}
	targetEpicIndex := filepath.Join(vaultPath, "epics", targetEpic, "index.md")
	if !fileExists(targetEpicIndex) {
		return tuskerError(errorNotFound, fmt.Sprintf("target epic %s does not exist - run `tusker new-epic --acronym %s` first", targetEpic, targetEpic), withContext(map[string]any{"acronym": targetEpic}))
	}
	allNotes, err := listAllNotes(vaultPath)
	if err != nil {
		return err
	}
	sequence := nextSequence(allNotes, targetEpic, parsed.Kind)
	letter := map[string]string{"story": "S", "bug": "B", "doc": "D"}[parsed.Kind]
	newID := fmt.Sprintf("%s-%s-%s", targetEpic, letter, padNumber(sequence))
	oldID := stringField(note.Data, "id")
	newAbsPath := filepath.Join(vaultPath, "epics", targetEpic, newID+".md")
	data, body, err := parseFrontmatterMustRead(note.AbsolutePath)
	if err != nil {
		return err
	}
	date := todayISO()
	now := time.Now().UTC().Format(time.RFC3339)
	data["id"] = newID
	data["epic"] = "[[" + targetEpic + "]]"
	targetEpicNote, err := resolveNote(vaultPath, targetEpic)
	if err != nil {
		return err
	}
	data["epic_record_id"] = stringField(targetEpicNote.Data, "record_id")
	data["updated"] = date
	appendTransition(data, orderedTransition(now, "move", oldID, newID, actor, reason))
	body = appendWorkLogBullet(body, fmt.Sprintf("%s - %s - moved %s -> %s%s", date, actor, oldID, newID, suffixReason(reason)))
	content, err := serializeDocument(data, body, frontmatterOrderForType(stringField(data, "type")))
	if err != nil {
		return err
	}
	if err := writeText(newAbsPath, content); err != nil {
		return err
	}
	if err := os.Remove(note.AbsolutePath); err != nil {
		return err
	}
	var references []map[string]string
	for _, current := range allNotes {
		if stringField(current.Data, "id") == oldID {
			continue
		}
		for _, field := range []string{"blocks", "blocked_by", "related", "epic", "story", "spec_source", "docs"} {
			value, ok := current.Data[field]
			if !ok || value == nil {
				continue
			}
			values := normalizeList(value)
			if len(values) == 0 {
				values = []string{toString(value)}
			}
			for _, candidate := range values {
				if wikiTarget(candidate) == oldID {
					references = append(references, map[string]string{"from": firstNonEmpty(stringField(current.Data, "id"), current.RelativePath), "field": field})
				}
			}
		}
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "from": oldID, "to": newID, "path": filepath.ToSlash(strings.TrimPrefix(newAbsPath, vaultPath+string(filepath.Separator))), "references": references})
		return nil
	}
	if !args.Bool("quiet") {
		fmt.Printf("Moved %s -> %s\n", oldID, newID)
		if len(references) > 0 {
			fmt.Printf("Warning: %d wikilink(s) still reference %s:\n", len(references), oldID)
			for _, ref := range references {
				fmt.Printf("  - %s (%s)\n", ref["from"], ref["field"])
			}
			fmt.Println("Fix manually, then run `tusker reindex` and `tusker validate`.")
		}
	}
	autoReindex(vaultPath)
	return nil
}

func writeVaultReadme(vaultPath string, epics, stories, bugs, docs []map[string]any, generatedAt string) error {
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
	statusOrder := []string{"active", "intake", "paused", "done", "cancelled"}
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
	for _, collection := range [][]map[string]any{stories, bugs, docs} {
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
		fmt.Sprintf("_Auto-generated %s. Run `tusker epics` for a live terminal view. Everything below this heading is regenerated on every `tusker reindex` — edits here get overwritten._", generatedAt),
		"",
		`Agents: read this file before logging new work. Pick the epic whose summary best matches; if nothing fits and the work will outlive one story, propose a new epic with `+"`tusker new-epic --summary \"...\"`"+`.`,
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
				summary = "_(no summary — run `tusker new-epic` with --summary or edit the epic index)_"
			}
			lines = append(lines,
				fmt.Sprintf("### [[%s]] — %s", stringValue(epic["id"]), stringValue(epic["title"])),
				"",
				"**Summary:** "+summary,
				"",
				fmt.Sprintf("**Counts:** %d stor%s, %d bug%s, %d doc%s (open: %d, done: %d)", counts["stories"], pluralY(counts["stories"]), counts["bugs"], plural(counts["bugs"]), counts["docs"], plural(counts["docs"]), counts["open"], counts["done"]),
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
		lines = append(lines, "_(no epics yet — create one with `tusker new-epic --acronym <ACR> --title <title> --summary \"...\"`)_", "")
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

func writeDashboardNote(vaultPath string, runtime map[string]any, generatedAt string) error {
	dashboardPath := filepath.Join(vaultPath, "Dashboard.md")
	body := ""
	if fileExists(dashboardPath) {
		existing, err := readText(dashboardPath)
		if err != nil {
			return err
		}
		body = existing
	} else {
		template, err := skillbundle.GetAsset("templates/dashboard.md")
		if err != nil {
			return err
		}
		body = replaceTemplateTokens(template, map[string]string{"{{date}}": generatedAt[:10]})
	}
	if !strings.Contains(body, "## Active stories") {
		body += "\n\n## Active stories\n\n![[_system/views/Stories.base#Active]]\n"
	}
	if !strings.Contains(body, dashboardRunsBegin) || !strings.Contains(body, dashboardRunsEnd) {
		body += "\n\n## Live runs\n\n" + dashboardRunsBegin + "\n" + dashboardRunsEnd + "\n"
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

func renderDashboardRunsBlock(runtime map[string]any, generatedAt string) string {
	activeRuns := anySlice(runtime["active_runs"])
	if len(activeRuns) == 0 {
		return fmt.Sprintf("_Auto-generated %s. No live runs right now._", generatedAt)
	}
	lines := []string{
		fmt.Sprintf("_Auto-generated %s. Live daemon activity is shown below._", generatedAt),
		"",
		"| Story | Runner | Lease | Session |",
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
