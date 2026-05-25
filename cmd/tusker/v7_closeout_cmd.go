package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func closeoutV7Cmd(args Args) error {
	switch strings.ToLower(args.String("_pos0")) {
	case "status":
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos1"))
		return closeoutV7StatusCmd(args)
	default:
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
		return closeoutV7CreateCmd(args)
	}
}

func closeoutV7StatusCmd(args Args) error {
	vaultPath, task, idx, report, err := loadV7CloseoutContext(args)
	if err != nil {
		return err
	}
	report, _ = v7CloseoutTerminalReport(vaultPath, task, report)
	latest, hasCloseout := latestV7Closeout(idx, stringField(task.Data, "id"))
	fingerprint := v7CloseoutFingerprint(vaultPath, task, idx, latest)
	checkpointValid := false
	packetExists := false
	if hasCloseout {
		checkpointValid = v7CloseoutCheckpointValid(vaultPath, task, idx, latest)
		packetExists = v7CloseoutPacketExists(vaultPath, latest)
	}
	packetPaths := v7CloseoutPacketPaths(latest)
	terminalHumanWait := v7CloseoutStatusTerminalHumanWait(task, report)
	agentAction := "continue"
	if terminalHumanWait {
		agentAction = "stop_until_human_response"
	}
	payload := map[string]any{
		"ok":                   true,
		"task":                 stringField(task.Data, "id"),
		"agent_action":         agentAction,
		"proof_agent_action":   report.AgentAction,
		"terminal_wait":        report.TerminalWait,
		"fingerprint":          fingerprint,
		"fingerprint_matches":  false,
		"checkpoint_exists":    hasCloseout,
		"checkpoint_valid":     checkpointValid,
		"checkpoint_needed":    terminalHumanWait && !checkpointValid,
		"validation_clean":     false,
		"packet_exists":        packetExists,
		"packet_needed":        terminalHumanWait && !packetExists,
		"machine_complete":     v7ProofReportMachineComplete(report),
		"human_action_pending": terminalHumanWait,
		"machine_missing":      report.MachineMissing,
		"human_missing":        report.HumanMissing,
		"reviewer_missing":     report.ReviewerMissing,
		"external_missing":     report.ExternalMissing,
		"open_human_gates":     report.OpenHumanGates,
		"review_packets":       packetPaths,
	}
	if hasCloseout {
		payload["closeout"] = stringField(latest.Data, "id")
		payload["closeout_state"] = stringField(latest.Data, "state")
		payload["fingerprint_matches"] = stringField(latest.Data, "state_fingerprint") == fingerprint
		payload["validation_clean"] = v7CloseoutValidationClean(latest)
	}
	if args.Bool("json") {
		emitJSON(payload)
		return nil
	}
	if terminalHumanWait {
		fmt.Printf("Machine work is complete for %s.\n", stringField(task.Data, "id"))
		fmt.Printf("Human action pending: %s\n", fallback(strings.Join(v7CloseoutHumanPending(report), ", "), "human review"))
		fmt.Println("Agent action: stop_until_human_response")
		if checkpointValid {
			fmt.Printf("Closeout checkpoint: valid (%s)\n", stringField(latest.Data, "id"))
		} else if hasCloseout {
			fmt.Printf("Closeout checkpoint: needed (latest %s is not valid for current state)\n", stringField(latest.Data, "id"))
		} else {
			fmt.Println("Closeout checkpoint: needed")
		}
		if packetExists {
			fmt.Printf("Review packet: present (%s)\n", fallback(strings.Join(packetPaths, ", "), "recorded"))
		} else {
			fmt.Println("Review packet: needed")
		}
		return nil
	}
	fmt.Printf("%s closeout status: terminal_wait=%t machine_complete=%t checkpoint_valid=%t\n", stringField(task.Data, "id"), report.TerminalWait, v7ProofReportMachineComplete(report), checkpointValid)
	return nil
}

