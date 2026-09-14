package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func autonomousWaveFixture(t *testing.T, members []string, extra map[string]any) (vault string, store *RuntimeStore, project RegisteredProject) {
	t.Helper()
	vault, store, project = authorityFixture(t)
	writeDirectWave(t, vault, "W-0001", members, extra)
	return vault, store, project
}

func markDirectTaskDone(t *testing.T, vault, id string) {
	t.Helper()
	rewriteTaskFile(t, vault, id, func(data map[string]any, body string) (map[string]any, string) {
		data["status"] = "done"
		data["readiness"] = "done"
		return data, body
	})
}

func queuedDirectives(t *testing.T, store *RuntimeStore, projectID string) []RunDirective {
	t.Helper()
	directives, err := store.ListActiveRunDirectives(projectID, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	return directives
}

func TestDirectWaveAutonomousFrontierAdvancesAfterCompletion(t *testing.T) {
	vault, store, project := autonomousWaveFixture(t, []string{"APP-T-0001", "APP-T-0002"}, map[string]any{"concurrency": 1})
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	writeDirectTask(t, vault, "APP-T-0002", "W-0001", map[string]any{"dependencies": []any{"APP-T-0001:hard"}})
	writeDirectTask(t, vault, "APP-T-0003", "", nil)
	writeDirectWave(t, vault, "W-0002", []string{"APP-T-0003"}, nil)
	result, err := directWaveStart(vault, store, "W-0001", "human:sarav")
	if err != nil {
		t.Fatal(err)
	}
	if result.Authorization != "authorized" || result.State != "Waiting" || result.Replayed {
		t.Fatalf("offline start did not report authorized Waiting: %#v", result)
	}
	if len(result.QueuedTaskIDs) != 1 || result.QueuedTaskIDs[0] != "APP-T-0001" {
		t.Fatalf("start queued non-roots: %v", result.QueuedTaskIDs)
	}
	directives := queuedDirectives(t, store, project.ProjectID)
	if len(directives) != 1 || directives[0].WaveID != "W-0001" {
		t.Fatalf("unexpected directives: %#v", directives)
	}
	if got, err := queueAuthorizedWaveFrontier(vault, store, project.ProjectID, "W-0002", time.Now().UTC()); err != nil || len(got) != 0 {
		t.Fatalf("disarmed wave queued: %v err=%v", got, err)
	}
	markDirectTaskDone(t, vault, "APP-T-0001")
	daemon := &Daemon{stateRoot: DefaultStateRoot(), store: store}
	if err := daemon.advanceAuthorizedWaveFrontiers(project); err != nil {
		t.Fatal(err)
	}
	directives = queuedDirectives(t, store, project.ProjectID)
	if len(directives) != 1 || directives[0].RecordID != "APP-T-0001" {
		t.Fatalf("concurrency=1 wave released the dependent while the root directive was still active: %#v", directives)
	}
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	wave := idx.Waves["W-0001"]
	fp, at := stringField(wave.Data, "authorization_fingerprint"), stringField(wave.Data, "authorized_at")
	root := RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", Runner: string(RunnerCodexExec), Lane: runLaneExecute, LeaseState: string(LeaseStateUnclaimed)}
	if err := store.UpsertRun(root); err != nil {
		t.Fatal(err)
	}
	if err := store.SetProjectEnabled(project.ProjectID, true); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	claimed, err := store.claimRunLeaseWithDirectiveAttempt(root, "attempt-root", 1, time.Minute, now, RuntimeLeaseClaimPrecondition{ExpectedLeaseState: LeaseStateUnclaimed}, RunAuthorization{Source: "human_run_directive", Actor: "human:sarav", DirectiveWaveID: "W-0001", DirectiveAuthorizationFingerprint: fp, DirectiveWaveAuthorizedAt: at}, RunAttempt{AttemptID: "attempt-root", Runner: string(RunnerCodexExec), Lane: runLaneExecute})
	if err != nil || !claimed {
		t.Fatalf("root directive claim did not consume: claimed=%t err=%v", claimed, err)
	}
	directive, err := store.RunDirective(project.ProjectID, "APP-T-0001")
	if err != nil || directive == nil || directive.State != "consumed" {
		t.Fatalf("root directive was not consumed by the claim: %#v err=%v", directive, err)
	}
	if err := daemon.advanceAuthorizedWaveFrontiers(project); err != nil {
		t.Fatal(err)
	}
	if got := queuedDirectives(t, store, project.ProjectID); len(got) != 0 {
		t.Fatalf("concurrency=1 wave oversubscribed while the root attempt was live: %#v", got)
	}
	root.LeaseState = string(LeaseStateReleased)
	root.Terminal = true
	root.AttemptOutcome = string(AttemptOutcomeSucceeded)
	if err := store.UpsertRun(root); err != nil {
		t.Fatal(err)
	}
	if err := daemon.advanceAuthorizedWaveFrontiers(project); err != nil {
		t.Fatal(err)
	}
	directives = queuedDirectives(t, store, project.ProjectID)
	if len(directives) != 1 || directives[0].RecordID != "APP-T-0002" {
		t.Fatalf("concurrency=1 wave did not release only the dependent after root completion: %#v", directives)
	}
	dep := directives[0]
	if dep.WaveID != "W-0001" || dep.AuthorizationFingerprint != fp || dep.WaveAuthorizedAt != at {
		t.Fatalf("dependent directive is not bound to exact wave authority: %#v", dep)
	}
}

func TestDirectWaveAutonomousLateRouteRemovalDoesNotQueueNextFrontier(t *testing.T) {
	vault, store, project := autonomousWaveFixture(t, []string{"APP-T-0001", "APP-T-0002"}, nil)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	writeDirectTask(t, vault, "APP-T-0002", "W-0001", map[string]any{"dependencies": []any{"APP-T-0001:hard"}})

	if _, err := directWaveStart(vault, store, "W-0001", "human:sarav"); err != nil {
		t.Fatal(err)
	}
	if directive, err := store.RunDirective(project.ProjectID, "APP-T-0001"); err != nil || directive == nil || directive.State != "queued" {
		t.Fatalf("root directive missing after wave start: %#v err=%v", directive, err)
	}

	configPath := managedTuskerLocalConfigPath(vault)
	configText, err := readText(configPath)
	if err != nil {
		t.Fatal(err)
	}
	removedConfig := strings.Replace(configText, "test-codex-exec:", "removed-profile:", 1)
	if removedConfig == configText {
		t.Fatal("fixture config did not contain the test route")
	}
	if err := writeText(configPath, removedConfig); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = writeText(configPath, configText) }()

	markDirectTaskDone(t, vault, "APP-T-0001")
	daemon := &Daemon{stateRoot: DefaultStateRoot(), store: store}
	if err := daemon.advanceAuthorizedWaveFrontiers(project); err == nil || !strings.Contains(err.Error(), "wave route admission blocked") {
		t.Fatalf("late route removal did not block frontier admission: %v", err)
	}
	if directive, err := store.RunDirective(project.ProjectID, "APP-T-0002"); err != nil || directive != nil {
		t.Fatalf("removed route created a downstream directive: %#v err=%v", directive, err)
	}

	if err := writeText(configPath, configText); err != nil {
		t.Fatal(err)
	}
	if err := daemon.advanceAuthorizedWaveFrontiers(project); err != nil {
		t.Fatalf("restored route did not advance frontier: %v", err)
	}
	directive, err := store.RunDirective(project.ProjectID, "APP-T-0002")
	if err != nil || directive == nil || directive.State != "queued" {
		t.Fatalf("restored route did not queue downstream directive: %#v err=%v", directive, err)
	}
}

