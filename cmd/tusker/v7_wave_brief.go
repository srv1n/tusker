package main

import (
	"fmt"
	"sort"
	"strings"
)

const waveBriefSchema = "tusker.wave-brief/v1"

var waveBriefSectionOrder = []string{"outcome", "seeIt", "landed", "reworkParked", "humanAction", "documentation"}

type waveBrief struct {
	Schema       string                   `json:"schema"`
	WaveID       string                   `json:"waveId"`
	Title        string                   `json:"title"`
	WaveHref     string                   `json:"waveHref"`
	SectionOrder []string                 `json:"sectionOrder"`
	Outcome      waveBriefOutcome         `json:"outcome"`
	SeeIt        []waveBriefArtifact      `json:"seeIt"`
	Landed       []waveBriefTask          `json:"landed"`
	Rework       []waveBriefRework        `json:"reworkParked"`
	HumanAction  []waveBriefHumanAction   `json:"humanAction"`
	Docs         []waveBriefDocumentation `json:"documentation"`
}

type waveBriefOutcome struct {
	Summary      string          `json:"summary"`
	FullyDrained bool            `json:"fullyDrained"`
	Counts       map[string]int  `json:"counts"`
	Tasks        []waveTaskState `json:"tasks"`
}

type waveTaskState struct {
	TaskID         string `json:"taskId"`
	Title          string `json:"title"`
	TaskHref       string `json:"taskHref"`
	Implementation string `json:"implementation"`
	Proof          string `json:"proof"`
	Review         string `json:"review"`
	Landing        string `json:"landing"`
	Documentation  string `json:"documentation"`
	FirstFailure   string `json:"firstActionableFailure,omitempty"`
}

type waveBriefArtifact struct {
	TaskID        string   `json:"taskId"`
	TaskHref      string   `json:"taskHref"`
	Kind          string   `json:"kind"`
	Priority      int      `json:"priority"`
	Summary       string   `json:"summary"`
	AcceptanceIDs []string `json:"acceptanceIds"`
	EvidenceRef   string   `json:"evidenceRef"`
	ArtifactRef   string   `json:"artifactRef,omitempty"`
	EvidenceHref  string   `json:"evidenceHref"`
}

type waveBriefTask struct {
	TaskID   string `json:"taskId"`
	Title    string `json:"title"`
	TaskHref string `json:"taskHref"`
	Commit   string `json:"commit,omitempty"`
	Target   string `json:"target,omitempty"`
}

type waveBriefRework struct {
	TaskID        string   `json:"taskId"`
	Title         string   `json:"title"`
	TaskHref      string   `json:"taskHref"`
	State         string   `json:"state"`
	Failure       string   `json:"firstActionableFailure"`
	AffectedTasks []string `json:"affectedTaskIds"`
}

type waveBriefHumanAction struct {
	GateID         string   `json:"gateId"`
	GateHref       string   `json:"gateHref"`
	Action         string   `json:"action"`
	ResumeID       string   `json:"resumeId"`
	BlockedTaskIDs []string `json:"blockedTaskIds"`
}

type waveBriefDocumentation struct {
	TaskID   string `json:"taskId"`
	TaskHref string `json:"taskHref"`
	Node     string `json:"node"`
	NodeHref string `json:"nodeHref"`
	State    string `json:"state"`
}

func waveV7BriefCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	id := strings.ToUpper(strings.TrimSpace(firstNonEmpty(args.String("id"), args.String("_pos0"))))
	if id == "" {
		return tuskerError(errorMissingArg, "Usage: tusker wave brief W-0001 [--json]")
	}
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return err
	}
	wave, ok := idx.Waves[id]
	if !ok {
		return tuskerError(errorNotFound, "V7 wave not found: "+id)
	}
	brief := buildWaveBrief(idx, wave)
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "brief": brief})
		return nil
	}
	fmt.Print(renderWaveBrief(brief))
	return nil
}

