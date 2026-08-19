package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func newV7Domain(args Args) error {
	args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	actor, err := v7AgentDefaultActor(args, "domain create")
	if err != nil {
		return err
	}
	id, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	id = strings.TrimSpace(id)
	if err := validateKnowledgeNodePath(id); err != "" {
		return tuskerError(errorInvalidArg, fmt.Sprintf(`invalid V7 domain id "%s": %s`, id, err))
	}
	if strings.Contains(id, "/") {
		return tuskerError(errorInvalidArg, "V7 domain id must be one portable path segment")
	}
	status := strings.ToLower(fallback(args.String("status"), "current"))
	if _, ok := v7DomainStatus[status]; !ok {
		return tuskerError(errorInvalidField, "invalid V7 domain status: "+status)
	}
	title := fallback(args.String("title"), v7DefaultDomainTitle(id))
	summary := fallback(args.String("summary"), "Durable source of truth for "+title+".")
	domainDir := filepath.Join(vaultPath, "knowledge", "domains", id)
	indexPath := filepath.Join(domainDir, "INDEX.md")
	canonPath := filepath.Join(domainDir, "CANON.md")
	if fileExists(indexPath) || fileExists(canonPath) {
		return tuskerError(errorAlreadyExists, "V7 domain already exists: "+id, withPath(domainDir))
	}
	if err := bootstrapV7Dirs(vaultPath); err != nil {
		return err
	}
	if err := ensureV7DomainLayout(domainDir); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	sourceOfTruth := splitCSV(args.String("source-of-truth"))
	if len(sourceOfTruth) == 0 {
		sourceOfTruth = []string{filepath.ToSlash(filepath.Join("knowledge", "domains", id, "CANON.md"))}
	}
	indexData := map[string]any{
		"schema":  "tusker.domain/v7",
		"kind":    "domain",
		"id":      id,
		"project": v7ProjectID(vaultPath),
		"title":   title,
		"status":  status,
		"summary": summary,
		"capsule": v7CapsuleOrdered(
			"Domain index for "+title+"; routes agents to canon and owned knowledge files.",
			"Use when a task touches "+id+" behavior or needs the domain reading order.",
			"Skip when another domain is narrower or task proof/gates are the target.",
		),
		"source_of_truth": sourceOfTruth,
		"canonical_files": []string{"INDEX.md", "CANON.md"},
		"created_at":      now,
		"updated_at":      now,
	}
	indexBody := v7DomainIndexBody(id, title, summary)
	indexData["state_rev"] = v7StateRev(indexData, indexBody)
	indexContent, err := serializeDocument(indexData, indexBody, v7FrontmatterOrder["domain"])
	if err != nil {
		return err
	}
	canonData := map[string]any{
		"schema":  "tusker.domain-canon/v7",
		"kind":    "domain_canon",
		"id":      id + "/canon",
		"project": v7ProjectID(vaultPath),
		"domain":  id,
		"title":   title + " Canon",
		"status":  status,
		"summary": "Current durable truth for " + title + ".",
		"capsule": v7CapsuleOrdered(
			"Current durable truth, invariants, and constraints for "+title+".",
			"Use before changing behavior owned by "+id+" or reviewing a domain-impacting task.",
			"Skip when you only need task proof, runtime events, or generated packets.",
		),
		"source_of_truth": sourceOfTruth,
		"created_at":      now,
		"updated_at":      now,
	}
	canonBody := v7DomainCanonBody(id, title)
	canonData["state_rev"] = v7StateRev(canonData, canonBody)
	canonContent, err := serializeDocument(canonData, canonBody, v7FrontmatterOrder["domain_canon"])
	if err != nil {
		return err
	}
	if err := writeText(indexPath, indexContent); err != nil {
		return err
	}
	if err := writeText(canonPath, canonContent); err != nil {
		return err
	}
	if err := refreshV7ProjectSkill(vaultPath); err != nil {
		return err
	}
	if !args.Bool("quiet") {
		fmt.Printf("Created V7 domain %s at %s\n", id, domainDir)
	}
	return emitV7Event(vaultPath, id, "domain", "created", actor, map[string]any{"path": filepath.ToSlash(filepath.Join("knowledge", "domains", id, "INDEX.md"))})
}