func TestDirectWaveAutonomousStartHonorsConcurrency(t *testing.T) {
	vault, store, project := autonomousWaveFixture(t, []string{"APP-T-0001", "APP-T-0002"}, map[string]any{"concurrency": 1})
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	writeDirectTask(t, vault, "APP-T-0002", "W-0001", nil)
	result, err := directWaveStart(vault, store, "W-0001", "human:sarav")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.QueuedTaskIDs) != 1 {
		t.Fatalf("start queued beyond concurrency=1: %v", result.QueuedTaskIDs)
	}
	if got, err := queueAuthorizedWaveFrontier(vault, store, project.ProjectID, "W-0001", time.Now().UTC()); err != nil || len(got) != 0 {
		t.Fatalf("re-poll oversubscribed the wave: %v err=%v", got, err)
	}
}

func TestDirectWaveAutonomousCrashBeforeQueueRecoversOnce(t *testing.T) {
	vault, store, project := autonomousWaveFixture(t, []string{"APP-T-0001", "APP-T-0002"}, nil)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	writeDirectTask(t, vault, "APP-T-0002", "W-0001", map[string]any{"dependencies": []any{"APP-T-0001:hard"}})
	directWaveStartInjectCrashBeforeQueue = func() bool { return true }
	result, err := directWaveStart(vault, store, "W-0001", "human:sarav")
	directWaveStartInjectCrashBeforeQueue = nil
	if err != nil {
		t.Fatal(err)
	}
	if result.Authorization != "authorized" || len(result.QueuedTaskIDs) != 0 {
		t.Fatalf("crashed start did not leave durable armed Waiting: %#v", result)
	}
	if got := len(queuedDirectives(t, store, project.ProjectID)); got != 0 {
		t.Fatalf("crashed start left %d directives", got)
	}
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	wave := idx.Waves["W-0001"]
	if stringField(wave.Data, "authorization") != "armed" || stringField(wave.Data, "authorization_fingerprint") == "" || stringField(wave.Data, "authorized_at") == "" {
		t.Fatalf("armed authorization was not durable: %#v", wave.Data)
	}
	recovered := &Daemon{stateRoot: DefaultStateRoot(), store: store}
	if err := recovered.advanceAuthorizedWaveFrontiers(project); err != nil {
		t.Fatal(err)
	}
	directives := queuedDirectives(t, store, project.ProjectID)
	if len(directives) != 1 || directives[0].RecordID != "APP-T-0001" || directives[0].WaveAuthorizedAt != stringField(wave.Data, "authorized_at") {
		t.Fatalf("restart recovery did not queue the armed root: %#v", directives)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, callErr := queueAuthorizedWaveFrontier(vault, store, project.ProjectID, "W-0001", time.Now().UTC())
			errs <- callErr
		}()
	}
	wg.Wait()
	close(errs)
	for callErr := range errs {
		if callErr != nil {
			t.Fatal(callErr)
		}
	}
	directives = queuedDirectives(t, store, project.ProjectID)
	if len(directives) != 1 {
		t.Fatalf("competing frontier calls produced %d directives", len(directives))
	}
}

