package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newDevinTestClient(server *httptest.Server) *DevinSessionsClient {
	return newDevinSessionsClient(server.URL, "org-1", "test-key", server.Client())
}

type devinFixture struct {
	sessionBody  map[string]any
	messages     []map[string]any
	endCursor    string
	hasNextPage  bool
	postStatus   int
	requestID    string
	posts        atomic.Int32
	lastPostBody string
	afterSeen    []string
}

func (f *devinFixture) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := "/v3/organizations/org-1/sessions/devin-1"
		switch {
		case r.Method == http.MethodGet && r.URL.Path == base:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(f.sessionBody)
		case r.Method == http.MethodGet && r.URL.Path == base+"/messages":
			if got := r.URL.Query().Get("first"); got != "200" {
				t.Errorf("messages request must use first=200, got %q", got)
			}
			f.afterSeen = append(f.afterSeen, r.URL.Query().Get("after"))
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"messages": f.messages, "end_cursor": f.endCursor, "has_next_page": f.hasNextPage})
		case r.Method == http.MethodPost && r.URL.Path == base+"/messages":
			f.posts.Add(1)
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.lastPostBody, _ = body["message"].(string)
			if f.requestID != "" {
				w.Header().Set("x-request-id", f.requestID)
			}
			w.WriteHeader(f.postStatus)
			_ = json.NewEncoder(w).Encode(map[string]any{"session_id": "devin-1", "status": "running", "updated_at": float64(1785450005), "acus_consumed": 2})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.String())
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func TestDevinWorkerSendAndReconcile(t *testing.T) {
	fixture := &devinFixture{
		sessionBody: map[string]any{"session_id": "devin-1", "status": "running", "updated_at": float64(1785450000), "acus_consumed": 1.5},
		postStatus:  200, requestID: "req-abc", endCursor: "cur-9",
	}
	server := httptest.NewServer(fixture.handler(t))
	defer server.Close()
	client := newDevinTestClient(server)

	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	identity := seedWorkerRun(t, store, "attempt-1", 1, 1, "devin", "devin-1")

	delivery, _, err := store.PutWorkerDelivery(WorkerDelivery{Identity: identity, Kind: "question", Body: "status?", IdempotencyKey: "k1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := SendDevinWorkerDelivery(context.Background(), store, delivery.DeliveryID, client); err != nil {
		t.Fatal(err)
	}
	if fixture.posts.Load() != 1 {
		t.Fatalf("expected exactly one POST, got %d", fixture.posts.Load())
	}
	if !strings.Contains(fixture.lastPostBody, "[Tusker delivery "+delivery.DeliveryID+"]") {
		t.Fatalf("sent body must embed the delivery tag, got %q", fixture.lastPostBody)
	}
	stored, _, _ := store.WorkerDelivery(delivery.DeliveryID)
	if stored.State != "accepted" || stored.ProviderAcceptedAt == "" {
		t.Fatalf("delivery must be accepted, got %#v", stored)
	}
	if stored.ProviderReceipt["session_id"] != "devin-1" || stored.ProviderReceipt["request_id"] != "req-abc" {
		t.Fatalf("receipt must contain session_id and request_id, got %#v", stored.ProviderReceipt)
	}
	if stored.ProviderReceipt["updated_at"] != "2026-07-30T22:20:05Z" {
		t.Fatalf("numeric provider timestamps must convert to RFC3339, got %#v", stored.ProviderReceipt["updated_at"])
	}

	replyTag := "[Tusker delivery " + delivery.DeliveryID + "]"
	fixture.messages = []map[string]any{
		{"event_id": "evt-user-1", "source": "user", "message": fixture.lastPostBody, "created_at": float64(1785450006)},
		{"event_id": "evt-devin-1", "source": "devin", "message": "working on it " + replyTag, "created_at": float64(1785450007)},
	}
	now := time.Date(2026, 8, 28, 9, 1, 0, 0, time.UTC)
	if err := ReconcileDevinWorker(context.Background(), store, identity, client, now, 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	cursor, err := store.WorkerProviderCursor(identity)
	if err != nil || cursor != "cur-9" {
		t.Fatalf("cursor must persist end_cursor, got %q err=%v", cursor, err)
	}
	stored, _, _ = store.WorkerDelivery(delivery.DeliveryID)
	if stored.State != "replied" || stored.CorrelatedReplyEventID != "evt-devin-1" {
		t.Fatalf("expected correlated reply, got %#v", stored)
	}
	if stored.WorkerActivityObservedAt != "2026-07-30T22:20:06Z" {
		t.Fatalf("observed timestamp must be the tagged message provider created_at, got %q", stored.WorkerActivityObservedAt)
	}
	if fixture.posts.Load() != 1 {
		t.Fatal("reconcile must never POST a model status-check message")
	}
}

func TestDevinWorkerReplyAcrossPolls(t *testing.T) {
	tag := ""
	fixture := &devinFixture{
		sessionBody: map[string]any{"session_id": "devin-1", "status": "running", "updated_at": float64(1785450000)},
		postStatus:  200, endCursor: "cur-1",
	}
	server := httptest.NewServer(fixture.handler(t))
	defer server.Close()
	client := newDevinTestClient(server)

	stateRoot := t.TempDir()
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	identity := seedWorkerRun(t, store, "attempt-1", 1, 1, "devin", "devin-1")
	delivery, _, err := store.PutWorkerDelivery(WorkerDelivery{Identity: identity, Kind: "instruction", Body: "go", IdempotencyKey: "k-split"})
	if err != nil {
		t.Fatal(err)
	}
	if err := SendDevinWorkerDelivery(context.Background(), store, delivery.DeliveryID, client); err != nil {
		t.Fatal(err)
	}
	tag = "[Tusker delivery " + delivery.DeliveryID + "]"

	fixture.messages = []map[string]any{
		{"event_id": "evt-user-tag", "source": "user", "message": tag + "\n\ngo", "created_at": float64(1785450006)},
	}
	if err := ReconcileDevinWorker(context.Background(), store, identity, client, time.Now().UTC(), 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	stored, _, _ := store.WorkerDelivery(delivery.DeliveryID)
	if stored.State != "accepted" || stored.WorkerActivityObservedAt != "2026-07-30T22:20:06Z" {
		t.Fatalf("first poll must record the observed tag timestamp only, got %#v", stored)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	fixture.messages = []map[string]any{
		{"event_id": "evt-devin-late", "source": "devin", "message": "done " + tag, "created_at": float64(1785450099)},
	}
	fixture.endCursor = "cur-2"
	if err := ReconcileDevinWorker(context.Background(), reopened, identity, client, time.Now().UTC(), 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	stored, _, _ = reopened.WorkerDelivery(delivery.DeliveryID)
	if stored.State != "replied" || stored.CorrelatedReplyEventID != "evt-devin-late" {
		t.Fatalf("second poll must correlate the later devin reply, got %#v", stored)
	}
	if fixture.posts.Load() != 1 {
		t.Fatal("no resend may occur during reconcile")
	}
}

func TestDevinWorkerDeliveringCrashRecovery(t *testing.T) {
	var postCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			postCalls.Add(1)
		}
		w.WriteHeader(200)
	}))
	defer server.Close()
	client := newDevinTestClient(server)

	stateRoot := t.TempDir()
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	identity := seedWorkerRun(t, store, "attempt-1", 1, 1, "devin", "devin-1")
	delivery, _, err := store.PutWorkerDelivery(WorkerDelivery{Identity: identity, Kind: "question", Body: "?", IdempotencyKey: "k-crash"})
	if err != nil {
		t.Fatal(err)
	}
	if _, claimed, err := store.ClaimWorkerDelivery(delivery.DeliveryID); err != nil || !claimed {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := SendDevinWorkerDelivery(context.Background(), reopened, delivery.DeliveryID, client); err == nil {
		t.Fatal("a crashed in-flight delivery must surface an error")
	}
	if postCalls.Load() != 0 {
		t.Fatalf("crashed delivery must not resend, got %d POSTs", postCalls.Load())
	}
	stored, _, _ := reopened.WorkerDelivery(delivery.DeliveryID)
	if stored.State != "delivering" {
		t.Fatalf("a competing send must not mutate the in-flight row, got %q", stored.State)
	}
}

func TestDevinWorkerUncertainNoResendThenObservedReconcile(t *testing.T) {
	var postCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			postCalls.Add(1)
			hj, ok := w.(http.Hijacker)
			if ok {
				conn, _, _ := hj.Hijack()
				_ = conn.Close()
				return
			}
			w.WriteHeader(500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/messages") {
			_ = json.NewEncoder(w).Encode(map[string]any{"messages": []any{}, "end_cursor": "cur-1", "has_next_page": false})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"session_id": "devin-1", "status": "running", "updated_at": float64(1785450000)})
	}))
	defer server.Close()
	client := newDevinTestClient(server)

	stateRoot := t.TempDir()
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	identity := seedWorkerRun(t, store, "attempt-1", 1, 1, "devin", "devin-1")
	delivery, _, err := store.PutWorkerDelivery(WorkerDelivery{Identity: identity, Kind: "instruction", Body: "go", IdempotencyKey: "k-lost"})
	if err != nil {
		t.Fatal(err)
	}
	if err := SendDevinWorkerDelivery(context.Background(), store, delivery.DeliveryID, client); err == nil {
		t.Fatal("a lost POST acknowledgement must surface an error")
	}
	stored, _, _ := store.WorkerDelivery(delivery.DeliveryID)
	if stored.State != "uncertain" {
		t.Fatalf("lost POST must mark uncertain, got %q", stored.State)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := SendDevinWorkerDelivery(context.Background(), reopened, delivery.DeliveryID, client); err != nil {
		t.Fatal(err)
	}
	if postCalls.Load() != 1 {
		t.Fatalf("uncertain is terminal for sending: expected 1 POST total, got %d", postCalls.Load())
	}

	tag := "[Tusker delivery " + delivery.DeliveryID + "]"
	observe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/messages") {
			_ = json.NewEncoder(w).Encode(map[string]any{"messages": []any{
				map[string]any{"event_id": "evt-user-tag", "source": "user", "message": tag + "\n\ngo", "created_at": float64(1785450004)},
				map[string]any{"event_id": "evt-devin-reply", "source": "devin", "message": "done " + tag, "created_at": float64(1785450009)},
			}, "end_cursor": "cur-2", "has_next_page": false})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"session_id": "devin-1", "status": "running", "updated_at": float64(1785450010)})
	}))
	defer observe.Close()
	observeClient := newDevinTestClient(observe)
	if err := ReconcileDevinWorker(context.Background(), reopened, identity, observeClient, time.Now().UTC(), 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	stored, _, _ = reopened.WorkerDelivery(delivery.DeliveryID)
	if stored.State != "replied" || stored.CorrelatedReplyEventID != "evt-devin-reply" {
		t.Fatalf("observed tagged message + devin reply must reconcile uncertain delivery, got %#v", stored)
	}
	if postCalls.Load() != 1 {
		t.Fatal("reconcile must never resend")
	}
}

