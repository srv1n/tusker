package main

import (
	"fmt"
	"path/filepath"
	"strings"

	skillbundle "tusker/skill"
)

func bootstrap(args Args) error {
	vaultPath, err := resolveVaultPath(args, true)
	if err != nil {
		return err
	}
	date := todayISO()

	for _, relative := range []string{
		"",
		"epics",
		"Attachments",
		"_system/templates",
		"_system/views",
		"_system/generated",
		"_system/snippets",
		"_system/archive",
		"_system/workspaces",
		"_system/logs",
	} {
		if err := ensureDir(filepath.Join(vaultPath, relative)); err != nil {
			return err
		}
	}
	if err := writeDefaultConfig(vaultPath); err != nil {
		return err
	}
	if err := writeEmbeddedTree("templates", filepath.Join(vaultPath, "_system", "templates"), true, nil); err != nil {
		return err
	}
	if err := writeEmbeddedTree("bases", filepath.Join(vaultPath, "_system", "views"), true, nil); err != nil {
		return err
	}
	architecturePath := filepath.Join(vaultPath, "architecture.md")
	if !fileExists(architecturePath) {
		content := fmt.Sprintf("---\ntitle: \"Product architecture\"\ntype: \"note\"\ncreated: \"%s\"\nupdated: \"%s\"\ntags:\n  - architecture\n---\n\n# Product architecture\n\nProduct-wide durable architectural decisions. Append one bullet per decision via `promote-decision --target architecture`.\n\n## Decisions\n\n", date, date)
		if err := writeText(architecturePath, content); err != nil {
			return err
		}
	}
	dashboardPath := filepath.Join(vaultPath, "Dashboard.md")
	if !fileExists(dashboardPath) {
		template, err := skillbundle.GetAsset("templates/dashboard.md")
		if err != nil {
			return err
		}
		if err := writeText(dashboardPath, replaceTemplateTokens(template, map[string]string{"{{date}}": date})); err != nil {
			return err
		}
	}
	cheatsheetPath := filepath.Join(vaultPath, "CHEATSHEET.md")
	if !fileExists(cheatsheetPath) {
		template, err := skillbundle.GetAsset("templates/cheatsheet.md")
		if err != nil {
			return err
		}
		if err := writeText(cheatsheetPath, replaceTemplateTokens(template, map[string]string{"{{date}}": date})); err != nil {
			return err
		}
	}
	if err := upsertGitignore(vaultPath); err != nil {
		return err
	}
	if epic := strings.ToUpper(args.String("epic")); epic != "" {
		if !epicAcronymPattern.MatchString(epic) {
			return tuskerError(errorInvalidArg, fmt.Sprintf(`--epic must be 3 uppercase letters, got "%s"`, args.String("epic")), withContext(map[string]any{"arg": "--epic", "value": args.String("epic")}))
		}
		title, err := requireArg(args, "title")
		if err != nil {
			return tuskerError(errorMissingArg, "--title (required with --epic)")
		}
		if err := createEpicInternal(vaultPath, epic, title, args); err != nil {
			return err
		}
	}
	if !args.Bool("quiet") {
		fmt.Printf("Tusker vault bootstrapped at %s\n", vaultPath)
	}
	return nil
}

func newEpic(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	acronym := strings.ToUpper(args.String("acronym"))
	if acronym == "" {
		return tuskerError(errorMissingArg, "Missing required argument --acronym", withContext(map[string]any{"arg": "--acronym"}))
	}
	if !epicAcronymPattern.MatchString(acronym) {
		return tuskerError(errorInvalidArg, fmt.Sprintf(`--acronym must be 3 uppercase letters, got "%s"`, args.String("acronym")), withContext(map[string]any{"arg": "--acronym", "value": args.String("acronym")}))
	}
	title, err := requireArg(args, "title")
	if err != nil {
		return err
	}
	notes, err := listAllNotes(vaultPath)
	if err != nil {
		return err
	}
	for _, note := range notes {
		if stringField(note.Data, "type") == "epic" && stringField(note.Data, "id") == acronym {
			return tuskerError(errorAlreadyExists, fmt.Sprintf(`Epic acronym "%s" already exists`, acronym), withContext(map[string]any{"acronym": acronym}))
		}
	}
	if err := createEpicInternal(vaultPath, acronym, title, args); err != nil {
		return err
	}
	if !args.Bool("quiet") {
		fmt.Printf("Created epic %s at %s\n", acronym, filepath.Join(vaultPath, "epics", acronym, "index.md"))
	}
	autoReindex(vaultPath)
	return nil
}

