package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

func executionCmd(args Args, action string) error {
	if action == "" {
		return tuskerError(errorMissingArg, "Usage: tusker execution register|attach|rename|bind|detach|rebind|inbox|list|show|cancel|launch ...")
	}
	// A launch refusal is deliberately the first operation. In particular, a
	// dispatched worker must not even open/migrate a state database merely by
	// attempting a forbidden nested launch.
	if action == "launch" {
		if err := rejectAgentSpawn("execution launch"); err != nil {
			return err
		}
	}
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	projectID, err := resolveV7ProjectID(vaultPath)
	if err != nil {
		return err
	}
	store, err := OpenRuntimeStore(firstNonEmpty(strings.TrimSpace(args.String("state-root")), DefaultStateRoot()))
	if err != nil {
		return err
	}
	defer store.Close()
	actor := firstNonEmpty(strings.TrimSpace(args.String("by")), strings.TrimSpace(args.String("actor")), "operator:"+defaultActorName())

	switch action {
	case "register":
		source := strings.TrimSpace(args.String("source"))
		if source == "" {
			source = "direct"
		}
		if !validDirectExecutionSource(source) {
			return tuskerError(errorInvalidArg, "execution register source must be direct_codex, direct_claude, codex_cloud, or direct")
		}
		record, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: projectID, DisplayName: args.String("name"), Source: source, Provider: args.String("provider"), AgentType: args.String("agent-type"), Creator: actor})
		if err != nil {
			return err
		}
		return emitExecutionResult(args, store, projectID, record.ExecutionID, true)
	case "attach":
		id, err := executionIDArg(args)
		if err != nil {
			return err
		}
		view, created, err := store.AttachExecution(ExecutionAttachmentInput{ProjectID: projectID, ExecutionID: id, Provider: args.String("provider"), ProviderSessionID: firstNonEmpty(args.String("provider-session-id"), args.String("cloud-task-id")), SessionRef: args.String("session-ref"), Source: args.String("source"), Actor: actor})
		if err != nil {
			return err
		}
		return emitExecutionView(args, view, created)
	case "rename":
		id, err := executionIDArg(args)
		if err != nil {
			return err
		}
		view, err := store.RenameExecution(projectID, id, firstNonEmpty(args.String("name"), args.String("display-name")), actor)
		if err != nil {
			return err
		}
		return emitExecutionView(args, view, true)
	case "bind", "rebind":
		id, err := executionIDArg(args)
		if err != nil {
			return err
		}
		taskID := strings.TrimSpace(firstNonEmpty(args.String("task"), args.String("task-id")))
		waveID, err := executionCanonicalWave(vaultPath, taskID)
		if err != nil {
			return err
		}
		if explicit := strings.TrimSpace(args.String("wave")); explicit != "" && explicit != waveID {
			return tuskerError(errorInvalidArg, "execution binding task and wave disagree")
		}
		view, err := store.BindExecution(ExecutionBindingInput{ProjectID: projectID, ExecutionID: id, TaskID: taskID, WaveID: waveID, Actor: actor}, action)
		if err != nil {
			return err
		}
		return emitExecutionView(args, view, true)
	case "detach":
		id, err := executionIDArg(args)
		if err != nil {
			return err
		}
		view, err := store.BindExecution(ExecutionBindingInput{ProjectID: projectID, ExecutionID: id, Actor: actor}, "detach")
		if err != nil {
			return err
		}
		return emitExecutionView(args, view, true)
	case "inbox":
		views, err := store.ListUnboundDirectExecutions(projectID)
		if err != nil {
			return err
		}
		emitJSON(map[string]any{"ok": true, "project_id": projectID, "executions": views})
		return nil
	case "list":
		page, err := store.ExecutionGraph(projectID, executionGraphFilterArgs(args))
		if err != nil {
			return err
		}
		emitJSON(map[string]any{"ok": true, "graph": page})
		return nil
	case "show":
		id, err := executionIDArg(args)
		if err != nil {
			return err
		}
		return emitExecutionResult(args, store, projectID, id, false)
	case "cancel":
		id, err := executionIDArg(args)
		if err != nil {
			return err
		}
		view, err := store.ExecutionView(id)
		if err != nil {
			return err
		}
		if view == nil || view.ProjectID != projectID {
			return tuskerError(errorNotFound, "execution not found")
		}
		control, err := store.RequestExecutionCancellation(id, firstNonEmpty(args.String("request-key"), args.String("idempotency-key"), actor))
		if err != nil {
			return err
		}
		emitJSON(map[string]any{"ok": control.Available, "control": control, "execution_id": id})
		return nil
	case "launch":
		id, err := executionIDArg(args)
		if err != nil {
			return err
		}
		view, err := store.ExecutionView(id)
		if err != nil {
			return err
		}
		if view == nil || view.ProjectID != projectID {
			return tuskerError(errorNotFound, "execution not found")
		}
		if view.Source == "codex_cloud" {
			if strings.TrimSpace(args.String("pid")) != "" {
				return tuskerError(errorInvalidArg, "codex_cloud execution launch cannot report a local pid")
			}
			emitJSON(map[string]any{"ok": true, "execution": view, "source": view.Source, "process": map[string]any{"available": false}, "authority": "observation_only"})
			return nil
		}
		pid := os.Getpid()
		if raw := strings.TrimSpace(args.String("pid")); raw != "" {
			parsed, parseErr := strconv.Atoi(raw)
			if parseErr != nil || parsed <= 0 {
				return tuskerError(errorInvalidArg, "execution launch pid must be positive")
			}
			pid = parsed
		}
		emitJSON(map[string]any{"ok": true, "execution": view, "source": view.Source, "process": map[string]any{"pid": pid, "available": true}, "authority": "observation_only"})
		return nil
	default:
		return tuskerError(errorInvalidArg, "unknown execution action: "+action)
	}
}

