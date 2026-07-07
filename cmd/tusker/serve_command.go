package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	serveassets "tusker/internal/serve"
)

const defaultServeAddr = "127.0.0.1:7420"

func serveCmd(args Args) error {
	if deferred, err := serveDeferToIncumbentDaemon(args, DefaultStateRoot()); deferred || err != nil {
		return err
	}
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	repoRoot := v7RepoRoot(vaultPath)
	addr, err := serveBindAddr(args)
	if err != nil {
		return err
	}
	dist, err := serveassets.DistFS()
	if err != nil {
		return err
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	server := newServeServer(vaultPath, repoRoot, addr, store, dist)
	httpServer := &http.Server{Addr: addr, Handler: server, ReadHeaderTimeout: 5 * time.Second}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "addr": addr, "vault": vaultPath})
	}
	if !args.Bool("quiet") {
		fmt.Printf("Serving Tusker on http://%s\n", addr)
	}
	err = httpServer.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func serveDeferToIncumbentDaemon(args Args, stateRoot string) (bool, error) {
	liveness := readDaemonLiveness(stateRoot, time.Now().UTC())
	if !liveness.Alive {
		return false, nil
	}
	addr := strings.TrimSpace(liveness.ServeAddr)
	if !liveness.ServeEnabled || addr == "" {
		if args.Bool("json") {
			emitJSON(map[string]any{"ok": true, "daemon_alive": true, "daemon_pid": liveness.PID, "serve_enabled": false})
			return true, nil
		}
		if !args.Bool("quiet") {
			fmt.Printf("Tusker daemon is running with embedded serve disabled (pid %d)\n", liveness.PID)
		}
		return true, nil
	}
	url := "http://" + addr
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "daemon_alive": true, "daemon_pid": liveness.PID, "serve_enabled": true, "addr": addr, "url": url})
		return true, nil
	}
	if !args.Bool("quiet") {
		fmt.Printf("Tusker daemon already serving on %s (pid %d)\n", url, liveness.PID)
	}
	return true, nil
}

func newServeServer(vaultPath, repoRoot, addr string, store *RuntimeStore, assets fs.FS) *serveServer {
	return &serveServer{
		vaultPath: vaultPath,
		repoRoot:  repoRoot,
		addr:      addr,
		store:     store,
		assets:    assets,
		now:       func() time.Time { return time.Now().UTC() },
	}
}

func serveBindAddr(args Args) (string, error) {
	raw := strings.TrimSpace(firstNonEmpty(args.String("addr"), args.String("listen"), defaultServeAddr))
	if port := strings.TrimSpace(args.String("port")); port != "" && raw == defaultServeAddr {
		raw = "127.0.0.1:" + strings.TrimPrefix(port, ":")
	}
	return serveNormalizeAddr(raw)
}

func serveNormalizeAddr(raw string) (string, error) {
	host, port, err := net.SplitHostPort(raw)
	if err != nil {
		if strings.Count(raw, ":") == 0 {
			host = "127.0.0.1"
			port = raw
		} else {
			return "", tuskerError(errorInvalidArg, "invalid serve address: "+raw)
		}
	}
	if strings.TrimSpace(port) == "" {
		port = "7420"
	}
	if strings.TrimSpace(host) == "" {
		host = "127.0.0.1"
	}
	if !serveIsLoopbackHost(host) {
		return "", tuskerError(errorInvalidArg, "tusker serve only binds localhost addresses", withContext(map[string]any{"addr": raw}))
	}
	return net.JoinHostPort(host, port), nil
}

func serveIsLoopbackHost(host string) bool {
	normalized := strings.ToLower(strings.Trim(host, "[] "))
	if normalized == "localhost" {
		return true
	}
	ip := net.ParseIP(normalized)
	return ip != nil && ip.IsLoopback()
}

func parseTruthyQuery(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "t", "yes", "y", "on", "all":
		return true
	default:
		return false
	}
}

func (s *serveServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Printf("tusker serve recovered handler panic: path=%s panic=%v", r.URL.Path, recovered)
			serveJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		}
	}()
	if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api" {
		s.handleAPI(w, r)
		return
	}
	s.handleAssets(w, r)
}