func createEpicInternal(vaultPath, acronym, title string, args Args) error {
	date := todayISO()
	actor := defaultActorName()
	epicDir := filepath.Join(vaultPath, "epics", acronym)
	indexPath := filepath.Join(epicDir, "index.md")
	if fileExists(indexPath) {
		return tuskerError(errorAlreadyExists, "Epic already exists: "+indexPath, withPath(indexPath))
	}
	if err := ensureDir(epicDir); err != nil {
		return err
	}
	template, err := skillbundle.GetAsset("templates/epic.md")
	if err != nil {
		return err
	}
	rendered := replaceTemplateTokens(template, map[string]string{
		"{{acronym}}":        acronym,
		"{{title}}":          title,
		"{{date}}":           date,
		"{{owner}}":          fallback(args.String("owner"), actor),
		"{{summary}}":        args.String("summary"),
		"{{spec_source}}":    args.String("spec-source"),
		"{{target_release}}": args.String("target-release"),
	})
	summary := strings.TrimSpace(args.String("summary"))
	if len(summary) > 120 {
		return tuskerError(errorInvalidArg, fmt.Sprintf("--summary is %d chars; keep it <=120", len(summary)), withContext(map[string]any{"arg": "--summary", "length": len(summary)}))
	}
	data, body, err := parseFrontmatter(rendered)
	if err != nil {
		return err
	}
	data["id"] = acronym
	data["schema_version"] = 2
	data["record_id"] = newRecordID()
	data["title"] = title
	data["type"] = "epic"
	data["status"] = fallback(args.String("status"), "intake")
	data["owner"] = fallback(args.String("owner"), actor)
	data["summary"] = summary
	data["target_release"] = args.String("target-release")
	data["spec_source"] = args.String("spec-source")
	data["docs"] = splitCSVLinks(args.String("docs"))
	data["docs_record_ids"] = []any{}
	data["created"] = date
	data["updated"] = date
	data["success_metrics"] = splitCSV(args.String("success-metrics"))
	data["transitions"] = []any{}
	data["tags"] = splitCSV(args.String("tags"))
	content, err := serializeDocument(data, body, frontmatterOrder["epic"])
	if err != nil {
		return err
	}
	return writeText(indexPath, content)
}

