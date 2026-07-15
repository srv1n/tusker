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
}{}

func beginCLIVaultMutationTracking() {
	cliVaultMutationState.Lock()
	defer cliVaultMutationState.Unlock()
	cliVaultMutationState.active = true
	cliVaultMutationState.vaults = map[string]struct{}{}
}

func finishCLIVaultMutationTracking() []string {
	cliVaultMutationState.Lock()
	defer cliVaultMutationState.Unlock()
	result := make([]string, 0, len(cliVaultMutationState.vaults))
	for vault := range cliVaultMutationState.vaults {
		result = append(result, vault)
	}
	sortStrings(result)
	cliVaultMutationState.active = false
	cliVaultMutationState.vaults = nil
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
	}
}

func mutationVaultRoot(filePath string) string {
	abs, err := filepath.Abs(filePath)
	if err != nil {
		return ""
	}
	for current := filepath.Dir(abs); ; current = filepath.Dir(current) {
		base := filepath.Base(current)
		if isVaultDir(current) && (base == defaultRepoVaultDir || base == "tusker" || fileExists(filepath.Join(current, "WORKFLOW.md")) || fileExists(filepath.Join(current, "SKILL.md"))) {
			return filepath.Clean(current)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return ""
		}
	}
}

func notifyDaemonForVaultPath(vaultPath string) {
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
	_ = sendDaemonControlOneWay(stateRoot, daemonControlRequest{Command: "reconcile_project", ProjectID: projectID}, 250*time.Millisecond)
}

func cliCommandMutatesProjectRegistry(command string) bool {
	switch command {
	case "projects add", "projects enable", "projects disable", "projects remove", "projects prune":
		return true
	default:
		return false
	}
}
