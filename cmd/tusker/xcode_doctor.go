package main

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	xcodeClassificationInfrastructure = "likely_infrastructure"
	xcodeClassificationCode           = "likely_code"
	xcodeClassificationUnknown        = "unknown"
)

type xcodeDoctorReport struct {
	Classification string               `json:"classification"`
	DryRun         bool                 `json:"dry_run"`
	Cleanup        bool                 `json:"cleanup"`
	Scope          []string             `json:"scope,omitempty"`
	InspectPaths   []string             `json:"inspect_paths"`
	CleanupTargets []xcodeCleanupTarget `json:"cleanup_targets"`
	Findings       []xcodeDoctorFinding `json:"findings"`
	Removed        []string             `json:"removed,omitempty"`
	Refusals       []string             `json:"refusals,omitempty"`
	Recipe         []string             `json:"recipe"`
}

type xcodeDoctorFinding struct {
	ID             string `json:"id"`
	Classification string `json:"classification"`
	Path           string `json:"path"`
	Summary        string `json:"summary"`
}

type xcodeCleanupTarget struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
	Action string `json:"action"`
}

type xcodeFailureSignature struct {
	ID             string
	Classification string
	Summary        string
	Groups         [][]string
}

var xcodeFailureSignatures = []xcodeFailureSignature{
	{
		ID:             "stale_build_db_lock",
		Classification: xcodeClassificationInfrastructure,
		Summary:        "Xcode build database lock or stale build.db state",
		Groups: [][]string{
			{"build.db", "database is locked"},
			{"build database", "database is locked"},
			{"sqlite_busy", "build"},
			{"xcbuilddata", "build.db", "locked"},
		},
	},
	{
		ID:             "build_database_io_inconsistency",
		Classification: xcodeClassificationInfrastructure,
		Summary:        "Xcode build database I/O failure or internal inconsistency",
		Groups: [][]string{
			{"build.db", "sqlite_ioerr"},
			{"build database", "unable to open database file"},
			{"build database", "internal inconsistency"},
			{"xcbuilddata", "i/o error"},
			{"database disk image is malformed", "build"},
		},
	},
	{
		ID:             "supplementary_outputs_corruption",
		Classification: xcodeClassificationInfrastructure,
		Summary:        "XCBuild supplementaryOutputs map corruption",
		Groups: [][]string{
			{"supplementaryoutputs"},
			{"supplementary outputs", "map"},
			{"supplementary outputs", "corrupt"},
		},
	},
	{
		ID:             "code_build_failure",
		Classification: xcodeClassificationCode,
		Summary:        "Compiler, linker, or module failure that looks code-owned",
		Groups: [][]string{
			{"compilec failed"},
			{"compileswift failed"},
			{"swift compiler error"},
			{"undefined symbols for architecture"},
			{"ld: symbol(s) not found"},
			{"no such module"},
			{"use of unresolved identifier"},
		},
	},
}

func xcodeDoctorCmd(args Args) error {
	report, err := buildXcodeDoctorReport(args)
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(report)
		return nil
	}
	printXcodeDoctorReport(report)
	return nil
}

func buildXcodeDoctorReport(args Args) (xcodeDoctorReport, error) {
	cleanup := args.Bool("cleanup")
	dryRun := !cleanup || args.Bool("dry-run")
	scope, err := xcodeScopeNames(args)
	if err != nil {
		return xcodeDoctorReport{}, err
	}
	if cleanup && len(scope) == 0 {
		return xcodeDoctorReport{}, tuskerError(errorInvalidArg, "xcode doctor --cleanup requires --project or --workspace", withHint("pass an explicit .xcodeproj or .xcworkspace path so cleanup can stay scoped"))
	}

	candidates, inspectPaths, refusals, err := xcodeDerivedDataCandidates(args, scope)
	if err != nil {
		return xcodeDoctorReport{}, err
	}
	targets, err := xcodeCleanupTargets(candidates, dryRun)
	if err != nil {
		return xcodeDoctorReport{}, err
	}
	inspectPaths = append(inspectPaths, xcodeExplicitInspectPaths(args)...)

	logPaths, err := xcodeCollectInspectableFiles(args, candidates)
	if err != nil {
		return xcodeDoctorReport{}, err
	}
	inspectPaths = append(inspectPaths, logPaths...)

	findings := []xcodeDoctorFinding{}
	for _, path := range logPaths {
		text, err := readXcodeDiagnosticText(path)
		if err != nil {
			continue
		}
		findings = append(findings, xcodeDetectFindings(path, text)...)
	}

	removed := []string{}
	if cleanup && !dryRun {
		for _, target := range targets {
			if !target.Exists {
				continue
			}
			if err := validateXcodeCleanupTarget(target.Path, candidates); err != nil {
				return xcodeDoctorReport{}, err
			}
			if err := os.RemoveAll(target.Path); err != nil {
				return xcodeDoctorReport{}, err
			}
			removed = append(removed, target.Path)
		}
	}

	report := xcodeDoctorReport{
		Classification: xcodeClassifyFindings(findings),
		DryRun:         dryRun,
		Cleanup:        cleanup,
		Scope:          scope,
		InspectPaths:   uniqueSortedStrings(inspectPaths),
		CleanupTargets: targets,
		Findings:       findings,
		Removed:        uniqueSortedStrings(removed),
		Refusals:       uniqueSortedStrings(refusals),
		Recipe:         xcodeDoctorProofRecipe(),
	}
	return report, nil
}

