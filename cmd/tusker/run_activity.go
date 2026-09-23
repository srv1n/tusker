package main

import (
	"encoding/json"
	"io"
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

// Read from the end, dropping partial records at either edge. The byte ceiling
// also bounds work while a runner is producing a large log.
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
	const maxBytes = 1024 * 1024
	start := max(int64(0), info.Size()-maxBytes)
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(file, info.Size()-start))
	if err != nil {
		return nil
	}
	text := string(raw)
	if start > 0 {
		if i := strings.IndexByte(text, '\n'); i >= 0 {
			text = text[i+1:]
		} else {
			return nil
		}
	}
	if i := strings.LastIndexByte(text, '\n'); i >= 0 {
		text = text[:i+1]
	} else {
		return nil
	}
	return parseEventTail(text)
}

func appendRunActivity(events []serveRunEvent, event serveRunEvent) []serveRunEvent {
	if event.ID != "" {
		for i := len(events) - 1; i >= 0; i-- {
			if events[i].ID == event.ID {
				// A completed tool belongs at its latest observation, even if
				// it started before the visible tail.
				events = append(events[:i], events[i+1:]...)
				break
			}
		}
	}
	return append(events, event)
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
				}
				events = append(events, result)
			}
		}
		return events
	case "result":
		if text := stringValue(record["result"]); text != "" {
			return []serveRunEvent{event("", "agent_message", text)}
		}
	}
	return nil
}

func cliToolActivityID(value any) string {
	if id := stringValue(value); id != "" {
		return "cli:tool:" + id
	}
	return ""
}
