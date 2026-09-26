package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

func agentMessageCmd(command string, args Args) error {
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	project := strings.TrimSpace(args.String("project"))
	if project == "" {
		return tuskerError(errorInvalidArg, "message command requires --project")
	}
	switch command {
	case "message list":
		messages, err := store.ListAgentMessages(project, args.String("recipient-kind"), args.String("recipient"))
		if err != nil {
			return err
		}
		emitJSON(map[string]any{"ok": true, "messages": messages})
		return nil
	case "message show":
		message, err := store.AgentMessage(project, firstNonEmpty(args.String("id"), args.String("_pos0")))
		if err != nil {
			return err
		}
		emitJSON(map[string]any{"ok": true, "message": message})
		return nil
	case "message consume", "message apply":
		state := "consumed"
		if command == "message apply" {
			state = "applied"
		}
		if err := store.MarkAgentMessage(project, firstNonEmpty(args.String("id"), args.String("_pos0")), state); err != nil {
			return err
		}
		emitJSON(map[string]any{"ok": true, "state": state})
		return nil
	}
	kind := strings.TrimPrefix(command, "message ")
	if kind == "send" {
		kind = firstNonEmpty(args.String("kind"), "notice")
	}
	if kind == "ask" {
		kind = "question"
	}
	recipientKind := firstNonEmpty(args.String("recipient-kind"), "task")
	recipient := strings.TrimSpace(args.String("recipient"))
	sender := strings.TrimSpace(args.String("sender"))
	workRevision, _ := strconv.Atoi(strings.TrimSpace(os.Getenv("TUSKER_WORK_REVISION")))
	routeGeneration, _ := strconv.Atoi(strings.TrimSpace(os.Getenv("TUSKER_LEASE_GENERATION")))
	if attemptID := strings.TrimSpace(os.Getenv("TUSKER_ATTEMPT_ID")); attemptID != "" {
		itemID := strings.TrimSpace(os.Getenv("TUSKER_ITEM_ID"))
		runtimeProject := strings.TrimSpace(os.Getenv("TUSKER_PROJECT_ID"))
		if runtimeProject != "" && runtimeProject != project {
			return tuskerError(errorInvalidTransition, "message project does not match the claimed attempt")
		}
		if sender != "task:"+itemID {
			return tuskerError(errorInvalidTransition, "message sender does not match the claimed task owner")
		}
	}
	if command == "message reply" {
		kind = "answer"
		recipient = firstNonEmpty(recipient, args.String("to"))
		if recipient == "" {
			parent, err := store.AgentMessage(project, args.String("reply-to"))
			if err != nil {
				return err
			}
			recipient = parent.Sender
		}
	}
	body := args.String("body")
	if bodyFile := strings.TrimSpace(args.String("body-file")); bodyFile != "" {
		if body != "" {
			return tuskerError(errorInvalidArg, "message accepts either --body or --body-file")
		}
		var data []byte
		var err error
		if bodyFile == "-" {
			data, err = io.ReadAll(io.LimitReader(os.Stdin, agentMessageBodyLimit+1))
		} else {
			data, err = os.ReadFile(bodyFile)
		}
		if err != nil {
			return err
		}
		body = string(data)
	}
	recipientGeneration := 0
	if recipient == "" && args.String("contact") != "" {
		taskID := args.String("task")
		role, name := args.String("contact"), ""
		if strings.HasPrefix(role, "peer:") {
			role, name = "peer", strings.TrimPrefix(role, "peer:")
		}
		if contact, _, contactErr := store.ResolveEffectiveAgentContact(project, taskID, role, name); contactErr == nil {
			recipientKind, recipient = contact.Address.Kind, contact.Address.ID
			recipientGeneration = contact.Generation
		}
	}
	if recipient == "" && args.String("contact") != "" {
		vault, resolveErr := resolveVaultPath(args, false)
		if resolveErr != nil {
			return resolveErr
		}
		taskID := args.String("task")
		idx, resolveErr := loadV7Index(vault)
		if resolveErr != nil {
			return resolveErr
		}
		note, ok := idx.Tasks[taskID]
		if !ok {
			return tuskerError(errorNotFound, "task contact source not found: "+taskID)
		}
		role, name := args.String("contact"), ""
		if strings.HasPrefix(role, "peer:") {
			role, name = "peer", strings.TrimPrefix(role, "peer:")
		}
		for _, contact := range authoredAgentContacts(note) {
			if contact.Role == role && contact.Name == name {
				recipientKind, recipient = contact.Address.Kind, contact.Address.ID
				break
			}
		}
		if recipient == "" {
			return tuskerError(errorNotFound, "task contact not found")
		}
	}
	if parsed, parseErr := normalizeAgentAddress(recipient, recipientKind); parseErr == nil {
		recipientKind, recipient = parsed.Kind, parsed.ID
	} else {
		return tuskerError(errorInvalidArg, "message recipient is invalid: "+parseErr.Error())
	}
	m := AgentMessage{IdempotencyKey: args.String("key"), ProjectID: project, Sender: sender, Recipient: AgentAddress{Kind: recipientKind, ID: recipient}, OriginTaskID: firstNonEmpty(args.String("task"), strings.TrimSpace(os.Getenv("TUSKER_ITEM_ID"))), OriginWaveID: args.String("wave"), WorkRevision: workRevision, RouteGeneration: routeGeneration, RecipientGeneration: recipientGeneration, Kind: kind, Body: body, ReplyTo: args.String("reply-to"), ReplyRequired: command == "message ask" || args.Bool("reply-required"), YieldSender: args.Bool("yield")}
	stored, duplicate, err := store.PutAgentMessage(m)
	if err != nil {
		return tuskerError(errorInvalidArg, fmt.Sprintf("message rejected: %v", err))
	}
	emitJSON(map[string]any{"ok": true, "duplicate": duplicate, "message": stored})
	return nil
}

func printMessageHelp() {
	fmt.Println(`Usage:
  tusker message ask     --project <id> --sender task:<id> --recipient <address> --key <stable-key> (--body <text> | --body-file <path|->) [--yield] [--json]
  tusker message send    --project <id> --sender <address> --recipient <address> --key <stable-key> (--body <text> | --body-file <path|->) [--kind notice|instruction] [--reply-required]
  tusker message reply   --project <id> --sender <address> --reply-to <message-id> --key <stable-key> (--body <text> | --body-file <path|->) [--to <address>]
  tusker message list    --project <id> [--recipient-kind task|execution|operator] [--recipient <id>]
  tusker message show    --project <id> <message-id>
  tusker message consume --project <id> <message-id>
  tusker message apply   --project <id> <message-id>
  tusker message inbox   --project <id> (--run <attempt-id> | --for task:<id>|execution:<id>|operator) [--format hook|text|json]
  tusker message hook print --harness claude|codex [--project <id>] [--json]

Addresses: operator, task:<TASK-ID>, execution:<EXECUTION-ID>. Instead of
--recipient, --contact architect|peer:<name> --task <TASK-ID> resolves the
task's current contact.

A dispatched worker (TUSKER_ATTEMPT_ID set) must send as task:<its task>. With
--yield, the worker should end its turn after asking; the answer returns by
soft delivery, hard Say (interrupt + resume the same session), or the next
continuation. Operator questions appear in Serve as needs-you items.`)
}
