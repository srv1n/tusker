package main

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type v7DiscardDependent struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

type v7DiscardImpact struct {
	TaskID             string               `json:"task_id"`
	Title              string               `json:"title"`
	Status             string               `json:"status"`
	DirectDependents   []v7DiscardDependent `json:"direct_dependents"`
	CascadeDependents  []v7DiscardDependent `json:"cascade_dependents"`
	OpenGates          []string             `json:"open_gates"`
	RequiresResolution bool                 `json:"requires_resolution"`
	PreservesHistory   bool                 `json:"preserves_history"`
}

func discardV7Cmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	id, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	id = strings.ToUpper(strings.TrimSpace(id))
	impact, err := v7DiscardImpactForTask(vaultPath, id)
	if err != nil {
		return err
	}
	if args.Bool("dry-run") || args.Bool("dry_run") {
		if args.Bool("json") {
			emitJSON(map[string]any{"ok": true, "dry_run": true, "impact": impact})
			return nil
		}
		printV7DiscardImpact(impact)
		return nil
	}
	if err := ensureV7ControlMutation(vaultPath, args); err != nil {
		return err
	}
	reason := strings.TrimSpace(args.String("reason"))
	if reason == "" {
		return tuskerError(errorMissingArg, "discard requires --reason", withHint("record why the work is being abandoned; the task remains as durable history"))
	}
	mode := strings.ToLower(strings.TrimSpace(firstNonEmpty(args.String("dependents"), args.String("resolve-dependents"))))
	if mode != "" && mode != "detach" && mode != "discard" {
		return tuskerError(errorInvalidArg, "--dependents must be detach or discard")
	}
	if impact.RequiresResolution && mode == "" {
		return tuskerError(
			errorInvalidTransition,
			id+": discard requires an explicit downstream dependency resolution",
			withHint("run `tusker discard "+id+" --dry-run`, then choose --dependents detach or --dependents discard"),
			withContext(map[string]any{"impact": impact}),
		)
	}
	actor := fallback(fallback(args.String("actor"), args.String("by")), "agent:"+defaultActorName())

	switch mode {
	case "detach":
		for _, dependent := range impact.DirectDependents {
			if err := detachV7TaskDependency(vaultPath, dependent.ID, id, actor, reason); err != nil {
				return err
			}
		}
	case "discard":
		// Cancel leaves before their prerequisites so no active task is briefly
		// made runnable by a partially applied cascade.
		for i := len(impact.CascadeDependents) - 1; i >= 0; i-- {
			if err := discardV7OneTask(vaultPath, impact.CascadeDependents[i].ID, actor, "Downstream of discarded "+id+": "+reason); err != nil {
				return err
			}
		}
	}
	if err := discardV7OneTask(vaultPath, id, actor, reason); err != nil {
		return err
	}

	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "discarded": id, "dependents": mode, "impact": impact})
		return nil
	}
	fmt.Printf("Discarded %s; history preserved", id)
	if mode != "" {
		fmt.Printf("; downstream resolution=%s", mode)
	}
	fmt.Println(".")
	return nil
}

func v7DiscardImpactForTask(vaultPath, taskID string) (v7DiscardImpact, error) {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return v7DiscardImpact{}, err
	}
	task, ok := idx.Tasks[taskID]
	if !ok {
		return v7DiscardImpact{}, tuskerError(errorNotFound, "V7 task not found: "+taskID)
	}
	impact := v7DiscardImpact{
		TaskID:            taskID,
		Title:             stringField(task.Data, "title"),
		Status:            stringField(task.Data, "status"),
		DirectDependents:  []v7DiscardDependent{},
		CascadeDependents: []v7DiscardDependent{},
		OpenGates:         []string{},
		PreservesHistory:  true,
	}
	impact.DirectDependents = activeV7Dependents(idx, taskID)
	impact.CascadeDependents = activeV7DependentClosure(idx, taskID)
	impact.RequiresResolution = len(impact.DirectDependents) > 0
	for _, gate := range sortedV7Gates(idx) {
		if stringField(gate.Data, "status") == "open" && v7GateTouchesTask(gate, taskID) {
			impact.OpenGates = append(impact.OpenGates, stringField(gate.Data, "id"))
		}
	}
	return impact, nil
}

func activeV7Dependents(idx v7Index, dependencyID string) []v7DiscardDependent {
	dependents := []v7DiscardDependent{}
	for _, task := range sortedV7Tasks(idx) {
		if v7TaskTerminal(task) || !v7TaskDependsOnID(task, dependencyID, idx) {
			continue
		}
		dependents = append(dependents, v7DiscardDependent{
			ID: stringField(task.Data, "id"), Title: stringField(task.Data, "title"), Status: stringField(task.Data, "status"),
		})
	}
	return dependents
}

func activeV7DependentClosure(idx v7Index, dependencyID string) []v7DiscardDependent {
	seen := map[string]bool{dependencyID: true}
	queue := []string{dependencyID}
	closure := []v7DiscardDependent{}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, dependent := range activeV7Dependents(idx, current) {
			if seen[dependent.ID] {
				continue
			}
			seen[dependent.ID] = true
			closure = append(closure, dependent)
			queue = append(queue, dependent.ID)
		}
	}
	return closure
}

