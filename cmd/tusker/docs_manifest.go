package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	docsManifestSchemaVersion    = 2
	docsPublicationIndexRelative = "_system/generated/publication.index.json"
	docsRegistryRelative         = "docs/publication.yaml"
	docsContentRootRelative      = "src/content/docs"
	docsGeneratedRootRelative    = "src/generated"
	docsAssetsRootRelative       = "public/generated/assets"
	docsNavigationRelative       = "src/generated/navigation.json"
	docsContentManifestRelative  = "src/generated/content-manifest.json"
	docsCanonManifestRelative    = "src/generated/canon-manifest.json"
	docsPublicCanonRelative      = "public/canon-manifest.json"
	docsRoutesRemovedRelative    = "src/generated/routes-removed.json"
	docsExportStateRelative      = "src/generated/export-state.json"
	docsExportReportRelative     = "src/generated/export-report.json"
	docsLLMSTxtRelative          = "public/llms.txt"
	docsLLMSFullTxtRelative      = "public/llms-full.txt"
)

var docsLaneOrder = []string{"developer", "user", "release-notes", "support", "internal"}

var docsLaneLabels = map[string]string{
	"developer":     "Developer Docs",
	"user":          "User Docs",
	"release-notes": "Release Notes",
	"support":       "Support Docs",
	"internal":      "Internal Docs",
}

type docsSourceKind string

const (
	docsSourceKindVault docsSourceKind = "vault_doc"
	docsSourceKindRepo  docsSourceKind = "repo_doc"
)

type docsSourceDocument struct {
	SourceKind      docsSourceKind
	SourceID        string
	Title           string
	Description     string
	Audience        string
	Mode            string
	AgentLayer      string
	SourceOfTruth   []string
	StaleWhenPaths  []string
	DocsMapOrder    int
	DocIntent       string
	Epic            string
	OwnerEpic       string
	Task            string
	CanonFor        string
	Canonical       bool
	CanonicalStatus string
	VerifiedAt      string
	Deprecated      bool
	SupersededBy    string
	RedirectFrom    []string
	SourcePath      string
	SourceAbsPath   string
	Body            string
	Tags            []string
	Updated         string
	RoutePath       string
	RouteURL        string
	SectionTitle    string
	Order           *int
	Internal        bool
	OutputExt       string
	OutputRelPath   string
}

func (d docsSourceDocument) ExportID() string {
	if strings.TrimSpace(d.SourceID) != "" {
		return d.SourceID
	}
	return d.SourcePath
}

func (d docsSourceDocument) RouteKey() string {
	return strings.TrimSpace(d.RoutePath)
}

func (d docsSourceDocument) AssetOwnerSlug() string {
	if d.SourceKind == docsSourceKindVault && strings.TrimSpace(d.SourceID) != "" {
		return docsSlugifySegment(d.SourceID)
	}
	route := strings.Trim(strings.TrimSpace(d.RoutePath), "/")
	if route == "" {
		return "root"
	}
	return docsSlugifySegment(strings.ReplaceAll(route, "/", "-"))
}

type docsRouteTable struct {
	ByRoute      map[string]*docsSourceDocument
	BySource     map[string]*docsSourceDocument
	AliasToRoute map[string]string
}

type docsExportState struct {
	GeneratedAt  string                 `json:"generatedAt"`
	ContentFiles []string               `json:"contentFiles"`
	AssetFiles   []string               `json:"assetFiles"`
	Routes       []docsExportStateRoute `json:"routes,omitempty"`
}

type docsExportStateRoute struct {
	Title      string `json:"title"`
	SourceKind string `json:"sourceKind"`
	SourceID   string `json:"sourceID,omitempty"`
	SourcePath string `json:"sourcePath"`
	Route      string `json:"route"`
	RouteURL   string `json:"routeURL"`
	OutputPath string `json:"outputPath"`
}

type docsRemovedRoutesReport struct {
	SchemaVersion int                `json:"schemaVersion"`
	GeneratedAt   string             `json:"generatedAt"`
	Removed       []docsRemovedRoute `json:"removed"`
}

type docsRemovedRoute struct {
	Title      string `json:"title"`
	SourceKind string `json:"sourceKind"`
	SourceID   string `json:"sourceID,omitempty"`
	SourcePath string `json:"sourcePath"`
	Route      string `json:"route"`
	RouteURL   string `json:"routeURL"`
	OutputPath string `json:"outputPath"`
}

