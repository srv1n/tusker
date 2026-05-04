package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var legacyWorkIDPattern = regexp.MustCompile(`^([A-Z]{3})-([SBT])-(\d{4})$`)

type v5MigrationReport struct {
	OK              bool              `json:"ok"`
	DryRun          bool              `json:"dryRun"`
	Vault           string            `json:"vault"`
	BackupPath      string            `json:"backupPath,omitempty"`
	NotesScanned    int               `json:"notesScanned"`
	NotesChanged    int               `json:"notesChanged"`
	FilesMoved      int               `json:"filesMoved"`
	DocsMapNodesAdd int               `json:"docsMapNodesAdded"`
	IDMap           map[string]string `json:"idMap,omitempty"`
	Moved           []v5MigrationMove `json:"moved,omitempty"`
	Changed         []string          `json:"changed,omitempty"`
	Warnings        []string          `json:"warnings,omitempty"`
}

type v5MigrationMove struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type legacyWorkRef struct {
	OldID   string
	Acronym string
	Letter  string
	Seq     int
	Path    string
}

type v5NoteMigrationPlan struct {
	TargetID   string
	TargetType string
	TargetKind string
	TargetRel  string
}

func migrateLegacyVaultToV5(args Args) (*v5MigrationReport, error) {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return nil, err
	}
	notes, err := listAllNotes(vaultPath)
	if err != nil {
		return nil, err
	}
	idMap := buildLegacyWorkIDMap(notes)
	report := &v5MigrationReport{
		OK:           true,
		DryRun:       args.Bool("dry-run"),
		Vault:        vaultPath,
		NotesScanned: len(notes),
		IDMap:        idMap,
	}
	if !report.DryRun && !args.Bool("no-backup") {
		backupPath, err := backupVaultForV5Migration(vaultPath)
		if err != nil {
			return nil, err
		}
		report.BackupPath = backupPath
	}

	for _, note := range notes {
		plan, ok := planV5Migration(note, idMap)
		if !ok {
			continue
		}
		original, err := readText(note.AbsolutePath)
		if err != nil {
			return nil, err
		}
		data, body, err := parseFrontmatter(original)
		if err != nil {
			return nil, err
		}
		oldID := stringField(data, "id")
		body = replaceLegacyWorkLinks(body, idMap)
		if mapped := idMap[oldID]; mapped != "" && mapped != oldID {
			body = replaceOwnHeadingID(body, oldID, mapped)
		}
		data = replaceLegacyWorkRefsInMap(data, idMap)
		applyV5Plan(data, &body, note, plan)

		content, err := serializeDocument(data, body, frontmatterOrderForType(plan.TargetType))
		if err != nil {
			return nil, err
		}
		targetAbs := filepath.Join(vaultPath, filepath.FromSlash(plan.TargetRel))
		targetChanged := filepath.Clean(targetAbs) != filepath.Clean(note.AbsolutePath)
		contentChanged := content != original
		if !targetChanged && !contentChanged {
			continue
		}
		report.NotesChanged++
		report.Changed = append(report.Changed, plan.TargetRel)
		if targetChanged {
			report.FilesMoved++
			report.Moved = append(report.Moved, v5MigrationMove{From: note.RelativePath, To: plan.TargetRel})
		}
		if report.DryRun {
			continue
		}
		if targetChanged {
			if fileExists(targetAbs) {
				return nil, tuskerError(errorAlreadyExists, "migration target already exists: "+targetAbs, withHint("inspect the existing file or rerun after moving it aside"))
			}
			if err := ensureDir(filepath.Dir(targetAbs)); err != nil {
				return nil, err
			}
			if err := os.Rename(note.AbsolutePath, targetAbs); err != nil {
				return nil, err
			}
		}
		if err := writeText(targetAbs, content); err != nil {
			return nil, err
		}
	}
	if !report.DryRun {
		added, err := ensureDocsMapIncludesPublishedDocs(vaultPath)
		if err != nil {
			return nil, err
		}
		report.DocsMapNodesAdd = added
	}
	sort.Strings(report.Changed)
	sort.Slice(report.Moved, func(i, j int) bool {
		return report.Moved[i].From < report.Moved[j].From
	})
	return report, nil
}

