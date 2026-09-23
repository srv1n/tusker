package main

import (
	"encoding/json"
	"os"
	"strings"
)

const runActivityTextLimit = 16 * 1024

// Activity is display-only. Select readable fields, never arbitrary provider
// metadata, prompts, reasoning, credentials, or authority-bearing receipts.
func runActivityText(text string) string {
	text = redactHookOutput(text)
	if len([]rune(text)) > runActivityTextLimit {
		return truncateRunes(text, runActivityTextLimit) + "\n[truncated]"
	}
	return text
}

type acpActivityUpdate struct {
	SessionUpdate string          `json:"sessionUpdate"`
	ToolCallID    string          `json:"toolCallId"`
	Title         string          `json:"title"`
	Status        string          `json:"status"`
	Content       json.RawMessage `json:"content"`
	RawInput      json.RawMessage `json:"rawInput"`
	Plan          json.RawMessage `json:"entries"`
}

func acpActivity(params json.RawMessage) acpActivityUpdate {
	var envelope struct {
		Update acpActivityUpdate `json:"update"`
	}
	_ = json.Unmarshal(params, &envelope)
	return envelope.Update
}

func activityContentText(raw json.RawMessage) string {
	var block struct {
		Type    string          `json:"type"`
		Text    string          `json:"text"`
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(raw, &block) == nil {
		if block.Type == "text" {
			return block.Text
		}
		if block.Type == "content" {
			return activityContentText(block.Content)
		}
		return ""
	}
	var blocks []json.RawMessage
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	var texts []string
	for _, block := range blocks {
		if text := activityContentText(block); text != "" {
			texts = append(texts, text)
		}
	}
	return strings.Join(texts, "\n")
}

// Read complete records backwards, within a bounded byte budget.
func runActivityTail(path string) []map[string]any {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil
	}
	const budget, chunk, records = 8 << 20, 64 << 10, 200
	end := info.Size()
	var lines []string
	var fragment string
	newestPartial := false
	firstChunk := true
	for end > 0 && info.Size()-end < budget && len(lines) < records {
		n := min(int64(chunk), end, int64(budget)-(info.Size()-end))
		start := end - n
		buf := make([]byte, n)
		if _, err := file.ReadAt(buf, start); err != nil {
			break
		}
		parts := strings.Split(string(buf)+fragment, "\n")
		if firstChunk {
			newestPartial = len(buf) > 0 && buf[len(buf)-1] != '\n'
		}
		fragment = parts[0]
		for i := len(parts) - 1; i > 0 && len(lines) < records; i-- {
			if newestPartial {
				newestPartial = false
				continue // the writer has not completed its newest record
			}
			if parts[i] != "" {
				if len(parts[i]) > 1<<20 {
					lines = append(lines, `{"kind":"truncated","payload":{"activity":true,"text":"[oversized raw log record truncated]"}}`)
				} else {
					lines = append(lines, parts[i])
				}
			}
		}
		firstChunk = false
		end = start
	}
	if end == 0 && fragment != "" && len(lines) < records {
		lines = append(lines, fragment)
	} else if end > 0 && len(lines) == 0 {
		return []map[string]any{{"kind": "truncated", "payload": map[string]any{"activity": true, "text": "[oversized raw log record truncated]"}}}
	}
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}
	return parseEventTail(strings.Join(lines, "\n"))
}

func appendRunActivity(events []serveRunEvent, event serveRunEvent) []serveRunEvent {
	if event.ID != "" {
		for i := len(events) - 1; i >= 0; i-- {
			if events[i].ID == event.ID {
				if event.Kind == "tool_call" || event.Kind == "tool_result" {
					previous := events[i]
					event.Text = mergeRunToolText(previous.Text, event.Text)
					event.Kind = "tool_call"
				}
				// A completed tool belongs at its latest observation, even if
				// it started before the visible tail.
				events = append(events[:i], events[i+1:]...)
				break
			}
		}
	}
	return append(events, event)
}

func mergeRunToolText(previous, current string) string {
	if previous == "" || strings.Contains(current, previous) {
		return current
	}
	oldLines, newLines := strings.Split(previous, "\n"), strings.Split(current, "\n")
	if oldLines[0] != newLines[0] {
		return runActivityText(previous + "\n" + current)
	}
	merged := []string{newLines[0]}
	for _, line := range oldLines[1:] {
		if line == "" || line == "pending" || line == "in_progress" || line == "completed" || line == "failed" || line == "cancelled" || strings.Contains(current, line) {
			continue
		}
		merged = append(merged, line)
	}
	merged = append(merged, newLines[1:]...)
	return runActivityText(strings.Join(merged, "\n"))
}

