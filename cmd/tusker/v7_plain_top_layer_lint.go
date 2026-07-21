package main

import (
	"fmt"
	"regexp"
	"strings"
)

// The top layer of a task is a promise to a non-engineer reader: plain
// sentences, no file paths, symbols, or commands. Those belong in the builder
// appendix under "## Implementation notes". This lint flags when the top layer
// slips into code words, so the promise is kept before the task moves forward.
//
// It looks only at the text above the appendix heading; the appendix, the
// acceptance/verification table rows, and fenced code blocks are exempt.

const v7TopLayerAppendixHeading = "## Implementation notes"

var (
	// A dotted filename like foo.go or app.tsx.
	v7CodeFilePattern = regexp.MustCompile(`(?i)\b[\w-]+\.(?:go|ts|tsx|js|jsx|mjs|cjs|py|rs|rb|md|ya?ml|json|toml|swift|java|kt|kts|c|h|cc|cpp|hpp|m|mm|sh|sql|proto|css|scss|html|tf)\b`)
	// A slash path token like cmd/tusker/foo or internal/serve/ui.
	v7CodePathPattern = regexp.MustCompile(`[\w.-]+(?:/[\w.-]+)+`)
	// A backticked span; whatever is inside is code, by convention.
	v7CodeBacktickPattern = regexp.MustCompile("`[^`]+`")
)

// v7CamelCaseAllowlist holds common product and proper nouns that read as plain
// words to a non-engineer even though they carry an internal capital. Keeping
// them off the flagged list avoids noisy warnings on ordinary prose.
var v7CamelCaseAllowlist = map[string]bool{
	"github": true, "gitlab": true, "javascript": true, "typescript": true,
	"postgresql": true, "mysql": true, "graphql": true, "nosql": true,
	"ios": true, "macos": true, "iphone": true, "ipad": true, "ipados": true,
	"oauth": true, "openai": true, "chatgpt": true, "youtube": true,
	"websocket": true, "webrtc": true, "webhook": true, "webhooks": true,
	"dynamodb": true, "mongodb": true, "sqlite": true,
}

// lintV7PlainTopLayer warns when a task's plain top layer slips into code words,
// and blocks a demanding task (p0/p1 or medium+ risk) from going ready while it
// still reads like code. It is the single wiring point called from
// validateV7Task; all detection lives here.
func lintV7PlainTopLayer(note Note, where string, errors, warnings *[]Issue) {
	offenders := v7TopLayerCodeWords(note.Body)
	if len(offenders) == 0 {
		return
	}
	shown := offenders
	if len(shown) > 5 {
		shown = shown[:5]
	}
	msg := fmt.Sprintf("V7 task plain top layer slips into code words: %s", strings.Join(shown, ", "))
	hint := "keep the top layer plain for a non-engineer; move file paths, symbols, and commands into the ## Implementation notes appendix"
	finding := issue("TASK_TOP_LAYER_CODE_WORDS", msg, where, hint, map[string]any{"words": offenders})
	if v7TaskIsDemanding(note.Data) && strings.TrimSpace(stringField(note.Data, "readiness")) == "ready" {
		*errors = append(*errors, finding)
		return
	}
	*warnings = append(*warnings, finding)
}

// v7TaskIsDemanding reports whether a task carries enough weight that its plain
// top layer must be clean before it can promote to ready: p0/p1 priority or
// medium-and-up risk, matching the spec's ready-gate rule for hard tasks.
func v7TaskIsDemanding(data map[string]any) bool {
	switch strings.ToLower(strings.TrimSpace(stringField(data, "priority"))) {
	case "p0", "p1":
		return true
	}
	switch strings.ToLower(strings.TrimSpace(stringField(data, "risk"))) {
	case "medium", "high", "critical":
		return true
	}
	return false
}

// v7TopLayerCodeWords returns the code-shaped tokens found in the plain top
// layer, deduped in first-seen order. The appendix, table rows, and fenced code
// blocks are skipped.
func v7TopLayerCodeWords(body string) []string {
	top := v7TaskTopLayer(body)
	if strings.TrimSpace(top) == "" {
		return nil
	}
	seen := map[string]bool{}
	var offenders []string
	add := func(word string) {
		word = strings.TrimSpace(word)
		if word == "" || seen[word] {
			return
		}
		seen[word] = true
		offenders = append(offenders, word)
	}

	inFence := false
	for _, line := range strings.Split(top, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		// Skip table rows: acceptance/verification proof cells legitimately
		// carry commands and are exempt from the plain-language check.
		if strings.HasPrefix(trimmed, "|") {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		v7CollectLineCodeWords(line, add)
	}
	return offenders
}

// v7CollectLineCodeWords finds the code-shaped tokens in one prose line:
// backticked spans, dotted filenames, slash paths, and camel-case symbols.
func v7CollectLineCodeWords(line string, add func(string)) {
	// Backticked spans first; their inner text is code by convention.
	remaining := v7CodeBacktickPattern.ReplaceAllStringFunc(line, func(span string) string {
		inner := strings.TrimSpace(strings.Trim(span, "`"))
		if inner != "" {
			add(inner)
		}
		return " "
	})

	for _, path := range v7CodePathPattern.FindAllString(remaining, -1) {
		if v7PathLooksLikeCode(path) {
			add(path)
		}
	}
	for _, file := range v7CodeFilePattern.FindAllString(remaining, -1) {
		add(file)
	}
	for _, token := range regexp.MustCompile(`[A-Za-z][A-Za-z0-9_]*`).FindAllString(remaining, -1) {
		if v7TokenIsCamelCase(token) {
			add(token)
		}
	}
}

// v7PathLooksLikeCode keeps ordinary slash prose ("and/or", "he/she") from
// being flagged: a code path either carries a file extension or has two or more
// slashes (a real directory path).
func v7PathLooksLikeCode(path string) bool {
	if v7CodeFilePattern.MatchString(path) {
		return true
	}
	return strings.Count(path, "/") >= 2
}

// v7TokenIsCamelCase reports whether a token is a camel-case or Pascal-case
// symbol (an internal lowercase-to-uppercase transition), excluding all-caps
// acronyms and known plain-word product names.
func v7TokenIsCamelCase(token string) bool {
	if v7CamelCaseAllowlist[strings.ToLower(token)] {
		return false
	}
	hasTransition := false
	for i := 0; i+1 < len(token); i++ {
		if token[i] >= 'a' && token[i] <= 'z' && token[i+1] >= 'A' && token[i+1] <= 'Z' {
			hasTransition = true
			break
		}
	}
	return hasTransition
}

// v7TaskTopLayer returns the plain top layer: everything above the appendix
// heading. When a task predates the two-layer template and has no appendix, the
// Intent section alone is treated as the top layer so legacy single-layer tasks
// are not flooded with warnings.
func v7TaskTopLayer(body string) string {
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == v7TopLayerAppendixHeading {
			return strings.Join(lines[:i], "\n")
		}
	}
	return sectionContent(body, "## Intent")
}
