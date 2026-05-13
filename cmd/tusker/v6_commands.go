package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	v6PublishExportStateRelative   = "src/generated/v6-export-state.json"
	v6PublishRoutesRemovedRelative = "src/generated/v6-routes-removed.json"
)

func domainListCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	index, err := loadV6KnowledgeMap(vaultPath)
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "domains": index.Domains})
		return nil
	}
	for _, domain := range index.Domains {
		fmt.Printf("- %s — %s\n", domain.ID, domain.Title)
		if domain.Summary != "" {
			fmt.Printf("  %s\n", domain.Summary)
		}
	}
	return nil
}

func domainShowCmd(args Args) error {
	args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	id, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	index, err := loadV6KnowledgeMap(vaultPath)
	if err != nil {
		return err
	}
	domain, ok := v6FindDomain(index.Domains, id)
	if !ok {
		return tuskerError(errorNotFound, "V6 domain not found: "+id)
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "domain": domain})
		return nil
	}
	if args.Bool("full") {
		content, err := readText(filepath.Join(vaultPath, filepath.FromSlash(domain.Path)))
		if err != nil {
			return err
		}
		fmt.Print(content)
		if !strings.HasSuffix(content, "\n") {
			fmt.Println()
		}
		return nil
	}
	fmt.Print(renderV6DomainCapsule(domain, index))
	return nil
}

func domainCanonCmd(args Args) error {
	args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	id, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	return knowledgeShowCmd(Args{"vault": vaultPath, "node": id + "/canon", "capsule": firstNonEmpty(args.String("capsule"), "true"), "full": args.String("full"), "json": args.String("json")})
}

func domainNewCmd(args Args) error {
	args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	id, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	if err := validateKnowledgeNodePath(id); err != "" {
		return tuskerError(errorInvalidArg, fmt.Sprintf(`invalid domain id "%s": %s`, id, err))
	}
	date := todayISO()
	if err := writeV6DomainStarter(vaultPath, id, date); err != nil {
		return err
	}
	indexPath := filepath.Join(vaultPath, "domains", id, "INDEX.md")
	if title := strings.TrimSpace(args.String("title")); title != "" {
		data, body, err := parseFrontmatterMustRead(indexPath)
		if err != nil {
			return err
		}
		data["title"] = title
		if summary := strings.TrimSpace(args.String("summary")); summary != "" {
			data["summary"] = summary
		}
		content, err := serializeDocument(data, body, v6FrontmatterOrder["domain"])
		if err != nil {
			return err
		}
		if err := writeText(indexPath, content); err != nil {
			return err
		}
	}
	if err := writeV6GeneratedIndexes(vaultPath, true); err != nil {
		return err
	}
	if !args.Bool("quiet") {
		fmt.Printf("Created V6 domain %s\n", id)
	}
	return nil
}

func domainGraphCmd(args Args) error {
	args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
	return graphCmd(args)
}

func knowledgeMapCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	index, err := loadV6KnowledgeMap(vaultPath)
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "knowledge_map": map[string]any{
			"schema":          "tusker.knowledge-map/v6",
			"generated_at":    index.GeneratedAt,
			"domains":         index.Domains,
			"knowledge_nodes": index.KnowledgeNodes,
			"tasks":           index.Tasks,
			"epics":           index.Epics,
		}})
		return nil
	}
	fmt.Printf("Knowledge map: %s\n\n", filepath.Join(vaultPath, v6KnowledgeMapRelative))
	fmt.Println("Domains:")
	for _, domain := range index.Domains {
		fmt.Printf("- %s — %s\n", domain.ID, domain.Title)
	}
	fmt.Println("\nKnowledge nodes:")
	for _, node := range index.KnowledgeNodes {
		fmt.Printf("- %s — %s\n", node.Node, node.Title)
		fmt.Printf("  %s · %s · %s · %s\n", node.Path, node.Domain, node.Kind, node.CanonicalStatus)
	}
	return nil
}

func knowledgeListCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	index, err := loadV6KnowledgeMap(vaultPath)
	if err != nil {
		return err
	}
	domainFilter := strings.TrimSpace(args.String("domain"))
	var nodes []v6KnowledgeRecord
	for _, node := range index.KnowledgeNodes {
		if domainFilter != "" && node.Domain != domainFilter {
			continue
		}
		nodes = append(nodes, node)
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "nodes": nodes})
		return nil
	}
	for _, node := range nodes {
		fmt.Printf("- %s — %s (%s/%s, %s)\n", node.Node, node.Title, node.Domain, node.Kind, node.Freshness)
	}
	return nil
}

func knowledgeShowCmd(args Args) error {
	args["node"] = firstNonEmpty(args.String("node"), args.String("_pos0"))
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	nodeID, err := requireArg(args, "node")
	if err != nil {
		return err
	}
	index, err := loadV6KnowledgeMap(vaultPath)
	if err != nil {
		return err
	}
	node, ok := v6FindKnowledge(index.KnowledgeNodes, nodeID)
	if !ok {
		return tuskerError(errorNotFound, "V6 knowledge node not found: "+nodeID)
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "node": node})
		return nil
	}
	content, err := readText(filepath.Join(vaultPath, filepath.FromSlash(node.Path)))
	if err != nil {
		return err
	}
	if section := strings.TrimSpace(args.String("section")); section != "" {
		_, body, err := parseFrontmatter(content)
		if err != nil {
			return err
		}
		if !strings.HasPrefix(section, "#") {
			section = "## " + section
		}
		fmt.Printf("%s  knowledge  %s\n\n%s\n", node.Node, node.Title, sectionContent(body, section))
		return nil
	}
	if args.Bool("full") {
		fmt.Print(content)
		if !strings.HasSuffix(content, "\n") {
			fmt.Println()
		}
		return nil
	}
	fmt.Print(renderV6KnowledgeCapsule(node))
	return nil
}

