package main

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const dependencyContractReviewSchema = "tusker.dependency-contract-review/v1"

// dependencyContractEntry is one durable pinned producer contract on a task.
// The ordinary `dependencies` edge remains authoritative; a contract pins the
// producer's task contract fingerprint so drift is visible instead of silent.
type dependencyContractEntry struct {
	TaskID                    string `yaml:"task_id" json:"taskId"`
	Kind                      string `yaml:"kind" json:"kind"`
	TargetContractFingerprint string `yaml:"target_contract_fingerprint" json:"targetContractFingerprint"`
}

// dependencyContractEntries reads canonical dependency_contracts rows. A
// malformed projection is an error, never silently skipped.
func dependencyContractEntries(task Note) ([]dependencyContractEntry, error) {
	raw, present := task.Data["dependency_contracts"]
	if !present || raw == nil {
		return nil, nil
	}
	encoded, err := yaml.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("dependency_contracts projection is malformed")
	}
	var entries []dependencyContractEntry
	if err := yaml.Unmarshal(encoded, &entries); err != nil {
		return nil, fmt.Errorf("dependency_contracts projection is malformed")
	}
	for _, entry := range entries {
		if strings.TrimSpace(entry.TaskID) == "" {
			return nil, fmt.Errorf("dependency_contracts contains a row without task_id")
		}
		if entry.Kind != v7DependencyHardnessHard && entry.Kind != v7DependencyHardnessSoft {
			return nil, fmt.Errorf("dependency_contracts row for %s has invalid kind %q", entry.TaskID, entry.Kind)
		}
	}
	return entries, nil
}

// v7DependencyContractIntegrityBlocker parks a consumer whose pinned producer
// contract no longer matches the durable target's current task contract. A
// pinned missing target blocks the same way an unresolved edge does; the pin
// preserves identity without claiming satisfaction.
func v7DependencyContractIntegrityBlocker(task Note, idx v7Index) (v7DependencyEdge, bool) {
	entries, err := dependencyContractEntries(task)
	if err != nil {
		return v7DependencyEdge{ID: "dependency contract", Hardness: v7DependencyHardnessHard}, true
	}
	if len(entries) == 0 {
		return v7DependencyEdge{}, false
	}
	edgeKinds := map[string]string{}
	edgeCount := map[string]int{}
	for _, edge := range v7TaskDependencyEdges(task, idx) {
		edgeKinds[edge.ID] = fallback(edge.Hardness, v7DependencyHardnessHard)
		edgeCount[edge.ID]++
	}
	for _, entry := range entries {
		target := strings.ToUpper(strings.TrimSpace(entry.TaskID))
		if edgeCount[target] != 1 || edgeKinds[target] != entry.Kind {
			return v7DependencyEdge{ID: target, Hardness: v7DependencyHardnessHard}, true
		}
		producer, exists := idx.Tasks[target]
		if !exists {
			return v7DependencyEdge{ID: target, Hardness: entry.Kind}, true
		}
		if pinned := strings.TrimSpace(entry.TargetContractFingerprint); pinned != "" && directWaveTaskContract(producer) != pinned {
			return v7DependencyEdge{ID: target, Hardness: entry.Kind}, true
		}
	}
	return v7DependencyEdge{}, false
}

type dependencyContractReviewProjection struct {
	Schema       string                        `json:"schema"`
	ReadOnly     bool                          `json:"readOnly"`
	Dependencies []dependencyContractReviewRow `json:"dependencies"`
}

type dependencyContractReviewRow struct {
	ConsumerTaskID               string `json:"consumerTaskId,omitempty"`
	TaskID                       string `json:"taskId"`
	Kind                         string `json:"kind"`
	PersistedContractFingerprint string `json:"persistedContractFingerprint,omitempty"`
	ProducerContractFingerprint  string `json:"producerContractFingerprint,omitempty"`
	TargetIntegrity              string `json:"targetIntegrity"`
	ProducerState                string `json:"producerState"`
	ProducerLifecycle            string `json:"producerLifecycle"`
	BlockerClass                 string `json:"blockerClass"`
	Satisfied                    bool   `json:"satisfied"`
	Repair                       string `json:"repair,omitempty"`
	Implication                  string `json:"implication"`
	TaskHref                     string `json:"taskHref,omitempty"`
}

