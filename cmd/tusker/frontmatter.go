package main

import (
	"bytes"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type orderedEntry struct {
	Key   string
	Value any
}

type orderedMap []orderedEntry

func parseFrontmatter(text string) (map[string]any, string, error) {
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	if !strings.HasPrefix(normalized, "---\n") {
		return map[string]any{}, normalized, nil
	}

	endMarker := "\n---\n"
	endIdx := strings.Index(normalized[4:], endMarker)
	if endIdx == -1 {
		return map[string]any{}, normalized, nil
	}
	endIdx += 4

	raw := normalized[4:endIdx]
	body := normalized[endIdx+len(endMarker):]
	if strings.TrimSpace(raw) == "" {
		return map[string]any{}, body, nil
	}

	var data map[string]any
	if err := yaml.Unmarshal([]byte(raw), &data); err != nil {
		return nil, "", err
	}
	if data == nil {
		data = map[string]any{}
	}
	return data, body, nil
}

func stringifyFrontmatter(data map[string]any, order []string) (string, error) {
	node := &yaml.Node{Kind: yaml.MappingNode}
	seen := map[string]struct{}{}
	for _, key := range order {
		value, ok := data[key]
		if !ok {
			continue
		}
		node.Content = append(node.Content, scalarNode(key), valueNode(value))
		seen[key] = struct{}{}
	}

	var rest []string
	for key := range data {
		if _, ok := seen[key]; ok {
			continue
		}
		rest = append(rest, key)
	}
	sort.Strings(rest)
	for _, key := range rest {
		node.Content = append(node.Content, scalarNode(key), valueNode(data[key]))
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(node); err != nil {
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	return "---\n" + strings.TrimSpace(buf.String()) + "\n---\n", nil
}

func serializeDocument(data map[string]any, body string, order []string) (string, error) {
	sanitizeCanonicalNoteData(data)
	normalizeOrderedFrontmatter(data)
	pruneEmptyOptionalFrontmatter(data)
	fm, err := stringifyFrontmatter(data, order)
	if err != nil {
		return "", err
	}
	trimmed := strings.TrimRight(strings.TrimLeft(body, "\n"), "\n\t ")
	return fm + "\n" + trimmed + "\n", nil
}

func normalizeOrderedFrontmatter(data map[string]any) {
	transitions, ok := data["transitions"]
	if !ok {
		return
	}
	switch current := transitions.(type) {
	case []any:
		normalized := make([]any, 0, len(current))
		for _, item := range current {
			normalized = append(normalized, normalizeOrderedTransition(item))
		}
		data["transitions"] = normalized
	case []orderedMap:
		return
	}
}

func normalizeOrderedTransition(value any) orderedMap {
	switch current := value.(type) {
	case orderedMap:
		return current
	case map[string]any:
		return orderedMap{
			{Key: "at", Value: current["at"]},
			{Key: "kind", Value: current["kind"]},
			{Key: "from", Value: current["from"]},
			{Key: "to", Value: current["to"]},
			{Key: "actor", Value: current["actor"]},
			{Key: "reason", Value: current["reason"]},
		}
	default:
		return orderedMap{}
	}
}

// Pruning stays keyed on legacy `type` only: V7 records compute state_rev over
// their full frontmatter at ~40 write sites before serialization, so pruning
// V7 kinds here would desync the stored rev from the written file (CAS storm).
func pruneEmptyOptionalFrontmatter(data map[string]any) []string {
	noteType := stringField(data, "type")
	if !managedNoteType(noteType) {
		return nil
	}
	var removed []string
	for _, key := range emptyOptionalFrontmatterFields(noteType) {
		value, ok := data[key]
		if !ok {
			continue
		}
		if isEmptyFrontmatterValue(value) {
			delete(data, key)
			removed = append(removed, key)
		}
	}
	return removed
}

func emptyOptionalFrontmatterFields(noteType string) []string {
	switch noteType {
	case "epic":
		return []string{"owner", "doc_nodes", "started", "blocked_since", "completed", "cancelled_at", "transitions", "tags"}
	case "task":
		return []string{
			"delegation", "ai_tools", "assignee", "domains", "doc_nodes", "blocked_by", "block_reason", "blocks",
			"started", "review_requested_at", "completed", "cancelled_at", "blocked_since",
			"verified_by", "verified_at", "verification_summary", "closed_by", "closed_at", "close_summary",
			"docs_resolution", "transitions", "tags",
		}
	case "doc":
		return []string{
			"epic", "doc_intent", "canon_for", "domains", "source_of_truth", "stale_when_paths",
			"last_verified_at", "owner_epic", "verified_at", "deprecated", "superseded_by",
			"publish_order", "publish_section_title", "redirect_from", "publish_url", "published_at", "tags",
		}
	default:
		return nil
	}
}

func isEmptyFrontmatterValue(value any) bool {
	switch current := value.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(current) == ""
	case []string:
		return len(filterStrings(current)) == 0
	case []any:
		return len(normalizeList(current)) == 0
	case []orderedMap:
		return len(current) == 0
	case map[string]any:
		return len(current) == 0
	default:
		return false
	}
}

func scalarNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

func valueNode(value any) *yaml.Node {
	switch v := value.(type) {
	case nil:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "", Style: yaml.DoubleQuotedStyle}
	case string:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v, Style: yaml.DoubleQuotedStyle}
	case bool:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(v)}
	case int:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(v)}
	case int64:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatInt(v, 10)}
	case float64:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: strconv.FormatFloat(v, 'f', -1, 64)}
	case []string:
		node := &yaml.Node{Kind: yaml.SequenceNode}
		for _, item := range v {
			node.Content = append(node.Content, valueNode(item))
		}
		return node
	case []any:
		node := &yaml.Node{Kind: yaml.SequenceNode}
		for _, item := range v {
			node.Content = append(node.Content, valueNode(item))
		}
		return node
	case orderedMap:
		node := &yaml.Node{Kind: yaml.MappingNode}
		for _, item := range v {
			node.Content = append(node.Content, scalarNode(item.Key), valueNode(item.Value))
		}
		return node
	case map[string]any:
		node := &yaml.Node{Kind: yaml.MappingNode}
		var keys []string
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			node.Content = append(node.Content, scalarNode(key), valueNode(v[key]))
		}
		return node
	default:
		rv := reflect.ValueOf(value)
		if !rv.IsValid() {
			return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: ""}
		}
		if rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array {
			node := &yaml.Node{Kind: yaml.SequenceNode}
			for i := 0; i < rv.Len(); i++ {
				node.Content = append(node.Content, valueNode(rv.Index(i).Interface()))
			}
			return node
		}
		if rv.Kind() == reflect.Map {
			node := &yaml.Node{Kind: yaml.MappingNode}
			var keys []string
			iter := rv.MapRange()
			for iter.Next() {
				keys = append(keys, toString(iter.Key().Interface()))
			}
			sort.Strings(keys)
			for _, key := range keys {
				node.Content = append(node.Content, scalarNode(key), valueNode(rv.MapIndex(reflect.ValueOf(key)).Interface()))
			}
			return node
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: toString(value), Style: yaml.DoubleQuotedStyle}
	}
}
