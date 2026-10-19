package main

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"
)

// serveProjectionState remembers, per project, the content hash of every
// stream topic last announced, so a projection refresh broadcasts only the
// topics that changed. One diff runs per project at a time; refreshes that
// arrive meanwhile merge into a single follow-up pass.
type serveProjectionState struct {
	mu      sync.Mutex
	hashes  map[string]map[string]string
	pending map[string]serveStreamEvent
	running map[string]bool
}

// Topics derived from the whole projection: they change when any topic does.
var serveProjectionAggregateKeys = map[string]bool{"projects": true, "review:batch": true, "factory-operations": true}

func (s *serveServer) queueProjectionDiff(event serveStreamEvent) {
	st := &s.projection
	project := strings.TrimSpace(event.Project)
	st.mu.Lock()
	if st.pending == nil {
		st.pending, st.running = map[string]serveStreamEvent{}, map[string]bool{}
	}
	if prev, ok := st.pending[project]; ok {
		event.Keys = serveStreamKeys(append(prev.Keys, event.Keys...)...)
		if prev.Kind == "projection_refreshed" {
			event.Kind = prev.Kind
		}
	}
	st.pending[project] = event
	if st.running[project] {
		st.mu.Unlock()
		return
	}
	st.running[project] = true
	st.mu.Unlock()
	for {
		st.mu.Lock()
		next, ok := st.pending[project]
		delete(st.pending, project)
		if !ok {
			delete(st.running, project)
			st.mu.Unlock()
			return
		}
		st.mu.Unlock()
		s.publishProjectionDiff(next)
	}
}

// publishProjectionDiff rebuilds the project's snapshot and broadcasts the
// event narrowed to the topics whose content differs from the last broadcast.
func (s *serveServer) publishProjectionDiff(event serveStreamEvent) {
	event.diffed = true
	project := strings.TrimSpace(event.Project)
	if project == "" {
		s.stream.Broadcast(event)
		return
	}
	if !s.stream.hasSubscriber(project) {
		// Nobody watches it; a client that connects later reconciles in full.
		return
	}
	snap, err := s.loadFreshSnapshotForProject(project)
	if err != nil {
		s.stream.Broadcast(event) // cannot diff; announce the full candidate set
		return
	}
	hashes := serveProjectionKeyHashes(snap)
	st := &s.projection
	st.mu.Lock()
	if st.hashes == nil {
		st.hashes = map[string]map[string]string{}
	}
	prev := st.hashes[project]
	if prev == nil {
		prev = map[string]string{}
		st.hashes[project] = prev
	}
	changed := []string{}
	anyChanged := false
	if event.Kind == "projection_refreshed" && prev["meta"] != hashes["meta"] {
		prev["meta"] = hashes["meta"]
		anyChanged = true
	}
	for _, key := range event.Keys {
		if hash, known := hashes[key]; known && prev[key] != hash {
			prev[key] = hash
			changed = append(changed, key)
			anyChanged = true
		}
	}
	for _, key := range event.Keys {
		if _, known := hashes[key]; known {
			continue
		}
		if !serveProjectionAggregateKeys[key] || anyChanged {
			changed = append(changed, key) // unknown topics pass through
		}
	}
	st.mu.Unlock()
	if len(changed) == 0 {
		return
	}
	event.Keys = changed
	s.stream.Broadcast(event)
}

func serveProjectionKeyHashes(snap serveSnapshot) map[string]string {
	runs := serveContentHash(sortedServeSnapshotRuns(snap.runs))
	out := map[string]string{
		"runs":   runs,
		"roster": runs,
		"needs":  serveContentHash(snap.needs),
		"agent-messages": serveContentHash(struct {
			Questions map[string][]AgentMessage
			Waits     map[string][]AgentAccessApproval
		}{snap.openQuestions, snap.permissionWaits}),
		"feedback": serveFeedbackStamp(snap.project.VaultRoot),
	}
	for key, hash := range serveSnapshotMetadataKeyHashes(snap) {
		out[key] = hash
	}
	out["attempts"] = out["attempt-notes"] + runs
	return out
}

// Feedback is read from disk per request, not from the snapshot; a stat
// signature of its directory is enough to notice adds and edits.
func serveFeedbackStamp(vaultRoot string) string {
	if strings.TrimSpace(vaultRoot) == "" {
		return ""
	}
	root := filepath.Join(vaultRoot, "feedback")
	var b strings.Builder
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		if info, infoErr := entry.Info(); infoErr == nil {
			fmt.Fprintf(&b, "%s\x00%d\x00%d\n", path, info.ModTime().UnixNano(), info.Size())
		}
		return nil
	})
	return serveContentHash(b.String())
}
