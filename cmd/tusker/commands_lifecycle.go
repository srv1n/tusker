package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

func setStatus(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	id, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	nextStatus, err := requireArg(args, "status")
	if err != nil {
		return err
	}
	actor := fallback(fallback(args.String("actor"), args.String("by")), "automation")
	reason := args.String("reason")
	note, err := resolveNote(vaultPath, id)
	if err != nil {
		return err
	}
	noteType := stringField(note.Data, "type")
	if isV6Schema(note.Data) {
		noteType = effectiveNoteKind(note.Data)
	}
	var statusSet map[string]struct{}
	switch noteType {
	case "epic", "task":
		statusSet = taskStatuses
	case "doc":
		statusSet = docStatuses
	default:
		return tuskerError(errorInvalidArg, fmt.Sprintf(`status only supports V5 epic/task/doc notes, got "%s"`, noteType), withContext(map[string]any{"id": id, "type": noteType}))
	}
	if _, ok := statusSet[nextStatus]; !ok {
		return tuskerError(errorInvalidField, fmt.Sprintf("invalid %s status: %s", noteType, nextStatus), withContext(map[string]any{"field": "status", "value": nextStatus}))
	}
	data, body, err := parseFrontmatterMustRead(note.AbsolutePath)
	if err != nil {
		return err
	}
	prev := stringField(data, "status")
	date := todayISO()
	now := time.Now().UTC().Format(time.RFC3339)
	blockerMetadataChanged := false
	if noteType == "task" {
		if blockedByArg := args.String("blocked-by"); blockedByArg != "" {
			data["blocked_by"] = splitCSVLinks(blockedByArg)
			blockerMetadataChanged = true
		}
		if nextStatus == "blocked" {
			blockReason := strings.TrimSpace(firstNonEmpty(args.String("block-reason"), args.String("reason")))
			if blockReason != "" {
				data["block_reason"] = blockReason
				blockerMetadataChanged = true
			}
			if len(normalizeList(data["blocked_by"])) == 0 && strings.TrimSpace(stringField(data, "block_reason")) == "" {
				return tuskerError(errorInvalidTransition, id+": blocked requires blocked_by or block_reason", withHint("use `--blocked-by <TASK-ID>` for Tusker dependencies or `--block-reason <text>` for an external blocker"))
			}
		} else if prev == "blocked" && strings.TrimSpace(stringField(data, "block_reason")) != "" {
			data["block_reason"] = ""
			blockerMetadataChanged = true
		}
	}
	if prev == nextStatus && !blockerMetadataChanged {
		if args.Bool("json") {
			emitJSON(map[string]any{"ok": true, "id": id, "status": nextStatus, "unchanged": true})
		} else if !args.Bool("quiet") {
			fmt.Printf("%s already at status %q\n", id, nextStatus)
		}
		return nil
	}
	if noteType == "task" && nextStatus == "done" {
		if stringField(data, "verified_at") == "" {
			return tuskerError(errorInvalidTransition, id+": done requires verification first", withHint("run `tusker verify "+id+" --by <name>`"))
		}
		if isV6Schema(data) {
			if issues := v6TaskProofIssues(data, body); len(issues) > 0 {
				return tuskerError(errorEvidenceGate, id+": proof is incomplete: "+strings.Join(issues, "; "), withHint("fill Acceptance, Evidence, and Verification log before done"))
			}
		}
		if len(normalizeList(data["doc_nodes"])) > 0 && !docsImpactResolved(data) {
			return tuskerError(errorDocsImpactUnresolved, id+": docs impact is unresolved", withHint("run `tusker docs check "+id+"`, then apply or waive each node"))
		}
		if len(normalizeList(data["knowledge_nodes"])) > 0 {
			if isV6Schema(data) {
				index, err := v6IndexVault(vaultPath)
				if err != nil {
					return err
				}
				if issues := knowledgeImpactFreshnessIssues(data, v6FreshnessByNode(index)); len(issues) > 0 {
					return tuskerError(errorDocsImpactUnresolved, id+": knowledge impact is stale or unresolved: "+strings.Join(issues, "; "), withHint("run `tusker knowledge check "+id+"`, then apply, noop, or waive each node with current sources"))
				}
			} else if hasV6Vault(vaultPath) {
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
	}
	if noteType == "epic" && nextStatus == "done" {
		allNotes, err := listAllNotes(vaultPath)
		if err != nil {
			return err
		}
		var unfinished []string
		for _, current := range allNotes {
			if stringField(current.Data, "type") != "task" {
				continue
			}
			if wikiTarget(current.Data["epic"]) != stringField(data, "id") {
				continue
			}
			status := stringField(current.Data, "status")
			if status != "done" && status != "cancelled" {
				unfinished = append(unfinished, stringField(current.Data, "id"))
			}
		}
		if len(unfinished) > 0 {
			return tuskerError(errorChildrenUnfinished, fmt.Sprintf("Epic %s has %d unfinished child(ren): %s", id, len(unfinished), strings.Join(unfinished, ", ")), withContext(map[string]any{"unfinished": unfinished}))
		}
	}
	data["status"] = nextStatus
	if isV6Schema(data) {
		data["updated_at"] = date
	} else {
		data["updated"] = date
	}
	if field := statusTransitionDateFields[nextStatus]; field != "" {
		if !(field == "started" && stringField(data, "started") != "") {
			data[field] = date
		}
	}
	if noteType == "doc" && nextStatus == "published" && stringField(data, "published_at") == "" {
		data["published_at"] = date
	}
	transitionKind := "status"
	if prev == nextStatus && blockerMetadataChanged {
		transitionKind = "blocker"
	}
	appendTransition(data, orderedTransition(now, transitionKind, prev, nextStatus, actor, reason))
	body = appendWorkLogBullet(body, fmt.Sprintf("%s — %s — status: %s → %s%s", date, actor, fallback(prev, "(unset)"), nextStatus, suffixReason(reason)))
	order := frontmatterOrderForType(noteType)
	if isV6Schema(data) {
		order = v6FrontmatterOrder[noteType]
	}
	content, err := serializeDocument(data, body, order)
	if err != nil {
		return err
	}
	if err := writeText(note.AbsolutePath, content); err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "id": id, "from": nilIfEmpty(prev), "to": nextStatus})
	} else if !args.Bool("quiet") {
		fmt.Printf("%s: %s → %s\n", id, fallback(prev, "(unset)"), nextStatus)
	}
	autoReindex(vaultPath)
	return nil
}