func buildWaveBrief(idx v7Index, wave Note) waveBrief {
	waveID := stringField(wave.Data, "id")
	b := waveBrief{Schema: waveBriefSchema, WaveID: waveID, Title: stringField(wave.Data, "title"), WaveHref: waveDeepLink(waveID), SectionOrder: append([]string{}, waveBriefSectionOrder...)}
	b.Outcome.Counts = map[string]int{"implemented": 0, "proven": 0, "reviewed": 0, "landed": 0, "documented": 0, "reworkParked": 0, "humanAction": 0}
	landing := successfulWaveLandings(wave)
	members := normalizeList(wave.Data["members"])
	for _, taskID := range members {
		task, ok := idx.Tasks[taskID]
		if !ok {
			continue
		}
		state := waveBriefState(idx, task, landing[taskID] != nil)
		b.Outcome.Tasks = append(b.Outcome.Tasks, state)
		if state.Implementation == "present" {
			b.Outcome.Counts["implemented"]++
		}
		if state.Proof == "satisfied" || state.Proof == "waived" {
			b.Outcome.Counts["proven"]++
		}
		if state.Review == "accepted" {
			b.Outcome.Counts["reviewed"]++
		}
		if state.Landing == "landed" {
			b.Outcome.Counts["landed"]++
		}
		if state.Documentation == "documented" {
			b.Outcome.Counts["documented"]++
		}
		b.SeeIt = append(b.SeeIt, normalizeWaveArtifacts(idx, task)...)
		if row := landing[taskID]; row != nil {
			b.Landed = append(b.Landed, waveBriefTask{TaskID: taskID, Title: stringField(task.Data, "title"), TaskHref: taskDeepLink(taskID), Commit: stringField(row, "commit"), Target: stringField(row, "target")})
		}
		if item, ok := waveReworkItem(idx, task, state); ok {
			b.Rework = append(b.Rework, item)
		}
		for _, node := range normalizeList(task.Data["knowledge_nodes"]) {
			b.Docs = append(b.Docs, waveBriefDocumentation{TaskID: taskID, TaskHref: taskDeepLink(taskID), Node: node, NodeHref: docDeepLink(node), State: state.Documentation})
		}
	}
	b.HumanAction = validWaveHumanActions(idx, members)
	b.Outcome.Counts["reworkParked"] = len(b.Rework)
	b.Outcome.Counts["humanAction"] = len(b.HumanAction)
	sortWaveBrief(&b)
	b.Outcome.FullyDrained = len(b.Outcome.Tasks) == len(members) && len(b.Rework) == 0 && len(b.HumanAction) == 0
	for _, state := range b.Outcome.Tasks {
		if state.Proof != "satisfied" && state.Proof != "waived" || state.Review != "accepted" || state.Landing != "landed" || (state.Documentation == "pending") {
			b.Outcome.FullyDrained = false
		}
	}
	if b.Outcome.FullyDrained {
		b.Outcome.Summary = fmt.Sprintf("Delivered %d of %d tasks; the wave is proven, reviewed, landed, and documented as required.", len(b.Outcome.Tasks), len(members))
	} else {
		b.Outcome.Summary = fmt.Sprintf("Delivered %d of %d tasks; %d parked for machine rework and %d require human action.", b.Outcome.Counts["landed"], len(members), len(b.Rework), len(b.HumanAction))
	}
	return b
}

func waveBriefState(idx v7Index, task Note, landed bool) waveTaskState {
	status := strings.ToLower(stringField(task.Data, "status"))
	proof := strings.ToLower(fallback(stringField(task.Data, "proof_status"), "pending"))
	implemented := "absent"
	if status == "review" || status == "rework" || status == "done" || len(idx.Attempts[stringField(task.Data, "id")]) > 0 || len(parseV7VerificationRows(task.Body)) > 0 {
		implemented = "present"
	}
	review := "not_started"
	if status == "review" {
		review = "pending"
	}
	if status == "rework" {
		review = "changes_requested"
	}
	if status == "done" {
		review = "accepted"
	}
	doc := "not_required"
	if len(normalizeList(task.Data["knowledge_nodes"])) > 0 {
		doc = "pending"
		if status == "done" {
			doc = "documented"
		}
	}
	landingState := "not_landed"
	if landed {
		landingState = "landed"
	}
	return waveTaskState{TaskID: stringField(task.Data, "id"), Title: stringField(task.Data, "title"), TaskHref: taskDeepLink(stringField(task.Data, "id")), Implementation: implemented, Proof: proof, Review: review, Landing: landingState, Documentation: doc, FirstFailure: firstWaveTaskFailure(task)}
}