func buildLegacyWorkIDMap(notes []Note) map[string]string {
	byEpic := map[string][]legacyWorkRef{}
	for _, note := range notes {
		id := stringField(note.Data, "id")
		match := legacyWorkIDPattern.FindStringSubmatch(id)
		if match == nil {
			continue
		}
		noteType := stringField(note.Data, "type")
		if noteType != "story" && noteType != "bug" && noteType != "task" {
			continue
		}
		byEpic[match[1]] = append(byEpic[match[1]], legacyWorkRef{
			OldID:   id,
			Acronym: match[1],
			Letter:  match[2],
			Seq:     atoiSafe(match[3]),
			Path:    note.RelativePath,
		})
	}
	out := map[string]string{}
	for acronym, refs := range byEpic {
		sort.Slice(refs, func(i, j int) bool {
			left := legacyWorkSortKey(refs[i])
			right := legacyWorkSortKey(refs[j])
			if left != right {
				return left < right
			}
			if refs[i].Seq != refs[j].Seq {
				return refs[i].Seq < refs[j].Seq
			}
			return refs[i].Path < refs[j].Path
		})
		used := map[int]struct{}{}
		maxSeq := 0
		for _, ref := range refs {
			desired := ref.Seq
			if desired <= 0 {
				desired = maxSeq + 1
			}
			if _, exists := used[desired]; exists {
				desired = nextUnusedSequence(used, maxSeq+1)
			}
			used[desired] = struct{}{}
			maxSeq = maxInt(maxSeq, desired)
			out[ref.OldID] = fmt.Sprintf("%s-T-%s", acronym, padNumber(desired))
		}
	}
	return out
}

func legacyWorkSortKey(ref legacyWorkRef) int {
	switch ref.Letter {
	case "T":
		return 0
	case "S":
		return 1
	case "B":
		return 2
	default:
		return 3
	}
}

func nextUnusedSequence(used map[int]struct{}, start int) int {
	for candidate := maxInt(1, start); ; candidate++ {
		if _, exists := used[candidate]; !exists {
			return candidate
		}
	}
}

func planV5Migration(note Note, idMap map[string]string) (v5NoteMigrationPlan, bool) {
	data := note.Data
	noteType := stringField(data, "type")
	id := stringField(data, "id")
	switch noteType {
	case "epic":
		if !epicAcronymPattern.MatchString(id) {
			return v5NoteMigrationPlan{}, false
		}
		return v5NoteMigrationPlan{
			TargetID:   id,
			TargetType: "epic",
			TargetRel:  filepath.ToSlash(filepath.Join("epics", id, id+".md")),
		}, true
	case "story", "bug", "task":
		targetID := idMap[id]
		if targetID == "" {
			targetID = id
		}
		parsed := parseID(targetID)
		if parsed == nil || parsed.Kind != "task" {
			return v5NoteMigrationPlan{}, false
		}
		return v5NoteMigrationPlan{
			TargetID:   targetID,
			TargetType: "task",
			TargetKind: legacyTaskKind(data, id),
			TargetRel:  filepath.ToSlash(filepath.Join("epics", parsed.Acronym, targetID+".md")),
		}, true
	case "doc":
		return v5NoteMigrationPlan{
			TargetID:   id,
			TargetType: "doc",
			TargetRel:  note.RelativePath,
		}, true
	default:
		return v5NoteMigrationPlan{}, false
	}
}

