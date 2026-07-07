package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"tusker/internal/v7policy"

	"gopkg.in/yaml.v3"
)

func inferV7ObjectKind(id string) string {
	switch {
	case v7EvidenceIDPattern.MatchString(id):
		return "evidence"
	case v7AttemptIDPattern.MatchString(id):
		return "attempt"
	case v7TaskIDPattern.MatchString(id):
		return "task"
	case v7GateIDPattern.MatchString(id):
		return "gate"
	case v7DecisionIDPattern.MatchString(id):
		return "decision"
	case v7ProposalIDPattern.MatchString(id):
		return "proposal"
	case epicAcronymPattern.MatchString(id):
		return "epic"
	default:
		return ""
	}
}

func gateV7Cmd(args Args) error {
	switch strings.ToLower(args.String("_pos0")) {
	case "list":
		return gateV7ListCmd(args)
	case "satisfy":
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos1"))
		return gateV7Transition(args, "satisfied")
	case "waive":
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos1"))
		return gateV7Transition(args, "waived")
	case "obsolete":
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos1"))
		return gateV7Transition(args, "obsolete")
	default:
		return tuskerError(errorMissingArg, "Usage: tusker gate list|satisfy|waive|obsolete ...")
	}
}

func gateV7ListCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return err
	}
	var gates []Note
	for _, gate := range idx.Gates {
		if args.Bool("open") && stringField(gate.Data, "status") != "open" {
			continue
		}
		if owner := args.String("owner"); owner != "" && stringField(gate.Data, "owner") != owner {
			continue
		}
		gates = append(gates, gate)
	}
	sort.Slice(gates, func(i, j int) bool { return stringField(gates[i].Data, "id") < stringField(gates[j].Data, "id") })
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "gates": v7NotesPayload(gates)})
		return nil
	}
	for _, gate := range gates {
		fmt.Printf("%s\t%s\t%s\t%s\n", stringField(gate.Data, "id"), stringField(gate.Data, "status"), stringField(gate.Data, "owner"), stringField(gate.Data, "action"))
	}
	return nil
}

func gateV7Transition(args Args, status string) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	if err := ensureV7ControlMutation(vaultPath, args); err != nil {
		return err
	}
	id, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	note, err := resolveV7Note(vaultPath, id, "gate")
	if err != nil {
		return err
	}
	data, body, err := parseFrontmatterMustRead(note.AbsolutePath)
	if err != nil {
		return err
	}
	baseRev := stringField(data, "state_rev")
	prev := stringField(data, "status")
	now := time.Now().UTC().Format(time.RFC3339)
	actor := fallback(args.String("by"), "agent:"+defaultActorName())
	data["status"] = status
	data["updated_at"] = now
	data["updated_by"] = actor
	switch status {
	case "satisfied":
		evidence := strings.TrimSpace(args.String("evidence"))
		evidenceRefs := splitCSV(firstNonEmpty(args.String("evidence-refs"), args.String("evidence-ref")))
		if boolField(data, "blocking") && evidence == "" && len(evidenceRefs) == 0 && !args.Bool("force") {
			return tuskerError(errorMissingArg, "satisfy requires --evidence for blocking gates", withHint("record the proof summary with --evidence, or pass --force only for explicit policy exceptions"))
		}
		if evidence != "" {
			data["satisfaction_evidence"] = evidence
		}
		if len(evidenceRefs) > 0 {
			data["satisfaction_evidence_refs"] = evidenceRefs
		}
		data["satisfied_by"] = actor
		data["satisfied_at"] = now
	case "waived":
		reason := args.String("reason")
		if reason == "" {
			return tuskerError(errorMissingArg, "waive requires --reason")
		}
		data["waived_by"] = actor
		data["waived_at"] = now
		data["waive_reason"] = reason
	case "obsolete":
		reason := args.String("reason")
		if reason == "" {
			return tuskerError(errorMissingArg, "obsolete requires --reason")
		}
		data["obsolete_reason"] = reason
	}
	if _, err := saveV7DocumentCAS(note.AbsolutePath, data, body, v7FrontmatterOrder["gate"], baseRev); err != nil {
		return err
	}
	eventKind := "gate_" + status
	if status == "obsolete" {
		eventKind = "gate_obsoleted"
	}
	if !args.Bool("quiet") {
		fmt.Printf("%s: %s -> %s\n", id, prev, status)
	}
	if err := emitV7Event(vaultPath, id, "gate", eventKind, actor, map[string]any{"from": prev, "to": status, "reason": args.String("reason"), "evidence": args.String("evidence")}); err != nil {
		return err
	}
	affected, err := v7TaskIDsForGateControl(vaultPath, id, normalizeList(data["blocks"]))
	if err != nil {
		return err
	}
	if _, err = reconcileV7ControlProjections(vaultPath, affected, actor, "gate:"+id); err != nil {
		return err
	}
	for _, taskID := range affected {
		if err := updateV7TaskProofStatus(vaultPath, taskID, actor); err != nil {
			return err
		}
	}
	return nil
}

