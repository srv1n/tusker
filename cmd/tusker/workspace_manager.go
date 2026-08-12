package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// staleWorkspaceThreshold is the generous idle window after which a live work
// copy with no recorded owning PID (legacy copies) is treated as abandoned.
const staleWorkspaceThreshold = 24 * time.Hour

type WorkspaceStrategy string

const (
	WorkspaceStrategyShared WorkspaceStrategy = "shared"
	// WorkspaceStrategyInPlace is accepted as a legacy configuration value.
	// New configuration and persisted metadata use "shared".
	WorkspaceStrategyInPlace  WorkspaceStrategy = "in_place"
	WorkspaceStrategyWorktree WorkspaceStrategy = "worktree"
	WorkspaceStrategyClone    WorkspaceStrategy = "clone"
	WorkspaceStrategyCopy     WorkspaceStrategy = "copy"
)

func normalizeWorkspaceStrategy(strategy WorkspaceStrategy) WorkspaceStrategy {
	switch strategy {
	case "", WorkspaceStrategyInPlace, WorkspaceStrategyShared:
		return WorkspaceStrategyShared
	default:
		return strategy
	}
}

type WorkspacePrepareRequest struct {
	ProjectID     string
	ProjectKey    string
	RecordID      string
	ItemID        string
	BranchName    string
	BranchBase    string
	RepoRoot      string
	StateRoot     string
	WorkspaceRoot string
	Strategy      WorkspaceStrategy
	WorkRevision  int
	// MaxLiveWorktrees caps how many live work copies may exist under the
	// workspace root at once. Zero leaves the cap off. The number is measured,
	// not guessed (see .tusker/specs/build-and-test-economics.md). Opening a new
	// work copy past the cap is refused before any git worktree is created.
	MaxLiveWorktrees int
}

type WorkspacePrepareResult struct {
	Path              string
	Metadata          WorkspaceMetadata
	NewlyMaterialized bool
}

type WorkspaceMetadata struct {
	ProjectID    string `json:"project_id"`
	RecordID     string `json:"record_id"`
	ItemID       string `json:"item_id"`
	BranchName   string `json:"branch_name"`
	BranchBase   string `json:"branch_base,omitempty"`
	RepoRoot     string `json:"repo_root"`
	Strategy     string `json:"strategy"`
	WorkRevision int    `json:"work_revision"`
	CreatedAt    string `json:"created_at"`
	PreparedAt   string `json:"prepared_at"`
	// PID records the process that last prepared (owns) this live work copy. It
	// is the liveness signal for orphan pruning: when this process is gone, no
	// run is using the copy and it can be reclaimed. Zero for legacy copies.
	PID int `json:"pid,omitempty"`
}

type FSWorkspaceManager struct{}

func NewWorkspaceManager() *FSWorkspaceManager {
	return &FSWorkspaceManager{}
}

func (m *FSWorkspaceManager) Prepare(req WorkspacePrepareRequest) (WorkspacePrepareResult, error) {
	req.Strategy = normalizeWorkspaceStrategy(req.Strategy)
	workspacePath, root, err := workspacePathForRequest(req)
	if err != nil {
		return WorkspacePrepareResult{}, err
	}
	if req.Strategy == WorkspaceStrategyShared {
		if err := assertInPlaceWorkspaceReady(req.RepoRoot); err != nil {
			return WorkspacePrepareResult{}, err
		}
	} else if err := assertWorkspaceWithinRoot(workspacePath, root); err != nil {
		return WorkspacePrepareResult{}, err
	}
	if req.Strategy != WorkspaceStrategyShared && req.MaxLiveWorktrees > 0 {
		// Serialize the count-and-materialize critical section across processes:
		// an in-process mutex is insufficient when several dispatchers Prepare
		// concurrently, so an flock in the (per-project) workspace root closes
		// the TOCTOU window where two callers both pass the count and exceed the
		// cap. The lock is held through materialization below.
		unlock, err := lockWorkspaceRoot(root)
		if err != nil {
			return WorkspacePrepareResult{}, err
		}
		defer unlock()
		if refusal := liveWorktreeCapRefusal(root, workspacePath, req.MaxLiveWorktrees); refusal != nil {
			return WorkspacePrepareResult{}, tuskerError(errorInvalidTransition,
				refusal.Detail+"; "+refusal.Remedy,
				withPath(workspacePath),
				withContext(map[string]any{"cause": refusal.Cause}))
		}
	}
	return m.prepareAtPath(workspacePath, req)
}

