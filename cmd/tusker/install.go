package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	skillbundle "tusker/skills/tusker"
)

const (
	currentSkillInstallDir = "tusker"
	skillInstallModeCopy   = "copy"
	skillInstallModeLink   = "symlink"
)

type repoPointerUpdate struct {
	path   string
	action string
}

type skillInstallResult struct {
	path string
	mode string
}

func syncRepoContract(args Args) error {
	repoPath, err := requirePathArg(args, "repo")
	if err != nil {
		return err
	}
	overwrite := args.Bool("force")
	var report []string
	if err := writeEmbeddedTree("repo-contract", repoPath, overwrite, &report); err != nil {
		return err
	}
	if path, created, err := ensureFeedbackReadmeForRepoVault(repoPath, args.String("vault")); err != nil {
		return err
	} else if created {
		report = append(report, "Created "+path)
	}
	pointerUpdates, err := upsertRepoTuskerPointers(repoPath, args.String("vault"))
	if err != nil {
		return err
	}
	if warning := repoAgentGuidanceFlatteningWarning(repoPath, args.String("vault"), pointerUpdates); warning != "" {
		report = append(report, "Warning: "+warning)
	}
	var pointerReport []string
	for _, update := range pointerUpdates {
		pointerReport = append(pointerReport, fmt.Sprintf("%s %s", capitalize(update.action), update.path))
	}
	for _, line := range append(report, pointerReport...) {
		fmt.Println(line)
	}
	if len(report) == 0 && len(pointerReport) == 0 {
		fmt.Printf("No repo-contract files changed in %s\n", repoPath)
	}
	return nil
}

func updateCmd(args Args) error {
	return updateCmdWithBinaryPreflight(args, preflightInstallBinary)
}

func updateCmdWithBinaryPreflight(args Args, preflight func(Args) error) error {
	if args.Bool("help") {
		printUpdateHelp()
		return nil
	}
	if !args.Bool("no-bin") {
		if err := preflight(args); err != nil {
			return err
		}
	}

	updatedSkills := []string{}
	destinations := []string{}
	repoRoot := ""
	if !args.Bool("repo-only") {
		destinations = existingUserSkillDestinations()
	}
	if repoPath := strings.TrimSpace(args.String("repo")); repoPath != "" {
		var err error
		repoRoot, err = filepath.Abs(repoPath)
		if err != nil {
			return err
		}
		destinations = append(destinations,
			filepath.Join(repoRoot, ".agents", "skills", currentSkillInstallDir),
			filepath.Join(repoRoot, ".claude", "skills", currentSkillInstallDir),
		)
	} else if args.Bool("repo-only") {
		return tuskerError(errorMissingArg, "--repo is required with --repo-only", withContext(map[string]any{"arg": "--repo"}))
	}
	destinations = uniqueInstallDestinations(destinations)

	installedSkillModes := map[string]string{}
	for _, destination := range destinations {
		mode, err := skillInstallModeForDestination(args, destination, repoRoot)
		if err != nil {
			return err
		}
		if err := installSkillPayloadWithModeFrom(destination, mode, args.String("source")); err != nil {
			return err
		}
		updatedSkills = append(updatedSkills, destination)
		installedSkillModes[destination] = mode
	}

	pointerUpdates := []repoPointerUpdate{}
	feedbackReadme := ""
	guidanceWarning := ""
	if repoRoot != "" {
		var err error
		pointerUpdates, err = upsertRepoTuskerPointers(repoRoot, "")
		if err != nil {
			return err
		}
		guidanceWarning = repoAgentGuidanceFlatteningWarning(repoRoot, "", pointerUpdates)
		path, created, err := ensureFeedbackReadmeForRepo(repoRoot)
		if err != nil {
			return err
		}
		if created {
			feedbackReadme = path
		}
	}

	binaryUpdated := false
	if !args.Bool("no-bin") {
		if err := installBinarySymlink(args); err != nil {
			return err
		}
		binaryUpdated = true
	}

	if args.Bool("json") {
		emitJSON(map[string]any{
			"ok":                true,
			"binary_updated":    binaryUpdated,
			"updated_skills":    updatedSkills,
			"skill_modes":       installedSkillModes,
			"updated_pointers":  repoPointerUpdatePaths(pointerUpdates),
			"feedback_readme":   nullIfEmptyString(feedbackReadme),
			"guidance_warning":  nullIfEmptyString(guidanceWarning),
			"skill_source":      nullIfEmptyString(args.String("skill-source")),
			"skill_source_kind": nullIfEmptyString(args.String("skill-source-kind")),
		})
		return nil
	}

	for _, destination := range updatedSkills {
		fmt.Printf("Updated Tusker skill at %s (%s)\n", destination, installedSkillModes[destination])
	}
	for _, update := range pointerUpdates {
		fmt.Printf("%s %s\n", capitalize(update.action), update.path)
	}
	if feedbackReadme != "" {
		fmt.Printf("Created %s\n", feedbackReadme)
	}
	if guidanceWarning != "" {
		fmt.Printf("Warning: %s\n", guidanceWarning)
	}
	if len(updatedSkills) == 0 {
		fmt.Println("No existing user skill installs found to refresh. Pass `--repo <path>` for repo-local skills.")
	}
	return nil
}