func TestDirectWaveAutonomousPollOnceQueuesAuthorizedFrontier(t *testing.T) {
	vault, store, project := autonomousWaveFixture(t, []string{"APP-T-0001", "APP-T-0002"}, nil)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	writeDirectTask(t, vault, "APP-T-0002", "W-0001", map[string]any{"dependencies": []any{"APP-T-0001:hard"}})
	directWaveStartInjectCrashBeforeQueue = func() bool { return true }
	result, err := directWaveStart(vault, store, "W-0001", "human:sarav")
	directWaveStartInjectCrashBeforeQueue = nil
	if err != nil {
		t.Fatal(err)
	}
	if result.Authorization != "authorized" || result.State != "Waiting" || len(result.QueuedTaskIDs) != 0 {
		t.Fatalf("crashed start did not leave durable armed Waiting: %#v", result)
	}
	if err := store.SetProjectEnabled(project.ProjectID, true); err != nil {
		t.Fatal(err)
	}
	daemon, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	daemon.dispatchRefusalReason = oneShotDispatchRefusal("tusker daemon run --once")
	pollErr := daemon.PollOnce(context.Background())
	if pollErr != nil && !strings.Contains(pollErr.Error(), "cannot dispatch local runners") {
		t.Fatalf("one-shot poll returned an unexpected error: %v", pollErr)
	}
	directives := queuedDirectives(t, store, project.ProjectID)
	if len(directives) != 1 || directives[0].RecordID != "APP-T-0001" || directives[0].WaveID != "W-0001" {
		t.Fatalf("poll did not queue exactly the authorized root: %#v", directives)
	}
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	wave := idx.Waves["W-0001"]
	if directives[0].AuthorizationFingerprint != stringField(wave.Data, "authorization_fingerprint") || directives[0].WaveAuthorizedAt != stringField(wave.Data, "authorized_at") {
		t.Fatalf("poll-queued directive is not bound to exact wave authority: %#v", directives[0])
	}
}

