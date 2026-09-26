package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const messageInboxLimit = 16 << 10

// agentMessageHookCmd prints the ready-to-paste mailbox hook block for an
// interactive architect session. It never writes a settings file.
func agentMessageHookCmd(args Args) error {
	if strings.TrimSpace(args.String("_pos")) != "print" {
		return tuskerError(errorMissingArg, "Usage: tusker message hook print --harness claude|codex [--project <id>] [--json]")
	}
	return runAgentMessageHookPrint(args, os.Stdout)
}

func runAgentMessageHookPrint(args Args, out io.Writer) error {
	harness := strings.ToLower(strings.TrimSpace(args.String("harness")))
	var target, note string
	switch harness {
	case "claude":
		target = `.claude/settings.json or ~/.claude/settings.json under "hooks"`
	case "codex":
		target = "hooks.json"
		note = "unverified: Codex hook output is not confirmed to reach exec context"
	default:
		return tuskerError(errorMissingArg, "message hook print requires --harness claude|codex")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, resolveErr := filepath.EvalSymlinks(exe); resolveErr == nil {
		exe = resolved
	}
	if !filepath.IsAbs(exe) {
		return tuskerError(errorConfigInvalid, "tusker binary path is not absolute")
	}
	project := strings.TrimSpace(args.String("project"))
	if project == "" {
		project = "<project-id>"
	}
	hookCommand := shellSingleQuote(exe) + " message inbox --project " + shellSingleQuote(project) + " --format hook"
	if stateRoot := strings.TrimSpace(os.Getenv("TUSKER_STATE_ROOT")); stateRoot != "" {
		hookCommand = "TUSKER_STATE_ROOT=" + shellSingleQuote(stateRoot) + " " + hookCommand
	}
	entry := []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": hookCommand}}}}
	snippet := map[string]any{"hooks": map[string]any{"UserPromptSubmit": entry, "Stop": entry}}
	if args.Bool("json") {
		enc := json.NewEncoder(out)
		enc.SetEscapeHTML(false)
		return enc.Encode(map[string]any{
			"ok": true, "harness": harness, "binary": exe, "project": project,
			"paste_into": target, "verified": note == "", "note": note,
			"hook_config": snippet,
		})
	}
	var snippetJSON bytes.Buffer
	enc := json.NewEncoder(&snippetJSON)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(snippet); err != nil {
		return err
	}
	if note != "" {
		fmt.Fprintf(out, "Paste into %s (%s):\n%s", target, note, snippetJSON.String())
		return nil
	}
	fmt.Fprintf(out, "Paste into %s:\n%s", target, snippetJSON.String())
	return nil
}

// Hook failures are deliberately silent on stdout and successful to the host.
func agentMessageInboxCmd(args Args) error {
	hook := firstNonEmpty(args.String("format"), "text") == "hook"
	if err := runAgentMessageInbox(args, os.Stdin, os.Stdout); err != nil {
		if hook {
			fmt.Fprintln(os.Stderr, "tusker message inbox: "+strings.ReplaceAll(err.Error(), "\n", " "))
			return nil
		}
		return err
	}
	return nil
}

func runAgentMessageInbox(args Args, in io.Reader, out io.Writer) error {
	project := strings.TrimSpace(args.String("project"))
	format := firstNonEmpty(args.String("format"), "text")
	if project == "" || (format != "hook" && format != "text" && format != "json") {
		return errors.New("inbox requires --project and --format hook|text|json")
	}
	if args.String("run") != "" && args.String("for") != "" {
		return errors.New("inbox accepts either --run or --for")
	}
	var event, sessionID string
	if format == "hook" {
		var input struct {
			Event     string `json:"hook_event_name"`
			SessionID string `json:"session_id"`
		}
		if err := json.NewDecoder(io.LimitReader(in, 1<<20)).Decode(&input); err != nil {
			return err
		}
		event, sessionID = input.Event, input.SessionID
		if event != "PostToolUse" && event != "UserPromptSubmit" && event != "Stop" {
			return nil
		}
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	var recipients []AgentAddress
	if attempt := strings.TrimSpace(args.String("run")); attempt != "" {
		identity, err := validateMCPWorkerIdentity(store)
		if err != nil {
			return err
		}
		if identity.AttemptID != attempt || identity.ProjectID != project {
			return errors.New("inbox run is not the live owner")
		}
		recipients = []AgentAddress{{Kind: "task", ID: identity.ItemID}}
	} else if target := strings.TrimSpace(args.String("for")); target != "" {
		if target == "operator" {
			recipients = []AgentAddress{{Kind: "operator", ID: "operator"}}
		} else {
			address, err := parseAgentAddress(target)
			if err != nil {
				return err
			}
			recipients = []AgentAddress{address}
		}
	} else if format == "hook" && sessionID != "" {
		recipients, err = store.messageInboxSessionRecipients(project, sessionID)
		if err != nil {
			return err
		}
	} else {
		return errors.New("inbox requires --run or --for")
	}
	var pending []AgentMessage
	for _, recipient := range recipients {
		messages, err := store.messageInboxPending(project, recipient)
		if err != nil {
			return err
		}
		pending = append(pending, messages...)
	}
	if len(pending) == 0 {
		if format == "json" {
			return json.NewEncoder(out).Encode(map[string]any{"messages": []AgentMessage{}})
		}
		return nil
	}
	selected := make([]AgentMessage, 0, len(pending))
	size := 0
	omitted := 0
	for _, message := range pending {
		if len(message.Body) > messageInboxLimit/2 {
			message.Body = message.Body[:messageInboxLimit/2] + "\n[truncated; full text: tusker message show --project " + fmt.Sprintf("%q %s", project, message.ID) + "]"
		}
		line := renderInboxMessage(project, message)
		if size+len(line)+100 > messageInboxLimit {
			omitted++
			continue
		}
		if format == "hook" || args.Bool("mark-delivered") {
			claimed, err := store.markInboxDelivered(project, message.ID)
			if err != nil {
				return err
			}
			if !claimed {
				continue
			}
		}
		selected = append(selected, message)
		size += len(line)
	}
	if len(selected) == 0 {
		if format == "json" {
			return json.NewEncoder(out).Encode(map[string]any{"messages": []AgentMessage{}})
		}
		return nil
	}
	if format == "json" {
		return json.NewEncoder(out).Encode(map[string]any{"messages": selected, "omitted": omitted})
	}
	var text strings.Builder
	for _, message := range selected {
		text.WriteString(renderInboxMessage(project, message))
	}
	if omitted > 0 {
		fmt.Fprintf(&text, "%d more messages omitted; they remain pending.\n", omitted)
	}
	if format == "text" {
		_, err = io.WriteString(out, text.String())
		return err
	}
	if event == "Stop" {
		return json.NewEncoder(out).Encode(map[string]any{"decision": "block", "reason": text.String()})
	}
	return json.NewEncoder(out).Encode(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": event, "additionalContext": text.String()}})
}

func renderInboxMessage(project string, m AgentMessage) string {
	reply := fmt.Sprintf("tusker message reply --project %q --reply-to %q --sender %q --key <unique> --body ...", project, m.ID, m.Recipient.Kind+":"+m.Recipient.ID)
	return fmt.Sprintf("Message %s (%s) from %s, reply-to %s\n%s\nReply: %s\nWorker: MCP check_messages is available.\n\n", m.ID, m.Kind, m.Sender, m.ReplyTo, m.Body, reply)
}

func (s *RuntimeStore) messageInboxPending(project string, recipient AgentAddress) ([]AgentMessage, error) {
	if recipient.Kind == "operator" {
		rows, err := s.query(`SELECT id,idempotency_key,project_id,sender,recipient_kind,recipient_id,origin_task_id,origin_wave_id,work_revision,route_generation,recipient_generation,kind,body,reply_to,reply_required,yield_sender,state,transport_state,consumed_at,answered_at,applied_at,expires_at,created_at FROM agent_messages WHERE project_id=? AND recipient_kind='operator' AND recipient_id='operator' AND consumed_at='' AND transport_state<>'delivered' ORDER BY created_at,id`, project)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var pending []AgentMessage
		for rows.Next() {
			m, err := scanAgentMessageValue(rows)
			if err != nil {
				return nil, err
			}
			pending = append(pending, m)
		}
		return pending, rows.Err()
	}
	messages, err := s.ListAgentMessages(project, recipient.Kind, recipient.ID)
	if err != nil {
		return nil, err
	}
	var pending []AgentMessage
	for _, m := range messages {
		if m.Recipient == recipient && m.ConsumedAt == "" && m.TransportState != "delivered" {
			pending = append(pending, m)
		}
	}
	return pending, nil
}

func (s *RuntimeStore) markInboxDelivered(project, id string) (bool, error) {
	result, err := s.exec(`UPDATE agent_messages SET transport_state='delivered' WHERE project_id=? AND id=? AND consumed_at='' AND transport_state<>'delivered'`, project, id)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

func (s *RuntimeStore) messageInboxSessionRecipients(project, sessionID string) ([]AgentAddress, error) {
	rows, err := s.query(`SELECT DISTINCT e.execution_id FROM execution_records e JOIN agent_contacts c ON c.project_id=e.project_id AND c.address_kind='execution' AND c.address_id=e.execution_id WHERE e.project_id=? AND e.provider_session_id=? AND e.source IN ('direct_claude','direct_codex')`, project, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var recipients []AgentAddress
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		recipients = append(recipients, AgentAddress{Kind: "execution", ID: id})
	}
	return recipients, rows.Err()
}
