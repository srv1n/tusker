package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type docsExportOptions struct {
	RepoRoot   string
	SiteRoot   string
	VaultRoot  string
	Clean      bool
	PublicOnly bool
}

func runDocsExport(options docsExportOptions) (docsExportSummary, error) {
	summary := docsExportSummary{
		GeneratedAt: todayTimestamp(),
		PublicOnly:  options.PublicOnly,
	}
	if err := ensureDir(filepath.Join(options.SiteRoot, docsContentRootRelative)); err != nil {
		return summary, err
	}
	if err := ensureDir(filepath.Join(options.SiteRoot, docsGeneratedRootRelative)); err != nil {
		return summary, err
	}
	if err := ensureDir(filepath.Join(options.SiteRoot, docsAssetsRootRelative)); err != nil {
		return summary, err
	}
	previousState, _ := loadDocsExportState(options.SiteRoot)
	if options.Clean {
		for _, rel := range append(append([]string{}, previousState.ContentFiles...), previousState.AssetFiles...) {
			_ = os.Remove(filepath.Join(options.SiteRoot, filepath.FromSlash(rel)))
		}
		previousState = docsExportState{}
	}
	sources, err := loadDocsSources(options)
	if err != nil {
		return summary, err
	}
	routeTable, err := buildDocsRouteTable(sources)
	if err != nil {
		return summary, err
	}
	if err := assertDocsManualRouteSafety(options.SiteRoot, previousState, sources); err != nil {
		return summary, err
	}
	assetExporter := newDocsAssetExporter(options.SiteRoot, options.RepoRoot)
	generatedAt := summary.GeneratedAt
	var report docsExportReport
	report.Summary.GeneratedAt = generatedAt
	report.Summary.PublicOnly = options.PublicOnly
	newState := docsExportState{GeneratedAt: generatedAt}
	for _, source := range sources {
		rewritten, err := rewriteDocsBody(source.Body, docsRewriteContext{
			RepoRoot:   options.RepoRoot,
			VaultRoot:  options.VaultRoot,
			Source:     source,
			RouteTable: routeTable,
			Assets:     assetExporter,
		})
		if err != nil {
			return summary, err
		}
		content, err := renderDocsExportedPage(source, rewritten)
		if err != nil {
			return summary, err
		}
		targetAbs := filepath.Join(options.SiteRoot, filepath.FromSlash(source.OutputRelPath))
		wrote, err := writeTextIfChanged(targetAbs, content)
		if err != nil {
			return summary, err
		}
		if wrote {
			summary.ExportedDocs++
		} else {
			summary.SkippedDocs++
		}
		newState.ContentFiles = append(newState.ContentFiles, docsNormalizePath(source.OutputRelPath))
		stateRoute := docsExportStateRoute{
			Title:      source.Title,
			SourceKind: string(source.SourceKind),
			SourceID:   source.SourceID,
			SourcePath: source.SourcePath,
			Route:      docsNormalizeRouteValue(source.RoutePath),
			RouteURL:   source.RouteURL,
			OutputPath: docsNormalizePath(source.OutputRelPath),
		}
		newState.Routes = append(newState.Routes, stateRoute)
		report.Routes = append(report.Routes, docsExportReportRoute{
			Title:      stateRoute.Title,
			SourceKind: stateRoute.SourceKind,
			SourceID:   stateRoute.SourceID,
			SourcePath: stateRoute.SourcePath,
			Route:      stateRoute.RouteURL,
			OutputPath: stateRoute.OutputPath,
		})
		for _, alternative := range docsAlternativeOutputPaths(source.RoutePath) {
			if alternative == docsNormalizePath(source.OutputRelPath) {
				continue
			}
			if fileExists(filepath.Join(options.SiteRoot, filepath.FromSlash(alternative))) && docsFileLooksGenerated(filepath.Join(options.SiteRoot, filepath.FromSlash(alternative))) {
				_ = os.Remove(filepath.Join(options.SiteRoot, filepath.FromSlash(alternative)))
			}
		}
	}
	newState.AssetFiles = docsUniqueStrings(assetExporter.AssetFiles())
	summary.ExportedAssets = len(newState.AssetFiles)
	for _, stale := range docsComputeStaleFiles(previousState.ContentFiles, newState.ContentFiles) {
		if err := os.Remove(filepath.Join(options.SiteRoot, filepath.FromSlash(stale))); err == nil {
			summary.DeletedDocs++
		}
	}
	for _, stale := range docsComputeStaleFiles(previousState.AssetFiles, newState.AssetFiles) {
		if err := os.Remove(filepath.Join(options.SiteRoot, filepath.FromSlash(stale))); err == nil {
			summary.DeletedAssets++
		}
	}
	legacyStale, err := docsFindLegacyGeneratedFiles(options.SiteRoot, newState.ContentFiles)
	if err != nil {
		return summary, err
	}
	for _, stale := range legacyStale {
		if err := os.Remove(filepath.Join(options.SiteRoot, filepath.FromSlash(stale))); err == nil {
			summary.DeletedDocs++
		}
	}
	removedRoutes := buildDocsRemovedRoutesReport(generatedAt, previousState, newState, sources)
	navigation := buildDocsNavigation(sources)
	contentManifest := buildDocsContentManifest(generatedAt, sources)
	canonManifest := buildDocsCanonManifest(generatedAt, sources)
	report.Summary = summary
	if err := writeJSON(filepath.Join(options.SiteRoot, docsNavigationRelative), navigation); err != nil {
		return summary, err
	}
	if err := writeJSON(filepath.Join(options.SiteRoot, docsContentManifestRelative), contentManifest); err != nil {
		return summary, err
	}
	if err := writeJSON(filepath.Join(options.SiteRoot, docsCanonManifestRelative), canonManifest); err != nil {
		return summary, err
	}
	if err := writeJSON(filepath.Join(options.SiteRoot, docsPublicCanonRelative), canonManifest); err != nil {
		return summary, err
	}
	if err := writeJSON(filepath.Join(options.SiteRoot, docsRoutesRemovedRelative), removedRoutes); err != nil {
		return summary, err
	}
	if err := writeJSON(filepath.Join(options.SiteRoot, docsExportReportRelative), report); err != nil {
		return summary, err
	}
	if err := writeJSON(filepath.Join(options.SiteRoot, docsExportStateRelative), newState); err != nil {
		return summary, err
	}
	if err := writeText(filepath.Join(options.SiteRoot, docsLLMSTxtRelative), renderLLMSText(sources, false)); err != nil {
		return summary, err
	}
	if err := writeText(filepath.Join(options.SiteRoot, docsLLMSFullTxtRelative), renderLLMSText(sources, true)); err != nil {
		return summary, err
	}
	return summary, nil
}

