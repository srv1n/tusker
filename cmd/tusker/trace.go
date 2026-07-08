package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const traceRecordSchemaVersion = "tusker.boundary_trace/v1"

type TraceRecord struct {
	SchemaVersion     string          `json:"schema_version"`
	TraceID           string          `json:"trace_id"`
	WorkItemID        string          `json:"work_item_id"`
	NodeID            string          `json:"node_id"`
	NodeType          string          `json:"node_type"`
	Input             json.RawMessage `json:"input"`
	Output            json.RawMessage `json:"output"`
	Error             json.RawMessage `json:"error"`
	ModelProvider     *string         `json:"model_provider"`
	ModelName         *string         `json:"model_name"`
	ModelParams       json.RawMessage `json:"model_params"`
	PromptVersion     *string         `json:"prompt_version"`
	SkillVersions     json.RawMessage `json:"skill_versions"`
	CodeSHA           string          `json:"code_sha"`
	ToolSchemaVersion *string         `json:"tool_schema_version"`
	PermissionScope   *string         `json:"permission_scope"`
	RetrievedChunkIDs []string        `json:"retrieved_chunk_ids"`
	CreatedAt         string          `json:"created_at"`
}

func TraceRecordJSONSchema() map[string]any {
	nullString := []any{"string", "null"}
	nullObject := []any{"object", "null"}
	nullArray := []any{"array", "null"}
	return map[string]any{
		"$schema":              "https://json-schema.org/draft/2020-12/schema",
		"$id":                  traceRecordSchemaVersion,
		"title":                "Tusker boundary trace record",
		"type":                 "object",
		"additionalProperties": false,
		"required": []string{
			"schema_version",
			"trace_id",
			"work_item_id",
			"node_id",
			"node_type",
			"input",
			"output",
			"error",
			"model_provider",
			"model_name",
			"model_params",
			"prompt_version",
			"skill_versions",
			"code_sha",
			"tool_schema_version",
			"permission_scope",
			"retrieved_chunk_ids",
			"created_at",
		},
		"properties": map[string]any{
			"schema_version":      map[string]any{"const": traceRecordSchemaVersion},
			"trace_id":            map[string]any{"type": "string"},
			"work_item_id":        map[string]any{"type": "string"},
			"node_id":             map[string]any{"type": "string"},
			"node_type":           map[string]any{"enum": []string{"model", "tool", "retrieval"}},
			"input":               map[string]any{"type": []any{"object", "array", "string", "number", "boolean", "null"}},
			"output":              map[string]any{"type": []any{"object", "array", "string", "number", "boolean", "null"}},
			"error":               map[string]any{"type": nullObject},
			"model_provider":      map[string]any{"type": nullString},
			"model_name":          map[string]any{"type": nullString},
			"model_params":        map[string]any{"type": nullObject},
			"prompt_version":      map[string]any{"type": nullString},
			"skill_versions":      map[string]any{"type": nullObject},
			"code_sha":            map[string]any{"type": "string"},
			"tool_schema_version": map[string]any{"type": nullString},
			"permission_scope":    map[string]any{"type": nullString},
			"retrieved_chunk_ids": map[string]any{"type": nullArray, "items": map[string]any{"type": "string"}},
			"created_at":          map[string]any{"type": "string", "format": "date-time"},
		},
	}
}

type TraceRecorderOptions struct {
	VaultPath     string
	WorkItemID    string
	AttemptID     string
	EventSinkPath string
	CodeSHA       string
	Now           func() time.Time
}

func RecordAttemptTraces(opts TraceRecorderOptions) (int, error) {
	opts.WorkItemID = strings.TrimSpace(opts.WorkItemID)
	opts.AttemptID = strings.TrimSpace(opts.AttemptID)
	opts.VaultPath = strings.TrimSpace(opts.VaultPath)
	opts.EventSinkPath = strings.TrimSpace(opts.EventSinkPath)
	opts.CodeSHA = firstNonEmpty(strings.TrimSpace(opts.CodeSHA), "unknown")
	if opts.Now == nil {
		opts.Now = func() time.Time { return time.Now().UTC() }
	}
	if opts.VaultPath == "" || opts.WorkItemID == "" || opts.AttemptID == "" || opts.EventSinkPath == "" {
		return 0, nil
	}
	if !fileExists(opts.EventSinkPath) {
		return 0, nil
	}
	events, err := readTraceEvents(opts.EventSinkPath)
	if err != nil {
		return 0, err
	}
	tracePath := traceAttemptPath(opts.VaultPath, opts.WorkItemID, opts.AttemptID)
	existing, err := existingTraceIDs(tracePath)
	if err != nil {
		return 0, err
	}
	var records []TraceRecord
	for _, event := range events {
		if event.AttemptID != "" && event.AttemptID != opts.AttemptID {
			continue
		}
		if event.AttemptID == "" {
			event.AttemptID = opts.AttemptID
		}
		record, ok := traceRecordFromEvent(opts, event)
		if !ok || existing[record.TraceID] {
			continue
		}
		existing[record.TraceID] = true
		records = append(records, record)
	}
	if len(records) == 0 {
		return 0, nil
	}
	if err := appendTraceRecords(tracePath, records); err != nil {
		return 0, err
	}
	return len(records), nil
}

