package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const museTestSession = "11111111-2222-3333-4444-555555555555"

type museFixtureRunner struct {
	calls      []MuseCommand
	execCalls  int
	exportJSON string
	listJSON   string
	listErr    error
	sendJSON   string
	sendErr    error
	execJSON   string
	version    string
}

func (f *museFixtureRunner) run(ctx context.Context, command MuseCommand) (string, error) {
	f.calls = append(f.calls, command)
	joined := strings.Join(command.Args, " ")
	switch {
	case joined == "--version":
		return firstNonEmpty(f.version, "muse 1.2.3"), nil
	case strings.HasPrefix(joined, "export "):
		if command.OutputPath != "" {
			if err := os.WriteFile(command.OutputPath, []byte(f.exportJSON), 0o600); err != nil {
				return "", err
			}
		}
		return f.exportJSON, nil
	case strings.HasPrefix(joined, "session-message list"):
		if f.listErr != nil {
			return "", f.listErr
		}
		return f.listJSON, nil
	case strings.HasPrefix(joined, "session-message send"):
		if f.sendErr != nil {
			return "", f.sendErr
		}
		return f.sendJSON, nil
	case strings.HasPrefix(joined, "exec "):
		f.execCalls++
		return f.execJSON, nil
	default:
		return "", fmt.Errorf("unexpected muse args: %s", joined)
	}
}

func museExportFixture(sessionID string) string {
	return fmt.Sprintf(`{"events":[
		{"stream":{"kind":"session","id":"%s"},"causation_id":"cmd-1","payload_type":"runtime.command_intake.received","payload":{"kind":"command_intake","record":{"prompt":"status?"}}},
		{"stream":{"kind":"session","id":"%s"},"causation_id":"cmd-1","payload_type":"runtime.command_intake.settled","payload":{"kind":"command_intake","record":{"kind":"settled","command_id":"cmd-1","outcome":{"kind":"accepted"}}}}
	]}`, sessionID, sessionID)
}

func museExecFixture(sessionID string) string {
	return fmt.Sprintf(`{"stream":{"kind":"session","id":"%s"},"payload_type":"runtime.command.accepted","causation_id":"cmd-9"}
{"stream":{"kind":"session","id":"%s"},"payload_type":"run.terminal.completed","causation_id":"cmd-9","payload":{"text":"echo ok"}}`, sessionID, sessionID)
}

func museFullFixture() *museFixtureRunner {
	return &museFixtureRunner{
		exportJSON: museExportFixture(museTestSession),
		listJSON:   `{"events":[]}`,
		sendJSON:   fmt.Sprintf(`{"target":"%s","status":"accepted"}`, museTestSession),
		execJSON:   museExecFixture(museTestSession),
	}
}

func TestMuseWorkerQualifiesAllCapabilitiesIndependently(t *testing.T) {
	fixture := museFullFixture()
	q, err := QualifyMuseEndpoint(context.Background(), fixture.run, museTestSession)
	if err != nil {
		t.Fatal(err)
	}
	c := q.Capabilities
	for name, value := range map[string]bool{
		"attach": c.AttachExternal, "deliver": c.DeliverActive, "resume": c.ResumeIdle,
		"observe": c.ObserveTurn, "reply": c.CaptureReply, "reconcile": c.ReconcileDelivery,
	} {
		if !value {
			t.Fatalf("capability %s must be proven true", name)
		}
	}
	var sawExport, sawSend bool
	for _, call := range fixture.calls {
		joined := strings.Join(call.Args, " ")
		if strings.HasPrefix(joined, "export --session "+museTestSession) {
			sawExport = true
			if !strings.Contains(joined, "--out ") || !strings.Contains(joined, "--redacted") {
				t.Fatalf("export must use --out <path> --redacted, got %q", joined)
			}
		}
		if strings.HasPrefix(joined, "session-message send --target "+museTestSession) {
			sawSend = true
			if call.Stdin == "" {
				t.Fatal("send probe must carry the probe body on stdin")
			}
		}
	}
	if !sawExport || !sawSend {
		t.Fatalf("qualification must run exact export and stdin send probes, got %#v", fixture.calls)
	}
	if fixture.execCalls != 2 {
		t.Fatalf("resume probe must run echo exec twice, got %d", fixture.execCalls)
	}
}

