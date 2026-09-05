package main

import (
	"sync"
	"testing"
	"time"
)

func TestTrustDagLeases(t *testing.T) {
	t.Run("armed branching frontier", func(t *testing.T) {
		index := newProjectFrontierIndex("project-1")
		index.rebuild([]Note{
			frontierTestNote("W-1", "wave", map[string]any{"authorization": "armed", "members": []any{"A", "B", "C"}}),
			frontierTestNote("A", "task", map[string]any{"status": "ready", "wave": "W-1"}),
			frontierTestNote("B", "task", map[string]any{"status": "ready", "wave": "W-1"}),
			frontierTestNote("C", "task", map[string]any{"status": "ready", "wave": "W-1", "dependencies": []any{"A:hard"}}),
		})
		assertEqual(t, []string{"A", "B"}, index.Frontier, "independent armed starts")
		assertEqual(t, "dependency A", index.Eligibility["C"].Blocker, "dependent blocked before prerequisite")
		index.apply([]Note{frontierTestNote("A", "task", map[string]any{"status": "done", "wave": "W-1"})})
		assertEqual(t, []string{"B", "C"}, index.Frontier, "sibling survives and dependent becomes eligible")
	})

	t.Run("one racing claim and stale fence", func(t *testing.T) {
		store, err := OpenRuntimeStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		now := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
		var wg sync.WaitGroup
		results := make([]struct {
			lease ResourceLease
			ok    bool
			err   error
		}, 2)
		for i := range results {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				results[i].lease, results[i].ok, results[i].err = store.AcquireResourceLease(ResourceLeaseAcquireInput{
					Name: "path:cmd/tusker/daemon.go", Owner: "attempt-" + string(rune('a'+i)), Purpose: "dispatch", ProjectID: "project-1", TTL: time.Minute, Now: now,
				})
			}(i)
		}
		wg.Wait()
		wins := 0
		var first ResourceLease
		for _, result := range results {
			if result.ok {
				wins++
				first = result.lease
			} else if result.err == nil {
				t.Fatal("losing race returned neither contention nor error")
			}
		}
		if wins != 1 {
			t.Fatalf("racing claims=%d results=%#v", wins, results)
		}
		second, ok, err := store.AcquireResourceLease(ResourceLeaseAcquireInput{
			Name: first.Name, Owner: "attempt-reassigned", Purpose: "dispatch", ProjectID: "project-1", TTL: time.Minute, Now: now.Add(2 * time.Minute),
		})
		if err != nil || !ok || second.Generation <= first.Generation {
			t.Fatalf("expired lease reassignment=%#v ok=%t err=%v", second, ok, err)
		}
		if matched, err := store.ResourceLeaseMatchesAt(first.Name, first.Owner, first.Generation, now.Add(2*time.Minute)); err != nil || matched {
			t.Fatalf("stale holder retained commit fence: matched=%t err=%v", matched, err)
		}
		if released, err := store.ReleaseResourceLease(first.Name, first.Owner, first.Generation, "stale worker", now.Add(2*time.Minute)); err != nil || released {
			t.Fatalf("stale holder released reassigned lease: released=%t err=%v", released, err)
		}
	})

	t.Run("all authored collision surfaces serialize", func(t *testing.T) {
		now := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
		for _, tc := range []struct {
			name, field, value, code string
		}{
			{"owned path", "owned_paths", "cmd/tusker", "OWNED_PATH_CONFLICT"},
			{"generated output", "generated_outputs", "internal/serve/ui/dist/app.js", "GENERATED_OUTPUT_CONFLICT"},
			{"migration key", "migration_keys", "runtime-v8", "MIGRATION_KEY_CONFLICT"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				candidate := Note{Data: map[string]any{"id": "candidate", tc.field: []string{tc.value}}}
				holder := Note{Data: map[string]any{"id": "holder", tc.field: []string{tc.value}}}
				conflict, found := ownedPathConflict(candidate, map[string]Note{"candidate": candidate, "holder": holder}, []RunStatus{{ItemID: "holder", LeaseOwner: "holder-attempt", LeaseState: string(LeaseStateRunning), LeaseExpiresAt: now.Add(time.Minute).Format(time.RFC3339)}}, now)
				if !found || conflict["code"] != tc.code || conflict["ownership_kind"] == "" {
					t.Fatalf("%s collision escaped: %#v found=%t", tc.name, conflict, found)
				}
			})
		}
	})
}
