package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

const (
	traceReplayModeMock      = "mock"
	traceReplayModeLiveTools = "live-tools"
)

type TraceStateTransition struct {
	Type    string   `json:"type"`
	Subject string   `json:"subject,omitempty"`
	From    string   `json:"from,omitempty"`
	To      string   `json:"to,omitempty"`
	Covers  string   `json:"covers,omitempty"`
	Check   string   `json:"check,omitempty"`
	Result  string   `json:"result,omitempty"`
	Files   []string `json:"files,omitempty"`
}

type TraceReplayEnvironment struct {
	WorktreePath      string `json:"worktree_path"`
	GitHistoryPresent bool   `json:"git_history_present"`
	NetworkAccess     string `json:"network_access"`
	NetworkEnabled    bool   `json:"network_enabled"`
}

type TraceReplayBoundaryReport struct {
	TraceID             string                 `json:"trace_id"`
	NodeID              string                 `json:"node_id"`
	NodeType            string                 `json:"node_type"`
	Mode                string                 `json:"mode"`
	ExpectedTransitions []TraceStateTransition `json:"expected_transitions"`
	ActualTransitions   []TraceStateTransition `json:"actual_transitions"`
	Diverged            bool                   `json:"diverged"`
	Divergence          string                 `json:"divergence,omitempty"`
}

type TraceReplayReport struct {
	TraceID             string                      `json:"trace_id"`
	AttemptID           string                      `json:"attempt_id"`
	TracePath           string                      `json:"trace_path"`
	Mode                string                      `json:"mode"`
	Passed              bool                        `json:"passed"`
	FirstDivergence     string                      `json:"first_divergence,omitempty"`
	DivergentBoundaries []string                    `json:"divergent_boundaries,omitempty"`
	ExpectedTransitions []TraceStateTransition      `json:"expected_transitions"`
	ActualTransitions   []TraceStateTransition      `json:"actual_transitions"`
	Boundaries          []TraceReplayBoundaryReport `json:"boundaries"`
	ModelCalls          int                         `json:"model_calls"`
	NetworkCalls        int                         `json:"network_calls"`
	ToolCalls           int                         `json:"tool_calls"`
	LiveToolCalls       int                         `json:"live_tool_calls"`
	Environment         TraceReplayEnvironment      `json:"environment"`
}

type TraceReplayOptions struct {
	VaultPath       string
	TraceID         string
	Mode            string
	RepoRoot        string
	KeepEnvironment bool
	ToolExecutor    TraceReplayToolExecutor
}

type TraceReplayToolExecutor interface {
	ExecuteTraceReplayTool(context.Context, TraceRecord, TraceReplayEnvironment) (json.RawMessage, error)
}

type shellTraceReplayToolExecutor struct{}

func traceReplayCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	traceID := firstNonEmpty(args.String("id"), args.String("_pos0"))
	if strings.TrimSpace(traceID) == "" {
		return tuskerError(errorMissingArg, "trace replay requires <trace-id>")
	}
	mode := strings.ToLower(fallback(args.String("mode"), traceReplayModeMock))
	report, err := ReplayTrace(context.Background(), TraceReplayOptions{
		VaultPath: vaultPath,
		TraceID:   traceID,
		Mode:      mode,
		RepoRoot:  firstNonEmpty(args.String("repo"), mustGetwd()),
	})
	if err != nil {
		return err
	}
	if args.Bool("json") && report.Passed {
		emitJSON(map[string]any{"ok": true, "replay": report})
	} else if !args.Bool("json") {
		printTraceReplayReport(report)
	}
	if !report.Passed {
		return tuskerError("TRACE_REPLAY_DIVERGED", "trace replay diverged: "+report.FirstDivergence, withContext(report))
	}
	return nil
}

