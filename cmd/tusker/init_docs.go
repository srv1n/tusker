package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"tusker/internal/docgraph"
	specskill "tusker/skills/spec"
)

type initDocWrite struct {
	path string
	undo string
}

// scaffoldDocumentationSystem creates only the day-zero documentation
// skeleton. Existing files and skill installs are left alone so a second init
// is a true no-op for this surface.
func scaffoldDocumentationSystem(repoRoot string) ([]initDocWrite, error) {
	repoRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, err
	}
	if _, err := docsAdoptCanonicalRoot(repoRoot, "repository"); err != nil {
		return nil, err
	}
	undo := "remove the generated documentation and repo-local skill directories manually"
	var writes []initDocWrite
	for _, relative := range []string{"docs/system", ".tusker/specs", ".tusker/specs/decisions"} {
		if symlinkPath, symlinkErr := docsAdoptSymlinkPath(repoRoot, relative); symlinkErr != nil {
			return nil, symlinkErr
		} else if symlinkPath != "" {
			return nil, fmt.Errorf("documentation scaffold refuses symlinked path: %s", relative)
		}
		path := filepath.Join(repoRoot, filepath.FromSlash(relative))
		_, statErr := os.Stat(path)
		if errors.Is(statErr, os.ErrNotExist) {
			if err := ensureDir(path); err != nil {
				return nil, err
			}
			writes = append(writes, initDocWrite{path: path, undo: undo})
			continue
		}
		if statErr != nil {
			return nil, statErr
		}
		if err := ensureDir(path); err != nil {
			return nil, err
		}
	}
	overview := filepath.Join(repoRoot, "docs", "system", "00-overview.md")
	createdOverview := false
	if !fileExists(overview) {
		if err := docsAdoptWriteText(repoRoot, "docs/system/00-overview.md", initDocumentationOverview()); err != nil {
			return nil, err
		}
		createdOverview = true
		writes = append(writes, initDocWrite{path: overview, undo: undo})
	}

	// The bundled Tusker package is the existing distribution mechanism for the
	// operator router and its references. Preserve any existing materialization
	// so init remains idempotent for hand-edited installs.
	for _, relative := range []string{filepath.Join(".agents", "skills", currentSkillInstallDir), filepath.Join(".claude", "skills", currentSkillInstallDir)} {
		destination := filepath.Join(repoRoot, relative)
		if fileExists(filepath.Join(destination, "SKILL.md")) {
			continue
		}
		if err := installSkillPayloadWithMode(destination, skillInstallModeCopy); err != nil {
			return nil, err
		}
		writes = append(writes, initDocWrite{path: destination, undo: undo})
	}
	for _, relative := range []string{filepath.Join(".agents", "skills", "spec"), filepath.Join(".claude", "skills", "spec")} {
		destination := filepath.Join(repoRoot, relative)
		if fileExists(filepath.Join(destination, "SKILL.md")) {
			continue
		}
		if err := docsAdoptWriteText(repoRoot, filepath.ToSlash(filepath.Join(relative, "SKILL.md")), string(specskill.Skill)); err != nil {
			return nil, err
		}
		writes = append(writes, initDocWrite{path: destination, undo: undo})
	}

	// A brand-new overview must have a valid generated map before init returns.
	// Do not regenerate an existing corpus: idempotency means init never rewrites
	// a user's overview or generated artifacts.
	if createdOverview {
		if err := docgraph.WriteDocsMap(repoRoot); err != nil {
			return nil, err
		}
		for _, relative := range []string{"docs/system/INDEX.md", "docs/system/graph.json"} {
			writes = append(writes, initDocWrite{path: filepath.Join(repoRoot, filepath.FromSlash(relative)), undo: undo})
		}
	}
	return writes, nil
}

func initDocumentationOverview() string {
	created := time.Now().Local().Format("2006-01-02")
	return fmt.Sprintf(`---
title: "System overview"
subject: overview
keywords: [system, documentation, architecture]
status: canonical
created: %s
last_verified:
read_when: "You need the top-level map of how this repository works."
skip_when: "You need one subsystem's current contract; use tusker docs find."
---

# System overview

This page is the top-level entry point for the repository's current system
truth. The diagram below is generated from document front matter; edit the
documents and run tusker docs map instead of drawing the graph by hand.

<!-- tusker:docs-map:begin -->
<!-- tusker:docs-map:end -->
`, created)
}
