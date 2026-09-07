package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"tusker/internal/docgraph"
)

type docsReadLink struct {
	Ref      string `json:"ref"`
	Subject  string `json:"subject,omitempty"`
	Path     string `json:"path,omitempty"`
	Resolved bool   `json:"resolved"`
}

type docsReadBacklink struct {
	Subject string `json:"subject"`
	Title   string `json:"title"`
	Path    string `json:"path"`
	Kind    string `json:"kind"`
	Via     string `json:"via"`
	Typed   bool   `json:"typed"`
}

type docsReadSuccessor struct {
	Subject string `json:"subject"`
	Path    string `json:"path"`
}

type docsReadResult struct {
	Schema             string             `json:"schema"`
	Subject            string             `json:"subject"`
	Title              string             `json:"title"`
	Path               string             `json:"path"`
	Kind               docgraph.Kind      `json:"kind"`
	Status             string             `json:"status"`
	Revision           string             `json:"revision"`
	ResolvedFrom       string             `json:"resolved_from,omitempty"`
	Header             map[string]any     `json:"header"`
	Body               string             `json:"body"`
	Section            string             `json:"section,omitempty"`
	Links              []docsReadLink     `json:"links"`
	Backlinks          []docsReadBacklink `json:"backlinks"`
	BacklinksTruncated bool               `json:"backlinks_truncated"`
	BacklinksOmitted   int                `json:"backlinks_omitted"`
	Successor          *docsReadSuccessor `json:"successor,omitempty"`
}

func docsReadCmd(args Args) error {
	ref := strings.TrimSpace(positionalPhrase(args))
	if ref == "" {
		return tuskerError(errorMissingArg, "docs read needs a subject or managed path, for example: tusker docs read orchestration")
	}
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	repoRoot := v7RepoRoot(vaultPath)
	corpus, resolver, doc, resolvedFrom, raw, err := docsResolveDocument(repoRoot, ref, args.Bool("current"))
	if err != nil {
		return docsDiscoveryError(err)
	}
	detail, err := docsBuildReadResult(corpus, resolver, doc, resolvedFrom, raw)
	if err != nil {
		return err
	}
	if section := strings.TrimSpace(args.String("section")); section != "" {
		selected, err := docgraph.ReadSection(doc.Body, section)
		if err != nil {
			return docsSectionError(err)
		}
		detail.Section = selected.Heading
		detail.Body = selected.Heading + "\n\n" + selected.Body
	}
	if args.Bool("json") {
		emitJSON(detail)
		return nil
	}
	fmt.Printf("%s\n%s [%s] status=%s\n", detail.Title, detail.Path, detail.Kind, detail.Status)
	if partOf := compactDocsReadValue(detail.Header["part_of"]); partOf != "" {
		fmt.Printf("Part of: %s\n", partOf)
	}
	if readWhen := compactDocsReadValue(detail.Header["read_when"]); readWhen != "" {
		fmt.Printf("Read when: %s\n", readWhen)
	}
	if skipWhen := compactDocsReadValue(detail.Header["skip_when"]); skipWhen != "" {
		fmt.Printf("Skip when: %s\n", skipWhen)
	}
	fmt.Printf("Relationships: %d links, %d backlinks\n\n", len(detail.Links), len(detail.Backlinks))
	if detail.ResolvedFrom != "" {
		fmt.Printf("Resolved forward from superseded %q.\n\n", detail.ResolvedFrom)
	}
	fmt.Print(strings.TrimSpace(detail.Body) + "\n")
	if detail.Successor != nil {
		fmt.Printf("\nSuccessor: %s (%s)\n", detail.Successor.Subject, detail.Successor.Path)
	}
	return nil
}

