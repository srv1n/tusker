package main

import (
	"os"
	"strings"
	"time"
)

// Detail reads prefer an exact record ID over managed children sharing its
// item ID. Keep the broader authority lookup's ambiguity checks unchanged.
func serveCurrentRun(store *RuntimeStore, projectID, identity string) (*RunStatus, error) {
	runs, err := store.listRunsQuery(`SELECT `+runtimeRunColumns+` FROM runs WHERE project_id = ? AND record_id = ? LIMIT 1`, projectID, identity)
	if err != nil {
		return nil, err
	}
	if len(runs) > 0 {
		return &runs[0], nil
	}
	return store.FindRunScoped(projectID, identity)
}

// Controls need current lifecycle facts, not a new notes/docs projection.
// Read the two known records directly and leave the shared snapshot immutable.
func serveFreshRunNotes(snap serveSnapshot, run RunStatus) (serveSnapshot, error) {
	knownNotes := snap.notesByID
	snap.notesByID = map[string]Note{}
	taskID := firstNonEmpty(run.ItemID, run.RecordID)
	for step := 0; step < 2; step++ {
		id := taskID
		if step == 1 {
			id = stringField(snap.notesByID[taskID].Data, "wave")
		}
		note, ok := knownNotes[id]
		if !ok || note.AbsolutePath == "" {
			continue
		}
		data, body, err := parseFrontmatterMustRead(note.AbsolutePath)
		if os.IsNotExist(err) {
			delete(snap.notesByID, id)
			continue
		}
		if err != nil {
			return serveSnapshot{}, err
		}
		note.Data, note.Body = data, body
		snap.notesByID[id] = note
	}
	return snap, nil
}

// Runtime events invalidate only cheap store-derived facts. External note/doc
// edits still use the existing full snapshot reconciliation path.
func (s *serveServer) invalidateProjectRuntimeSnapshot(projectID string) {
	projectID = strings.TrimSpace(projectID)
	s.snapshotMu.Lock()
	for key, entry := range s.snapshots {
		if entry != nil && (projectID == "" || entry.project.ProjectID == projectID || key == projectID) {
			entry.runtimeInvalid = true
		}
	}
	s.snapshotMu.Unlock()
	s.summaryMu.Lock()
	s.summary = nil
	s.summaryMu.Unlock()
}

func (s *serveServer) refreshProjectRuntimeSnapshot(projectID string) {
	s.invalidateProjectRuntimeSnapshot(projectID)
	s.broadcastProjectRuntimeSnapshotRefresh(projectID)
}

func (s *serveServer) broadcastProjectRuntimeSnapshotRefresh(projectID string) {
	if s.stream != nil {
		s.stream.Broadcast(serveStreamEvent{Kind: "runtime_projection_refreshed", Project: strings.TrimSpace(projectID), Keys: []string{"runs", "needs", "attempts", "roster", "agent-messages"}})
	}
}

func (s *serveServer) refreshSnapshotRuntime(snap serveSnapshot) (serveSnapshot, error) {
	runs, truncated, err := s.store.ListRunsForProjectPage(snap.projectID, serveSnapshotRunCap)
	if err != nil {
		return serveSnapshot{}, err
	}
	if truncated {
		return serveSnapshot{}, tuskerError("RUNTIME_RUN_SNAPSHOT_LIMIT", "project exceeds the Serve runtime snapshot limit")
	}
	snap.runs = runs
	snap.openQuestions, err = s.serveOpenQuestions(snap)
	if err != nil {
		return serveSnapshot{}, err
	}
	snap.permissionWaits, err = s.servePermissionWaits(snap)
	if err != nil {
		return serveSnapshot{}, err
	}
	snap.needs = serveNeeds(snap, s.now())
	return snap, nil
}

// Removal and rebind release both projection and parser caches for old roots.
func (s *serveServer) forgetProjectSnapshot(projectID string, oldVaults ...string) {
	projectID = strings.TrimSpace(projectID)
	var vaults []string
	for _, vault := range oldVaults {
		if strings.TrimSpace(vault) != "" {
			vaults = append(vaults, vault)
		}
	}
	s.snapshotMu.Lock()
	for key, entry := range s.snapshots {
		if entry != nil && (projectID == "" || entry.project.ProjectID == projectID || key == projectID) {
			entry.invalid = true
			vaults = append(vaults, entry.project.VaultRoot)
			delete(s.snapshots, key)
		}
	}
	s.snapshotMu.Unlock()
	for _, vault := range uniqueStrings(vaults) {
		disposeCachedNotes(vault)
	}
	s.summaryMu.Lock()
	s.summary = nil
	s.summaryAt = time.Time{}
	s.summaryMu.Unlock()
}
