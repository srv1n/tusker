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
		return tuskerError(errorInvalidArg, fmt.Sprintf(`invalid domain id "%s": %s`, id, err))
	}
	if strings.Contains(id, "/") {
		return tuskerError(errorInvalidArg, "domain id must be one portable path segment")
	}
	status := strings.ToLower(fallback(args.String("status"), "current"))
	if _, ok := v7DomainStatus[status]; !ok {
		return tuskerError(errorInvalidField, "invalid domain status: "+status)
	}
	title := fallback(args.String("title"), v7DefaultDomainTitle(id))
	summary := fallback(args.String("summary"), "Durable source of truth for "+title+".")
	domainDir := filepath.Join(vaultPath, "knowledge", "domains", id)
	indexPath := filepath.Join(domainDir, "INDEX.md")
	canonPath := filepath.Join(domainDir, "CANON.md")
	if fileExists(indexPath) || fileExists(canonPath) {
		return tuskerError(errorAlreadyExists, "domain already exists: "+id, withPath(domainDir))
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
	// S46: route the same domain into the portable tree. The portable
	// 00-index is the human-readable authority; the legacy INDEX/CANON
	// records above stay as compatibility pointers. Never overwrite an
	// existing portable index.
	if err := ensureV7PortableDomainIndex(vaultPath, id, title, summary); err != nil {
		return err
	}
	if err := refreshV7ProjectSkill(vaultPath); err != nil {
		return err
	}
	if !args.Bool("quiet") {
		fmt.Printf("Created domain %s at %s\n", id, domainDir)
	}
	return emitV7Event(vaultPath, id, "domain", "created", actor, map[string]any{"path": filepath.ToSlash(filepath.Join("knowledge", "domains", id, "INDEX.md"))})
}

// v7PortableDomainIndexRel is the stable portable route for one domain's
// human-readable index. Product domain knowledge lives under docs/system;
// the legacy knowledge/domains records remain as compatibility pointers.
func v7PortableDomainIndexRel(id string) string {
	return filepath.ToSlash(filepath.Join("docs", "system", "domains", id, "00-index.md"))
}

func v7PortableDomainAbs(repoRoot, id string) string {
	return filepath.Join(repoRoot, "docs", "system", "domains", id, "00-index.md")
}

// v7HasPortableDomain reports whether the portable domain index exists. It is
// a pure existence check so readers stay fail-closed on parse errors instead
// of silently falling back to a stale legacy copy.
func v7HasPortableDomain(repoRoot, id string) bool {
	return fileExists(v7PortableDomainAbs(repoRoot, id))
}

// v7PortableDomainIDs lists every domain with a portable index, ordered for
// deterministic routing and skill tables.
func v7PortableDomainIDs(repoRoot string) []string {
	entries, err := os.ReadDir(filepath.Join(repoRoot, "docs", "system", "domains"))
	if err != nil {
		return nil
	}
	var ids []string
	for _, entry := range entries {
		if !entry.IsDir() || !v7HasPortableDomain(repoRoot, entry.Name()) {
			continue
		}
		if err := validateKnowledgeNodePath(entry.Name()); err != "" || strings.Contains(entry.Name(), "/") {
			continue
		}
		ids = append(ids, entry.Name())
	}
	sort.Strings(ids)
	return ids
}