func applyV5Plan(data map[string]any, body *string, note Note, plan v5NoteMigrationPlan) {
	date := todayISO()
	data["schema"] = "tusker." + plan.TargetType + "/v5"
	data["id"] = plan.TargetID
	data["type"] = plan.TargetType
	delete(data, "schema_version")
	if stringField(data, "created") == "" {
		data["created"] = date
	}
	if stringField(data, "updated") == "" {
		data["updated"] = date
	}
	switch plan.TargetType {
	case "epic":
		data["status"] = mapLegacyStatus(stringField(data, "status"), "epic")
		if len(normalizeList(data["doc_nodes"])) == 0 {
			data["doc_nodes"] = normalizeList(data["docs"])
		}
	case "task":
		data["kind"] = plan.TargetKind
		data["status"] = mapLegacyStatus(stringField(data, "status"), "task")
		data["epic"] = taskEpicFromID(plan.TargetID, data["epic"])
		setDefaultString(data, "priority", "p2")
		normalizeMigratedTaskPriority(data)
		setDefaultString(data, "risk", "medium")
		setDefaultString(data, "size", "m")
		setDefaultString(data, "delegation", "execute")
		setDefaultString(data, "ai_assistance", "heavy")
		if _, ok := data["ai_tools"]; !ok {
			data["ai_tools"] = []any{}
		}
		if stringField(data, "status") == "review" && stringField(data, "review_requested_at") == "" {
			data["review_requested_at"] = date
		}
		if stringField(data, "status") == "done" {
			setDefaultString(data, "closed_by", firstNonEmpty(stringField(data, "reviewed_by"), stringField(data, "verified_by"), stringField(data, "attested_by")))
			setDefaultString(data, "closed_at", firstNonEmpty(stringField(data, "reviewed_at"), stringField(data, "verified_at"), stringField(data, "attested_at"), stringField(data, "completed")))
			ensureDocsResolutionForClosedTask(data, date)
		}
		delete(data, "change_type")
		if riskNeedsKnowledgeDelta(stringField(data, "risk")) && !hasValidKnowledgeDelta(*body) {
			*body = appendMigrationKnowledgeDelta(*body)
		}
	case "doc":
		ensureV5DocFields(data, note.RelativePath)
	}
	cleanV5Frontmatter(data, plan.TargetType)
}

func cleanV5Frontmatter(data map[string]any, noteType string) {
	for _, key := range []string{
		"schema_version",
		"record_id",
		"requester",
		"epic_record_id",
		"docs_record_ids",
		"related_record_ids",
		"blocks_record_ids",
		"blocked_by_record_ids",
		"story_record_id",
		"story_record_ids",
		"attested_at",
		"attested_by",
		"attested_role",
		"signoff_at",
		"signoff_by",
		"review_state",
		"reviewed_at",
		"reviewed_by",
		"dod_code_complete",
		"dod_user_verified",
		"due",
		"prs",
		"ai_session_log",
		"surfaces",
		"target_release",
		"spec_source",
		"success_metrics",
		"docs",
		"canonical",
		"story",
	} {
		delete(data, key)
	}
	for key := range data {
		if strings.HasSuffix(key, "_record_id") || strings.HasSuffix(key, "_record_ids") {
			delete(data, key)
		}
	}

	for _, key := range []string{
		"started",
		"review_requested_at",
		"completed",
		"cancelled_at",
		"blocked_since",
		"verified_by",
		"verified_at",
		"closed_by",
		"closed_at",
		"last_verified_at",
		"published_at",
		"assignee",
	} {
		if strings.TrimSpace(stringField(data, key)) == "" {
			delete(data, key)
		}
	}

	for _, key := range []string{"tags", "related", "redirect_from", "transitions", "docs_resolution"} {
		if len(normalizeList(data[key])) == 0 && len(anySlice(data[key])) == 0 {
			delete(data, key)
		}
	}

	if intField(data, "work_revision") == 0 {
		delete(data, "work_revision")
	}

	switch noteType {
	case "epic":
		for _, key := range []string{"doc_nodes"} {
			if len(normalizeList(data[key])) == 0 {
				delete(data, key)
			}
		}
	case "task":
		if len(normalizeList(data["domains"])) == 0 {
			delete(data, "domains")
		}
		if len(normalizeList(data["doc_nodes"])) == 0 {
			delete(data, "doc_nodes")
		}
	case "doc":
		if len(normalizeList(data["domains"])) == 0 {
			delete(data, "domains")
		}
		if len(normalizeList(data["source_of_truth"])) == 0 {
			delete(data, "source_of_truth")
		}
		if len(normalizeList(data["stale_when_paths"])) == 0 {
			delete(data, "stale_when_paths")
		}
	}
}