func domainV7ListCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	notes, err := listAllNotes(vaultPath)
	if err != nil {
		return err
	}
	var domains []Note
	for _, note := range notes {
		if effectiveV7Kind(note.Data) == "domain" && stringField(note.Data, "schema") == "tusker.domain/v7" {
			domains = append(domains, note)
		}
	}
	sort.Slice(domains, func(i, j int) bool {
		return stringField(domains[i].Data, "id") < stringField(domains[j].Data, "id")
	})
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "domains": v7DomainPayload(domains)})
		return nil
	}
	for _, domain := range domains {
		fmt.Printf("- %s — %s\n", stringField(domain.Data, "id"), stringField(domain.Data, "title"))
		if summary := stringField(domain.Data, "summary"); summary != "" {
			fmt.Printf("  %s\n", summary)
		}
	}
	return nil
}

func domainV7ShowCmd(args Args) error {
	args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	id, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	note, err := readV7DomainIndex(vaultPath, id)
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "domain": v7DomainPayload([]Note{note})[0]})
		return nil
	}
	if args.Bool("full") {
		content, err := readText(note.AbsolutePath)
		if err != nil {
			return err
		}
		fmt.Print(content)
		if !strings.HasSuffix(content, "\n") {
			fmt.Println()
		}
		return nil
	}
	fmt.Print(v7DomainCapsule(note))
	return nil
}

func domainV7CanonCmd(args Args) error {
	args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	id, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	note, err := readV7DomainCanon(vaultPath, id)
	if err != nil {
		return err
	}
	if args.Bool("json") {
		item := map[string]any{
			"id":      stringField(note.Data, "id"),
			"domain":  stringField(note.Data, "domain"),
			"title":   stringField(note.Data, "title"),
			"status":  stringField(note.Data, "status"),
			"summary": stringField(note.Data, "summary"),
			"path":    note.RelativePath,
		}
		if capsule := v7CapsuleMap(note); len(capsule) > 0 {
			item["capsule"] = capsule
		}
		emitJSON(map[string]any{"ok": true, "canon": item})
		return nil
	}
	if args.Bool("full") {
		content, err := readText(note.AbsolutePath)
		if err != nil {
			return err
		}
		fmt.Print(content)
		if !strings.HasSuffix(content, "\n") {
			fmt.Println()
		}
		return nil
	}
	fmt.Printf("%s  domain canon  %s\n\n%s\n", stringField(note.Data, "domain"), stringField(note.Data, "title"), sectionContent(note.Body, "## Current Truth"))
	return nil
}

func readV7DomainIndex(vaultPath, id string) (Note, error) {
	id = strings.TrimSpace(id)
	if err := validateKnowledgeNodePath(id); err != "" || strings.Contains(id, "/") {
		return Note{}, tuskerError(errorInvalidArg, "invalid V7 domain id: "+id)
	}
	path := filepath.Join(vaultPath, "knowledge", "domains", id, "INDEX.md")
	if !fileExists(path) {
		return Note{}, tuskerError(errorNotFound, "V7 domain not found: "+id, withPath(path))
	}
	data, body, err := parseFrontmatterMustRead(path)
	if err != nil {
		return Note{}, err
	}
	return Note{AbsolutePath: path, RelativePath: filepath.ToSlash(filepath.Join("knowledge", "domains", id, "INDEX.md")), Data: data, Body: body}, nil
}

func readV7DomainCanon(vaultPath, id string) (Note, error) {
	id = strings.TrimSpace(id)
	if err := validateKnowledgeNodePath(id); err != "" || strings.Contains(id, "/") {
		return Note{}, tuskerError(errorInvalidArg, "invalid V7 domain id: "+id)
	}
	path := filepath.Join(vaultPath, "knowledge", "domains", id, "CANON.md")
	if !fileExists(path) {
		return Note{}, tuskerError(errorNotFound, "V7 domain canon not found: "+id, withPath(path))
	}
	data, body, err := parseFrontmatterMustRead(path)
	if err != nil {
		return Note{}, err
	}
	return Note{AbsolutePath: path, RelativePath: filepath.ToSlash(filepath.Join("knowledge", "domains", id, "CANON.md")), Data: data, Body: body}, nil
}

