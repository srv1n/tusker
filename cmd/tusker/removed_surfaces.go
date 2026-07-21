package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// removedSurfaces centralizes the hard fence around V5/V6/docs-map era code.
// The implementations were deleted from the V7-only build; these functions keep
// accidental command invocations explicit instead of silently reviving legacy
// behavior through hidden compatibility aliases.

func removedSurfaceError(name string) error {
	return tuskerError(
		errorInvalidArg,
		fmt.Sprintf("%s was removed from the V7-only Tusker build", name),
		withHint("use .tusker V7 work records, project SKILL.md, and knowledge/domains/**; migrate old vaults outside the default repo path before importing current work"),
	)
}

func docsInitCmd(args Args) error           { return removedSurfaceError("docs init") }
func docsModelCmd(args Args) error          { return removedSurfaceError("docs model") }
func docsCatalogCmd(args Args) error        { return removedSurfaceError("docs catalog") }
func docsFreshnessCmd(args Args) error      { return removedSurfaceError("docs freshness") }
func docsImpactCheckCmd(args Args) error    { return removedSurfaceError("docs check") }
func docsImpactApplyCmd(args Args) error    { return removedSurfaceError("docs apply") }
func docsImpactNoopCmd(args Args) error     { return removedSurfaceError("docs noop") }
func docsImpactWaiveCmd(args Args) error    { return removedSurfaceError("docs waive") }
func docsExportCmd(args Args) error         { return removedSurfaceError("docs export") }
func docsDevCmd(args Args) error            { return removedSurfaceError("docs dev") }
func docsBuildCmd(args Args) error          { return removedSurfaceError("docs build") }
func domainListCmd(args Args) error         { return domainV7ListCmd(args) }
func domainShowCmd(args Args) error         { return domainV7ShowCmd(args) }
func domainNewCmd(args Args) error          { return newV7Domain(args) }
func domainCanonCmd(args Args) error        { return domainV7CanonCmd(args) }
func domainGraphCmd(args Args) error        { return removedSurfaceError("domain graph") }
func knowledgeMapCmd(args Args) error       { return removedSurfaceError("knowledge map") }
func knowledgeListCmd(args Args) error      { return removedSurfaceError("knowledge list") }
func knowledgeShowCmd(args Args) error      { return removedSurfaceError("knowledge show") }
func knowledgeRouteCmd(args Args) error     { return removedSurfaceError("knowledge route") }
func knowledgeFreshnessCmd(args Args) error { return removedSurfaceError("knowledge freshness") }
func knowledgeCheckCmd(args Args) error     { return removedSurfaceError("knowledge check") }
func knowledgeApplyCmd(args Args) error     { return removedSurfaceError("knowledge apply") }
func knowledgeNoopCmd(args Args) error      { return removedSurfaceError("knowledge noop") }
func knowledgeWaiveCmd(args Args) error     { return removedSurfaceError("knowledge waive") }
func knowledgeNewCmd(args Args) error       { return knowledgeV7NewCmd(args) }
func publishExportCmd(args Args) error      { return removedSurfaceError("publish export") }
func publishBuildCmd(args Args) error       { return removedSurfaceError("publish build") }
func publishDevCmd(args Args) error         { return removedSurfaceError("publish dev") }
func publishLLMSCmd(args Args) error        { return removedSurfaceError("publish llms") }

func newV5Epic(args Args) error              { return removedSurfaceError("new V5 epic") }
func newV5Task(args Args, kind string) error { return removedSurfaceError("new V5 task") }
func newV5Doc(args Args) error               { return removedSurfaceError("new V5 doc") }
func nextV5Cmd(args Args) error              { return removedSurfaceError("legacy next") }
func verifyCmd(args Args) error              { return removedSurfaceError("legacy verify") }
func closeV5Cmd(args Args) error             { return removedSurfaceError("legacy close") }
func bootstrapV6(args Args) error            { return removedSurfaceError("V6 init") }