type docsExportSummary struct {
	GeneratedAt    string `json:"generatedAt"`
	ExportedDocs   int    `json:"exportedDocs"`
	ExportedAssets int    `json:"exportedAssets"`
	DeletedDocs    int    `json:"deletedDocs"`
	DeletedAssets  int    `json:"deletedAssets"`
	SkippedDocs    int    `json:"skippedDocs"`
	PublicOnly     bool   `json:"publicOnly"`
}

type docsExportReport struct {
	Summary docsExportSummary       `json:"summary"`
	Routes  []docsExportReportRoute `json:"routes"`
}

type docsExportReportRoute struct {
	Title      string   `json:"title"`
	SourceKind string   `json:"sourceKind"`
	SourceID   string   `json:"sourceID,omitempty"`
	SourcePath string   `json:"sourcePath"`
	Route      string   `json:"route"`
	OutputPath string   `json:"outputPath"`
	Assets     []string `json:"assets,omitempty"`
}

type docsNavigation struct {
	Lanes []docsNavigationLane `json:"lanes"`
}

type docsNavigationLane struct {
	Slug     string                  `json:"slug"`
	Label    string                  `json:"label"`
	Items    []docsNavigationItem    `json:"items,omitempty"`
	Sections []docsNavigationSection `json:"sections,omitempty"`
}

type docsNavigationSection struct {
	Slug     string                  `json:"slug"`
	Label    string                  `json:"label"`
	Items    []docsNavigationItem    `json:"items,omitempty"`
	Sections []docsNavigationSection `json:"sections,omitempty"`
}

type docsNavigationItem struct {
	Title string `json:"title"`
	Route string `json:"route"`
	Order *int   `json:"order,omitempty"`
}

type docsContentManifest struct {
	SchemaVersion int                       `json:"schemaVersion"`
	GeneratedAt   string                    `json:"generatedAt"`
	Items         []docsContentManifestItem `json:"items"`
}

type docsContentManifestItem struct {
	SourceKind      string   `json:"source_kind"`
	ID              string   `json:"id"`
	Title           string   `json:"title"`
	Route           string   `json:"route"`
	Audience        string   `json:"audience"`
	Mode            string   `json:"mode,omitempty"`
	AgentLayer      string   `json:"agent_layer,omitempty"`
	SourceOfTruth   []string `json:"source_of_truth,omitempty"`
	StaleWhenPaths  []string `json:"stale_when_paths,omitempty"`
	DocIntent       string   `json:"doc_intent,omitempty"`
	Epic            string   `json:"epic,omitempty"`
	OwnerEpic       string   `json:"owner_epic,omitempty"`
	Task            string   `json:"task,omitempty"`
	CanonFor        string   `json:"canon_for,omitempty"`
	Canonical       bool     `json:"canonical,omitempty"`
	CanonicalStatus string   `json:"canonical_status,omitempty"`
	VerifiedAt      string   `json:"verified_at,omitempty"`
	Deprecated      bool     `json:"deprecated,omitempty"`
	SupersededBy    string   `json:"superseded_by,omitempty"`
	RedirectFrom    []string `json:"redirect_from,omitempty"`
	SourcePath      string   `json:"source_path"`
	Tags            []string `json:"tags,omitempty"`
	Updated         string   `json:"updated,omitempty"`
	Summary         string   `json:"summary,omitempty"`
}

type docsCanonManifest struct {
	SchemaVersion int                    `json:"schemaVersion"`
	GeneratedAt   string                 `json:"generatedAt"`
	Canon         []docsCanonManifestDoc `json:"canon"`
	Published     []docsCanonManifestDoc `json:"published"`
	DoNotCite     []string               `json:"do_not_cite"`
}

