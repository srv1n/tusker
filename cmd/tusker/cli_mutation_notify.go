package main

import (
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var cliVaultMutationState = struct {
	sync.Mutex
	active bool
	vaults map[string]struct{}
	hints  map[string][]daemonControlChange
	last   map[string][]daemonControlChange
}{}

// daemonControlChange is the rich mutation payload assembled at the canonical
// write boundary. The wire envelope remains owned by daemon_control.go; callers
// that cannot transport this newer payload retain the old project-only wakeup.
type daemonControlChange struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind,omitempty"`
	Revision    string   `json:"revision,omitempty"`
	Eligibility []string `json:"eligibility,omitempty"`
}

func beginCLIVaultMutationTracking() {
	cliVaultMutationState.Lock()
	defer cliVaultMutationState.Unlock()
	cliVaultMutationState.active = true
	cliVaultMutationState.vaults = map[string]struct{}{}
	cliVaultMutationState.hints = map[string][]daemonControlChange{}
}

func finishCLIVaultMutationTracking() []string {
	cliVaultMutationState.Lock()
	defer cliVaultMutationState.Unlock()
	result := make([]string, 0, len(cliVaultMutationState.vaults))
	for vault := range cliVaultMutationState.vaults {
		result = append(result, vault)
	}
	cliVaultMutationState.last = cliVaultMutationState.hints
	sortStrings(result)
	cliVaultMutationState.active = false
	cliVaultMutationState.vaults = nil
	cliVaultMutationState.hints = nil
	return result
}

func recordCLIVaultMutation(filePath string) {
	cliVaultMutationState.Lock()
	defer cliVaultMutationState.Unlock()
	if !cliVaultMutationState.active {
		return
	}
	if vault := mutationVaultRoot(filePath); vault != "" {
		cliVaultMutationState.vaults[vault] = struct{}{}
		if change, ok := daemonControlChangeForFile(filePath); ok {
			cliVaultMutationState.hints[vault] = append(cliVaultMutationState.hints[vault], change)
		}
	}
}

// daemonControlChangeForFile is deliberately best effort. A control wakeup must
// remain valid when an old writer, a non-Markdown write, or a partially removed
// record cannot provide a rich hint; the daemon then performs its normal
// adaptive reconciliation.
func daemonControlChangeForFile(filePath string) (daemonControlChange, bool) {
	if !strings.HasSuffix(strings.ToLower(filePath), ".md") {
		return daemonControlChange{}, false
	}
	raw, err := readText(filePath)
	if err != nil {
		return daemonControlChange{}, false
	}
	data, _, err := parseFrontmatter(raw)
	if err != nil {
		return daemonControlChange{}, false
	}
	id := firstNonEmpty(stringField(data, "id"), stringField(data, "record_id"))
	if id == "" {
		return daemonControlChange{}, false
	}
	kind := firstNonEmpty(stringField(data, "kind"), stringField(data, "type"))
	return daemonControlChange{
		ID: id, Kind: kind, Revision: firstNonEmpty(stringField(data, "state_rev"), stringField(data, "work_revision")),
		Eligibility: daemonControlEligibilityClasses(kind),
	}, true
}

func daemonControlEligibilityClasses(kind string) []string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "task":
		return []string{"dependencies", "gates", "proof", "runtime", "wave"}
	case "wave":
		return []string{"wave", "authorization", "dependencies"}
	case "gate":
		return []string{"gates", "eligibility"}
	default:
		return []string{"eligibility"}
	}
}

func mutationVaultRoot(filePath string) string {
	abs, err := filepath.Abs(filePath)
	if err != nil {
		return ""
	}
	for current := filepath.Dir(abs); ; current = filepath.Dir(current) {
		base := filepath.Base(current)
		if isVaultDir(current) && (base == defaultRepoVaultDir || fileExists(filepath.Join(current, "WORKFLOW.md")) || fileExists(filepath.Join(current, "SKILL.md"))) {
			return filepath.Clean(current)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return ""
		}
	}
}

func notifyDaemonForVaultPath(vaultPath string) {
	cliVaultMutationState.Lock()
	changes := append([]daemonControlChange(nil), cliVaultMutationState.last[filepath.Clean(vaultPath)]...)
	delete(cliVaultMutationState.last, filepath.Clean(vaultPath))
	cliVaultMutationState.Unlock()
	notifyDaemonForVaultPathWithChanges(vaultPath, changes)
}

func notifyDaemonForVaultPathWithChanges(vaultPath string, changes []daemonControlChange) {
	stateRoot := DefaultStateRoot()
	projectID := ""
	if store, err := OpenRuntimeStore(stateRoot); err == nil {
		if loaded, listErr := loadRegisteredProjects(store, registeredProjectLoadOptions{MetadataOnly: true, LoadDisabled: true}); listErr == nil {
			for _, project := range loadedRegisteredProjects(loaded) {
				if sameCanonicalProjectPath(project.VaultRoot, vaultPath) {
					projectID = project.ProjectID
					break
				}
			}
		}
		_ = store.Close()
	}
	if projectID == "" {
		projectID, _ = resolveV7ProjectID(vaultPath)
	}
	if strings.TrimSpace(projectID) == "" {
		return
	}
	_ = sendDaemonControlOneWay(stateRoot, daemonControlRequest{Command: "reconcile_project", ProjectID: projectID, Cause: "cli_mutation", Changes: changes}, 250*time.Millisecond)
}

func cliCommandMutatesProjectRegistry(command string, args Args) bool {
	if command == "projects rebind" && args.Bool("dry-run") {
		return false
	}
	if command == "projects prune" && (!args.Bool("apply") || args.Bool("dry-run")) {
		return false
	}
	switch command {
	case "projects add", "projects enable", "projects disable", "projects rebind", "projects remove", "projects prune", "reset", "relaunch":
		return true
	default:
		return false
	}
}
