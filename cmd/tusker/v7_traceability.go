package main

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	v7WorkStreamTaskIDPattern  = regexp.MustCompile(`\b[A-Z]{3}-T-\d{4}\b`)
	v7WorkStreamEpicPathRegexp = regexp.MustCompile(`(?:^|[\s(])(?:\.tusker/)?work/epics/([A-Z]{3})\.md\b`)
	v7WikiLinkPattern          = regexp.MustCompile(`\[\[([^\]]+)\]\]`)
	v7MarkdownLinkPattern      = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
)

type v7TraceabilityDoc struct {
	Path string
	Body string
}

func v7SpecRefsCapsuleLine(vaultPath string, note Note) string {
	kind := noteDisplayKind(note.Data)
	if kind != "task" && kind != "epic" {
		return ""
	}
	refs := normalizeList(note.Data["spec_refs"])
	if len(refs) == 0 {
		return ""
	}
	return "- Read next spec refs: " + strings.Join(v7SpecRefDisplayTargets(vaultPath, refs), ", ")
}

func v7SpecRefsPacketSection(vaultPath string, note Note) string {
	refs := normalizeList(note.Data["spec_refs"])
	if len(refs) == 0 {
		return "- None declared."
	}
	lines := []string{"Read these governing specs/decisions before implementation:"}
	for _, target := range v7SpecRefDisplayTargets(vaultPath, refs) {
		lines = append(lines, "- "+target)
	}
	return strings.Join(lines, "\n")
}

func v7SpecRefDisplayTargets(vaultPath string, refs []string) []string {
	var out []string
	for _, ref := range refs {
		clean := v7CleanSpecRef(ref)
		if clean == "" {
			continue
		}
		readPath := v7SpecRefReadPath(vaultPath, clean)
		if readPath != "" && readPath != clean {
			out = append(out, "`"+clean+"` -> `"+readPath+"`")
			continue
		}
		out = append(out, "`"+clean+"`")
	}
	return out
}

func v7SpecRefReadPath(vaultPath, ref string) string {
	ref = v7CleanSpecRef(ref)
	if ref == "" {
		return ""
	}
	if id := v7SpecRefDecisionID(ref); id != "" {
		return vaultDisplayPath(vaultPath, filepath.ToSlash(filepath.Join("work", "decisions", id+".md")))
	}
	if strings.HasPrefix(ref, "work/") {
		return vaultDisplayPath(vaultPath, ref)
	}
	return ref
}

func v7SpecRefRequiredReads(vaultPath string, note Note) []string {
	var reads []string
	for _, ref := range normalizeList(note.Data["spec_refs"]) {
		if target := v7SpecRefReadPath(vaultPath, ref); target != "" {
			reads = append(reads, target)
		}
	}
	return uniqueStrings(reads)
}

func validateV7SpecTraceability(vaultPath string, notes []Note) []Issue {
	var warnings []Issue
	taskIDs := map[string]bool{}
	epicIDs := map[string]bool{}
	decisionIDs := map[string]Note{}
	var workNotes []Note
	var decisionDocs []v7TraceabilityDoc

	for _, note := range notes {
		kind := effectiveV7Kind(note.Data)
		switch kind {
		case "task":
			if strings.HasSuffix(stringField(note.Data, "schema"), "/v7") {
				taskIDs[stringField(note.Data, "id")] = true
				workNotes = append(workNotes, note)
			}
		case "epic":
			if strings.HasSuffix(stringField(note.Data, "schema"), "/v7") {
				epicIDs[stringField(note.Data, "id")] = true
				workNotes = append(workNotes, note)
			}
		case "decision":
			id := stringField(note.Data, "id")
			if id != "" {
				decisionIDs[id] = note
			}
			decisionDocs = append(decisionDocs, v7TraceabilityDoc{Path: vaultDisplayPath(vaultPath, note.RelativePath), Body: note.Body})
		}
	}

	for _, note := range workNotes {
		warnings = append(warnings, validateV7SpecRefs(vaultPath, note, decisionIDs)...)
	}

	docs, docWarnings := v7TraceabilityDocs(vaultPath, decisionDocs)
	warnings = append(warnings, docWarnings...)
	for _, doc := range docs {
		content := v7WorkStreamsSection(doc.Body)
		if content == "" {
			continue
		}
		for _, ref := range v7WorkStreamRefs(content) {
			switch {
			case v7TaskIDPattern.MatchString(ref):
				if !taskIDs[ref] {
					warnings = append(warnings, issue("WORK_STREAM_REF_DANGLING", "Work streams references unknown task "+ref, doc.Path, "link to an existing task or remove the stale work-stream link", map[string]any{"ref": ref}))
				}
			case epicAcronymPattern.MatchString(ref):
				if !epicIDs[ref] {
					warnings = append(warnings, issue("WORK_STREAM_REF_DANGLING", "Work streams references unknown epic "+ref, doc.Path, "link to an existing epic or remove the stale work-stream link", map[string]any{"ref": ref}))
				}
			}
		}
	}
	return warnings
}

