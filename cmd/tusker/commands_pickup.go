package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type noteLookup struct {
	ByID       map[string]Note
	ByRecordID map[string]Note
}

type claimResult struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	From     string `json:"from"`
	To       string `json:"to"`
	Assignee string `json:"assignee"`
	Path     string `json:"path"`
}

func claimCmd(args Args) error {
	args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	id, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	if note, err := resolveV7Note(vaultPath, id, "task"); err == nil && strings.HasSuffix(stringField(note.Data, "schema"), "/v7") {
		if args.String("owner") == "" && args.String("as") != "" {
			args["owner"] = args.String("as")
		}
		if args.String("owner") == "" && args.String("actor") != "" {
			args["owner"] = args.String("actor")
		}
		if args.String("owner") == "" {
			args["owner"] = "agent:" + defaultActorName()
		}
		if _, err := writeV7Lease(args, "active"); err != nil {
			return err
		}
		_ = emitV7Event(vaultPath, id, "task", "claimed", fallback(args.String("owner"), "agent:"+defaultActorName()), map[string]any{"branch": currentGitBranch()})
		return nil
	}
	actor, err := requireClaimActor(args)
	if err != nil {
		return err
	}
	result, err := claimTask(vaultPath, id, actor, args.String("reason"), args.Bool("force"))
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "claim": result})
		return nil
	}
	if !args.Bool("quiet") {
		fmt.Printf("%s claimed by %s: %s → %s\n", result.ID, result.Assignee, fallback(result.From, "(unset)"), result.To)
	}
	return nil
}

func nextCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	epic := strings.ToUpper(strings.TrimSpace(args.String("epic")))
	owner := strings.TrimSpace(firstNonEmpty(args.String("owner"), args.String("assignee")))
	selected, ok := pickV7Next(vaultPath, epic, owner)
	if !ok {
		return tuskerError(errorNotFound, "No pickable V7 tasks found", withHint("pickable means status ready/rework, readiness ready, and next_owner matching --owner when provided"))
	}
	if args.Bool("claim") {
		if args.String("owner") == "" {
			args["owner"] = firstNonEmpty(owner, args.String("as"), args.String("actor"), "agent:"+defaultActorName())
		}
		args["id"] = stringField(selected.Data, "id")
		if _, err := writeV7Lease(args, "active"); err != nil {
			return err
		}
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "item": selected.Data})
		return nil
	}
	if !args.Bool("quiet") {
		fmt.Printf("%-14s  %-5s  %-8s  %-6s  %s\n", stringField(selected.Data, "id"), stringField(selected.Data, "priority"), stringField(selected.Data, "risk"), stringField(selected.Data, "status"), stringField(selected.Data, "title"))
	}
	return nil
}

func nextV5Cmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	notes, err := listAllNotes(vaultPath)
	if err != nil {
		return err
	}
	lookup := buildNoteLookup(notes)
	epic := strings.ToUpper(strings.TrimSpace(args.String("epic")))
	owner := strings.TrimSpace(firstNonEmpty(args.String("owner"), args.String("assignee")))
	candidates := pickableTasks(notes, lookup, epic, owner)
	if len(candidates) == 0 {
		return tuskerError(errorNotFound, "No pickable tasks found", withHint("pickable means status ready/rework, no unresolved blockers, and not already assigned to someone else"))
	}
	selected := candidates[0]
	if args.Bool("claim") {
		actor, err := requireClaimActor(args)
		if err != nil {
			return err
		}
		result, err := claimTask(vaultPath, stringField(selected.Data, "id"), actor, args.String("reason"), args.Bool("force"))
		if err != nil {
			return err
		}
		if args.Bool("json") {
			emitJSON(map[string]any{"ok": true, "selected": taskSummary(selected), "claim": result})
			return nil
		}
		if !args.Bool("quiet") {
			fmt.Printf("%s claimed by %s: %s → %s\n", result.ID, result.Assignee, fallback(result.From, "(unset)"), result.To)
		}
		return nil
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "item": taskSummary(selected)})
		return nil
	}
	if !args.Bool("quiet") {
		fmt.Printf("%-14s  %-5s  %-8s  %-6s  %s\n", stringField(selected.Data, "id"), stringField(selected.Data, "priority"), stringField(selected.Data, "risk"), stringField(selected.Data, "status"), stringField(selected.Data, "title"))
	}
	return nil
}

