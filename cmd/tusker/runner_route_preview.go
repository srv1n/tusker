package main

import (
	"fmt"
	"strings"
)

type runnerRoutePrecedence struct {
	Source   string `json:"source"`
	Reason   string `json:"reason"`
	Selected bool   `json:"selected"`
}

type runnerRoutePreview struct {
	Schema       string                  `json:"schema"`
	ReadOnly     bool                    `json:"read_only"`
	Task         string                  `json:"task"`
	Lane         string                  `json:"lane"`
	Complexity   string                  `json:"complexity,omitempty"`
	SemanticRole string                  `json:"semantic_role,omitempty"`
	Profile      string                  `json:"profile,omitempty"`
	Harness      string                  `json:"harness,omitempty"`
	Model        string                  `json:"model,omitempty"`
	Effort       string                  `json:"effort,omitempty"`
	Source       string                  `json:"source,omitempty"`
	Reason       string                  `json:"reason,omitempty"`
	Rule         string                  `json:"rule,omitempty"`
	Precedence   []runnerRoutePrecedence `json:"precedence"`
	Blockers     []string                `json:"blockers"`
}

// runnerRouteCmd intentionally avoids the automation context and runtime store:
// this is an explanation of dispatch policy, not a claim or readiness probe.
func runnerRouteCmd(args Args) error {
	id, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	lane := strings.TrimSpace(args.String("lane"))
	if lane != runLaneExecute && lane != runLaneReview {
		return tuskerError(errorInvalidArg, "--lane must be execute or review")
	}
	vault, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	wfFile, err := loadWorkflow(vault)
	if err != nil {
		return err
	}
	note, err := resolveV7Note(vault, id, "task")
	if err != nil {
		return err
	}
	preview := routePreviewForNote(note, wfFile.Data, lane)
	if args.Bool("json") {
		emitJSON(preview)
		return nil
	}
	fmt.Printf("%s %s: %s\n", preview.Task, preview.Lane, firstNonEmpty(preview.Profile, "blocked"))
	for _, blocker := range preview.Blockers {
		fmt.Println("blocker: " + blocker)
	}
	return nil
}

func routePreviewForNote(note Note, wf Workflow, lane string) runnerRoutePreview {
	complexity := strings.ToLower(strings.TrimSpace(stringField(note.Data, "complexity")))
	preview := runnerRoutePreview{Schema: "tusker.runner-route/v1", ReadOnly: true, Task: stringField(note.Data, "id"), Lane: lane, Complexity: complexity, Blockers: []string{}}
	preview.Precedence = []runnerRoutePrecedence{
		{Source: "task frontmatter", Reason: "runner_profile"}, {Source: "automation.routing", Reason: "first matching routing rule"},
		{Source: "automation.lane_profiles", Reason: "lane mapping"}, {Source: "task complexity", Reason: "semantic complexity role"},
		{Source: "automation.default_profile", Reason: "project default or built-in default"},
	}
	if complexity != "" && !validTaskComplexity(complexity) {
		preview.Blockers = append(preview.Blockers, "invalid task complexity: "+complexity)
		return preview
	}
	preview.SemanticRole = semanticRunnerRole(complexity, lane)
	selected, err := resolveRunnerProfileForNote(note, wf, lane)
	if err != nil {
		preview.Blockers = append(preview.Blockers, err.Error())
		return preview
	}
	preview.Profile, preview.Harness, preview.Model, preview.Effort = selected.Name, selected.Definition.Harness, selected.Definition.Model, selected.Definition.Effort
	preview.Source, preview.Reason, preview.Rule = selected.Source, selected.Reason, selected.RuleName
	preview.Precedence = []runnerRoutePrecedence{
		{Source: "task frontmatter", Reason: "runner_profile", Selected: selected.Source == "task frontmatter"},
		{Source: "automation.routing", Reason: "first matching routing rule", Selected: selected.Source == "automation.routing"},
		{Source: "automation.lane_profiles", Reason: "lane mapping", Selected: selected.Source == "automation.lane_profiles"},
		{Source: "task complexity", Reason: "semantic complexity role", Selected: selected.Source == "task complexity"},
		{Source: "automation.default_profile", Reason: "project default or built-in default", Selected: selected.Source == "automation.default_profile" || selected.Source == configSourceBuiltIn},
	}
	return preview
}
