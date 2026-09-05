package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type WorkspaceVaultConfig struct {
	ObsidianVault string             `yaml:"obsidian_vault" json:"obsidian_vault"`
	Projects      []WorkspaceProject `yaml:"projects" json:"projects"`
}

type WorkspaceProject struct {
	ProjectID   string `yaml:"project_id" json:"project_id"`
	DisplayName string `yaml:"display_name" json:"display_name"`
	RepoRoot    string `yaml:"repo_root" json:"repo_root"`
	TrackerRoot string `yaml:"tracker_root" json:"tracker_root"`
	MountName   string `yaml:"mount_name" json:"mount_name"`
	MountPath   string `yaml:"mount_path" json:"mount_path"`
	AddedAt     string `yaml:"added_at" json:"added_at"`
	UpdatedAt   string `yaml:"updated_at" json:"updated_at"`
}

type ProjectTrackerMetadata struct {
	ProjectID   string `yaml:"project_id" json:"project_id"`
	DisplayName string `yaml:"display_name" json:"display_name"`
	RepoRoot    string `yaml:"repo_root" json:"repo_root"`
	MountName   string `yaml:"mount_name" json:"mount_name"`
	Created     string `yaml:"created" json:"created"`
	Updated     string `yaml:"updated" json:"updated"`
}

type MountStatus struct {
	WorkspaceProject `json:",inline"`
	State            string `json:"state"`
	Target           string `json:"target"`
}

func workspaceConfigPath() string {
	return filepath.Join(DefaultStateRoot(), "workspace.yaml")
}

func configuredWorkspaceVaultPath() string {
	cfg, err := loadWorkspaceVaultConfig()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(cfg.ObsidianVault)
}

func loadWorkspaceVaultConfig() (WorkspaceVaultConfig, error) {
	path := workspaceConfigPath()
	if !fileExists(path) {
		return WorkspaceVaultConfig{}, nil
	}
	raw, err := readText(path)
	if err != nil {
		return WorkspaceVaultConfig{}, err
	}
	var cfg WorkspaceVaultConfig
	if err := yaml.Unmarshal([]byte(raw), &cfg); err != nil {
		return WorkspaceVaultConfig{}, tuskerError(errorConfigInvalid, "failed to parse workspace config: "+err.Error(), withPath(path))
	}
	return cfg, nil
}

func saveWorkspaceVaultConfig(cfg WorkspaceVaultConfig) error {
	sort.SliceStable(cfg.Projects, func(i, j int) bool {
		return cfg.Projects[i].MountName < cfg.Projects[j].MountName
	})
	raw, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return writeText(workspaceConfigPath(), string(raw))
}

func vaultSetCmd(args Args) error {
	path, err := workspaceVaultPathArg(args, "path")
	if err != nil {
		return err
	}
	if err := ensureDir(path); err != nil {
		return err
	}
	cfg, err := loadWorkspaceVaultConfig()
	if err != nil {
		return err
	}
	cfg.ObsidianVault = path
	cfg.refreshMountPaths()
	if err := saveWorkspaceVaultConfig(cfg); err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "obsidian_vault": path, "config": workspaceConfigPath()})
		return nil
	}
	fmt.Printf("Obsidian vault set to %s\n", path)
	return nil
}

func vaultStatusCmd(args Args) error {
	cfg, err := loadWorkspaceVaultConfig()
	if err != nil {
		return err
	}
	statuses := workspaceMountStatuses(cfg)
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "obsidian_vault": cfg.ObsidianVault, "config": workspaceConfigPath(), "count": len(statuses), "projects": statuses})
		return nil
	}
	if strings.TrimSpace(cfg.ObsidianVault) == "" {
		fmt.Println("No Obsidian vault configured. Run `tusker vault set --path <obsidian-vault>`.")
		return nil
	}
	fmt.Printf("Obsidian vault: %s\n", cfg.ObsidianVault)
	if len(statuses) == 0 {
		fmt.Println("(no mounted projects)")
		return nil
	}
	for _, status := range statuses {
		fmt.Printf("%-24s %-12s %s -> %s\n", status.MountName, status.State, status.MountPath, status.TrackerRoot)
	}
	return nil
}

