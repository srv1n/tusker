package main

import (
	"path/filepath"
	"sync"
	"testing"
)

func TestDepartureRunStoreCreatesUniqueWindowAndMigratesExistingDB(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.exec(`DROP TABLE departure_runs`); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	// The existing runtime database has no departure table. Reopening must add
	// it without needing a reset or a version-specific migration command.
	store, err = OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	input := DepartureRun{ProjectID: "project", PolicyID: "nightly", ScheduledWindow: "2026-07-25T19:00:00Z"}
	first, created, err := store.GetOrCreateDepartureRun(input)
	if err != nil || !created {
		t.Fatalf("first create = %#v, %v, %v", first, created, err)
	}
	if first.State != DepartureStateDue || first.StateRevision != 1 {
		t.Fatalf("unexpected created run %#v", first)
	}

	var wg sync.WaitGroup
	results := make(chan bool, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, made, createErr := store.GetOrCreateDepartureRun(input)
			results <- made
			errs <- createErr
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for made := range results {
		if made {
			t.Fatal("duplicate window was created")
		}
	}
	var count int
	if err := store.queryRowScan(`SELECT COUNT(*) FROM departure_runs WHERE project_id = ? AND policy_id = ? AND scheduled_window = ?`, []any{"project", "nightly", input.ScheduledWindow}, &count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("window rows = %d, want 1", count)
	}
}

func TestDepartureRunStoreCASPreservesWinningTerminalResult(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run, _, err := store.GetOrCreateDepartureRun(DepartureRun{ProjectID: "project", PolicyID: "policy", ScheduledWindow: "2026-07-25T19:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}

	winner := run
	winner.State = DepartureStatePassed
	winner.Promotion = DeparturePromotion{CommittedRef: "refs/heads/main", CommittedSHA: "abc", CommittedAt: "2026-07-25T19:03:00Z"}
	if updated, err := store.TransitionDepartureRun(winner, run.StateRevision); err != nil || !updated {
		t.Fatalf("winner CAS = %v, %v", updated, err)
	}

	stale := run
	stale.State = DepartureStateFailed
	stale.BlockReason = "stale writer"
	if updated, err := store.TransitionDepartureRun(stale, run.StateRevision); err != nil || updated {
		t.Fatalf("stale CAS = %v, %v", updated, err)
	}
	got, err := store.FindDepartureRun(run.ID)
	if err != nil || got == nil {
		t.Fatalf("find = %#v, %v", got, err)
	}
	if got.State != DepartureStatePassed || got.Promotion.CommittedSHA != "abc" || got.StateRevision != 2 {
		t.Fatalf("winner was lost: %#v", got)
	}
}

func TestDepartureRunStoreRecoveryDoesNotRepeatCommittedActions(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cases := []struct {
		name   string
		run    DepartureRun
		want   DepartureRecoveryDisposition
		resume DepartureState
		state  DepartureState
	}{
		{"ordinary gate resumes", DepartureRun{ProjectID: "p", PolicyID: "gate", ScheduledWindow: "one", State: DepartureStateGating}, DepartureRecoveryResumable, DepartureStateGating, DepartureStateGating},
		{"committed ref goes only to release", DepartureRun{ProjectID: "p", PolicyID: "promoted", ScheduledWindow: "two", State: DepartureStateGating, Promotion: DeparturePromotion{CommittedRef: "refs/heads/main", CommittedSHA: "abc", CommittedAt: "now"}, Release: DepartureRelease{Profile: "prod"}}, DepartureRecoveryResumable, DepartureStateReleasing, DepartureStateGating},
		{"ambiguous ref is blocked", DepartureRun{ProjectID: "p", PolicyID: "ambiguous-ref", ScheduledWindow: "three", State: DepartureStateGating, Promotion: DeparturePromotion{AttemptedAt: "now"}}, DepartureRecoveryBlocked, "", DepartureStateBlocked},
		{"ambiguous release is blocked", DepartureRun{ProjectID: "p", PolicyID: "ambiguous-release", ScheduledWindow: "four", State: DepartureStateReleasing, Promotion: DeparturePromotion{CommittedRef: "refs/heads/main", CommittedSHA: "abc"}, Release: DepartureRelease{Profile: "prod", AttemptedAt: "now"}}, DepartureRecoveryBlocked, "", DepartureStateBlocked},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run, _, createErr := store.GetOrCreateDepartureRun(tc.run)
			if createErr != nil {
				t.Fatal(createErr)
			}
			recoveries, reconcileErr := store.ReconcileDepartureRuns("p")
			if reconcileErr != nil {
				t.Fatal(reconcileErr)
			}
			var recovery *DepartureRecovery
			for i := range recoveries {
				if recoveries[i].Run.ID == run.ID {
					recovery = &recoveries[i]
					break
				}
			}
			if recovery == nil {
				t.Fatal("missing recovery")
			}
			if recovery.Disposition != tc.want || recovery.ResumeState != tc.resume {
				t.Fatalf("recovery = %#v", recovery)
			}
			got, findErr := store.FindDepartureRun(run.ID)
			if findErr != nil || got == nil {
				t.Fatalf("find = %#v, %v", got, findErr)
			}
			if got.State != tc.state {
				t.Fatalf("state = %s, want %s", got.State, tc.state)
			}
		})
	}
}

func TestDepartureRunStoreClassifiesEveryRestartPoint(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, state := range []DepartureState{DepartureStateDue, DepartureStateEvaluating, DepartureStateStaging, DepartureStateGating} {
		run, _, err := store.GetOrCreateDepartureRun(DepartureRun{ProjectID: "project", PolicyID: "restart-" + string(state), ScheduledWindow: "window", State: state})
		if err != nil {
			t.Fatal(err)
		}
		recovery := classifyDepartureRecovery(run)
		if recovery.Disposition != DepartureRecoveryResumable || recovery.ResumeState != state {
			t.Fatalf("%s restart classification = %#v", state, recovery)
		}
	}
	committed, _, err := store.GetOrCreateDepartureRun(DepartureRun{
		ProjectID: "project", PolicyID: "restart-promoted", ScheduledWindow: "window", State: DepartureStatePromoted,
		Promotion: DeparturePromotion{CommittedRef: "refs/heads/main", CommittedSHA: "abc", CommittedAt: "now"},
		Release:   DepartureRelease{Profile: "production"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if recovery := classifyDepartureRecovery(committed); recovery.Disposition != DepartureRecoveryResumable || recovery.ResumeState != DepartureStateReleasing {
		t.Fatalf("committed promotion restart classification = %#v", recovery)
	}
	released := committed
	released.Release = DepartureRelease{Profile: "production", Status: "released", CompletedAt: "now"}
	if recovery := classifyDepartureRecovery(released); recovery.Disposition != DepartureRecoveryResumable || recovery.ResumeState != DepartureStatePassed {
		t.Fatalf("completed release restart classification = %#v", recovery)
	}
	corrupt := DepartureRun{State: DepartureStatePromoted}
	if recovery := classifyDepartureRecovery(corrupt); recovery.Disposition != DepartureRecoveryBlocked {
		t.Fatalf("unrecorded promoted ref must block, got %#v", recovery)
	}
}

func TestDepartureRunStoreRejectsMalformedPersistedPayload(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, err = store.exec(`INSERT INTO departure_runs (id, project_id, policy_id, scheduled_window, state, state_revision, candidate_json, gate_json, promotion_json, release_json, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, "bad", "p", "policy", "window", "due", 1, "{", "{}", "{}", "{}", "now", "now")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.FindDepartureRun("bad"); err == nil {
		t.Fatalf("malformed JSON was accepted: %v", err)
	}
}
