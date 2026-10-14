package main

import (
	"strings"
	"testing"
)

func TestOperatorMergeShiftPositionals(t *testing.T) {
	got := shiftPositionals(Args{"_pos": "service\nrefresh", "_pos0": "service", "_pos1": "refresh", "json": "true"})
	if got["_pos0"] != "refresh" || got["_pos"] != "refresh" || got["_pos1"] != "" || got["json"] != "true" {
		t.Fatalf("shifted args = %#v", got)
	}
}

func TestOperatorMergeRejectsConflictingFlags(t *testing.T) {
	t.Setenv("TUSKER_STATE_ROOT", t.TempDir())
	cases := map[string]func() error{
		"daemon bogus":            func() error { return daemonCmd(Args{"_pos0": "bogus"}) },
		"projects bogus":          func() error { return projectsCmd(Args{"_pos0": "bogus"}) },
		"automation bogus":        func() error { return automationCmd(Args{"_pos0": "bogus"}) },
		"models bogus":            func() error { return modelsRoute(Args{"_pos0": "bogus"}) },
		"projects set on+off":     func() error { return projectsSetCmd(Args{"_pos0": "app", "on": "true", "off": "true"}) },
		"projects set no flag":    func() error { return projectsSetCmd(Args{"_pos0": "app"}) },
		"runs restart fresh+req":  func() error { return runsRestartCmd(Args{"id": "T", "fresh": "true", "requeue": "true"}) },
		"models profile two acts": func() error { return modelsProfileRoute(Args{"_pos0": "p", "disable": "true", "remove": "true"}) },
		"models profile set+act":  func() error { return modelsProfileRoute(Args{"_pos0": "p", "set": "true", "enable": "true"}) },
		"message mark no state":   func() error { return messageMarkCmd(Args{"_pos0": "msg-1", "project": "app"}) },
		"message mark both": func() error {
			return messageMarkCmd(Args{"_pos0": "msg-1", "project": "app", "consumed": "true", "applied": "true"})
		},
		"message mark ESC read": func() error { return messageMarkCmd(Args{"_pos0": "ESC-0001", "consumed": "true"}) },
		"ask severity to task":  func() error { return messageAskCmd(Args{"severity": "P1", "to": "task:T1"}) },
	}
	for name, run := range cases {
		if err := run(); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestOperatorMergeMessageAskNotifyAndMark(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TUSKER_STATE_ROOT", root)
	base := func(key string) Args {
		return Args{"project": "app", "sender": "operator:operator", "to": "task:T1", "key": key, "body": "hello"}
	}
	captureStdout(t, func() {
		if err := messageAskCmd(base("q")); err != nil {
			t.Fatal(err)
		}
		notify := base("n")
		notify["notify"] = "true"
		if err := messageAskCmd(notify); err != nil {
			t.Fatal(err)
		}
	})
	store, err := OpenRuntimeStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	messages, err := store.ListAgentMessages("app", "task", "T1")
	if err != nil || len(messages) != 2 {
		t.Fatalf("messages=%v err=%v", messages, err)
	}
	byKey := map[string]AgentMessage{}
	for _, m := range messages {
		byKey[m.IdempotencyKey] = m
	}
	if q := byKey["q"]; q.Kind != "question" || !q.ReplyRequired {
		t.Fatalf("ask stored %#v", q)
	}
	if n := byKey["n"]; n.Kind != "notice" || n.ReplyRequired {
		t.Fatalf("ask --notify stored %#v", n)
	}
	captureStdout(t, func() {
		if err := messageMarkCmd(Args{"project": "app", "_pos0": byKey["n"].ID, "applied": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	marked, err := store.AgentMessage("app", byKey["n"].ID)
	if err != nil || marked.State != "applied" {
		t.Fatalf("marked=%#v err=%v", marked, err)
	}
}

func TestOperatorMergeEscalationThroughMessage(t *testing.T) {
	vault := pickupV7TestVault(t)
	writeDigestTask(t, vault, "APP-T-0001", "Escalating task", "ready", "medium", "pending", "")
	oldNotify := notifyEscalationUser
	notifyEscalationUser = func(title, message string) error { return nil }
	t.Cleanup(func() { notifyEscalationUser = oldNotify })

	if err := messageAskCmd(Args{"vault": vault, "local": "true", "quiet": "true", "severity": "P1", "to": "operator", "task": "APP-T-0001", "reason": "stuck_loop", "body": "loop on the same failure"}); err != nil {
		t.Fatal(err)
	}
	note, err := resolveV7Note(vault, "ESC-0001", "escalation")
	if err != nil {
		t.Fatal(err)
	}
	if stringField(note.Data, "severity") != "P1" || !strings.Contains(stringField(note.Data, "description"), "loop on the same failure") {
		t.Fatalf("escalation = %#v", note.Data)
	}
	if err := messageMarkCmd(Args{"vault": vault, "local": "true", "quiet": "true", "_pos0": "esc-0001", "applied": "true", "by": "human:sarav"}); err != nil {
		t.Fatal(err)
	}
	acked, err := resolveV7Note(vault, "ESC-0001", "escalation")
	if err != nil || stringField(acked.Data, "status") != escalationStatusAcknowledged {
		t.Fatalf("acked=%#v err=%v", acked.Data, err)
	}
}
