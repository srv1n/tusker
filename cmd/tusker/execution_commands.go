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
	selector := strings.TrimSpace(args.String("project"))
	vaultPath, localID := "", ""
	if selector == "" {
		var err error
		if vaultPath, err = resolveVaultPath(args, false); err != nil {
			return err
		}
		if localID, err = resolveV7ProjectID(vaultPath); err != nil {
			return err
		}
	}
	store, err := OpenRuntimeStore(firstNonEmpty(strings.TrimSpace(args.String("state-root")), DefaultStateRoot()))
	if err != nil {
		return err
	}
	defer store.Close()
	projectID, vaultPath, err := executionRuntimeProject(store, selector, vaultPath, localID)
	if err != nil {
		return err
	}
	actor := firstNonEmpty(strings.TrimSpace(args.String("by")), strings.TrimSpace(args.String("actor")), "operator:"+defaultActorName())

	switch action {
	case "register":
		if strings.TrimSpace(args.String("contact-role")) != "" {
			return executionRegisterContact(args, store, projectID, vaultPath, actor)
		}
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

func executionRegisterContact(args Args, store *RuntimeStore, projectID, vaultPath, actor string) error {
	taskID := strings.TrimSpace(firstNonEmpty(args.String("task"), args.String("task-id")))
	waveID := strings.TrimSpace(firstNonEmpty(args.String("wave"), args.String("wave-id")))
	if (taskID == "") == (waveID == "") {
		return tuskerError(errorInvalidArg, "execution register contact requires exactly one of --task or --wave")
	}
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return err
	}
	subjectID, subjectKind := taskID, "task"
	if taskID != "" {
		if _, ok := idx.Tasks[taskID]; !ok {
			return tuskerError(errorNotFound, "task not found: "+taskID)
		}
	} else {
		if _, ok := idx.Waves[waveID]; !ok {
			return tuskerError(errorNotFound, "wave not found: "+waveID)
		}
		subjectID = waveID
		subjectKind = "wave"
	}
	role := strings.TrimSpace(args.String("contact-role"))
	if role != "architect" && role != "origin" && role != "peer" {
		return tuskerError(errorInvalidArg, "contact-role must be architect, origin, or peer")
	}
	name := strings.TrimSpace(args.String("contact-name"))
	if role == "peer" && name == "" {
		return tuskerError(errorMissingField, "peer contact requires --contact-name")
	}
	if role != "peer" && name != "" {
		return tuskerError(errorInvalidArg, "--contact-name is only valid for peer contacts")
	}
	by, byPresent := args["by"]
	if !byPresent || strings.TrimSpace(fmt.Sprint(by)) == "" {
		return tuskerError(errorMissingField, "external contact registration requires an explicit --by human:<name> or operator:<name>")
	}
	if !strings.HasPrefix(actor, "human:") && !strings.HasPrefix(actor, "operator:") {
		return tuskerError(errorInvalidArg, "external contact registration requires --by human:<name> or operator:<name>")
	}
	harness := strings.ToLower(strings.TrimSpace(args.String("harness")))
	source := strings.TrimSpace(args.String("source"))
	if expected, ok := externalContactSource[harness]; !ok || source != expected {
		return tuskerError(errorInvalidArg, "contact harness/source pairing must be codex/direct_codex, claude-code/direct_claude, or devin/direct_devin")
	}
	rawGeneration, ok := args["if-generation"]
	if !ok {
		return tuskerError(errorMissingField, "external contact registration requires --if-generation; use 0 to create")
	}
	generation, err := strconv.Atoi(strings.TrimSpace(fmt.Sprint(rawGeneration)))
	if err != nil || generation < 0 {
		return tuskerError(errorInvalidArg, "--if-generation must be a non-negative integer")
	}
	contact, record, err := store.RegisterExternalAgentContact(ExternalContactRegistrationInput{
		ProjectID: projectID, SubjectID: subjectID, SubjectKind: subjectKind,
		Role: role, Name: name,
		Harness: args.String("harness"), Provider: args.String("provider"),
		ConversationID: firstNonEmpty(args.String("conversation-id"), args.String("conversation_id")),
		ConnectionID:   firstNonEmpty(args.String("connection-id"), args.String("connection_id")),
		Source:         args.String("source"), Actor: actor, ExpectedGeneration: generation,
	})
	if err != nil {
		return err
	}
	view, err := store.ExecutionView(record.ExecutionID)
	if err != nil {
		return err
	}
	binding, err := store.ResolveAgentContactBinding(projectID, subjectID, role, name)
	if err != nil {
		binding = AgentContactBinding{Contact: contact, State: AgentContactBindingUnbound, Reason: "contact binding could not be resolved"}
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "created": contact.Generation == 1, "contact": contact, "execution": view, "binding": binding})
		return nil
	}
	fmt.Printf("%s %s %s\n", contact.Address.ID, contact.Role, view.EffectiveDisplayName)
	return nil
}

var externalContactSource = map[string]string{
	"codex":       "direct_codex",
	"claude-code": "direct_claude",
	"devin":       "direct_devin",
}

func validDirectExecutionSource(source string) bool {
	switch strings.TrimSpace(source) {
	case "direct", "direct_codex", "direct_claude", "codex_cloud", "direct_devin":
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
  register  Allocate an immutable direct-execution ID before provider launch;
            with --contact-role, register a pre-existing external conversation
            as an architect/origin/peer contact on a durable task or wave
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

The project is the runtime-registered project whose vault matches --vault or
the current directory; --project <id|key|name> selects a registered project
explicitly.

All registration and observation operations are authority-neutral: they never
claim a task, arm a wave, start a daemon, or create a delivery lease.`)
}

// executionRuntimeProject maps the vault to the runtime registry's project ID
// (a ULID), which is what contacts, mailboxes and Serve key on. --project
// selects a registered project by ID, key or name. An unregistered vault
// keeps its config project_id so local-only executions still work.
func executionRuntimeProject(store *RuntimeStore, selector, vaultPath, localID string) (string, string, error) {
	if selector == "" {
		return digestRuntimeProjectID(store, vaultPath, localID), vaultPath, nil
	}
	project, err := resolveLoadedRegisteredProject(store, Args{"id": selector}, registeredProjectLoadOptions{MetadataOnly: true, LoadDisabled: true})
	if err != nil {
		return "", "", err
	}
	return project.Project.ProjectID, project.Project.VaultRoot, nil
}