func closeoutV7CreateCmd(args Args) error {
	vaultPath, task, idx, report, err := loadV7CloseoutContext(args)
	if err != nil {
		return err
	}
	report, terminalWait := v7CloseoutTerminalReport(vaultPath, task, report)
	if !terminalWait {
		if len(report.MachineMissing) > 0 || len(report.OpenMachineGates) > 0 || len(report.ReviewerMissing) > 0 || len(report.OpenReviewerGates) > 0 || len(report.ExternalMissing) > 0 || len(report.OpenExternalGates) > 0 {
			return tuskerError(errorEvidenceGate, stringField(task.Data, "id")+": closeout refused; machine/reviewer/external gaps remain", withContext(map[string]any{
				"machine_missing":     report.MachineMissing,
				"open_machine_gates":  report.OpenMachineGates,
				"reviewer_missing":    report.ReviewerMissing,
				"open_reviewer_gates": report.OpenReviewerGates,
				"external_missing":    report.ExternalMissing,
				"open_external_gates": report.OpenExternalGates,
				"human_missing":       report.HumanMissing,
				"open_human_gates":    report.OpenHumanGates,
			}))
		}
		return tuskerError(errorInvalidTransition, stringField(task.Data, "id")+": closeout requires human-owned pending proof, gates, or human close policy")
	}
	validateCommand := strings.TrimSpace(args.String("validate"))
	if validateCommand == "" || validateCommand == "true" {
		return tuskerError(errorMissingArg, stringField(task.Data, "id")+": closeout requires --validate <command>", withHint("record a clean validation snapshot before emitting a terminal human-wait checkpoint"))
	}
	if !args.Bool("emit-packet") {
		return tuskerError(errorMissingArg, stringField(task.Data, "id")+": closeout requires --emit-packet", withHint("terminal human-wait checkpoints must leave a durable review packet"))
	}
	validation, err := runV7CloseoutValidation(vaultPath, validateCommand)
	if err != nil {
		return err
	}
	idx, err = loadV7Index(vaultPath)
	if err != nil {
		return err
	}
	task, ok := idx.Tasks[stringField(task.Data, "id")]
	if !ok {
		return tuskerError(errorNotFound, "V7 task not found after validation: "+stringField(task.Data, "id"))
	}
	report = computeV7ProofReport(vaultPath, task, idx)
	report, terminalWait = v7CloseoutTerminalReport(vaultPath, task, report)
	if !terminalWait {
		return tuskerError(errorEvidenceGate, stringField(task.Data, "id")+": closeout refused; terminal human-wait state changed during validation", withContext(map[string]any{
			"machine_missing":      report.MachineMissing,
			"open_machine_gates":   report.OpenMachineGates,
			"reviewer_missing":     report.ReviewerMissing,
			"open_reviewer_gates":  report.OpenReviewerGates,
			"external_missing":     report.ExternalMissing,
			"open_external_gates":  report.OpenExternalGates,
			"human_missing":        report.HumanMissing,
			"open_human_gates":     report.OpenHumanGates,
			"close_policy_human":   v7ClosePolicyHumanWait(vaultPath, task, report),
			"validation_command":   validateCommand,
			"validation_succeeded": true,
		}))
	}
	var packetPaths []string
	packetRel := filepath.ToSlash(filepath.Join(filepath.Base(vaultPath), "_generated", "packets", stringField(task.Data, "id")+".reviewer.md"))
	packetPath := filepath.Join(vaultPath, "_generated", "packets", stringField(task.Data, "id")+".reviewer.md")
	if err := writeText(packetPath, v7Packet(vaultPath, task, idx, "reviewer")); err != nil {
		return err
	}
	packetPaths = append(packetPaths, packetRel)
	actor := fallback(args.String("by"), "agent:"+defaultActorName())
	closeoutID := nextV7CloseoutID(vaultPath, stringField(task.Data, "id"))
	preparedTask, taskBaseRev, err := prepareV7TaskForHumanWait(task, idx, report, actor, closeoutID)
	if err != nil {
		return err
	}
	preparedIdx := v7IndexWithTask(idx, preparedTask)
	preparedReport := computeV7ProofReport(vaultPath, preparedTask, preparedIdx)
	preparedReport, _ = v7CloseoutTerminalReport(vaultPath, preparedTask, preparedReport)
	closeout, err := writeV7CloseoutCheckpoint(vaultPath, closeoutID, preparedTask, preparedIdx, preparedReport, packetPaths, validation, actor)
	if err != nil {
		return err
	}
	if err := writePreparedV7TaskForHumanWait(vaultPath, preparedTask, taskBaseRev, actor, closeoutID); err != nil {
		_ = os.Remove(closeout.AbsolutePath)
		return err
	}
	if err := emitV7Event(vaultPath, closeoutID, "closeout", "created", actor, map[string]any{"task": stringField(preparedTask.Data, "id")}); err != nil {
		return err
	}
	task = preparedTask
	idx = preparedIdx
	report = preparedReport
	if err := releaseV7ActiveLeaseForCloseout(vaultPath, stringField(task.Data, "id")); err != nil {
		return err
	}
	payload := map[string]any{
		"ok":               true,
		"task":             stringField(task.Data, "id"),
		"closeout":         stringField(closeout.Data, "id"),
		"closeout_state":   "machine_complete_waiting_for_human",
		"agent_action":     "stop_until_human_response",
		"validation":       validation,
		"fingerprint":      stringField(closeout.Data, "state_fingerprint"),
		"machine_missing":  report.MachineMissing,
		"human_missing":    report.HumanMissing,
		"open_human_gates": report.OpenHumanGates,
		"review_packets":   packetPaths,
	}
	if args.Bool("json") {
		emitJSON(payload)
		return nil
	}
	if !args.Bool("quiet") {
		fmt.Printf("%s closeout emitted; agent_action=stop_until_human_response\n", stringField(task.Data, "id"))
	}
	return nil
}

