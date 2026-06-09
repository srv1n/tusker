package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func proposalV7Cmd(args Args) error {
	switch strings.ToLower(args.String("_pos0")) {
	case "list":
		return proposalV7ListCmd(args)
	case "accept", "accepted":
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos1"))
		return proposalV7TransitionCmd(args, "accepted")
	case "reject", "rejected":
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos1"))
		return proposalV7TransitionCmd(args, "rejected")
	case "apply":
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos1"))
		return proposalV7ApplyCmd(args)
	case "new":
		args["target"] = firstNonEmpty(args.String("target"), args.String("_pos1"))
		args["action"] = fallback(args.String("action"), "change")
		return proposalV7NewCmd(args)
	case "close", "status", "change", "create_task", "create_gate", "create_decision":
		args["action"] = args.String("_pos0")
		args["target"] = firstNonEmpty(args.String("target"), args.String("_pos1"))
		return proposalV7NewCmd(args)
	default:
		return tuskerError(errorMissingArg, "Usage: tusker propose close <task-id> | tusker propose status <task-id> --status review | tusker proposal accept|reject|apply <proposal-id> | tusker proposal list")
	}
}

func proposalV7NewCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	action := strings.ToLower(fallback(args.String("action"), "change"))
	if _, ok := v7ProposalAction[action]; !ok {
		return tuskerError(errorInvalidField, "invalid V7 proposal action: "+action)
	}
	target := strings.ToUpper(firstNonEmpty(args.String("target"), args.String("id")))
	if target == "" && !strings.HasPrefix(action, "create_") {
		return tuskerError(errorMissingArg, "proposal requires a target id")
	}
	targetKind := strings.ToLower(firstNonEmpty(args.String("target-kind"), v7ProposalTargetKind(target, action)))
	epic := strings.ToUpper(firstNonEmpty(args.String("epic"), v7EpicFromProposalTarget(target)))
	if epic == "" || !epicAcronymPattern.MatchString(epic) {
		return tuskerError(errorInvalidArg, "proposal requires --epic or a target id with an ABC prefix")
	}
	if strings.HasPrefix(action, "create_") && target == "" {
		target = epic
		targetKind = "epic"
	}
	id := strings.ToUpper(args.String("proposal-id"))
	if id == "" {
		id = fmt.Sprintf("%s-P-%s", epic, padNumber(nextV7Sequence(vaultPath, epic, "proposal")))
	}
	if !v7ProposalIDPattern.MatchString(id) {
		return tuskerError(errorInvalidArg, "invalid V7 proposal id: "+id)
	}
	fields, err := v7ProposalFields(args, action)
	if err != nil {
		return err
	}
	path := filepath.Join(vaultPath, "work", "inbox", id+".md")
	if fileExists(path) {
		return tuskerError(errorAlreadyExists, "Proposal already exists: "+id, withPath(path))
	}
	now := time.Now().UTC().Format(time.RFC3339)
	title := fallback(args.String("title"), v7ProposalDefaultTitle(action, target))
	actor := fallback(args.String("by"), "agent:"+defaultActorName())
	data := map[string]any{
		"schema":          "tusker.proposal/v1",
		"kind":            "proposal",
		"id":              id,
		"project":         v7ProjectID(vaultPath),
		"title":           title,
		"status":          "proposed",
		"action":          action,
		"target_kind":     targetKind,
		"target":          target,
		"proposed_fields": fields,
		"proposed_by":     actor,
		"source_branch":   firstNonEmpty(args.String("branch"), currentGitBranch()),
		"created_at":      now,
		"updated_at":      now,
	}
	body := v7ProposalBody(id, title, action, target, fields, args)
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["proposal"])
	if err != nil {
		return err
	}
	if err := writeText(path, content); err != nil {
		return err
	}
	if !args.Bool("quiet") {
		fmt.Printf("Created proposal %s at %s\n", id, path)
	}
	return emitV7Event(vaultPath, id, "proposal", "created", actor, map[string]any{"action": action, "target": target})
}