func TestDirectWaveAutonomousPausePreservesAuthorityAndContinuity(t *testing.T) {
	vault, store, project := autonomousWaveFixture(t, []string{"APP-T-0001", "APP-T-0002"}, nil)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	writeDirectTask(t, vault, "APP-T-0002", "W-0001", map[string]any{"dependencies": []any{"APP-T-0001:hard"}})
	if _, err := directWaveStart(vault, store, "W-0001", "human:sarav"); err != nil {
		t.Fatal(err)
	}
	directive, err := store.RunDirective(project.ProjectID, "APP-T-0001")
	if err != nil || directive == nil {
		t.Fatalf("missing wave directive: %#v err=%v", directive, err)
	}
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	wave := idx.Waves["W-0001"]
	fp, at := stringField(wave.Data, "authorization_fingerprint"), stringField(wave.Data, "authorized_at")
	task := idx.Tasks["APP-T-0001"]
	run := RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", LeaseGeneration: 3, LeaseState: "running"}
	auth := &RunAuthorization{Source: "human_run_directive", Actor: "human:sarav", LeaseGeneration: 3, DirectiveWaveID: "W-0001", DirectiveAuthorizationFingerprint: fp, DirectiveWaveAuthorizedAt: at}
	if !runDirectiveMatchesTaskAuthority(vault, task, directive, time.Now().UTC()) {
		t.Fatal("queued wave directive did not match while armed")
	}
	if !runDirectiveAuthorizationMatchesTaskAuthority(vault, task, run, auth) {
		t.Fatal("admitted authorization did not match while armed")
	}
	paused, err := directWavePause(vault, store, "W-0001", "human:sarav")
	if err != nil {
		t.Fatal(err)
	}
	if paused.State != "Paused" || paused.Authorization != "paused" || !strings.Contains(paused.Reason, "admitted attempts may finish") {
		t.Fatalf("pause result: %#v", paused)
	}
	idx, err = loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	wave = idx.Waves["W-0001"]
	if stringField(wave.Data, "authorization") != "paused" || stringField(wave.Data, "authorization_fingerprint") != fp || stringField(wave.Data, "authorized_at") != at || stringField(wave.Data, "authorized_by") != "human:sarav" {
		t.Fatalf("pause rewrote authorization identity: %#v", wave.Data)
	}
	if runDirectiveMatchesTaskAuthority(vault, task, directive, time.Now().UTC()) {
		t.Fatal("queued wave directive still admits new work after pause")
	}
	if runDirectiveAuthorizationMatchesTaskAuthority(vault, task, run, auth) {
		t.Fatal("strict admission matcher accepted a paused wave")
	}
	if !runDirectiveAdmittedContinuityMatchesTaskAuthority(vault, task, run, auth) {
		t.Fatal("admitted attempt lost continuity after pause")
	}
	if !consumedRunDirectiveContinuityMatchesTaskAuthority(vault, task, run, &RunDirective{State: "consumed", Actor: "human:sarav", ExpiresAt: time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano), WaveID: "W-0001", AuthorizationFingerprint: fp, WaveAuthorizedAt: at}, auth, time.Now().UTC()) {
		t.Fatal("consumed directive lost continuity after pause")
	}
	review, err := buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
	if err != nil {
		t.Fatal(err)
	}
	if review.State != "Paused" || review.Authorization != "paused" {
		t.Fatalf("paused wave did not project Paused: %#v", review.State)
	}
	var resumeControl, taskControl *directStartControl
	for i := range review.Controls {
		switch review.Controls[i].Action {
		case "wave resume":
			resumeControl = &review.Controls[i]
		case "task start":
			if review.Controls[i].Scope == "APP-T-0001" {
				taskControl = &review.Controls[i]
			}
		}
	}
	if resumeControl == nil || !resumeControl.Enabled || resumeControl.Scope != "W-0001" {
		t.Fatalf("paused wave missing resume control: %#v", review.Controls)
	}
	if taskControl == nil || !taskControl.Enabled || !strings.Contains(taskControl.Reason, "remains paused") {
		t.Fatalf("paused wave task-scoped start control wrong: %#v", taskControl)
	}
	if _, err := directWaveStart(vault, store, "W-0001", "human:sarav"); err == nil || !strings.Contains(err.Error(), "paused") {
		t.Fatalf("wave start on paused wave: %v", err)
	}
	if got, err := queueAuthorizedWaveFrontier(vault, store, project.ProjectID, "W-0001", time.Now().UTC()); err != nil || len(got) != 0 {
		t.Fatalf("paused wave queued frontier work: %v err=%v", got, err)
	}
}

