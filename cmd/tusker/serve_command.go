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
	"net/url"
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
	go server.warmRegisteredProjectSnapshots()
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
		stream:    newServeStreamBroker(),
		now:       func() time.Time { return time.Now().UTC() },
		snapshots: map[string]*serveSnapshotEntry{},
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

func serveMutationOriginRefusal(r *http.Request) string {
	if !serveRequestHostIsLoopback(r.Host) {
		return "refused mutation for non-loopback Host header"
	}
	for _, header := range []string{"Origin", "Referer"} {
		raw := strings.TrimSpace(r.Header.Get(header))
		if raw == "" {
			continue
		}
		if !serveSameOrigin(raw, r.Host) {
			return "refused cross-origin mutation"
		}
	}
	return ""
}

func serveRequestHostIsLoopback(hostport string) bool {
	host := strings.TrimSpace(hostport)
	if host == "" {
		return false
	}
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	}
	return serveIsLoopbackHost(host)
}

func serveSameOrigin(rawOrigin, requestHost string) bool {
	u, err := url.Parse(rawOrigin)
	if err != nil || u == nil || strings.TrimSpace(u.Host) == "" {
		return false
	}
	return serveCanonicalOriginHost(u.Host, u.Scheme) == serveCanonicalOriginHost(requestHost, "http")
}

func serveCanonicalOriginHost(hostport, scheme string) string {
	host := strings.TrimSpace(hostport)
	port := ""
	normalizedScheme := strings.ToLower(strings.TrimSpace(scheme))
	if normalizedScheme == "" {
		normalizedScheme = "http"
	}
	if parsedHost, parsedPort, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
		port = parsedPort
	}
	host = strings.ToLower(strings.Trim(host, "[] "))
	if port == "" {
		switch normalizedScheme {
		case "https":
			port = "443"
		default:
			port = "80"
		}
	}
	return normalizedScheme + "://" + net.JoinHostPort(host, port)
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
	path := strings.TrimSuffix(r.URL.Path, "/")
	if path == "" {
		path = "/"
	}
	if r.Method == http.MethodPost {
		if reason := serveMutationOriginRefusal(r); reason != "" {
			serveJSON(w, http.StatusForbidden, serveActionResult{OK: false, Refused: true, Reason: reason})
			return
		}
		if projectID, ok := serveProjectRefreshID(path); ok {
			s.handleProjectRefresh(w, projectID)
			return
		}
		if taskID, ok := serveRunInterruptTaskID(path); ok {
			s.handleRunInterrupt(w, r, taskID)
			return
		}
		if executionID, ok := serveExecutionCancelID(path); ok {
			s.handleExecutionCancel(w, r, executionID)
			return
		}
		if executionID, ok := serveExecutionActionID(path, "rename"); ok {
			s.handleExecutionRename(w, r, executionID)
			return
		}
		if executionID, ok := serveExecutionActionID(path, "bind"); ok {
			s.handleExecutionBind(w, r, executionID)
			return
		}
	}
	if r.Method == http.MethodPut && path == "/api/docgraph/doc" {
		if reason := serveMutationOriginRefusal(r); reason != "" {
			serveJSON(w, http.StatusForbidden, serveActionResult{OK: false, Refused: true, Reason: reason})
			return
		}
		s.handleDocgraphDocSave(w, r)
		return
	}
	if s.handleAPIMutation(w, r, path) {
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		serveJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "unsupported API method"})
		return
	}
	switch {
	case path == "/api/delivery/plans":
		s.handleDeliveryPlans(w, r)
	case path == "/api/delivery/review":
		s.handleDeliveryReview(w, r)
	case path == "/api/stream":
		s.handleStream(w, r)
	case path == "/api/daemon":
		s.handleDaemon(w, r)
	case path == "/api/projects":
		s.handleProjects(w, r)
	case path == "/api/needs":
		s.handleNeeds(w, r)
	case path == "/api/digest":
		s.handleDigest(w, r)
	case path == "/api/summary":
		s.handleSummary(w, r)
	case path == "/api/morning-brief":
		s.handleScheduledPromotionMorningBrief(w, r)
	case path == "/api/factory-operations":
		s.handleFactoryOperations(w, r)
	case path == "/api/runs":
		s.handleRuns(w, r)
	case path == "/api/executions":
		s.handleExecutionGraph(w, r)
	case path == "/api/executions/inbox":
		s.handleExecutionInbox(w, r)
	case path == "/api/executions/timeline":
		s.handleExecutionTimeline(w, r)
	case strings.HasPrefix(path, "/api/executions/"):
		if executionID, ok := serveExecutionActionID(path, "binding-preview"); ok {
			s.handleExecutionBindPreview(w, r, executionID)
			return
		}
		serveJSON(w, http.StatusNotFound, map[string]any{"error": "execution endpoint not found"})
	case strings.HasPrefix(path, "/api/runs/"):
		s.handleRun(w, r, strings.TrimPrefix(path, "/api/runs/"))
	case path == "/api/epics":
		s.handleEpics(w, r)
	case path == "/api/waves":
		s.handleWaves(w, r)
	case strings.HasPrefix(path, "/api/waves/"):
		s.handleWave(w, r, strings.TrimPrefix(path, "/api/waves/"))
	case path == "/api/gates":
		s.handleGates(w, r)
	case strings.HasPrefix(path, "/api/gates/"):
		s.handleGate(w, r, strings.TrimPrefix(path, "/api/gates/"))
	case path == "/api/evidence":
		s.handleEvidence(w, r)
	case strings.HasPrefix(path, "/api/evidence/"):
		s.handleEvidenceDoc(w, r, strings.TrimPrefix(path, "/api/evidence/"))
	case path == "/api/decisions":
		s.handleDecisions(w, r)
	case strings.HasPrefix(path, "/api/decisions/"):
		s.handleDecision(w, r, strings.TrimPrefix(path, "/api/decisions/"))
	case path == "/api/feedback":
		s.handleFeedback(w, r)
	case strings.HasPrefix(path, "/api/feedback/"):
		s.handleFeedbackDoc(w, r, strings.TrimPrefix(path, "/api/feedback/"))
	case path == "/api/attempts":
		s.handleAttempts(w, r)
	case strings.HasPrefix(path, "/api/attempts/"):
		s.handleAttempt(w, r, strings.TrimPrefix(path, "/api/attempts/"))
	case path == "/api/tasks":
		s.handleTasks(w, r)
	case strings.HasPrefix(path, "/api/tasks/"):
		s.handleTask(w, r, strings.TrimPrefix(path, "/api/tasks/"))
	case path == "/api/docs":
		s.handleDocs(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/docs/"):
		s.handleDoc(w, r, strings.TrimPrefix(r.URL.Path, "/api/docs/"))
	case path == "/api/docgraph":
		s.handleDocgraph(w, r)
	case path == "/api/docgraph/doc":
		s.handleDocgraphDoc(w, r)
	case path == "/api/roster":
		s.handleRoster(w, r)
	case path == "/api/review/batch":
		s.handleReviewBatch(w, r)
	default:
		serveJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})
	}
}

