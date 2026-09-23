package main

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestServeNeedsWorkerQuestions(t *testing.T) {
	server := newServeFixture(t)
	question, _, err := server.store.PutAgentMessage(AgentMessage{
		ProjectID: "app", Sender: "task:APP-T-0004", Recipient: AgentAddress{Kind: "operator", ID: "operator"},
		IdempotencyKey: "worker-question", Kind: "question", Body: "Which release window?", ReplyRequired: true, YieldSender: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var needs []serveNeedItem
	serveDecode(t, server, "/api/needs?project=app", &needs)
	found := false
	for _, need := range needs {
		if need["messageId"] == question.ID {
			found = true
			if need["kind"] != "question" || need["body"] != question.Body {
				t.Fatalf("question need: %#v", need)
			}
		}
	}
	if !found {
		t.Fatalf("question absent from needs: %#v", needs)
	}
	var detail serveTaskDetail
	serveDecode(t, server, "/api/tasks/APP-T-0004?project=app", &detail)
	if len(detail.HumanActions) != 1 || detail.HumanActions[0].MessageID != question.ID {
		t.Fatalf("rework question actions: %#v", detail.HumanActions)
	}

	answer := fmt.Sprintf(`{"projectId":"app","recipientKind":"task","recipientId":"APP-T-0004","originTaskId":"APP-T-0004","body":"Friday","replyTo":%q,"idempotencyKey":"operator-answer"}`, question.ID)
	status, payload := serveMutationRaw(t, server, http.MethodPost, "/api/messages", answer)
	if status != http.StatusOK || !strings.Contains(payload, `"ok":true`) {
		t.Fatalf("answer status=%d body=%s", status, payload)
	}
	needs = nil // json.Unmarshal reuses existing map entries when decoding into a non-nil slice.
	serveDecode(t, server, "/api/needs?project=app", &needs)
	for _, need := range needs {
		if need["messageId"] == question.ID {
			t.Fatalf("answered question still needed: %#v", need)
		}
	}
	parent, err := server.store.AgentMessage("app", question.ID)
	if err != nil || parent.AnsweredAt == "" {
		t.Fatalf("answer not recorded: %#v %v", parent, err)
	}
	_, stale := serveMutationRaw(t, server, http.MethodPost, "/api/messages", strings.Replace(answer, "operator-answer", "second-answer", 1))
	if !strings.Contains(stale, `"refused":true`) {
		t.Fatalf("stale answer accepted: %s", stale)
	}
}