func TestMuseWorkerIngressListFailureSkipsSend(t *testing.T) {
	fixture := museFullFixture()
	fixture.listErr = fmt.Errorf("unknown command")
	q, err := QualifyMuseEndpoint(context.Background(), fixture.run, museTestSession)
	if err != nil {
		t.Fatal(err)
	}
	if q.Capabilities.DeliverActive {
		t.Fatal("deliver must stay false when ingress list fails")
	}
	for _, call := range fixture.calls {
		if strings.HasPrefix(strings.Join(call.Args, " "), "session-message send") {
			t.Fatal("no send probe may run when the ingress list fails")
		}
	}
}

func TestMuseWorkerCapabilitiesAreIndependent(t *testing.T) {
	fixture := museFullFixture()
	fixture.execJSON = `{"stream":{"kind":"session","id":"` + museTestSession + `","payload_type":"other"}`
	q, err := QualifyMuseEndpoint(context.Background(), fixture.run, museTestSession)
	if err != nil {
		t.Fatal(err)
	}
	if !q.Capabilities.AttachExternal || !q.Capabilities.DeliverActive || !q.Capabilities.ReconcileDelivery {
		t.Fatal("attach, deliver, and reconcile must not depend on exec records")
	}
	if q.Capabilities.ObserveTurn || q.Capabilities.CaptureReply {
		t.Fatal("observe/reply must stay false without durable acceptance plus terminal")
	}
	if !q.Capabilities.ResumeIdle {
		t.Fatal("resume only needs the exact session in the second exec")
	}

	other := museFullFixture()
	other.exportJSON = `{"events":[]}`
	q, err = QualifyMuseEndpoint(context.Background(), other.run, museTestSession)
	if err != nil {
		t.Fatal(err)
	}
	if q.Capabilities.AttachExternal || q.Capabilities.ReconcileDelivery {
		t.Fatal("attach/reconcile must be false when the exact session is absent from the export")
	}
	if !q.Capabilities.DeliverActive {
		t.Fatal("deliver must be unaffected by an empty export")
	}
}

func TestMuseWorkerExportSettledRejectionFailsReconcile(t *testing.T) {
	for _, outcome := range []string{"rejected", "failed"} {
		fixture := museFullFixture()
		fixture.exportJSON = fmt.Sprintf(`{"events":[
			{"stream":{"kind":"session","id":"%s"},"causation_id":"cmd-1","payload_type":"runtime.command_intake.settled","payload":{"kind":"command_intake","record":{"kind":"settled","command_id":"cmd-1","outcome":{"kind":"%s"}}}}
		]}`, museTestSession, outcome)
		q, err := QualifyMuseEndpoint(context.Background(), fixture.run, museTestSession)
		if err != nil {
			t.Fatal(err)
		}
		if q.Capabilities.ReconcileDelivery {
			t.Fatalf("settled outcome %q must not prove reconcile capability", outcome)
		}
		if !q.Capabilities.AttachExternal {
			t.Fatal("the exact session is still present in the export, attach must hold")
		}
	}

	mismatched := museFullFixture()
	mismatched.exportJSON = fmt.Sprintf(`{"events":[
		{"stream":{"kind":"session","id":"%s"},"causation_id":"cmd-1","payload_type":"runtime.command_intake.settled","payload":{"kind":"command_intake","record":{"kind":"settled","command_id":"cmd-other","outcome":{"kind":"accepted"}}}}
	]}`, museTestSession)
	q, err := QualifyMuseEndpoint(context.Background(), mismatched.run, museTestSession)
	if err != nil {
		t.Fatal(err)
	}
	if q.Capabilities.ReconcileDelivery {
		t.Fatal("command_id not equal to causation_id must not prove reconcile capability")
	}
}

func TestMuseWorkerIngressClosedFailsDeliverActive(t *testing.T) {
	fixture := museFullFixture()
	fixture.sendJSON = `{"error":"external_agent_ingress_closed"}`
	q, err := QualifyMuseEndpoint(context.Background(), fixture.run, museTestSession)
	if err != nil {
		t.Fatal(err)
	}
	if q.Capabilities.DeliverActive {
		t.Fatal("external_agent_ingress_closed output must fail active delivery closed")
	}
	if !q.Capabilities.AttachExternal || !q.Capabilities.ResumeIdle {
		t.Fatal("other capabilities must remain independently proven")
	}
}

