package main

import (
	"os"
	"path/filepath"
	"strings"
)

type docsAssetExporter struct {
	SiteRoot string
	RepoRoot string
	files    map[string]string
}

func newDocsAssetExporter(siteRoot, repoRoot string) *docsAssetExporter {
	return &docsAssetExporter{
		SiteRoot: siteRoot,
		RepoRoot: repoRoot,
		files:    map[string]string{},
	}
}

func (e *docsAssetExporter) AssetFiles() []string {
	out := make([]string, 0, len(e.files))
	for _, rel := range e.files {
		out = append(out, rel)
	}
	return docsUniqueStrings(out)
}

func (e *docsAssetExporter) RewriteAssetPath(source docsSourceDocument, rawPath string) (string, error) {
	href, suffix, anchor := docsSplitHref(rawPath)
	if href == "" || docsIsExternalHref(href) || strings.HasPrefix(href, "/") {
		return rawPath, nil
	}
	assetAbs, err := docsResolveLocalPath(source, e.RepoRoot, href)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(assetAbs)
	if err != nil {
		return "", tuskerError(errorNotFound, "Referenced asset not found: "+href, withPath(source.SourcePath))
	}
	if info.IsDir() {
		return "", tuskerError(errorInvalidField, "Referenced asset is a directory: "+href, withPath(source.SourcePath))
	}
	relTarget, publicHref := docsAssetDestination(source, assetAbs)
	targetAbs := filepath.Join(e.SiteRoot, filepath.FromSlash(relTarget))
	if _, ok := e.files[assetAbs]; ok {
		return docsRebuildHref(publicHref, suffix, anchor), nil
	}
	if err := copyFile(assetAbs, targetAbs); err != nil {
		return "", err
	}
	e.files[assetAbs] = relTarget
	return docsRebuildHref(publicHref, suffix, anchor), nil
}

func docsAssetDestination(source docsSourceDocument, assetAbs string) (string, string) {
	kind := docsSourceLabel(source.SourceKind)
	sourceDir := filepath.Dir(source.SourceAbsPath)
	relTail := docsRelativeTo(sourceDir, assetAbs)
	owner := source.AssetOwnerSlug()
	relTarget := docsNormalizePath(filepath.Join(docsAssetsRootRelative, kind, owner, filepath.FromSlash(relTail)))
	publicHref := "/" + docsNormalizePath(filepath.Join("generated", "assets", kind, owner, filepath.FromSlash(relTail)))
	return relTarget, publicHref
}

func docsResolveLocalPath(source docsSourceDocument, repoRoot, href string) (string, error) {
	href = strings.TrimSpace(href)
	if href == "" {
		return "", tuskerError(errorInvalidField, "empty asset reference", withPath(source.SourcePath))
	}
	candidates := docsResolveCandidatePaths(source, repoRoot, href)
	for _, candidate := range candidates {
		if fileExists(candidate) {
			return candidate, nil
		}
	}
	return "", tuskerError(errorNotFound, "Unable to resolve local path: "+href, withPath(source.SourcePath))
}
