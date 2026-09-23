package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"tusker/internal/docgraph"
)

func s46RoutingRepo(t *testing.T) (repo, vault string) {
	t.Helper()
	repo = t.TempDir()
	vault = filepath.Join(repo, ".tusker")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Routing policy.", "v7": "true"}, newV7Epic)
	writeTestDoc(t, repo, "docs/system/00-overview.md", "---\nkind: doc\nsubject: overview\nstatus: current\n---\n# Overview\n")
	return repo, vault
}

func s46RoutingDecisions(t *testing.T, vault string) map[string]Note {
	t.Helper()
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	decisionIDs := map[string]Note{}
	for id, decision := range idx.Decisions {
		decisionIDs[id] = decision
	}
	return decisionIDs
}

// TestS46DomainRouting covers TSK-T-0057 A1: domain commands, validation,
// skill doctor, and task packets use portable knowledge with bounded reads.
func TestS46DomainRouting(t *testing.T) {
	repo, vault := s46RoutingRepo(t)

	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "v7": "true", "id": "billing", "title": "Billing", "summary": "Billing canon."}, newV7Domain)

	// Creation routes the same domain into the portable tree while keeping
	// the legacy records as compatibility pointers.
	portableRel := "docs/system/domains/billing/00-index.md"
	if !fileExists(filepath.Join(repo, filepath.FromSlash(portableRel))) {
		t.Fatalf("domain create did not scaffold portable index at %s", portableRel)
	}
	for _, legacy := range []string{"knowledge/domains/billing/INDEX.md", "knowledge/domains/billing/CANON.md"} {
		if !fileExists(filepath.Join(vault, filepath.FromSlash(legacy))) {
			t.Fatalf("domain create dropped legacy compatibility record %s", legacy)
		}
	}
	doc, err := docgraph.ParseDocHeaders(portableRel, mustReadFile(t, filepath.Join(repo, filepath.FromSlash(portableRel))))
	if err != nil {
		t.Fatalf("portable domain index does not parse: %v", err)
	}
	if doc.Kind != docgraph.KindDoc || doc.Subject != "billing" {
		t.Fatalf("portable domain index kind/subject = %q/%q", doc.Kind, doc.Subject)
	}

	// A portable-only domain owns canon with no legacy record at all.
	writeTestDoc(t, repo, "docs/system/domains/risk/00-index.md", strings.Join([]string{
		"---",
		"kind: doc",
		"subject: risk",
		"part_of: overview",
		"status: current",
		"code_conformance: not_applicable",
		"read_when: You need risk canon before implementing a task.",
		"skip_when: You only need task proof or runtime events.",
		"---",
		"# Risk",
		"",
		"## Purpose",
		"",
		"Risk appetite and review thresholds.",
		"",
		"## Reading order",
		"",
		"- Start here.",
		"",
	}, "\n"))
	if err := refreshV7ProjectSkill(vault); err != nil {
		t.Fatal(err)
	}
	skillBody := string(mustReadFile(t, filepath.Join(vault, "SKILL.md")))
	for _, want := range []string{"docs/system/domains/billing/00-index.md", "docs/system/domains/risk/00-index.md"} {
		if !strings.Contains(skillBody, want) {
			t.Fatalf("project skill omits portable route %s", want)
		}
	}

	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Routed task", "domains": "billing,risk"}, newV7Task)

	// Domain list merges portable-only domains with managed records.
	listed := captureStdout(t, func() {
		if err := domainV7ListCmd(Args{"vault": vault, "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var list struct {
		Domains []struct {
			ID   string `json:"id"`
			Path string `json:"path"`
		} `json:"domains"`
	}
	if err := json.Unmarshal([]byte(listed), &list); err != nil {
		t.Fatalf("domain list JSON: %v\n%s", err, listed)
	}
	paths := map[string]string{}
	for _, domain := range list.Domains {
		paths[domain.ID] = domain.Path
	}
	if paths["billing"] != portableRel {
		t.Fatalf("billing route = %q, want portable %q", paths["billing"], portableRel)
	}
	if paths["risk"] != "docs/system/domains/risk/00-index.md" {
		t.Fatalf("risk route = %q", paths["risk"])
	}

	// Domain reads return the actual portable document.
	shown := captureStdout(t, func() {
		if err := domainV7ShowCmd(Args{"vault": vault, "id": "risk", "full": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(shown, "Risk appetite and review thresholds.") {
		t.Fatalf("domain show did not return the portable document:\n%s", shown)
	}
	canon := captureStdout(t, func() {
		if err := domainV7CanonCmd(Args{"vault": vault, "id": "risk"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(canon, "Risk appetite and review thresholds.") {
		t.Fatalf("domain canon did not route to the portable document:\n%s", canon)
	}

	// The packet routes both domains through their portable indexes with
	// bounded context and no stale legacy required-read path.
	packet := captureStdout(t, func() {
		if err := packetV7Cmd(Args{"vault": vault, "id": "APP-T-0001", "for": "agent", "force": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	for _, want := range []string{
		"docs/system/domains/billing/00-index.md",
		"docs/system/domains/risk/00-index.md",
		"Risk appetite and review thresholds.",
	} {
		if !strings.Contains(packet, want) {
			t.Fatalf("packet missing portable domain content %q", want)
		}
	}
	if strings.Contains(packet, "knowledge/domains/risk") {
		t.Fatalf("packet routes portable-only domain at a stale legacy path")
	}
	context := packetSection(t, packet, "## Domain context")
	if lines := len(strings.Split(strings.TrimSpace(context), "\n")); lines > 60 {
		t.Fatalf("domain context is not bounded: %d lines", lines)
	}

	// Validation, skill doctor, and intent routing accept portable routes.
	for _, got := range s46RelevantIssues(t, vault) {
		t.Fatalf("portable routing issue: %s: %s", got.Code, got.Message)
	}
	routes, err := v7SkillRoutesForIntent(vault, "risk billing review thresholds")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"docs/system/domains/billing/00-index.md", "docs/system/domains/risk/00-index.md"} {
		if !containsString(routes, want) {
			t.Fatalf("skill route missing %s, got %#v", want, routes)
		}
	}
}

func s46RelevantIssues(t *testing.T, vault string) []Issue {
	t.Helper()
	var relevant []Issue
	errs, _ := validateV7SkillKnowledge(vault)
	for _, got := range errs {
		if got.Code == "PROJECT_SKILL_DOMAIN_ROUTE_MISSING" || got.Code == "TASK_DOMAIN_ROUTE_MISSING" {
			relevant = append(relevant, got)
		}
	}
	doctorErrs, _ := skillDoctorIssues(vault, false, false)
	for _, got := range doctorErrs {
		if got.Code == "PROJECT_SKILL_DOMAIN_ROUTE_MISSING" || got.Code == "TASK_DOMAIN_ROUTE_MISSING" {
			relevant = append(relevant, got)
		}
	}
	return relevant
}

func packetSection(t *testing.T, packet, heading string) string {
	t.Helper()
	lines := strings.Split(packet, "\n")
	var out []string
	active := false
	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			if active {
				break
			}
			active = line == heading
			continue
		}
		if active {
			out = append(out, line)
		}
	}
	if !active && len(out) == 0 {
		t.Fatalf("packet missing section %s", heading)
	}
	return strings.Join(out, "\n")
}

// TestS46TaskReferences covers TSK-T-0057 A2: new and legacy governing refs
// and anchors resolve; invalid kinds, targets, and sections fail usefully.
func TestS46TaskReferences(t *testing.T) {
	repo, vault := s46RoutingRepo(t)
	_ = repo

	writeTestDoc(t, repo, "docs/system/proposals/checkout-flow.md", strings.Join([]string{
		"---",
		"kind: proposal",
		"subject: checkout-flow",
		"part_of: overview",
		"status: proposed",
		"code_conformance: not_applicable",
		"---",
		"# Checkout flow",
		"",
		"## Lifecycle",
		"",
		"Proposed change, not yet accepted.",
		"",
	}, "\n"))
	writeTestDoc(t, repo, "docs/system/decisions/record-choice.md", strings.Join([]string{
		"---",
		"kind: decision",
		"subject: record-choice",
		"part_of: overview",
		"decides_for: checkout-flow",
		"status: accepted",
		"code_conformance: not_applicable",
		"---",
		"# Record choice",
		"",
		"## Rationale",
		"",
		"Settled by review.",
		"",
	}, "\n"))
	writeTestDoc(t, repo, ".tusker/specs/legacy-spec.md", strings.Join([]string{
		"---",
		"subject: legacy-spec",
		"part_of: overview",
		"---",
		"# Legacy spec",
		"",
	}, "\n"))
	writeTestDoc(t, repo, "docs/system/domains/billing/invoicing.md", strings.Join([]string{
		"---",
		"kind: doc",
		"subject: invoicing",
		"part_of: overview",
		"status: current",
		"---",
		"# Invoicing",
		"",
	}, "\n"))
	// A migrated legacy stub forwards to the actual portable document.
	writeTestDoc(t, repo, ".tusker/specs/old-flow.md", strings.Join([]string{
		"---",
		"subject: old-flow",
		"part_of: overview",
		"status: superseded",
		"superseded_by: checkout-flow",
		"---",
		"# Old flow",
		"",
		"Superseded by checkout-flow.",
		"",
	}, "\n"))
	for _, name := range []string{"dupe-a", "dupe-b"} {
		writeTestDoc(t, repo, "docs/system/proposals/"+name+".md", strings.Join([]string{
			"---",
			"kind: proposal",
			"subject: dupe-subject",
			"part_of: overview",
			"status: proposed",
			"---",
			"# Dupe " + name,
			"",
		}, "\n"))
	}

	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Recorded choice", "decision": "Use checkout flow."}, newV7Decision)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Governed task",
		"spec-refs": "checkout-flow,docs/system/decisions/record-choice.md,.tusker/specs/legacy-spec.md,APP-D-0001,docs/system/proposals/checkout-flow.md#Lifecycle"}, newV7Task)
	decisionIDs := s46RoutingDecisions(t, vault)

	// New subjects, new paths, legacy managed paths, tracker lifecycle
	// decisions, and section anchors all resolve.
	for _, ref := range []string{
		"checkout-flow",
		"docs/system/decisions/record-choice.md",
		".tusker/specs/legacy-spec.md",
		"legacy-spec",
		"APP-D-0001",
		"docs/system/proposals/checkout-flow.md#Lifecycle",
	} {
		if !v7SpecRefExists(vault, ref, decisionIDs) {
			t.Fatalf("governing ref did not resolve: %s (%s)", ref, v7SpecRefFailureReason(vault, ref, decisionIDs))
		}
	}
	if got := v7SpecRefReadPath(vault, "checkout-flow"); got != "docs/system/proposals/checkout-flow.md" {
		t.Fatalf("subject read path = %q", got)
	}
	if got := v7SpecRefReadPath(vault, "APP-D-0001"); got != ".tusker/work/decisions/APP-D-0001.md" {
		t.Fatalf("tracker decision read path = %q", got)
	}
	if got := v7SpecRefReadPath(vault, "docs/system/proposals/checkout-flow.md#Lifecycle"); got != "docs/system/proposals/checkout-flow.md#Lifecycle" {
		t.Fatalf("anchored read path = %q", got)
	}

	// Product decision files and tracker lifecycle decisions stay distinct.
	if v7SpecRefReadPath(vault, "record-choice") == v7SpecRefReadPath(vault, "APP-D-0001") {
		t.Fatalf("product decision file and tracker decision share a route")
	}

	task, err := resolveV7Note(vault, "APP-T-0001", "task")
	if err != nil {
		t.Fatal(err)
	}
	reads := automationPlanRequiredReads(vault, task)
	for _, want := range []string{"docs/system/proposals/checkout-flow.md", "docs/system/decisions/record-choice.md", ".tusker/specs/legacy-spec.md"} {
		if !containsString(reads, want) {
			t.Fatalf("required reads missing %s, got %#v", want, reads)
		}
	}
	packet := captureStdout(t, func() {
		if err := packetV7Cmd(Args{"vault": vault, "id": "APP-T-0001", "for": "agent", "force": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	for _, want := range []string{"docs/system/proposals/checkout-flow.md", "docs/system/decisions/record-choice.md"} {
		if !strings.Contains(packet, want) {
			t.Fatalf("packet missing portable governing path %s", want)
		}
	}
	if warnings := v7PacketSpecRefWarnings(vault, task, mustV7Index(t, vault)); len(warnings) != 0 {
		t.Fatalf("valid governing refs warned: %#v", warnings)
	}

	// Missing targets, ambiguous subjects, wrong kinds, and missing sections
	// fail usefully instead of manufacturing authority.
	for _, failure := range []struct {
		ref  string
		want string
	}{
		{"docs/system/proposals/nope.md", "missing target"},
		{"unknown-subject-xyz", "missing target"},
		{"dupe-subject", "ambiguous"},
		{"invoicing", "wrong kind"},
		{"docs/system/proposals/checkout-flow.md#NoSuchSection", "missing section"},
		{"APP-D-0001#Nope", "missing section"},
		{"APP-D-9999", "unknown task/decision id"},
	} {
		reason := v7SpecRefFailureReason(vault, failure.ref, decisionIDs)
		if reason == "" || !strings.Contains(reason, failure.want) {
			t.Fatalf("ref %q reason = %q, want %q", failure.ref, reason, failure.want)
		}
		if v7SpecRefExists(vault, failure.ref, decisionIDs) {
			t.Fatalf("invalid ref resolved: %s", failure.ref)
		}
	}

	bad, err := resolveV7Note(vault, "APP-T-0001", "task")
	if err != nil {
		t.Fatal(err)
	}
	bad.Data["spec_refs"] = []string{"docs/system/proposals/nope.md", "dupe-subject", "invoicing", "docs/system/proposals/checkout-flow.md#NoSuchSection"}
	warnings := validateV7SpecRefs(vault, bad, decisionIDs)
	if !issuesContainCode(warnings, "SPEC_REF_DANGLING") {
		t.Fatalf("expected dangling spec ref warnings, got %#v", warnings)
	}
	for _, needle := range []string{"nope.md", "dupe-subject", "invoicing", "NoSuchSection"} {
		if !issueMessageContains(warnings, needle) {
			t.Fatalf("warnings omit %s: %#v", needle, warnings)
		}
	}
	packetWarnings := v7PacketSpecRefWarnings(vault, bad, mustV7Index(t, vault))
	if len(packetWarnings) != 4 {
		t.Fatalf("packet warnings = %#v", packetWarnings)
	}

	// A proposal change against a portable subject applies through the same
	// resolver instead of a stale hint path.
	if err := proposalV7NewCmd(Args{
		"vault": vault, "quiet": "true", "action": "change", "target": "APP-T-0001",
		"set": "spec_refs=checkout-flow", "by": "agent:codex",
	}); err != nil {
		t.Fatalf("proposal change with portable subject: %v", err)
	}
}

func mustV7Index(t *testing.T, vault string) v7Index {
	t.Helper()
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	return idx
}
