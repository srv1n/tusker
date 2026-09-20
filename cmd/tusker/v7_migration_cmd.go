package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

type v7PacketStatusProjection struct {
	Schema              string                             `json:"schema"`
	ReadOnly            bool                               `json:"readOnly"`
	TaskID              string                             `json:"taskId"`
	Audience            string                             `json:"audience"`
	Content             string                             `json:"content"`
	Path                string                             `json:"path,omitempty"`
	DependencyContracts dependencyContractReviewProjection `json:"dependencyContracts"`
}

func packetV7Cmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return err
	}
	id := firstNonEmpty(args.String("id"), args.String("_pos0"))
	if id == "" {
		return tuskerError(errorMissingArg, "Missing task id")
	}
	task, ok := idx.Tasks[id]
	if !ok {
		return tuskerError(errorNotFound, "V7 task not found: "+id)
	}
	// The runtime overlay below is packet rendering context only: `project` and
	// the contact fields participate in state_rev hashing, so dispatchability
	// must be validated against the canonical task record. Validating the
	// overlaid copy self-reports a stale revision for every registered project
	// whose id differs from the authored project field.
	dispatchTask := task
	if store, missing, openErr := openRuntimeStoreReadOnly(DefaultStateRoot()); openErr == nil && !missing {
		if projectID, registered, projectErr := registeredProjectIDForVault(store, vaultPath); projectErr == nil && registered {
			task.Data = cloneMap(task.Data)
			task.Data["project"] = projectID
			if contacts, contactErr := store.AgentContacts(projectID, id); contactErr == nil && len(contacts) > 0 {
				peers := map[string]any{}
				for _, contact := range contacts {
					value := contact.Address.Kind + ":" + contact.Address.ID
					switch contact.Role {
					case "architect", "origin":
						task.Data[contact.Role] = value
					case "peer":
						peers[contact.Name] = value
					}
				}
				if len(peers) > 0 {
					task.Data["peer_contacts"] = peers
				}
			}
		}
		_ = store.Close()
	}
	contracts := dependencyContractReviewForTask(idx, dispatchTask)
	audience := fallback(args.String("for"), "agent")
	if audience == "integrator" {
		if stringField(task.Data, "work_kind") != "integrator" {
			return tuskerError(errorInvalidArg, id+": integrator packet requires work_kind: integrator")
		}
		content := appendV7PacketDependencyContracts(integratorPacket(vaultPath, task, idx), contracts)
		return emitV7PacketStatus(args, vaultPath, id, audience, content, contracts)
	}
	if audience == "agent" && !args.Bool("force") {
		if reasons := v7TaskDispatchBlockers(vaultPath, dispatchTask); len(reasons) > 0 {
			return tuskerError(
				errorInvalidTransition,
				id+": task is not dispatchable",
				withHint("fix dispatch blockers or pass --force to inspect the packet anyway: "+strings.Join(reasons, "; ")),
				withContext(map[string]any{"id": id, "dispatch_blockers": reasons}),
			)
		}
	}
	content := appendV7PacketDependencyContracts(v7Packet(vaultPath, task, idx, audience), contracts)
	content = appendV7PacketAgentMessages(content, vaultPath, task)
	return emitV7PacketStatus(args, vaultPath, id, audience, content, contracts)
}

func appendV7PacketAgentMessages(content, vaultPath string, task Note) string {
	store, missing, err := openRuntimeStoreReadOnly(DefaultStateRoot())
	if err != nil || missing {
		return content
	}
	defer store.Close()
	projectID, registered, err := registeredProjectIDForVault(store, vaultPath)
	if err != nil || !registered {
		return content
	}
	messages, err := store.ListAgentMessages(projectID, "task", stringField(task.Data, "id"))
	if err != nil || len(messages) == 0 {
		return content
	}
	var b strings.Builder
	b.WriteString(strings.TrimRight(content, "\n"))
	b.WriteString("\n\n## Outstanding agent messages\n\n")
	for _, message := range messages {
		if message.State == "applied" {
			continue
		}
		fmt.Fprintf(&b, "- `%s` %s from `%s`: %s\n", message.ID, message.Kind, message.Sender, message.Body)
		if message.ReplyRequired && message.Kind == "question" {
			fmt.Fprintf(&b, "  Reply: `tusker message reply --project %s --sender task:%s --recipient %s --recipient-kind task --key <stable-key> --reply-to %s --body <answer> --json`\n", projectID, stringField(task.Data, "id"), message.Sender, message.ID)
		}
	}
	return b.String()
}

