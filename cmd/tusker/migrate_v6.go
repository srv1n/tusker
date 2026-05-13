package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

type v6MigrationReport struct {
	Vault         string            `json:"vault"`
	DryRun        bool              `json:"dry_run"`
	Compatibility string            `json:"compatibility"`
	Moves         []v6MigrationMove `json:"moves"`
	FieldRewrites []v6FieldRewrite  `json:"field_rewrites"`
	Warnings      []string          `json:"warnings,omitempty"`
}

type v6MigrationMove struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
}

type v6FieldRewrite struct {
	Path  string `json:"path"`
	From  string `json:"from"`
	To    string `json:"to"`
	Count int    `json:"count,omitempty"`
}

func migrateV5VaultToV6(args Args) (*v6MigrationReport, error) {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return nil, err
	}
	if !args.Bool("dry-run") {
		return nil, tuskerError(errorInvalidArg, "V6 migration apply is not automated yet; run with --dry-run for the explicit move/rewrite plan")
	}
	notes, err := listAllNotes(vaultPath)
	if err != nil {
		return nil, err
	}
	docsMap, err := loadDocsMap(vaultPath)
	if err != nil {
		return nil, err
	}
	report := &v6MigrationReport{
		Vault:         vaultPath,
		DryRun:        true,
		Compatibility: "clean break: docs-map is not preserved as authored source; docs/** moves to domains/**; doc_nodes/docs_resolution become knowledge_nodes/knowledge_resolution",
	}
	for _, note := range notes {
		switch {
		case stringField(note.Data, "schema") == "tusker.doc/v5":
			node := stringField(note.Data, "node")
			domain := ""
			if docsMap != nil {
				if mapped, ok := docsMap.Node(node); ok {
					domain = mapped.Domain
				}
			}
			if domain == "" {
				domains := normalizeList(note.Data["domains"])
				if len(domains) > 0 {
					domain = domains[0]
				}
			}
			if domain == "" {
				domain = "product"
			}
			leaf := strings.TrimPrefix(node, domain+"/")
			leaf = strings.TrimPrefix(leaf, "reference/")
			if leaf == "" {
				leaf = strings.TrimSuffix(strings.TrimPrefix(note.RelativePath, "docs/"), ".md")
			}
			target := filepath.ToSlash(filepath.Join("domains", domain, leaf+".md"))
			if strings.HasSuffix(strings.ToLower(leaf), "canon") || stringField(note.Data, "kind") == "canon" {
				target = filepath.ToSlash(filepath.Join("domains", domain, "CANON.md"))
			}
			report.Moves = append(report.Moves, v6MigrationMove{From: note.RelativePath, To: target, Kind: "doc-to-knowledge"})
			report.FieldRewrites = append(report.FieldRewrites, v6FieldRewrite{Path: note.RelativePath, From: "schema: tusker.doc/v5", To: "schema: tusker.knowledge/v6"})
		case stringField(note.Data, "type") == "task":
			if len(normalizeList(note.Data["doc_nodes"])) > 0 {
				report.FieldRewrites = append(report.FieldRewrites, v6FieldRewrite{Path: note.RelativePath, From: "doc_nodes", To: "knowledge_nodes", Count: len(normalizeList(note.Data["doc_nodes"]))})
			}
			if len(anySlice(note.Data["docs_resolution"])) > 0 {
				report.FieldRewrites = append(report.FieldRewrites, v6FieldRewrite{Path: note.RelativePath, From: "docs_resolution", To: "knowledge_resolution", Count: len(anySlice(note.Data["docs_resolution"]))})
			}
		case stringField(note.Data, "type") == "epic":
			if len(normalizeList(note.Data["doc_nodes"])) > 0 {
				report.FieldRewrites = append(report.FieldRewrites, v6FieldRewrite{Path: note.RelativePath, From: "doc_nodes", To: "knowledge_nodes", Count: len(normalizeList(note.Data["doc_nodes"]))})
			}
		}
	}
	sort.Slice(report.Moves, func(i, j int) bool { return report.Moves[i].From < report.Moves[j].From })
	sort.Slice(report.FieldRewrites, func(i, j int) bool {
		if report.FieldRewrites[i].Path != report.FieldRewrites[j].Path {
			return report.FieldRewrites[i].Path < report.FieldRewrites[j].Path
		}
		return report.FieldRewrites[i].From < report.FieldRewrites[j].From
	})
	if len(report.Moves) == 0 {
		report.Warnings = append(report.Warnings, "No V5 docs were found to move.")
	}
	return report, nil
}

func printV6MigrationReport(report *v6MigrationReport, args Args) {
	if args.Bool("json") {
		emitJSON(report)
		return
	}
	fmt.Printf("Would migrate V5 vault to V6 clean-break layout in %s\n", report.Vault)
	fmt.Printf("Compatibility: %s\n", report.Compatibility)
	if len(report.Moves) > 0 {
		fmt.Println("Moves:")
		for _, move := range report.Moves {
			fmt.Printf("  %s -> %s (%s)\n", move.From, move.To, move.Kind)
		}
	}
	if len(report.FieldRewrites) > 0 {
		fmt.Println("Field rewrites:")
		for _, rewrite := range report.FieldRewrites {
			suffix := ""
			if rewrite.Count > 0 {
				suffix = fmt.Sprintf(" (%d)", rewrite.Count)
			}
			fmt.Printf("  %s: %s -> %s%s\n", rewrite.Path, rewrite.From, rewrite.To, suffix)
		}
	}
	for _, warning := range report.Warnings {
		fmt.Println("Warning: " + warning)
	}
}