// liveWorktreeCapRefusal refuses opening a new live work copy once the count of
// existing live copies under root has reached the configured (measured) cap. It
// is a no-op when the cap is off (max <= 0) or when the target work copy already
// exists (reusing a copy does not add to the live count). The cause is named so
// the refusal is machine-routable, matching the gate preflight's GateRefusal.
func liveWorktreeCapRefusal(root, workspacePath string, max int) *GateRefusal {
	if max <= 0 {
		return nil
	}
	// An existing target is a reuse, not a new copy — never refuse it.
	if fileExists(filepath.Join(workspacePath, ".tusker", "workspace.json")) {
		return nil
	}
	live := countLiveWorktrees(root)
	if live < max {
		return nil
	}
	return &GateRefusal{
		Cause:  gateRefusalWorktreeCap,
		Detail: fmt.Sprintf("cannot open another live work copy: %d already live under %s, at the configured per-project cap of %d", live, root, max),
		// These %d copies are actively in use: stale/orphaned copies (their run
		// crashed or exited) are pruned automatically on every prepare, so they
		// are never part of this count. There is nothing to reclaim by hand.
		Remedy: "these copies are actively in use (orphaned copies are pruned automatically, so none are counted here); wait for a running copy to finish and be cleaned up, or raise the per-project cap in workspace.max_live_worktrees",
	}
}

// countLiveWorktrees counts the immediate child directories under root that hold
// a materialized, still-live workspace (a .tusker/workspace.json). Each is one
// live work copy. The shared/in_place repo is never under root, so it is not
// counted. Orphaned copies — whose recording run has crashed or exited and left
// the copy behind — are opportunistically pruned here (through the same removal
// path Cleanup uses, so git worktree metadata stays consistent) rather than
// counted, so accumulated orphans can never wedge dispatch by exhausting the cap
// forever.
func countLiveWorktrees(root string) int {
	entries, err := os.ReadDir(root)
	if err != nil {
		return 0
	}
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		child := filepath.Join(root, entry.Name())
		metadataPath := filepath.Join(child, ".tusker", "workspace.json")
		if !fileExists(metadataPath) {
			continue
		}
		if workspaceCopyStale(metadataPath) {
			_ = cleanupWorkspacePath(child)
			continue
		}
		count++
	}
	return count
}

// workspaceCopyStale reports whether the live copy described by metadataPath is
// an orphan that nothing is using. The most reliable adjacent signal is the
// owning PID recorded in workspace.json: if a PID was recorded and that process
// is no longer alive, its run has gone and the copy is stale. For legacy copies
// with no recorded PID, fall back to the workspace.json mtime — a copy untouched
// for longer than the generous staleWorkspaceThreshold is treated as abandoned.
func workspaceCopyStale(metadataPath string) bool {
	text, err := readText(metadataPath)
	if err != nil {
		return false
	}
	var meta WorkspaceMetadata
	if err := json.Unmarshal([]byte(text), &meta); err != nil {
		return false
	}
	if meta.PID > 0 {
		return !processAlive(meta.PID)
	}
	info, err := os.Stat(metadataPath)
	if err != nil {
		return false
	}
	return time.Since(info.ModTime()) > staleWorkspaceThreshold
}

