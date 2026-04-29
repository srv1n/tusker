package main

import (
	"path/filepath"
	"sort"
	"strings"
)

func validateDocsRoutePath(route string) error {
	route = strings.TrimSpace(route)
	if route == "" {
		return tuskerError(errorInvalidField, "documentation route is empty")
	}
	if strings.HasPrefix(route, "/") || strings.HasSuffix(route, "/") {
		return tuskerError(errorInvalidField, "documentation route must not start or end with '/': "+route)
	}
	segments := strings.Split(route, "/")
	if len(segments) == 0 {
		return tuskerError(errorInvalidField, "documentation route is empty")
	}
	if _, ok := docsLaneLabels[segments[0]]; !ok {
		return tuskerError(errorInvalidField, "documentation route must begin with a supported lane: "+route)
	}
	for _, segment := range segments {
		if segment == "" || segment == "." || segment == ".." {
			return tuskerError(errorInvalidField, "documentation route contains an invalid path segment: "+route)
		}
	}
	if strings.EqualFold(segments[len(segments)-1], "index") {
		return tuskerError(errorInvalidField, "documentation route final segment must not be index: "+route)
	}
	return nil
}

func docsOutputPathForRoute(route, ext string, asIndex bool) string {
	segments := strings.Split(strings.Trim(route, "/"), "/")
	if len(segments) == 0 {
		return filepath.Join(docsContentRootRelative, "index"+ext)
	}
	if asIndex {
		return filepath.Join(append([]string{docsContentRootRelative}, append(segments, "index"+ext)...)...)
	}
	fileName := segments[len(segments)-1] + ext
	dir := segments[:len(segments)-1]
	if len(dir) == 0 {
		return filepath.Join(append([]string{docsContentRootRelative}, fileName)...)
	}
	return filepath.Join(append([]string{docsContentRootRelative}, append(dir, fileName)...)...)
}

func docsAlternativeOutputPaths(route string) []string {
	var out []string
	for _, ext := range []string{".md", ".mdx"} {
		out = append(out,
			docsNormalizePath(docsOutputPathForRoute(route, ext, false)),
			docsNormalizePath(docsOutputPathForRoute(route, ext, true)),
		)
	}
	return docsUniqueStrings(out)
}

func buildDocsRouteTable(sources []docsSourceDocument) (docsRouteTable, error) {
	table := docsRouteTable{
		ByRoute:      map[string]*docsSourceDocument{},
		BySource:     map[string]*docsSourceDocument{},
		AliasToRoute: map[string]string{},
	}
	sort.SliceStable(sources, func(i, j int) bool {
		leftPriority := docsAliasPriority(sources[i])
		rightPriority := docsAliasPriority(sources[j])
		if leftPriority != rightPriority {
			return leftPriority > rightPriority
		}
		if sources[i].RoutePath != sources[j].RoutePath {
			return sources[i].RoutePath < sources[j].RoutePath
		}
		return sources[i].SourcePath < sources[j].SourcePath
	})
	for i := range sources {
		source := &sources[i]
		if err := validateDocsRoutePath(source.RoutePath); err != nil {
			return docsRouteTable{}, err
		}
		route := strings.Trim(source.RoutePath, "/")
		if existing, ok := table.ByRoute[route]; ok {
			return docsRouteTable{}, tuskerError(errorInvalidField, "documentation route collision: "+route, withContext(map[string]any{
				"left":  existing.SourcePath,
				"right": source.SourcePath,
			}))
		}
		table.ByRoute[route] = source
		table.BySource[source.SourceAbsPath] = source
		docsRegisterAlias(table.AliasToRoute, route, source.SourceID)
		docsRegisterAlias(table.AliasToRoute, route, source.CanonFor)
		docsRegisterAlias(table.AliasToRoute, route, source.Story)
		if source.SourceKind == docsSourceKindVault && source.DocIntent == "canon" && strings.TrimSpace(source.Epic) != "" {
			docsRegisterAlias(table.AliasToRoute, route, source.Epic)
		}
	}
	return table, nil
}

func docsAliasPriority(source docsSourceDocument) int {
	score := 0
	if source.SourceKind == docsSourceKindVault {
		score += 10
	}
	if source.DocIntent == "canon" {
		score += 5
	}
	if strings.TrimSpace(source.CanonFor) != "" {
		score += 3
	}
	if strings.TrimSpace(source.Story) != "" {
		score += 1
	}
	return score
}

func docsRegisterAlias(index map[string]string, route, alias string) {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return
	}
	if _, ok := index[alias]; ok {
		return
	}
	index[alias] = route
}

