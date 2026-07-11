package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	serveStreamHeartbeatInterval = 15 * time.Second
	serveStreamClientBuffer      = 16
)

type serveStreamEvent struct {
	ID           int64    `json:"id"`
	Kind         string   `json:"kind"`
	Project      string   `json:"project,omitempty"`
	TaskID       string   `json:"task_id,omitempty"`
	Title        string   `json:"title,omitempty"`
	Status       string   `json:"status,omitempty"`
	Urgency      string   `json:"urgency,omitempty"`
	DeepLinkPath string   `json:"deep_link_path,omitempty"`
	OccurredAt   string   `json:"occurred_at,omitempty"`
	Keys         []string `json:"keys"`
}

type serveStreamBroker struct {
	mu                sync.Mutex
	nextID            int
	nextEventID       int64
	clients           map[int]serveStreamClient
	closed            bool
	heartbeatInterval time.Duration
}

type serveStreamClient struct {
	events  chan serveStreamEvent
	project string
}

func newServeStreamBroker() *serveStreamBroker {
	return &serveStreamBroker{
		clients:           map[int]serveStreamClient{},
		heartbeatInterval: serveStreamHeartbeatInterval,
	}
}

func (b *serveStreamBroker) Subscribe() (<-chan serveStreamEvent, func(), bool) {
	return b.SubscribeProject("")
}

func (b *serveStreamBroker) SubscribeProject(projectID string) (<-chan serveStreamEvent, func(), bool) {
	if b == nil {
		return nil, func() {}, false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, func() {}, false
	}
	b.nextID++
	id := b.nextID
	ch := make(chan serveStreamEvent, serveStreamClientBuffer)
	b.clients[id] = serveStreamClient{events: ch, project: strings.TrimSpace(projectID)}
	return ch, func() { b.remove(id) }, true
}

func (b *serveStreamBroker) Broadcast(event serveStreamEvent) {
	if b == nil {
		return
	}
	event.Kind = strings.TrimSpace(event.Kind)
	event.Keys = serveStreamKeys(event.Keys...)
	if event.Kind == "" || len(event.Keys) == 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.nextEventID++
	event.ID = b.nextEventID
	if strings.TrimSpace(event.OccurredAt) == "" {
		event.OccurredAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	for id, client := range b.clients {
		if client.project != "" && event.Project != "" && client.project != event.Project {
			continue
		}
		select {
		case client.events <- event:
		default:
			close(client.events)
			delete(b.clients, id)
		}
	}
}

func (b *serveStreamBroker) Close() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for id, client := range b.clients {
		close(client.events)
		delete(b.clients, id)
	}
}

func (b *serveStreamBroker) remove(id int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	client, ok := b.clients[id]
	if !ok {
		return
	}
	close(client.events)
	delete(b.clients, id)
}

func (b *serveStreamBroker) attendedProjects() []string {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	seen := map[string]struct{}{}
	for _, client := range b.clients {
		if client.project != "" {
			seen[client.project] = struct{}{}
		}
	}
	projects := make([]string, 0, len(seen))
	for projectID := range seen {
		projects = append(projects, projectID)
	}
	sort.Strings(projects)
	return projects
}

func (b *serveStreamBroker) clientCount() int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.clients)
}

func (b *serveStreamBroker) heartbeatEvery() time.Duration {
	if b == nil || b.heartbeatInterval <= 0 {
		return serveStreamHeartbeatInterval
	}
	return b.heartbeatInterval
}

func (s *serveServer) handleStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "read-only stream", http.StatusMethodNotAllowed)
		return
	}
	if s.stream == nil {
		http.Error(w, "stream unavailable", http.StatusServiceUnavailable)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	ch, unsubscribe, ok := s.stream.SubscribeProject(r.URL.Query().Get("project"))
	if !ok {
		http.Error(w, "stream closed", http.StatusServiceUnavailable)
		return
	}
	defer unsubscribe()

	header := w.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-store")
	header.Set("Connection", "keep-alive")
	header.Set("X-Accel-Buffering", "no")

	_, _ = fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	heartbeat := time.NewTicker(s.stream.heartbeatEvery())
	defer heartbeat.Stop()
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			if err := writeServeStreamEvent(ctx, w, flusher, event); err != nil {
				return
			}
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func writeServeStreamEvent(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, event serveStreamEvent) error {
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", raw); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

func serveStreamKeys(keys ...string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	return out
}

func serveStreamRunKeys(recordID string) []string {
	keys := []string{"daemon", "projects", "needs", "runs"}
	recordID = strings.TrimSpace(recordID)
	if recordID != "" {
		keys = append(keys, "runs:"+recordID, "tasks:"+recordID)
	}
	return serveStreamKeys(keys...)
}

func serveStreamTaskKeys(recordID string) []string {
	keys := []string{"projects", "needs", "tasks", "runs"}
	recordID = strings.TrimSpace(recordID)
	if recordID != "" {
		keys = append(keys, "tasks:"+recordID, "runs:"+recordID)
	}
	return serveStreamKeys(keys...)
}
