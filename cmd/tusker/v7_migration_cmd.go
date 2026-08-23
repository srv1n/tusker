package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

type v7PacketStatusProjection struct {
	Schema                 string                             `json:"schema"`
	ReadOnly               bool                               `json:"readOnly"`
	TaskID                 string                             `json:"taskId"`
	Audience               string                             `json:"audience"`
	Content                string                             `json:"content"`
	Path                   string                             `json:"path,omitempty"`
	CrossScopeDependencies deliveryCrossScopeReviewProjection `json:"crossScopeDependencies"`
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
	crossScope := deliveryCrossScopeReviewForTask(idx, task)
	audience := fallback(args.String("for"), "agent")
	if audience == "integrator" {
		if stringField(task.Data, "work_kind") != "integrator" {
			return tuskerError(errorInvalidArg, id+": integrator packet requires work_kind: integrator")
		}
		content := appendV7PacketCrossScopeProjection(integratorPacket(vaultPath, task, idx), crossScope)
		return emitV7PacketStatus(args, vaultPath, id, audience, content, crossScope)
	}
	if audience == "agent" && !args.Bool("force") {
		if reasons := v7TaskDispatchBlockers(vaultPath, task); len(reasons) > 0 {
			return tuskerError(
				errorInvalidTransition,
				id+": task is not dispatchable",
				withHint("fix dispatch blockers or pass --force to inspect the packet anyway: "+strings.Join(reasons, "; ")),
				withContext(map[string]any{"id": id, "dispatch_blockers": reasons}),
			)
		}
	}
	content := appendV7PacketCrossScopeProjection(v7Packet(vaultPath, task, idx, audience), crossScope)
	return emitV7PacketStatus(args, vaultPath, id, audience, content, crossScope)
}

func appendV7PacketCrossScopeProjection(content string, projection deliveryCrossScopeReviewProjection) string {
	if len(projection.Dependencies) == 0 {
		return content
	}
	rendered := renderDeliveryCrossScopeReview(projection.Dependencies)
	rendered = strings.TrimPrefix(rendered, "Cross-scope hard dependencies\n")
	return strings.TrimRight(content, "\n") + "\n\n## Cross-scope hard dependencies\n\n" + rendered
}

func emitV7PacketStatus(args Args, vaultPath, id, audience, content string, crossScope deliveryCrossScopeReviewProjection) error {
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
			Content: content, Path: path, CrossScopeDependencies: crossScope,
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
