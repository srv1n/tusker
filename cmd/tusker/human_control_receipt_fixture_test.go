package main

import (
	"fmt"
	"testing"
)

// gateV7TransitionWithTrustedHumanReceiptForTest exercises the production
// post-verifier transition seam. Signature/challenge verification belongs in
// TestTrustHumanReceipts; fixtures using this helper must provide a current
// state_rev and cannot make a raw human actor authoritative.
func gateV7TransitionWithTrustedHumanReceiptForTest(t *testing.T, vaultPath, id, status, actor string) error {
	t.Helper()
	note, err := resolveV7Note(vaultPath, id, "gate")
	if err != nil {
		return err
	}
	data, body, err := parseFrontmatterMustRead(note.AbsolutePath)
	if err != nil {
		return err
	}
	action := v7HumanControlAction(status)
	materialRevision := stringField(data, "state_rev")
	if action == "" || materialRevision == "" {
		return fmt.Errorf("fixture gate %s lacks a current human receipt binding", id)
	}
	receipt := &verifiedHumanControlReceipt{
		GateID:           id,
		Actor:            actor,
		MaterialRevision: materialRevision,
		ActionDigest:     humanControlActionDigest(data, body, action),
		Action:           action,
	}
	return gateV7TransitionWithHumanReceipt(Args{"vault": vaultPath, "repo": v7RepoRoot(vaultPath), "id": id, "quiet": "true"}, status, receipt)
}