func (s *serveServer) handleAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		serveJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "read-only API"})
		return
	}
	path := strings.TrimSuffix(r.URL.Path, "/")
	if path == "" {
		path = "/"
	}
	switch {
	case path == "/api/daemon":
		s.handleDaemon(w, r)
	case path == "/api/projects":
		s.handleProjects(w, r)
	case path == "/api/needs":
		s.handleNeeds(w, r)
	case path == "/api/digest":
		s.handleDigest(w, r)
	case path == "/api/runs":
		s.handleRuns(w, r)
	case strings.HasPrefix(path, "/api/runs/"):
		s.handleRun(w, r, strings.TrimPrefix(path, "/api/runs/"))
	case path == "/api/epics":
		s.handleEpics(w, r)
	case path == "/api/waves":
		s.handleWaves(w, r)
	case strings.HasPrefix(path, "/api/waves/"):
		s.handleWave(w, r, strings.TrimPrefix(path, "/api/waves/"))
	case path == "/api/tasks":
		s.handleTasks(w, r)
	case strings.HasPrefix(path, "/api/tasks/"):
		s.handleTask(w, r, strings.TrimPrefix(path, "/api/tasks/"))
	case path == "/api/docs":
		s.handleDocs(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/docs/"):
		s.handleDoc(w, r, strings.TrimPrefix(r.URL.Path, "/api/docs/"))
	case path == "/api/roster":
		s.handleRoster(w, r)
	case path == "/api/review/batch":
		serveJSON(w, http.StatusOK, []any{})
	default:
		serveJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})
	}
}

func (s *serveServer) handleAssets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "read-only", http.StatusMethodNotAllowed)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	if serveFSExists(s.assets, path) {
		serveStaticFile(w, r, s.assets, path)
		return
	}
	serveStaticFile(w, r, s.assets, "index.html")
}

func serveStaticFile(w http.ResponseWriter, r *http.Request, assets fs.FS, path string) {
	file, err := assets.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	if ctype := mime.TypeByExtension(filepath.Ext(path)); ctype != "" {
		w.Header().Set("Content-Type", ctype)
	}
	http.ServeContent(w, r, path, info.ModTime(), file.(io.ReadSeeker))
}

func serveFSExists(files fs.FS, path string) bool {
	info, err := fs.Stat(files, path)
	return err == nil && !info.IsDir()
}

func serveJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func (s *serveServer) loadSnapshot() (serveSnapshot, error) {
	notes, err := listAllNotes(s.vaultPath)
	if err != nil {
		return serveSnapshot{}, err
	}
	projectID, err := resolveV7ProjectID(s.vaultPath)
	if err != nil {
		projectID = sanitizeProjectID(filepath.Base(s.repoRoot))
	}
	snap := serveSnapshot{
		projectID:   projectID,
		projectName: filepath.Base(s.repoRoot),
		notesByID:   map[string]Note{},
		queue:       map[string]automationTaskExplanation{},
	}
	if wfFile, err := loadWorkflow(s.vaultPath); err == nil {
		snap.workflow = wfFile.Data
	} else {
		snap.workflow = defaultWorkflow()
	}
	for _, note := range notes {
		id := stringField(note.Data, "id")
		if id != "" {
			snap.notesByID[id] = note
		}
		switch serveNoteKind(note) {
		case "task":
			snap.tasks = append(snap.tasks, note)
		case "epic":
			snap.epics = append(snap.epics, note)
		case "gate":
			snap.gates = append(snap.gates, note)
		case "wave":
			snap.waves = append(snap.waves, note)
		case "evidence":
			snap.evidence = append(snap.evidence, note)
		}
	}
	if projects, err := loadRegisteredProjects(s.store, registeredProjectLoadOptions{}); err == nil {
		for _, project := range projects {
			if project.Project.ProjectID == snap.projectID || sameCleanPath(project.Project.VaultRoot, s.vaultPath) || sameCleanPath(project.Project.RepoRoot, s.repoRoot) {
				snap.project = project.Project
				snap.projectRegistered = true
				snap.projectID = firstNonEmpty(project.Project.ProjectID, snap.projectID)
				snap.projectName = firstNonEmpty(project.Project.Name, snap.projectName)
				break
			}
		}
	}
	if snap.project.ProjectID == "" {
		snap.project = RegisteredProject{
			ProjectID:    snap.projectID,
			ProjectKey:   projectKeyFromPath(s.repoRoot),
			Name:         snap.projectName,
			RepoRoot:     s.repoRoot,
			VaultRoot:    s.vaultPath,
			WorkflowPath: workflowPath(s.vaultPath),
			Enabled:      true,
			Health:       projectHealthHealthy,
		}
	}
	if runs, err := s.store.ListRuns(); err == nil {
		for _, run := range runs {
			if run.ProjectID == "" || run.ProjectID == snap.projectID {
				snap.runs = append(snap.runs, run)
			}
		}
	}
	snap.queue = s.loadQueueExplanations()
	return snap, nil
}