func installCmd(args Args) error {
	if args.Bool("help") {
		printInstallHelp()
		return nil
	}

	installedSkills := []string{}
	destinations := []string{}
	repoPath := strings.TrimSpace(args.String("repo"))
	repoRoot := ""
	if repoPath == "" || args.Bool("refresh-existing-user-skills") {
		destinations = existingUserSkillDestinations()
	}
	if args.Bool("codex-user") {
		destinations = append(destinations, defaultCodexUserSkillDestination())
	}
	if args.Bool("claude-user") {
		destinations = append(destinations, filepath.Join(userHomeDir(), ".claude", "skills", currentSkillInstallDir))
	}
	if repoPath != "" {
		var err error
		repoRoot, err = filepath.Abs(repoPath)
		if err != nil {
			return err
		}
		destinations = append(destinations,
			filepath.Join(repoRoot, ".agents", "skills", currentSkillInstallDir),
			filepath.Join(repoRoot, ".claude", "skills", currentSkillInstallDir),
		)
	}
	destinations = uniqueInstallDestinations(destinations)

	installedSkillModes := map[string]string{}
	for _, destination := range destinations {
		mode, err := skillInstallModeForDestination(args, destination, repoRoot)
		if err != nil {
			return err
		}
		if err := installSkillPayloadWithModeFrom(destination, mode, args.String("source")); err != nil {
			return err
		}
		installedSkills = append(installedSkills, destination)
		installedSkillModes[destination] = mode
	}

	pointerUpdates := []repoPointerUpdate{}
	feedbackReadme := ""
	guidanceWarning := ""
	if repoRoot != "" {
		var err error
		pointerUpdates, err = upsertRepoTuskerPointers(repoRoot, "")
		if err != nil {
			return err
		}
		guidanceWarning = repoAgentGuidanceFlatteningWarning(repoRoot, "", pointerUpdates)
		path, created, err := ensureFeedbackReadmeForRepo(repoRoot)
		if err != nil {
			return err
		}
		if created {
			feedbackReadme = path
		}
	}

	binaryInstalled := false
	if !args.Bool("no-bin") {
		if err := installBinarySymlink(args); err != nil {
			return err
		}
		binaryInstalled = true
	}

	if args.Bool("json") {
		emitJSON(map[string]any{
			"ok":                true,
			"binary_installed":  binaryInstalled,
			"installed_skills":  installedSkills,
			"skill_modes":       installedSkillModes,
			"updated_pointers":  repoPointerUpdatePaths(pointerUpdates),
			"feedback_readme":   nullIfEmptyString(feedbackReadme),
			"guidance_warning":  nullIfEmptyString(guidanceWarning),
			"skill_source":      nullIfEmptyString(args.String("skill-source")),
			"skill_source_kind": nullIfEmptyString(args.String("skill-source-kind")),
		})
		return nil
	}

	for _, destination := range installedSkills {
		fmt.Printf("Installed Tusker skill at %s (%s)\n", destination, installedSkillModes[destination])
	}
	for _, update := range pointerUpdates {
		fmt.Printf("%s %s\n", capitalize(update.action), update.path)
	}
	if feedbackReadme != "" {
		fmt.Printf("Created %s\n", feedbackReadme)
	}
	if guidanceWarning != "" {
		fmt.Printf("Warning: %s\n", guidanceWarning)
	}
	if len(installedSkills) == 0 && args.Bool("no-bin") {
		fmt.Println("No install destinations selected.")
	}
	return nil
}

func uniqueInstallDestinations(values []string) []string {
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || containsString(out, value) {
			continue
		}
		out = append(out, value)
	}
	return out
}

func defaultCodexUserSkillDestination() string {
	return filepath.Join(userHomeDir(), ".agents", "skills", currentSkillInstallDir)
}

func existingUserSkillDestinations() []string {
	candidates := []string{
		defaultCodexUserSkillDestination(),
		filepath.Join(userHomeDir(), ".codex", "skills", currentSkillInstallDir),
		filepath.Join(userHomeDir(), ".claude", "skills", currentSkillInstallDir),
	}
	var destinations []string
	for _, candidate := range candidates {
		if fileExists(candidate) {
			destinations = append(destinations, candidate)
		}
	}
	return destinations
}

func installSkillPayload(destination string) error {
	return installSkillPayloadWithMode(destination, skillInstallModeCopy)
}

func installSkillPayloadWithMode(destination, mode string) error {
	return installSkillPayloadWithModeFrom(destination, mode, "")
}

func installSkillPayloadWithModeFrom(destination, mode, sourceArg string) error {
	switch mode {
	case skillInstallModeCopy:
		return installSkillPayloadCopyFrom(destination, sourceArg)
	case skillInstallModeLink:
		return installSkillPayloadSymlink(destination, sourceArg)
	default:
		return tuskerError(errorInvalidArg, "invalid skill install mode: "+mode, withHint("Use --skill-mode copy or --skill-mode symlink."))
	}
}

func installSkillPayloadCopy(destination string) error {
	return installSkillPayloadCopyFrom(destination, "")
}

func installSkillPayloadCopyFrom(destination, sourceArg string) error {
	entries, contract, sourceKind, sourceIdentity, err := skillCopySource(sourceArg)
	if err != nil {
		return err
	}
	return replaceSkillInstallDestination(destination, func(stage string) error {
		for _, entry := range entries {
			target := filepath.Join(stage, filepath.FromSlash(entry.Relative))
			if err := writeText(target, entry.Content); err != nil {
				return err
			}
		}
		return writeSkillMaterializationProvenanceWithContract(stage, sourceKind, sourceIdentity, contract)
	})
}

func skillCopySource(sourceArg string) ([]skillbundle.PayloadEntry, factoryIntakeContractProvenance, string, string, error) {
	if strings.TrimSpace(sourceArg) == "" {
		entries, err := skillbundle.PayloadEntries()
		if err != nil {
			return nil, factoryIntakeContractProvenance{}, "", "", err
		}
		contract, err := embeddedFactoryIntakeContractProvenance()
		return entries, contract, "embedded", portableSkillSourceIdentity("embedded"), err
	}
	source, err := canonicalSkillSourceDir(sourceArg)
	if err != nil {
		return nil, factoryIntakeContractProvenance{}, "", "", err
	}
	if err := validateCurrentCanonicalTuskerSkillPackage(source); err != nil {
		return nil, factoryIntakeContractProvenance{}, "", "", err
	}
	contract, err := factoryIntakeContractProvenanceFromPackage(source)
	if err != nil {
		return nil, factoryIntakeContractProvenance{}, "", "", err
	}
	entries := []skillbundle.PayloadEntry{}
	err = filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("canonical skill source contains symlinked file: %s", path)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("canonical skill source contains special file: %s", path)
		}
		rel, err := filepath.Rel(source, path)
		if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.Base(rel) == skillProvenanceFilename {
			return fmt.Errorf("canonical skill source contains invalid package path: %s", path)
		}
		for _, forbidden := range []string{"work", "epics", "evidence", "attempts", "events", "_generated", "_system", "dashboards", "Attachments"} {
			if strings.HasPrefix(filepath.ToSlash(rel), forbidden+"/") {
				return fmt.Errorf("canonical skill source contains forbidden package path: %s", rel)
			}
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		entries = append(entries, skillbundle.PayloadEntry{Relative: filepath.ToSlash(rel), Content: string(raw)})
		return nil
	})
	if err != nil {
		return nil, factoryIntakeContractProvenance{}, "", "", err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Relative < entries[j].Relative })
	return entries, contract, "canonical", portableSkillSourceIdentity("canonical"), nil
}

