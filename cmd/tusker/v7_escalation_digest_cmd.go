package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const (
	escalationStatusOpen         = "open"
	escalationStatusAcknowledged = "acknowledged"
	defaultEscalationThresholdH  = 4
	escalationNotifierModeEnv    = "TUSKER_ESCALATION_NOTIFIER"
	escalationNotifierRecordEnv  = "TUSKER_ESCALATION_NOTIFIER_RECORD"
)

var runnerEscalationReasons = makeSet("system_error", "security_concern", "unresolvable_conflict", "stuck_loop")

type escalationNotifier func(title, message string) error

var notifyEscalationUser escalationNotifier = defaultNotifyEscalationUser

type escalationRuntimeConfig struct {
	NotificationsEnabled bool
	StaleThresholdHours  int
}

type escalationCreateRequest struct {
	Severity     string
	TaskID       string
	Description  string
	Source       string
	Reason       string
	Actor        string
	DedupeKey    string
	ValidateTask bool
	Now          time.Time
}

type digestBuildOptions struct {
	Since          time.Time
	SinceOverride  string
	MarkWatermark  bool
	ApplyStaleBump bool
	Now            time.Time
}

type tuskerDigest struct {
	ProjectID                  string                  `json:"projectId"`
	GeneratedAt                string                  `json:"generatedAt"`
	Since                      string                  `json:"since"`
	Watermark                  string                  `json:"watermark"`
	PersistentEscalationBanner bool                    `json:"persistentEscalationBanner"`
	OpenEscalations            []digestEscalation      `json:"openEscalations"`
	Landed                     []digestLanded          `json:"landed"`
	RedParked                  []digestRedParked       `json:"redParked"`
	PendingHardGates           []digestPendingHardGate `json:"pendingHardGates"`
	ArmedWaves                 []armedWaveSnapshot     `json:"armedWaves"`
}

