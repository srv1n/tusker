package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	runnercore "tusker/internal/runner"
	"tusker/internal/v7schema"
)

const agentAccessSchemaV1 = "tusker.agent-access/v1"

// Keep these aliases in the command package so profile and API callers use
// exactly the same wire types as the provider-neutral runner boundary.
type AgentAccessV1 = runnercore.AgentAccessV1
type AgentAccessFolder = runnercore.AccessFolder
type AccessControl = runnercore.AccessControl
type ControlSupport = runnercore.ControlSupport
type AccessIssue = runnercore.AccessIssue
type ResolvedAccess = runnercore.ResolvedAccess

const (
	accessModeProjects = "work_in_projects"
	accessModeReview   = "review_only"
)

// AgentAccessResolutionContext contains only runtime-derived paths and
// qualification evidence. None of these values are copied into profile
// defaults.
type AgentAccessResolutionContext struct {
	Workspace          string
	TemporaryDirectory string
	References         []string
	PrivateFolders     []string
	NativeSupportRoots []string
	Controls           []runnercore.ControlSupport
	Route              string
	ExecutableIdentity string
	Transport          string
	Version            string
	Configuration      string
	Revision           string
}

// newAgentAccessDefaults is used only by profile creation. A missing access
// object while reading an existing profile remains legacy behavior.
func newAgentAccessDefaults() *AgentAccessV1 {
	return &AgentAccessV1{
		Schema:             agentAccessSchemaV1,
		Mode:               accessModeProjects,
		Network:            true,
		DestructiveActions: "deny",
		Folders:            []AgentAccessFolder{},
		PrivateFolders:     []string{},
	}
}

func agentAccessFromSchema(in *v7schema.TuskerAgentAccessConfig) *AgentAccessV1 {
	if in == nil {
		return nil
	}
	out := &AgentAccessV1{
		Schema:             strings.TrimSpace(in.Schema),
		Mode:               strings.TrimSpace(in.Mode),
		Network:            in.Network,
		DestructiveActions: strings.TrimSpace(in.DestructiveActions),
		Folders:            make([]AgentAccessFolder, 0, len(in.Folders)),
		PrivateFolders:     cleanAccessPaths(in.PrivateFolders),
	}
	for _, folder := range in.Folders {
		out.Folders = append(out.Folders, AgentAccessFolder{Path: strings.TrimSpace(folder.Path), Access: strings.TrimSpace(folder.Access)})
	}
	return out
}

func validateAgentAccessDefinition(name string, access *AgentAccessV1, path string) error {
	if access == nil {
		return nil
	}
	field := "automation.profiles." + strings.TrimSpace(name) + ".access"
	if strings.TrimSpace(access.Schema) != agentAccessSchemaV1 {
		return tuskerError(errorConfigInvalid, field+".schema must be "+agentAccessSchemaV1, withPath(path), withHint("use the current versioned agent access shape"))
	}
	if access.Mode != accessModeProjects && access.Mode != accessModeReview {
		return tuskerError(errorConfigInvalid, field+".mode must be work_in_projects or review_only", withPath(path))
	}
	if access.DestructiveActions != "ask" && access.DestructiveActions != "deny" {
		return tuskerError(errorConfigInvalid, field+".destructive_actions must be ask or deny", withPath(path))
	}
	for i, folder := range access.Folders {
		if folder.Access != "read" && folder.Access != "write" {
			return tuskerError(errorConfigInvalid, fmt.Sprintf("%s.folders[%d].access must be read or write", field, i), withPath(path))
		}
		if _, err := canonicalAccessPath(folder.Path, false); err != nil {
			return tuskerError(errorConfigInvalid, fmt.Sprintf("%s.folders[%d].path: %v", field, i, err), withPath(path))
		}
	}
	for i, folder := range access.PrivateFolders {
		if _, err := canonicalAccessPath(folder, false); err != nil {
			return tuskerError(errorConfigInvalid, fmt.Sprintf("%s.private_folders[%d]: %v", field, i, err), withPath(path))
		}
	}
	if strings.TrimSpace(access.Mode) == accessModeReview && access.DestructiveActions == "ask" {
		// Review-only is a tightening, not an invalid authoring value. The
		// effective resolver changes it to deny below.
	}
	return nil
}

