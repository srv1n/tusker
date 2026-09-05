package main

import (
	"fmt"
	"path/filepath"
	"strings"
)

const defaultCapsuleTokenBudget = 80

type frontmatterCapsule struct {
	Present  bool
	What     string
	UseWhen  []string
	SkipWhen []string
}

func capsuleScaffold() map[string]any {
	return map[string]any{
		"what":      "",
		"use_when":  []string{},
		"skip_when": []string{},
	}
}

func capsuleBlock(what string, useWhen, skipWhen []string) map[string]any {
	return map[string]any{
		"what":      strings.TrimSpace(what),
		"use_when":  filterStrings(useWhen),
		"skip_when": filterStrings(skipWhen),
	}
}

func frontmatterCapsuleFor(note Note) frontmatterCapsule {
	value, ok := note.Data["capsule"]
	if !ok {
		return frontmatterCapsule{}
	}
	capsule := frontmatterCapsule{Present: true}
	data, mapOK := capsuleMap(value)
	if !mapOK {
		return capsule
	}
	capsule.What = strings.Join(strings.Fields(stringField(data, "what")), " ")
	capsule.UseWhen = normalizeList(data["use_when"])
	capsule.SkipWhen = normalizeList(data["skip_when"])
	return capsule
}

func capsuleMap(value any) (map[string]any, bool) {
	switch current := value.(type) {
	case nil:
		return map[string]any{}, true
	case map[string]any:
		return current, true
	case map[any]any:
		out := map[string]any{}
		for key, item := range current {
			out[toString(key)] = item
		}
		return out, true
	default:
		return nil, false
	}
}

func capsuleHasContent(capsule frontmatterCapsule) bool {
	return capsule.What != "" || len(capsule.UseWhen) > 0 || len(capsule.SkipWhen) > 0
}

func capsulePayload(note Note) any {
	capsule := frontmatterCapsuleFor(note)
	if !capsule.Present {
		return nil
	}
	return map[string]any{
		"what":      capsule.What,
		"use_when":  capsule.UseWhen,
		"skip_when": capsule.SkipWhen,
	}
}

func capsuleSearchText(note Note) string {
	capsule := frontmatterCapsuleFor(note)
	if !capsule.Present {
		return ""
	}
	return strings.Join(append(append([]string{capsule.What}, capsule.UseWhen...), capsule.SkipWhen...), "\n")
}

func capsuleOneLine(note Note) string {
	capsule := frontmatterCapsuleFor(note)
	if !capsuleHasContent(capsule) {
		return ""
	}
	var parts []string
	if capsule.What != "" {
		parts = append(parts, capsule.What)
	}
	if len(capsule.UseWhen) > 0 {
		parts = append(parts, "use when "+strings.Join(capsule.UseWhen, "; "))
	}
	if len(capsule.SkipWhen) > 0 {
		parts = append(parts, "skip when "+strings.Join(capsule.SkipWhen, "; "))
	}
	text := strings.Join(parts, " | ")
	return compactCapsuleOneLine(text, note, defaultCapsuleTokenBudget)
}

func compactCapsuleOneLine(text string, note Note, budget int) string {
	text = strings.TrimSpace(text)
	words := strings.Fields(text)
	if budget <= 0 || len(words) <= budget {
		return text
	}
	continuation := capsuleContinuationTarget(note)
	marker := "... capsule shortened; read " + continuation
	markerWords := len(strings.Fields(marker))
	if markerWords >= budget {
		return strings.Join(words[:budget], " ")
	}
	return strings.Join(append(append([]string{}, words[:budget-markerWords]...), marker), " ")
}

func capsuleContinuationTarget(note Note) string {
	if id := strings.TrimSpace(stringField(note.Data, "id")); id != "" {
		return "`tusker show " + id + " --capsule`"
	}
	if path := strings.TrimSpace(filepath.ToSlash(note.RelativePath)); path != "" {
		return "`" + path + "`"
	}
	return "the full note"
}

func renderFrontmatterCapsule(note Note) string {
	capsule := frontmatterCapsuleFor(note)
	if !capsuleHasContent(capsule) {
		return ""
	}
	var lines []string
	if capsule.What != "" {
		lines = append(lines, "- What: "+capsule.What)
	}
	if len(capsule.UseWhen) > 0 {
		lines = append(lines, "- Use when:")
		for _, item := range capsule.UseWhen {
			lines = append(lines, "  - "+item)
		}
	}
	if len(capsule.SkipWhen) > 0 {
		lines = append(lines, "- Skip when:")
		for _, item := range capsule.SkipWhen {
			lines = append(lines, "  - "+item)
		}
	}
	return strings.Join(lines, "\n")
}

func capsuleTokenCount(capsule frontmatterCapsule) int {
	text := strings.Join(append(append([]string{capsule.What}, capsule.UseWhen...), capsule.SkipWhen...), " ")
	return len(strings.Fields(text))
}

func capsuleRequiredForSpecPath(path string) bool {
	normalized := filepath.ToSlash(path)
	return strings.HasPrefix(normalized, ".tusker/specs/") && strings.HasSuffix(normalized, ".md")
}

func capsuleBudgetFor(vaultPath string) int {
	budget := defaultCapsuleTokenBudget
	if strings.TrimSpace(vaultPath) == "" {
		return budget
	}
	cfg, _, err := readV7TuskerConfig(vaultPath)
	if err != nil {
		return budget
	}
	if cfg.Validation.CapsuleTokenBudget > 0 {
		budget = cfg.Validation.CapsuleTokenBudget
	}
	return budget
}

func validateCapsule(note Note, vaultPath, where string, required bool, errors, warnings *[]Issue) {
	schema := stringField(note.Data, "schema")
	if strings.HasPrefix(schema, "tusker.") && isV7StoreObject(note.Data) {
		validateV7Capsule(note, validationContext{VaultPath: vaultPath}, where, errors, warnings)
		return
	}
	value, present := note.Data["capsule"]
	if !present {
		if required {
			*warnings = append(*warnings, issue("CAPSULE_MISSING", "frontmatter capsule is missing", where, "add capsule.what, capsule.use_when, and capsule.skip_when for one-glance triage", nil))
		}
		return
	}
	if _, ok := capsuleMap(value); !ok {
		*errors = append(*errors, issue(errorInvalidField, "frontmatter capsule must be a map with what/use_when/skip_when", where, "", map[string]any{"field": "capsule"}))
		return
	}
	capsule := frontmatterCapsuleFor(note)
	budget := capsuleBudgetFor(vaultPath)
	count := capsuleTokenCount(capsule)
	failLimit := budget * 2
	if failLimit > 0 && count >= failLimit {
		*errors = append(*errors, issue("CAPSULE_TOO_LONG", fmt.Sprintf("frontmatter capsule has %d tokens; hard limit is %d", count, failLimit), where, "tighten capsule.what/use_when/skip_when; detailed prose belongs in the body", map[string]any{"tokens": count, "budget": budget, "limit": failLimit}))
		return
	}
	if budget > 0 && count > budget {
		*warnings = append(*warnings, issue("CAPSULE_LONG", fmt.Sprintf("frontmatter capsule has %d tokens; budget is %d", count, budget), where, "keep the capsule to one-glance triage detail", map[string]any{"tokens": count, "budget": budget, "limit": budget}))
	}
}
