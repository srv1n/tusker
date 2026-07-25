package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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
	wf := Workflow{}
	if fileExists(workflowPath(vault)) {
		loaded, err := loadWorkflow(vault)
		if err != nil {
			return err
		}
		wf = loaded.Data
	} else {
		wf.ScheduledPromotion = defaultScheduledPromotionPolicy()
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
	run := DepartureRun{ID: "departure-fixture-" + strings.ToLower(newRecordID()), ProjectID: v7ProjectID(vault), PolicyID: departurePolicyID(wf.ScheduledPromotion.Effective), ScheduledWindow: window, State: DepartureStateStaging, Candidate: candidate}
	created, _, err := d.store.GetOrCreateDepartureRun(run)
	if err != nil {
		return err
	}
	authority, err := d.issueV7LandingAuthority(RegisteredProject{ProjectID: created.ProjectID, ProjectKey: created.ProjectID, RepoRoot: repo, VaultRoot: vault, Health: projectHealthHealthy}, wf, created, candidate, v7WaveIntegrationBranch(idx.Waves[waveID]))
	if err != nil {
		return err
	}
	return landV7CmdWithDepartureAuthority(args, sources, authority)
}

// These are focused trust-boundary tests: receipt/index JSON and daemon-looking
// labels are not authority, while a real issuance remains verifiable through
// the exact daemon store that owns it.
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

