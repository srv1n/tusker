package main

import (
	"strings"
	"testing"
	"time"
)

func TestMailboxAskWaitDelivery(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.UpsertProject(RegisteredProject{ProjectID: "app", ProjectKey: "app", Name: "app", RepoRoot: t.TempDir(), VaultRoot: t.TempDir(), Enabled: true, Health: projectHealthHealthy}); err != nil {
		t.Fatal(err)
	}
	d := &Daemon{store: store}
	run := RunStatus{ProjectID: "app", RecordID: "task-1", ItemID: "task-1", WorkRevision: 1, LeaseGeneration: 1, LeaseState: string(LeaseStateReleased), Terminal: true, AttemptOutcome: string(AttemptOutcomeWaitingForHuman)}
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	q, _, err := store.PutAgentMessage(AgentMessage{ProjectID: "app", IdempotencyKey: "q", Sender: "task:task-1", Recipient: AgentAddress{Kind: "task", ID: "architect"}, OriginTaskID: "task-1", WorkRevision: 1, RouteGeneration: 1, Kind: "question", Body: "Choose", ReplyRequired: true, YieldSender: true})
	if err != nil {
		t.Fatal(err)
	}
	if open, err := d.openYieldQuestion(run); err != nil || !open {
		t.Fatalf("open question = %v, %v", open, err)
	}
	a, _, err := store.PutAgentMessage(AgentMessage{ProjectID: "app", IdempotencyKey: "a", Sender: "task:architect", Recipient: AgentAddress{Kind: "task", ID: "task-1"}, Kind: "answer", Body: "Use A", ReplyTo: q.ID})
	if err != nil {
		t.Fatal(err)
	}
	if open, err := d.openYieldQuestion(run); err != nil || open {
		t.Fatalf("answered question still open = %v, %v", open, err)
	}
	if err := d.processAgentWakeups("app"); err != nil {
		t.Fatal(err)
	}
	wake, _, err := store.QueueAgentWakeup("app", AgentAddress{Kind: "task", ID: "task-1"}, "answer", "message:"+a.ID, []string{a.ID})
	if err != nil || wake.State != "scheduled" {
		t.Fatalf("answer wake = %#v, %v", wake, err)
	}
	updated, err := store.FindRunScoped("app", "task-1")
	if err != nil || updated.LeaseState != string(LeaseStateRetryQueued) {
		t.Fatalf("run = %#v, %v", updated, err)
	}
	if err := d.processAgentWakeups("app"); err != nil {
		t.Fatal(err)
	}
	if _, duplicate, err := store.PutAgentMessage(AgentMessage{ProjectID: "app", IdempotencyKey: "a", Sender: "task:architect", Recipient: AgentAddress{Kind: "task", ID: "task-1"}, Kind: "answer", Body: "Use A", ReplyTo: q.ID}); err != nil || !duplicate {
		t.Fatalf("duplicate = %v, %v", duplicate, err)
	}
}

func TestMailboxDeliveryRoutesPerHarness(t *testing.T) {
	for _, harness := range []string{"claude-code", "codex_exec", "muse", "devin_acp"} {
		t.Run(harness, func(t *testing.T) {
			store, err := OpenRuntimeStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			if err := store.UpsertProject(RegisteredProject{ProjectID: "app", ProjectKey: "app", Name: "app", RepoRoot: t.TempDir(), VaultRoot: t.TempDir(), Enabled: true, Health: projectHealthHealthy}); err != nil {
				t.Fatal(err)
			}
			live := RunStatus{ProjectID: "app", RecordID: "task", ItemID: "task", WorkRevision: 1, LeaseGeneration: 1, RunnerHarness: harness, LeaseState: string(LeaseStateRunning), LeaseExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}
			if err := store.UpsertRun(live); err != nil {
				t.Fatal(err)
			}
			q, _, err := store.PutAgentMessage(AgentMessage{ProjectID: "app", IdempotencyKey: "q", Sender: "task:task", Recipient: AgentAddress{Kind: "operator", ID: "operator"}, OriginTaskID: "task", WorkRevision: 1, RouteGeneration: 1, Kind: "question", Body: "Choose"})
			if err != nil {
				t.Fatal(err)
			}
			a, _, err := store.PutAgentMessage(AgentMessage{ProjectID: "app", IdempotencyKey: "a", Sender: "operator:operator", Recipient: AgentAddress{Kind: "task", ID: "task"}, Kind: "answer", Body: "A", ReplyTo: q.ID})
			if err != nil {
				t.Fatal(err)
			}
			w, _, err := store.QueueAgentWakeup("app", a.Recipient, "answer", "message:"+a.ID, []string{a.ID})
			if err != nil {
				t.Fatal(err)
			}
			if err := (&Daemon{store: store}).processAgentWakeups("app"); err != nil {
				t.Fatal(err)
			}
			var state string
			if err := store.queryRowScan(`SELECT state FROM agent_wakeups WHERE id=?`, []any{w.ID}, &state); err != nil {
				t.Fatal(err)
			}
			if state != "held" {
				t.Fatalf("non-yield answer interrupted %s: %s", harness, state)
			}
			got, err := store.AgentMessage("app", a.ID)
			if err != nil || got.ConsumedAt != "" {
				t.Fatalf("answer consumed before worker read: %#v %v", got, err)
			}
			claimed, err := store.ConsumeAgentMessage("app", a.ID)
			if err != nil || !claimed {
				t.Fatalf("first worker read: %v %v", claimed, err)
			}
			claimed, err = store.ConsumeAgentMessage("app", a.ID)
			if err != nil || claimed {
				t.Fatalf("duplicate worker read: %v %v", claimed, err)
			}
		})
	}
}

