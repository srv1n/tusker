package main

import "testing"

func TestBuildDocsCanonManifestSeparatesCanonFromPublished(t *testing.T) {
	sources := []docsSourceDocument{
		{
			SourceKind:      docsSourceKindVault,
			SourceID:        "MEM-D-0001",
			Title:           "Memory Architecture",
			Description:     "Canonical memory architecture.",
			Audience:        "developer",
			DocIntent:       "canon",
			Epic:            "MEM",
			OwnerEpic:       "MEM",
			CanonFor:        "MEM",
			Canonical:       true,
			CanonicalStatus: "approved",
			VerifiedAt:      "2026-04-28",
			SourcePath:      "epics/MEM/MEM-D-0001.md",
			RoutePath:       "developer/architecture/memory",
			RouteURL:        "/developer/architecture/memory/",
			Tags:            []string{"architecture"},
			Updated:         "2026-04-28",
		},
		{
			SourceKind:  docsSourceKindRepo,
			Title:       "CLI Guide",
			Description: "User-facing CLI guide.",
			Audience:    "user",
			SourcePath:  "skill/references/COMMANDS.md",
			RoutePath:   "user/reference/commands",
			RouteURL:    "/user/reference/commands/",
			Tags:        []string{"reference"},
			Updated:     "2026-04-28",
		},
	}

	manifest := buildDocsCanonManifest("2026-04-28T00:00:00Z", sources)
	if manifest.SchemaVersion != docsManifestSchemaVersion {
		t.Fatalf("expected schema version %d, got %d", docsManifestSchemaVersion, manifest.SchemaVersion)
	}
	if len(manifest.Published) != 2 {
		t.Fatalf("expected 2 published docs, got %d", len(manifest.Published))
	}
	if len(manifest.Canon) != 1 {
		t.Fatalf("expected 1 canon doc, got %d", len(manifest.Canon))
	}
	if manifest.Canon[0].Topic != "MEM" {
		t.Fatalf("expected canon topic MEM, got %q", manifest.Canon[0].Topic)
	}
	if manifest.Canon[0].SourcePath != "epics/MEM/MEM-D-0001.md" {
		t.Fatalf("expected source path to survive manifest, got %q", manifest.Canon[0].SourcePath)
	}
	if manifest.Canon[0].CanonicalStatus != "approved" || manifest.Canon[0].VerifiedAt != "2026-04-28" {
		t.Fatalf("expected canonical lifecycle fields, got %#v", manifest.Canon[0])
	}
	if len(manifest.DoNotCite) == 0 {
		t.Fatal("expected do_not_cite guidance")
	}
}

func TestBuildDocsCanonManifestDoesNotInferCanonFromTags(t *testing.T) {
	sources := []docsSourceDocument{
		{
			SourceKind:  docsSourceKindRepo,
			Title:       "Draft Spec",
			Description: "Draft spec with a specs tag.",
			Audience:    "developer",
			SourcePath:  "docs/specs/draft.md",
			RoutePath:   "developer/specs/draft",
			RouteURL:    "/developer/specs/draft/",
			Tags:        []string{"specs", "architecture"},
			Updated:     "2026-04-28",
		},
		{
			SourceKind:      docsSourceKindRepo,
			Title:           "Approved Spec",
			Description:     "Explicitly canonical spec.",
			Audience:        "developer",
			OwnerEpic:       "ORC",
			Canonical:       true,
			CanonicalStatus: "approved",
			SourcePath:      "docs/specs/approved.md",
			RoutePath:       "developer/specs/approved",
			RouteURL:        "/developer/specs/approved/",
			Tags:            []string{"specs"},
			Updated:         "2026-04-28",
		},
	}

	manifest := buildDocsCanonManifest("2026-04-28T00:00:00Z", sources)
	if len(manifest.Canon) != 1 {
		t.Fatalf("expected only explicit canonical doc, got %#v", manifest.Canon)
	}
	if manifest.Canon[0].SourcePath != "docs/specs/approved.md" {
		t.Fatalf("expected approved spec in canon, got %q", manifest.Canon[0].SourcePath)
	}
}