func ReplayTrace(ctx context.Context, opts TraceReplayOptions) (TraceReplayReport, error) {
	mode := strings.ToLower(fallback(opts.Mode, traceReplayModeMock))
	if mode != traceReplayModeMock && mode != traceReplayModeLiveTools {
		return TraceReplayReport{}, tuskerError(errorInvalidArg, "invalid trace replay mode: "+mode, withHint("use --mode mock or --mode live-tools"))
	}
	traceID := strings.TrimSpace(opts.TraceID)
	if traceID == "" {
		return TraceReplayReport{}, tuskerError(errorMissingArg, "trace replay requires trace_id")
	}
	record, tracePath, err := findTraceRecord(opts.VaultPath, traceID)
	if err != nil {
		return TraceReplayReport{}, err
	}
	records, err := readTraceRecords(tracePath)
	if err != nil {
		return TraceReplayReport{}, err
	}
	env, cleanup, err := prepareTraceReplayEnvironment(mode, opts)
	if err != nil {
		return TraceReplayReport{}, err
	}
	if !opts.KeepEnvironment {
		defer cleanup()
	}
	executor := opts.ToolExecutor
	if executor == nil {
		executor = shellTraceReplayToolExecutor{}
	}
	report := TraceReplayReport{
		TraceID:     traceID,
		AttemptID:   strings.TrimSuffix(filepath.Base(tracePath), ".jsonl"),
		TracePath:   tracePath,
		Mode:        mode,
		Passed:      true,
		Environment: env,
	}
	_ = record
	for _, boundaryRecord := range records {
		boundary := TraceReplayBoundaryReport{
			TraceID:  boundaryRecord.TraceID,
			NodeID:   boundaryRecord.NodeID,
			NodeType: boundaryRecord.NodeType,
			Mode:     mode,
		}
		if boundaryRecord.NodeType == "tool" {
			report.ToolCalls++
		}
		expected := expectedTraceTransitions(boundaryRecord)
		actualOutput := boundaryRecord.Output
		outputDivergence := ""
		if mode == traceReplayModeLiveTools && boundaryRecord.NodeType == "tool" {
			report.LiveToolCalls++
			liveOutput, err := executor.ExecuteTraceReplayTool(ctx, boundaryRecord, env)
			if err != nil {
				actualOutput = traceReplayErrorOutput(boundaryRecord, err)
			} else {
				actualOutput = liveOutput
			}
			if !jsonRawEqual(boundaryRecord.Output, actualOutput) {
				outputDivergence = "boundary " + boundaryRecord.TraceID + " output diverged from recording"
			}
		}
		actual := actualTraceTransitions(actualOutput)
		if len(expected) == 0 {
			expected = actualTraceTransitions(boundaryRecord.Output)
		}
		boundary.ExpectedTransitions = expected
		boundary.ActualTransitions = actual
		report.ExpectedTransitions = append(report.ExpectedTransitions, expected...)
		report.ActualTransitions = append(report.ActualTransitions, actual...)
		if divergence := firstTransitionDivergence(expected, actual); divergence != "" {
			boundary.Diverged = true
			boundary.Divergence = "boundary " + boundaryRecord.TraceID + ": " + divergence
		} else if outputDivergence != "" {
			boundary.Diverged = true
			boundary.Divergence = outputDivergence
		}
		if boundary.Diverged {
			report.Passed = false
			report.DivergentBoundaries = append(report.DivergentBoundaries, boundaryRecord.TraceID)
			if report.FirstDivergence == "" {
				report.FirstDivergence = boundary.Divergence
			}
		}
		report.Boundaries = append(report.Boundaries, boundary)
	}
	if divergence := firstTransitionDivergence(report.ExpectedTransitions, report.ActualTransitions); divergence != "" {
		report.Passed = false
		if report.FirstDivergence == "" {
			report.FirstDivergence = divergence
		}
	}
	if report.FirstDivergence == "" {
		report.FirstDivergence = "none"
	}
	report.DivergentBoundaries = uniqueStrings(report.DivergentBoundaries)
	return report, nil
}

