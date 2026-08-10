package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type vaultRootMigrationReport struct {
	From         string   `json:"from"`
	To           string   `json:"to"`
	Config       string   `json:"config"`
	Moved        bool     `json:"moved"`
	UpdatedFiles []string `json:"updated_files"`
	Warnings     []string `json:"warnings,omitempty"`
	DryRun       bool     `json:"dry_run,omitempty"`
}

type vaultRootMigrationFile struct {
	path    string
	exists  bool
	before  []byte
	after   []byte
	mode    os.FileMode
	changed bool
}

var (
	vaultRootMigrationRename = os.Rename
	vaultRootMigrationRemove = os.Remove
	vaultRootMigrationWrite  = writeVaultRootMigrationFile
)

func migrateVaultRootCmd(args Args) error {
	if args.Bool("help") {
		printMigrateVaultRootHelp()
		return nil
	}
	toArg := strings.TrimSpace(args.String("to"))
	if toArg == "" {
		return tuskerError(errorMissingArg, "migrate vault-root requires --to <path>", withContext(map[string]any{"arg": "--to"}))
	}
	sourceVault, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	repoRoot := filepath.Dir(sourceVault)
	destVault := toArg
	if !filepath.IsAbs(destVault) {
		destVault = filepath.Join(repoRoot, filepath.FromSlash(toArg))
	}
	destVault, err = filepath.Abs(destVault)
	if err != nil {
		return err
	}
	sourceVault, err = filepath.Abs(sourceVault)
	if err != nil {
		return err
	}
	if !isDiscoverableVaultDestination(repoRoot, destVault) {
		return tuskerError(errorInvalidArg, fmt.Sprintf("destination is not a supported discoverable vault root: %s", destVault), withContext(map[string]any{"to": destVault, "supported": []string{defaultRepoVaultDir, legacyRepoVaultDir}}))
	}
	configRoot := relativeFromRepo(repoRoot, destVault)
	configPath := preferredTuskerConfigPath(sourceVault)
	configMovesWithVault := samePath(configPath, managedTuskerConfigPath(sourceVault))
	report := vaultRootMigrationReport{
		From:   sourceVault,
		To:     destVault,
		Config: configPath,
		DryRun: args.Bool("dry-run"),
	}
	if samePath(sourceVault, destVault) {
		report.Warnings = append(report.Warnings, "source vault already matches requested destination")
		if args.Bool("json") {
			emitJSON(report)
		} else {
			printVaultRootMigrationReport(report)
		}
		return nil
	}
	if warning := gitDirtyWarning(repoRoot); warning != "" {
		report.Warnings = append(report.Warnings, warning)
	}
	if fileExists(destVault) || dirExists(destVault) {
		return tuskerError(errorInvalidArg, fmt.Sprintf("destination already exists: %s", destVault), withContext(map[string]any{"to": destVault}))
	}
	if configMovesWithVault {
		configPath = managedTuskerConfigPath(destVault)
		report.Config = configPath
	}
	configSourcePath := configPath
	if configMovesWithVault {
		configSourcePath = managedTuskerConfigPath(sourceVault)
	}
	files, err := planVaultRootMigrationFiles(configSourcePath, configPath, configRoot, repoRoot)
	if err != nil {
		return err
	}
	for _, file := range files {
		if file.changed {
			report.UpdatedFiles = append(report.UpdatedFiles, file.path)
		}
	}
	if !report.DryRun {
		if err := vaultRootMigrationRename(sourceVault, destVault); err != nil {
			return err
		}
		report.Moved = true
		for _, file := range files {
			if !file.changed {
				continue
			}
			if err := vaultRootMigrationWrite(file.path, file.after, file.mode); err != nil {
				return vaultRootMigrationFailure(err, sourceVault, destVault, files)
			}
		}
		discovered, err := discoverVault(repoRoot)
		if err != nil {
			return vaultRootMigrationFailure(err, sourceVault, destVault, files)
		}
		if !samePath(discovered, destVault) {
			return vaultRootMigrationFailure(fmt.Errorf("post-migration discovery resolved %q, want %q", discovered, destVault), sourceVault, destVault, files)
		}
	}
	if args.Bool("json") {
		emitJSON(report)
		return nil
	}
	printVaultRootMigrationReport(report)
	return nil
}