func v7DomainPayload(notes []Note) []map[string]any {
	payload := make([]map[string]any, 0, len(notes))
	for _, note := range notes {
		item := map[string]any{
			"id":              stringField(note.Data, "id"),
			"title":           stringField(note.Data, "title"),
			"status":          stringField(note.Data, "status"),
			"summary":         stringField(note.Data, "summary"),
			"source_of_truth": normalizeList(note.Data["source_of_truth"]),
			"canonical_files": normalizeList(note.Data["canonical_files"]),
			"path":            note.RelativePath,
		}
		if capsule := v7CapsuleMap(note); len(capsule) > 0 {
			item["capsule"] = capsule
		}
		payload = append(payload, item)
	}
	return payload
}

func v7DomainCapsule(note Note) string {
	if capsule := strings.TrimSpace(renderFrontmatterCapsule(note)); capsule != "" {
		return fmt.Sprintf(`%s  domain  %s

%s
Status: %s
Summary: %s
Path: %s

Read this when:
%s
`, stringField(note.Data, "id"), stringField(note.Data, "title"), capsule, stringField(note.Data, "status"), stringField(note.Data, "summary"), note.RelativePath, sectionContent(note.Body, "## Read This When"))
	}
	return fmt.Sprintf(`%s  domain  %s

Status: %s
Summary: %s
Path: %s

Read this when:
%s
`, stringField(note.Data, "id"), stringField(note.Data, "title"), stringField(note.Data, "status"), stringField(note.Data, "summary"), note.RelativePath, sectionContent(note.Body, "## Read This When"))
}

func v7DefaultDomainTitle(id string) string {
	words := strings.NewReplacer("-", " ", "_", " ").Replace(id)
	return strings.Title(words)
}

func v7DomainIndexBody(id, title, summary string) string {
	return fmt.Sprintf(`# %s

## Summary

%s

## Read This When

- You need current source-of-truth context for %s.
- You are changing behavior owned by this domain.

## Canonical Files

- CANON.md - current durable truth.
- INDEX.md - domain map and routing hints.

## Runbooks

- _None yet._

## Interfaces

- _No stable interfaces declared yet._

## Invariants

- Keep durable truth in CANON.md.
- Put procedural guidance in runbooks/.

## Sources

- Raw external input belongs in sources/. Do not treat root docs/ or site output as canonical V7 knowledge.

## Glossary

- See glossary.md.

## Current Work

- _No current work linked._
`, title, summary, id)
}

func v7DomainCanonBody(id, title string) string {
	return fmt.Sprintf(`# %s Canon

## Current Truth

- %s is the canonical domain id for %s.

## Stable Interfaces

- _No stable interfaces declared yet._

## Constraints

- Keep this canon short enough to read before implementation.
- Move obsolete details to Deprecated Or Stale instead of deleting useful history.

## Deprecated Or Stale

- _None known._

## Open Questions

- _None yet._
`, title, id, title)
}

func bootstrapV7Profile(vaultPath string, profile string) error {
	if err := bootstrapV7(Args{"vault": vaultPath, "quiet": "true"}); err != nil {
		return err
	}
	if err := bootstrapV7Dirs(vaultPath); err != nil {
		return err
	}
	hadProjectSkill := fileExists(filepath.Join(vaultPath, "SKILL.md"))
	for _, domain := range defaultV7ProfileDomains(profile) {
		if err := ensureV7Domain(vaultPath, domain, v7DefaultDomainTitle(domain), "Durable project knowledge for "+domain+"."); err != nil {
			return err
		}
	}
	if !hadProjectSkill || hasV7ProjectSkill(vaultPath) || hasV7KnowledgeDomains(vaultPath) {
		if err := writeV7ProjectSkill(vaultPath, filepath.Join(vaultPath, "SKILL.md")); err != nil {
			return err
		}
	}
	return nil
}

func defaultV7ProfileDomains(profile string) []string {
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case "cli":
		return []string{"project", "cli"}
	case "app":
		return []string{"project", "product"}
	case "infra":
		return []string{"project", "operations"}
	case "library":
		return []string{"project", "api"}
	default:
		return []string{"project"}
	}
}

func ensureV7Domain(vaultPath, id, title, summary string) error {
	indexPath := filepath.Join(vaultPath, "knowledge", "domains", id, "INDEX.md")
	canonPath := filepath.Join(vaultPath, "knowledge", "domains", id, "CANON.md")
	if fileExists(indexPath) && fileExists(canonPath) {
		return ensureV7DomainLayout(filepath.Join(vaultPath, "knowledge", "domains", id))
	}
	return newV7Domain(Args{
		"vault":   vaultPath,
		"quiet":   "true",
		"v7":      "true",
		"id":      id,
		"title":   title,
		"summary": summary,
	})
}

