package main

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRuntimePollQueriesKeepProjectHistoryAndGlobalCapacity(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, run := range []RunStatus{
		{ProjectID: "a", RecordID: "a-history", Terminal: true, LeaseState: "released"},
		{ProjectID: "a", RecordID: "a-active", LeaseState: "running"},
		{ProjectID: "b", RecordID: "b-active", LeaseState: "claimed"},
		{ProjectID: "b", RecordID: "b-legacy-live", Terminal: true, LeaseState: "live"},
		{ProjectID: "b", RecordID: "b-history", Terminal: true, LeaseState: "released"},
		{ProjectID: "", RecordID: "unattributed-active", LeaseState: "claimed"},
	} {
		if err := store.UpsertRun(run); err != nil {
			t.Fatal(err)
		}
	}
	// Unrelated completed history must not even be decoded on a targeted poll.
	if _, err := store.exec(`UPDATE runs SET infrastructure_json = 'invalid historical json' WHERE record_id = 'b-history'`); err != nil {
		t.Fatal(err)
	}
	assertIDs := func(runs []RunStatus, want ...string) {
		t.Helper()
		got := map[string]bool{}
		for _, run := range runs {
			got[run.RecordID] = true
		}
		if len(got) != len(want) {
			t.Fatalf("run IDs = %v, want %v", got, want)
		}
		for _, id := range want {
			if !got[id] {
				t.Fatalf("missing %s in %v", id, got)
			}
		}
	}
	runs, err := store.ListRunsForPoll("a")
	if err != nil {
		t.Fatal(err)
	}
	assertIDs(runs, "a-history", "a-active", "b-active", "b-legacy-live", "unattributed-active")
	runs, err = store.ListReconcileRuns("")
	if err != nil {
		t.Fatal(err)
	}
	assertIDs(runs, "a-active", "b-active", "b-legacy-live", "unattributed-active")
	runs, err = store.ListReconcileRuns("a")
	if err != nil {
		t.Fatal(err)
	}
	assertIDs(runs, "a-active")
	runs, err = store.ListRunsForProject("a")
	if err != nil {
		t.Fatal(err)
	}
	assertIDs(runs, "a-history", "a-active")
	if _, err := store.exec(`UPDATE runs SET infrastructure_json = '' WHERE record_id = 'b-history'`); err != nil {
		t.Fatal(err)
	}
	runs, err = store.ListRunsForPoll("")
	if err != nil {
		t.Fatal(err)
	}
	assertIDs(runs, "a-history", "a-active", "b-active", "b-legacy-live", "b-history", "unattributed-active")
}