func installSkillPayloadSymlink(destination, sourceArg string) error {
	source, err := canonicalSkillSourceDir(sourceArg)
	if err != nil {
		return err
	}
	if err := validateCurrentCanonicalTuskerSkillPackage(source); err != nil {
		return err
	}
	return replaceSkillInstallDestination(destination, func(stage string) error {
		return os.Symlink(skillSymlinkTarget(source, destination), stage)
	})
}

// skillSymlinkTarget keeps repo-local skill installs portable. An absolute
// target only resolves inside the checkout that wrote it, so the generated
// install looks canonical in one working copy and points at a foreign tree in
// every git worktree and fresh clone. A relative target travels with the tree.
// Installs outside the source repo — user-home destinations — keep the absolute
// target, because a relative path between two unrelated trees is meaningless.
func skillSymlinkTarget(source, destination string) string {
	absDestination, err := filepath.Abs(destination)
	if err != nil {
		return source
	}
	repoRoot, err := findRepoRoot(source)
	if err != nil || repoRoot == "" || !isRepoLocalSkillDestination(absDestination, repoRoot) {
		return source
	}
	relative, err := filepath.Rel(filepath.Dir(absDestination), source)
	if err != nil || relative == "" {
		return source
	}
	return relative
}

func canonicalSkillSourceDir(sourceArg string) (string, error) {
	if sourceArg = firstNonEmpty(strings.TrimSpace(sourceArg), strings.TrimSpace(os.Getenv("TUSKER_SKILL_SOURCE"))); sourceArg != "" {
		source, err := filepath.Abs(sourceArg)
		if err != nil {
			return "", err
		}
		if fileExists(filepath.Join(source, "SKILL.md")) {
			if resolved, resolveErr := filepath.EvalSymlinks(source); resolveErr == nil {
				source = resolved
			}
			return source, nil
		}
		for _, nested := range []string{filepath.Join(source, "skills", "tusker"), filepath.Join(source, "skill")} {
			if fileExists(filepath.Join(nested, "SKILL.md")) {
				return nested, nil
			}
		}
		return "", tuskerError(errorNotFound, "canonical Tusker skill source is missing: "+source, withHint("pass --source <tusker-checkout> or --source <tusker-checkout>/skills/tusker"))
	}
	repoRoot, err := findRepoRoot(mustGetwd())
	if err != nil {
		return "", err
	}
	if repoRoot == "" {
		return "", tuskerError(errorNotFound, "cannot symlink Tusker skill without a source checkout", withHint("Use --source <tusker-checkout>, set TUSKER_SKILL_SOURCE, or use --skill-mode copy for portable installs from a release binary."))
	}
	source := filepath.Join(repoRoot, "skills", "tusker")
	if !fileExists(filepath.Join(source, "SKILL.md")) {
		return "", tuskerError(errorNotFound, "canonical Tusker skill source is missing: "+source)
	}
	return source, nil
}

func skillInstallModeForDestination(args Args, destination, repoRoot string) (string, error) {
	if mode := strings.TrimSpace(firstNonEmpty(args.String("skill-mode"), args.String("mode"))); mode != "" {
		return normalizeSkillInstallMode(mode)
	}
	if repoRoot != "" && isRepoLocalSkillDestination(destination, repoRoot) {
		return skillInstallModeLink, nil
	}
	return skillInstallModeCopy, nil
}

func normalizeSkillInstallMode(mode string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", skillInstallModeCopy:
		return skillInstallModeCopy, nil
	case skillInstallModeLink, "link":
		return skillInstallModeLink, nil
	default:
		return "", tuskerError(errorInvalidArg, "invalid skill install mode: "+mode, withHint("Use copy or symlink."))
	}
}

func isRepoLocalSkillDestination(destination, repoRoot string) bool {
	if repoRoot == "" {
		return false
	}
	absDestination, err := filepath.Abs(destination)
	if err != nil {
		return false
	}
	absRepo, err := filepath.Abs(repoRoot)
	if err != nil {
		return false
	}
	for _, rel := range []string{
		filepath.Join(".agents", "skills", currentSkillInstallDir),
		filepath.Join(".claude", "skills", currentSkillInstallDir),
	} {
		if absDestination == filepath.Join(absRepo, rel) {
			return true
		}
	}
	return false
}

