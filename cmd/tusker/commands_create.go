package main

import (
	"fmt"
	"path/filepath"
	"strings"
)

func bootstrap(args Args) error {
	vaultPath, err := resolveVaultPath(args, true)
	if err != nil {
		return err
	}
	date := todayISO()

	for _, relative := range []string{
		"",
		"epics",
		"docs/spec",
		"docs/reference",
		"_config",
		"Attachments",
		"_system/templates",
		"_system/views",
		"_system/generated",
		"_system/runs",
		"_system/events",
		"_system/snippets",
		"_system/archive",
		"_system/workspaces",
		"_system/logs",
	} {
		if err := ensureDir(filepath.Join(vaultPath, relative)); err != nil {
			return err
		}
	}
	if err := writeDefaultConfig(vaultPath); err != nil {
		return err
	}
	docsMapPath := filepath.Join(vaultPath, "_config", "docs-map.yaml")
	if !fileExists(docsMapPath) {
		if err := writeText(docsMapPath, defaultDocsMapYAML(date)); err != nil {
			return err
		}
	}
	if err := writeDefaultV5Docs(vaultPath, date); err != nil {
		return err
	}
	if err := writeDefaultV5VaultTemplates(vaultPath, date); err != nil {
		return err
	}
	if err := writeDefaultV5VaultViews(vaultPath); err != nil {
		return err
	}
	architecturePath := filepath.Join(vaultPath, "architecture.md")
	if !fileExists(architecturePath) {
		content := fmt.Sprintf("---\ntitle: \"Product architecture\"\ntype: \"note\"\ncreated: \"%s\"\nupdated: \"%s\"\ntags:\n  - architecture\n---\n\n# Product architecture\n\nProduct-wide durable architectural decisions.\n\n## Decisions\n\n", date, date)
		if err := writeText(architecturePath, content); err != nil {
			return err
		}
	}
	dashboardPath := filepath.Join(vaultPath, "Dashboard.md")
	if err := writeText(dashboardPath, defaultV5DashboardNote(date)); err != nil {
		return err
	}
	cheatsheetPath := filepath.Join(vaultPath, "CHEATSHEET.md")
	if err := writeText(cheatsheetPath, defaultV5CheatsheetNote(date)); err != nil {
		return err
	}
	if err := upsertGitignore(vaultPath); err != nil {
		return err
	}
	if epic := strings.ToUpper(args.String("epic")); epic != "" {
		if !epicAcronymPattern.MatchString(epic) {
			return tuskerError(errorInvalidArg, fmt.Sprintf(`--epic must be 3 uppercase letters, got "%s"`, args.String("epic")), withContext(map[string]any{"arg": "--epic", "value": args.String("epic")}))
		}
		title, err := requireArg(args, "title")
		if err != nil {
			return tuskerError(errorMissingArg, "--title (required with --epic)")
		}
		if err := newV5Epic(Args{
			"vault":   vaultPath,
			"quiet":   "true",
			"acronym": epic,
			"title":   title,
			"owner":   args.String("owner"),
			"summary": args.String("summary"),
			"status":  fallback(args.String("status"), "draft"),
		}); err != nil {
			return err
		}
	}
	if !args.Bool("quiet") {
		fmt.Printf("Tusker vault initialized at %s\n", vaultPath)
	}
	return nil
}