func knowledgeRouteCmd(args Args) error {
	query := firstNonEmpty(args.String("query"), args.String("_pos0"))
	if strings.TrimSpace(query) == "" {
		return tuskerError(errorMissingArg, "knowledge route requires an intent query")
	}
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	index, err := loadV6KnowledgeMap(vaultPath)
	if err != nil {
		return err
	}
	matches := scoreV6KnowledgeRoute(query, index.KnowledgeNodes, args.Bool("include-historical"))
	limit := atoiSafe(args.String("limit"))
	if limit <= 0 {
		limit = 5
	}
	if len(matches) > limit {
		matches = matches[:limit]
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "query": query, "matches": matches})
		return nil
	}
	if len(matches) == 0 {
		fmt.Println("No knowledge routes matched.")
		return nil
	}
	fmt.Println("Best matches:")
	for i, match := range matches {
		fmt.Printf("%d. %s\n", i+1, match.Node)
		fmt.Printf("   why: %s\n", strings.Join(match.Reasons, ", "))
		fmt.Printf("   read: tusker knowledge show %s --capsule\n", match.Node)
	}
	return nil
}

type v6RouteMatch struct {
	Node    string   `json:"node"`
	Title   string   `json:"title"`
	Score   int      `json:"score"`
	Reasons []string `json:"reasons"`
}

func scoreV6KnowledgeRoute(query string, nodes []v6KnowledgeRecord, includeHistorical bool) []v6RouteMatch {
	normalized := strings.ToLower(strings.TrimSpace(query))
	terms := routeTerms(normalized)
	var matches []v6RouteMatch
	for _, node := range nodes {
		if _, historical := v6HistoricalStatuses[node.CanonicalStatus]; historical && !includeHistorical {
			continue
		}
		score := 0
		var reasons []string
		if strings.EqualFold(normalized, node.Node) {
			score += 100
			reasons = append(reasons, "exact node")
		}
		for _, alias := range node.Aliases {
			if alias != "" && strings.Contains(normalized, strings.ToLower(alias)) {
				score += 80
				reasons = append(reasons, `alias "`+alias+`"`)
			}
		}
		if title := strings.ToLower(node.Title); title != "" && strings.Contains(normalized, title) {
			score += 60
			reasons = append(reasons, "title phrase")
		}
		if node.Domain != "" && strings.Contains(normalized, strings.ToLower(node.Domain)) {
			score += 40
			reasons = append(reasons, "domain "+node.Domain)
		}
		score += routeTermScore(terms, node.ReadWhen, 30, &reasons, "read_when")
		score += routeTermScore(terms, node.Summary, 20, &reasons, "summary")
		for _, path := range node.SourceOfTruth {
			if strings.Contains(normalized, strings.ToLower(path)) {
				score += 10
				reasons = append(reasons, "source path")
			}
		}
		if routeTextMatches(terms, node.DoNotReadWhen) {
			score -= 100
			reasons = append(reasons, "do_not_read_when penalty")
		}
		if _, historical := v6HistoricalStatuses[node.CanonicalStatus]; historical {
			score -= 50
			reasons = append(reasons, "historical penalty")
		}
		if score > 0 {
			matches = append(matches, v6RouteMatch{Node: node.Node, Title: node.Title, Score: score, Reasons: uniqueStrings(reasons)})
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Score != matches[j].Score {
			return matches[i].Score > matches[j].Score
		}
		return matches[i].Node < matches[j].Node
	})
	return matches
}

func routeTerms(value string) []string {
	stop := makeSet("a", "an", "and", "are", "for", "how", "i", "is", "of", "or", "the", "to", "what", "when", "with")
	var out []string
	for _, term := range strings.FieldsFunc(value, func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	}) {
		if len(term) < 3 {
			continue
		}
		if _, ok := stop[term]; ok {
			continue
		}
		out = append(out, term)
	}
	return uniqueStrings(out)
}

func routeTermScore(terms []string, text string, weight int, reasons *[]string, label string) int {
	if text == "" {
		return 0
	}
	lower := strings.ToLower(text)
	score := 0
	for _, term := range terms {
		if strings.Contains(lower, term) {
			score += weight
			*reasons = append(*reasons, label+" "+term)
		}
	}
	return score
}

func routeTextMatches(terms []string, text string) bool {
	if text == "" {
		return false
	}
	lower := strings.ToLower(text)
	for _, term := range terms {
		if strings.Contains(lower, term) {
			return true
		}
	}
	return false
}

func knowledgeFreshnessCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	index, err := v6IndexVault(vaultPath)
	if err != nil {
		return err
	}
	items := index.Freshness
	if args.Bool("stale") {
		var filtered []v6FreshnessRecord
		for _, item := range items {
			if item.State == "stale" || item.State == "missing" {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "items": items})
		return nil
	}
	fmt.Printf("Knowledge freshness: %s\n", filepath.Join(vaultPath, v6FreshnessRelative))
	if len(items) == 0 {
		fmt.Println("No knowledge nodes matched the freshness filter.")
		return nil
	}
	for _, item := range items {
		fmt.Printf("- %s — %s\n", item.Node, item.State)
		if len(item.Missing) > 0 {
			fmt.Printf("  missing: %s\n", strings.Join(item.Missing, ", "))
		}
	}
	return nil
}