func (s *serveServer) loadQueueExplanations() map[string]automationTaskExplanation {
	ctx, err := loadAutomationCommandContextWithStore(Args{"vault": s.vaultPath, "repo": s.repoRoot}, DefaultStateRoot(), s.store)
	if err != nil {
		return map[string]automationTaskExplanation{}
	}
	defer ctx.Close()
	report := ctx.automationQueueReport()
	out := map[string]automationTaskExplanation{}
	for _, explanation := range append(report.Eligible, report.Blocked...) {
		out[explanation.ID] = explanation
	}
	return out
}

func sameCleanPath(left, right string) bool {
	if left == "" || right == "" {
		return false
	}
	l, _ := filepath.Abs(left)
	r, _ := filepath.Abs(right)
	return filepath.Clean(l) == filepath.Clean(r)
}

func serveNoteKind(note Note) string {
	return strings.ToLower(firstNonEmpty(stringField(note.Data, "kind"), stringField(note.Data, "type")))
}

func (s *serveServer) handleDaemon(w http.ResponseWriter, _ *http.Request) {
	snap, err := s.loadSnapshot()
	if err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	active := 0
	for _, run := range snap.runs {
		if isDispatchingLeaseState(run.LeaseState) {
			active++
		}
	}
	queued := 0
	for _, explanation := range snap.queue {
		if explanation.Dispatchable {
			queued++
		}
	}
	daemonStatus, _ := s.store.DaemonStatus()
	loadedProjects, _ := loadRegisteredProjects(s.store, registeredProjectLoadOptions{})
	projects := loadedRegisteredProjects(loadedProjects)
	serveJSON(w, http.StatusOK, serveDaemonStatus{
		Connected:                  true,
		Addr:                       s.addr,
		ActiveRuns:                 active,
		QueuedTasks:                queued,
		LastPollAt:                 nullIfBlank(snap.project.LastPollAt),
		StateRoot:                  DefaultStateRoot(),
		ProjectCount:               intFromAny(daemonStatus["projects"]),
		Projects:                   projects,
		ParkedBudgetRuns:           intFromAny(daemonStatus["parkedBudgetRuns"]),
		PersistentEscalationBanner: hasOpenP0Escalation(s.vaultPath),
		BudgetCircuit:              daemonStatus["budgetCircuit"],
		InvariantCircuit:           daemonStatus["invariantCircuit"],
		DaemonAlive:                boolFromAny(daemonStatus["daemon_alive"]),
		DaemonPID:                  intFromAny(daemonStatus["daemon_pid"]),
		DaemonStartedAt:            nullIfBlank(stringValue(daemonStatus["daemon_started_at"])),
		DaemonLastPollAt:           nullIfBlank(stringValue(daemonStatus["daemon_last_poll_at"])),
	})
}