func xcodeScopeNames(args Args) ([]string, error) {
	values := []string{}
	for _, key := range []string{"project", "workspace"} {
		raw := strings.TrimSpace(args.String(key))
		if raw == "" {
			continue
		}
		ext := strings.ToLower(filepath.Ext(raw))
		if key == "project" && ext != ".xcodeproj" {
			return nil, tuskerError(errorInvalidArg, "--project must point at a .xcodeproj path", withPath(raw))
		}
		if key == "workspace" && ext != ".xcworkspace" {
			return nil, tuskerError(errorInvalidArg, "--workspace must point at a .xcworkspace path", withPath(raw))
		}
		name := strings.TrimSuffix(filepath.Base(raw), filepath.Ext(raw))
		if strings.TrimSpace(name) != "" {
			values = append(values, name)
		}
	}
	return uniqueSortedStrings(values), nil
}

func xcodeDerivedDataCandidates(args Args, scope []string) ([]string, []string, []string, error) {
	var candidates []string
	var inspect []string
	var refusals []string
	if explicit := strings.TrimSpace(args.String("derived-data")); explicit != "" {
		for _, path := range splitCSV(explicit) {
			abs, err := filepath.Abs(path)
			if err != nil {
				return nil, nil, nil, err
			}
			inspect = append(inspect, abs)
			ok, reason := safeXcodeDerivedDataCandidate(abs, scope)
			if !ok {
				return nil, nil, nil, tuskerError(errorInvalidArg, "refusing ambiguous Xcode cleanup path: "+abs, withHint(reason), withPath(abs))
			}
			candidates = append(candidates, abs)
		}
		return uniqueSortedStrings(candidates), uniqueSortedStrings(inspect), refusals, nil
	}

	root := firstNonEmpty(args.String("derived-data-root"), defaultXcodeDerivedDataRoot())
	if strings.TrimSpace(root) == "" {
		return nil, nil, nil, nil
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, nil, nil, err
	}
	inspect = append(inspect, rootAbs)
	if len(scope) == 0 {
		refusals = append(refusals, "cleanup discovery needs --project or --workspace before DerivedData entries are selected")
		return nil, uniqueSortedStrings(inspect), refusals, nil
	}
	entries, err := os.ReadDir(rootAbs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, uniqueSortedStrings(inspect), refusals, nil
		}
		return nil, nil, nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		candidate := filepath.Join(rootAbs, entry.Name())
		if ok, _ := safeXcodeDerivedDataCandidate(candidate, scope); ok {
			candidates = append(candidates, candidate)
		}
	}
	return uniqueSortedStrings(candidates), uniqueSortedStrings(inspect), refusals, nil
}

func xcodeCleanupTargets(candidates []string, dryRun bool) ([]xcodeCleanupTarget, error) {
	targets := []xcodeCleanupTarget{}
	for _, candidate := range candidates {
		for _, rel := range []string{
			filepath.Join("Build", "Intermediates.noindex", "XCBuildData"),
			filepath.Join("Build", "XCBuildData"),
		} {
			path := filepath.Join(candidate, rel)
			exists := dirExists(path)
			action := "skip_missing"
			if exists && dryRun {
				action = "would_remove"
			} else if exists {
				action = "remove"
			}
			targets = append(targets, xcodeCleanupTarget{Path: path, Exists: exists, Action: action})
		}
	}
	sort.SliceStable(targets, func(i, j int) bool { return targets[i].Path < targets[j].Path })
	return targets, nil
}

func xcodeExplicitInspectPaths(args Args) []string {
	paths := []string{}
	for _, key := range []string{"log", "logs", "result-bundle", "result-bundles", "xcresult"} {
		for _, path := range splitCSV(args.String(key)) {
			if abs, err := filepath.Abs(path); err == nil {
				paths = append(paths, abs)
			}
		}
	}
	return paths
}