func vaultMoveCmd(args Args) error {
	to, err := workspaceVaultPathArg(args, "to")
	if err != nil {
		return err
	}
	cfg, err := loadWorkspaceVaultConfig()
	if err != nil {
		return err
	}
	from := strings.TrimSpace(cfg.ObsidianVault)
	renamed := false
	if from == "" {
		cfg.ObsidianVault = to
		cfg.refreshMountPaths()
		if err := ensureDir(to); err != nil {
			return err
		}
		if err := saveWorkspaceVaultConfig(cfg); err != nil {
			return err
		}
		return vaultRepairAfterMove(args, cfg, from, to, false)
	}
	if canonicalPath(from) != canonicalPath(to) {
		if fileExists(from) && !fileExists(to) {
			if err := ensureDir(filepath.Dir(to)); err != nil {
				return err
			}
			if err := os.Rename(from, to); err != nil {
				return err
			}
			renamed = true
		} else if !fileExists(to) {
			if err := ensureDir(to); err != nil {
				return err
			}
		}
	}
	cfg.ObsidianVault = to
	cfg.refreshMountPaths()
	if err := saveWorkspaceVaultConfig(cfg); err != nil {
		return err
	}
	return vaultRepairAfterMove(args, cfg, from, to, renamed)
}

func vaultRepairAfterMove(args Args, cfg WorkspaceVaultConfig, from, to string, moved bool) error {
	results, err := repairWorkspaceMounts(cfg, args.Bool("force"))
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "from": nullIfEmptyString(from), "to": to, "moved": moved, "projects": results})
		return nil
	}
	if from == "" {
		fmt.Printf("Obsidian vault set to %s\n", to)
	} else if moved {
		fmt.Printf("Obsidian vault moved to %s\n", to)
	} else {
		fmt.Printf("Obsidian vault set to %s\n", to)
	}
	return nil
}

func vaultRepairCmd(args Args) error {
	cfg, err := requireWorkspaceVaultConfig()
	if err != nil {
		return err
	}
	results, err := repairWorkspaceMounts(cfg, args.Bool("force"))
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "count": len(results), "projects": results})
		return nil
	}
	if len(results) == 0 {
		fmt.Println("(no mounted projects)")
		return nil
	}
	for _, status := range results {
		fmt.Printf("%-24s %-12s %s\n", status.MountName, status.State, status.MountPath)
	}
	return nil
}

func vaultMountCmd(args Args) error {
	cfg, err := requireWorkspaceVaultConfig()
	if err != nil {
		return err
	}
	repoRoot, err := filepath.Abs(firstNonEmpty(args.String("repo"), mustGetwd()))
	if err != nil {
		return err
	}
	trackerRoot, err := resolveTrackerForMount(args, repoRoot)
	if err != nil {
		return err
	}
	if !isVaultDir(trackerRoot) {
		return tuskerError(errorNotFound, "Tusker tracker not found: "+trackerRoot, withHint("Run `tusker init --yes` or pass --vault <path>."))
	}
	mountName := strings.TrimSpace(args.String("name"))
	if mountName == "" {
		mountName = strings.TrimSpace(args.String("mount-name"))
	}
	if mountName == "" {
		mountName = defaultMountName(repoRoot)
	}
	mountName = sanitizeMountName(mountName)
	if mountName == "" {
		return tuskerError(errorInvalidArg, "mount name is empty after sanitizing", withContext(map[string]any{"arg": "--name"}))
	}
	project, err := upsertProjectTrackerMetadata(trackerRoot, repoRoot, mountName)
	if err != nil {
		return err
	}
	mountPath := filepath.Join(cfg.ObsidianVault, mountName)
	if err := ensureWorkspaceMount(mountPath, trackerRoot, args.Bool("force")); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	entry := WorkspaceProject{
		ProjectID:   project.ProjectID,
		DisplayName: project.DisplayName,
		RepoRoot:    repoRoot,
		TrackerRoot: trackerRoot,
		MountName:   mountName,
		MountPath:   mountPath,
		AddedAt:     now,
		UpdatedAt:   now,
	}
	cfg.upsertProject(entry)
	if err := saveWorkspaceVaultConfig(cfg); err != nil {
		return err
	}
	status := workspaceMountStatus(entry)
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "project": status})
		return nil
	}
	if !args.Bool("quiet") {
		fmt.Printf("Mounted %s at %s\n", trackerRoot, mountPath)
	}
	return nil
}

