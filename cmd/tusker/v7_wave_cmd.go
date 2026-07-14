package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var v7WaveGroupOrder = []string{
	armedWaveRunnable, armedWaveRunning, armedWaveReview, armedWaveLanded,
	armedWaveMachineParked, armedWaveHumanBlocked, armedWaveStaleAuthorization,
	armedWaveDependencyWaiting,
	// Retain the old empty headings for scripts parsing the pre-DAG display.
	"done", "parked", "ready", "blocked",
}

func waveV7Cmd(args Args) error {
	switch strings.ToLower(args.String("_pos0")) {
	case "create":
		return waveV7CreateCmd(shiftV7WaveArgs(args, 1))
	case "add":
		return waveV7AddCmd(shiftV7WaveArgs(args, 1))
	case "remove":
		return waveV7RemoveCmd(shiftV7WaveArgs(args, 1))
	case "show":
		return waveV7ShowCmd(shiftV7WaveArgs(args, 1))
	case "brief":
		return waveV7BriefCmd(shiftV7WaveArgs(args, 1))
	case "preflight":
		return waveV7PreflightCmd(shiftV7WaveArgs(args, 1))
	case "arm":
		return waveV7ArmCmd(shiftV7WaveArgs(args, 1))
	case "pause":
		return waveV7PauseCmd(shiftV7WaveArgs(args, 1))
	case "resume":
		return waveV7ResumeCmd(shiftV7WaveArgs(args, 1))
	case "disarm":
		return waveV7DisarmCmd(shiftV7WaveArgs(args, 1))
	default:
		return tuskerError(errorMissingArg, "Usage: tusker wave create|add|remove|show|brief|preflight|arm|pause|resume|disarm ...")
	}
}

func shiftV7WaveArgs(args Args, n int) Args {
	out := Args{}
	for key, value := range args {
		if strings.HasPrefix(key, "_pos") {
			continue
		}
		out[key] = value
	}
	positionals := wavePositionals(args, n)
	if len(positionals) > 0 {
		out["_pos"] = strings.Join(positionals, "\n")
		for i, value := range positionals {
			out[fmt.Sprintf("_pos%d", i)] = value
		}
	}
	return out
}

func waveV7CreateCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	if err := ensureV7ControlMutation(vaultPath, args); err != nil {
		return err
	}
	title := strings.TrimSpace(firstNonEmpty(args.String("title"), args.String("_pos0")))
	if title == "" {
		return tuskerError(errorMissingArg, `Usage: tusker wave create "<title>" <TASK-ID>...`)
	}
	members := waveTaskArgs(args, 1)
	if len(members) == 0 {
		return tuskerError(errorMissingArg, "wave create requires at least one task id")
	}
	members, err = validateV7WaveMembership(vaultPath, "", members)
	if err != nil {
		return err
	}
	id := strings.ToUpper(strings.TrimSpace(args.String("id")))
	if id == "" {
		id = nextV7WaveID(vaultPath)
	}
	if !v7WaveIDPattern.MatchString(id) {
		return tuskerError(errorInvalidArg, "invalid V7 wave id: "+id)
	}
	path := filepath.Join(vaultPath, "work", "waves", id+".md")
	if fileExists(path) {
		return tuskerError(errorAlreadyExists, "V7 wave already exists: "+id, withPath(path))
	}
	integrationBranch := v7IntegrationBranchName(id)
	if err := ensureV7IntegrationBranch(vaultPath, integrationBranch); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	actor := fallback(fallback(args.String("actor"), args.String("by")), "agent:"+defaultActorName())
	data := map[string]any{
		"schema":             "tusker.wave/v7",
		"kind":               "wave",
		"id":                 id,
		"project":            v7ProjectID(vaultPath),
		"title":              title,
		"status":             "open",
		"authorization":      "disarmed",
		"members":            members,
		"integration_branch": integrationBranch,
		"created_at":         now,
		"created_by":         actor,
		"updated_at":         now,
		"updated_by":         actor,
	}
	body := fmt.Sprintf(`# %s · %s

## Members

Membership is stored in frontmatter. Run `+"`tusker wave show %s`"+` for the boundary view.
`, id, title, id)
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["wave"])
	if err != nil {
		return err
	}
	if err := writeText(path, content); err != nil {
		return err
	}
	if err := emitV7Event(vaultPath, id, "wave", "created", actor, map[string]any{"members": members}); err != nil {
		return err
	}
	if err := reconcileV7AfterWaveMutation(vaultPath, args); err != nil {
		return err
	}
	if args.Bool("json") {
		idx, _ := loadV7Index(vaultPath)
		emitJSON(map[string]any{"ok": true, "wave": v7WavePayload(vaultPath, idx, idx.Waves[id])})
		return nil
	}
	if !args.Bool("quiet") {
		fmt.Printf("Created wave %s with %d member%s.\n", id, len(members), plural(len(members)))
	}
	return nil
}