func printTraceReplayReport(report TraceReplayReport) {
	result := "FAIL"
	if report.Passed {
		result = "PASS"
	}
	fmt.Printf("trace replay %s mode=%s %s\n", report.TraceID, report.Mode, result)
	fmt.Printf("attempt=%s boundaries=%d transitions=%d\n", report.AttemptID, len(report.Boundaries), len(report.ActualTransitions))
	fmt.Printf("calls model=%d live_tools=%d network=%d\n", report.ModelCalls, report.LiveToolCalls, report.NetworkCalls)
	fmt.Printf("environment worktree=%s git_history=%t network=%s\n", report.Environment.WorktreePath, report.Environment.GitHistoryPresent, report.Environment.NetworkAccess)
	if !report.Passed {
		fmt.Printf("first divergence: %s\n", report.FirstDivergence)
		if len(report.DivergentBoundaries) > 0 {
			fmt.Printf("divergent boundaries: %s\n", strings.Join(report.DivergentBoundaries, ","))
		}
	}
}

func prepareTraceReplayEnvironment(mode string, opts TraceReplayOptions) (TraceReplayEnvironment, func(), error) {
	root, err := os.MkdirTemp("", "tusker-trace-replay-*")
	if err != nil {
		return TraceReplayEnvironment{}, func() {}, err
	}
	worktree := filepath.Join(root, "worktree")
	if err := ensureDir(worktree); err != nil {
		_ = os.RemoveAll(root)
		return TraceReplayEnvironment{}, func() {}, err
	}
	networkAccess := "host"
	networkEnabled := true
	if mode == traceReplayModeMock {
		networkAccess = "off"
		networkEnabled = false
	}
	env := TraceReplayEnvironment{
		WorktreePath:      worktree,
		GitHistoryPresent: dirExists(filepath.Join(worktree, ".git")),
		NetworkAccess:     networkAccess,
		NetworkEnabled:    networkEnabled,
	}
	return env, func() { _ = os.RemoveAll(root) }, nil
}

func (shellTraceReplayToolExecutor) ExecuteTraceReplayTool(ctx context.Context, record TraceRecord, env TraceReplayEnvironment) (json.RawMessage, error) {
	input := rawObject(record.Input)
	command := firstNonEmpty(stringValue(input["command"]), stringValue(input["cmd"]), stringValue(input["subject"]))
	if command == "" {
		return traceReplayErrorOutput(record, fmt.Errorf("recorded tool boundary has no command")), nil
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(timeoutCtx, "sh", "-lc", command)
	cwd := firstNonEmpty(stringValue(input["cwd"]), env.WorktreePath)
	if !filepath.IsAbs(cwd) {
		cwd = filepath.Join(env.WorktreePath, cwd)
	}
	cmd.Dir = cwd
	output, err := cmd.CombinedOutput()
	exitCode := 0
	if err != nil {
		exitCode = 1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}
	status := "success"
	if err != nil {
		status = "failed"
	}
	payload := map[string]any{
		"command":   command,
		"exit_code": exitCode,
		"stdout":    string(output),
		"status":    status,
	}
	if err != nil {
		payload["error"] = err.Error()
	}
	raw, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		return nil, marshalErr
	}
	return raw, nil
}

func traceReplayErrorOutput(record TraceRecord, err error) json.RawMessage {
	raw, marshalErr := json.Marshal(map[string]any{
		"status":   "failed",
		"trace_id": record.TraceID,
		"error":    err.Error(),
	})
	if marshalErr != nil {
		return json.RawMessage(`{"status":"failed","error":"trace replay failed"}`)
	}
	return raw
}

func expectedTraceTransitions(record TraceRecord) []TraceStateTransition {
	for _, raw := range []json.RawMessage{record.Output, record.Input} {
		object := rawObject(raw)
		for _, key := range []string{"expected_transitions", "recorded_transitions", "replay_expected_transitions"} {
			if transitions := transitionsFromAny(object[key]); len(transitions) > 0 {
				return transitions
			}
		}
	}
	return nil
}

