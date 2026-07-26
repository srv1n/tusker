package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const completionWorkspaceTrustKeyToken = "{{completion_workspace_trust_key}}"

// completionWorkerSafety is intentionally stricter than generic automation:
// completion consumes a reviewer verdict as lifecycle authority, so every
// profile participating in that flow must have an enforceable filesystem
// boundary.  A friendly name or a denylist is not a sandbox.
func completionWorkerSafety(stateRoot, workspace string, profile ResolvedRunnerProfile) error {
	mode := strings.TrimSpace(profile.Definition.Sandbox.Mode)
	if mode != "workspace-write" && mode != "read-only" {
		return fmt.Errorf("completion authority refuses profile %q: enforceable sandbox must be workspace-write or read-only", profile.Name)
	}
	if strings.TrimSpace(profile.Definition.PermissionPreset) == "danger-full-access" || mode == "danger-full-access" {
		return fmt.Errorf("completion authority refuses profile %q: danger-full-access is not admissible", profile.Name)
	}
	if profile.Definition.Sandbox.Network == nil || *profile.Definition.Sandbox.Network {
		return fmt.Errorf("completion authority refuses profile %q: worker network must be explicitly disabled", profile.Name)
	}
	// Only codex_exec currently translates the resolved policy into Codex's
	// detached-command sandbox flags.  Codex's generic/app-server paths and
	// Claude merely receive metadata (Claude defaults to bypassPermissions), so
	// treating their sandbox strings as authority would be security theatre.
	if RunnerName(strings.TrimSpace(profile.Definition.Harness)) != RunnerCodexExec {
		return fmt.Errorf("completion authority refuses profile %q: runner does not mechanically enforce the resolved sandbox", profile.Name)
	}
	if strings.TrimSpace(stateRoot) == "" || strings.TrimSpace(workspace) == "" || pathWithin(workspace, stateRoot) {
		return fmt.Errorf("completion authority requires daemon state root outside worker-writable workspace")
	}
	return nil
}

func completionWorkerSafetyForLane(stateRoot, workspace, lane, command string, profile ResolvedRunnerProfile) error {
	if err := completionWorkerSafety(stateRoot, workspace, profile); err != nil {
		return err
	}
	if lane == runLaneReview && strings.TrimSpace(profile.Definition.Sandbox.Mode) != "read-only" {
		return fmt.Errorf("completion authority requires read-only review profile")
	}
	if strings.TrimSpace(profile.Definition.Command) != "" {
		return fmt.Errorf("completion authority refuses profile %q: project-defined runner command is not admissible", profile.Name)
	}
	if _, err := completionAuthoritativeCodexExecArgv(command, lane, profile); err != nil {
		return err
	}
	return nil
}

func completionAuthoritativeCodexExecArgv(command, lane string, profile ResolvedRunnerProfile) ([]string, error) {
	if command != defaultCodexExecCommand() {
		return nil, fmt.Errorf("completion authority requires the exact built-in codex exec command")
	}
	mode := strings.TrimSpace(profile.Definition.Sandbox.Mode)
	if lane == runLaneReview && mode != "read-only" {
		return nil, fmt.Errorf("completion authority requires read-only review profile")
	}
	if lane == runLaneExecute && mode != "workspace-write" && mode != "read-only" {
		return nil, fmt.Errorf("completion authority requires workspace-write or read-only execute profile")
	}
	if lane != runLaneExecute && lane != runLaneReview {
		return nil, fmt.Errorf("completion authority refuses unknown worker lane %q", lane)
	}
	if profile.Definition.Sandbox.Network == nil || *profile.Definition.Sandbox.Network {
		return nil, fmt.Errorf("completion authority requires explicit network=false")
	}

	// This is a policy template, not a shell fragment. At dispatch the daemon
	// replaces the trust-key token with the canonical workspace path and argv[0]
	// with the physical executable that passed preflight. Ignoring user config
	// preserves CODEX_HOME authentication but removes user trust declarations;
	// the explicit untrusted workspace override then disables that repository's
	// .codex config (including project hooks and MCP declarations). The feature
	// and rules switches are independent defense in depth.
	argv := []string{
		"codex", "exec",
		"--ignore-user-config",
		"--ignore-rules",
		"--disable", "hooks",
		"--strict-config",
		"--json",
		"--skip-git-repo-check",
		"-c", `projects.` + completionWorkspaceTrustKeyToken + `.trust_level="untrusted"`,
		"-c", `approval_policy="never"`,
		"-c", `sandbox_mode="` + mode + `"`,
		"-c", `sandbox_workspace_write.network_access=false`,
	}
	if model := strings.TrimSpace(profile.Definition.Model); model != "" {
		argv = append(argv, "--model", model)
	}
	if effort := strings.TrimSpace(profile.Definition.Effort); effort != "" {
		argv = append(argv, "-c", `model_reasoning_effort="`+effort+`"`)
	}
	return append(argv, "-"), nil
}

