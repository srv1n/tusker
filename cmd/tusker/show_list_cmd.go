package main

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// `tusker show <ID>` and `tusker list [kind]` are the single read surface.
// They only route: every view below is an existing handler or builder.

var (
	showEscalationIDPattern = regexp.MustCompile(`^ESC-\d+$`)
	showBatchIDPattern      = regexp.MustCompile(`^B-\d{8}-\d{6}$`)
)

// showBoolFlags are flags that take no value. parseArgs gives a bare flag
// "true", so `tusker show --why APP-T-0001` would otherwise swallow the ID.
var showBoolFlags = []string{"why", "progress", "lease", "proof", "run", "logs", "events", "traces", "closeout", "styled", "follow", "check", "backlinks", "capsule", "acceptance", "evidence", "verification", "full", "json", "plain", "routing", "write", "force", "verbose", "quiet", "current"}

type showView struct {
	flag string
	run  func(Args) (int, error)
}

// showTaskViews are the task views, in precedence order; the first flag
// present wins.
var showTaskViews = []showView{
	{"styled", run0(printCmd)},
	{"packet", func(a Args) (int, error) {
		if v := a.String("packet"); v != "true" {
			a["for"] = v
		}
		return 0, packetV7Cmd(a)
	}},
	{"why", showWhy},
	{"progress", func(a Args) (int, error) { return 0, workRealLifecycleCmd(a, "progress") }},
	{"lease", func(a Args) (int, error) { return 0, workSessionCmd(a, "status") }},
	{"proof", run0(proofV7StatusCmd)},
	{"route", func(a Args) (int, error) {
		a["lane"] = runLaneExecute
		if v := a.String("route"); v != "true" {
			a["lane"] = v
		}
		return 0, runnerRouteCmd(a)
	}},
	{"run", run0(runsInspectCmd)},
	{"logs", run0(runsLogsCmd)},
	{"events", run0(runsEventsCmd)},
	{"traces", run0(traceListCmd)},
	{"trace", func(a Args) (int, error) {
		if v := a.String("trace"); v != "true" {
			a["id"] = v
		} else {
			return 2, tuskerError(errorMissingArg, "--trace requires a trace id", withHint("list them with `tusker show <TASK-ID> --traces`"))
		}
		return 0, traceShowCmd(a)
	}},
	{"closeout", run0(closeoutV7StatusCmd)},
}

func showCmd(args Args) error {
	_, err := showRun(args)
	return err
}

// showRun is the `tusker show` entry point. It returns an exit code so
// `--why` keeps doctor's 0/1/2 contract.
func showRun(args Args) (int, error) {
	id := showID(args)
	if id == "" {
		return 2, tuskerError(errorMissingArg, "show requires an id", withHint("use `tusker show <ID>`; see `tusker show --help`"))
	}
	upper := strings.ToUpper(id)
	switch {
	case v7WaveIDPattern.MatchString(upper):
		return showWave(args, upper)
	case strings.HasPrefix(id, "exec_"):
		return 0, executionCmd(args, "show")
	case strings.HasPrefix(id, "msg-") || strings.HasPrefix(id, "msg_"):
		if err := defaultRuntimeProject(args); err != nil {
			return 0, err
		}
		return 0, agentMessageCmd("message show", args)
	case showEscalationIDPattern.MatchString(upper):
		return 0, showEscalation(args, upper)
	case showBatchIDPattern.MatchString(id):
		return 0, coordinatorBatchCmd(args, "status")
	}
	for _, view := range showTaskViews {
		if _, ok := args[view.flag]; ok {
			return view.run(copyArgsForInternalMutation(args))
		}
	}
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return 0, err
	}
	_, noteErr := resolveNote(vaultPath, id)
	if noteErr == nil {
		return 0, showNoteCmd(args)
	}
	if _, batchErr := coordinatorLoadBatch(vaultPath, id); batchErr == nil {
		return 0, coordinatorBatchCmd(args, "status")
	}
	docErr := docsReadCmd
	if args.Bool("backlinks") {
		docErr = docsBacklinksCmd
	}
	if err := docErr(args); err != nil {
		if errorToIssue(err).Code == "DOC_NOT_FOUND" {
			return 0, noteErr
		}
		return 0, err
	}
	return 0, nil
}

// showID reads the ID and normalises it into id/_pos0/_pos for the handlers.
func showID(args Args) string {
	id := strings.TrimSpace(firstNonEmpty(args.String("id"), args.String("_pos0")))
	if id == "" {
		for _, flag := range showBoolFlags {
			if v, ok := args[flag]; ok && v != "true" && v != "false" && v != "" {
				args[flag] = "true"
				id = strings.TrimSpace(v)
				break
			}
		}
	}
	if id == "" {
		return ""
	}
	args["id"], args["_pos0"] = id, id
	if args.String("_pos") == "" {
		args["_pos"] = id
	}
	return id
}