func validateV7SpecRefs(vaultPath string, note Note, decisionIDs map[string]Note) []Issue {
	var warnings []Issue
	for _, ref := range normalizeList(note.Data["spec_refs"]) {
		clean := v7CleanSpecRef(ref)
		if clean == "" {
			continue
		}
		if v7SpecRefExists(vaultPath, clean, decisionIDs) {
			continue
		}
		warnings = append(warnings, issue("SPEC_REF_DANGLING", fmt.Sprintf("%s spec_refs reference does not resolve: %s", effectiveV7Kind(note.Data), clean), note.RelativePath, "use a repo-relative docs/specs or docs/design path, a repo-relative decision path, or a V7 decision id", map[string]any{"ref": clean}))
	}
	return warnings
}

func v7SpecRefExists(vaultPath, ref string, decisionIDs map[string]Note) bool {
	if id := v7SpecRefDecisionID(ref); id != "" {
		_, ok := decisionIDs[id]
		return ok
	}
	if v7SpecRefPathEscapes(ref) {
		return false
	}
	repoRoot := v7RepoRoot(vaultPath)
	candidates := []string{filepath.Join(repoRoot, filepath.FromSlash(ref))}
	if strings.HasPrefix(ref, "work/") {
		candidates = append(candidates, filepath.Join(vaultPath, filepath.FromSlash(ref)))
	}
	for _, candidate := range candidates {
		if fileExists(candidate) {
			return true
		}
	}
	return false
}

func v7SpecRefDecisionID(ref string) string {
	target := v7CleanSpecRef(ref)
	if v7DecisionIDPattern.MatchString(target) {
		return target
	}
	return ""
}

func v7SpecRefPathEscapes(ref string) bool {
	if filepath.IsAbs(ref) {
		return true
	}
	clean := filepath.Clean(filepath.FromSlash(ref))
	return clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator))
}

func v7CleanSpecRef(ref string) string {
	ref = strings.TrimSpace(strings.Trim(ref, "`"))
	if ref == "" {
		return ""
	}
	ref = wikiTarget(ref)
	ref = strings.Split(ref, "#")[0]
	ref = strings.TrimSpace(strings.Trim(ref, "`"))
	if ref == "" || strings.Contains(ref, "://") {
		return ref
	}
	ref = filepath.ToSlash(filepath.Clean(filepath.FromSlash(ref)))
	if ref == "." {
		return ""
	}
	return strings.TrimPrefix(ref, "./")
}

func v7TraceabilityDocs(vaultPath string, decisionDocs []v7TraceabilityDoc) ([]v7TraceabilityDoc, []Issue) {
	docs := append([]v7TraceabilityDoc{}, decisionDocs...)
	var warnings []Issue
	repoRoot := v7RepoRoot(vaultPath)
	for _, relRoot := range []string{"docs/specs", "docs/design"} {
		root := filepath.Join(repoRoot, filepath.FromSlash(relRoot))
		if !dirExists(root) {
			continue
		}
		err := walkDirUnsorted(root, func(current string, entry fs.DirEntry) error {
			if entry.IsDir() {
				return nil
			}
			if !strings.HasSuffix(entry.Name(), ".md") {
				return nil
			}
			body, err := readText(current)
			if err != nil {
				rel := v7PathForMessage(vaultPath, current)
				warnings = append(warnings, issue("WORK_STREAM_DOC_READ_FAILED", "could not read traceability doc: "+err.Error(), rel, "", nil))
				return nil
			}
			rel, err := filepath.Rel(repoRoot, current)
			if err != nil {
				rel = current
			}
			docs = append(docs, v7TraceabilityDoc{Path: filepath.ToSlash(rel), Body: body})
			return nil
		})
		if err != nil {
			warnings = append(warnings, issue("WORK_STREAM_DOC_SCAN_FAILED", "could not scan traceability docs: "+err.Error(), relRoot, "", nil))
		}
	}
	return docs, warnings
}

func v7WorkStreamsSection(body string) string {
	for _, heading := range []string{"## Work streams", "## Work Streams"} {
		if content := strings.TrimSpace(sectionContent(body, heading)); content != "" {
			return content
		}
	}
	return ""
}

func v7WorkStreamRefs(content string) []string {
	seen := map[string]bool{}
	add := func(raw string) {
		if ref := v7WorkStreamRefID(raw); ref != "" {
			seen[ref] = true
		}
	}
	for _, match := range v7WorkStreamTaskIDPattern.FindAllString(content, -1) {
		add(match)
	}
	for _, match := range v7WorkStreamEpicPathRegexp.FindAllStringSubmatch(content, -1) {
		if len(match) > 1 {
			add(match[1])
		}
	}
	for _, match := range v7WikiLinkPattern.FindAllStringSubmatch(content, -1) {
		if len(match) > 1 {
			add(match[1])
		}
	}
	for _, match := range v7MarkdownLinkPattern.FindAllStringSubmatch(content, -1) {
		if len(match) > 1 {
			add(match[1])
		}
		if len(match) > 2 {
			add(match[2])
		}
	}
	refs := make([]string, 0, len(seen))
	for ref := range seen {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	return refs
}

func v7WorkStreamRefID(raw string) string {
	target := strings.TrimSpace(strings.Trim(wikiTarget(raw), "`"))
	target = strings.Split(target, "#")[0]
	target = strings.TrimPrefix(target, "./")
	target = filepath.ToSlash(target)
	base := strings.TrimSuffix(filepath.Base(filepath.FromSlash(target)), ".md")
	if v7TaskIDPattern.MatchString(base) {
		return base
	}
	if epicAcronymPattern.MatchString(base) {
		if target == base || strings.Contains(target, "work/epics/") {
			return base
		}
	}
	return ""
}
