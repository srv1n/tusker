package main

import (
	"fmt"
	"os"
	"strings"
)

func approvalsListCmd(args Args) error {
	project := strings.TrimSpace(args.String("project"))
	if project == "" {
		vault, err := resolveVaultPath(args, false)
		if err != nil {
			return err
		}
		project, err = resolveV7ProjectID(vault)
		if err != nil {
			return err
		}
	}
	store, err := OpenRuntimeStoreReadOnly(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	rows, err := store.ListAgentAccessApprovals(project)
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(rows)
		return nil
	}
	for _, row := range rows {
		fmt.Printf("%s  %s  %s  task=%s  tool=%s  expires=%s\n", row.RequestID, row.State, row.Reason, row.TaskID, row.Tool, row.ExpiresAt)
	}
	return nil
}

func approvalsRespondCmd(args Args) error {
	if err := requireOwnerSession("approvals respond"); err != nil {
		return err
	}
	id := strings.TrimSpace(firstNonEmpty(args.String("id"), args.String("_pos0")))
	if id == "" || args.Bool("allow-once") == args.Bool("block") {
		return tuskerError(errorInvalidArg, "usage: tusker approvals respond <id> --allow-once|--block [--by human:<actor>]")
	}
	actor := strings.TrimSpace(args.String("by"))
	if actor == "" {
		actor = "human:" + os.Getenv("USER")
	}
	if !strings.HasPrefix(actor, "human:") || strings.TrimSpace(strings.TrimPrefix(actor, "human:")) == "" {
		return tuskerError(errorInvalidArg, "approval actor must be human:<name>")
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	approval, err := store.AgentAccessApproval(id)
	if err != nil {
		return err
	}
	decision := AgentAccessApprovalDeny
	if args.Bool("allow-once") {
		decision = AgentAccessApprovalAllowOnce
	}
	settled, err := store.SettleAgentAccessApproval(AgentAccessApprovalResponse{RequestID: id, ExpectedRevision: approval.StateRevision, Decision: decision, Actor: actor})
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(settled)
	} else {
		fmt.Printf("%s  %s  revision=%d\n", settled.RequestID, settled.State, settled.StateRevision)
	}
	return nil
}