func createWorkItem(args Args, noteType string) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	actor := defaultActorName()
	acronym, err := requireArg(args, "epic")
	if err != nil {
		return err
	}
	acronym = strings.ToUpper(acronym)
	if !epicAcronymPattern.MatchString(acronym) {
		return tuskerError(errorInvalidArg, fmt.Sprintf(`--epic must be 3 uppercase letters, got "%s"`, args.String("epic")))
	}
	title, err := requireArg(args, "title")
	if err != nil {
		return err
	}
	changeType := args.String("change-type")
	if changeType == "" {
		if noteType == "bug" {
			changeType = "bug"
		} else {
			changeType = "feature"
		}
	}
	size, err := requireArg(args, "size")
	if err != nil {
		return err
	}
	risk, err := requireArg(args, "risk")
	if err != nil {
		return err
	}
	size = strings.ToLower(size)
	risk = strings.ToLower(risk)
	priority := fallback(args.String("priority"), "p2")
	delegation := fallback(args.String("delegation"), "execute")
	aiAssistance := fallback(args.String("ai-assistance"), "heavy")
	if _, ok := changeTypes[changeType]; !ok {
		return tuskerError(errorInvalidField, "Invalid change_type: "+changeType, withContext(map[string]any{"field": "change_type", "value": changeType}))
	}
	if _, ok := sizes[size]; !ok {
		return tuskerError(errorInvalidField, "Invalid size: "+size, withContext(map[string]any{"field": "size", "value": size}))
	}
	if _, ok := risks[risk]; !ok {
		return tuskerError(errorInvalidField, "Invalid risk: "+risk, withContext(map[string]any{"field": "risk", "value": risk}))
	}
	if _, ok := priorities[priority]; !ok {
		return tuskerError(errorInvalidField, "Invalid priority: "+priority, withContext(map[string]any{"field": "priority", "value": priority}))
	}
	if _, ok := delegations[delegation]; !ok {
		return tuskerError(errorInvalidField, "Invalid delegation: "+delegation, withContext(map[string]any{"field": "delegation", "value": delegation}))
	}
	if _, ok := aiAssistanceLevels[aiAssistance]; !ok {
		return tuskerError(errorInvalidField, "Invalid ai_assistance: "+aiAssistance, withContext(map[string]any{"field": "ai_assistance", "value": aiAssistance}))
	}
	epicDir := filepath.Join(vaultPath, "epics", acronym)
	if !fileExists(filepath.Join(epicDir, "index.md")) {
		return tuskerError(errorNotFound, fmt.Sprintf("Epic %s does not exist. Run `tusker new-epic` first.", acronym), withContext(map[string]any{"acronym": acronym}))
	}
	notes, err := listAllNotes(vaultPath)
	if err != nil {
		return err
	}
	sequence := nextSequence(notes, acronym, noteType)
	id := fmt.Sprintf("%s-%s-%s", acronym, map[string]string{"story": "S", "bug": "B"}[noteType], padNumber(sequence))
	filePath := filepath.Join(epicDir, id+".md")
	date := todayISO()
	epicNote, err := resolveNote(vaultPath, acronym)
	if err != nil {
		return err
	}
	epicRecordID := stringField(epicNote.Data, "record_id")
	templateName := "story.md"
	if noteType == "bug" {
		templateName = "bug.md"
	}
	template, err := skillbundle.GetAsset("templates/" + templateName)
	if err != nil {
		return err
	}
	rendered := replaceTemplateTokens(template, map[string]string{
		"{{id}}":    id,
		"{{title}}": title,
		"{{epic}}":  acronym,
		"{{date}}":  date,
	})
	data, body, err := parseFrontmatter(rendered)
	if err != nil {
		return err
	}
	data["id"] = id
	data["schema_version"] = 2
	data["record_id"] = newRecordID()
	data["title"] = title
	data["type"] = noteType
	data["status"] = "intake"
	data["review_state"] = "none"
	data["work_revision"] = 0
	data["change_type"] = changeType
	data["epic"] = "[[" + acronym + "]]"
	data["epic_record_id"] = epicRecordID
	data["size"] = size
	data["risk"] = risk
	data["priority"] = priority
	data["delegation"] = delegation
	data["surfaces"] = splitCSV(args.String("surfaces"))
	data["assignee"] = args.String("assignee")
	data["requester"] = fallback(args.String("requester"), fallback(args.String("assignee"), actor))
	data["ai_assistance"] = aiAssistance
	data["ai_tools"] = splitCSV(args.String("ai-tools"))
	data["ai_session_log"] = args.String("ai-session-log")
	data["attested_by"] = ""
	data["attested_at"] = ""
	data["attested_role"] = ""
	data["signoff_by"] = ""
	data["signoff_at"] = ""
	data["dod_code_complete"] = false
	data["dod_user_verified"] = false
	data["created"] = date
	data["updated"] = date
	data["due"] = args.String("due")
	data["review_requested_at"] = ""
	data["verified_by"] = ""
	data["verified_at"] = ""
	data["reviewed_by"] = ""
	data["reviewed_at"] = ""
	data["prs"] = []any{}
	data["related"] = splitCSVLinks(args.String("related"))
	data["related_record_ids"] = resolveRecordIDsByLink(notes, data["related"])
	data["blocks"] = splitCSVLinks(args.String("blocks"))
	data["blocks_record_ids"] = resolveRecordIDsByLink(notes, data["blocks"])
	data["blocked_by"] = splitCSVLinks(args.String("blocked-by"))
	data["blocked_by_record_ids"] = resolveRecordIDsByLink(notes, data["blocked_by"])
	data["transitions"] = []any{}
	data["tags"] = splitCSV(args.String("tags"))
	content, err := serializeDocument(data, body, frontmatterOrderForType(noteType))
	if err != nil {
		return err
	}
	if err := writeText(filePath, content); err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "id": id, "type": noteType, "path": filePath})
	} else if !args.Bool("quiet") {
		fmt.Printf("Created %s %s at %s\n", noteType, id, filePath)
	}
	autoReindex(vaultPath)
	return nil
}

