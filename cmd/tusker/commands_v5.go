package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

func newV5Epic(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	acronym := strings.ToUpper(args.String("acronym"))
	if acronym == "" {
		return tuskerError(errorMissingArg, "Missing required argument --acronym")
	}
	if !epicAcronymPattern.MatchString(acronym) {
		return tuskerError(errorInvalidArg, fmt.Sprintf(`--acronym must be 3 uppercase letters, got "%s"`, args.String("acronym")))
	}
	title, err := requireArg(args, "title")
	if err != nil {
		return err
	}
	date := todayISO()
	epicDir := filepath.Join(vaultPath, "epics", acronym)
	path := filepath.Join(epicDir, acronym+".md")
	if fileExists(path) || fileExists(filepath.Join(epicDir, "index.md")) {
		return tuskerError(errorAlreadyExists, "Epic already exists: "+acronym)
	}
	if err := ensureDir(epicDir); err != nil {
		return err
	}
	template := defaultV5EpicTemplate()
	rendered := replaceTemplateTokens(template, map[string]string{
		"{{acronym}}": acronym,
		"{{title}}":   title,
		"{{owner}}":   fallback(args.String("owner"), defaultActorName()),
		"{{summary}}": args.String("summary"),
		"{{date}}":    date,
	})
	data, body, err := parseFrontmatter(rendered)
	if err != nil {
		return err
	}
	data["schema"] = "tusker.epic/v5"
	data["id"] = acronym
	data["title"] = title
	data["type"] = "epic"
	data["status"] = fallback(args.String("status"), "draft")
	data["owner"] = fallback(args.String("owner"), defaultActorName())
	data["summary"] = args.String("summary")
	data["doc_nodes"] = splitCSV(args.String("doc-nodes"))
	data["created"] = date
	data["updated"] = date
	content, err := serializeDocument(data, body, frontmatterOrderForType("epic"))
	if err != nil {
		return err
	}
	if err := writeText(path, content); err != nil {
		return err
	}
	if !args.Bool("quiet") {
		fmt.Printf("Created epic %s at %s\n", acronym, path)
	}
	autoReindex(vaultPath)
	return nil
}