func skillSyncCmd(args Args) error {
	if args.Bool("help") {
		printSkillSyncHelp()
		return nil
	}
	repo := strings.TrimSpace(args.String("repo"))
	if repo == "" {
		repo = "."
	}
	mode := strings.TrimSpace(args.String("mode"))
	if mode == "" {
		mode = skillInstallModeLink
	}
	sourceArg := firstNonEmpty(args.String("source"), os.Getenv("TUSKER_SKILL_SOURCE"))
	source := skillSourceReport{Kind: "embedded", Path: "embedded://tusker/skills/tusker"}
	if mode != skillInstallModeCopy || strings.TrimSpace(sourceArg) != "" {
		source = classifySkillSyncSource(sourceArg, "")
	}
	if source.Kind != "canonical" && source.Kind != "embedded" {
		return tuskerError(errorInvalidArg, "skill sync source is "+source.Kind+": "+source.Path,
			withHint("Pass --source <canonical-tusker-checkout>; generated installs are outputs, not editable source."),
			withContext(map[string]any{"source_kind": source.Kind, "source": source.Path}))
	}
	mode, err := normalizeSkillInstallMode(mode)
	if err != nil {
		return err
	}
	repoRoot, err := filepath.Abs(repo)
	if err != nil {
		return err
	}
	// Sync is deliberately narrower than install/update. In particular it must
	// not upsert AGENTS/CLAUDE pointers or create repo feedback files: callers
	// use it as the safe repair for exactly these generated package paths.
	destinations := []string{
		filepath.Join(repoRoot, ".agents", "skills", currentSkillInstallDir),
		filepath.Join(repoRoot, ".claude", "skills", currentSkillInstallDir),
	}
	for _, destination := range destinations {
		copySource := ""
		if source.Kind == "canonical" {
			copySource = source.Path
		}
		if err := installSkillPayloadWithModeFrom(destination, mode, copySource); err != nil {
			return err
		}
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "repo": repoRoot, "mode": mode, "source_kind": source.Kind, "source": source.Path, "skill_source_kind": source.Kind, "skill_source": source.Path, "installed": destinations, "scope": "managed Tusker skill packages only"})
		return nil
	}
	if !args.Bool("quiet") {
		for _, destination := range destinations {
			fmt.Printf("Synced Tusker skill at %s (%s)\n", destination, mode)
		}
	}
	return nil
}

func skillBundleCmd(args Args) error {
	if args.Bool("help") {
		printSkillBundleHelp()
		return nil
	}
	repo := strings.TrimSpace(args.String("repo"))
	if repo == "" {
		repo = "."
	}
	repoRoot, err := filepath.Abs(repo)
	if err != nil {
		return err
	}
	out := strings.TrimSpace(args.String("out"))
	explicitOut := out != ""
	if out == "" {
		out = filepath.Join(repoRoot, ".tusker", "_generated", "skill-bundle")
	} else if !filepath.IsAbs(out) {
		out = filepath.Join(repoRoot, out)
	}
	out, err = filepath.Abs(out)
	if err != nil {
		return err
	}
	boundary, err := bundleOutputBoundary(repoRoot, out)
	if err != nil {
		return err
	}
	if info, statErr := os.Lstat(out); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return tuskerError(errorInvalidArg, "bundle output must be a managed directory, not a symlink or file: "+out)
		}
		defaultOut := filepath.Join(repoRoot, ".tusker", "_generated", "skill-bundle")
		if explicitOut && !sameCleanPath(out, defaultOut) && !fileExists(filepath.Join(out, skillBundleMarker)) {
			return tuskerError(errorInvalidArg, "refusing to replace an arbitrary existing bundle output directory: "+out, withHint("choose a new --out path or an existing Tusker bundle containing "+skillBundleMarker))
		}
	} else if !os.IsNotExist(statErr) {
		return statErr
	}
	destinations := []string{
		filepath.Join(out, ".agents", "skills", currentSkillInstallDir),
		filepath.Join(out, ".claude", "skills", currentSkillInstallDir),
	}
	if err := replaceOwnedFilesystemEntry(out, boundary, false, func(stage string) error {
		if err := os.Mkdir(stage, 0o755); err != nil {
			return err
		}
		for _, rel := range []string{
			filepath.Join(".agents", "skills", currentSkillInstallDir),
			filepath.Join(".claude", "skills", currentSkillInstallDir),
		} {
			if err := installSkillPayloadWithMode(filepath.Join(stage, rel), skillInstallModeCopy); err != nil {
				return err
			}
		}
		return writeText(filepath.Join(stage, skillBundleMarker), "schema: tusker.skill-bundle/v1\n")
	}); err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{
			"ok":            true,
			"mode":          skillInstallModeCopy,
			"out":           out,
			"installed":     destinations,
			"dereferenced":  true,
			"source_policy": "canonical skill source; generated bundle is not editable source",
		})
		return nil
	}
	fmt.Printf("Bundled materialized Tusker skills at %s\n", out)
	for _, destination := range destinations {
		fmt.Printf("Installed Tusker skill at %s (copy)\n", destination)
	}
	return nil
}

func installBinarySymlink(args Args) error {
	binarySource, err := ensureInstallBinarySource()
	if err != nil {
		return err
	}
	return installBinarySymlinkFrom(args, binarySource)
}

func preflightInstallBinary(args Args) error {
	binarySource, err := ensureInstallBinarySource()
	if err != nil {
		return err
	}
	_, err = binaryInstallPlanForSource(args, binarySource)
	return err
}

func installBinarySymlinkFrom(args Args, binarySource string) error {
	return installBinarySymlinkFromWithRename(args, binarySource, os.Rename)
}

func installBinarySymlinkFromWithRename(args Args, binarySource string, rename func(string, string) error) error {
	plan, err := binaryInstallPlanForSource(args, binarySource)
	if err != nil {
		return err
	}
	if err := ensureDir(filepath.Dir(plan.target)); err != nil {
		return err
	}
	if err := replaceBinarySymlinkAtomically(plan.source, plan.target, rename); err != nil {
		return err
	}
	fmt.Printf("Symlinked %s -> %s\n", plan.target, plan.source)

	pathParts := strings.Split(os.Getenv("PATH"), string(os.PathListSeparator))
	if !containsString(pathParts, filepath.Dir(plan.target)) {
		fmt.Printf("Note: %s is not on your PATH. Add it to your shell rc to call `tusker` directly.\n", filepath.Dir(plan.target))
	}
	return nil
}

type binaryInstallPlan struct {
	source string
	target string
}