func TestLatestExternalApplyEventScopeSurvivesMigration(t *testing.T) {
	root := t.TempDir()
	store, err := OpenRuntimeStore(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range []ExternalLoopEvent{
		{EventID: "event-a", JobID: "\u2003job-a\u2003", Stage: " External-Collected ", Action: " PATCH ", PayloadJSON: `{"work_revision":"2"}`, CreatedAt: "2026-10-15T00:00:01Z"},
		{EventID: "event-b-a", JobID: "job-b", PayloadJSON: `{"work_revision":2}`, CreatedAt: "2026-10-15T00:00:02Z"},
		{EventID: "event-b-z", JobID: "job-b", PayloadJSON: `{"work_revision":2}`, CreatedAt: "2026-10-15T00:00:02Z"},
		{EventID: "event-blocked", JobID: "job-a", Status: " blocked ", PayloadJSON: `{"work_revision":2}`, CreatedAt: "2026-10-15T00:00:03Z"},
		{EventID: "event-invalid", JobID: "job-a", PayloadJSON: `{broken`, CreatedAt: "2026-10-15T00:00:04Z"},
		{EventID: "event-revision-3", JobID: "job-a", PayloadJSON: `{"work_revision":3}`, CreatedAt: "2026-10-15T00:00:05Z"},
		{EventID: "event-notes", JobID: "job-a", Action: "record_research", PayloadJSON: `{"work_revision":2}`, CreatedAt: "2026-10-15T00:00:06Z"},
		{EventID: "event-zero", JobID: "job-zero", CreatedAt: "2026-10-15T00:00:07Z"},
		{EventID: "event-null", JobID: "job-zero", PayloadJSON: `null`, CreatedAt: "2026-10-15T00:00:08Z"},
	} {
		event.ProjectID, event.RecordID = "project", "task"
		event.Stage = firstNonEmpty(event.Stage, externalLoopStageCollected)
		event.Action = firstNonEmpty(event.Action, externalLoopActionApplyPatch)
		event.Status = firstNonEmpty(event.Status, "ok")
		event.IdempotencyKey = event.EventID
		if _, _, err := store.SaveExternalLoopEvent(event); err != nil {
			store.Close()
			t.Fatal(err)
		}
	}
	assertSelection := func(store *RuntimeStore) {
		t.Helper()
		for _, tc := range []struct {
			revision  int
			job, want string
		}{
			{2, "", "event-b-z"}, {2, "job-a", "event-a"}, {2, "job-b", "event-b-z"},
			{2, "missing", ""}, {0, "job-zero", "event-zero"},
		} {
			event, err := currentExternalApplyEvent(store, "project", "task", RunStatus{WorkRevision: tc.revision, CloudTaskID: tc.job})
			if err != nil {
				t.Fatal(err)
			}
			got := ""
			if event != nil {
				got = event.EventID
			}
			if got != tc.want {
				t.Fatalf("revision=%d job=%q event=%q, want %q", tc.revision, tc.job, got, tc.want)
			}
		}
		if event, err := store.LatestExternalApplyEvent("other-project", "task", 2, ""); err != nil || event != nil {
			t.Fatalf("cross-project event = %#v, %v", event, err)
		}
	}
	assertSelection(store)
	before, err := store.ListExternalLoopEvents("project", "task")
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`DROP INDEX external_loop_events_apply_revision`, `DROP INDEX external_loop_events_apply_job`,
		`ALTER TABLE external_loop_events DROP COLUMN apply_work_revision`, `ALTER TABLE external_loop_events DROP COLUMN apply_job_id`,
	} {
		if _, err := store.exec(stmt); err != nil {
			store.Close()
			t.Fatal(err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenRuntimeStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	assertSelection(store)
	after, err := store.ListExternalLoopEvents("project", "task")
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("migration changed audit events: before=%#v after=%#v err=%v", before, after, err)
	}
	admission, err := store.AdmitExternalLoopEvent(ExternalLoopEvent{
		ProjectID: "project", RecordID: "admitted-task", JobID: "admitted-job", Stage: "collected", Action: "apply",
		PayloadJSON: `{"work_revision":4}`,
	}, ExternalLoopCaps{MaxCycles: 10, MaxExternalThreads: 10, MaxRepairContinuations: 10, WallClockTimeoutHours: 8}, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	latest, err := store.LatestExternalApplyEvent("project", "admitted-task", 4, "admitted-job")
	if err != nil || latest == nil || latest.EventID != admission.Event.EventID {
		t.Fatalf("admission did not populate apply lookup: %#v, %v", latest, err)
	}
}

func TestPendingAgentWakeupQueriesPreserveBackoffAndRecovery(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	for _, tc := range []struct{ project, state, claimed string }{
		{"queued", "queued", ""}, {"due", "held", now.Add(-time.Minute).Format(time.RFC3339Nano)},
		{"future", "held", now.Add(time.Hour).Format(time.RFC3339Nano)}, {"outside-change", "held", ""},
		{"recovery", "delivering", now.Add(-2 * time.Minute).Format(time.RFC3339Nano)},
		{"delivering", "delivering", now.Format(time.RFC3339Nano)}, {"delivered", "delivered", ""},
	} {
		wake, _, err := store.QueueAgentWakeup(tc.project, AgentAddress{Kind: "task", ID: "worker"}, "message", tc.project, []string{"message"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.exec(`UPDATE agent_wakeups SET state=?, claimed_at=? WHERE id=?`, tc.state, tc.claimed, wake.ID); err != nil {
			t.Fatal(err)
		}
	}
	projects, err := store.PendingAgentWakeupProjects()
	if err != nil || !reflect.DeepEqual(projects, []string{"due", "queued"}) {
		t.Fatalf("pending projects=%v, err=%v", projects, err)
	}
	wakeups, err := store.ListQueuedAgentWakeups("")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, wake := range wakeups {
		got[wake.ProjectID] = true
	}
	if !reflect.DeepEqual(got, map[string]bool{"queued": true, "due": true, "outside-change": true, "recovery": true}) {
		t.Fatalf("delivery/recovery projects=%v", got)
	}
	wakeups, err = store.ListQueuedAgentWakeups("due")
	if err != nil || len(wakeups) != 1 || wakeups[0].ProjectID != "due" {
		t.Fatalf("project-scoped wakeups=%#v, err=%v", wakeups, err)
	}
}

func TestRuntimePollingQueryPlansUseScopedIndexes(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, tc := range []struct {
		name, query, index string
		args               []any
		noSort             bool
	}{
		{"project", `SELECT ` + runtimeRunColumns + ` FROM runs WHERE project_id = ? ORDER BY updated_at DESC, item_id LIMIT 101`, "runs_poll_project", []any{"project"}, true},
		{"reconcile", `SELECT ` + runtimeRunColumns + ` FROM runs WHERE terminal = 0 OR lease_state = 'live'`, "runs_reconcile", nil, false},
		{"wakeups", `SELECT DISTINCT project_id FROM agent_wakeups WHERE state='queued' OR (state='held' AND claimed_at<>'' AND claimed_at<=?) ORDER BY project_id`, "agent_wakeups_pending", []any{"2026-10-15T00:00:00Z"}, false},
		{"questions", `SELECT id FROM agent_messages WHERE project_id=? AND kind='question' AND answered_at='' ORDER BY created_at,id`, "agent_messages_open_questions", []any{"project"}, true},
		{"session", `SELECT session_ref FROM sessions WHERE project_id=? AND record_id=? AND runner=? ORDER BY last_seen_at DESC, started_at DESC LIMIT 1`, "sessions_latest", []any{"project", "task", "runner"}, true},
		{"apply revision", `SELECT event_id FROM external_loop_events WHERE project_id=? AND record_id=? AND apply_work_revision=? AND apply_work_revision<>'' ORDER BY created_at DESC, event_id DESC LIMIT 1`, "external_loop_events_apply_revision", []any{"project", "task", "2"}, true},
		{"apply job", `SELECT event_id FROM external_loop_events WHERE project_id=? AND record_id=? AND apply_work_revision=? AND apply_work_revision<>'' AND apply_job_id=? ORDER BY created_at DESC, event_id DESC LIMIT 1`, "external_loop_events_apply_job", []any{"project", "task", "2", "job"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := store.query("EXPLAIN QUERY PLAN "+tc.query, tc.args...)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			var details []string
			for rows.Next() {
				var id, parent, unused int
				var detail string
				if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
					t.Fatal(err)
				}
				details = append(details, detail)
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			plan := strings.Join(details, "\n")
			if !strings.Contains(plan, tc.index) || (tc.noSort && strings.Contains(plan, "USE TEMP B-TREE")) {
				t.Fatalf("poll query lacks indexed ordered lookup %s:\n%s", tc.index, plan)
			}
		})
	}
	if _, err := store.exec(`DROP INDEX agent_wakeups_pending`); err != nil {
		t.Fatal(err)
	}
	if store.runtimeSchemaComplete() {
		t.Fatal("schema marker accepted missing polling index")
	}
	if err := store.Migrate(); err != nil {
		t.Fatal(err)
	}
	if !store.runtimeSchemaComplete() {
		t.Fatal("migration did not restore polling index")
	}
}
