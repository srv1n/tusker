package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// landFrozenSourcesAsIssuedDeparture is intentionally test-only fixture
// plumbing. It follows the production shape (durable departure row, resident
// daemon, in-memory private key) rather than granting a caller an actor-label
// bypass. Tests that are specifically checking rejection must not use it.
func landFrozenSourcesAsIssuedDeparture(t *testing.T, repo, vault string, args Args, sources map[string]string) error {
	t.Helper()
	if !fileExists(workflowPath(vault)) {
		if err := writeDefaultWorkflow(vault); err != nil {
			return err
		}
	}
	wf, err := loadWorkflow(vault)
	if err != nil {
		return err
	}
	ids := landTargets(args)
	if len(ids) == 0 {
		return fmt.Errorf("fixture requires landing targets")
	}
	idx, err := loadV7Index(vault)
	if err != nil {
		return err
	}
	// Implicit singleton construction is normally performed by landing. Do it
	// before issuance in this fixture so the durable authority can bind its
	// exact target ref and membership.
	for _, id := range ids {
		if stringField(idx.Tasks[id].Data, "wave") == "" {
			if _, _, err := ensureV7ImplicitSingletonDeliveryUnit(vault, id, args); err != nil {
				return err
			}
			idx, err = loadV7Index(vault)
			if err != nil {
				return err
			}
		}
	}
	waveID := stringField(idx.Tasks[ids[0]].Data, "wave")
	if waveID == "" {
		return fmt.Errorf("fixture target has no wave")
	}
	for _, id := range ids {
		if stringField(idx.Tasks[id].Data, "wave") != waveID {
			return fmt.Errorf("fixture authority spans multiple waves")
		}
	}
	candidate := DepartureCandidate{CargoTaskIDs: append([]string(nil), ids...), WaveIDs: []string{waveID}, TaskStateRevisions: map[string]string{}, TaskSourceSHAs: map[string]string{}}
	for _, id := range ids {
		candidate.TaskStateRevisions[id], candidate.TaskSourceSHAs[id] = stringField(idx.Tasks[id].Data, "state_rev"), sources[id]
	}
	d, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer d.Close()
	window := time.Now().UTC().Format(time.RFC3339Nano)
	run := DepartureRun{ID: "departure-fixture-" + strings.ToLower(newRecordID()), ProjectID: v7ProjectID(vault), PolicyID: departurePolicyID(wf.Data.ScheduledPromotion.Effective), ScheduledWindow: window, State: DepartureStateStaging, Candidate: candidate}
	created, _, err := d.store.GetOrCreateDepartureRun(run)
	if err != nil {
		return err
	}
	authority, err := d.issueV7LandingAuthority(RegisteredProject{ProjectID: created.ProjectID, ProjectKey: created.ProjectID, RepoRoot: repo, VaultRoot: vault, Health: projectHealthHealthy}, wf.Data, created, candidate, v7WaveIntegrationBranch(idx.Waves[waveID]))
	if err != nil {
		return err
	}
	return landV7CmdWithDepartureAuthority(args, sources, authority)
}

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

func TestV7LandingAuthorityRejectsActorLabelAndFrozenSources(t *testing.T) {
	repo, vault := newLandTestRepo(t, 1, "true")
	source := commitLandBranch(t, repo, "task/APP-T-0001", "integration/W-0001", map[string]string{"actor-label.txt": "no authority\n"})
	err := landV7CmdWithFrozenSources(
		Args{"vault": vault, "quiet": "true", "actor": "daemon:departure:forged", "_pos0": "APP-T-0001"},
		map[string]string{"APP-T-0001": source},
	)
	if err == nil {
		t.Fatal("actor label and frozen sources minted scheduled landing authority")
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