func (s *serveServer) handleReviewBatch(w http.ResponseWriter, r *http.Request) {
	snap, err := s.loadSnapshotForRequest(r)
	if err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	items := make([]serveTaskCapsule, 0)
	for _, task := range snap.tasks {
		if serveTaskStatus(snap, task) == "review" {
			items = append(items, serveTaskCapsuleFor(snap, task))
		}
	}
	serveJSON(w, http.StatusOK, items)
}

func (s *serveServer) handleScheduledPromotionMorningBrief(w http.ResponseWriter, r *http.Request) {
	now := s.now()
	night, err := scheduledPromotionMorningBriefNight(r.URL.Query().Get("date"), now)
	if err != nil {
		serveJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	snap, err := s.loadSnapshotForRequest(r)
	if err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	vaultPath := firstNonEmpty(strings.TrimSpace(snap.project.VaultRoot), s.vaultPath)
	brief, err := buildScheduledPromotionMorningBrief(vaultPath, snap.projectID, snap.workflow, s.store, night, now)
	if err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	serveJSON(w, http.StatusOK, brief)
}

func (s *serveServer) handleFactoryOperations(w http.ResponseWriter, r *http.Request) {
	snap, err := s.loadSnapshotForRequest(r)
	if err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	vaultPath := firstNonEmpty(strings.TrimSpace(snap.project.VaultRoot), s.vaultPath)
	projection, err := buildFactoryOperations(vaultPath, snap.project, snap.workflow, s.store, s.now())
	if err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	serveJSON(w, http.StatusOK, projection)
}

func serveRunInterruptTaskID(path string) (string, bool) {
	const prefix = "/api/runs/"
	const suffix = "/interrupt"
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return "", false
	}
	taskID := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	if taskID == "" || strings.Contains(taskID, "/") {
		return "", false
	}
	return taskID, true
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
	w.Header().Set("Cache-Control", serveStaticCacheControl(path))
	http.ServeContent(w, r, path, info.ModTime(), file.(io.ReadSeeker))
}

func serveStaticCacheControl(path string) string {
	clean := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(path)), "/")
	if strings.HasPrefix(clean, "assets/") && serveAssetIsFingerprinted(clean) {
		return "public, max-age=31536000, immutable"
	}
	// The SPA document may be stored so the native app can render it while the
	// daemon starts, but a live server must revalidate it before normal reuse.
	return "no-cache"
}