func requireClaimActor(args Args) (string, error) {
	actor := strings.TrimSpace(firstNonEmpty(args.String("as"), args.String("actor"), args.String("by"), args.String("owner")))
	if actor == "" {
		return "", tuskerError(errorMissingArg, "claim requires --as <agent-or-person>", withContext(map[string]any{"arg": "--as"}))
	}
	return actor, nil
}

func claimTask(vaultPath, id, actor, reason string, force bool) (claimResult, error) {
	var result claimResult
	err := withClaimLock(vaultPath, id, func() error {
		notes, err := listAllNotes(vaultPath)
		if err != nil {
			return err
		}
		lookup := buildNoteLookup(notes)
		target := wikiTarget(id)
		note, ok := lookup.ByID[target]
		if !ok {
			note, ok = lookup.ByRecordID[target]
		}
		if !ok {
			return tuskerError(errorNotFound, "Note not found: "+target, withContext(map[string]any{"id": target}))
		}
		if stringField(note.Data, "type") != "task" {
			return tuskerError(errorInvalidArg, fmt.Sprintf(`claim only supports V5 tasks, got "%s"`, stringField(note.Data, "type")), withContext(map[string]any{"id": id, "type": stringField(note.Data, "type")}))
		}
		if !isV5Schema(note.Data) {
			return tuskerError(errorInvalidArg, fmt.Sprintf(`claim only supports V5 notes, got schema "%s"`, stringField(note.Data, "schema")), withContext(map[string]any{"id": id, "schema": stringField(note.Data, "schema")}))
		}
		prev := stringField(note.Data, "status")
		if !claimableStatus(prev) {
			return tuskerError(errorInvalidTransition, fmt.Sprintf(`%s: claim requires status "ready" or "rework", got "%s"`, stringField(note.Data, "id"), prev), withContext(map[string]any{"id": stringField(note.Data, "id"), "status": prev}))
		}
		if assignee := stringField(note.Data, "assignee"); assignee != "" && assignee != actor && !force {
			return tuskerError(errorAlreadyExists, fmt.Sprintf(`%s is already assigned to %s`, stringField(note.Data, "id"), assignee), withHint("use --force only when deliberately taking over the task"), withContext(map[string]any{"id": stringField(note.Data, "id"), "assignee": assignee}))
		}
		if blocker := unresolvedBlockerReason(note, lookup.ByID, lookup.ByRecordID); blocker != "" {
			return tuskerError(errorInvalidTransition, fmt.Sprintf(`%s cannot be claimed: %s`, stringField(note.Data, "id"), blocker), withContext(map[string]any{"id": stringField(note.Data, "id")}))
		}
		data, body, err := parseFrontmatterMustRead(note.AbsolutePath)
		if err != nil {
			return err
		}
		date := todayISO()
		now := time.Now().UTC().Format(time.RFC3339)
		data["assignee"] = actor
		data["status"] = "active"
		data["updated"] = date
		if stringField(data, "started") == "" {
			data["started"] = date
		}
		if strings.TrimSpace(stringField(data, "block_reason")) != "" {
			data["block_reason"] = ""
		}
		appendTransition(data, orderedTransition(now, "claim", prev, "active", actor, reason))
		body = appendWorkLogBullet(body, fmt.Sprintf("%s — %s — claimed: %s → active%s", date, actor, fallback(prev, "(unset)"), suffixReason(reason)))
		content, err := serializeDocument(data, body, frontmatterOrderForType("task"))
		if err != nil {
			return err
		}
		if err := writeText(note.AbsolutePath, content); err != nil {
			return err
		}
		result = claimResult{
			ID:       stringField(data, "id"),
			Title:    stringField(data, "title"),
			From:     prev,
			To:       "active",
			Assignee: actor,
			Path:     note.RelativePath,
		}
		return nil
	})
	if err != nil {
		return claimResult{}, err
	}
	autoReindex(vaultPath)
	return result, nil
}

func withClaimLock(vaultPath, id string, fn func() error) error {
	lockDir := filepath.Join(vaultPath, "_system", "locks")
	if err := ensureDir(lockDir); err != nil {
		return err
	}
	lockPath := filepath.Join(lockDir, sanitizeLockName(wikiTarget(id))+".lock")
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return tuskerError(errorAlreadyExists, "Claim lock already exists for "+wikiTarget(id), withHint("another agent may be claiming it; remove the lock only if you know the claim died"), withPath(lockPath))
		}
		return err
	}
	_, _ = fmt.Fprintf(file, "%s %s\n", time.Now().UTC().Format(time.RFC3339), defaultActorName())
	_ = file.Close()
	defer os.Remove(lockPath)
	return fn()
}

