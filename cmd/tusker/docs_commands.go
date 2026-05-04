package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func docsInitCmd(args Args) error {
	siteRoot, err := docsResolveSiteRoot(args)
	if err != nil {
		return err
	}
	for _, rel := range []string{
		docsGeneratedRootRelative,
		docsAssetsRootRelative,
		filepath.Join(docsContentRootRelative, "developer"),
		filepath.Join(docsContentRootRelative, "user"),
	} {
		if err := ensureDir(filepath.Join(siteRoot, rel)); err != nil {
			return err
		}
	}
	placeholderNav := filepath.Join(siteRoot, docsNavigationRelative)
	if !fileExists(placeholderNav) || args.Bool("force") {
		if err := writeJSON(placeholderNav, docsNavigation{}); err != nil {
			return err
		}
	}
	placeholderManifest := filepath.Join(siteRoot, docsContentManifestRelative)
	if !fileExists(placeholderManifest) || args.Bool("force") {
		if err := writeJSON(placeholderManifest, docsContentManifest{SchemaVersion: docsManifestSchemaVersion, GeneratedAt: time.Now().UTC().Format(time.RFC3339), Items: []docsContentManifestItem{}}); err != nil {
			return err
		}
	}
	placeholderCanon := filepath.Join(siteRoot, docsCanonManifestRelative)
	if !fileExists(placeholderCanon) || args.Bool("force") {
		if err := writeJSON(placeholderCanon, docsCanonManifest{SchemaVersion: docsManifestSchemaVersion, GeneratedAt: time.Now().UTC().Format(time.RFC3339), Canon: []docsCanonManifestDoc{}, Published: []docsCanonManifestDoc{}}); err != nil {
			return err
		}
	}
	publicCanon := filepath.Join(siteRoot, docsPublicCanonRelative)
	if !fileExists(publicCanon) || args.Bool("force") {
		if err := writeJSON(publicCanon, docsCanonManifest{SchemaVersion: docsManifestSchemaVersion, GeneratedAt: time.Now().UTC().Format(time.RFC3339), Canon: []docsCanonManifestDoc{}, Published: []docsCanonManifestDoc{}}); err != nil {
			return err
		}
	}
	removedRoutes := filepath.Join(siteRoot, docsRoutesRemovedRelative)
	if !fileExists(removedRoutes) || args.Bool("force") {
		if err := writeJSON(removedRoutes, docsRemovedRoutesReport{SchemaVersion: docsManifestSchemaVersion, GeneratedAt: time.Now().UTC().Format(time.RFC3339), Removed: []docsRemovedRoute{}}); err != nil {
			return err
		}
	}
	if !args.Bool("json") {
		fmt.Printf("Initialized docs output directories under %s\n", siteRoot)
	}
	return nil
}

func docsModelCmd(args Args) error {
	if args.Bool("json") {
		emitJSON(map[string]any{
			"ok": true,
			"diataxis": []map[string]string{
				{"mode": "tutorial", "reader_need": "learn by doing", "default_section": "Start here"},
				{"mode": "how-to", "reader_need": "complete a task", "default_section": "Guides"},
				{"mode": "reference", "reader_need": "look up facts", "default_section": "Reference"},
				{"mode": "explanation", "reader_need": "understand why", "default_section": "Concepts"},
			},
			"agent_layers": []map[string]string{
				{"agent_layer": "none", "meaning": "human-facing doc only"},
				{"agent_layer": "capsule", "meaning": "human-facing doc with a compact agent note"},
				{"agent_layer": "standalone", "meaning": "agent-facing runbook or recipe"},
			},
		})
		return nil
	}
	fmt.Println(`Tusker docs model

Docs are durable knowledge pages under tusker/docs/**. Tasks do not become docs;
tasks point at exact doc_nodes and prove the docs impact was handled.

Access layer:
  _config/docs-map.yaml is the controlled catalog. It maps every doc_node to a
  source page, domain, Diátaxis mode, audience, agent layer, source-of-truth,
  and stale_when paths.

Diátaxis modes:
  tutorial     learn by doing       -> Start here
  how-to       complete a task      -> Guides
  reference    look up facts        -> Reference
  explanation  understand why       -> Concepts

Agent layer:
  none         human-facing doc only
  capsule      human-facing doc with a compact agent note
  standalone   agent-facing runbook or recipe

Close gate:
  If a task names doc_nodes, it cannot close until each node is applied,
  verified as no-op, or waived with a reason. High-risk tasks also need a
  Knowledge delta table explaining the reader-facing before/after change.

Useful commands:
  tusker docs map
  tusker docs catalog
  tusker docs freshness
  tusker docs check <TASK-ID>`)
	return nil
}

func docsMapCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	docsMap, err := loadDocsMap(vaultPath)
	if err != nil {
		return err
	}
	if docsMap == nil {
		return tuskerError(errorNotFound, "_config/docs-map.yaml not found", withHint("run `tusker init --yes` or create a docs map"))
	}
	nodeID := firstNonEmpty(args.String("node"), args.String("_pos0"))
	if nodeID != "" {
		node, ok := docsMap.Node(nodeID)
		if !ok {
			return tuskerError(errorUnknownDocNode, "unknown doc_node: "+nodeID, withHint("run `tusker docs map` to list valid nodes"))
		}
		if args.Bool("json") {
			emitJSON(map[string]any{"ok": true, "vault": vaultPath, "node": node})
			return nil
		}
		printDocsMapNode(node)
		return nil
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "vault": vaultPath, "docs_map": docsMap})
		return nil
	}
	fmt.Printf("Docs map: %s\n\n", filepath.Join(vaultPath, docsMapRelative))
	fmt.Println("This is the controlled docs catalog. Domains are broad areas; doc_nodes are exact targets tasks can name.")
	fmt.Println("Each node declares mode, audience, agent_layer, source_of_truth, and stale_when so docs freshness is mechanical.")
	fmt.Println()
	fmt.Println("Domains:")
	domainIDs := make([]string, 0, len(docsMap.Domains))
	for id := range docsMap.Domains {
		domainIDs = append(domainIDs, id)
	}
	sort.Strings(domainIDs)
	for _, id := range domainIDs {
		domain := docsMap.Domains[id]
		fmt.Printf("- %s — %s\n", id, fallback(domain.Description, domain.Label))
	}
	fmt.Println("\nDoc nodes:")
	for _, node := range docsMap.Nodes {
		fmt.Printf("- %s — %s\n", node.ID, node.CatalogTitle())
		fmt.Printf("  %s · %s · %s · %s · %s\n", node.SourcePath(), node.Domain, node.EffectiveMode(), node.Audience, node.EffectiveAgentLayer())
	}
	fmt.Println("\nInspect one node with `tusker docs map <doc-node>`.")
	return nil
}

func printDocsMapNode(node DocsMapNode) {
	fmt.Printf("Doc node: %s\n", node.ID)
	fmt.Printf("Title: %s\n", node.CatalogTitle())
	fmt.Printf("Page: %s\n", node.SourcePath())
	fmt.Printf("Domain: %s\n", node.Domain)
	fmt.Printf("Mode: %s\n", node.EffectiveMode())
	fmt.Printf("Audience: %s\n", node.Audience)
	fmt.Printf("Agent layer: %s\n", node.EffectiveAgentLayer())
	fmt.Printf("Publish: %s -> %s\n", fallback(node.PublishLane, "(none)"), fallback(node.PublishPath, "(none)"))
	if len(node.SourceOfTruth) > 0 {
		fmt.Printf("Source of truth: %s\n", strings.Join(node.SourceOfTruth, ", "))
	}
	if len(node.StaleWhen.Paths) > 0 {
		fmt.Printf("Stale when: %s\n", strings.Join(node.StaleWhen.Paths, ", "))
	}
	if len(node.Evals) > 0 {
		fmt.Printf("Evals: %s\n", strings.Join(node.Evals, ", "))
	}
}

type docsIndexPayload struct {
	GeneratedAt string           `json:"generatedAt"`
	Items       []map[string]any `json:"items"`
	Catalog     []map[string]any `json:"catalog"`
}

func loadDocsIndexPayload(vaultPath string) (docsIndexPayload, error) {
	raw, err := os.ReadFile(filepath.Join(vaultPath, "_system", "generated", "docs.index.json"))
	if err != nil {
		return docsIndexPayload{}, err
	}
	var payload docsIndexPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return docsIndexPayload{}, err
	}
	return payload, nil
}

func docsCatalogCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	if err := reindex(Args{"vault": vaultPath, "quiet": "true"}); err != nil {
		return err
	}
	payload, err := loadDocsIndexPayload(vaultPath)
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "vault": vaultPath, "generatedAt": payload.GeneratedAt, "catalog": payload.Catalog})
		return nil
	}
	fmt.Printf("Docs catalog: %s\n", filepath.Join(vaultPath, "Docs.md"))
	fmt.Printf("Generated: %s\n\n", payload.GeneratedAt)
	fmt.Println("Reader navigation is grouped by intent. Diátaxis mode stays as metadata so folders do not become taxonomy jail.")
	fmt.Println()
	currentSection := ""
	for _, item := range payload.Catalog {
		section := stringValue(item["section"])
		if section != currentSection {
			currentSection = section
			fmt.Printf("%s\n", section)
		}
		fmt.Printf("- %s — %s\n", stringValue(item["doc_node"]), stringValue(item["title"]))
		fmt.Printf("  %s · %s · %s · %s\n", stringValue(item["path"]), stringValue(item["mode"]), stringValue(item["audience"]), stringValue(item["freshness"]))
	}
	return nil
}

func docsFreshnessCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	if err := reindex(Args{"vault": vaultPath, "quiet": "true"}); err != nil {
		return err
	}
	payload, err := loadDocsIndexPayload(vaultPath)
	if err != nil {
		return err
	}
	filtered := docsFilterFreshness(payload.Catalog, args.Bool("stale"))
	counts := docsFreshnessCounts(payload.Catalog)
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "vault": vaultPath, "generatedAt": payload.GeneratedAt, "counts": counts, "items": filtered})
		return nil
	}
	fmt.Printf("Docs freshness: %s\n", filepath.Join(vaultPath, "_system", "generated", "docs.index.json"))
	fmt.Printf("Generated: %s\n", payload.GeneratedAt)
	fmt.Printf("Counts: %d verified, %d verified_by_task, %d needs_verification, %d waived, %d missing\n\n", counts["verified"], counts["verified_by_task"], counts["needs_verification"], counts["waived"], counts["missing"])
	if len(filtered) == 0 {
		fmt.Println("No docs matched the freshness filter.")
		return nil
	}
	for _, item := range filtered {
		fmt.Printf("- %s — %s\n", stringValue(item["doc_node"]), stringValue(item["freshness"]))
		fmt.Printf("  page: %s\n", stringValue(item["path"]))
		if linked := normalizeList(item["linked_tasks"]); len(linked) > 0 {
			fmt.Printf("  linked tasks: %s\n", strings.Join(linked, ", "))
		}
		if event, ok := item["last_verified_event"].(map[string]any); ok && len(event) > 0 {
			fmt.Printf("  last event: %s by %s on %s via %s\n", stringValue(event["status"]), stringValue(event["actor"]), stringValue(event["date"]), stringValue(event["task"]))
		}
		if staleDueTo := normalizeList(item["stale_due_to"]); len(staleDueTo) > 0 {
			fmt.Printf("  stale due to: %s\n", strings.Join(staleDueTo, ", "))
		}
	}
	return nil
}

func docsFilterFreshness(catalog []map[string]any, staleOnly bool) []map[string]any {
	var out []map[string]any
	for _, item := range catalog {
		if staleOnly {
			switch stringValue(item["freshness"]) {
			case "verified", "verified_by_task":
				continue
			}
		}
		out = append(out, item)
	}
	return out
}

func docsFreshnessCounts(catalog []map[string]any) map[string]int {
	counts := map[string]int{"verified": 0, "verified_by_task": 0, "needs_verification": 0, "waived": 0, "missing": 0}
	for _, item := range catalog {
		status := stringValue(item["freshness"])
		if _, ok := counts[status]; !ok {
			counts[status] = 0
		}
		counts[status]++
	}
	return counts
}

func docsExportCmd(args Args) error {
	options, err := docsResolveExportOptions(args)
	if err != nil {
		return err
	}
	if strings.TrimSpace(options.VaultRoot) != "" {
		reindexArgs := Args{"vault": options.VaultRoot, "quiet": "true"}
		if err := reindex(reindexArgs); err != nil {
			return err
		}
	}
	summary, err := runDocsExport(options)
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "summary": summary})
		return nil
	}
	fmt.Printf("Exported %d doc%s, copied %d asset%s.\n", summary.ExportedDocs, plural(summary.ExportedDocs), summary.ExportedAssets, plural(summary.ExportedAssets))
	if summary.DeletedDocs > 0 || summary.DeletedAssets > 0 {
		fmt.Printf("Removed %d stale doc%s and %d stale asset%s.\n", summary.DeletedDocs, plural(summary.DeletedDocs), summary.DeletedAssets, plural(summary.DeletedAssets))
	}
	return nil
}