func legacyTaskKind(data map[string]any, id string) string {
	if stringField(data, "type") == "bug" || strings.Contains(id, "-B-") {
		return "bug"
	}
	for _, key := range []string{"kind", "change_type"} {
		value := strings.ToLower(stringField(data, key))
		if _, ok := changeTypes[value]; ok {
			if value == "task" {
				return "feature"
			}
			return value
		}
	}
	return "feature"
}

func mapLegacyStatus(status, noteType string) string {
	normalized := strings.ToLower(strings.TrimSpace(status))
	switch normalized {
	case "":
		if noteType == "epic" {
			return "draft"
		}
		return "ready"
	case "intake", "todo", "open":
		if noteType == "epic" {
			return "draft"
		}
		return "ready"
	case "in_review", "review_requested", "verification_requested":
		return "review"
	case "complete", "completed", "closed":
		return "done"
	default:
		if _, ok := taskStatuses[normalized]; ok {
			return normalized
		}
		if noteType == "epic" {
			return "draft"
		}
		return "ready"
	}
}

func normalizeMigratedTaskPriority(data map[string]any) {
	priority := strings.ToLower(strings.TrimSpace(stringField(data, "priority")))
	if priority == "icebox" {
		data["priority"] = "p3"
		status := stringField(data, "status")
		if status != "done" && status != "cancelled" {
			data["status"] = "backlog"
		}
		return
	}
	if _, ok := priorities[priority]; !ok {
		data["priority"] = "p2"
		return
	}
	data["priority"] = priority
}

func taskEpicFromID(id string, current any) string {
	if target := wikiTarget(current); target != "" {
		return target
	}
	if parsed := parseID(id); parsed != nil {
		return parsed.Acronym
	}
	return ""
}

func setDefaultString(data map[string]any, key, value string) {
	if stringField(data, key) == "" {
		data[key] = value
	}
}

func ensureDocsResolutionForClosedTask(data map[string]any, date string) {
	nodes := normalizeList(data["doc_nodes"])
	if len(nodes) == 0 || docsImpactResolved(data) {
		return
	}
	existing := anySlice(data["docs_resolution"])
	resolved := map[string]struct{}{}
	for _, raw := range existing {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if status := stringValue(row["status"]); status == "applied" || status == "verified_noop" || status == "waived" {
			resolved[stringValue(row["node"])] = struct{}{}
		}
	}
	for _, node := range nodes {
		if _, ok := resolved[node]; ok {
			continue
		}
		existing = append(existing, map[string]any{
			"node":   node,
			"status": "verified_noop",
			"actor":  "tusker-migrate-v5",
			"date":   date,
			"reason": "Legacy completed item migrated with no explicit docs delta.",
		})
	}
	data["docs_resolution"] = existing
}

func appendMigrationKnowledgeDelta(body string) string {
	if findHeading(body, "## Knowledge delta") == nil {
		body = strings.TrimRight(body, "\n") + "\n\n## Knowledge delta\n\n"
	}
	table := "| Change type | Topic | Before | After | Audience | Doc nodes | Mode | Status |\n" +
		"|---|---|---|---|---|---|---|---|\n" +
		"| migration | Tusker note schema | Work item used legacy story/bug metadata with implicit documentation routing. | Work item declares V5 task metadata so docs impact can be tracked by docs-map. | developer |  | explanation | verified_noop |\n"
	return appendSectionContent(body, "## Knowledge delta", table)
}

func appendSectionContent(body, heading, content string) string {
	pos := findHeading(body, heading)
	if pos == nil {
		return strings.TrimRight(body, "\n") + "\n\n" + heading + "\n\n" + content
	}
	lines := strings.Split(body, "\n")
	insertAt := pos.NextIndex
	for insertAt > pos.Index+1 && strings.TrimSpace(lines[insertAt-1]) == "" {
		insertAt--
	}
	insert := strings.Split(strings.TrimRight(content, "\n"), "\n")
	next := append([]string{}, lines[:insertAt]...)
	next = append(next, insert...)
	next = append(next, lines[insertAt:]...)
	return strings.Join(next, "\n")
}

