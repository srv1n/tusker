package main

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDepartureSchedulerOptOutDoesNoWork(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	called := false
	d := &Daemon{store: store, departurePlan: func(RegisteredProject, Workflow) (DepartureDecision, error) {
		called = true
		return DepartureDecision{}, nil
	}}
	wf := defaultWorkflow()
	wf.Orchestration.BatchGate.Windows = []string{"13:00"}
	if err := d.scheduleDepartureIfDue(RegisteredProject{ProjectID: "opt-out"}, wf, time.Date(2026, 7, 25, 13, 0, 0, 0, time.Local)); err != nil {
		t.Fatal(err)
	}
	runs, err := store.ListDepartureRuns("opt-out")
	if err != nil {
		t.Fatal(err)
	}
	if called || len(runs) != 0 || len(d.departureProjectsDue(time.Now())) != 0 {
		t.Fatalf("opt-out performed departure work: called=%v runs=%#v schedules=%#v", called, runs, d.departureSchedules)
	}
}

func TestDepartureSchedulerUsesSharedDailyWindowClock(t *testing.T) {
	wf := defaultWorkflow()
	wf.ScheduledPromotion.Effective = scheduledPromotionProjection(ScheduledPromotionPolicy{Mode: scheduledPromotionShadow}, true, "test")
	wf.Orchestration.BatchGate.Windows = []string{"19:00", "03:00", "13:00"}
	windows, err := departureWindows(wf)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 25, 13, 5, 0, 0, time.Local)
	if got, want := mergeWindowNext(windows, now), time.Date(2026, 7, 25, 19, 0, 0, 0, time.Local); !got.Equal(want) {
		t.Fatalf("shared clock next=%s want=%s", got, want)
	}
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	d := &Daemon{store: store}
	if err := d.refreshDepartureSchedule("app", wf, now); err != nil {
		t.Fatal(err)
	}
	if wait := d.nextDepartureWait(now, 24*time.Hour); wait != 5*time.Hour+55*time.Minute {
		t.Fatalf("next scheduler wake=%s", wait)
	}
}

func TestDepartureSchedulerHoldsAreDurableAndExplainResume(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	hold, err := store.SetDepartureHold("app", false, "operator maintenance", "human:sara", now)
	if err != nil {
		t.Fatal(err)
	}
	if hold.ResumeAction != "tusker departure resume --project app --by <actor>" {
		t.Fatalf("resume action=%q", hold.ResumeAction)
	}
	loaded, err := store.departureHold("app", false)
	if err != nil || loaded == nil || loaded.By != "human:sara" || loaded.Reason != "operator maintenance" {
		t.Fatalf("durable hold=%#v err=%v", loaded, err)
	}
	if err := store.ClearDepartureHold("app", false); err != nil {
		t.Fatal(err)
	}
	loaded, err = store.departureHold("app", false)
	if err != nil || loaded != nil {
		t.Fatalf("cleared hold=%#v err=%v", loaded, err)
	}
	release, err := store.SetDepartureHold("", true, "release freeze", "human:sara", now)
	if err != nil || release.ResumeAction != "tusker departure resume --release-only --by <actor>" {
		t.Fatalf("release hold=%#v err=%v", release, err)
	}
	global, err := store.SetDepartureHold("", false, "global stop", "human:sara", now)
	if err != nil {
		t.Fatal(err)
	}
	if loaded, err := store.departureHold("another-project", false); err != nil || loaded == nil || loaded.Reason != global.Reason {
		t.Fatalf("global hold=%#v err=%v", loaded, err)
	}
}