func resolveTrackerForMount(args Args, repoRoot string) (string, error) {
	if explicit := strings.TrimSpace(args.String("vault")); explicit != "" {
		return filepath.Abs(explicit)
	}
	if discovered, err := discoverVault(repoRoot); err != nil {
		return "", err
	} else if discovered != "" {
		return discovered, nil
	}
	return "", tuskerError(errorMissingArg, "No Tusker tracker found for repo: "+repoRoot, withHint("Run `tusker init --yes` in that repo, or pass --vault <path>."))
}

func vaultUnmountCmd(args Args) error {
	cfg, err := requireWorkspaceVaultConfig()
	if err != nil {
		return err
	}
	project, err := cfg.resolveProject(args)
	if err != nil {
		return err
	}
	if err := removeWorkspaceMount(project.MountPath); err != nil {
		return err
	}
	cfg.removeProject(project)
	if err := saveWorkspaceVaultConfig(cfg); err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "unmounted": project.MountName, "path": project.MountPath})
		return nil
	}
	fmt.Printf("Unmounted %s\n", project.MountName)
	return nil
}

func requireWorkspaceVaultConfig() (WorkspaceVaultConfig, error) {
	cfg, err := loadWorkspaceVaultConfig()
	if err != nil {
		return cfg, err
	}
	if strings.TrimSpace(cfg.ObsidianVault) == "" {
		return cfg, tuskerError(errorMissingArg, "No Obsidian vault configured.", withHint("Run `tusker init --vault <repo>/.tusker --yes` without --mount, or configure the Obsidian vault in the workspace config file."))
	}
	if err := ensureDir(cfg.ObsidianVault); err != nil {
		return cfg, err
	}
	cfg.refreshMountPaths()
	return cfg, nil
}

func workspaceVaultPathArg(args Args, name string) (string, error) {
	raw := firstNonEmpty(args.String(name), args.String("path"), args.String("vault"))
	if strings.TrimSpace(raw) == "" {
		return "", tuskerError(errorMissingArg, "Missing required argument --"+name, withContext(map[string]any{"arg": "--" + name}))
	}
	return expandAbsPath(raw)
}

func expandAbsPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "~" {
		path = userHomeDir()
	} else if strings.HasPrefix(path, "~/") {
		path = filepath.Join(userHomeDir(), strings.TrimPrefix(path, "~/"))
	}
	return filepath.Abs(path)
}

func upsertProjectTrackerMetadata(trackerRoot, repoRoot, mountName string) (ProjectTrackerMetadata, error) {
	path := filepath.Join(trackerRoot, "_system", "project.yaml")
	nowDate := todayISO()
	meta := ProjectTrackerMetadata{}
	if fileExists(path) {
		raw, err := readText(path)
		if err != nil {
			return meta, err
		}
		if err := yaml.Unmarshal([]byte(raw), &meta); err != nil {
			return meta, tuskerError(errorConfigInvalid, "failed to parse project metadata: "+err.Error(), withPath(path))
		}
	}
	if strings.TrimSpace(meta.ProjectID) == "" {
		meta.ProjectID = sanitizeProjectID(defaultMountName(repoRoot))
	}
	if strings.TrimSpace(meta.DisplayName) == "" {
		meta.DisplayName = filepath.Base(repoRoot)
	}
	meta.RepoRoot = repoRoot
	meta.MountName = mountName
	if strings.TrimSpace(meta.Created) == "" {
		meta.Created = nowDate
	}
	meta.Updated = nowDate
	raw, err := yaml.Marshal(meta)
	if err != nil {
		return meta, err
	}
	if err := writeText(path, string(raw)); err != nil {
		return meta, err
	}
	return meta, nil
}