func docsBacklinksCmd(args Args) error {
	ref := strings.TrimSpace(positionalPhrase(args))
	if ref == "" {
		return tuskerError(errorMissingArg, "docs backlinks needs a subject or managed path, for example: tusker docs backlinks overview")
	}
	limit, err := docsDiscoveryLimit(args)
	if err != nil {
		return err
	}
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	corpus, resolver, doc, _, _, err := docsResolveDocument(v7RepoRoot(vaultPath), ref, false)
	if err != nil {
		return docsDiscoveryError(err)
	}
	backlinks := docsBacklinksFor(doc.Subject, corpus, resolver)
	truncated := len(backlinks) > limit
	omitted := 0
	if truncated {
		omitted = len(backlinks) - limit
		backlinks = backlinks[:limit]
	}
	_, allBroken := docgraph.SemanticLinks(corpus)
	if allBroken == nil {
		allBroken = []docgraph.BrokenLink{}
	}
	brokenTruncated := len(allBroken) > limit
	brokenOmitted := 0
	if brokenTruncated {
		brokenOmitted = len(allBroken) - limit
		allBroken = allBroken[:limit]
	}
	result := map[string]any{
		"schema": "tusker.docs-backlinks/v1", "subject": doc.Subject, "path": doc.Path,
		"backlinks": backlinks, "broken": allBroken, "truncated": truncated,
		"limit": limit, "omitted": omitted,
		"broken_truncated": brokenTruncated, "broken_omitted": brokenOmitted,
	}
	if args.Bool("json") {
		emitJSON(result)
		return nil
	}
	fmt.Printf("Backlinks to %s (%s)\n", doc.Subject, doc.Path)
	for _, backlink := range backlinks {
		fmt.Printf("  %s — %s [%s] via %s\n", backlink.Subject, backlink.Path, backlink.Kind, backlink.Via)
	}
	if len(allBroken) > 0 {
		fmt.Println("Broken managed references:")
		for _, link := range allBroken {
			fmt.Printf("  %s — %s (%s)\n", link.Path, link.Ref, "DOC_LINK_DANGLING")
		}
	}
	if truncated {
		fmt.Printf("Showing %d backlinks; %d omitted.\n", len(backlinks), omitted)
	}
	if brokenTruncated {
		fmt.Printf("Showing %d broken references; %d omitted.\n", len(allBroken), brokenOmitted)
	}
	return nil
}

func docsResolveDocument(repoRoot, ref string, current bool) (docgraph.Corpus, *docgraph.Resolver, docgraph.Document, string, []byte, error) {
	corpus, _, err := docgraph.LoadRepository(repoRoot)
	if err != nil {
		return docgraph.Corpus{}, nil, docgraph.Document{}, "", nil, err
	}
	resolver := docgraph.NewResolver(corpus)
	var resolution docgraph.Resolution
	var ok bool
	pathRef := strings.Contains(ref, "/") || strings.HasSuffix(strings.ToLower(ref), ".md")
	if pathRef {
		clean, pathErr := docgraph.NormalizeManagedPath(ref)
		if pathErr != nil {
			return docgraph.Corpus{}, nil, docgraph.Document{}, "", nil, pathErr
		}
		if current {
			resolution, ok = resolver.ResolveCurrent(clean)
		} else {
			resolution, ok = resolver.Resolve(clean)
		}
		if !ok {
			content, readErr := docgraph.ReadDocumentFile(repoRoot, clean)
			if readErr != nil {
				var pathErr *docgraph.PathError
				if errors.As(readErr, &pathErr) {
					return docgraph.Corpus{}, nil, docgraph.Document{}, "", nil, pathErr
				}
			}
			if readErr == nil {
				if _, parseErr := docgraph.ParseDocHeaders(clean, content); parseErr != nil {
					return docgraph.Corpus{}, nil, docgraph.Document{}, "", nil, browseParseErrorForCommand(clean, parseErr)
				}
			}
		}
	} else if current {
		resolution, ok = resolver.ResolveCurrent(ref)
	} else {
		resolution, ok = resolver.Resolve(ref)
	}
	if !ok {
		return docgraph.Corpus{}, nil, docgraph.Document{}, "", nil, &docgraph.PathError{Code: "DOC_NOT_FOUND", Path: ref, Message: fmt.Sprintf("managed document %q was not found", ref)}
	}
	raw, err := docgraph.ReadDocumentFile(repoRoot, resolution.Document.Path)
	if err != nil {
		return docgraph.Corpus{}, nil, docgraph.Document{}, "", nil, err
	}
	resolvedFrom := resolution.ResolvedFrom
	return corpus, resolver, resolution.Document, resolvedFrom, raw, nil
}

