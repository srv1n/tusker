package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func TestSkillContractCompatibility(t *testing.T) {
	root := filepath.Join("..", "..", "skills", "tusker")
	contract, err := readSkillCompatibilityContract(root)
	if err != nil {
		t.Fatal(err)
	}
	factory, err := embeddedFactoryIntakeContractProvenance()
	if err != nil {
		t.Fatal(err)
	}
	if contract.Schema != skillCompatibilitySchema || contract.FactoryIntakeContract != factory {
		t.Fatalf("compatibility contract = %#v", contract)
	}

	first, err := buildCapabilitiesManifest(nil, "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := buildCapabilitiesManifest(nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if first.Compatibility.Schema != skillCompatibilitySchema ||
		!strings.HasPrefix(first.Compatibility.Fingerprint, "sha256:") ||
		first.Compatibility.CanonicalPayloadFP == "" ||
		first.Compatibility.MaterializationSchema != skillMaterializationSchema ||
		first.Compatibility.ProvenanceManifest != skillProvenanceFilename ||
		!reflect.DeepEqual(first.Compatibility, second.Compatibility) {
		t.Fatalf("capability compatibility is incomplete or unstable: %#v", first.Compatibility)
	}
	changed := first
	changed.Commands = append(append([]capabilityCommand(nil), first.Commands...), capabilityCommand{Command: "future-command"})
	sortCapabilitiesManifest(&changed)
	changedCompatibility, err := buildCapabilityCompatibility(changed)
	if err != nil {
		t.Fatal(err)
	}
	if changedCompatibility.Fingerprint == first.Compatibility.Fingerprint {
		t.Fatal("compatibility fingerprint did not bind command/schema support")
	}
	changed = first
	changed.OptionalCapabilities[0].Available = !changed.OptionalCapabilities[0].Available
	changedCompatibility, err = buildCapabilityCompatibility(changed)
	if err != nil {
		t.Fatal(err)
	}
	if changedCompatibility.Fingerprint == first.Compatibility.Fingerprint {
		t.Fatal("compatibility fingerprint did not bind optional capability availability")
	}

	current := filepath.Join(t.TempDir(), "current")
	if err := installSkillPayloadCopy(current); err != nil {
		t.Fatal(err)
	}
	report := inspectSkillMaterialization(current)
	if report.Status != "current" || report.Manifest == nil ||
		report.Manifest.CompatibilityFingerprint != first.Compatibility.Fingerprint ||
		report.Manifest.CanonicalPayloadFingerprint != first.Compatibility.CanonicalPayloadFP {
		t.Fatalf("current materialization = %#v", report)
	}
	if missing := inspectSkillMaterialization(filepath.Join(t.TempDir(), "missing")); missing.Status != "missing" {
		t.Fatalf("missing materialization = %#v", missing)
	}

	stale := filepath.Join(t.TempDir(), "stale")
	if err := installSkillPayloadCopy(stale); err != nil {
		t.Fatal(err)
	}
	staleManifest := filepath.Join(stale, skillProvenanceFilename)
	raw, err := os.ReadFile(staleManifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(staleManifest, strings.Replace(string(raw), first.Compatibility.Fingerprint, "sha256:stale", 1)); err != nil {
		t.Fatal(err)
	}
	if got := inspectSkillMaterialization(stale); got.Status != "stale" {
		t.Fatalf("stale materialization = %#v", got)
	}

	incompatible := filepath.Join(t.TempDir(), "incompatible")
	if err := installSkillPayloadCopy(incompatible); err != nil {
		t.Fatal(err)
	}
	incompatibleManifest := filepath.Join(incompatible, skillProvenanceFilename)
	raw, err = os.ReadFile(incompatibleManifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(incompatibleManifest, strings.Replace(string(raw), "compatibility_schema: "+skillCompatibilitySchema, "compatibility_schema: tusker.skill-compatibility/v0", 1)); err != nil {
		t.Fatal(err)
	}
	if got := inspectSkillMaterialization(incompatible); got.Status != "incompatible" {
		t.Fatalf("incompatible materialization = %#v", got)
	}

	modified := filepath.Join(t.TempDir(), "modified")
	if err := installSkillPayloadCopy(modified); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(modified, "SKILL.md"), "local edit\n"); err != nil {
		t.Fatal(err)
	}
	if got := inspectSkillMaterialization(modified); got.Status != "locally_modified" {
		t.Fatalf("locally modified materialization = %#v", got)
	}
	if repair := skillSyncRepairAction(); !strings.Contains(repair, "tusker skill sync --repo .") {
		t.Fatalf("repair action = %q", repair)
	}
}

func TestTuskerSkillProgressiveDisclosure(t *testing.T) {
	root := filepath.Join("..", "..", "skills", "tusker")
	data, body, err := parseFrontmatterMustRead(filepath.Join(root, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 2 || stringField(data, "name") != "tusker" || strings.TrimSpace(stringField(data, "description")) == "" {
		t.Fatalf("SKILL.md frontmatter keys = %#v", data)
	}
	if words := len(strings.Fields(body)); words > 900 {
		t.Fatalf("SKILL.md body words = %d, max 900", words)
	}
	if lines := len(strings.Split(strings.TrimSuffix(body, "\n"), "\n")); lines > 140 {
		t.Fatalf("SKILL.md body lines = %d, max 140", lines)
	}

	routePattern := regexp.MustCompile("`(references/[A-Z0-9_-]+\\.md)`")
	routes := routePattern.FindAllStringSubmatch(body, -1)
	if len(routes) != 7 {
		t.Fatalf("router routes = %#v", routes)
	}
	for _, match := range routes {
		if !fileExists(filepath.Join(root, filepath.FromSlash(match[1]))) {
			t.Fatalf("router has broken route %s", match[1])
		}
	}
	routeTable := parseSkillRouteTable(body)
	expectedRoutes := map[string]string{
		"Create, update, or close tracked work":       "references/TRACK.md",
		"Answer from or write repo knowledge":         "references/KNOWLEDGE.md",
		"Read or update documentation/spec contracts": "references/SPECS.md",
		"Run a task, resolve gates, watch runs":       "references/RUN.md",
		"Tracker diagnosis or stuck task state":       "references/OPERATE.md",
		"Existing-repo onboarding":                    "references/REPO_ONBOARDING.md",
		"Xcode generated build-state failure":         "references/XCODE_BUILD_STATE.md",
	}
	if !reflect.DeepEqual(routeTable, expectedRoutes) {
		t.Fatalf("router table = %#v, want %#v", routeTable, expectedRoutes)
	}
	for _, rel := range []string{"references/TRACK.md", "references/KNOWLEDGE.md", "references/SPECS.md", "references/RUN.md", "references/OPERATE.md"} {
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "references/") {
			t.Fatalf("primary guide %s recursively routes to another reference", rel)
		}
	}
	assertNoDuplicateSkillParagraphs(t, root, []string{"SKILL.md", "references/TRACK.md", "references/KNOWLEDGE.md", "references/SPECS.md", "references/RUN.md", "references/OPERATE.md"})

	raw, err := os.ReadFile(filepath.Join(root, "testdata", "progressive-disclosure-budget.json"))
	if err != nil {
		t.Fatal(err)
	}
	var budget struct {
		Schema string `json:"schema"`
		Router struct {
			Path     string `json:"path"`
			MaxWords int    `json:"max_words"`
			MaxLines int    `json:"max_lines"`
		} `json:"router"`
		Cases []struct {
			ID             string   `json:"id"`
			Prompt         string   `json:"prompt"`
			Route          string   `json:"route"`
			Guide          string   `json:"guide"`
			LoadedWords    int      `json:"loaded_words"`
			MaxLoadedWords int      `json:"max_loaded_words"`
			Required       []string `json:"required"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &budget); err != nil {
		t.Fatal(err)
	}
	if budget.Schema != "tusker.skill-disclosure-budget/v1" || len(budget.Cases) != 7 {
		t.Fatalf("budget fixture = %#v", budget)
	}
	routerRaw, err := os.ReadFile(filepath.Join(root, budget.Router.Path))
	if err != nil {
		t.Fatal(err)
	}
	routerText := string(routerRaw)
	seen := map[string]bool{}
	for _, testCase := range budget.Cases {
		if strings.TrimSpace(testCase.Prompt) == "" || seen[testCase.ID] {
			t.Fatalf("invalid disclosure case %#v", testCase)
		}
		seen[testCase.ID] = true
		if routed := routeTable[testCase.Route]; routed == "" || routed != testCase.Guide {
			t.Fatalf("%s route %q selects %q, want %q", testCase.ID, testCase.Route, routed, testCase.Guide)
		}
		guideRaw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(testCase.Guide)))
		if err != nil {
			t.Fatal(err)
		}
		loaded := routerText + "\n" + string(guideRaw)
		words := len(strings.Fields(loaded))
		if words != testCase.LoadedWords {
			t.Fatalf("%s recorded %d loaded words, measured %d", testCase.ID, testCase.LoadedWords, words)
		}
		if words > testCase.MaxLoadedWords {
			t.Fatalf("%s loaded %d words, budget %d", testCase.ID, words, testCase.MaxLoadedWords)
		}
		for _, required := range testCase.Required {
			if !strings.Contains(loaded, required) {
				t.Fatalf("%s route lost safety behavior %q", testCase.ID, required)
			}
		}
	}

	canonicalFingerprint, err := skillPayloadFingerprint(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, install := range []string{filepath.Join("..", "..", ".agents", "skills", "tusker"), filepath.Join("..", "..", ".claude", "skills", "tusker")} {
		resolved, err := filepath.EvalSymlinks(install)
		if err != nil {
			t.Fatal(err)
		}
		fingerprint, err := skillPayloadFingerprint(resolved)
		if err != nil || fingerprint != canonicalFingerprint {
			t.Fatalf("generated install %s fingerprint = %s, want %s (%v)", install, fingerprint, canonicalFingerprint, err)
		}
	}
}

func parseSkillRouteTable(body string) map[string]string {
	routes := map[string]string{}
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "|") {
			continue
		}
		columns := strings.Split(strings.Trim(line, "|"), "|")
		if len(columns) != 2 {
			continue
		}
		request := strings.TrimSpace(columns[0])
		guide := strings.Trim(strings.TrimSpace(columns[1]), "`")
		if request == "Request" || strings.HasPrefix(request, "---") || !strings.HasPrefix(guide, "references/") {
			continue
		}
		routes[request] = guide
	}
	return routes
}

func assertNoDuplicateSkillParagraphs(t *testing.T, root string, files []string) {
	t.Helper()
	owners := map[string]string{}
	for _, rel := range files {
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		for _, paragraph := range strings.Split(string(raw), "\n\n") {
			normalized := strings.Join(strings.Fields(paragraph), " ")
			if len(normalized) < 100 || strings.HasPrefix(normalized, "```") {
				continue
			}
			if owner, exists := owners[normalized]; exists {
				t.Fatalf("normative paragraph duplicated in %s and %s: %s", owner, rel, normalized)
			}
			owners[normalized] = rel
		}
	}
}
