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

var (
	// A dotted filename like foo.go or app.tsx. Single-letter extensions
	// (.c/.h/.m) require a stem of two or more characters so ordinary prose
	// abbreviations like "p.m." or "a.m." are not mistaken for filenames.
	v7CodeFilePattern = regexp.MustCompile(`(?i)\b(?:[\w-]+\.(?:go|ts|tsx|js|jsx|mjs|cjs|py|rs|rb|md|ya?ml|json|toml|swift|java|kt|kts|cc|cpp|hpp|mm|sh|sql|proto|css|scss|html|tf)|[\w-]{2,}\.[chm])\b`)
	// A slash path token like cmd/tusker/foo or internal/serve/ui.
	v7CodePathPattern = regexp.MustCompile(`[\w.-]+(?:/[\w.-]+)+`)
	// A backticked span; whatever is inside is code, by convention.
	v7CodeBacktickPattern = regexp.MustCompile("`[^`]+`")
	// The builder appendix heading. Case-insensitive, tolerant of heading
	// level (## or ###) and trailing variations ("Implementation Notes").
	v7AppendixHeadingPattern = regexp.MustCompile(`(?i)^#{2,3}\s+implementation notes\b`)
	// An identifier-shaped token.
	v7IdentTokenPattern = regexp.MustCompile(`[A-Za-z][A-Za-z0-9_]*`)
)

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
// blocks are skipped. A mixed-case word (product names read as plain prose) is
// only flagged when corroborating code evidence backs it: see v7MixedCaseIsCode.
func v7TopLayerCodeWords(body string) []string {
	top, appendix := v7TaskLayers(body)
	if strings.TrimSpace(top) == "" {
		return nil
	}
	appendixSymbols := v7IdentifierSet(appendix)

	seen := map[string]bool{}
	seenBase := map[string]bool{}
	var offenders []string
	// add dedups by exact token and by base name, so a slash path and the
	// bare filename it ends with ("cmd/tusker/foo.go" and "foo.go") together
	// occupy a single display slot rather than double-reporting.
	add := func(word string) {
		word = strings.TrimSpace(word)
		if word == "" || seen[word] {
			return
		}
		base := word
		if idx := strings.LastIndex(word, "/"); idx >= 0 {
			base = word[idx+1:]
		}
		if seenBase[base] {
			return
		}
		seen[word] = true
		seenBase[base] = true
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
		v7CollectLineCodeWords(line, appendixSymbols, add)
	}
	return offenders
}