// v7PortableDomainNote reads the actual portable domain document and maps its
// metadata onto the domain route shape agents already consume. The returned
// note carries the live portable body: callers must never persist it back as
// a legacy INDEX/CANON copy.
func v7PortableDomainNote(repoRoot, id string) (Note, error) {
	rel := v7PortableDomainIndexRel(id)
	abs := v7PortableDomainAbs(repoRoot, id)
	data, body, err := parseFrontmatterMustRead(abs)
	if err != nil {
		return Note{}, err
	}
	title := firstNonEmpty(strings.TrimSpace(stringField(data, "title")), strings.TrimSpace(stringField(data, "subject")))
	if title == "" {
		title = v7FirstMarkdownHeading(body)
	}
	title = fallback(title, v7DefaultDomainTitle(id))
	summary := firstNonEmpty(strings.TrimSpace(stringField(data, "summary")), strings.TrimSpace(stringField(data, "read_when")))
	summary = fallback(summary, "Durable source of truth for "+title+".")
	capsule := map[string]any{"what": summary}
	if useWhen := strings.TrimSpace(stringField(data, "read_when")); useWhen != "" {
		capsule["use_when"] = []string{useWhen}
	}
	if skipWhen := strings.TrimSpace(stringField(data, "skip_when")); skipWhen != "" {
		capsule["skip_when"] = []string{skipWhen}
	}
	return Note{
		AbsolutePath: abs,
		RelativePath: rel,
		Data: map[string]any{
			"schema":          "tusker.domain/v7",
			"kind":            "domain",
			"id":              id,
			"project":         v7ProjectID(filepath.Join(repoRoot, defaultRepoVaultDir)),
			"title":           title,
			"status":          fallback(strings.TrimSpace(stringField(data, "status")), "current"),
			"summary":         summary,
			"capsule":         capsule,
			"source_of_truth": []string{rel},
			"canonical_files": []string{"00-index.md"},
		},
		Body: body,
	}, nil
}

// v7FirstMarkdownHeading returns the first "# Heading" line without its
// markers, or "" when the body names no heading.

func v7FirstMarkdownHeading(body string) string {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			if heading := strings.TrimSpace(strings.TrimLeft(trimmed, "#")); heading != "" {
				return heading
			}
		}
	}
	return ""
}

// ensureV7PortableDomainIndex scaffolds the portable domain index for a new
// domain without overwriting an existing one. Legacy-only repositories (no
// portable overview) keep their current behavior untouched.
func ensureV7PortableDomainIndex(vaultPath, id, title, summary string) error {
	repoRoot := v7RepoRoot(vaultPath)
	if !fileExists(filepath.Join(repoRoot, "docs", "system", "00-overview.md")) {
		return nil
	}
	abs := v7PortableDomainAbs(repoRoot, id)
	if fileExists(abs) {
		return nil
	}
	if err := ensureDir(filepath.Dir(abs)); err != nil {
		return err
	}
	return writeText(abs, v7PortableDomainIndexScaffold(id, title, summary))
}

// v7PortableDomainIndexScaffold renders a docgraph-valid domain index. Kind,
// lifecycle and conformance start at their truthful defaults: kind doc,
// status current, and code conformance not_applicable for a routing index.
func v7PortableDomainIndexScaffold(id, title, summary string) string {
	return fmt.Sprintf(`---
kind: doc
subject: %s
keywords: [domain]
part_of: overview
status: current
code_conformance: not_applicable
read_when: "You need durable %s canon before implementing a task."
skip_when: "You only need task proof, runtime events, or generated packets."
---

# %s

## Purpose

%s

## Reading order

- Start here, then open focused chapters in docs/system/domains/%s/ as they are added.

## Chapters

- _No focused chapters yet._
`, id, id, title, summary, id)
}

