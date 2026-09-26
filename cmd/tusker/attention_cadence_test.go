package main

import (
	"testing"
	"time"
)

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