func ensureV5DocFields(data map[string]any, relativePath string) {
	node := firstNonEmpty(stringField(data, "node"), stringField(data, "publish_path"))
	if node == "" {
		node = strings.TrimSuffix(relativePath, ".md")
	}
	data["node"] = docsNormalizePath(node)
	setDefaultString(data, "audience", "developer")
	setDefaultString(data, "mode", "reference")
	setDefaultString(data, "agent_layer", "none")
	setDefaultString(data, "kind", "reference")
	if strings.HasPrefix(docsNormalizePath(relativePath), "docs/") && stringField(data, "kind") == "canon" && stringField(data, "owner_epic") == "" {
		data["kind"] = "reference"
	}
	if stringField(data, "canonical_status") == "" {
		if boolField(data, "canonical") {
			data["canonical_status"] = "approved"
		} else {
			data["canonical_status"] = "draft"
		}
	}
	if _, ok := data["publish"]; !ok {
		data["publish"] = true
	}
	if boolField(data, "publish") {
		setDefaultString(data, "publish_lane", "developer")
		setDefaultString(data, "publish_path", stringField(data, "node"))
		setDefaultString(data, "publish_description", fallback(stringField(data, "title"), stringField(data, "node"))+".")
	}
	if _, ok := data["redirect_from"]; !ok {
		data["redirect_from"] = []any{}
	}
}

func replaceLegacyWorkRefsInMap(data map[string]any, idMap map[string]string) map[string]any {
	out := map[string]any{}
	for key, value := range data {
		out[key] = replaceLegacyWorkRefsInAny(value, idMap)
	}
	return out
}

func replaceLegacyWorkRefsInAny(value any, idMap map[string]string) any {
	switch current := value.(type) {
	case string:
		return replaceLegacyWorkLinks(current, idMap)
	case []any:
		out := make([]any, 0, len(current))
		for _, item := range current {
			out = append(out, replaceLegacyWorkRefsInAny(item, idMap))
		}
		return out
	case []string:
		out := make([]string, 0, len(current))
		for _, item := range current {
			out = append(out, replaceLegacyWorkLinks(item, idMap))
		}
		return out
	case map[string]any:
		out := map[string]any{}
		for key, item := range current {
			out[key] = replaceLegacyWorkRefsInAny(item, idMap)
		}
		return out
	default:
		return value
	}
}

func replaceLegacyWorkLinks(text string, idMap map[string]string) string {
	out := text
	for oldID, newID := range idMap {
		if oldID == newID {
			continue
		}
		re := regexp.MustCompile(`\[\[` + regexp.QuoteMeta(oldID) + `([|#\]])`)
		out = re.ReplaceAllString(out, "[["+newID+"$1")
	}
	return out
}

func replaceOwnHeadingID(body, oldID, newID string) string {
	for _, marker := range []string{"# " + oldID + " ·", "# " + oldID + " -", "# " + oldID + ":"} {
		if strings.Contains(body, marker) {
			return strings.Replace(body, marker, strings.Replace(marker, oldID, newID, 1), 1)
		}
	}
	return body
}