func knowledgeCheckCmd(args Args) error {
	args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	id, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	note, err := resolveNote(vaultPath, id)
	if err != nil {
		return err
	}
	nodes := normalizeList(note.Data["knowledge_nodes"])
	for _, row := range parseKnowledgeDeltaRows(note.Body) {
		for _, node := range row.DocNodes {
			if !containsString(nodes, node) {
				nodes = append(nodes, node)
			}
		}
	}
	index, err := v6IndexVault(vaultPath)
	if err != nil {
		return err
	}
	freshnessByNode := map[string]v6FreshnessRecord{}
	known := map[string]bool{}
	for _, item := range index.Freshness {
		freshnessByNode[item.Node] = item
		known[item.Node] = true
	}
	var rows []map[string]any
	for _, node := range nodes {
		status := "needs_review"
		if !known[node] {
			status = "unknown_node"
		} else if freshnessByNode[node].State == "current" {
			status = "current"
		}
		rows = append(rows, map[string]any{"node": node, "status": status, "freshness": freshnessByNode[node]})
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "id": id, "knowledge_nodes": rows})
		return nil
	}
	if len(rows) == 0 {
		fmt.Printf("%s has no knowledge_nodes; knowledge impact is not required.\n", id)
		return nil
	}
	fmt.Printf("Knowledge impact for %s:\n", id)
	for _, row := range rows {
		fmt.Printf("- %s (%s)\n", row["node"], row["status"])
	}
	fmt.Println("Resolve each node with `tusker knowledge apply <id> --node <node>` or `tusker knowledge waive <id> <node> --reason ...`.")
	return nil
}

func knowledgeApplyCmd(args Args) error {
	return recordKnowledgeResolution(args, "applied")
}

func knowledgeNoopCmd(args Args) error {
	return recordKnowledgeResolution(args, "verified_noop")
}

func knowledgeWaiveCmd(args Args) error {
	return recordKnowledgeResolution(args, "waived")
}

func recordKnowledgeResolution(args Args, status string) error {
	args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
	args["node"] = firstNonEmpty(args.String("node"), args.String("_pos1"))
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	id, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	node, err := requireArg(args, "node")
	if err != nil {
		return err
	}
	note, err := resolveNote(vaultPath, id)
	if err != nil {
		return err
	}
	if effectiveNoteKind(note.Data) != "task" {
		return tuskerError(errorInvalidArg, "knowledge resolution applies to tasks")
	}
	allowedNodes := normalizeList(note.Data["knowledge_nodes"])
	for _, row := range parseKnowledgeDeltaRows(note.Body) {
		for _, target := range row.DocNodes {
			if !containsString(allowedNodes, target) {
				allowedNodes = append(allowedNodes, target)
			}
		}
	}
	if !containsString(allowedNodes, node) {
		return tuskerError(errorInvalidArg, fmt.Sprintf(`task %s does not target knowledge node "%s"`, id, node))
	}
	reason := strings.TrimSpace(args.String("reason"))
	if status == "waived" && reason == "" {
		return tuskerError(errorMissingArg, "knowledge waiver requires --reason", withContext(map[string]any{"id": id, "node": node}))
	}
	data, body, err := parseFrontmatterMustRead(note.AbsolutePath)
	if err != nil {
		return err
	}
	index, err := v6IndexVault(vaultPath)
	if err != nil {
		return err
	}
	fingerprint := ""
	for _, item := range index.Freshness {
		if item.Node == node {
			fingerprint = item.Fingerprint
			break
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	date := todayISO()
	actor := fallback(fallback(args.String("actor"), args.String("by")), "automation")
	resolutions := anySlice(data["knowledge_resolution"])
	var next []any
	for _, item := range resolutions {
		row, ok := item.(map[string]any)
		if ok && stringValue(row["node"]) == node {
			continue
		}
		next = append(next, item)
	}
	next = append(next, map[string]any{
		"node":               node,
		"status":             status,
		"actor":              actor,
		"at":                 now,
		"reason":             reason,
		"source_fingerprint": fingerprint,
	})
	data["knowledge_resolution"] = next
	if isV6Schema(data) {
		data["updated_at"] = date
	} else {
		data["updated"] = date
	}
	body = appendWorkLogBullet(body, fmt.Sprintf("%s — %s — knowledge %s for %s%s", date, actor, status, node, suffixReason(reason)))
	content, err := serializeDocument(data, body, frontmatterOrderForType("task"))
	if isV6Schema(data) {
		content, err = serializeDocument(data, body, v6FrontmatterOrder["task"])
	}
	if err != nil {
		return err
	}
	if err := writeText(note.AbsolutePath, content); err != nil {
		return err
	}
	autoReindex(vaultPath)
	if !args.Bool("quiet") {
		fmt.Printf("%s knowledge %s for %s\n", id, status, node)
	}
	return nil
}

func knowledgeNewCmd(args Args) error {
	args["node"] = firstNonEmpty(args.String("node"), args.String("_pos0"))
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	node, err := requireArg(args, "node")
	if err != nil {
		return err
	}
	if reason := validateKnowledgeNodePath(node); reason != "" {
		return tuskerError(errorInvalidArg, fmt.Sprintf(`invalid knowledge node "%s": %s`, node, reason))
	}
	parts := strings.Split(node, "/")
	if len(parts) < 2 {
		return tuskerError(errorInvalidArg, "knowledge node must include domain and leaf path")
	}
	domain := parts[0]
	title := firstNonEmpty(args.String("title"), strings.Title(strings.ReplaceAll(parts[len(parts)-1], "-", " ")))
	kind := firstNonEmpty(args.String("kind"), "reference")
	audience := firstNonEmpty(args.String("audience"), "developer")
	agentLayer := firstNonEmpty(args.String("agent-layer"), args.String("agent_layer"), "capsule")
	source := splitCSV(args.String("source"))
	if len(source) == 0 {
		source = []string{"tusker/SKILL.md"}
	}
	rel := filepath.ToSlash(filepath.Join("domains", domain, filepath.Join(parts[1:]...)+".md"))
	if strings.HasSuffix(node, "/canon") {
		rel = filepath.ToSlash(filepath.Join("domains", domain, "CANON.md"))
	}
	path := filepath.Join(vaultPath, filepath.FromSlash(rel))
	if fileExists(path) {
		return tuskerError(errorAlreadyExists, "knowledge node already exists: "+node, withPath(rel))
	}
	date := todayISO()
	data := map[string]any{
		"schema":           "tusker.knowledge/v6",
		"node":             node,
		"title":            title,
		"domain":           domain,
		"kind":             kind,
		"audience":         audience,
		"agent_layer":      agentLayer,
		"canonical_status": "draft",
		"summary":          firstNonEmpty(args.String("summary"), title+"."),
		"aliases":          splitCSV(args.String("aliases")),
		"source_of_truth":  source,
		"stale_when":       map[string]any{"paths": source},
		"related_nodes":    []string{},
		"related_epics":    []string{},
		"publish":          map[string]any{"lane": "internal", "path": node, "include_in_llms": true},
		"created_at":       date,
		"updated_at":       date,
	}
	body := `# ` + title + `

## Read this when

Read this when ` + strings.ToLower(title) + ` is the narrowest matching knowledge node.

## Do not read this when

Do not read this for unrelated domains or task proof history.

## Source of truth

` + renderMarkdownBulletList(source) + `

## Related

- [[` + domain + `/CANON]]

## Recent changes

` + v6BackrefsBlockBegin + `
_Run ` + "`tusker reindex`" + ` to refresh recent task proof._
` + v6BackrefsBlockEnd + `
`
	content, err := serializeDocument(data, body, v6FrontmatterOrder["knowledge"])
	if err != nil {
		return err
	}
	if err := writeText(path, content); err != nil {
		return err
	}
	if err := writeV6GeneratedIndexes(vaultPath, true); err != nil {
		return err
	}
	if !args.Bool("quiet") {
		fmt.Printf("Created V6 knowledge node %s at %s\n", node, rel)
	}
	return nil
}

func graphCmd(args Args) error {
	args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	id, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	index, err := v6IndexVault(vaultPath)
	if err != nil {
		return err
	}
	depth := atoiSafe(args.String("depth"))
	if depth <= 0 {
		depth = 1
	}
	nodes, edges := v6GraphNeighborhood(index.Graph, id, depth)
	if len(nodes) == 0 {
		return tuskerError(errorNotFound, "graph node not found: "+id)
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "seed": id, "depth": depth, "nodes": nodes, "edges": edges})
		return nil
	}
	fmt.Printf("Graph for %s depth %d:\n", id, depth)
	for _, node := range nodes {
		fmt.Printf("- %s (%s) %s\n", node.ID, node.Kind, node.Title)
	}
	if len(edges) > 0 {
		fmt.Println("Edges:")
		for _, edge := range edges {
			fmt.Printf("- %s -[%s]-> %s\n", edge.From, edge.Relation, edge.To)
		}
	}
	return nil
}

func publishExportCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	siteRoot, err := docsResolveSiteRoot(args)
	if err != nil {
		return err
	}
	index, err := v6IndexVault(vaultPath)
	if err != nil {
		return err
	}
	summary, err := runV6PublishExport(vaultPath, siteRoot, index)
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "summary": summary})
		return nil
	}
	fmt.Printf("Published %d V6 knowledge page%s and %d LLM lane%s.\n", intValue(summary["pages"]), plural(intValue(summary["pages"])), intValue(summary["llm_lanes"]), plural(intValue(summary["llm_lanes"])))
	return nil
}

func publishBuildCmd(args Args) error {
	if err := publishExportCmd(Args{"vault": args.String("vault"), "site": args.String("site"), "quiet": "true"}); err != nil {
		return err
	}
	siteRoot, err := docsResolveSiteRoot(args)
	if err != nil {
		return err
	}
	if args.Bool("quiet") || args.Bool("json") {
		if err := runAstroCommandQuiet(siteRoot, "build"); err != nil {
			return err
		}
	} else if err := runAstroCommand(siteRoot, "build"); err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "built": true})
	}
	return nil
}

func publishDevCmd(args Args) error {
	if err := publishExportCmd(Args{"vault": args.String("vault"), "site": args.String("site"), "quiet": "true"}); err != nil {
		return err
	}
	siteRoot, err := docsResolveSiteRoot(args)
	if err != nil {
		return err
	}
	devArgs := []string{"dev"}
	if host := strings.TrimSpace(args.String("host")); host != "" {
		devArgs = append(devArgs, "--host", host)
	}
	if port := strings.TrimSpace(args.String("port")); port != "" {
		devArgs = append(devArgs, "--port", port)
	}
	return runAstroCommand(siteRoot, devArgs...)
}

func publishLLMSCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	siteRoot, err := docsResolveSiteRoot(args)
	if err != nil {
		return err
	}
	index, err := v6IndexVault(vaultPath)
	if err != nil {
		return err
	}
	if err := writeV6LLMSFiles(vaultPath, siteRoot, index); err != nil {
		return err
	}
	if !args.Bool("quiet") {
		fmt.Printf("Wrote V6 LLM lanes under %s\n", filepath.Join(siteRoot, "public"))
	}
	return nil
}

func publishSkillCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	out := firstNonEmpty(args.String("out"), filepath.Join(mustGetwd(), "dist", "project-skill"))
	if strings.HasSuffix(out, ".zip") {
		out = strings.TrimSuffix(out, ".zip")
	}
	if err := ensureDir(out); err != nil {
		return err
	}
	for _, rel := range []string{"SKILL.md", "domains"} {
		src := filepath.Join(vaultPath, rel)
		dst := filepath.Join(out, rel)
		if info, err := os.Stat(src); err == nil && info.IsDir() {
			if err := copyTreeForV5Backup(src, dst); err != nil {
				return err
			}
		} else if err == nil {
			if err := copyFile(src, dst); err != nil {
				return err
			}
		}
	}
	if !args.Bool("quiet") {
		fmt.Printf("Wrote project skill package directory at %s\n", out)
	}
	return nil
}

func runV6PublishExport(vaultPath, siteRoot string, index v6KnowledgeIndex) (map[string]any, error) {
	if err := ensureDir(filepath.Join(siteRoot, docsContentRootRelative)); err != nil {
		return nil, err
	}
	if err := ensureDir(filepath.Join(siteRoot, docsGeneratedRootRelative)); err != nil {
		return nil, err
	}
	knownTargets := v6PublishLinkTargets(index)
	if err := validateV6PublishLinks(vaultPath, index, knownTargets); err != nil {
		return nil, err
	}
	generatedAt := time.Now().UTC().Format(time.RFC3339)
	previousState, _ := loadV6PublishExportState(siteRoot)
	newState := docsExportState{GeneratedAt: generatedAt}
	pages := 0
	routeByNode := map[string]string{}
	for _, entry := range index.Publication {
		url := docsRouteURL(filepath.ToSlash(filepath.Join(entry.Lane, entry.Route)))
		addV6PublishRoute(routeByNode, entry.Node, url)
		if strings.HasSuffix(entry.Node, "/canon") {
			domain := strings.TrimSuffix(entry.Node, "/canon")
			addV6PublishRoute(routeByNode, domain, url)
			addV6PublishRoute(routeByNode, domain+"/INDEX", url)
			addV6PublishRoute(routeByNode, domain+"/CANON", url)
		}
		if node, ok := v6FindKnowledge(index.KnowledgeNodes, entry.Node); ok {
			for _, alias := range node.Aliases {
				addV6PublishRoute(routeByNode, alias, url)
			}
		}
	}
	for _, entry := range index.Publication {
		node, ok := v6FindKnowledge(index.KnowledgeNodes, entry.Node)
		if !ok {
			continue
		}
		body, err := v6PublishedBody(vaultPath, node, routeByNode, knownTargets)
		if err != nil {
			return nil, err
		}
		outRel := filepath.ToSlash(filepath.Join(docsContentRootRelative, entry.Lane, entry.Route+".md"))
		if strings.HasSuffix(outRel, "/index.md") {
			outRel = strings.TrimSuffix(outRel, "/index.md") + ".md"
		}
		if err := writeText(filepath.Join(siteRoot, filepath.FromSlash(outRel)), body); err != nil {
			return nil, err
		}
		newState.ContentFiles = append(newState.ContentFiles, docsNormalizePath(outRel))
		newState.Routes = append(newState.Routes, docsExportStateRoute{
			Title:      entry.Title,
			SourceKind: "v6_knowledge",
			SourceID:   entry.Node,
			SourcePath: entry.Path,
			Route:      docsNormalizeRouteValue(filepath.ToSlash(filepath.Join(entry.Lane, entry.Route))),
			RouteURL:   docsRouteURL(filepath.ToSlash(filepath.Join(entry.Lane, entry.Route))),
			OutputPath: docsNormalizePath(outRel),
		})
		pages++
	}
	deletedPages := 0
	for _, stale := range docsComputeStaleFiles(previousState.ContentFiles, newState.ContentFiles) {
		staleAbs := filepath.Join(siteRoot, filepath.FromSlash(stale))
		if v6FileLooksGenerated(staleAbs) {
			if err := os.Remove(staleAbs); err == nil {
				deletedPages++
			}
		}
	}
	if err := writeV6LLMSFiles(vaultPath, siteRoot, index); err != nil {
		return nil, err
	}
	if err := writeJSON(filepath.Join(siteRoot, docsGeneratedRootRelative, "v6-publication.index.json"), map[string]any{"schema": "tusker.publication/v6", "generated_at": generatedAt, "items": index.Publication}); err != nil {
		return nil, err
	}
	if err := writeJSON(filepath.Join(siteRoot, v6PublishRoutesRemovedRelative), buildDocsRemovedRoutesReport(generatedAt, previousState, newState, nil)); err != nil {
		return nil, err
	}
	if err := writeJSON(filepath.Join(siteRoot, v6PublishExportStateRelative), newState); err != nil {
		return nil, err
	}
	return map[string]any{"pages": pages, "deleted_pages": deletedPages, "llm_lanes": 4}, nil
}

func v6PublishLinkTargets(index v6KnowledgeIndex) map[string]bool {
	targets := map[string]bool{}
	for _, domain := range index.Domains {
		addV6ValidationLinkTarget(targets, domain.ID)
		addV6ValidationLinkTarget(targets, domain.ID+"/INDEX")
		addV6ValidationLinkTarget(targets, domain.ID+"/CANON")
	}
	for _, node := range index.KnowledgeNodes {
		addV6ValidationLinkTarget(targets, node.Node)
		addV6ValidationLinkTarget(targets, node.Node+".md")
		for _, alias := range node.Aliases {
			addV6ValidationLinkTarget(targets, alias)
		}
	}
	for _, doc := range index.Documents {
		if id := stringField(doc.Frontmatter, "id"); id != "" {
			addV6ValidationLinkTarget(targets, id)
		}
	}
	return targets
}

