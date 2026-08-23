package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type contextAudit struct {
	File                         string                            `json:"file"`
	Lines                        int                               `json:"lines"`
	Bytes                        int64                             `json:"bytes"`
	SessionID                    string                            `json:"session_id,omitempty"`
	CWD                          string                            `json:"cwd,omitempty"`
	ByTypeBytes                  map[string]int                    `json:"by_type_bytes"`
	FunctionCallOutputBytes      int                               `json:"function_call_output_bytes"`
	FunctionCallOutputTokenCount int                               `json:"function_call_output_original_tokens"`
	CategoryTotals               []contextAuditCategoryTotal       `json:"category_totals"`
	TopOutputs                   []contextAuditOutput              `json:"top_outputs"`
	LastTotalUsage               map[string]any                    `json:"last_total_usage,omitempty"`
	Recommendations              []string                          `json:"recommendations"`
	SessionMetaBytes             int                               `json:"session_meta_bytes"`
	Messages                     []contextAuditMessageContribution `json:"large_messages,omitempty"`
}

type contextAuditCategoryTotal struct {
	Category       string `json:"category"`
	Count          int    `json:"count"`
	Bytes          int    `json:"bytes"`
	OriginalTokens int    `json:"original_tokens"`
}

type contextAuditOutput struct {
	Command        string `json:"command"`
	Category       string `json:"category"`
	Bytes          int    `json:"bytes"`
	OriginalTokens int    `json:"original_tokens"`
	Preview        string `json:"preview"`
}

type contextAuditMessageContribution struct {
	Role    string `json:"role"`
	Bytes   int    `json:"bytes"`
	Preview string `json:"preview"`
}

type transcriptCall struct {
	Name string
	Args string
	Cmd  string
}

type transcriptOutput struct {
	Command        string
	Category       string
	Bytes          int
	OriginalTokens int
	Preview        string
}

func contextAuditCmd(args Args) error {
	file := strings.TrimSpace(args.String("file"))
	if file == "" {
		return tuskerError(errorMissingArg, "context audit requires --file", withHint("use the Codex session JSONL path, for example ~/.codex/sessions/YYYY/MM/DD/*.jsonl"))
	}
	if strings.HasPrefix(file, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		file = filepath.Join(home, strings.TrimPrefix(file, "~/"))
	}
	audit, err := auditContextJSONL(file, atoiDefault(args.String("top"), 12))
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(audit)
		return nil
	}
	printContextAudit(audit)
	return nil
}