func completionBindAuthoritativeCodexExec(command string, argv []string, workspace, repoRoot string) ([]string, string, string, error) {
	if len(argv) < 2 || argv[0] != "codex" || argv[1] != "exec" {
		return nil, "", "", fmt.Errorf("completion authority requires the canonical codex exec argv")
	}
	materialized, err := completionMaterializeCodexTrustArgv(argv, workspace)
	if err != nil {
		return nil, "", "", err
	}
	searchPath, err := completionAuthoritativeRunnerSearchPath(workspace, repoRoot)
	if err != nil {
		return nil, "", "", err
	}
	preflight, reason := runnerCommandPreflightWithSearchPath(RunnerCodexExec, command, searchPath)
	if reason != "" {
		return nil, "", "", fmt.Errorf("%s", reason)
	}
	executable, fingerprint, err := completionExecutableIdentity(preflight.ResolvedExecutable, preflight.ExecutableVersion)
	if err != nil {
		return nil, "", "", err
	}
	if pathWithin(workspace, executable) || (strings.TrimSpace(repoRoot) != "" && pathWithin(repoRoot, executable)) {
		return nil, "", "", fmt.Errorf("completion authority refuses a codex executable from the worker workspace or repository")
	}
	materialized[0] = executable
	return materialized, fingerprint, preflight.SearchPath, nil
}

func completionAuthoritativeRunnerSearchPath(workspace, repoRoot string) (string, error) {
	out := []string{}
	for _, entry := range filepath.SplitList(runnerCommandSearchPathWithoutLogin()) {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		absolute, err := filepath.Abs(entry)
		if err != nil {
			return "", err
		}
		absolute = canonicalPath(absolute)
		if pathWithin(workspace, absolute) || (strings.TrimSpace(repoRoot) != "" && pathWithin(repoRoot, absolute)) {
			continue
		}
		out = append(out, absolute)
	}
	out = uniquePathStrings(out)
	if len(out) == 0 {
		return "", fmt.Errorf("completion authority could not construct a non-login runner search path outside the worker repository")
	}
	return strings.Join(out, string(os.PathListSeparator)), nil
}

func completionMaterializeCodexTrustArgv(argv []string, workspace string) ([]string, error) {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return nil, fmt.Errorf("completion authority requires a worker workspace trust key")
	}
	absolute, err := filepath.Abs(workspace)
	if err != nil {
		return nil, err
	}
	absolute = canonicalPath(absolute)
	if !filepath.IsAbs(absolute) {
		return nil, fmt.Errorf("completion authority requires an absolute worker workspace trust key")
	}
	if !strings.Contains(strings.Join(argv, "\x00"), completionWorkspaceTrustKeyToken) {
		return nil, fmt.Errorf("completion authority codex argv is missing its workspace trust override")
	}
	quoted, err := json.Marshal(absolute)
	if err != nil {
		return nil, err
	}
	out := append([]string(nil), argv...)
	for i := range out {
		out[i] = strings.ReplaceAll(out[i], completionWorkspaceTrustKeyToken, string(quoted))
	}
	if strings.Contains(strings.Join(out, "\x00"), completionWorkspaceTrustKeyToken) {
		return nil, fmt.Errorf("completion authority codex argv retained an unresolved trust key")
	}
	return out, nil
}