func validateV6PublishLinks(vaultPath string, index v6KnowledgeIndex, targets map[string]bool) error {
	ctx := validationContext{VaultPath: vaultPath, V6LinkTargets: targets}
	for _, entry := range index.Publication {
		node, ok := v6FindKnowledge(index.KnowledgeNodes, entry.Node)
		if !ok {
			continue
		}
		note := Note{
			AbsolutePath: filepath.Join(vaultPath, filepath.FromSlash(node.Path)),
			RelativePath: node.Path,
		}
		if issues := validateV6DocumentLinks(note, ctx, node.Path); len(issues) > 0 {
			return tuskerError(errorUnknownDocNode, "publish export cannot resolve V6 links in "+node.Path+": "+issues[0].Message, withContext(map[string]any{"issues": issues}))
		}
	}
	return nil
}

func addV6PublishRoute(routeByNode map[string]string, target, url string) {
	normalized := v6NormalizeLinkTarget(target)
	if normalized == "" || url == "" {
		return
	}
	routeByNode[normalized] = url
	routeByNode[strings.ToLower(normalized)] = url
}

func loadV6PublishExportState(siteRoot string) (docsExportState, error) {
	statePath := filepath.Join(siteRoot, v6PublishExportStateRelative)
	if !fileExists(statePath) {
		return docsExportState{}, nil
	}
	raw, err := os.ReadFile(statePath)
	if err != nil {
		return docsExportState{}, err
	}
	var state docsExportState
	if err := json.Unmarshal(raw, &state); err != nil {
		return docsExportState{}, err
	}
	state.ContentFiles = docsUniqueStrings(state.ContentFiles)
	state.Routes = docsUniqueStateRoutes(state.Routes)
	return state, nil
}

func v6FileLooksGenerated(filePath string) bool {
	text, err := readText(filePath)
	if err != nil {
		return false
	}
	return strings.Contains(text, "Generated by tusker publish export")
}

func v6PublishedBody(vaultPath string, node v6KnowledgeRecord, routeByNode map[string]string, knownTargets map[string]bool) (string, error) {
	content, err := readText(filepath.Join(vaultPath, filepath.FromSlash(node.Path)))
	if err != nil {
		return "", err
	}
	_, body, err := parseFrontmatter(content)
	if err != nil {
		return "", err
	}
	body, err = v6RewriteWikiLinksForPublish(body, routeByNode, knownTargets)
	if err != nil {
		return "", err
	}
	header := fmt.Sprintf(`---
title: "%s"
description: "%s"
---

`, node.Title, strings.ReplaceAll(firstNonEmpty(node.Summary, node.Title), `"`, `'`))
	return header + "<!-- Generated by tusker publish export. Do not edit directly. -->\n\n" + body, nil
}

func v6RewriteWikiLinksForPublish(body string, routeByNode map[string]string, knownTargets map[string]bool) (string, error) {
	var unresolved []string
	return regexpReplaceWikiLinks(body, func(target, label string) string {
		normalized := v6NormalizeLinkTarget(target)
		text := firstNonEmpty(label, filepath.Base(normalized))
		if normalized == "" {
			return text
		}
		if href := routeByNode[normalized]; href != "" {
			return "[" + text + "](" + href + ")"
		}
		if href := routeByNode[strings.ToLower(normalized)]; href != "" {
			return "[" + text + "](" + href + ")"
		}
		if !knownTargets[normalized] && !knownTargets[strings.ToLower(normalized)] {
			unresolved = append(unresolved, target)
		}
		return text
	}), nilIfEmptyUnresolvedPublishLinks(unresolved)
}

func nilIfEmptyUnresolvedPublishLinks(unresolved []string) error {
	unresolved = uniqueStrings(unresolved)
	if len(unresolved) == 0 {
		return nil
	}
	return tuskerError(errorUnknownDocNode, "publish export cannot rewrite V6 wikilinks: "+strings.Join(unresolved, ", "), withHint("publish linked knowledge nodes or remove the wikilinks"))
}

func regexpReplaceWikiLinks(body string, replace func(target, label string) string) string {
	pattern := regexp.MustCompile(`\[\[([^\]]+)\]\]`)
	return pattern.ReplaceAllStringFunc(body, func(match string) string {
		parts := pattern.FindStringSubmatch(match)
		if len(parts) < 2 {
			return match
		}
		inner := strings.TrimSpace(parts[1])
		label := ""
		if split := strings.SplitN(inner, "|", 2); len(split) == 2 {
			inner = strings.TrimSpace(split[0])
			label = strings.TrimSpace(split[1])
		}
		if split := strings.SplitN(inner, "#", 2); len(split) == 2 {
			inner = strings.TrimSpace(split[0])
		}
		return replace(inner, label)
	})
}

func writeV6LLMSFiles(vaultPath, siteRoot string, index v6KnowledgeIndex) error {
	if err := ensureDir(filepath.Join(siteRoot, "public")); err != nil {
		return err
	}
	lanes := map[string]string{
		"llms.txt":            renderV6LLMS(vaultPath, index, "default"),
		"llms-full.txt":       renderV6LLMS(vaultPath, index, "full"),
		"llms-internal.txt":   renderV6LLMS(vaultPath, index, "internal"),
		"llms-historical.txt": renderV6LLMS(vaultPath, index, "historical"),
	}
	for name, content := range lanes {
		if err := writeText(filepath.Join(siteRoot, "public", name), content); err != nil {
			return err
		}
	}
	return nil
}

