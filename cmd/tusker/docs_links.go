package main

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	docsWikiLinkPattern     = regexp.MustCompile(`\[\[([^\]]+)\]\]`)
	docsMarkdownLinkPattern = regexp.MustCompile(`(!?)\[([^\]]*?)\]\(([^)]+)\)`)
	docsHTMLAssetPattern    = regexp.MustCompile(`(?i)\b(src|poster)=["']([^"']+)["']`)
)

func rewriteDocsBody(body string, ctx docsRewriteContext) (string, error) {
	return docsRewriteOutsideCodeFences(body, func(chunk string) (string, error) {
		replaced, err := rewriteDocsWikiLinks(chunk, ctx)
		if err != nil {
			return "", err
		}
		replaced, err = rewriteDocsMarkdownLinks(replaced, ctx)
		if err != nil {
			return "", err
		}
		replaced, err = rewriteDocsHTMLAssets(replaced, ctx)
		if err != nil {
			return "", err
		}
		return replaced, nil
	})
}

func docsRewriteOutsideCodeFences(body string, fn func(string) (string, error)) (string, error) {
	lines := strings.Split(body, "\n")
	var out strings.Builder
	var chunk []string
	inFence := false
	flushChunk := func() error {
		if len(chunk) == 0 {
			return nil
		}
		text := strings.Join(chunk, "\n")
		rewritten, err := fn(text)
		if err != nil {
			return err
		}
		if out.Len() > 0 {
			out.WriteByte('\n')
		}
		out.WriteString(rewritten)
		chunk = chunk[:0]
		return nil
	}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			if !inFence {
				if err := flushChunk(); err != nil {
					return "", err
				}
				if out.Len() > 0 {
					out.WriteByte('\n')
				}
				out.WriteString(line)
				inFence = true
				continue
			}
			out.WriteByte('\n')
			out.WriteString(line)
			inFence = false
			continue
		}
		if inFence {
			out.WriteByte('\n')
			out.WriteString(line)
			continue
		}
		chunk = append(chunk, line)
	}
	if err := flushChunk(); err != nil {
		return "", err
	}
	return out.String(), nil
}

func rewriteDocsWikiLinks(body string, ctx docsRewriteContext) (string, error) {
	var rewriteErr error
	replaced := docsWikiLinkPattern.ReplaceAllStringFunc(body, func(match string) string {
		if rewriteErr != nil {
			return match
		}
		inner := strings.TrimSuffix(strings.TrimPrefix(match, "[["), "]]")
		label := ""
		if idx := strings.Index(inner, "|"); idx >= 0 {
			label = strings.TrimSpace(inner[idx+1:])
			inner = strings.TrimSpace(inner[:idx])
		}
		target, anchor := docsSplitWikiTarget(inner)
		resolved := docsResolveWikiTarget(ctx.RouteTable, target, anchor, label)
		switch resolved.Kind {
		case "link":
			return "[" + resolved.Text + "](" + resolved.Href + ")"
		case "text":
			return resolved.Text
		default:
			rewriteErr = tuskerError(errorNotFound, "Unresolved wikilink: "+match, withPath(ctx.Source.SourcePath))
			return match
		}
	})
	return replaced, rewriteErr
}

func docsResolveWikiTarget(table docsRouteTable, target, anchor, label string) docsResolvedLink {
	target = strings.TrimSpace(target)
	label = strings.TrimSpace(label)
	if route, ok := table.AliasToRoute[target]; ok {
		source := table.ByRoute[route]
		linkText := label
		if linkText == "" && source != nil {
			linkText = source.Title
		}
		if linkText == "" {
			linkText = target
		}
		href := docsRouteURL(route)
		if anchor != "" {
			href += "#" + docsHeadingAnchor(anchor)
		}
		return docsResolvedLink{Kind: "link", Href: href, Text: linkText}
	}
	if label != "" {
		return docsResolvedLink{Kind: "text", Text: label}
	}
	if anchor != "" {
		return docsResolvedLink{Kind: "text", Text: fmt.Sprintf("%s (%s)", target, anchor)}
	}
	return docsResolvedLink{Kind: "text", Text: target}
}

func rewriteDocsMarkdownLinks(body string, ctx docsRewriteContext) (string, error) {
	var rewriteErr error
	replaced := docsMarkdownLinkPattern.ReplaceAllStringFunc(body, func(match string) string {
		if rewriteErr != nil {
			return match
		}
		parts := docsMarkdownLinkPattern.FindStringSubmatch(match)
		if len(parts) < 4 {
			return match
		}
		isImage := parts[1] == "!"
		label := parts[2]
		rawTarget := parts[3]
		resolved, err := docsResolveMarkdownTarget(ctx, rawTarget, isImage, label)
		if err != nil {
			rewriteErr = err
			return match
		}
		switch resolved.Kind {
		case "keep":
			return match
		case "link":
			prefix := ""
			if isImage {
				prefix = "!"
			}
			return prefix + "[" + label + "](" + resolved.Href + ")"
		case "text":
			if isImage {
				return match
			}
			return resolved.Text
		default:
			return match
		}
	})
	return replaced, rewriteErr
}