func newV5Task(args Args, defaultKind string) error {
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
	kind := fallback(args.String("kind"), defaultKind)
	if kind == "task" {
		kind = "feature"
	}
	if _, ok := changeTypes[kind]; !ok {
		return tuskerError(errorInvalidField, "Invalid kind: "+kind, withContext(map[string]any{"field": "kind"}))
	}
	size := strings.ToLower(fallback(args.String("size"), "m"))
	risk := strings.ToLower(fallback(args.String("risk"), "medium"))
	priority := strings.ToLower(fallback(args.String("priority"), "p2"))
	delegation := strings.ToLower(fallback(args.String("delegation"), "execute"))
	aiAssistance := strings.ToLower(fallback(args.String("ai-assistance"), "heavy"))
	status := strings.ToLower(fallback(args.String("status"), "draft"))
	if _, ok := sizes[size]; !ok {
		return tuskerError(errorInvalidField, "Invalid size: "+size)
	}
	if _, ok := risks[risk]; !ok {
		return tuskerError(errorInvalidField, "Invalid risk: "+risk)
	}
	if _, ok := priorities[priority]; !ok {
		return tuskerError(errorInvalidField, "Invalid priority: "+priority)
	}
	if _, ok := taskStatuses[status]; !ok {
		return tuskerError(errorInvalidField, "Invalid status: "+status)
	}
	if _, ok := delegations[delegation]; !ok {
		return tuskerError(errorInvalidField, "Invalid delegation: "+delegation)
	}
	if _, ok := aiAssistanceLevels[aiAssistance]; !ok {
		return tuskerError(errorInvalidField, "Invalid ai-assistance: "+aiAssistance)
	}
	epicDir := filepath.Join(vaultPath, "epics", acronym)
	if !fileExists(filepath.Join(epicDir, acronym+".md")) && !fileExists(filepath.Join(epicDir, "index.md")) {
		return tuskerError(errorNotFound, fmt.Sprintf("Epic %s does not exist. Run `tusker new epic` first.", acronym))
	}
	notes, err := listAllNotes(vaultPath)
	if err != nil {
		return err
	}
	id := fmt.Sprintf("%s-T-%s", acronym, padNumber(nextSequence(notes, acronym, "task")))
	filePath := filepath.Join(epicDir, id+".md")
	date := todayISO()
	rendered := defaultV5TaskDocument(id, title, kind, acronym, risk, size, priority, date)
	data, body, err := parseFrontmatter(rendered)
	if err != nil {
		return err
	}
	data["schema"] = "tusker.task/v5"
	data["id"] = id
	data["title"] = title
	data["type"] = "task"
	data["kind"] = kind
	data["epic"] = acronym
	data["status"] = status
	data["priority"] = priority
	data["risk"] = risk
	data["size"] = size
	data["delegation"] = delegation
	data["ai_assistance"] = aiAssistance
	data["ai_tools"] = splitCSV(args.String("ai-tools"))
	data["assignee"] = args.String("assignee")
	data["domains"] = splitCSV(args.String("domains"))
	data["doc_nodes"] = splitCSV(args.String("doc-nodes"))
	data["blocked_by"] = splitCSV(args.String("blocked-by"))
	data["block_reason"] = args.String("block-reason")
	if status == "blocked" && len(normalizeList(data["blocked_by"])) == 0 && strings.TrimSpace(stringField(data, "block_reason")) == "" {
		return tuskerError(errorInvalidTransition, "blocked task requires --blocked-by or --block-reason", withHint("use --blocked-by for Tusker dependencies or --block-reason for an external blocker"))
	}
	data["blocks"] = splitCSV(args.String("blocks"))
	data["created"] = date
	data["updated"] = date
	content, err := serializeDocument(data, body, frontmatterOrderForType("task"))
	if err != nil {
		return err
	}
	if err := writeText(filePath, content); err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "id": id, "type": "task", "kind": kind, "path": filePath})
	} else if !args.Bool("quiet") {
		fmt.Printf("Created task %s at %s\n", id, filePath)
	}
	autoReindex(vaultPath)
	return nil
}

