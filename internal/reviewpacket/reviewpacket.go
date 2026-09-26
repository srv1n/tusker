package reviewpacket

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

type Document struct {
	ItemID              string
	ItemTitle           string
	Run                 Run
	Turns               []Turn
	SupervisorDecisions []SupervisorDecision
	Facts               Facts
}

type Run struct {
	RecordID      string
	AttemptID     string
	Runner        string
	RunnerProfile string
	RunnerHarness string
	RunnerModel   string
	RunnerEffort  string
	Lane          string
	WorkRevision  int
	WorkspacePath string
	SessionRef    string
	StartedAt     string
	LastEventAt   string
	PromptPath    string
	EventSinkPath string
	RawLogPath    string
	StatusPath    string
}

type Turn struct {
	Index       int
	ID          string
	SessionRef  string
	Status      string
	LastEventAt string
	LastError   string
}

type SupervisorDecision struct {
	Kind             string
	Reason           string
	ParentAttemptID  string
	ParentSessionRef string
	BranchName       string
	WorkspacePath    string
	ContextSignal    string
	TotalTokens      int
	CreatedAt        string
	ValidationDelta  string
	MergeRule        string
}

type Facts struct {
	ChangedFiles                  []string
	ChangedFilesStatement         string
	DiffSummary                   []string
	DiffSummaryStatement          string
	CommandSummaries              []string
	CommandSummariesStatement     string
	VerificationCommands          []string
	VerificationCommandsStatement string
	ValidationSummaries           []string
	ValidationSummariesStatement  string
	SessionRefs                   []string
	TurnIDs                       []string
	RuntimeSummaries              []string
	OpenRisks                     []string
	SoftDependencyDependents      []string
}