func loadV7CloseoutContext(args Args) (string, Note, v7Index, v7ProofReport, error) {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return "", Note{}, v7Index{}, v7ProofReport{}, err
	}
	taskID, err := requireArg(args, "id")
	if err != nil {
		return "", Note{}, v7Index{}, v7ProofReport{}, err
	}
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return "", Note{}, v7Index{}, v7ProofReport{}, err
	}
	task, ok := idx.Tasks[taskID]
	if !ok {
		return "", Note{}, v7Index{}, v7ProofReport{}, tuskerError(errorNotFound, "V7 task not found: "+taskID)
	}
	report := computeV7ProofReport(vaultPath, task, idx)
	return vaultPath, task, idx, report, nil
}

func runV7CloseoutValidation(vaultPath, command string) (map[string]any, error) {
	command = strings.TrimSpace(command)
	result := map[string]any{
		"result":      "skipped",
		"cached":      false,
		"fingerprint": "",
	}
	if command == "" {
		return result, nil
	}
	started := time.Now().UTC()
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = filepath.Dir(vaultPath)
	out, err := cmd.CombinedOutput()
	result["command"] = command
	result["validated_at"] = started.Format(time.RFC3339)
	result["output_tail"] = tailString(strings.TrimSpace(string(out)), 20)
	if err != nil {
		result["result"] = "fail"
		return result, tuskerError(errorEvidenceGate, "closeout validation failed: "+command, withContext(result))
	}
	result["result"] = "pass"
	return result, nil
}

func writeV7CloseoutCheckpoint(vaultPath, id string, task Note, idx v7Index, report v7ProofReport, packetPaths []string, validation map[string]any, actor string) (Note, error) {
	taskID := stringField(task.Data, "id")
	now := time.Now().UTC().Format(time.RFC3339)
	data := map[string]any{
		"schema":            "tusker.closeout/v1",
		"kind":              "closeout",
		"id":                id,
		"project":           v7ProjectID(vaultPath),
		"task":              taskID,
		"state":             "machine_complete_waiting_for_human",
		"agent_action":      "stop_until_human_response",
		"state_fingerprint": "",
		"machine_missing":   report.MachineMissing,
		"human_missing":     report.HumanMissing,
		"reviewer_missing":  report.ReviewerMissing,
		"external_missing":  report.ExternalMissing,
		"open_human_gates":  report.OpenHumanGates,
		"review_packet":     packetPaths,
		"validation":        validation,
		"created_by":        actor,
		"created_at":        now,
	}
	data["state_fingerprint"] = v7CloseoutFingerprint(vaultPath, task, idx, Note{Data: data})
	validation["fingerprint"] = data["state_fingerprint"]
	body := fmt.Sprintf("# %s\n\nMachine work is complete. Remaining blockers are human-owned.\n\nValidation: %s\n", id, toString(validation["result"]))
	data["state_rev"] = v7StateRev(data, body)
	path := filepath.Join(vaultPath, "work", "closeouts", id+".md")
	content, err := serializeDocument(data, body, v7FrontmatterOrder["closeout"])
	if err != nil {
		return Note{}, err
	}
	if err := writeText(path, content); err != nil {
		return Note{}, err
	}
	return Note{AbsolutePath: path, RelativePath: filepath.ToSlash(filepath.Join("work", "closeouts", id+".md")), Data: data, Body: body}, nil
}