func waveV7AddCmd(args Args) error {
	return waveV7EditMembership(args, true)
}

func waveV7RemoveCmd(args Args) error {
	return waveV7EditMembership(args, false)
}

func waveV7EditMembership(args Args, add bool) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	if err := ensureV7ControlMutation(vaultPath, args); err != nil {
		return err
	}
	id := strings.ToUpper(strings.TrimSpace(firstNonEmpty(args.String("id"), args.String("_pos0"))))
	if id == "" {
		return tuskerError(errorMissingArg, "wave id is required")
	}
	taskStart := 1
	tasks := waveTaskArgs(args, taskStart)
	if len(tasks) == 0 {
		return tuskerError(errorMissingArg, "wave membership edit requires at least one task id")
	}
	note, err := resolveV7Note(vaultPath, id, "wave")
	if err != nil {
		return err
	}
	data, body, err := parseFrontmatterMustRead(note.AbsolutePath)
	if err != nil {
		return err
	}
	current := normalizeList(data["members"])
	next := append([]string{}, current...)
	if add {
		next = append(next, tasks...)
		next, err = validateV7WaveMembership(vaultPath, id, next)
		if err != nil {
			return err
		}
	} else {
		removeSet := makeSet(tasks...)
		var removed []string
		var kept []string
		for _, member := range next {
			if _, ok := removeSet[member]; ok {
				removed = append(removed, member)
				continue
			}
			kept = append(kept, member)
		}
		if len(removed) != len(uniqueStrings(tasks)) {
			return tuskerError(errorInvalidArg, "wave remove includes a task that is not a member")
		}
		if len(kept) == 0 {
			return tuskerError(errorInvalidArg, "wave remove cannot remove the last member")
		}
		next, err = validateV7WaveMembership(vaultPath, id, kept)
		if err != nil {
			return err
		}
	}
	if stringSlicesEqual(current, next) {
		if !args.Bool("quiet") {
			fmt.Printf("%s unchanged.\n", id)
		}
		return nil
	}
	actor := fallback(fallback(args.String("actor"), args.String("by")), "agent:"+defaultActorName())
	baseRev := stringField(data, "state_rev")
	data["members"] = next
	data["updated_at"] = time.Now().UTC().Format(time.RFC3339)
	data["updated_by"] = actor
	if _, err := saveV7DocumentCAS(note.AbsolutePath, data, body, v7FrontmatterOrder["wave"], baseRev); err != nil {
		return err
	}
	if err := emitV7Event(vaultPath, id, "wave", "updated", actor, map[string]any{"members": next}); err != nil {
		return err
	}
	if err := reconcileV7AfterWaveMutation(vaultPath, args); err != nil {
		return err
	}
	if args.Bool("json") {
		idx, _ := loadV7Index(vaultPath)
		emitJSON(map[string]any{"ok": true, "wave": v7WavePayload(vaultPath, idx, idx.Waves[id])})
		return nil
	}
	if !args.Bool("quiet") {
		action := "Updated"
		fmt.Printf("%s wave %s; %d member%s.\n", action, id, len(next), plural(len(next)))
	}
	return nil
}

func waveV7ShowCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	id := strings.ToUpper(strings.TrimSpace(firstNonEmpty(args.String("id"), args.String("_pos0"))))
	if id == "" {
		return tuskerError(errorMissingArg, "Usage: tusker wave show <W-0001>")
	}
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return err
	}
	wave, ok := idx.Waves[id]
	if !ok {
		return tuskerError(errorNotFound, "V7 wave not found: "+id)
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "wave": v7WavePayload(vaultPath, idx, wave)})
		return nil
	}
	fmt.Print(renderV7WaveShow(vaultPath, idx, wave))
	return nil
}

func waveTaskArgs(args Args, start int) []string {
	out := wavePositionals(args, start)
	out = append(out, splitCSV(args.String("tasks"))...)
	out = append(out, splitCSV(args.String("task"))...)
	for i, value := range out {
		out[i] = strings.ToUpper(strings.TrimSpace(wikiTarget(value)))
	}
	return filterStrings(out)
}

func wavePositionals(args Args, start int) []string {
	var out []string
	for i := start; ; i++ {
		value, ok := args[fmt.Sprintf("_pos%d", i)]
		if !ok {
			break
		}
		out = append(out, value)
	}
	return out
}