func isDiscoverableVaultDestination(repoRoot, destVault string) bool {
	return samePath(destVault, filepath.Join(repoRoot, defaultRepoVaultDir)) ||
		samePath(destVault, filepath.Join(repoRoot, legacyRepoVaultDir))
}

func planVaultRootMigrationFiles(configSourcePath, configPath, configRoot, repoRoot string) ([]vaultRootMigrationFile, error) {
	config, err := planVaultRootMigrationConfig(configSourcePath, configPath, configRoot)
	if err != nil {
		return nil, err
	}
	files := []vaultRootMigrationFile{config}
	readmeLink := filepath.ToSlash(filepath.Join(configRoot, "README.md"))
	for _, filename := range []string{"AGENTS.md", "CLAUDE.md"} {
		pointer, err := planVaultRootMigrationPointer(filepath.Join(repoRoot, filename), readmeLink)
		if err != nil {
			return nil, err
		}
		files = append(files, pointer)
	}
	return files, nil
}

func planVaultRootMigrationConfig(sourcePath, targetPath, root string) (vaultRootMigrationFile, error) {
	file := vaultRootMigrationFile{path: targetPath, mode: 0o644}
	info, err := os.Lstat(sourcePath)
	if err != nil && !os.IsNotExist(err) {
		return file, err
	}
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return file, fmt.Errorf("refusing to migrate non-regular config file: %s", sourcePath)
		}
		file.exists = true
		file.mode = info.Mode().Perm()
		file.before, err = os.ReadFile(sourcePath)
		if err != nil {
			return file, err
		}
	}
	file.after, err = patchedTuskerStorageRoot(file.before, root)
	if err != nil {
		return file, err
	}
	file.changed = !file.exists || string(file.before) != string(file.after)
	return file, nil
}

func planVaultRootMigrationPointer(path, readmeLink string) (vaultRootMigrationFile, error) {
	file := vaultRootMigrationFile{path: path, mode: 0o644}
	info, err := os.Lstat(path)
	if err != nil && !os.IsNotExist(err) {
		return file, err
	}
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return file, fmt.Errorf("refusing to migrate non-regular pointer file: %s", path)
		}
		file.exists = true
		file.mode = info.Mode().Perm()
		file.before, err = os.ReadFile(path)
		if err != nil {
			return file, err
		}
	}
	block := renderTuskerPointerBlock(readmeLink)
	current := string(file.before)
	begin, end := strings.Index(current, tuskerPointerBegin), strings.Index(current, tuskerPointerEnd)
	if begin != -1 && end != -1 && end > begin {
		file.after = []byte(current[:begin] + block + current[end+len(tuskerPointerEnd):])
	} else if !file.exists {
		file.after = []byte(block + "\n")
	} else {
		file.after = []byte(strings.TrimRight(current, " \t\r\n") + "\n\n" + block + "\n")
	}
	file.changed = !file.exists || string(file.before) != string(file.after)
	return file, nil
}

func vaultRootMigrationFailure(cause error, sourceVault, destVault string, files []vaultRootMigrationFile) error {
	var rollbackErrs []error
	for i := len(files) - 1; i >= 0; i-- {
		file := files[i]
		if !file.changed {
			continue
		}
		if err := restoreVaultRootMigrationFile(file); err != nil {
			rollbackErrs = append(rollbackErrs, err)
		}
	}
	if err := vaultRootMigrationRename(destVault, sourceVault); err != nil {
		rollbackErrs = append(rollbackErrs, fmt.Errorf("restore vault root: %w", err))
	}
	if rollback := errors.Join(rollbackErrs...); rollback != nil {
		return fmt.Errorf("vault-root migration failed: %w", errors.Join(cause, fmt.Errorf("rollback failed: %w", rollback)))
	}
	return fmt.Errorf("vault-root migration failed and was rolled back: %w", cause)
}

func restoreVaultRootMigrationFile(file vaultRootMigrationFile) error {
	if !file.exists {
		if err := vaultRootMigrationRemove(file.path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove %s: %w", file.path, err)
		}
		return nil
	}
	if err := vaultRootMigrationWrite(file.path, file.before, file.mode); err != nil {
		return fmt.Errorf("restore %s: %w", file.path, err)
	}
	return nil
}