func statusV7Cmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	if err := ensureV7ControlMutation(vaultPath, args); err != nil {
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
	nextStatus = strings.ToLower(nextStatus)
	if _, ok := v7TaskStatuses[nextStatus]; !ok {
		if nextStatus == "active" {
			return tuskerError(errorInvalidField, "invalid V7 task status: "+nextStatus, withHint(v7ProtectedImplementationFlowHint(args)))
		}
		return tuskerError(errorInvalidField, "invalid V7 task status: "+nextStatus)
	}
	if nextStatus == "done" {
		return tuskerError(errorInvalidTransition, "status cannot set done directly; use tusker close so close policy, gates, evidence, and acceptance metadata are enforced")
	}
	note, err := resolveV7Note(vaultPath, id, "task")
	if err != nil {
		return err
	}
	data, body, err := parseFrontmatterMustRead(note.AbsolutePath)
	if err != nil {
		return err
	}
	note.Data = data
	note.Body = body
	if nextStatus == "review" && len(v7PacketStubAcceptanceItems(body)) > 0 && len(v7AcceptanceWaivers(data)) == 0 {
		return tuskerError(errorEvidenceGate, id+": review blocked by placeholder acceptance", withHint("replace stub acceptance with observable outcomes and proof mapping, or record an explicit waiver"))
	}
	baseRev := stringField(data, "state_rev")
	prev := stringField(data, "status")
	actor := fallback(fallback(args.String("actor"), args.String("by")), "agent:"+defaultActorName())
	now := time.Now().UTC().Format(time.RFC3339)
	data["status"] = nextStatus
	data["updated_at"] = now
	data["updated_by"] = actor
	if nextStatus != "done" {
		delete(data, "accepted_by")
		delete(data, "accepted_at")
		delete(data, "closed_at")
	}
	if _, err := saveV7DocumentCAS(note.AbsolutePath, data, body, v7FrontmatterOrder["task"], baseRev); err != nil {
		return err
	}
	if !args.Bool("quiet") {
		fmt.Printf("%s: %s -> %s\n", id, prev, nextStatus)
	}
	if err := emitV7Event(vaultPath, id, "task", "status_changed", actor, map[string]any{"from": prev, "to": nextStatus, "reason": args.String("reason")}); err != nil {
		return err
	}
	affected, err := v7TaskIDsForTaskControl(vaultPath, id)
	if err != nil {
		return err
	}
	_, err = reconcileV7ControlProjections(vaultPath, affected, actor, "task:"+id)
	return err
}

func closeV7Cmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	if err := ensureV7ControlMutation(vaultPath, args); err != nil {
		return err
	}
	id, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return err
	}
	note, ok := idx.Tasks[id]
	if !ok {
		return tuskerError(errorNotFound, "V7 task not found: "+id)
	}
	if prev := stringField(note.Data, "status"); prev != "review" && !args.Bool("force") {
		return tuskerError(
			errorInvalidTransition,
			id+": close requires status review",
			withHint("run `tusker status "+id+" review` on a control/local branch, or `tusker propose status "+id+" --status review` from an implementation branch"),
			withContext(map[string]any{"status": prev}),
		)
	}
	for _, gate := range idx.Gates {
		if stringField(gate.Data, "status") == "open" && boolField(gate.Data, "blocking") && containsString(normalizeList(gate.Data["blocks"]), id) {
			return tuskerError(errorInvalidTransition, id+": close blocked by open gate "+stringField(gate.Data, "id"))
		}
	}
	if dep, blocked := v7UnclosedDependency(note, idx); blocked {
		return tuskerError(errorInvalidTransition, id+": close blocked by unfinished dependency "+dep.ID)
	}
	if missing := missingRequiredEvidence(vaultPath, id, normalizeList(note.Data["evidence_required"])); len(missing) > 0 {
		return tuskerError(errorEvidenceGate, id+": close missing required evidence: "+strings.Join(missing, ", "))
	}
	actor := fallback(fallback(args.String("actor"), args.String("by")), "reviewer:agent")
	if err := enforceV7ClosePolicy(vaultPath, note, idx, actor); err != nil {
		return err
	}
	data, body, err := parseFrontmatterMustRead(note.AbsolutePath)
	if err != nil {
		return err
	}
	currentNote := note
	currentNote.Data = data
	currentNote.Body = body
	if err := enforceV7AcceptanceClose(vaultPath, currentNote, idx); err != nil {
		return err
	}
	baseRev := stringField(data, "state_rev")
	prev := stringField(data, "status")
	now := time.Now().UTC().Format(time.RFC3339)
	data["status"] = "done"
	data["readiness"] = "done"
	if stringField(data, "proof_status") != "waived" {
		data["proof_status"] = "satisfied"
	}
	data["next_owner"] = "none"
	data["next_source"] = "status"
	data["next_ref"] = ""
	data["next_action"] = ""
	data["agent_action"] = ""
	data["machine_status"] = ""
	data["human_status"] = ""
	data["closeout_status"] = ""
	data["accepted_by"] = actor
	data["accepted_at"] = now
	data["closed_at"] = now
	data["updated_at"] = now
	data["updated_by"] = actor
	if _, err := saveV7DocumentCAS(note.AbsolutePath, data, body, v7FrontmatterOrder["task"], baseRev); err != nil {
		return err
	}
	if !args.Bool("quiet") {
		fmt.Printf("%s: %s -> done\n", id, prev)
	}
	if err := emitV7Event(vaultPath, id, "task", "closed", actor, map[string]any{"from": prev, "reason": args.String("reason")}); err != nil {
		return err
	}
	affected, err := v7TaskIDsForTaskControl(vaultPath, id)
	if err != nil {
		return err
	}
	_, err = reconcileV7ControlProjections(vaultPath, affected, actor, "task:"+id)
	return err
}