func prepareV7TaskForHumanWait(task Note, idx v7Index, report v7ProofReport, actor, closeoutID string) (Note, string, error) {
	data, body, err := parseFrontmatterMustRead(task.AbsolutePath)
	if err != nil {
		return Note{}, "", err
	}
	baseRev := stringField(data, "state_rev")
	next := v7HumanWaitProjection(task, report, idx, "closeout:"+closeoutID)
	for key, value := range next {
		data[key] = value
	}
	if status := stringField(data, "status"); status != "done" && status != "cancelled" && status != "superseded" {
		data["status"] = "review"
	}
	data["machine_status"] = "complete"
	data["human_status"] = "pending"
	data["closeout_status"] = "machine_complete_waiting_for_human"
	data["updated_at"] = time.Now().UTC().Format(time.RFC3339)
	data["updated_by"] = actor
	data["state_rev"] = v7StateRev(data, body)
	prepared := task
	prepared.Data = data
	prepared.Body = body
	return prepared, baseRev, nil
}

func writePreparedV7TaskForHumanWait(vaultPath string, task Note, baseRev, actor, closeoutID string) error {
	if _, err := saveV7DocumentCAS(task.AbsolutePath, task.Data, task.Body, v7FrontmatterOrder["task"], baseRev); err != nil {
		return err
	}
	return emitV7Event(vaultPath, stringField(task.Data, "id"), "task", "updated", actor, map[string]any{"source": "closeout", "closeout": closeoutID})
}

func v7IndexWithTask(idx v7Index, task Note) v7Index {
	tasks := map[string]Note{}
	for id, existing := range idx.Tasks {
		tasks[id] = existing
	}
	tasks[stringField(task.Data, "id")] = task
	idx.Tasks = tasks
	return idx
}

func releaseV7ActiveLeaseForCloseout(vaultPath, taskID string) error {
	store := v7FileRuntimeStore{VaultPath: vaultPath}
	lease, err := store.findActiveLease(context.Background(), taskID)
	if err != nil {
		return nil
	}
	updated, err := store.writeLease(context.Background(), lease.Task, lease.ID, lease.Owner, lease.Workspace, lease.Branch, "released", 0)
	if err != nil {
		return err
	}
	return emitV7Event(vaultPath, updated.Task, "task", "claim_released", updated.Owner, map[string]any{"lease": updated.ID, "source": "closeout"})
}

func latestV7Closeout(idx v7Index, taskID string) (Note, bool) {
	closeouts := append([]Note{}, idx.Closeouts[taskID]...)
	if len(closeouts) == 0 {
		return Note{}, false
	}
	sort.Slice(closeouts, func(i, j int) bool {
		return stringField(closeouts[i].Data, "id") < stringField(closeouts[j].Data, "id")
	})
	return closeouts[len(closeouts)-1], true
}

func nextV7CloseoutID(vaultPath, taskID string) string {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return taskID + "-C-0001"
	}
	maxSeq := 0
	for _, closeout := range idx.Closeouts[taskID] {
		id := stringField(closeout.Data, "id")
		if strings.HasPrefix(id, taskID+"-C-") {
			maxSeq = maxInt(maxSeq, atoiSafe(strings.TrimPrefix(id, taskID+"-C-")))
		}
	}
	return fmt.Sprintf("%s-C-%s", taskID, padNumber(maxSeq+1))
}

const v7CloseoutFingerprintVersion = "tusker.closeout-fingerprint/v2"