func TestDepartureSchedulerHoldAndResumeCLIIsRealAndAttributable(t *testing.T) {
	stateRoot := t.TempDir()
	holdOut := captureStdout(t, func() {
		if err := departureHoldCmd(Args{"state-root": stateRoot, "project": "app", "reason": "maintenance", "by": "human:sara", "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var held struct {
		OK   bool          `json:"ok"`
		Hold DepartureHold `json:"hold"`
	}
	if err := json.Unmarshal([]byte(holdOut), &held); err != nil {
		t.Fatal(err)
	}
	if !held.OK || held.Hold.ResumeAction != "tusker departure resume --project app --by <actor>" {
		t.Fatalf("hold output=%s", holdOut)
	}
	resumeOut := captureStdout(t, func() {
		if err := departureResumeCmd(Args{"state-root": stateRoot, "project": "app", "by": "human:lee", "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var resumed struct {
		OK      bool          `json:"ok"`
		Resumed DepartureHold `json:"resumed"`
	}
	if err := json.Unmarshal([]byte(resumeOut), &resumed); err != nil {
		t.Fatal(err)
	}
	if !resumed.OK || resumed.Resumed.ClearedBy != "human:lee" || resumed.Resumed.ClearedAt == "" {
		t.Fatalf("resume output=%s", resumeOut)
	}
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if active, err := store.departureHold("app", false); err != nil || active != nil {
		t.Fatalf("resumed hold remains active: %#v err=%v", active, err)
	}
	raw, err := store.GetSetting(departureHoldSetting("app", false))
	if err != nil || !strings.Contains(raw, `"cleared_by":"human:lee"`) {
		t.Fatalf("resume audit was not retained: %q err=%v", raw, err)
	}
}

func TestDepartureSchedulerHoldBlocksFutureDecisionWithoutPlanning(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.SetDepartureHold("app", false, "maintenance", "human:sara", time.Now()); err != nil {
		t.Fatal(err)
	}
	called := false
	d := &Daemon{store: store, departurePlan: func(RegisteredProject, Workflow) (DepartureDecision, error) {
		called = true
		return DepartureDecision{}, nil
	}}
	wf := defaultWorkflow()
	wf.ScheduledPromotion.Effective = scheduledPromotionProjection(ScheduledPromotionPolicy{Mode: scheduledPromotionPromote}, true, "test")
	wf.Orchestration.BatchGate.Windows = []string{"13:00"}
	if err := d.scheduleDepartureIfDue(RegisteredProject{ProjectID: "app"}, wf, time.Date(2026, 7, 25, 13, 0, 0, 0, time.Local)); err != nil {
		t.Fatal(err)
	}
	runs, err := store.ListDepartureRuns("app")
	if err != nil || called || len(runs) != 1 || runs[0].State != DepartureStateBlocked || runs[0].BlockReason == "" {
		t.Fatalf("hold decision called=%v runs=%#v err=%v", called, runs, err)
	}
}

func TestDepartureSchedulerMisfireAndIdempotency(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	project := RegisteredProject{ProjectID: "app"}
	late := time.Date(2026, 7, 25, 14, 30, 0, 0, time.Local)

	stage := defaultWorkflow()
	stage.ScheduledPromotion.Effective = scheduledPromotionProjection(ScheduledPromotionPolicy{Mode: scheduledPromotionStage}, true, "test")
	stage.Orchestration.BatchGate.Windows = []string{"13:00", "19:00"}
	d := &Daemon{store: store}
	if err := d.scheduleDepartureIfDue(project, stage, late); err != nil {
		t.Fatal(err)
	}
	runs, err := store.ListDepartureRuns(project.ProjectID)
	if err != nil || len(runs) != 1 || runs[0].State != DepartureStateSkipped || runs[0].SkipReason == "" {
		t.Fatalf("stage misfire runs=%#v err=%v", runs, err)
	}

	promote := stage
	promote.ScheduledPromotion.Effective = scheduledPromotionProjection(ScheduledPromotionPolicy{Mode: scheduledPromotionPromote}, true, "test")
	plans := 0
	d.departurePlan = func(RegisteredProject, Workflow) (DepartureDecision, error) {
		plans++
		return DepartureDecision{Disposition: "ready", Reasons: []DepartureReason{{Code: "eligible_cargo", Message: "ready"}}, Candidate: DepartureCandidate{}, GateIntent: DepartureGate{Status: "required"}}, nil
	}
	if err := d.scheduleDepartureIfDue(project, promote, late); err != nil {
		t.Fatal(err)
	}
	if err := d.scheduleDepartureIfDue(project, promote, late.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	runs, err = store.ListDepartureRuns(project.ProjectID)
	if err != nil || len(runs) != 2 || plans != 1 {
		t.Fatalf("promotion coalescing runs=%#v plans=%d err=%v", runs, plans, err)
	}
}

func TestDepartureSchedulerConcurrentAndRestartedTriggersConverge(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	wf := defaultWorkflow()
	wf.ScheduledPromotion.Effective = scheduledPromotionProjection(ScheduledPromotionPolicy{Mode: scheduledPromotionPromote}, true, "test")
	wf.Orchestration.BatchGate.Windows = []string{"13:00"}
	project := RegisteredProject{ProjectID: "app"}
	now := time.Date(2026, 7, 25, 13, 0, 0, 0, time.Local)
	d := &Daemon{store: store, departurePlan: func(RegisteredProject, Workflow) (DepartureDecision, error) {
		return DepartureDecision{Disposition: "ready", Reasons: []DepartureReason{{Message: "ready"}}}, nil
	}}
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- d.scheduleDepartureIfDue(project, wf, now)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	// A fresh daemon process sees the durable unique row and creates nothing.
	restarted := &Daemon{store: store, departurePlan: d.departurePlan}
	if err := restarted.scheduleDepartureIfDue(project, wf, now); err != nil {
		t.Fatal(err)
	}
	runs, err := store.ListDepartureRuns(project.ProjectID)
	if err != nil || len(runs) != 1 {
		t.Fatalf("runs=%#v err=%v", runs, err)
	}
}