func loadDocsSources(options docsExportOptions) ([]docsSourceDocument, error) {
	vaultSources, err := loadVaultDocsPublicationSources(options.VaultRoot, options.PublicOnly)
	if err != nil {
		return nil, err
	}
	repoSources, err := loadRepoDocsPublicationSources(options.RepoRoot, options.PublicOnly)
	if err != nil {
		return nil, err
	}
	sources := append(vaultSources, repoSources...)
	if docsMap, err := loadDocsMap(options.VaultRoot); err == nil && docsMap != nil {
		applyDocsMapMetadata(sources, docsMap)
	}
	indexRoutes := docsRoutesNeedingIndexPaths(sources)
	for i := range sources {
		sources[i].RouteURL = docsRouteURL(sources[i].RoutePath)
		if strings.TrimSpace(sources[i].OutputExt) == "" {
			sources[i].OutputExt = docsInferOutputExt(sources[i].SourceAbsPath, sources[i].Body)
		}
		sources[i].OutputRelPath = docsNormalizePath(docsOutputPathForRoute(sources[i].RoutePath, sources[i].OutputExt, indexRoutes[sources[i].RoutePath]))
	}
	docsSortSources(sources)
	return sources, nil
}

func applyDocsMapMetadata(sources []docsSourceDocument, docsMap *DocsMap) {
	byID := map[string]DocsMapNode{}
	byPath := map[string]DocsMapNode{}
	order := map[string]int{}
	for i, node := range docsMap.Nodes {
		byID[node.ID] = node
		if path := docsNormalizePath(node.SourcePath()); path != "" {
			byPath[path] = node
		}
		order[node.ID] = i + 1
	}
	for i := range sources {
		source := &sources[i]
		node, ok := byID[source.SourceID]
		if !ok {
			node, ok = byPath[docsNormalizePath(source.SourcePath)]
		}
		if !ok {
			continue
		}
		if source.SourceID == "" {
			source.SourceID = node.ID
		}
		source.Mode = node.EffectiveMode()
		source.AgentLayer = node.EffectiveAgentLayer()
		source.SourceOfTruth = append([]string{}, node.SourceOfTruth...)
		source.StaleWhenPaths = append([]string{}, node.StaleWhen.Paths...)
		source.DocsMapOrder = order[node.ID]
		source.Audience = firstNonEmpty(source.Audience, node.Audience)
		source.Description = firstNonEmpty(source.Description, node.PublishDescription)
	}
}

