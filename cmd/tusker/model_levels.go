package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

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
	Schema                 string                             `json:"schema"`
	Revision               string                             `json:"revision"`
	Profiles               map[string]RunnerProfileDefinition `json:"profiles"`
	ProfileStates          map[string]string                  `json:"profile_states"`
	ProfileReferences      map[string][]string                `json:"profile_references"`
	ReferenceCheckComplete bool                               `json:"reference_check_complete"`
	PrivateFolders         []string                           `json:"private_folders"`
	Levels                 []modelLevelRow                    `json:"levels"`
	Warnings               []string                           `json:"warnings,omitempty"`
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
	if _, explicitlyUnclassified := note.Data["work_level"]; explicitlyUnclassified && strings.TrimSpace(stringField(note.Data, "work_level")) == "" {
		return "", "task frontmatter", tuskerError(errorConfigInvalid, "work_level is required for agent work; use light, standard, or demanding")
	}
	if explicit := strings.ToLower(strings.TrimSpace(stringField(note.Data, field))); explicit != "" {
		if !validModelLevel(explicit) {
			return "", "", tuskerError(errorConfigInvalid, field+" must be light, standard, or demanding")
		}
		return explicit, "task frontmatter", nil
	}
	if lane == runLaneReview {
		if work := strings.ToLower(strings.TrimSpace(stringField(note.Data, "work_level"))); work != "" {
			if !validModelLevel(work) {
				return "", "", tuskerError(errorConfigInvalid, "work_level must be light, standard, or demanding")
			}
			return work, "task work level", nil
		}
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
			return nil, tuskerError(errorConfigInvalid, "automation.model_levels."+level+" references unknown profile "+name+"; define it in the global config "+userGlobalTuskerConfigPath())
		}
		if profile.EligibleTiers != nil && !containsString(profile.EligibleTiers, level) {
			return nil, tuskerError(errorConfigInvalid, fmt.Sprintf("runner profile %s is not eligible for %s", name, level), withHint("add tier eligibility before assigning the profile"))
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
	// Profiles written before eligible_tiers existed inherit membership only from
	// their existing tier references. An explicit [] remains intentionally unassigned.
	for name, profile := range profiles {
		if profile.DisplayName == "" {
			profile.DisplayName = generatedProfileDisplayName(profile.Harness, profile.Model, name)
		}
		if profile.EligibleTiers != nil {
			profiles[name] = profile
			continue
		}
		for level, mapping := range resolved.Config.Automation.ModelLevels {
			if containsString(mapping.Execute, name) || containsString(mapping.Review, name) {
				profile.EligibleTiers = append(profile.EligibleTiers, level)
			}
		}
		profile.EligibleTiers = cleanProfileList(profile.EligibleTiers)
		profiles[name] = profile
	}
	states := make(map[string]string, len(profiles))
	for name, profile := range profiles {
		states[name] = profileTestState(name, profile, configRevision(resolved.Raw))
	}
	references, complete := modelProfileReferences(vault, resolved)
	privateFolders := []string{}
	if raw, ok := lookupConfigValue(resolved.Raw, "automation.private_folders"); ok {
		privateFolders, _ = stringSliceValue(raw)
		privateFolders = cleanAccessPaths(privateFolders)
	}
	report := modelLevelsReport{Schema: modelLevelsSchema, Revision: configRevision(resolved.Raw), Profiles: profiles, ProfileStates: states, ProfileReferences: references, ReferenceCheckComplete: complete, PrivateFolders: privateFolders, Levels: []modelLevelRow{}, Warnings: resolved.Warnings}
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

func modelLevelsReadForScope(vault, scope string) (modelLevelsReport, error) {
	report, err := modelLevelsRead(vault)
	if err != nil || scope != "global" {
		return report, err
	}
	refs, complete := modelProfileReferencesForScope(vault, scope)
	report.ProfileReferences = refs
	report.ReferenceCheckComplete = complete
	return report, nil
}

func profileTestState(name string, profile RunnerProfileDefinition, revision string) string {
	if profile.Disabled {
		return "disabled"
	}
	if RunnerName(profile.Harness) == RunnerClaude {
		return "unavailable"
	}
	var report struct {
		ProfileID       string     `json:"profile_id"`
		ProfileRevision string     `json:"profile_revision"`
		Model           string     `json:"model"`
		Effort          string     `json:"effort"`
		Preset          string     `json:"preset"`
		Live            bool       `json:"live"`
		Ready           bool       `json:"ready"`
		ValidUntil      *time.Time `json:"valid_until"`
	}
	raw, err := os.ReadFile(filepath.Join(DefaultStateRoot(), "runner-conformance", name+"-"+profile.PermissionPreset+".json"))
	if err != nil || json.Unmarshal(raw, &report) != nil || !report.Live || !report.Ready || report.ValidUntil == nil || !report.ValidUntil.After(time.Now().UTC()) {
		return "configured_unverified"
	}
	if report.ProfileID != name || report.ProfileRevision != revision || report.Model != profile.Model || report.Effort != profile.Effort || report.Preset != profile.PermissionPreset {
		return "configured_unverified"
	}
	return "tested"
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
	lock, err := acquireModelSettingsLock(vault, firstNonEmpty(args.String("scope"), "project"))
	if err != nil {
		return err
	}
	defer lock.Close()
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
	if expected := strings.TrimSpace(args.String("if-revision")); expected == "" || expected != current.Revision {
		return tuskerError(errorInvalidTransition, "model settings changed; refresh before saving")
	}
	for _, name := range profiles {
		profile, ok := current.Profiles[name]
		if !ok {
			return tuskerError(errorConfigInvalid, "unknown runner profile "+name)
		}
		if profile.Disabled {
			return tuskerError(errorInvalidTransition, "runner profile "+name+" is disabled", withHint("enable it before assigning it"))
		}
		if !containsString(profile.EligibleTiers, level) {
			return tuskerError(errorConfigInvalid, fmt.Sprintf("runner profile %s is not eligible for %s", name, level), withHint("save the profile with that tier membership first"))
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
	lock, err := acquireModelSettingsLock(vault, firstNonEmpty(args.String("scope"), "project"))
	if err != nil {
		return err
	}
	defer lock.Close()
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
	} else {
		return tuskerError(errorInvalidTransition, "model settings changed; refresh before saving")
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
	if err := requireGlobalProfileScope(args); err != nil {
		return err
	}
	if name == "" {
		return tuskerError(errorMissingArg, "models profile-set requires --name")
	}
	lock, err := acquireModelSettingsLock(vault, "global")
	if err != nil {
		return err
	}
	defer lock.Close()
	current, err := modelLevelsRead(vault)
	if err != nil {
		return err
	}
	if expected := strings.TrimSpace(args.String("if-revision")); expected == "" || expected != current.Revision {
		return tuskerError(errorInvalidTransition, "model settings changed; refresh before saving")
	}
	presetArg := strings.TrimSpace(args.String("preset"))
	preset := firstNonEmpty(presetArg, "workspace-write-offline")
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
	profile := RunnerProfileDefinition{DisplayName: strings.TrimSpace(args.String("display-name")), EligibleTiers: cleanProfileList(strings.Split(args.String("eligible-tiers"), ",")), Harness: strings.TrimSpace(args.String("harness")), Model: strings.TrimSpace(args.String("model")), Effort: strings.TrimSpace(args.String("effort")), PermissionPreset: preset, Command: strings.TrimSpace(args.String("command")), Sandbox: RunnerSandboxDefinition{Mode: mode, Network: boolPtr(network)}, Subagents: RunnerSubagentPolicyDefinition{Allowed: boolPtr(false)}}
	rawAccess := strings.TrimSpace(args.String("access"))
	_, existingProfile := current.Profiles[name]
	if rawAccess != "" {
		var access AgentAccessV1
		if err := json.Unmarshal([]byte(rawAccess), &access); err != nil {
			return tuskerError(errorInvalidArg, "models profile-set access must be a valid JSON object")
		}
		profile.Access = &access
		profile.PermissionPreset = ""
		profile.Sandbox = RunnerSandboxDefinition{}
	} else if existing, ok := current.Profiles[name]; ok && existing.Access != nil && presetArg == "" {
		// Editing model/display/tier metadata must not silently migrate a new
		// access profile back to the legacy preset authority.
		profile.Access = existing.Access
		profile.PermissionPreset = ""
		profile.Sandbox = existing.Sandbox
	} else if !existingProfile && presetArg == "" {
		// A newly created ordinary profile receives the access defaults. An
		// explicit legacy preset remains an explicit request for legacy mode.
		profile.Access = newAgentAccessDefaults()
		profile.PermissionPreset = ""
		profile.Sandbox = RunnerSandboxDefinition{}
	}
	if existing, ok := current.Profiles[name]; ok {
		profile.Disabled = existing.Disabled
		if profile.DisplayName == "" {
			profile.DisplayName = existing.DisplayName
		}
		if _, supplied := args["eligible-tiers"]; !supplied {
			profile.EligibleTiers = existing.EligibleTiers
		}
	}
	if profile.DisplayName == "" {
		profile.DisplayName = generatedProfileDisplayName(profile.Harness, profile.Model, name)
	}
	for tier, mapping := range currentLevelMappings(current) {
		if containsString(mapping, name) && !containsString(profile.EligibleTiers, tier) {
			return tuskerError(errorInvalidTransition, fmt.Sprintf("runner profile %s is still assigned to %s", name, tier), withHint("replace the tier assignment before removing membership"))
		}
	}
	if err := validateRunnerProfileDefinition(name, profile, "models profile-set"); err != nil {
		return err
	}
	if err := changeRemovedProfile(name, false); err != nil {
		return err
	}
	if _, err = setUserGlobalConfigWithReadback("automation.profiles."+name, profile); err != nil {
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

// modelsPrivateFoldersSetCmd keeps shared private-folder settings in the
// existing revision-checked model settings document. It deliberately accepts
// only absolute paths and never creates or deletes any path.
func modelsPrivateFoldersSetCmd(args Args) error {
	vault, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	scope := firstNonEmpty(args.String("scope"), "project")
	if scope != "project" && scope != "global" {
		return tuskerError(errorInvalidArg, "--scope must be global or project")
	}
	lock, err := acquireModelSettingsLock(vault, scope)
	if err != nil {
		return err
	}
	defer lock.Close()
	current, err := modelLevelsRead(vault)
	if err != nil {
		return err
	}
	if expected := strings.TrimSpace(args.String("if-revision")); expected == "" || expected != current.Revision {
		return tuskerError(errorInvalidTransition, "model settings changed; refresh before saving")
	}
	paths := cleanAccessPaths(strings.Split(args.String("private-folders"), ","))
	for _, path := range paths {
		if _, pathErr := canonicalAccessPath(path, false); pathErr != nil {
			return tuskerError(errorInvalidArg, "private_folders: "+pathErr.Error(), withHint("use absolute paths with an existing ancestor"))
		}
	}
	key := "automation.private_folders"
	if scope == "global" {
		_, err = setUserGlobalConfigWithReadback(key, paths)
	} else {
		_, err = setProjectLocalConfigWithReadback(vault, key, paths)
	}
	return err
}

func readableProfileName(id string) string {
	return strings.Title(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(id), "-", " "), "_", " ")) //nolint:staticcheck -- stable compatibility with supported Go versions
}

func generatedProfileDisplayName(harness, model, fallback string) string {
	provider := map[string]string{string(RunnerCodexExec): "Codex", string(RunnerMuse): "Muse"}[strings.TrimSpace(harness)]
	if provider == "" {
		provider = readableProfileName(harness)
	}
	model = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(model), "gpt-"))
	if provider != "" && model != "" {
		return provider + " · " + readableProfileName(model)
	}
	return readableProfileName(fallback)
}

type modelSettingsLock []*v7DocumentLock

func (locks modelSettingsLock) Close() error {
	var first error
	for i := len(locks) - 1; i >= 0; i-- {
		if err := locks[i].Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// acquireModelSettingsLock serializes model settings writes. Global-scope
// writes also lock the user-global config itself, which every project shares;
// the project epoch lock alone would let two projects race on that file. The
// global lock is always taken last, so lock order is consistent.
func acquireModelSettingsLock(vault, scope string) (modelSettingsLock, error) {
	epoch, err := acquireV7MaterialEpochLock(vault)
	if err != nil {
		return nil, err
	}
	if scope != "global" {
		return modelSettingsLock{epoch}, nil
	}
	path := userGlobalTuskerConfigPath()
	abs, err := filepath.Abs(path)
	if err != nil {
		_ = epoch.Close()
		return nil, err
	}
	global, err := acquireV7LockForIdentity("user-global-config:"+abs, path, v7DocumentLockTimeout)
	if err != nil {
		_ = epoch.Close()
		return nil, err
	}
	return modelSettingsLock{epoch, global}, nil
}

func currentLevelMappings(report modelLevelsReport) map[string][]string {
	out := map[string][]string{}
	for _, row := range report.Levels {
		out[row.Level] = cleanProfileList(append(append([]string{}, row.Execute.Profiles...), row.Review.Profiles...))
	}
	return out
}

func modelsProfileLifecycleCmd(args Args, action string) error {
	vault, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	name := strings.TrimSpace(args.String("name"))
	if name == "" {
		return tuskerError(errorMissingArg, "models "+action+" requires --name")
	}
	if err := requireGlobalProfileScope(args); err != nil {
		return err
	}
	path := userGlobalTuskerConfigPath()
	lock, err := acquireModelSettingsLock(vault, "global")
	if err != nil {
		return err
	}
	defer lock.Close()
	current, err := modelLevelsRead(vault)
	if err != nil {
		return err
	}
	if expected := strings.TrimSpace(args.String("if-revision")); expected == "" || expected != current.Revision {
		return tuskerError(errorInvalidTransition, "model settings changed; refresh before saving")
	}
	profile, ok := current.Profiles[name]
	if !ok {
		return tuskerError(errorNotFound, "runner profile "+name+" is not defined")
	}
	key := "automation.profiles." + name
	switch action {
	case "profile-disable", "profile-enable":
		profile.Disabled = action == "profile-disable"
		_, err = setUserGlobalConfigWithReadback(key, profile)
	case "profile-remove":
		if err = refuseReferencedProfileRemoval(vault, name); err != nil {
			return err
		}
		if err = changeRemovedProfile(name, true); err == nil {
			err = removeConfigKey(path, key)
		}
	}
	if err != nil {
		return err
	}
	if !args.Bool("_no-output") {
		out := Args{"vault": vault}
		if args.Bool("json") {
			out["json"] = "true"
		}
		return modelsCmd(out)
	}
	return nil
}

// refuseReferencedProfileRemoval keeps a global profile from silently vanishing
// out of any registered project's tier mappings or unstarted task overrides.
// When some project cannot be checked, removal is refused rather than claimed
// safe; disabling keeps references intact.
func refuseReferencedProfileRemoval(vault, name string) error {
	refs, complete := modelProfileReferencesForScope(vault, "global")
	if len(refs[name]) > 0 {
		return tuskerError(errorInvalidTransition, fmt.Sprintf("runner profile %s is still referenced by %s", name, strings.Join(refs[name], ", ")), withHint("replace or remove those references first, or disable the profile instead"))
	}
	if !complete {
		return tuskerError(errorInvalidTransition, "runner profile "+name+" references could not be checked in every registered project", withHint("disable the profile instead, or retry once every registered project is readable"))
	}
	return nil
}

// requireGlobalProfileScope enforces that runner profile definitions live only
// in the user-global config; a project may only select them.
func requireGlobalProfileScope(args Args) error {
	if scope := firstNonEmpty(args.String("scope"), "global"); scope != "global" {
		return tuskerError(errorInvalidArg, "runner profiles can only be defined in the global config "+userGlobalTuskerConfigPath(), withHint("use --scope global; a project selects global profiles through model level mappings"))
	}
	return nil
}

func changeRemovedProfile(name string, removed bool) error {
	raw, _, err := readTuskerConfigRawLayer(userGlobalTuskerConfigPath())
	if err != nil {
		return err
	}
	removedValue, _ := lookupConfigValue(raw, "automation.removed_profiles")
	profiles := cleanProfileList(normalizeList(removedValue))
	if removed {
		if containsString(profiles, name) {
			return nil
		}
		profiles = append(profiles, name)
	} else {
		updated := removeProfileNames(profiles, []string{name})
		if len(updated) == len(profiles) {
			return nil
		}
		profiles = updated
	}
	_, err = setUserGlobalConfigWithReadback("automation.removed_profiles", profiles)
	return err
}

func modelProfileReferences(vault string, resolved resolvedTuskerConfig) (map[string][]string, bool) {
	refs := map[string][]string{}
	add := func(name, ref string) {
		name = strings.TrimSpace(name)
		if name != "" {
			refs[name] = append(refs[name], ref)
		}
	}
	// Built-in defaults are not user references: removing a seeded profile
	// intentionally drops it from the defaults (applyRemovedProfiles).
	configured := func(key string) bool {
		for _, layer := range resolved.Layers {
			if layer.Name == configSourceBuiltIn {
				continue
			}
			if _, ok := lookupConfigValue(appliedConfigRaw(layer), key); ok {
				return true
			}
		}
		return false
	}
	for level, mapping := range resolved.Config.Automation.ModelLevels {
		for lane, names := range map[string][]string{"execute": mapping.Execute, "review": mapping.Review} {
			if !configured("automation.model_levels." + level + "." + lane) {
				continue
			}
			for _, name := range names {
				add(name, "model_levels."+level+"."+lane)
			}
		}
	}
	if configured("automation.default_profile") {
		add(resolved.Config.Automation.DefaultProfile, "default_profile")
	}
	for lane, name := range resolved.Config.Automation.LaneProfiles {
		if configured("automation.lane_profiles." + lane) {
			add(name, "lane_profiles."+lane)
		}
	}
	if configured("automation.routing") {
		for _, rule := range resolved.Config.Automation.Routing {
			add(rule.Profile, "routing."+rule.Name)
		}
	}
	idx, err := loadV7Index(vault)
	if err != nil {
		return refs, false
	}
	for id, task := range idx.Tasks {
		if !v7TerminalTaskStatus(stringField(task.Data, "status")) {
			add(stringField(task.Data, "execute_profile"), "task."+id+".execute_profile")
			add(stringField(task.Data, "review_profile"), "task."+id+".review_profile")
			add(stringField(task.Data, "runner_profile"), "task."+id+".runner_profile")
		}
	}
	for name := range refs {
		sort.Strings(refs[name])
	}
	return refs, true
}

func modelProfileReferencesForScope(vault, scope string) (map[string][]string, bool) {
	resolved, err := resolveTuskerConfig(vault)
	if err != nil {
		return map[string][]string{}, false
	}
	refs, complete := modelProfileReferences(vault, resolved)
	if scope != "global" {
		return refs, complete
	}

	store, err := OpenRuntimeStoreReadOnly(DefaultStateRoot())
	if errors.Is(err, fs.ErrNotExist) {
		return refs, complete // no runtime store: no other registered projects
	}
	if err != nil {
		return refs, false
	}
	defer store.Close()
	projects, err := loadRegisteredProjects(store, registeredProjectLoadOptions{MetadataOnly: true})
	if err != nil {
		return refs, false
	}
	for _, loaded := range projects {
		project := loaded.Project
		if sameCanonicalProjectPath(project.VaultRoot, vault) {
			continue
		}
		projectResolved, resolveErr := resolveTuskerConfig(project.VaultRoot)
		if resolveErr != nil {
			complete = false
			continue
		}
		projectRefs, projectComplete := modelProfileReferences(project.VaultRoot, projectResolved)
		complete = complete && projectComplete
		for name, values := range projectRefs {
			for _, value := range values {
				if !containsString(refs[name], value) {
					refs[name] = append(refs[name], project.ProjectID+": "+value)
				}
			}
		}
	}
	for name := range refs {
		sort.Strings(refs[name])
	}
	return refs, complete
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
		if candidate.Definition.Disabled {
			unavailable = append(unavailable, candidate.Name+": disabled")
			continue
		}
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