func newV5Doc(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	title, err := requireArg(args, "title")
	if err != nil {
		return err
	}
	node := strings.Trim(strings.TrimSpace(firstNonEmpty(args.String("node"), args.String("publish-path"))), "/")
	if node == "" {
		return tuskerError(errorMissingArg, "Missing required argument --node")
	}
	if reason := validateDocNodePath(node); reason != "" {
		return tuskerError(errorInvalidArg, fmt.Sprintf(`invalid --node "%s": %s`, node, reason), withContext(map[string]any{"field": "node", "value": node}))
	}
	docsMap, err := loadDocsMap(vaultPath)
	if err != nil {
		return err
	}
	mappedNode, hasMappedNode := DocsMapNode{}, false
	if docsMap != nil {
		mappedNode, hasMappedNode = docsMap.Node(node)
	}
	mode := strings.ToLower(fallback(args.String("mode"), mappedNode.EffectiveMode()))
	agentLayer := strings.ToLower(fallback(args.String("agent-layer"), mappedNode.EffectiveAgentLayer()))
	audience := strings.ToLower(fallback(args.String("audience"), mappedNode.Audience))
	if audience == "" {
		audience = "developer"
	}
	if mode == "" {
		mode = "reference"
	}
	if agentLayer == "" {
		agentLayer = "none"
	}
	if _, ok := docModes[mode]; !ok {
		return tuskerError(errorInvalidField, "Invalid mode: "+mode, withContext(map[string]any{"field": "mode"}))
	}
	if _, ok := docAudiences[audience]; !ok {
		return tuskerError(errorInvalidField, "Invalid audience: "+audience, withContext(map[string]any{"field": "audience"}))
	}
	if _, ok := docAgentLayers[agentLayer]; !ok {
		return tuskerError(errorInvalidField, "Invalid agent-layer: "+agentLayer, withContext(map[string]any{"field": "agent_layer"}))
	}
	date := todayISO()
	publishPath := firstNonEmpty(args.String("publish-path"), mappedNode.PublishPath, node)
	publishDescription := firstNonEmpty(args.String("publish-description"), mappedNode.PublishDescription, title+".")
	relativeDocPath := filepath.ToSlash(filepath.Join("docs", filepath.FromSlash(node)+".md"))
	if hasMappedNode && mappedNode.SourcePath() != "" {
		relativeDocPath = mappedNode.SourcePath()
	}
	filePath := filepath.Join(vaultPath, filepath.FromSlash(relativeDocPath))
	if fileExists(filePath) {
		return tuskerError(errorAlreadyExists, "Doc already exists: "+filePath)
	}
	template := defaultV5DocTemplate()
	if audience == "agent" || agentLayer == "standalone" {
		template = defaultV5AgentDocTemplate()
	} else if agentLayer == "capsule" {
		template = strings.Replace(template, "\n## Content\n", "\n## Agent capsule\n\n- Agent-facing notes, caveats, and automation cues for this page.\n\n## Content\n", 1)
	}
	rendered := replaceTemplateTokens(template, map[string]string{
		"{{node}}":                node,
		"{{title}}":               title,
		"{{publish_path}}":        publishPath,
		"{{publish_description}}": publishDescription,
		"{{date}}":                date,
	})
	data, body, err := parseFrontmatter(rendered)
	if err != nil {
		return err
	}
	data["schema"] = "tusker.doc/v5"
	data["id"] = node
	data["title"] = title
	data["type"] = "doc"
	data["node"] = node
	data["audience"] = audience
	data["mode"] = mode
	data["agent_layer"] = agentLayer
	data["kind"] = fallback(args.String("kind"), "reference")
	if data["kind"] == "reference" && mode != "reference" {
		data["kind"] = mode
	}
	data["domains"] = firstNonEmptyList(splitCSV(args.String("domains")), splitCSV(mappedNode.Domain))
	data["source_of_truth"] = firstNonEmptyList(splitCSV(args.String("source-of-truth")), mappedNode.SourceOfTruth)
	data["stale_when_paths"] = firstNonEmptyList(splitCSV(args.String("stale-when")), mappedNode.StaleWhen.Paths)
	data["canonical_status"] = fallback(args.String("canonical-status"), "draft")
	data["last_verified_at"] = args.String("last-verified-at")
	data["publish"] = !args.Bool("no-publish")
	data["publish_lane"] = fallback(args.String("publish-lane"), fallback(mappedNode.PublishLane, "internal"))
	data["publish_path"] = publishPath
	data["publish_description"] = publishDescription
	data["redirect_from"] = splitCSV(args.String("redirect-from"))
	data["created"] = date
	data["updated"] = date
	content, err := serializeDocument(data, body, frontmatterOrderForType("doc"))
	if err != nil {
		return err
	}
	if err := writeText(filePath, content); err != nil {
		return err
	}
	if !args.Bool("quiet") {
		fmt.Printf("Created doc %s at %s\n", node, filePath)
	}
	autoReindex(vaultPath)
	return nil
}

func statusCmd(args Args) error {
	id := firstNonEmpty(args.String("id"), args.String("_pos0"))
	status := firstNonEmpty(args.String("status"), args.String("_pos1"))
	args["id"] = id
	args["status"] = status
	if id != "" {
		if _, err := requireV5NoteForCommand(args, id, "status", "epic", "task", "doc"); err != nil {
			return err
		}
	}
	return setStatus(args)
}

func evidenceCmd(args Args) error {
	args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
	args["kind"] = firstNonEmpty(args.String("kind"), args.String("_pos1"))
	args["path"] = firstNonEmpty(args.String("path"), args.String("_pos2"))
	if id := args.String("id"); id != "" {
		if _, err := requireV5NoteForCommand(args, id, "evidence", "task"); err != nil {
			return err
		}
	}
	return attachEvidence(args)
}

func verifyCmd(args Args) error {
	args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
	if args.String("by") == "" {
		args["by"] = fallback(args.String("actor"), "automation")
	}
	return verifyV5Cmd(args)
}