func v7TaskTerminal(task Note) bool {
	switch strings.ToLower(strings.TrimSpace(stringField(task.Data, "status"))) {
	case "done", "cancelled", "superseded":
		return true
	default:
		return false
	}
}

func detachV7TaskDependency(vaultPath, taskID, dependencyID, actor, reason string) error {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return err
	}
	task, ok := idx.Tasks[taskID]
	if !ok {
		return tuskerError(errorNotFound, "V7 task not found: "+taskID)
	}
	var before, after []string
	_, changed, err := mutateV7DocumentLocked(task.AbsolutePath, v7FrontmatterOrder["task"], func(data map[string]any, body string) (map[string]any, string, bool, error) {
		before = normalizeList(data["dependencies"])
		for _, raw := range before {
			if parseV7DependencyEdge(raw).ID != dependencyID {
				after = append(after, raw)
			}
		}
		if len(after) == len(before) {
			return data, body, false, nil
		}
		data["dependencies"] = after
		data["updated_at"] = time.Now().UTC().Format(time.RFC3339)
		data["updated_by"] = actor
		return data, body, true, nil
	})
	if err != nil || !changed {
		return err
	}
	if err := emitV7Event(vaultPath, taskID, "task", "updated", actor, map[string]any{
		"changes": map[string]any{"dependencies": map[string]any{"from": before, "to": after}},
		"source":  "discard:" + dependencyID,
		"reason":  reason,
	}); err != nil {
		return err
	}
	_, err = reconcileV7ControlProjections(vaultPath, []string{taskID}, actor, "discard:detach:"+dependencyID)
	return err
}

func discardV7OneTask(vaultPath, taskID, actor, reason string) error {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return err
	}
	task, ok := idx.Tasks[taskID]
	if !ok {
		return tuskerError(errorNotFound, "V7 task not found: "+taskID)
	}
	status := strings.ToLower(strings.TrimSpace(stringField(task.Data, "status")))
	if status == "cancelled" {
		return nil
	}
	if status == "done" || status == "superseded" {
		return tuskerError(errorInvalidTransition, taskID+": cannot discard terminal task in status "+status)
	}
	for _, gate := range sortedV7Gates(idx) {
		if stringField(gate.Data, "status") != "open" || !v7GateTouchesTask(gate, taskID) {
			continue
		}
		if err := gateV7Transition(Args{
			"vault": vaultPath, "id": stringField(gate.Data, "id"), "reason": "Task discarded: " + reason, "by": actor, "quiet": "true",
		}, "obsolete"); err != nil {
			return err
		}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	previous := status
	_, changed, err := mutateV7DocumentLocked(task.AbsolutePath, v7FrontmatterOrder["task"], func(data map[string]any, body string) (map[string]any, string, bool, error) {
		current := strings.ToLower(strings.TrimSpace(stringField(data, "status")))
		if current == "cancelled" {
			return data, body, false, nil
		}
		if current == "done" || current == "superseded" {
			return data, body, false, tuskerError(errorInvalidTransition, taskID+": cannot discard terminal task in status "+current)
		}
		data["status"] = "cancelled"
		data["readiness"] = "cancelled"
		data["next_owner"] = "none"
		data["next_source"] = "status"
		data["next_ref"] = ""
		data["next_action"] = ""
		data["agent_action"] = ""
		data["machine_status"] = ""
		data["human_status"] = ""
		data["closeout_status"] = ""
		delete(data, "accepted_by")
		delete(data, "accepted_at")
		delete(data, "closed_at")
		data["discarded_by"] = actor
		data["discarded_at"] = now
		data["discard_reason"] = reason
		data["updated_at"] = now
		data["updated_by"] = actor
		return data, body, true, nil
	})
	if err != nil || !changed {
		return err
	}
	if err := emitV7Event(vaultPath, taskID, "task", "cancelled", actor, map[string]any{"from": previous, "to": "cancelled", "reason": reason}); err != nil {
		return err
	}
	if _, err := retireCanonicalRuntimeRowsForTask(vaultPath, taskID, "cancelled", actor, "discard"); err != nil {
		return err
	}
	if err := removeTaskPlanFile(vaultPath, taskID); err != nil {
		return err
	}
	affected, err := v7TaskIDsForTaskControl(vaultPath, taskID)
	if err != nil {
		return err
	}
	_, err = reconcileV7ControlProjections(vaultPath, affected, actor, "discard:"+taskID)
	return err
}

func printV7DiscardImpact(impact v7DiscardImpact) {
	fmt.Printf("Discard %s (%s)\n", impact.TaskID, impact.Status)
	fmt.Printf("History preserved: yes\n")
	fmt.Printf("Open gates to obsolete: %d\n", len(impact.OpenGates))
	fmt.Printf("Direct active dependents: %d\n", len(impact.DirectDependents))
	for _, dependent := range impact.DirectDependents {
		fmt.Printf("- %s [%s] %s\n", dependent.ID, dependent.Status, dependent.Title)
	}
	if len(impact.CascadeDependents) > len(impact.DirectDependents) {
		ids := make([]string, 0, len(impact.CascadeDependents))
		for _, dependent := range impact.CascadeDependents {
			ids = append(ids, dependent.ID)
		}
		sort.Strings(ids)
		fmt.Printf("Full discard cascade: %s\n", strings.Join(ids, ", "))
	}
}