func sanitizeLockName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", " ", "_")
	return replacer.Replace(value)
}

func claimableStatus(status string) bool {
	return status == "ready" || status == "rework"
}

func pickableTasks(notes []Note, lookup noteLookup, epic, owner string) []Note {
	var candidates []Note
	for _, note := range notes {
		if stringField(note.Data, "type") != "task" {
			continue
		}
		if !claimableStatus(stringField(note.Data, "status")) {
			continue
		}
		if epic != "" && wikiTarget(note.Data["epic"]) != epic {
			continue
		}
		assignee := stringField(note.Data, "assignee")
		if owner != "" {
			if assignee != "" && assignee != owner {
				continue
			}
		} else if assignee != "" {
			continue
		}
		if unresolvedBlockerReason(note, lookup.ByID, lookup.ByRecordID) != "" {
			continue
		}
		candidates = append(candidates, note)
	}
	sortPickableTasks(candidates)
	return candidates
}

func sortPickableTasks(notes []Note) {
	sort.SliceStable(notes, func(i, j int) bool {
		left, right := notes[i], notes[j]
		if lp, rp := priorityRank(stringField(left.Data, "priority")), priorityRank(stringField(right.Data, "priority")); lp != rp {
			return lp < rp
		}
		if lr, rr := pickupRiskRank(stringField(left.Data, "risk")), pickupRiskRank(stringField(right.Data, "risk")); lr != rr {
			return lr < rr
		}
		if lc, rc := firstNonEmpty(stringField(left.Data, "created"), stringField(left.Data, "updated")), firstNonEmpty(stringField(right.Data, "created"), stringField(right.Data, "updated")); lc != rc {
			if lc == "" {
				return false
			}
			if rc == "" {
				return true
			}
			return lc < rc
		}
		return stringField(left.Data, "id") < stringField(right.Data, "id")
	})
}

func pickupRiskRank(value string) int {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "critical":
		return 0
	case "high":
		return 1
	case "medium":
		return 2
	case "low":
		return 3
	default:
		return 9
	}
}

func buildNoteLookup(notes []Note) noteLookup {
	lookup := noteLookup{
		ByID:       map[string]Note{},
		ByRecordID: map[string]Note{},
	}
	for _, note := range notes {
		if id := stringField(note.Data, "id"); id != "" {
			lookup.ByID[id] = note
		}
		if recordID := stringField(note.Data, "record_id"); recordID != "" {
			lookup.ByRecordID[recordID] = note
		}
	}
	return lookup
}

func unresolvedBlockerReason(note Note, notesByID map[string]Note, notesByRecordID map[string]Note) string {
	for _, blocker := range normalizeList(note.Data["blocked_by"]) {
		target := wikiTarget(blocker)
		if target == "" {
			continue
		}
		blockingNote, ok := notesByID[target]
		if !ok {
			return "unresolved blocker " + target
		}
		if !blockerResolved(blockingNote) {
			return "waiting on " + target
		}
	}
	for _, recordID := range normalizeList(note.Data["blocked_by_record_ids"]) {
		recordID = strings.TrimSpace(recordID)
		if recordID == "" {
			continue
		}
		blockingNote, ok := notesByRecordID[recordID]
		if !ok {
			return "unresolved blocker record " + recordID
		}
		if !blockerResolved(blockingNote) {
			return "waiting on " + firstNonEmpty(stringField(blockingNote.Data, "id"), recordID)
		}
	}
	return ""
}

func taskSummary(note Note) map[string]any {
	return map[string]any{
		"id":         stringField(note.Data, "id"),
		"title":      stringField(note.Data, "title"),
		"status":     stringField(note.Data, "status"),
		"priority":   stringField(note.Data, "priority"),
		"risk":       stringField(note.Data, "risk"),
		"epic":       wikiTarget(note.Data["epic"]),
		"assignee":   stringField(note.Data, "assignee"),
		"path":       note.RelativePath,
		"created":    stringField(note.Data, "created"),
		"updated":    stringField(note.Data, "updated"),
		"blocked_by": normalizeList(note.Data["blocked_by"]),
	}
}
