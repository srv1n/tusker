package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// drainWakes returns the project wakes queued on the daemon's control loop.
func drainWakes(d *Daemon) map[string]bool {
	woken := map[string]bool{}
	for {
		select {
		case id := <-d.notifyWake:
			d.takeProjectReconcile(id)
			woken[id] = true
		default:
			return woken
		}
	}
}

// 1. A live runner is judged by the poll's stall rule, not a fixed 120 s, and
// the event fires when the run actually stalls later.
func TestLivenessStallUsesProjectRule(t *testing.T) {
	d := &Daemon{}
	now := time.Now().UTC()
	wf := defaultWorkflow()
	wf.Codex.StallTimeoutMS = int((10 * time.Minute).Milliseconds())
	run := RunStatus{
		ProjectID: "p", RecordID: "APP-T-0001", Runner: string(RunnerCodexExec),
		LeaseState: string(LeaseStateRunning), ProcessPID: os.Getpid(),
		LeaseExpiresAt:   now.Add(time.Hour).Format(time.RFC3339),
		ProcessStartedAt: now.Add(-20 * time.Minute).Format(time.RFC3339),
		FirstEventAt:     now.Add(-19 * time.Minute).Format(time.RFC3339),
		LastHeartbeatAt:  now.Add(-3 * time.Minute).Format(time.RFC3339),
	}
	if d.runEventDue(run, &wf, now) {
		t.Fatal("a run silent for 3 minutes fired under a 10-minute stall timeout")
	}
	later := now.Add(8 * time.Minute)
	if !d.runEventDue(run, &wf, later) {
		t.Fatal("the run did not fire once its silence passed the stall timeout")
	}
	if d.runEventDue(run, &wf, later.Add(reconcileLiveCadence)) {
		t.Fatal("an unchanged stalled run fired again")
	}

	// A runner that never produced a first event stalls at the first-event deadline.
	fresh := run
	fresh.RecordID, fresh.FirstEventAt, fresh.LastHeartbeatAt = "APP-T-0002", "", ""
	fresh.ProcessStartedAt = now.Format(time.RFC3339)
	plain := defaultWorkflow()
	plain.Codex.StallTimeoutMS = 0 // falls back to daemonFirstEventDeadline
	if d.runEventDue(fresh, &plain, now.Add(3*time.Minute)) {
		t.Fatal("a runner fired before its first-event deadline")
	}
	if !d.runEventDue(fresh, &plain, now.Add(daemonFirstEventDeadline+time.Minute)) {
		t.Fatal("a runner with no first event past the deadline did not fire")
	}
}

// 2. When the landing worker frees, the project it skipped is polled, and so is
// the project it landed for.
func TestLivenessLandingWorkerWakesProjects(t *testing.T) {
	env := newReviewPassEnv(t, map[string]string{"reviewed.txt": "reviewed\n"}, []string{"reviewed.txt"})
	env.submitProposal(t, "pass")
	d := env.daemon
	d.reviewPassAsync = true
	d.reviewPassLanding.Store(true) // another project's landing holds the worker
	env.passHandler(t)
	drainWakes(d)
	d.releaseReviewPassLanding()
	if woken := drainWakes(d); !woken[env.project.ProjectID] {
		t.Fatalf("the skipped project was not woken when the worker freed: %v", woken)
	}

	env.passHandler(t)
	deadline := time.Now().Add(time.Minute)
	for d.reviewPassLanding.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	if woken := drainWakes(d); !env.landed() || !woken[env.project.ProjectID] {
		t.Fatalf("the finished landing did not wake its project: landed=%v wakes=%v", env.landed(), woken)
	}
}

