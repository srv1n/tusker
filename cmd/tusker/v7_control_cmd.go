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

const (
	v7GateHardDependencyIncompleteCode = "GATE_HARD_DEPENDENCY_INCOMPLETE"
	v7GateAuthorityReceiptStaleCode    = "GATE_AUTHORITY_RECEIPT_STALE"
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
	case v7WaveIDPattern.MatchString(id):
		return "wave"
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
	materialLock, err := acquireV7MaterialEpochLock(vaultPath)
	if err != nil {
		return err
	}
	materialLockHeld := true
	defer func() {
		if materialLockHeld {
			_ = materialLock.Close()
		}
	}()
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
	gateKind := strings.ToLower(stringField(data, "gate_kind"))
	if (status == "satisfied" || status == "waived") && (gateKind == "auth" || gateKind == "release") {
		idx, indexErr := loadV7Index(vaultPath)
		if indexErr != nil {
			return indexErr
		}
		fingerprint, incomplete := v7GateHardClosureFingerprint(data, idx)
		if len(incomplete) > 0 {
			return tuskerError(v7GateHardDependencyIncompleteCode, id+": "+gateKind+" gate cannot be satisfied before hard dependency closure completes: "+strings.Join(sortedStrings(incomplete), ", "), withHint("complete or repair the named hard dependencies, then satisfy the gate against current material"))
		}
		data["dependency_material_fingerprint"] = fingerprint
	}
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
	if _, err := saveV7DocumentCASUnderMaterialLock(note.AbsolutePath, data, body, v7FrontmatterOrder["gate"], baseRev); err != nil {
		return err
	}
	closeErr := materialLock.Close()
	materialLockHeld = false
	if closeErr != nil {
		return closeErr
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

func v7GateHardClosureFingerprint(gate map[string]any, idx v7Index) (string, []string) {
	seen := map[string]bool{}
	hardTargets := map[string]bool{}
	var incomplete []string
	var visit func(string)
	visit = func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		task, ok := idx.Tasks[id]
		if !ok {
			incomplete = append(incomplete, id+" (missing)")
			return
		}
		if edge, blocked := v7CrossScopeIntegrityBlocker(task, idx); blocked {
			incomplete = append(incomplete, edge.ID+" (stale material)")
		}
		for _, edge := range v7TaskDependencyEdges(task, idx) {
			if edge.Hardness != v7DependencyHardnessHard {
				continue
			}
			hardTargets[edge.ID] = true
			dep, exists := idx.Tasks[edge.ID]
			if !exists || stringField(dep.Data, "status") != "done" {
				incomplete = append(incomplete, edge.ID)
			}
			visit(edge.ID)
		}
	}
	for _, id := range sortedStrings(normalizeList(gate["blocks"])) {
		visit(id)
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	rows := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		task, ok := idx.Tasks[id]
		if !ok {
			continue
		}
		row := map[string]any{
			"id":           id,
			"contract":     stringField(task.Data, "delivery_contract_fingerprint"),
			"dependencies": sortedStrings(normalizeList(task.Data["dependencies"])),
			"cross_scope":  task.Data["delivery_cross_scope_dependencies"],
		}
		if hardTargets[id] {
			row["material_epoch"] = stringField(task.Data, "state_rev")
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return stringField(rows[i], "id") < stringField(rows[j], "id") })
	raw, _ := yaml.Marshal(rows)
	return deliveryFingerprint(raw), uniqueStrings(incomplete)
}

func v7GateAuthorityReceiptCurrent(gate Note, idx v7Index) bool {
	kind := strings.ToLower(stringField(gate.Data, "gate_kind"))
	if kind != "auth" && kind != "release" {
		return true
	}
	if status := stringField(gate.Data, "status"); status != "satisfied" && status != "waived" {
		return true
	}
	want := stringField(gate.Data, "dependency_material_fingerprint")
	have, incomplete := v7GateHardClosureFingerprint(gate.Data, idx)
	return want != "" && len(incomplete) == 0 && want == have
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
	if nextStatus == "cancelled" {
		return tuskerError(errorInvalidTransition, "status cannot set cancelled directly; use tusker discard so dependencies, gates, runtime rows, and discard history are handled together")
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
	if nextStatus == "review" {
		if err := requireAgentWorkSession(vaultPath, id, actor, args); err != nil {
			return err
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	data["status"] = nextStatus
	data["updated_at"] = now
	data["updated_by"] = actor
	if nextStatus != "done" {
		delete(data, "accepted_by")
		delete(data, "accepted_at")
		delete(data, "closed_at")
		delete(data, "close_authority")
	}
	if nextStatus != "cancelled" {
		delete(data, "discarded_by")
		delete(data, "discarded_at")
		delete(data, "discard_reason")
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
	if canonicalStatusRetiresRuntimeRows(defaultWorkflow(), nextStatus) {
		if _, err := retireCanonicalRuntimeRowsForTask(vaultPath, id, nextStatus, actor, "status change"); err != nil {
			return err
		}
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
	actor := fallback(fallback(args.String("actor"), args.String("by")), "reviewer:agent")
	preflight, err := v7ClosePreflight(vaultPath, note, idx, v7ClosePreflightRequest{
		Args: args, Actor: actor, Action: "close", RequireReview: true, Force: args.Bool("force"), ExpectedTaskID: id,
	})
	if err != nil {
		return err
	}
	data, body := preflight.Task.Data, preflight.Task.Body
	baseRev := stringField(data, "state_rev")
	prev := stringField(data, "status")
	now := time.Now().UTC().Format(time.RFC3339)
	applyV7TaskCloseProjection(data, actor, now, nil)
	if _, err := saveV7CloseProjectionCAS(note.AbsolutePath, data, body, baseRev, id); err != nil {
		return err
	}
	if !args.Bool("quiet") {
		fmt.Printf("%s: %s -> done\n", id, prev)
	}
	if err := emitV7TaskClosedEvent(vaultPath, id, actor, now, prev, args.String("reason"), nil); err != nil {
		return err
	}
	if _, err := retireCanonicalRuntimeRowsForTask(vaultPath, id, "done", "close ceremony", ""); err != nil {
		return err
	}
	if err := removeTaskPlanFile(vaultPath, id); err != nil {
		return err
	}
	affected, err := v7TaskIDsForTaskControl(vaultPath, id)
	if err != nil {
		return err
	}
	_, err = reconcileV7ControlProjections(vaultPath, affected, actor, "task:"+id)
	return err
}

func v7ReviewerIntegratedDependencyIndex(vaultPath string, args Args, task Note, idx v7Index) v7Index {
	branch, err := currentGitBranchIn(v7RepoRoot(vaultPath))
	if err != nil || !v7ReviewerControlMutationAllowed(vaultPath, args, branch) {
		return idx
	}
	wave, ok := idx.Waves[stringField(task.Data, "wave")]
	if !ok {
		return idx
	}
	projected, err := v7CloseDependencyIndexAtRef(vaultPath, v7WaveIntegrationBranch(wave), task, idx)
	// This legacy helper cannot return an error. Callers which select a frozen
	// ref (the close ceremony/reactor) use the error-returning function above;
	// keep the live view here rather than silently manufacturing a projection.
	if err != nil {
		return idx
	}
	return projected
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