func docsResolveMarkdownTarget(ctx docsRewriteContext, rawTarget string, isAsset bool, label string) (docsResolvedLink, error) {
	href, suffix, anchor := docsSplitHref(rawTarget)
	if href == "" || docsIsExternalHref(href) || strings.HasPrefix(href, "#") || strings.HasPrefix(href, "/") {
		return docsResolvedLink{Kind: "keep"}, nil
	}
	if isAsset || !docsLooksLikeMarkdown(href) {
		assetHref, err := ctx.Assets.RewriteAssetPath(ctx.Source, rawTarget)
		if err != nil {
			return docsResolvedLink{}, err
		}
		return docsResolvedLink{Kind: "link", Href: assetHref}, nil
	}
	candidates := docsResolveCandidatePaths(ctx.Source, ctx.RepoRoot, href)
	for _, candidate := range candidates {
		if routed := ctx.RouteTable.BySource[candidate]; routed != nil {
			return docsResolvedLink{Kind: "link", Href: docsRebuildHref(routed.RouteURL, suffix, anchor)}, nil
		}
	}
	for _, candidate := range candidates {
		if fileExists(candidate) {
			return docsResolvedLink{Kind: "text", Text: label}, nil
		}
	}
	return docsResolvedLink{}, tuskerError(errorNotFound, "Unresolved markdown link: "+href, withPath(ctx.Source.SourcePath))
}

func rewriteDocsHTMLAssets(body string, ctx docsRewriteContext) (string, error) {
	var rewriteErr error
	replaced := docsHTMLAssetPattern.ReplaceAllStringFunc(body, func(match string) string {
		if rewriteErr != nil {
			return match
		}
		parts := docsHTMLAssetPattern.FindStringSubmatch(match)
		if len(parts) < 3 {
			return match
		}
		attr := parts[1]
		raw := parts[2]
		rewritten, err := ctx.Assets.RewriteAssetPath(ctx.Source, raw)
		if err != nil {
			rewriteErr = err
			return match
		}
		return attr + "=\"" + rewritten + "\""
	})
	return replaced, rewriteErr
}

func docsSplitWikiTarget(value string) (string, string) {
	target := strings.TrimSpace(value)
	if idx := strings.Index(target, "#"); idx >= 0 {
		return strings.TrimSpace(target[:idx]), strings.TrimSpace(target[idx+1:])
	}
	return target, ""
}

func docsSplitHref(value string) (string, string, string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", "", ""
	}
	suffix := ""
	href := value
	if strings.HasPrefix(href, "<") && strings.HasSuffix(href, ">") {
		href = strings.TrimSuffix(strings.TrimPrefix(href, "<"), ">")
	}
	if idx := strings.IndexAny(href, " \t"); idx >= 0 {
		suffix = strings.TrimSpace(href[idx:])
		href = strings.TrimSpace(href[:idx])
	}
	anchor := ""
	if idx := strings.Index(href, "#"); idx >= 0 {
		anchor = href[idx+1:]
		href = href[:idx]
	}
	return href, suffix, anchor
}

func docsRebuildHref(href, suffix, anchor string) string {
	if anchor != "" {
		href += "#" + anchor
	}
	if suffix != "" {
		href += " " + suffix
	}
	return href
}

func docsIsExternalHref(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.HasPrefix(value, "http://") ||
		strings.HasPrefix(value, "https://") ||
		strings.HasPrefix(value, "mailto:") ||
		strings.HasPrefix(value, "tel:") ||
		strings.HasPrefix(value, "data:") ||
		strings.HasPrefix(value, "javascript:")
}

func docsLooksLikeMarkdown(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.HasSuffix(value, ".md") || strings.HasSuffix(value, ".mdx")
}

func docsResolveCandidatePaths(source docsSourceDocument, repoRoot, href string) []string {
	var candidates []string
	sourceDir := filepath.Dir(source.SourceAbsPath)
	candidates = append(candidates, filepath.Clean(filepath.Join(sourceDir, filepath.FromSlash(href))))
	if strings.TrimSpace(repoRoot) != "" {
		candidates = append(candidates, filepath.Clean(filepath.Join(repoRoot, filepath.FromSlash(href))))
	}
	trimmed := strings.TrimSuffix(href, filepath.Ext(href))
	if filepath.Ext(href) == "" {
		candidates = append(candidates,
			filepath.Clean(filepath.Join(sourceDir, filepath.FromSlash(trimmed+".md"))),
			filepath.Clean(filepath.Join(sourceDir, filepath.FromSlash(trimmed+".mdx"))),
			filepath.Clean(filepath.Join(sourceDir, filepath.FromSlash(trimmed), "README.md")),
			filepath.Clean(filepath.Join(sourceDir, filepath.FromSlash(trimmed), "index.md")),
		)
		if strings.TrimSpace(repoRoot) != "" {
			candidates = append(candidates,
				filepath.Clean(filepath.Join(repoRoot, filepath.FromSlash(trimmed+".md"))),
				filepath.Clean(filepath.Join(repoRoot, filepath.FromSlash(trimmed+".mdx"))),
				filepath.Clean(filepath.Join(repoRoot, filepath.FromSlash(trimmed), "README.md")),
				filepath.Clean(filepath.Join(repoRoot, filepath.FromSlash(trimmed), "index.md")),
			)
		}
	}
	return docsUniquePaths(candidates)
}

func docsUniquePaths(paths []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(paths))
	for _, current := range paths {
		current = filepath.Clean(current)
		if _, ok := seen[current]; ok {
			continue
		}
		seen[current] = struct{}{}
		out = append(out, current)
	}
	return out
}