func actualTraceTransitions(raw json.RawMessage) []TraceStateTransition {
	object := rawObject(raw)
	var transitions []TraceStateTransition
	for _, key := range []string{"actual_transitions", "state_transitions", "transitions"} {
		transitions = append(transitions, transitionsFromAny(object[key])...)
	}
	transitions = append(transitions, derivedTraceTransitions(object)...)
	return normalizeTraceTransitions(transitions)
}

func derivedTraceTransitions(object map[string]any) []TraceStateTransition {
	if len(object) == 0 {
		return nil
	}
	var out []TraceStateTransition
	fromLease := strings.TrimSpace(stringValue(object["from_lease_state"]))
	toLease := strings.TrimSpace(firstNonEmpty(stringValue(object["to_lease_state"]), stringValue(object["lease_state"])))
	if fromLease != "" || toLease != "" {
		out = append(out, TraceStateTransition{
			Type:    "lease_change",
			Subject: firstNonEmpty(stringValue(object["task_id"]), stringValue(object["record_id"]), "run"),
			From:    fromLease,
			To:      toLease,
		})
	}
	if row := traceVerificationRowTransition(object); row.Type != "" {
		out = append(out, row)
	}
	files := traceReplayPathValues(firstNonEmptyAny(object["file_touch_set"], object["files_touched"], object["changed_files"], object["files"], object["paths"]))
	if len(files) > 0 {
		out = append(out, TraceStateTransition{Type: "file_touch_set", Files: files})
	}
	return out
}

func traceVerificationRowTransition(object map[string]any) TraceStateTransition {
	for _, key := range []string{"verify_row", "verification_row", "verification"} {
		if nested, ok := object[key].(map[string]any); ok {
			return TraceStateTransition{
				Type:    "verify_row",
				Subject: firstNonEmpty(stringValue(nested["task_id"]), stringValue(nested["record_id"])),
				Covers:  stringValue(nested["covers"]),
				Check:   stringValue(nested["check"]),
				Result:  stringValue(nested["result"]),
			}
		}
	}
	if stringValue(object["check"]) != "" && stringValue(object["result"]) != "" {
		return TraceStateTransition{
			Type:    "verify_row",
			Subject: firstNonEmpty(stringValue(object["task_id"]), stringValue(object["record_id"])),
			Covers:  stringValue(object["covers"]),
			Check:   stringValue(object["check"]),
			Result:  stringValue(object["result"]),
		}
	}
	return TraceStateTransition{}
}

func transitionsFromAny(value any) []TraceStateTransition {
	switch typed := value.(type) {
	case []TraceStateTransition:
		return normalizeTraceTransitions(typed)
	case []any:
		var out []TraceStateTransition
		for _, item := range typed {
			object, ok := item.(map[string]any)
			if !ok {
				continue
			}
			out = append(out, transitionFromObject(object))
		}
		return normalizeTraceTransitions(out)
	default:
		return nil
	}
}

func transitionFromObject(object map[string]any) TraceStateTransition {
	files := traceReplayPathValues(firstNonEmptyAny(object["files"], object["file_touch_set"], object["paths"]))
	return TraceStateTransition{
		Type:    stringValue(object["type"]),
		Subject: firstNonEmpty(stringValue(object["subject"]), stringValue(object["target"]), stringValue(object["task_id"]), stringValue(object["record_id"])),
		From:    stringValue(object["from"]),
		To:      stringValue(object["to"]),
		Covers:  stringValue(object["covers"]),
		Check:   stringValue(object["check"]),
		Result:  stringValue(object["result"]),
		Files:   files,
	}
}

