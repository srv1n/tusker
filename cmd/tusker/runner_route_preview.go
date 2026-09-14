package main

import (
	"fmt"
	"strings"

	runnercore "tusker/internal/runner"
)

type runnerRoutePrecedence struct {
	Source   string `json:"source"`
	Reason   string `json:"reason"`
	Selected bool   `json:"selected"`
}

type runnerRoutePreview struct {
	Schema       string `json:"schema"`
	ReadOnly     bool   `json:"read_only"`
	Task         string `json:"task"`
	Lane         string `json:"lane"`
	Complexity   string `json:"complexity,omitempty"`
	WorkLevel    string `json:"work_level,omitempty"`
	SemanticRole string `json:"semantic_role,omitempty"`
	Profile      string `json:"profile,omitempty"`
	// ProfileDefinition exposes the complete resolved execution policy, not
	// merely its display name. Callers can therefore explain the selected
	// permission, sandbox, and subagent limits without re-resolving config.
	ProfileDefinition RunnerProfileDefinition   `json:"profile_definition,omitempty"`
	Access            *AgentAccessV1            `json:"access,omitempty"`
	ResolvedAccess    *ResolvedAccess           `json:"resolved_access,omitempty"`
	CommandPolicy     *runnercore.CommandPolicy `json:"command_policy,omitempty"`
	Harness           string                    `json:"harness,omitempty"`
	Model             string                    `json:"model,omitempty"`
	Effort            string                    `json:"effort,omitempty"`
	Source            string                    `json:"source,omitempty"`
	Reason            string                    `json:"reason,omitempty"`
	Rule              string                    `json:"rule,omitempty"`
	Warnings          []string                  `json:"warnings,omitempty"`
	Fallbacks         []string                  `json:"fallbacks,omitempty"`
	Precedence        []runnerRoutePrecedence   `json:"precedence"`
	Blockers          []string                  `json:"blockers"`
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
	note, err := resolveV7Note(vault, id, "task")
	if err != nil {
		return err
	}
	wfFile, err := loadWorkflow(vault)
	if err != nil {
		if args.Bool("json") {
			emitJSON(runnerRoutePreview{
				Schema: "tusker.runner-route/v1", ReadOnly: true, Task: stringField(note.Data, "id"), Lane: lane,
				Blockers: []string{runnerRouteBlocker(err)}, Precedence: runnerRoutePrecedenceTable(lane),
			})
			return nil
		}
		return err
	}
	preview := routePreviewForNote(note, wfFile.Data, lane)
	if args.Bool("json") {
		emitJSON(preview)
		return nil
	}
	fmt.Printf("%s %s: %s\n", preview.Task, preview.Lane, firstNonEmpty(preview.Profile, preview.Harness, "blocked"))
	for _, blocker := range preview.Blockers {
		fmt.Println("blocker: " + blocker)
	}
	return nil
}