func writeVaultRootMigrationFile(path string, content []byte, mode os.FileMode) error {
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".vault-root-migration-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	committed := false
	defer func() {
		_ = temp.Close()
		if !committed {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(mode.Perm()); err != nil {
		return err
	}
	if _, err := temp.Write(content); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := vaultRootMigrationRename(tempPath, path); err != nil {
		return err
	}
	committed = true
	return syncV7DocumentDirectory(filepath.Dir(path))
}

func printVaultRootMigrationReport(report vaultRootMigrationReport) {
	mode := "Migrated"
	if report.DryRun {
		mode = "Would migrate"
	}
	fmt.Printf("%s vault root:\n", mode)
	fmt.Printf("  from: %s\n", report.From)
	fmt.Printf("  to:   %s\n", report.To)
	if len(report.UpdatedFiles) > 0 {
		fmt.Println("Updated files:")
		for _, path := range report.UpdatedFiles {
			fmt.Println("  " + path)
		}
	}
	for _, warning := range report.Warnings {
		fmt.Println("Warning: " + warning)
	}
}

func patchTuskerStorageRoot(configPath, root string) (bool, error) {
	before := ""
	if fileExists(configPath) {
		raw, err := readText(configPath)
		if err != nil {
			return false, err
		}
		before = raw
	}
	afterRaw, err := patchedTuskerStorageRoot([]byte(before), root)
	if err != nil {
		return false, err
	}
	after := string(afterRaw)
	if before == after {
		return false, nil
	}
	return true, writeText(configPath, after)
}

func patchedTuskerStorageRoot(before []byte, root string) ([]byte, error) {
	root = filepath.ToSlash(strings.TrimSpace(root))
	if root == "" {
		root = defaultRepoVaultDir
	}
	doc := yaml.Node{Kind: yaml.DocumentNode}
	if len(before) > 0 {
		if err := yaml.Unmarshal(before, &doc); err != nil {
			return nil, err
		}
	}
	if len(doc.Content) == 0 {
		doc.Content = []*yaml.Node{{Kind: yaml.MappingNode}}
	}
	mapping := doc.Content[0]
	if mapping.Kind != yaml.MappingNode {
		mapping.Kind = yaml.MappingNode
		mapping.Content = nil
	}
	setYAMLScalar(mapping, "schema", "tusker.config/v1")
	storage := ensureYAMLMap(mapping, "storage")
	setYAMLScalar(storage, "root", root)
	setYAMLScalar(storage, "generated_root", filepath.ToSlash(filepath.Join(root, "_generated")))
	setYAMLScalar(storage, "evidence_root", filepath.ToSlash(filepath.Join(root, "evidence")))
	setYAMLScalar(storage, "events_root", filepath.ToSlash(filepath.Join(root, "events")))
	setYAMLScalar(storage, "attempts_root", filepath.ToSlash(filepath.Join(root, "attempts")))
	raw, err := yaml.Marshal(&doc)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func ensureYAMLMap(parent *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(parent.Content); i += 2 {
		if parent.Content[i].Value == key {
			value := parent.Content[i+1]
			if value.Kind != yaml.MappingNode {
				value.Kind = yaml.MappingNode
				value.Content = nil
			}
			return value
		}
	}
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: key}
	valueNode := &yaml.Node{Kind: yaml.MappingNode}
	parent.Content = append(parent.Content, keyNode, valueNode)
	return valueNode
}

func setYAMLScalar(parent *yaml.Node, key, value string) {
	for i := 0; i+1 < len(parent.Content); i += 2 {
		if parent.Content[i].Value == key {
			parent.Content[i+1] = &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
			return
		}
	}
	parent.Content = append(parent.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value},
	)
}

func gitDirtyWarning(repoRoot string) string {
	if !dirExists(filepath.Join(repoRoot, ".git")) {
		return ""
	}
	out, err := exec.Command("git", "-C", repoRoot, "status", "--short").Output()
	if err != nil {
		return ""
	}
	if strings.TrimSpace(string(out)) == "" {
		return ""
	}
	return "git worktree has uncommitted changes; migration continued without auto-cleanup"
}

func samePath(a, b string) bool {
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	if errA == nil && errB == nil {
		return aa == bb
	}
	return filepath.Clean(a) == filepath.Clean(b)
}