func proposalV7ListCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return err
	}
	var proposals []Note
	for _, proposal := range idx.Proposals {
		if status := args.String("status"); status != "" && stringField(proposal.Data, "status") != strings.ToLower(status) {
			continue
		}
		if target := strings.ToUpper(args.String("target")); target != "" && stringField(proposal.Data, "target") != target {
			continue
		}
		if action := strings.ToLower(args.String("action")); action != "" && stringField(proposal.Data, "action") != action {
			continue
		}
		proposals = append(proposals, proposal)
	}
	sort.Slice(proposals, func(i, j int) bool {
		return stringField(proposals[i].Data, "id") < stringField(proposals[j].Data, "id")
	})
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "proposals": v7NotesPayload(proposals)})
		return nil
	}
	for _, proposal := range proposals {
		fmt.Printf("%s\t%s\t%s\t%s\t%s\n", stringField(proposal.Data, "id"), stringField(proposal.Data, "status"), stringField(proposal.Data, "action"), stringField(proposal.Data, "target"), stringField(proposal.Data, "title"))
	}
	return nil
}

func proposalV7TransitionCmd(args Args, status string) error {
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
	id = strings.ToUpper(id)
	note, err := resolveV7Note(vaultPath, id, "proposal")
	if err != nil {
		return err
	}
	data, body, err := parseFrontmatterMustRead(note.AbsolutePath)
	if err != nil {
		return err
	}
	if _, ok := v7ProposalStatus[status]; !ok || status == "proposed" {
		return tuskerError(errorInvalidField, "invalid V7 proposal transition target: "+status)
	}
	prev := stringField(data, "status")
	if prev != "proposed" {
		return tuskerError(errorInvalidTransition, id+": proposal transition requires status proposed", withContext(map[string]any{"status": prev}))
	}
	reason := args.String("reason")
	if status == "rejected" && reason == "" {
		return tuskerError(errorMissingArg, "proposal reject requires --reason")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	actor := fallback(args.String("by"), "human:"+defaultActorName())
	if status == "accepted" && !args.Bool("self-review-ok") && actor == stringField(data, "proposed_by") {
		return tuskerError(
			errorInvalidTransition,
			id+": proposal acceptance requires an independent reviewer",
			withHint("pass --by human:<name> or --by reviewer:<name>; use --self-review-ok only when policy explicitly permits it"),
			withContext(map[string]any{"proposed_by": stringField(data, "proposed_by"), "reviewed_by": actor}),
		)
	}
	baseRev := stringField(data, "state_rev")
	data["status"] = status
	data["reviewed_by"] = actor
	data["reviewed_at"] = now
	data["updated_by"] = actor
	data["updated_at"] = now
	if reason != "" {
		data["review_reason"] = reason
	}
	if _, err := saveV7DocumentCAS(note.AbsolutePath, data, body, v7FrontmatterOrder["proposal"], baseRev); err != nil {
		return err
	}
	if !args.Bool("quiet") {
		fmt.Printf("%s: %s -> %s\n", id, prev, status)
	}
	return emitV7Event(vaultPath, id, "proposal", "updated", actor, map[string]any{"from": prev, "to": status, "reason": reason})
}

func proposalV7ApplyCmd(args Args) error {
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
	id = strings.ToUpper(id)
	note, err := resolveV7Note(vaultPath, id, "proposal")
	if err != nil {
		return err
	}
	data, body, err := parseFrontmatterMustRead(note.AbsolutePath)
	if err != nil {
		return err
	}
	if stringField(data, "status") != "accepted" {
		return tuskerError(errorInvalidTransition, id+": proposal apply requires status accepted", withContext(map[string]any{"status": stringField(data, "status")}))
	}
	if stringField(data, "applied_at") != "" {
		return tuskerError(errorInvalidTransition, id+": proposal is already applied", withContext(map[string]any{"applied_at": stringField(data, "applied_at")}))
	}
	actor := fallback(args.String("by"), "human:"+defaultActorName())
	now := time.Now().UTC().Format(time.RFC3339)
	baseRev := stringField(data, "state_rev")
	transactionID := firstNonEmpty(args.String("transaction"), fmt.Sprintf("%s-apply-%s", id, strings.ReplaceAll(now, ":", "")))
	data["applying_by"] = actor
	data["applying_at"] = now
	data["apply_transaction"] = transactionID
	data["updated_by"] = actor
	data["updated_at"] = now
	nextRev, err := saveV7DocumentCAS(note.AbsolutePath, data, body, v7FrontmatterOrder["proposal"], baseRev)
	if err != nil {
		return err
	}
	data["state_rev"] = nextRev
	target := stringField(data, "target")
	targetKind := stringField(data, "target_kind")
	action := stringField(data, "action")
	fields := v7ProposalFieldMap(data["proposed_fields"])
	switch action {
	case "close":
		if targetKind != "task" {
			return tuskerError(errorInvalidTransition, id+": close proposal apply requires a task target", withContext(map[string]any{"target_kind": targetKind}))
		}
		closeArgs := Args{"vault": vaultPath, "quiet": "true", "id": target, "by": actor}
		if reason := firstNonEmpty(args.String("reason"), toString(fields["reason"]), stringField(data, "review_reason")); reason != "" {
			closeArgs["reason"] = reason
		}
		if err := closeV7Cmd(closeArgs); err != nil {
			return err
		}
	case "status":
		if targetKind != "task" {
			return tuskerError(errorInvalidTransition, id+": status proposal apply requires a task target", withContext(map[string]any{"target_kind": targetKind}))
		}
		status := strings.ToLower(toString(fields["status"]))
		if status == "" {
			return tuskerError(errorMissingField, id+": status proposal missing proposed_fields.status")
		}
		statusArgs := Args{"vault": vaultPath, "quiet": "true", "id": target, "status": status, "by": actor}
		if reason := firstNonEmpty(args.String("reason"), toString(fields["reason"]), stringField(data, "review_reason")); reason != "" {
			statusArgs["reason"] = reason
		}
		if err := statusV7Cmd(statusArgs); err != nil {
			return err
		}
	case "create_gate":
		created, err := applyV7CreateGateProposal(vaultPath, target, targetKind, fields, actor)
		if err != nil {
			return err
		}
		target = created
		targetKind = "gate"
	case "create_task":
		created, err := applyV7CreateTaskProposal(vaultPath, target, targetKind, fields, actor)
		if err != nil {
			return err
		}
		target = created
		targetKind = "task"
	case "create_decision":
		created, err := applyV7CreateDecisionProposal(vaultPath, target, targetKind, fields, actor)
		if err != nil {
			return err
		}
		target = created
		targetKind = "decision"
	default:
		return tuskerError(errorInvalidTransition, id+": proposal apply currently supports close, status, create_task, create_gate, and create_decision actions", withContext(map[string]any{"action": action}))
	}
	targetNote, err := resolveV7Note(vaultPath, target, targetKind)
	if err != nil {
		return err
	}
	now = time.Now().UTC().Format(time.RFC3339)
	baseRev = stringField(data, "state_rev")
	data["applied_by"] = actor
	data["applied_at"] = now
	data["applied_target"] = target
	data["applied_target_rev"] = stringField(targetNote.Data, "state_rev")
	data["updated_by"] = actor
	data["updated_at"] = now
	if _, err := saveV7DocumentCAS(note.AbsolutePath, data, body, v7FrontmatterOrder["proposal"], baseRev); err != nil {
		return err
	}
	if !args.Bool("quiet") {
		fmt.Printf("Applied proposal %s to %s\n", id, target)
	}
	return emitV7Event(vaultPath, id, "proposal", "updated", actor, map[string]any{"action": "applied", "target": target, "target_rev": stringField(targetNote.Data, "state_rev")})
}

func applyV7CreateGateProposal(vaultPath, target, targetKind string, fields map[string]any, actor string) (string, error) {
	blocks := normalizeList(fields["blocks"])
	if len(blocks) == 0 && targetKind == "task" {
		blocks = []string{target}
	}
	if len(blocks) == 0 {
		return "", tuskerError(errorMissingField, "create_gate proposal requires proposed_fields.blocks or a task target")
	}
	epic := v7EpicFromTaskID(blocks[0])
	if epic == "" {
		return "", tuskerError(errorInvalidArg, "create_gate proposal block target must look like ABC-T-0001")
	}
	gateID := strings.ToUpper(toString(fields["id"]))
	if gateID == "" {
		gateID = fmt.Sprintf("%s-G-%s", epic, padNumber(nextV7Sequence(vaultPath, epic, "gate")))
	}
	args := Args{
		"vault":        vaultPath,
		"quiet":        "true",
		"id":           gateID,
		"blocks":       strings.Join(blocks, ","),
		"kind":         strings.ToLower(toString(fields["kind"])),
		"owner":        toString(fields["owner"]),
		"action":       toString(fields["action"]),
		"verification": toString(fields["verification"]),
		"by":           actor,
	}
	if title := toString(fields["title"]); title != "" {
		args["title"] = title
	}
	if whyAgentCannot := firstNonEmpty(toString(fields["why_agent_cannot"]), toString(fields["why-agent-cannot"]), toString(fields["why-agent-cannot-do-this"])); whyAgentCannot != "" {
		args["why-agent-cannot"] = whyAgentCannot
	}
	if suggestion := firstNonEmpty(toString(fields["suggestion"]), toString(fields["recommendation"]), toString(fields["suggested-resolution"]), toString(fields["suggested_resolution"])); suggestion != "" {
		args["suggestion"] = suggestion
	}
	if err := newV7Gate(args); err != nil {
		return "", err
	}
	return gateID, nil
}

func applyV7CreateTaskProposal(vaultPath, target, targetKind string, fields map[string]any, actor string) (string, error) {
	if targetKind != "epic" || !epicAcronymPattern.MatchString(target) {
		return "", tuskerError(errorInvalidTransition, "create_task proposal apply requires an epic target", withContext(map[string]any{"target": target, "target_kind": targetKind}))
	}
	title := toString(fields["title"])
	if title == "" {
		return "", tuskerError(errorMissingField, "create_task proposal requires proposed_fields.title")
	}
	taskID := strings.ToUpper(toString(fields["id"]))
	if taskID == "" {
		taskID = fmt.Sprintf("%s-T-%s", target, padNumber(nextV7Sequence(vaultPath, target, "task")))
	}
	args := Args{
		"vault":        vaultPath,
		"quiet":        "true",
		"id":           taskID,
		"epic":         target,
		"title":        title,
		"risk":         fallback(strings.ToLower(toString(fields["risk"])), "medium"),
		"priority":     fallback(strings.ToLower(toString(fields["priority"])), "p2"),
		"size":         fallback(strings.ToLower(toString(fields["size"])), "m"),
		"next-owner":   fallback(firstNonEmpty(toString(fields["next_owner"]), toString(fields["next-owner"])), "agent"),
		"next-action":  fallback(firstNonEmpty(toString(fields["next_action"]), toString(fields["next-action"])), "Execute the task contract and attach evidence."),
		"domains":      toString(fields["domains"]),
		"dependencies": toString(fields["dependencies"]),
		"by":           actor,
	}
	if evidence := firstNonEmpty(toString(fields["evidence_required"]), toString(fields["evidence-required"])); evidence != "" {
		args["evidence-required"] = evidence
	}
	if err := newV7Task(args); err != nil {
		return "", err
	}
	return taskID, nil
}

func applyV7CreateDecisionProposal(vaultPath, target, targetKind string, fields map[string]any, actor string) (string, error) {
	if targetKind != "epic" || !epicAcronymPattern.MatchString(target) {
		return "", tuskerError(errorInvalidTransition, "create_decision proposal apply requires an epic target", withContext(map[string]any{"target": target, "target_kind": targetKind}))
	}
	title := toString(fields["title"])
	if title == "" {
		return "", tuskerError(errorMissingField, "create_decision proposal requires proposed_fields.title")
	}
	decisionID := strings.ToUpper(toString(fields["id"]))
	if decisionID == "" {
		decisionID = fmt.Sprintf("%s-D-%s", target, padNumber(nextV7Sequence(vaultPath, target, "decision")))
	}
	args := Args{
		"vault":    vaultPath,
		"quiet":    "true",
		"id":       decisionID,
		"epic":     target,
		"title":    title,
		"decision": fallback(toString(fields["decision"]), "TBD."),
		"by":       actor,
	}
	if status := toString(fields["status"]); status != "" {
		args["status"] = status
	}
	if err := newV7Decision(args); err != nil {
		return "", err
	}
	return decisionID, nil
}

func v7ProposalFieldMap(value any) map[string]any {
	switch current := value.(type) {
	case map[string]any:
		return current
	case map[any]any:
		out := map[string]any{}
		for key, val := range current {
			out[toString(key)] = val
		}
		return out
	default:
		return map[string]any{}
	}
}

func v7ProposalFields(args Args, action string) (map[string]any, error) {
	fields := parseV7ProposalSet(args.String("set"))
	switch action {
	case "close":
		fields["status"] = "done"
		if reason := args.String("reason"); reason != "" {
			fields["reason"] = reason
		}
	case "status":
		status := strings.ToLower(args.String("status"))
		if status == "" {
			return nil, tuskerError(errorMissingArg, "status proposal requires --status")
		}
		if _, ok := v7TaskStatuses[status]; !ok {
			return nil, tuskerError(errorInvalidField, "invalid V7 task status: "+status)
		}
		if status == "done" {
			return nil, tuskerError(errorInvalidTransition, "status proposal cannot set done; use propose close")
		}
		fields["status"] = status
	case "create_task":
		for _, key := range []string{"id", "title", "summary", "risk", "priority", "size", "domains", "dependencies", "evidence-required", "next-owner", "next-action"} {
			if value := args.String(key); value != "" {
				fields[key] = value
			}
		}
	case "create_gate":
		for _, key := range []string{"id", "title", "kind", "owner", "action", "verification", "blocks"} {
			if value := args.String(key); value != "" {
				fields[key] = value
			}
		}
		if value := v7GateWhyAgentCannotArg(args); value != "" {
			fields["why_agent_cannot"] = value
		}
		if value := v7GateSuggestionArg(args); value != "" {
			fields["suggestion"] = value
		}
	case "create_decision":
		for _, key := range []string{"id", "title", "decision", "summary", "status"} {
			if value := args.String(key); value != "" {
				fields[key] = value
			}
		}
	}
	if len(fields) == 0 {
		return nil, tuskerError(errorMissingArg, "proposal requires --set field=value or an action-specific field")
	}
	return fields, nil
}

func parseV7ProposalSet(value string) map[string]any {
	fields := map[string]any{}
	for _, item := range splitCSV(value) {
		key, raw, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		raw = strings.TrimSpace(raw)
		if key != "" {
			fields[key] = raw
		}
	}
	return fields
}

func sortedMapKeys(fields map[string]any) []string {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func v7ProposalDefaultTitle(action, target string) string {
	switch action {
	case "close":
		return "Propose close " + target
	case "status":
		return "Propose status change for " + target
	case "create_task":
		return "Propose new task"
	case "create_gate":
		return "Propose new gate"
	case "create_decision":
		return "Propose new decision"
	default:
		return "Propose change for " + target
	}
}

func v7ProposalBody(id, title, action, target string, fields map[string]any, args Args) string {
	var fieldLines []string
	for _, key := range sortedMapKeys(fields) {
		fieldLines = append(fieldLines, fmt.Sprintf("- `%s`: %s", key, toString(fields[key])))
	}
	return fmt.Sprintf(`# %s · %s

## Summary

%s

## Proposed change

- Action: %s
- Target: %s
%s

## Rationale
 
%s

## Review

Pending.
`, id, title, fallback(args.String("summary"), "Proposal created."), action, fallback(target, "new "+action), strings.Join(fieldLines, "\n"), fallback(args.String("reason"), "No rationale recorded."))
}