func ensureV7DomainLayout(domainDir string) error {
	for _, relative := range []string{"runbooks", "decisions", "interfaces", "invariants", "sources", "glossary"} {
		if err := ensureDir(filepath.Join(domainDir, relative)); err != nil {
			return err
		}
	}
	glossaryPath := filepath.Join(domainDir, "glossary.md")
	if !fileExists(glossaryPath) {
		if err := writeText(glossaryPath, "# Glossary\n\n- _No terms yet._\n"); err != nil {
			return err
		}
	}
	return nil
}

func knowledgeV7NewCmd(args Args) error {
	args["node"] = firstNonEmpty(args.String("node"), args.String("_pos0"))
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	node, err := requireArg(args, "node")
	if err != nil {
		return err
	}
	node = strings.Trim(strings.ToLower(strings.TrimSpace(node)), "/")
	if reason := validateKnowledgeNodePath(node); reason != "" {
		return tuskerError(errorInvalidArg, fmt.Sprintf(`invalid V7 knowledge node "%s": %s`, node, reason))
	}
	if strings.Contains(node, "/references/") || strings.HasPrefix(node, "references/") || strings.HasSuffix(node, "/references") {
		return tuskerError(errorInvalidArg, "V7 knowledge uses sources/ for raw external input, not references/")
	}
	parts := strings.Split(node, "/")
	if len(parts) != 3 {
		return tuskerError(errorInvalidArg, "V7 knowledge node must be <domain>/<folder>/<slug>", withHint("example: tusker knowledge new providers/runbooks/oauth-refresh --v7 --kind runbook --title \"OAuth refresh\""))
	}
	domain, folder, slug := parts[0], parts[1], parts[2]
	if _, err := readV7DomainIndex(vaultPath, domain); err != nil {
		return err
	}
	kind := strings.ToLower(strings.TrimSpace(firstNonEmpty(args.String("kind"), v7KnowledgeKindForFolder(folder))))
	if _, ok := v7KnowledgeKinds[kind]; !ok {
		return tuskerError(errorInvalidField, "invalid V7 knowledge kind: "+kind)
	}
	expectedFolder := v7KnowledgeFolderForKind(kind)
	if folder != expectedFolder {
		return tuskerError(errorInvalidArg, fmt.Sprintf("V7 %s nodes must live under %s/", kind, expectedFolder), withContext(map[string]any{"kind": kind, "folder": folder, "expected_folder": expectedFolder}))
	}
	title := firstNonEmpty(args.String("title"), v7DefaultDomainTitle(slug))
	summary := firstNonEmpty(args.String("summary"), title+".")
	rel := filepath.ToSlash(filepath.Join("knowledge", "domains", domain, folder, slug+".md"))
	path := filepath.Join(vaultPath, filepath.FromSlash(rel))
	if fileExists(path) {
		return tuskerError(errorAlreadyExists, "V7 knowledge node already exists: "+node, withPath(rel))
	}
	if err := ensureV7DomainLayout(filepath.Join(vaultPath, "knowledge", "domains", domain)); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	sourceOfTruth := splitCSV(args.String("source-of-truth"))
	if len(sourceOfTruth) == 0 {
		sourceOfTruth = []string{filepath.ToSlash(filepath.Join("knowledge", "domains", domain, "CANON.md"))}
	}
	data := map[string]any{
		"schema":  "tusker.knowledge/v7",
		"kind":    kind,
		"id":      node,
		"project": v7ProjectID(vaultPath),
		"domain":  domain,
		"title":   title,
		"status":  "current",
		"summary": summary,
		"capsule": v7CapsuleOrdered(
			title+" "+kind+" for "+domain+".",
			"Use when this is the narrowest matching "+kind+" for the task.",
			"Skip when the domain INDEX or CANON already answers the question.",
		),
		"source_of_truth": sourceOfTruth,
		"related":         []string{domain + "/canon"},
		"created_at":      now,
		"updated_at":      now,
	}
	body := v7KnowledgeLeafBody(kind, title, summary, domain, sourceOfTruth)
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["knowledge"])
	if err != nil {
		return err
	}
	if err := writeText(path, content); err != nil {
		return err
	}
	if !args.Bool("quiet") {
		fmt.Printf("Created V7 %s knowledge node %s at %s\n", kind, node, rel)
	}
	return nil
}

