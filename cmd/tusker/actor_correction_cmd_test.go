package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func actorCorrectionFixtureEvent(t *testing.T, vault, object, eventID, actor string) (string, string) {
	t.Helper()
	path := filepath.Join(vault, "events", "2026", "08", object+"--20260818T072438Z--"+eventID+".json")
	if err := writeJSON(path, map[string]any{
		"actor": actor, "at": "2026-08-18T07:24:38Z", "event_kind": "updated", "id": eventID,
		"object": object, "object_kind": "proposal", "payload": map[string]any{"action": "applied"}, "project": "tusker", "schema": "tusker.event/v1",
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return eventID, "sha256:" + hexDigest(raw)
}

func hexDigest(raw []byte) string {
	// The production command uses the same standard-library digest format, but
	// this fixture intentionally computes the expected value independently.
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func actorCorrectionTestGate(t *testing.T, vault, gateID, evidence, actor string) {
	t.Helper()
	path := filepath.Join(vault, "work", "gates", gateID+".md")
	data := map[string]any{
		"schema": "tusker.gate/v1", "kind": "gate", "id": gateID, "project": "tusker", "title": "Correct historical attribution",
		"gate_kind": "decision", "status": "satisfied", "owner": actor, "priority": "p1", "blocking": false,
		"blocks": []string{"APP-T-0001"}, "covers": []string{"A1"}, "action": "Decide whether historical actor metadata is accurate.",
		"verification": "The typed actor correction receipt records the decision.", "satisfied_by": actor, "satisfied_at": "2026-08-18T08:00:00Z",
		"satisfaction_evidence": evidence, "created_at": "2026-08-18T07:00:00Z", "created_by": "agent:codex",
		"updated_at": "2026-08-18T08:00:00Z", "updated_by": actor,
	}
	body := "# " + gateID + " · Correct historical attribution\n\n## Action\n\nDecide whether historical actor metadata is accurate.\n\n## Verification\n\nThe typed actor correction receipt records the decision.\n"
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["gate"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(path, content); err != nil {
		t.Fatal(err)
	}
}

func TestActorCorrectionAGXSRVFixturesPlanAndProjection(t *testing.T) {
	vault := pickupV7TestVault(t)
	id1, hash1 := actorCorrectionFixtureEvent(t, vault, "AGX-P-0001", "01M09W41YYPT53DFSK5S6BX93F", "human:sarav")
	id2, hash2 := actorCorrectionFixtureEvent(t, vault, "SRV-P-0002", "01M09W42MHZRX59A50SYTH70QX", "human:sarav")
	clearAgentSessionEnvForTest(t)
	targets, err := resolveActorCorrectionTargets(vault, Args{
		"event-ids":        id1 + "," + id2,
		"original-sha256":  hash1 + "," + hash2,
		"corrected-actors": "agent:codex,agent:codex",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 || targets[0].EventID != id1 || targets[1].EventID != id2 {
		t.Fatalf("targets=%#v", targets)
	}
	if got := actorCorrectionScopeDigest(targets); !strings.HasPrefix(got, "sha256:") {
		t.Fatalf("scope digest=%q", got)
	}
	projection, err := loadV7ActorCorrectionProjection(vault)
	if err != nil {
		t.Fatal(err)
	}
	if len(projection) != 0 {
		t.Fatalf("fresh projection=%#v", projection)
	}
}

func TestActorCorrectionApplyFailsClosedWithoutExactHumanAuthority(t *testing.T) {
	vault := pickupV7TestVault(t)
	id, hash := actorCorrectionFixtureEvent(t, vault, "AGX-P-0001", "01M09W41YYPT53DFSK5S6BX93F", "human:sarav")
	eventPath := filepath.Join(vault, "events", "2026", "08", "AGX-P-0001--20260818T072438Z--"+id+".json")
	original, err := os.ReadFile(eventPath)
	if err != nil {
		t.Fatal(err)
	}
	clearAgentSessionEnvForTest(t)
	targets, err := resolveActorCorrectionTargets(vault, Args{"event-id": id, "original-sha256": hash, "corrected-actor": "agent:codex"})
	if err != nil {
		t.Fatal(err)
	}
	receipt := actorCorrectionReceipt{Schema: humanControlReceiptSchema, Kind: actorCorrectionSchema, GateID: "APP-G-0099", Actor: "human:sarav", IssuedAt: "2026-08-18T08:00:00Z", Targets: targets}
	receipt.ScopeDigest = actorCorrectionScopeDigest(receipt.Targets)
	receiptRaw, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	actorCorrectionTestGate(t, vault, receipt.GateID, string(receiptRaw), receipt.Actor)
	args := Args{"vault": vault, "event-id": id, "original-sha256": hash, "corrected-actor": "agent:codex", "gate": receipt.GateID, "by": receipt.Actor, "receipt": string(receiptRaw), "json": "true"}
	err = actorCorrectionApplyCmd(args)
	if err == nil || errorToIssue(err).Code != humanControlReceiptUnavailableCode {
		t.Fatalf("apply did not fail closed with typed authority refusal: %v", err)
	}
	after, err := os.ReadFile(eventPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(original) {
		t.Fatal("source event bytes changed during append-only correction")
	}
	projection, err := loadV7ActorCorrectionProjection(vault)
	if err != nil {
		t.Fatal(err)
	}
	if len(projection) != 0 {
		t.Fatalf("fail-closed apply wrote projection: %#v", projection)
	}
}

func TestActorCorrectionRefusesAgentSessionAndHashMismatchWithoutWrites(t *testing.T) {
	vault := pickupV7TestVault(t)
	id, hash := actorCorrectionFixtureEvent(t, vault, "SRV-P-0002", "01M09W42MHZRX59A50SYTH70QX", "human:sarav")
	clearAgentSessionEnvForTest(t)
	targets, err := resolveActorCorrectionTargets(vault, Args{"event-id": id, "original-sha256": hash, "corrected-actor": "agent:codex"})
	if err != nil {
		t.Fatal(err)
	}
	receipt := actorCorrectionReceipt{Schema: humanControlReceiptSchema, Kind: actorCorrectionSchema, GateID: "APP-G-0098", Actor: "human:sarav", IssuedAt: "2026-08-18T08:00:00Z", Targets: targets}
	receipt.ScopeDigest = actorCorrectionScopeDigest(receipt.Targets)
	receiptRaw, _ := json.Marshal(receipt)
	actorCorrectionTestGate(t, vault, receipt.GateID, string(receiptRaw), receipt.Actor)
	args := Args{"vault": vault, "event-id": id, "original-sha256": hash, "corrected-actor": "agent:codex", "gate": receipt.GateID, "by": receipt.Actor, "receipt": string(receiptRaw)}
	t.Setenv("CODEX_THREAD_ID", "actor-correction-agent")
	if err := actorCorrectionApplyCmd(args); err == nil || errorToIssue(err).Code != humanControlReceiptUnavailableCode {
		t.Fatalf("agent session did not receive typed authority refusal: %v", err)
	}
	clearAgentSessionEnvForTest(t)
	bad := args
	bad["original-sha256"] = "sha256:" + strings.Repeat("0", 64)
	if err := actorCorrectionApplyCmd(bad); err == nil || errorToIssue(err).Code != humanControlReceiptUnavailableCode {
		t.Fatalf("forged receipt did not receive typed authority refusal: %v", err)
	}
	projection, err := loadV7ActorCorrectionProjection(vault)
	if err != nil {
		t.Fatal(err)
	}
	if len(projection) != 0 {
		t.Fatalf("refused correction wrote projection: %#v", projection)
	}
}

func TestActorCorrectionRejectsHumanOrReviewerAsCorrectedActor(t *testing.T) {
	for _, actor := range []string{"human:sarav", "reviewer:independent", "evil:name", "agent:"} {
		if _, err := canonicalActorCorrectionActor(actor); err == nil {
			t.Fatalf("corrected actor %q was accepted", actor)
		}
	}
}