func TestMailboxNoHardSayDuringInlineWait(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.UpsertProject(RegisteredProject{ProjectID: "app", ProjectKey: "app", Name: "app", RepoRoot: t.TempDir(), VaultRoot: t.TempDir(), Enabled: true, Health: projectHealthHealthy}); err != nil {
		t.Fatal(err)
	}
	run := RunStatus{ProjectID: "app", RecordID: "task", ItemID: "task", WorkRevision: 1, LeaseGeneration: 1, RunnerHarness: "codex_exec", LeaseState: string(LeaseStateRunning), LeaseExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	q, _, err := store.PutAgentMessage(AgentMessage{ProjectID: "app", IdempotencyKey: "q", Sender: "task:task", Recipient: AgentAddress{Kind: "operator", ID: "operator"}, OriginTaskID: "task", WorkRevision: 1, RouteGeneration: 1, Kind: "question", Body: "Choose", YieldSender: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.markAgentQuestionAwaiting("app", q.ID, time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if waiting, err := store.agentQuestionAwaiting("app", q.ID, time.Now().Add(29*time.Second)); err != nil || !waiting {
		t.Fatalf("grace lost: %v %v", waiting, err)
	}
	if waiting, err := store.agentQuestionAwaiting("app", q.ID, time.Now().Add(32*time.Second)); err != nil || waiting {
		t.Fatalf("grace never expired: %v %v", waiting, err)
	}
	a, _, err := store.PutAgentMessage(AgentMessage{ProjectID: "app", IdempotencyKey: "a", Sender: "operator:operator", Recipient: AgentAddress{Kind: "task", ID: "task"}, Kind: "answer", Body: "A", ReplyTo: q.ID})
	if err != nil {
		t.Fatal(err)
	}
	w, _, err := store.QueueAgentWakeup("app", a.Recipient, "answer", "message:"+a.ID, []string{a.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := (&Daemon{store: store}).processAgentWakeups("app"); err != nil {
		t.Fatal(err)
	}
	var state string
	if err := store.queryRowScan(`SELECT state FROM agent_wakeups WHERE id=?`, []any{w.ID}, &state); err != nil {
		t.Fatal(err)
	}
	if state != "held" {
		t.Fatalf("inline wait interrupted by hard say: %s", state)
	}
}

func TestRegisteredSessionWakeupBackoff(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	w, _, err := store.QueueAgentWakeup("app", AgentAddress{Kind: "execution", ID: "direct"}, "answer", "one", []string{"message"})
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, 15 * time.Minute} {
		claim := store.ClaimAgentWakeup(w.ID)
		if claim == "" {
			t.Fatalf("claim %d refused", i)
		}
		before := time.Now()
		if err := store.SetAgentWakeupClaimState(w.ID, claim, "held"); err != nil {
			t.Fatal(err)
		}
		var due string
		if err := store.queryRowScan(`SELECT claimed_at FROM agent_wakeups WHERE id=?`, []any{w.ID}, &due); err != nil {
			t.Fatal(err)
		}
		at, err := time.Parse(time.RFC3339Nano, due)
		if err != nil || at.Before(before.Add(want)) || at.After(time.Now().Add(want+time.Second)) {
			t.Fatalf("backoff %d: %s %v", i, due, err)
		}
		if store.ClaimAgentWakeup(w.ID) != "" {
			t.Fatalf("claim %d retried early", i)
		}
		_, err = store.exec(`UPDATE agent_wakeups SET claimed_at=? WHERE id=?`, time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano), w.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestMailboxPromptDelivery(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	q, _, err := store.PutAgentMessage(AgentMessage{ProjectID: "app", IdempotencyKey: "q", Sender: "task:task", Recipient: AgentAddress{Kind: "operator", ID: "operator"}, OriginTaskID: "task", WorkRevision: 2, Kind: "question", Body: "Choose"})
	if err != nil {
		t.Fatal(err)
	}
	a, _, err := store.PutAgentMessage(AgentMessage{ProjectID: "app", IdempotencyKey: "a", Sender: "operator:operator", Recipient: AgentAddress{Kind: "task", ID: "task"}, Kind: "answer", Body: "Use A", ReplyTo: q.ID})
	if err != nil {
		t.Fatal(err)
	}
	prompt, ids, err := store.claimTaskAnswerPrompt("app", "task", 2, "attempt-1")
	if err != nil || len(ids) != 1 || ids[0] != a.ID || !strings.Contains(prompt, "Use A") {
		t.Fatalf("prompt=%q ids=%#v err=%v", prompt, ids, err)
	}
	store.finishTaskAnswerPrompt("app", "attempt-1", ids, false)
	prompt, ids, err = store.claimTaskAnswerPrompt("app", "task", 2, "attempt-2")
	if err != nil || len(ids) != 1 || !strings.Contains(prompt, "Use A") {
		t.Fatalf("retry prompt=%q ids=%#v err=%v", prompt, ids, err)
	}
	store.finishTaskAnswerPrompt("app", "attempt-2", ids, true)
	prompt, ids, err = store.claimTaskAnswerPrompt("app", "task", 2, "attempt-3")
	if err != nil || len(ids) != 0 || prompt != "" {
		t.Fatalf("duplicate prompt=%q ids=%#v err=%v", prompt, ids, err)
	}
}