func xcodeCollectInspectableFiles(args Args, candidates []string) ([]string, error) {
	var paths []string
	for _, key := range []string{"log", "logs"} {
		for _, path := range splitCSV(args.String(key)) {
			abs, err := filepath.Abs(path)
			if err != nil {
				return nil, err
			}
			paths = append(paths, abs)
		}
	}
	for _, key := range []string{"result-bundle", "result-bundles", "xcresult"} {
		for _, path := range splitCSV(args.String(key)) {
			abs, err := filepath.Abs(path)
			if err != nil {
				return nil, err
			}
			collected, err := xcodeReadableFilesUnder(abs, 0)
			if err != nil {
				return nil, err
			}
			paths = append(paths, collected...)
		}
	}
	recentCutoff := time.Now().Add(-time.Duration(xcodeSinceHours(args)) * time.Hour)
	for _, candidate := range candidates {
		for _, rel := range []string{filepath.Join("Logs", "Build"), filepath.Join("Logs", "Test")} {
			logDir := filepath.Join(candidate, rel)
			collected, err := xcodeReadableFilesUnder(logDir, recentCutoff.Unix())
			if err != nil && !os.IsNotExist(err) {
				return nil, err
			}
			paths = append(paths, collected...)
		}
	}
	return uniqueSortedStrings(paths), nil
}

func xcodeReadableFilesUnder(path string, minModUnix int64) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []string{path}, nil
	}
	paths := []string{}
	err = filepath.WalkDir(path, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		if minModUnix > 0 && info.ModTime().Unix() < minModUnix {
			return nil
		}
		if xcodeLooksReadableDiagnosticFile(current, info.Size()) {
			paths = append(paths, current)
		}
		return nil
	})
	return paths, err
}

func xcodeLooksReadableDiagnosticFile(path string, size int64) bool {
	if size <= 0 || size > 8*1024*1024 {
		return false
	}
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".log", ".txt", ".json", ".plist", ".xcactivitylog":
		return true
	default:
		return ext == ""
	}
}

func readXcodeDiagnosticText(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if len(raw) >= 2 && raw[0] == 0x1f && raw[1] == 0x8b {
		reader, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return "", err
		}
		defer reader.Close()
		decoded, err := io.ReadAll(io.LimitReader(reader, 8*1024*1024))
		if err != nil {
			return "", err
		}
		raw = decoded
	}
	return string(raw), nil
}

func xcodeDetectFindings(path, text string) []xcodeDoctorFinding {
	lower := strings.ToLower(text)
	findings := []xcodeDoctorFinding{}
	seen := map[string]bool{}
	for _, signature := range xcodeFailureSignatures {
		if !xcodeSignatureMatches(lower, signature.Groups) || seen[signature.ID] {
			continue
		}
		seen[signature.ID] = true
		findings = append(findings, xcodeDoctorFinding{
			ID:             signature.ID,
			Classification: signature.Classification,
			Path:           path,
			Summary:        signature.Summary,
		})
	}
	return findings
}

