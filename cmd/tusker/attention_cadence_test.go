package main

import (
	"testing"
	"time"
)

func TestAttentionCadenceBoostsOnlyScopedSubscriberProject(t *testing.T) {
	t.Parallel()
	broker := newServeStreamBroker()
	d := &Daemon{stream: broker}
	_, unsubscribeGlobal, ok := broker.Subscribe()
	if !ok {
		t.Fatal("expected global stream subscription")
	}
	defer unsubscribeGlobal()
	_, unsubscribeAlpha, ok := broker.SubscribeProject("alpha")
	if !ok {
		t.Fatal("expected scoped stream subscription")
	}
	defer unsubscribeAlpha()

	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	if due := d.attentionProjectsDue(now); len(due) != 1 || due[0] != "alpha" {
		t.Fatalf("attention due=%v, want alpha only", due)
	}
	d.attentionLastPoll["alpha"] = now
	if due := d.attentionProjectsDue(now.Add(attentionProjectPollCadence - time.Second)); len(due) != 0 {
		t.Fatalf("alpha polled before attention cadence: %v", due)
	}
	if due := d.attentionProjectsDue(now.Add(attentionProjectPollCadence)); len(due) != 1 || due[0] != "alpha" {
		t.Fatalf("alpha did not become due at attention cadence: %v", due)
	}
}

func TestAttentionCadenceExpiresOnScopedDisconnect(t *testing.T) {
	t.Parallel()
	broker := newServeStreamBroker()
	d := &Daemon{stream: broker}
	_, unsubscribe, ok := broker.SubscribeProject("alpha")
	if !ok {
		t.Fatal("expected scoped stream subscription")
	}
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	d.attentionLastPoll = map[string]time.Time{"alpha": now.Add(-attentionProjectPollCadence)}
	unsubscribe()
	if due := d.attentionProjectsDue(now); len(due) != 0 {
		t.Fatalf("disconnected project remained attention-boosted: %v", due)
	}
	if _, retained := d.attentionLastPoll["alpha"]; retained {
		t.Fatal("disconnect did not clear attention cadence state")
	}
}

func TestAttentionCadenceScopedStreamFiltersOtherProjectEvents(t *testing.T) {
	t.Parallel()
	broker := newServeStreamBroker()
	ch, unsubscribe, ok := broker.SubscribeProject("alpha")
	if !ok {
		t.Fatal("expected scoped stream subscription")
	}
	defer unsubscribe()
	broker.Broadcast(serveStreamEvent{Kind: "projection_refreshed", Project: "beta", Keys: []string{"tasks"}})
	broker.Broadcast(serveStreamEvent{Kind: "projection_refreshed", Project: "alpha", Keys: []string{"tasks"}})
	select {
	case event := <-ch:
		if event.Project != "alpha" {
			t.Fatalf("scoped stream received wrong project: %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("scoped stream did not receive target project event")
	}
}
