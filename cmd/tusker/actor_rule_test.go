package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestActorRuleInteractiveHumanAndAgent(t *testing.T) {
	clearAgentSessionEnvForTest(t)
	t.Setenv("CLAUDECODE", "1")
	if got := agentSessionKind(); got != "interactive Claude session" {
		t.Fatalf("execution role = %q", got)
	}
	for _, actor := range []string{"human:sarav", "agent:claude"} {
		if got, err := v7HumanActor(Args{"by": actor}, "gate satisfy"); err != nil || got != actor {
			t.Fatalf("shared actor %q = %q, %v", actor, got, err)
		}
		if got, err := directStartActor(Args{"by": actor}, "wave start"); err != nil || got != actor {
			t.Fatalf("wave actor %q = %q, %v", actor, got, err)
		}
	}
	if _, err := directStartActor(Args{"by": "agent:"}, "wave start"); err == nil {
		t.Fatal("blank agent name accepted")
	}
}

func TestWaveStartRecordsInteractiveExecutionRole(t *testing.T) {
	clearAgentSessionEnvForTest(t)
	t.Setenv("CLAUDECODE", "1")
	vault, store, _ := authorityFixture(t)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001"}, nil)
	if _, err := directWaveStart(vault, store, "W-0001", "human:sarav"); err != nil {
		t.Fatal(err)
	}
	paths, err := filepath.Glob(filepath.Join(vault, "events", "*", "*", "W-0001--*.json"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("wave events = %v, %v", paths, err)
	}
	data, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	var event struct {
		Actor   string         `json:"actor"`
		Payload map[string]any `json:"payload"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		t.Fatal(err)
	}
	if event.Actor != "human:sarav" || event.Payload["execution_role"] != "interactive Claude session" {
		t.Fatalf("wave event actor=%q payload=%v", event.Actor, event.Payload)
	}
}
