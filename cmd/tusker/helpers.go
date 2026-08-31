package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
)

const (
	defaultRepoVaultDir = ".tusker"
	legacyRepoVaultDir  = "tusker"
)

func replaceTemplateTokens(template string, replacements map[string]string) string {
	output := template
	for key, value := range replacements {
		output = strings.ReplaceAll(output, key, value)
	}
	return output
}

func appendTransition(data map[string]any, entry orderedMap) {
	switch current := data["transitions"].(type) {
	case []any:
		data["transitions"] = append(current, entry)
	case []orderedMap:
		data["transitions"] = append(current, entry)
	default:
		data["transitions"] = []any{entry}
	}
}

func orderedTransition(at, kind, from, to, actor, reason string) orderedMap {
	return orderedMap{
		{Key: "at", Value: at},
		{Key: "kind", Value: kind},
		{Key: "from", Value: from},
		{Key: "to", Value: to},
		{Key: "actor", Value: actor},
		{Key: "reason", Value: reason},
	}
}

func appendWorkLogBullet(body, text string) string {
	return appendSectionBullet(body, "## Work log", "- "+text, false)
}

func appendSectionBullet(body, heading, bullet string, appendAtEnd bool) string {
	lines := strings.Split(body, "\n")
	target := strings.TrimSpace(heading)
	headingIdx := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == target {
			headingIdx = i
			break
		}
	}
	if headingIdx == -1 {
		return body
	}
	insertAt := len(lines)
	for i := headingIdx + 1; i < len(lines); i++ {
		if matched, _ := regexp.MatchString(`^#{2,6}\s+`, lines[i]); matched {
			insertAt = i
			break
		}
	}
	if !appendAtEnd {
		firstContent := headingIdx + 1
		for firstContent < insertAt && strings.TrimSpace(lines[firstContent]) == "" {
			firstContent++
		}
		lines = append(lines[:firstContent], append([]string{bullet}, lines[firstContent:]...)...)
	} else {
		for insertAt > headingIdx+1 && strings.TrimSpace(lines[insertAt-1]) == "" {
			insertAt--
		}
		lines = append(lines[:insertAt], append([]string{bullet}, lines[insertAt:]...)...)
	}
	return strings.Join(lines, "\n")
}

func replaceSection(body, heading, newContent string) string {
	lines := strings.Split(body, "\n")
	target := strings.TrimSpace(heading)
	headingIdx := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == target {
			headingIdx = i
			break
		}
	}
	if headingIdx == -1 {
		return body
	}
	endIdx := len(lines)
	for i := headingIdx + 1; i < len(lines); i++ {
		if matched, _ := regexp.MatchString(`^#{2,6}\s+`, lines[i]); matched {
			endIdx = i
			break
		}
	}
	prefix := strings.Join(lines[:headingIdx+1], "\n")
	suffix := strings.Join(lines[endIdx:], "\n")
	return prefix + "\n\n" + newContent + "\n\n" + suffix
}

func sectionContent(body, heading string) string {
	lines := strings.Split(body, "\n")
	target := strings.TrimSpace(heading)
	headingIdx := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == target {
			headingIdx = i
			break
		}
	}
	if headingIdx == -1 {
		return ""
	}
	endIdx := len(lines)
	for i := headingIdx + 1; i < len(lines); i++ {
		if matched, _ := regexp.MatchString(`^#{2,6}\s+`, lines[i]); matched {
			endIdx = i
			break
		}
	}
	content := strings.Join(lines[headingIdx+1:endIdx], "\n")
	return strings.TrimSpace(stripHTMLComments(content))
}

