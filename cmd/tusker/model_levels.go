package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"tusker/internal/v7schema"

	"gopkg.in/yaml.v3"
)

const modelLevelsSchema = "tusker.model-levels/v1"

type ModelLevelDefinition struct {
	Execute []string `json:"execute" yaml:"execute"`
	Review  []string `json:"review" yaml:"review"`
}

type modelLevelValue struct {
	Profiles   []string `json:"profiles"`
	Source     string   `json:"source"`
	Overridden bool     `json:"overridden"`
}

type modelLevelRow struct {
	Level   string          `json:"level"`
	Execute modelLevelValue `json:"execute"`
	Review  modelLevelValue `json:"review"`
}

type modelLevelsReport struct {
	Schema        string                             `json:"schema"`
	Revision      string                             `json:"revision"`
	Profiles      map[string]RunnerProfileDefinition `json:"profiles"`
	ProfileStates map[string]string                  `json:"profile_states"`
	Levels        []modelLevelRow                    `json:"levels"`
}

func modelLevelsFromSchema(in map[string]v7schema.TuskerModelLevelConfig) map[string]ModelLevelDefinition {
	out := map[string]ModelLevelDefinition{}
	for level, value := range in {
		out[strings.ToLower(strings.TrimSpace(level))] = ModelLevelDefinition{Execute: cleanProfileList(value.Execute), Review: cleanProfileList(value.Review)}
	}
	return out
}