type v5MigrationReport struct {
	OK              bool              `json:"ok"`
	DryRun          bool              `json:"dryRun"`
	Vault           string            `json:"vault"`
	BackupPath      string            `json:"backupPath,omitempty"`
	NotesScanned    int               `json:"notesScanned"`
	NotesChanged    int               `json:"notesChanged"`
	FilesMoved      int               `json:"filesMoved"`
	DocsMapNodesAdd int               `json:"docsMapNodesAdded"`
	IDMap           map[string]string `json:"idMap,omitempty"`
	Moved           []v5MigrationMove `json:"moved,omitempty"`
	Changed         []string          `json:"changed,omitempty"`
	Warnings        []string          `json:"warnings,omitempty"`
}

type v5MigrationMove struct {
	From string `json:"from"`
	To   string `json:"to"`
}

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

func migrateLegacyVaultToV5(args Args) (*v5MigrationReport, error) {
	return nil, removedSurfaceError("migrate-v5")
}
func migrateV5VaultToV6(args Args) (*v6MigrationReport, error) {
	return nil, removedSurfaceError("migrate-v6")
}

func printV6MigrationReport(report *v6MigrationReport, args Args) {
	if args.Bool("json") {
		emitJSON(report)
		return
	}
	if report == nil {
		fmt.Println("V6 migration was removed from the V7-only build.")
		return
	}
	fmt.Printf("V6 migration report for %s\n", report.Vault)
}

func publishSkillCmd(args Args) error {
	if !args.Bool("v7") {
		return tuskerError(errorInvalidArg, "publish skill now only supports explicit --v7", withHint("run `tusker publish skill --v7 --out <dir>`"))
	}
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	out := strings.TrimSpace(args.String("out"))
	if out == "" {
		out = filepath.Join("dist", "project-skill")
	}
	out, err = filepath.Abs(out)
	if err != nil {
		return err
	}
	if err := validateV7PublishSkillOutputPath(vaultPath, out); err != nil {
		return err
	}
	if err := os.RemoveAll(out); err != nil {
		return err
	}
	if err := ensureDir(out); err != nil {
		return err
	}
	if err := writeV7ProjectSkill(vaultPath, filepath.Join(out, "SKILL.md")); err != nil {
		return err
	}
	knowledgeRoot := filepath.Join(vaultPath, "knowledge", "domains")
	if dirExists(knowledgeRoot) {
		if err := copyDirFiltered(knowledgeRoot, filepath.Join(out, "knowledge", "domains"), func(path string, d os.DirEntry) bool {
			name := d.Name()
			if strings.HasPrefix(name, ".") && name != ".gitkeep" {
				return false
			}
			return true
		}); err != nil {
			return err
		}
	}
	if err := writeText(filepath.Join(out, ".tusker-project-skill-export"), "schema: tusker.project-skill-export/v7\n"); err != nil {
		return err
	}
	if !args.Bool("quiet") {
		fmt.Printf("Wrote V7 project skill package: %s\n", out)
	}
	return nil
}

func validateV7PublishSkillOutputPath(vaultPath, out string) error {
	if parent := filepath.Dir(out); parent == out {
		return tuskerError(errorInvalidArg, "unsafe publish skill output path: "+out, withHint("choose a dedicated generated output directory such as dist/project-skill"))
	}
	protected := map[string]string{
		"current directory": mustGetwd(),
		"repo root":         v7RepoRoot(vaultPath),
		"vault root":        vaultPath,
	}
	for _, env := range []string{"HOME", "USERPROFILE"} {
		if value := strings.TrimSpace(os.Getenv(env)); value != "" {
			protected[strings.ToLower(env)] = value
		}
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		protected["home"] = home
	}
	for label, path := range protected {
		if strings.TrimSpace(path) == "" {
			continue
		}
		if samePathOrChild(path, out) {
			return tuskerError(
				errorInvalidArg,
				"unsafe publish skill output path: "+out,
				withHint("choose a dedicated generated output directory such as dist/project-skill"),
				withContext(map[string]any{"protected_path": label}),
			)
		}
	}
	return nil
}

func copyDirFiltered(src, dst string, include func(path string, d os.DirEntry) bool) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !include(path, d) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return ensureDir(dst)
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return ensureDir(target)
		}
		return copyRegularFile(path, target)
	})
}

func copyRegularFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := ensureDir(filepath.Dir(dst)); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

// Minimal V5/V6/docs-map shapes retained only so the remaining shared index and
// validation code can reject legacy records cleanly. They no longer load config,
// validate docs publication state, or add graph routes.

const docsMapRelative = "_config/docs-map.yaml"

type DocsMap struct {
	Schema  string                   `yaml:"schema"`
	Domains map[string]DocsMapDomain `yaml:"domains"`
	Nodes   []DocsMapNode            `yaml:"nodes"`
}

type DocsMapDomain struct {
	Label       string `yaml:"label"`
	Description string `yaml:"description"`
	OwnerEpic   string `yaml:"owner_epic"`
}

type DocsMapStaleWhen struct {
	Paths []string `yaml:"paths"`
}

type DocsMapNode struct {
	ID                 string           `yaml:"id"`
	Title              string           `yaml:"title"`
	Page               string           `yaml:"page"`
	Domain             string           `yaml:"domain"`
	Mode               string           `yaml:"mode"`
	Audience           string           `yaml:"audience"`
	AgentLayer         string           `yaml:"agent_layer"`
	Kind               string           `yaml:"kind"`
	Role               string           `yaml:"role"`
	PublishLane        string           `yaml:"publish_lane"`
	PublishPath        string           `yaml:"publish_path"`
	PublishDescription string           `yaml:"publish_description"`
	OwnerEpic          string           `yaml:"owner_epic"`
	SourceOfTruth      []string         `yaml:"source_of_truth"`
	StaleWhen          DocsMapStaleWhen `yaml:"stale_when"`
	Evals              []string         `yaml:"evals"`
}

func loadDocsMap(vaultPath string) (*DocsMap, error) { return nil, nil }
func validateDocsMapConfig(m *DocsMap) []Issue       { return nil }
func defaultDocsMapYAML(date string) string          { return "" }

func (m *DocsMap) Node(id string) (DocsMapNode, bool) { return DocsMapNode{}, false }
func (m *DocsMap) HasDomain(domain string) bool       { return false }
func (n DocsMapNode) SourcePath() string {
	if strings.TrimSpace(n.Page) != "" {
		return n.Page
	}
	return n.ID
}
func (n DocsMapNode) EffectiveMode() string       { return n.Mode }
func (n DocsMapNode) EffectiveAgentLayer() string { return n.AgentLayer }

type v6FreshnessRecord struct {
	Node      string `json:"node"`
	Freshness string `json:"freshness"`
}

type v6KnowledgeDomain struct {
	ID string `json:"id"`
}
type v6KnowledgeNode struct {
	Node    string   `json:"node"`
	Aliases []string `json:"aliases"`
}

type v6KnowledgeIndex struct {
	Domains        []v6KnowledgeDomain `json:"domains"`
	KnowledgeNodes []v6KnowledgeNode   `json:"knowledge_nodes"`
	Freshness      []v6FreshnessRecord `json:"freshness"`
}

var v6FrontmatterOrder = map[string][]string{}

func hasV6Vault(vaultPath string) bool                                            { return false }
func v6IndexVault(vaultPath string) (v6KnowledgeIndex, error)                     { return v6KnowledgeIndex{}, nil }
func addV6ValidationLinkTarget(targets map[string]bool, value string)             {}
func validateV6Vault(vaultPath string, index v6KnowledgeIndex) ([]Issue, []Issue) { return nil, nil }
func writeV6GeneratedIndexes(vaultPath string, quiet bool) error                  { return nil }
func v6TaskProofIssues(data map[string]any, body string) []string                 { return nil }
func v6FreshnessByNode(index v6KnowledgeIndex) map[string]v6FreshnessRecord {
	return map[string]v6FreshnessRecord{}
}
func knowledgeImpactFreshnessIssues(data map[string]any, freshness map[string]v6FreshnessRecord) []string {
	return nil
}
func validateDocsPublicationState(vaultPath string, notes []Note) ([]Issue, []Issue) { return nil, nil }