func loadVaultDocsPublicationSources(vaultRoot string, publicOnly bool) ([]docsSourceDocument, error) {
	vaultRoot = strings.TrimSpace(vaultRoot)
	if vaultRoot == "" {
		return nil, nil
	}
	indexPath := filepath.Join(vaultRoot, docsPublicationIndexRelative)
	if !fileExists(indexPath) {
		return nil, tuskerError(errorNotFound, "Publication index not found. Run `tusker reindex` first.", withPath(indexPath))
	}
	raw, err := os.ReadFile(indexPath)
	if err != nil {
		return nil, err
	}
	var payload struct {
		GeneratedAt string           `json:"generatedAt"`
		Items       []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	var out []docsSourceDocument
	for _, item := range payload.Items {
		if !boolValue(item["publish"]) {
			continue
		}
		relPath := docsNormalizePath(stringValue(item["path"]))
		if relPath == "" {
			return nil, tuskerError(errorInvalidField, "Published vault doc is missing path in publication index", withPath(indexPath))
		}
		sourceAbs := filepath.Join(vaultRoot, filepath.FromSlash(relPath))
		data, body, err := parseFrontmatterMustRead(sourceAbs)
		if err != nil {
			return nil, err
		}
		route := docsRouteFromPublishPath(
			firstNonEmpty(stringField(data, "publish_path"), stringValue(item["publish_path"])),
			firstNonEmpty(stringField(data, "publish_lane"), stringValue(item["publish_lane"])),
		)
		if err := validateDocsRoutePath(route); err != nil {
			return nil, tuskerError(errorInvalidField, "Published vault doc has invalid publish_path: "+route, withPath(relPath))
		}
		description := firstNonEmpty(stringField(data, "publish_description"), stringValue(item["publish_description"]))
		if strings.TrimSpace(description) == "" {
			return nil, tuskerError(errorInvalidField, "Published vault doc is missing publish_description", withPath(relPath))
		}
		source := docsSourceDocument{
			SourceKind:      docsSourceKindVault,
			SourceID:        firstNonEmpty(stringField(data, "node"), stringField(data, "id"), stringValue(item["id"])),
			Title:           firstNonEmpty(stringField(data, "title"), stringValue(item["title"]), docsFirstHeading(body), docsTitleizeSegment(filepath.Base(relPath))),
			Description:     description,
			Audience:        firstNonEmpty(stringField(data, "audience"), stringValue(item["audience"])),
			Mode:            firstNonEmpty(stringField(data, "mode"), stringValue(item["mode"])),
			AgentLayer:      firstNonEmpty(stringField(data, "agent_layer"), stringValue(item["agent_layer"])),
			SourceOfTruth:   firstNonEmptyList(normalizeList(data["source_of_truth"]), normalizeList(item["source_of_truth"])),
			StaleWhenPaths:  firstNonEmptyList(normalizeList(data["stale_when_paths"]), normalizeList(item["stale_when_paths"])),
			DocIntent:       firstNonEmpty(stringField(data, "doc_intent"), stringValue(item["doc_intent"])),
			Epic:            wikiTarget(firstNonEmpty(stringField(data, "epic"), stringValue(item["epic"]))),
			OwnerEpic:       wikiTarget(firstNonEmpty(stringField(data, "owner_epic"), stringValue(item["owner_epic"]))),
			Task:            wikiTarget(firstNonEmpty(stringField(data, "task"), stringValue(item["task"]))),
			CanonFor:        wikiTarget(firstNonEmpty(stringField(data, "canon_for"), stringValue(item["canon_for"]))),
			Canonical:       boolField(data, "canonical") || stringField(data, "kind") == "canon" || boolValue(item["canonical"]),
			CanonicalStatus: firstNonEmpty(stringField(data, "canonical_status"), stringValue(item["canonical_status"])),
			VerifiedAt:      firstNonEmpty(stringField(data, "last_verified_at"), stringField(data, "verified_at"), stringValue(item["verified_at"])),
			Deprecated:      boolField(data, "deprecated") || boolValue(item["deprecated"]),
			SupersededBy:    firstNonEmpty(stringField(data, "superseded_by"), stringValue(item["superseded_by"])),
			RedirectFrom:    docsNormalizeRouteList(normalizeList(data["redirect_from"]), normalizeList(item["redirect_from"])),
			SourcePath:      relPath,
			SourceAbsPath:   sourceAbs,
			Body:            body,
			Tags:            docsMergeTags(normalizeList(data["tags"]), normalizeList(item["tags"])),
			Updated:         firstNonEmpty(stringField(data, "updated"), stringValue(item["updated"]), docsFileUpdatedDate(sourceAbs)),
			RoutePath:       route,
			SectionTitle:    firstNonEmpty(stringField(data, "publish_section_title"), stringValue(item["publish_section_title"])),
			Order:           docsOptionalInt(data["publish_order"], item["publish_order"]),
			Internal:        strings.HasPrefix(route, "internal/") || strings.EqualFold(firstNonEmpty(stringField(data, "audience"), stringValue(item["audience"])), "internal"),
			OutputExt:       docsInferOutputExt(sourceAbs, body),
		}
		if source.OwnerEpic == "" {
			source.OwnerEpic = firstNonEmpty(source.CanonFor, source.Epic)
		}
		applyDocsCanonicalDefaults(&source, firstNonEmpty(stringField(data, "status"), stringField(data, "canonical_status")))
		if err := validateDocsCanonicalLifecycle(source); err != nil {
			return nil, err
		}
		if publicOnly && source.Internal {
			continue
		}
		out = append(out, source)
	}
	return out, nil
}

func docsRouteFromPublishPath(route, lane string) string {
	route = strings.Trim(strings.TrimSpace(route), "/")
	if route == "" {
		return ""
	}
	first := strings.Split(route, "/")[0]
	if _, ok := docsLaneLabels[first]; ok {
		return route
	}
	lane = strings.Trim(strings.TrimSpace(lane), "/")
	if _, ok := docsLaneLabels[lane]; ok {
		return lane + "/" + route
	}
	return route
}

func loadRepoDocsPublicationSources(repoRoot string, publicOnly bool) ([]docsSourceDocument, error) {
	entries, err := loadDocsRegistry(repoRoot)
	if err != nil {
		return nil, err
	}
	var out []docsSourceDocument
	for _, entry := range entries {
		data, body, err := parseFrontmatterMustRead(entry.SourceAbsPath)
		if err != nil {
			return nil, err
		}
		route := strings.TrimSpace(entry.RoutePath)
		if err := validateDocsRoutePath(route); err != nil {
			return nil, tuskerError(errorInvalidField, "Registry route is invalid: "+route, withPath(docsRegistryRelative))
		}
		internal := entry.Entry.Internal || strings.HasPrefix(route, "internal/") || strings.EqualFold(entry.Entry.Audience, "internal")
		if publicOnly && internal {
			continue
		}
		canonical := entry.Entry.Canonical
		if entry.FromDirectory {
			if _, ok := data["canonical"]; ok {
				canonical = boolField(data, "canonical")
			}
		} else if !canonical && boolField(data, "canonical") {
			canonical = true
		}
		canonicalStatus := docsRegistryString(entry.Entry.CanonicalStatus, stringField(data, "canonical_status"), entry.FromDirectory)
		ownerEpic := docsRegistryString(entry.Entry.OwnerEpic, stringField(data, "owner_epic"), entry.FromDirectory)
		verifiedAt := docsRegistryString(entry.Entry.VerifiedAt, stringField(data, "verified_at"), entry.FromDirectory)
		deprecated := entry.Entry.Deprecated
		if entry.FromDirectory {
			if _, ok := data["deprecated"]; ok {
				deprecated = boolField(data, "deprecated")
			}
		} else if !deprecated && boolField(data, "deprecated") {
			deprecated = true
		}
		supersededBy := docsRegistryString(entry.Entry.SupersededBy, stringField(data, "superseded_by"), entry.FromDirectory)
		redirectFrom := docsNormalizeRouteList(entry.Entry.RedirectFrom, normalizeList(data["redirect_from"]))
		explicitCanonical := canonical
		explicitCanonicalStatus := canonicalStatus
		if explicitCanonical && strings.TrimSpace(explicitCanonicalStatus) == "" {
			return nil, tuskerError(errorInvalidField, "Canonical registry docs must set canonical_status", withPath(entry.SourcePath))
		}
		description := firstNonEmpty(entry.Entry.Description, stringField(data, "description"), docsFirstParagraph(body), "Published from "+entry.SourcePath+".")
		source := docsSourceDocument{
			SourceKind:      docsSourceKindRepo,
			Title:           firstNonEmpty(entry.Entry.Title, stringField(data, "title"), stringField(data, "name"), docsFirstHeading(body), docsTitleizeSegment(filepath.Base(entry.SourceAbsPath))),
			Description:     description,
			Audience:        firstNonEmpty(entry.Entry.Audience, stringField(data, "audience"), docsAudienceFromRoute(route)),
			Mode:            stringField(data, "mode"),
			AgentLayer:      stringField(data, "agent_layer"),
			SourceOfTruth:   normalizeList(data["source_of_truth"]),
			StaleWhenPaths:  normalizeList(data["stale_when_paths"]),
			DocIntent:       stringField(data, "doc_intent"),
			Epic:            wikiTarget(stringField(data, "epic")),
			OwnerEpic:       wikiTarget(ownerEpic),
			Task:            wikiTarget(stringField(data, "task")),
			CanonFor:        wikiTarget(stringField(data, "canon_for")),
			Canonical:       canonical,
			CanonicalStatus: canonicalStatus,
			VerifiedAt:      verifiedAt,
			Deprecated:      deprecated,
			SupersededBy:    supersededBy,
			RedirectFrom:    redirectFrom,
			SourcePath:      entry.SourcePath,
			SourceAbsPath:   entry.SourceAbsPath,
			Body:            body,
			Tags:            docsMergeTags(entry.Entry.Tags, normalizeList(data["tags"])),
			Updated:         firstNonEmpty(stringField(data, "updated"), docsFileUpdatedDate(entry.SourceAbsPath)),
			RoutePath:       route,
			SectionTitle:    strings.TrimSpace(entry.Entry.SectionTitle),
			Internal:        internal,
			OutputExt:       docsInferOutputExt(entry.SourceAbsPath, body),
		}
		applyDocsCanonicalDefaults(&source, stringField(data, "status"))
		if err := validateDocsCanonicalLifecycle(source); err != nil {
			return nil, err
		}
		out = append(out, source)
	}
	return out, nil
}

func renderDocsExportedPage(source docsSourceDocument, body string) (string, error) {
	data := map[string]any{
		"title":       source.Title,
		"description": source.Description,
	}
	if source.Order != nil {
		data["sidebar"] = map[string]any{"order": *source.Order}
	}
	tusker := map[string]any{
		"source_kind": string(source.SourceKind),
		"audience":    source.Audience,
		"source_path": source.SourcePath,
		"route":       source.RouteURL,
		"updated":     source.Updated,
		"summary":     source.Description,
		"tags":        append([]string{}, source.Tags...),
	}
	if source.SourceID != "" {
		tusker["id"] = source.SourceID
	}
	if source.DocIntent != "" {
		tusker["doc_intent"] = source.DocIntent
	}
	if source.Mode != "" {
		tusker["mode"] = source.Mode
	}
	if source.AgentLayer != "" {
		tusker["agent_layer"] = source.AgentLayer
	}
	if len(source.SourceOfTruth) > 0 {
		tusker["source_of_truth"] = append([]string{}, source.SourceOfTruth...)
	}
	if len(source.StaleWhenPaths) > 0 {
		tusker["stale_when_paths"] = append([]string{}, source.StaleWhenPaths...)
	}
	if source.Epic != "" {
		tusker["epic"] = source.Epic
	}
	if source.Task != "" {
		tusker["task"] = source.Task
	}
	if source.CanonFor != "" {
		tusker["canon_for"] = source.CanonFor
	}
	if source.OwnerEpic != "" {
		tusker["owner_epic"] = source.OwnerEpic
	}
	if source.Canonical {
		tusker["canonical"] = true
	}
	if source.CanonicalStatus != "" {
		tusker["canonical_status"] = source.CanonicalStatus
	}
	if source.VerifiedAt != "" {
		tusker["verified_at"] = source.VerifiedAt
	}
	if source.Deprecated {
		tusker["deprecated"] = true
	}
	if source.SupersededBy != "" {
		tusker["superseded_by"] = source.SupersededBy
	}
	if len(source.RedirectFrom) > 0 {
		tusker["redirect_from"] = append([]string{}, source.RedirectFrom...)
	}
	if strings.TrimSpace(source.RoutePath) != "" {
		tusker["publish_path"] = source.RoutePath
	}
	if strings.TrimSpace(source.SectionTitle) != "" {
		tusker["publish_section_title"] = source.SectionTitle
	}
	if source.Order != nil {
		tusker["publish_order"] = *source.Order
	}
	data["tusker"] = tusker
	fm, err := stringifyFrontmatter(data, []string{"title", "description", "sidebar", "tusker"})
	if err != nil {
		return "", err
	}
	trimmed := strings.TrimLeft(body, "\n")
	trimmed = strings.TrimRight(trimmed, "\n")
	if trimmed == "" {
		return fm + "\n", nil
	}
	return fm + "\n" + trimmed + "\n", nil
}

func writeTextIfChanged(filePath, content string) (bool, error) {
	existing, err := os.ReadFile(filePath)
	if err == nil && string(existing) == content {
		return false, nil
	}
	if err := writeText(filePath, content); err != nil {
		return false, err
	}
	return true, nil
}

func loadDocsExportState(siteRoot string) (docsExportState, error) {
	statePath := filepath.Join(siteRoot, docsExportStateRelative)
	if !fileExists(statePath) {
		return docsExportState{}, nil
	}
	raw, err := os.ReadFile(statePath)
	if err != nil {
		return docsExportState{}, err
	}
	var state docsExportState
	if err := json.Unmarshal(raw, &state); err != nil {
		return docsExportState{}, err
	}
	state.ContentFiles = docsUniqueStrings(state.ContentFiles)
	state.AssetFiles = docsUniqueStrings(state.AssetFiles)
	state.Routes = docsUniqueStateRoutes(state.Routes)
	return state, nil
}

func assertDocsManualRouteSafety(siteRoot string, previous docsExportState, sources []docsSourceDocument) error {
	owned := map[string]struct{}{}
	for _, rel := range previous.ContentFiles {
		owned[docsNormalizePath(rel)] = struct{}{}
	}
	for _, source := range sources {
		for _, candidate := range docsAlternativeOutputPaths(source.RoutePath) {
			targetAbs := filepath.Join(siteRoot, filepath.FromSlash(candidate))
			if !fileExists(targetAbs) {
				continue
			}
			if _, ok := owned[candidate]; ok {
				continue
			}
			if docsFileLooksGenerated(targetAbs) {
				continue
			}
			return tuskerError(errorAlreadyExists, "Generated docs route collides with a human-owned file", withPath(candidate))
		}
	}
	return nil
}

func docsFileLooksGenerated(filePath string) bool {
	text, err := readText(filePath)
	if err != nil {
		return false
	}
	return strings.Contains(text, "_Synced from `") || strings.Contains(text, "source_kind:")
}

func docsComputeStaleFiles(previous, current []string) []string {
	currentSet := map[string]struct{}{}
	for _, rel := range current {
		currentSet[docsNormalizePath(rel)] = struct{}{}
	}
	var stale []string
	for _, rel := range previous {
		rel = docsNormalizePath(rel)
		if _, ok := currentSet[rel]; !ok {
			stale = append(stale, rel)
		}
	}
	sort.Strings(stale)
	return stale
}

func buildDocsRemovedRoutesReport(generatedAt string, previous, current docsExportState, sources []docsSourceDocument) docsRemovedRoutesReport {
	currentRoutes := map[string]struct{}{}
	for _, route := range current.Routes {
		key := docsNormalizeRouteValue(route.Route)
		if key != "" {
			currentRoutes[key] = struct{}{}
		}
	}
	redirectedRoutes := map[string]struct{}{}
	for _, source := range sources {
		for _, redirect := range source.RedirectFrom {
			key := docsNormalizeRouteValue(redirect)
			if key != "" {
				redirectedRoutes[key] = struct{}{}
			}
		}
	}
	var removed []docsRemovedRoute
	for _, route := range previous.Routes {
		key := docsNormalizeRouteValue(route.Route)
		if key == "" {
			continue
		}
		if _, ok := currentRoutes[key]; ok {
			continue
		}
		if _, ok := redirectedRoutes[key]; ok {
			continue
		}
		removed = append(removed, docsRemovedRoute{
			Title:      route.Title,
			SourceKind: route.SourceKind,
			SourceID:   route.SourceID,
			SourcePath: route.SourcePath,
			Route:      key,
			RouteURL:   firstNonEmpty(route.RouteURL, docsRouteURL(key)),
			OutputPath: route.OutputPath,
		})
	}
	sort.Slice(removed, func(i, j int) bool {
		if removed[i].Route != removed[j].Route {
			return removed[i].Route < removed[j].Route
		}
		return removed[i].SourcePath < removed[j].SourcePath
	})
	if removed == nil {
		removed = []docsRemovedRoute{}
	}
	return docsRemovedRoutesReport{
		SchemaVersion: docsManifestSchemaVersion,
		GeneratedAt:   generatedAt,
		Removed:       removed,
	}
}

func docsUniqueStateRoutes(routes []docsExportStateRoute) []docsExportStateRoute {
	seen := map[string]struct{}{}
	var out []docsExportStateRoute
	for _, route := range routes {
		route.Route = docsNormalizeRouteValue(route.Route)
		if route.Route == "" {
			continue
		}
		if route.RouteURL == "" {
			route.RouteURL = docsRouteURL(route.Route)
		}
		route.OutputPath = docsNormalizePath(route.OutputPath)
		if _, ok := seen[route.Route]; ok {
			continue
		}
		seen[route.Route] = struct{}{}
		out = append(out, route)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Route != out[j].Route {
			return out[i].Route < out[j].Route
		}
		return out[i].SourcePath < out[j].SourcePath
	})
	return out
}