func newDependencyContractReviewProjection() dependencyContractReviewProjection {
	return dependencyContractReviewProjection{
		Schema: dependencyContractReviewSchema, ReadOnly: true,
		Dependencies: []dependencyContractReviewRow{},
	}
}

func dependencyContractReviewForTask(idx v7Index, task Note) dependencyContractReviewProjection {
	out := newDependencyContractReviewProjection()
	entries, err := dependencyContractEntries(task)
	if err != nil {
		out.Dependencies = append(out.Dependencies, dependencyContractReviewStructuralRow(
			stringField(task.Data, "id"), "", "", "invalid", "corrupt", "unknown",
		))
		return out
	}
	for _, entry := range entries {
		out.Dependencies = append(out.Dependencies, dependencyContractReviewRowFor(idx, task, entry))
	}
	sortDependencyContractReviewRows(out.Dependencies)
	return out
}

func dependencyContractReviewForTaskIDs(idx v7Index, taskIDs []string) dependencyContractReviewProjection {
	out := newDependencyContractReviewProjection()
	ids := uniqueStrings(taskIDs)
	sort.Strings(ids)
	for _, id := range ids {
		task, ok := idx.Tasks[id]
		if !ok {
			continue
		}
		projected := dependencyContractReviewForTask(idx, task)
		out.Dependencies = append(out.Dependencies, projected.Dependencies...)
	}
	sortDependencyContractReviewRows(out.Dependencies)
	return out
}

func dependencyContractReviewForTaskAtVault(vault string, task Note) (dependencyContractReviewProjection, error) {
	idx, err := loadV7Index(vault)
	if err != nil {
		return newDependencyContractReviewProjection(), err
	}
	return dependencyContractReviewForTask(idx, task), nil
}

func dependencyContractReviewRowFor(idx v7Index, consumer Note, entry dependencyContractEntry) dependencyContractReviewRow {
	target := strings.ToUpper(strings.TrimSpace(entry.TaskID))
	row := dependencyContractReviewRow{
		ConsumerTaskID:               stringField(consumer.Data, "id"),
		TaskID:                       target,
		Kind:                         entry.Kind,
		PersistedContractFingerprint: strings.TrimSpace(entry.TargetContractFingerprint),
		TargetIntegrity:              "resolved",
		ProducerState:                "missing",
		ProducerLifecycle:            "unknown",
		BlockerClass:                 "structural",
	}
	row.Implication = dependencyContractReviewImplication(row)
	edgeCount := 0
	edgeKindMatches := false
	for _, edge := range v7TaskDependencyEdges(consumer, idx) {
		if edge.ID != target {
			continue
		}
		edgeCount++
		edgeKindMatches = fallback(edge.Hardness, v7DependencyHardnessHard) == entry.Kind
	}
	if edgeCount != 1 || !edgeKindMatches {
		return dependencyContractReviewMarkStructural(row, "corrupt")
	}
	producer, ok := idx.Tasks[target]
	if !ok {
		return dependencyContractReviewMarkStructural(row, "missing")
	}
	row.ProducerState = dependencyContractReviewProducerState(producer)
	row.ProducerLifecycle = dependencyContractReviewProducerLifecycle(producer)
	row.ProducerContractFingerprint = directWaveTaskContract(producer)
	if projectID := firstNonEmpty(stringField(producer.Data, "project"), stringField(consumer.Data, "project")); projectID != "" {
		row.TaskHref = taskDeepLink(projectID, target)
	}
	if row.ProducerLifecycle == "unknown" {
		return dependencyContractReviewMarkStructural(row, "corrupt")
	}
	if row.ProducerLifecycle == "failed" {
		row.BlockerClass = "lifecycle"
		row.Repair = dependencyContractReviewLifecycleRepair(row, true)
		return row
	}
	if row.PersistedContractFingerprint != "" && row.ProducerContractFingerprint != row.PersistedContractFingerprint {
		return dependencyContractReviewMarkStructural(row, "corrupt")
	}
	if row.ProducerLifecycle == "complete" {
		row.BlockerClass = "none"
		row.Satisfied = true
		return row
	}
	row.BlockerClass = "lifecycle"
	row.Repair = dependencyContractReviewLifecycleRepair(row, false)
	return row
}