func stripHTMLComments(text string) string {
	commentRegex := regexp.MustCompile(`(?s)<!--.*?-->`)
	cleaned := commentRegex.ReplaceAllString(text, "")
	lines := strings.Split(cleaned, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func resolveVaultPath(args Args, allowCreate bool) (string, error) {
	if explicit := args.String("vault"); explicit != "" {
		return filepath.Abs(explicit)
	}
	startDir := mustGetwd()
	project, registered, discoveryErr := discoverRegisteredProjectVault(startDir)
	if args.Bool("use-project-vault") {
		if discoveryErr != nil {
			return "", discoveryErr
		} else if registered {
			return project.VaultRoot, nil
		}
	}
	found, err := discoverVault(startDir)
	if err != nil {
		return "", err
	}
	if discoveryErr != nil {
		if allowCreate && args.Bool("isolated-vault") {
			return filepath.Abs(filepath.Join(startDir, defaultRepoVaultDir))
		}
		return "", discoveryErr
	}
	if registered {
		if args.Bool("use-project-vault") {
			return project.VaultRoot, nil
		}
		if allowCreate && args.Bool("isolated-vault") {
			return filepath.Abs(filepath.Join(startDir, defaultRepoVaultDir))
		}
		if found != "" && sameCanonicalProjectPath(found, project.VaultRoot) {
			return found, nil
		}
		return "", registeredProjectVaultError(startDir, project)
	}
	if found != "" {
		if vaultIsExplicitlyConfigured(startDir, found) || vaultMatchesCurrentGitWorktree(startDir, found) {
			return found, nil
		}
		found = ""
	}
	if allowCreate {
		return filepath.Abs(filepath.Join(startDir, defaultRepoVaultDir))
	}
	return "", tuskerError(
		errorMissingArg,
		"No Tusker vault found.\n\nRecommended:\n  tusker init --yes\n\nOther option:\n  tusker --vault <path>    # use an existing vault",
		withHint("Run `tusker init --yes` for setup, or pass --vault <path> for an existing vault."),
		withContext(map[string]any{
			"arg":            "--vault",
			"cwd":            mustGetwd(),
			"repo_wiring":    "tusker init --yes",
			"existing_vault": "--vault <path>",
		}),
	)
}

func vaultIsExplicitlyConfigured(startDir, vaultPath string) bool {
	repoRoot, err := gitFactOutput(startDir, "rev-parse", "--show-toplevel")
	if err != nil || strings.TrimSpace(repoRoot) == "" {
		return false
	}
	configured := configuredVaultPathFromRepo(canonicalProjectPath(repoRoot))
	return configured != "" && sameCanonicalProjectPath(configured, vaultPath)
}

// vaultMatchesCurrentGitWorktree rejects an unrelated ancestor .tusker that
// discoverVault may encounter while walking upward from a linked worktree.
// Non-Git workspaces retain the historical discovery behavior.
func vaultMatchesCurrentGitWorktree(startDir, vaultPath string) bool {
	repoRoot, err := gitFactOutput(startDir, "rev-parse", "--show-toplevel")
	if err != nil || strings.TrimSpace(repoRoot) == "" {
		return true
	}
	repoRoot = canonicalProjectPath(repoRoot)
	currentCommon, err := gitCommonDirectory(repoRoot)
	if err != nil {
		return true
	}
	candidateRoot, err := gitFactOutput(filepath.Dir(vaultPath), "rev-parse", "--show-toplevel")
	if err != nil || strings.TrimSpace(candidateRoot) == "" {
		// An explicitly configured external vault is still valid. An ancestor
		// directory that is not a repository cannot be trusted implicitly.
		return pathWithinRoot(repoRoot, vaultPath)
	}
	candidateCommon, err := gitCommonDirectory(canonicalProjectPath(candidateRoot))
	return err == nil && candidateCommon == currentCommon
}

func pathWithinRoot(root, target string) bool {
	rel, err := filepath.Rel(canonicalProjectPath(root), canonicalProjectPath(target))
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// discoverRegisteredProjectVault bridges linked worktrees to the one vault
// registered for their project. A worktree has its own files, but Git's common
// directory and the project_id in tusker.yaml are shared identity anchors.
func discoverRegisteredProjectVault(startDir string) (RegisteredProject, bool, error) {
	repoRoot, err := gitFactOutput(startDir, "rev-parse", "--show-toplevel")
	if err != nil || strings.TrimSpace(repoRoot) == "" {
		return RegisteredProject{}, false, nil
	}
	repoRoot = canonicalProjectPath(repoRoot)
	commonDir, err := gitCommonDirectory(repoRoot)
	if err != nil {
		return RegisteredProject{}, false, nil
	}
	resolved, err := resolveTuskerConfigForRepo(repoRoot, true)
	if err != nil {
		return RegisteredProject{}, false, registeredProjectConfigInvalidError(startDir, repoRoot, err)
	}
	projectID := strings.TrimSpace(resolved.Config.ProjectID)
	if projectID == "" {
		return RegisteredProject{}, false, nil
	}
	stateRoot := DefaultStateRoot()
	store, missing, err := openRuntimeStoreReadOnly(stateRoot)
	if err != nil {
		return RegisteredProject{}, false, registeredProjectVaultLookupUnavailableError(startDir, projectID, stateRoot, err)
	}
	if missing {
		return RegisteredProject{}, false, nil
	}
	defer store.Close()
	projects, err := store.ListProjects()
	if err != nil {
		return RegisteredProject{}, false, registeredProjectVaultLookupUnavailableError(startDir, projectID, stateRoot, err)
	}
	// ponytail: linear scan and one Git probe per registration; index by common
	// directory only if registry size makes discovery measurably slow.
	var identityMatches []RegisteredProject
	var matches []RegisteredProject
	for _, candidate := range projects {
		if !registeredProjectConfigIdentityMatches(candidate, projectID) {
			continue
		}
		candidate.RepoRoot = canonicalProjectPath(candidate.RepoRoot)
		candidate.VaultRoot = canonicalProjectPath(candidate.VaultRoot)
		candidate.WorkflowPath = workflowPath(candidate.VaultRoot)
		identityMatches = append(identityMatches, candidate)
		candidateCommon, commonErr := gitCommonDirectory(candidate.RepoRoot)
		if commonErr == nil && candidateCommon == commonDir && dirExists(candidate.VaultRoot) {
			matches = append(matches, candidate)
		}
	}
	if len(matches) > 1 {
		vaults := make([]string, 0, len(matches))
		for _, match := range matches {
			vaults = append(vaults, match.VaultRoot)
		}
		return RegisteredProject{}, false, tuskerError(
			errorConfigInvalid,
			fmt.Sprintf("multiple registered Tusker vaults match project %s in this Git repository", projectID),
			withHint("run `tusker projects list` and reconcile duplicate registrations before initializing a vault"),
			withContext(map[string]any{"project_id": projectID, "git_common_dir": commonDir, "matching_vaults": vaults}),
		)
	}
	if len(matches) == 1 {
		return matches[0], true, nil
	}
	if len(identityMatches) > 0 {
		return RegisteredProject{}, false, staleRegisteredProjectVaultError(startDir, projectID, commonDir, identityMatches)
	}
	return RegisteredProject{}, false, nil
}

func registeredProjectConfigInvalidError(startDir, repoRoot string, cause error) error {
	return tuskerError(
		errorConfigInvalid,
		fmt.Sprintf("cannot resolve Tusker project config for Git worktree %s: %v", repoRoot, cause),
		withHint("repair the worktree's tusker.yaml/project config before initializing a vault; Tusker will not create a duplicate graph from malformed identity"),
		withContext(map[string]any{
			"cwd":       startDir,
			"repo_root": repoRoot,
			"config":    filepath.Join(repoRoot, legacyTuskerConfigName),
		}),
	)
}

func registeredProjectVaultLookupUnavailableError(startDir, projectID, stateRoot string, cause error) error {
	return tuskerError(
		errorConfigInvalid,
		fmt.Sprintf("cannot inspect the Tusker project registry for project %s: %v", projectID, cause),
		withHint("repair the runtime registry, or explicitly pass `tusker init --isolated-vault` to create an intentionally separate graph"),
		withContext(map[string]any{
			"cwd":            startDir,
			"project_id":     projectID,
			"state_root":     stateRoot,
			"isolated_vault": "--isolated-vault",
		}),
	)
}

func registeredProjectConfigIdentityMatches(project RegisteredProject, identity string) bool {
	identity = strings.TrimSpace(identity)
	if identity == "" {
		return false
	}
	for _, candidate := range []string{project.ProjectID, project.ProjectKey, project.Name, registeredProjectLabel(project)} {
		if strings.EqualFold(strings.TrimSpace(candidate), identity) {
			return true
		}
	}
	return false
}

func gitCommonDirectory(repoRoot string) (string, error) {
	commonDir, err := gitFactOutput(repoRoot, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(repoRoot, commonDir)
	}
	return canonicalProjectPath(commonDir), nil
}

func registeredProjectVaultError(startDir string, project RegisteredProject) error {
	identity := registeredProjectLabel(project)
	return tuskerError(
		errorMissingArg,
		fmt.Sprintf("No Tusker vault found in this worktree. Project %s is registered to the canonical vault-owning checkout %s (%s) (runtime ID %s).", identity, project.RepoRoot, project.VaultRoot, project.ProjectID),
		withHint(fmt.Sprintf("Use the canonical project vault: `tusker <command> --vault %s` (or `tusker <command> --use-project-vault`). To create a second graph, explicitly pass `tusker init --isolated-vault` from %s.", project.VaultRoot, startDir)),
		withContext(map[string]any{
			"arg":                 "--vault",
			"cwd":                 startDir,
			"project_id":          identity,
			"runtime_project_id":  project.ProjectID,
			"project_key":         project.ProjectKey,
			"project_name":        project.Name,
			"canonical_repo_root": project.RepoRoot,
			"canonical_vault":     project.VaultRoot,
			"use_project_vault":   "--use-project-vault",
			"isolated_vault":      "--isolated-vault",
		}),
	)
}

func duplicateProjectVaultInitError(startDir, target string, project RegisteredProject) error {
	identity := registeredProjectLabel(project)
	return tuskerError(
		errorInvalidTransition,
		fmt.Sprintf("refusing to create a second Tusker vault for project %s (runtime ID %s): canonical vault is %s (owned by %s), requested target is %s", identity, project.ProjectID, project.VaultRoot, project.RepoRoot, target),
		withHint(fmt.Sprintf("use `tusker <command> --vault %s` (or `tusker <command> --use-project-vault`) or explicitly confirm an isolated graph with `tusker init --isolated-vault --yes`", project.VaultRoot)),
		withContext(map[string]any{
			"cwd":                 startDir,
			"project_id":          identity,
			"runtime_project_id":  project.ProjectID,
			"project_key":         project.ProjectKey,
			"project_name":        project.Name,
			"canonical_repo_root": project.RepoRoot,
			"canonical_vault":     project.VaultRoot,
			"requested_vault":     target,
			"isolated_vault":      "--isolated-vault",
		}),
	)
}

func staleRegisteredProjectVaultError(startDir, projectID, commonDir string, projects []RegisteredProject) error {
	paths := make([]string, 0, len(projects))
	for _, project := range projects {
		paths = append(paths, firstNonEmpty(project.VaultRoot, project.RepoRoot))
	}
	return tuskerError(
		errorConfigInvalid,
		fmt.Sprintf("registered Tusker project identity %s has no usable canonical vault for this Git worktree", projectID),
		withHint("repair the matching registration with `tusker projects rebind` or `tusker projects prune`; Tusker will not initialize a second vault implicitly"),
		withContext(map[string]any{
			"cwd":                 startDir,
			"project_id":          projectID,
			"git_common_dir":      commonDir,
			"matching_registries": paths,
			"isolated_vault":      "--isolated-vault",
		}),
	)
}

func discoverVault(startDir string) (string, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", err
	}
	for {
		if isVaultDir(dir) {
			return dir, nil
		}
		if configured := configuredVaultPathFromRepo(dir); configured != "" {
			return configured, nil
		}
		child := filepath.Join(dir, defaultRepoVaultDir)
		if isVaultDir(child) || (fileExists(managedTuskerConfigPath(child)) && dirExists(child)) || (fileExists(legacyTuskerConfigPath(dir)) && dirExists(child)) {
			return child, nil
		}
		legacyChild := filepath.Join(dir, legacyRepoVaultDir)
		if isVaultDir(legacyChild) {
			return legacyChild, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil
		}
		dir = parent
	}
}

func configuredVaultPathFromRepo(repoPath string) string {
	configured := configuredVaultRoot(repoPath)
	if configured == "" {
		return ""
	}
	if filepath.IsAbs(configured) {
		if isVaultDir(configured) || dirExists(configured) {
			return configured
		}
		return ""
	}
	vaultPath := filepath.Join(repoPath, filepath.FromSlash(configured))
	if isVaultDir(vaultPath) || dirExists(vaultPath) {
		return vaultPath
	}
	return ""
}

func configuredVaultRoot(repoPath string) string {
	vaultPath := filepath.Join(repoPath, defaultRepoVaultDir)
	resolved, err := resolveTuskerConfigForPaths(repoPath, vaultPath, true)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(filepath.ToSlash(resolved.Config.Storage.Root))
}

func vaultDisplayRoot(vaultPath string) string {
	if strings.TrimSpace(vaultPath) == "" {
		return defaultRepoVaultDir
	}
	repoRoot := filepath.Dir(vaultPath)
	if rel := relativeFromRepo(repoRoot, vaultPath); rel != "" {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(filepath.Base(vaultPath))
}

func vaultDisplayPath(vaultPath, relative string) string {
	relative = strings.TrimLeft(filepath.ToSlash(relative), "/")
	root := vaultDisplayRoot(vaultPath)
	if relative == "" {
		return root
	}
	return filepath.ToSlash(filepath.Join(root, relative))
}

func isVaultDir(dir string) bool {
	return fileExists(filepath.Join(dir, "WORKFLOW.md")) ||
		fileExists(filepath.Join(dir, "SKILL.md")) ||
		dirExists(filepath.Join(dir, "work")) ||
		dirExists(filepath.Join(dir, "knowledge", "domains"))
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func requireArg(args Args, name string) (string, error) {
	value := strings.TrimSpace(args.String(name))
	if value == "" {
		return "", tuskerError(errorMissingArg, "Missing required argument --"+name, withContext(map[string]any{"arg": "--" + name}))
	}
	return value, nil
}

func requirePathArg(args Args, name string) (string, error) {
	value, err := requireArg(args, name)
	if err != nil {
		return "", err
	}
	return filepath.Abs(value)
}

func readText(filePath string) (string, error) {
	raw, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func writeText(filePath, content string) error {
	if err := ensureDir(filepath.Dir(filePath)); err != nil {
		return err
	}
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		return err
	}
	invalidateCachedNote(filePath)
	recordCLIVaultMutation(filePath)
	return nil
}

func writeJSON(filePath string, payload any) error {
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return writeText(filePath, string(raw)+"\n")
}

func ensureDir(dir string) error {
	return os.MkdirAll(dir, 0o755)
}

func fileExists(filePath string) bool {
	_, err := os.Stat(filePath)
	return err == nil
}

func copyFile(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := ensureDir(filepath.Dir(target)); err != nil {
		return err
	}
	out, err := os.Create(target)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

func parseFrontmatterMustRead(filePath string) (map[string]any, string, error) {
	text, err := readText(filePath)
	if err != nil {
		return nil, "", err
	}
	return parseFrontmatter(text)
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func splitCSVLinks(value string) []string {
	parts := splitCSV(value)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.HasPrefix(part, "[[") && strings.HasSuffix(part, "]]") {
			out = append(out, part)
		} else if strings.HasPrefix(part, "http://") || strings.HasPrefix(part, "https://") {
			out = append(out, part)
		} else {
			out = append(out, "[["+part+"]]")
		}
	}
	return out
}

func parseBooleanArg(value string, fallback bool) (bool, error) {
	if value == "" {
		return fallback, nil
	}
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "true", "1", "yes", "y":
		return true, nil
	case "false", "0", "no", "n":
		return false, nil
	default:
		return false, fmt.Errorf("expected boolean-like value, got: %s", value)
	}
}

func stringField(data map[string]any, key string) string {
	return strings.TrimSpace(toString(data[key]))
}

func boolField(data map[string]any, key string) bool {
	return boolValue(data[key])
}

func intField(data map[string]any, key string) int {
	return intValue(data[key])
}

func toString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		if float64(int(v)) == v {
			return strconv.Itoa(int(v))
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	default:
		return fmt.Sprint(value)
	}
}

func stringValue(value any) string { return toString(value) }

func boolValue(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		parsed, _ := parseBooleanArg(v, false)
		return parsed
	case int:
		return v != 0
	case float64:
		return v != 0
	default:
		return false
	}
}

func intValue(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case string:
		return atoiSafe(v)
	default:
		return 0
	}
}

func todayISO() string {
	return time.Now().UTC().Format("2006-01-02")
}

func padNumber(value int) string {
	return fmt.Sprintf("%04d", value)
}

func defaultActorName() string {
	return firstNonEmpty(os.Getenv("TUSKER_ACTOR"), os.Getenv("USER"), os.Getenv("LOGNAME"), "automation")
}

func suffixReason(reason string) string {
	if strings.TrimSpace(reason) == "" {
		return ""
	}
	return " — " + strings.TrimSpace(reason)
}

func fallback(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func nilIfEmpty(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func mustGetwd() string {
	cwd, _ := os.Getwd()
	return cwd
}

func isTTY(file *os.File) bool {
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

func capitalize(value string) string {
	if value == "" {
		return ""
	}
	return strings.ToUpper(value[:1]) + value[1:]
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func pluralY(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}

func countStatus(items []map[string]any, status string) int {
	count := 0
	for _, item := range items {
		if stringValue(item["status"]) == status {
			count++
		}
	}
	return count
}

func countKind(items []map[string]any, kind string) int {
	count := 0
	for _, item := range items {
		if stringValue(item["kind"]) == kind {
			count++
		}
	}
	return count
}

func countNotPublished(items []map[string]any) int {
	count := 0
	for _, item := range items {
		if stringValue(item["status"]) != "published" {
			count++
		}
	}
	return count
}

func atoiSafe(value string) int {
	n, _ := strconv.Atoi(value)
	return n
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func containsString(items []string, needle string) bool {
	for _, item := range items {
		if item == needle {
			return true
		}
	}
	return false
}

func userHomeDir() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return home
	}
	if current, err := user.Current(); err == nil && current.HomeDir != "" {
		return current.HomeDir
	}
	return ""
}

func unixWritable(dir string) bool {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return false
	}
	testPath := filepath.Join(dir, ".tusker-write-test")
	if err := os.WriteFile(testPath, []byte("ok"), 0o644); err != nil {
		return false
	}
	_ = os.Remove(testPath)
	return true
}

func looksLikeGoBuildCache(path string) bool {
	return strings.Contains(path, string(filepath.Separator)+"go-build"+string(filepath.Separator)) ||
		strings.Contains(path, string(filepath.Separator)+"Library"+string(filepath.Separator)+"Caches"+string(filepath.Separator)+"go-build"+string(filepath.Separator))
}

func newRecordID() string {
	entropy := ulid.Monotonic(rand.Reader, 0)
	return ulid.MustNew(ulid.Timestamp(time.Now().UTC()), entropy).String()
}