func attachEvidence(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	id, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	kind, err := requireArg(args, "kind")
	if err != nil {
		return err
	}
	inputPath, err := requireArg(args, "path")
	if err != nil {
		return err
	}
	noteRef, err := resolveNote(vaultPath, id)
	if err != nil {
		return err
	}
	if stringField(noteRef.Data, "type") != "task" || !isV5Schema(noteRef.Data) {
		return tuskerError(errorInvalidArg, fmt.Sprintf(`evidence only supports V5 tasks, got type "%s" schema "%s"`, stringField(noteRef.Data, "type"), stringField(noteRef.Data, "schema")), withContext(map[string]any{"id": id}))
	}
	noteText := args.String("note")
	date := todayISO()
	data, body, err := parseFrontmatterMustRead(noteRef.AbsolutePath)
	if err != nil {
		return err
	}
	link := inputPath
	if !strings.HasPrefix(inputPath, "http://") && !strings.HasPrefix(inputPath, "https://") {
		resolved, err := filepath.Abs(inputPath)
		if err != nil {
			return err
		}
		if !fileExists(resolved) {
			return tuskerError(errorNotFound, "File not found: "+resolved, withPath(resolved))
		}
		attachmentDir := filepath.Join(vaultPath, "Attachments", id)
		if err := ensureDir(attachmentDir); err != nil {
			return err
		}
		target := filepath.Join(attachmentDir, filepath.Base(resolved))
		if err := copyFile(resolved, target); err != nil {
			return err
		}
		link = filepath.ToSlash(filepath.Join("Attachments", id, filepath.Base(resolved)))
	}
	body = appendSectionBullet(body, "## Evidence", buildEvidenceBullet(kind, link, noteText, date), false)
	body = appendWorkLogBullet(body, fmt.Sprintf("%s — automation — attached %s: %s%s", date, kind, link, suffixReason(noteText)))
	data["updated"] = date
	content, err := serializeDocument(data, body, frontmatterOrderForType("task"))
	if err != nil {
		return err
	}
	if err := writeText(noteRef.AbsolutePath, content); err != nil {
		return err
	}
	if !args.Bool("quiet") {
		fmt.Printf("Attached %s to %s: %s\n", kind, id, link)
	}
	return nil
}

func assertEvidenceGate(data map[string]any, body, id string) error {
	risk := strings.ToLower(stringField(data, "risk"))
	if risk == "medium" || risk == "high" || risk == "critical" {
		if !sectionHasSubstance(body, "## Evidence") {
			return tuskerError(errorEvidenceGate, fmt.Sprintf(`%s: risk "%s" requires substantive "## Evidence" before this transition`, id, risk), withContext(map[string]any{"id": id, "risk": risk}))
		}
	}
	if stringField(data, "type") == "task" && stringField(data, "kind") == "feature" && isUISurface(data["surfaces"]) && (risk == "medium" || risk == "high" || risk == "critical") {
		if !evidenceHasAsset(body) {
			return tuskerError(errorUIDemoMissing, fmt.Sprintf(`%s: UI feature at risk "%s" needs a demo asset (video/gif/screenshot) in "## Evidence"`, id, risk), withContext(map[string]any{"id": id, "risk": risk}))
		}
	}
	return nil
}

func buildEvidenceBullet(kind, link, note, date string) string {
	noteText := ""
	if note != "" {
		noteText = " — " + note
	}
	if kind == "screenshot" || kind == "video" {
		if strings.HasPrefix(link, "http://") || strings.HasPrefix(link, "https://") {
			return fmt.Sprintf("- %s — %s: [view](%s)%s", date, kind, link, noteText)
		}
		return fmt.Sprintf("- %s — %s: ![[%s]]%s", date, kind, link, noteText)
	}
	if kind == "pr" {
		return fmt.Sprintf("- %s — PR: %s%s", date, link, noteText)
	}
	if strings.HasPrefix(link, "http://") || strings.HasPrefix(link, "https://") {
		return fmt.Sprintf("- %s — %s: [link](%s)%s", date, kind, link, noteText)
	}
	return fmt.Sprintf("- %s — %s: [[%s]]%s", date, kind, link, noteText)
}