func TestDirectWaveAutonomousTaskStartInsidePausedWave(t *testing.T) {
	vault, store, project := autonomousWaveFixture(t, []string{"APP-T-0001", "APP-T-0002"}, nil)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	writeDirectTask(t, vault, "APP-T-0002", "W-0001", map[string]any{"dependencies": []any{"APP-T-0001:hard"}})
	if _, err := directWaveStart(vault, store, "W-0001", "human:sarav"); err != nil {
		t.Fatal(err)
	}
	if _, err := directWavePause(vault, store, "W-0001", "human:sarav"); err != nil {
		t.Fatal(err)
	}
	result, err := directTaskBackgroundStart(vault, store, "APP-T-0001", "human:sarav")
	if err != nil {
		t.Fatal(err)
	}
	if result.Authorization != "authorized" || len(result.QueuedTaskIDs) != 1 {
		t.Fatalf("task-scoped start inside paused wave failed: %#v", result)
	}
	directive, err := store.RunDirective(project.ProjectID, "APP-T-0001")
	if err != nil || directive == nil {
		t.Fatalf("missing directive after task start: %#v err=%v", directive, err)
	}
	if directive.WaveID != "" || directive.WaveAuthorizedAt != "" {
		t.Fatalf("directive remained wave-scoped: %#v", directive)
	}
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	task := idx.Tasks["APP-T-0001"]
	if directive.AuthorizationFingerprint != directWaveTaskContract(task) {
		t.Fatalf("task directive is not bound to the task contract: %#v", directive)
	}
	wave := idx.Waves["W-0001"]
	if stringField(wave.Data, "authorization") != "paused" {
		t.Fatal("task start un-paused the wave")
	}
	resumed, err := directWaveResume(vault, store, "W-0001", "operator:ops")
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Authorization != "authorized" {
		t.Fatalf("resume did not reauthorize: %#v", resumed)
	}
	idx, err = loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	wave = idx.Waves["W-0001"]
	if stringField(wave.Data, "authorization") != "armed" || stringField(wave.Data, "authorized_by") != "human:sarav" || stringField(wave.Data, "authorized_at") == "" {
		t.Fatalf("resume rewrote authorization identity: %#v", wave.Data)
	}
	if len(queuedDirectives(t, store, project.ProjectID)) != 1 {
		t.Fatalf("resume oversubscribed the wave: %#v", queuedDirectives(t, store, project.ProjectID))
	}
	markDirectTaskDone(t, vault, "APP-T-0001")
	daemon := &Daemon{stateRoot: DefaultStateRoot(), store: store}
	if err := daemon.advanceAuthorizedWaveFrontiers(project); err != nil {
		t.Fatal(err)
	}
	directives := queuedDirectives(t, store, project.ProjectID)
	found := false
	for _, d := range directives {
		if d.RecordID == "APP-T-0002" && d.WaveID == "W-0001" {
			found = true
		}
	}
	if !found {
		t.Fatalf("automatic progression did not resume after resume: %#v", directives)
	}
}

func TestDirectWaveAutonomousResumeRefusesStaleMaterial(t *testing.T) {
	vault, store, project := autonomousWaveFixture(t, []string{"APP-T-0001", "APP-T-0002"}, nil)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	writeDirectTask(t, vault, "APP-T-0002", "W-0001", map[string]any{"dependencies": []any{"APP-T-0001:hard"}})
	if _, err := directWaveStart(vault, store, "W-0001", "human:sarav"); err != nil {
		t.Fatal(err)
	}
	if _, err := directWavePause(vault, store, "W-0001", "human:sarav"); err != nil {
		t.Fatal(err)
	}
	rewriteTaskFile(t, vault, "APP-T-0002", func(data map[string]any, body string) (map[string]any, string) {
		return data, body + "\nAmended material.\n"
	})
	if _, err := directWaveResume(vault, store, "W-0001", "human:sarav"); err == nil || !strings.Contains(err.Error(), "changed while paused") {
		t.Fatalf("stale resume err=%v", err)
	}
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	if stringField(idx.Waves["W-0001"].Data, "authorization") != "paused" {
		t.Fatal("stale resume un-paused the wave")
	}
	for _, d := range queuedDirectives(t, store, project.ProjectID) {
		if d.RecordID == "APP-T-0002" {
			t.Fatalf("stale resume queued downstream work: %#v", d)
		}
	}
}

