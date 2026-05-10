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
