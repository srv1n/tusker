package main

// The frontier index is an in-memory projection only. Canonical Markdown and
// the runtime store remain authoritative; callers may discard this index after
// any uncertainty and rebuild it from an operational-note scan.

import (
	"sort"
	"strings"
	"time"
)

type frontierIndexCounters struct {
	RecordsStated   int           `json:"recordsStated"`
	RecordsRead     int           `json:"recordsRead"`
	RecordsParsed   int           `json:"recordsParsed"`
	GraphRecomputed int           `json:"graphNodesRecomputed"`
	ProjectsWoken   int           `json:"projectsWoken"`
	Elapsed         time.Duration `json:"elapsed"`
}

type frontierEligibility struct {
	Eligible bool   `json:"eligible"`
	Blocker  string `json:"blocker,omitempty"`
}

type projectFrontierIndex struct {
	ProjectID   string
	Records     map[string]Note
	Forward     map[string][]v7DependencyEdge // task -> prerequisites
	Reverse     map[string][]v7DependencyEdge // prerequisite -> dependents
	Waves       map[string][]string
	Eligibility map[string]frontierEligibility
	Frontier    []string
	Generation  uint64
}

func newProjectFrontierIndex(projectID string) *projectFrontierIndex {
	return &projectFrontierIndex{ProjectID: projectID, Records: map[string]Note{}, Forward: map[string][]v7DependencyEdge{}, Reverse: map[string][]v7DependencyEdge{}, Waves: map[string][]string{}, Eligibility: map[string]frontierEligibility{}}
}

func (p *projectFrontierIndex) rebuild(notes []Note) frontierIndexCounters {
	started := time.Now()
	p.Records, p.Forward, p.Reverse, p.Waves, p.Eligibility = map[string]Note{}, map[string][]v7DependencyEdge{}, map[string][]v7DependencyEdge{}, map[string][]string{}, map[string]frontierEligibility{}
	for _, note := range notes {
		id := stringField(note.Data, "id")
		if id != "" {
			p.Records[id] = note
		}
	}
	p.rebuildEdges()
	p.recompute(p.allTaskIDs())
	p.Generation++
	return frontierIndexCounters{RecordsStated: len(notes), RecordsRead: len(notes), RecordsParsed: len(notes), GraphRecomputed: len(p.Eligibility), ProjectsWoken: 1, Elapsed: time.Since(started)}
}

// apply replaces only changed operational records and recomputes their reverse
// dependency and wave closure. An empty or unknown change returns no partial
// update; its caller must use the adaptive full-rebuild recovery path.
func (p *projectFrontierIndex) apply(changed []Note) frontierIndexCounters {
	started := time.Now()
	if len(changed) == 0 {
		return frontierIndexCounters{ProjectsWoken: 1, Elapsed: time.Since(started)}
	}
	affected := map[string]bool{}
	// Capture the old closures before replacing records. Moving a task between
	// waves or retargeting a gate can invalidate both the old and new closure.
	for _, note := range changed {
		id := stringField(note.Data, "id")
		if id == "" {
			continue
		}
		if old, ok := p.Records[id]; ok {
			p.addRecordClosure(old, affected)
		}
	}
	for _, note := range changed {
		id := stringField(note.Data, "id")
		if id == "" {
			continue
		}
		p.removeRecord(id)
		p.Records[id] = note
		affected[id] = true
	}
	p.rebuildEdges()
	for _, note := range changed {
		if stringField(note.Data, "id") != "" {
			p.addRecordClosure(note, affected)
		}
	}
	affectedTasks := p.taskIDs(keysOf(affected))
	p.recompute(affectedTasks)
	p.Generation++
	return frontierIndexCounters{RecordsRead: len(changed), RecordsParsed: len(changed), GraphRecomputed: len(affectedTasks), ProjectsWoken: 1, Elapsed: time.Since(started)}
}