func TestDirectWaveAutonomousServePauseResume(t *testing.T) {
	vault, store, project := autonomousWaveFixture(t, []string{"APP-T-0001"}, nil)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	if _, err := directWaveStart(vault, store, "W-0001", "human:sarav"); err != nil {
		t.Fatal(err)
	}
	server := newServeServer(vault, project.RepoRoot, defaultServeAddr, store, nil)
	server.operatorActor = "human:test"
	var paused directStartResult
	servePost(t, server, "/api/actions/projects/"+project.ProjectID+"/waves/W-0001/pause", `{}`, &paused)
	if paused.State != "Paused" || paused.Authorization != "paused" {
		t.Fatalf("serve pause did not pause: %#v", paused)
	}
	var resumed directStartResult
	servePost(t, server, "/api/actions/projects/"+project.ProjectID+"/waves/W-0001/resume", `{}`, &resumed)
	if resumed.Authorization != "authorized" {
		t.Fatalf("serve resume did not reauthorize: %#v", resumed)
	}
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	if stringField(idx.Waves["W-0001"].Data, "authorization") != "armed" {
		t.Fatal("serve resume did not restore armed authorization")
	}
}

func TestDirectWaveAutonomousUnrelatedWaveNeverQueues(t *testing.T) {
	vault, store, project := autonomousWaveFixture(t, []string{"APP-T-0001"}, nil)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	writeDirectTask(t, vault, "APP-T-0002", "W-0002", nil)
	writeDirectWave(t, vault, "W-0002", []string{"APP-T-0002"}, nil)
	if _, err := directWaveStart(vault, store, "W-0001", "human:sarav"); err != nil {
		t.Fatal(err)
	}
	daemon := &Daemon{stateRoot: DefaultStateRoot(), store: store}
	if err := daemon.advanceAuthorizedWaveFrontiers(project); err != nil {
		t.Fatal(err)
	}
	for _, d := range queuedDirectives(t, store, project.ProjectID) {
		if d.RecordID == "APP-T-0002" || d.WaveID == "W-0002" {
			t.Fatalf("unrelated disarmed wave was queued: %#v", d)
		}
	}
	if len(queuedDirectives(t, store, project.ProjectID)) != 1 {
		t.Fatalf("unexpected directive count: %#v", queuedDirectives(t, store, project.ProjectID))
	}
}

// writeTaskFileOutOfBand rewrites a task record without refreshing its stored
// contract_fingerprint or state_rev — the direct file mutation an admission
// guard must catch.
func writeTaskFileOutOfBand(t *testing.T, vault, id string, mutate func(data map[string]any, body string) (map[string]any, string)) {
	t.Helper()
	path := filepath.Join(vault, "work", "tasks", id+".md")
	data, body, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	data, body = mutate(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(path, content); err != nil {
		t.Fatal(err)
	}
}

func waveEventFiles(t *testing.T, vault, waveID string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(vault, "events", "*", "*", waveID+"--*.json"))
	if err != nil {
		t.Fatal(err)
	}
	return matches
}

func TestDirectWaveAutonomousQueuedDirectiveRefusesOutOfBandEdit(t *testing.T) {
	vault, store, project := autonomousWaveFixture(t, []string{"APP-T-0001", "APP-T-0002"}, nil)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	writeDirectTask(t, vault, "APP-T-0002", "W-0001", map[string]any{"dependencies": []any{"APP-T-0001:hard"}})
	writeDirectTask(t, vault, "APP-T-0003", "", nil)
	if _, err := directWaveStart(vault, store, "W-0001", "human:sarav"); err != nil {
		t.Fatal(err)
	}
	if _, err := directTaskBackgroundStart(vault, store, "APP-T-0003", "human:sarav"); err != nil {
		t.Fatal(err)
	}
	// Queue-then-edit: contract bytes change out of band while the stored
	// contract_fingerprint and state_rev pins stay behind.
	for _, id := range []string{"APP-T-0001", "APP-T-0003"} {
		writeTaskFileOutOfBand(t, vault, id, func(data map[string]any, body string) (map[string]any, string) {
			return data, strings.Replace(body, "Do "+id+".", "Do something else entirely.", 1)
		})
	}
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	wfFile, err := loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	notesByID, notesByRecordID := daemonNoteMaps([]Note{idx.Tasks["APP-T-0001"], idx.Tasks["APP-T-0003"]})
	for _, id := range []string{"APP-T-0001", "APP-T-0003"} {
		task := idx.Tasks[id]
		reason := directWaveTaskContractStaleReason(task)
		if reason == "" {
			t.Fatalf("%s out-of-band edit did not stale the contract pin", id)
		}
		if runDirectiveBypassableBlocker(reason) {
			t.Fatalf("%s stale-contract reason is directive-bypassable: %q", id, reason)
		}
		directive, err := store.RunDirective(project.ProjectID, id)
		if err != nil || directive == nil || directive.State != "queued" {
			t.Fatalf("%s directive missing: %#v err=%v", id, directive, err)
		}
		if runDirectiveMatchesTaskAuthority(vault, task, directive, time.Now().UTC()) {
			t.Fatalf("%s queued directive still matches edited contract bytes", id)
		}
		if got := daemonDispatchBlockedReasonWithAuthorization(vault, task, notesByID, notesByRecordID, true); !strings.Contains(got, "contract") {
			t.Fatalf("%s daemon admission did not report stale contract: %q", id, got)
		}
	}
	if got := armedWaveDispatchBlocker(vault, idx.Tasks["APP-T-0001"], wfFile.Data, nil); !strings.Contains(got, "contract") {
		t.Fatalf("armed-wave admission did not report stale contract: %q", got)
	}
	if got := armedWaveDispatchBlocker(vault, idx.Tasks["APP-T-0003"], wfFile.Data, nil); !strings.Contains(got, "contract") {
		t.Fatalf("standalone task admission did not report stale contract: %q", got)
	}
	// The poll path must not consume, rebind, or supersede the stale work.
	daemon := &Daemon{stateRoot: DefaultStateRoot(), store: store}
	if err := daemon.advanceAuthorizedWaveFrontiers(project); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"APP-T-0001", "APP-T-0003"} {
		directive, err := store.RunDirective(project.ProjectID, id)
		if err != nil || directive == nil {
			t.Fatalf("%s directive missing after poll: %v", id, err)
		}
		if directive.State != "queued" {
			t.Fatalf("%s directive was consumed under stale contract: %#v", id, directive)
		}
	}
}

