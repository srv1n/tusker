package main

import (
	"fmt"
	"sort"
	"strings"
)

const (
	v7DependencyHardnessHard = "hard"
	v7DependencyHardnessSoft = "soft"
)

type v7DependencyEdge struct {
	Raw              string
	ID               string
	Hardness         string
	ExplicitHardness bool
}

func parseV7DependencyEdge(raw string) v7DependencyEdge {
	edge := v7DependencyEdge{Raw: strings.TrimSpace(raw)}
	value := edge.Raw
	lower := strings.ToLower(value)
	for _, hardness := range []string{v7DependencyHardnessSoft, v7DependencyHardnessHard} {
		suffix := ":" + hardness
		if strings.HasSuffix(lower, suffix) {
			edge.Hardness = hardness
			edge.ExplicitHardness = true
			value = strings.TrimSpace(value[:len(value)-len(suffix)])
			break
		}
	}
	edge.ID = wikiTarget(value)
	return edge
}

func v7TaskDependencyEdges(task Note, idx v7Index) []v7DependencyEdge {
	var edges []v7DependencyEdge
	for _, raw := range normalizeList(task.Data["dependencies"]) {
		edge := parseV7DependencyEdge(raw)
		if edge.ID == "" {
			continue
		}
		if edge.Hardness == "" {
			if dep, ok := idx.Tasks[edge.ID]; ok {
				edge.Hardness = v7DefaultDependencyHardness(dep)
			} else {
				edge.Hardness = v7DependencyHardnessHard
			}
		}
		edges = append(edges, edge)
	}
	return edges
}

func v7DefaultDependencyHardness(dep Note) string {
	switch strings.ToLower(strings.TrimSpace(stringField(dep.Data, "risk"))) {
	case "low", "medium":
		return v7DependencyHardnessSoft
	case "high", "critical":
		return v7DependencyHardnessHard
	default:
		return v7DependencyHardnessHard
	}
}

func v7DependencySatisfiedForReadiness(edge v7DependencyEdge, dep Note, exists bool) bool {
	if !exists {
		return false
	}
	status := strings.ToLower(strings.TrimSpace(stringField(dep.Data, "status")))
	if status == "done" {
		return true
	}
	return edge.Hardness == v7DependencyHardnessSoft &&
		status == "review" &&
		strings.EqualFold(stringField(dep.Data, "proof_status"), "satisfied")
}

func v7BlockingDependencyForReadiness(task Note, idx v7Index) (v7DependencyEdge, bool) {
	if edge, blocked := v7CrossScopeIntegrityBlocker(task, idx); blocked {
		return edge, true
	}
	for _, edge := range v7TaskDependencyEdges(task, idx) {
		dep, exists := idx.Tasks[edge.ID]
		if !v7DependencySatisfiedForReadiness(edge, dep, exists) {
			return edge, true
		}
	}
	return v7DependencyEdge{}, false
}

// v7CrossScopeIntegrityBlocker keeps semantic provenance on the same ordinary
// dependency path used by readiness. It scopes validation to this task's
// dependency closure so drift parks only affected consumers; the projection
// never becomes a second scheduler edge.
func v7CrossScopeIntegrityBlocker(task Note, idx v7Index) (v7DependencyEdge, bool) {
	if stringField(task.Data, "delivery_plan_scope") == "" && task.Data["delivery_cross_scope_dependencies"] == nil {
		return v7DependencyEdge{}, false
	}

	scoped := idx
	scoped.Tasks = map[string]Note{}
	seen := map[string]bool{}
	var visit func(Note)
	visit = func(current Note) {
		id := stringField(current.Data, "id")
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		scoped.Tasks[id] = current
		for _, edge := range v7TaskDependencyEdges(current, idx) {
			if dependency, ok := idx.Tasks[edge.ID]; ok {
				visit(dependency)
			}
		}
	}
	visit(task)
	if err := validateDeliveryCrossScopeIndex(scoped, stringField(task.Data, "project")); err == nil {
		return v7DependencyEdge{}, false
	}

	projections, _ := deliveryCrossScopeProjections(task)
	projected := map[string]bool{}
	for _, projection := range projections {
		projected[projection.TaskID] = true
	}
	consumerScope := stringField(task.Data, "delivery_plan_scope")
	edges := v7TaskDependencyEdges(task, idx)
	for _, edge := range edges {
		dependency, exists := idx.Tasks[edge.ID]
		if projected[edge.ID] || (exists && stringField(dependency.Data, "delivery_plan_scope") != "" && stringField(dependency.Data, "delivery_plan_scope") != consumerScope) {
			return edge, true
		}
	}
	if len(edges) > 0 {
		return edges[0], true
	}
	return v7DependencyEdge{ID: "cross-scope projection", Hardness: v7DependencyHardnessHard}, true
}

func v7UnclosedDependency(task Note, idx v7Index) (v7DependencyEdge, bool) {
	for _, edge := range v7TaskDependencyEdges(task, idx) {
		dep, exists := idx.Tasks[edge.ID]
		if !exists || strings.ToLower(strings.TrimSpace(stringField(dep.Data, "status"))) != "done" {
			return edge, true
		}
	}
	return v7DependencyEdge{}, false
}

func v7TaskDependsOnID(task Note, dependencyID string, idx v7Index) bool {
	dependencyID = strings.TrimSpace(dependencyID)
	if dependencyID == "" {
		return false
	}
	for _, edge := range v7TaskDependencyEdges(task, idx) {
		if edge.ID == dependencyID {
			return true
		}
	}
	return false
}

func v7SoftDependencyDependentLines(idx v7Index, dependencyID string) []string {
	dependencyID = strings.TrimSpace(dependencyID)
	if dependencyID == "" {
		return nil
	}
	var lines []string
	for _, task := range sortedV7Tasks(idx) {
		for _, edge := range v7TaskDependencyEdges(task, idx) {
			if edge.ID != dependencyID || edge.Hardness != v7DependencyHardnessSoft {
				continue
			}
			lines = append(lines, fmt.Sprintf(
				"- `%s` status=%s readiness=%s next_ref=%s",
				stringField(task.Data, "id"),
				fallback(stringField(task.Data, "status"), "(missing)"),
				fallback(stringField(task.Data, "readiness"), "(missing)"),
				fallback(stringField(task.Data, "next_ref"), "(none)"),
			))
		}
	}
	sort.Strings(lines)
	return lines
}

func v7SoftDependencyBlastRadius(idx v7Index, dependencyID string) string {
	lines := v7SoftDependencyDependentLines(idx, dependencyID)
	if len(lines) == 0 {
		return "- None."
	}
	return strings.Join(lines, "\n")
}

func v7DependencyWaitAction(edge v7DependencyEdge) string {
	if edge.Hardness == v7DependencyHardnessSoft {
		return "Wait for dependency " + edge.ID + " to reach review with satisfied proof or done."
	}
	return "Wait for dependency " + edge.ID + " to reach done."
}
