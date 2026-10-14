package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestShowListRouting(t *testing.T) {
	t.Setenv("TUSKER_STATE_ROOT", t.TempDir())
	vault := newWaveTestVault(t, 2)
	mustWave(t, Args{"vault": vault, "quiet": "true", "_pos0": "Routing", "_pos1": "APP-T-0001"}, waveV7CreateCmd)

	cases := []struct {
		name    string
		run     func(Args) error
		args    Args
		want    []string
		wantErr string
	}{
		{"show task capsule", showCmd, Args{"_pos0": "APP-T-0001"}, []string{"APP-T-0001", "Task 0001"}, ""},
		{"show task acceptance", showCmd, Args{"_pos0": "APP-T-0001", "acceptance": "true"}, []string{"APP-T-0001"}, ""},
		{"show swallowed id", showCmd, Args{"capsule": "APP-T-0002"}, []string{"APP-T-0002"}, ""},
		{"show styled", showCmd, Args{"_pos0": "APP-T-0001", "styled": "true", "plain": "true"}, []string{"Task 0001"}, ""},
		{"show packet", showCmd, Args{"_pos0": "APP-T-0001", "packet": "agent"}, nil, "task is not dispatchable"},
		{"show proof", showCmd, Args{"_pos0": "APP-T-0001", "proof": "true", "json": "true"}, []string{"APP-T-0001"}, ""},
		{"show trace needs id", showCmd, Args{"_pos0": "APP-T-0001", "trace": "true"}, nil, "--trace requires a trace id"},
		{"show wave", showCmd, Args{"_pos0": "w-0001"}, []string{"W-0001", "== Start review", "== Brief"}, ""},
		{"show wave json", showCmd, Args{"_pos0": "W-0001", "json": "true"}, []string{`"wave"`, `"review"`, `"brief"`}, ""},
		{"show escalation", showCmd, Args{"_pos0": "ESC-0099"}, nil, "escalation not found: ESC-0099"},
		{"show execution", showCmd, Args{"_pos0": "exec_missing"}, nil, "execution"},
		{"show message", showCmd, Args{"_pos0": "msg_missing"}, nil, ""},
		{"show unknown falls through docs", showCmd, Args{"_pos0": "no-such-subject"}, nil, "Note not found: no-such-subject"},
		{"list tasks open", listCmd, Args{"_pos0": "tasks"}, []string{"APP-T-0001", "APP-T-0002"}, ""},
		{"list bare epic table", listCmd, Args{}, []string{"APP"}, ""},
		{"list epics", listCmd, Args{"_pos0": "epics"}, []string{"APP"}, ""},
		{"list positional epic", listCmd, Args{"_pos0": "APP", "format": "ids"}, []string{"APP-T-0001"}, ""},
		{"list tasks grep", listCmd, Args{"_pos0": "tasks", "grep": "Task 0002"}, []string{"APP-T-0002"}, ""},
		{"list next", listCmd, Args{"next": "true", "json": "true"}, nil, "No pickable V7 tasks found"},
		{"list next claim refused", listCmd, Args{"next": "true", "claim": "true"}, nil, "does not claim"},
		{"list escalations", listCmd, Args{"_pos0": "escalations", "json": "true"}, []string{`"escalations"`}, ""},
		{"list grep unsupported kind", listCmd, Args{"_pos0": "runs", "grep": "x"}, nil, "--grep applies to"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := Args{"vault": vault}
			for k, v := range tc.args {
				args[k] = v
			}
			if v, ok := args["_pos0"]; ok {
				args["_pos"] = v
			}
			var err error
			out := captureStdout(t, func() { err = tc.run(args) })
			if tc.name == "show message" {
				// Routed to the runtime message store, not the vault note lookup.
				if err == nil || strings.Contains(err.Error(), "Note not found") {
					t.Fatalf("message id was not routed to message show: %v", err)
				}
				return
			}
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want %q\n%s", err, tc.wantErr, out)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v\n%s", err, out)
			}
			for _, want := range tc.want {
				if !strings.Contains(out, want) {
					t.Fatalf("output missing %q:\n%s", want, out)
				}
			}
		})
	}

	t.Run("wave json is one document", func(t *testing.T) {
		out := captureStdout(t, func() {
			if err := showCmd(Args{"vault": vault, "_pos0": "W-0001", "json": "true"}); err != nil {
				t.Fatal(err)
			}
		})
		var doc map[string]any
		if err := json.Unmarshal([]byte(out), &doc); err != nil {
			t.Fatalf("not one JSON value: %v\n%s", err, out)
		}
	})

	t.Run("why keeps doctor exit code", func(t *testing.T) {
		var want, got int
		captureStdout(t, func() {
			want, _ = executionDoctorCmd(Args{"vault": vault, "_pos0": "APP-T-0001", "json": "true"})
			got, _ = showRun(Args{"vault": vault, "_pos0": "APP-T-0001", "why": "true", "json": "true"})
		})
		if got != want {
			t.Fatalf("show --why exit = %d, doctor exit = %d", got, want)
		}
	})
}

func TestShiftPositional(t *testing.T) {
	args := Args{"_pos": "tasks\nAPP", "_pos0": "tasks", "_pos1": "APP"}
	shiftPositional(args)
	if args["_pos0"] != "APP" || args["_pos"] != "APP" || args["_pos1"] != "" {
		t.Fatalf("shift = %#v", args)
	}
}