func firstWaveTaskFailure(task Note) string {
	for _, row := range parseV7VerificationRows(task.Body) {
		if serveProofResult(row.Result) == "fail" {
			return firstNonEmpty(strings.TrimSpace(row.Notes), strings.TrimSpace(row.Check), "verification failed")
		}
	}
	status := strings.ToLower(stringField(task.Data, "status"))
	readiness := strings.ToLower(stringField(task.Data, "readiness"))
	if status == "rework" || status == "cancelled" || status == "superseded" || readiness == "held" || strings.HasPrefix(readiness, "waiting_on_ci") {
		return firstNonEmpty(strings.TrimSpace(stringField(task.Data, "next_action")), "No actionable recovery was recorded.")
	}
	return ""
}

func waveReworkItem(idx v7Index, task Note, state waveTaskState) (waveBriefRework, bool) {
	status := strings.ToLower(stringField(task.Data, "status"))
	readiness := strings.ToLower(stringField(task.Data, "readiness"))
	gateFailure := invalidWaveGateFailure(idx, state.TaskID)
	parked := status == "rework" || status == "cancelled" || status == "superseded" || readiness == "held" || readiness == "waiting_on_ci" || state.FirstFailure != "" || gateFailure != ""
	if !parked {
		return waveBriefRework{}, false
	}
	return waveBriefRework{TaskID: state.TaskID, Title: state.Title, TaskHref: state.TaskHref, State: firstNonEmpty(status, readiness), Failure: firstNonEmpty(state.FirstFailure, gateFailure, "No actionable recovery was recorded."), AffectedTasks: waveDependentClosure(idx, state.TaskID)}, true
}

func invalidWaveGateFailure(idx v7Index, taskID string) string {
	for _, gate := range idx.Gates {
		if !strings.EqualFold(stringField(gate.Data, "status"), "open") || !serveGateBlocksTask(gate, taskID) {
			continue
		}
		owner := stringField(gate.Data, "owner")
		if !serveHumanOwner(owner) {
			return "Resolve the agent-owned gate " + stringField(gate.Data, "id") + "."
		}
		kind := strings.ToLower(stringField(gate.Data, "gate_kind"))
		action := strings.TrimSpace(stringField(gate.Data, "action"))
		verification := strings.TrimSpace(stringField(gate.Data, "verification"))
		why := strings.TrimSpace(stringField(gate.Data, "why_agent_cannot"))
		if action == "" || verification == "" || why == "" || v7HumanGateOwnsAgentCapableWork(kind, owner, action, verification, why, stringField(gate.Data, "suggestion")) {
			return "Return invalid human gate " + stringField(gate.Data, "id") + " to agent rework."
		}
	}
	return ""
}

