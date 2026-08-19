package reviewpacket

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
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

func ParseEvents(content string) []map[string]any {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	if strings.HasPrefix(content, "[") {
		var events []map[string]any
		if json.Unmarshal([]byte(content), &events) == nil {
			return events
		}
	}
	if strings.HasPrefix(content, "{") {
		var event map[string]any
		if json.Unmarshal([]byte(content), &event) == nil {
			return []map[string]any{event}
		}
	}
	var events []map[string]any
	for _, line := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		var event map[string]any
		if line = strings.TrimSpace(line); line != "" && json.Unmarshal([]byte(line), &event) == nil {
			events = append(events, event)
		}
	}
	return events
}

func AnalyzeEvents(events []map[string]any) Facts {
	facts := Facts{
		ChangedFilesStatement:         "No changed files were observed in normalized events or workspace status.",
		DiffSummaryStatement:          "No diff summary was observed in normalized events or workspace status.",
		CommandSummariesStatement:     "No command or test summaries were observed in normalized events.",
		VerificationCommandsStatement: "No verification commands were observed in normalized events.",
		ValidationSummariesStatement:  "No validation results were observed in normalized events.",
	}
	for _, event := range events {
		kind, payload := eventKind(event), eventPayload(event)
		if payload == nil {
			continue
		}
		if strings.Contains(kind, "file") || strings.Contains(kind, "change") || payload["changed_files"] != nil || payload["files"] != nil {
			for _, path := range payloadPathValues(payload) {
				facts.ChangedFiles = append(facts.ChangedFiles, fmt.Sprintf("`%s` (event:%s)", path, firstNonEmpty(kind, "file_change")))
			}
		}
		for _, key := range []string{"diff_summary", "diff_stat", "diffstat", "stat"} {
			if summary := SafeText(stringValue(payload[key]), 240); summary != "" {
				facts.DiffSummary = append(facts.DiffSummary, fmt.Sprintf("%s (event:%s)", summary, firstNonEmpty(kind, "diff_summary")))
			}
		}
		facts.DiffSummary = append(facts.DiffSummary, fileDiffSummaries(payload["changed_files"], kind)...)
		facts.DiffSummary = append(facts.DiffSummary, fileDiffSummaries(payload["files"], kind)...)
		path := firstNonEmpty(pathValuesJoined(payload["path"]), pathValuesJoined(payload["file"]))
		if path != "" && (payload["insertions"] != nil || payload["deletions"] != nil || payload["additions"] != nil) {
			facts.DiffSummary = append(facts.DiffSummary, fmt.Sprintf("`%s` +%d -%d (event:%s)", path,
				packetInt(firstNonEmptyAny(payload["insertions"], payload["additions"], payload["added"])),
				packetInt(firstNonEmptyAny(payload["deletions"], payload["removals"], payload["removed"])), firstNonEmpty(kind, "diff_summary")))
		}
		command := firstNonEmpty(stringValue(payload["check"]), stringValue(payload["command"]), stringValue(payload["cmd"]), stringValue(payload["argv"]))
		if command != "" && (strings.Contains(kind, "verification") || strings.Contains(kind, "command") || strings.Contains(kind, "test")) {
			facts.VerificationCommands = append(facts.VerificationCommands, commandSummary(event, payload, command, false))
		}
		if command != "" && (strings.Contains(kind, "attempt") || strings.Contains(kind, "command") || strings.Contains(kind, "test") || strings.Contains(kind, "verification") || strings.Contains(kind, "validation")) {
			facts.CommandSummaries = append(facts.CommandSummaries, commandSummary(event, payload, command, true))
		}
		validationCommand := firstNonEmpty(stringValue(payload["validation_command"]), stringValue(payload["check"]), stringValue(payload["command"]), stringValue(payload["cmd"]))
		if strings.Contains(kind, "validation") || strings.Contains(strings.ToLower(validationCommand), "tusker validate") || payload["validation_result"] != nil {
			facts.ValidationSummaries = append(facts.ValidationSummaries, validationSummary(kind, payload, validationCommand))
		}
		if ref := sessionRefFromEvent(event); ref != "" {
			facts.SessionRefs = append(facts.SessionRefs, ref)
		}
		turnID := SafeText(firstNonEmpty(stringValue(payload["turn_id"]), stringValue(payload["turnId"]), stringValue(event["turn_id"]), stringValue(event["turnId"])), 120)
		if turnID != "" {
			facts.TurnIDs = append(facts.TurnIDs, turnID)
		}
		facts.OpenRisks = append(facts.OpenRisks, risksFromEvent(event, kind, payload)...)
	}
	facts.ChangedFiles = DedupeStrings(facts.ChangedFiles)
	facts.DiffSummary = DedupeStrings(facts.DiffSummary)
	facts.CommandSummaries = DedupeStrings(facts.CommandSummaries)
	facts.VerificationCommands = DedupeStrings(facts.VerificationCommands)
	facts.ValidationSummaries = DedupeStrings(facts.ValidationSummaries)
	facts.SessionRefs = DedupeStrings(facts.SessionRefs)
	facts.TurnIDs = DedupeStrings(facts.TurnIDs)
	facts.OpenRisks = DedupeStrings(facts.OpenRisks)
	return facts
}