func Render(doc Document) string {
	run, facts := doc.Run, doc.Facts
	var out []string
	out = append(out, "# Review packet", "")
	out = append(out, fmt.Sprintf("- Item: %s - %s", doc.ItemID, doc.ItemTitle))
	out = append(out, fmt.Sprintf("- Record: %s", run.RecordID))
	out = append(out, fmt.Sprintf("- Attempt: %s", run.AttemptID))
	out = append(out, fmt.Sprintf("- Runner: %s", run.Runner))
	out = append(out, fmt.Sprintf("- Runner profile: %s", fallback(run.RunnerProfile, "(none)")))
	out = append(out, fmt.Sprintf("- Harness: %s", fallback(run.RunnerHarness, run.Runner)))
	out = append(out, fmt.Sprintf("- Model: %s", fallback(run.RunnerModel, "(unknown)")))
	out = append(out, fmt.Sprintf("- Effort: %s", fallback(run.RunnerEffort, "(unknown)")))
	out = append(out, fmt.Sprintf("- Lane: %s", firstNonEmpty(run.Lane, "execute")))
	out = append(out, fmt.Sprintf("- Work revision: %d", run.WorkRevision))
	out = append(out, fmt.Sprintf("- Turns: %d", len(doc.Turns)))
	out = append(out, "- Usage telemetry: raw diagnostic data only; it is neither billable nor an exact aggregate.")
	out = append(out, fmt.Sprintf("- Workspace: %s", run.WorkspacePath))
	out = append(out, fmt.Sprintf("- Session: %s", run.SessionRef))
	out = append(out, fmt.Sprintf("- Started: %s", run.StartedAt))
	out = append(out, fmt.Sprintf("- Last event: %s", run.LastEventAt))
	out = append(out, "", "## Runtime summary", "")
	if len(facts.RuntimeSummaries) == 0 {
		out = append(out, "- No normalized runtime summary was recorded for this attempt.")
	} else {
		for _, summary := range facts.RuntimeSummaries {
			out = append(out, "- "+summary)
		}
	}
	out = append(out, "", "## Soft dependency blast radius", "")
	if len(facts.SoftDependencyDependents) == 0 {
		out = append(out, "- No soft-edge dependents were found for this task.")
	} else {
		out = append(out, facts.SoftDependencyDependents...)
	}
	out = append(out, "", "## Runtime artifacts", "")
	for _, artifact := range []struct{ label, path string }{
		{"prompt", run.PromptPath}, {"events", run.EventSinkPath},
		{"raw log pointer", run.RawLogPath}, {"status", run.StatusPath},
	} {
		if strings.TrimSpace(artifact.path) != "" {
			out = append(out, fmt.Sprintf("- %s: `%s`", artifact.label, artifact.path))
		}
	}
	out = append(out, "", "## Turns", "")
	if len(doc.Turns) == 0 {
		out = append(out, "- No normalized turns were recorded for this attempt.")
	} else {
		for _, turn := range doc.Turns {
			out = append(out, fmt.Sprintf("- #%d `%s` session=%s status=%s last_event=%s error=%s",
				turn.Index, turn.ID, firstNonEmpty(turn.SessionRef, "none"), turn.Status, turn.LastEventAt, firstNonEmpty(turn.LastError, "none")))
		}
	}
	out = append(out, "", "## Sessions and turns", "")
	sessionRefs := append([]string{}, facts.SessionRefs...)
	if ref := SafeText(run.SessionRef, 120); ref != "" {
		sessionRefs = append(sessionRefs, ref)
	}
	turnIDs := append([]string{}, facts.TurnIDs...)
	for _, turn := range doc.Turns {
		if ref := SafeText(turn.SessionRef, 120); ref != "" {
			sessionRefs = append(sessionRefs, ref)
		}
		if id := SafeText(turn.ID, 120); id != "" {
			turnIDs = append(turnIDs, id)
		}
	}
	if sessionRefs = DedupeStrings(sessionRefs); len(sessionRefs) == 0 {
		out = append(out, "- Session refs: none observed.")
	} else {
		out = append(out, "- Session refs: "+backtickList(sessionRefs))
	}
	if turnIDs = DedupeStrings(turnIDs); len(turnIDs) == 0 {
		out = append(out, "- Turn ids: none observed.")
	} else {
		out = append(out, "- Turn ids: "+backtickList(turnIDs))
	}
	out = append(out, "", "## Supervisor decisions", "")
	if len(doc.SupervisorDecisions) == 0 {
		out = append(out, "- No supervisor decisions were recorded for this attempt.")
	} else {
		for _, decision := range doc.SupervisorDecisions {
			out = append(out, fmt.Sprintf("- `%s` reason=%s parent_attempt=%s parent_session=%s branch=%s workspace=%s signal=%s tokens=%d at=%s",
				decision.Kind, firstNonEmpty(decision.Reason, "none"), firstNonEmpty(decision.ParentAttemptID, "none"), firstNonEmpty(decision.ParentSessionRef, "none"), firstNonEmpty(decision.BranchName, "none"), firstNonEmpty(decision.WorkspacePath, "none"), firstNonEmpty(decision.ContextSignal, "none"), decision.TotalTokens, decision.CreatedAt))
			if decision.ValidationDelta != "" || decision.MergeRule != "" {
				out = append(out, fmt.Sprintf("  validation_delta=%s merge_rule=%s", firstNonEmpty(decision.ValidationDelta, "none"), firstNonEmpty(decision.MergeRule, "none")))
			}
		}
	}
	appendSection := func(title, empty string, values []string) {
		out = append(out, "", title, "")
		if len(values) == 0 {
			out = append(out, "- "+empty)
			return
		}
		for _, value := range values {
			out = append(out, "- "+value)
		}
	}
	appendSection("## Changed files", firstNonEmpty(facts.ChangedFilesStatement, "No changed files were observed in normalized events or workspace status."), facts.ChangedFiles)
	appendSection("### Diff summary", firstNonEmpty(facts.DiffSummaryStatement, "No diff summary was observed in normalized events or workspace status."), facts.DiffSummary)
	appendSection("## Commands and tests", firstNonEmpty(facts.CommandSummariesStatement, "No command or test summaries were observed in normalized events."), facts.CommandSummaries)
	appendSection("## Verification", firstNonEmpty(facts.VerificationCommandsStatement, "No verification commands were observed in normalized events."), facts.VerificationCommands)
	appendSection("## Validation", firstNonEmpty(facts.ValidationSummariesStatement, "No validation results were observed in normalized events."), facts.ValidationSummaries)
	risks := append([]string{}, facts.OpenRisks...)
	for _, turn := range doc.Turns {
		if risk := SafeText(turn.LastError, 220); risk != "" {
			risks = append(risks, fmt.Sprintf("turn `%s`: %s", turn.ID, risk))
		}
	}
	for _, decision := range doc.SupervisorDecisions {
		kind := strings.ToLower(strings.TrimSpace(decision.Kind))
		if strings.Contains(kind, "stop") || strings.Contains(kind, "human") || strings.Contains(kind, "audit") {
			if reason := SafeText(firstNonEmpty(decision.Reason, decision.ValidationDelta, decision.ContextSignal), 220); reason != "" {
				risks = append(risks, fmt.Sprintf("supervisor `%s`: %s", decision.Kind, reason))
			}
		}
	}
	appendSection("## Open risks", "No open risks were observed in normalized events or runtime status.", DedupeStrings(risks))
	out = append(out, "- Reviewer must still check claims against the current tree before approval.")
	out = append(out, "- This packet summarizes daemon-observed runtime facts. It does not embed raw logs or full transcripts.")
	return strings.Join(out, "\n") + "\n"
}

func SafeText(value string, limit int) string {
	if value = strings.TrimSpace(value); value == "" {
		return ""
	}
	value = strings.Join(strings.Fields(strings.ReplaceAll(value, "\r\n", "\n")), " ")
	for _, pattern := range []string{`(?i)(authorization:\s*bearer\s+)[^\s]+`, `(?i)((?:api[_-]?key|access[_-]?token|refresh[_-]?token|token|password|secret)=)[^\s]+`, `(?i)((?:api[_-]?key|access[_-]?token|refresh[_-]?token|token|password|secret):\s*)[^\s]+`} {
		value = regexp.MustCompile(pattern).ReplaceAllString(value, "${1}[redacted]")
	}
	if limit > 0 && len(value) > limit {
		value = strings.TrimSpace(value[:limit]) + "..."
	}
	return value
}

func DedupeStrings(values []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, value := range values {
		if value = strings.TrimSpace(value); value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
func backtickList(values []string) string {
	var out []string
	for _, value := range values {
		if safe := SafeText(value, 120); safe != "" {
			out = append(out, "`"+safe+"`")
		}
	}
	return strings.Join(out, ", ")
}
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
func fallback(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
