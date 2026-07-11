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
	"gopkg.in/yaml.v3"
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
	found, err := discoverVault(mustGetwd())
	if err != nil {
		return "", err
	}
	if found != "" {
		return found, nil
	}
	if allowCreate {
		return filepath.Abs(filepath.Join(mustGetwd(), defaultRepoVaultDir))
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
		if isVaultDir(child) || (fileExists(filepath.Join(dir, "tusker.yaml")) && dirExists(child)) {
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
	configPath := filepath.Join(repoPath, "tusker.yaml")
	if !fileExists(configPath) {
		return ""
	}
	raw, err := readText(configPath)
	if err != nil {
		return ""
	}
	var cfg struct {
		Storage struct {
			Root string `yaml:"root"`
		} `yaml:"storage"`
	}
	if err := yaml.Unmarshal([]byte(raw), &cfg); err != nil {
		return ""
	}
	return strings.TrimSpace(filepath.ToSlash(cfg.Storage.Root))
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