func validateV7WaveMembership(vaultPath, currentWaveID string, members []string) ([]string, error) {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return nil, err
	}
	var cleaned []string
	seen := map[string]bool{}
	for _, member := range members {
		member = strings.ToUpper(strings.TrimSpace(wikiTarget(member)))
		if member == "" {
			continue
		}
		if !v7TaskIDPattern.MatchString(member) {
			return nil, tuskerError(errorInvalidArg, "invalid V7 wave member task id: "+member)
		}
		if _, ok := idx.Tasks[member]; !ok {
			return nil, tuskerError(errorNotFound, "V7 task not found: "+member)
		}
		if seen[member] {
			return nil, tuskerError(errorInvalidArg, "duplicate V7 wave member: "+member)
		}
		seen[member] = true
		cleaned = append(cleaned, member)
	}
	if len(cleaned) == 0 {
		return nil, tuskerError(errorMissingArg, "wave requires at least one member task")
	}
	for _, wave := range idx.Waves {
		waveID := stringField(wave.Data, "id")
		if waveID == currentWaveID || stringField(wave.Data, "status") != "open" {
			continue
		}
		for _, member := range cleaned {
			if containsString(normalizeList(wave.Data["members"]), member) {
				return nil, tuskerError(errorInvalidArg, fmt.Sprintf("%s already belongs to open wave %s", member, waveID))
			}
		}
	}
	return cleaned, nil
}

func reconcileV7AfterWaveMutation(vaultPath string, args Args) error {
	reconcileArgs := Args{"vault": vaultPath, "quiet": "true"}
	for _, key := range []string{"local", "force"} {
		if args.Bool(key) {
			reconcileArgs[key] = "true"
		}
	}
	return reconcileV7Cmd(reconcileArgs)
}

func nextV7WaveID(vaultPath string) string {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return "W-0001"
	}
	maxSeq := 0
	for id := range idx.Waves {
		if match := v7WaveIDPattern.FindStringSubmatch(id); match != nil {
			maxSeq = maxInt(maxSeq, atoiSafe(match[1]))
		}
	}
	return fmt.Sprintf("W-%s", padNumber(maxSeq+1))
}

func reconcileV7WaveProjections(vaultPath string, idx v7Index, actor, source string) (int, int, error) {
	waveChanges, err := reconcileV7WaveStates(vaultPath, idx, actor, source)
	if err != nil {
		return waveChanges, 0, err
	}
	if waveChanges > 0 {
		idx, err = loadV7Index(vaultPath)
		if err != nil {
			return waveChanges, 0, err
		}
	}
	taskChanges, err := reconcileV7TaskWaveBackPointers(vaultPath, idx, actor, source)
	return waveChanges, taskChanges, err
}

func reconcileV7WaveStates(vaultPath string, idx v7Index, actor, source string) (int, error) {
	changed := 0
	for _, wave := range sortedV7Waves(idx) {
		nextStatus, nextLandedAt := v7DerivedWaveState(idx, wave)
		changes := map[string]any{}
		waveID := ""
		nextRev, updated, err := mutateV7DocumentLocked(wave.AbsolutePath, v7FrontmatterOrder["wave"], func(data map[string]any, body string) (map[string]any, string, bool, error) {
			waveID = stringField(data, "id")
			if stringField(data, "status") != nextStatus {
				changes["status"] = map[string]any{"from": stringField(data, "status"), "to": nextStatus}
				data["status"] = nextStatus
			}
			if nextLandedAt == "" {
				if stringField(data, "landed_at") != "" {
					changes["landed_at"] = map[string]any{"from": stringField(data, "landed_at"), "to": ""}
					delete(data, "landed_at")
				}
			} else if stringField(data, "landed_at") != nextLandedAt {
				changes["landed_at"] = map[string]any{"from": stringField(data, "landed_at"), "to": nextLandedAt}
				data["landed_at"] = nextLandedAt
			}
			if len(changes) == 0 {
				return data, body, false, nil
			}
			data["updated_at"] = time.Now().UTC().Format(time.RFC3339)
			data["updated_by"] = actor
			return data, body, true, nil
		})
		if err != nil {
			return changed, err
		}
		if !updated {
			continue
		}
		if err := emitV7Event(vaultPath, waveID, "wave", "updated", actor, map[string]any{"changes": changes, "source": source, "state_rev": nextRev}); err != nil {
			return changed, err
		}
		changed++
	}
	return changed, nil
}