func commandSummary(event, payload map[string]any, command string, verbose bool) string {
	kind := eventKind(event)
	command = SafeText(command, 260)
	result := SafeText(firstNonEmpty(stringValue(payload["result"]), stringValue(payload["status"]), stringValue(payload["outcome"]), "observed"), 80)
	if verbose && strings.Contains(kind, "attempt_started") {
		result = "started"
	}
	if !verbose {
		detail := fmt.Sprintf("`%s` result=%s", command, result)
		if value := firstNonEmpty(stringValue(payload["exit_code"]), stringValue(payload["exitCode"])); value != "" {
			detail += " exit_code=" + value
		}
		if value := firstNonEmpty(stringValue(payload["turn_id"]), stringValue(payload["turnId"])); value != "" {
			detail += " turn=" + value
		}
		if value := firstNonEmpty(stringValue(event["at"]), stringValue(payload["at"]), stringValue(payload["normalized_at"])); value != "" {
			detail += " at=" + value
		}
		return detail
	}
	parts := []string{fmt.Sprintf("`%s` kind=%s result=%s", command, firstNonEmpty(kind, "command"), result)}
	if value := firstNonEmpty(stringValue(payload["exit_code"]), stringValue(payload["exitCode"])); value != "" {
		parts = append(parts, "exit_code="+value)
	}
	if value := durationText(payload); value != "" {
		parts = append(parts, "duration="+value)
	}
	if value := firstNonEmpty(stringValue(payload["turn_id"]), stringValue(payload["turnId"])); value != "" {
		parts = append(parts, "turn="+SafeText(value, 120))
	}
	if value := sessionRefFromEvent(event); value != "" {
		parts = append(parts, "session="+value)
	}
	if value := SafeText(firstNonEmpty(stringValue(payload["summary"]), stringValue(payload["note"])), 180); value != "" {
		parts = append(parts, "summary="+value)
	}
	return strings.Join(parts, " ")
}

func validationSummary(kind string, payload map[string]any, command string) string {
	result := firstNonEmpty(stringValue(payload["validation_result"]), stringValue(payload["result"]), stringValue(payload["status"]), stringValue(payload["outcome"]))
	if command = SafeText(command, 260); command == "" {
		command = firstNonEmpty(kind, "validation")
	}
	parts := []string{fmt.Sprintf("`%s` result=%s", command, SafeText(firstNonEmpty(result, "observed"), 80))}
	if value := firstNonEmpty(stringValue(payload["exit_code"]), stringValue(payload["exitCode"])); value != "" {
		parts = append(parts, "exit_code="+value)
	}
	if value := SafeText(firstNonEmpty(stringValue(payload["summary"]), stringValue(payload["note"])), 180); value != "" {
		parts = append(parts, "summary="+value)
	}
	return strings.Join(parts, " ")
}

func risksFromEvent(event map[string]any, kind string, payload map[string]any) []string {
	var out []string
	for _, key := range []string{"open_risks", "risks"} {
		for _, risk := range normalizeList(payload[key]) {
			if safe := SafeText(risk, 220); safe != "" {
				out = append(out, safe)
			}
		}
	}
	if risk := SafeText(stringValue(payload["risk"]), 220); risk != "" && (strings.Contains(kind, "risk") || strings.Contains(kind, "blocked")) {
		out = append(out, risk)
	}
	result := strings.ToLower(strings.TrimSpace(firstNonEmpty(stringValue(payload["result"]), stringValue(payload["status"]), stringValue(payload["outcome"]))))
	if result == "fail" || result == "failed" || result == "error" || result == "blocked" {
		if command := SafeText(firstNonEmpty(stringValue(payload["check"]), stringValue(payload["command"]), stringValue(payload["cmd"])), 180); command != "" {
			out = append(out, fmt.Sprintf("`%s` result=%s", command, result))
		}
	}
	if strings.Contains(kind, "denied") || strings.Contains(kind, "error") || strings.Contains(kind, "blocked") || strings.Contains(kind, "failed") {
		if reason := SafeText(firstNonEmpty(stringValue(payload["reason"]), stringValue(payload["error"]), stringValue(payload["last_error"]), stringValue(event["message"])), 220); reason != "" {
			out = append(out, fmt.Sprintf("%s: %s", firstNonEmpty(kind, "runtime"), reason))
		}
	}
	return out
}

