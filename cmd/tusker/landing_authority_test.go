package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// These are adversarial boundary tests: receipt/index JSON and daemon-looking
// labels are not authority.  The positive signing/restart fixture belongs with
// the scheduled-departure integration matrix because it requires a real
// registered project and isolated gate runner.
func TestV7LandingAuthorityRejectsForgedReceiptAndRuntimeRecord(t *testing.T) {
	repo, _ := newLandTestRepo(t, 1, "true")
	t.Setenv("TUSKER_STATE_ROOT", filepath.Join(t.TempDir(), "state"))
	now := time.Now().UTC().Format(time.RFC3339Nano)
	receipt := v7LandingReceipt{
		Schema: v7LandingReceiptSchema, Fingerprint: "forged", ControlAuthority: v7LandingAuthorityDeparture,
		ProjectID: "app", RepoIdentity: "sha256:forged", DepartureID: "departure-forged", PolicyID: "nightly",
		ScheduledWindow: now, DaemonSessionID: "daemon:departure:forged", DaemonHost: "forged", DaemonProcess: "forged",
		AuthorityID: "forged", AuthorityGen: 1, AuthoritySignature: make([]byte, ed25519.SignatureSize),
	}
	if verifyV7LandingReceiptAuthority(repo, receipt) {
		t.Fatal("self-consistent receipt and daemon-looking actor label minted authority")
	}

	// A caller-provided runtime public key is equally useless without the exact
	// durable issuance/context and a signature over the complete receipt.
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	receipt.AuthoritySignature = ed25519.Sign(private, []byte(receipt.Fingerprint))
	if verifyV7LandingReceiptAuthority(repo, receipt) {
		t.Fatal("forged runtime signing session was accepted")
	}
}

func TestV7LandingAuthorityRequiresSandboxForScheduledGate(t *testing.T) {
	if _, err := exec.LookPath("sandbox-exec"); err == nil {
		t.Skip("host has the required isolation primitive")
	}
	if _, err := runV7LandingGateCommand(t.TempDir(), "true", true); err == nil {
		t.Fatal("unsupported host ran a scheduled gate without isolation")
	}
}