type digestEscalation struct {
	ID          string `json:"id"`
	Severity    string `json:"severity"`
	TaskID      string `json:"taskId,omitempty"`
	TaskTitle   string `json:"taskTitle,omitempty"`
	Reason      string `json:"reason"`
	Description string `json:"description"`
	Source      string `json:"source"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

type digestLanded struct {
	Kind     string   `json:"kind"`
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	LandedAt string   `json:"landedAt"`
	Members  []string `json:"members,omitempty"`
	WaveID   string   `json:"waveId,omitempty"`
}

type digestRedParked struct {
	TaskID      string `json:"taskId"`
	TaskTitle   string `json:"taskTitle"`
	LeaseState  string `json:"leaseState"`
	Outcome     string `json:"outcome"`
	FailingGate string `json:"failingGate"`
	RedriveHint string `json:"redriveHint"`
	UpdatedAt   string `json:"updatedAt"`
}

type digestPendingHardGate struct {
	TaskID    string `json:"taskId"`
	TaskTitle string `json:"taskTitle"`
	Risk      string `json:"risk"`
	Status    string `json:"status"`
	Proof     string `json:"proof"`
	UpdatedAt string `json:"updatedAt"`
}

func escalationV7Cmd(args Args) error {
	if strings.EqualFold(args.String("_pos0"), "ack") {
		shifted := Args{}
		for key, value := range args {
			if strings.HasPrefix(key, "_pos") {
				continue
			}
			shifted[key] = value
		}
		if id := strings.TrimSpace(args.String("_pos1")); id != "" {
			shifted["_pos0"] = id
		}
		return escalationV7AckCmd(shifted)
	}
	return escalationV7CreateCmd(args)
}

func escalationV7CreateCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	if err := ensureV7ControlMutation(vaultPath, args); err != nil {
		return err
	}
	positionals := escalationPositionals(args)
	severity := strings.ToUpper(strings.TrimSpace(firstNonEmpty(args.String("severity"), args.String("s"))))
	if len(positionals) >= 2 && positionals[0] == "-s" {
		severity = strings.ToUpper(strings.TrimSpace(positionals[1]))
		positionals = positionals[2:]
	}
	if severity == "" {
		severity = "P2"
	}
	reason := normalizeEscalationReason(firstNonEmpty(args.String("reason"), "system_error"))
	if _, ok := runnerEscalationReasons[reason]; !ok {
		return tuskerError(errorInvalidArg, "runner escalations require reason system_error, security_concern, unresolvable_conflict, or stuck_loop", withContext(map[string]any{"reason": reason}))
	}
	taskID := strings.ToUpper(strings.TrimSpace(firstNonEmpty(args.String("task"), args.String("id"))))
	description := strings.TrimSpace(firstNonEmpty(args.String("description"), strings.Join(positionals, " ")))
	note, created, err := createV7Escalation(vaultPath, escalationCreateRequest{
		Severity:     severity,
		TaskID:       taskID,
		Description:  description,
		Source:       "runner",
		Reason:       reason,
		Actor:        fallback(fallback(args.String("actor"), args.String("by")), "agent:"+defaultActorName()),
		ValidateTask: true,
	})
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "created": created, "escalation": escalationPayload(note)})
		return nil
	}
	if !args.Bool("quiet") {
		verb := "Created"
		if !created {
			verb = "Updated"
		}
		fmt.Printf("%s escalation %s (%s).\n", verb, stringField(note.Data, "id"), stringField(note.Data, "severity"))
	}
	return nil
}

func escalationV7AckCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	if err := ensureV7ControlMutation(vaultPath, args); err != nil {
		return err
	}
	id := strings.ToUpper(strings.TrimSpace(firstNonEmpty(args.String("id"), args.String("_pos0"))))
	if id == "" {
		return tuskerError(errorMissingArg, "Usage: tusker escalate ack <ESC-0001>")
	}
	note, err := resolveV7Note(vaultPath, id, "escalation")
	if err != nil {
		return err
	}
	data, body, err := parseFrontmatterMustRead(note.AbsolutePath)
	if err != nil {
		return err
	}
	if stringField(data, "status") == escalationStatusAcknowledged {
		if args.Bool("json") {
			emitJSON(map[string]any{"ok": true, "escalation": escalationPayload(note)})
		}
		return nil
	}
	actor := fallback(fallback(args.String("actor"), args.String("by")), "agent:"+defaultActorName())
	now := time.Now().UTC().Format(time.RFC3339)
	baseRev := stringField(data, "state_rev")
	data["status"] = escalationStatusAcknowledged
	data["acknowledged_by"] = actor
	data["acknowledged_at"] = now
	data["updated_by"] = actor
	data["updated_at"] = now
	if _, err := saveV7DocumentCAS(note.AbsolutePath, data, body, v7FrontmatterOrder["escalation"], baseRev); err != nil {
		return err
	}
	if err := emitV7Event(vaultPath, id, "escalation", "acknowledged", actor, map[string]any{"severity": stringField(data, "severity")}); err != nil {
		return err
	}
	if args.Bool("json") {
		updated, _ := resolveV7Note(vaultPath, id, "escalation")
		emitJSON(map[string]any{"ok": true, "escalation": escalationPayload(updated)})
		return nil
	}
	if !args.Bool("quiet") {
		fmt.Printf("Acknowledged escalation %s.\n", id)
	}
	return nil
}

func createV7Escalation(vaultPath string, req escalationCreateRequest) (Note, bool, error) {
	req.Severity = strings.ToUpper(strings.TrimSpace(req.Severity))
	if _, ok := v7EscalationSeverities[req.Severity]; !ok {
		return Note{}, false, tuskerError(errorInvalidArg, "invalid escalation severity: "+req.Severity)
	}
	req.TaskID = strings.ToUpper(strings.TrimSpace(req.TaskID))
	if req.ValidateTask {
		if req.TaskID == "" {
			return Note{}, false, tuskerError(errorMissingArg, "escalation requires --task <TASK-ID>")
		}
		if _, err := resolveV7Note(vaultPath, req.TaskID, "task"); err != nil {
			return Note{}, false, err
		}
	} else if req.TaskID != "" && !v7TaskIDPattern.MatchString(req.TaskID) {
		req.TaskID = ""
	} else if req.TaskID != "" {
		if _, err := resolveV7Note(vaultPath, req.TaskID, "task"); err != nil {
			req.TaskID = ""
		}
	}
	req.Description = strings.TrimSpace(req.Description)
	if req.Description == "" {
		return Note{}, false, tuskerError(errorMissingArg, "escalation requires a description")
	}
	req.Source = normalizeEscalationReason(fallback(req.Source, "daemon"))
	req.Reason = normalizeEscalationReason(fallback(req.Reason, "system_error"))
	req.Actor = fallback(req.Actor, "tusker:daemon")
	now := req.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	nowText := now.Format(time.RFC3339)
	cfg := readEscalationRuntimeConfig(vaultPath)

	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return Note{}, false, err
	}
	if req.DedupeKey != "" {
		if note, ok := openEscalationByDedupe(idx, req.DedupeKey); ok {
			data, body, err := parseFrontmatterMustRead(note.AbsolutePath)
			if err != nil {
				return Note{}, false, err
			}
			previousSeverity := stringField(data, "severity")
			if escalationSeverityRank(req.Severity) < escalationSeverityRank(previousSeverity) {
				data["severity"] = req.Severity
			}
			data["description"] = req.Description
			data["last_seen_at"] = nowText
			data["updated_at"] = nowText
			data["updated_by"] = req.Actor
			baseRev := stringField(data, "state_rev")
			if _, err := saveV7DocumentCAS(note.AbsolutePath, data, body, v7FrontmatterOrder["escalation"], baseRev); err != nil {
				return Note{}, false, err
			}
			updated, _ := resolveV7Note(vaultPath, stringField(data, "id"), "escalation")
			if stringField(data, "severity") != previousSeverity {
				_ = routeEscalationSeverity(vaultPath, updated, cfg)
				updated, _ = resolveV7Note(vaultPath, stringField(data, "id"), "escalation")
			}
			return updated, false, nil
		}
	}

	id := nextV7EscalationID(idx)
	path := filepath.Join(vaultPath, "work", "escalations", id+".md")
	data := map[string]any{
		"schema":                "tusker.escalation/v7",
		"kind":                  "escalation",
		"id":                    id,
		"project":               v7ProjectID(vaultPath),
		"severity":              req.Severity,
		"status":                escalationStatusOpen,
		"source":                req.Source,
		"reason":                req.Reason,
		"description":           req.Description,
		"stale_threshold_hours": cfg.StaleThresholdHours,
		"created_at":            nowText,
		"created_by":            req.Actor,
		"updated_at":            nowText,
		"updated_by":            req.Actor,
	}
	if req.TaskID != "" {
		data["task"] = req.TaskID
	}
	if req.DedupeKey != "" {
		data["dedupe_key"] = req.DedupeKey
		data["last_seen_at"] = nowText
	}
	body := fmt.Sprintf("# %s · %s escalation\n\n## Description\n\n%s\n", id, req.Severity, req.Description)
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["escalation"])
	if err != nil {
		return Note{}, false, err
	}
	if err := writeText(path, content); err != nil {
		return Note{}, false, err
	}
	if err := emitV7Event(vaultPath, id, "escalation", "created", req.Actor, map[string]any{"severity": req.Severity, "task": req.TaskID, "source": req.Source, "reason": req.Reason}); err != nil {
		return Note{}, false, err
	}
	note, err := resolveV7Note(vaultPath, id, "escalation")
	if err != nil {
		return Note{}, false, err
	}
	if err := routeEscalationSeverity(vaultPath, note, cfg); err != nil {
		return Note{}, false, err
	}
	note, _ = resolveV7Note(vaultPath, id, "escalation")
	return note, true, nil
}

func applyStaleEscalationBumps(vaultPath string, now time.Time) (int, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return 0, err
	}
	cfg := readEscalationRuntimeConfig(vaultPath)
	changed := 0
	for _, note := range sortedEscalationNotes(idx) {
		data, body, err := parseFrontmatterMustRead(note.AbsolutePath)
		if err != nil {
			return changed, err
		}
		if stringField(data, "status") != escalationStatusOpen || stringField(data, "acknowledged_at") != "" || stringField(data, "stale_bumped_at") != "" {
			continue
		}
		severity := stringField(data, "severity")
		nextSeverity := escalationNextSeverity(severity)
		if nextSeverity == severity {
			continue
		}
		threshold := intField(data, "stale_threshold_hours")
		if threshold <= 0 {
			threshold = cfg.StaleThresholdHours
		}
		createdAt, ok := parseTuskerTime(stringField(data, "created_at"))
		if !ok || now.Sub(createdAt) < time.Duration(threshold)*time.Hour {
			continue
		}
		nowText := now.Format(time.RFC3339)
		baseRev := stringField(data, "state_rev")
		data["severity"] = nextSeverity
		data["stale_bumped_from"] = severity
		data["stale_bumped_at"] = nowText
		data["updated_at"] = nowText
		data["updated_by"] = "tusker:digest"
		if _, err := saveV7DocumentCAS(note.AbsolutePath, data, body, v7FrontmatterOrder["escalation"], baseRev); err != nil {
			return changed, err
		}
		if err := emitV7Event(vaultPath, stringField(data, "id"), "escalation", "stale_bumped", "tusker:digest", map[string]any{"from": severity, "to": nextSeverity}); err != nil {
			return changed, err
		}
		updated, _ := resolveV7Note(vaultPath, stringField(data, "id"), "escalation")
		if err := routeEscalationSeverity(vaultPath, updated, cfg); err != nil {
			return changed, err
		}
		changed++
	}
	return changed, nil
}

func digestCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	since, sinceOverride, err := digestSinceFromArgs(args)
	if err != nil {
		return err
	}
	digest, err := buildTuskerDigest(vaultPath, store, digestBuildOptions{
		Since:          since,
		SinceOverride:  sinceOverride,
		MarkWatermark:  true,
		ApplyStaleBump: true,
	})
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(digest)
		return nil
	}
	fmt.Print(renderTuskerDigestMarkdown(digest))
	return nil
}

func buildTuskerDigest(vaultPath string, store *RuntimeStore, opts digestBuildOptions) (tuskerDigest, error) {
	now := opts.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if opts.ApplyStaleBump {
		if _, err := applyStaleEscalationBumps(vaultPath, now); err != nil {
			return tuskerDigest{}, err
		}
	}
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return tuskerDigest{}, err
	}
	projectID := v7ProjectID(vaultPath)
	watermarkKey := digestWatermarkKey(projectID)
	watermark := ""
	if store != nil {
		watermark, _ = store.GetSetting(watermarkKey)
	}
	since := opts.Since
	if since.IsZero() && strings.TrimSpace(opts.SinceOverride) == "" {
		since, _ = parseTuskerTime(watermark)
	}
	generatedAt := now.Format(time.RFC3339)
	digest := tuskerDigest{
		ProjectID:        projectID,
		GeneratedAt:      generatedAt,
		Since:            formatDigestSince(since),
		Watermark:        watermark,
		OpenEscalations:  []digestEscalation{},
		Landed:           []digestLanded{},
		RedParked:        []digestRedParked{},
		PendingHardGates: []digestPendingHardGate{},
		ArmedWaves:       []armedWaveSnapshot{},
	}
	digest.OpenEscalations = digestOpenEscalations(idx)
	for _, escalation := range digest.OpenEscalations {
		if escalation.Severity == "P0" {
			digest.PersistentEscalationBanner = true
			break
		}
	}
	digest.Landed = digestLandedItems(idx, since)
	if store != nil {
		runs, _ := store.ListRuns()
		digest.RedParked = digestRedParkedItems(idx, runs, projectID)
		digest.ArmedWaves = digestArmedWaveItems(vaultPath, idx, runs, projectID, now)
	} else {
		digest.ArmedWaves = digestArmedWaveItems(vaultPath, idx, nil, projectID, now)
	}
	digest.PendingHardGates = digestPendingHardGates(idx)
	if opts.MarkWatermark && store != nil {
		if err := store.SetSetting(watermarkKey, generatedAt); err != nil {
			return digest, err
		}
		digest.Watermark = generatedAt
	}
	return digest, nil
}

func renderTuskerDigestMarkdown(d tuskerDigest) string {
	var b strings.Builder
	b.WriteString("# Tusker Morning Digest\n\n")
	if d.Since == "" {
		b.WriteString("- Since: all recorded state\n")
	} else {
		b.WriteString("- Since: " + d.Since + "\n")
	}
	b.WriteString("- Generated: " + d.GeneratedAt + "\n\n")
	renderDigestEscalations(&b, d.OpenEscalations)
	renderDigestLanded(&b, d.Landed)
	renderDigestRedParked(&b, d.RedParked)
	renderDigestHardGates(&b, d.PendingHardGates)
	renderDigestArmedWaves(&b, d.ArmedWaves)
	return b.String()
}

func renderDigestArmedWaves(b *strings.Builder, waves []armedWaveSnapshot) {
	b.WriteString("## Armed wave timeline\n\n")
	if len(waves) == 0 {
		b.WriteString("- None.\n\n")
		return
	}
	for _, wave := range waves {
		b.WriteString(fmt.Sprintf("- `%s` authorization=%s frontier=%s\n", wave.WaveID, wave.Authorization, fallback(strings.Join(wave.Frontier, ","), "none")))
		for _, member := range wave.Members {
			b.WriteString(fmt.Sprintf("  - `%s` state=%s reason=%s\n", member.ID, member.State, fallback(member.Reason, "none")))
		}
	}
	b.WriteString("\n")
}

func digestArmedWaveItems(vaultPath string, idx v7Index, runs []RunStatus, projectID string, now time.Time) []armedWaveSnapshot {
	projectRuns := map[string]RunStatus{}
	for _, run := range runs {
		if projectID == "" || run.ProjectID == "" || run.ProjectID == projectID {
			projectRuns[firstNonEmpty(run.ItemID, run.RecordID)] = run
		}
	}
	var out []armedWaveSnapshot
	for _, wave := range sortedV7Waves(idx) {
		if stringField(wave.Data, "status") == "landed" {
			continue
		}
		out = append(out, buildArmedWaveSnapshot(vaultPath, idx, wave, projectRuns, now))
	}
	return out
}

func renderDigestEscalations(b *strings.Builder, rows []digestEscalation) {
	b.WriteString("## Open Escalations\n\n")
	if len(rows) == 0 {
		b.WriteString("_None._\n\n")
		return
	}
	for _, row := range rows {
		task := ""
		if row.TaskID != "" {
			task = " " + row.TaskID
		}
		b.WriteString(fmt.Sprintf("- [%s] %s%s — %s\n", row.Severity, row.ID, task, row.Description))
	}
	b.WriteString("\n")
}

func renderDigestLanded(b *strings.Builder, rows []digestLanded) {
	b.WriteString("## Landed\n\n")
	if len(rows) == 0 {
		b.WriteString("_None._\n\n")
		return
	}
	for _, row := range rows {
		meta := row.LandedAt
		if row.Kind == "wave" && len(row.Members) > 0 {
			meta += fmt.Sprintf(" · %d members", len(row.Members))
		}
		b.WriteString(fmt.Sprintf("- %s %s — %s (%s)\n", row.Kind, row.ID, row.Title, meta))
	}
	b.WriteString("\n")
}

func renderDigestRedParked(b *strings.Builder, rows []digestRedParked) {
	b.WriteString("## Red / Parked\n\n")
	if len(rows) == 0 {
		b.WriteString("_None._\n\n")
		return
	}
	for _, row := range rows {
		b.WriteString(fmt.Sprintf("- %s — %s; %s; %s\n", row.TaskID, row.FailingGate, row.LeaseState, row.RedriveHint))
	}
	b.WriteString("\n")
}

func renderDigestHardGates(b *strings.Builder, rows []digestPendingHardGate) {
	b.WriteString("## Pending Hard Gates\n\n")
	if len(rows) == 0 {
		b.WriteString("_None._\n\n")
		return
	}
	for _, row := range rows {
		b.WriteString(fmt.Sprintf("- %s [%s] — %s; proof: %s\n", row.TaskID, row.Risk, row.TaskTitle, row.Proof))
	}
	b.WriteString("\n")
}

func digestOpenEscalations(idx v7Index) []digestEscalation {
	out := []digestEscalation{}
	for _, note := range sortedEscalationNotes(idx) {
		if stringField(note.Data, "status") != escalationStatusOpen {
			continue
		}
		taskID := stringField(note.Data, "task")
		out = append(out, digestEscalation{
			ID:          stringField(note.Data, "id"),
			Severity:    stringField(note.Data, "severity"),
			TaskID:      taskID,
			TaskTitle:   digestTaskTitle(idx, taskID),
			Reason:      stringField(note.Data, "reason"),
			Description: stringField(note.Data, "description"),
			Source:      stringField(note.Data, "source"),
			CreatedAt:   stringField(note.Data, "created_at"),
			UpdatedAt:   stringField(note.Data, "updated_at"),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if escalationSeverityRank(out[i].Severity) != escalationSeverityRank(out[j].Severity) {
			return escalationSeverityRank(out[i].Severity) < escalationSeverityRank(out[j].Severity)
		}
		return out[i].CreatedAt < out[j].CreatedAt
	})
	return out
}

func digestLandedItems(idx v7Index, since time.Time) []digestLanded {
	out := []digestLanded{}
	for _, wave := range sortedV7Waves(idx) {
		if stringField(wave.Data, "status") != "landed" || !armedWaveIntegrated(wave) {
			continue
		}
		landedAt := stringField(wave.Data, "landed_at")
		if !digestTimeInWindow(landedAt, since) {
			continue
		}
		out = append(out, digestLanded{
			Kind:     "wave",
			ID:       stringField(wave.Data, "id"),
			Title:    stringField(wave.Data, "title"),
			LandedAt: landedAt,
			Members:  normalizeList(wave.Data["members"]),
		})
	}
	tasks := make([]Note, 0, len(idx.Tasks))
	for _, task := range idx.Tasks {
		tasks = append(tasks, task)
	}
	sort.Slice(tasks, func(i, j int) bool { return stringField(tasks[i].Data, "id") < stringField(tasks[j].Data, "id") })
	for _, task := range tasks {
		if stringField(task.Data, "status") != "done" {
			continue
		}
		if waveID := stringField(task.Data, "wave"); waveID != "" {
			wave, ok := idx.Waves[waveID]
			if !ok || !armedWaveLandedMembers(wave)[stringField(task.Data, "id")] {
				continue
			}
		}
		closedAt := stringField(task.Data, "closed_at")
		if !digestTimeInWindow(closedAt, since) {
			continue
		}
		out = append(out, digestLanded{
			Kind:     "task",
			ID:       stringField(task.Data, "id"),
			Title:    stringField(task.Data, "title"),
			LandedAt: closedAt,
			WaveID:   stringField(task.Data, "wave"),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].LandedAt != out[j].LandedAt {
			return out[i].LandedAt < out[j].LandedAt
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func digestRedParkedItems(idx v7Index, runs []RunStatus, projectID string) []digestRedParked {
	out := []digestRedParked{}
	for _, run := range runs {
		if projectID != "" && run.ProjectID != "" && run.ProjectID != projectID {
			continue
		}
		if !digestRunIsRedOrParked(run) {
			continue
		}
		taskID := firstNonEmpty(run.ItemID, run.RecordID)
		failingGate := firstNonEmpty(run.LastError, run.AttemptOutcome, "run stopped without a recorded failure")
		out = append(out, digestRedParked{
			TaskID:      taskID,
			TaskTitle:   digestTaskTitle(idx, taskID),
			LeaseState:  run.LeaseState,
			Outcome:     firstNonEmpty(run.AttemptOutcome, serveRunOutcome(run, time.Now().UTC())),
			FailingGate: oneLine(failingGate),
			RedriveHint: "run `tusker redrive " + taskID + " --reason <why>`",
			UpdatedAt:   run.UpdatedAt,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].UpdatedAt != out[j].UpdatedAt {
			return out[i].UpdatedAt < out[j].UpdatedAt
		}
		return out[i].TaskID < out[j].TaskID
	})
	return out
}

func digestPendingHardGates(idx v7Index) []digestPendingHardGate {
	out := []digestPendingHardGate{}
	for _, gate := range sortedV7Gates(idx) {
		if stringField(gate.Data, "status") != "open" || !boolField(gate.Data, "blocking") || !v7GateOwnerNeedsAgentBoundary(stringField(gate.Data, "owner")) {
			continue
		}
		for _, taskID := range normalizeList(gate.Data["blocks"]) {
			task, ok := idx.Tasks[taskID]
			if !ok {
				continue
			}
			out = append(out, digestPendingHardGate{
				TaskID:    taskID,
				TaskTitle: stringField(task.Data, "title"),
				Risk:      stringField(gate.Data, "gate_kind"),
				Status:    stringField(task.Data, "status"),
				Proof:     firstNonEmpty(stringField(task.Data, "proof_status"), "pending"),
				UpdatedAt: firstNonEmpty(stringField(gate.Data, "updated_at"), stringField(task.Data, "updated_at")),
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].TaskID != out[j].TaskID {
			return out[i].TaskID < out[j].TaskID
		}
		return out[i].Risk < out[j].Risk
	})
	return out
}

func routeEscalationSeverity(vaultPath string, note Note, cfg escalationRuntimeConfig) error {
	severity := stringField(note.Data, "severity")
	if escalationSeverityRank(severity) > escalationSeverityRank("P1") || !cfg.NotificationsEnabled {
		return nil
	}
	message := stringField(note.Data, "description")
	if task := stringField(note.Data, "task"); task != "" {
		message = task + ": " + message
	}
	err := notifyEscalationUser("Tusker "+severity+" escalation", message)
	data, body, readErr := parseFrontmatterMustRead(note.AbsolutePath)
	if readErr != nil {
		return readErr
	}
	baseRev := stringField(data, "state_rev")
	now := time.Now().UTC().Format(time.RFC3339)
	data["notified_at"] = now
	if err != nil {
		data["notification_error"] = err.Error()
	} else {
		data["notification_error"] = ""
	}
	data["updated_at"] = now
	if _, saveErr := saveV7DocumentCAS(note.AbsolutePath, data, body, v7FrontmatterOrder["escalation"], baseRev); saveErr != nil {
		return saveErr
	}
	_ = emitV7Event(vaultPath, stringField(data, "id"), "escalation", "notified", "tusker:escalation", map[string]any{"severity": severity, "error": stringField(data, "notification_error")})
	return nil
}

func recordDaemonEscalationForRun(project RegisteredProject, run RunStatus, reason, description string) {
	if strings.TrimSpace(project.VaultRoot) == "" {
		return
	}
	taskID := firstNonEmpty(run.ItemID, run.RecordID)
	_, _, _ = createV7Escalation(project.VaultRoot, escalationCreateRequest{
		Severity:    "P1",
		TaskID:      taskID,
		Description: description,
		Source:      "daemon",
		Reason:      reason,
		Actor:       "tusker:daemon",
		DedupeKey:   "daemon:" + reason + ":" + project.ProjectID + ":" + run.RecordID,
	})
}

func (d *Daemon) recordDaemonEscalationForStoredRun(run RunStatus, reason, description string) {
	if d == nil || d.store == nil {
		return
	}
	projects, err := loadRegisteredProjects(d.store, registeredProjectLoadOptions{})
	if err != nil {
		return
	}
	for _, project := range projects {
		if project.Project.ProjectID == run.ProjectID {
			recordDaemonEscalationForRun(project.Project, run, reason, description)
			return
		}
	}
}

func (d *Daemon) recordInvariantEscalations(snapshot runtimeSentinelSnapshot, status invariantCircuitStatus) {
	if d == nil || len(status.Violations) == 0 {
		return
	}
	projects := map[string]RegisteredProject{}
	for _, project := range snapshot.Projects {
		projects[project.Project.ProjectID] = project.Project
	}
	for _, violation := range status.Violations {
		project := projects[violation.ProjectID]
		if strings.TrimSpace(project.VaultRoot) == "" {
			continue
		}
		taskID := firstNonEmpty(violation.ItemID, violation.RecordID)
		_, _, _ = createV7Escalation(project.VaultRoot, escalationCreateRequest{
			Severity:    "P1",
			TaskID:      taskID,
			Description: invariantViolationReason + ": " + violation.Detail,
			Source:      "daemon",
			Reason:      "sentinel_violation",
			Actor:       "tusker:daemon",
			DedupeKey:   "sentinel:" + violation.ProjectID + ":" + violation.RecordID + ":" + violation.Check,
		})
	}
}

func hasOpenP0Escalation(vaultPath string) bool {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return false
	}
	for _, note := range idx.Escalations {
		if stringField(note.Data, "status") == escalationStatusOpen && stringField(note.Data, "severity") == "P0" {
			return true
		}
	}
	return false
}

func escalationPayload(note Note) map[string]any {
	return map[string]any{
		"id":                stringField(note.Data, "id"),
		"severity":          stringField(note.Data, "severity"),
		"status":            stringField(note.Data, "status"),
		"task":              nullIfEmptyString(stringField(note.Data, "task")),
		"source":            stringField(note.Data, "source"),
		"reason":            stringField(note.Data, "reason"),
		"description":       stringField(note.Data, "description"),
		"stale_bumped_from": nullIfEmptyString(stringField(note.Data, "stale_bumped_from")),
		"stale_bumped_at":   nullIfEmptyString(stringField(note.Data, "stale_bumped_at")),
		"notified_at":       nullIfEmptyString(stringField(note.Data, "notified_at")),
		"acknowledged_by":   nullIfEmptyString(stringField(note.Data, "acknowledged_by")),
		"acknowledged_at":   nullIfEmptyString(stringField(note.Data, "acknowledged_at")),
		"created_at":        stringField(note.Data, "created_at"),
		"updated_at":        stringField(note.Data, "updated_at"),
	}
}

func readEscalationRuntimeConfig(vaultPath string) escalationRuntimeConfig {
	cfg := escalationRuntimeConfig{NotificationsEnabled: false, StaleThresholdHours: defaultEscalationThresholdH}
	file, _, err := readV7TuskerConfig(vaultPath)
	if err != nil {
		return cfg
	}
	if file.Escalation.NotificationsEnabled != nil {
		cfg.NotificationsEnabled = *file.Escalation.NotificationsEnabled
	}
	if file.Escalation.StaleThresholdHours > 0 {
		cfg.StaleThresholdHours = file.Escalation.StaleThresholdHours
	}
	return cfg
}

func defaultNotifyEscalationUser(title, message string) error {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(escalationNotifierModeEnv))) {
	case "off":
		return nil
	case "record":
		return recordEscalationNotification(title, message)
	}
	if runtime.GOOS != "darwin" {
		return nil
	}
	script := fmt.Sprintf("display notification %s with title %s", appleScriptString(message), appleScriptString(title))
	return exec.Command("osascript", "-e", script).Run()
}

func recordEscalationNotification(title, message string) error {
	path := strings.TrimSpace(os.Getenv(escalationNotifierRecordEnv))
	if path == "" {
		return nil
	}
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return err
	}
	line := strings.Join([]string{
		time.Now().UTC().Format(time.RFC3339Nano),
		escapeNotificationRecordField(title),
		escapeNotificationRecordField(message),
	}, "\t") + "\n"
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.WriteString(line)
	return err
}

func escapeNotificationRecordField(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\t", "\\t")
	value = strings.ReplaceAll(value, "\r", "\\r")
	value = strings.ReplaceAll(value, "\n", "\\n")
	return value
}

func appleScriptString(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, `"`, `\"`)
	return `"` + value + `"`
}