// lockWorkspaceRoot takes an exclusive cross-process lock in the (per-project)
// workspace root, mirroring the flock convention used elsewhere (see
// runOwnershipService.lockOwnedPathClaims). It serializes the worktree-cap
// count-and-materialize critical section so concurrent Prepare calls cannot both
// pass the count and exceed the cap.
func lockWorkspaceRoot(root string) (func(), error) {
	if err := ensureDir(root); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(root, ".worktree-cap.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, err
	}
	return func() { _ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN); _ = file.Close() }, nil
}

func workspacePathForRequest(req WorkspacePrepareRequest) (string, string, error) {
	if req.Strategy == WorkspaceStrategyShared {
		repoRoot := strings.TrimSpace(req.RepoRoot)
		if repoRoot == "" {
			return "", "", tuskerError(errorConfigInvalid, "in_place workspace requires repo_root")
		}
		abs, err := filepath.Abs(repoRoot)
		if err != nil {
			return "", "", err
		}
		return abs, abs, nil
	}
	workspaceKey := workspaceKeyForRequest(req)
	root, err := workspaceRootForRequest(req)
	if err != nil {
		return "", "", err
	}
	return filepath.Join(root, workspaceKey), root, nil
}

func workspaceRootForRequest(req WorkspacePrepareRequest) (string, error) {
	stateRoot := strings.TrimSpace(req.StateRoot)
	if stateRoot == "" {
		return "", tuskerError(errorConfigInvalid, "workspace preparation requires state_root")
	}
	sharedRoot := filepath.Join(stateRoot, "workspaces")
	root := sharedRoot
	if configured := strings.TrimSpace(req.WorkspaceRoot); configured != "" {
		if filepath.Clean(configured) == "." {
			root = sharedRoot
		} else if filepath.IsAbs(configured) {
			root = filepath.Clean(configured)
		} else {
			root = filepath.Join(stateRoot, configured)
		}
	}
	if !pathWithinLexical(sharedRoot, root) || !pathWithinResolved(sharedRoot, root) {
		return "", tuskerError(errorConfigInvalid,
			"workspace.root must stay under the shared runtime workspace directory",
			withPath(root),
			withHint("use workspace.root: workspaces or a subdirectory such as workspaces/team"),
			withContext(map[string]any{"state_root": stateRoot, "workspace_root": root, "required_prefix": sharedRoot}))
	}
	projectKey := strings.TrimSpace(req.ProjectKey)
	if projectKey == "" {
		projectKey = "project"
	}
	return filepath.Join(root, projectKey), nil
}

func validSharedWorkspaceRootConfig(root string) bool {
	root = strings.TrimSpace(root)
	if root == "" || filepath.IsAbs(root) {
		return false
	}
	clean := filepath.Clean(root)
	return clean == "workspaces" || strings.HasPrefix(clean, "workspaces"+string(filepath.Separator))
}

