package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ponytail: fixed threshold; make it config if different machines need tuning.
const buildWaitLimit = 2 * time.Minute

type buildLaneStats struct {
	Builds, WarmBuilds int
	MedianWaitMS       int64
	DepRebuildRate     float64
	Commands           []string
}

// buildLaneEntry is one xcodebuild/swift build, or one rustc compile. Rustc
// lines carry Local and are grouped into builds by target dir and cargo pid.
type buildLaneEntry struct {
	At           time.Time `json:"at"`
	ProjectID    string    `json:"project_id"`
	Cmd          string    `json:"cmd"`
	Cold         bool      `json:"cold"`
	WaitMS       int64     `json:"wait_ms"`
	CompiledDeps int       `json:"compiled_deps"`
	Target       string    `json:"target"`
	CargoPID     int       `json:"cargo_pid"`
	Crate        string    `json:"crate"`
	Local        *bool     `json:"local"`
}

// groupRustcBuilds folds rustc lines into one entry per cargo build: the
// latest time, the longest slot wait, and the dependency crates compiled.
// The first build of a target dir in the window counts as cold.
func groupRustcBuilds(entries []buildLaneEntry) []buildLaneEntry {
	type key struct {
		target string
		pid    int
	}
	builds := make([]buildLaneEntry, 0, len(entries))
	started := map[int]time.Time{}
	index := map[key]int{}
	for _, entry := range entries {
		if entry.Local == nil {
			builds = append(builds, entry)
			continue
		}
		k := key{entry.Target, entry.CargoPID}
		i, ok := index[k]
		if !ok {
			i = len(builds)
			index[k] = i
			builds = append(builds, buildLaneEntry{At: entry.At, ProjectID: entry.ProjectID, Target: entry.Target, Cmd: "cargo rebuilt"})
			started[i] = entry.At
		}
		build := &builds[i]
		if entry.At.After(build.At) {
			build.At = entry.At
		}
		if entry.At.Before(started[i]) {
			started[i] = entry.At
		}
		if entry.WaitMS > build.WaitMS {
			build.WaitMS = entry.WaitMS
		}
		if !*entry.Local {
			build.CompiledDeps++
			if build.CompiledDeps <= 3 {
				build.Cmd += " " + entry.Crate
			}
		}
	}
	order := make([]int, 0, len(started))
	for i := range started {
		order = append(order, i)
	}
	sort.Slice(order, func(a, b int) bool { return started[order[a]].Before(started[order[b]]) })
	seen := map[string]bool{}
	for _, i := range order {
		builds[i].Cold = !seen[builds[i].Target]
		seen[builds[i].Target] = true
	}
	return builds
}

var buildLaneCache struct {
	sync.Mutex
	path    string
	size    int64
	modTime time.Time
	entries []buildLaneEntry
}

func readBuildLaneStats(stateRoot string, now time.Time) map[string]buildLaneStats {
	path := filepath.Join(stateRoot, "build-lane", "builds.jsonl")
	info, err := os.Stat(path)
	if err != nil {
		return map[string]buildLaneStats{}
	}
	buildLaneCache.Lock()
	if buildLaneCache.path != path || buildLaneCache.size != info.Size() || !buildLaneCache.modTime.Equal(info.ModTime()) {
		data, err := os.ReadFile(path)
		if err != nil {
			buildLaneCache.Unlock()
			return map[string]buildLaneStats{}
		}
		entries := make([]buildLaneEntry, 0)
		for _, line := range bytes.Split(data, []byte{'\n'}) {
			var entry buildLaneEntry
			if json.Unmarshal(line, &entry) == nil && !entry.At.IsZero() {
				entries = append(entries, entry)
			}
		}
		buildLaneCache.path, buildLaneCache.size, buildLaneCache.modTime, buildLaneCache.entries = path, info.Size(), info.ModTime(), entries
	}
	entries := buildLaneCache.entries
	buildLaneCache.Unlock()

	byProject := map[string][]buildLaneEntry{}
	cutoff := now.Add(-30 * time.Minute)
	for _, entry := range entries {
		if !entry.At.Before(cutoff) && !entry.At.After(now) {
			byProject[entry.ProjectID] = append(byProject[entry.ProjectID], entry)
		}
	}
	stats := make(map[string]buildLaneStats, len(byProject))
	for projectID, builds := range byProject {
		builds = groupRustcBuilds(builds)
		sort.Slice(builds, func(i, j int) bool { return builds[i].At.After(builds[j].At) })
		if len(builds) > 10 {
			builds = builds[:10]
		}
		result := buildLaneStats{Builds: len(builds)}
		waits := make([]int64, 0, len(builds))
		seen := map[string]bool{}
		rebuilds := 0
		for _, build := range builds {
			waits = append(waits, build.WaitMS)
			if !build.Cold {
				result.WarmBuilds++
				if build.CompiledDeps > 0 {
					rebuilds++
				}
			}
			if !build.Cold && build.CompiledDeps > 0 && build.Cmd != "" && !seen[build.Cmd] && len(result.Commands) < 3 {
				seen[build.Cmd] = true
				result.Commands = append(result.Commands, build.Cmd)
			}
		}
		sort.Slice(waits, func(i, j int) bool { return waits[i] < waits[j] })
		result.MedianWaitMS = waits[len(waits)/2]
		if len(waits)%2 == 0 {
			result.MedianWaitMS = (waits[len(waits)/2-1] + waits[len(waits)/2]) / 2
		}
		if result.WarmBuilds > 0 {
			result.DepRebuildRate = float64(rebuilds) / float64(result.WarmBuilds)
		}
		stats[projectID] = result
	}
	return stats
}

func buildAdmissionReason(stats buildLaneStats, lane string, activeExecute bool) string {
	if lane != runLaneExecute {
		return ""
	}
	if stats.Builds >= 3 && stats.MedianWaitMS > buildWaitLimit.Milliseconds() {
		return fmt.Sprintf("waiting: build queue busy (median wait %ds)", stats.MedianWaitMS/1000)
	}
	if stats.WarmBuilds >= 5 && stats.DepRebuildRate > 0.3 && activeExecute {
		return fmt.Sprintf("serial: dependency cache keeps rebuilding (seen: %s); mismatched build flags usually cause this", strings.Join(stats.Commands, ", "))
	}
	return ""
}

func projectHasActiveExecute(runs []RunStatus, projectID, candidateRecordID string) bool {
	for _, run := range runs {
		if run.ProjectID == projectID && run.RecordID != candidateRecordID && firstNonEmpty(run.Lane, runLaneExecute) == runLaneExecute && runConsumesDispatchCapacity(run) {
			return true
		}
	}
	return false
}