func TestLivenessDepartureWorkerWakesProject(t *testing.T) {
	fixture := newDepartureExecutionFixture(t, scheduledPromotionShadow, "wake.txt")
	d, err := NewDaemon(fixture.stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	createDepartureExecutionRun(t, d.store, fixture)
	d.departurePlan = func(RegisteredProject, Workflow) (DepartureDecision, error) {
		return DepartureDecision{}, errors.New("plan failed")
	}
	previous := departureExecutionLog
	departureExecutionLog = func(string, string, string, string) {}
	defer func() { departureExecutionLog = previous }()
	if err := d.startPendingDepartureExecutions(context.Background(), fixture.project, fixture.wf); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if drainWakes(d)[fixture.project.ProjectID] {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the finished departure worker did not wake its project")
}

// 3. After a restart the in-memory frontier is empty; an armed wave with no
// runs still gets its project rescanned and kept active.
func TestLivenessArmedWaveSurvivesRestart(t *testing.T) {
	d, project, vault := evdProject(t, false)
	path := filepath.Join(vault, "work", "waves", "W-0001.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	wave, err := serializeDocument(map[string]any{
		"schema": "tusker.wave/v7", "kind": "wave", "id": "W-0001", "status": "active",
		"authorization": "armed", "members": []any{"APP-T-0001"},
	}, "\n# Wave\n", v7FrontmatterOrder["wave"])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(wave), 0o600); err != nil {
		t.Fatal(err)
	}
	if d.projectHasArmedWork(project.ProjectID) {
		t.Fatal("fixture: a fresh daemon already had a frontier")
	}
	if err := d.startupReconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !d.projectHasArmedWork(project.ProjectID) || d.adaptiveReconcileStatus(project.ProjectID).Tier != "active" {
		t.Fatalf("startup did not rescan the armed project: tier=%s", d.adaptiveReconcileStatus(project.ProjectID).Tier)
	}
	if _, err := d.heartbeatProjectsDue(time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if d.adaptiveReconcileStatus(project.ProjectID).Tier != "active" {
		t.Fatal("the heartbeat dropped the armed project to idle")
	}
}

// 4. A claimed run whose lease expired with no process makes its own project due.
func TestLivenessExpiredLeaseWithoutPIDIsDue(t *testing.T) {
	d, project, _ := evdProject(t, true)
	run, err := d.store.FindRunScoped(project.ProjectID, "APP-T-0001")
	if err != nil || run == nil {
		t.Fatalf("find run: %v", err)
	}
	now := time.Now().UTC()
	run.LeaseExpiresAt = now.Add(-time.Minute).Format(time.RFC3339)
	run.LastHeartbeatAt = now.Add(-time.Hour).Format(time.RFC3339)
	run.ProcessPID, run.ProcessPGID = 0, 0
	if err := d.store.UpsertRun(*run); err != nil {
		t.Fatal(err)
	}
	due, err := d.heartbeatProjectsDue(now)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := due[project.ProjectID]; !ok {
		t.Fatalf("an expired lease with no process did not make its project due: %v", due)
	}
	if again, _ := d.heartbeatProjectsDue(now.Add(reconcileLiveCadence)); len(again) != 0 {
		t.Fatalf("the unchanged expired lease fired again: %v", again)
	}
}

// 5. Agent commits move other branch refs; only HEAD and the integration
// branch (loose or packed) count as an outside edit.
func TestLivenessAgentCommitIsNotAHandEdit(t *testing.T) {
	repo := t.TempDir()
	gitDir := filepath.Join(repo, ".git")
	write := func(rel, text string) {
		t.Helper()
		path := filepath.Join(gitDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("HEAD", "ref: refs/heads/main\n")
	write("refs/heads/main", "aaaa\n")
	project := RegisteredProject{ProjectID: "p", RepoRoot: repo, VaultRoot: filepath.Join(repo, ".tusker")}
	base := projectEditSignature(project)
	write("refs/heads/task/APP-T-0001", "bbbb\n")
	if projectEditSignature(project) != base {
		t.Fatal("an agent branch commit read as a hand edit")
	}
	write("refs/heads/main", "cccc\n")
	moved := projectEditSignature(project)
	if moved == base {
		t.Fatal("a commit on the integration branch was missed")
	}

	if err := os.Remove(filepath.Join(gitDir, "refs", "heads", "main")); err != nil {
		t.Fatal(err)
	}
	write("packed-refs", "cccc refs/heads/main\ndddd refs/heads/task/APP-T-0002\n")
	packed := projectEditSignature(project)
	write("packed-refs", "cccc refs/heads/main\neeee refs/heads/task/APP-T-0002\n")
	if projectEditSignature(project) != packed {
		t.Fatal("a packed agent ref read as a hand edit")
	}
	write("packed-refs", "ffff refs/heads/main\neeee refs/heads/task/APP-T-0002\n")
	if projectEditSignature(project) == packed {
		t.Fatal("a packed integration ref change was missed")
	}
}

// 6. Freed capacity with several waiting projects polls them all together.
func TestLivenessFreedCapacityPollsAllWaitingProjects(t *testing.T) {
	stateRoot := t.TempDir()
	d, err := NewDaemon(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	now := time.Now().UTC()
	register := func(name string) string {
		repo := filepath.Join(t.TempDir(), name)
		vault := filepath.Join(repo, ".tusker")
		if err := os.MkdirAll(vault, 0o755); err != nil {
			t.Fatal(err)
		}
		project := newRegisteredProject(repo, vault)
		project.Enabled, project.Health = true, projectHealthHealthy
		if err := d.store.UpsertProject(project); err != nil {
			t.Fatal(err)
		}
		if err := d.store.UpsertRun(RunStatus{
			ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", Lane: runLaneExecute,
			LeaseState: string(LeaseStateUnclaimed), UpdatedAt: now.Format(time.RFC3339),
		}); err != nil {
			t.Fatal(err)
		}
		return project.ProjectID
	}
	first := register("one")
	d.recordPollSchedule(first, now)
	due, err := d.heartbeatProjectsDue(now)
	if err != nil {
		t.Fatal(err)
	}
	if _, global := due["*"]; global {
		t.Fatalf("one waiting project asked for a registry poll: %v", due)
	}
	second := register("two")
	d.recordPollSchedule(second, now)
	d.heartbeatCapacity = 1 // a run just finished
	due, err = d.heartbeatProjectsDue(now)
	if err != nil {
		t.Fatal(err)
	}
	if _, global := due["*"]; !global {
		t.Fatalf("freed capacity with two waiting projects did not poll both: %v", due)
	}
}