// touch recomputes a task-owned closure when canonical runtime state changed
// but no Markdown record did. The daemon will read the runtime store normally;
// reparsing the unchanged task note would add cost without adding truth.
func (p *projectFrontierIndex) touch(taskIDs []string) (frontierIndexCounters, bool) {
	started := time.Now()
	affected := map[string]bool{}
	for _, taskID := range uniqueStrings(taskIDs) {
		note, ok := p.Records[taskID]
		if !ok || effectiveV7Kind(note.Data) != "task" {
			return frontierIndexCounters{ProjectsWoken: 1, Elapsed: time.Since(started)}, false
		}
		p.addRecordClosure(note, affected)
	}
	affectedTasks := p.taskIDs(keysOf(affected))
	p.recompute(affectedTasks)
	p.Generation++
	return frontierIndexCounters{GraphRecomputed: len(affectedTasks), ProjectsWoken: 1, Elapsed: time.Since(started)}, true
}

func (p *projectFrontierIndex) removeRecord(id string) { delete(p.Records, id) }

func (p *projectFrontierIndex) rebuildEdges() {
	p.Forward, p.Reverse, p.Waves = map[string][]v7DependencyEdge{}, map[string][]v7DependencyEdge{}, map[string][]string{}
	idx := v7Index{Tasks: map[string]Note{}}
	for id, n := range p.Records {
		if effectiveV7Kind(n.Data) == "task" {
			idx.Tasks[id] = n
		}
	}
	for id, task := range idx.Tasks {
		edges := v7TaskDependencyEdges(task, idx)
		p.Forward[id] = edges
		for _, edge := range edges {
			p.Reverse[edge.ID] = append(p.Reverse[edge.ID], v7DependencyEdge{ID: id, Hardness: edge.Hardness})
		}
		if wave := stringField(task.Data, "wave"); wave != "" {
			p.Waves[wave] = append(p.Waves[wave], id)
		}
	}
	for wave := range p.Waves {
		sort.Strings(p.Waves[wave])
	}
}

func (p *projectFrontierIndex) addReverseClosure(id string, affected map[string]bool) {
	for _, edge := range p.Reverse[id] {
		if !affected[edge.ID] {
			affected[edge.ID] = true
			p.addReverseClosure(edge.ID, affected)
		}
	}
}

func (p *projectFrontierIndex) addWaveClosure(id string, affected map[string]bool) {
	task := p.Records[id]
	wave := stringField(task.Data, "wave")
	if wave == "" {
		return
	}
	for _, member := range p.Waves[wave] {
		affected[member] = true
	}
}

func (p *projectFrontierIndex) addRecordClosure(note Note, affected map[string]bool) {
	id := stringField(note.Data, "id")
	if id == "" {
		return
	}
	switch effectiveV7Kind(note.Data) {
	case "task":
		affected[id] = true
		p.addReverseClosure(id, affected)
		waveID := stringField(note.Data, "wave")
		for _, member := range p.Waves[waveID] {
			affected[member] = true
			p.addReverseClosure(member, affected)
		}
	case "wave":
		for _, member := range p.Waves[id] {
			affected[member] = true
			p.addReverseClosure(member, affected)
		}
		for _, member := range normalizeList(note.Data["members"]) {
			affected[member] = true
			p.addReverseClosure(member, affected)
		}
	case "gate":
		for _, taskID := range normalizeList(note.Data["blocks"]) {
			p.addTaskClosure(taskID, affected)
		}
		for taskID, task := range p.Records {
			if effectiveV7Kind(task.Data) == "task" && containsString(normalizeList(task.Data["gates"]), id) {
				p.addTaskClosure(taskID, affected)
			}
		}
	default:
		// Evidence, attempts, closeouts, and other task-owned operational
		// records can change proof/readiness without changing the task file.
		// Their canonical `task` back-pointer therefore invalidates the same
		// reverse and wave closure as a task mutation.
		p.addTaskClosure(stringField(note.Data, "task"), affected)
	}
}