func completionExecutableIdentity(path, version string) (string, string, error) {
	path = strings.TrimSpace(path)
	version = strings.TrimSpace(version)
	if path == "" || version == "" {
		return "", "", fmt.Errorf("completion authority requires a versioned preflighted codex executable")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", "", err
	}
	physical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", "", fmt.Errorf("resolve preflighted codex executable: %w", err)
	}
	if !filepath.IsAbs(physical) {
		return "", "", fmt.Errorf("completion authority requires an absolute codex executable")
	}
	before, err := os.Stat(physical)
	if err != nil {
		return "", "", err
	}
	if !before.Mode().IsRegular() || before.Mode()&0o111 == 0 {
		return "", "", fmt.Errorf("completion authority requires a regular executable codex file")
	}
	file, err := os.Open(physical)
	if err != nil {
		return "", "", err
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, file)
	opened, statErr := file.Stat()
	closeErr := file.Close()
	if copyErr != nil {
		return "", "", copyErr
	}
	if statErr != nil {
		return "", "", statErr
	}
	if closeErr != nil {
		return "", "", closeErr
	}
	after, err := os.Stat(physical)
	if err != nil {
		return "", "", err
	}
	if !os.SameFile(before, opened) || !os.SameFile(opened, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return "", "", fmt.Errorf("preflighted codex executable changed while its identity was captured")
	}
	payload := strings.Join([]string{
		"tusker.completion-executable/v1",
		filepath.Clean(physical),
		before.Mode().String(),
		hex.EncodeToString(hash.Sum(nil)),
		version,
	}, "\x00")
	sum := sha256.Sum256([]byte(payload))
	return filepath.Clean(physical), "sha256:" + hex.EncodeToString(sum[:]), nil
}

func completionVerifyExecutableIdentity(path, expected, searchPath string) error {
	if !v7CloseAuthorityDigest(expected, "sha256:") {
		return fmt.Errorf("completion authority requires a valid codex executable identity")
	}
	if strings.TrimSpace(searchPath) == "" {
		return fmt.Errorf("completion authority requires the captured non-login runner search path")
	}
	version, err := runnerExecutableHealthCheck(path, searchPath)
	if err != nil {
		return err
	}
	physical, actual, err := completionExecutableIdentity(path, version)
	if err != nil {
		return err
	}
	if filepath.Clean(path) != physical || actual != expected {
		return fmt.Errorf("completion authority refuses codex executable path or identity drift")
	}
	return nil
}

