package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

var agentSkillNamePattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
var agentSkillReferencePattern = regexp.MustCompile(`(?:\(|\x60)(references|scripts|assets)/([^\s\)\x60]+)`)

func skillV7DoctorCmd(args Args) (int, error) {
	root, packageMode, err := skillDoctorRoot(args)
	if err != nil {
		return 0, err
	}
	errs, warns := skillDoctorIssues(root, packageMode, args.Bool("strict"))
	if args.Bool("json") {
		emitJSON(map[string]any{
			"ok":           len(errs) == 0,
			"package":      packageMode,
			"root":         root,
			"strict":       args.Bool("strict"),
			"counts":       map[string]any{"errors": len(errs), "warnings": len(warns)},
			"errors":       errs,
			"warnings":     warns,
			"do_not_read":  v7SkillForbiddenReadGlobs(),
			"doctor_scope": v7SkillDoctorScope(packageMode),
		})
		if len(errs) > 0 {
			return 1, nil
		}
		return 0, nil
	}
	if len(warns) > 0 {
		fmt.Println("Warnings:")
		for _, warning := range warns {
			fmt.Printf("- %s\n", formatIssue(warning))
		}
	}
	if len(errs) > 0 {
		fmt.Println("Errors:")
		for _, current := range errs {
			fmt.Printf("- %s\n", formatIssue(current))
		}
		return 1, nil
	}
	if !args.Bool("quiet") {
		fmt.Printf("Skill doctor passed for %s.\n", root)
	}
	return 0, nil
}

func skillDoctorRoot(args Args) (string, bool, error) {
	if pkg := strings.TrimSpace(args.String("package")); pkg != "" {
		abs, err := filepath.Abs(pkg)
		if err != nil {
			return "", true, err
		}
		if resolved, resolveErr := filepath.EvalSymlinks(abs); resolveErr == nil {
			abs = resolved
		}
		return abs, true, nil
	}
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return "", false, err
	}
	return vaultPath, false, nil
}

func skillDoctorIssues(root string, packageMode, strict bool) ([]Issue, []Issue) {
	if packageMode && isAgentSkillPackage(root) {
		return agentSkillPackageIssues(root, strict)
	}
	var errs, warns []Issue
	if !fileExists(filepath.Join(root, "SKILL.md")) {
		errs = append(errs, issue(errorMissingField, "V7 project skill package requires SKILL.md", "SKILL.md", "", nil))
	}
	if !fileExists(filepath.Join(root, "knowledge", "domains")) {
		errs = append(errs, issue(errorMissingField, "V7 project skill package requires knowledge/domains/**", "knowledge/domains", "", nil))
	}
	if skillErrs, skillWarns := validateV7SkillKnowledge(root); len(skillErrs) > 0 || len(skillWarns) > 0 {
		errs = append(errs, skillErrs...)
		warns = append(warns, skillWarns...)
	}
	if noteErrs, noteWarns := validateSkillPackageNotes(root); len(noteErrs) > 0 || len(noteWarns) > 0 {
		errs = append(errs, noteErrs...)
		warns = append(warns, noteWarns...)
	}
	errs = append(errs, skillDoctorForbiddenPaths(root, packageMode)...)
	errs = append(errs, skillDoctorLocalAbsolutePaths(root, packageMode)...)
	if !packageMode {
		repoRoot := v7RepoRoot(root)
		if fileExists(filepath.Join(repoRoot, "go.mod")) || fileExists(filepath.Join(repoRoot, "skill")) {
			for _, rel := range []string{"skills/tusker/SKILL.md", "skills/tusker/README.md", "skills/tusker/references/COMMANDS.md", "skills/tusker/references/WORKFLOW.md"} {
				if !fileExists(filepath.Join(repoRoot, rel)) {
					errs = append(errs, issue(errorMissingField, "repo skill source is missing: "+rel, rel, "", nil))
				}
			}
		}
	}
	if strict {
		errs = append(errs, skillDoctorRouteCollisions(root)...)
	} else {
		warns = append(warns, skillDoctorRouteCollisions(root)...)
	}
	return errs, warns
}