func docsBuildReadResult(corpus docgraph.Corpus, resolver *docgraph.Resolver, doc docgraph.Document, resolvedFrom string, raw []byte) (docsReadResult, error) {
	sum := sha256.Sum256(raw)
	backlinks := docsBacklinksFor(doc.Subject, corpus, resolver)
	backlinksOmitted := 0
	backlinksTruncated := len(backlinks) > docgraph.MaxBrowseLimit
	if backlinksTruncated {
		backlinksOmitted = len(backlinks) - docgraph.MaxBrowseLimit
		backlinks = backlinks[:docgraph.MaxBrowseLimit]
	}
	result := docsReadResult{
		Schema: "tusker.docs-read/v1", Subject: doc.Subject, Title: docgraph.DocumentTitle(doc),
		Path: doc.Path, Kind: doc.Kind, Status: doc.Status, Revision: hex.EncodeToString(sum[:]),
		ResolvedFrom: resolvedFrom, Header: doc.Raw, Body: doc.Body,
		Links: docsReadLinks(doc, resolver), Backlinks: backlinks,
		BacklinksTruncated: backlinksTruncated, BacklinksOmitted: backlinksOmitted,
	}
	if successor := strings.TrimSpace(doc.SupersededBy); successor != "" && strings.EqualFold(strings.TrimSpace(doc.Status), "superseded") {
		if resolved, ok := resolver.ResolveFrom(doc.Path, successor); ok {
			result.Successor = &docsReadSuccessor{Subject: resolved.Document.Subject, Path: resolved.Document.Path}
		}
	}
	return result, nil
}

func compactDocsReadValue(value any) string {
	if value == nil {
		return ""
	}
	return strings.Join(strings.Fields(fmt.Sprint(value)), " ")
}

func docsReadLinks(doc docgraph.Document, resolver *docgraph.Resolver) []docsReadLink {
	refs := docgraph.ExtractReferences(doc.Body)
	result := make([]docsReadLink, 0, len(refs))
	for _, ref := range refs {
		link := docsReadLink{Ref: ref}
		if resolved, ok := resolver.ResolveFrom(doc.Path, ref); ok {
			link.Subject, link.Path, link.Resolved = resolved.Document.Subject, resolved.Document.Path, true
		}
		result = append(result, link)
	}
	return result
}

func docsBacklinksFor(subject string, corpus docgraph.Corpus, resolver *docgraph.Resolver) []docsReadBacklink {
	result := make([]docsReadBacklink, 0)
	for _, link := range resolver.Backlinks(subject, corpus) {
		source, ok := resolver.Resolve(link.From)
		if !ok || source.Document.Subject == subject {
			continue
		}
		result = append(result, docsReadBacklink{
			Subject: source.Document.Subject, Title: docgraph.DocumentTitle(source.Document),
			Path: source.Document.Path, Kind: string(source.Document.Kind), Via: link.Kind,
			Typed: link.Kind != "link",
		})
	}
	return result
}

func docsDiscoveryLimit(args Args) (int, error) {
	limit := docgraph.DefaultBrowseLimit
	if raw := strings.TrimSpace(args.String("limit")); raw != "" {
		limit = atoiSafe(raw)
		if limit <= 0 {
			return 0, tuskerError(errorInvalidArg, "--limit must be a positive integer")
		}
	}
	if limit > docgraph.MaxBrowseLimit {
		limit = docgraph.MaxBrowseLimit
	}
	return limit, nil
}

func docsSectionError(err error) error {
	var missing *docgraph.SectionNotFoundError
	if errors.As(err, &missing) {
		return tuskerError("DOC_SECTION_NOT_FOUND", missing.Error(), withHint("use `tusker docs read <subject> --json` to inspect the document headings"), withContext(map[string]any{"available_headings": missing.Available}))
	}
	return err
}

func browseParseErrorForCommand(path string, err error) error {
	code := "DOC_HEADER_PARSE_ERROR"
	if parsed, ok := err.(*docgraph.ParseError); ok {
		code = parsed.Code
	}
	return &docgraph.PathError{Code: code, Path: path, Message: path + ": " + err.Error()}
}

func docsDiscoveryError(err error) error {
	var pathErr *docgraph.PathError
	if errors.As(err, &pathErr) {
		hint := "run `tusker docs find <query>` to locate a managed document"
		if pathErr.Code == "DOC_HEADER_PARSE_ERROR" || pathErr.Code == "DOC_HEADER_MISSING" || pathErr.Code == "DOC_HEADER_TYPE_INVALID" {
			hint = "run `tusker docs check " + pathErr.Path + "` to inspect the document header"
		}
		return tuskerError(pathErr.Code, pathErr.Message, withPath(pathErr.Path), withHint(hint))
	}
	return err
}