func waveDependentClosure(idx v7Index, root string) []string {
	seen := map[string]bool{root: true}
	queue := []string{root}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for id, task := range idx.Tasks {
			if seen[id] {
				continue
			}
			for _, dep := range serveTaskDepIDs(task) {
				if dep == current {
					seen[id] = true
					queue = append(queue, id)
					break
				}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func successfulWaveLandings(wave Note) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, row := range normalizeLandingAudit(wave.Data["landings"]) {
		if strings.EqualFold(stringField(row, "gate_result"), "pass") {
			out[stringField(row, "task")] = row
		}
	}
	return out
}

func normalizeWaveArtifacts(idx v7Index, task Note) []waveBriefArtifact {
	taskID := stringField(task.Data, "id")
	acceptance := v7AcceptanceIDs(task.Body)
	contractKind := canonicalWaveArtifactKind(stringField(mapField(task.Data, "artifact_contract"), "kind"))
	out := []waveBriefArtifact{}
	for _, evidence := range idx.Evidence[taskID] {
		if strings.EqualFold(stringField(evidence.Data, "status"), "rejected") || strings.EqualFold(stringField(evidence.Data, "status"), "superseded") {
			continue
		}
		kind := waveArtifactKind(stringField(evidence.Data, "evidence_kind"))
		if contractKind != "" && (kind == "" || kind == "diff_summary") {
			kind = contractKind
		}
		if kind == "" {
			continue
		}
		covers := v7CoversToAcceptanceIDs(normalizeList(evidence.Data["covers"]), acceptance)
		if len(covers) == 0 {
			covers = append(covers, acceptance...)
		}
		summary := firstNonEmpty(strings.TrimSpace(sectionContent(evidence.Body, "## Summary")), stringField(evidence.Data, "title"), stringField(evidence.Data, "id"))
		paths := normalizeList(evidence.Data["artifact_paths"])
		if len(paths) == 0 {
			paths = []string{""}
		}
		for _, path := range paths {
			out = append(out, waveBriefArtifact{TaskID: taskID, TaskHref: taskDeepLink(taskID), Kind: kind, Priority: waveArtifactPriority(kind), Summary: summary, AcceptanceIDs: covers, EvidenceRef: stringField(evidence.Data, "id"), ArtifactRef: path, EvidenceHref: evidenceDeepLink(stringField(evidence.Data, "id"))})
		}
	}
	return out
}

func canonicalWaveArtifactKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "screenshot", "screenshot_set":
		return "screenshot"
	case "video", "recording":
		return "video"
	case "benchmark", "benchmark_delta":
		return "benchmark_delta"
	case "trace":
		return "trace"
	case "replay":
		return "replay"
	case "behavior_matrix", "matrix", "request_response":
		return "behavior_matrix"
	case "reliability_summary", "reliability_timeline":
		return "reliability_summary"
	case "security_note", "security_summary":
		return "security_note"
	case "diff_summary":
		return "diff_summary"
	case "knowledge_link", "documentation", "document":
		return "knowledge_link"
	default:
		return ""
	}
}

func waveArtifactKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "screenshot":
		return "screenshot"
	case "video":
		return "video"
	case "benchmark", "performance_profile":
		return "benchmark_delta"
	case "trace":
		return "trace"
	case "provider_probe", "integration_test", "e2e_test":
		return "behavior_matrix"
	case "release_smoke":
		return "reliability_summary"
	case "security_review", "privacy_review":
		return "security_note"
	case "verification_summary", "automated_test", "unit_test":
		return "diff_summary"
	default:
		return ""
	}
}

func waveArtifactPriority(kind string) int {
	switch kind {
	case "screenshot", "video":
		return 1
	case "benchmark_delta":
		return 2
	case "trace", "behavior_matrix":
		return 3
	case "reliability_summary", "security_note":
		return 4
	default:
		return 5
	}
}

func validWaveHumanActions(idx v7Index, members []string) []waveBriefHumanAction {
	member := map[string]bool{}
	for _, id := range members {
		member[id] = true
	}
	out := []waveBriefHumanAction{}
	for _, gate := range idx.Gates {
		if !strings.EqualFold(stringField(gate.Data, "status"), "open") || !serveHumanOwner(stringField(gate.Data, "owner")) {
			continue
		}
		blocks := []string{}
		for _, id := range serveGateBlockIDs(gate) {
			if member[id] {
				blocks = append(blocks, id)
			}
		}
		if len(blocks) == 0 {
			continue
		}
		kind := strings.ToLower(stringField(gate.Data, "gate_kind"))
		action := strings.TrimSpace(stringField(gate.Data, "action"))
		verification := strings.TrimSpace(stringField(gate.Data, "verification"))
		why := strings.TrimSpace(stringField(gate.Data, "why_agent_cannot"))
		if action == "" || verification == "" || why == "" || v7HumanGateOwnsAgentCapableWork(kind, stringField(gate.Data, "owner"), action, verification, why, stringField(gate.Data, "suggestion")) {
			continue
		}
		sort.Strings(blocks)
		id := stringField(gate.Data, "id")
		out = append(out, waveBriefHumanAction{GateID: id, GateHref: gateDeepLink(id), Action: action, ResumeID: id, BlockedTaskIDs: blocks})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].GateID < out[j].GateID })
	return out
}