func isAgentSkillPackage(root string) bool {
	data, _, err := parseFrontmatterMustRead(filepath.Join(root, "SKILL.md"))
	return err == nil && strings.TrimSpace(stringField(data, "schema")) == "" && strings.TrimSpace(stringField(data, "name")) != ""
}

func agentSkillPackageIssues(root string, strict bool) ([]Issue, []Issue) {
	var errs, warns []Issue
	skillPath := filepath.Join(root, "SKILL.md")
	data, body, err := parseFrontmatterMustRead(skillPath)
	if err != nil {
		return []Issue{issue(errorInvalidField, "Agent Skills package requires a readable SKILL.md with YAML frontmatter: "+err.Error(), "SKILL.md", "add valid name and description frontmatter", nil)}, nil
	}
	name := strings.TrimSpace(stringField(data, "name"))
	description := strings.TrimSpace(stringField(data, "description"))
	if name == "" {
		errs = append(errs, issue(errorMissingField, "Agent Skills package requires name", "SKILL.md", "set lowercase package name metadata", map[string]any{"field": "name"}))
	} else {
		if !agentSkillNamePattern.MatchString(name) || len(name) > 64 {
			errs = append(errs, issue(errorInvalidField, "Agent Skills name must be 1-64 lowercase letters, numbers, or single hyphens", "SKILL.md", "rename the skill and package directory", map[string]any{"field": "name", "value": name}))
		}
		if filepath.Base(filepath.Clean(root)) != name {
			errs = append(errs, issue("AGENT_SKILL_NAME_PATH_MISMATCH", "Agent Skills name must match its parent directory", "SKILL.md", "move the canonical package to a directory named "+name, map[string]any{"name": name, "directory": filepath.Base(filepath.Clean(root))}))
		}
	}
	if description == "" || utf8.RuneCountInString(description) > 1024 {
		errs = append(errs, issue(errorInvalidField, "Agent Skills description must be 1-1024 characters and state what the skill does and when to use it", "SKILL.md", "write focused discovery metadata with trigger conditions", map[string]any{"field": "description"}))
	}
	if compatibility := strings.TrimSpace(stringField(data, "compatibility")); utf8.RuneCountInString(compatibility) > 500 {
		errs = append(errs, issue(errorInvalidField, "Agent Skills compatibility must be at most 500 characters", "SKILL.md", "shorten compatibility metadata", map[string]any{"field": "compatibility"}))
	}
	if metadata, ok := data["metadata"].(map[string]any); ok {
		for key, value := range metadata {
			if _, ok := value.(string); !ok {
				errs = append(errs, issue("AGENT_SKILL_METADATA_VALUE", "Agent Skills metadata values must be strings", "SKILL.md", "quote metadata value "+key, map[string]any{"field": "metadata." + key}))
			}
		}
	}
	if license := strings.TrimSpace(stringField(data, "license")); license != "" && !strings.ContainsAny(license, " \t") && !fileExists(filepath.Join(root, filepath.Clean(license))) {
		warns = append(warns, issue("AGENT_SKILL_LICENSE_REFERENCE", "Agent Skills license reference does not resolve inside the package", "SKILL.md", "bundle the referenced license file or use a license identifier", map[string]any{"license": license}))
	}
	lineCount := 1 + strings.Count(body, "\n")
	if lineCount > 500 {
		current := issue("AGENT_SKILL_BODY_BUDGET", "Agent Skills SKILL.md exceeds the 500-line progressive-disclosure budget", "SKILL.md", "move detailed material into focused references", map[string]any{"lines": lineCount, "limit": 500})
		if strict {
			errs = append(errs, current)
		} else {
			warns = append(warns, current)
		}
	}
	for _, match := range agentSkillReferencePattern.FindAllStringSubmatch(body, -1) {
		if len(match) != 3 {
			continue
		}
		rel := filepath.ToSlash(filepath.Join(match[1], strings.TrimRight(match[2], ".,;:")))
		if strings.Count(rel, "/") > 2 {
			warns = append(warns, issue("AGENT_SKILL_DEEP_REFERENCE", "Agent Skills references should stay one resource hop from SKILL.md", rel, "route directly to a focused resource", nil))
		}
		if !fileExists(filepath.Join(root, filepath.FromSlash(rel))) {
			errs = append(errs, issue(errorNotFound, "Agent Skills referenced resource is missing: "+rel, rel, "add the resource or remove the stale reference", nil))
		}
	}
	errs = append(errs, skillDoctorForbiddenPaths(root, true)...)
	errs = append(errs, skillDoctorLocalAbsolutePaths(root, true)...)
	return errs, warns
}