func binaryInstallPlanForSource(args Args, binarySource string) (binaryInstallPlan, error) {
	binDir := args.String("bin-dir")
	if binDir == "" {
		binDir = pickBinDir()
	}
	if binDir == "" {
		return binaryInstallPlan{}, tuskerError(errorInvalidArg, "No writable bin dir found on PATH. Pass --bin-dir <path> (e.g. ~/.local/bin), or --no-bin to skip.")
	}
	binDir, err := filepath.Abs(binDir)
	if err != nil {
		return binaryInstallPlan{}, err
	}
	binarySource, err = filepath.Abs(binarySource)
	if err != nil {
		return binaryInstallPlan{}, err
	}
	if evaluated, evalErr := filepath.EvalSymlinks(binarySource); evalErr != nil {
		return binaryInstallPlan{}, fmt.Errorf("resolve binary install source %s: %w", binarySource, evalErr)
	} else {
		binarySource = evaluated
	}
	target := filepath.Join(binDir, "tusker")
	same, err := binaryInstallSourceMatchesTarget(binarySource, target)
	if err != nil {
		return binaryInstallPlan{}, err
	}
	if same {
		return binaryInstallPlan{}, tuskerError(
			errorInvalidArg,
			fmt.Sprintf("refusing to update %s: the binary source and destination are the same file; release-installed Tusker binaries must be updated by rerunning the release installer", target),
			withHint("Run `curl -fsSL https://raw.githubusercontent.com/srv1n/tusker/main/scripts/install.sh | sh` to install the latest release, or use `tusker update --no-bin` to refresh skills only."),
		)
	}
	return binaryInstallPlan{source: binarySource, target: target}, nil
}

func binaryInstallSourceMatchesTarget(binarySource, target string) (bool, error) {
	targetEndpoint, err := canonicalInstallEndpoint(target)
	if err != nil {
		return false, err
	}
	if filepath.Clean(binarySource) == targetEndpoint {
		return true, nil
	}

	sourceInfo, err := os.Stat(binarySource)
	if err != nil {
		return false, err
	}
	targetInfo, err := os.Lstat(target)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if targetInfo.Mode()&os.ModeSymlink != 0 {
		return false, nil
	}
	return os.SameFile(sourceInfo, targetInfo), nil
}

func canonicalInstallEndpoint(path string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	parent := filepath.Dir(absPath)
	evaluatedParent, err := filepath.EvalSymlinks(parent)
	if err == nil {
		parent = evaluatedParent
	} else if !os.IsNotExist(err) {
		return "", err
	}
	return filepath.Clean(filepath.Join(parent, filepath.Base(absPath))), nil
}

func replaceBinarySymlinkAtomically(binarySource, target string, rename func(string, string) error) error {
	stageDir, err := os.MkdirTemp(filepath.Dir(target), ".tusker-install-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stageDir)

	staged := filepath.Join(stageDir, filepath.Base(target))
	if err := os.Symlink(binarySource, staged); err != nil {
		return err
	}
	if err := rename(staged, target); err != nil {
		return fmt.Errorf("atomically replace %s: %w", target, err)
	}
	return nil
}

func ensureInstallBinarySource() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	evaluated, evalErr := filepath.EvalSymlinks(executable)
	if evalErr == nil {
		executable = evaluated
	}
	repoRoot, err := findRepoRoot(mustGetwd())
	if err != nil {
		return "", err
	}
	if repoRoot != "" {
		relativeToRepo, relErr := filepath.Rel(repoRoot, executable)
		if relErr == nil && relativeToRepo != "." && !strings.HasPrefix(relativeToRepo, "..") && !looksLikeGoBuildCache(executable) {
			return executable, nil
		}

		binaryPath := filepath.Join(repoRoot, "dist", "tusker")
		if err := ensureDir(filepath.Dir(binaryPath)); err != nil {
			return "", err
		}
		cmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/tusker")
		cmd.Dir = repoRoot
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return "", tuskerError(errorInvalidArg, "Failed to build dist/tusker. Run `go build -o dist/tusker ./cmd/tusker` manually and retry.")
		}
		stripMacOSBuildProvenance(binaryPath)
		return binaryPath, nil
	}

	if executable != "" && !looksLikeGoBuildCache(executable) && !strings.HasPrefix(executable, os.TempDir()+string(filepath.Separator)) {
		return executable, nil
	}

	return "", tuskerError(errorNotFound, "Could not find repo root to build dist/tusker for installation.")
}

func pickBinDir() string {
	candidates := []string{
		filepath.Join(userHomeDir(), ".local", "bin"),
		"/opt/homebrew/bin",
		"/usr/local/bin",
	}
	pathParts := strings.Split(os.Getenv("PATH"), string(os.PathListSeparator))
	for _, candidate := range candidates {
		if !containsString(pathParts, candidate) {
			continue
		}
		if unixWritable(candidate) {
			return candidate
		}
	}
	return filepath.Join(userHomeDir(), ".local", "bin")
}

func findRepoRoot(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		if fileExists(filepath.Join(dir, "go.mod")) && (fileExists(filepath.Join(dir, "skills", "tusker", "SKILL.md")) || fileExists(filepath.Join(dir, "skill", "SKILL.md"))) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil
		}
		dir = parent
	}
}

func printUpdateHelp() {
	fmt.Println(`Usage:
  tusker update [--bin-dir <path>] [--no-bin] [--repo <path>] [--repo-only] [--skill-mode copy|symlink] [--source <checkout>] [--json]

Purpose:
  Refresh the installed tusker binary link and all existing user skill installs
  from the currently running binary. This is the command to run after pulling,
  rebuilding, or replacing the Tusker binary.

Behavior:
  - refreshes existing user skills in ~/.agents, ~/.codex, and ~/.claude
  - relinks tusker on PATH unless --no-bin is passed
  - with --repo, refreshes the repo-local .agents skill install and the
    generated .claude compatibility install for Claude Code; repo-local
    installs default to symlink mode, while user installs default to copy mode
    and refreshes the managed AGENTS.md/CLAUDE.md Tusker bootstrap block
    with compact proof/context guidance
  - --source points symlink mode at a canonical Tusker checkout or skill dir
  - with --repo-only, skips user skill installs and touches only the repo

Examples:
  tusker update
  tusker update --bin-dir ~/.local/bin
  tusker update --repo . --repo-only --no-bin
  tusker update --repo . --repo-only --no-bin --skill-mode copy
  tusker update --repo . --repo-only --no-bin --skill-mode symlink --source ~/src/tusker`)
}

