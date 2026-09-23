package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDemoSessionAskSeedPrompt(t *testing.T) {
	context := demoTaskContext(demoFixtureWaves()[0], demoFixtureWaves()[0].Tasks[0])
	if !strings.Contains(context, "call the Tusker MCP ask tool") || !strings.Contains(context, "session-ask.txt") {
		t.Fatal("seeded standalone prompt lacks conditional MCP ask instruction")
	}
	for _, scenario := range []string{"ask-wait", "ask-nowait"} {
		t.Run(scenario, func(t *testing.T) {
			repo := t.TempDir()
			if err := demoSessionPrepareAsk(repo, scenario); err != nil {
				t.Fatal(err)
			}
			contents, err := os.ReadFile(filepath.Join(repo, "sample", "standalone", "session-ask.txt"))
			if err != nil {
				t.Fatal(err)
			}
			want := "wait_seconds: 0"
			if scenario == "ask-wait" {
				want = "wait_seconds: 120"
			}
			if !strings.Contains(string(contents), demoAskQuestion) || !strings.Contains(string(contents), want) {
				t.Fatalf("marker: %s", contents)
			}
			if err := demoSessionPrepareAsk(repo, scenario); err == nil {
				t.Fatal("existing marker overwritten")
			}
		})
	}
}

func TestDemoSessionAskCorrelatedReply(t *testing.T) {
	for _, scenario := range []string{"ask-wait", "ask-nowait"} {
		t.Run(scenario, func(t *testing.T) {
			question := AgentMessage{ID: "q1", ProjectID: "demo", Sender: "task:task", Recipient: AgentAddress{Kind: "operator", ID: "operator"}, Kind: "question", Body: demoAskQuestion, ReplyRequired: true, YieldSender: scenario == "ask-wait"}
			answer := AgentMessage{ID: "a1", ProjectID: "demo", Sender: "human:test", Recipient: AgentAddress{Kind: "task", ID: "task"}, Kind: "answer", Body: demoAskAnswer, ReplyTo: question.ID}
			continued := false
			stopped := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.Method == "GET" && r.URL.Path == "/api/messages" && r.URL.Query().Get("recipientKind") == "operator":
					_ = json.NewEncoder(w).Encode(map[string]any{"messages": []AgentMessage{question}})
				case r.Method == "POST" && r.URL.Path == "/api/messages":
					var body map[string]any
					_ = json.NewDecoder(r.Body).Decode(&body)
					if body["replyTo"] != question.ID || body["body"] != demoAskAnswer {
						t.Errorf("uncorrelated reply: %#v", body)
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "message": answer})
				case r.Method == "POST" && r.URL.Path == "/api/runs/task/continue":
					continued = true
					_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
				case r.Method == "POST" && r.URL.Path == "/api/runs/task/control":
					stopped = true
					_ = json.NewEncoder(w).Encode(map[string]any{"supported": true, "settled": true})
				case r.Method == "GET" && r.URL.Path == "/api/runs/task":
					state := "working"
					if stopped && !continued {
						state = "stopped"
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"operatorState": map[string]string{"state": state}, "session": map[string]string{"session_ref": "native-1"}})
				case r.Method == "GET" && r.URL.Path == "/api/messages" && r.URL.Query().Get("recipientKind") == "task":
					_ = json.NewEncoder(w).Encode(map[string]any{"messages": []AgentMessage{answer}})
				default:
					t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			old := demoServeBaseURL
			demoServeBaseURL = server.URL
			defer func() { demoServeBaseURL = old }()
			var after serveRunDetail
			proof := demoSessionProof{}
			endpoint := server.URL + "/api/runs/task?project=demo"
			if err := demoSessionAsk(context.Background(), "demo", "task", endpoint, "cap", scenario, &after, &proof); err != nil {
				t.Fatal(err)
			}
			if continued != (scenario == "ask-nowait") {
				t.Fatalf("continue=%v", continued)
			}
			if stopped != (scenario == "ask-nowait") {
				t.Fatalf("stopped=%v", stopped)
			}
			if after.OperatorState.State != "working" {
				t.Fatalf("state=%s", after.OperatorState.State)
			}
		})
	}
}
