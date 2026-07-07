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
	for _, edge := range v7TaskDependencyEdges(task, idx) {
		dep, exists := idx.Tasks[edge.ID]
		if !v7DependencySatisfiedForReadiness(edge, dep, exists) {
			return edge, true
		}
	}
	return v7DependencyEdge{}, false
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