func executionGraphFilterArgs(args Args) ExecutionGraphFilter {
	limit := 0
	if raw := strings.TrimSpace(args.String("limit")); raw != "" {
		limit, _ = strconv.Atoi(raw)
	}
	return ExecutionGraphFilter{ExecutionID: firstNonEmpty(args.String("execution"), args.String("execution-id")), RootID: firstNonEmpty(args.String("root"), args.String("root-id")), ParentID: firstNonEmpty(args.String("parent"), args.String("parent-id")), TaskID: firstNonEmpty(args.String("task"), args.String("task-id")), WaveID: firstNonEmpty(args.String("wave"), args.String("wave-id")), Source: args.String("source"), Provider: args.String("provider"), ProviderID: firstNonEmpty(args.String("provider-id"), args.String("provider-session-id")), AgentType: args.String("agent-type"), Binding: args.String("binding"), Lifecycle: args.String("lifecycle"), Name: firstNonEmpty(args.String("name"), args.String("search")), Attention: args.String("attention"), Cursor: args.String("cursor"), Limit: limit}
}

func validDirectExecutionSource(source string) bool {
	switch strings.TrimSpace(source) {
	case "direct", "direct_codex", "direct_claude", "codex_cloud":
		return true
	default:
		return false
	}
}

func executionIDArg(args Args) (string, error) {
	id := strings.TrimSpace(firstNonEmpty(args.String("id"), args.String("execution"), args.String("_pos0")))
	if id == "" {
		return "", tuskerError(errorMissingArg, "execution command requires --id")
	}
	return id, nil
}

func executionCanonicalWave(vaultPath, taskID string) (string, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return "", tuskerError(errorMissingArg, "execution bind requires --task")
	}
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return "", err
	}
	task, ok := idx.Tasks[taskID]
	if !ok {
		return "", tuskerError(errorNotFound, "task not found: "+taskID)
	}
	canonical := ""
	for waveID, wave := range idx.Waves {
		for _, member := range normalizeList(wave.Data["members"]) {
			if member == taskID {
				if canonical != "" && canonical != waveID {
					return "", tuskerError(errorInvalidTransition, "execution binding task belongs to multiple waves")
				}
				canonical = waveID
			}
		}
	}
	if canonical == "" {
		return "", tuskerError(errorInvalidTransition, "execution binding task has no canonical wave")
	}
	if backPointer := strings.TrimSpace(stringField(task.Data, "wave")); backPointer != "" && backPointer != canonical {
		return "", tuskerError(errorInvalidTransition, "execution binding task/wave back-pointer disagrees with canonical membership")
	}
	return canonical, nil
}

func emitExecutionResult(args Args, store *RuntimeStore, projectID, id string, created bool) error {
	view, err := store.ExecutionView(id)
	if err != nil {
		return err
	}
	if view == nil || view.ProjectID != projectID {
		return tuskerError(errorNotFound, "execution not found")
	}
	return emitExecutionView(args, view, created)
}

func emitExecutionView(args Args, view *ExecutionView, created bool) error {
	if view == nil {
		return tuskerError(errorNotFound, "execution not found")
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "created": created, "execution": view})
		return nil
	}
	name := view.EffectiveDisplayName
	if name == "" {
		name = view.ExecutionID
	}
	fmt.Printf("%s %s\n", view.ExecutionID, name)
	return nil
}

func printExecutionHelp() {
	fmt.Println(`Usage: tusker execution <action> [flags]

Actions:
  register  Allocate an immutable direct-execution ID before provider launch
  attach    Idempotently correlate a provider session or cloud task
  rename    Add an audited display-name change
  bind      Bind an execution to a task's canonical wave
  detach    Remove the current binding with a new generation boundary
  rebind    Move binding with a new generation boundary
  inbox     List unbound direct executions
	  list      Search the versioned relationship-complete graph
  show      Inspect the effective execution projection
	  cancel    Request only a capability-proved cancellation; records settlement evidence
  launch    Report local launch process facts; refuses nested agent sessions

All registration and observation operations are authority-neutral: they never
claim a task, arm a wave, start a daemon, or create a delivery lease.`)
}
