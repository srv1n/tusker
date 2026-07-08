package main

import (
	"fmt"
	"path/filepath"
	"strings"
)

const defaultV7CapsuleTokenBudget = 80

type v7Capsule struct {
	What     string `json:"what,omitempty"`
	UseWhen  string `json:"use_when,omitempty"`
	SkipWhen string `json:"skip_when,omitempty"`
}

func v7CapsuleFromData(data map[string]any) (v7Capsule, bool, bool) {
	raw, ok := data["capsule"]
	if !ok || raw == nil {
		return v7Capsule{}, false, true
	}
	fields := map[string]any{}
	switch current := raw.(type) {
	case map[string]any:
		fields = current
	case map[any]any:
		for key, value := range current {
			fields[strings.TrimSpace(toString(key))] = value
		}
	case orderedMap:
		for _, item := range current {
			fields[item.Key] = item.Value
		}
	default:
		return v7Capsule{}, true, false
	}
	return v7Capsule{
		What:     strings.Join(strings.Fields(toString(fields["what"])), " "),
		UseWhen:  strings.Join(strings.Fields(toString(fields["use_when"])), " "),
		SkipWhen: strings.Join(strings.Fields(toString(fields["skip_when"])), " "),
	}, true, true
}

func v7CapsuleMap(note Note) map[string]string {
	capsule, present, valid := v7CapsuleFromData(note.Data)
	if !present || !valid {
		return nil
	}
	out := map[string]string{}
	if capsule.What != "" {
		out["what"] = capsule.What
	}
	if capsule.UseWhen != "" {
		out["use_when"] = capsule.UseWhen
	}
	if capsule.SkipWhen != "" {
		out["skip_when"] = capsule.SkipWhen
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func v7CapsuleOrdered(what, useWhen, skipWhen string) orderedMap {
	return orderedMap{
		{Key: "what", Value: what},
		{Key: "use_when", Value: useWhen},
		{Key: "skip_when", Value: skipWhen},
	}
}

func v7EmptyCapsuleOrdered() orderedMap {
	return v7CapsuleOrdered("", "", "")
}

func v7CapsuleTokenCount(capsule v7Capsule) int {
	return len(strings.Fields(strings.Join([]string{capsule.What, capsule.UseWhen, capsule.SkipWhen}, " ")))
}

func v7CapsuleTokenBudget(vaultPath string) int {
	limit := defaultV7CapsuleTokenBudget
	if strings.TrimSpace(vaultPath) == "" {
		return limit
	}
	cfg, _, err := readV7TuskerConfig(vaultPath)
	if err != nil {
		return limit
	}
	if cfg.Validation.CapsuleTokenBudget > 0 {
		limit = cfg.Validation.CapsuleTokenBudget
	}
	return limit
}

func validateV7Capsule(note Note, ctx validationContext, where string, errors, warnings *[]Issue) {
	required := v7CapsuleRequired(note)
	capsule, present, valid := v7CapsuleFromData(note.Data)
	if !present {
		if required {
			*warnings = append(*warnings, issue("CAPSULE_MISSING", "V7 "+v7CapsuleKindLabel(note)+" should include a frontmatter capsule block", where, "add capsule.what, capsule.use_when, and capsule.skip_when for one-glance triage", nil))
		}
		return
	}
	if !valid {
		*errors = append(*errors, issue(errorInvalidField, "V7 capsule must be a frontmatter mapping with what, use_when, and skip_when", where, "", map[string]any{"field": "capsule"}))
		return
	}
	budget := v7CapsuleTokenBudget(ctx.VaultPath)
	if budget <= 0 {
		return
	}
	count := v7CapsuleTokenCount(capsule)
	context := map[string]any{"tokens": count, "budget": budget, "fail_limit": budget * 2}
	if count > budget*2 {
		*errors = append(*errors, issue("CAPSULE_TOKEN_BUDGET_EXCEEDED", fmt.Sprintf("V7 capsule has %d tokens; fail limit is %d", count, budget*2), where, "shorten capsule.what, capsule.use_when, and capsule.skip_when", context))
		return
	}
	if count > budget {
		*warnings = append(*warnings, issue("CAPSULE_TOKEN_BUDGET_WARN", fmt.Sprintf("V7 capsule has %d tokens; warning budget is %d", count, budget), where, "keep the capsule short enough for bulk triage", context))
	}
}

func v7CapsuleRequired(note Note) bool {
	schema := stringField(note.Data, "schema")
	kind := effectiveV7Kind(note.Data)
	switch kind {
	case "domain", "domain_canon", "knowledge", "project_skill", "epic", "doc", "spec":
		return strings.HasSuffix(schema, "/v7")
	default:
		return schema == "tusker.knowledge/v7" || schema == "tusker.doc/v7" || schema == "tusker.spec/v7"
	}
}

func v7CapsuleKindLabel(note Note) string {
	switch kind := effectiveV7Kind(note.Data); kind {
	case "domain_canon":
		return "domain canon"
	case "project_skill":
		return "project skill"
	case "":
		return "note"
	default:
		return strings.ReplaceAll(kind, "_", " ")
	}
}

func renderV7FrontmatterCapsule(note Note) (string, bool) {
	capsule, present, valid := v7CapsuleFromData(note.Data)
	if !present || !valid {
		return "", false
	}
	var lines []string
	lines = append(lines, "- What: "+firstNonEmpty(capsule.What, "not specified"))
	lines = append(lines, "- Use when: "+firstNonEmpty(capsule.UseWhen, "not specified"))
	lines = append(lines, "- Skip when: "+firstNonEmpty(capsule.SkipWhen, "not specified"))
	return strings.Join(lines, "\n"), true
}

func v7CapsuleSearchText(note Note) string {
	capsule, present, valid := v7CapsuleFromData(note.Data)
	if !present || !valid {
		return ""
	}
	return strings.Join([]string{capsule.What, capsule.UseWhen, capsule.SkipWhen}, "\n")
}

func v7RoutedFileCapsules(vaultPath string, task Note) string {
	type routedFile struct {
		Path string
		Note Note
	}
	var files []routedFile
	if note, ok := readV7RoutedFile(vaultPath, "SKILL.md"); ok {
		files = append(files, routedFile{Path: vaultDisplayPath(vaultPath, "SKILL.md"), Note: note})
	}
	for _, domain := range v7PacketDomains(vaultPath, task) {
		for _, rel := range []string{
			filepath.ToSlash(filepath.Join("knowledge", "domains", domain, "INDEX.md")),
			filepath.ToSlash(filepath.Join("knowledge", "domains", domain, "CANON.md")),
		} {
			if note, ok := readV7RoutedFile(vaultPath, rel); ok {
				files = append(files, routedFile{Path: rel, Note: note})
			}
		}
	}
	if len(files) == 0 {
		return "- None available."
	}
	var sections []string
	for _, file := range files {
		capsule, present := renderV7FrontmatterCapsule(file.Note)
		if !present {
			sections = append(sections, "- `"+file.Path+"`: capsule missing.")
			continue
		}
		sections = append(sections, "- `"+file.Path+"`\n"+indentMarkdown(capsule, "  "))
	}
	return strings.Join(sections, "\n")
}

func v7PacketDomains(vaultPath string, task Note) []string {
	domains := normalizeList(task.Data["domains"])
	if len(domains) > 0 {
		return domains
	}
	if fileExists(filepath.Join(vaultPath, "knowledge", "domains", "project", "INDEX.md")) &&
		fileExists(filepath.Join(vaultPath, "knowledge", "domains", "project", "CANON.md")) {
		return []string{"project"}
	}
	return nil
}

func readV7RoutedFile(vaultPath, rel string) (Note, bool) {
	path := filepath.Join(vaultPath, filepath.FromSlash(rel))
	if !fileExists(path) {
		return Note{}, false
	}
	data, body, err := parseFrontmatterMustRead(path)
	if err != nil {
		return Note{}, false
	}
	return Note{AbsolutePath: path, RelativePath: rel, Data: data, Body: body}, true
}

func indentMarkdown(text, prefix string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			lines[i] = line
			continue
		}
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}