// validateAgentAccessRaw catches unknown access keys before yaml.v3's
// permissive struct decoder discards them. It is called for each authored
// layer, retaining the existing field-presence-aware merge model.
func validateAgentAccessRaw(raw map[string]any, path string) error {
	automation := mapAny(raw["automation"])
	if automation == nil {
		return nil
	}
	if private, present := automation["private_folders"]; present {
		if _, ok := stringSliceValue(private); !ok {
			return tuskerError(errorConfigInvalid, "automation.private_folders must be a list of absolute paths", withPath(path))
		}
	}
	profiles := mapAny(automation["profiles"])
	for name, value := range profiles {
		profile := mapAny(value)
		if profile == nil {
			continue
		}
		access, present := profile["access"]
		if !present {
			continue
		}
		accessMap := mapAny(access)
		if accessMap == nil {
			return tuskerError(errorConfigInvalid, "automation.profiles."+name+".access must be an object", withPath(path))
		}
		for key := range accessMap {
			switch key {
			case "schema", "mode", "network", "destructive_actions", "folders", "private_folders":
			default:
				return tuskerError(errorConfigInvalid, "automation.profiles."+name+".access has unknown field "+key, withPath(path), withHint("remove the field or use the versioned contract"))
			}
		}
	}
	return nil
}

func stringSliceValue(value any) ([]string, bool) {
	switch values := value.(type) {
	case []string:
		return values, true
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			text, ok := value.(string)
			if !ok {
				return nil, false
			}
			out = append(out, text)
		}
		return out, true
	default:
		return nil, false
	}
}

func cleanAccessPaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	seen := map[string]bool{}
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path != "" && !seen[path] {
			seen[path] = true
			out = append(out, path)
		}
	}
	return out
}

func canonicalAccessPath(path string, requireDirectory bool) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" || !filepath.IsAbs(path) {
		return "", fmt.Errorf("path must be absolute")
	}
	path = filepath.Clean(path)
	if requireDirectory {
		info, err := os.Stat(path)
		if err != nil {
			return "", fmt.Errorf("path is not accessible: %w", err)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("path must be a directory")
		}
	}
	// Resolve the longest existing ancestor. This makes overlap checks
	// symlink-aware while allowing an authored folder to be created later.
	missing := []string{}
	probe := path
	for {
		if _, err := os.Lstat(probe); err == nil {
			resolved, err := filepath.EvalSymlinks(probe)
			if err != nil {
				return "", fmt.Errorf("cannot resolve path: %w", err)
			}
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return filepath.Clean(resolved), nil
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return "", fmt.Errorf("path has no existing ancestor")
		}
		missing = append(missing, filepath.Base(probe))
		probe = parent
	}
}

func accessPathContains(parent, child string) bool {
	parent = filepath.Clean(parent)
	child = filepath.Clean(child)
	if parent == child {
		return true
	}
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
}

func accessPathsOverlap(left, right string) bool {
	return accessPathContains(left, right) || accessPathContains(right, left)
}

