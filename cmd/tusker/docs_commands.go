package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