func xcodeSignatureMatches(text string, groups [][]string) bool {
	for _, group := range groups {
		matched := true
		for _, needle := range group {
			if !strings.Contains(text, strings.ToLower(needle)) {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func xcodeClassifyFindings(findings []xcodeDoctorFinding) string {
	for _, finding := range findings {
		if finding.Classification == xcodeClassificationInfrastructure {
			return xcodeClassificationInfrastructure
		}
	}
	for _, finding := range findings {
		if finding.Classification == xcodeClassificationCode {
			return xcodeClassificationCode
		}
	}
	return xcodeClassificationUnknown
}

func safeXcodeDerivedDataCandidate(path string, scope []string) (bool, string) {
	if len(scope) == 0 {
		return false, "pass --project or --workspace so DerivedData can be matched by product name"
	}
	clean := filepath.Clean(path)
	base := filepath.Base(clean)
	if strings.EqualFold(base, "DerivedData") {
		return false, "do not pass the global DerivedData root as --derived-data; use --derived-data-root or a scoped project entry"
	}
	if !pathHasSegment(clean, "DerivedData") {
		return false, "cleanup targets must live under an Xcode DerivedData directory"
	}
	for _, name := range scope {
		if strings.EqualFold(base, name) || strings.HasPrefix(strings.ToLower(base), strings.ToLower(name)+"-") {
			return true, ""
		}
	}
	return false, "DerivedData entry does not match the requested project/workspace scope"
}

func validateXcodeCleanupTarget(path string, candidates []string) error {
	clean := filepath.Clean(path)
	if clean == string(filepath.Separator) || clean == "." || strings.EqualFold(filepath.Base(clean), "DerivedData") {
		return tuskerError(errorInvalidArg, "refusing broad Xcode cleanup target: "+clean, withPath(clean))
	}
	if !pathHasSegment(clean, "XCBuildData") {
		return tuskerError(errorInvalidArg, "refusing Xcode cleanup target outside XCBuildData: "+clean, withPath(clean))
	}
	for _, candidate := range candidates {
		if pathWithin(candidate, clean) {
			return nil
		}
	}
	return tuskerError(errorPathEscape, "Xcode cleanup target escapes scoped DerivedData entries: "+clean, withPath(clean))
}

func pathHasSegment(path, segment string) bool {
	for _, part := range strings.Split(filepath.Clean(path), string(filepath.Separator)) {
		if strings.EqualFold(part, segment) {
			return true
		}
	}
	return false
}

func xcodeSinceHours(args Args) int {
	raw := firstNonEmpty(args.String("since-hours"), args.String("recent-hours"))
	if strings.TrimSpace(raw) == "" {
		return 168
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || parsed <= 0 {
		return 168
	}
	return parsed
}

func defaultXcodeDerivedDataRoot() string {
	home := userHomeDir()
	if home == "" {
		return ""
	}
	return filepath.Join(home, "Library", "Developer", "Xcode", "DerivedData")
}

func xcodeDoctorProofRecipe() []string {
	return []string{
		"Use likely_infrastructure when Xcode generated state is corrupt; record the doctor output as blocked proof, not build-green proof.",
		"Run cleanup in dry-run first and verify every listed path is DerivedData/XCBuildData for the intended project or workspace.",
		"After cleanup, rerun the original xcodebuild command; only that rerun can prove code validation.",
	}
}

func printXcodeDoctorReport(report xcodeDoctorReport) {
	fmt.Println("Xcode build-state doctor")
	fmt.Printf("classification: %s\n", report.Classification)
	fmt.Printf("dry-run: %t\n", report.DryRun)
	if len(report.Scope) > 0 {
		fmt.Printf("scope: %s\n", strings.Join(report.Scope, ", "))
	}
	fmt.Println()
	fmt.Println("inspect paths:")
	if len(report.InspectPaths) == 0 {
		fmt.Println("  - none")
	} else {
		for _, path := range report.InspectPaths {
			fmt.Printf("  - %s\n", path)
		}
	}
	fmt.Println()
	fmt.Println("cleanup targets:")
	if len(report.CleanupTargets) == 0 {
		fmt.Println("  - none")
	} else {
		for _, target := range report.CleanupTargets {
			fmt.Printf("  - %s (%s)\n", target.Path, target.Action)
		}
	}
	if len(report.Removed) > 0 {
		fmt.Println()
		fmt.Println("removed:")
		for _, path := range report.Removed {
			fmt.Printf("  - %s\n", path)
		}
	}
	if len(report.Refusals) > 0 {
		fmt.Println()
		fmt.Println("refusals:")
		for _, refusal := range report.Refusals {
			fmt.Printf("  - %s\n", refusal)
		}
	}
	fmt.Println()
	fmt.Println("findings:")
	if len(report.Findings) == 0 {
		fmt.Println("  - none")
	} else {
		for _, finding := range report.Findings {
			fmt.Printf("  - %s: %s (%s)\n", finding.ID, finding.Summary, finding.Path)
		}
	}
	fmt.Println()
	fmt.Println("proof recipe:")
	for _, step := range report.Recipe {
		fmt.Printf("  - %s\n", step)
	}
}

func printXcodeHelp() {
	fmt.Println(`Usage:
  tusker xcode doctor [--project <App.xcodeproj>|--workspace <App.xcworkspace>] [--log <path>[,<path>]] [--result-bundle <path>] [--derived-data-root <path>] [--dry-run] [--cleanup] [--json]
  tusker xcode doctor --project <App.xcodeproj> --derived-data-root ~/Library/Developer/Xcode/DerivedData --dry-run
  tusker xcode doctor --workspace <App.xcworkspace> --cleanup

Purpose:
  Diagnose generated Xcode build-state failures separately from code failures.

Behavior:
  - detects stale build.db locks, build database I/O/internal inconsistency, and supplementaryOutputs map corruption
  - classifies output as likely_infrastructure, likely_code, or unknown
  - dry-run lists exact log/result-bundle paths inspected and DerivedData/XCBuildData cleanup targets
  - cleanup removes only XCBuildData directories under DerivedData entries matching the explicit project/workspace name
  - broad paths such as the global DerivedData root are refused

Proof guardrail:
  Do not claim code validation from this command. If classification is likely_infrastructure,
  record it as blocked infrastructure proof, run scoped cleanup if appropriate, then rerun the original xcodebuild command for actual build-green proof.`)
}

func uniqueSortedStrings(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
