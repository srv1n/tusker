package main

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestResourceLeaseAtomicContentionAndUnrelatedResources(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 7, 25, 10, 0, 0, 0, time.UTC)
	first, ok, err := store.AcquireResourceLease(ResourceLeaseAcquireInput{Name: "host:cargo", Owner: "a", Purpose: "full gate", ProjectID: "project-a", DepartureID: "departure-a", TTL: time.Minute, Now: now})
	if err != nil || !ok || first.Generation != 1 {
		t.Fatalf("first acquire: %#v %v %v", first, ok, err)
	}
	_, ok, err = store.AcquireResourceLease(ResourceLeaseAcquireInput{Name: "host:cargo", Owner: "b", Purpose: "full gate", ProjectID: "project-b", DepartureID: "departure-b", TTL: time.Minute, Now: now.Add(time.Second)})
	var refusal *TuskerError
	if ok || !errors.As(err, &refusal) || refusal.Code != resourceLeaseRefusal || !strings.Contains(refusal.Hint, "wait for the holder") {
		t.Fatalf("contention must have stable cause/remedy: ok=%v err=%v", ok, err)
	}
	_, ok, err = store.AcquireResourceLease(ResourceLeaseAcquireInput{Name: "host:cargo", Owner: "a", Purpose: "full gate", ProjectID: "project-b", DepartureID: "departure-b", TTL: time.Minute, Now: now.Add(time.Second)})
	if ok || !errors.As(err, &refusal) || refusal.Code != resourceLeaseRefusal {
		t.Fatalf("same owner label in another project bypassed contention: ok=%v err=%v", ok, err)
	}
	if matches, err := store.ResourceLeaseMatchesAt(first.Name, first.Owner, first.Generation, now.Add(30*time.Second)); err != nil || !matches {
		t.Fatalf("fresh holder failed fence: %v %v", matches, err)
	}
	if matches, err := store.ResourceLeaseMatchesAt(first.Name, first.Owner, first.Generation, now.Add(2*time.Minute)); err != nil || matches {
		t.Fatalf("expired holder retained fence: %v %v", matches, err)
	}
	if renewed, err := store.RenewResourceLease(ResourceLeaseRenewal{Name: first.Name, Owner: first.Owner, Generation: first.Generation, TTL: time.Minute, Now: now.Add(2 * time.Minute)}); err != nil || renewed {
		t.Fatalf("expired holder was resurrected: %v %v", renewed, err)
	}
	reacquired, ok, err := store.AcquireResourceLease(ResourceLeaseAcquireInput{Name: first.Name, Owner: first.Owner, Purpose: first.Purpose, ProjectID: first.ProjectID, DepartureID: first.DepartureID, TTL: time.Minute, Now: now.Add(2 * time.Minute)})
	if err != nil || !ok || reacquired.Generation != first.Generation+1 {
		t.Fatalf("expired same-label holder did not receive a new fence: %#v ok=%v err=%v", reacquired, ok, err)
	}
	if matches, err := store.ResourceLeaseMatchesAt(first.Name, first.Owner, first.Generation, now.Add(2*time.Minute)); err != nil || matches {
		t.Fatalf("expired generation survived same-label reacquire: matches=%v err=%v", matches, err)
	}
	other, ok, err := store.AcquireResourceLease(ResourceLeaseAcquireInput{Name: "release:staging", Owner: "b", Purpose: "release", ProjectID: "project-b", DepartureID: "departure-b", TTL: time.Minute, Now: now})
	if err != nil || !ok || other.Generation != 1 {
		t.Fatalf("unrelated resource should remain concurrent: %#v %v %v", other, ok, err)
	}
}