func ensureWorkspaceMount(mountPath, trackerRoot string, force bool) error {
	if err := ensureDir(filepath.Dir(mountPath)); err != nil {
		return err
	}
	info, err := os.Lstat(mountPath)
	if err == nil {
		if info.Mode()&os.ModeSymlink == 0 {
			return tuskerError(errorAlreadyExists, "mount path exists and is not a symlink: "+mountPath, withHint("Choose --name <folder> or move the existing folder yourself."), withPath(mountPath))
		}
		target, err := os.Readlink(mountPath)
		if err != nil {
			return err
		}
		resolved := target
		if !filepath.IsAbs(resolved) {
			resolved = filepath.Join(filepath.Dir(mountPath), resolved)
		}
		if canonicalPath(resolved) == canonicalPath(trackerRoot) {
			return nil
		}
		if !force {
			return tuskerError(errorAlreadyExists, "mount path already points somewhere else: "+mountPath, withHint("Use --force only if replacing that symlink is intentional."), withPath(mountPath))
		}
		if err := os.Remove(mountPath); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.Symlink(trackerRoot, mountPath)
}

func removeWorkspaceMount(mountPath string) error {
	info, err := os.Lstat(mountPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return tuskerError(errorInvalidArg, "refusing to unmount a non-symlink path: "+mountPath, withPath(mountPath))
	}
	return os.Remove(mountPath)
}

func repairWorkspaceMounts(cfg WorkspaceVaultConfig, force bool) ([]MountStatus, error) {
	if strings.TrimSpace(cfg.ObsidianVault) == "" {
		return nil, tuskerError(errorMissingArg, "No Obsidian vault configured.", withHint("Run `tusker init --vault <repo>/.tusker --yes` without --mount, or configure the Obsidian vault in the workspace config file."))
	}
	if err := ensureDir(cfg.ObsidianVault); err != nil {
		return nil, err
	}
	cfg.refreshMountPaths()
	results := make([]MountStatus, 0, len(cfg.Projects))
	for _, project := range cfg.Projects {
		if err := ensureWorkspaceMount(project.MountPath, project.TrackerRoot, force); err != nil {
			return nil, err
		}
		results = append(results, workspaceMountStatus(project))
	}
	return results, nil
}

func workspaceMountStatuses(cfg WorkspaceVaultConfig) []MountStatus {
	cfg.refreshMountPaths()
	statuses := make([]MountStatus, 0, len(cfg.Projects))
	for _, project := range cfg.Projects {
		statuses = append(statuses, workspaceMountStatus(project))
	}
	return statuses
}

func workspaceMountStatus(project WorkspaceProject) MountStatus {
	state := "missing"
	target := ""
	info, err := os.Lstat(project.MountPath)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			target, _ = os.Readlink(project.MountPath)
			resolved := target
			if !filepath.IsAbs(resolved) {
				resolved = filepath.Join(filepath.Dir(project.MountPath), resolved)
			}
			if canonicalPath(resolved) == canonicalPath(project.TrackerRoot) {
				state = "mounted"
			} else {
				state = "wrong-target"
			}
		} else {
			state = "blocked"
		}
	}
	return MountStatus{WorkspaceProject: project, State: state, Target: target}
}

func (cfg *WorkspaceVaultConfig) refreshMountPaths() {
	for i := range cfg.Projects {
		if strings.TrimSpace(cfg.Projects[i].MountName) == "" {
			cfg.Projects[i].MountName = defaultMountName(cfg.Projects[i].RepoRoot)
		}
		cfg.Projects[i].MountPath = filepath.Join(cfg.ObsidianVault, cfg.Projects[i].MountName)
	}
}