func appendV7PacketDependencyContracts(content string, projection dependencyContractReviewProjection) string {
	if len(projection.Dependencies) == 0 {
		return content
	}
	rendered := renderDependencyContractReview(projection.Dependencies)
	rendered = strings.TrimPrefix(rendered, "Pinned dependency contracts\n")
	return strings.TrimRight(content, "\n") + "\n\n## Pinned dependency contracts\n\n" + rendered
}

func emitV7PacketStatus(args Args, vaultPath, id, audience, content string, contracts dependencyContractReviewProjection) error {
	path := ""
	if args.Bool("write") {
		path = filepath.Join(vaultPath, "_generated", "packets", id+"."+audience+".md")
		if err := writeText(path, content); err != nil {
			return err
		}
	}
	if args.Bool("json") {
		emitJSON(v7PacketStatusProjection{
			Schema: "tusker.task-packet/v1", ReadOnly: true, TaskID: id, Audience: audience,
			Content: content, Path: path, DependencyContracts: contracts,
		})
		return nil
	}
	if path != "" {
		if !args.Bool("quiet") {
			fmt.Println(path)
		}
		return nil
	}
	fmt.Print(content)
	return nil
}

func dashboardV7Cmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	switch strings.ToLower(args.String("_pos0")) {
	case "build", "":
		idx, err := loadV7Index(vaultPath)
		if err != nil {
			return err
		}
		if err := buildV7Dashboards(vaultPath, idx); err != nil {
			return err
		}
		if !args.Bool("quiet") {
			fmt.Println("Built V7 dashboards.")
		}
		return nil
	case "open":
		name := fallback(args.String("_pos1"), "human-actions")
		fmt.Println(filepath.Join(vaultPath, "dashboards", name+".md"))
		return nil
	default:
		return tuskerError(errorInvalidArg, "Usage: tusker dashboard build|open <name>")
	}
}

func stateV7Cmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	backend := v7GitStateBackend{VaultPath: vaultPath}
	switch strings.ToLower(args.String("_pos0")) {
	case "sync", "push", "":
		branch := fallback(args.String("branch"), v7StateBranch(vaultPath))
		remote := args.String("remote")
		if remote == "" && (args.Bool("push") || strings.ToLower(args.String("_pos0")) == "push") {
			remote = "origin"
		}
		commit, err := backend.Sync(context.Background(), v7StateSyncOptions{Branch: branch, Remote: remote, Message: args.String("message")})
		if err != nil {
			return err
		}
		if !args.Bool("quiet") {
			target := branch
			if remote != "" {
				target = remote + "/" + branch
			}
			fmt.Printf("Synced V7 runtime state to %s at %s\n", target, commit)
		}
		return nil
	case "import":
		branch := fallback(args.String("branch"), v7StateBranch(vaultPath))
		remote := args.String("remote")
		if remote == "" && args.Bool("fetch") {
			remote = "origin"
		}
		count, err := backend.Import(context.Background(), v7StateSyncOptions{Branch: branch, Remote: remote})
		if err != nil {
			return err
		}
		if !args.Bool("quiet") {
			fmt.Printf("Imported %d V7 lease%s from %s\n", count, plural(count), branch)
		}
		return nil
	case "export":
		dir := args.String("dir")
		if dir == "" {
			dir = filepath.Join(filepath.Dir(vaultPath), ".tusker-runtime", "state")
		}
		count, err := backend.Export(context.Background(), v7StateSyncOptions{Dir: dir})
		if err != nil {
			return err
		}
		if !args.Bool("quiet") {
			fmt.Printf("Exported %d V7 state file%s to %s\n", count, plural(count), dir)
		}
		return nil
	default:
		return tuskerError(errorInvalidArg, "Usage: tusker state sync|import|export")
	}
}