func v7KnowledgeFolderForKind(kind string) string {
	switch kind {
	case "runbook":
		return "runbooks"
	case "decision":
		return "decisions"
	case "invariant":
		return "invariants"
	case "interface":
		return "interfaces"
	case "source":
		return "sources"
	case "glossary":
		return "glossary"
	default:
		return ""
	}
}

func v7KnowledgeKindForFolder(folder string) string {
	switch folder {
	case "runbooks":
		return "runbook"
	case "decisions":
		return "decision"
	case "invariants":
		return "invariant"
	case "interfaces":
		return "interface"
	case "sources":
		return "source"
	case "glossary":
		return "glossary"
	default:
		return ""
	}
}

func v7KnowledgeLeafBody(kind, title, summary, domain string, sourceOfTruth []string) string {
	sections := []string{
		"# " + title,
		"",
		"## Summary",
		"",
		summary,
		"",
		"## Read This When",
		"",
		"- This is the narrowest matching " + kind + " for `" + domain + "` work.",
		"",
		"## Do Not Read This When",
		"",
		"- The domain `INDEX.md` or `CANON.md` already answers the question.",
		"- You are looking for task proof, attempts, events, generated packets, or raw logs.",
		"",
		"## Source Truth",
		"",
		renderMarkdownBulletList(sourceOfTruth),
		"",
		"## Related",
		"",
		"- [[" + domain + "/CANON]]",
		"",
	}
	switch kind {
	case "runbook":
		sections = append(sections, "## Procedure", "", "1. State the precondition.", "2. Apply the smallest safe change.", "3. Run the validation named by the owning task.", "")
	case "interface":
		sections = append(sections, "## Contract", "", "- Caller responsibility: _to fill._", "- Callee responsibility: _to fill._", "- Failure mode: _to fill._", "")
	case "invariant":
		sections = append(sections, "## Rule", "", "- This invariant is not yet specified.", "", "## Enforcement", "", "- Name the validator, test, or review check that enforces it.", "")
	case "decision":
		sections = append(sections, "## Decision", "", "- Pending durable decision text.", "", "## Rationale", "", "- Capture the tradeoff that made this decision true.", "")
	case "source":
		sections = append(sections, "## Raw Attribution", "", "- Source: _link or file path._", "- Captured at: _timestamp or version._", "- Use: summarize into CANON.md before treating as durable truth.", "")
	case "glossary":
		sections = append(sections, "## Terms", "", "| Term | Meaning |", "|---|---|", "| _term_ | _meaning_ |", "")
	}
	return strings.Join(sections, "\n")
}

func writeDefaultV7ProjectSkillIfMissing(vaultPath string) error {
	path := filepath.Join(vaultPath, "SKILL.md")
	if fileExists(path) {
		return nil
	}
	return writeV7ProjectSkill(vaultPath, path)
}

func refreshV7ProjectSkill(vaultPath string) error {
	path := filepath.Join(vaultPath, "SKILL.md")
	if fileExists(path) && !hasV7ProjectSkill(vaultPath) && !hasV7KnowledgeDomains(vaultPath) {
		return nil
	}
	return writeV7ProjectSkill(vaultPath, path)
}

func writeV7ProjectSkill(vaultPath, path string) error {
	domains, err := listV7ProjectSkillDomains(vaultPath)
	if err != nil {
		return err
	}
	body := renderV7ProjectSkillBody(domains)
	now := time.Now().UTC().Format(time.RFC3339)
	createdAt := now
	capsule := any(capsuleScaffold())
	if existing, _, err := parseFrontmatterMustRead(path); err == nil && stringField(existing, "schema") == "tusker.project-skill/v7" {
		createdAt = fallback(stringField(existing, "created_at"), now)
		if existingCapsule, ok := existing["capsule"]; ok {
			capsule = existingCapsule
		}
	}
	data := map[string]any{
		"schema":          "tusker.project-skill/v7",
		"kind":            "project_skill",
		"name":            "project-knowledge",
		"project":         v7ProjectID(vaultPath),
		"status":          "current",
		"description":     "Route agents through this repository's V7 domain canon without publishing task proof or runtime state.",
		"capsule":         capsule,
		"operator_skill":  "tusker",
		"source_of_truth": []string{"knowledge/domains"},
		"canonical_files": []string{"SKILL.md", "knowledge/domains/*/INDEX.md", "knowledge/domains/*/CANON.md"},
		"created_at":      createdAt,
		"updated_at":      now,
	}
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["project_skill"])
	if err != nil {
		return err
	}
	return writeText(path, content)
}