func TestBuildDocsCanonManifestExcludesDeprecatedCanon(t *testing.T) {
	sources := []docsSourceDocument{
		{
			SourceKind:      docsSourceKindRepo,
			Title:           "Old Spec",
			Description:     "Superseded spec.",
			Audience:        "developer",
			OwnerEpic:       "ORC",
			Canonical:       true,
			CanonicalStatus: "deprecated",
			Deprecated:      true,
			SupersededBy:    "/developer/specs/new/",
			SourcePath:      "docs/specs/old.md",
			RoutePath:       "developer/specs/old",
			RouteURL:        "/developer/specs/old/",
		},
	}

	manifest := buildDocsCanonManifest("2026-04-28T00:00:00Z", sources)
	if len(manifest.Canon) != 0 {
		t.Fatalf("expected deprecated canonical doc to be excluded from canon, got %#v", manifest.Canon)
	}
	if len(manifest.Published) != 1 || !manifest.Published[0].Deprecated || manifest.Published[0].SupersededBy == "" {
		t.Fatalf("expected deprecation metadata in published list, got %#v", manifest.Published)
	}
}

func TestBuildDocsContentManifestIncludesAgentRoutingFields(t *testing.T) {
	sources := []docsSourceDocument{
		{
			SourceKind:      docsSourceKindVault,
			SourceID:        "MEM-D-0001",
			Title:           "Memory Architecture",
			Description:     "Canonical memory architecture.",
			Audience:        "developer",
			DocIntent:       "canon",
			Epic:            "MEM",
			OwnerEpic:       "MEM",
			CanonFor:        "MEM",
			Canonical:       true,
			CanonicalStatus: "approved",
			SourcePath:      "epics/MEM/MEM-D-0001.md",
			RoutePath:       "developer/architecture/memory",
			RouteURL:        "/developer/architecture/memory/",
		},
	}

	manifest := buildDocsContentManifest("2026-04-28T00:00:00Z", sources)
	if manifest.SchemaVersion != docsManifestSchemaVersion {
		t.Fatalf("expected schema version %d, got %d", docsManifestSchemaVersion, manifest.SchemaVersion)
	}
	if len(manifest.Items) != 1 {
		t.Fatalf("expected 1 manifest item, got %d", len(manifest.Items))
	}
	item := manifest.Items[0]
	if item.DocIntent != "canon" || item.CanonFor != "MEM" || item.Epic != "MEM" {
		t.Fatalf("manifest item lost canon routing fields: %#v", item)
	}
	if !item.Canonical || item.CanonicalStatus != "approved" || item.OwnerEpic != "MEM" {
		t.Fatalf("manifest item lost canonical lifecycle fields: %#v", item)
	}
	if item.SourcePath != "epics/MEM/MEM-D-0001.md" {
		t.Fatalf("expected source path, got %q", item.SourcePath)
	}
}

func TestBuildDocsRemovedRoutesReportIgnoresRedirectedRoutes(t *testing.T) {
	previous := docsExportState{
		Routes: []docsExportStateRoute{
			{
				Title:      "Old Memory",
				SourceKind: string(docsSourceKindRepo),
				SourcePath: "docs/old-memory.md",
				Route:      "developer/architecture/old-memory",
				RouteURL:   "/developer/architecture/old-memory/",
				OutputPath: "src/content/docs/developer/architecture/old-memory.md",
			},
			{
				Title:      "Gone",
				SourceKind: string(docsSourceKindRepo),
				SourcePath: "docs/gone.md",
				Route:      "developer/architecture/gone",
				RouteURL:   "/developer/architecture/gone/",
				OutputPath: "src/content/docs/developer/architecture/gone.md",
			},
		},
	}
	current := docsExportState{
		Routes: []docsExportStateRoute{
			{
				Title:      "New Memory",
				SourceKind: string(docsSourceKindRepo),
				SourcePath: "docs/new-memory.md",
				Route:      "developer/architecture/new-memory",
				RouteURL:   "/developer/architecture/new-memory/",
				OutputPath: "src/content/docs/developer/architecture/new-memory.md",
			},
		},
	}
	sources := []docsSourceDocument{
		{
			SourceKind:   docsSourceKindRepo,
			SourcePath:   "docs/new-memory.md",
			RoutePath:    "developer/architecture/new-memory",
			RedirectFrom: []string{"developer/architecture/old-memory"},
		},
	}

	report := buildDocsRemovedRoutesReport("2026-04-28T00:00:00Z", previous, current, sources)
	if report.SchemaVersion != docsManifestSchemaVersion {
		t.Fatalf("expected schema version %d, got %d", docsManifestSchemaVersion, report.SchemaVersion)
	}
	if len(report.Removed) != 1 {
		t.Fatalf("expected one unredirected removed route, got %#v", report.Removed)
	}
	if report.Removed[0].Route != "developer/architecture/gone" {
		t.Fatalf("expected gone route to be reported, got %#v", report.Removed[0])
	}
}
