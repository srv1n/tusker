package main

import (
	"strings"
	"sync"
	"testing"
)

func TestAgentContactsRoundTripReplacementAndAmbiguity(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	c := AgentContact{ProjectID: "app", TaskID: "T4", Role: "architect", Address: AgentAddress{Kind: "execution", ID: "exec-1"}}
	saved, err := store.PutAgentContact(c, 0)
	if err != nil {
		t.Fatal(err)
	}
	c.Address.ID = "exec-2"
	c.PredecessorID = saved.Address.ID
	saved, err = store.PutAgentContact(c, 1)
	if err != nil || saved.Generation != 2 {
		t.Fatalf("saved=%#v err=%v", saved, err)
	}
	address, err := store.ResolveAgentContact("app", "T4", "architect", "")
	if err != nil || address.ID != "exec-2" {
		t.Fatalf("address=%#v err=%v", address, err)
	}
	if _, err = store.PutAgentContact(c, 1); err == nil {
		t.Fatal("stale replacement accepted")
	}
	var wg sync.WaitGroup
	success := make(chan AgentContact, 2)
	for _, id := range []string{"exec-3", "exec-4"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			next := c
			next.Address.ID = id
			if saved, err := store.PutAgentContact(next, 2); err == nil {
				success <- saved
			}
		}(id)
	}
	wg.Wait()
	close(success)
	if len(success) != 1 {
		t.Fatalf("concurrent replacements succeeded=%d", len(success))
	}
}

func TestAgentContactsRejectInvalidRole(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, err = store.PutAgentContact(AgentContact{ProjectID: "app", TaskID: "T4", Role: "dependency", Address: AgentAddress{Kind: "task", ID: "T1"}}, 0)
	if err == nil {
		t.Fatal("dependency accepted as contact")
	}
}

func TestAgentContactsPacketProjectsAuthoredReferences(t *testing.T) {
	vault := pickupV7TestVault(t)
	if err := newV7Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "id": "APP-T-0001", "title": "Contact packet"}); err != nil {
		t.Fatal(err)
	}
	task := mustV7Task(t, vault, "APP-T-0001")
	task.Data["architect"] = "execution:architect-session"
	task.Data["architect_source"] = "wave"
	task.Data["origin"] = "task:APP-T-0009"
	task.Data["peer_contacts"] = map[string]any{"schema": "task:APP-T-0002"}
	idx := mustIndex(t, vault)
	packet := v7Packet(vault, task, idx, "agent")
	for _, want := range []string{"## Coordination contacts", "execution:architect-session", "Peer `schema`", "tusker message ask"} {
		if !strings.Contains(packet, want) {
			t.Fatalf("packet missing %q", want)
		}
	}
	reviewer := v7Packet(vault, task, idx, "reviewer")
	if !strings.Contains(reviewer, "execution:architect-session") || strings.Contains(reviewer, "tusker message ask") {
		t.Fatalf("reviewer packet must project contacts without a task-sender command: %s", reviewer)
	}
}