func (p *projectFrontierIndex) addTaskClosure(taskID string, affected map[string]bool) {
	if taskID == "" {
		return
	}
	affected[taskID] = true
	p.addReverseClosure(taskID, affected)
	if task, ok := p.Records[taskID]; ok {
		for _, member := range p.Waves[stringField(task.Data, "wave")] {
			affected[member] = true
			p.addReverseClosure(member, affected)
		}
	}
}

func (p *projectFrontierIndex) allTaskIDs() []string {
	ids := []string{}
	for id, note := range p.Records {
		if effectiveV7Kind(note.Data) == "task" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

func (p *projectFrontierIndex) taskIDs(ids []string) []string {
	tasks := make([]string, 0, len(ids))
	for _, id := range ids {
		if note, ok := p.Records[id]; ok && effectiveV7Kind(note.Data) == "task" {
			tasks = append(tasks, id)
		} else {
			delete(p.Eligibility, id)
		}
	}
	sort.Strings(tasks)
	return tasks
}

func (p *projectFrontierIndex) recompute(ids []string) {
	// Eligibility is evaluated against the complete projection, while only the
	// requested closure is overwritten. This avoids stale soft-edge relocks.
	for _, id := range ids {
		p.Eligibility[id] = p.eligibility(id)
	}
	p.Frontier = make([]string, 0)
	for _, id := range p.allTaskIDs() {
		if p.Eligibility[id].Eligible {
			p.Frontier = append(p.Frontier, id)
		}
	}
	sort.Strings(p.Frontier)
}

func (p *projectFrontierIndex) eligibility(id string) frontierEligibility {
	task, ok := p.Records[id]
	if !ok || effectiveV7Kind(task.Data) != "task" {
		return frontierEligibility{Blocker: "task missing"}
	}
	status := strings.ToLower(stringField(task.Data, "status"))
	if status != "ready" && status != "rework" {
		return frontierEligibility{Blocker: "status " + status}
	}
	if wave := stringField(task.Data, "wave"); wave != "" && !p.waveAuthorizesTask(wave, id) {
		return frontierEligibility{Blocker: "wave authorization"}
	}
	for _, gate := range p.Records {
		if effectiveV7Kind(gate.Data) != "gate" || stringField(gate.Data, "status") != "open" || !boolField(gate.Data, "blocking") {
			continue
		}
		gateID := stringField(gate.Data, "id")
		if containsString(normalizeList(gate.Data["blocks"]), id) || containsString(normalizeList(task.Data["gates"]), gateID) {
			return frontierEligibility{Blocker: "gate " + gateID}
		}
	}
	for _, edge := range p.Forward[id] {
		dep, exists := p.Records[edge.ID]
		if !v7DependencySatisfiedForReadiness(edge, dep, exists) {
			return frontierEligibility{Blocker: "dependency " + edge.ID}
		}
	}
	return frontierEligibility{Eligible: true}
}

func (p *projectFrontierIndex) waveAuthorizesTask(id, taskID string) bool {
	wave, ok := p.Records[id]
	if !ok {
		return false
	}
	state := strings.ToLower(firstNonEmpty(stringField(wave.Data, "authorization"), stringField(wave.Data, "authorization_state")))
	return state == "armed" && containsString(normalizeList(wave.Data["members"]), taskID)
}

func keysOf(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func (p *projectFrontierIndex) notes() []Note {
	result := make([]Note, 0, len(p.Records))
	for _, note := range p.Records {
		result = append(result, note)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].RelativePath < result[j].RelativePath })
	return result
}

func (p *projectFrontierIndex) pathsForChanges(changes []daemonControlChange) ([]string, bool) {
	paths := make([]string, 0, len(changes))
	for _, change := range changes {
		note, ok := p.Records[change.ID]
		if !ok || note.AbsolutePath == "" {
			return nil, false
		}
		paths = append(paths, note.AbsolutePath)
	}
	return paths, true
}