func (m *FSWorkspaceManager) prepareAtPath(workspacePath string, req WorkspacePrepareRequest) (WorkspacePrepareResult, error) {
	metadataPath := filepath.Join(workspacePath, ".tusker", "workspace.json")
	created := false
	if !fileExists(metadataPath) {
		if req.Strategy != WorkspaceStrategyShared {
			if err := m.materializeWorkspace(workspacePath, req); err != nil {
				return WorkspacePrepareResult{}, err
			}
		}
		created = true
	}
	if err := ensureDir(filepath.Dir(metadataPath)); err != nil {
		return WorkspacePrepareResult{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	metadata := WorkspaceMetadata{
		ProjectID: req.ProjectID, RecordID: req.RecordID, ItemID: req.ItemID,
		BranchName: req.BranchName, BranchBase: req.BranchBase, RepoRoot: req.RepoRoot, Strategy: string(req.Strategy), WorkRevision: req.WorkRevision, CreatedAt: now, PreparedAt: now,
		PID: os.Getpid(),
	}
	if !created && fileExists(metadataPath) {
		text, err := readText(metadataPath)
		if err == nil {
			var existing WorkspaceMetadata
			if json.Unmarshal([]byte(text), &existing) == nil && existing.CreatedAt != "" {
				if err := validateWorkspaceMetadata(existing, req); err != nil {
					return WorkspacePrepareResult{}, err
				}
				metadata.CreatedAt = existing.CreatedAt
			}
		}
	}
	raw, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return WorkspacePrepareResult{}, err
	}
	if err := writeText(metadataPath, string(raw)+"\n"); err != nil {
		return WorkspacePrepareResult{}, err
	}
	return WorkspacePrepareResult{Path: workspacePath, Metadata: metadata, NewlyMaterialized: created}, nil
}

func assertWorkspaceWithinRoot(workspacePath, root string) error {
	workspaceAbs, err := filepath.Abs(workspacePath)
	if err != nil {
		return err
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(rootAbs, workspaceAbs)
	if err != nil {
		return err
	}
	if rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." || filepath.IsAbs(rel) {
		return tuskerError(errorConfigInvalid, "workspace path escapes workspace root", withPath(workspacePath))
	}
	return nil
}

func assertInPlaceWorkspaceReady(repoRoot string) error {
	repoRoot = strings.TrimSpace(repoRoot)
	if repoRoot == "" || !fileExists(repoRoot) {
		return tuskerError(errorConfigInvalid, "in_place workspace requires an existing repo root", withPath(repoRoot))
	}
	dirty, err := inPlaceDirtyPaths(repoRoot)
	if err != nil {
		return err
	}
	if len(dirty) > 0 {
		return tuskerError(errorInvalidTransition, "in_place workspace requires a clean working tree outside .tusker; dirty paths: "+strings.Join(limitStrings(dirty, 5), ", "), withPath(repoRoot))
	}
	return nil
}

func inPlaceDirtyPaths(repoRoot string) ([]string, error) {
	if _, err := exec.LookPath("git"); err != nil || !fileExists(filepath.Join(repoRoot, ".git")) {
		return nil, nil
	}
	out, err := exec.Command("git", "-C", repoRoot, "status", "--porcelain", "--untracked-files=all").Output()
	if err != nil {
		return nil, err
	}
	var dirty []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		for _, path := range porcelainDirtyPaths(line) {
			if !workspacePathIsTuskerBookkeeping(path) {
				dirty = append(dirty, path)
			}
		}
	}
	return dirty, nil
}

func porcelainDirtyPaths(line string) []string {
	if len(line) > 3 {
		line = line[3:]
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}
	parts := strings.Split(line, " -> ")
	for i, part := range parts {
		parts[i] = strings.Trim(strings.TrimSpace(part), `"`)
	}
	return parts
}

func workspacePathIsTuskerBookkeeping(path string) bool {
	path = filepath.ToSlash(strings.TrimSpace(path))
	return path == ".tusker" || strings.HasPrefix(path, ".tusker/")
}

func validateWorkspaceMetadata(metadata WorkspaceMetadata, req WorkspacePrepareRequest) error {
	if metadata.ProjectID != "" && metadata.ProjectID != req.ProjectID {
		return tuskerError(errorConfigInvalid, "workspace metadata project_id does not match requested project", withPath(req.RecordID))
	}
	if metadata.RecordID != "" && metadata.RecordID != req.RecordID {
		return tuskerError(errorConfigInvalid, "workspace metadata record_id does not match requested record", withPath(req.RecordID))
	}
	if strings.TrimSpace(metadata.BranchName) != strings.TrimSpace(req.BranchName) {
		return tuskerError(errorConfigInvalid, "workspace metadata branch_name does not match requested branch", withPath(req.RecordID))
	}
	if strings.TrimSpace(metadata.BranchBase) != strings.TrimSpace(req.BranchBase) {
		return tuskerError(errorConfigInvalid, "workspace metadata branch_base does not match requested branch base", withPath(req.RecordID))
	}
	return nil
}

