package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	skillbundle "tusker/skill"
)

const (
	currentSkillInstallDir = "tusker"
	legacySkillInstallDir  = "obsidian-vault-tracker"
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

func installCmd(args Args) error {
	if args.Bool("help") {
		printInstallHelp()
		return nil
	}
	if !args.Bool("codex-user") && !args.Bool("claude-user") && args.String("repo") == "" && !args.Bool("bin-only") && !args.Bool("refresh-existing-user-skills") {
		printInstallHelp()
		return tuskerError(errorMissingArg, "install needs one of --codex-user, --claude-user, --repo, --bin-only, or --refresh-existing-user-skills")
	}

	destinations := []string{}
	if args.Bool("codex-user") {
		destinations = append(destinations, defaultCodexUserSkillDestination())
	}
	if args.Bool("claude-user") {
		destinations = append(destinations, filepath.Join(userHomeDir(), ".claude", "skills", currentSkillInstallDir))
	}
	if repoPath := args.String("repo"); repoPath != "" {
		repoRoot, err := filepath.Abs(repoPath)
		if err != nil {
			return err
		}
		destinations = append(destinations,
			filepath.Join(repoRoot, ".agents", "skills", currentSkillInstallDir),
			filepath.Join(repoRoot, ".claude", "skills", currentSkillInstallDir),
		)
	}
	migrationReport := []string{}
	refreshDestinations := []string{}
	if args.Bool("refresh-existing-user-skills") {
		var err error
		migrationReport, err = migrateLegacyUserSkillInstalls()
		if err != nil {
			return err
		}
		refreshDestinations = existingUserSkillDestinations()
	}

	for _, destination := range destinations {
		if !args.Bool("force") && fileExists(destination) {
			return tuskerError(errorAlreadyExists, "Refusing to overwrite existing install without --force: "+destination, withPath(destination))
		}
	}

	if !args.Bool("bin-only") {
		for _, destination := range destinations {
			if err := installSkillPayload(destination); err != nil {
				return err
			}
			fmt.Printf("Installed to %s\n", destination)
		}
	}
	for _, destination := range refreshDestinations {
		if err := installSkillPayload(destination); err != nil {
			return err
		}
		fmt.Printf("Refreshed existing user skill at %s\n", destination)
	}
	if !args.Bool("refresh-existing-user-skills") && (args.Bool("codex-user") || args.Bool("claude-user")) {
		var err error
		migrationReport, err = migrateLegacyUserSkillInstalls()
		if err != nil {
			return err
		}
	}
	for _, line := range migrationReport {
		fmt.Println(line)
	}
	if args.Bool("refresh-existing-user-skills") && len(refreshDestinations) == 0 {
		fmt.Println("No existing user skill installs found to refresh")
	}

	if !args.Bool("no-bin") {
		if err := installBinarySymlink(args); err != nil {
			return err
		}
	}

	return nil
}

func updateCmd(args Args) error {
	if args.Bool("help") {
		printUpdateHelp()
		return nil
	}

	updatedSkills := []string{}
	migrationReport, err := migrateLegacyUserSkillInstalls()
	if err != nil {
		return err
	}

	destinations := existingUserSkillDestinations()
	if repoPath := strings.TrimSpace(args.String("repo")); repoPath != "" {
		repoRoot, err := filepath.Abs(repoPath)
		if err != nil {
			return err
		}
		destinations = append(destinations,
			filepath.Join(repoRoot, ".agents", "skills", currentSkillInstallDir),
			filepath.Join(repoRoot, ".claude", "skills", currentSkillInstallDir),
		)
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
			"ok":              true,
			"binary_updated":  binaryUpdated,
			"updated_skills":  updatedSkills,
			"migration_notes": migrationReport,
		})
		return nil
	}

	for _, line := range migrationReport {
		fmt.Println(line)
	}
	for _, destination := range updatedSkills {
		fmt.Printf("Updated Tusker skill at %s\n", destination)
	}
	if len(updatedSkills) == 0 {
		fmt.Println("No existing user skill installs found to refresh. Run `tusker install --codex-user --claude-user` once, or pass `--repo <path>` for repo-local skills.")
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

func migrateLegacyUserSkillInstalls() ([]string, error) {
	pairs := [][2]string{
		{
			filepath.Join(userHomeDir(), ".agents", "skills", currentSkillInstallDir),
			filepath.Join(userHomeDir(), ".agents", "skills", legacySkillInstallDir),
		},
		{
			filepath.Join(userHomeDir(), ".codex", "skills", currentSkillInstallDir),
			filepath.Join(userHomeDir(), ".codex", "skills", legacySkillInstallDir),
		},
		{
			filepath.Join(userHomeDir(), ".claude", "skills", currentSkillInstallDir),
			filepath.Join(userHomeDir(), ".claude", "skills", legacySkillInstallDir),
		},
	}
	var report []string
	for _, pair := range pairs {
		current := pair[0]
		legacy := pair[1]
		if !fileExists(legacy) {
			continue
		}
		if !fileExists(current) {
			if err := ensureDir(filepath.Dir(current)); err != nil {
				return nil, err
			}
			if err := os.Rename(legacy, current); err != nil {
				return nil, err
			}
			report = append(report, fmt.Sprintf("Migrated legacy skill install %s -> %s", legacy, current))
			continue
		}
		backup := legacySkillBackupPath(legacy)
		if err := ensureDir(filepath.Dir(backup)); err != nil {
			return nil, err
		}
		if err := os.Rename(legacy, backup); err != nil {
			return nil, err
		}
		report = append(report, fmt.Sprintf("Archived legacy skill install %s -> %s", legacy, backup))
	}
	return report, nil
}

func legacySkillBackupPath(legacy string) string {
	root := filepath.Dir(filepath.Dir(legacy))
	name := fmt.Sprintf("%s-%d", legacySkillInstallDir, time.Now().UnixNano())
	return filepath.Join(root, "skill-backups", name)
}

func installSkillPayload(destination string) error {
	entries, err := skillbundle.PayloadEntries()
	if err != nil {
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

func printInstallHelp() {
	fmt.Println(`Usage:
  tusker install --codex-user
  tusker install --claude-user
  tusker install --codex-user --claude-user
  tusker install --repo /path/to/repo
  tusker install --bin-only
  tusker install --bin-only --refresh-existing-user-skills
  tusker update

Every run also symlinks ` + "`tusker`" + ` onto PATH (default ~/.local/bin).
Override with --bin-dir <path> or skip with --no-bin.

Flags:
  --codex-user              install skill into ~/.agents/skills/
  --claude-user             install skill into ~/.claude/skills/
  --repo <path>             install skill into <repo>/.agents/skills/ and <repo>/.claude/skills/
  --bin-only                just install the binary, skip the skill copy
  --refresh-existing-user-skills
                            refresh existing root user skills only; checks ~/.agents, ~/.codex, and ~/.claude
  --no-bin                  skip the binary symlink
  --bin-dir <path>          custom directory for the tusker symlink
  --force                   overwrite existing installs

Use ` + "`tusker update`" + ` after pulling or rebuilding Tusker. It refreshes the
currently installed binary link and every existing Tusker user skill bundle from
the currently running binary.`)
}

func printUpdateHelp() {
	fmt.Println(`Usage:
  tusker update [--bin-dir <path>] [--no-bin] [--repo <path>] [--json]

Purpose:
  Refresh the installed tusker binary link and all existing user skill installs
  from the currently running binary. This is the command to run after pulling,
  rebuilding, or installing a newer Tusker release.

Behavior:
  - refreshes existing user skills in ~/.agents, ~/.codex, and ~/.claude
  - migrates legacy obsidian-vault-tracker skill installs to tusker
  - relinks tusker on PATH unless --no-bin is passed
  - with --repo, also refreshes repo-local .agents/.claude skill installs

Examples:
  tusker update
  tusker update --bin-dir ~/.local/bin
  tusker update --repo . --no-bin`)
}

func initCmd(args Args) error {
	if args.Bool("help") {
		printInitHelp()
		return nil
	}
	cwd := mustGetwd()
	yes := args.Bool("yes")
	registerDaemon := args.Bool("daemon")
	_, mountArgPresent := args["mount"]
	noMount := args.Bool("no-mount")
	mountTracker := args.Bool("mount") && !noMount
	fresh := args.Bool("fresh")
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
	} else if discovered, _ := discoverVault(cwd); discovered != "" {
		existingVault = discovered
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
			fmt.Printf("Bootstrapped vault at %s\n", vaultPath)
		} else {
			fmt.Println("Skipped vault bootstrap. Aborting - init needs a vault.")
			return nil
		}
	}
	effectiveVault := existingVault
	if effectiveVault == "" {
		effectiveVault = vaultPath
	}
	if err := workflowInitCmd(Args{"vault": effectiveVault, "quiet": "true"}); err != nil {
		return err
	}
	vaultRelative := relativeFromRepo(cwd, effectiveVault)
	if vaultRelative == "" {
		vaultRelative = "tusker"
	}
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
	doContract, err := ask("Install repo-contract files (.github templates, agent-workflow docs)?", true)
	if err != nil {
		return err
	}
	if doContract {
		if err := syncRepoContract(Args{"repo": cwd, "vault": effectiveVault}); err != nil {
			return err
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
	fmt.Println("  tusker epics --vault " + effectiveVault)
	fmt.Println("  tusker new-epic --vault " + effectiveVault + " --acronym APP --title \"App foundation\"")
	if mountTracker {
		fmt.Println("  tusker vault status")
	} else {
		fmt.Println("  tusker vault set --path <obsidian-vault>   # then: tusker mount")
	}
	if registerDaemon {
		fmt.Println("  tusker daemon run --once")
		fmt.Println("  tusker runs --json")
	} else {
		fmt.Println("  tusker projects add --repo . --vault " + effectiveVault + "   # when you want daemon mode")
	}
	return nil
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
		"## Project overview + epic roster (Tusker)",
		"",
		fmt.Sprintf("This project's vault landing page lives at `%s` — read it before logging new work. It has two sections: a hand-edited **Project overview** (preserved across regens, between `<!-- tusker:overview:begin -->` markers) and an auto-generated **Epic roster** (regenerated on every `tusker reindex`). For a live terminal view of epics only, run `tusker epics`.", readmeLink),
		"",
		"When logging work: pick the epic whose summary best matches, and announce the ID **plus a one-line rationale for the epic choice**. If nothing fits and the work will outlive one story, create a new epic with `tusker new-epic --acronym <ACR> --summary \"...\"`.",
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