func docsBuildCmd(args Args) error {
	options, err := docsResolveExportOptions(args)
	if err != nil {
		return err
	}
	if strings.TrimSpace(options.VaultRoot) != "" {
		reindexArgs := Args{"vault": options.VaultRoot, "quiet": "true"}
		if err := reindex(reindexArgs); err != nil {
			return err
		}
	}
	summary, err := runDocsExport(options)
	if err != nil {
		return err
	}
	if err := runAstroCommand(options.SiteRoot, "build"); err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "summary": summary, "built": true})
		return nil
	}
	fmt.Printf("Built docs site from %d route%s at %s\n", summary.ExportedDocs+summary.SkippedDocs, plural(summary.ExportedDocs+summary.SkippedDocs), options.SiteRoot)
	return nil
}

func docsDevCmd(args Args) error {
	options, err := docsResolveExportOptions(args)
	if err != nil {
		return err
	}
	if strings.TrimSpace(options.VaultRoot) != "" {
		reindexArgs := Args{"vault": options.VaultRoot, "quiet": "true"}
		if err := reindex(reindexArgs); err != nil {
			return err
		}
	}
	if _, err := runDocsExport(options); err != nil {
		return err
	}
	devArgs := []string{"dev"}
	if host := strings.TrimSpace(args.String("host")); host != "" {
		devArgs = append(devArgs, "--host", host)
	}
	if port := strings.TrimSpace(args.String("port")); port != "" {
		devArgs = append(devArgs, "--port", port)
	}
	if args.Bool("watch") {
		fmt.Fprintln(os.Stderr, "docs watch mode enabled; Tusker will re-export when vault or registered repo docs change.")
		stopWatch := make(chan struct{})
		watchDone := make(chan struct{})
		go docsWatchLoop(options, 2*time.Second, stopWatch, watchDone)
		defer func() {
			close(stopWatch)
			<-watchDone
		}()
	}
	return runAstroCommand(options.SiteRoot, devArgs...)
}

func docsResolveExportOptions(args Args) (docsExportOptions, error) {
	repoRoot, err := docsResolveRepoRoot()
	if err != nil {
		return docsExportOptions{}, err
	}
	siteRoot, err := docsResolveSiteRoot(args)
	if err != nil {
		return docsExportOptions{}, err
	}
	vaultRoot, err := docsResolveOptionalVault(args)
	if err != nil {
		return docsExportOptions{}, err
	}
	return docsExportOptions{
		RepoRoot:   repoRoot,
		SiteRoot:   siteRoot,
		VaultRoot:  vaultRoot,
		Clean:      args.Bool("clean"),
		PublicOnly: args.Bool("public-only"),
	}, nil
}

func docsResolveRepoRoot() (string, error) {
	if found, err := findRepoRoot(mustGetwd()); err != nil {
		return "", err
	} else if strings.TrimSpace(found) != "" {
		return found, nil
	}
	return filepath.Abs(mustGetwd())
}

func docsResolveSiteRoot(args Args) (string, error) {
	if explicit := strings.TrimSpace(args.String("site")); explicit != "" {
		return filepath.Abs(explicit)
	}
	return filepath.Abs(filepath.Join(mustGetwd(), "site"))
}

func docsResolveOptionalVault(args Args) (string, error) {
	if explicit := strings.TrimSpace(args.String("vault")); explicit != "" {
		return filepath.Abs(explicit)
	}
	return discoverVault(mustGetwd())
}

func runAstroCommand(siteRoot string, astroArgs ...string) error {
	binary := filepath.Join(siteRoot, "node_modules", ".bin", "astro")
	if !fileExists(binary) {
		return tuskerError(errorNotFound, "Astro binary not found under site/node_modules/.bin/astro", withPath(binary))
	}
	cmd := exec.Command(binary, astroArgs...)
	cmd.Dir = siteRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

type docsWatchedFile struct {
	ModTime int64
	Size    int64
}

func docsWatchLoop(options docsExportOptions, interval time.Duration, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	last, err := docsWatchSnapshot(options)
	if err != nil {
		fmt.Fprintln(os.Stderr, "docs watch snapshot failed:", err)
		last = map[string]docsWatchedFile{}
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			current, err := docsWatchSnapshot(options)
			if err != nil {
				fmt.Fprintln(os.Stderr, "docs watch snapshot failed:", err)
				continue
			}
			if docsWatchSnapshotsEqual(last, current) {
				continue
			}
			last = current
			if strings.TrimSpace(options.VaultRoot) != "" {
				if err := reindex(Args{"vault": options.VaultRoot, "quiet": "true"}); err != nil {
					fmt.Fprintln(os.Stderr, "docs watch reindex failed:", err)
					continue
				}
			}
			summary, err := runDocsExport(options)
			if err != nil {
				fmt.Fprintln(os.Stderr, "docs watch export failed:", err)
				continue
			}
			fmt.Fprintf(os.Stderr, "docs watch exported %d doc%s, copied %d asset%s.\n", summary.ExportedDocs, plural(summary.ExportedDocs), summary.ExportedAssets, plural(summary.ExportedAssets))
			if next, err := docsWatchSnapshot(options); err == nil {
				last = next
			}
		}
	}
}

