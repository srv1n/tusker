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
	tasks := sortedServeSnapshotNotes(snap.tasks)
	epics := sortedServeSnapshotNotes(snap.epics)
	gates := sortedServeSnapshotNotes(snap.gates)
	waves := sortedServeSnapshotNotes(snap.waves)
	evidence := sortedServeSnapshotNotes(snap.evidence)
	decisions := sortedServeSnapshotNotes(snap.decisions)
	attempts := sortedServeSnapshotNotes(snap.attemptNotes)
	runs := append([]RunStatus(nil), snap.runs...)
	sort.Slice(runs, func(i, j int) bool {
		if runs[i].ProjectID != runs[j].ProjectID {
			return runs[i].ProjectID < runs[j].ProjectID
		}
		return runs[i].RecordID < runs[j].RecordID
	})
	payload := struct {
		ProjectID        string                               `json:"project_id"`
		ProjectName      string                               `json:"project_name"`
		Workflow         Workflow                             `json:"workflow"`
		Tasks            []Note                               `json:"tasks"`
		Epics            []Note                               `json:"epics"`
		Gates            []Note                               `json:"gates"`
		Waves            []Note                               `json:"waves"`
		Evidence         []Note                               `json:"evidence"`
		Decisions        []Note                               `json:"decisions"`
		Attempts         []Note                               `json:"attempts"`
		Docs             []serveDocListEntry                  `json:"docs"`
		Needs            []serveNeedItem                      `json:"needs"`
		Runs             []RunStatus                          `json:"runs"`
		Queue            map[string]automationTaskExplanation `json:"queue"`
		OpenP0Escalation bool                                 `json:"open_p0_escalation"`
	}{
		ProjectID: snap.projectID, ProjectName: snap.projectName, Workflow: snap.workflow,
		Tasks: tasks, Epics: epics, Gates: gates, Waves: waves,
		Evidence: evidence, Decisions: decisions, Attempts: attempts,
		Docs: snap.docs, Needs: snap.needs, Runs: runs, Queue: snap.queue,
		OpenP0Escalation: snap.openP0Escalation,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
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
