package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestV7PacketPreservesCompleteTaskContract(t *testing.T) {
	vault := v7DispatchTestVault(t)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Complete handoff"}, newV7Task)
	task := mustV7Task(t, vault, "APP-T-0001")
	task.Data["owned_paths"] = []string{"internal/billing/"}
	task.Data["generated_outputs"] = []string{"generated/billing.go"}
	task.Data["migration_keys"] = []string{"billing-0042"}
	task.Data["resource_refs"] = []string{"migration-slot"}
	var acceptance, verification strings.Builder
	for i := 1; i <= 24; i++ {
		fmt.Fprintf(&acceptance, "| A%d | Preserve outcome %d. | Mapped check. |\n", i, i)
		fmt.Fprintf(&verification, "| A%d | command: check-outcome %d | pending | |\n", i, i)
	}
	task.Body = replaceSection(task.Body, "## Acceptance", acceptance.String())
	task.Body = replaceSection(task.Body, "## Verification", verification.String())
	task.Body = replaceSection(task.Body, "## Non-goals", "Do not change billing or deploy the service.")
	task.Body = replaceSection(task.Body, "## Implementation notes", "```sh\nif check-ready; then\n  run-check\nfi\n```")
	task.Body += "\n## Artifact contract\n\nKeep before and after measurements in evidence.\n"
	idx := mustIndex(t, vault)
	for _, audience := range []string{"agent", "reviewer"} {
		t.Run(audience, func(t *testing.T) {
			packet := v7Packet(vault, task, idx, audience)
			if !strings.Contains(packet, strings.TrimSpace(task.Body)) {
				t.Fatalf("%s packet lost or reformatted the task contract:\n%s", audience, packet)
			}
			if strings.Count(packet, "command: check-outcome 24") != 1 {
				t.Fatal("verification must appear exactly once, including the last check")
			}
			for _, expected := range []string{"internal/billing/", "generated/billing.go", "billing-0042", "migration-slot"} {
				if !strings.Contains(packet, expected) {
					t.Fatalf("packet lost ownership constraint %q", expected)
				}
			}
		})
	}
}

func TestDeliveryImportPreservesNonGoalsInPackets(t *testing.T) {
	vault := deliveryTestVault(t)
	plan := operationalDeliveryPlanV2()
	path := writeDeliveryV2TestPlan(t, vault, plan)
	if err := deliveryImportCmd(Args{"vault": vault, "plan": path, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	idx := mustIndex(t, vault)
	checked := 0
	for _, task := range idx.Tasks {
		if stringField(task.Data, "delivery_plan_scope") != plan.Scope {
			continue
		}
		checked++
		for _, audience := range []string{"agent", "reviewer"} {
			packet := v7Packet(vault, task, idx, audience)
			for _, nonGoal := range plan.NonGoals {
				if !strings.Contains(packet, nonGoal) {
					t.Fatalf("%s packet lost imported non-goal %q", audience, nonGoal)
				}
			}
		}
	}
	if checked != len(plan.Tasks) {
		t.Fatalf("checked %d imported tasks, want %d", checked, len(plan.Tasks))
	}
}

func TestTrustHandoffPreservesIntegratorContract(t *testing.T) {
	vault := v7DispatchTestVault(t)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Integrate handoff", "v7": "true"}, newV7Task)
	task := mustV7Task(t, vault, "APP-T-0001")
	task.Data["owned_paths"] = []string{"internal/billing/"}
	task.Body = replaceSection(task.Body, "## Acceptance", "| A1 | Preserve the last required outcome. | command: check-last |\n")
	task.Body = replaceSection(task.Body, "## Non-goals", "Do not deploy the service.")
	task.Body += "\n## Artifact contract\n\nKeep before and after measurements in evidence.\n"

	packet := integratorPacket(vault, task, mustIndex(t, vault))
	for _, want := range []string{
		strings.TrimSpace(task.Body),
		"internal/billing/",
	} {
		if !strings.Contains(packet, want) {
			t.Fatalf("integrator packet lost contract content %q:\n%s", want, packet)
		}
	}
}