func sortWaveBrief(b *waveBrief) {
	sort.SliceStable(b.SeeIt, func(i, j int) bool {
		if b.SeeIt[i].Priority != b.SeeIt[j].Priority {
			return b.SeeIt[i].Priority < b.SeeIt[j].Priority
		}
		if b.SeeIt[i].TaskID != b.SeeIt[j].TaskID {
			return b.SeeIt[i].TaskID < b.SeeIt[j].TaskID
		}
		return b.SeeIt[i].EvidenceRef < b.SeeIt[j].EvidenceRef
	})
	sort.Slice(b.Landed, func(i, j int) bool { return b.Landed[i].TaskID < b.Landed[j].TaskID })
	sort.Slice(b.Rework, func(i, j int) bool { return b.Rework[i].TaskID < b.Rework[j].TaskID })
	sort.Slice(b.Docs, func(i, j int) bool {
		if b.Docs[i].TaskID != b.Docs[j].TaskID {
			return b.Docs[i].TaskID < b.Docs[j].TaskID
		}
		return b.Docs[i].Node < b.Docs[j].Node
	})
}

func renderWaveBrief(b waveBrief) string {
	var s strings.Builder
	s.WriteString(fmt.Sprintf("# %s · %s\n\n", b.WaveID, b.Title))
	s.WriteString("## Outcome\n\n" + b.Outcome.Summary + "\n\n")
	for _, t := range b.Outcome.Tasks {
		s.WriteString(fmt.Sprintf("- %s — implementation: %s; proof: %s; review: %s; landing: %s; documentation: %s", t.TaskID, t.Implementation, t.Proof, t.Review, t.Landing, t.Documentation))
		if t.FirstFailure != "" {
			s.WriteString("; first failure: " + t.FirstFailure)
		}
		s.WriteString("\n")
	}
	s.WriteString("\n## See it\n\n")
	if len(b.SeeIt) == 0 {
		s.WriteString("- None.\n")
	} else {
		for _, a := range b.SeeIt {
			s.WriteString(fmt.Sprintf("- [%s] %s · %s · acceptance %s · evidence %s", a.Kind, a.TaskID, a.Summary, strings.Join(a.AcceptanceIDs, ","), a.EvidenceRef))
			if a.ArtifactRef != "" {
				s.WriteString(" · " + a.ArtifactRef)
			}
			s.WriteString("\n")
		}
	}
	s.WriteString("\n## Landed\n\n")
	if len(b.Landed) == 0 {
		s.WriteString("- None.\n")
	} else {
		for _, x := range b.Landed {
			s.WriteString(fmt.Sprintf("- %s · %s", x.TaskID, x.Title))
			if x.Commit != "" {
				s.WriteString(" · " + x.Commit)
			}
			s.WriteString("\n")
		}
	}
	s.WriteString("\n## Rework/parked\n\n")
	if len(b.Rework) == 0 {
		s.WriteString("- None.\n")
	} else {
		for _, x := range b.Rework {
			s.WriteString(fmt.Sprintf("- %s · %s · %s (affected: %s)\n", x.TaskID, x.State, x.Failure, strings.Join(x.AffectedTasks, ", ")))
		}
	}
	s.WriteString("\n## Human action\n\n")
	if len(b.HumanAction) == 0 {
		s.WriteString("- None.\n")
	} else {
		for _, x := range b.HumanAction {
			s.WriteString(fmt.Sprintf("- %s · %s · resume: %s\n", x.GateID, x.Action, x.ResumeID))
		}
	}
	s.WriteString("\n## Documentation\n\n")
	if len(b.Docs) == 0 {
		s.WriteString("- None.\n")
	} else {
		for _, x := range b.Docs {
			s.WriteString(fmt.Sprintf("- %s · %s · %s\n", x.TaskID, x.Node, x.State))
		}
	}
	return s.String()
}

func waveDeepLink(id string) string     { return "/waves/" + id }
func taskDeepLink(id string) string     { return "/tasks/" + id }
func gateDeepLink(id string) string     { return "/gates/" + id }
func evidenceDeepLink(id string) string { return "/evidence/" + id }
func docDeepLink(path string) string    { return "/docs?path=" + path }
