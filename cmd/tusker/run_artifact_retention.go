package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const runArtifactRetention = 7 * 24 * time.Hour

type runArtifactPurgeReport struct {
	OK    bool  `json:"ok"`
	Files int   `json:"files"`
	Bytes int64 `json:"bytes"`
}

// purgeRunArtifacts only touches Tusker-owned run files. Nonterminal runs keep
// their artifacts, including files needed for retries and human decisions.
func purgeRunArtifacts(store *RuntimeStore, now time.Time, all bool) (runArtifactPurgeReport, error) {
	report := runArtifactPurgeReport{OK: true}
	root := filepath.Join(store.stateRoot, "runs")
	protectedDirs := map[string]bool{}
	terminalAt := map[string]time.Time{}
	rows, err := store.query(`SELECT prompt_path, event_sink_path, raw_log_path, status_path FROM runs WHERE terminal = 0 OR lease_state != 'released'
		UNION SELECT a.prompt_path, a.event_sink_path, a.raw_log_path, a.status_path FROM attempts a
		LEFT JOIN runs r ON r.project_id = a.project_id AND r.record_id = a.record_id
		WHERE r.terminal = 0 OR r.lease_state != 'released' OR (r.project_id IS NULL AND a.finished_at = '')`)
	if err != nil {
		return report, err
	}
	for rows.Next() {
		var paths [4]string
		if err := rows.Scan(&paths[0], &paths[1], &paths[2], &paths[3]); err != nil {
			rows.Close()
			return report, err
		}
		for _, path := range paths {
			if path != "" {
				protectedDirs[filepath.Dir(path)] = true
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return report, err
	}
	rows, err = store.query(`SELECT r.prompt_path, r.event_sink_path, r.raw_log_path, r.status_path,
		COALESCE(NULLIF((SELECT MAX(a.finished_at) FROM attempts a WHERE a.project_id = r.project_id AND a.record_id = r.record_id), ''), r.updated_at)
		FROM runs r WHERE r.terminal = 1 AND r.lease_state = 'released'
		UNION ALL SELECT a.prompt_path, a.event_sink_path, a.raw_log_path, a.status_path, a.finished_at
		FROM attempts a JOIN runs r ON r.project_id = a.project_id AND r.record_id = a.record_id WHERE r.terminal = 1 AND r.lease_state = 'released'`)
	if err != nil {
		return report, err
	}
	for rows.Next() {
		var paths [4]string
		var updated string
		if err := rows.Scan(&paths[0], &paths[1], &paths[2], &paths[3], &updated); err != nil {
			rows.Close()
			return report, err
		}
		at, err := time.Parse(time.RFC3339Nano, updated)
		if err != nil {
			continue
		}
		for _, path := range paths {
			if path != "" && at.After(terminalAt[filepath.Dir(path)]) {
				terminalAt[filepath.Dir(path)] = at
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return report, err
	}
	cutoff := now.Add(-runArtifactRetention)
	var dirs []string
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if os.IsNotExist(walkErr) {
			return nil
		}
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root {
				dirs = append(dirs, path)
			}
			return nil
		}
		if protectedDirs[filepath.Dir(path)] || !isRunArtifactFile(entry.Name()) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		agedAt := info.ModTime()
		if terminalAt[filepath.Dir(path)].After(agedAt) {
			agedAt = terminalAt[filepath.Dir(path)]
		}
		if !info.Mode().IsRegular() || (!all && agedAt.After(cutoff)) {
			return nil
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		report.Files++
		report.Bytes += info.Size()
		return nil
	})
	if os.IsNotExist(err) {
		return report, nil
	}
	if err != nil {
		return report, err
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		_ = os.Remove(dirs[i]) // Leave any protected or non-Tusker content in place.
	}
	return report, err
}

func isRunArtifactFile(name string) bool {
	for _, suffix := range []string{".raw.log", ".events.jsonl", ".prompt.md", ".status.json", ".events.jsonl.seq", ".events.jsonl.lock", ".status.json.wrapper-request.json"} {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}
