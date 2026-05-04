package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func validateDocsPublicationState(vaultPath string, notes []Note) ([]Issue, []Issue) {
	repoRoot, err := docsResolveRepoRoot()
	if err != nil || strings.TrimSpace(repoRoot) == "" {
		return nil, nil
	}
	if !docsValidationVaultBelongsToRepo(repoRoot, vaultPath) {
		return nil, nil
	}
	siteRoot := filepath.Join(repoRoot, "site")
	publicPath := filepath.Join(siteRoot, docsPublicCanonRelative)
	generatedPath := filepath.Join(siteRoot, docsCanonManifestRelative)
	if !fileExists(publicPath) && !fileExists(generatedPath) {
		return nil, nil
	}

	var errs, warns []Issue
	if fileExists(publicPath) && fileExists(generatedPath) {
		publicRaw, publicErr := os.ReadFile(publicPath)
		generatedRaw, generatedErr := os.ReadFile(generatedPath)
		if publicErr == nil && generatedErr == nil && !bytes.Equal(publicRaw, generatedRaw) {
			errs = append(errs, issue(errorDocsManifestMismatch, "public and generated canon manifests differ", docsPublicCanonRelative, "run `tusker docs export` so both locations come from the same compiler pass", map[string]any{
				"public":    docsPublicCanonRelative,
				"generated": docsCanonManifestRelative,
			}))
		}
	}

	manifestPath := publicPath
	manifestRel := docsPublicCanonRelative
	if !fileExists(manifestPath) {
		manifestPath = generatedPath
		manifestRel = docsCanonManifestRelative
	}
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		errs = append(errs, issue(errorDocsManifestInvalid, fmt.Sprintf("cannot read canon manifest: %v", err), manifestRel, "", nil))
		return errs, warns
	}
	var manifest docsCanonManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		errs = append(errs, issue(errorDocsManifestInvalid, fmt.Sprintf("cannot parse canon manifest: %v", err), manifestRel, "", nil))
		return errs, warns
	}
	if manifest.SchemaVersion != docsManifestSchemaVersion {
		errs = append(errs, issue(errorDocsManifestInvalid, fmt.Sprintf("canon manifest schemaVersion is %d; expected %d", manifest.SchemaVersion, docsManifestSchemaVersion), manifestRel, "run `tusker docs export` with the current CLI", nil))
	}
	for _, doc := range manifest.Published {
		sourceAbs := docsManifestSourceAbsPath(repoRoot, vaultPath, doc)
		if strings.TrimSpace(sourceAbs) == "" {
			continue
		}
		if !fileExists(sourceAbs) {
			errs = append(errs, issue(errorDocsSourceMissing, fmt.Sprintf(`manifest source "%s" no longer exists`, doc.SourcePath), doc.SourcePath, "restore the source or run `tusker docs export` after removing/redirecting the route", map[string]any{
				"route":       doc.Route,
				"source_kind": doc.SourceKind,
			}))
		}
	}
	generatedAt, err := time.Parse(time.RFC3339, manifest.GeneratedAt)
	if err != nil {
		errs = append(errs, issue(errorDocsManifestInvalid, fmt.Sprintf("canon manifest generatedAt is invalid: %v", err), manifestRel, "run `tusker docs export`", nil))
		return errs, warns
	}

	checkFresh := func(absPath, relPath string) {
		if strings.TrimSpace(absPath) == "" {
			return
		}
		info, err := os.Stat(absPath)
		if err != nil {
			return
		}
		if info.ModTime().UTC().After(generatedAt.Add(time.Second)) {
			errs = append(errs, issue(errorDocsManifestStale, fmt.Sprintf(`published source "%s" is newer than canon-manifest.json`, relPath), relPath, "run `tusker docs export` before relying on the manifest", map[string]any{
				"generatedAt": manifest.GeneratedAt,
				"modifiedAt":  info.ModTime().UTC().Format(time.RFC3339),
			}))
		}
	}

	for _, note := range notes {
		if stringField(note.Data, "type") == "doc" && boolField(note.Data, "publish") {
			checkFresh(note.AbsolutePath, note.RelativePath)
		}
	}
	registryPath := filepath.Join(repoRoot, docsRegistryRelative)
	if fileExists(registryPath) {
		checkFresh(registryPath, docsRegistryRelative)
		registrySources, err := loadDocsRegistry(repoRoot)
		if err != nil {
			errs = append(errs, errorToIssue(err))
		} else {
			for _, source := range registrySources {
				checkFresh(source.SourceAbsPath, source.SourcePath)
			}
		}
	}
	routesRemovedPath := filepath.Join(siteRoot, docsRoutesRemovedRelative)
	if fileExists(routesRemovedPath) {
		raw, err := os.ReadFile(routesRemovedPath)
		if err != nil {
			errs = append(errs, issue(errorDocsManifestInvalid, fmt.Sprintf("cannot read removed-routes report: %v", err), docsRoutesRemovedRelative, "", nil))
		} else {
			var report docsRemovedRoutesReport
			if err := json.Unmarshal(raw, &report); err != nil {
				errs = append(errs, issue(errorDocsManifestInvalid, fmt.Sprintf("cannot parse removed-routes report: %v", err), docsRoutesRemovedRelative, "", nil))
			} else {
				if report.SchemaVersion != docsManifestSchemaVersion {
					errs = append(errs, issue(errorDocsManifestInvalid, fmt.Sprintf("removed-routes schemaVersion is %d; expected %d", report.SchemaVersion, docsManifestSchemaVersion), docsRoutesRemovedRelative, "run `tusker docs export` with the current CLI", nil))
				}
				for _, removed := range report.Removed {
					errs = append(errs, issue(errorDocsRouteRemoved, fmt.Sprintf(`published route "%s" disappeared without redirect_from`, firstNonEmpty(removed.RouteURL, docsRouteURL(removed.Route))), docsRoutesRemovedRelative, "restore the route or add redirect_from on the replacement page, then run `tusker docs export`", map[string]any{
						"route":       removed.Route,
						"source_path": removed.SourcePath,
						"title":       removed.Title,
					}))
				}
			}
		}
	}

	canonByEpic := map[string]struct{}{}
	for _, doc := range manifest.Canon {
		for _, key := range []string{doc.OwnerEpic, doc.CanonFor, doc.Topic} {
			key = strings.TrimSpace(key)
			if key != "" {
				canonByEpic[key] = struct{}{}
			}
		}
	}
	for _, note := range notes {
		if stringField(note.Data, "type") != "epic" || stringField(note.Data, "status") != "active" {
			continue
		}
		epicID := stringField(note.Data, "id")
		if _, ok := canonByEpic[epicID]; !ok {
			errs = append(errs, issue(errorDocsCanonMissing, fmt.Sprintf(`active epic "%s" has no published canon entry in canon-manifest.json`, epicID), note.RelativePath, "publish a canonical doc with owner_epic set, then run `tusker docs export`", map[string]any{"epic": epicID}))
		}
	}

	return errs, warns
}

func docsManifestSourceAbsPath(repoRoot, vaultPath string, doc docsCanonManifestDoc) string {
	sourcePath := docsNormalizePath(doc.SourcePath)
	if sourcePath == "" {
		return ""
	}
	switch doc.SourceKind {
	case string(docsSourceKindVault):
		return filepath.Join(vaultPath, filepath.FromSlash(sourcePath))
	case string(docsSourceKindRepo):
		return filepath.Join(repoRoot, filepath.FromSlash(sourcePath))
	default:
		return ""
	}
}

func docsValidationVaultBelongsToRepo(repoRoot, vaultPath string) bool {
	repoAbs, err := filepath.Abs(repoRoot)
	if err != nil {
		return false
	}
	vaultAbs, err := filepath.Abs(vaultPath)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(repoAbs, vaultAbs)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..")
}