func printInstallHelp() {
	fmt.Println(`Usage:
  tusker install [--bin-dir <path>] [--no-bin] [--codex-user] [--claude-user] [--repo <path>] [--refresh-existing-user-skills] [--skill-mode copy|symlink] [--source <checkout>] [--force] [--json]

Purpose:
  Install the Tusker binary link and refresh/install skill bundles from the
  currently running binary. This is used by make install and the shell installer.

Behavior:
  - refreshes already-installed user skills in ~/.agents, ~/.codex, and ~/.claude
  - --codex-user installs ~/.agents/skills/tusker
  - --claude-user installs ~/.claude/skills/tusker
  - --repo installs the repo-local .agents skill bundle and the generated
    .claude compatibility install without ambient user skill refresh;
    repo-local installs default to symlink mode, while user installs default
    to copy mode
    and refreshes the managed AGENTS.md/CLAUDE.md Tusker bootstrap block
    with compact proof/context guidance
  - --refresh-existing-user-skills also refreshes existing user skills when --repo is used
  - relinks tusker on PATH unless --no-bin is passed
  - --source points symlink mode at a canonical Tusker checkout or skill dir
  - --force is accepted for installer compatibility

Examples:
  tusker install --codex-user --claude-user
  tusker install --repo . --no-bin
  tusker install --repo . --no-bin --skill-mode copy
  tusker install --repo . --no-bin --skill-mode symlink --source ~/src/tusker
  tusker install --no-bin`)
}

func printSkillSyncHelp() {
	fmt.Println(`Usage:
  tusker skill sync [--repo <path>] [--mode symlink|copy] [--source <checkout>] [--json]

Purpose:
  Refresh repo-local generated skill installs for local agents. Local sync
  defaults to symlink mode so canonical skill source remains the source of
  truth. The command reports canonical source provenance and rejects generated
  install output or invalid paths as source. Use --source when running from
  outside the Tusker checkout. Use --mode copy only when the repo must be
  self-contained. Sync overwrites only the exact managed
  .agents/.claude skills/tusker package targets; it never rewrites project
  knowledge, repo instructions, secrets, unrelated skills, or plugins.

Examples:
  tusker skill sync --repo .
  tusker skill sync --repo . --source ~/src/tusker
  tusker skill sync --repo . --mode copy`)
}

func printSkillBundleHelp() {
	fmt.Println(`Usage:
  tusker skill bundle [--repo <path>] [--out <path>] [--dereference-symlinks] [--json]

Purpose:
  Create a portable materialized skill bundle for handoff packets, cloud
  runners, or machines that cannot follow local symlinks. Generated bundle
  copies are not editable source.

Examples:
  tusker skill bundle --repo .
  tusker skill bundle --repo . --out .tusker/_generated/skill-bundle`)
}

func printSyncRepoContractHelp() {
	fmt.Println(`Usage:
  tusker sync-repo-contract --repo <path> [--vault <path>] [--force]

Purpose:
  Install or refresh repo-local agent workflow files and Tusker pointers.`)
}