// CLI output is already retained in the attempt's raw log. Project its public
// messages and tool activity at read time, including logs from existing runs.
func cliRunActivity(record map[string]any) []serveRunEvent {
	at := firstNonEmpty(stringValue(record["timestamp"]), stringValue(record["at"]), stringValue(record["ts"]))
	event := func(id, kind, text string) serveRunEvent {
		return serveRunEvent{TS: at, ID: id, Kind: kind, Text: runActivityText(text), Activity: true}
	}
	kind := stringValue(record["type"])
	switch kind {
	case "item.started", "item.updated", "item.completed":
		item, _ := record["item"].(map[string]any)
		id := stringValue(item["id"])
		if id != "" {
			id = "cli:" + id
		}
		switch stringValue(item["type"]) {
		case "error":
			result := event(id, "error", firstNonEmpty(providerErrorText(item), "Provider error"))
			result.Level = "error"
			return []serveRunEvent{result}
		case "agent_message":
			return []serveRunEvent{event(id, "agent_message", stringValue(item["text"]))}
		case "command_execution":
			text := stringValue(item["command"]) + "\n" + stringValue(item["status"])
			if code, exists := item["exit_code"]; exists && code != nil {
				text += "\nexit " + toString(code)
			}
			if output := stringValue(item["aggregated_output"]); output != "" {
				text += "\n" + output
			}
			result := event(id, "tool_call", text)
			if stringValue(item["status"]) == "failed" || intValue(item["exit_code"]) != 0 {
				result.Level = "error"
			}
			return []serveRunEvent{result}
		case "file_change":
			var paths []string
			changes, _ := item["changes"].([]any)
			for _, change := range changes {
				if change, ok := change.(map[string]any); ok {
					paths = append(paths, stringValue(change["kind"])+" "+stringValue(change["path"]))
				}
			}
			return []serveRunEvent{event(id, "file_change", strings.Join(paths, "\n"))}
		case "mcp_tool_call":
			return []serveRunEvent{event(id, "tool_call", stringValue(item["server"])+" / "+stringValue(item["tool"])+"\n"+stringValue(item["status"]))}
		}
	case "assistant", "user":
		message, _ := record["message"].(map[string]any)
		blocks, _ := message["content"].([]any)
		var events []serveRunEvent
		for _, block := range blocks {
			block, _ := block.(map[string]any)
			switch stringValue(block["type"]) {
			case "text":
				if kind == "assistant" {
					events = append(events, event("", "agent_message", stringValue(block["text"])))
				}
			case "tool_use":
				input, _ := block["input"].(map[string]any)
				text := stringValue(block["name"])
				if command := stringValue(input["command"]); command != "" {
					text += "\n" + command
				}
				events = append(events, event(cliToolActivityID(block["id"]), "tool_call", text))
			case "tool_result":
				text, ok := block["content"].(string)
				if !ok {
					raw, _ := json.Marshal(block["content"])
					text = activityContentText(raw)
				}
				result := event(cliToolActivityID(block["tool_use_id"]), "tool_result", text)
				if block["is_error"] == true {
					result.Level = "error"
					if text == "" {
						result.Text = "Tool failed"
					}
				}
				events = append(events, result)
			}
		}
		return events
	case "result":
		if record["is_error"] == true || strings.Contains(strings.ToLower(stringValue(record["subtype"])), "error") {
			result := event("", "error", firstNonEmpty(stringValue(record["result"]), providerErrorText(record), stringValue(record["subtype"]), "Run failed"))
			result.Level = "error"
			return []serveRunEvent{result}
		}
		if text := stringValue(record["result"]); text != "" {
			return []serveRunEvent{event("", "agent_message", text)}
		}
	case "turn.failed", "error":
		result := event("", "error", firstNonEmpty(providerErrorText(record), "Run failed"))
		result.Level = "error"
		return []serveRunEvent{result}
	}
	return nil
}

func providerErrorText(record map[string]any) string {
	if err, ok := record["error"].(map[string]any); ok {
		return firstNonEmpty(stringValue(err["message"]), stringValue(err["type"]))
	}
	return firstNonEmpty(stringValue(record["message"]), stringValue(record["error"]))
}

func cliToolActivityID(value any) string {
	if id := stringValue(value); id != "" {
		return "cli:tool:" + id
	}
	return ""
}