func escalationPositionals(args Args) []string {
	var out []string
	for i := 0; ; i++ {
		value, ok := args[fmt.Sprintf("_pos%d", i)]
		if !ok {
			break
		}
		out = append(out, strings.TrimSpace(value))
	}
	return filterStrings(out)
}

func normalizeEscalationReason(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "-", "_")
	value = strings.ReplaceAll(value, " ", "_")
	return value
}

func nextV7EscalationID(idx v7Index) string {
	maxSeq := 0
	for id := range idx.Escalations {
		if match := v7EscalationIDPattern.FindStringSubmatch(id); match != nil {
			maxSeq = maxInt(maxSeq, atoiSafe(match[1]))
		}
	}
	return fmt.Sprintf("ESC-%s", padNumber(maxSeq+1))
}

func openEscalationByDedupe(idx v7Index, dedupeKey string) (Note, bool) {
	for _, note := range idx.Escalations {
		if stringField(note.Data, "status") == escalationStatusOpen && stringField(note.Data, "dedupe_key") == dedupeKey {
			return note, true
		}
	}
	return Note{}, false
}

func sortedEscalationNotes(idx v7Index) []Note {
	out := make([]Note, 0, len(idx.Escalations))
	for _, note := range idx.Escalations {
		out = append(out, note)
	}
	sort.Slice(out, func(i, j int) bool { return stringField(out[i].Data, "id") < stringField(out[j].Data, "id") })
	return out
}

