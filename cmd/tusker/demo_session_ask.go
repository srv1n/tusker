package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

const demoAskQuestion = "Session demo: which fixture word should I put in the smoke artifact?"
const demoAskAnswer = "Keep the seeded artifact content unchanged: standalone-smoke ok."

func demoSessionPrepareAsk(repo, scenario string) error {
	wait := 0
	if scenario == "ask-wait" {
		wait = 120
	}
	path := filepath.Join(repo, "sample", "standalone", "session-ask.txt")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("session ask marker already exists; reset the demo before rerunning")
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.WriteFile(path, []byte(fmt.Sprintf("question: %s\nwait_seconds: %d\n", demoAskQuestion, wait)), 0o644)
}

func demoSessionRemoveAsk(repo string) {
	_ = os.Remove(filepath.Join(repo, "sample", "standalone", "session-ask.txt"))
}

func demoSessionAsk(ctx context.Context, project, task, endpoint, capability, scenario string, after *serveRunDetail, proof *demoSessionProof) error {
	question, err := demoSessionQuestion(ctx, project, task)
	if err != nil {
		return err
	}
	if question.Body != demoAskQuestion || question.Sender != "task:"+task || question.Recipient.Kind != "operator" || question.Recipient.ID != "operator" || !question.ReplyRequired || question.YieldSender != (scenario == "ask-wait") {
		return fmt.Errorf("recorded worker question does not match the seeded MCP ask")
	}
	if scenario == "ask-nowait" {
		var stop runSessionControlResult
		if err := demoSessionPost(ctx, endpoint, "control", capability, map[string]string{"action": "stop"}, &stop); err != nil || stop.Refused || !stop.Supported {
			return fmt.Errorf("stop before answer continuation refused: %s", firstNonEmpty(stop.Reason, demoSessionError(err)))
		}
		var stopped serveRunDetail
		if err := demoSessionWait(ctx, endpoint, "stopped", &stopped, proof); err != nil {
			return err
		}
	}
	var reply struct {
		OK      bool         `json:"ok"`
		Refused bool         `json:"refused"`
		Reason  string       `json:"reason"`
		Message AgentMessage `json:"message"`
	}
	body := map[string]any{"projectId": project, "recipientKind": "task", "recipientId": task, "originTaskId": task, "body": demoAskAnswer, "replyTo": question.ID, "idempotencyKey": "demo-session-" + scenario + "-" + question.ID}
	if err := demoSessionMessagePost(ctx, capability, body, &reply); err != nil {
		return err
	}
	if !reply.OK || reply.Refused || reply.Message.ReplyTo != question.ID || reply.Message.Kind != "answer" {
		return fmt.Errorf("correlated operator reply refused: %s", reply.Reason)
	}
	if scenario == "ask-nowait" {
		var continued serveRunSayResponse
		if err := demoSessionPost(ctx, endpoint, "continue", capability, map[string]string{"message": "Use the operator answer to the session demo question and finish the smoke task."}, &continued); err != nil || !continued.OK || continued.Refused {
			return fmt.Errorf("answer continuation refused: %s", firstNonEmpty(continued.Reason, demoSessionError(err)))
		}
	}
	if err := demoSessionWait(ctx, endpoint, "working", after, proof); err != nil {
		return err
	}
	var messages struct {
		Messages []AgentMessage `json:"messages"`
	}
	listURL := demoServeBaseURL + "/api/messages?project=" + url.QueryEscape(project) + "&recipientKind=task&recipient=" + url.QueryEscape(task)
	if err := demoHTTPJSON(ctx, http.MethodGet, listURL, "", &messages); err != nil {
		return err
	}
	for _, message := range messages.Messages {
		if message.ID == reply.Message.ID && message.ReplyTo == question.ID && message.Body == demoAskAnswer {
			return nil
		}
	}
	return fmt.Errorf("correlated answer missing from message readback")
}

func demoSessionQuestion(ctx context.Context, project, task string) (AgentMessage, error) {
	endpoint := demoServeBaseURL + "/api/messages?project=" + url.QueryEscape(project) + "&recipientKind=operator&recipient=operator"
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		var list struct {
			Messages []AgentMessage `json:"messages"`
		}
		if err := demoHTTPJSON(ctx, http.MethodGet, endpoint, "", &list); err == nil {
			for _, message := range list.Messages {
				if message.Kind == "question" && message.Sender == "task:"+task && message.Body == demoAskQuestion && message.AnsweredAt == "" {
					return message, nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return AgentMessage{}, fmt.Errorf("timed out waiting for seeded worker MCP question")
		case <-ticker.C:
		}
	}
}

func demoSessionMessagePost(ctx context.Context, capability string, body any, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, demoServeBaseURL+"/api/messages", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(serveCapabilityHeader, capability)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("message reply returned %s", resp.Status)
	}
	return nil
}