func renderV6LLMS(vaultPath string, index v6KnowledgeIndex, lane string) string {
	var b strings.Builder
	b.WriteString("# Tusker V6 knowledge graph\n\n")
	for _, node := range index.KnowledgeNodes {
		if !v6NodeIncludedInLane(node, lane) {
			continue
		}
		b.WriteString("## " + node.Title + "\n")
		b.WriteString("- node: `" + node.Node + "`\n")
		b.WriteString("- domain: `" + node.Domain + "`\n")
		b.WriteString("- status: `" + node.CanonicalStatus + "`\n")
		if node.Summary != "" {
			b.WriteString("- summary: " + node.Summary + "\n")
		}
		if lane == "full" || lane == "internal" || lane == "historical" {
			if content, err := readText(filepath.Join(vaultPath, filepath.FromSlash(node.Path))); err == nil {
				_, body, _ := parseFrontmatter(content)
				b.WriteString("\n" + strings.TrimSpace(body) + "\n")
			}
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

func v6NodeIncludedInLane(node v6KnowledgeRecord, lane string) bool {
	if node.Kind == "asset" {
		return false
	}
	if _, historical := v6HistoricalStatuses[node.CanonicalStatus]; lane == "historical" {
		return historical
	} else if historical {
		return false
	}
	switch lane {
	case "default", "full":
		if node.Audience == "internal" {
			return false
		}
		if node.Publish != nil {
			if raw, ok := node.Publish["include_in_llms"]; ok && !boolValue(raw) {
				return false
			}
		}
		return node.CanonicalStatus == "approved" || node.CanonicalStatus == "draft"
	case "internal":
		return node.Audience == "internal" || firstNonEmpty(stringValue(node.Publish["lane"]), "") == "internal" || node.AgentLayer == "standalone"
	default:
		return false
	}
}

func v6GraphNeighborhood(graph v6GraphIndex, seed string, depth int) ([]v6GraphNode, []v6GraphEdge) {
	nodeByID := map[string]v6GraphNode{}
	for _, node := range graph.Nodes {
		nodeByID[node.ID] = node
	}
	if _, ok := nodeByID[seed]; !ok {
		return nil, nil
	}
	seen := map[string]int{seed: 0}
	queue := []string{seed}
	var edges []v6GraphEdge
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		currentDepth := seen[current]
		if currentDepth >= depth {
			continue
		}
		for _, edge := range graph.Edges {
			next := ""
			if edge.From == current {
				next = edge.To
			} else if edge.To == current {
				next = edge.From
			}
			if next == "" {
				continue
			}
			edges = append(edges, edge)
			if _, ok := seen[next]; !ok {
				seen[next] = currentDepth + 1
				queue = append(queue, next)
			}
		}
	}
	var nodes []v6GraphNode
	for id := range seen {
		nodes = append(nodes, nodeByID[id])
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		return edges[i].To < edges[j].To
	})
	return nodes, edges
}

func renderV6DomainCapsule(domain v6DomainRecord, index v6KnowledgeIndex) string {
	var lines []string
	lines = append(lines, domain.Title)
	lines = append(lines, "Domain: "+domain.ID)
	if domain.ReadWhen != "" {
		lines = append(lines, "\nRead this when:\n"+domain.ReadWhen)
	}
	if domain.DoNotReadWhen != "" {
		lines = append(lines, "\nDo not read this when:\n"+domain.DoNotReadWhen)
	}
	lines = append(lines, "\nCurrent canon: `tusker knowledge show "+domain.ID+"/canon --capsule`")
	if len(domain.KnowledgeNodes) > 0 {
		lines = append(lines, "\nTop knowledge nodes:")
		for _, node := range domain.KnowledgeNodes {
			lines = append(lines, "- "+node)
		}
	}
	stale := 0
	for _, item := range index.Freshness {
		if strings.HasPrefix(item.Node, domain.ID+"/") && item.State != "current" && item.State != "historical" {
			stale++
		}
	}
	lines = append(lines, fmt.Sprintf("\nStale nodes: %d", stale))
	return strings.Join(lines, "\n") + "\n"
}

func renderV6KnowledgeCapsule(node v6KnowledgeRecord) string {
	var lines []string
	lines = append(lines, node.Title)
	lines = append(lines, "Node: "+node.Node)
	lines = append(lines, "Domain/kind/audience/layer: "+node.Domain+"/"+node.Kind+"/"+node.Audience+"/"+node.AgentLayer)
	lines = append(lines, "Canonical status: "+node.CanonicalStatus)
	if node.ReadWhen != "" {
		lines = append(lines, "\nRead this when:\n"+node.ReadWhen)
	}
	if node.DoNotReadWhen != "" {
		lines = append(lines, "\nDo not read this when:\n"+node.DoNotReadWhen)
	}
	if node.Summary != "" {
		lines = append(lines, "\nSummary: "+node.Summary)
	}
	if len(node.SourceOfTruth) > 0 {
		lines = append(lines, "\nSource of truth:")
		for _, source := range node.SourceOfTruth {
			lines = append(lines, "- "+source)
		}
	}
	lines = append(lines, "\nFreshness: "+fallback(node.Freshness, "unknown"))
	if len(node.RelatedNodes) > 0 {
		lines = append(lines, "\nRelated nodes:")
		for _, related := range node.RelatedNodes {
			lines = append(lines, "- "+related)
		}
	}
	if len(node.RecentTasks) > 0 {
		lines = append(lines, "\nRecent proof tasks:")
		for _, task := range node.RecentTasks {
			lines = append(lines, "- "+task)
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

func v6FindDomain(domains []v6DomainRecord, id string) (v6DomainRecord, bool) {
	for _, domain := range domains {
		if domain.ID == id {
			return domain, true
		}
	}
	return v6DomainRecord{}, false
}

func v6FindKnowledge(nodes []v6KnowledgeRecord, nodeID string) (v6KnowledgeRecord, bool) {
	for _, node := range nodes {
		if node.Node == nodeID {
			return node, true
		}
		for _, alias := range node.Aliases {
			if strings.EqualFold(alias, nodeID) {
				return node, true
			}
		}
	}
	return v6KnowledgeRecord{}, false
}

func validateKnowledgeNodePath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "must not be empty"
	}
	if strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") {
		return "must not start or end with /"
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return `must not contain empty, ".", or ".." segments`
		}
		if strings.ContainsAny(segment, `\:`) {
			return "must use portable path segments"
		}
	}
	return ""
}