func eventKind(event map[string]any) string {
	return strings.ToLower(strings.TrimSpace(firstNonEmpty(stringValue(event["kind"]), stringValue(event["event_kind"]), stringValue(event["action"]), stringValue(event["type"]))))
}
func eventPayload(event map[string]any) map[string]any {
	if value, ok := event["payload"].(map[string]any); ok {
		return value
	}
	if value, ok := event["data"].(map[string]any); ok {
		return value
	}
	return event
}
func payloadPathValues(payload map[string]any) []string {
	var out []string
	for _, key := range []string{"path", "file", "file_path", "filename", "relative_path", "target_path", "changed_files", "files", "paths"} {
		out = append(out, pathValues(payload[key])...)
	}
	return DedupeStrings(out)
}
func pathValues(value any) []string {
	switch value := value.(type) {
	case string:
		if path := SafeText(value, 240); path != "" {
			return []string{path}
		}
	case []string:
		var out []string
		for _, item := range value {
			out = append(out, pathValues(item)...)
		}
		return out
	case []any:
		var out []string
		for _, item := range value {
			out = append(out, pathValues(item)...)
		}
		return out
	case map[string]any:
		return payloadPathValues(value)
	}
	return nil
}
func pathValuesJoined(value any) string {
	values := pathValues(value)
	if len(values) > 0 {
		return values[0]
	}
	return ""
}
func firstNonEmptyAny(values ...any) any {
	for _, value := range values {
		if strings.TrimSpace(stringValue(value)) != "" {
			return value
		}
	}
	return nil
}

func fileDiffSummaries(value any, kind string) []string {
	var out []string
	switch value := value.(type) {
	case []any:
		for _, item := range value {
			out = append(out, fileDiffSummaries(item, kind)...)
		}
	case []map[string]any:
		for _, item := range value {
			out = append(out, fileDiffSummaries(item, kind)...)
		}
	case map[string]any:
		path := firstNonEmpty(pathValuesJoined(value["path"]), pathValuesJoined(value["file"]), pathValuesJoined(value["filename"]))
		if path == "" {
			return nil
		}
		insertions := packetInt(firstNonEmptyAny(value["insertions"], value["additions"], value["added"]))
		deletions := packetInt(firstNonEmptyAny(value["deletions"], value["removals"], value["removed"]))
		status := SafeText(firstNonEmpty(stringValue(value["status"]), stringValue(value["change_type"]), "changed"), 80)
		if insertions == 0 && deletions == 0 {
			out = append(out, fmt.Sprintf("`%s` %s (event:%s)", path, status, firstNonEmpty(kind, "diff_summary")))
		} else {
			out = append(out, fmt.Sprintf("`%s` %s +%d -%d (event:%s)", path, status, insertions, deletions, firstNonEmpty(kind, "diff_summary")))
		}
	}
	return out
}

func sessionRefFromEvent(event map[string]any) string {
	payload := eventPayload(event)
	return SafeText(firstNonEmpty(stringValue(payload["session_ref"]), stringValue(payload["session_id"]), stringValue(payload["sessionId"]), stringValue(payload["thread_id"]), stringValue(payload["threadId"]), stringValue(event["session_ref"]), stringValue(event["session_id"]), stringValue(event["thread_id"])), 120)
}
func durationText(payload map[string]any) string {
	if value := packetInt(firstNonEmptyAny(payload["duration_ms"], payload["elapsed_ms"])); value > 0 {
		return (time.Duration(value) * time.Millisecond).Round(time.Millisecond).String()
	}
	if value := packetInt(firstNonEmptyAny(payload["duration_seconds"], payload["elapsed_seconds"])); value > 0 {
		return (time.Duration(value) * time.Second).String()
	}
	return ""
}
func packetInt(value any) int {
	switch value := value.(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		n, _ := value.Int64()
		return int(n)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(value))
		return n
	}
	return 0
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
func stringValue(value any) string {
	switch value := value.(type) {
	case nil:
		return ""
	case string:
		return value
	case fmt.Stringer:
		return value.String()
	case int:
		return strconv.Itoa(value)
	case int64:
		return strconv.FormatInt(value, 10)
	case float64:
		if float64(int(value)) == value {
			return strconv.Itoa(int(value))
		}
		return strconv.FormatFloat(value, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(value)
	default:
		return fmt.Sprint(value)
	}
}
func normalizeList(value any) []string {
	switch value := value.(type) {
	case nil:
		return nil
	case []string:
		var out []string
		for _, item := range value {
			if item = strings.TrimSpace(item); item != "" {
				out = append(out, item)
			}
		}
		return out
	case []any:
		var out []string
		for _, item := range value {
			if text := strings.TrimSpace(stringValue(item)); text != "" {
				out = append(out, text)
			}
		}
		return out
	case string:
		if value = strings.TrimSpace(value); value != "" {
			return []string{value}
		}
	default:
		if text := strings.TrimSpace(stringValue(value)); text != "" {
			return []string{text}
		}
	}
	return nil
}