// showWhy is doctor plus readiness and the automation plan. --json emits only
// the doctor document so the output stays one JSON value.
func showWhy(args Args) (int, error) {
	code, err := executionDoctorCmd(copyArgsForInternalMutation(args))
	if err != nil || args.Bool("json") {
		return code, err
	}
	parts := []struct {
		title string
		run   func(Args) error
	}{
		{"Readiness", func(a Args) error { return workRealLifecycleCmd(a, "readiness") }},
		{"Automation plan", automationPlanCmd},
	}
	for _, part := range parts {
		fmt.Printf("\n== %s\n", part.title)
		var emitted resultEmittedError
		if e := part.run(copyArgsForInternalMutation(args)); e != nil && !errors.As(e, &emitted) {
			fmt.Printf("(unavailable: %v)\n", e)
		}
	}
	return code, nil
}

// showWave is wave show + wave review + wave brief; --why is doctor on the
// wave and --check fails unless the wave can start.
func showWave(args Args, id string) (int, error) {
	args["id"], args["_pos0"] = id, id
	if args.Bool("why") {
		return executionDoctorCmd(args)
	}
	if args.Bool("json") {
		if err := showWaveJSON(args, id); err != nil {
			return 0, err
		}
	} else {
		if err := waveV7ShowCmd(copyArgsForInternalMutation(args)); err != nil {
			return 0, err
		}
		review := copyArgsForInternalMutation(args)
		delete(review, "check")
		fmt.Println("\n== Start review")
		if err := waveReviewCmd(review); err != nil {
			fmt.Printf("(unavailable: %v)\n", err)
		}
		fmt.Println("\n== Brief")
		if err := waveV7BriefCmd(copyArgsForInternalMutation(args)); err != nil {
			fmt.Printf("(unavailable: %v)\n", err)
		}
	}
	if args.Bool("check") {
		check := copyArgsForInternalMutation(args)
		check["quiet"] = "true"
		delete(check, "json")
		if err := waveReviewCmd(check); err != nil {
			if args.Bool("json") {
				return 0, afterResultEmitted(err)
			}
			return 0, err
		}
	}
	return 0, nil
}

func showWaveJSON(args Args, id string) error {
	vault, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	idx, err := loadV7Index(vault)
	if err != nil {
		return err
	}
	wave, ok := idx.Waves[id]
	if !ok {
		return tuskerError(errorNotFound, "V7 wave not found: "+id)
	}
	store, runtimeErr := directWaveReviewRuntimeStore()
	if store != nil {
		defer store.Close()
	}
	projectID, _ := resolveV7ProjectID(vault)
	review, err := buildDirectWaveReview(vault, store, projectID, id, runtimeErr)
	if err != nil {
		return err
	}
	briefIdx, err := armedWaveBriefProjectedIndex(vault, idx, wave)
	if err != nil {
		return err
	}
	brief := buildWaveBriefWithRuns(briefIdx, wave, v7WaveRuntimeRuns(vault, stringField(wave.Data, "project")))
	emitJSON(map[string]any{"ok": true, "wave": v7WavePayload(vault, idx, wave), "review": review, "brief": brief})
	return nil
}

func showEscalation(args Args, id string) error {
	vault, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	idx, err := loadV7Index(vault)
	if err != nil {
		return err
	}
	note, ok := idx.Escalations[id]
	if !ok {
		return tuskerError(errorNotFound, "escalation not found: "+id)
	}
	payload := escalationPayload(note)
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "escalation": payload})
		return nil
	}
	fmt.Printf("%s  %s  %s  task=%v  reason=%s\n%s\n", payload["id"], payload["severity"], payload["status"], payload["task"], payload["reason"], payload["description"])
	return nil
}

// defaultRuntimeProject fills --project for runtime-store commands from the
// current vault: the registered project id when there is one.
func defaultRuntimeProject(args Args) error {
	if strings.TrimSpace(args.String("project")) != "" {
		return nil
	}
	vault, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	projectID, err := resolveV7ProjectID(vault)
	if err != nil {
		return err
	}
	if store, missing, openErr := openRuntimeStoreReadOnly(DefaultStateRoot()); openErr == nil && !missing {
		defer store.Close()
		if registered, ok, _ := registeredProjectIDForVault(store, vault); ok && registered != "" {
			projectID = registered
		}
	}
	args["project"] = projectID
	return nil
}

