package main

import (
	"fmt"
	"sort"
	"strings"
)

type integratorLaneReport struct {
	TaskID   string
	EndState RunEndState
	Files    []string
}

func integratorPacket(vaultPath string, task Note, idx v7Index) string {
	var b strings.Builder
	id := stringField(task.Data, "id")
	fmt.Fprintf(&b, "# %s integrator packet\n\n", id)
	fmt.Fprintf(&b, "## Required reads\n\n- `references/INTEGRATION_MERGE.md` from the installed Tusker skill\n- `.tusker/SKILL.md`\n\n")
	wf, _ := loadWorkflow(vaultPath)
	fmt.Fprintf(&b, "## Shared namespaces owned by this lane\n\n%s\n\n", v7BulletList(wf.Data.Orchestration.SharedNamespaces))
	reports := integratorDependencyReports(vaultPath, task)
	fmt.Fprintf(&b, "## Lane end states\n\n| Task | Branch | HEAD | Worktree | Dirty | Gate verdicts |\n|---|---|---|---|---|---|\n")
	for _, report := range reports {
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %t | %s |\n", report.TaskID, report.EndState.Branch, report.EndState.HeadSHA, report.EndState.WorktreePath, report.EndState.Dirty, formatGateVerdicts(report.EndState.GateVerdicts))
	}
	fmt.Fprintf(&b, "\n## File overlap audit\n\n")
	overlaps := integratorOverlapRows(reports)
	if len(overlaps) == 0 {
		b.WriteString("- No cross-lane file overlap detected.\n")
	} else {
		for _, overlap := range overlaps {
			fmt.Fprintf(&b, "- %s\n", overlap)
		}
	}
	fmt.Fprintf(&b, "\n## Merge contract\n\n%s\n", v7PacketSnippet(sectionContent(task.Body, "## Acceptance"), 18))
	_ = idx
	return b.String()
}

func integratorDependencyReports(vaultPath string, task Note) []integratorLaneReport {
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return nil
	}
	defer store.Close()
	var reports []integratorLaneReport
	for _, dependency := range normalizeList(task.Data["dependencies"]) {
		run, err := store.FindRun(dependency)
		if err != nil || run == nil {
			continue
		}
		attempts, err := store.ListAttemptsForRun(run.ProjectID, run.RecordID)
		if err != nil {
			continue
		}
		for _, attempt := range attempts {
			if attempt.EndState.Schema == "" {
				continue
			}
			report := integratorLaneReport{TaskID: dependency, EndState: attempt.EndState}
			report.Files = laneChangedFiles(attempt.EndState)
			reports = append(reports, report)
			break
		}
	}
	sort.Slice(reports, func(i, j int) bool { return reports[i].TaskID < reports[j].TaskID })
	return reports
}

func laneChangedFiles(state RunEndState) []string {
	if state.WorktreePath == "" || state.HeadSHA == "" {
		return nil
	}
	base := resolveDefaultBranch(state.WorktreePath, "")
	if base == "" {
		return nil
	}
	out, err := gitFactOutput(state.WorktreePath, "diff", "--name-only", base+"..."+state.HeadSHA)
	if err != nil || out == "" {
		return nil
	}
	return uniqueStrings(strings.Split(out, "\n"))
}

func integratorOverlapRows(reports []integratorLaneReport) []string {
	var rows []string
	for i := 0; i < len(reports); i++ {
		left := makeSet(reports[i].Files...)
		for j := i + 1; j < len(reports); j++ {
			var overlap []string
			for _, path := range reports[j].Files {
				if _, ok := left[path]; ok {
					overlap = append(overlap, path)
				}
			}
			sort.Strings(overlap)
			if len(overlap) > 0 {
				rows = append(rows, fmt.Sprintf("%s ↔ %s: %s", reports[i].TaskID, reports[j].TaskID, strings.Join(overlap, ", ")))
			}
		}
	}
	return rows
}

func formatGateVerdicts(verdicts map[string]string) string {
	keys := make([]string, 0, len(verdicts))
	for key := range verdicts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+verdicts[key])
	}
	return strings.Join(parts, ", ")
}