func reconcileV7TaskWaveBackPointers(vaultPath string, idx v7Index, actor, source string) (int, error) {
	desired := v7DesiredTaskWaveMap(idx)
	changed := 0
	for _, task := range sortedV7Tasks(idx) {
		taskID := stringField(task.Data, "id")
		want := desired[taskID]
		have := ""
		_, updated, err := mutateV7DocumentLocked(task.AbsolutePath, v7FrontmatterOrder["task"], func(data map[string]any, body string) (map[string]any, string, bool, error) {
			have = stringField(data, "wave")
			if have == want {
				return data, body, false, nil
			}
			if want == "" {
				delete(data, "wave")
			} else {
				data["wave"] = want
			}
			data["updated_at"] = time.Now().UTC().Format(time.RFC3339)
			data["updated_by"] = actor
			return data, body, true, nil
		})
		if err != nil {
			return changed, err
		}
		if !updated {
			continue
		}
		if err := emitV7Event(vaultPath, taskID, "task", "updated", actor, map[string]any{"changes": map[string]any{"wave": map[string]any{"from": have, "to": want}}, "source": source}); err != nil {
			return changed, err
		}
		changed++
	}
	return changed, nil
}

func v7DesiredTaskWaveMap(idx v7Index) map[string]string {
	desired := map[string]string{}
	waves := sortedV7Waves(idx)
	for _, wave := range waves {
		if stringField(wave.Data, "status") != "open" {
			continue
		}
		for _, member := range normalizeList(wave.Data["members"]) {
			if desired[member] == "" {
				desired[member] = stringField(wave.Data, "id")
			}
		}
	}
	for _, wave := range waves {
		if stringField(wave.Data, "status") == "open" {
			continue
		}
		for _, member := range normalizeList(wave.Data["members"]) {
			if desired[member] == "" {
				desired[member] = stringField(wave.Data, "id")
			}
		}
	}
	return desired
}

func v7DerivedWaveState(idx v7Index, wave Note) (string, string) {
	members := normalizeList(wave.Data["members"])
	if len(members) == 0 {
		return "open", ""
	}
	landedAt := ""
	for _, member := range members {
		task, ok := idx.Tasks[member]
		if !ok || stringField(task.Data, "status") != "done" {
			return "open", ""
		}
		closedAt := v7TaskClosedAt(task)
		if closedAt > landedAt {
			landedAt = closedAt
		}
	}
	if landedAt == "" {
		landedAt = time.Now().UTC().Format(time.RFC3339)
	}
	return "landed", landedAt
}

func v7TaskClosedAt(task Note) string {
	for _, key := range []string{"closed_at", "accepted_at", "updated_at", "created_at"} {
		if value := stringField(task.Data, key); value != "" {
			return value
		}
	}
	return ""
}

func sortedV7Waves(idx v7Index) []Note {
	waves := make([]Note, 0, len(idx.Waves))
	for _, wave := range idx.Waves {
		waves = append(waves, wave)
	}
	sort.Slice(waves, func(i, j int) bool {
		return stringField(waves[i].Data, "id") < stringField(waves[j].Data, "id")
	})
	return waves
}

func renderV7WaveShow(vaultPath string, idx v7Index, wave Note) string {
	groups := v7WaveMemberGroups(vaultPath, idx, wave)
	var b strings.Builder
	b.WriteString(fmt.Sprintf("# %s · %s\n\n", stringField(wave.Data, "id"), stringField(wave.Data, "title")))
	b.WriteString(fmt.Sprintf("Status: %s", stringField(wave.Data, "status")))
	if landed := stringField(wave.Data, "landed_at"); landed != "" {
		b.WriteString(" (landed " + landed + ")")
	}
	b.WriteString("\n\n")
	auth := waveAuthorizationProjection(vaultPath, idx, wave)
	b.WriteString(fmt.Sprintf("Authorization: %s | action: %s\n\n", stringField(auth, "state"), stringField(auth, "action")))
	for _, group := range v7WaveGroupOrder {
		b.WriteString("## " + group + "\n\n")
		rows := groups[group]
		if len(rows) == 0 {
			b.WriteString("- None.\n\n")
			continue
		}
		for _, row := range rows {
			b.WriteString(fmt.Sprintf("- %s | state: %s | task: %s | proof: %s | %s\n", row.ID, row.State, row.Status, row.Proof, row.Title))
		}
		b.WriteString("\n")
	}
	b.WriteString("## timeline\n\n")
	sequence := 0
	for _, member := range buildArmedWaveSnapshot(vaultPath, idx, wave, v7WaveRuntimeRuns(stringField(wave.Data, "project")), time.Now().UTC()).Members {
		sequence++
		b.WriteString(fmt.Sprintf("- %d | %s | %s | %s\n", sequence, member.ID, member.State, fallback(member.Reason, "no blocker")))
	}
	b.WriteString("\n")
	return b.String()
}