var listKinds = map[string]func(Args) error{
	"tasks": listTasksOrEpics("task"),
	"epics": listTasksOrEpics("epic"),
	"waves": waveListCmd,
	"docs":  listDocsCmd,
	"runs":  runsListCmd,
	"executions": func(a Args) error {
		if a.Bool("unbound") {
			return executionCmd(a, "inbox")
		}
		return executionCmd(a, "list")
	},
	"messages":    listMessagesCmd,
	"approvals":   approvalsListCmd,
	"forks":       func(a Args) error { return coordinatorForkCmd(a, "list") },
	"flaky":       func(a Args) error { return coordinatorFlakyCmd(a, "list") },
	"projects":    projectsListCmd,
	"queue":       automationQueueCmd,
	"escalations": listEscalationsCmd,
}

// listCmd is the `tusker list [kind]` entry point. A first positional that is
// not a kind keeps its old meaning (an epic acronym).
func listCmd(args Args) error {
	kind := strings.TrimSpace(args.String("_pos0"))
	handler, ok := listKinds[kind]
	if !ok {
		handler, ok = listKinds[kind+"s"]
	}
	if ok {
		shiftPositional(args)
	} else {
		kind, handler = "", listTasksOrEpics("")
	}
	if args.Bool("next") {
		if args.Bool("claim") {
			return tuskerError(errorInvalidArg, "list --next does not claim", withHint("claim with `tusker start <TASK-ID> --by <agent>`"))
		}
		return nextCmd(args)
	}
	if args.String("grep") != "" && kind != "" && kind != "tasks" && kind != "task" && kind != "epics" && kind != "epic" && kind != "docs" && kind != "doc" {
		return tuskerError(errorInvalidArg, "--grep applies to tasks, epics, docs, or all notes")
	}
	return handler(args)
}

func listTasksOrEpics(noteType string) func(Args) error {
	return func(args Args) error {
		if grep := strings.TrimSpace(args.String("grep")); grep != "" {
			args["query"] = grep
			if noteType != "" {
				args["type"] = noteType
			}
			return searchCmd(args)
		}
		if noteType == "" {
			return listNotesCmd(args)
		}
		args["type"] = noteType
		if noteType == "task" && args.String("status") == "" && !args.Bool("closed") && !args.Bool("all") {
			args["open"] = "true"
		}
		return listNotesCmd(args)
	}
}

func listDocsCmd(args Args) error {
	setPhrase := func(value string) {
		args["_pos"], args["_pos0"] = value, value
	}
	switch {
	case strings.TrimSpace(args.String("grep")) != "":
		setPhrase(args.String("grep"))
		return docsFindCmd(args)
	case args.String("dir") != "":
		if dir := args.String("dir"); dir != "true" {
			setPhrase(dir)
		}
		return docsBrowseCmd(args)
	}
	return docsStatusCmd(args)
}

func listMessagesCmd(args Args) error {
	if err := defaultRuntimeProject(args); err != nil {
		return err
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	messages, err := store.ListAgentMessages(args.String("project"), args.String("recipient-kind"), firstNonEmpty(args.String("for"), args.String("recipient")))
	if err != nil {
		return err
	}
	if args.Bool("pending") {
		pending := []AgentMessage{}
		for _, message := range messages {
			if message.State == "queued" {
				pending = append(pending, message)
			}
		}
		messages = pending
	}
	emitJSON(map[string]any{"ok": true, "messages": messages})
	return nil
}

func listEscalationsCmd(args Args) error {
	vault, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	idx, err := loadV7Index(vault)
	if err != nil {
		return err
	}
	items := []map[string]any{}
	for _, note := range sortedEscalationNotes(idx) {
		if args.Bool("open") && stringField(note.Data, "status") != escalationStatusOpen {
			continue
		}
		items = append(items, escalationPayload(note))
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "escalations": items})
		return nil
	}
	if len(items) == 0 {
		fmt.Println("(no escalations)")
	}
	for _, item := range items {
		fmt.Printf("%s  %s  %s  task=%v  %s\n", item["id"], item["severity"], item["status"], item["task"], item["reason"])
	}
	return nil
}

// shiftPositional drops the first positional and renumbers the rest.
func shiftPositional(args Args) {
	var rest []string
	for i := 1; ; i++ {
		key := fmt.Sprintf("_pos%d", i)
		value, ok := args[key]
		if !ok {
			break
		}
		rest = append(rest, value)
		delete(args, key)
	}
	delete(args, "_pos0")
	delete(args, "_pos")
	for i, value := range rest {
		args[fmt.Sprintf("_pos%d", i)] = value
	}
	if len(rest) > 0 {
		args["_pos"] = strings.Join(rest, "\n")
	}
}