func renderMarkdownBulletList(values []string) string {
	if len(values) == 0 {
		return "- _None declared._"
	}
	var lines []string
	for _, value := range values {
		lines = append(lines, "- `"+value+"`")
	}
	return strings.Join(lines, "\n")
}

func ensureSectionBeforeHeading(body, heading, beforeHeading string) string {
	if findHeading(body, heading) != nil {
		return body
	}
	lines := strings.Split(body, "\n")
	if before := findHeading(body, beforeHeading); before != nil {
		insert := []string{heading, ""}
		lines = append(lines[:before.Index], append(insert, lines[before.Index:]...)...)
		return strings.Join(lines, "\n")
	}
	body = strings.TrimRight(body, "\n")
	if body == "" {
		return heading + "\n"
	}
	return body + "\n\n" + heading + "\n"
}

func verifyV6TaskCmd(vaultPath string, note Note, args Args) error {
	id := stringField(note.Data, "id")
	if effectiveNoteKind(note.Data) != "task" {
		return tuskerError(errorInvalidArg, fmt.Sprintf(`verify only supports tasks, got schema "%s"`, stringField(note.Data, "schema")), withContext(map[string]any{"id": id}))
	}
	if stringField(note.Data, "status") != "review" {
		return tuskerError(errorInvalidTransition, fmt.Sprintf(`%s: verify requires status "review"`, id), withContext(map[string]any{"id": id, "status": stringField(note.Data, "status")}))
	}
	by := fallback(args.String("by"), "automation")
	data, body, err := parseFrontmatterMustRead(note.AbsolutePath)
	if err != nil {
		return err
	}
	if err := ensureReviewerMayVerify(vaultPath, data, by); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	date := todayISO()
	data["verified_by"] = by
	data["verified_at"] = now
	if summary := strings.TrimSpace(args.String("summary")); summary != "" {
		data["verification_summary"] = summary
	}
	data["updated_at"] = date
	body = ensureSectionBeforeHeading(body, "## Verification log", "## Evidence")
	body = appendSectionBullet(body, "## Verification log", fmt.Sprintf("- %s — %s — verified%s", date, by, suffixReason(args.String("summary"))), false)
	content, err := serializeDocument(data, body, v6FrontmatterOrder["task"])
	if err != nil {
		return err
	}
	if err := writeText(note.AbsolutePath, content); err != nil {
		return err
	}
	autoReindex(vaultPath)
	if !args.Bool("quiet") {
		fmt.Printf("%s verified by %s\n", id, by)
	}
	return nil
}

func closeV6TaskCmd(vaultPath string, note Note, args Args) error {
	id := stringField(note.Data, "id")
	if effectiveNoteKind(note.Data) != "task" {
		return tuskerError(errorInvalidArg, fmt.Sprintf(`close only supports tasks, got schema "%s"`, stringField(note.Data, "schema")), withContext(map[string]any{"id": id}))
	}
	data, body, err := parseFrontmatterMustRead(note.AbsolutePath)
	if err != nil {
		return err
	}
	if stringField(data, "status") != "review" {
		return tuskerError(errorInvalidTransition, fmt.Sprintf(`%s: close requires status "review"`, id), withHint("run `tusker status "+id+" review` before verification and close"))
	}
	if stringField(data, "verified_at") == "" {
		return tuskerError(errorInvalidTransition, id+": close requires verification first", withHint("run `tusker verify "+id+" --by <name>`"))
	}
	if issues := v6TaskProofIssues(data, body); len(issues) > 0 {
		return tuskerError(errorEvidenceGate, id+": proof is incomplete: "+strings.Join(issues, "; "), withHint("fill Acceptance, Evidence, and Verification log before close"))
	}
	if len(normalizeList(data["knowledge_nodes"])) > 0 {
		index, err := v6IndexVault(vaultPath)
		if err != nil {
			return err
		}
		if issues := knowledgeImpactFreshnessIssues(data, v6FreshnessByNode(index)); len(issues) > 0 {
			return tuskerError(errorDocsImpactUnresolved, id+": knowledge impact is stale or unresolved: "+strings.Join(issues, "; "), withHint("run `tusker knowledge check "+id+"`, then apply, noop, or waive each node with current sources"))
		}
	}
	date := todayISO()
	now := time.Now().UTC().Format(time.RFC3339)
	actor := fallback(fallback(args.String("actor"), args.String("by")), "automation")
	if err := ensureReviewerMayClose(vaultPath, data, actor); err != nil {
		return err
	}
	reviewedBy := firstNonEmpty(stringField(data, "verified_by"), "unknown")
	data["status"] = "done"
	data["closed_by"] = actor
	data["closed_at"] = now
	if reason := strings.TrimSpace(args.String("reason")); reason != "" {
		data["close_summary"] = reason
	}
	data["updated_at"] = date
	body = appendWorkLogBullet(body, fmt.Sprintf("%s — %s — closed after review by %s%s", date, actor, reviewedBy, suffixReason(args.String("reason"))))
	content, err := serializeDocument(data, body, v6FrontmatterOrder["task"])
	if err != nil {
		return err
	}
	if err := writeText(note.AbsolutePath, content); err != nil {
		return err
	}
	autoReindex(vaultPath)
	if !args.Bool("quiet") {
		fmt.Printf("%s closed (reviewed by %s, closed by %s)\n", id, reviewedBy, actor)
	}
	return nil
}