func listV7ProjectSkillDomains(vaultPath string) ([]Note, error) {
	root := filepath.Join(vaultPath, "knowledge", "domains")
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var domains []Note
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		note, err := readV7DomainIndex(vaultPath, entry.Name())
		if err != nil {
			continue
		}
		domains = append(domains, note)
	}
	sort.Slice(domains, func(i, j int) bool {
		return stringField(domains[i].Data, "id") < stringField(domains[j].Data, "id")
	})
	return domains, nil
}

func renderV7ProjectSkillBody(domains []Note) string {
	var rows []string
	rows = append(rows, "| Domain | Read when | Read first | Canon |")
	rows = append(rows, "|---|---|---|---|")
	for _, domain := range domains {
		id := stringField(domain.Data, "id")
		rows = append(rows, fmt.Sprintf("| %s | %s | `knowledge/domains/%s/INDEX.md` | `knowledge/domains/%s/CANON.md` |", stringField(domain.Data, "title"), stringField(domain.Data, "summary"), id, id))
	}
	if len(rows) == 2 {
		rows = append(rows, "| No domains yet. | Create a V7 domain first. | Create a V7 domain first. | Create a V7 domain first. |")
	}
	return strings.Join([]string{
		"# Project Knowledge Skill",
		"",
		"This is the project knowledge skill for this repository. Use it after the Tusker operator skill when repository-specific canon is needed.",
		"",
		"## Read This When",
		"",
		"- You need durable repository-specific canon before implementing a task.",
		"- A task packet routes you to one or more project domains.",
		"- You are updating project knowledge after behavior, policy, or interfaces changed.",
		"",
		"## Do Not Read This When",
		"",
		"- You only need Tusker task lifecycle, proof, gates, closeout, or CLI semantics; use the Tusker operator skill.",
		"- You are looking for raw proof logs, task history, attempts, events, generated packets, or local runtime state.",
		"",
		"## First Action",
		"",
		"Task agents must run `tusker packet <TASK-ID> --for agent`, then read only the routed domains from that packet unless the task contract names a narrower file.",
		"",
		"## Routing Algorithm",
		"",
		"1. Read this `SKILL.md`.",
		"2. Use the task packet or intent to choose the narrowest matching domain.",
		"3. Read that domain `INDEX.md`.",
		"4. Read that domain `CANON.md`.",
		"5. Open deeper runbooks, decisions, interfaces, invariants, sources, or glossary entries only when the domain files route you there.",
		"",
		"## Domains",
		"",
		strings.Join(rows, "\n"),
		"",
		"## Repo Command Policy",
		"",
		"- Put repository-specific command rules here or in routed runbooks: validation commands, build-lock/status commands, token/noise wrappers, and forbidden expensive probes.",
		"- Keep root `AGENTS.md` and `CLAUDE.md` as managed Tusker bootstrap pointers; do not copy Tusker workflow mechanics there.",
		"- Agents should prefer path-scoped status/search, lock/status commands over process-table probes, redirected validation logs, and command + PASS/FAIL summaries.",
		"",
		"## Updating Canon",
		"",
		"- Update the narrowest owning domain `CANON.md` when durable truth changes.",
		"- Create or update a leaf node only when the canon needs a stable runbook, interface, invariant, decision, glossary entry, or source attribution.",
		"- Run `tusker validate --json` after changing project knowledge.",
		"- Do not put proof logs, task history, attempts, event streams, generated packets, or raw terminal output in canon.",
		"",
		"## Forbidden Source Truth",
		"",
		"- Do not publish task records, evidence logs, attempts, event files, generated output, runtime state, or raw logs as project skill source.",
		"- Forbidden paths include `work/**`, `epics/**`, `evidence/**`, `attempts/**`, `events/**`, `_generated/**`, `_system/**`, `dashboards/**`, packet caches, `.tusker-*`, raw logs, and local absolute paths.",
		"- Raw external input belongs in `knowledge/domains/<domain>/sources/`.",
		"- Root `docs/` may contain optional repository engineering guardrails; it is not the V7 canonical knowledge source.",
		"",
		"## Validation",
		"",
		"- `tusker skill doctor --strict --json` checks project skill routes and package hygiene.",
		"- `tusker validate --json` checks V7 domain layout and task-domain coverage.",
		"",
	}, "\n")
}