func verifyV5Cmd(args Args) error {
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
	if isV6Schema(note.Data) {
		return verifyV6TaskCmd(vaultPath, note, args)
	}
	if stringField(note.Data, "type") != "task" {
		return tuskerError(errorInvalidArg, fmt.Sprintf(`verify only supports V5 tasks, got "%s"`, stringField(note.Data, "type")), withContext(map[string]any{"id": id, "type": stringField(note.Data, "type")}))
	}
	if !isV5Schema(note.Data) {
		return tuskerError(errorInvalidArg, fmt.Sprintf(`verify only supports V5 notes, got schema "%s"`, stringField(note.Data, "schema")), withContext(map[string]any{"id": id, "schema": stringField(note.Data, "schema")}))
	}
	if stringField(note.Data, "status") != "review" {
		return tuskerError(errorInvalidTransition, fmt.Sprintf(`%s: verify requires status "review"`, id), withContext(map[string]any{"id": id, "status": stringField(note.Data, "status")}))
	}
	by := fallback(args.String("by"), "automation")
	data, body, err := parseFrontmatterMustRead(note.AbsolutePath)
	if err != nil {
		return err
	}
	if err := ensureReviewerMayVerify(vaultPath, data, by); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	date := todayISO()
	data["verified_by"] = by
	data["verified_at"] = now
	if summary := strings.TrimSpace(args.String("summary")); summary != "" {
		data["verification_summary"] = summary
	}
	data["updated"] = date
	body = appendSectionBullet(body, "## Verification log", fmt.Sprintf("- %s — %s — verified%s", date, by, suffixReason(args.String("summary"))), false)
	content, err := serializeDocument(data, body, frontmatterOrderForType("task"))
	if err != nil {
		return err
	}
	if err := writeText(note.AbsolutePath, content); err != nil {
		return err
	}
	autoReindex(vaultPath)
	if !args.Bool("quiet") {
		fmt.Printf("%s verified by %s\n", id, by)
	}
	return nil
}

func closeV5Cmd(args Args) error {
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
	if isV6Schema(note.Data) {
		return closeV6TaskCmd(vaultPath, note, args)
	}
	if stringField(note.Data, "type") != "task" {
		return tuskerError(errorInvalidArg, fmt.Sprintf(`close only supports V5 tasks, got "%s"`, stringField(note.Data, "type")), withContext(map[string]any{"id": id, "type": stringField(note.Data, "type")}))
	}
	if !isV5Schema(note.Data) {
		return tuskerError(errorInvalidArg, fmt.Sprintf(`close only supports V5 notes, got schema "%s"`, stringField(note.Data, "schema")), withContext(map[string]any{"id": id, "schema": stringField(note.Data, "schema")}))
	}
	data, body, err := parseFrontmatterMustRead(note.AbsolutePath)
	if err != nil {
		return err
	}
	if stringField(data, "status") != "review" {
		return tuskerError(errorInvalidTransition, fmt.Sprintf(`%s: close requires status "review"`, id), withHint("run `tusker status "+id+" review` before verification and close"))
	}
	if stringField(data, "verified_at") == "" {
		return tuskerError(errorInvalidTransition, id+": close requires verification first", withHint("run `tusker verify "+id+" --by <name>`"))
	}
	if len(normalizeList(data["doc_nodes"])) > 0 && !docsImpactResolved(data) {
		return tuskerError(errorDocsImpactUnresolved, id+": docs impact is unresolved", withHint("run `tusker docs check "+id+"`, then apply or waive each node"))
	}
	if len(normalizeList(data["knowledge_nodes"])) > 0 {
		if hasV6Vault(vaultPath) {
			index, err := v6IndexVault(vaultPath)
			if err != nil {
				return err
			}
			if issues := knowledgeImpactFreshnessIssues(data, v6FreshnessByNode(index)); len(issues) > 0 {
				return tuskerError(errorDocsImpactUnresolved, id+": knowledge impact is stale or unresolved: "+strings.Join(issues, "; "), withHint("run `tusker knowledge check "+id+"`, then apply, noop, or waive each node with current sources"))
			}
		} else if !knowledgeImpactResolved(data) {
			return tuskerError(errorDocsImpactUnresolved, id+": knowledge impact is unresolved", withHint("run `tusker knowledge check "+id+"`, then apply, noop, or waive each node"))
		}
	}
	date := todayISO()
	now := time.Now().UTC().Format(time.RFC3339)
	actor := fallback(fallback(args.String("actor"), args.String("by")), "automation")
	if err := ensureReviewerMayClose(vaultPath, data, actor); err != nil {
		return err
	}
	reviewedBy := firstNonEmpty(stringField(data, "verified_by"), "unknown")
	data["status"] = "done"
	data["closed_by"] = actor
	data["closed_at"] = now
	if reason := strings.TrimSpace(args.String("reason")); reason != "" {
		data["close_summary"] = reason
	}
	data["completed"] = date
	data["updated"] = date
	body = appendWorkLogBullet(body, fmt.Sprintf("%s — %s — closed after review by %s%s", date, actor, reviewedBy, suffixReason(args.String("reason"))))
	content, err := serializeDocument(data, body, frontmatterOrderForType("task"))
	if err != nil {
		return err
	}
	if err := writeText(note.AbsolutePath, content); err != nil {
		return err
	}
	autoReindex(vaultPath)
	if !args.Bool("quiet") {
		fmt.Printf("%s closed (reviewed by %s, closed by %s)\n", id, reviewedBy, actor)
	}
	return nil
}