func serveAssetIsFingerprinted(path string) bool {
	base := filepath.Base(path)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	dash := strings.LastIndex(stem, "-")
	if dash < 0 || len(stem[dash+1:]) < 8 {
		return false
	}
	for _, char := range stem[dash+1:] {
		if (char < 'a' || char > 'z') &&
			(char < 'A' || char > 'Z') &&
			(char < '0' || char > '9') &&
			char != '_' && char != '-' {
			return false
		}
	}
	return true
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

const serveSnapshotBackgroundRefresh = 30 * time.Second

// loadSnapshot retains the launch project's compatibility path for callers
// without a project selector. All HTTP reads use loadSnapshotForProject.
func (s *serveServer) loadSnapshot() (serveSnapshot, error) {
	return s.loadSnapshotForProject("")
}

func (s *serveServer) projectForSnapshot(projectID string) (RegisteredProject, error) {
	projectID = strings.TrimSpace(projectID)
	loaded, err := loadRegisteredProjects(s.store, registeredProjectLoadOptions{MetadataOnly: true, LoadDisabled: true})
	if err == nil {
		for _, item := range loaded {
			project := item.Project
			if projectID != "" && project.ProjectID == projectID {
				return project, nil
			}
			if projectID == "" && (sameCanonicalProjectPath(project.VaultRoot, s.vaultPath) || sameCanonicalProjectPath(project.RepoRoot, s.repoRoot)) {
				return project, nil
			}
		}
	}
	if projectID != "" {
		return RegisteredProject{}, tuskerError(errorNotFound, "registered project not found: "+projectID)
	}
	resolvedID, resolveErr := resolveV7ProjectID(s.vaultPath)
	if resolveErr != nil {
		resolvedID = sanitizeProjectID(filepath.Base(s.repoRoot))
	}
	return RegisteredProject{
		ProjectID: resolvedID, ProjectKey: projectKeyFromPath(s.repoRoot), Name: filepath.Base(s.repoRoot),
		RepoRoot: s.repoRoot, VaultRoot: s.vaultPath, WorkflowPath: workflowPath(s.vaultPath), Enabled: true, Health: projectHealthHealthy,
	}, nil
}

func serveSnapshotKey(project RegisteredProject) string {
	return firstNonEmpty(project.ProjectID, filepath.Clean(project.VaultRoot), filepath.Clean(project.RepoRoot))
}

func (s *serveServer) cachedProjectForSnapshot(projectID string) (RegisteredProject, bool) {
	projectID = strings.TrimSpace(projectID)
	s.snapshotMu.Lock()
	defer s.snapshotMu.Unlock()
	if entry := s.snapshots[projectID]; projectID != "" && entry != nil {
		return entry.project, true
	}
	for _, entry := range s.snapshots {
		if entry == nil {
			continue
		}
		if projectID != "" && entry.project.ProjectID == projectID {
			return entry.project, true
		}
		if projectID == "" && (sameCanonicalProjectPath(entry.project.VaultRoot, s.vaultPath) || sameCanonicalProjectPath(entry.project.RepoRoot, s.repoRoot)) {
			return entry.project, true
		}
	}
	return RegisteredProject{}, false
}

func (s *serveServer) loadSnapshotForRequest(r *http.Request) (serveSnapshot, error) {
	return s.loadSnapshotForProject(strings.TrimSpace(r.URL.Query().Get("project")))
}

func (s *serveServer) loadSnapshotForProject(projectID string) (serveSnapshot, error) {
	return s.loadSnapshotForProjectMode(projectID, false)
}

func (s *serveServer) loadFreshSnapshotForProject(projectID string) (serveSnapshot, error) {
	return s.loadSnapshotForProjectMode(projectID, true)
}

func (s *serveServer) loadSnapshotForProjectMode(projectID string, waitFresh bool) (serveSnapshot, error) {
	project, cached := s.cachedProjectForSnapshot(projectID)
	if !cached {
		var err error
		project, err = s.projectForSnapshot(projectID)
		if err != nil {
			return serveSnapshot{}, err
		}
	}
	key := serveSnapshotKey(project)
	for {
		now := s.now()
		s.snapshotMu.Lock()
		if s.snapshots == nil {
			s.snapshots = map[string]*serveSnapshotEntry{}
		}
		entry := s.snapshots[key]
		if entry == nil {
			entry = &serveSnapshotEntry{project: project}
			s.snapshots[key] = entry
		} else {
			entry.project = project
		}
		if entry.ready && !entry.invalid {
			snap, cachedErr := entry.snapshot, entry.err
			if now.Sub(entry.builtAt) >= serveSnapshotBackgroundRefresh && !entry.building {
				entry.invalid = true
				go s.warmSnapshot(project.ProjectID)
			}
			s.snapshotMu.Unlock()
			return snap, cachedErr
		}
		if entry.ready && entry.invalid && !waitFresh {
			snap, cachedErr := entry.snapshot, entry.err
			if !entry.building {
				go s.warmSnapshot(project.ProjectID)
			}
			s.snapshotMu.Unlock()
			return snap, cachedErr
		}
		if entry.building {
			if entry.ready && !waitFresh {
				snap, cachedErr := entry.snapshot, entry.err
				s.snapshotMu.Unlock()
				return snap, cachedErr
			}
			done := entry.done
			s.snapshotMu.Unlock()
			<-done
			continue
		}
		entry.building = true
		entry.done = make(chan struct{})
		wasReady := entry.ready
		s.snapshotMu.Unlock()

		snap, buildErr := s.buildSnapshotForProject(project, true)
		contentHash := ""
		if buildErr == nil {
			contentHash = serveSnapshotContentHash(snap)
		}
		s.snapshotMu.Lock()
		previousHash := entry.contentHash
		entry.snapshot = snap
		entry.contentHash = contentHash
		entry.err = buildErr
		entry.ready = buildErr == nil
		entry.invalid = false
		entry.building = false
		entry.builtAt = s.now()
		entry.buildCount++
		close(entry.done)
		s.snapshotMu.Unlock()
		if wasReady && buildErr == nil && contentHash != previousHash && s.stream != nil {
			s.stream.Broadcast(serveStreamEvent{
				Kind: "projection_refreshed", Project: snap.projectID,
				Keys: []string{"projects", "needs", "runs", "tasks", "epics", "docs", "waves", "gates", "evidence", "decisions", "feedback", "attempts", "review:batch", "factory-operations"},
			})
		}
		return snap, buildErr
	}
}

func (s *serveServer) warmSnapshot(projectID string) {
	_, _ = s.loadFreshSnapshotForProject(projectID)
}

func (s *serveServer) warmRegisteredProjectSnapshots() {
	loaded, err := loadRegisteredProjects(s.store, registeredProjectLoadOptions{MetadataOnly: true, LoadDisabled: true})
	if err != nil || len(loaded) == 0 {
		s.warmSnapshot("")
		return
	}
	for _, item := range loaded {
		projectID := item.Project.ProjectID
		go s.warmSnapshot(projectID)
	}
}

func (s *serveServer) refreshProjectSnapshot(projectID string) {
	s.invalidateProjectSnapshot(projectID)
	go s.warmSnapshot(projectID)
}

func (s *serveServer) refreshRegisteredProjectSnapshots() {
	s.invalidateProjectSnapshot("")
	go s.warmRegisteredProjectSnapshots()
}

func (s *serveServer) buildSnapshot(includeQueue bool) (serveSnapshot, error) {
	project, err := s.projectForSnapshot("")
	if err != nil {
		return serveSnapshot{}, err
	}
	return s.buildSnapshotForProject(project, includeQueue)
}

func (s *serveServer) buildSnapshotForProject(project RegisteredProject, includeQueue bool) (serveSnapshot, error) {
	loaded, err := loadProjectContents(s.store, project, true)
	if err != nil {
		return serveSnapshot{}, err
	}
	project = loaded.Project
	notes := loaded.Notes
	projectID, err := resolveV7ProjectID(project.VaultRoot)
	if err != nil {
		projectID = firstNonEmpty(project.ProjectID, sanitizeProjectID(filepath.Base(project.RepoRoot)))
	}
	snap := serveSnapshot{
		projectID:         projectID,
		projectName:       firstNonEmpty(project.Name, filepath.Base(project.RepoRoot)),
		project:           project,
		projectRegistered: project.ProjectID != "",
		notesByID:         map[string]Note{},
		queue:             map[string]automationTaskExplanation{},
	}
	snap.workflow = loaded.Workflow.Data
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
		case "decision":
			snap.decisions = append(snap.decisions, note)
		case "attempt":
			snap.attemptNotes = append(snap.attemptNotes, note)
		case "escalation":
			if strings.EqualFold(stringField(note.Data, "status"), escalationStatusOpen) && strings.EqualFold(stringField(note.Data, "severity"), "P0") {
				snap.openP0Escalation = true
			}
		}
	}
	snap.projectID = firstNonEmpty(project.ProjectID, snap.projectID)
	if snap.project.ProjectID == "" {
		snap.project = RegisteredProject{
			ProjectID:    snap.projectID,
			ProjectKey:   projectKeyFromPath(project.RepoRoot),
			Name:         snap.projectName,
			RepoRoot:     project.RepoRoot,
			VaultRoot:    project.VaultRoot,
			WorkflowPath: workflowPath(project.VaultRoot),
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
	if includeQueue {
		snap.queue = s.loadQueueExplanationsForProject(project)
	}
	snap.docs, err = serveDocList(project.RepoRoot, project.VaultRoot, snap.workflow.Runtime.Serve.DocsDirs)
	if err != nil {
		return serveSnapshot{}, err
	}
	snap.needs = serveNeeds(snap, s.now())
	return snap, nil
}

func (s *serveServer) invalidateSnapshotCaches() {
	s.invalidateProjectSnapshot("")
}

func (s *serveServer) invalidateProjectSnapshot(projectID string) {
	projectID = strings.TrimSpace(projectID)
	s.snapshotMu.Lock()
	for key, entry := range s.snapshots {
		if projectID == "" || entry.project.ProjectID == projectID || key == projectID {
			entry.invalid = true
		}
	}
	s.snapshotMu.Unlock()
	s.summaryMu.Lock()
	s.summary = nil
	s.summaryAt = time.Time{}
	s.summaryMu.Unlock()
}

func (s *serveServer) loadQueueExplanations() map[string]automationTaskExplanation {
	project, err := s.projectForSnapshot("")
	if err != nil {
		return map[string]automationTaskExplanation{}
	}
	return s.loadQueueExplanationsForProject(project)
}

func (s *serveServer) loadQueueExplanationsForProject(project RegisteredProject) map[string]automationTaskExplanation {
	ctx, err := loadAutomationCommandContextWithStore(Args{"vault": project.VaultRoot, "repo": project.RepoRoot}, DefaultStateRoot(), s.store)
	if err != nil {
		return map[string]automationTaskExplanation{}
	}
	defer ctx.Close()
	// Serve is a read-only control surface.  Runner preflight belongs at the
	// claim boundary, not on every dashboard refresh.
	report := ctx.automationQueueReportWithRunnerHealth(false)
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
	serveJSON(w, http.StatusOK, s.daemonStatusFromSnapshot(snap))
}

func (s *serveServer) daemonStatusFromSnapshot(snap serveSnapshot) *serveDaemonStatus {
	active := 0
	parked := 0
	for _, run := range snap.runs {
		if !run.Terminal && isDispatchingLeaseState(run.LeaseState) {
			active++
		}
		if LeaseState(strings.TrimSpace(run.LeaseState)) == LeaseStateParkedNoProgress {
			parked++
		}
	}
	queued := 0
	for _, explanation := range snap.queue {
		if explanation.Dispatchable {
			queued++
		}
	}
	daemonStatus, _ := s.store.DaemonStatus()
	loadedProjects, _ := loadRegisteredProjects(s.store, registeredProjectLoadOptions{MetadataOnly: true, LoadDisabled: true})
	projects := loadedRegisteredProjects(loadedProjects)
	limit, _ := s.store.GlobalActiveRunLimit()
	if limit <= 0 {
		limit = 2
	}
	daemonAlive := boolFromAny(daemonStatus["daemon_alive"])
	var daemonDownReason any
	if !daemonAlive {
		daemonDownReason = "Daemon process is not running. Start the daemon to dispatch queued work."
	}
	return &serveDaemonStatus{
		Connected:                  true,
		Addr:                       s.addr,
		ActiveRuns:                 active,
		MaxActiveRuns:              limit,
		QueuedTasks:                queued,
		LastPollAt:                 nullIfBlank(snap.project.LastPollAt),
		StateRoot:                  DefaultStateRoot(),
		ProjectCount:               intFromAny(daemonStatus["projects"]),
		Projects:                   projects,
		CrashLoop:                  daemonStatus["crashLoop"],
		InvariantCircuit:           daemonStatus["invariantCircuit"],
		DiskPressure:               diskPressureStatusFromAny(daemonStatus["disk_pressure"]),
		DaemonAlive:                daemonAlive,
		DaemonDownReason:           daemonDownReason,
		DaemonPID:                  intFromAny(daemonStatus["daemon_pid"]),
		DaemonStartedAt:            nullIfBlank(stringValue(daemonStatus["daemon_started_at"])),
		DaemonLastPollAt:           nullIfBlank(stringValue(daemonStatus["daemon_last_poll_at"])),
		PersistentEscalationBanner: snap.openP0Escalation,
	}
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
	loadedProjects, err := loadRegisteredProjects(s.store, registeredProjectLoadOptions{LoadDisabled: true})
	if err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	projects := loadedRegisteredProjects(loadedProjects)
	if len(projects) == 0 {
		project, projectErr := s.projectForSnapshot("")
		if projectErr != nil {
			serveJSON(w, http.StatusInternalServerError, map[string]any{"error": projectErr.Error()})
			return
		}
		projects = []RegisteredProject{project}
	}
	allRuns, _ := s.store.ListRuns()
	workflowByProject := make(map[string]WorkflowFile, len(loadedProjects))
	for _, loaded := range loadedProjects {
		if loaded.LoadError == nil {
			workflowByProject[loaded.Project.ProjectID] = loaded.Workflow
		}
	}
	target := strings.TrimSpace(r.URL.Query().Get("project"))
	items := make([]serveProjectSummary, 0, len(projects))
	for _, project := range projects {
		if target != "" && target != project.ProjectID {
			continue
		}
		active := 0
		var worst any
		for _, run := range allRuns {
			if run.ProjectID != project.ProjectID || !isDispatchingLeaseState(run.LeaseState) {
				continue
			}
			active++
			worst = serveWorstLiveness(worst, serveRunLiveness(run, s.now()))
		}
		needsCount := 0
		wf, ok := workflowByProject[project.ProjectID]
		if !ok {
			continue
		}
		autoReport, _ := configResolveForRepo(project.RepoRoot, true, "automation.enabled")
		workspaceReport, _ := configResolveForRepo(project.RepoRoot, true, "workspace.strategy")
		concurrencyReport, _ := configResolveForRepo(project.RepoRoot, true, "runtime.max_active_runs_per_project")
		if snap, snapshotErr := s.loadSnapshotForProject(project.ProjectID); snapshotErr == nil {
			needsCount = len(snap.needs)
		}
		items = append(items, serveProjectSummary{
			ID: project.ProjectID, Name: project.Name,
			RepoRoot: project.RepoRoot, VaultRoot: project.VaultRoot,
			AutomationEnabled: wf.Data.AutomationEnabled,
			AutomationSource:  autoReport.Source,
			DispatchScope:     wf.Data.DispatchScope,
			WorkspaceMode:     string(workspaceStrategyFromWorkflow(wf.Data.Workspace.Strategy)), WorkspaceSource: workspaceReport.Source,
			MaxActiveRunsPerProject: wf.Data.Runtime.MaxActiveRunsPerProject, ConcurrencySource: concurrencyReport.Source,
			Health: string(project.Health), LastError: nullIfBlank(project.LastError),
			NeedsCount: needsCount, ActiveRuns: active, WorstLiveness: worst,
			DaemonConnected: true, LastPollAt: nullIfBlank(project.LastPollAt),
			Reconciliation: func() adaptiveProjectReconcileStatus {
				if s.reconcileStatus == nil {
					return adaptiveProjectReconcileStatus{}
				}
				return s.reconcileStatus(project.ProjectID)
			}(),
		})
	}
	serveJSON(w, http.StatusOK, items)
}

func (s *serveServer) handleNeeds(w http.ResponseWriter, r *http.Request) {
	snap, err := s.loadSnapshotForRequest(r)
	if err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	serveJSON(w, http.StatusOK, snap.needs)
}

type serveSummary struct {
	Attention    int    `json:"attention"`
	Review       int    `json:"review"`
	Running      int    `json:"running"`
	FailedRecent int    `json:"failed_recent"`
	GeneratedAt  string `json:"generated_at"`
}

func (s *serveServer) handleSummary(w http.ResponseWriter, r *http.Request) {
	snap, err := s.loadSnapshotForRequest(r)
	if err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	maxAttempts := snap.workflow.Retry.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	review := 0
	running := 0
	failed := 0
	for _, task := range snap.tasks {
		if strings.EqualFold(stringField(task.Data, "status"), "review") {
			review++
		}
	}
	for _, run := range snap.runs {
		if isDispatchingLeaseState(run.LeaseState) {
			running++
		}
		if serveTerminalFailure(run, maxAttempts) {
			failed++
		}
	}
	serveJSON(w, http.StatusOK, serveSummary{
		Attention: serveAttentionCount(snap), Review: review, Running: running,
		FailedRecent: failed, GeneratedAt: s.now().Format(time.RFC3339Nano),
	})
}

// loadSummarySnapshot keeps repeated badge refreshes on the warm in-memory
// projection. Stream-driven clients may request this endpoint several times in
// a burst; avoid a full vault walk for every one of those requests.
func (s *serveServer) loadSummarySnapshot() (serveSnapshot, error) {
	now := s.now()
	s.summaryMu.Lock()
	defer s.summaryMu.Unlock()
	if s.summary != nil && now.Sub(s.summaryAt) < time.Second {
		return *s.summary, nil
	}
	snap, err := s.buildSnapshot(false)
	if err != nil {
		return serveSnapshot{}, err
	}
	s.summary = &snap
	s.summaryAt = now
	return snap, nil
}

func serveAttentionCount(snap serveSnapshot) int {
	count := 0
	for _, task := range snap.tasks {
		for _, gate := range serveUnsatisfiedGatesForTask(snap, stringField(task.Data, "id")) {
			if serveHumanOwner(gate.Owner) {
				count++
			}
		}
		if serveReworkCount(task) >= 2 {
			count++
		}
	}
	maxAttempts := snap.workflow.Retry.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	for _, run := range snap.runs {
		if serveTerminalFailure(run, maxAttempts) {
			count++
		}
	}
	return count
}

func (s *serveServer) handleRuns(w http.ResponseWriter, r *http.Request) {
	snap, err := s.loadSnapshotForRequest(r)
	if err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	out := []serveRunSummary{}
	includeAll := parseTruthyQuery(r.URL.Query().Get("all"))
	for _, run := range snap.runs {
		// A retired run is a cleared record: it leaves the board so an
		// acknowledged failure stays gone. Its history is still reachable via
		// GET /api/runs/<task-id> for anyone who wants the trail.
		if serveRunRetired(run) {
			continue
		}
		if !includeAll && serveRunHiddenByDefault(run) {
			continue
		}
		out = append(out, s.runSummary(snap, run))
	}
	serveJSON(w, http.StatusOK, out)
}

func (s *serveServer) handleRun(w http.ResponseWriter, r *http.Request, taskID string) {
	snap, err := s.loadSnapshotForRequest(r)
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
	auth, _ := s.store.LatestRunAuthorization(run.ProjectID, run.RecordID)
	identity, _ := s.store.RunIdentity(run.ProjectID, run.RecordID)
	session, _ := s.store.LatestSession(run.ProjectID, run.RecordID, run.Runner)
	delivery := serveRunDelivery{Summary: run.FinalSummary, Verification: run.LogsSummary, ProofStatus: "pending", Artifact: run.ApplyRef}
	if task, found := snap.notesByID[taskID]; found {
		delivery.ProofStatus = firstNonEmpty(stringField(task.Data, "proof_status"), "pending")
	}
	detail := serveRunDetail{serveRunSummary: summary, WorkspacePath: summary.WorkspacePath, Attempts: []serveAttempt{}, Authorization: auth, Identity: identity, Session: session, Resume: resumeCapability(&run, session), Delivery: delivery}
	for i, attempt := range attempts {
		detail.Attempts = append(detail.Attempts, serveAttempt{
			N:           i + 1,
			Outcome:     serveRunOutcomeFromAttempt(attempt.Outcome, run.LeaseState),
			DurationSec: serveDurationSec(attempt.StartedAt, firstNonEmpty(attempt.FinishedAt, run.UpdatedAt), s.now()),
			StartedAt:   attempt.StartedAt,
		})
	}
	detail.Events = serveRunEvents(run, attempts)
	serveJSON(w, http.StatusOK, detail)
}

// handleRunRedrive maps the run-detail Retry control to `tusker redrive`. It
// guards on canonical task status first: a review/done task has no execution to
// redrive, so it returns a visible refusal instead of requeuing into the
// daemon's silent retire. A redrivable task is requeued and reported as such.
// The response always carries a reason so the UI can never swallow the result.
func (s *serveServer) handleRunRedrive(w http.ResponseWriter, r *http.Request, taskID string) {
	snap, err := s.loadSnapshotForRequest(r)
	if err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	taskID = strings.TrimSpace(taskID)
	run, ok := serveFindRun(snap.runs, taskID)
	if !ok {
		serveJSON(w, http.StatusNotFound, serveRedriveResult{TaskID: taskID, Reason: "no run found for this task"})
		return
	}
	rawStatus := ""
	if task, ok := snap.notesByID[taskID]; ok {
		rawStatus = stringField(task.Data, "status")
	}
	result := serveRedriveResult{
		TaskID:          taskID,
		CanonicalStatus: strings.ToLower(strings.TrimSpace(rawStatus)),
		LeaseState:      run.LeaseState,
	}
	if refused, reason := serveRedriveRefusal(rawStatus, run); refused {
		result.Refused = true
		result.Reason = reason
		serveJSON(w, http.StatusOK, result)
		return
	}
	actor := defaultActorName()
	reason := "operator redrive from serve"
	if err := serveRedriveRun(s.store, &run, actor, reason, s.now()); err != nil {
		serveJSON(w, http.StatusInternalServerError, serveRedriveResult{TaskID: taskID, Reason: "redrive failed: " + err.Error()})
		return
	}
	result.OK = true
	result.Requeued = true
	result.LeaseState = run.LeaseState
	result.Reason = "redrive requested — attempt window reset; the daemon will spawn a fresh attempt"
	s.refreshProjectSnapshot(snap.projectID)
	serveJSON(w, http.StatusOK, result)
}

func (s *serveServer) handleEpics(w http.ResponseWriter, r *http.Request) {
	snap, err := s.loadSnapshotForRequest(r)
	if err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	serveJSON(w, http.StatusOK, serveEpics(snap))
}

func (s *serveServer) handleWaves(w http.ResponseWriter, r *http.Request) {
	snap, err := s.loadSnapshotForRequest(r)
	if err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	serveJSON(w, http.StatusOK, serveWaves(snap))
}

func (s *serveServer) handleWave(w http.ResponseWriter, r *http.Request, id string) {
	snap, err := s.loadSnapshotForRequest(r)
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
	snap, err := s.loadSnapshotForRequest(r)
	if err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	out := []serveTaskCapsule{}
	for _, task := range snap.tasks {
		out = append(out, serveTaskCapsuleFor(snap, task))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	serveJSON(w, http.StatusOK, out)
}

func (s *serveServer) handleTask(w http.ResponseWriter, r *http.Request, id string) {
	snap, err := s.loadSnapshotForRequest(r)
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
		HumanAction:      serveHumanActionForTask(snap, task),
		HumanActions:     serveHumanActionsForTask(snap, task),
		RunHistory:       serveRunHistory(s, snap, stringField(task.Data, "id")),
	}
	if directive, directiveErr := s.store.RunDirective(snap.projectID, trackerRecordID(task)); directiveErr == nil && directive != nil {
		detail.RunDirective = &serveRunDirective{State: directive.State, Actor: directive.Actor, CreatedAt: directive.CreatedAt, ExpiresAt: directive.ExpiresAt, Reason: directive.Reason}
	}
	serveJSON(w, http.StatusOK, detail)
}

func (s *serveServer) handleRoster(w http.ResponseWriter, r *http.Request) {
	snap, err := s.loadSnapshotForRequest(r)
	if err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
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
	idx := v7Index{Tasks: map[string]Note{}, Gates: map[string]Note{}, Waves: map[string]Note{}, Evidence: map[string][]Note{}, Attempts: map[string][]Note{}}
	for _, task := range snap.tasks {
		idx.Tasks[stringField(task.Data, "id")] = task
	}
	for _, gate := range snap.gates {
		idx.Gates[stringField(gate.Data, "id")] = gate
	}
	for _, evidence := range snap.evidence {
		taskID := stringField(evidence.Data, "task")
		idx.Evidence[taskID] = append(idx.Evidence[taskID], evidence)
	}
	for _, attempt := range snap.attemptNotes {
		taskID := stringField(attempt.Data, "task")
		idx.Attempts[taskID] = append(idx.Attempts[taskID], attempt)
	}
	for _, item := range snap.waves {
		idx.Waves[stringField(item.Data, "id")] = item
	}
	if projected, err := armedWaveBriefProjectedIndex(snap.project.VaultRoot, idx, wave); err == nil {
		idx = projected
	}
	runs := map[string]RunStatus{}
	for _, run := range snap.runs {
		runs[firstNonEmpty(run.ItemID, run.RecordID)] = run
	}
	return serveWaveSummary{
		ID:            stringField(wave.Data, "id"),
		Title:         stringField(wave.Data, "title"),
		Status:        stringField(wave.Data, "status"),
		LandedAt:      nullIfBlank(stringField(wave.Data, "landed_at")),
		MemberIDs:     normalizeList(wave.Data["members"]),
		Members:       members,
		Counts:        counts,
		Authorization: waveAuthorizationProjection(snap.project.VaultRoot, idx, wave),
		Brief:         buildWaveBriefWithRuns(idx, wave, runs),
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
	openGates := serveOpenHumanGatesForTask(snap, id)
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
		HasGate:         len(openGates) > 0,
		OpenGates:       openGates,
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
	rawStatus := strings.ToLower(stringField(task.Data, "status"))
	// Human-owned work that has reached its gate is review work even when the
	// canonical task row still says backlog. The raw status remains exposed for
	// traceability; this is the operator-facing lifecycle projection.
	if rawStatus != "rework" && serveHumanActionForTask(snap, task) != nil {
		return "review"
	}
	switch rawStatus {
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
	if strings.EqualFold(stringField(task.Data, "status"), "rework") {
		return "ready"
	}
	raw := strings.ToLower(firstNonEmpty(stringField(task.Data, "readiness"), stringField(task.Data, "status")))
	switch {
	case raw == "waiting_on_human":
		return "waiting_on_human"
	case raw == "waiting_on_review":
		return "waiting_on_review"
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
		if !gate.Satisfied && strings.EqualFold(gateStatusForServe(snap, gate.ID), "open") {
			out = append(out, gate)
		}
	}
	return out
}

func serveOpenHumanGatesForTask(snap serveSnapshot, taskID string) []serveGateDetail {
	out := []serveGateDetail{}
	for _, gate := range snap.gates {
		if !strings.EqualFold(stringField(gate.Data, "status"), "open") ||
			!serveHumanOwner(stringField(gate.Data, "owner")) ||
			!serveGateBlocksTask(gate, taskID) {
			continue
		}
		detail := serveGateDetailFromNote(gate)
		detail.Body = ""
		out = append(out, detail)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func gateStatusForServe(snap serveSnapshot, gateID string) string {
	for _, gate := range snap.gates {
		if stringField(gate.Data, "id") == gateID {
			return stringField(gate.Data, "status")
		}
	}
	return ""
}

func serveGateBlocksTask(gate Note, taskID string) bool {
	for _, block := range normalizeList(gate.Data["blocks"]) {
		if strings.EqualFold(wikiTarget(block), taskID) {
			return true
		}
	}
	for _, cover := range normalizeList(gate.Data["covers"]) {
		cover = strings.ToUpper(strings.TrimSpace(cover))
		if strings.HasPrefix(cover, strings.ToUpper(taskID)+":") {
			return true
		}
	}
	return false
}

func serveGateBlockIDs(gate Note) []string {
	blocks := make([]string, 0)
	for _, block := range normalizeList(gate.Data["blocks"]) {
		if id := wikiTarget(block); id != "" {
			blocks = append(blocks, id)
		}
	}
	return uniqueStrings(blocks)
}

func serveGateBlockedTaskIDs(snap serveSnapshot, gateID string) []string {
	var gateNote Note
	for _, gate := range snap.gates {
		if stringField(gate.Data, "id") == gateID {
			gateNote = gate
			break
		}
	}
	if len(gateNote.Data) == 0 {
		return []string{}
	}
	ids := make([]string, 0)
	for _, task := range snap.tasks {
		id := stringField(task.Data, "id")
		if id != "" && serveGateBlocksTask(gateNote, id) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return uniqueStrings(ids)
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
	return status == "satisfied" || status == "closed" || status == "waived" || status == "obsolete" || stringField(gate.Data, "satisfied_at") != ""
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
  Serve the embedded Tusker operator control room and JSON API over the
  repo-local vault plus daemon runtime store. The server is localhost-only.

Endpoints:
  GET /api/daemon
  POST /api/daemon/(start|stop|resume|limits)
  GET /api/projects
  GET /api/needs?project=<id>
  GET /api/summary?project=<id>
  GET /api/morning-brief?project=<id>[&date=YYYY-MM-DD]
  GET /api/runs?project=<id>
  GET /api/runs/<task-id>
  GET /api/executions?project=<id>[&root=&parent=&task=&wave=&provider=&provider_id=&agent_type=&name=&cursor=&limit=]
  GET /api/executions/inbox?project=<id>
  POST /api/runs/<task-id>/redrive
  GET /api/epics?project=<id>
  GET /api/waves?project=<id>
  GET /api/waves/<wave-id>
  POST /api/waves/<wave-id>/land
  GET /api/tasks?project=<id>
  GET /api/tasks/<task-id>
  POST /api/tasks/<task-id>/(status|discard|close|land)
  GET /api/gates[?task=<id>]
  POST /api/gates/<gate-id>/(satisfy|waive|obsolete)
  GET /api/evidence[?task=<id>]
  POST /api/evidence
  GET /api/decisions[?epic=<id>]
  GET /api/feedback
  POST /api/feedback
  GET /api/attempts[?task=<id>]
  GET /api/docs?project=<id>
  GET /api/docs/<repo-path>`)
}