func dependencyContractReviewStructuralRow(consumerID, taskID, fingerprint, provenance, integrity, producerState string) dependencyContractReviewRow {
	row := dependencyContractReviewRow{
		ConsumerTaskID:               consumerID,
		TaskID:                       taskID,
		Kind:                         v7DependencyHardnessHard,
		PersistedContractFingerprint: fingerprint,
		TargetIntegrity:              integrity,
		ProducerState:                fallback(producerState, "unknown"),
		ProducerLifecycle:            "unknown",
		BlockerClass:                 "structural",
	}
	row.Implication = dependencyContractReviewImplication(row)
	row.Repair = dependencyContractReviewStructuralRepair(row, integrity)
	return row
}

func dependencyContractReviewMarkStructural(row dependencyContractReviewRow, integrity string) dependencyContractReviewRow {
	row.TargetIntegrity = integrity
	row.BlockerClass = "structural"
	row.Satisfied = false
	row.Repair = dependencyContractReviewStructuralRepair(row, integrity)
	return row
}

func dependencyContractReviewProducerState(producer Note) string {
	if stringField(producer.Data, "discarded_at") != "" {
		return "discarded"
	}
	return fallback(strings.ToLower(strings.TrimSpace(stringField(producer.Data, "status"))), "unknown")
}

func dependencyContractReviewProducerLifecycle(producer Note) string {
	state := dependencyContractReviewProducerState(producer)
	switch state {
	case "done":
		return "complete"
	case "cancelled", "discarded", "superseded":
		return "failed"
	case "idea", "backlog", "ready", "review", "rework":
		return "incomplete"
	default:
		return "unknown"
	}
}

func dependencyContractReviewTarget(row dependencyContractReviewRow) string {
	return fallback(row.TaskID, "the named producer")
}

func dependencyContractReviewConsumer(row dependencyContractReviewRow) string {
	return firstNonEmpty(row.ConsumerTaskID, "the consumer")
}

func dependencyContractReviewImplication(row dependencyContractReviewRow) string {
	return "Producer " + dependencyContractReviewTarget(row) + " must complete before " + dependencyContractReviewConsumer(row) +
		"; the consumer can run only after the pinned dependency reaches its contract state."
}

func dependencyContractReviewStructuralRepair(row dependencyContractReviewRow, integrity string) string {
	if integrity == "missing" {
		return "Restore the durable producer task " + dependencyContractReviewTarget(row) +
			", then retry " + dependencyContractReviewConsumer(row) + "."
	}
	return "Restore the exact durable edge and pinned contract fingerprint for " + dependencyContractReviewTarget(row) +
		", or explicitly amend the consumer task before retrying " + dependencyContractReviewConsumer(row) + "."
}

func dependencyContractReviewLifecycleRepair(row dependencyContractReviewRow, failed bool) string {
	if failed {
		return "Repair or reopen producer " + dependencyContractReviewTarget(row) + ", then complete it; " +
			dependencyContractReviewConsumer(row) + " remains blocked until that dependency reaches done."
	}
	return "Complete producer " + dependencyContractReviewTarget(row) + "; " +
		dependencyContractReviewConsumer(row) + " remains blocked until its dependency reaches done."
}

func sortDependencyContractReviewRows(rows []dependencyContractReviewRow) {
	sort.Slice(rows, func(i, j int) bool {
		left := rows[i].TaskID + "\x00" + rows[i].ConsumerTaskID
		right := rows[j].TaskID + "\x00" + rows[j].ConsumerTaskID
		return left < right
	})
}

func renderDependencyContractReview(rows []dependencyContractReviewRow) string {
	if len(rows) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Pinned dependency contracts\n")
	for _, row := range rows {
		fmt.Fprintf(&b, "- Dependency: %s\n", dependencyContractReviewTarget(row))
		fmt.Fprintf(&b, "  Durable target: %s (%s; %s; contract %s)\n",
			fallback(row.TaskID, "missing"), row.ProducerState, row.Kind, fallback(row.PersistedContractFingerprint, "not recorded"))
		fmt.Fprintf(&b, "  Classification: target=%s lifecycle=%s blocker=%s\n",
			row.TargetIntegrity, row.ProducerLifecycle, row.BlockerClass)
		fmt.Fprintf(&b, "  Producer before consumer: %s\n", row.Implication)
		if row.Repair != "" {
			fmt.Fprintf(&b, "  Repair: %s\n", row.Repair)
		}
	}
	return b.String()
}
