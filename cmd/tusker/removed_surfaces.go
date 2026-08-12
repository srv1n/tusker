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

func domainListCmd(args Args) error   { return domainV7ListCmd(args) }
func domainShowCmd(args Args) error   { return domainV7ShowCmd(args) }
func domainNewCmd(args Args) error    { return newV7Domain(args) }
func domainCanonCmd(args Args) error  { return domainV7CanonCmd(args) }
func knowledgeNewCmd(args Args) error { return knowledgeV7NewCmd(args) }

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