func TestDirectWaveAutonomousLedgerWritesDoNotStaleContract(t *testing.T) {
	vault, store, project := authorityFixture(t)
	writeDirectTask(t, vault, "APP-T-0001", "", nil)
	if _, err := directTaskBackgroundStart(vault, store, "APP-T-0001", "human:sarav"); err != nil {
		t.Fatal(err)
	}
	directive, err := store.RunDirective(project.ProjectID, "APP-T-0001")
	if err != nil || directive == nil || directive.State != "queued" {
		t.Fatalf("missing queued directive: %#v err=%v", directive, err)
	}
	// Sanctioned lifecycle appends: a CAS write refreshes state_rev while the
	// contract pin stays — evidence, work log, generated findings, verification
	// results, and the typed-review receipt are ledger, not contract.
	writeTaskFileOutOfBand(t, vault, "APP-T-0001", func(data map[string]any, body string) (map[string]any, string) {
		body = strings.Replace(body, "| A1 | command: go test ./x | pending | |",
			"| A1 | command: go test ./x | pass | recorded |\n| A1 | typed review execute | pass | [tusker-review-result:abc123] |", 1)
		body += "\n## Evidence\n\n- proof: ran go test ./x\n\n## Work log\n\n- progress noted\n"
		body += "\n## Reviewer findings\n\n" + reviewerFindingGeneratedMarker + "\n\n- rework requested: tighten the check\n"
		data["state_rev"] = v7StateRev(data, body)
		return data, body
	})
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	task := idx.Tasks["APP-T-0001"]
	if reason := directWaveTaskContractStaleReason(task); reason != "" {
		t.Fatalf("sanctioned ledger writes staled the contract: %q", reason)
	}
	if !runDirectiveMatchesTaskAuthority(vault, task, directive, time.Now().UTC()) {
		t.Fatal("queued directive stopped matching after ledger appends")
	}
	// A stale state_rev alone must still refuse admission.
	writeTaskFileOutOfBand(t, vault, "APP-T-0001", func(data map[string]any, body string) (map[string]any, string) {
		return data, body + "\n## Work log\n\n- another note\n"
	})
	idx, err = loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	if reason := directWaveTaskContractStaleReason(idx.Tasks["APP-T-0001"]); !strings.Contains(reason, "state_rev") {
		t.Fatalf("unrefreshed state_rev was not caught: %q", reason)
	}
}