func workspaceKeyForRequest(req WorkspacePrepareRequest) string {
	recordID := strings.TrimSpace(req.RecordID)
	branchName := strings.TrimSpace(req.BranchName)
	if branchName == "" {
		return recordID
	}
	return recordID + "__" + sanitizeWorkspaceKey(branchName)
}

func sanitizeWorkspaceKey(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "default"
	}
	var out strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(value) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			out.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			out.WriteByte('-')
			lastDash = true
		}
	}
	cleaned := strings.Trim(out.String(), "-")
	if cleaned == "" {
		return "branch"
	}
	return cleaned
}

func (m *FSWorkspaceManager) Cleanup(path string) error {
	return cleanupWorkspacePath(path)
}

// cleanupWorkspacePath removes a live work copy, first detaching any git worktree
// so git's worktree metadata stays consistent, then deleting the directory. It is
// the single removal path shared by Cleanup and the orphan pruning in
// countLiveWorktrees.
func cleanupWorkspacePath(path string) error {
	metadataPath := filepath.Join(path, ".tusker", "workspace.json")
	if fileExists(metadataPath) {
		text, err := readText(metadataPath)
		if err == nil {
			var metadata WorkspaceMetadata
			if json.Unmarshal([]byte(text), &metadata) == nil && metadata.Strategy == string(WorkspaceStrategyWorktree) && metadata.RepoRoot != "" {
				if _, lookErr := exec.LookPath("git"); lookErr == nil {
					_ = exec.Command("git", "-C", metadata.RepoRoot, "worktree", "remove", "--force", path).Run()
				}
			}
		}
	}
	return os.RemoveAll(path)
}

func (m *FSWorkspaceManager) ResetForRework(path string, workRevision int) error {
	metadataPath := filepath.Join(path, ".tusker", "workspace.json")
	if !fileExists(metadataPath) {
		return nil
	}
	text, err := readText(metadataPath)
	if err != nil {
		return err
	}
	var metadata WorkspaceMetadata
	if err := json.Unmarshal([]byte(text), &metadata); err != nil {
		return err
	}
	metadata.WorkRevision = workRevision
	metadata.PreparedAt = time.Now().UTC().Format(time.RFC3339)
	raw, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	return writeText(metadataPath, string(raw)+"\n")
}

func (m *FSWorkspaceManager) materializeWorkspace(workspacePath string, req WorkspacePrepareRequest) error {
	_ = os.RemoveAll(workspacePath)
	if req.RepoRoot == "" || !fileExists(req.RepoRoot) {
		return ensureDir(workspacePath)
	}
	if _, err := exec.LookPath("git"); err == nil && fileExists(filepath.Join(req.RepoRoot, ".git")) {
		switch req.Strategy {
		case WorkspaceStrategyWorktree:
			if strings.TrimSpace(req.BranchName) != "" {
				base := firstNonEmpty(strings.TrimSpace(req.BranchBase), "HEAD")
				if gitBranchExists(req.RepoRoot, req.BranchName) {
					if err := exec.Command("git", "-C", req.RepoRoot, "worktree", "add", workspacePath, req.BranchName).Run(); err == nil {
						return nil
					}
				}
				if err := exec.Command("git", "-C", req.RepoRoot, "worktree", "add", "-b", req.BranchName, workspacePath, base).Run(); err == nil {
					return nil
				}
			} else {
				if err := exec.Command("git", "-C", req.RepoRoot, "worktree", "add", "--detach", workspacePath, "HEAD").Run(); err == nil {
					return nil
				}
			}
		case WorkspaceStrategyClone:
			if err := exec.Command("git", "clone", req.RepoRoot, workspacePath).Run(); err == nil {
				return nil
			}
		}
	}
	return ensureDir(workspacePath)
}