func routePreviewForNote(note Note, wf Workflow, lane string) runnerRoutePreview {
	complexity := strings.ToLower(strings.TrimSpace(stringField(note.Data, "complexity")))
	preview := runnerRoutePreview{Schema: "tusker.runner-route/v1", ReadOnly: true, Task: stringField(note.Data, "id"), Lane: lane, Complexity: complexity, Blockers: []string{}}
	var levelErr error
	preview.WorkLevel, _, levelErr = modelLevelForNote(note, lane)
	profileField := "execute_profile"
	if lane == runLaneReview {
		profileField = "review_profile"
	}
	preview.Precedence = runnerRoutePrecedenceTable(lane)
	if complexity != "" && !validTaskComplexity(complexity) {
		preview.Blockers = append(preview.Blockers, "invalid task complexity: "+complexity)
		return preview
	}
	if levelErr != nil {
		preview.Blockers = append(preview.Blockers, runnerRouteBlocker(levelErr))
		return preview
	}
	preview.SemanticRole = semanticRunnerRole(complexity, lane)
	// The daemon still accepts the legacy per-lane runner for projects that
	// have no named profile. Use that same compatibility input here so a
	// read-only preview cannot advertise the built-in profile while dispatch
	// later selects the workflow's legacy runner.
	selected, err := resolveRunProfileForLane(note, wf, lane, legacyRunnerForLane(note, wf, lane))
	if err != nil {
		preview.Blockers = append(preview.Blockers, runnerRouteBlocker(err))
		return preview
	}
	if selected.Definition.Disabled {
		candidates, candidatesErr := resolvedProfileCandidates(selected, wf)
		if candidatesErr != nil {
			preview.Blockers = append(preview.Blockers, runnerRouteBlocker(candidatesErr))
			return preview
		}
		selected, _, err = selectModelProfile(candidates, func(ResolvedRunnerProfile) (bool, string, error) { return true, "", nil })
		if err != nil {
			preview.Blockers = append(preview.Blockers, runnerRouteBlocker(err))
			return preview
		}
	}
	preview.Profile, preview.ProfileDefinition = selected.Name, selected.Definition
	preview.Access = selected.Definition.Access
	if selected.Definition.Access != nil {
		commandPolicy := runnercore.NewCommandPolicy(selected.Definition.Access.Mode == accessModeReview, selected.Definition.Access.DestructiveActions)
		preview.CommandPolicy = &commandPolicy
	}
	preview.Harness, preview.Model, preview.Effort = selected.Definition.Harness, selected.Definition.Model, selected.Definition.Effort
	preview.Source, preview.Reason, preview.Rule = selected.Source, selected.Reason, selected.RuleName
	preview.Warnings = append([]string{}, selected.Warnings...)
	preview.Fallbacks = append([]string{}, selected.Fallbacks...)
	preview.Precedence = []runnerRoutePrecedence{
		{Source: "task frontmatter", Reason: profileField, Selected: selected.Source == "task frontmatter" && selected.Reason == profileField},
		{Source: "task frontmatter", Reason: "runner_profile (legacy)", Selected: selected.Source == "task frontmatter" && strings.Contains(selected.Reason, "runner_profile")},
		{Source: "automation.routing", Reason: "first matching routing rule", Selected: selected.Source == "automation.routing"},
		{Source: "automation.lane_profiles", Reason: "lane mapping", Selected: selected.Source == "automation.lane_profiles"},
		{Source: "automation.model_levels", Reason: "authored or compatible work level", Selected: strings.Contains(selected.Source, ":light") || strings.Contains(selected.Source, ":standard") || strings.Contains(selected.Source, ":demanding")},
		{Source: "task complexity", Reason: "legacy semantic complexity role", Selected: selected.Source == "task complexity"},
		{Source: "automation.default_profile", Reason: "project default or built-in default", Selected: selected.Source == "automation.default_profile" || selected.Source == configSourceBuiltIn},
	}
	return preview
}

// legacyRunnerForLane is the compatibility input used by the daemon's fresh
// run setup. Named task/routing/model-level profiles take precedence inside
// resolveRunProfileForLane; this value only matters when resolution falls back
// to the old workflow runner fields.
func legacyRunnerForLane(note Note, wf Workflow, lane string) string {
	if strings.TrimSpace(lane) == runLaneReview {
		return firstNonEmpty(wf.Reviewer.Runner, wf.Agents.Default)
	}
	return resolveRunnerForNote(note, wf)
}

func runnerRoutePrecedenceTable(lane string) []runnerRoutePrecedence {
	profileField := "execute_profile"
	if lane == runLaneReview {
		profileField = "review_profile"
	}
	return []runnerRoutePrecedence{
		{Source: "task frontmatter", Reason: profileField},
		{Source: "task frontmatter", Reason: "runner_profile (legacy)"},
		{Source: "automation.routing", Reason: "first matching routing rule"},
		{Source: "automation.lane_profiles", Reason: "lane mapping"},
		{Source: "automation.model_levels", Reason: "authored or compatible work level"},
		{Source: "task complexity", Reason: "legacy semantic complexity role"},
		{Source: "automation.default_profile", Reason: "project default or built-in default"},
	}
}

// runnerRouteBlocker preserves the resolver's repair hint and structured path
// in read-only output. Error() alone intentionally contains only the message,
// which made missing mappings look like unexplained generic runner failures.
func runnerRouteBlocker(err error) string {
	if err == nil {
		return ""
	}
	issue := errorToIssue(err)
	message := strings.TrimSpace(issue.Message)
	if message == "" {
		message = strings.TrimSpace(err.Error())
	}
	if issue.Path != "" && !strings.Contains(message, issue.Path) {
		message += " (path: " + issue.Path + ")"
	}
	if issue.Hint != "" {
		message += "; repair: " + strings.TrimSpace(issue.Hint)
	}
	return message
}