// resolveAccess is the single policy projection used by settings, previews,
// and execution callers. Provider native adapters supply Controls later; this
// function only admits a route when required controls are qualified.
func resolveAccess(profile RunnerProfileDefinition, sharedPrivate []string, context AgentAccessResolutionContext) (ResolvedAccess, error) {
	if profile.Access == nil {
		preset := strings.TrimSpace(profile.PermissionPreset)
		if preset == "" {
			preset = legacyPresetFromProfile(profile)
		}
		effective := legacyEffectivePolicy(preset, profile)
		fingerprint := accessFingerprint(runnercore.PermissionPreset(preset), effective, context)
		effective.AccessFingerprint = fingerprint
		return ResolvedAccess{Requested: runnercore.PermissionPreset(preset), Effective: effective, Controls: []runnercore.ControlSupport{}, State: "ready", Issues: []runnercore.AccessIssue{}, Fingerprint: fingerprint}, nil
	}
	if strings.TrimSpace(profile.PermissionPreset) != "" {
		return ResolvedAccess{}, tuskerError(errorConfigInvalid, "profile access and permission_preset are mutually exclusive", withHint("remove permission_preset when using access"))
	}
	if err := validateAgentAccessDefinition("profile", profile.Access, "access"); err != nil {
		return ResolvedAccess{}, err
	}
	workspace, err := canonicalAccessPath(context.Workspace, true)
	if err != nil {
		return ResolvedAccess{}, accessIssueError("access_workspace_invalid", "workspace", err.Error(), "Choose an existing absolute workspace directory.")
	}
	temp := ""
	if strings.TrimSpace(context.TemporaryDirectory) != "" {
		temp, err = canonicalAccessPath(context.TemporaryDirectory, true)
		if err != nil {
			return ResolvedAccess{}, accessIssueError("access_temporary_invalid", "temporary_directory", err.Error(), "Use a Tusker-owned temporary directory.")
		}
	}
	private := append([]string{}, sharedPrivate...)
	private = append(private, profile.Access.PrivateFolders...)
	canonicalPrivate := make([]string, 0, len(private))
	for _, path := range cleanAccessPaths(private) {
		canonical, pathErr := canonicalAccessPath(path, false)
		if pathErr != nil {
			return ResolvedAccess{}, accessIssueError("access_private_path_invalid", "private_folders", pathErr.Error(), "Use an absolute path with an existing ancestor.")
		}
		canonicalPrivate = appendUniquePath(canonicalPrivate, canonical)
	}
	sort.Strings(canonicalPrivate)
	context.PrivateFolders = canonicalPrivate
	// A private ancestor (or the workspace itself) would hide the entire
	// execution root. A private descendant is the intended narrow exclusion and
	// must remain valid.
	privateWorkspaceConflict := false
	for _, privatePath := range canonicalPrivate {
		if accessPathContains(privatePath, workspace) {
			privateWorkspaceConflict = true
			break
		}
	}
	if privateWorkspaceConflict {
		return ResolvedAccess{}, accessIssueError("access_workspace_private_conflict", "private_folders", "the execution workspace is private", "Remove the workspace or its ancestor from private_folders.")
	}

	folders := make([]runnercore.AccessFolder, 0, len(profile.Access.Folders)+2)
	for _, folder := range profile.Access.Folders {
		canonical, pathErr := canonicalAccessPath(folder.Path, false)
		if pathErr != nil {
			return ResolvedAccess{}, accessIssueError("access_folder_invalid", "folders", pathErr.Error(), "Use an absolute path with an existing ancestor.")
		}
		folders = append(folders, runnercore.AccessFolder{Path: canonical, Access: folder.Access})
	}
	folders = append(folders, runnercore.AccessFolder{Path: workspace, Access: "write"})
	for _, reference := range context.References {
		canonical, pathErr := canonicalAccessPath(reference, false)
		if pathErr != nil {
			return ResolvedAccess{}, accessIssueError("access_reference_invalid", "references", pathErr.Error(), "References must be absolute paths with an existing ancestor.")
		}
		folders = append(folders, runnercore.AccessFolder{Path: canonical, Access: "read"})
	}
	if temp != "" {
		folders = append(folders, runnercore.AccessFolder{Path: temp, Access: "write"})
	}
	for _, root := range context.NativeSupportRoots {
		canonical, pathErr := canonicalAccessPath(root, false)
		if pathErr == nil {
			folders = append(folders, runnercore.AccessFolder{Path: canonical, Access: "read"})
		}
	}
	// A private descendant/ancestor always wins. Keep the authored grants in
	// the report and let consumers apply this precedence at the tool boundary.
	for _, folder := range folders {
		if folder.Access == "write" && containsPath(canonicalPrivate, folder.Path) {
			// This is not a conflict: private precedence is the intended narrow
			// exception and remains visible in the effective report.
			continue
		}
	}

	reviewOnly := profile.Access.Mode == accessModeReview
	effective := effectivePolicyForAgentAccess(profile.Access)
	filesystem := effective.Filesystem
	canonicalRefs := canonicalReferences(context.References)
	effective.Workspace, effective.TemporaryDirectory = workspace, temp
	controls := append([]runnercore.ControlSupport{}, context.Controls...)
	issues := []runnercore.AccessIssue{}
	state := "ready"
	if reviewOnly && profile.Access.Network {
		state = "unsupported"
		issues = append(issues, runnercore.AccessIssue{Code: "review_network_unsupported", Field: "access.network", Message: "Review only cannot keep internet On on this route.", Remedy: "Turn internet Off or choose Work in projects."})
	}
	// A route must affirmatively declare every control its request needs. An
	// omitted declaration is not evidence of support: treating it as such
	// silently widens the effective policy when a provider matrix is incomplete.
	required := []struct {
		control runnercore.AccessControl
		needed  bool
	}{
		{runnercore.AccessWorkspaceWrite, filesystem == "workspace-write"},
		{runnercore.AccessReferenceRead, len(context.References) > 0 || accessRequestsExternalRead(profile.Access.Folders, workspace)},
		{runnercore.AccessReferenceWrite, accessRequestsExternalWrite(profile.Access.Folders, workspace)},
		{runnercore.AccessPrivateReadDeny, len(canonicalPrivate) > 0},
		{runnercore.AccessPrivateWriteDeny, len(canonicalPrivate) > 0},
		{runnercore.AccessNetwork, true},
		{runnercore.AccessDestructiveApproval, true},
		{runnercore.AccessReviewOnly, reviewOnly},
	}
	for _, requirement := range required {
		if !requirement.needed {
			continue
		}
		affirmative := false
		for _, support := range controls {
			if support.Control == requirement.control && (support.Mechanism == runnercore.AccessNativeSetting || support.Mechanism == runnercore.AccessNativeHook) {
				affirmative = true
				break
			}
		}
		if !affirmative {
			state = "unsupported"
			issues = append(issues, runnercore.AccessIssue{Code: "access_control_unsupported", Field: string(requirement.control), Message: "required control has no affirmative native support declaration for this route", Remedy: "Choose a qualified route or change the access request."})
		}
	}
	requested := *profile.Access
	fingerprint := accessFingerprint(requested, effective, context)
	effective.AccessFingerprint = fingerprint
	commandPolicy := runnercore.NewCommandPolicy(reviewOnly, profile.Access.DestructiveActions)
	return ResolvedAccess{Requested: requested, Effective: effective, Folders: folders, References: canonicalRefs, PrivateFolders: canonicalPrivate, Controls: controls, CommandPolicy: &commandPolicy, State: state, Issues: issues, Fingerprint: fingerprint}, nil
}

