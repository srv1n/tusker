package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type mcpWorkerIdentity struct {
	ProjectID, ItemID, AttemptID  string
	LeaseGeneration, WorkRevision int
	Runner                        RunnerName
}

func validateMCPWorkerIdentity(store *RuntimeStore) (mcpWorkerIdentity, error) {
	id := mcpWorkerIdentity{ProjectID: strings.TrimSpace(os.Getenv("TUSKER_PROJECT_ID")), ItemID: strings.TrimSpace(os.Getenv("TUSKER_ITEM_ID")), AttemptID: strings.TrimSpace(os.Getenv("TUSKER_ATTEMPT_ID"))}
	var err error
	id.LeaseGeneration, err = strconv.Atoi(strings.TrimSpace(os.Getenv("TUSKER_LEASE_GENERATION")))
	if err != nil || id.LeaseGeneration < 1 {
		return mcpWorkerIdentity{}, errors.New("worker lease identity is missing or invalid")
	}
	id.WorkRevision, err = strconv.Atoi(strings.TrimSpace(os.Getenv("TUSKER_WORK_REVISION")))
	if err != nil || id.WorkRevision < 0 {
		return mcpWorkerIdentity{}, errors.New("worker revision identity is missing or invalid")
	}
	if id.ProjectID == "" || id.ItemID == "" || id.AttemptID == "" {
		return mcpWorkerIdentity{}, errors.New("worker attempt identity is missing")
	}
	if store == nil {
		return mcpWorkerIdentity{}, errors.New("worker runtime store is unavailable")
	}
	run, err := store.FindRunScoped(id.ProjectID, id.ItemID)
	if err != nil || run == nil {
		return mcpWorkerIdentity{}, errors.New("worker run is unavailable")
	}
	if run.ItemID != id.ItemID || run.ActiveAttemptID != id.AttemptID || run.LeaseGeneration != id.LeaseGeneration || run.WorkRevision != id.WorkRevision || run.Terminal ||
		(run.LeaseState != string(LeaseStateClaimed) && run.LeaseState != string(LeaseStateRunning)) {
		return mcpWorkerIdentity{}, errors.New("worker attempt is no longer active")
	}
	id.Runner = RunnerName(run.Runner)
	return id, nil
}

type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type mcpToolCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
	Meta      struct {
		ProgressToken json.RawMessage `json:"progressToken"`
	} `json:"_meta"`
}