func (cfg *WorkspaceVaultConfig) upsertProject(project WorkspaceProject) {
	for i, existing := range cfg.Projects {
		if sameWorkspaceProject(existing, project) {
			if strings.TrimSpace(existing.AddedAt) != "" {
				project.AddedAt = existing.AddedAt
			}
			cfg.Projects[i] = project
			return
		}
	}
	cfg.Projects = append(cfg.Projects, project)
}

func (cfg *WorkspaceVaultConfig) removeProject(project WorkspaceProject) {
	next := cfg.Projects[:0]
	for _, existing := range cfg.Projects {
		if sameWorkspaceProject(existing, project) {
			continue
		}
		next = append(next, existing)
	}
	cfg.Projects = next
}

func (cfg WorkspaceVaultConfig) resolveProject(args Args) (WorkspaceProject, error) {
	name := strings.TrimSpace(firstNonEmpty(args.String("name"), args.String("mount-name")))
	if name != "" {
		name = sanitizeMountName(name)
		for _, project := range cfg.Projects {
			if project.MountName == name {
				return project, nil
			}
		}
		return WorkspaceProject{}, tuskerError(errorNotFound, "mounted project not found: "+name)
	}
	repoArg := strings.TrimSpace(args.String("repo"))
	vaultArg := strings.TrimSpace(args.String("vault"))
	target := firstNonEmpty(repoArg, vaultArg, mustGetwd())
	abs, err := filepath.Abs(target)
	if err != nil {
		return WorkspaceProject{}, err
	}
	var matches []WorkspaceProject
	for _, project := range cfg.Projects {
		repoMatches := strings.TrimSpace(project.RepoRoot) != "" && pathWithin(project.RepoRoot, abs)
		trackerMatches := strings.TrimSpace(project.TrackerRoot) != "" && pathWithin(project.TrackerRoot, abs)
		mountMatches := strings.TrimSpace(project.MountPath) != "" && canonicalPath(project.MountPath) == canonicalPath(abs)
		if repoMatches || trackerMatches || mountMatches {
			matches = append(matches, project)
		}
	}
	if len(matches) == 0 {
		return WorkspaceProject{}, tuskerError(errorNotFound, "no mounted project matches this path; use --name")
	}
	if len(matches) > 1 {
		return WorkspaceProject{}, tuskerError(errorInvalidArg, "multiple mounted projects match this path; use --name")
	}
	return matches[0], nil
}

func sameWorkspaceProject(a, b WorkspaceProject) bool {
	if strings.TrimSpace(a.ProjectID) != "" && strings.TrimSpace(b.ProjectID) != "" && a.ProjectID == b.ProjectID {
		return true
	}
	if strings.TrimSpace(a.RepoRoot) != "" && strings.TrimSpace(b.RepoRoot) != "" && canonicalPath(a.RepoRoot) == canonicalPath(b.RepoRoot) {
		return true
	}
	return strings.TrimSpace(a.TrackerRoot) != "" && strings.TrimSpace(b.TrackerRoot) != "" && canonicalPath(a.TrackerRoot) == canonicalPath(b.TrackerRoot)
}

func defaultMountName(repoRoot string) string {
	name := filepath.Base(filepath.Clean(repoRoot))
	if name == "." || name == string(filepath.Separator) || strings.TrimSpace(name) == "" {
		name = "project"
	}
	return sanitizeMountName(name)
}

var mountNameUnsafePattern = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func sanitizeMountName(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, string(filepath.Separator))
	value = strings.ReplaceAll(value, string(filepath.Separator), "-")
	value = mountNameUnsafePattern.ReplaceAllString(value, "-")
	value = strings.Trim(value, ".-_")
	return value
}

func sanitizeProjectID(value string) string {
	value = strings.ToLower(sanitizeMountName(value))
	if value == "" {
		return "project"
	}
	return value
}