func statusCmd(args Args) error {
	id := firstNonEmpty(args.String("id"), args.String("_pos0"))
	status := firstNonEmpty(args.String("status"), args.String("_pos1"))
	if args.String("id") == "" && args.String("status") == "" {
		first := strings.ToLower(args.String("_pos0"))
		second := strings.ToUpper(args.String("_pos1"))
		if _, ok := v7TaskStatuses[first]; ok && v7TaskIDPattern.MatchString(second) {
			id = second
			status = first
		}
	}
	args["id"] = id
	args["status"] = status
	return statusV7Cmd(args)
}

func evidenceCmd(args Args) error {
	switch strings.ToLower(args.String("_pos0")) {
	case "add":
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos1"))
		return evidenceV7AddCmd(args)
	case "promote":
		return evidenceV7PromoteCmd(args)
	case "prune":
		return evidenceV7PruneCmd(args)
	default:
		return removedSurfaceError("legacy evidence")
	}
}

func verifyV5Cmd(args Args) error { return removedSurfaceError("legacy verify") }

func isV5Schema(data map[string]any) bool { return false }
func isV6Schema(data map[string]any) bool { return false }
func effectiveNoteKind(data map[string]any) string {
	return firstNonEmpty(stringField(data, "kind"), stringField(data, "type"))
}

func validateV5Note(note Note, ctx validationContext, where string) ([]Issue, []Issue) {
	return nil, nil
}
func validateV6Note(note Note, ctx validationContext, where string) ([]Issue, []Issue) {
	return nil, nil
}

func assertEvidenceGate(data map[string]any, body, id string) error {
	risk := strings.ToLower(stringField(data, "risk"))
	if risk == "medium" || risk == "high" || risk == "critical" {
		if !sectionHasSubstance(body, "## Evidence") {
			return tuskerError(errorEvidenceGate, fmt.Sprintf(`%s: risk "%s" requires substantive "## Evidence" before this transition`, id, risk), withContext(map[string]any{"id": id, "risk": risk}))
		}
	}
	if stringField(data, "type") == "task" && stringField(data, "kind") == "feature" && isUISurface(data["surfaces"]) && (risk == "medium" || risk == "high" || risk == "critical") {
		if !evidenceHasAsset(body) {
			return tuskerError(errorUIDemoMissing, fmt.Sprintf(`%s: UI feature at risk "%s" needs a demo asset (video/gif/screenshot) in "## Evidence"`, id, risk), withContext(map[string]any{"id": id, "risk": risk}))
		}
	}
	return nil
}

func docsImpactResolved(data map[string]any) bool      { return true }
func knowledgeImpactResolved(data map[string]any) bool { return true }

func splitMarkdownTableRow(row string) []string {
	row = strings.Trim(row, "|")
	parts := strings.Split(row, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func firstNonEmptyList(values ...[]string) []string {
	for _, value := range values {
		if len(value) > 0 {
			return value
		}
	}
	return nil
}

func docsNormalizePath(value string) string {
	return filepath.ToSlash(strings.TrimSpace(value))
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

func defaultV5DashboardNote(date string) string {
	return v7DashboardLandingNote()
}

func validateKnowledgeNodePath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "must not be empty"
	}
	if strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") {
		return "must not start or end with /"
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return `must not contain empty, ".", or ".." segments`
		}
		if strings.ContainsAny(segment, `\:`) {
			return "must use portable path segments"
		}
	}
	return ""
}

func renderMarkdownBulletList(values []string) string {
	if len(values) == 0 {
		return "- _None declared._"
	}
	var lines []string
	for _, value := range values {
		lines = append(lines, "- `"+value+"`")
	}
	return strings.Join(lines, "\n")
}

func mapField(data map[string]any, key string) map[string]any {
	switch v := data[key].(type) {
	case map[string]any:
		return v
	case map[any]any:
		out := map[string]any{}
		for key, value := range v {
			out[toString(key)] = value
		}
		return out
	default:
		return nil
	}
}

func hasV7ProjectSkill(vaultPath string) bool {
	return fileExists(filepath.Join(vaultPath, "SKILL.md"))
}

func hasV7KnowledgeDomains(vaultPath string) bool {
	return dirExists(filepath.Join(vaultPath, "knowledge", "domains"))
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	var out []string
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