func effectivePolicyForAgentAccess(access *AgentAccessV1) runnercore.EffectivePolicy {
	reviewOnly := access.Mode == accessModeReview
	preset, filesystem := runnercore.PresetWorkspaceOffline, "workspace-write"
	if reviewOnly {
		preset, filesystem = runnercore.PresetReadOnly, "read-only"
	} else if access.Network {
		preset = runnercore.PresetWorkspaceNetwork
	}
	approvals := access.DestructiveActions
	if reviewOnly {
		approvals = "deny"
	}
	return runnercore.EffectivePolicy{
		Preset: preset, Filesystem: filesystem, Network: access.Network && !reviewOnly,
		Approvals: approvals, ReviewOnly: reviewOnly,
	}
}

func legacyPresetFromProfile(profile RunnerProfileDefinition) string {
	if strings.TrimSpace(profile.Sandbox.Mode) == "read-only" {
		return string(runnercore.PresetReadOnly)
	}
	if profile.Sandbox.Network != nil && *profile.Sandbox.Network {
		return string(runnercore.PresetWorkspaceNetwork)
	}
	if profile.Sandbox.Mode == "danger-full-access" {
		return string(runnercore.PresetDangerFullAccess)
	}
	return string(runnercore.PresetWorkspaceOffline)
}