type docsCanonManifestDoc struct {
	Topic           string   `json:"topic"`
	Title           string   `json:"title"`
	Route           string   `json:"route"`
	SourceKind      string   `json:"source_kind"`
	SourceID        string   `json:"source_id,omitempty"`
	SourcePath      string   `json:"source_path"`
	Audience        string   `json:"audience,omitempty"`
	Mode            string   `json:"mode,omitempty"`
	AgentLayer      string   `json:"agent_layer,omitempty"`
	SourceOfTruth   []string `json:"source_of_truth,omitempty"`
	StaleWhenPaths  []string `json:"stale_when_paths,omitempty"`
	DocIntent       string   `json:"doc_intent,omitempty"`
	OwnerEpic       string   `json:"owner_epic,omitempty"`
	Task            string   `json:"task,omitempty"`
	CanonFor        string   `json:"canon_for,omitempty"`
	Canonical       bool     `json:"canonical,omitempty"`
	CanonicalStatus string   `json:"canonical_status,omitempty"`
	VerifiedAt      string   `json:"verified_at,omitempty"`
	Deprecated      bool     `json:"deprecated,omitempty"`
	SupersededBy    string   `json:"superseded_by,omitempty"`
	RedirectFrom    []string `json:"redirect_from,omitempty"`
	Tags            []string `json:"tags,omitempty"`
	Updated         string   `json:"updated,omitempty"`
	Summary         string   `json:"summary,omitempty"`
}

type docsLLMRecord struct {
	Title    string
	Route    string
	Summary  string
	Audience string
	Updated  string
}

type docsResolvedLink struct {
	Kind string
	Href string
	Text string
}

type docsRewriteContext struct {
	RepoRoot   string
	VaultRoot  string
	Source     docsSourceDocument
	RouteTable docsRouteTable
	Assets     *docsAssetExporter
}

func docsSourceLabel(kind docsSourceKind) string {
	return strings.ReplaceAll(string(kind), "_", "-")
}

func docsNormalizePath(value string) string {
	return filepath.ToSlash(strings.TrimSpace(value))
}

func docsJoinRoute(parts ...string) string {
	segments := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.Trim(strings.TrimSpace(part), "/")
		if part == "" {
			continue
		}
		for _, segment := range strings.Split(part, "/") {
			segment = strings.TrimSpace(segment)
			if segment != "" {
				segments = append(segments, segment)
			}
		}
	}
	return strings.Join(segments, "/")
}

func docsNormalizeRouteValue(route string) string {
	return strings.Trim(strings.TrimSpace(route), "/")
}

func docsNormalizeRouteList(values ...[]string) []string {
	var out []string
	for _, list := range values {
		for _, value := range list {
			route := docsNormalizeRouteValue(value)
			if route != "" {
				out = append(out, route)
			}
		}
	}
	return docsUniqueStrings(out)
}

func docsRouteURL(route string) string {
	route = strings.Trim(strings.TrimSpace(route), "/")
	if route == "" {
		return "/"
	}
	return "/" + route + "/"
}

func docsRelativeTo(base, target string) string {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return docsNormalizePath(target)
	}
	return docsNormalizePath(rel)
}

func docsUniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func docsTitleizeSegment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.ReplaceAll(value, "-", " ")
	value = strings.ReplaceAll(value, "_", " ")
	fields := strings.Fields(value)
	for i, field := range fields {
		if field == strings.ToUpper(field) {
			continue
		}
		fields[i] = capitalize(strings.ToLower(field))
	}
	return strings.Join(fields, " ")
}

func docsSlugifySegment(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			lastDash = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_' || r == ' ' || r == '/':
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return "doc"
	}
	return slug
}

func docsHeadingAnchor(value string) string {
	return docsSlugifySegment(value)
}

func docsInferOutputExt(sourcePath, body string) string {
	if strings.EqualFold(filepath.Ext(sourcePath), ".mdx") {
		return ".mdx"
	}
	lines := strings.Split(body, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "import ") || strings.HasPrefix(trimmed, "export ") {
			return ".mdx"
		}
		if strings.HasPrefix(trimmed, "<") && len(trimmed) > 1 {
			next := trimmed[1]
			if next >= 'A' && next <= 'Z' {
				return ".mdx"
			}
		}
	}
	return ".md"
}

func docsFileUpdatedDate(filePath string) string {
	info, err := os.Stat(filePath)
	if err != nil {
		return ""
	}
	return info.ModTime().UTC().Format("2006-01-02")
}

func docsSortSources(sources []docsSourceDocument) {
	sort.SliceStable(sources, func(i, j int) bool {
		left := sources[i]
		right := sources[j]
		if left.DocsMapOrder != right.DocsMapOrder {
			if left.DocsMapOrder == 0 {
				return false
			}
			if right.DocsMapOrder == 0 {
				return true
			}
			return left.DocsMapOrder < right.DocsMapOrder
		}
		if left.RoutePath != right.RoutePath {
			return left.RoutePath < right.RoutePath
		}
		return left.SourcePath < right.SourcePath
	})
}
