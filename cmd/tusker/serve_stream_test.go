package main

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func TestServeStreamEmitsInvalidationEvents(t *testing.T) {
	server, broker, closeServer := newServeStreamTestServer(t)
	defer closeServer()

	reader, closeStream := openServeStream(t, server)
	defer closeStream()

	events := []serveStreamEvent{
		{Kind: serveStreamKindPollTick, Keys: []string{"daemon", "runs"}},
		{Kind: serveStreamKindDispatch, Keys: serveStreamRunKeys("APP-T-0001")},
		{Kind: serveStreamKindLeaseTransition, Keys: serveStreamRunKeys("APP-T-0001")},
		{Kind: serveStreamKindTaskStatusChange, Keys: serveStreamTaskKeys("APP-T-0001")},
		{Kind: serveStreamKindReviewBatch, Keys: []string{"review:batch", "needs"}},
	}
	for _, event := range events {
		broker.Broadcast(event)
		got, raw := readServeStreamEvent(t, reader)
		assertEqual(t, event.Kind, got.Kind, "stream event kind")
		if len(got.Keys) == 0 {
			t.Fatalf("expected invalidation keys for %s", event.Kind)
		}
		if len(raw) != 2 || raw["kind"] == nil || raw["keys"] == nil {
			t.Fatalf("stream event must contain only kind and keys, got %s", raw)
		}
	}

	resp, err := server.Client().Post(server.URL+"/api/stream", "text/plain", strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected read-only stream endpoint to reject POST, got %d", resp.StatusCode)
	}
}

func TestStreamHeartbeat(t *testing.T) {
	server, _, closeServer := newServeStreamTestServer(t)
	defer closeServer()

	reader, closeStream := openServeStream(t, server)
	defer closeStream()

	line := readServeStreamLine(t, reader)
	if !strings.HasPrefix(line, ": heartbeat") {
		t.Fatalf("expected heartbeat comment, got %q", line)
	}
}

func TestStreamReconnectUsesFreshSubscription(t *testing.T) {
	server, broker, closeServer := newServeStreamTestServer(t)
	defer closeServer()

	_, closeFirst := openServeStream(t, server)
	closeFirst()

	reader, closeSecond := openServeStream(t, server)
	defer closeSecond()
	broker.Broadcast(serveStreamEvent{Kind: serveStreamKindPollTick, Keys: []string{"daemon"}})
	got, _ := readServeStreamEvent(t, reader)
	assertEqual(t, serveStreamKindPollTick, got.Kind, "reconnected stream event kind")
}

func TestStreamShutdownAndSlowClient(t *testing.T) {
	_, broker, closeServer := newServeStreamTestServer(t)
	defer closeServer()

	ch, unsubscribe, ok := broker.Subscribe()
	if !ok {
		t.Fatal("expected slow test subscription")
	}
	defer unsubscribe()
	start := time.Now()
	for i := 0; i < serveStreamClientBuffer+2; i++ {
		broker.Broadcast(serveStreamEvent{Kind: serveStreamKindLeaseTransition, Keys: []string{"runs"}})
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("broadcast to a slow client blocked too long")
	}
	if broker.clientCount() != 0 {
		t.Fatalf("expected slow client to be dropped, count=%d", broker.clientCount())
	}
	if _, ok := <-ch; !ok {
		t.Fatal("expected buffered event before dropped channel closes")
	}

	server, broker, closeServer := newServeStreamTestServer(t)
	defer closeServer()
	reader, closeStream := openServeStream(t, server)
	defer closeStream()
	broker.Close()
	if _, err := reader.ReadString('\n'); err != io.EOF {
		t.Fatalf("expected stream EOF after broker shutdown, got %v", err)
	}
}

func newServeStreamTestServer(t *testing.T) (*httptest.Server, *serveStreamBroker, func()) {
	t.Helper()
	stateRoot := t.TempDir()
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	assets := fstest.MapFS{"index.html": {Data: []byte("stream fixture")}}
	handler := newServeServer(t.TempDir(), t.TempDir(), "127.0.0.1:0", store, assets)
	broker := newServeStreamBroker()
	broker.heartbeatInterval = 10 * time.Millisecond
	handler.stream = broker
	server := httptest.NewServer(handler)
	return server, broker, func() {
		broker.Close()
		server.Close()
		_ = store.Close()
	}
}

func openServeStream(t *testing.T, server *httptest.Server) (*bufio.Reader, func()) {
	t.Helper()
	return openServeStreamURL(t, server.Client(), server.URL+"/api/stream")
}

func openServeStreamURL(t *testing.T, client *http.Client, url string) (*bufio.Reader, func()) {
	t.Helper()
	resp, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		t.Fatalf("stream status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	reader := bufio.NewReader(resp.Body)
	line := readServeStreamLine(t, reader)
	if !strings.HasPrefix(line, ": connected") {
		t.Fatalf("expected initial connected comment, got %q", line)
	}
	if blank := readServeStreamLine(t, reader); blank != "" {
		t.Fatalf("expected blank line after initial comment, got %q", blank)
	}
	return reader, func() { _ = resp.Body.Close() }
}

func readServeStreamEvent(t *testing.T, reader *bufio.Reader) (serveStreamEvent, map[string]json.RawMessage) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for SSE data")
		default:
		}
		line := readServeStreamLine(t, reader)
		if strings.HasPrefix(line, "event: ") {
			t.Fatalf("stream events must use the default SSE message event for EventSource.onmessage, got %q", line)
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		rawData := strings.TrimSpace(strings.TrimPrefix(line, "data: "))
		var raw map[string]json.RawMessage
		if err := json.Unmarshal([]byte(rawData), &raw); err != nil {
			t.Fatalf("decode raw stream event: %v: %s", err, rawData)
		}
		var event serveStreamEvent
		if err := json.Unmarshal([]byte(rawData), &event); err != nil {
			t.Fatalf("decode stream event: %v: %s", err, rawData)
		}
		return event, raw
	}
}

func readServeStreamLine(t *testing.T, reader *bufio.Reader) string {
	t.Helper()
	lineCh := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		line, err := reader.ReadString('\n')
		if err != nil {
			errCh <- err
			return
		}
		lineCh <- strings.TrimRight(line, "\r\n")
	}()
	select {
	case line := <-lineCh:
		return line
	case err := <-errCh:
		t.Fatalf("read stream line: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out reading stream line")
	}
	return ""
}