func recordRunBoundaryTraces(project RegisteredProject, run RunStatus) error {
	if strings.TrimSpace(run.EventSinkPath) == "" || strings.TrimSpace(run.ActiveAttemptID) == "" {
		return nil
	}
	_, err := RecordAttemptTraces(TraceRecorderOptions{
		VaultPath:     project.VaultRoot,
		WorkItemID:    firstNonEmpty(run.RecordID, run.ItemID),
		AttemptID:     run.ActiveAttemptID,
		EventSinkPath: run.EventSinkPath,
		CodeSHA:       resolveTraceCodeSHA(project.RepoRoot),
	})
	return err
}

func readTraceEvents(path string) ([]Event, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	var events []Event
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event Event
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, scanner.Err()
}

func traceRecordFromEvent(opts TraceRecorderOptions, event Event) (TraceRecord, bool) {
	nodeType, ok := traceNodeTypeForEvent(event.Kind)
	if !ok {
		return TraceRecord{}, false
	}
	payload := event.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	nodeID := traceNodeID(event, payload)
	createdAt := strings.TrimSpace(event.At)
	if createdAt == "" {
		createdAt = opts.Now().UTC().Format(time.RFC3339)
	}
	record := TraceRecord{
		SchemaVersion:     traceRecordSchemaVersion,
		WorkItemID:        opts.WorkItemID,
		NodeID:            nodeID,
		NodeType:          nodeType,
		ModelProvider:     optionalString(payload, "model_provider", "provider"),
		ModelName:         optionalString(payload, "model_name", "model"),
		ModelParams:       optionalJSON(payload["model_params"]),
		PromptVersion:     optionalString(payload, "prompt_version"),
		SkillVersions:     optionalJSON(payload["skill_versions"]),
		CodeSHA:           opts.CodeSHA,
		ToolSchemaVersion: optionalString(payload, "tool_schema_version"),
		PermissionScope:   tracePermissionScope(payload),
		RetrievedChunkIDs: stringSliceFromAny(payload["retrieved_chunk_ids"]),
		CreatedAt:         createdAt,
	}
	switch nodeType {
	case "model":
		record.Output = optionalMapJSON(payload)
		record.Error = traceErrorFromEvent(event.Kind, payload)
	case "tool":
		record.Input = optionalJSON(traceToolInput(payload))
		if traceEventIsError(event.Kind, payload) {
			record.Error = traceErrorFromEvent(event.Kind, payload)
		} else {
			record.Output = optionalJSON(payload)
		}
	case "retrieval":
		record.Input = optionalJSON(traceRetrievalInput(payload))
		record.Output = optionalMapJSON(payload)
		record.Error = traceErrorFromEvent(event.Kind, payload)
	}
	record.TraceID = traceIDForEvent(opts.WorkItemID, opts.AttemptID, event, nodeType, nodeID)
	return record, true
}

func traceNodeTypeForEvent(kind string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(kind))
	switch {
	case normalized == "turn_completed" || strings.HasPrefix(normalized, "model_"):
		return "model", true
	case normalized == "codex_approval_decision":
		return "tool", true
	case strings.HasPrefix(normalized, "extension_tool_") || strings.HasPrefix(normalized, "tool_"):
		return "tool", true
	case strings.HasPrefix(normalized, "retrieval_"):
		return "retrieval", true
	default:
		return "", false
	}
}

func traceNodeID(event Event, payload map[string]any) string {
	for _, key := range []string{"node_id", "turn_id", "turnId", "tool", "tool_name", "retrieval_id", "chunk_id"} {
		if value := strings.TrimSpace(stringValue(payload[key])); value != "" {
			return value
		}
	}
	if event.Seq > 0 {
		return fmt.Sprintf("%s:%d", strings.TrimSpace(event.Kind), event.Seq)
	}
	return strings.TrimSpace(event.Kind)
}