func normalizeTraceTransitions(rows []TraceStateTransition) []TraceStateTransition {
	out := make([]TraceStateTransition, 0, len(rows))
	for _, row := range rows {
		row.Type = strings.ToLower(strings.TrimSpace(row.Type))
		row.Subject = strings.TrimSpace(row.Subject)
		row.From = strings.TrimSpace(row.From)
		row.To = strings.TrimSpace(row.To)
		row.Covers = strings.TrimSpace(row.Covers)
		row.Check = strings.TrimSpace(row.Check)
		row.Result = strings.ToLower(strings.TrimSpace(row.Result))
		row.Files = dedupeSortedStrings(row.Files)
		if row.Type == "" {
			continue
		}
		out = append(out, row)
	}
	return out
}

func firstTransitionDivergence(expected, actual []TraceStateTransition) string {
	max := len(expected)
	if len(actual) > max {
		max = len(actual)
	}
	for i := 0; i < max; i++ {
		if i >= len(expected) {
			return fmt.Sprintf("unexpected transition #%d: %s", i+1, traceTransitionSummary(actual[i]))
		}
		if i >= len(actual) {
			return fmt.Sprintf("missing transition #%d: %s", i+1, traceTransitionSummary(expected[i]))
		}
		if !reflect.DeepEqual(expected[i], actual[i]) {
			return fmt.Sprintf("transition #%d: expected %s got %s", i+1, traceTransitionSummary(expected[i]), traceTransitionSummary(actual[i]))
		}
	}
	return ""
}

func traceTransitionSummary(row TraceStateTransition) string {
	raw, err := json.Marshal(row)
	if err != nil {
		return row.Type
	}
	return string(raw)
}

func rawObject(raw json.RawMessage) map[string]any {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil
	}
	return object
}

func jsonRawEqual(left, right json.RawMessage) bool {
	var leftValue any
	var rightValue any
	if len(bytes.TrimSpace(left)) == 0 {
		leftValue = nil
	} else if err := json.Unmarshal(left, &leftValue); err != nil {
		leftValue = string(left)
	}
	if len(bytes.TrimSpace(right)) == 0 {
		rightValue = nil
	} else if err := json.Unmarshal(right, &rightValue); err != nil {
		rightValue = string(right)
	}
	return reflect.DeepEqual(leftValue, rightValue)
}

func traceReplayPathValues(value any) []string {
	switch typed := value.(type) {
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		return []string{strings.TrimSpace(typed)}
	case []string:
		return dedupeSortedStrings(typed)
	case []any:
		var out []string
		for _, item := range typed {
			if value := strings.TrimSpace(stringValue(item)); value != "" {
				out = append(out, value)
			}
		}
		return dedupeSortedStrings(out)
	default:
		return nil
	}
}

func traceIDFromReplayCheck(check string) (string, bool) {
	trimmed := strings.TrimSpace(check)
	if !strings.HasPrefix(trimmed, "replay:") {
		return "", false
	}
	traceID := strings.TrimSpace(strings.TrimPrefix(trimmed, "replay:"))
	return traceID, traceID != ""
}

func evaluateV7ReplayVerificationRow(vaultPath string, row v7VerificationRow) v7VerificationRow {
	traceID, ok := traceIDFromReplayCheck(row.Check)
	if !ok {
		return row
	}
	report, err := ReplayTrace(context.Background(), TraceReplayOptions{VaultPath: vaultPath, TraceID: traceID, Mode: traceReplayModeMock})
	if err != nil {
		row.Result = "fail"
		row.Notes = appendTraceReplayNote(row.Notes, "replay failed: "+err.Error())
		return row
	}
	if !report.Passed {
		row.Result = "fail"
		row.Notes = appendTraceReplayNote(row.Notes, "first divergent transition: "+report.FirstDivergence)
	}
	return row
}

func appendTraceReplayNote(notes, addition string) string {
	notes = strings.TrimSpace(notes)
	addition = strings.TrimSpace(addition)
	if notes == "" {
		return addition
	}
	if strings.Contains(notes, addition) {
		return notes
	}
	return notes + "; " + addition
}
