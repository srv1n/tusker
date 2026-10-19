package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// serveSnapshotContentHash excludes poll timestamps and other transport-only
// metadata. It changes only when a client-visible project projection changes.
func serveSnapshotContentHash(snap serveSnapshot) string {
	return serveSnapshotMetadataContentHash(snap) + ":" + serveSnapshotRuntimeContentHash(snap)
}

func serveSnapshotMetadataContentHash(snap serveSnapshot) string {
	return "sha256:" + serveContentHash(serveSnapshotMetadataKeyHashes(snap)) // map keys marshal sorted
}

// serveSnapshotMetadataKeyHashes hashes each stream topic's note/doc inputs
// once per full build and memoizes them on snap.derived, which the runtime
// refresh path keeps. The live stream compares them per topic so a refresh
// announces only the topics whose content changed.
func serveSnapshotMetadataKeyHashes(snap serveSnapshot) map[string]string {
	if d := snap.derived; d != nil {
		d.mu.Lock()
		cached := d.keyHashes
		d.mu.Unlock()
		if cached != nil {
			return cached
		}
	}
	keys := map[string]string{
		"tasks": serveContentHash(struct {
			Tasks []Note
			Queue map[string]automationTaskExplanation
		}{sortedServeSnapshotNotes(snap.tasks), snap.queue}),
		"epics":         serveContentHash(sortedServeSnapshotNotes(snap.epics)),
		"waves":         serveContentHash(sortedServeSnapshotNotes(snap.waves)),
		"gates":         serveContentHash(sortedServeSnapshotNotes(snap.gates)),
		"evidence":      serveContentHash(sortedServeSnapshotNotes(snap.evidence)),
		"decisions":     serveContentHash(sortedServeSnapshotNotes(snap.decisions)),
		"attempt-notes": serveContentHash(sortedServeSnapshotNotes(snap.attemptNotes)),
		"docs":          serveContentHash(snap.docs),
		"meta": serveContentHash(struct {
			ProjectID, ProjectName string
			Workflow               Workflow
			OpenP0Escalation       bool
		}{snap.projectID, snap.projectName, snap.workflow, snap.openP0Escalation}),
	}
	if d := snap.derived; d != nil {
		d.mu.Lock()
		d.keyHashes = keys
		d.mu.Unlock()
	}
	return keys
}

func serveContentHash(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func sortedServeSnapshotNotes(notes []Note) []Note {
	copy := append([]Note(nil), notes...)
	sort.Slice(copy, func(i, j int) bool {
		if copy[i].RelativePath != copy[j].RelativePath {
			return copy[i].RelativePath < copy[j].RelativePath
		}
		return copy[i].AbsolutePath < copy[j].AbsolutePath
	})
	return copy
}

func sortedServeSnapshotRuns(runs []RunStatus) []RunStatus {
	runs = append([]RunStatus(nil), runs...)
	sort.Slice(runs, func(i, j int) bool { return runs[i].RecordID < runs[j].RecordID })
	return runs
}

// Runtime ticks hash only dynamic facts; note bodies and docs were hashed when
// their projection was built and do not need sorting/marshalling each tick.
func serveSnapshotRuntimeContentHash(snap serveSnapshot) string {
	return serveContentHash(struct {
		Runs  []RunStatus
		Needs []serveNeedItem
	}{sortedServeSnapshotRuns(snap.runs), snap.needs})
}