func ensureDocsMapIncludesPublishedDocs(vaultPath string) (int, error) {
	docsMap, err := loadDocsMap(vaultPath)
	if err != nil {
		return 0, err
	}
	if docsMap == nil {
		docsMap = &DocsMap{Schema: "tusker.docs-map/v5", Generated: todayISO(), Domains: map[string]DocsMapDomain{}}
	}
	if docsMap.Domains == nil {
		docsMap.Domains = map[string]DocsMapDomain{}
	}
	seen := map[string]struct{}{}
	for _, node := range docsMap.Nodes {
		seen[node.ID] = struct{}{}
	}
	notes, err := listAllNotes(vaultPath)
	if err != nil {
		return 0, err
	}
	added := 0
	for _, note := range notes {
		if stringField(note.Data, "type") != "doc" || !boolField(note.Data, "publish") {
			continue
		}
		nodeID := stringField(note.Data, "node")
		if nodeID == "" {
			continue
		}
		if _, ok := seen[nodeID]; ok {
			continue
		}
		domain := docsMapDomainForMigratedDoc(note)
		if _, ok := docsMap.Domains[domain]; !ok {
			docsMap.Domains[domain] = DocsMapDomain{
				Label:       strings.ToUpper(domain),
				OwnerEpic:   wikiTarget(firstNonEmpty(stringField(note.Data, "owner_epic"), stringField(note.Data, "epic"), stringField(note.Data, "canon_for"))),
				Description: "Migrated project documentation domain.",
			}
		}
		docsMap.Nodes = append(docsMap.Nodes, DocsMapNode{
			ID:                 nodeID,
			Title:              fallback(stringField(note.Data, "title"), nodeID),
			Page:               note.RelativePath,
			Domain:             domain,
			Mode:               fallback(stringField(note.Data, "mode"), "reference"),
			AgentLayer:         fallback(stringField(note.Data, "agent_layer"), "none"),
			Audience:           fallback(stringField(note.Data, "audience"), "developer"),
			Kind:               fallback(stringField(note.Data, "kind"), "reference"),
			SourceOfTruth:      firstNonEmptyList(normalizeList(note.Data["source_of_truth"]), []string{note.RelativePath}),
			StaleWhen:          DocsMapStaleWhen{Paths: firstNonEmptyList(normalizeList(note.Data["stale_when_paths"]), []string{note.RelativePath})},
			PublishLane:        fallback(stringField(note.Data, "publish_lane"), "developer"),
			PublishPath:        fallback(stringField(note.Data, "publish_path"), nodeID),
			PublishDescription: fallback(stringField(note.Data, "publish_description"), fallback(stringField(note.Data, "title"), nodeID)+"."),
		})
		seen[nodeID] = struct{}{}
		added++
	}
	if added == 0 {
		return 0, nil
	}
	docsMap.Generated = todayISO()
	raw, err := yamlMarshal(docsMap)
	if err != nil {
		return 0, err
	}
	if err := writeText(filepath.Join(vaultPath, docsMapRelative), raw); err != nil {
		return 0, err
	}
	return added, nil
}

func docsMapDomainForMigratedDoc(note Note) string {
	for _, value := range normalizeList(note.Data["domains"]) {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			return value
		}
	}
	for _, key := range []string{"owner_epic", "epic", "canon_for"} {
		value := strings.ToLower(wikiTarget(note.Data[key]))
		if value != "" {
			return value
		}
	}
	return "docs"
}

func yamlMarshal(value any) (string, error) {
	raw, err := yaml.Marshal(value)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(raw)) + "\n", nil
}

func backupVaultForV5Migration(vaultPath string) (string, error) {
	parent := filepath.Dir(vaultPath)
	base := filepath.Base(vaultPath) + ".backup-v5-" + time.Now().UTC().Format("20060102-150405")
	target := filepath.Join(parent, base)
	for suffix := 2; fileExists(target); suffix++ {
		target = filepath.Join(parent, fmt.Sprintf("%s-%d", base, suffix))
	}
	if err := copyTreeForV5Backup(vaultPath, target); err != nil {
		return "", err
	}
	return target, nil
}

func copyTreeForV5Backup(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return ensureDir(target)
		}
		if entry.IsDir() && skipV5BackupDir(rel) {
			return fs.SkipDir
		}
		dst := filepath.Join(target, filepath.FromSlash(rel))
		if entry.IsDir() {
			return ensureDir(dst)
		}
		return copyFile(path, dst)
	})
}

func skipV5BackupDir(rel string) bool {
	for _, skip := range []string{
		"_system/workspaces",
		"_system/runs",
		"_system/events",
		"_system/logs",
		"_system/archive",
	} {
		if rel == skip || strings.HasPrefix(rel, skip+"/") {
			return true
		}
	}
	return false
}