// v7CollectLineCodeWords finds the code-shaped tokens in one prose line:
// backticked spans, dotted filenames, slash paths, and mixed-case symbols that
// carry corroborating code evidence.
func v7CollectLineCodeWords(line string, appendixSymbols map[string]bool, add func(string)) {
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
	for _, loc := range v7IdentTokenPattern.FindAllStringIndex(remaining, -1) {
		token := remaining[loc[0]:loc[1]]
		if !v7TokenIsMixedCase(token) {
			continue
		}
		if v7MixedCaseIsCode(token, remaining, loc[0], loc[1], appendixSymbols) {
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

// v7TokenIsMixedCase reports whether a token has an internal lowercase-to-
// uppercase transition (camelCase or PascalCase), excluding all-caps acronyms
// and plain capitalized words.
func v7TokenIsMixedCase(token string) bool {
	for i := 0; i+1 < len(token); i++ {
		if token[i] >= 'a' && token[i] <= 'z' && token[i+1] >= 'A' && token[i+1] <= 'Z' {
			return true
		}
	}
	return false
}

// v7MixedCaseIsCode decides whether a mixed-case token is a real code symbol
// rather than a marketing-style product name (TestFlight, PayPal, iCloud). A
// bare product name with no corroborating evidence is left alone; a token is
// flagged only when it looks like a genuine identifier or the surrounding text
// treats it as code:
//
//   - it is lowerCamelCase in the identifier sense (v7MixedCaseIsCode's helper),
//   - it sits next to code punctuation ((), ., ::, ->, =, or path separators),
//   - it ends in a code-ish suffix (Cmd, Fn, Func, Impl, Ctx, Cfg, Err, Ptr, or
//     a compound Id/ID/IDs like taskID), or
//   - it appears verbatim among the symbols named in the builder appendix.
func v7MixedCaseIsCode(token, line string, start, end int, appendixSymbols map[string]bool) bool {
	if v7IsLowerCamelSymbol(token) {
		return true
	}
	if v7HasCodeAdjacency(line, start, end) {
		return true
	}
	if v7HasCodeSuffix(token) {
		return true
	}
	if appendixSymbols[token] {
		return true
	}
	return false
}

// v7IsLowerCamelSymbol reports whether a mixed-case token reads as a genuine
// lowerCamelCase identifier. A leading lowercase word (two or more letters, like
// "schedule" in scheduleBatchGateIfDue) or an embedded digit (v7TaskBody) marks
// a real symbol, while a single leading lowercase letter ("iCloud") does not.
func v7IsLowerCamelSymbol(token string) bool {
	if token == "" || token[0] < 'a' || token[0] > 'z' {
		return false
	}
	runLen := 0
	for runLen < len(token) && token[runLen] >= 'a' && token[runLen] <= 'z' {
		runLen++
	}
	if runLen >= 2 {
		return true
	}
	return strings.ContainsAny(token, "0123456789")
}

// v7HasCodeAdjacency reports whether the token at [start,end) in line abuts a
// character that only appears when the surrounding text is treating it as code:
// a call paren, an assignment, an arrow or scope operator, a path separator, or
// an attached dot (method or field access, distinct from a sentence period).
func v7HasCodeAdjacency(line string, start, end int) bool {
	var before, after byte
	hasBefore := start > 0
	if hasBefore {
		before = line[start-1]
	}
	hasAfter := end < len(line)
	if hasAfter {
		after = line[end]
	}
	if hasAfter {
		switch after {
		case '(', '=', '/', ':':
			return true
		case '.':
			// Attached dot only counts as method access when an identifier
			// follows it (Bar.foo), not a sentence period ("Bar. ").
			if end+1 < len(line) && v7IsIdentByte(line[end+1]) {
				return true
			}
		}
	}
	if hasBefore {
		switch before {
		case '=', '/', ':', '>', '.':
			return true
		}
	}
	return false
}

// v7HasCodeSuffix reports whether a token ends in a suffix that marks it as a
// code symbol: Cmd/Fn/Func/Impl/Ctx/Cfg/Err/Ptr, or a compound identifier
// suffix Id/ID/IDs (taskID), but not the bare word "ID" on its own.
func v7HasCodeSuffix(token string) bool {
	for _, suf := range []string{"Cmd", "Fn", "Func", "Impl", "Ctx", "Cfg", "Err", "Ptr"} {
		if strings.HasSuffix(token, suf) {
			return true
		}
	}
	for _, suf := range []string{"IDs", "ID", "Ids", "Id"} {
		if strings.HasSuffix(token, suf) && len(token) > len(suf) {
			return true
		}
	}
	return false
}

func v7IsIdentByte(b byte) bool {
	return b == '_' ||
		(b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9')
}

// v7IdentifierSet returns the set of identifier-shaped tokens in text, used to
// recognize a top-layer word that also appears verbatim in the builder
// appendix (a strong signal it names a real symbol).
func v7IdentifierSet(text string) map[string]bool {
	set := map[string]bool{}
	for _, token := range v7IdentTokenPattern.FindAllString(text, -1) {
		set[token] = true
	}
	return set
}

// v7TaskLayers splits a task body into its plain top layer (everything above the
// appendix heading) and the builder appendix (everything from the heading on).
// When a task predates the two-layer template and has no appendix, the Intent
// section alone is treated as the top layer so legacy single-layer tasks are not
// flooded with warnings, and the appendix is empty.
func v7TaskLayers(body string) (top, appendix string) {
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if v7AppendixHeadingPattern.MatchString(strings.TrimSpace(line)) {
			return strings.Join(lines[:i], "\n"), strings.Join(lines[i:], "\n")
		}
	}
	return sectionContent(body, "## Intent"), ""
}
