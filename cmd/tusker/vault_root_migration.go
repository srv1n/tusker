package main

import (
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
	configRoot := relativeFromRepo(repoRoot, destVault)
	configPath := filepath.Join(repoRoot, "tusker.yaml")
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
	if !report.DryRun {
		if err := os.Rename(sourceVault, destVault); err != nil {
			return err
		}
		report.Moved = true
		changed, err := patchTuskerStorageRoot(configPath, configRoot)
		if err != nil {
			return err
		}
		if changed {
			report.UpdatedFiles = append(report.UpdatedFiles, configPath)
		}
		readmeLink := filepath.ToSlash(filepath.Join(configRoot, "README.md"))
		for _, filename := range []string{"AGENTS.md", "CLAUDE.md"} {
			path := filepath.Join(repoRoot, filename)
			changed, err := upsertTuskerPointer(path, readmeLink)
			if err != nil {
				return err
			}
			if changed != "" {
				report.UpdatedFiles = append(report.UpdatedFiles, path)
			}
		}
	}
	if args.Bool("json") {
		emitJSON(report)
		return nil
	}
	printVaultRootMigrationReport(report)
	return nil
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
	root = filepath.ToSlash(strings.TrimSpace(root))
	if root == "" {
		root = defaultRepoVaultDir
	}
	before := ""
	doc := yaml.Node{Kind: yaml.DocumentNode}
	if fileExists(configPath) {
		raw, err := readText(configPath)
		if err != nil {
			return false, err
		}
		before = raw
		if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
			return false, err
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
		return false, err
	}
	after := string(raw)
	if before == after {
		return false, nil
	}
	return true, writeText(configPath, after)
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