func validateSkillPackageNotes(root string) ([]Issue, []Issue) {
	notes, err := listAllNotes(root)
	if err != nil {
		return []Issue{issue(errorInvalidField, "could not list skill package notes: "+err.Error(), root, "", nil)}, nil
	}
	var errs, warns []Issue
	for _, note := range notes {
		if !strings.HasSuffix(stringField(note.Data, "schema"), "/v7") && stringField(note.Data, "schema") != "tusker.knowledge/v7" {
			continue
		}
		noteErrs, noteWarns := validateNote(note, validationContext{RelativePath: note.RelativePath, Basename: filepath.Base(note.AbsolutePath), VaultPath: root})
		errs = append(errs, noteErrs...)
		warns = append(warns, noteWarns...)
	}
	return errs, warns
}

func skillDoctorForbiddenPaths(root string, packageMode bool) []Issue {
	var errs []Issue
	if !packageMode {
		return errs
	}
	for _, rel := range []string{"work", "epics", "evidence", "attempts", "events", "_generated", "_system", "dashboards", "Attachments"} {
		if fileExists(filepath.Join(root, rel)) {
			errs = append(errs, issue("SKILL_FORBIDDEN_PATH", "skill package includes forbidden runtime/source-history path: "+rel, rel, "export a package with the V7 skill exporter", nil))
		}
	}
	return errs
}

func skillDoctorLocalAbsolutePaths(root string, packageMode bool) []Issue {
	var errs []Issue
	scanRoot := root
	if !packageMode {
		scanRoot = filepath.Join(root, "knowledge", "domains")
	}
	_ = filepath.WalkDir(scanRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return nil
		}
		text, err := readText(path)
		if err != nil {
			return nil
		}
		if strings.Contains(text, "/"+"Users/") || strings.Contains(text, "C:\\Users\\") {
			rel := path
			if next, err := filepath.Rel(root, path); err == nil {
				rel = filepath.ToSlash(next)
			}
			errs = append(errs, issue("SKILL_LOCAL_ABSOLUTE_PATH", "skill source contains a local absolute path", rel, "replace local paths with repo-relative paths or external links", nil))
		}
		return nil
	})
	if !packageMode {
		skillPath := filepath.Join(root, "SKILL.md")
		if text, err := readText(skillPath); err == nil && (strings.Contains(text, "/"+"Users/") || strings.Contains(text, "C:\\Users\\")) {
			errs = append(errs, issue("SKILL_LOCAL_ABSOLUTE_PATH", "project skill contains a local absolute path", "SKILL.md", "replace local paths with repo-relative paths or external links", nil))
		}
	}
	return errs
}

func skillDoctorRouteCollisions(root string) []Issue {
	domains, err := listV7ProjectSkillDomains(root)
	if err != nil {
		return []Issue{issue(errorInvalidField, "could not inspect skill routes: "+err.Error(), "knowledge/domains", "", nil)}
	}
	seen := map[string]string{}
	var errs []Issue
	for _, domain := range domains {
		id := stringField(domain.Data, "id")
		title := strings.ToLower(strings.TrimSpace(stringField(domain.Data, "title")))
		if title == "" {
			continue
		}
		if previous, ok := seen[title]; ok {
			errs = append(errs, issue("SKILL_ROUTE_COLLISION", "multiple domains share route title "+title, domain.RelativePath, "", map[string]any{"domain": id, "previous": previous}))
			continue
		}
		seen[title] = id
	}
	return errs
}