func readV7DomainIndex(vaultPath, id string) (Note, error) {
	id = strings.TrimSpace(id)
	if err := validateKnowledgeNodePath(id); err != "" || strings.Contains(id, "/") {
		return Note{}, tuskerError(errorInvalidArg, "invalid domain id: "+id)
	}
	if repoRoot := v7RepoRoot(vaultPath); v7HasPortableDomain(repoRoot, id) {
		return v7PortableDomainNote(repoRoot, id)
	}
	path := filepath.Join(vaultPath, "knowledge", "domains", id, "INDEX.md")
	if !fileExists(path) {
		return Note{}, tuskerError(errorNotFound, "domain not found: "+id, withPath(path))
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
		return Note{}, tuskerError(errorInvalidArg, "invalid domain id: "+id)
	}
	if repoRoot := v7RepoRoot(vaultPath); v7HasPortableDomain(repoRoot, id) {
		// The portable index is the canon route. Synthesize the canon view
		// from the actual portable document so readers never receive a
		// copied legacy snapshot.
		index, err := v7PortableDomainNote(repoRoot, id)
		if err != nil {
			return Note{}, err
		}
		index.Data["kind"] = "domain_canon"
		index.Data["id"] = id + "/canon"
		index.Data["domain"] = id
		index.Data["title"] = fallback(stringField(index.Data, "title"), v7DefaultDomainTitle(id)) + " Canon"
		return index, nil
	}
	path := filepath.Join(vaultPath, "knowledge", "domains", id, "CANON.md")
	if !fileExists(path) {
		return Note{}, tuskerError(errorNotFound, "domain canon not found: "+id, withPath(path))
	}
	data, body, err := parseFrontmatterMustRead(path)
	if err != nil {
		return Note{}, err
	}
	return Note{AbsolutePath: path, RelativePath: filepath.ToSlash(filepath.Join("knowledge", "domains", id, "CANON.md")), Data: data, Body: body}, nil
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

- Raw external input belongs in sources/. Do not treat root docs/ or site output as domain canon.

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
		// Git drops empty directories; keep the layout in every clone.
		keep := filepath.Join(domainDir, relative, ".gitkeep")
		if !fileExists(keep) {
			if err := writeText(keep, ""); err != nil {
				return err
			}
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
		"description":     "Route agents through this repository's domain canon without publishing task proof or runtime state.",
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
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	seen := map[string]bool{}
	var domains []Note
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		seen[entry.Name()] = true
		note, err := readV7DomainIndex(vaultPath, entry.Name())
		if err != nil {
			continue
		}
		domains = append(domains, note)
	}
	// Portable-only domains have no legacy record. They still own durable
	// canon, so they join the same route set through model compatibility.
	repoRoot := v7RepoRoot(vaultPath)
	for _, id := range v7PortableDomainIDs(repoRoot) {
		if seen[id] {
			continue
		}
		note, err := v7PortableDomainNote(repoRoot, id)
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
		indexRef := fmt.Sprintf("knowledge/domains/%s/INDEX.md", id)
		canonRef := fmt.Sprintf("knowledge/domains/%s/CANON.md", id)
		if strings.HasPrefix(filepath.ToSlash(domain.RelativePath), "docs/system/domains/") {
			// Portable route: the 00-index is both the reading entry and
			// the canon route. Legacy knowledge/domains rows below remain
			// as compatibility pointers only.
			indexRef = domain.RelativePath
			canonRef = domain.RelativePath
		}
		rows = append(rows, fmt.Sprintf("| %s | %s | `%s` | `%s` |", stringField(domain.Data, "title"), stringField(domain.Data, "summary"), indexRef, canonRef))
	}
	if len(rows) == 2 {
		rows = append(rows, "| No domains yet. | Create a domain first. | Create a domain first. | Create a domain first. |")
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
		"Task agents must run `tusker show <TASK-ID> --packet agent`, then read only the routed domains from that packet unless the task contract names a narrower file.",
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
		"- Project canon lives in `docs/system/` (chapters, `proposals/`, `decisions/`, and `domains/<domain>/`). Update the narrowest owning doc when durable truth changes.",
		"- Find and open canon with `tusker list docs --grep <query>` and `tusker show <subject>`; create new docs, proposals, or decisions with `tusker new doc` instead of hand-writing files.",
		"- Run `tusker check --only docs` after editing canon.",
		"- Legacy `knowledge/domains/<domain>/CANON.md` records remain compatibility pointers; run `tusker check --only validate --json` after changing them.",
		"- Do not put proof logs, task history, attempts, event streams, generated packets, or raw terminal output in canon.",
		"",
		"## Forbidden Source Truth",
		"",
		"- Do not publish task records, evidence logs, attempts, event files, generated output, runtime state, or raw logs as project skill source.",
		"- Forbidden paths include `work/**`, `epics/**`, `evidence/**`, `attempts/**`, `events/**`, `_generated/**`, `_system/**`, `dashboards/**`, packet caches, `.tusker-*`, raw logs, and local absolute paths.",
		"- Raw external input belongs in `knowledge/domains/<domain>/sources/`.",
		"- `docs/system/` is the project canon (chapters, `proposals/`, `decisions/`); other files under root `docs/` are repository guides.",
		"",
		"## Validation",
		"",
		"- `tusker check --only skills --strict --json` checks project skill routes and package hygiene.",
		"- `tusker check --only docs` checks canon front matter, links, and placement under `docs/system/`.",
		"- `tusker check --only validate --json` checks domain layout and task-domain coverage.",
		"",
	}, "\n")
}