func legacyEffectivePolicy(preset string, profile RunnerProfileDefinition) runnercore.EffectivePolicy {
	policy := runnercore.EffectivePolicy{Preset: runnercore.PermissionPreset(preset), Filesystem: "workspace-write", Approvals: "deny"}
	switch preset {
	case string(runnercore.PresetReadOnly):
		policy.Filesystem, policy.Network = "read-only", false
	case string(runnercore.PresetWorkspaceNetwork):
		policy.Network = true
	case string(runnercore.PresetDangerFullAccess):
		policy.Network, policy.Filesystem, policy.Approvals = true, "danger-full-access", "never"
	}
	return policy
}

func accessRequestsExternalWrite(folders []AgentAccessFolder, workspace string) bool {
	for _, folder := range folders {
		if folder.Access != "write" {
			continue
		}
		canonical, err := canonicalAccessPath(folder.Path, false)
		if err != nil || canonical != workspace {
			return true
		}
	}
	return false
}

func accessRequestsExternalRead(folders []AgentAccessFolder, workspace string) bool {
	for _, folder := range folders {
		if folder.Access != "read" {
			continue
		}
		canonical, err := canonicalAccessPath(folder.Path, false)
		if err != nil || canonical != workspace {
			return true
		}
	}
	return false
}

func canonicalReferences(references []string) []string {
	out := []string{}
	for _, reference := range references {
		if canonical, err := canonicalAccessPath(reference, false); err == nil {
			out = appendUniquePath(out, canonical)
		}
	}
	sort.Strings(out)
	return out
}

// resolvedPrivateFoldersForStart carries the same profile/shared exclusions
// that resolveAccess qualified into a live runner. Keep them canonical so the
// Claude evaluator compares provider paths across symlink aliases correctly.
func resolvedPrivateFoldersForStart(vault string, access *AgentAccessV1) ([]string, error) {
	if access == nil {
		return nil, nil
	}
	paths := append([]string{}, access.PrivateFolders...)
	report, err := modelLevelsRead(vault)
	if err != nil {
		return nil, err
	}
	paths = append(paths, report.PrivateFolders...)
	out := make([]string, 0, len(paths))
	for _, path := range cleanAccessPaths(paths) {
		canonical, pathErr := canonicalAccessPath(path, false)
		if pathErr != nil {
			return nil, pathErr
		}
		out = appendUniquePath(out, canonical)
	}
	sort.Strings(out)
	return out, nil
}

func appendUniquePath(paths []string, path string) []string {
	for _, existing := range paths {
		if existing == path {
			return paths
		}
	}
	return append(paths, path)
}

func containsPath(private []string, candidate string) bool {
	for _, path := range private {
		if accessPathsOverlap(path, candidate) {
			return true
		}
	}
	return false
}

func accessFingerprint(requested any, effective runnercore.EffectivePolicy, context AgentAccessResolutionContext) string {
	value := struct {
		Requested                                                    any
		Effective                                                    runnercore.EffectivePolicy
		References, PrivateFolders                                   []string
		Route, Identity, Transport, Version, Configuration, Revision string
	}{requested, effective, context.References, context.PrivateFolders, context.Route, context.ExecutableIdentity, context.Transport, context.Version, context.Configuration, context.Revision}
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

type accessResolutionError struct{ Issue runnercore.AccessIssue }

func (e *accessResolutionError) Error() string { return e.Issue.Code + ": " + e.Issue.Message }

func accessIssueError(code, field, message, remedy string) error {
	return &accessResolutionError{Issue: runnercore.AccessIssue{Code: code, Field: field, Message: message, Remedy: remedy}}
}