func traceIDForEvent(workItemID, attemptID string, event Event, nodeType, nodeID string) string {
	parts := []string{
		traceRecordSchemaVersion,
		strings.TrimSpace(workItemID),
		strings.TrimSpace(attemptID),
		fmt.Sprintf("%d", event.Seq),
		strings.TrimSpace(event.At),
		strings.TrimSpace(event.Kind),
		nodeType,
		nodeID,
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "trc_" + hex.EncodeToString(sum[:16])
}

func traceEventIsError(kind string, payload map[string]any) bool {
	normalized := strings.ToLower(strings.TrimSpace(kind))
	if strings.Contains(normalized, "denied") || strings.Contains(normalized, "failed") || strings.Contains(normalized, "error") {
		return true
	}
	status := strings.ToLower(strings.TrimSpace(stringValue(payload["status"])))
	if status != "" && status != "completed" && status != "ok" && status != "success" {
		return true
	}
	return strings.TrimSpace(firstNonEmpty(stringValue(payload["last_error"]), stringValue(payload["error"]), stringValue(payload["reason"]))) != ""
}

func traceErrorFromEvent(kind string, payload map[string]any) json.RawMessage {
	if !traceEventIsError(kind, payload) {
		return nil
	}
	message := firstNonEmpty(
		stringValue(payload["last_error"]),
		stringValue(payload["error"]),
		stringValue(payload["reason"]),
		stringValue(payload["message"]),
	)
	value := map[string]any{}
	if message != "" {
		value["message"] = message
	}
	if status := strings.TrimSpace(stringValue(payload["status"])); status != "" {
		value["status"] = status
	}
	if len(value) == 0 {
		value["message"] = firstNonEmpty(strings.TrimSpace(kind), "boundary event failed")
	}
	return optionalJSON(value)
}

func traceToolInput(payload map[string]any) map[string]any {
	out := map[string]any{}
	for _, key := range []string{"method", "tool", "tool_name", "request_type", "subject", "command", "cmd", "path", "cwd", "arguments", "input", "request"} {
		if value, ok := payload[key]; ok {
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func traceRetrievalInput(payload map[string]any) map[string]any {
	out := map[string]any{}
	for _, key := range []string{"query", "retrieval_id", "input"} {
		if value, ok := payload[key]; ok {
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func optionalString(payload map[string]any, keys ...string) *string {
	for _, key := range keys {
		if value := strings.TrimSpace(stringValue(payload[key])); value != "" {
			return &value
		}
	}
	return nil
}

func tracePermissionScope(payload map[string]any) *string {
	if value := optionalString(payload, "permission_scope"); value != nil {
		return value
	}
	var parts []string
	for _, key := range []string{"approval_policy", "thread_sandbox", "turn_sandbox_policy"} {
		if value := strings.TrimSpace(stringValue(payload[key])); value != "" {
			parts = append(parts, key+"="+value)
		}
	}
	if len(parts) == 0 {
		return nil
	}
	joined := strings.Join(parts, ";")
	return &joined
}

func optionalJSON(value any) json.RawMessage {
	if value == nil {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return raw
}

func optionalMapJSON(value map[string]any) json.RawMessage {
	if len(value) == 0 {
		return nil
	}
	return optionalJSON(value)
}

func stringSliceFromAny(value any) []string {
	switch typed := value.(type) {
	case []string:
		return append([]string{}, typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if value := strings.TrimSpace(stringValue(item)); value != "" {
				out = append(out, value)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

func existingTraceIDs(path string) (map[string]bool, error) {
	out := map[string]bool{}
	if !fileExists(path) {
		return out, nil
	}
	records, err := readTraceRecords(path)
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		if strings.TrimSpace(record.TraceID) != "" {
			out[record.TraceID] = true
		}
	}
	return out, nil
}

func appendTraceRecords(path string, records []TraceRecord) error {
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	for _, record := range records {
		raw, err := json.Marshal(record)
		if err != nil {
			return err
		}
		if _, err := file.Write(append(raw, '\n')); err != nil {
			return err
		}
	}
	return nil
}

func readTraceRecords(path string) ([]TraceRecord, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	var records []TraceRecord
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var record TraceRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, scanner.Err()
}

func traceAttemptPath(vaultPath, workItemID, attemptID string) string {
	return filepath.Join(vaultPath, "_generated", "traces", filepath.FromSlash(workItemID), attemptID+".jsonl")
}

func traceRootPath(vaultPath string) string {
	return filepath.Join(vaultPath, "_generated", "traces")
}

func resolveTraceCodeSHA(repoRoot string) string {
	repoRoot = strings.TrimSpace(repoRoot)
	if repoRoot == "" {
		return "unknown"
	}
	out, err := exec.Command("git", "-C", repoRoot, "rev-parse", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	if sha := strings.TrimSpace(string(out)); sha != "" {
		return sha
	}
	return "unknown"
}

type TraceAttemptSummary struct {
	AttemptID     string   `json:"attempt_id"`
	RecordCount   int      `json:"record_count"`
	BoundaryTypes []string `json:"boundary_types"`
	Path          string   `json:"path"`
}

func traceV7Cmd(args Args) error {
	switch strings.ToLower(strings.TrimSpace(args.String("_pos0"))) {
	case "list":
		shiftPositionalArgs(args)
		return traceListCmd(args)
	case "show":
		shiftPositionalArgs(args)
		return traceShowCmd(args)
	case "replay":
		shiftPositionalArgs(args)
		return traceReplayCmd(args)
	default:
		return tuskerError(errorMissingArg, "trace requires a subcommand", withHint("use `tusker trace list <TASK-ID>`, `tusker trace show <trace-id>`, or `tusker trace replay <trace-id>`"))
	}
}

func traceListCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	taskID := firstNonEmpty(args.String("id"), args.String("_pos0"))
	if strings.TrimSpace(taskID) == "" {
		return tuskerError(errorMissingArg, "trace list requires <TASK-ID>")
	}
	summaries, err := listTraceAttempts(vaultPath, taskID)
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "task": taskID, "attempts": summaries})
		return nil
	}
	if len(summaries) == 0 {
		fmt.Printf("%s traces: no attempts\n", taskID)
		return nil
	}
	for _, summary := range summaries {
		types := "-"
		if len(summary.BoundaryTypes) > 0 {
			types = strings.Join(summary.BoundaryTypes, ",")
		}
		fmt.Printf("%s count=%d types=%s\n", summary.AttemptID, summary.RecordCount, types)
	}
	return nil
}

func traceShowCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	traceID := firstNonEmpty(args.String("id"), args.String("_pos0"))
	if strings.TrimSpace(traceID) == "" {
		return tuskerError(errorMissingArg, "trace show requires <trace-id>")
	}
	record, path, err := findTraceRecord(vaultPath, traceID)
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "path": path, "record": record})
		return nil
	}
	raw, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	fmt.Printf("%s\n", raw)
	return nil
}

func listTraceAttempts(vaultPath, taskID string) ([]TraceAttemptSummary, error) {
	dir := filepath.Join(traceRootPath(vaultPath), filepath.FromSlash(taskID))
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []TraceAttemptSummary{}, nil
	}
	if err != nil {
		return nil, err
	}
	var out []TraceAttemptSummary
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".jsonl" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		records, err := readTraceRecords(path)
		if err != nil {
			return nil, err
		}
		typeSet := map[string]bool{}
		for _, record := range records {
			if record.NodeType != "" {
				typeSet[record.NodeType] = true
			}
		}
		out = append(out, TraceAttemptSummary{
			AttemptID:     strings.TrimSuffix(entry.Name(), ".jsonl"),
			RecordCount:   len(records),
			BoundaryTypes: sortedTraceTypes(typeSet),
			Path:          path,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AttemptID < out[j].AttemptID })
	return out, nil
}

func findTraceRecord(vaultPath, traceID string) (TraceRecord, string, error) {
	root := traceRootPath(vaultPath)
	if !dirExists(root) {
		return TraceRecord{}, "", tuskerError(errorNotFound, "trace not found: "+traceID)
	}
	var found TraceRecord
	foundPath := ""
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".jsonl" || foundPath != "" {
			return nil
		}
		records, err := readTraceRecords(path)
		if err != nil {
			return err
		}
		for _, record := range records {
			if record.TraceID == traceID {
				found = record
				foundPath = path
				return nil
			}
		}
		return nil
	})
	if err != nil {
		return TraceRecord{}, "", err
	}
	if foundPath == "" {
		return TraceRecord{}, "", tuskerError(errorNotFound, "trace not found: "+traceID)
	}
	return found, foundPath, nil
}

func sortedTraceTypes(typeSet map[string]bool) []string {
	if len(typeSet) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(typeSet))
	for value := range typeSet {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func shiftPositionalArgs(args Args) {
	for i := 0; ; i++ {
		nextKey := fmt.Sprintf("_pos%d", i+1)
		currentKey := fmt.Sprintf("_pos%d", i)
		next, ok := args[nextKey]
		if !ok {
			delete(args, currentKey)
			break
		}
		args[currentKey] = next
	}
	var positionals []string
	for i := 0; ; i++ {
		value, ok := args[fmt.Sprintf("_pos%d", i)]
		if !ok {
			break
		}
		positionals = append(positionals, value)
	}
	if len(positionals) == 0 {
		delete(args, "_pos")
		return
	}
	args["_pos"] = strings.Join(positionals, "\n")
}
