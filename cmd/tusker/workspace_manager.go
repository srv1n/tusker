package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type WorkspaceStrategy string

const (
	WorkspaceStrategyWorktree WorkspaceStrategy = "worktree"
	WorkspaceStrategyClone    WorkspaceStrategy = "clone"
	WorkspaceStrategyCopy     WorkspaceStrategy = "copy"
)

type WorkspacePrepareRequest struct {
	ProjectID    string
	ProjectKey   string
	RecordID     string
	ItemID       string
	BranchName   string
	RepoRoot     string
	StateRoot    string
	Strategy     WorkspaceStrategy
	WorkRevision int
}

type WorkspacePrepareResult struct {
	Path     string
	Metadata WorkspaceMetadata
}

type WorkspaceMetadata struct {
	ProjectID    string `json:"project_id"`
	RecordID     string `json:"record_id"`
	ItemID       string `json:"item_id"`
	BranchName   string `json:"branch_name"`
	RepoRoot     string `json:"repo_root"`
	Strategy     string `json:"strategy"`
	WorkRevision int    `json:"work_revision"`
	CreatedAt    string `json:"created_at"`
	PreparedAt   string `json:"prepared_at"`
}

type WorkspaceManager interface {
	Prepare(req WorkspacePrepareRequest) (WorkspacePrepareResult, error)
	Cleanup(path string) error
	ResetForRework(path string, workRevision int) error
}

type FSWorkspaceManager struct{}

func NewWorkspaceManager() WorkspaceManager {
	return &FSWorkspaceManager{}
}

func (m *FSWorkspaceManager) Prepare(req WorkspacePrepareRequest) (WorkspacePrepareResult, error) {
	workspaceKey := workspaceKeyForRequest(req)
	workspacePath := filepath.Join(req.StateRoot, "workspaces", req.ProjectKey, workspaceKey)
	root := filepath.Join(req.StateRoot, "workspaces", req.ProjectKey)
	if err := assertWorkspaceWithinRoot(workspacePath, root); err != nil {
		return WorkspacePrepareResult{}, err
	}
	metadataPath := filepath.Join(workspacePath, ".tusker", "workspace.json")
	created := false
	if !fileExists(metadataPath) {
		if err := m.materializeWorkspace(workspacePath, req); err != nil {
			return WorkspacePrepareResult{}, err
		}
		created = true
	}
	if err := ensureDir(filepath.Dir(metadataPath)); err != nil {
		return WorkspacePrepareResult{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	metadata := WorkspaceMetadata{
		ProjectID: req.ProjectID, RecordID: req.RecordID, ItemID: req.ItemID,
		BranchName: req.BranchName, RepoRoot: req.RepoRoot, Strategy: string(req.Strategy), WorkRevision: req.WorkRevision, CreatedAt: now, PreparedAt: now,
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
	return WorkspacePrepareResult{Path: workspacePath, Metadata: metadata}, nil
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
				if err := exec.Command("git", "-C", req.RepoRoot, "worktree", "add", "-b", req.BranchName, workspacePath, "HEAD").Run(); err == nil {
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