func v7SkillDoctorScope(packageMode bool) []string {
	if packageMode {
		return []string{"SKILL.md", "references/**", "scripts/**", "assets/**"}
	}
	return []string{defaultRepoVaultDir + "/SKILL.md", defaultRepoVaultDir + "/knowledge/domains/**", "skills/tusker/**"}
}

func skillV7RouteCmd(args Args) error {
	args["intent"] = firstNonEmpty(args.String("intent"), strings.ReplaceAll(args.String("_pos"), "\n", " "), args.String("_pos0"))
	intent, err := requireArg(args, "intent")
	if err != nil {
		return err
	}
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	routes, err := v7SkillRoutesForIntent(vaultPath, intent)
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "intent": intent, "read": routes, "do_not_read": v7SkillForbiddenReadGlobs()})
		return nil
	}
	for _, route := range routes {
		fmt.Println(route)
	}
	return nil
}

func v7SkillRoutesForIntent(vaultPath, intent string) ([]string, error) {
	domains, err := listV7ProjectSkillDomains(vaultPath)
	if err != nil {
		return nil, err
	}
	type scored struct {
		ID    string
		Score int
	}
	var scoredDomains []scored
	needle := strings.ToLower(intent)
	for _, domain := range domains {
		id := stringField(domain.Data, "id")
		haystack := strings.ToLower(strings.Join([]string{id, stringField(domain.Data, "title"), stringField(domain.Data, "summary"), domain.Body}, "\n"))
		score := 0
		if strings.Contains(needle, id) {
			score += 8
		}
		for _, token := range strings.FieldsFunc(needle, func(r rune) bool { return r < 'a' || r > 'z' }) {
			if len(token) < 3 {
				continue
			}
			if strings.Contains(haystack, token) {
				score++
			}
		}
		if score > 0 {
			scoredDomains = append(scoredDomains, scored{ID: id, Score: score})
		}
	}
	sort.SliceStable(scoredDomains, func(i, j int) bool {
		if scoredDomains[i].Score != scoredDomains[j].Score {
			return scoredDomains[i].Score > scoredDomains[j].Score
		}
		return scoredDomains[i].ID < scoredDomains[j].ID
	})
	routes := []string{"SKILL.md"}
	limit := len(scoredDomains)
	if limit == 0 {
		limit = 1
		scoredDomains = append(scoredDomains, scored{ID: "project", Score: 0})
	}
	if limit > 3 {
		limit = 3
	}
	for _, route := range scoredDomains[:limit] {
		if !fileExists(filepath.Join(vaultPath, "knowledge", "domains", route.ID, "INDEX.md")) {
			continue
		}
		routes = append(routes,
			filepath.ToSlash(filepath.Join("knowledge", "domains", route.ID, "INDEX.md")),
			filepath.ToSlash(filepath.Join("knowledge", "domains", route.ID, "CANON.md")),
		)
	}
	return routes, nil
}

func skillV7PackCmd(args Args) error {
	args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
	if args.String("id") == "" {
		return tuskerError(errorMissingArg, "skill pack requires a task id", withHint("use `tusker skill pack <TASK-ID> --for agent`"))
	}
	return packetV7Cmd(args)
}

func v7SkillForbiddenReadGlobs() []string {
	return []string{
		"work/**",
		"epics/**",
		"evidence/**",
		"attempts/**",
		"events/**",
		"_generated/**",
		"_system/**",
		"dashboards/**",
		"Attachments/**",
		"**/*.log",
		"/" + "Users/**",
	}
}