func auditContextJSONL(file string, topN int) (contextAudit, error) {
	if topN <= 0 {
		topN = 12
	}
	f, err := os.Open(file)
	if err != nil {
		return contextAudit{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return contextAudit{}, err
	}
	audit := contextAudit{
		File:        file,
		Bytes:       info.Size(),
		ByTypeBytes: map[string]int{},
	}
	calls := map[string]transcriptCall{}
	sessionCommands := map[string]string{}
	var outputs []transcriptOutput
	categoryTotals := map[string]*contextAuditCategoryTotal{}
	var messages []contextAuditMessageContribution

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		audit.Lines++
		var item struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(line, &item); err != nil {
			continue
		}
		audit.ByTypeBytes[item.Type] += len(line)
		switch item.Type {
		case "session_meta":
			audit.SessionMetaBytes += len(line)
			var meta struct {
				ID  string `json:"id"`
				CWD string `json:"cwd"`
			}
			_ = json.Unmarshal(item.Payload, &meta)
			audit.SessionID = meta.ID
			audit.CWD = meta.CWD
		case "event_msg":
			var event struct {
				Type string `json:"type"`
				Info struct {
					TotalUsage map[string]any `json:"total_token_usage"`
				} `json:"info"`
			}
			if json.Unmarshal(item.Payload, &event) == nil && event.Type == "token_count" {
				audit.LastTotalUsage = event.Info.TotalUsage
			}
		case "response_item":
			var response struct {
				Type      string `json:"type"`
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
				CallID    string `json:"call_id"`
				Output    string `json:"output"`
				Role      string `json:"role"`
				Content   []struct {
					Text string `json:"text"`
				} `json:"content"`
			}
			if json.Unmarshal(item.Payload, &response) != nil {
				continue
			}
			switch response.Type {
			case "function_call":
				cmd := commandFromTranscriptCall(response.Name, response.Arguments)
				calls[response.CallID] = transcriptCall{Name: response.Name, Args: response.Arguments, Cmd: cmd}
			case "function_call_output":
				call := calls[response.CallID]
				cmd := resolvedTranscriptCommand(call, response.Output, sessionCommands)
				if sessionID := transcriptSessionID(response.Output); sessionID != "" && call.Cmd != "" {
					sessionCommands[sessionID] = call.Cmd
				}
				output := transcriptOutput{
					Category:       contextOutputCategory(cmd),
					Command:        compactPreview(cmd, 240),
					Bytes:          len(response.Output),
					OriginalTokens: transcriptOriginalTokenCount(response.Output),
					Preview:        compactPreview(response.Output, 160),
				}
				outputs = append(outputs, output)
				audit.FunctionCallOutputBytes += output.Bytes
				audit.FunctionCallOutputTokenCount += output.OriginalTokens
				total := categoryTotals[output.Category]
				if total == nil {
					total = &contextAuditCategoryTotal{Category: output.Category}
					categoryTotals[output.Category] = total
				}
				total.Count++
				total.Bytes += output.Bytes
				total.OriginalTokens += output.OriginalTokens
			case "message":
				text := responseContentText(response.Content)
				messages = append(messages, contextAuditMessageContribution{
					Role:    response.Role,
					Bytes:   len(text),
					Preview: compactPreview(text, 120),
				})
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return contextAudit{}, err
	}
	sort.Slice(outputs, func(i, j int) bool { return outputs[i].Bytes > outputs[j].Bytes })
	for i := 0; i < len(outputs) && i < topN; i++ {
		audit.TopOutputs = append(audit.TopOutputs, contextAuditOutput(outputs[i]))
	}
	for _, total := range categoryTotals {
		audit.CategoryTotals = append(audit.CategoryTotals, *total)
	}
	sort.Slice(audit.CategoryTotals, func(i, j int) bool { return audit.CategoryTotals[i].Bytes > audit.CategoryTotals[j].Bytes })
	sort.Slice(messages, func(i, j int) bool { return messages[i].Bytes > messages[j].Bytes })
	if len(messages) > topN {
		messages = messages[:topN]
	}
	audit.Messages = messages
	audit.Recommendations = contextAuditRecommendations(audit)
	return audit, nil
}

func printContextAudit(audit contextAudit) {
	fmt.Printf("Transcript: %s\n", audit.File)
	fmt.Printf("Session: %s  cwd=%s\n", audit.SessionID, audit.CWD)
	fmt.Printf("Size: %d lines, %d bytes\n", audit.Lines, audit.Bytes)
	if audit.LastTotalUsage != nil {
		fmt.Printf("Last total token usage: input=%s cached=%s output=%s total=%s\n",
			formatTokenUsage(audit.LastTotalUsage["input_tokens"]),
			formatTokenUsage(audit.LastTotalUsage["cached_input_tokens"]),
			formatTokenUsage(audit.LastTotalUsage["output_tokens"]),
			formatTokenUsage(audit.LastTotalUsage["total_tokens"]),
		)
	}
	fmt.Println("\nTop output categories:")
	for _, total := range audit.CategoryTotals {
		fmt.Printf("- %-22s %5d outputs  %8d bytes  %7d original-tokens\n", total.Category, total.Count, total.Bytes, total.OriginalTokens)
	}
	fmt.Println("\nLargest tool outputs:")
	for _, output := range audit.TopOutputs {
		fmt.Printf("- %6d tokens  %7d bytes  [%s] %s\n", output.OriginalTokens, output.Bytes, output.Category, output.Command)
	}
	fmt.Println("\nRecommendations:")
	for _, rec := range audit.Recommendations {
		fmt.Printf("- %s\n", rec)
	}
}

func commandFromTranscriptCall(name, args string) string {
	var parsed map[string]any
	if json.Unmarshal([]byte(args), &parsed) == nil {
		if cmd, ok := parsed["cmd"].(string); ok {
			return cmd
		}
		if sessionID, ok := parsed["session_id"]; ok {
			return fmt.Sprintf("%s session_id=%v", name, sessionID)
		}
	}
	return firstNonEmpty(args, name)
}

func resolvedTranscriptCommand(call transcriptCall, output string, sessionCommands map[string]string) string {
	if call.Name == "write_stdin" {
		if sessionID := writeStdinSessionID(call.Args); sessionID != "" {
			if cmd := sessionCommands[sessionID]; cmd != "" {
				return "continued: " + cmd
			}
		}
	}
	return call.Cmd
}

func writeStdinSessionID(args string) string {
	var parsed struct {
		SessionID any `json:"session_id"`
	}
	if json.Unmarshal([]byte(args), &parsed) != nil || parsed.SessionID == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(parsed.SessionID))
}

func transcriptSessionID(output string) string {
	match := regexp.MustCompile(`Process running with session ID (\d+)`).FindStringSubmatch(output)
	if match == nil {
		return ""
	}
	return match[1]
}

func transcriptOriginalTokenCount(output string) int {
	match := regexp.MustCompile(`Original token count: (\d+)`).FindStringSubmatch(output)
	if match == nil {
		return 0
	}
	return atoiSafe(match[1])
}

func responseContentText(content []struct {
	Text string `json:"text"`
}) string {
	var parts []string
	for _, part := range content {
		parts = append(parts, part.Text)
	}
	return strings.Join(parts, "\n")
}

func contextOutputCategory(cmd string) string {
	switch {
	case strings.Contains(cmd, ".codex/sessions") && (strings.Contains(cmd, "python") || strings.Contains(cmd, "node -") || strings.Contains(cmd, "node <<")):
		return "ad hoc jsonl script"
	case strings.Contains(cmd, ".codex/sessions") && (strings.Contains(cmd, "head ") || strings.Contains(cmd, "tail ") || strings.Contains(cmd, "sed -n") || strings.Contains(cmd, "rg ")):
		return "raw jsonl read"
	case strings.Contains(cmd, "rg -n"):
		return "broad rg output"
	case strings.Contains(cmd, "git status --short"):
		return "git status"
	case strings.Contains(cmd, "git diff --stat"):
		return "git diff stat"
	case strings.Contains(cmd, "git diff --"):
		return "git diff inspect"
	case strings.Contains(cmd, "docs build"):
		return "docs build output"
	case strings.Contains(cmd, "sed -n") || strings.Contains(cmd, "head ") || strings.Contains(cmd, "tail ") || strings.Contains(cmd, "find "):
		return "file reads/listing"
	case strings.Contains(cmd, "go test"):
		return "go test"
	case strings.Contains(cmd, "tusker"):
		return "tusker cli output"
	default:
		return "other"
	}
}

func contextAuditRecommendations(audit contextAudit) []string {
	var recs []string
	for _, total := range audit.CategoryTotals {
		switch total.Category {
		case "broad rg output":
			if total.OriginalTokens > 0 {
				recs = append(recs, "Run `rg -l`, `rg --count`, or add `-m`/narrow globs before `rg -n`; broad match dumps were the biggest avoidable output.")
			}
		case "raw jsonl read":
			recs = append(recs, "Do not `head`/`tail` raw Codex JSONL. Use `tusker context audit --file <jsonl>` so session metadata and tool output are summarized.")
		case "ad hoc jsonl script":
			recs = append(recs, "Replace one-off Python/Node JSONL transcript probes with `tusker context audit`; it gives the same signal with bounded output.")
		case "git status":
			if total.Count > 5 {
				recs = append(recs, "Avoid repeated full `git status --short` in generated-heavy repos; use counts or a capped preview unless staging/commit state matters.")
			}
		case "large json output":
			recs = append(recs, "For bulk JSON commands, default to changed rows plus summary and require `--verbose` for unchanged entries.")
		case "docs build output":
			recs = append(recs, "Capture full docs build logs to an artifact and print only pass/fail plus the final error tail.")
		}
	}
	if audit.SessionMetaBytes > 200000 {
		recs = append(recs, "Base/session instructions are a fixed large cost in this thread; repo-local AGENTS and skill files should stay short and route to references.")
	}
	if len(recs) == 0 {
		recs = append(recs, "No single noisy source dominates; keep using bounded list/search/show flows and capped command output.")
	}
	return recs
}

func compactPreview(value string, limit int) string {
	preview := strings.Join(strings.Fields(value), " ")
	if len(preview) <= limit {
		return preview
	}
	return preview[:limit] + "..."
}

func formatTokenUsage(value any) string {
	switch v := value.(type) {
	case float64:
		return fmt.Sprintf("%.0f", v)
	case float32:
		return fmt.Sprintf("%.0f", v)
	case int:
		return fmt.Sprintf("%d", v)
	case int64:
		return fmt.Sprintf("%d", v)
	case json.Number:
		return v.String()
	case nil:
		return "0"
	default:
		return fmt.Sprint(v)
	}
}

func atoiDefault(value string, fallback int) int {
	parsed := atoiSafe(value)
	if parsed == 0 && strings.TrimSpace(value) == "" {
		return fallback
	}
	return parsed
}