func enforceV7ClosePolicy(vaultPath string, task Note, idx v7Index, actor string) error {
	id := stringField(task.Data, "id")
	risk := strings.ToLower(fallback(stringField(task.Data, "risk"), "medium"))
	policy, err := v7ClosePolicyFor(vaultPath, risk)
	if err != nil {
		return err
	}
	requiredAcceptor := policy.RequiredAcceptor
	if !v7CloseAcceptorAllowed(actor, requiredAcceptor) {
		if requiredAcceptor == "human" {
			return tuskerError(errorInvalidTransition, id+": close requires human acceptor for "+risk+" risk", withContext(map[string]any{"risk": risk, "actor": actor, "required_acceptor": requiredAcceptor}))
		}
		return tuskerError(errorInvalidTransition, id+": close requires reviewer or human acceptor for "+risk+" risk", withContext(map[string]any{"risk": risk, "actor": actor, "required_acceptor": requiredAcceptor}))
	}
	requiredEvidence := mergeUniqueStrings(normalizeList(task.Data["evidence_required"]), policy.RequiredEvidence)
	if missing := missingRequiredEvidence(vaultPath, id, requiredEvidence); len(missing) > 0 {
		return tuskerError(errorEvidenceGate, id+": close missing risk-policy evidence: "+strings.Join(missing, ", "), withContext(map[string]any{"risk": risk, "missing": missing}))
	}
	for _, kind := range policy.RequiredGates {
		if !v7CloseGateKindSatisfied(idx, id, kind) {
			return tuskerError(errorInvalidTransition, id+": close requires satisfied or waived "+kind+" gate for "+risk+" risk", withContext(map[string]any{"risk": risk, "gate_kind": kind}))
		}
	}
	return nil
}

func enforceV7AcceptanceClose(vaultPath string, task Note, idx v7Index) error {
	if len(v7PacketStubAcceptanceItems(task.Body)) > 0 && len(v7AcceptanceWaivers(task.Data)) == 0 {
		return tuskerError(errorEvidenceGate, stringField(task.Data, "id")+": close blocked by placeholder acceptance", withHint("replace stub acceptance with observable outcomes and proof mapping, or record an explicit waiver"))
	}
	report := computeV7ProofReport(vaultPath, task, idx)
	missing := append([]string{}, report.Missing...)
	missing = append(missing, report.ModeMissing...)
	if len(missing) == 0 || stringField(task.Data, "proof_status") == "waived" {
		return nil
	}
	id := stringField(task.Data, "id")
	return tuskerError(errorEvidenceGate, id+": close proof incomplete: "+strings.Join(missing, ", "), withHint("cover acceptance with inline verification, accepted evidence, satisfied gates, or explicit waivers"))
}

func v7CloseAcceptorAllowed(actor, requiredAcceptor string) bool {
	return v7policy.AcceptorAllowed(actor, requiredAcceptor)
}

func v7ClosePolicyFor(vaultPath, risk string) (v7ClosePolicy, error) {
	risk = strings.ToLower(strings.TrimSpace(risk))
	policy := defaultV7ClosePolicy(risk)
	configPath := filepath.Join(filepath.Dir(vaultPath), "tusker.yaml")
	if !fileExists(configPath) {
		return policy, nil
	}
	raw, err := readText(configPath)
	if err != nil {
		return policy, err
	}
	var cfg v7ClosePolicyConfigFile
	if err := yaml.Unmarshal([]byte(raw), &cfg); err != nil {
		return policy, tuskerError(errorConfigInvalid, "failed to parse tusker.yaml close_policy: "+err.Error(), withPath(configPath))
	}
	rule, ok := cfg.ClosePolicy[risk]
	if !ok {
		return policy, nil
	}
	if strings.TrimSpace(rule.RequiredAcceptor) != "" {
		policy.RequiredAcceptor = strings.TrimSpace(rule.RequiredAcceptor)
	}
	if rule.RequiredEvidence != nil {
		policy.RequiredEvidence = filterStrings(*rule.RequiredEvidence)
	}
	if rule.RequiredGates != nil {
		policy.RequiredGates = filterStrings(*rule.RequiredGates)
	}
	if err := validateV7ClosePolicyConfig(configPath, risk, policy); err != nil {
		return policy, err
	}
	return policy, nil
}
