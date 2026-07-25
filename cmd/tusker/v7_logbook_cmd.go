package main

import (
	"context"
	"fmt"
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

func logbookCmd(args Args) error {
	if args.Bool("scheduled-promotion") || args.Bool("morning-brief") {
		return scheduledPromotionMorningBriefCmd(args)
	}
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	day, err := logbookDateFromArgs(args)
	if err != nil {
		return err
	}
	logbook, err := buildTuskerLogbook(vaultPath, day, time.Now().UTC())
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(logbook)
		return nil
	}
	markdown := renderTuskerLogbookMarkdown(logbook)
	if args.Bool("write") {
		path := filepath.Join(vaultPath, "logbook", logbook.Date+".md")
		if err := writeText(path, markdown); err != nil {
			return err
		}
		if !args.Bool("quiet") {
			fmt.Print(markdown)
		}
		return nil
	}
	fmt.Print(markdown)
	return nil
}

func logbookDateFromArgs(args Args) (time.Time, error) {
	raw := strings.TrimSpace(args.String("date"))
	if raw == "" {
		now := time.Now().Local()
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local), nil
	}
	parsed, err := time.ParseInLocation("2006-01-02", raw, time.Local)
	if err != nil {
		return time.Time{}, tuskerError(errorInvalidArg, "logbook --date must be YYYY-MM-DD: "+raw)
	}
	return parsed, nil
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

func renderTuskerLogbookMarkdown(l tuskerLogbook) string {
	var b strings.Builder
	b.WriteString("# Morning Logbook — " + l.Date + "\n\n")
	b.WriteString("_A plain-language read on the day's work: what shipped, what it means, and what needs you._\n\n")

	quiet := len(l.Shipped) == 0 &&
		l.Meaning.ChecksTotal == 0 &&
		len(l.Meaning.Repairs) == 0 &&
		len(l.Meaning.Evidence) == 0 &&
		len(l.NeedsHuman) == 0
	if quiet {
		b.WriteString("It was a quiet day — nothing was recorded against this date. Enjoy the calm.\n\n")
	}

	b.WriteString("## What shipped\n\n")
	if len(l.Shipped) == 0 {
		b.WriteString("Nothing crossed the finish line today.\n\n")
	} else {
		for _, item := range l.Shipped {
			verb := logbookShippedVerb(item.Milestone)
			line := "- " + logbookLink(item.Title, item.Link) + " " + verb + "."
			if item.Outcome != "" {
				line += " " + logbookEnsureSentence(item.Outcome)
			}
			b.WriteString(line + "\n")
		}
		b.WriteString("\n")
	}

	b.WriteString("## What it means\n\n")
	b.WriteString(logbookMeaningProse(l.Meaning))
	b.WriteString("\n")

	b.WriteString("## What needs your attention\n\n")
	if len(l.NeedsHuman) == 0 {
		b.WriteString("Nothing needs you right now — enjoy your coffee.\n\n")
	} else {
		for _, need := range l.NeedsHuman {
			label := need.Label
			if need.Link != "" {
				label = logbookLink(need.Label, need.Link)
			}
			b.WriteString("- " + label + " " + need.Detail + ".\n")
		}
		b.WriteString("\n")
	}

	if len(l.References) > 0 {
		b.WriteString("## References\n\n")
		for _, ref := range l.References {
			title := ref.Title
			if title == "" {
				title = ref.ID
			}
			b.WriteString("- " + logbookLink(ref.ID, ref.Link) + " — " + title + "\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

func logbookMeaningProse(m logbookMeaning) string {
	var b strings.Builder
	switch {
	case m.ChecksTotal == 0:
		b.WriteString("No new proof or quality checks were recorded today.\n")
	case m.Defects == 0:
		b.WriteString(fmt.Sprintf("Every quality check held: %d verification%s ran and all passed, so nothing regressed.\n", m.ChecksTotal, plural(m.ChecksTotal)))
	default:
		b.WriteString(fmt.Sprintf("%d of %d checks surfaced a problem, so %d defect%s need follow-up.\n", m.Defects, m.ChecksTotal, m.Defects, plural(m.Defects)))
	}
	if len(m.Repairs) > 0 {
		b.WriteString("\nRepair work was opened to close the gaps:\n")
		for _, repair := range m.Repairs {
			b.WriteString("- " + logbookLink(repair.Title, repair.Link) + "\n")
		}
	}
	if len(m.Evidence) > 0 {
		b.WriteString("\nSupporting evidence:\n")
		for _, evidence := range m.Evidence {
			b.WriteString("- " + logbookLink(evidence.Label, evidence.Link) + "\n")
		}
	}
	return b.String()
}

func logbookShippedVerb(milestone string) string {
	switch strings.ToLower(strings.TrimSpace(milestone)) {
	case "done", "landed", "integrated":
		return "shipped"
	case "review":
		return "was finished and handed off for review"
	default:
		return "made progress"
	}
}

func logbookLink(text, link string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		text = "this work"
	}
	if strings.TrimSpace(link) == "" {
		return text
	}
	return "[" + text + "](" + link + ")"
}

func logbookEnsureSentence(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if !strings.HasSuffix(text, ".") && !strings.HasSuffix(text, "!") && !strings.HasSuffix(text, "?") {
		text += "."
	}
	return text
}