func v7CloseoutFingerprint(vaultPath string, task Note, idx v7Index, closeout Note) string {
	taskID := stringField(task.Data, "id")
	report := computeV7ProofReport(vaultPath, task, idx)
	report, closeoutTerminal := v7CloseoutTerminalReport(vaultPath, task, report)
	closePolicy, err := v7ClosePolicyFor(vaultPath, strings.ToLower(fallback(stringField(task.Data, "risk"), "medium")))
	if err != nil {
		closePolicy = defaultV7ClosePolicy(strings.ToLower(fallback(stringField(task.Data, "risk"), "medium")))
	}
	payload := map[string]any{
		"fingerprint_version": v7CloseoutFingerprintVersion,
		"tusker_schema":       "v7",
		"closeout_terminal":   closeoutTerminal,
		"close_policy": map[string]any{
			"required_acceptor": closePolicy.RequiredAcceptor,
			"required_evidence": closePolicy.RequiredEvidence,
			"required_gates":    closePolicy.RequiredGates,
		},
		"task": map[string]any{
			"path":      task.RelativePath,
			"data":      task.Data,
			"body_hash": v7StateRev(map[string]any{}, task.Body),
			"state_rev": stringField(task.Data, "state_rev"),
		},
		"proof_report": map[string]any{
			"status":               report.Status,
			"terminal_wait":        report.TerminalWait,
			"machine_missing":      report.MachineMissing,
			"human_missing":        report.HumanMissing,
			"reviewer_missing":     report.ReviewerMissing,
			"external_missing":     report.ExternalMissing,
			"open_machine_gates":   report.OpenMachineGates,
			"open_human_gates":     report.OpenHumanGates,
			"open_reviewer_gates":  report.OpenReviewerGates,
			"open_external_gates":  report.OpenExternalGates,
			"satisfied_gates":      report.SatisfiedGates,
			"mode_missing":         report.ModeMissing,
			"acceptance_missing":   report.Missing,
			"agent_action":         report.AgentAction,
			"proof_required_owner": report.ProofOwner,
		},
		"gates":      v7CloseoutRelatedNoteRevs(v7CloseoutRelatedGates(taskID, idx)),
		"evidence":   v7CloseoutRelatedNoteRevs(idx.Evidence[taskID]),
		"attempts":   v7CloseoutRelatedNoteRevs(idx.Attempts[taskID]),
		"repo_state": v7CloseoutRepoState(vaultPath),
		"validation": v7CloseoutValidationFingerprint(closeout),
		"packets":    v7CloseoutPacketFingerprint(vaultPath, closeout),
	}
	return v7StateRev(payload, "")
}

func v7CloseoutCheckpointValid(vaultPath string, task Note, idx v7Index, closeout Note) bool {
	if stringField(closeout.Data, "state") != "machine_complete_waiting_for_human" {
		return false
	}
	if stringField(closeout.Data, "agent_action") != "stop_until_human_response" {
		return false
	}
	if !v7CloseoutValidationClean(closeout) || !v7CloseoutPacketExists(vaultPath, closeout) {
		return false
	}
	report := computeV7ProofReport(vaultPath, task, idx)
	report, terminalWait := v7CloseoutTerminalReport(vaultPath, task, report)
	if !terminalWait || !v7ProofReportMachineComplete(report) {
		return false
	}
	return stringField(closeout.Data, "state_fingerprint") == v7CloseoutFingerprint(vaultPath, task, idx, closeout)
}

func v7CloseoutTerminalReport(vaultPath string, task Note, report v7ProofReport) (v7ProofReport, bool) {
	if report.TerminalWait {
		return report, true
	}
	if !v7ClosePolicyHumanWait(vaultPath, task, report) {
		return report, false
	}
	report.TerminalWait = true
	report.AgentAction = "stop_until_human_response"
	report.HumanMissing = uniqueStrings(append(report.HumanMissing, "close_policy:human_acceptor"))
	return report, true
}

func v7CloseoutStatusTerminalHumanWait(task Note, report v7ProofReport) bool {
	if !report.TerminalWait || !v7ProofReportMachineComplete(report) {
		return false
	}
	switch stringField(task.Data, "status") {
	case "rework", "done", "cancelled", "superseded":
		return false
	default:
		return true
	}
}

func v7CloseoutHumanPending(report v7ProofReport) []string {
	return uniqueStrings(append(append([]string{}, report.HumanMissing...), report.OpenHumanGates...))
}