func mcpServeCmd(args Args) error {
	maxWait := 900
	if value := args.String("max-wait"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 0 {
			return errors.New("--max-wait must be a nonnegative number of seconds")
		}
		maxWait = parsed
	}
	if maxWait > 900 {
		maxWait = 900
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	return serveMCP(os.Stdin, os.Stdout, store, time.Duration(maxWait)*time.Second)
}

func serveMCP(input io.Reader, output io.Writer, store *RuntimeStore, maxWait time.Duration) error {
	var writeMu, pendingMu sync.Mutex
	pending := map[string]context.CancelFunc{}
	var calls sync.WaitGroup
	write := func(value any) {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = json.NewEncoder(output).Encode(value)
	}
	response := func(id json.RawMessage, result any, rpcErr any) {
		body := map[string]any{"jsonrpc": "2.0", "id": id}
		if rpcErr != nil {
			body["error"] = rpcErr
		} else {
			body["result"] = result
		}
		write(body)
	}
	reader := bufio.NewReaderSize(input, 1<<20)
	for {
		line, readErr := reader.ReadSlice('\n')
		if errors.Is(readErr, bufio.ErrBufferFull) {
			for errors.Is(readErr, bufio.ErrBufferFull) {
				_, readErr = reader.ReadSlice('\n')
			}
			response(nil, nil, map[string]any{"code": -32700, "message": "malformed JSON-RPC request"})
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				return readErr
			}
			continue
		}
		if readErr == io.EOF && len(line) == 0 {
			break
		}
		if readErr != nil && readErr != io.EOF {
			return readErr
		}
		if len(line) > 1<<20 {
			response(nil, nil, map[string]any{"code": -32700, "message": "malformed JSON-RPC request"})
			continue
		}
		var request mcpRequest
		if !json.Valid(line) {
			response(nil, nil, map[string]any{"code": -32700, "message": "malformed JSON-RPC request"})
			continue
		}
		if err := json.Unmarshal(line, &request); err != nil || request.JSONRPC != "2.0" || request.Method == "" {
			response(request.ID, nil, map[string]any{"code": -32600, "message": "invalid JSON-RPC request"})
			continue
		}
		if request.Method == "notifications/initialized" {
			continue
		}
		if request.Method == "notifications/cancelled" {
			var p struct {
				RequestID json.RawMessage `json:"requestId"`
			}
			_ = json.Unmarshal(request.Params, &p)
			pendingMu.Lock()
			if cancel := pending[string(p.RequestID)]; cancel != nil {
				cancel()
			}
			pendingMu.Unlock()
			continue
		}
		if len(request.ID) == 0 {
			continue
		}
		if string(request.ID) == "null" {
			response(request.ID, nil, map[string]any{"code": -32600, "message": "invalid JSON-RPC request"})
			continue
		}
		switch request.Method {
		case "initialize":
			var p struct {
				ProtocolVersion string `json:"protocolVersion"`
			}
			_ = json.Unmarshal(request.Params, &p)
			version := p.ProtocolVersion
			if version != "2024-11-05" && version != "2025-03-26" && version != "2025-06-18" {
				version = "2025-06-18"
			}
			response(request.ID, map[string]any{"protocolVersion": version, "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "tusker", "version": "1"}}, nil)
		case "ping":
			response(request.ID, map[string]any{}, nil)
		case "tools/list":
			response(request.ID, map[string]any{"tools": mcpToolDefinitions()}, nil)
		case "tools/call":
			var call mcpToolCall
			if err := json.Unmarshal(request.Params, &call); err != nil {
				response(request.ID, nil, map[string]any{"code": -32602, "message": "invalid tool call"})
				continue
			}
			ctx, cancel := context.WithCancel(context.Background())
			key := string(request.ID)
			pendingMu.Lock()
			pending[key] = cancel
			pendingMu.Unlock()
			calls.Add(1)
			go func(req mcpRequest, call mcpToolCall) {
				defer calls.Done()
				defer func() { pendingMu.Lock(); delete(pending, key); pendingMu.Unlock(); cancel() }()
				result, err := callMCPTool(ctx, store, call, maxWait, func(elapsed time.Duration) {
					if len(call.Meta.ProgressToken) > 0 {
						write(map[string]any{"jsonrpc": "2.0", "method": "notifications/progress", "params": map[string]any{"progressToken": call.Meta.ProgressToken, "progress": int(elapsed.Seconds()), "message": "waiting for answer"}})
					}
				})
				if err != nil {
					response(req.ID, mcpToolResult(err.Error(), true), nil)
				} else {
					response(req.ID, mcpToolResult(result, false), nil)
				}
			}(request, call)
		default:
			response(request.ID, nil, map[string]any{"code": -32601, "message": "method not found"})
		}
	}
	pendingMu.Lock()
	for _, cancel := range pending {
		cancel()
	}
	pendingMu.Unlock()
	calls.Wait()
	return nil
}

func mcpToolDefinitions() []map[string]any {
	return []map[string]any{
		{"name": "ask", "description": "Ask an architect, operator, or named peer a question.", "inputSchema": map[string]any{"type": "object", "required": []string{"to", "question"}, "properties": map[string]any{"to": map[string]any{"type": "string"}, "question": map[string]any{"type": "string"}, "wait_seconds": map[string]any{"type": "integer", "minimum": 0}}}},
		{"name": "post_update", "description": "Post a brief progress update to the run event stream.", "inputSchema": map[string]any{"type": "object", "required": []string{"text"}, "properties": map[string]any{"text": map[string]any{"type": "string"}}}},
		{"name": "check_messages", "description": "Read and consume pending messages for this task.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}},
	}
}

func mcpToolResult(value string, isError bool) map[string]any {
	return map[string]any{"content": []map[string]string{{"type": "text", "text": value}}, "isError": isError}
}

func callMCPTool(ctx context.Context, store *RuntimeStore, call mcpToolCall, maxWait time.Duration, progress func(time.Duration)) (string, error) {
	id, err := validateMCPWorkerIdentity(store)
	if err != nil {
		return "", err
	}
	switch call.Name {
	case "ask":
		var a struct {
			To          string `json:"to"`
			Question    string `json:"question"`
			WaitSeconds int    `json:"wait_seconds"`
		}
		if err := json.Unmarshal(call.Arguments, &a); err != nil {
			return "", errors.New("invalid ask arguments")
		}
		if a.WaitSeconds < 0 {
			return "", errors.New("wait_seconds must be nonnegative")
		}
		to := strings.TrimSpace(a.To)
		var recipient AgentAddress
		generation := 0
		if to == "operator" {
			recipient = AgentAddress{Kind: "operator", ID: "operator"}
		} else {
			role, name := to, ""
			if strings.HasPrefix(to, "peer:") && len(to) > len("peer:") {
				role, name = "peer", strings.TrimPrefix(to, "peer:")
			}
			if role != "architect" && role != "peer" {
				return "", errors.New("to must be architect, operator, or peer:<name>")
			}
			contact, _, err := store.ResolveEffectiveAgentContact(id.ProjectID, id.ItemID, role, name)
			if err != nil {
				if role == "architect" {
					return "", errors.New("architect contact is unavailable; ask operator")
				}
				return "", errors.New("peer contact is unavailable")
			}
			recipient, generation = contact.Address, contact.Generation
		}
		hash := sha256.Sum256([]byte(to + "\x00" + a.Question))
		key := "mcp:" + id.AttemptID + ":" + hex.EncodeToString(hash[:8])
		if a.WaitSeconds > 900 {
			a.WaitSeconds = 900
		}
		wait := time.Duration(a.WaitSeconds) * time.Second
		if wait > maxWait {
			wait = maxWait
		}
		message, err := store.agentMessageByKey(id.ProjectID, "task:"+id.ItemID, key)
		if errors.Is(err, sql.ErrNoRows) {
			candidate := AgentMessage{IdempotencyKey: key, ProjectID: id.ProjectID, Sender: "task:" + id.ItemID, Recipient: recipient, OriginTaskID: id.ItemID, WorkRevision: id.WorkRevision, RouteGeneration: id.LeaseGeneration, RecipientGeneration: generation, Kind: "question", Body: a.Question, ReplyRequired: true, YieldSender: a.WaitSeconds > 0}
			if err := candidate.validate(); err != nil {
				return "", err
			}
			message, _, err = store.PutAgentMessage(candidate)
			if err != nil {
				message, err = store.agentMessageByKey(id.ProjectID, "task:"+id.ItemID, key)
				if err != nil {
					return "", errors.New("question could not be recorded")
				}
			}
		}
		if err != nil {
			return "", errors.New("question could not be read")
		}
		return waitMCPAnswer(ctx, store, id.ProjectID, id.ItemID, message.ID, wait, progress)
	case "post_update":
		var a struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(call.Arguments, &a); err != nil || strings.TrimSpace(a.Text) == "" || len(a.Text) > 4096 {
			return "", errors.New("text must contain 1 to 4096 bytes")
		}
		sink := strings.TrimSpace(os.Getenv("TUSKER_EVENT_SINK"))
		if sink == "" {
			return "", errors.New("worker event sink is unavailable")
		}
		if err := NewEventLog(sink).Append("worker_update", id.AttemptID, id.Runner, map[string]any{"text": a.Text}); err != nil {
			return "", errors.New("update could not be recorded")
		}
		return "update recorded", nil
	case "check_messages":
		messages, err := store.ListAgentMessages(id.ProjectID, "task", id.ItemID)
		if err != nil {
			return "", errors.New("messages are unavailable")
		}
		out := make([]map[string]string, 0)
		for _, m := range messages {
			if m.ConsumedAt != "" || m.TransportState == "delivered" || m.WorkRevision != id.WorkRevision || (m.Kind != "answer" && m.Kind != "instruction" && m.Kind != "notice" && m.Kind != "question") {
				continue
			}
			claimed, err := store.ConsumeAgentMessage(id.ProjectID, m.ID)
			if err != nil {
				return "", errors.New("messages could not be consumed")
			}
			if claimed {
				out = append(out, map[string]string{"id": m.ID, "kind": m.Kind, "sender": m.Sender, "replyTo": m.ReplyTo, "body": m.Body})
			}
		}
		if len(out) == 0 {
			return "no pending messages", nil
		}
		raw, _ := json.Marshal(out)
		return string(raw), nil
	default:
		return "", errors.New("unknown tool")
	}
}

func waitMCPAnswer(ctx context.Context, store *RuntimeStore, projectID, itemID, questionID string, wait time.Duration, progress func(time.Duration)) (string, error) {
	started := time.Now()
	deadline := time.NewTimer(wait)
	defer deadline.Stop()
	poll := time.NewTicker(2 * time.Second)
	defer poll.Stop()
	progressTick := time.NewTicker(15 * time.Second)
	defer progressTick.Stop()
	for {
		if wait > 0 {
			if err := store.markAgentQuestionAwaiting(projectID, questionID, time.Now().UTC().Add(5*time.Second)); err != nil {
				return "", err
			}
		}
		messages, err := store.ListAgentMessages(projectID, "task", itemID)
		if err != nil {
			return "", errors.New("answer is unavailable")
		}
		for _, m := range messages {
			if m.Kind == "answer" && m.ReplyTo == questionID {
				claimed, err := store.ConsumeAgentMessage(projectID, m.ID)
				if err != nil {
					return "", errors.New("answer could not be consumed")
				}
				if !claimed {
					continue
				}
				return fmt.Sprintf("question %s answered by %s: %s", questionID, m.ID, m.Body), nil
			}
		}
		if wait <= 0 {
			break
		}
		select {
		case <-ctx.Done():
			return fmt.Sprintf("question %s remains pending after cancellation", questionID), nil
		case <-deadline.C:
			return fmt.Sprintf("question %s is recorded and pending. If you cannot continue without it, say so and end your turn; the answer will arrive by soft delivery, hard say, or continue.", questionID), nil
		case <-poll.C:
		case <-progressTick.C:
			progress(time.Since(started))
		}
	}
	return fmt.Sprintf("question %s is recorded and pending. If you cannot continue without it, say so and end your turn; the answer will arrive by soft delivery, hard say, or continue.", questionID), nil
}

func (s *RuntimeStore) markAgentQuestionAwaiting(project, question string, until time.Time) error {
	_, err := s.exec(`UPDATE agent_messages SET awaiting_until=? WHERE project_id=? AND id=? AND kind='question'`, until.Format(time.RFC3339Nano), project, question)
	return err
}