func TestMuseWorkerSendDelivery(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	identity := seedWorkerRun(t, store, "attempt-1", 1, 1, "muse", museTestSession)
	delivery, _, err := store.PutWorkerDelivery(WorkerDelivery{Identity: identity, Kind: "instruction", Body: "go", IdempotencyKey: "k-muse"})
	if err != nil {
		t.Fatal(err)
	}
	fixture := museFullFixture()
	if err := SendMuseWorkerDelivery(context.Background(), store, delivery.DeliveryID, fixture.run); err != nil {
		t.Fatal(err)
	}
	if len(fixture.calls) != 1 {
		t.Fatalf("send must issue exactly one command, got %#v", fixture.calls)
	}
	call := fixture.calls[0]
	if strings.Join(call.Args, " ") != "session-message send --target "+museTestSession+" --json" {
		t.Fatalf("unexpected send argv %q", strings.Join(call.Args, " "))
	}
	if !strings.Contains(call.Stdin, "[Tusker delivery "+delivery.DeliveryID+"]") {
		t.Fatal("send stdin must embed the delivery tag")
	}
	stored, _, _ := store.WorkerDelivery(delivery.DeliveryID)
	if stored.State != "accepted" {
		t.Fatalf("expected accepted, got %q", stored.State)
	}
}

func TestMuseWorkerSendCrashRecovery(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	identity := seedWorkerRun(t, store, "attempt-1", 1, 1, "muse", museTestSession)
	delivery, _, err := store.PutWorkerDelivery(WorkerDelivery{Identity: identity, Kind: "question", Body: "?", IdempotencyKey: "k-muse-crash"})
	if err != nil {
		t.Fatal(err)
	}
	if _, claimed, err := store.ClaimWorkerDelivery(delivery.DeliveryID); err != nil || !claimed {
		t.Fatal(err)
	}
	fixture := museFullFixture()
	if err := SendMuseWorkerDelivery(context.Background(), store, delivery.DeliveryID, fixture.run); err == nil {
		t.Fatal("crashed in-flight delivery must surface an error")
	}
	if len(fixture.calls) != 0 {
		t.Fatal("crashed delivery must not resend")
	}
	stored, _, _ := store.WorkerDelivery(delivery.DeliveryID)
	if stored.State != "delivering" {
		t.Fatalf("a competing send must not mutate the in-flight row, got %q", stored.State)
	}
}

func TestMuseWorkerQualificationPersists(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	fixture := museFullFixture()
	q, err := QualifyMuseEndpoint(context.Background(), fixture.run, museTestSession)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveWorkerProviderQualification(q); err != nil {
		t.Fatal(err)
	}
	stored, err := store.WorkerProviderQualifications()
	if err != nil || len(stored) != 1 {
		t.Fatalf("qualification must persist, got %#v err=%v", stored, err)
	}
	if !stored[0].Capabilities.DeliverActive || stored[0].Capabilities.Evidence == "" {
		t.Fatalf("persisted qualification must carry capabilities and evidence, got %#v", stored[0])
	}
}

func TestMuseWorkerSendReceiptDropsProviderSecrets(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	identity := seedWorkerRun(t, store, "attempt-1", 1, 1, "muse", museTestSession)
	delivery, _, err := store.PutWorkerDelivery(WorkerDelivery{Identity: identity, Kind: "instruction", Body: "go", IdempotencyKey: "k-secret"})
	if err != nil {
		t.Fatal(err)
	}
	fixture := museFullFixture()
	fixture.sendJSON = `{"event_id":"muse-evt-9","target":"` + museTestSession + `","token":"sk-sentinel-secret-123","echo":"sk-sentinel-secret-123 body"}`
	if err := SendMuseWorkerDelivery(context.Background(), store, delivery.DeliveryID, fixture.run); err != nil {
		t.Fatal(err)
	}
	stored, _, _ := store.WorkerDelivery(delivery.DeliveryID)
	if stored.State != "accepted" {
		t.Fatalf("expected accepted, got %q", stored.State)
	}
	if stored.ProviderReceipt["event_id"] != "muse-evt-9" || stored.ProviderReceipt["target"] != museTestSession {
		t.Fatalf("receipt must keep only curated fields, got %#v", stored.ProviderReceipt)
	}
	raw, err := json.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "sk-sentinel-secret-123") {
		t.Fatal("provider output secrets must never persist into the delivery receipt")
	}
}