func initCmd(args Args) error {
	if args.Bool("help") {
		printInitHelp()
		return nil
	}
	profileForMode := strings.TrimSpace(args.String("profile"))
	if args.Bool("legacy") || args.Bool("migrate-v5") || args.Bool("migrate-v6") || (profileForMode != "" && !strings.EqualFold(profileForMode, "v7")) {
		return removedSurfaceError("non-V7 init/migration")
	}
	if args.Bool("migrate-v5") && args.Bool("dry-run") {
		report, err := migrateLegacyVaultToV5(args)
		if err != nil {
			return err
		}
		printV5MigrationReport(report, args)
		return nil
	}
	if args.Bool("migrate-v6") {
		report, err := migrateV5VaultToV6(args)
		if err != nil {
			return err
		}
		printV6MigrationReport(report, args)
		return nil
	}
	cwd := mustGetwd()
	yes := args.Bool("yes")
	if args.Bool("purge-state") {
		purgeArgs := Args{"repo": cwd, "only-tusker-state": "true", "yes": "true"}
		if args.Bool("quiet") {
			purgeArgs["quiet"] = "true"
		}
		if err := tuskerPurgeCmd(purgeArgs); err != nil {
			return err
		}
	}
	registerDaemon := args.Bool("daemon")
	_, mountArgPresent := args["mount"]
	noMount := args.Bool("no-mount")
	mountTracker := args.Bool("mount") && !noMount
	fresh := args.Bool("fresh")
	vaultOnly := args.Bool("vault-only")
	profile := strings.TrimSpace(args.String("profile"))
	useLegacy := args.Bool("legacy") || args.Bool("migrate-v5")
	useV7 := !useLegacy && (args.Bool("v7") || profile == "" || strings.EqualFold(profile, "v7"))
	useV6 := !useLegacy && profile != "" && !strings.EqualFold(profile, "v7")
	interactive := !yes && isTTY(os.Stdin)
	reader := bufio.NewReader(os.Stdin)
	ask := func(question string, defaultYes bool) (bool, error) {
		if yes {
			return true, nil
		}
		if !interactive {
			return false, tuskerError(errorInvalidArg, question+" - stdin is not a TTY; pass --yes to accept all defaults")
		}
		suffix := "[Y/n]"
		if !defaultYes {
			suffix = "[y/N]"
		}
		fmt.Printf("%s %s ", question, suffix)
		answer, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return false, err
		}
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer == "" {
			return defaultYes, nil
		}
		return answer == "y" || answer == "yes", nil
	}
	vaultPath := filepath.Join(cwd, defaultRepoVaultDir)
	explicitVault := args.String("vault") != ""
	if explicit := args.String("vault"); explicit != "" {
		vaultPath, _ = filepath.Abs(explicit)
	}
	if fresh && fileExists(vaultPath) {
		backupPath := vaultPath + ".backup-" + time.Now().UTC().Format("20060102-150405")
		doBackup, err := ask(fmt.Sprintf("Move existing %s to %s and recreate it cleanly?", vaultPath, backupPath), true)
		if err != nil {
			return err
		}
		if doBackup {
			if err := os.Rename(vaultPath, backupPath); err != nil {
				return err
			}
			fmt.Printf("Moved existing vault to %s\n", backupPath)
		}
	}
	existingVault := ""
	if isVaultDir(vaultPath) {
		existingVault = vaultPath
	} else if !explicitVault && !mountTracker {
		discovered, _ := discoverVault(cwd)
		if discovered != "" {
			existingVault = discovered
		}
	}
	if existingVault != "" {
		fmt.Printf("Vault already present at %s\n", existingVault)
	} else {
		doVault, err := ask(fmt.Sprintf("Create vault at %s?", vaultPath), true)
		if err != nil {
			return err
		}
		if doVault {
			if useLegacy {
				if err := bootstrapLegacy(Args{"vault": vaultPath, "quiet": "true"}); err != nil {
					return err
				}
			} else if useV7 {
				if err := bootstrapV7Profile(vaultPath, profile); err != nil {
					return err
				}
			} else if useV6 {
				if err := bootstrapV6(Args{"vault": vaultPath, "quiet": "true", "profile": profile}); err != nil {
					return err
				}
			} else {
				if err := bootstrap(Args{"vault": vaultPath, "quiet": "true"}); err != nil {
					return err
				}
			}
			fmt.Printf("Initialized vault at %s\n", vaultPath)
		} else {
			fmt.Println("Skipped vault initialization. Aborting - init needs a vault.")
			return nil
		}
	}
	effectiveVault := existingVault
	if effectiveVault == "" {
		effectiveVault = vaultPath
	}
	if useLegacy {
		if err := bootstrapLegacy(Args{"vault": effectiveVault, "quiet": "true"}); err != nil {
			return err
		}
	} else if useV6 {
		if err := bootstrapV6(Args{"vault": effectiveVault, "quiet": "true", "profile": profile}); err != nil {
			return err
		}
	} else if useV7 {
		if err := bootstrapV7Profile(effectiveVault, profile); err != nil {
			return err
		}
	} else {
		if err := bootstrap(Args{"vault": effectiveVault, "quiet": "true"}); err != nil {
			return err
		}
	}
	if err := workflowInitCmd(Args{"vault": effectiveVault, "quiet": "true"}); err != nil {
		return err
	}
	if args.Bool("migrate-v5") {
		report, err := migrateLegacyVaultToV5(args)
		if err != nil {
			return err
		}
		printV5MigrationReport(report, args)
	}
	vaultRelative := relativeFromRepo(cwd, effectiveVault)
	if vaultRelative == "" {
		vaultRelative = defaultRepoVaultDir
	}
	if !vaultOnly && !args.Bool("no-pointers") {
		readmeLink := filepath.ToSlash(filepath.Join(vaultRelative, "README.md"))
		for _, filename := range []string{"AGENTS.md", "CLAUDE.md"} {
			filePath := filepath.Join(cwd, filename)
			question := fmt.Sprintf("%s doesn't exist - create it with the tusker pointer?", filename)
			if fileExists(filePath) {
				question = fmt.Sprintf("Found %s - inject tusker epic-roster pointer?", filename)
			}
			doInject, err := ask(question, true)
			if err != nil {
				return err
			}
			if !doInject {
				continue
			}
			changed, err := upsertTuskerPointer(filePath, readmeLink)
			if err != nil {
				return err
			}
			if changed != "" {
				fmt.Printf("%s %s\n", capitalize(changed), filePath)
			}
		}
	}
	if !vaultOnly && !args.Bool("no-contract") {
		doContract, err := ask("Install repo-contract files (.github templates, agent-workflow docs)?", true)
		if err != nil {
			return err
		}
		if doContract {
			if err := syncRepoContract(Args{"repo": cwd, "vault": effectiveVault}); err != nil {
				return err
			}
		}
	}
	if err := reindex(Args{"vault": effectiveVault, "quiet": "true"}); err != nil {
		return err
	}
	if !mountTracker && !mountArgPresent && !noMount && configuredWorkspaceVaultPath() != "" {
		doMount, err := ask("Mount this repo tracker in your configured Obsidian vault?", true)
		if err != nil {
			return err
		}
		mountTracker = doMount
	}
	if mountTracker {
		mountArgs := Args{"repo": cwd, "vault": effectiveVault, "quiet": "true"}
		if name := strings.TrimSpace(args.String("mount-name")); name != "" {
			mountArgs["name"] = name
		}
		if err := vaultMountCmd(mountArgs); err != nil {
			return err
		}
	}
	if registerDaemon {
		if err := projectsAddCmd(Args{"repo": cwd, "vault": effectiveVault}); err != nil {
			return err
		}
	}
	fmt.Println()
	fmt.Println("Done. Next steps:")
	fmt.Println("  tusker validate --vault " + effectiveVault)
	if useV7 {
		fmt.Println("  tusker domain list --v7 --vault " + effectiveVault)
		fmt.Println("  tusker publish skill --v7 --vault " + effectiveVault)
	} else if useV6 {
		fmt.Println("  tusker domain list --vault " + effectiveVault)
		fmt.Println("  tusker knowledge route \"change CLI flag\" --vault " + effectiveVault)
	} else {
		fmt.Println("  tusker list --vault " + effectiveVault + " --type epic")
		fmt.Println("  tusker new epic --vault " + effectiveVault + " --acronym APP --title \"App foundation\"")
	}
	return nil
}

func printV5MigrationReport(report *v5MigrationReport, args Args) {
	if args.Bool("json") {
		emitJSON(report)
		return
	}
	mode := "Migrated"
	if report.DryRun {
		mode = "Would migrate"
	}
	fmt.Printf("%s %d notes in %s\n", mode, report.NotesChanged, report.Vault)
	if report.BackupPath != "" {
		fmt.Printf("Backup: %s\n", report.BackupPath)
	}
	if report.FilesMoved > 0 || report.DocsMapNodesAdd > 0 {
		fmt.Printf("Files moved: %d\n", report.FilesMoved)
		fmt.Printf("Docs-map nodes added: %d\n", report.DocsMapNodesAdd)
	}
	if len(report.IDMap) > 0 {
		fmt.Println("ID mapping:")
		keys := make([]string, 0, len(report.IDMap))
		for key := range report.IDMap {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if report.IDMap[key] != key {
				fmt.Printf("  %s -> %s\n", key, report.IDMap[key])
			}
		}
	}
}