func v7ClosePolicyHumanWait(vaultPath string, task Note, report v7ProofReport) bool {
	if stringField(task.Data, "status") != "review" || !v7ProofReportMachineComplete(report) {
		return false
	}
	policy, err := v7ClosePolicyFor(vaultPath, strings.ToLower(fallback(stringField(task.Data, "risk"), "medium")))
	if err != nil {
		policy = defaultV7ClosePolicy(strings.ToLower(fallback(stringField(task.Data, "risk"), "medium")))
	}
	return policy.RequiredAcceptor == "human"
}

func v7LatestValidTerminalCloseout(vaultPath string, task Note, idx v7Index) (Note, bool) {
	if stringField(task.Data, "closeout_status") != "machine_complete_waiting_for_human" {
		return Note{}, false
	}
	closeout, ok := latestV7Closeout(idx, stringField(task.Data, "id"))
	if !ok {
		return Note{}, false
	}
	if !v7CloseoutCheckpointValid(vaultPath, task, idx, closeout) {
		return Note{}, false
	}
	return closeout, true
}

func v7CloseoutValidationClean(closeout Note) bool {
	validation := mapStringAny(closeout.Data["validation"])
	return strings.TrimSpace(toString(validation["command"])) != "" && strings.EqualFold(strings.TrimSpace(toString(validation["result"])), "pass")
}

func v7CloseoutPacketExists(vaultPath string, closeout Note) bool {
	paths := v7CloseoutPacketPaths(closeout)
	if len(paths) == 0 {
		return false
	}
	if strings.TrimSpace(vaultPath) == "" {
		return true
	}
	for _, packet := range paths {
		if !fileExists(v7CloseoutPacketAbsPath(vaultPath, packet)) {
			return false
		}
	}
	return true
}

func v7CloseoutRelatedGates(taskID string, idx v7Index) []Note {
	var gates []Note
	for _, gate := range idx.Gates {
		if v7GateTouchesTask(gate, taskID) || containsString(normalizeList(gate.Data["blocks"]), taskID) {
			gates = append(gates, gate)
		}
	}
	return gates
}

func v7CloseoutRelatedNoteRevs(notes []Note) []map[string]string {
	notes = append([]Note{}, notes...)
	sort.Slice(notes, func(i, j int) bool { return stringField(notes[i].Data, "id") < stringField(notes[j].Data, "id") })
	var out []map[string]string
	for _, note := range notes {
		out = append(out, map[string]string{
			"id":        stringField(note.Data, "id"),
			"kind":      effectiveV7Kind(note.Data),
			"path":      note.RelativePath,
			"status":    stringField(note.Data, "status"),
			"state_rev": stringField(note.Data, "state_rev"),
			"body_hash": v7StateRev(map[string]any{}, note.Body),
		})
	}
	return out
}

func v7CloseoutValidationFingerprint(closeout Note) map[string]any {
	validation := mapStringAny(closeout.Data["validation"])
	out := map[string]any{}
	for _, key := range []string{"command", "result", "validated_at", "output_tail", "cached"} {
		if value, ok := validation[key]; ok {
			out[key] = value
		}
	}
	return out
}

func v7CloseoutPacketFingerprint(vaultPath string, closeout Note) []map[string]any {
	var out []map[string]any
	for _, packet := range v7CloseoutPacketPaths(closeout) {
		item := map[string]any{"path": packet}
		if strings.TrimSpace(vaultPath) != "" {
			abs := v7CloseoutPacketAbsPath(vaultPath, packet)
			item["exists"] = fileExists(abs)
			if text, err := readText(abs); err == nil {
				item["content_hash"] = v7StateRev(map[string]any{}, text)
			}
		}
		out = append(out, item)
	}
	return out
}

func v7CloseoutPacketAbsPath(vaultPath, packet string) string {
	packet = strings.TrimSpace(packet)
	if filepath.IsAbs(packet) {
		return packet
	}
	repoRoot := filepath.Dir(vaultPath)
	candidate := filepath.Join(repoRoot, filepath.FromSlash(packet))
	if fileExists(candidate) {
		return candidate
	}
	if strings.HasPrefix(filepath.ToSlash(packet), filepath.Base(vaultPath)+"/") {
		return candidate
	}
	return filepath.Join(vaultPath, filepath.FromSlash(packet))
}