func TestDirectWaveAutonomousStartReauthorizesStalePausedWave(t *testing.T) {
	vault, store, project := autonomousWaveFixture(t, []string{"APP-T-0001", "APP-T-0002"}, nil)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	writeDirectTask(t, vault, "APP-T-0002", "W-0001", map[string]any{"dependencies": []any{"APP-T-0001:hard"}})
	if _, err := directWaveStart(vault, store, "W-0001", "human:sarav"); err != nil {
		t.Fatal(err)
	}
	if _, err := directWavePause(vault, store, "W-0001", "human:sarav"); err != nil {
		t.Fatal(err)
	}
	// A sanctioned member edit rebinds the task pin but stales the wave
	// authorization: Resume refuses, so Start is the supported recovery.
	rewriteTaskFile(t, vault, "APP-T-0002", func(data map[string]any, body string) (map[string]any, string) {
		return data, body + "\nAmended material.\n"
	})
	if _, err := directWaveResume(vault, store, "W-0001", "human:sarav"); err == nil || !strings.Contains(err.Error(), "changed while paused") {
		t.Fatalf("stale resume err=%v", err)
	}
	result, err := directWaveStart(vault, store, "W-0001", "operator:ops")
	if err != nil {
		t.Fatalf("stale paused wave start: %v", err)
	}
	if result.Authorization != "authorized" || result.State != "Waiting" {
		t.Fatalf("reauthorization result=%#v", result)
	}
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	wave := idx.Waves["W-0001"]
	newFP, newAt := stringField(wave.Data, "authorization_fingerprint"), stringField(wave.Data, "authorized_at")
	if stringField(wave.Data, "authorization") != "armed" || newFP == "" || newAt == "" || stringField(wave.Data, "authorized_by") != "operator:ops" {
		t.Fatalf("stale paused wave was not reauthorized: %#v", wave.Data)
	}
	// The frontier directive queued under the superseded authorization must be
	// re-bound to the new one — it can never match the old fingerprint again.
	directive, err := store.RunDirective(project.ProjectID, "APP-T-0001")
	if err != nil || directive == nil || directive.State != "queued" {
		t.Fatalf("frontier directive missing after reauthorization: %#v err=%v", directive, err)
	}
	if directive.AuthorizationFingerprint != newFP || directive.WaveAuthorizedAt != newAt {
		t.Fatalf("queued directive was not re-bound to the new authorization: %#v", directive)
	}
	task := idx.Tasks["APP-T-0001"]
	if !runDirectiveMatchesTaskAuthority(vault, task, directive, time.Now().UTC()) {
		t.Fatal("re-bound frontier directive does not admit under the new authorization")
	}
	events := waveEventFiles(t, vault, "W-0001")
	var replaced string
	found := false
	for _, path := range events {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var event map[string]any
		if err := json.Unmarshal(raw, &event); err != nil {
			t.Fatal(err)
		}
		payload, _ := event["payload"].(map[string]any)
		if payload["authorization"] == "armed" && stringField(payload, "replaced_authorization") != "" {
			found = true
			replaced = stringField(payload, "replaced_authorization")
		}
	}
	if !found || replaced != "paused" {
		t.Fatalf("replacement authorization event missing or wrong: found=%t replaced=%q events=%v", found, replaced, events)
	}
}

func TestDirectWaveAutonomousStartCommitsWaveAndEventAtomically(t *testing.T) {
	vault, store, _ := autonomousWaveFixture(t, []string{"APP-T-0001"}, nil)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	wavePath := filepath.Join(vault, "work", "waves", "W-0001.md")
	before, err := os.ReadFile(wavePath)
	if err != nil {
		t.Fatal(err)
	}
	directWaveStartInjectCommitFailAfter = 1
	_, err = directWaveStart(vault, store, "W-0001", "human:sarav")
	directWaveStartInjectCommitFailAfter = 0
	if err == nil {
		t.Fatal("injected commit failure did not surface")
	}
	after, err := os.ReadFile(wavePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("wave document was committed despite the transaction failure")
	}
	if events := waveEventFiles(t, vault, "W-0001"); len(events) != 0 {
		t.Fatalf("audit event survived the rolled-back transaction: %v", events)
	}
	result, err := directWaveStart(vault, store, "W-0001", "human:sarav")
	if err != nil {
		t.Fatalf("retry after rolled-back start failed: %v", err)
	}
	if result.Authorization != "authorized" {
		t.Fatalf("retry result=%#v", result)
	}
	events := waveEventFiles(t, vault, "W-0001")
	if len(events) != 1 {
		t.Fatalf("retry did not land exactly one audit event: %v", events)
	}
	raw, err := os.ReadFile(events[0])
	if err != nil {
		t.Fatal(err)
	}
	var event map[string]any
	if err := json.Unmarshal(raw, &event); err != nil {
		t.Fatal(err)
	}
	payload, _ := event["payload"].(map[string]any)
	if payload["authorization"] != "armed" {
		t.Fatalf("audit event does not record the armed authorization: %s", raw)
	}
}
