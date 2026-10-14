package main

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// The logbook is the narrative, product-facing cousin of `tusker digest`.
// Where the digest is a terse dev/escalation board, the logbook composes a
// day's records into three plain-language sections a PM can read over coffee:
// what shipped, what it means, and what needs the human. It is a pure
// projection of records Tusker already holds (events, tasks, gates,
// escalations, evidence) — no proof re-computation and no external calls.

type logbookShipped struct {
	TaskID    string `json:"taskId"`
	Title     string `json:"title"`
	Milestone string `json:"milestone"`
	Outcome   string `json:"outcome"`
	Link      string `json:"link"`
	At        string `json:"at"`
}

type logbookRepair struct {
	TaskID string `json:"taskId"`
	Title  string `json:"title"`
	Link   string `json:"link"`
}

type logbookEvidence struct {
	Label string `json:"label"`
	Link  string `json:"link"`
}

type logbookMeaning struct {
	ChecksTotal  int               `json:"checksTotal"`
	ChecksPassed int               `json:"checksPassed"`
	Defects      int               `json:"defects"`
	Repairs      []logbookRepair   `json:"repairs"`
	Evidence     []logbookEvidence `json:"evidence"`
}

type logbookNeed struct {
	Kind   string `json:"kind"`
	Label  string `json:"label"`
	Detail string `json:"detail"`
	Link   string `json:"link"`
}

type logbookReference struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Link  string `json:"link"`
}

type tuskerLogbook struct {
	ProjectID   string             `json:"projectId"`
	Date        string             `json:"date"`
	GeneratedAt string             `json:"generatedAt"`
	Shipped     []logbookShipped   `json:"shipped"`
	Meaning     logbookMeaning     `json:"meaning"`
	NeedsHuman  []logbookNeed      `json:"needsHuman"`
	References  []logbookReference `json:"references"`
}

func buildTuskerLogbook(vaultPath string, day, now time.Time) (tuskerLogbook, error) {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return tuskerLogbook{}, err
	}
	store := v7MarkdownStore{VaultPath: vaultPath}
	events, err := store.GetEvents(context.Background(), v7EventScope{})
	if err != nil {
		return tuskerLogbook{}, err
	}
	dayKey := day.Format("2006-01-02")

	logbook := tuskerLogbook{
		ProjectID:   v7ProjectID(vaultPath),
		Date:        dayKey,
		GeneratedAt: now.Format(time.RFC3339),
		Shipped:     []logbookShipped{},
		NeedsHuman:  []logbookNeed{},
		References:  []logbookReference{},
	}

	refs := map[string]logbookReference{}
	addRef := func(id string) string {
		id = strings.TrimSpace(id)
		if id == "" {
			return ""
		}
		if task, ok := idx.Tasks[id]; ok {
			link := logbookNoteLink(task)
			refs[id] = logbookReference{ID: id, Title: stringField(task.Data, "title"), Link: link}
			return link
		}
		return ""
	}

	// What shipped: tasks that reached a completion milestone on the day, in
	// plain language, with their one-line outcome taken from the recorded
	// status-change reason. Keep only the strongest milestone per task.
	milestoneRank := map[string]int{"review": 1, "done": 2, "landed": 3, "integrated": 3}
	shippedByTask := map[string]logbookShipped{}
	touched := map[string]bool{}
	for _, ev := range events {
		if !logbookEventOnDay(ev.At, dayKey) {
			continue
		}
		if ev.ObjectKind == "task" {
			touched[ev.ObjectID] = true
		}
		if ev.ObjectKind != "task" || ev.EventKind != "status_changed" {
			continue
		}
		to := strings.ToLower(stringField(ev.Payload, "to"))
		rank, ok := milestoneRank[to]
		if !ok {
			continue
		}
		existing, seen := shippedByTask[ev.ObjectID]
		if seen && milestoneRank[strings.ToLower(existing.Milestone)] > rank {
			continue
		}
		link := addRef(ev.ObjectID)
		title := firstNonEmpty(logbookTaskTitle(idx, ev.ObjectID), ev.ObjectID)
		shippedByTask[ev.ObjectID] = logbookShipped{
			TaskID:    ev.ObjectID,
			Title:     title,
			Milestone: to,
			Outcome:   oneLine(stringField(ev.Payload, "reason")),
			Link:      link,
			At:        ev.At,
		}
	}
	for _, item := range shippedByTask {
		logbook.Shipped = append(logbook.Shipped, item)
	}
	sort.SliceStable(logbook.Shipped, func(i, j int) bool {
		if logbook.Shipped[i].At != logbook.Shipped[j].At {
			return logbook.Shipped[i].At < logbook.Shipped[j].At
		}
		return logbook.Shipped[i].TaskID < logbook.Shipped[j].TaskID
	})

	// What it means: gate / proof results recorded on the day, any repair work
	// opened, and evidence links — never the raw check transcripts themselves.
	repairSeen := map[string]bool{}
	for _, ev := range events {
		if !logbookEventOnDay(ev.At, dayKey) {
			continue
		}
		switch ev.EventKind {
		case "verification_added":
			logbook.Meaning.ChecksTotal++
			if strings.EqualFold(stringField(ev.Payload, "result"), "pass") {
				logbook.Meaning.ChecksPassed++
			} else {
				logbook.Meaning.Defects++
			}
		case "created":
			if ev.ObjectKind == "task" && logbookIsRepairTask(idx, ev.ObjectID) && !repairSeen[ev.ObjectID] {
				repairSeen[ev.ObjectID] = true
				logbook.Meaning.Repairs = append(logbook.Meaning.Repairs, logbookRepair{
					TaskID: ev.ObjectID,
					Title:  firstNonEmpty(logbookTaskTitle(idx, ev.ObjectID), ev.ObjectID),
					Link:   addRef(ev.ObjectID),
				})
			}
		}
	}
	sort.SliceStable(logbook.Meaning.Repairs, func(i, j int) bool {
		return logbook.Meaning.Repairs[i].TaskID < logbook.Meaning.Repairs[j].TaskID
	})
	logbook.Meaning.Evidence = logbookEvidenceLinks(idx, touched)

	// What needs the human: currently open escalations, blocking human gates,
	// and work waiting on review — the morning's action list.
	for _, escalation := range digestOpenEscalations(idx) {
		label := firstNonEmpty(oneLine(escalation.Description), "An escalation is open")
		detail := logbookEscalationDetail(escalation.Severity, escalation.Reason)
		link := ""
		if escalation.TaskID != "" {
			link = addRef(escalation.TaskID)
		}
		logbook.NeedsHuman = append(logbook.NeedsHuman, logbookNeed{
			Kind:   "escalation",
			Label:  label,
			Detail: detail,
			Link:   link,
		})
	}
	for _, gate := range digestPendingHardGates(idx) {
		logbook.NeedsHuman = append(logbook.NeedsHuman, logbookNeed{
			Kind:   "gate",
			Label:  firstNonEmpty(gate.TaskTitle, gate.TaskID),
			Detail: "is waiting on a human sign-off before it can land",
			Link:   addRef(gate.TaskID),
		})
	}
	for _, task := range logbookTasksInReview(idx) {
		logbook.NeedsHuman = append(logbook.NeedsHuman, logbookNeed{
			Kind:   "review",
			Label:  firstNonEmpty(stringField(task.Data, "title"), stringField(task.Data, "id")),
			Detail: "is finished and waiting for your review",
			Link:   addRef(stringField(task.Data, "id")),
		})
	}

	for _, ref := range refs {
		logbook.References = append(logbook.References, ref)
	}
	sort.SliceStable(logbook.References, func(i, j int) bool {
		return logbook.References[i].ID < logbook.References[j].ID
	})
	return logbook, nil
}