func TestResourceLeaseAtomicAcrossRuntimeStores(t *testing.T) {
	stateRoot := t.TempDir()
	first, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	now := time.Date(2026, 7, 25, 10, 0, 0, 0, time.UTC)
	stores := []*RuntimeStore{first, second}
	var wg sync.WaitGroup
	results := make(chan bool, len(stores))
	for index, store := range stores {
		wg.Add(1)
		go func(index int, store *RuntimeStore) {
			defer wg.Done()
			_, acquired, err := store.AcquireResourceLease(ResourceLeaseAcquireInput{Name: "host:cargo", Owner: fmt.Sprintf("owner-%d", index), Purpose: "gate", ProjectID: fmt.Sprintf("project-%d", index), TTL: time.Minute, Now: now})
			if err != nil && !strings.Contains(err.Error(), "held by") {
				t.Errorf("concurrent acquire: %v", err)
			}
			results <- acquired
		}(index, store)
	}
	wg.Wait()
	close(results)
	winners := 0
	for acquired := range results {
		if acquired {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("atomic acquire must have one winner, got %d", winners)
	}
}

func TestResourceLeaseTakeoverIsFencedAndAttributable(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 7, 25, 10, 0, 0, 0, time.UTC)
	first, ok, err := store.AcquireResourceLease(ResourceLeaseAcquireInput{Name: "host:xcode", Owner: "a", Purpose: "gate", ProjectID: "project-a", TTL: time.Minute, Now: now})
	if err != nil || !ok {
		t.Fatal(err)
	}
	_, ok, err = store.AcquireResourceLease(ResourceLeaseAcquireInput{Name: "host:xcode", Owner: "b", Purpose: "gate", ProjectID: "project-b", TTL: time.Minute, Now: now.Add(2 * time.Minute), HolderAlive: func(ResourceLease) bool { return true }})
	var refusal *TuskerError
	if ok || !errors.As(err, &refusal) || refusal.Code != resourceLeaseRefusal || !strings.Contains(fmt.Sprint(refusal.Context), "lease_expired_holder_alive") {
		t.Fatalf("live expired holder must block: ok=%v err=%v", ok, err)
	}
	second, ok, err := store.AcquireResourceLease(ResourceLeaseAcquireInput{Name: "host:xcode", Owner: "b", Purpose: "gate", ProjectID: "project-b", TTL: time.Minute, Now: now.Add(2 * time.Minute)})
	if err != nil || !ok || second.Generation != first.Generation+1 {
		t.Fatalf("takeover: %#v %v %v", second, ok, err)
	}
	if released, err := store.ReleaseResourceLease(first.Name, first.Owner, first.Generation, "old terminal outcome", now.Add(2*time.Minute)); err != nil || released {
		t.Fatalf("stale holder released takeover: %v %v", released, err)
	}
	if matches, err := store.ResourceLeaseMatches(first.Name, first.Owner, first.Generation); err != nil || matches {
		t.Fatalf("stale holder must fail generation fence: %v %v", matches, err)
	}
	events, err := store.ListResourceLeaseEvents(first.Name)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[1].EventType != "taken_over" || events[1].PreviousOwner != "a" || events[1].PreviousGeneration != 1 {
		t.Fatalf("missing attributable takeover: %#v", events)
	}
}

func TestResourceLeaseTerminalReleaseAndRestartRecovery(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 7, 25, 10, 0, 0, 0, time.UTC)
	lease, ok, err := store.AcquireResourceLease(ResourceLeaseAcquireInput{Name: "release:prod", Owner: "a", Purpose: "release", ProjectID: "project-a", DepartureID: "dep-a", TTL: time.Minute, Now: now})
	if err != nil || !ok {
		t.Fatal(err)
	}
	if released, err := store.ReleaseResourceLease(lease.Name, lease.Owner, lease.Generation, "release passed", now.Add(time.Second)); err != nil || !released {
		t.Fatalf("terminal release: %v %v", released, err)
	}
	lease, ok, err = store.AcquireResourceLease(ResourceLeaseAcquireInput{Name: lease.Name, Owner: "b", Purpose: "release", ProjectID: "project-b", TTL: time.Minute, Now: now.Add(2 * time.Second)})
	if err != nil || !ok {
		t.Fatal(err)
	}
	recovered, err := store.ReconcileExpiredResourceLeases(now.Add(2*time.Minute), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(recovered) != 1 || !recovered[0].Recovered {
		t.Fatalf("restart recovery: %#v", recovered)
	}
	stored, err := store.FindResourceLease(lease.Name)
	if err != nil || stored == nil || stored.State != resourceLeaseReleased {
		t.Fatalf("recovered lease not released: %#v %v", stored, err)
	}
}