func v7CloseoutRepoState(vaultPath string) map[string]any {
	state := map[string]any{"vcs": "none"}
	if strings.TrimSpace(vaultPath) == "" {
		return state
	}
	repoRoot := filepath.Dir(vaultPath)
	if !dirExists(filepath.Join(repoRoot, ".git")) && !fileExists(filepath.Join(repoRoot, ".git")) {
		return state
	}
	state["vcs"] = "git"
	if out, err := exec.Command("git", "-C", repoRoot, "rev-parse", "HEAD").CombinedOutput(); err == nil {
		state["head"] = strings.TrimSpace(string(out))
	} else {
		state["head_error"] = strings.TrimSpace(string(out))
	}
	vaultRel, err := filepath.Rel(repoRoot, vaultPath)
	if err != nil {
		vaultRel = filepath.Base(vaultPath)
	}
	vaultRel = filepath.ToSlash(vaultRel)
	pathspec := v7CloseoutRepoPathspec(vaultRel)
	statusArgs := append([]string{"-C", repoRoot, "status", "--porcelain=v1", "--untracked-files=all"}, pathspec...)
	if out, err := exec.Command("git", statusArgs...).CombinedOutput(); err == nil {
		state["status"] = strings.TrimSpace(string(out))
	} else {
		state["status_error"] = strings.TrimSpace(string(out))
	}
	state["diff"] = v7CloseoutGitOutputHash(repoRoot, append([]string{"diff", "--binary"}, pathspec...))
	state["staged_diff"] = v7CloseoutGitOutputHash(repoRoot, append([]string{"diff", "--cached", "--binary"}, pathspec...))
	state["untracked"] = v7CloseoutUntrackedFileHashes(repoRoot, pathspec)
	return state
}

func v7CloseoutRepoPathspec(vaultRel string) []string {
	pathspec := []string{"--", "."}
	if vaultRel != "" && vaultRel != "." {
		pathspec = append(pathspec, ":(exclude)"+vaultRel+"/**")
	}
	return pathspec
}

func v7CloseoutGitOutputHash(repoRoot string, args []string) map[string]any {
	out, err := exec.Command("git", append([]string{"-C", repoRoot}, args...)...).CombinedOutput()
	result := map[string]any{
		"args":  args,
		"hash":  v7CloseoutRawHash(out),
		"bytes": len(out),
	}
	if err != nil {
		result["error"] = strings.TrimSpace(string(out))
	}
	return result
}

func v7CloseoutUntrackedFileHashes(repoRoot string, pathspec []string) []map[string]any {
	args := append([]string{"-C", repoRoot, "ls-files", "--others", "--exclude-standard", "-z"}, pathspec...)
	out, err := exec.Command("git", args...).CombinedOutput()
	if err != nil {
		return []map[string]any{{"error": strings.TrimSpace(string(out))}}
	}
	var files []map[string]any
	for _, rel := range strings.Split(string(out), "\x00") {
		rel = strings.TrimSpace(rel)
		if rel == "" {
			continue
		}
		abs := filepath.Join(repoRoot, filepath.FromSlash(rel))
		info, err := os.Stat(abs)
		if err != nil || info.IsDir() {
			continue
		}
		raw, err := os.ReadFile(abs)
		item := map[string]any{"path": filepath.ToSlash(rel), "size": info.Size()}
		if err != nil {
			item["error"] = err.Error()
		} else {
			item["hash"] = v7CloseoutRawHash(raw)
		}
		files = append(files, item)
	}
	sort.Slice(files, func(i, j int) bool { return toString(files[i]["path"]) < toString(files[j]["path"]) })
	return files
}

func v7CloseoutRawHash(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func mapStringAny(value any) map[string]any {
	switch v := value.(type) {
	case map[string]any:
		return v
	case map[any]any:
		out := map[string]any{}
		for key, item := range v {
			out[toString(key)] = item
		}
		return out
	default:
		return map[string]any{}
	}
}

func v7CloseoutPacketPaths(closeout Note) []string {
	return normalizeList(closeout.Data["review_packet"])
}

func tailString(text string, maxLines int) string {
	if maxLines <= 0 {
		return ""
	}
	lines := strings.Split(text, "\n")
	if len(lines) <= maxLines {
		return text
	}
	return strings.Join(lines[len(lines)-maxLines:], "\n")
}
