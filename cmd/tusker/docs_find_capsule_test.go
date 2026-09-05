package main

import (
	"encoding/json"
	"strings"
	"testing"

	"tusker/internal/docgraph"
)

func TestDocsFindOutputIncludesDiscoveryGuidance(t *testing.T) {
	result := docgraph.FindResult{
		Query: "latency",
		Matches: []docgraph.Match{{
			Path:        "docs/system/storage.md",
			Subject:     "storage",
			Description: "Storage",
			ReadWhen:    "latency cost tradeoff",
			SkipWhen:    "only when changing the login UI",
		}},
	}

	readable := renderDocsFind(result)
	for _, want := range []string{
		"read when: latency cost tradeoff",
		"skip when: only when changing the login UI",
	} {
		if !strings.Contains(readable, want) {
			t.Fatalf("readable output missing %q: %q", want, readable)
		}
	}

	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var decoded struct {
		Matches []map[string]any `json:"Matches"`
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(decoded.Matches) != 1 {
		t.Fatalf("decoded matches = %#v", decoded.Matches)
	}
	if got := decoded.Matches[0]["read_when"]; got != "latency cost tradeoff" {
		t.Fatalf("JSON read_when = %#v", got)
	}
	if got := decoded.Matches[0]["skip_when"]; got != "only when changing the login UI" {
		t.Fatalf("JSON skip_when = %#v", got)
	}
}
