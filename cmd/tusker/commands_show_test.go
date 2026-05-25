package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestNewTaskTemplateIsRiskScaledAndCapsuleFirst(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustRunIndexTest(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustRunIndexTest(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App foundation", "summary": "Build the app foundation."}, newV5Epic)
	mustRunIndexTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Tiny cleanup", "risk": "low", "size": "s"}, func(args Args) error {
		return newV5Task(args, "feature")
	})
	mustRunIndexTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Risky migration", "risk": "high", "size": "m"}, func(args Args) error {
		return newV5Task(args, "feature")
	})

	low := mustReadIndexTest(t, filepath.Join(vault, "epics", "APP", "APP-T-0001.md"))
	assertContainsIndexTest(t, low, "## Agent capsule")
	assertContainsIndexTest(t, low, "## Intent")
	assertContainsIndexTest(t, low, "## Acceptance contract")
	assertContainsIndexTest(t, low, "## Evidence")
	assertNotContainsIndexTest(t, low, "## Work log")
	assertNotContainsIndexTest(t, low, "## Execution plan")
	assertNotContainsIndexTest(t, low, "## Knowledge delta")
	lowData, _, err := parseFrontmatter(low)
	if err != nil {
		t.Fatal(err)
	}
	for _, emptyField := range []string{"ai_tools", "assignee", "domains", "doc_nodes", "blocked_by", "block_reason", "blocks"} {
		if _, ok := lowData[emptyField]; ok {
			t.Fatalf("new low task should omit empty optional field %q:\n%s", emptyField, low)
		}
	}

	high := mustReadIndexTest(t, filepath.Join(vault, "epics", "APP", "APP-T-0002.md"))
	assertContainsIndexTest(t, high, "## Agent capsule")
	assertContainsIndexTest(t, high, "## Knowledge delta")
	assertContainsIndexTest(t, high, "## Verification log")
	assertNotContainsIndexTest(t, high, "## Work log")
}

func TestShowDefaultsToCapsuleAndCanReadAcceptanceOnly(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustRunIndexTest(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustRunIndexTest(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App foundation", "summary": "Build the app foundation."}, newV5Epic)
	mustRunIndexTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Capsule task", "risk": "medium", "size": "s"}, func(args Args) error {
		return newV5Task(args, "feature")
	})

	capsule := captureStdout(t, func() {
		if err := showCmd(Args{"vault": vault, "id": "APP-T-0001"}); err != nil {
			t.Fatal(err)
		}
	})
	assertContainsIndexTest(t, capsule, "APP-T-0001")
	assertContainsIndexTest(t, capsule, "Essence: Capsule task.")
	assertNotContainsIndexTest(t, capsule, "## Acceptance contract")

	acceptance := captureStdout(t, func() {
		if err := showCmd(Args{"vault": vault, "id": "APP-T-0001", "acceptance": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	assertContainsIndexTest(t, acceptance, "Outcome")
	if strings.Contains(acceptance, "## Scope") {
		t.Fatalf("acceptance slice should not include later sections:\n%s", acceptance)
	}
}

func TestShowSynthesizesV7TaskCapsuleWithRoutingFacts(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	if err := bootstrapV7Profile(vault, "v7"); err != nil {
		t.Fatal(err)
	}
	mustRunIndexTest(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App foundation", "summary": "Build the app foundation.", "v7": "true"}, newV7Epic)
	mustRunIndexTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Capsule task", "risk": "medium", "priority": "p1", "domains": "project", "v7": "true"}, newV7Task)

	capsule := captureStdout(t, func() {
		if err := showCmd(Args{"vault": vault, "id": "APP-T-0001", "capsule": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	for _, expected := range []string{
		"APP-T-0001  task  ready  Capsule task",
		"Readiness: ready",
		"Proof:",
		"Next owner:",
		"Domains: project",
		"Project skill route:",
		"tusker packet APP-T-0001 --for agent",
		"Forbidden paths:",
	} {
		assertContainsIndexTest(t, capsule, expected)
	}
}

func TestShowAcceptanceReadsV7AcceptanceSection(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	if err := bootstrapV7Profile(vault, "v7"); err != nil {
		t.Fatal(err)
	}
	mustRunIndexTest(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App foundation", "summary": "Build the app foundation.", "v7": "true"}, newV7Epic)
	mustRunIndexTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Capsule task", "risk": "medium", "priority": "p1", "v7": "true"}, newV7Task)

	acceptance := captureStdout(t, func() {
		if err := showCmd(Args{"vault": vault, "id": "APP-T-0001", "acceptance": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	assertContainsIndexTest(t, acceptance, "| ID | Outcome | Proof |")
	assertContainsIndexTest(t, acceptance, "Complete the task contract")
	assertNotContainsIndexTest(t, acceptance, "no Acceptance contract section")
}

func TestShowVerificationUsesFrontmatterSummaryWithoutLogSection(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustRunIndexTest(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustRunIndexTest(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App foundation", "summary": "Build the app foundation."}, newV5Epic)
	mustRunIndexTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Verified medium task", "risk": "medium", "size": "s"}, func(args Args) error {
		return newV5Task(args, "feature")
	})
	mustRunIndexTest(t, Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "status": "review", "actor": "agent"}, setStatus)
	mustRunIndexTest(t, Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "by": "agent", "summary": "acceptance and evidence checked"}, verifyV5Cmd)

	output := captureStdout(t, func() {
		if err := showCmd(Args{"vault": vault, "id": "APP-T-0001", "verification": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	assertContainsIndexTest(t, output, "Verification: acceptance and evidence checked")
	assertContainsIndexTest(t, output, "Verified by: agent")
	assertNotContainsIndexTest(t, output, "no Verification log section")
}

func TestShowVerificationBoundsLegacyLogTail(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustRunIndexTest(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustRunIndexTest(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App foundation", "summary": "Build the app foundation."}, newV5Epic)
	mustRunIndexTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Long verification", "risk": "medium", "size": "s"}, func(args Args) error {
		return newV5Task(args, "feature")
	})

	taskPath := filepath.Join(vault, "epics", "APP", "APP-T-0001.md")
	content := mustReadIndexTest(t, taskPath)
	content += "\n## Verification log\n\n- line 1\n- line 2\n- line 3\n- line 4\n- line 5\n- line 6\n- line 7\n"
	if err := writeText(taskPath, content); err != nil {
		t.Fatal(err)
	}

	output := captureStdout(t, func() {
		if err := showCmd(Args{"vault": vault, "id": "APP-T-0001", "verification": "true", "lines": "3"}); err != nil {
			t.Fatal(err)
		}
	})
	assertContainsIndexTest(t, output, "Verification log: last 3 of 7 entries")
	assertContainsIndexTest(t, output, "- line 7")
	assertContainsIndexTest(t, output, `Full log: tusker show APP-T-0001 --section "Verification log"`)
	assertNotContainsIndexTest(t, output, "- line 1")
}