func upsertGitignore(vaultPath string) error {
	marker := "# tusker"
	block := marker + "\n_system/generated/\n.obsidian/workspace*\n.obsidian/cache\n.trash/\n"
	filePath := filepath.Join(vaultPath, ".gitignore")
	if !fileExists(filePath) {
		return writeText(filePath, block)
	}
	current, err := readText(filePath)
	if err != nil {
		return err
	}
	if strings.Contains(current, marker) {
		return nil
	}
	return writeText(filePath, strings.TrimRight(current, "\n")+"\n\n"+block)
}

func renderTuskerPointerBlock(readmeLink string) string {
	skillLink := tuskerSkillLinkFromReadme(readmeLink)
	vaultRoot := tuskerVaultRootFromReadme(readmeLink)
	return strings.Join([]string{
		tuskerPointerBegin,
		"## Tusker",
		"",
		"Use Tusker for tracked repo work.",
		"",
		"- Task mechanics live in the installed `tusker` skill.",
		fmt.Sprintf("- Project knowledge starts at `%s`.", skillLink),
		"- Start runnable work with `tusker next`; inspect named work with `tusker show <TASK-ID> --capsule`.",
		fmt.Sprintf("- Do not read `%s/events`, `_generated`, `attempts`, `evidence`, `Attachments`, raw logs, or full task files unless the task explicitly requires it.", vaultRoot),
		"- Keep proof compact: use capsules, path-scoped status/search, and command + PASS/FAIL summaries; put noisy logs in `.tusker/scratch/<TASK-ID>/`.",
		feedbackPointerInstruction(),
		tuskerPointerEnd,
	}, "\n")
}

func tuskerSkillLinkFromReadme(readmeLink string) string {
	readmeLink = strings.TrimSpace(filepath.ToSlash(readmeLink))
	if readmeLink == "" {
		return filepath.ToSlash(filepath.Join(defaultRepoVaultDir, "SKILL.md"))
	}
	dir := filepath.ToSlash(filepath.Dir(readmeLink))
	if dir == "." || dir == "" {
		return "SKILL.md"
	}
	return filepath.ToSlash(filepath.Join(dir, "SKILL.md"))
}

func tuskerVaultRootFromReadme(readmeLink string) string {
	readmeLink = strings.TrimSpace(filepath.ToSlash(readmeLink))
	if readmeLink == "" {
		return defaultRepoVaultDir
	}
	dir := filepath.ToSlash(filepath.Dir(readmeLink))
	if dir == "." || dir == "" {
		return "."
	}
	return dir
}

func upsertRepoTuskerPointers(repoPath, vaultArg string) ([]repoPointerUpdate, error) {
	vaultPath, err := repoTuskerVaultPath(repoPath, vaultArg)
	if err != nil {
		return nil, err
	}
	readmeLink := repoTuskerReadmeLink(repoPath, vaultPath)
	var updates []repoPointerUpdate
	for _, filename := range []string{"AGENTS.md", "CLAUDE.md"} {
		filePath := filepath.Join(repoPath, filename)
		changed, err := upsertTuskerPointer(filePath, readmeLink)
		if err != nil {
			return nil, err
		}
		if changed != "" {
			updates = append(updates, repoPointerUpdate{path: filePath, action: changed})
		}
	}
	return updates, nil
}

func repoTuskerReadmeLink(repoPath, vaultPath string) string {
	vaultRelative := defaultRepoVaultDir
	if vaultPath != "" {
		vaultRelative = relativeFromRepo(repoPath, vaultPath)
	}
	readmeLink := "README.md"
	if vaultRelative != "" {
		readmeLink = filepath.ToSlash(filepath.Join(vaultRelative, "README.md"))
	}
	return readmeLink
}

func repoTuskerVaultPath(repoPath, vaultArg string) (string, error) {
	if strings.TrimSpace(vaultArg) != "" {
		return filepath.Abs(vaultArg)
	}
	if discovered, _ := discoverVault(repoPath); discovered != "" {
		return discovered, nil
	}
	return filepath.Join(repoPath, defaultRepoVaultDir), nil
}

func repoPointerUpdatePaths(updates []repoPointerUpdate) []string {
	paths := make([]string, 0, len(updates))
	for _, update := range updates {
		paths = append(paths, update.path)
	}
	return paths
}

func repoAgentGuidanceFlatteningWarning(repoPath, vaultArg string, updates []repoPointerUpdate) string {
	if len(updates) == 0 {
		return ""
	}
	vaultPath, err := repoTuskerVaultPath(repoPath, vaultArg)
	if err != nil || fileExists(filepath.Join(vaultPath, "SKILL.md")) {
		return ""
	}
	audit, err := auditV7AgentGuidance(repoPath, vaultPath)
	if err != nil || len(audit.Findings) == 0 {
		return ""
	}
	return fmt.Sprintf("non-managed AGENTS/CLAUDE guidance exists but %s is missing; run `tusker skill audit-agent-guidance --repo %s --write` before flattening root guidance", filepath.ToSlash(filepath.Join(relativeFromRepo(repoPath, vaultPath), "SKILL.md")), repoPath)
}

func upsertTuskerPointer(filePath, readmeLink string) (string, error) {
	block := renderTuskerPointerBlock(readmeLink)
	if !fileExists(filePath) {
		return "created", writeText(filePath, block+"\n")
	}
	current, err := readText(filePath)
	if err != nil {
		return "", err
	}
	begin := strings.Index(current, tuskerPointerBegin)
	end := strings.Index(current, tuskerPointerEnd)
	if begin != -1 && end != -1 && end > begin {
		next := current[:begin] + block + current[end+len(tuskerPointerEnd):]
		if next == current {
			return "", nil
		}
		return "updated", writeText(filePath, next)
	}
	next := strings.TrimRight(current, " \t\r\n") + "\n\n" + block + "\n"
	return "updated", writeText(filePath, next)
}

func relativeFromRepo(repoPath, vaultPath string) string {
	rel, err := filepath.Rel(repoPath, vaultPath)
	if err != nil {
		return vaultPath
	}
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return vaultPath
	}
	return filepath.ToSlash(rel)
}