func docsFindLegacyGeneratedFiles(siteRoot string, keep []string) ([]string, error) {
	keepSet := map[string]struct{}{}
	for _, rel := range keep {
		keepSet[docsNormalizePath(rel)] = struct{}{}
	}
	root := filepath.Join(siteRoot, docsContentRootRelative)
	var stale []string
	err := filepath.WalkDir(root, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel := docsRelativeTo(siteRoot, current)
		if _, ok := keepSet[rel]; ok {
			return nil
		}
		if docsFileLooksGenerated(current) {
			stale = append(stale, rel)
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	sort.Strings(stale)
	return stale, nil
}

func docsPublicSources(sources []docsSourceDocument) []docsSourceDocument {
	var out []docsSourceDocument
	for _, source := range sources {
		if source.Internal {
			continue
		}
		out = append(out, source)
	}
	return out
}

func renderLLMSText(sources []docsSourceDocument, full bool) string {
	var b strings.Builder
	b.WriteString("# Tusker Docs\n\n")
	if full {
		b.WriteString("Generated catalog of published documentation routes.\n\n")
	} else {
		b.WriteString("Compact catalog of exported docs.\n\n")
	}
	docsSortSources(sources)
	for _, source := range sources {
		b.WriteString("- ")
		b.WriteString(source.Title)
		b.WriteString(" | ")
		b.WriteString(source.RouteURL)
		if source.Audience != "" {
			b.WriteString(" | audience: ")
			b.WriteString(source.Audience)
		}
		if source.Mode != "" {
			b.WriteString(" | mode: ")
			b.WriteString(source.Mode)
		}
		if source.AgentLayer != "" && source.AgentLayer != "none" {
			b.WriteString(" | agent_layer: ")
			b.WriteString(source.AgentLayer)
		}
		if source.Updated != "" {
			b.WriteString(" | updated: ")
			b.WriteString(source.Updated)
		}
		b.WriteByte('\n')
		if strings.TrimSpace(source.Description) != "" {
			b.WriteString("  ")
			b.WriteString(strings.TrimSpace(source.Description))
			b.WriteByte('\n')
		}
		if full {
			b.WriteString("  source: ")
			b.WriteString(source.SourcePath)
			b.WriteByte('\n')
			if len(source.Tags) > 0 {
				b.WriteString("  tags: ")
				b.WriteString(strings.Join(source.Tags, ", "))
				b.WriteByte('\n')
			}
			if len(source.SourceOfTruth) > 0 {
				b.WriteString("  source_of_truth: ")
				b.WriteString(strings.Join(source.SourceOfTruth, ", "))
				b.WriteByte('\n')
			}
			if strings.TrimSpace(source.Body) != "" {
				b.WriteString("\n")
				b.WriteString(strings.TrimSpace(source.Body))
				b.WriteString("\n")
			}
		}
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

func docsOptionalInt(values ...any) *int {
	for _, value := range values {
		if value == nil {
			continue
		}
		switch current := value.(type) {
		case int:
			v := current
			return &v
		case int64:
			v := int(current)
			return &v
		case float64:
			v := int(current)
			return &v
		case string:
			current = strings.TrimSpace(current)
			if current == "" {
				continue
			}
			v := atoiSafe(current)
			return &v
		}
	}
	return nil
}

func docsMergeTags(parts ...[]string) []string {
	var merged []string
	for _, part := range parts {
		merged = append(merged, part...)
	}
	return docsUniqueStrings(merged)
}

func docsRegistryString(registryValue, frontmatterValue string, allowFrontmatterOverride bool) string {
	registryValue = strings.TrimSpace(registryValue)
	frontmatterValue = strings.TrimSpace(frontmatterValue)
	if allowFrontmatterOverride && frontmatterValue != "" {
		return frontmatterValue
	}
	return firstNonEmpty(registryValue, frontmatterValue)
}

func applyDocsCanonicalDefaults(source *docsSourceDocument, sourceStatus string) {
	source.CanonicalStatus = strings.ToLower(strings.TrimSpace(source.CanonicalStatus))
	source.Deprecated = source.Deprecated || source.CanonicalStatus == "deprecated" || source.CanonicalStatus == "historical"
	if source.SourceKind == docsSourceKindVault && source.DocIntent == "canon" {
		source.Canonical = true
	}
	if source.Canonical && source.CanonicalStatus == "" {
		switch strings.ToLower(strings.TrimSpace(sourceStatus)) {
		case "approved", "published":
			source.CanonicalStatus = "approved"
		case "deprecated", "archived", "superseded":
			source.CanonicalStatus = "deprecated"
			source.Deprecated = true
		default:
			source.CanonicalStatus = "draft"
		}
	}
	if source.OwnerEpic == "" {
		source.OwnerEpic = firstNonEmpty(source.CanonFor, source.Epic)
	}
}

func validateDocsCanonicalLifecycle(source docsSourceDocument) error {
	status := strings.TrimSpace(source.CanonicalStatus)
	if status != "" {
		if _, ok := canonicalStatuses[status]; !ok {
			return tuskerError(errorInvalidField, fmt.Sprintf(`invalid canonical_status "%s"`, status), withPath(source.SourcePath))
		}
	}
	if source.Canonical && status == "" {
		return tuskerError(errorInvalidField, "canonical docs must set canonical_status", withPath(source.SourcePath))
	}
	if source.Canonical && strings.TrimSpace(source.OwnerEpic) == "" {
		return tuskerError(errorInvalidField, "canonical docs must set owner_epic", withPath(source.SourcePath))
	}
	if source.CanonicalStatus == "approved" && strings.TrimSpace(source.VerifiedAt) == "" {
		return tuskerError(errorInvalidField, "approved canonical docs must set verified_at", withPath(source.SourcePath))
	}
	return nil
}

func docsAudienceFromRoute(route string) string {
	segments := strings.Split(strings.Trim(route, "/"), "/")
	if len(segments) == 0 {
		return ""
	}
	switch segments[0] {
	case "developer", "user", "support", "internal":
		return segments[0]
	case "release-notes":
		return "release"
	default:
		return ""
	}
}

func docsFirstHeading(body string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return ""
}

func docsFirstParagraph(body string) string {
	lines := strings.Split(body, "\n")
	var chunks []string
	inFence := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "_Synced from `") {
			continue
		}
		if trimmed == "" {
			if len(chunks) > 0 {
				break
			}
			continue
		}
		chunks = append(chunks, trimmed)
	}
	if len(chunks) == 0 {
		return ""
	}
	text := strings.Join(chunks, " ")
	text = strings.ReplaceAll(text, "`", "")
	text = docsMarkdownLinkPattern.ReplaceAllString(text, `$2`)
	return strings.TrimSpace(text)
}

func todayTimestamp() string {
	return time.Now().UTC().Format(time.RFC3339)
}