func (s *serveServer) handleDigest(w http.ResponseWriter, r *http.Request) {
	since, sinceOverride, err := digestSinceFromQuery(r.URL.Query().Get("since"))
	if err != nil {
		serveJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	digest, err := buildTuskerDigest(s.vaultPath, s.store, digestBuildOptions{
		Since:         since,
		SinceOverride: sinceOverride,
		Now:           s.now(),
	})
	if err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	serveJSON(w, http.StatusOK, digest)
}

func (s *serveServer) handleProjects(w http.ResponseWriter, r *http.Request) {
	snap, err := s.loadSnapshot()
	if err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	needs := serveNeeds(snap, s.now())
	active := 0
	var worst any
	for _, run := range snap.runs {
		if !isDispatchingLeaseState(run.LeaseState) {
			continue
		}
		active++
		liveness := serveRunLiveness(run, s.now())
		worst = serveWorstLiveness(worst, liveness)
	}
	item := serveProjectSummary{
		ID:              snap.projectID,
		Name:            snap.projectName,
		Health:          string(snap.project.Health),
		LastError:       nullIfBlank(snap.project.LastError),
		NeedsCount:      len(needs),
		ActiveRuns:      active,
		WorstLiveness:   worst,
		DaemonConnected: true,
		LastPollAt:      nullIfBlank(snap.project.LastPollAt),
	}
	if project := strings.TrimSpace(r.URL.Query().Get("project")); project != "" && project != item.ID {
		serveJSON(w, http.StatusOK, []serveProjectSummary{})
		return
	}
	serveJSON(w, http.StatusOK, []serveProjectSummary{item})
}

func (s *serveServer) handleNeeds(w http.ResponseWriter, r *http.Request) {
	snap, err := s.loadSnapshot()
	if err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	if project := strings.TrimSpace(r.URL.Query().Get("project")); project != "" && project != snap.projectID {
		serveJSON(w, http.StatusOK, []serveNeedItem{})
		return
	}
	serveJSON(w, http.StatusOK, serveNeeds(snap, s.now()))
}

func (s *serveServer) handleRuns(w http.ResponseWriter, r *http.Request) {
	snap, err := s.loadSnapshot()
	if err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	if project := strings.TrimSpace(r.URL.Query().Get("project")); project != "" && project != snap.projectID {
		serveJSON(w, http.StatusOK, []serveRunSummary{})
		return
	}
	out := []serveRunSummary{}
	includeAll := parseTruthyQuery(r.URL.Query().Get("all"))
	for _, run := range snap.runs {
		if !includeAll && serveRunHiddenByDefault(run) {
			continue
		}
		out = append(out, s.runSummary(snap, run))
	}
	serveJSON(w, http.StatusOK, out)
}

func (s *serveServer) handleRun(w http.ResponseWriter, _ *http.Request, taskID string) {
	snap, err := s.loadSnapshot()
	if err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	taskID = strings.TrimSpace(taskID)
	run, ok := serveFindRun(snap.runs, taskID)
	if !ok {
		serveJSON(w, http.StatusNotFound, map[string]any{"error": "run not found"})
		return
	}
	summary := s.runSummary(snap, run)
	attempts, _ := s.store.ListAttemptsForRun(run.ProjectID, run.RecordID)
	sort.Slice(attempts, func(i, j int) bool { return attempts[i].StartedAt < attempts[j].StartedAt })
	detail := serveRunDetail{serveRunSummary: summary, WorkspacePath: run.WorkspacePath, Attempts: []serveAttempt{}}
	for i, attempt := range attempts {
		turns, _ := s.store.ListTurnsForAttempt(attempt.AttemptID)
		detail.Attempts = append(detail.Attempts, serveAttempt{
			N:           i + 1,
			Outcome:     serveRunOutcomeFromAttempt(attempt.Outcome, run.LeaseState),
			DurationSec: serveDurationSec(attempt.StartedAt, firstNonEmpty(attempt.FinishedAt, run.UpdatedAt), s.now()),
			Tokens:      serveTokenTotalsForTurns(turns),
			StartedAt:   attempt.StartedAt,
		})
	}
	detail.Events = serveRunEvents(run, attempts)
	serveJSON(w, http.StatusOK, detail)
}

func (s *serveServer) handleEpics(w http.ResponseWriter, r *http.Request) {
	snap, err := s.loadSnapshot()
	if err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	if project := strings.TrimSpace(r.URL.Query().Get("project")); project != "" && project != snap.projectID {
		serveJSON(w, http.StatusOK, []serveEpicSummary{})
		return
	}
	serveJSON(w, http.StatusOK, serveEpics(snap))
}

func (s *serveServer) handleWaves(w http.ResponseWriter, r *http.Request) {
	snap, err := s.loadSnapshot()
	if err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	if project := strings.TrimSpace(r.URL.Query().Get("project")); project != "" && project != snap.projectID {
		serveJSON(w, http.StatusOK, []serveWaveSummary{})
		return
	}
	serveJSON(w, http.StatusOK, serveWaves(snap))
}

func (s *serveServer) handleWave(w http.ResponseWriter, _ *http.Request, id string) {
	snap, err := s.loadSnapshot()
	if err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	id = strings.TrimSpace(id)
	for _, wave := range snap.waves {
		if stringField(wave.Data, "id") == id {
			serveJSON(w, http.StatusOK, serveWaveSummaryFor(snap, wave))
			return
		}
	}
	serveJSON(w, http.StatusNotFound, map[string]any{"error": "wave not found"})
}

func (s *serveServer) handleTasks(w http.ResponseWriter, r *http.Request) {
	snap, err := s.loadSnapshot()
	if err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	if project := strings.TrimSpace(r.URL.Query().Get("project")); project != "" && project != snap.projectID {
		serveJSON(w, http.StatusOK, []serveTaskCapsule{})
		return
	}
	out := []serveTaskCapsule{}
	for _, task := range snap.tasks {
		out = append(out, serveTaskCapsuleFor(snap, task))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	serveJSON(w, http.StatusOK, out)
}

func (s *serveServer) handleTask(w http.ResponseWriter, _ *http.Request, id string) {
	snap, err := s.loadSnapshot()
	if err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	task, ok := snap.notesByID[strings.TrimSpace(id)]
	if !ok || serveNoteKind(task) != "task" {
		serveJSON(w, http.StatusNotFound, map[string]any{"error": "task not found"})
		return
	}
	detail := serveTaskDetail{
		serveTaskCapsule: serveTaskCapsuleFor(snap, task),
		Intent:           sectionContent(task.Body, "## Intent"),
		Acceptance:       serveAcceptanceRows(task),
		NonGoals:         serveBullets(sectionContent(task.Body, "## Non-goals")),
		Verification:     serveVerificationRows(task),
		Evidence:         serveEvidenceCards(snap, task),
		KnowledgeDelta:   sectionContent(task.Body, "## Knowledge delta"),
		Deps:             serveTaskDeps(snap, task),
		Gates:            serveGatesForTask(snap, stringField(task.Data, "id")),
		RunHistory:       serveRunHistory(s, snap, stringField(task.Data, "id")),
	}
	serveJSON(w, http.StatusOK, detail)
}

func (s *serveServer) handleRoster(w http.ResponseWriter, r *http.Request) {
	snap, err := s.loadSnapshot()
	if err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	if project := strings.TrimSpace(r.URL.Query().Get("project")); project != "" && project != snap.projectID {
		serveJSON(w, http.StatusOK, map[string]any{"rows": []any{}, "edges": []any{}})
		return
	}
	rows := []map[string]any{}
	edges := []map[string]any{}
	for _, run := range snap.runs {
		taskID := firstNonEmpty(run.ItemID, run.RecordID)
		row := map[string]any{
			"id":            firstNonEmpty(run.Runner, "runner") + ":" + taskID,
			"runner":        run.Runner,
			"projectId":     firstNonEmpty(run.ProjectID, snap.projectID),
			"taskId":        taskID,
			"workingOn":     nil,
			"blockedOn":     nil,
			"handingOffTo":  nil,
			"lastEventAt":   nullIfBlank(run.LastEventAt),
			"leaseState":    run.LeaseState,
			"attemptCount":  run.AttemptCount,
			"workspacePath": nullIfBlank(run.WorkspacePath),
		}
		if isDispatchingLeaseState(run.LeaseState) {
			row["workingOn"] = taskID
		}
		if strings.TrimSpace(run.LastError) != "" && !isDispatchingLeaseState(run.LeaseState) {
			row["blockedOn"] = taskID
		}
		if serveLane(run.Lane) == "execute" && serveRunOutcome(run, s.now()) == "succeeded" {
			row["handingOffTo"] = "reviewer"
			edges = append(edges, map[string]any{"from": row["id"], "to": "reviewer:" + taskID, "kind": "runner-reviewer"})
		}
		rows = append(rows, row)
	}
	serveJSON(w, http.StatusOK, map[string]any{"rows": rows, "edges": edges})
}

func serveEpics(snap serveSnapshot) []serveEpicSummary {
	epics := map[string]serveEpicSummary{}
	for _, epic := range snap.epics {
		id := stringField(epic.Data, "id")
		if id == "" {
			continue
		}
		epics[id] = serveEpicSummary{ID: id, Title: stringField(epic.Data, "title"), Counts: serveEmptyStatusCounts()}
	}
	for _, task := range snap.tasks {
		epicID := firstNonEmpty(stringField(task.Data, "epic"), "NONE")
		item, ok := epics[epicID]
		if !ok {
			item = serveEpicSummary{ID: epicID, Title: epicID, Counts: serveEmptyStatusCounts()}
		}
		status := serveTaskStatus(snap, task)
		item.Counts[status]++
		epics[epicID] = item
	}
	out := []serveEpicSummary{}
	for _, item := range epics {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func serveWaves(snap serveSnapshot) []serveWaveSummary {
	out := []serveWaveSummary{}
	for _, wave := range snap.waves {
		out = append(out, serveWaveSummaryFor(snap, wave))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func serveWaveSummaryFor(snap serveSnapshot, wave Note) serveWaveSummary {
	counts := serveEmptyStatusCounts()
	members := []serveWaveTaskSummary{}
	for _, taskID := range normalizeList(wave.Data["members"]) {
		task, ok := snap.notesByID[taskID]
		if !ok || serveNoteKind(task) != "task" {
			continue
		}
		group := serveWaveTaskGroup(snap, task)
		counts[group]++
		members = append(members, serveWaveTaskSummary{
			ID:     taskID,
			Title:  stringField(task.Data, "title"),
			Group:  group,
			Status: stringField(task.Data, "status"),
			Proof:  firstNonEmpty(stringField(task.Data, "proof_status"), "pending"),
		})
	}
	sort.Slice(members, func(i, j int) bool { return members[i].ID < members[j].ID })
	return serveWaveSummary{
		ID:        stringField(wave.Data, "id"),
		Title:     stringField(wave.Data, "title"),
		Status:    stringField(wave.Data, "status"),
		LandedAt:  nullIfBlank(stringField(wave.Data, "landed_at")),
		MemberIDs: normalizeList(wave.Data["members"]),
		Members:   members,
		Counts:    counts,
	}
}

func serveWaveTaskGroup(snap serveSnapshot, task Note) string {
	status := serveTaskStatus(snap, task)
	switch status {
	case "done", "review":
		return status
	case "in_progress":
		return "running"
	case "blocked":
		return "blocked"
	}
	rawStatus := strings.ToLower(strings.TrimSpace(stringField(task.Data, "status")))
	readiness := strings.ToLower(strings.TrimSpace(stringField(task.Data, "readiness")))
	if rawStatus == "cancelled" || rawStatus == "superseded" || readiness == "held" {
		return "parked"
	}
	if strings.HasPrefix(readiness, "blocked") || strings.HasPrefix(readiness, "waiting_on") {
		return "blocked"
	}
	return "ready"
}

func serveEmptyStatusCounts() map[string]int {
	return map[string]int{"backlog": 0, "ready": 0, "in_progress": 0, "running": 0, "review": 0, "blocked": 0, "parked": 0, "done": 0}
}

func serveTaskCapsuleFor(snap serveSnapshot, task Note) serveTaskCapsule {
	id := stringField(task.Data, "id")
	epicID := stringField(task.Data, "epic")
	waveID := stringField(task.Data, "wave")
	explanation := snap.queue[id]
	return serveTaskCapsule{
		ID:              id,
		Title:           stringField(task.Data, "title"),
		WaveID:          waveID,
		WaveTitle:       serveWaveTitle(snap, waveID),
		EpicID:          epicID,
		EpicTitle:       serveEpicTitle(snap, epicID),
		Status:          serveTaskStatus(snap, task),
		Readiness:       serveReadiness(task),
		Priority:        strings.ToLower(firstNonEmpty(stringField(task.Data, "priority"), "p2")),
		Risk:            strings.ToLower(firstNonEmpty(stringField(task.Data, "risk"), "medium")),
		HasGate:         len(serveUnsatisfiedGatesForTask(snap, id)) > 0,
		UpdatedAt:       serveUpdatedAt(task),
		ProjectID:       snap.projectID,
		RawStatus:       stringField(task.Data, "status"),
		RawReadiness:    stringField(task.Data, "readiness"),
		ReworkCount:     serveReworkCount(task),
		Blockers:        append([]string{}, explanation.Blockers...),
		Dispatchable:    explanation.Dispatchable,
		NextOwner:       stringField(task.Data, "next_owner"),
		NextAction:      stringField(task.Data, "next_action"),
		WorkRevision:    intField(task.Data, "work_revision"),
		ReadinessSource: "automation_queue",
	}
}

func serveTaskStatus(snap serveSnapshot, task Note) string {
	id := stringField(task.Data, "id")
	for _, run := range snap.runs {
		if (run.ItemID == id || run.RecordID == id) && isDispatchingLeaseState(run.LeaseState) {
			return "in_progress"
		}
	}
	switch strings.ToLower(stringField(task.Data, "status")) {
	case "done", "closed":
		return "done"
	case "review":
		return "review"
	case "blocked":
		return "blocked"
	case "ready", "rework":
		return "ready"
	case "backlog", "draft":
		return "backlog"
	default:
		return "backlog"
	}
}

func serveReadiness(task Note) string {
	raw := strings.ToLower(firstNonEmpty(stringField(task.Data, "readiness"), stringField(task.Data, "status")))
	switch {
	case strings.Contains(raw, "dep"):
		return "blocked_dependency"
	case strings.Contains(raw, "gate"), strings.Contains(raw, "human"), strings.Contains(raw, "review"):
		return "blocked_gate"
	case strings.Contains(raw, "draft"):
		return "draft"
	default:
		return "ready"
	}
}

func serveEpicTitle(snap serveSnapshot, epicID string) string {
	if epic, ok := snap.notesByID[epicID]; ok {
		return firstNonEmpty(stringField(epic.Data, "title"), epicID)
	}
	return epicID
}

func serveWaveTitle(snap serveSnapshot, waveID string) string {
	if waveID == "" {
		return ""
	}
	if wave, ok := snap.notesByID[waveID]; ok {
		return firstNonEmpty(stringField(wave.Data, "title"), waveID)
	}
	return waveID
}

func serveUpdatedAt(note Note) string {
	if v := firstNonEmpty(stringField(note.Data, "updated_at"), stringField(note.Data, "updated"), stringField(note.Data, "created_at"), stringField(note.Data, "created")); v != "" {
		return v
	}
	if info, err := os.Stat(note.AbsolutePath); err == nil {
		return info.ModTime().UTC().Format(time.RFC3339)
	}
	return ""
}

func serveAcceptanceRows(task Note) []serveAcceptanceRow {
	proofs := serveAcceptanceProofs(task)
	content := sectionContent(task.Body, "## Acceptance")
	out := []serveAcceptanceRow{}
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") {
			continue
		}
		cells := v7MarkdownTableCells(trimmed)
		if len(cells) < 2 || strings.EqualFold(cells[0], "id") || strings.Trim(cells[0], "-: ") == "" {
			continue
		}
		id := normalizeV7AcceptanceID(cells[0])
		if id == "" {
			continue
		}
		out = append(out, serveAcceptanceRow{ID: id, Text: cells[1], Proof: firstNonEmpty(proofs[id], "pending")})
	}
	return out
}

func serveAcceptanceProofs(task Note) map[string]string {
	ids := v7AcceptanceIDs(task.Body)
	proofs := map[string]string{}
	for _, id := range ids {
		proofs[id] = "pending"
	}
	for _, row := range parseV7VerificationRows(task.Body) {
		result := serveProofResult(row.Result)
		for _, id := range v7CoverTextToAcceptanceIDs(row.CoverText, ids) {
			if result == "fail" || proofs[id] != "pass" {
				proofs[id] = result
			}
		}
	}
	return proofs
}

func serveProofResult(result string) string {
	switch strings.ToLower(strings.TrimSpace(result)) {
	case "pass":
		return "pass"
	case "fail", "blocked":
		return "fail"
	default:
		return "pending"
	}
}

func serveVerificationRows(task Note) []serveVerificationRow {
	out := []serveVerificationRow{}
	for _, row := range parseV7VerificationRows(task.Body) {
		out = append(out, serveVerificationRow{
			ID:      row.CoverText,
			Command: row.Check,
			Result:  serveProofResult(row.Result),
			Detail:  row.Notes,
		})
	}
	return out
}

func serveEvidenceCards(snap serveSnapshot, task Note) []serveEvidenceCard {
	taskID := stringField(task.Data, "id")
	out := []serveEvidenceCard{}
	for _, evidence := range snap.evidence {
		if !serveEvidenceCoversTask(evidence, taskID) {
			continue
		}
		id := stringField(evidence.Data, "id")
		out = append(out, serveEvidenceCard{
			ID:    id,
			Label: firstNonEmpty(stringField(evidence.Data, "title"), id),
			Kind:  firstNonEmpty(stringField(evidence.Data, "evidence_kind"), "file"),
			Ref:   firstNonEmpty(stringField(evidence.Data, "artifact"), evidence.RelativePath),
		})
	}
	return out
}

func serveEvidenceCoversTask(evidence Note, taskID string) bool {
	for _, cover := range normalizeList(evidence.Data["covers"]) {
		if strings.HasPrefix(wikiTarget(cover), taskID) {
			return true
		}
	}
	return false
}

func serveTaskDeps(snap serveSnapshot, task Note) []serveTaskDependency {
	out := []serveTaskDependency{}
	for _, dep := range serveTaskDepIDs(task) {
		if note, ok := snap.notesByID[dep]; ok {
			out = append(out, serveTaskDependency{ID: dep, Title: stringField(note.Data, "title"), Status: serveTaskStatus(snap, note)})
		} else {
			out = append(out, serveTaskDependency{ID: dep, Title: dep, Status: "backlog"})
		}
	}
	return out
}

func serveTaskDepIDs(task Note) []string {
	var deps []string
	for _, key := range []string{"dependencies", "deps", "blocked_by"} {
		for _, dep := range normalizeList(task.Data[key]) {
			dep = wikiTarget(dep)
			if dep != "" {
				deps = append(deps, dep)
			}
		}
	}
	return uniqueStrings(deps)
}

func serveGatesForTask(snap serveSnapshot, taskID string) []serveGate {
	out := []serveGate{}
	for _, gate := range snap.gates {
		if !serveGateBlocksTask(gate, taskID) {
			continue
		}
		out = append(out, serveGateFromNote(gate))
	}
	return out
}

func serveUnsatisfiedGatesForTask(snap serveSnapshot, taskID string) []serveGate {
	out := []serveGate{}
	for _, gate := range serveGatesForTask(snap, taskID) {
		if !gate.Satisfied {
			out = append(out, gate)
		}
	}
	return out
}

func serveGateBlocksTask(gate Note, taskID string) bool {
	for _, block := range normalizeList(gate.Data["blocks"]) {
		if wikiTarget(block) == taskID {
			return true
		}
	}
	return false
}

func serveGateFromNote(gate Note) serveGate {
	rawKind := firstNonEmpty(stringField(gate.Data, "gate_kind"), stringField(gate.Data, "kind"))
	kind := serveGateKind(rawKind)
	out := serveGate{
		ID:        stringField(gate.Data, "id"),
		Kind:      kind,
		Owner:     stringField(gate.Data, "owner"),
		Satisfied: serveGateSatisfied(gate),
		RawKind:   rawKind,
		Question:  nil,
		Ask:       nil,
		Path:      nil,
		SpecTitle: nil,
		SpecPath:  nil,
	}
	action := firstNonEmpty(stringField(gate.Data, "action"), sectionContent(gate.Body, "## Action"), stringField(gate.Data, "title"))
	switch kind {
	case "clarify":
		out.Question = firstNonEmpty(stringField(gate.Data, "question"), action)
	case "provision":
		out.Ask = firstNonEmpty(stringField(gate.Data, "ask"), action)
		out.Path = nullIfBlank(firstNonEmpty(stringField(gate.Data, "path"), stringField(gate.Data, "material_path")))
	case "approve-spec":
		out.SpecTitle = firstNonEmpty(stringField(gate.Data, "spec_title"), stringField(gate.Data, "title"))
		out.SpecPath = nullIfBlank(firstNonEmpty(stringField(gate.Data, "spec_path"), serveFirstMarkdownPath(action)))
	}
	return out
}

func serveGateKind(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	switch {
	case strings.Contains(raw, "provision"), raw == "auth", raw == "env":
		return "provision"
	case strings.Contains(raw, "approve"), strings.Contains(raw, "signoff"), strings.Contains(raw, "sign-off"), strings.Contains(raw, "spec"):
		return "approve-spec"
	case raw == "review":
		return "review"
	case raw == "failed":
		return "failed"
	default:
		return "clarify"
	}
}

func serveGateSatisfied(gate Note) bool {
	status := strings.ToLower(stringField(gate.Data, "status"))
	return status == "satisfied" || status == "closed" || stringField(gate.Data, "satisfied_at") != ""
}

func serveFirstMarkdownPath(text string) string {
	for _, field := range strings.Fields(text) {
		cleaned := strings.Trim(field, "`'\"()[],:;")
		if strings.HasSuffix(cleaned, ".md") {
			return cleaned
		}
	}
	return ""
}

func serveBullets(text string) []string {
	out := []string{}
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		trimmed = strings.TrimPrefix(trimmed, "- ")
		trimmed = strings.TrimSpace(trimmed)
		if trimmed != "" && trimmed != "TBD." {
			out = append(out, trimmed)
		}
	}
	return out
}

func nullIfBlank(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func printServeHelp() {
	fmt.Println(`Usage:
  tusker serve [--addr 127.0.0.1:7420] [--vault <path>]

Purpose:
  Serve the embedded read-only Tusker control room and JSON API over the
  repo-local vault plus daemon runtime store. The server is localhost-only and
  exposes no mutating API routes.

Endpoints:
  GET /api/daemon
  GET /api/projects
  GET /api/needs?project=<id>
  GET /api/runs?project=<id>
  GET /api/runs/<task-id>
  GET /api/epics?project=<id>
  GET /api/waves?project=<id>
  GET /api/waves/<wave-id>
  GET /api/tasks?project=<id>
  GET /api/tasks/<task-id>
  GET /api/docs?project=<id>
  GET /api/docs/<repo-path>`)
}
