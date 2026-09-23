package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDemoSessionLiveServeReadback(t *testing.T) {
	for _, tc := range []struct {
		name, harness, route string
	}{
		{"say-hard", "codex_exec", "hard"},
		{"say-soft", "claude-code", "soft"},
		{"stop-continue", "codex_exec", ""},
		{"start-fresh", "codex_exec", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			phase := "initial"
			var actions []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				write := func(value any) {
					t.Helper()
					if err := json.NewEncoder(w).Encode(value); err != nil {
						t.Error(err)
					}
				}
				switch {
				case r.Method == "GET" && r.URL.Path == "/api/capability":
					write(map[string]any{"capability": "test"})
				case r.Method == "POST" && r.URL.Path == "/api/actions/projects/demo/waves/standalone/start":
					write(map[string]any{"authorization": "authorized"})
				case r.Method == "GET" && r.URL.Path == "/api/runs/task":
					state, session := "working", "native-1"
					if phase == "stopped" {
						state = "stopped"
					}
					if phase == "fresh" {
						session = "native-2"
					}
					attempts := []map[string]string{{"id": "attempt-1"}}
					if phase == "continued" || phase == "fresh" || phase == "said" && tc.name == "say-hard" {
						attempts = append(attempts, map[string]string{"id": "attempt-2"})
					}
					detail := map[string]any{"operatorState": map[string]string{"state": state}, "session": map[string]string{"session_ref": session}, "attempts": attempts}
					if phase == "said" {
						detail["lastSayDelivery"] = map[string]string{"body": "Session demo instruction: acknowledge this message in the run transcript."}
					}
					write(detail)
				case r.Method == "POST" && r.URL.Path == "/api/runs/task/say":
					phase = "said"
					actions = append(actions, "say")
					write(map[string]any{"ok": true, "route": tc.route})
				case r.Method == "POST" && r.URL.Path == "/api/runs/task/control":
					var body struct {
						Action string `json:"action"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					actions = append(actions, body.Action)
					if body.Action == "stop" {
						phase = "stopped"
					} else if body.Action == "start_fresh" {
						phase = "fresh"
					}
					write(map[string]any{"ok": true, "supported": true, "settled": true})
				case r.Method == "POST" && r.URL.Path == "/api/runs/task/continue":
					phase = "continued"
					actions = append(actions, "continue")
					write(map[string]any{"ok": true})
				default:
					t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			old := demoServeBaseURL
			demoServeBaseURL = server.URL
			defer func() { demoServeBaseURL = old }()
			manifest := &demoManifest{RuntimeProjectID: "demo", Tasks: map[string]demoTaskRecord{"s1": {TaskID: "task"}}, Waves: map[string]demoWaveRecord{"standalone": {WaveID: "standalone"}}}
			proof := demoRunSessionLive(t.TempDir(), manifest, tc.harness, tc.name)
			if proof.Status != "passed" || proof.Label != "live-provider" {
				t.Fatalf("proof: %#v", proof)
			}
			if proof.NativeBefore != "native-1" {
				t.Fatalf("native before: %#v", proof)
			}
			if tc.name == "start-fresh" && proof.NativeAfter != "native-2" || tc.name != "start-fresh" && proof.NativeAfter != "native-1" {
				t.Fatalf("native after: %#v", proof)
			}
			if len(actions) == 0 || tc.name == "say-hard" && strings.Join(actions, ",") != "say" {
				t.Fatalf("actions: %v", actions)
			}
		})
	}
}