func completionWorkerPolicyFingerprint(lane, command string, profile ResolvedRunnerProfile) (string, error) {
	argv, err := completionAuthoritativeCodexExecArgv(command, lane, profile)
	if err != nil {
		return "", err
	}
	payload := strings.Join([]string{"tusker.completion-worker-policy/v3", lane, profile.Name, profile.Source, profile.Definition.Harness, profile.Definition.Model, profile.Definition.Effort, profile.Definition.PermissionPreset, profile.Definition.Sandbox.Mode, fmt.Sprintf("%t", profile.Definition.Sandbox.Network != nil && *profile.Definition.Sandbox.Network), strings.Join(argv, "\x00")}, "\x00")
	sum := sha256.Sum256([]byte(payload))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func completionLaneWorkerPolicy(wf Workflow, note Note, lane string) (ResolvedRunnerProfile, []string, string, error) {
	profile, err := resolveRunProfileForLane(note, wf, lane, "")
	if err != nil {
		return ResolvedRunnerProfile{}, nil, "", err
	}
	declared, exists := wf.RunnerProfiles[profile.Name]
	definitionSource := wf.RunnerProfileSources[profile.Name]
	if profile.Name == "" || wf.RunnerLaneProfiles[lane] != profile.Name || !exists || strings.TrimSpace(declared.Harness) == "" ||
		(definitionSource != configSourceProject && definitionSource != configSourceLocal) {
		return ResolvedRunnerProfile{}, nil, "", fmt.Errorf("completion authority requires an explicit project or machine-local profile for lane %q", lane)
	}
	profile.Source = definitionSource
	_, base, err := runnerForName(profile.Definition.Harness, wf)
	if err != nil {
		return ResolvedRunnerProfile{}, nil, "", err
	}
	if err := completionWorkerSafetyForLane("/trusted-state", "/worker", lane, base, profile); err != nil {
		return ResolvedRunnerProfile{}, nil, "", err
	}
	argv, err := completionAuthoritativeCodexExecArgv(base, lane, profile)
	if err != nil {
		return ResolvedRunnerProfile{}, nil, "", err
	}
	fingerprint, err := completionWorkerPolicyFingerprint(lane, base, profile)
	if err != nil {
		return ResolvedRunnerProfile{}, nil, "", err
	}
	return profile, argv, fingerprint, nil
}

func completionCombinedWorkerPolicyFingerprint(execute, review string) (string, error) {
	if !v7CloseAuthorityDigest(execute, "sha256:") || !v7CloseAuthorityDigest(review, "sha256:") {
		return "", fmt.Errorf("completion authority requires exact execute and review worker policy fingerprints")
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{"tusker.completion-worker-policy-chain/v1", execute, review}, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func completionWorkflowPolicyFingerprint(wf Workflow, note Note) (string, error) {
	parts := make([]string, 0, 2)
	for _, lane := range []string{runLaneExecute, runLaneReview} {
		_, _, fingerprint, err := completionLaneWorkerPolicy(wf, note, lane)
		if err != nil {
			return "", err
		}
		parts = append(parts, fingerprint)
	}
	return completionCombinedWorkerPolicyFingerprint(parts[0], parts[1])
}

func (d *Daemon) validateCompletionWorkerAuthority(project RegisteredProject, wf Workflow, note Note, result ReviewResult) error {
	if d == nil || d.store == nil {
		return fmt.Errorf("completion authority requires the resident daemon store")
	}
	review, err := resolveRunProfileForLane(note, wf, runLaneReview, result.Runner)
	if err != nil {
		return err
	}
	if review.Name == "" || review.Name != result.RunnerProfile {
		return fmt.Errorf("completion authority refuses review profile drift")
	}
	execute, err := resolveRunProfileForLane(note, wf, runLaneExecute, "")
	if err != nil {
		return err
	}
	if execute.Name == "" || review.Name == "" {
		return fmt.Errorf("completion authority requires explicit named execute and review profiles")
	}
	if wf.RunnerLaneProfiles[runLaneExecute] != execute.Name || wf.RunnerLaneProfiles[runLaneReview] != review.Name || wf.RunnerProfiles[execute.Name].Harness == "" || wf.RunnerProfiles[review.Name].Harness == "" || execute.Source == configSourceBuiltIn || review.Source == configSourceBuiltIn {
		return fmt.Errorf("completion authority requires explicit project or machine-local lane profiles")
	}
	if err := completionWorkerSafety(d.stateRoot, workspaceForCompletionSafety(project, result.TaskID), execute); err != nil {
		return err
	}
	if err := completionWorkerSafety(d.stateRoot, workspaceForCompletionSafety(project, result.TaskID), review); err != nil {
		return err
	}
	if strings.TrimSpace(review.Definition.Sandbox.Mode) != "read-only" {
		return fmt.Errorf("completion authority requires read-only review profile")
	}
	expectedPolicy, err := completionWorkflowPolicyFingerprint(wf, note)
	if err != nil {
		return err
	}
	if result.WorkerPolicyFP == "" || result.WorkerPolicyFP != expectedPolicy {
		return fmt.Errorf("completion authority refuses worker policy drift")
	}
	runs, err := d.store.ListRuns()
	if err != nil {
		return err
	}
	for _, run := range runs {
		if run.ProjectID != project.ProjectID || run.RecordID != result.TaskID || strings.TrimSpace(run.WorkspacePath) == "" {
			continue
		}
		if pathWithin(run.WorkspacePath, d.stateRoot) {
			return fmt.Errorf("completion authority requires daemon state root outside every worker workspace")
		}
	}
	return nil
}

func workspaceForCompletionSafety(project RegisteredProject, taskID string) string {
	// The exact run rows are checked separately.  This fallback is only for
	// resolving profile safety before a row has retained a workspace path.
	return firstNonEmpty(project.RepoRoot, taskID)
}