func cleanProfileList(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, value := range in {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func validModelLevel(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "light", "standard", "demanding":
		return true
	default:
		return false
	}
}

func modelLevelForNote(note Note, lane string) (string, string, error) {
	field := "work_level"
	if lane == runLaneReview {
		field = "review_level"
	}
	if explicit := strings.ToLower(strings.TrimSpace(stringField(note.Data, field))); explicit != "" {
		if !validModelLevel(explicit) {
			return "", "", tuskerError(errorConfigInvalid, field+" must be light, standard, or demanding")
		}
		return explicit, "task frontmatter", nil
	}
	switch strings.ToLower(strings.TrimSpace(stringField(note.Data, "complexity"))) {
	case "routine":
		return "light", "task complexity", nil
	case "complex", "frontier":
		return "demanding", "task complexity", nil
	default:
		return "standard", "default work level", nil
	}
}

func modelLevelProfiles(note Note, wf Workflow, lane string) ([]ResolvedRunnerProfile, error) {
	level, source, err := modelLevelForNote(note, lane)
	if err != nil {
		return nil, err
	}
	definition := wf.ModelLevels[level]
	names := definition.Execute
	if lane == runLaneReview {
		names = definition.Review
	}
	if len(names) == 0 {
		return nil, tuskerError(errorConfigInvalid, fmt.Sprintf("%s %s profile mapping is empty", level, lane), withHint("configure automation.model_levels."+level+"."+lane))
	}
	out := make([]ResolvedRunnerProfile, 0, len(names))
	for i, name := range names {
		profile, ok := wf.RunnerProfiles[name]
		if !ok {
			return nil, tuskerError(errorConfigInvalid, "model level references unknown runner profile "+name)
		}
		reason := "model level primary"
		if i > 0 {
			reason = "explicit model level fallback"
		}
		out = append(out, ResolvedRunnerProfile{Name: name, Source: source + ":" + level, Reason: reason, Definition: profile})
	}
	return out, nil
}

func modelLevelsRead(vault string) (modelLevelsReport, error) {
	resolved, err := resolveTuskerConfig(vault)
	if err != nil {
		return modelLevelsReport{}, err
	}
	profiles := runnerProfilesFromSchema(resolved.Config.Automation.Profiles)
	states := make(map[string]string, len(profiles))
	for name := range profiles {
		states[name] = "configured_unverified"
	}
	report := modelLevelsReport{Schema: modelLevelsSchema, Revision: configRevision(resolved.Raw), Profiles: profiles, ProfileStates: states, Levels: []modelLevelRow{}}
	for _, level := range []string{"light", "standard", "demanding"} {
		row := modelLevelRow{Level: level}
		for _, lane := range []string{runLaneExecute, runLaneReview} {
			key := "automation.model_levels." + level + "." + lane
			value, source := []string{}, configSourceBuiltIn
			if raw, ok := lookupConfigValue(resolved.Raw, key); ok {
				value = normalizeList(raw)
			}
			for _, layer := range resolved.Layers {
				if _, ok := lookupConfigValue(appliedConfigRaw(layer), key); ok {
					source = layer.Name
				}
			}
			item := modelLevelValue{Profiles: value, Source: source, Overridden: source == configSourceProject || source == configSourceLocal}
			if lane == runLaneExecute {
				row.Execute = item
			} else {
				row.Review = item
			}
		}
		report.Levels = append(report.Levels, row)
	}
	return report, nil
}

func configRevision(raw map[string]any) string {
	encoded, _ := yaml.Marshal(raw)
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func modelsCmd(args Args) error {
	vault, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	report, err := modelLevelsRead(vault)
	if err != nil {
		return err
	}
	if args.Bool("json") {
		if args.Bool("compact") {
			report = compactModelLevelsReport(report)
		}
		emitJSON(report)
		return nil
	}
	for _, row := range report.Levels {
		fmt.Printf("%-9s execute=%s review=%s\n", row.Level, strings.Join(row.Execute.Profiles, ","), strings.Join(row.Review.Profiles, ","))
	}
	return nil
}

func compactModelLevelsReport(report modelLevelsReport) modelLevelsReport {
	wanted := map[string]bool{}
	for _, row := range report.Levels {
		for _, name := range append(append([]string{}, row.Execute.Profiles...), row.Review.Profiles...) {
			wanted[name] = true
		}
	}
	profiles := make(map[string]RunnerProfileDefinition, len(wanted))
	states := make(map[string]string, len(wanted))
	for name := range wanted {
		if profile, ok := report.Profiles[name]; ok {
			profiles[name] = profile
		}
		if state, ok := report.ProfileStates[name]; ok {
			states[name] = state
		}
	}
	report.Profiles, report.ProfileStates = profiles, states
	return report
}

func modelsSetCmd(args Args) error {
	vault, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	level, lane := strings.ToLower(args.String("level")), strings.ToLower(args.String("lane"))
	if scope := firstNonEmpty(args.String("scope"), "project"); scope != "project" && scope != "global" {
		return tuskerError(errorInvalidArg, "--scope must be global or project")
	}
	if !validModelLevel(level) || (lane != runLaneExecute && lane != runLaneReview) {
		return tuskerError(errorInvalidArg, "--level must be light, standard, or demanding and --lane must be execute or review")
	}
	profiles := cleanProfileList(strings.Split(args.String("profiles"), ","))
	if len(profiles) == 0 {
		return tuskerError(errorInvalidArg, "--profiles requires one or more ordered profile names")
	}
	current, err := modelLevelsRead(vault)
	if err != nil {
		return err
	}
	if expected := strings.TrimSpace(args.String("if-revision")); expected != "" && expected != current.Revision {
		return tuskerError(errorInvalidTransition, "model settings changed; refresh before saving")
	}
	resolved, err := resolveTuskerConfig(vault)
	if err != nil {
		return err
	}
	for _, name := range profiles {
		if _, ok := resolved.Config.Automation.Profiles[name]; !ok {
			return tuskerError(errorConfigInvalid, "unknown runner profile "+name)
		}
	}
	key := "automation.model_levels." + level + "." + lane
	if args.String("scope") == "global" {
		_, err = setUserGlobalConfigWithReadback(key, profiles)
	} else {
		_, err = setProjectLocalConfigWithReadback(vault, key, profiles)
	}
	if err != nil {
		return err
	}
	if args.Bool("_no-output") {
		return nil
	}
	out := Args{"vault": vault}
	if args.Bool("json") {
		out["json"] = "true"
	}
	return modelsCmd(out)
}

func modelsResetCmd(args Args) error {
	vault, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	level, lane := strings.ToLower(args.String("level")), strings.ToLower(args.String("lane"))
	if scope := firstNonEmpty(args.String("scope"), "project"); scope != "project" && scope != "global" {
		return tuskerError(errorInvalidArg, "--scope must be global or project")
	}
	if !validModelLevel(level) || (lane != runLaneExecute && lane != runLaneReview) {
		return tuskerError(errorInvalidArg, "invalid level or lane")
	}
	path := managedTuskerLocalConfigPath(vault)
	if args.String("scope") == "global" {
		path = userGlobalTuskerConfigPath()
	}
	if expected := strings.TrimSpace(args.String("if-revision")); expected != "" {
		current, readErr := modelLevelsRead(vault)
		if readErr != nil {
			return readErr
		}
		if current.Revision != expected {
			return tuskerError(errorInvalidTransition, "model settings changed; refresh before saving")
		}
	}
	if err := removeConfigKey(path, "automation.model_levels."+level+"."+lane); err != nil {
		return err
	}
	if args.Bool("_no-output") {
		return nil
	}
	out := Args{"vault": vault}
	if args.Bool("json") {
		out["json"] = "true"
	}
	return modelsCmd(out)
}

func modelsProfileSetCmd(args Args) error {
	vault, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	name := strings.TrimSpace(args.String("name"))
	if scope := firstNonEmpty(args.String("scope"), "project"); scope != "project" && scope != "global" {
		return tuskerError(errorInvalidArg, "--scope must be global or project")
	}
	if name == "" {
		return tuskerError(errorMissingArg, "models profile-set requires --name")
	}
	current, err := modelLevelsRead(vault)
	if err != nil {
		return err
	}
	if expected := strings.TrimSpace(args.String("if-revision")); expected != "" && expected != current.Revision {
		return tuskerError(errorInvalidTransition, "model settings changed; refresh before saving")
	}
	preset := firstNonEmpty(strings.TrimSpace(args.String("preset")), "workspace-write-offline")
	mode, network := "workspace-write", false
	if preset == "read-only" {
		mode = "read-only"
	}
	if preset == "workspace-write-network" {
		network = true
	}
	if preset == "danger-full-access" {
		mode, network = "danger-full-access", true
	}
	profile := RunnerProfileDefinition{Harness: strings.TrimSpace(args.String("harness")), Model: strings.TrimSpace(args.String("model")), Effort: strings.TrimSpace(args.String("effort")), PermissionPreset: preset, Command: strings.TrimSpace(args.String("command")), Sandbox: RunnerSandboxDefinition{Mode: mode, Network: boolPtr(network)}, Subagents: RunnerSubagentPolicyDefinition{Allowed: boolPtr(false)}}
	if err := validateRunnerProfileDefinition(name, profile, "models profile-set"); err != nil {
		return err
	}
	key := "automation.profiles." + name
	if args.String("scope") == "global" {
		_, err = setUserGlobalConfigWithReadback(key, profile)
	} else {
		_, err = setProjectLocalConfigWithReadback(vault, key, profile)
	}
	if err != nil {
		return err
	}
	if args.Bool("_no-output") {
		return nil
	}
	out := Args{"vault": vault}
	if args.Bool("json") {
		out["json"] = "true"
	}
	return modelsCmd(out)
}

func removeConfigKey(path, key string) error {
	raw, _, err := readTuskerConfigRawLayer(path)
	if err != nil {
		return err
	}
	if raw == nil {
		return nil
	}
	removeNestedConfigKey(raw, strings.Split(key, "."))
	encoded, err := yaml.Marshal(raw)
	if err != nil {
		return err
	}
	return writeConfigTextAtomically(path, string(encoded))
}

func removeNestedConfigKey(current map[string]any, parts []string) bool {
	if len(parts) == 1 {
		delete(current, parts[0])
		return len(current) == 0
	}
	next, ok := current[parts[0]].(map[string]any)
	if !ok {
		return len(current) == 0
	}
	if removeNestedConfigKey(next, parts[1:]) {
		delete(current, parts[0])
	}
	return len(current) == 0
}

func preserveResolvedRunIdentity(run RunStatus, lane string, selected ResolvedRunnerProfile) ResolvedRunnerProfile {
	if run.AttemptCount == 0 || strings.TrimSpace(run.RunnerProfile) == "" || strings.TrimSpace(run.Lane) != strings.TrimSpace(lane) {
		return selected
	}
	selected.Name = run.RunnerProfile
	selected.Source = "recorded task cycle"
	selected.Reason = "retry preserves resolved identity"
	selected.Fallbacks = nil
	selected.Definition.Harness = firstNonEmpty(run.RunnerHarness, run.Runner)
	selected.Definition.Model = run.RunnerModel
	selected.Definition.Effort = run.RunnerEffort
	return selected
}

func resolvedProfileCandidates(selected ResolvedRunnerProfile, wf Workflow) ([]ResolvedRunnerProfile, error) {
	out := []ResolvedRunnerProfile{selected}
	for _, name := range selected.Fallbacks {
		definition, ok := wf.RunnerProfiles[name]
		if !ok {
			return nil, tuskerError(errorConfigInvalid, "fallback runner profile "+name+" is not defined")
		}
		out = append(out, ResolvedRunnerProfile{Name: name, Source: selected.Source, Reason: "explicit model level fallback", Definition: definition})
	}
	return out, nil
}

func selectModelProfile(candidates []ResolvedRunnerProfile, probe func(ResolvedRunnerProfile) (bool, string, error)) (ResolvedRunnerProfile, string, error) {
	var unavailable []string
	for _, candidate := range candidates {
		ok, reason, err := probe(candidate)
		if err != nil {
			return ResolvedRunnerProfile{}, "", err
		}
		if ok {
			if len(unavailable) == 0 {
				return candidate, "", nil
			}
			return candidate, strings.Join(unavailable, "; "), nil
		}
		unavailable = append(unavailable, candidate.Name+": "+reason)
	}
	return ResolvedRunnerProfile{}, strings.Join(unavailable, "; "), tuskerError(errorInvalidTransition, "all explicitly configured model profiles are unavailable")
}