type v7WaveMemberRow struct {
	ID     string
	Title  string
	Status string
	Proof  string
	State  string
	Reason string
}

func v7WaveMemberGroups(vaultPath string, idx v7Index, wave Note) map[string][]v7WaveMemberRow {
	runs := v7WaveRuntimeRuns(stringField(wave.Data, "project"))
	snapshot := buildArmedWaveSnapshot(vaultPath, idx, wave, runs, time.Now().UTC())
	states := map[string]string{}
	reasons := map[string]string{}
	for _, member := range snapshot.Members {
		states[member.ID] = member.State
		reasons[member.ID] = member.Reason
	}
	groups := map[string][]v7WaveMemberRow{}
	for _, member := range normalizeList(wave.Data["members"]) {
		task, ok := idx.Tasks[member]
		if !ok {
			continue
		}
		group := fallback(states[member], armedWaveDependencyWaiting)
		groups[group] = append(groups[group], v7WaveMemberRow{
			ID:     member,
			Title:  stringField(task.Data, "title"),
			Status: stringField(task.Data, "status"),
			Proof:  v7WaveProofLine(vaultPath, idx, task),
			State:  group,
			Reason: reasons[member],
		})
	}
	for _, group := range groups {
		sort.Slice(group, func(i, j int) bool {
			return group[i].ID < group[j].ID
		})
	}
	return groups
}

func v7WaveRuntimeRuns(projectID string) map[string]RunStatus {
	out := map[string]RunStatus{}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return out
	}
	defer store.Close()
	runs, err := store.ListRuns()
	if err != nil {
		return out
	}
	for _, run := range runs {
		if projectID == "" || run.ProjectID == projectID {
			out[firstNonEmpty(run.ItemID, run.RecordID)] = run
		}
	}
	return out
}

func v7WaveTaskGroup(task Note, active map[string]v7LeaseRecord) string {
	id := stringField(task.Data, "id")
	status := strings.ToLower(strings.TrimSpace(stringField(task.Data, "status")))
	readiness := strings.ToLower(strings.TrimSpace(stringField(task.Data, "readiness")))
	if status == "done" {
		return "done"
	}
	if _, ok := active[id]; ok {
		return "running"
	}
	if status == "review" {
		return "review"
	}
	if status == "cancelled" || status == "superseded" || readiness == "held" {
		return "parked"
	}
	if status == "blocked" || strings.HasPrefix(readiness, "blocked") || strings.HasPrefix(readiness, "waiting_on") {
		return "blocked"
	}
	return "ready"
}

func v7WaveProofLine(vaultPath string, idx v7Index, task Note) string {
	status := fallback(stringField(task.Data, "proof_status"), "pending")
	if status == "satisfied" || status == "waived" {
		return status
	}
	report := computeV7ProofReport(vaultPath, task, idx)
	missing := append([]string{}, report.Missing...)
	missing = append(missing, report.ModeMissing...)
	if len(missing) == 0 {
		return status
	}
	if len(missing) > 3 {
		missing = append(missing[:3], "...")
	}
	return status + " (" + strings.Join(missing, ", ") + ")"
}

func v7WavePayload(vaultPath string, idx v7Index, wave Note) map[string]any {
	if stringField(wave.Data, "id") == "" {
		return map[string]any{}
	}
	members := []map[string]any{}
	groups := v7WaveMemberGroups(vaultPath, idx, wave)
	for _, group := range v7WaveGroupOrder {
		for _, row := range groups[group] {
			members = append(members, map[string]any{
				"id":     row.ID,
				"title":  row.Title,
				"group":  group,
				"status": row.Status,
				"proof":  row.Proof,
				"state":  row.State,
				"reason": nullIfBlank(row.Reason),
			})
		}
	}
	timeline := []map[string]any{}
	for i, member := range buildArmedWaveSnapshot(vaultPath, idx, wave, v7WaveRuntimeRuns(stringField(wave.Data, "project")), time.Now().UTC()).Members {
		timeline = append(timeline, map[string]any{"sequence": i + 1, "task": member.ID, "state": member.State, "reason": nullIfBlank(member.Reason)})
	}
	return map[string]any{
		"id":            stringField(wave.Data, "id"),
		"title":         stringField(wave.Data, "title"),
		"status":        stringField(wave.Data, "status"),
		"landedAt":      nullIfBlank(stringField(wave.Data, "landed_at")),
		"members":       members,
		"memberIds":     normalizeList(wave.Data["members"]),
		"authorization": waveAuthorizationProjection(vaultPath, idx, wave),
		"timeline":      timeline,
	}
}