func logbookEscalationDetail(severity, reason string) string {
	reason = strings.TrimSpace(strings.ReplaceAll(reason, "_", " "))
	switch strings.ToUpper(strings.TrimSpace(severity)) {
	case "P0":
		if reason != "" {
			return "urgent — " + reason
		}
		return "urgent and needs a decision"
	default:
		if reason != "" {
			return "needs a decision — " + reason
		}
		return "needs a decision"
	}
}

func logbookTaskTitle(idx v7Index, taskID string) string {
	if task, ok := idx.Tasks[taskID]; ok {
		return stringField(task.Data, "title")
	}
	return ""
}

func logbookNoteLink(note Note) string {
	if rel := strings.TrimSpace(note.RelativePath); rel != "" {
		return filepath.ToSlash(rel)
	}
	return ""
}

func logbookEventOnDay(at, dayKey string) bool {
	parsed, ok := parseTuskerTime(at)
	if !ok {
		return false
	}
	// Bucket by the host-local calendar day so a "morning" read lines up with
	// the reader's clock rather than UTC.
	return parsed.Local().Format("2006-01-02") == dayKey
}

func logbookIsRepairTask(idx v7Index, taskID string) bool {
	if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(taskID)), "BGR-") {
		return true
	}
	if task, ok := idx.Tasks[taskID]; ok {
		if strings.TrimSpace(stringField(task.Data, "repair_of")) != "" {
			return true
		}
		if strings.EqualFold(strings.TrimSpace(stringField(task.Data, "epic")), "BGR") {
			return true
		}
	}
	return false
}

func logbookTasksInReview(idx v7Index) []Note {
	out := []Note{}
	for _, task := range idx.Tasks {
		if stringField(task.Data, "status") == "review" {
			out = append(out, task)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return stringField(out[i].Data, "id") < stringField(out[j].Data, "id")
	})
	return out
}

func logbookEvidenceLinks(idx v7Index, touched map[string]bool) []logbookEvidence {
	out := []logbookEvidence{}
	seen := map[string]bool{}
	ids := make([]string, 0, len(touched))
	for id := range touched {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		for _, note := range idx.Evidence[id] {
			link := logbookNoteLink(note)
			if link == "" || seen[link] {
				continue
			}
			seen[link] = true
			label := firstNonEmpty(stringField(note.Data, "title"), stringField(note.Data, "kind"), "evidence")
			out = append(out, logbookEvidence{Label: oneLine(label), Link: link})
		}
	}
	return out
}