func newDoc(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	acronym, err := requireArg(args, "epic")
	if err != nil {
		return err
	}
	acronym = strings.ToUpper(acronym)
	if !epicAcronymPattern.MatchString(acronym) {
		return tuskerError(errorInvalidArg, fmt.Sprintf(`--epic must be 3 uppercase letters, got "%s"`, args.String("epic")))
	}
	title, err := requireArg(args, "title")
	if err != nil {
		return err
	}
	audience := fallback(args.String("audience"), "developer")
	status := fallback(args.String("status"), "draft")
	canonFor := strings.ToUpper(wikiTarget(args.String("canon-for")))
	companionTo := wikiTarget(firstNonEmpty(args.String("companion-to"), args.String("story")))
	publish, err := parseBooleanArg(args.String("publish"), false)
	if err != nil {
		return err
	}
	publishPath := strings.TrimSpace(args.String("publish-path"))
	publishDescription := strings.TrimSpace(args.String("publish-description"))
	publishOrder := strings.TrimSpace(args.String("publish-order"))
	publishSectionTitle := strings.TrimSpace(args.String("publish-section-title"))
	if _, ok := docStatuses[status]; !ok {
		return tuskerError(errorInvalidField, "Invalid doc status: "+status, withContext(map[string]any{"field": "status", "value": status}))
	}
	if _, ok := docAudiences[audience]; !ok {
		return tuskerError(errorInvalidField, "Invalid audience: "+audience, withContext(map[string]any{"field": "audience", "value": audience}))
	}
	if canonFor != "" && companionTo != "" {
		return tuskerError(errorInvalidArg, "Use either --canon-for or --companion-to, not both", withContext(map[string]any{"canon_for": canonFor, "companion_to": companionTo}))
	}
	if audience == "developer" && canonFor == "" && companionTo == "" {
		return tuskerError(errorInvalidArg, "Developer docs must declare intent with --canon-for <EPIC> or --companion-to <STORY-ID>", withHint("Use --canon-for for canonical specs or --companion-to for supporting docs"))
	}
	if canonFor != "" {
		if !epicAcronymPattern.MatchString(canonFor) {
			return tuskerError(errorInvalidArg, fmt.Sprintf(`--canon-for must be a 3-letter epic acronym, got "%s"`, canonFor), withContext(map[string]any{"arg": "--canon-for", "value": canonFor}))
		}
		if canonFor != acronym {
			return tuskerError(errorInvalidArg, fmt.Sprintf(`--canon-for %s must match --epic %s`, canonFor, acronym), withHint("Canonical docs live under the epic they define"))
		}
	}
	if publish {
		if _, ok := publishableDocStatuses[status]; !ok {
			return tuskerError(errorInvalidArg, "--publish true requires --status approved or --status published", withContext(map[string]any{"status": status}), withHint("Create as a draft with --publish false, or approve it before publishing."))
		}
		if strings.TrimSpace(publishPath) == "" {
			return tuskerError(errorInvalidArg, "--publish true requires --publish-path", withContext(map[string]any{"field": "publish_path"}))
		}
		if strings.TrimSpace(publishDescription) == "" {
			return tuskerError(errorInvalidArg, "--publish true requires --publish-description", withContext(map[string]any{"field": "publish_description"}))
		}
	}
	if publishOrder != "" && !isIntegerValue(publishOrder) {
		return tuskerError(errorInvalidArg, "--publish-order must be an integer", withContext(map[string]any{"field": "publish_order", "value": publishOrder}))
	}
	if strings.TrimSpace(publishPath) != "" {
		if reason := validatePublishPath(publishPath); reason != "" {
			return tuskerError(errorInvalidArg, fmt.Sprintf(`invalid --publish-path "%s": %s`, publishPath, reason), withContext(map[string]any{"field": "publish_path", "value": publishPath}))
		}
	}
	epicDir := filepath.Join(vaultPath, "epics", acronym)
	if !fileExists(filepath.Join(epicDir, "index.md")) {
		return tuskerError(errorNotFound, fmt.Sprintf("Epic %s does not exist.", acronym), withContext(map[string]any{"acronym": acronym}))
	}
	notes, err := listAllNotes(vaultPath)
	if err != nil {
		return err
	}
	epicNote, err := resolveNote(vaultPath, acronym)
	if err != nil {
		return err
	}
	epicRecordID := stringField(epicNote.Data, "record_id")
	sequence := nextSequence(notes, acronym, "doc")
	id := fmt.Sprintf("%s-D-%s", acronym, padNumber(sequence))
	filePath := filepath.Join(epicDir, id+".md")
	date := todayISO()
	template, err := skillbundle.GetAsset("templates/doc.md")
	if err != nil {
		return err
	}
	rendered := replaceTemplateTokens(template, map[string]string{
		"{{id}}":       id,
		"{{title}}":    title,
		"{{epic}}":     acronym,
		"{{audience}}": audience,
		"{{date}}":     date,
	})
	data, body, err := parseFrontmatter(rendered)
	if err != nil {
		return err
	}
	data["id"] = id
	data["schema_version"] = 2
	data["record_id"] = newRecordID()
	data["title"] = title
	data["type"] = "doc"
	data["status"] = status
	data["epic"] = "[[" + acronym + "]]"
	data["epic_record_id"] = epicRecordID
	if companionTo != "" {
		storyNote, err := resolveNote(vaultPath, companionTo)
		if err != nil {
			return err
		}
		if stringField(storyNote.Data, "type") != "story" {
			return tuskerError(errorInvalidArg, fmt.Sprintf(`--companion-to must point at a story, got "%s"`, companionTo), withContext(map[string]any{"arg": "--companion-to", "value": companionTo}))
		}
		if parsed := parseID(companionTo); parsed == nil || parsed.Acronym != acronym {
			return tuskerError(errorInvalidArg, fmt.Sprintf(`story "%s" does not belong to epic %s`, companionTo, acronym), withContext(map[string]any{"story": companionTo, "epic": acronym}))
		}
		data["doc_intent"] = "companion"
		data["canon_for"] = ""
		data["canonical"] = false
		data["canonical_status"] = ""
		data["owner_epic"] = ""
		data["verified_at"] = ""
		data["deprecated"] = false
		data["superseded_by"] = ""
		data["story"] = "[[" + companionTo + "]]"
		data["story_record_id"] = stringField(storyNote.Data, "record_id")
	} else if canonFor != "" {
		data["doc_intent"] = "canon"
		data["canon_for"] = "[[" + canonFor + "]]"
		data["canonical"] = true
		if status == "approved" || status == "published" {
			data["canonical_status"] = "approved"
			data["verified_at"] = date
		} else {
			data["canonical_status"] = "draft"
			data["verified_at"] = ""
		}
		data["owner_epic"] = "[[" + canonFor + "]]"
		data["deprecated"] = false
		data["superseded_by"] = ""
		data["story"] = ""
		data["story_record_id"] = ""
	} else {
		data["doc_intent"] = ""
		data["canon_for"] = ""
		data["canonical"] = false
		data["canonical_status"] = ""
		data["owner_epic"] = ""
		data["verified_at"] = ""
		data["deprecated"] = false
		data["superseded_by"] = ""
		data["story"] = ""
		data["story_record_id"] = ""
	}
	data["audience"] = audience
	data["publish"] = publish
	data["publish_path"] = publishPath
	data["publish_description"] = publishDescription
	if publishOrder != "" {
		data["publish_order"] = publishOrder
	}
	if publishSectionTitle != "" {
		data["publish_section_title"] = publishSectionTitle
	}
	data["publish_url"] = args.String("publish-url")
	if status == "published" {
		data["published_at"] = date
	} else {
		data["published_at"] = ""
	}
	data["created"] = date
	data["updated"] = date
	data["tags"] = splitCSV(args.String("tags"))
	content, err := serializeDocument(data, body, frontmatterOrder["doc"])
	if err != nil {
		return err
	}
	if err := writeText(filePath, content); err != nil {
		return err
	}
	if !args.Bool("quiet") {
		fmt.Printf("Created doc %s at %s\n", id, filePath)
	}
	autoReindex(vaultPath)
	return nil
}