func ensureReviewerMayVerify(vaultPath string, data map[string]any, actor string) error {
	policy, ok := loadReviewerPolicyForGate(vaultPath)
	if !ok || !policy.Enabled || strings.TrimSpace(actor) != strings.TrimSpace(policy.Actor) {
		return nil
	}
	risk := stringField(data, "risk")
	if reviewerRequiresHumanRisk(policy, risk) {
		return tuskerError(errorInvalidTransition, fmt.Sprintf("%s risk requires human verification; configured reviewer %s may only advise", risk, actor))
	}
	if !reviewerMayAutoCloseRisk(policy, risk) {
		return tuskerError(errorInvalidTransition, fmt.Sprintf("%s risk is not in reviewer.auto_close_risks for %s", risk, actor))
	}
	return nil
}

func ensureReviewerMayClose(vaultPath string, data map[string]any, actor string) error {
	policy, ok := loadReviewerPolicyForGate(vaultPath)
	if !ok || !policy.Enabled || strings.TrimSpace(actor) != strings.TrimSpace(policy.Actor) {
		return nil
	}
	risk := stringField(data, "risk")
	if reviewerRequiresHumanRisk(policy, risk) {
		return tuskerError(errorInvalidTransition, fmt.Sprintf("%s risk requires human close; configured reviewer %s may only advise", risk, actor))
	}
	if !reviewerMayAutoCloseRisk(policy, risk) {
		return tuskerError(errorInvalidTransition, fmt.Sprintf("%s risk is not in reviewer.auto_close_risks for %s", risk, actor))
	}
	return nil
}

func loadReviewerPolicyForGate(vaultPath string) (ReviewerPolicy, bool) {
	wfFile, err := loadWorkflow(vaultPath)
	if err != nil {
		return ReviewerPolicy{}, false
	}
	return wfFile.Data.Reviewer, true
}

func requireV5NoteForCommand(args Args, id, command string, allowedTypes ...string) (*Note, error) {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return nil, err
	}
	note, err := resolveNote(vaultPath, id)
	if err != nil {
		return nil, err
	}
	if !isV5Schema(note.Data) {
		return nil, tuskerError(errorInvalidArg, fmt.Sprintf(`%s only supports V5 notes, got schema "%s"`, command, stringField(note.Data, "schema")), withContext(map[string]any{"id": id, "schema": stringField(note.Data, "schema")}))
	}
	if len(allowedTypes) > 0 {
		noteType := stringField(note.Data, "type")
		for _, allowed := range allowedTypes {
			if noteType == allowed {
				return &note, nil
			}
		}
		return nil, tuskerError(errorInvalidArg, fmt.Sprintf(`%s only supports %s, got "%s"`, command, strings.Join(allowedTypes, "/"), noteType), withContext(map[string]any{"id": id, "type": noteType}))
	}
	return &note, nil
}

func isV5Schema(data map[string]any) bool {
	return strings.HasSuffix(stringField(data, "schema"), "/v5")
}
