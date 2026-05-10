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

	skillbundle "tusker/skill"
)

const (
	currentSkillInstallDir = "tusker"
)

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
	var vaultPath string
	if args.String("vault") != "" {
		vaultPath, err = filepath.Abs(args.String("vault"))
		if err != nil {
			return err
		}
	} else {
		if discovered, _ := discoverVault(repoPath); discovered != "" {
			vaultPath = discovered
		} else if discovered, _ := discoverVault(mustGetwd()); discovered != "" {
			vaultPath = discovered
		}
	}
	vaultRelative := "tusker"
	if vaultPath != "" {
		vaultRelative = relativeFromRepo(repoPath, vaultPath)
	}
	readmeLink := "README.md"
	if vaultRelative != "" {
		readmeLink = filepath.ToSlash(filepath.Join(vaultRelative, "README.md"))
	}
	var pointerReport []string
	for _, filename := range []string{"AGENTS.md", "CLAUDE.md"} {
		changed, err := upsertTuskerPointer(filepath.Join(repoPath, filename), readmeLink)
		if err != nil {
			return err
		}
		if changed != "" {
			action := "Updated"
			if changed == "created" {
				action = "Created"
			}
			pointerReport = append(pointerReport, fmt.Sprintf("%s %s", action, filepath.Join(repoPath, filename)))
		}
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
	if args.Bool("help") {
		printUpdateHelp()
		return nil
	}

	updatedSkills := []string{}
	destinations := []string{}
	if !args.Bool("repo-only") {
		destinations = existingUserSkillDestinations()
	}
	if repoPath := strings.TrimSpace(args.String("repo")); repoPath != "" {
		repoRoot, err := filepath.Abs(repoPath)
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

	for _, destination := range destinations {
		if err := installSkillPayload(destination); err != nil {
			return err
		}
		updatedSkills = append(updatedSkills, destination)
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
			"ok":             true,
			"binary_updated": binaryUpdated,
			"updated_skills": updatedSkills,
		})
		return nil
	}

	for _, destination := range updatedSkills {
		fmt.Printf("Updated Tusker skill at %s\n", destination)
	}
	if len(updatedSkills) == 0 {
		fmt.Println("No existing user skill installs found to refresh. Pass `--repo <path>` for repo-local skills.")
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
	entries, err := skillbundle.PayloadEntries()
	if err != nil {
		return err
	}
	if err := os.RemoveAll(destination); err != nil {
		return err
	}
	for _, entry := range entries {
		target := filepath.Join(destination, filepath.FromSlash(entry.Relative))
		if err := writeText(target, entry.Content); err != nil {
			return err
		}
	}
	return nil
}

func installBinarySymlink(args Args) error {
	binarySource, err := ensureInstallBinarySource()
	if err != nil {
		return err
	}

	binDir := args.String("bin-dir")
	if binDir == "" {
		binDir = pickBinDir()
	}
	if binDir == "" {
		return tuskerError(errorInvalidArg, "No writable bin dir found on PATH. Pass --bin-dir <path> (e.g. ~/.local/bin), or --no-bin to skip.")
	}
	binDir, err = filepath.Abs(binDir)
	if err != nil {
		return err
	}
	if err := ensureDir(binDir); err != nil {
		return err
	}
	target := filepath.Join(binDir, "tusker")
	_ = os.Remove(target)
	if err := os.Symlink(binarySource, target); err != nil {
		return err
	}
	fmt.Printf("Symlinked %s -> %s\n", target, binarySource)

	pathParts := strings.Split(os.Getenv("PATH"), string(os.PathListSeparator))
	if !containsString(pathParts, binDir) {
		fmt.Printf("Note: %s is not on your PATH. Add it to your shell rc to call `tusker` directly.\n", binDir)
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
		if fileExists(filepath.Join(dir, "go.mod")) && fileExists(filepath.Join(dir, "skill", "SKILL.md")) {
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
  tusker update [--bin-dir <path>] [--no-bin] [--repo <path>] [--repo-only] [--json]

Purpose:
  Refresh the installed tusker binary link and all existing user skill installs
  from the currently running binary. This is the command to run after pulling,
  rebuilding, or replacing the Tusker binary.

Behavior:
  - refreshes existing user skills in ~/.agents, ~/.codex, and ~/.claude
  - relinks tusker on PATH unless --no-bin is passed
  - with --repo, also refreshes repo-local .agents/.claude skill installs
  - with --repo-only, skips user skill installs and touches only the repo

Examples:
  tusker update
  tusker update --bin-dir ~/.local/bin
  tusker update --repo . --repo-only --no-bin`)
}

func initCmd(args Args) error {
	if args.Bool("help") {
		printInitHelp()
		return nil
	}
	if args.Bool("migrate-v5") && args.Bool("dry-run") {
		report, err := migrateLegacyVaultToV5(args)
		if err != nil {
			return err
		}
		printV5MigrationReport(report, args)
		return nil
	}
	cwd := mustGetwd()
	yes := args.Bool("yes")
	registerDaemon := args.Bool("daemon")
	_, mountArgPresent := args["mount"]
	noMount := args.Bool("no-mount")
	mountTracker := args.Bool("mount") && !noMount
	fresh := args.Bool("fresh")
	vaultOnly := args.Bool("vault-only")
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
	vaultPath := filepath.Join(cwd, "tusker")
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
	} else if !explicitVault {
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
			if err := bootstrap(Args{"vault": vaultPath, "quiet": "true"}); err != nil {
				return err
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
	if err := bootstrap(Args{"vault": effectiveVault, "quiet": "true"}); err != nil {
		return err
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
		vaultRelative = "tusker"
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
	fmt.Println("  tusker list --vault " + effectiveVault + " --type epic")
	fmt.Println("  tusker new epic --vault " + effectiveVault + " --acronym APP --title \"App foundation\"")
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
	return strings.Join([]string{
		tuskerPointerBegin,
		"## Progressive Tusker context",
		"",
		fmt.Sprintf("Start with `tusker list --type epic` to see the short epic roster. Use `%s` only when the project overview is needed; it intentionally omits task lists from the top-level roster.", "`"+readmeLink+"`"),
		"",
		"Progressive drill-down: `tusker list --epic <ACR> --type task --open` for one epic's open tasks, then open only the selected task file. Use `tusker search \"<term>\" --type task` before creating possible duplicates.",
		"",
		"When logging work: pick the epic whose summary best matches, and announce the ID **plus a one-line rationale for the epic choice**. If nothing fits and the work will outlive one task, create a new epic with `tusker new epic --acronym <ACR> --title \"<name>\" --summary \"...\"`.",
		tuskerPointerEnd,
	}, "\n")
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
	if strings.HasPrefix(rel, "..") {
		return vaultPath
	}
	return filepath.ToSlash(rel)
}