func TestDevinWorkerRepeatedPollDoesNotRefreshAttention(t *testing.T) {
	fixture := &devinFixture{
		sessionBody: map[string]any{"session_id": "devin-1", "status": "running", "updated_at": float64(1785450000)},
		endCursor:   "cur-1",
	}
	server := httptest.NewServer(fixture.handler(t))
	defer server.Close()
	client := newDevinTestClient(server)

	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	identity := seedWorkerRun(t, store, "attempt-1", 1, 1, "devin", "devin-1")
	now := time.Now().UTC()
	if err := ReconcileDevinWorker(context.Background(), store, identity, client, now, 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	attention, err := store.EvaluateWorkerAttention(identity, now.Add(30*time.Minute), 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !attention.AttentionRequired {
		t.Fatal("a stale unchanged status poll must not clear silence: provider updated_at stayed put")
	}
	if err := ReconcileDevinWorker(context.Background(), store, identity, client, now.Add(31*time.Minute), 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	attention, err = store.EvaluateWorkerAttention(identity, now.Add(32*time.Minute), 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !attention.AttentionRequired {
		t.Fatal("repeated identical GETs must not refresh attention")
	}
}

func TestDevinWorkerMultiPageReconcile(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := "/v3/organizations/org-1/sessions/devin-1"
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == base+"/messages":
			calls.Add(1)
			switch r.URL.Query().Get("after") {
			case "":
				_ = json.NewEncoder(w).Encode(map[string]any{"messages": []any{
					map[string]any{"event_id": "e1", "source": "devin", "message": "one", "created_at": float64(1785450001)},
				}, "end_cursor": "cur-1", "has_next_page": true})
			case "cur-1":
				_ = json.NewEncoder(w).Encode(map[string]any{"messages": []any{
					map[string]any{"event_id": "e2", "source": "devin", "message": "two", "created_at": float64(1785450002)},
				}, "end_cursor": "cur-2", "has_next_page": false})
			default:
				t.Errorf("unexpected after cursor %q", r.URL.Query().Get("after"))
			}
		case r.Method == http.MethodGet && r.URL.Path == base:
			_ = json.NewEncoder(w).Encode(map[string]any{"session_id": "devin-1", "status": "running", "updated_at": float64(1785450000)})
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	client := newDevinTestClient(server)

	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	identity := seedWorkerRun(t, store, "attempt-1", 1, 1, "devin", "devin-1")
	if err := ReconcileDevinWorker(context.Background(), store, identity, client, time.Now().UTC(), 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected two message pages, got %d", calls.Load())
	}
	cursor, _ := store.WorkerProviderCursor(identity)
	if cursor != "cur-2" {
		t.Fatalf("cursor must persist per page until has_next_page=false, got %q", cursor)
	}
}

func TestDevinWorkerRejectsNonDevinSessionID(t *testing.T) {
	client := newDevinSessionsClient("http://127.0.0.1:1", "org-1", "k", &http.Client{})
	if _, err := client.GetSession(context.Background(), "abc-123"); err == nil {
		t.Fatal("non devin- session ids must be rejected")
	}
}

func TestDevinWorkerReconcileStaleIdentityKeepsCursor(t *testing.T) {
	fixture := &devinFixture{
		sessionBody: map[string]any{"session_id": "devin-1", "status": "running", "updated_at": float64(1785450000)},
		endCursor:   "cur-late",
		messages:    []map[string]any{{"event_id": "evt-1", "source": "devin", "message": "hi", "created_at": float64(1785450001)}},
	}
	server := httptest.NewServer(fixture.handler(t))
	defer server.Close()
	client := newDevinTestClient(server)

	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	identity := seedWorkerRun(t, store, "attempt-1", 1, 1, "devin", "devin-1")
	if err := store.UpsertRun(RunStatus{ProjectID: "project-1", RecordID: "TASK-1", ItemID: "TASK-1", Runner: "test", LeaseState: string(LeaseStateReleased), ActiveAttemptID: "", LeaseGeneration: 1, WorkRevision: 1}); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileDevinWorker(context.Background(), store, identity, client, time.Now().UTC(), 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	cursor, err := store.WorkerProviderCursor(identity)
	if err != nil || cursor != "" {
		t.Fatalf("stale identity must not advance the cursor, got %q", cursor)
	}
}

func TestDevinWorkerCursorCommitsOnlyAfterProcessing(t *testing.T) {
	var failPage2 atomic.Bool
	failPage2.Store(true)
	var afterSeen []string
	var tag atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := "/v3/organizations/org-1/sessions/devin-1"
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == base+"/messages":
			after := r.URL.Query().Get("after")
			afterSeen = append(afterSeen, after)
			switch after {
			case "cur-0":
				_ = json.NewEncoder(w).Encode(map[string]any{"messages": []any{
					map[string]any{"event_id": "evt-user-tag", "source": "user", "message": tag.Load().(string) + "\n\ngo", "created_at": float64(1785450006)},
				}, "end_cursor": "cur-1", "has_next_page": true})
			case "cur-1":
				if failPage2.Load() {
					w.WriteHeader(500)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"messages": []any{
					map[string]any{"event_id": "evt-devin-reply", "source": "devin", "message": "done " + tag.Load().(string), "created_at": float64(1785450010)},
				}, "end_cursor": "cur-2", "has_next_page": false})
			default:
				t.Errorf("unexpected after cursor %q", after)
			}
		case r.Method == http.MethodGet && r.URL.Path == base:
			_ = json.NewEncoder(w).Encode(map[string]any{"session_id": "devin-1", "status": "running", "updated_at": float64(1785450000)})
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	client := newDevinTestClient(server)

	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	identity := seedWorkerRun(t, store, "attempt-1", 1, 1, "devin", "devin-1")
	if err := store.saveWorkerProviderCursor(identity, "cur-0"); err != nil {
		t.Fatal(err)
	}
	delivery, _, err := store.PutWorkerDelivery(WorkerDelivery{Identity: identity, Kind: "instruction", Body: "go", IdempotencyKey: "k-cursor"})
	if err != nil {
		t.Fatal(err)
	}
	if _, claimed, err := store.ClaimWorkerDelivery(delivery.DeliveryID); err != nil || !claimed {
		t.Fatal(err)
	}
	if err := store.MarkWorkerDeliveryAccepted(delivery.DeliveryID, nil, "2026-07-30T22:00:00Z"); err != nil {
		t.Fatal(err)
	}
	tag.Store("[Tusker delivery " + delivery.DeliveryID + "]")

	if err := ReconcileDevinWorker(context.Background(), store, identity, client, time.Now().UTC(), 10*time.Minute); err == nil {
		t.Fatal("page 2 failure must surface an error")
	}
	cursor, err := store.WorkerProviderCursor(identity)
	if err != nil || cursor != "cur-0" {
		t.Fatalf("failed reconcile must leave the durable cursor at cur-0, got %q", cursor)
	}
	stored, _, _ := store.WorkerDelivery(delivery.DeliveryID)
	if stored.WorkerActivityObservedAt != "" {
		t.Fatal("derived delivery state must not persist when a later page fails")
	}

	failPage2.Store(false)
	if err := ReconcileDevinWorker(context.Background(), store, identity, client, time.Now().UTC(), 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	if afterSeen[0] != "cur-0" || afterSeen[2] != "cur-0" {
		t.Fatalf("retry must restart from the durable cursor cur-0, got %#v", afterSeen)
	}
	stored, _, _ = store.WorkerDelivery(delivery.DeliveryID)
	if stored.State != "replied" || stored.CorrelatedReplyEventID != "evt-devin-reply" {
		t.Fatalf("replayed tag plus page-2 reply must correlate, got %#v", stored)
	}
	cursor, err = store.WorkerProviderCursor(identity)
	if err != nil || cursor != "cur-2" {
		t.Fatalf("final cursor must commit only after processing, got %q", cursor)
	}
}

func TestDevinWorkerExactReplyCorrelation(t *testing.T) {
	fixture := &devinFixture{
		sessionBody: map[string]any{"session_id": "devin-1", "status": "running", "updated_at": float64(1785450000)},
		endCursor:   "cur-1",
	}
	server := httptest.NewServer(fixture.handler(t))
	defer server.Close()
	client := newDevinTestClient(server)

	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	identity := seedWorkerRun(t, store, "attempt-1", 1, 1, "devin", "devin-1")
	var deliveries []WorkerDelivery
	for i, key := range []string{"k-a", "k-b"} {
		d, _, err := store.PutWorkerDelivery(WorkerDelivery{Identity: identity, Kind: "question", Body: "q" + key, IdempotencyKey: key})
		if err != nil {
			t.Fatal(err)
		}
		if _, claimed, err := store.ClaimWorkerDelivery(d.DeliveryID); err != nil || !claimed {
			t.Fatal(err)
		}
		if err := store.MarkWorkerDeliveryAccepted(d.DeliveryID, nil, "2026-07-30T22:00:0"+string(rune('0'+i))+"Z"); err != nil {
			t.Fatal(err)
		}
		deliveries = append(deliveries, d)
	}
	tagA := "[Tusker delivery " + deliveries[0].DeliveryID + "]"
	tagB := "[Tusker delivery " + deliveries[1].DeliveryID + "]"
	fixture.messages = []map[string]any{
		{"event_id": "evt-user-a", "source": "user", "message": tagA + "\n\nqa", "created_at": float64(1785450001)},
		{"event_id": "evt-user-b", "source": "user", "message": tagB + "\n\nqb", "created_at": float64(1785450002)},
		{"event_id": "evt-untagged", "source": "devin", "message": "autonomous progress note", "created_at": float64(1785450003)},
		{"event_id": "evt-reply-b", "source": "devin", "message": "answer b " + tagB, "created_at": float64(1785450004)},
	}
	if err := ReconcileDevinWorker(context.Background(), store, identity, client, time.Now().UTC(), 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	a, _, _ := store.WorkerDelivery(deliveries[0].DeliveryID)
	b, _, _ := store.WorkerDelivery(deliveries[1].DeliveryID)
	if a.State != "accepted" {
		t.Fatalf("untagged devin activity must not resolve delivery A, got %#v", a)
	}
	if b.State != "replied" || b.CorrelatedReplyEventID != "evt-reply-b" {
		t.Fatalf("tagged reply must resolve only its delivery, got %#v", b)
	}

	fixture.messages = []map[string]any{
		{"event_id": "evt-user-a", "source": "user", "message": tagA + "\n\nqa", "created_at": float64(1785450001)},
		{"event_id": "evt-user-b", "source": "user", "message": tagB + "\n\nqb", "created_at": float64(1785450002)},
		{"event_id": "evt-untagged", "source": "devin", "message": "autonomous progress note", "created_at": float64(1785450003)},
		{"event_id": "evt-reply-b", "source": "devin", "message": "answer b " + tagB, "created_at": float64(1785450004)},
		{"event_id": "evt-reply-a", "source": "devin", "message": "answer a " + tagA, "created_at": float64(1785450005)},
	}
	if err := ReconcileDevinWorker(context.Background(), store, identity, client, time.Now().UTC(), 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	a, _, _ = store.WorkerDelivery(deliveries[0].DeliveryID)
	if a.State != "replied" || a.CorrelatedReplyEventID != "evt-reply-a" {
		t.Fatalf("interleaved tagged reply must resolve delivery A, got %#v", a)
	}
}

func TestDevinWorkerConcurrentSendInFlight(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var postCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := "/v3/organizations/org-1/sessions/devin-1"
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == base+"/messages":
			postCalls.Add(1)
			close(entered)
			<-release
			w.WriteHeader(200)
			_ = json.NewEncoder(w).Encode(map[string]any{"session_id": "devin-1"})
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	client := newDevinTestClient(server)

	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	identity := seedWorkerRun(t, store, "attempt-1", 1, 1, "devin", "devin-1")
	delivery, _, err := store.PutWorkerDelivery(WorkerDelivery{Identity: identity, Kind: "question", Body: "?", IdempotencyKey: "k-race"})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- SendDevinWorkerDelivery(context.Background(), store, delivery.DeliveryID, client) }()
	<-entered
	if err := SendDevinWorkerDelivery(context.Background(), store, delivery.DeliveryID, client); err == nil {
		t.Fatal("concurrent sender must fail closed while the delivery is in flight")
	}
	stored, _, _ := store.WorkerDelivery(delivery.DeliveryID)
	if stored.State != "delivering" {
		t.Fatalf("concurrent sender must not mutate the in-flight row, got %q", stored.State)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	stored, _, _ = store.WorkerDelivery(delivery.DeliveryID)
	if stored.State != "accepted" {
		t.Fatalf("owning sender must record accepted, got %q", stored.State)
	}
	if postCalls.Load() != 1 {
		t.Fatalf("exactly one POST may occur, got %d", postCalls.Load())
	}
}

func TestDevinWorkerSupersedeDuringFetchSkipsDerivedWrites(t *testing.T) {
	var tag atomic.Value
	fixture := &devinFixture{
		sessionBody: map[string]any{"session_id": "devin-1", "status": "running", "updated_at": float64(1785450000)},
		endCursor:   "cur-late",
	}
	server := httptest.NewServer(fixture.handler(t))
	defer server.Close()
	client := newDevinTestClient(server)

	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	identity := seedWorkerRun(t, store, "attempt-1", 1, 1, "devin", "devin-1")
	delivery, _, err := store.PutWorkerDelivery(WorkerDelivery{Identity: identity, Kind: "question", Body: "?", IdempotencyKey: "k-supersede"})
	if err != nil {
		t.Fatal(err)
	}
	if _, claimed, err := store.ClaimWorkerDelivery(delivery.DeliveryID); err != nil || !claimed {
		t.Fatal(err)
	}
	if err := store.MarkWorkerDeliveryAccepted(delivery.DeliveryID, nil, "2026-07-30T22:00:00Z"); err != nil {
		t.Fatal(err)
	}
	tag.Store("[Tusker delivery " + delivery.DeliveryID + "]")
	fixture.messages = []map[string]any{
		{"event_id": "evt-user-tag", "source": "user", "message": tag.Load().(string) + "\n\n?", "created_at": float64(1785450001)},
		{"event_id": "evt-devin-reply", "source": "devin", "message": "done " + tag.Load().(string), "created_at": float64(1785450002)},
	}
	client.afterFetch = func() {
		if err := store.UpsertRun(RunStatus{
			ProjectID: "project-1", RecordID: "TASK-1", ItemID: "TASK-1", Runner: "test",
			LeaseState: string(LeaseStateClaimed), ActiveAttemptID: "attempt-2", LeaseGeneration: 2, WorkRevision: 1,
		}); err != nil {
			t.Errorf("supersede: %v", err)
		}
		root, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project-1", DisplayName: "root-2"})
		if err != nil {
			t.Errorf("supersede root: %v", err)
			return
		}
		if _, err := store.CreateManagedExecution(ManagedExecutionInput{
			ProjectID: "project-1", ParentExecutionID: root.ExecutionID, TaskID: "TASK-1",
			AttemptID: "attempt-2", LeaseGeneration: 2, Provider: "devin", ProviderSessionID: "devin-2", Source: "test",
		}); err != nil {
			t.Errorf("supersede execution: %v", err)
		}
	}
	if err := ReconcileDevinWorker(context.Background(), store, identity, client, time.Now().UTC(), 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	cursor, err := store.WorkerProviderCursor(identity)
	if err != nil || cursor != "" {
		t.Fatalf("superseded identity must not commit the cursor, got %q", cursor)
	}
	stored, _, _ := store.WorkerDelivery(delivery.DeliveryID)
	if stored.State != "accepted" || stored.CorrelatedReplyEventID != "" || stored.WorkerActivityObservedAt != "" {
		t.Fatalf("superseded reconcile must not mutate the delivery, got %#v", stored)
	}
}