func TestV7LandingAuthorityVerificationUsesIssuingDaemonStore(t *testing.T) {
	repo, vault := newLandTestRepo(t, 1, "true")
	t.Setenv("TUSKER_STATE_ROOT", filepath.Join(t.TempDir(), "unrelated-default"))
	store, err := OpenRuntimeStore(filepath.Join(t.TempDir(), "issuing-daemon"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	source := gitRevisionForTest(t, repo, "integration/W-0001")
	window := time.Now().UTC().Format(time.RFC3339Nano)
	candidate := DepartureCandidate{
		CargoTaskIDs: []string{"APP-T-0001"}, WaveIDs: []string{"W-0001"},
		TaskStateRevisions: map[string]string{"APP-T-0001": "task-state"},
		TaskSourceSHAs:     map[string]string{"APP-T-0001": source},
	}
	run, _, err := store.GetOrCreateDepartureRun(DepartureRun{
		ID: "departure-custom-root", ProjectID: "app", PolicyID: "test-policy",
		ScheduledWindow: window, State: DepartureStateStaging, Candidate: candidate,
	})
	if err != nil {
		t.Fatal(err)
	}
	daemon := &Daemon{store: store}
	authority, err := daemon.issueV7LandingAuthority(
		RegisteredProject{ProjectID: "app", RepoRoot: repo, VaultRoot: vault},
		Workflow{}, run, candidate, "integration/W-0001",
	)
	if err != nil {
		t.Fatal(err)
	}
	issuance := authority.Issuance
	receipt := v7LandingReceipt{
		Schema: v7LandingReceiptSchema, Actor: "daemon:departure:" + run.ID,
		ControlAuthority: v7LandingAuthorityDeparture, Target: "integration/W-0001",
		ReceiptIssuedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Tasks:           []v7LandingReceiptTask{{Task: "APP-T-0001", SourceSHA: source}},
		ProjectID:       issuance.ProjectID, RepoIdentity: issuance.RepoIdentity, DepartureID: issuance.DepartureID,
		PolicyID: issuance.PolicyID, ScheduledWindow: issuance.ScheduledWindow,
		DaemonSessionID: issuance.SessionID, DaemonHost: issuance.HostIdentity, DaemonProcess: issuance.ProcessIdentity,
		AuthorityID: issuance.AuthorityID, AuthorityGen: issuance.Generation,
	}
	receipt.Fingerprint = v7LandingReceiptFingerprint(receipt)
	receipt.AuthoritySignature = ed25519.Sign(authority.private, []byte(receipt.Fingerprint))

	if verifyV7LandingReceiptAuthority(repo, receipt) {
		t.Fatal("unrelated default runtime store authenticated another daemon's issuance")
	}
	if !verifyV7LandingReceiptAuthorityWithStore(repo, receipt, store) {
		t.Fatal("issuing daemon store did not authenticate its own signed receipt")
	}
}

func TestV7LandingRepoIdentitySurvivesRemoteConfiguration(t *testing.T) {
	repo, _ := newLandTestRepo(t, 1, "true")
	before, err := v7LandingRepoIdentity(repo)
	if err != nil {
		t.Fatal(err)
	}
	runGitDir(t, repo, "remote", "add", "origin", filepath.Join(t.TempDir(), "first.git"))
	afterAdd, err := v7LandingRepoIdentity(repo)
	if err != nil {
		t.Fatal(err)
	}
	runGitDir(t, repo, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "second.git"))
	afterMigration, err := v7LandingRepoIdentity(repo)
	if err != nil {
		t.Fatal(err)
	}
	if before != afterAdd || before != afterMigration {
		t.Fatalf("mutable remote configuration changed landing receipt repository identity: before=%s after_add=%s after_migration=%s", before, afterAdd, afterMigration)
	}
}

func TestV7LandingAuthorityRejectsForgedWaveDrainReceiptIndex(t *testing.T) {
	repo, vault := newLandTestRepo(t, 1, "true")
	t.Setenv("TUSKER_STATE_ROOT", filepath.Join(t.TempDir(), "state"))
	source := commitLandBranch(t, repo, "task/APP-T-0001", "integration/W-0001", map[string]string{"wave-drain-forgery.txt": "forged\n"})
	setDepartureTaskSourceForTest(t, vault, "APP-T-0001", source)
	if err := landV7Cmd(Args{"vault": vault, "quiet": "true", "actor": "agent:test", "_pos0": "APP-T-0001"}); err != nil {
		t.Fatal(err)
	}
	wave, err := resolveV7Note(vault, "W-0001", "wave")
	if err != nil {
		t.Fatal(err)
	}
	var original v7LandingReceipt
	for _, row := range normalizeLandingAudit(wave.Data["landings"]) {
		if stringField(row, "task") != "APP-T-0001" {
			continue
		}
		original, _ = loadV7LandingReceipt(vault, stringField(row, "receipt_fingerprint"))
		break
	}
	if original.Fingerprint == "" {
		t.Fatal("forgery fixture did not create a discoverable ordinary receipt")
	}
	forged := original
	forged.Actor = "daemon:wave-drain"
	forged.ControlAuthority = v7LandingAuthorityWaveDrain
	forged.Fingerprint = v7LandingReceiptFingerprint(forged)
	forged.AuthoritySignature = make([]byte, ed25519.SignatureSize)
	if err := writeV7LandingReceipt(vault, forged); err != nil {
		t.Fatal(err)
	}
	indexed := indexedV7LandingReceipts(vault, "integration/W-0001", "APP-T-0001", source)
	if len(indexed) == 0 || indexed[0].Fingerprint != forged.Fingerprint {
		t.Fatalf("forged public receipt was not exercised through the universal index: %#v", indexed)
	}
	if trustedV7LandingControlAuthority(forged.ControlAuthority, forged.Actor) {
		t.Fatal("wave-drain actor label minted trusted landing authority")
	}
	if _, recovered := recoverV7LandingAuditFromReceipt(
		vault, repo, "integration/W-0001",
		v7LandTask{ID: "APP-T-0001", Branch: "task/APP-T-0001", SourceSHA: source},
		nil,
	); recovered {
		t.Fatal("forged wave-drain receipt/index recovered a control-plane landing audit")
	}
}

func TestV7LandingAuthorityCandidateBindsWaveSubsetWithoutWideningSources(t *testing.T) {
	all := DepartureCandidate{CargoTaskIDs: []string{"APP-T-0001", "APP-T-0002"}, WaveIDs: []string{"W-0001", "W-0002"}, TaskStateRevisions: map[string]string{"APP-T-0001": "one", "APP-T-0002": "two"}, TaskSourceSHAs: map[string]string{"APP-T-0001": "source-one", "APP-T-0002": "source-two"}}
	subset := v7LandingAuthorityCandidate(all, []string{"APP-T-0002"})
	if len(subset.CargoTaskIDs) != 1 || subset.CargoTaskIDs[0] != "APP-T-0002" || len(subset.TaskSourceSHAs) != 1 || subset.TaskSourceSHAs["APP-T-0002"] != "source-two" || subset.TaskSourceSHAs["APP-T-0001"] != "" {
		t.Fatalf("authority subset widened frozen cargo: %#v", subset)
	}
}

func TestV7LandingAuthorityRequiresSandboxForScheduledGate(t *testing.T) {
	original := landingGateSandboxPath
	defer func() { landingGateSandboxPath = original }()
	landingGateSandboxPath = func() (string, error) { return "", os.ErrNotExist }
	if _, err := runV7LandingGateCommand(t.TempDir(), "true", true); err == nil {
		t.Fatal("unsupported host ran a scheduled gate without isolation")
	}
}

func TestV7LandingGateSandboxContract(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin sandbox-exec contract")
	}
	if _, err := landingGateSandboxPath(); err != nil {
		t.Skip("sandbox-exec unavailable: " + err.Error())
	}
	worktree := t.TempDir()
	if err := os.WriteFile(filepath.Join(worktree, "allowed.txt"), []byte("ok\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "go.mod"), []byte("module sandboxfixture\n\ngo 1.22\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "main.go"), []byte("package main\nimport \"fmt\"\nfunc main(){fmt.Println(\"ok\")}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stateRoot := filepath.Join(t.TempDir(), "runtime-state")
	if err := os.MkdirAll(stateRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(stateRoot, "authority-secret")
	if err := os.WriteFile(sentinel, []byte("must-not-read"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	for _, command := range []string{"test -f allowed.txt", "go version >/dev/null", "go run . >/dev/null", "echo gate-write > gate-output.txt"} {
		if output, err := runV7LandingGateCommand(worktree, command, true); err != nil {
			t.Fatalf("sandbox rejected required gate command %q: %v: %s", command, err, output)
		}
	}
	if _, err := os.Stat(filepath.Join(worktree, "gate-output.txt")); err != nil {
		t.Fatalf("sandbox did not permit worktree write: %v", err)
	}
	if output, err := runV7LandingGateCommand(worktree, "cat "+shellQuoteForSandboxTest(sentinel), true); err == nil {
		t.Fatalf("sandbox read runtime sentinel: %s", output)
	}
	if output, err := runV7LandingGateCommand(worktree, "echo overwrite > "+shellQuoteForSandboxTest(sentinel), true); err == nil {
		t.Fatalf("sandbox wrote runtime sentinel: %s", output)
	}
	if output, err := runV7LandingGateCommand(worktree, "curl --max-time 1 --silent http://127.0.0.1:9 >/dev/null", true); err == nil {
		t.Fatalf("sandbox allowed network: %s", output)
	}
}

func shellQuoteForSandboxTest(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