func escalationSeverityRank(severity string) int {
	switch strings.ToUpper(strings.TrimSpace(severity)) {
	case "P0":
		return 0
	case "P1":
		return 1
	case "P2":
		return 2
	default:
		return 99
	}
}

func escalationNextSeverity(severity string) string {
	switch strings.ToUpper(strings.TrimSpace(severity)) {
	case "P2":
		return "P1"
	case "P1":
		return "P0"
	default:
		return strings.ToUpper(strings.TrimSpace(severity))
	}
}

func digestSinceFromArgs(args Args) (time.Time, string, error) {
	raw := strings.TrimSpace(args.String("since"))
	return digestSinceFromQuery(raw)
}

func digestSinceFromQuery(raw string) (time.Time, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, "", nil
	}
	parsed, ok := parseTuskerTime(raw)
	if !ok {
		return time.Time{}, raw, tuskerError(errorInvalidArg, "digest --since must be RFC3339 or YYYY-MM-DD: "+raw)
	}
	return parsed, raw, nil
}

func digestWatermarkKey(projectID string) string {
	return "digest_watermark:" + strings.TrimSpace(projectID)
}

func parseTuskerTime(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}

func formatDigestSince(since time.Time) string {
	if since.IsZero() {
		return ""
	}
	return since.UTC().Format(time.RFC3339)
}

func digestTimeInWindow(value string, since time.Time) bool {
	parsed, ok := parseTuskerTime(value)
	if !ok {
		return since.IsZero()
	}
	return since.IsZero() || !parsed.Before(since)
}

func digestTaskTitle(idx v7Index, taskID string) string {
	if task, ok := idx.Tasks[taskID]; ok {
		return stringField(task.Data, "title")
	}
	return ""
}

func digestRunIsRedOrParked(run RunStatus) bool {
	switch LeaseState(strings.TrimSpace(run.LeaseState)) {
	case LeaseStateParkedNoProgress, LeaseStateParkedBudget:
		return true
	case LeaseStateRetryQueued, LeaseStateRunning, LeaseStateClaimed:
		return false
	}
	outcome := AttemptOutcome(strings.TrimSpace(run.AttemptOutcome))
	return outcome == AttemptOutcomeFailed || outcome == AttemptOutcomeBlocked || outcome == AttemptOutcomeBudgetExceeded
}

func oneLine(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func openClosed(open bool) string {
	if open {
		return "open"
	}
	return "closed"
}