func TestMuseWorkerReconcileRequiresProviderDirectionAndTag(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	identity := seedWorkerRun(t, store, "attempt-1", 1, 1, "muse", museTestSession)
	delivery, _, err := store.PutWorkerDelivery(WorkerDelivery{Identity: identity, Kind: "question", Body: "?", IdempotencyKey: "k-dir"})
	if err != nil {
		t.Fatal(err)
	}
	if _, claimed, err := store.ClaimWorkerDelivery(delivery.DeliveryID); err != nil || !claimed {
		t.Fatal(err)
	}
	if err := store.MarkWorkerDeliveryAccepted(delivery.DeliveryID, nil, "2026-07-30T22:00:00Z"); err != nil {
		t.Fatal(err)
	}
	tag := "[Tusker delivery " + delivery.DeliveryID + "]"
	fixture := museFullFixture()
	fixture.listJSON = `{"events":[
		{"event_id":"m-out","session_id":"` + museTestSession + `","direction":"outbound","text":"` + tag + ` question","created_at":"2026-07-30T22:20:01Z"},
		{"event_id":"m-wrong","session_id":"` + museTestSession + `","direction":"user","text":"operator note ` + tag + `","created_at":"2026-07-30T22:20:02Z"},
		{"event_id":"m-untagged","session_id":"` + museTestSession + `","direction":"assistant","text":"progress without tag","created_at":"2026-07-30T22:20:03Z"}
	]}`
	if err := ReconcileMuseWorker(context.Background(), store, identity, fixture.run, time.Now().UTC(), time.Minute); err != nil {
		t.Fatal(err)
	}
	stored, _, _ := store.WorkerDelivery(delivery.DeliveryID)
	if stored.WorkerActivityObservedAt != "2026-07-30T22:20:01Z" {
		t.Fatalf("observation must record the tagged outbound timestamp, got %#v", stored)
	}
	if stored.State != "accepted" {
		t.Fatalf("wrong-direction and untagged records must not resolve the reply, got %#v", stored)
	}
	fixture.listJSON = `{"events":[
		{"event_id":"m-out","session_id":"` + museTestSession + `","direction":"outbound","text":"` + tag + ` question","created_at":"2026-07-30T22:20:01Z"},
		{"event_id":"m-reply","session_id":"` + museTestSession + `","direction":"assistant","text":"answer ` + tag + `","created_at":"2026-07-30T22:20:04Z"}
	]}`
	if err := ReconcileMuseWorker(context.Background(), store, identity, fixture.run, time.Now().UTC(), time.Minute); err != nil {
		t.Fatal(err)
	}
	stored, _, _ = store.WorkerDelivery(delivery.DeliveryID)
	if stored.State != "replied" || stored.CorrelatedReplyEventID != "m-reply" {
		t.Fatalf("provider-direction tagged record must correlate the reply, got %#v", stored)
	}
}

func TestMuseWorkerConcurrentSendInFlight(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	identity := seedWorkerRun(t, store, "attempt-1", 1, 1, "muse", museTestSession)
	delivery, _, err := store.PutWorkerDelivery(WorkerDelivery{Identity: identity, Kind: "instruction", Body: "go", IdempotencyKey: "k-mrace"})
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	var sends atomic.Int32
	runner := func(ctx context.Context, command MuseCommand) (string, error) {
		if strings.HasPrefix(strings.Join(command.Args, " "), "session-message send") {
			sends.Add(1)
			close(entered)
			<-release
			return `{"target":"` + museTestSession + `","event_id":"m-1"}`, nil
		}
		return "", fmt.Errorf("unexpected args")
	}
	done := make(chan error, 1)
	go func() { done <- SendMuseWorkerDelivery(context.Background(), store, delivery.DeliveryID, runner) }()
	<-entered
	if err := SendMuseWorkerDelivery(context.Background(), store, delivery.DeliveryID, runner); err == nil {
		t.Fatal("concurrent sender must fail closed while in flight")
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
	if sends.Load() != 1 {
		t.Fatalf("exactly one provider send may occur, got %d", sends.Load())
	}
}