func docsWatchSnapshot(options docsExportOptions) (map[string]docsWatchedFile, error) {
	out := map[string]docsWatchedFile{}
	addFile := func(path string) error {
		path = strings.TrimSpace(path)
		if path == "" {
			return nil
		}
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if info.IsDir() {
			return nil
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		out[abs] = docsWatchedFile{ModTime: info.ModTime().UnixNano(), Size: info.Size()}
		return nil
	}
	addTree := func(root string, include func(string, os.DirEntry) bool) error {
		root = strings.TrimSpace(root)
		if root == "" || !fileExists(root) {
			return nil
		}
		return filepath.WalkDir(root, func(current string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if docsWatchSkipDir(root, current) {
					return filepath.SkipDir
				}
				return nil
			}
			if include != nil && !include(current, entry) {
				return nil
			}
			return addFile(current)
		})
	}
	if err := addTree(options.VaultRoot, docsWatchIncludeVaultFile); err != nil {
		return nil, err
	}
	if strings.TrimSpace(options.RepoRoot) != "" {
		if err := addFile(filepath.Join(options.RepoRoot, docsRegistryRelative)); err != nil {
			return nil, err
		}
		for _, rel := range []string{"README.md", "docs", "skill"} {
			if err := addTree(filepath.Join(options.RepoRoot, rel), docsWatchIncludeRepoFile); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

func docsWatchSkipDir(root, current string) bool {
	rel := docsNormalizePath(docsRelativeTo(root, current))
	switch rel {
	case ".", "":
		return false
	case "_system/generated", "_system/logs", "_system/workspaces", ".git", "node_modules", "dist":
		return true
	default:
		return strings.HasPrefix(rel, "_system/generated/") ||
			strings.HasPrefix(rel, "_system/logs/") ||
			strings.HasPrefix(rel, "_system/workspaces/") ||
			strings.Contains(rel, "/node_modules/") ||
			strings.Contains(rel, "/dist/")
	}
}

func docsWatchIncludeVaultFile(path string, entry os.DirEntry) bool {
	name := strings.ToLower(entry.Name())
	return strings.HasSuffix(name, ".md") ||
		strings.HasSuffix(name, ".mdx") ||
		strings.HasSuffix(name, ".png") ||
		strings.HasSuffix(name, ".jpg") ||
		strings.HasSuffix(name, ".jpeg") ||
		strings.HasSuffix(name, ".gif") ||
		strings.HasSuffix(name, ".svg") ||
		strings.HasSuffix(name, ".webp") ||
		strings.HasSuffix(name, ".mp4") ||
		strings.HasSuffix(name, ".mov")
}

func docsWatchIncludeRepoFile(path string, entry os.DirEntry) bool {
	name := strings.ToLower(entry.Name())
	return strings.HasSuffix(name, ".md") ||
		strings.HasSuffix(name, ".mdx") ||
		strings.HasSuffix(name, ".png") ||
		strings.HasSuffix(name, ".jpg") ||
		strings.HasSuffix(name, ".jpeg") ||
		strings.HasSuffix(name, ".gif") ||
		strings.HasSuffix(name, ".svg") ||
		strings.HasSuffix(name, ".webp") ||
		strings.HasSuffix(name, ".mp4") ||
		strings.HasSuffix(name, ".mov") ||
		strings.HasSuffix(name, ".yaml") ||
		strings.HasSuffix(name, ".yml")
}

func docsWatchSnapshotsEqual(left, right map[string]docsWatchedFile) bool {
	if len(left) != len(right) {
		return false
	}
	for path, leftFile := range left {
		rightFile, ok := right[path]
		if !ok || leftFile != rightFile {
			return false
		}
	}
	return true
}
