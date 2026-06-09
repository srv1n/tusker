package main

import (
	"fmt"
	"strings"
)

type noteLookup struct {
	ByID       map[string]Note
	ByRecordID map[string]Note
}

type nextPickFilters struct {
	Epic   string
	Owner  string
	Domain string
	Lane   string
}

type nextPickSkipped struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Path    string   `json:"path"`
	Reasons []string `json:"reasons"`
}

type nextPickReport struct {
	Selected Note              `json:"-"`
	Skipped  []nextPickSkipped `json:"skipped"`
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
	return removedSurfaceError("legacy claim")
}

func nextCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	epic := strings.ToUpper(strings.TrimSpace(args.String("epic")))
	owner := strings.TrimSpace(firstNonEmpty(args.String("owner"), args.String("assignee")))
	domain := strings.TrimSpace(args.String("domain"))
	lane := strings.TrimSpace(args.String("lane"))
	explain := args.Bool("explain")
	if domain != "" || lane != "" || explain {
		report, ok := pickV7NextWithReport(vaultPath, nextPickFilters{Epic: epic, Owner: owner, Domain: domain, Lane: lane})
		if !ok {
			if explain {
				if args.Bool("json") {
					emitJSON(map[string]any{"ok": false, "item": nil, "skipped": report.Skipped})
				} else if !args.Bool("quiet") {
					printNextExplanation(Note{}, report.Skipped)
				}
				return nil
			}
			return tuskerError(errorNotFound, "No pickable V7 tasks found", withHint("pickable means status ready/rework, readiness ready, next_owner matching --owner when provided, and matching --domain/--lane when provided"))
		}
		return emitNextSelection(args, vaultPath, report.Selected, report.Skipped, explain)
	}
	selected, ok := pickV7Next(vaultPath, epic, owner)
	if !ok {
		return tuskerError(errorNotFound, "No pickable V7 tasks found", withHint("pickable means status ready/rework, readiness ready, and next_owner matching --owner when provided"))
	}
	return emitNextSelection(args, vaultPath, selected, nil, false)
}

func emitNextSelection(args Args, vaultPath string, selected Note, skipped []nextPickSkipped, explain bool) error {
	if args.Bool("claim") {
		if args.String("owner") == "" {
			args["owner"] = firstNonEmpty(args.String("assignee"), args.String("owner"), args.String("as"), args.String("actor"), "agent:"+defaultActorName())
		}
		args["id"] = stringField(selected.Data, "id")
		if _, err := writeV7Lease(args, "active"); err != nil {
			return err
		}
	}
	if args.Bool("json") {
		payload := map[string]any{"ok": true, "item": selected.Data}
		if explain {
			payload["skipped"] = skipped
		}
		emitJSON(payload)
		return nil
	}
	if !args.Bool("quiet") {
		if explain {
			printNextExplanation(selected, skipped)
			return nil
		}
		title := stringField(selected.Data, "title")
		if idx, err := loadV7Index(vaultPath); err == nil {
			if attempt := v7LatestAttemptRuntimeSummary(idx, stringField(selected.Data, "id")); attempt != "" {
				title += " [" + attempt + "]"
			}
		}
		fmt.Printf("%-14s  %-5s  %-8s  %-6s  %s\n", stringField(selected.Data, "id"), stringField(selected.Data, "priority"), stringField(selected.Data, "risk"), stringField(selected.Data, "status"), title)
	}
	return nil
}

func pickV7NextWithReport(vaultPath string, filters nextPickFilters) (nextPickReport, bool) {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return nextPickReport{}, false
	}
	var report nextPickReport
	for _, task := range sortedV7Tasks(idx) {
		reasons := v7NextSkipReasons(vaultPath, task, filters)
		if len(reasons) == 0 && stringField(report.Selected.Data, "id") == "" {
			report.Selected = task
			continue
		}
		if len(reasons) == 0 {
			reasons = append(reasons, "earlier candidate selected")
		}
		report.Skipped = append(report.Skipped, nextPickSkipped{
			ID:      stringField(task.Data, "id"),
			Title:   stringField(task.Data, "title"),
			Path:    task.RelativePath,
			Reasons: reasons,
		})
	}
	return report, stringField(report.Selected.Data, "id") != ""
}

func v7NextSkipReasons(vaultPath string, task Note, filters nextPickFilters) []string {
	var reasons []string
	if filters.Epic != "" && strings.ToUpper(stringField(task.Data, "epic")) != filters.Epic {
		reasons = append(reasons, "epic "+stringField(task.Data, "epic")+" does not match "+filters.Epic)
	}
	reasons = append(reasons, v7TaskDispatchBlockers(vaultPath, task)...)
	nextOwner := stringField(task.Data, "next_owner")
	if filters.Owner != "" && nextOwner != filters.Owner {
		reasons = append(reasons, "next_owner "+fallback(nextOwner, "(unset)")+" does not match "+filters.Owner)
	}
	if filters.Domain != "" && !containsString(normalizeList(task.Data["domains"]), filters.Domain) {
		reasons = append(reasons, "domain "+nextListDisplay(normalizeList(task.Data["domains"]))+" does not match "+filters.Domain)
	}
	if filters.Lane != "" && !containsString(v7TaskLanes(task), filters.Lane) {
		reasons = append(reasons, "lane "+nextListDisplay(v7TaskLanes(task))+" does not match "+filters.Lane)
	}
	return uniqueStrings(reasons)
}

func v7TaskLanes(task Note) []string {
	return firstNonEmptyList(normalizeList(task.Data["lane"]), normalizeList(task.Data["lanes"]))
}

func nextListDisplay(items []string) string {
	if len(items) == 0 {
		return "(none)"
	}
	return strings.Join(items, ",")
}

func printNextExplanation(selected Note, skipped []nextPickSkipped) {
	fmt.Println("Selected:")
	if stringField(selected.Data, "id") == "" {
		fmt.Println("  none")
	} else {
		fmt.Printf("  %-14s  %-5s  %-8s  %-6s  %s\n", stringField(selected.Data, "id"), stringField(selected.Data, "priority"), stringField(selected.Data, "risk"), stringField(selected.Data, "status"), stringField(selected.Data, "title"))
	}
	fmt.Println("Skipped:")
	if len(skipped) == 0 {
		fmt.Println("  none")
		return
	}
	for _, item := range skipped {
		fmt.Printf("  %-14s  %s\n", item.ID, strings.Join(item.Reasons, "; "))
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