func buildDocsNavigation(sources []docsSourceDocument) docsNavigation {
	type sectionNode struct {
		Slug     string
		Label    string
		Items    []docsNavigationItem
		Children map[string]*sectionNode
	}
	type laneNode struct {
		Slug     string
		Label    string
		Items    []docsNavigationItem
		Children map[string]*sectionNode
	}

	childPrefixes := map[string]map[string]struct{}{}
	sectionLabels := map[string]string{}
	for _, source := range sources {
		segments := strings.Split(strings.Trim(source.RoutePath, "/"), "/")
		if len(segments) == 0 {
			continue
		}
		lane := segments[0]
		if childPrefixes[lane] == nil {
			childPrefixes[lane] = map[string]struct{}{}
		}
		if len(segments) >= 3 {
			for i := 2; i < len(segments); i++ {
				childPrefixes[lane][strings.Join(segments[1:i], "/")] = struct{}{}
			}
		}
		if strings.TrimSpace(source.SectionTitle) != "" && len(segments) >= 2 {
			key := lane + "/" + segments[1]
			if sectionLabels[key] == "" {
				sectionLabels[key] = strings.TrimSpace(source.SectionTitle)
			}
		}
	}

	lanes := map[string]*laneNode{}
	getLane := func(slug string) *laneNode {
		lane := lanes[slug]
		if lane != nil {
			return lane
		}
		lane = &laneNode{
			Slug:     slug,
			Label:    docsLaneLabels[slug],
			Children: map[string]*sectionNode{},
		}
		lanes[slug] = lane
		return lane
	}

	var ensureSection func(children map[string]*sectionNode, lane, fullPath string) *sectionNode
	ensureSection = func(children map[string]*sectionNode, lane, fullPath string) *sectionNode {
		if node := children[fullPath]; node != nil {
			return node
		}
		pieces := strings.Split(fullPath, "/")
		label := sectionLabels[lane+"/"+pieces[0]]
		if label == "" {
			label = docsTitleizeSegment(pieces[len(pieces)-1])
		}
		node := &sectionNode{
			Slug:     fullPath,
			Label:    label,
			Children: map[string]*sectionNode{},
		}
		children[fullPath] = node
		return node
	}

	for _, source := range sources {
		segments := strings.Split(strings.Trim(source.RoutePath, "/"), "/")
		if len(segments) == 0 {
			continue
		}
		item := docsNavigationItem{
			Title: source.Title,
			Route: source.RouteURL,
			Order: source.Order,
		}
		lane := getLane(segments[0])
		switch {
		case len(segments) == 1:
			lane.Items = append(lane.Items, item)
		case len(segments) == 2:
			sectionPath := segments[1]
			if _, ok := childPrefixes[lane.Slug][sectionPath]; ok {
				section := ensureSection(lane.Children, lane.Slug, sectionPath)
				if item.Order == nil {
					order := 0
					item.Order = &order
				}
				section.Items = append(section.Items, item)
			} else {
				lane.Items = append(lane.Items, item)
			}
		default:
			currentChildren := lane.Children
			var currentSection *sectionNode
			for i := 1; i < len(segments)-1; i++ {
				path := strings.Join(segments[1:i+1], "/")
				currentSection = ensureSection(currentChildren, lane.Slug, path)
				currentChildren = currentSection.Children
			}
			if currentSection != nil {
				currentSection.Items = append(currentSection.Items, item)
			}
		}
	}

	var buildSections func(children map[string]*sectionNode) []docsNavigationSection
	buildSections = func(children map[string]*sectionNode) []docsNavigationSection {
		var keys []string
		for key := range children {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		var sections []docsNavigationSection
		for _, key := range keys {
			node := children[key]
			sortNavigationItems(node.Items)
			sections = append(sections, docsNavigationSection{
				Slug:     node.Slug,
				Label:    node.Label,
				Items:    append([]docsNavigationItem{}, node.Items...),
				Sections: buildSections(node.Children),
			})
		}
		return sections
	}

	var out []docsNavigationLane
	for _, slug := range docsLaneOrder {
		lane := lanes[slug]
		if lane == nil {
			continue
		}
		sortNavigationItems(lane.Items)
		sections := buildSections(lane.Children)
		out = append(out, docsNavigationLane{
			Slug:     lane.Slug,
			Label:    lane.Label,
			Items:    append([]docsNavigationItem{}, lane.Items...),
			Sections: sections,
		})
	}
	return docsNavigation{Lanes: out}
}

func buildDocsContentManifest(generatedAt string, sources []docsSourceDocument) docsContentManifest {
	items := make([]docsContentManifestItem, 0, len(sources))
	for _, source := range sources {
		items = append(items, docsContentManifestItem{
			SourceKind:      string(source.SourceKind),
			ID:              source.ExportID(),
			Title:           source.Title,
			Route:           source.RouteURL,
			Audience:        source.Audience,
			DocIntent:       source.DocIntent,
			Epic:            source.Epic,
			OwnerEpic:       source.OwnerEpic,
			Story:           source.Story,
			CanonFor:        source.CanonFor,
			Canonical:       source.Canonical,
			CanonicalStatus: source.CanonicalStatus,
			VerifiedAt:      source.VerifiedAt,
			Deprecated:      source.Deprecated,
			SupersededBy:    source.SupersededBy,
			RedirectFrom:    append([]string{}, source.RedirectFrom...),
			SourcePath:      source.SourcePath,
			Tags:            append([]string{}, source.Tags...),
			Updated:         source.Updated,
			Summary:         source.Description,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Route != items[j].Route {
			return items[i].Route < items[j].Route
		}
		return items[i].Title < items[j].Title
	})
	return docsContentManifest{
		SchemaVersion: docsManifestSchemaVersion,
		GeneratedAt:   generatedAt,
		Items:         items,
	}
}

func buildDocsCanonManifest(generatedAt string, sources []docsSourceDocument) docsCanonManifest {
	canon := make([]docsCanonManifestDoc, 0)
	published := make([]docsCanonManifestDoc, 0, len(sources))
	for _, source := range sources {
		entry := docsCanonManifestDoc{
			Topic:           docsCanonTopic(source),
			Title:           source.Title,
			Route:           source.RouteURL,
			SourceKind:      string(source.SourceKind),
			SourceID:        source.ExportID(),
			SourcePath:      source.SourcePath,
			Audience:        source.Audience,
			DocIntent:       source.DocIntent,
			OwnerEpic:       source.OwnerEpic,
			Story:           source.Story,
			CanonFor:        source.CanonFor,
			Canonical:       source.Canonical,
			CanonicalStatus: source.CanonicalStatus,
			VerifiedAt:      source.VerifiedAt,
			Deprecated:      source.Deprecated,
			SupersededBy:    source.SupersededBy,
			RedirectFrom:    append([]string{}, source.RedirectFrom...),
			Tags:            append([]string{}, source.Tags...),
			Updated:         source.Updated,
			Summary:         source.Description,
		}
		published = append(published, entry)
		if docsIsCanonicalSource(source) {
			canon = append(canon, entry)
		}
	}
	sort.Slice(canon, func(i, j int) bool {
		if canon[i].Topic != canon[j].Topic {
			return canon[i].Topic < canon[j].Topic
		}
		return canon[i].Route < canon[j].Route
	})
	sort.Slice(published, func(i, j int) bool {
		if published[i].Route != published[j].Route {
			return published[i].Route < published[j].Route
		}
		return published[i].Title < published[j].Title
	})
	return docsCanonManifest{
		SchemaVersion: docsManifestSchemaVersion,
		GeneratedAt:   generatedAt,
		Canon:         canon,
		Published:     published,
		DoNotCite: []string{
			"site/src/content/docs/**",
			"site/scripts/sync-docs.mjs",
			"tusker/_system/generated/**",
			"docs/archive/**",
			"docs/progress/**",
			"docs/strategy/**",
		},
	}
}

func docsIsCanonicalSource(source docsSourceDocument) bool {
	return source.Canonical && !source.Deprecated
}

func docsCanonTopic(source docsSourceDocument) string {
	for _, value := range []string{source.OwnerEpic, source.CanonFor, source.Epic, source.Story, source.SourceID, source.RoutePath, source.SourcePath} {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return source.Title
}

func docsOrderValue(value *int) int {
	if value == nil {
		return 1 << 30
	}
	return *value
}

func docsRoutesNeedingIndexPaths(sources []docsSourceDocument) map[string]bool {
	routes := map[string]struct{}{}
	for _, source := range sources {
		routes[source.RoutePath] = struct{}{}
	}
	out := map[string]bool{}
	for route := range routes {
		prefix := route + "/"
		for other := range routes {
			if other == route {
				continue
			}
			if strings.HasPrefix(other, prefix) {
				out[route] = true
				break
			}
		}
	}
	return out
}

func sortNavigationItems(items []docsNavigationItem) {
	sort.SliceStable(items, func(i, j int) bool {
		leftOrder := docsOrderValue(items[i].Order)
		rightOrder := docsOrderValue(items[j].Order)
		if leftOrder != rightOrder {
			return leftOrder < rightOrder
		}
		if items[i].Title != items[j].Title {
			return items[i].Title < items[j].Title
		}
		return items[i].Route < items[j].Route
	})
}
