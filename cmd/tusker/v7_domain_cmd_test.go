package main

import (
	"strings"
	"testing"
)

func TestV7ProjectSkillBodyNamesDocsSystemCanon(t *testing.T) {
	body := renderV7ProjectSkillBody(nil)
	if strings.Contains(body, "not domain canon") {
		t.Fatal("project skill still says root docs/ is not domain canon")
	}
	for _, want := range []string{
		"`docs/system/` is the project canon",
		"tusker docs new",
		"tusker docs read",
		"tusker docs check",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("project skill body missing %q", want)
		}
	}
}
