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
		"tusker new doc",
		"tusker list docs --grep",
		"tusker check --only docs",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("project skill body missing %q", want)
		}
	}
}
