package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"tusker/internal/v7policy"
	"tusker/internal/v7schema"

	"gopkg.in/yaml.v3"
)

var (
	v7TaskIDPattern       = v7schema.TaskIDPattern
	v7GateIDPattern       = v7schema.GateIDPattern
	v7WaveIDPattern       = v7schema.WaveIDPattern
	v7EscalationIDPattern = v7schema.EscalationIDPattern
	v7DecisionIDPattern   = v7schema.DecisionIDPattern
	v7ProposalIDPattern   = v7schema.ProposalIDPattern
	v7EvidenceIDPattern   = v7schema.EvidenceIDPattern
	v7AttemptIDPattern    = v7schema.AttemptIDPattern

	v7TaskStatuses         = v7schema.TaskStatuses
	v7WaveStatuses         = v7schema.WaveStatuses
	v7EscalationSeverities = v7schema.EscalationSeverities
	v7EscalationStatuses   = v7schema.EscalationStatuses
	v7Readiness            = v7schema.Readiness
	v7GateKinds            = v7schema.GateKinds
	v7GateStatuses         = v7schema.GateStatuses
	v7ProofModes           = v7schema.ProofModes
	v7ProofStatuses        = v7schema.ProofStatuses
	v7EvidenceKinds        = v7schema.EvidenceKinds
	v7EvidenceStatus       = v7schema.EvidenceStatus
	v7AttemptStatus        = v7schema.AttemptStatus
	v7DecisionStatus       = v7schema.DecisionStatus
	v7ProposalStatus       = v7schema.ProposalStatus
	v7ProposalAction       = v7schema.ProposalAction
	v7DomainStatus         = v7schema.DomainStatus
)

var v7FrontmatterOrder = v7schema.FrontmatterOrder

// Used by focused tests to assert that one command invocation shares its
// index work. Production leaves this nil.
var loadV7IndexObserver func()

type v7Index struct {
	Tasks       map[string]Note
	Gates       map[string]Note
	Waves       map[string]Note
	Escalations map[string]Note
	Evidence    map[string][]Note
	Attempts    map[string][]Note
	Decisions   map[string]Note
	Epics       map[string]Note
	Proposals   map[string]Note
	Closeouts   map[string][]Note
}

type v7LeaseRecord struct {
	Schema      string `json:"schema"`
	ID          string `json:"id"`
	Project     string `json:"project"`
	Task        string `json:"task"`
	Owner       string `json:"owner"`
	Workspace   string `json:"workspace"`
	Branch      string `json:"branch"`
	Status      string `json:"status"`
	ClaimedAt   string `json:"claimed_at"`
	ExpiresAt   string `json:"expires_at"`
	HeartbeatAt string `json:"heartbeat_at"`
	StaleAt     string `json:"stale_at,omitempty"`
	StaleReason string `json:"stale_reason,omitempty"`
	Path        string `json:"path,omitempty"`
}

type v7StateFile struct {
	Path    string
	Content string
}

type v7ObjectKind string
type v7ObjectID string
type v7Rev string

type v7Object interface {
	Kind() v7ObjectKind
	ID() v7ObjectID
	Rev() v7Rev
	Validate(context.Context) []Issue
}

type v7Store interface {
	Load(context.Context, v7ObjectID) (v7Object, error)
	SaveCAS(context.Context, v7Object, v7Rev) (v7Rev, error)
	List(context.Context, v7Query) ([]v7ObjectRef, error)
	AppendEvent(context.Context, v7Event) error
	GetEvents(context.Context, v7EventScope) ([]v7Event, error)
}

type v7RemoteStore interface {
	GetObject(context.Context, v7ObjectID) (v7Object, v7Rev, error)
	PutObjectCAS(context.Context, v7ObjectID, v7Rev, v7Object) (v7Rev, error)
	ListObjects(context.Context, v7Query) ([]v7ObjectRef, error)
	AppendEvent(context.Context, v7Event) error
	GetEvents(context.Context, v7EventScope) ([]v7Event, error)
}

type v7RuntimeStore interface {
	Claim(context.Context, v7ObjectID, string, time.Duration) (v7LeaseRecord, error)
	Heartbeat(context.Context, string) error
	Release(context.Context, string) error
	ListLeases(context.Context, v7LeaseQuery) ([]v7LeaseRecord, error)
}

type v7StateBackend interface {
	Sync(context.Context, v7StateSyncOptions) (string, error)
	Import(context.Context, v7StateSyncOptions) (int, error)
	Export(context.Context, v7StateSyncOptions) (int, error)
}

type v7Query struct {
	Kind v7ObjectKind
}

type v7LeaseQuery struct {
	Task   string
	Owner  string
	Status string
}

type v7StateSyncOptions struct {
	Branch  string
	Remote  string
	Message string
	Dir     string
}

type v7ObjectRef struct {
	ID   v7ObjectID
	Kind v7ObjectKind
	Path string
	Rev  v7Rev
}

type v7Event struct {
	ID         string
	ObjectID   string
	ObjectKind string
	EventKind  string
	Actor      string
	At         string
	Path       string
	Payload    map[string]any
}

type v7EventScope struct {
	ObjectID   string
	ObjectKind string
	EventKind  string
	Actor      string
}

type v7MarkdownStore struct {
	VaultPath string
}

type v7FileRuntimeStore struct {
	VaultPath string
}

type v7GitStateBackend struct {
	VaultPath string
}

type v7MarkdownObject struct {
	VaultPath string
	Note      Note
	Data      map[string]any
	Body      string
}

type v7ClosePolicy = v7policy.ClosePolicy
type v7ClosePolicyConfigFile = v7policy.ClosePolicyConfigFile
type v7ClosePolicyConfigRule = v7policy.ClosePolicyConfigRule
type v7TuskerConfigFile = v7schema.TuskerConfigFile

func (o v7MarkdownObject) Kind() v7ObjectKind {
	return v7ObjectKind(effectiveV7Kind(o.Data))
}

func (o v7MarkdownObject) ID() v7ObjectID {
	return v7ObjectID(stringField(o.Data, "id"))
}

func (o v7MarkdownObject) Rev() v7Rev {
	return v7Rev(stringField(o.Data, "state_rev"))
}

func (o v7MarkdownObject) Validate(ctx context.Context) []Issue {
	select {
	case <-ctx.Done():
		return []Issue{issue("CONTEXT_CANCELLED", ctx.Err().Error(), o.Note.RelativePath, "", nil)}
	default:
	}
	note := o.Note
	note.Data = o.Data
	note.Body = o.Body
	errs, _ := validateV7Note(note, validationContext{
		RelativePath: o.Note.RelativePath,
		Basename:     filepath.Base(o.Note.AbsolutePath),
		VaultPath:    o.VaultPath,
	}, firstNonEmpty(o.Note.RelativePath, o.Note.AbsolutePath))
	return errs
}

func (s v7MarkdownStore) Load(ctx context.Context, id v7ObjectID) (v7Object, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	notes, err := listAllNotes(s.VaultPath)
	if err != nil {
		return nil, err
	}
	for _, note := range notes {
		if stringField(note.Data, "id") != string(id) {
			continue
		}
		if !isV7StoreObject(note.Data) {
			continue
		}
		data, body, err := parseFrontmatterMustRead(note.AbsolutePath)
		if err != nil {
			return nil, err
		}
		return v7MarkdownObject{VaultPath: s.VaultPath, Note: note, Data: data, Body: body}, nil
	}
	return nil, tuskerError(errorNotFound, "V7 object not found: "+string(id))
}

func (s v7MarkdownStore) SaveCAS(ctx context.Context, obj v7Object, base v7Rev) (v7Rev, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	current, ok := obj.(v7MarkdownObject)
	if !ok {
		return "", tuskerError(errorInvalidArg, "markdown store can only save V7 markdown objects")
	}
	order := v7FrontmatterOrder[string(current.Kind())]
	if len(order) == 0 {
		order = frontmatterOrderForType(string(current.Kind()))
	}
	next, err := saveV7DocumentCAS(current.Note.AbsolutePath, current.Data, current.Body, order, string(base))
	return v7Rev(next), err
}

func (s v7MarkdownStore) List(ctx context.Context, q v7Query) ([]v7ObjectRef, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	notes, err := listAllNotes(s.VaultPath)
	if err != nil {
		return nil, err
	}
	var refs []v7ObjectRef
	for _, note := range notes {
		if !isV7StoreObject(note.Data) {
			continue
		}
		kind := v7ObjectKind(effectiveV7Kind(note.Data))
		if q.Kind != "" && kind != q.Kind {
			continue
		}
		refs = append(refs, v7ObjectRef{
			ID:   v7ObjectID(stringField(note.Data, "id")),
			Kind: kind,
			Path: note.RelativePath,
			Rev:  v7Rev(stringField(note.Data, "state_rev")),
		})
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Kind == refs[j].Kind {
			return refs[i].ID < refs[j].ID
		}
		return refs[i].Kind < refs[j].Kind
	})
	return refs, nil
}

func (s v7MarkdownStore) AppendEvent(ctx context.Context, ev v7Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return emitV7Event(s.VaultPath, ev.ObjectID, ev.ObjectKind, ev.EventKind, ev.Actor, ev.Payload)
}

func (s v7MarkdownStore) GetEvents(ctx context.Context, scope v7EventScope) ([]v7Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	eventsRoot := filepath.Join(s.VaultPath, "events")
	if _, err := os.Stat(eventsRoot); os.IsNotExist(err) {
		return nil, nil
	}
	var events []v7Event
	err := filepath.WalkDir(eventsRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			return nil
		}
		raw, err := readText(path)
		if err != nil {
			return err
		}
		var data map[string]any
		if err := json.Unmarshal([]byte(raw), &data); err != nil {
			return err
		}
		ev := v7Event{
			ID:         stringField(data, "id"),
			ObjectID:   stringField(data, "object"),
			ObjectKind: stringField(data, "object_kind"),
			EventKind:  stringField(data, "event_kind"),
			Actor:      stringField(data, "actor"),
			At:         stringField(data, "at"),
			Path:       v7RelativeToVault(s.VaultPath, path),
		}
		if payload, ok := data["payload"].(map[string]any); ok {
			ev.Payload = payload
		}
		if scope.ObjectID != "" && ev.ObjectID != scope.ObjectID {
			return nil
		}
		if scope.ObjectKind != "" && ev.ObjectKind != scope.ObjectKind {
			return nil
		}
		if scope.EventKind != "" && ev.EventKind != scope.EventKind {
			return nil
		}
		if scope.Actor != "" && ev.Actor != scope.Actor {
			return nil
		}
		events = append(events, ev)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(events, func(i, j int) bool {
		if events[i].At == events[j].At {
			return events[i].ID < events[j].ID
		}
		return events[i].At < events[j].At
	})
	return events, nil
}

func (s v7FileRuntimeStore) Claim(ctx context.Context, taskID v7ObjectID, owner string, ttl time.Duration) (v7LeaseRecord, error) {
	if err := ctx.Err(); err != nil {
		return v7LeaseRecord{}, err
	}
	return s.writeLease(ctx, string(taskID), "", owner, "", currentGitBranch(), "active", ttl)
}

func (s v7FileRuntimeStore) Heartbeat(ctx context.Context, leaseID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	lease, err := s.findActiveLease(ctx, leaseID)
	if err != nil {
		return err
	}
	_, err = s.writeLease(ctx, lease.Task, lease.ID, lease.Owner, lease.Workspace, lease.Branch, "active", 0)
	return err
}

func (s v7FileRuntimeStore) Release(ctx context.Context, leaseID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	lease, err := s.findActiveLease(ctx, leaseID)
	if err != nil {
		return err
	}
	released, err := s.writeLease(ctx, lease.Task, lease.ID, lease.Owner, lease.Workspace, lease.Branch, "released", 0)
	if err != nil {
		return err
	}
	return emitV7Event(s.VaultPath, released.Task, "task", "claim_released", released.Owner, map[string]any{"lease": released.ID})
}

func (s v7FileRuntimeStore) ListLeases(ctx context.Context, q v7LeaseQuery) ([]v7LeaseRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	leases, err := loadV7Leases(s.VaultPath)
	if err != nil {
		return nil, err
	}
	var out []v7LeaseRecord
	for _, lease := range leases {
		if q.Task != "" && lease.Task != q.Task {
			continue
		}
		if q.Owner != "" && lease.Owner != q.Owner {
			continue
		}
		if q.Status != "" && lease.Status != q.Status {
			continue
		}
		out = append(out, lease)
	}
	return out, nil
}

func (s v7FileRuntimeStore) findLease(ctx context.Context, leaseID string) (v7LeaseRecord, error) {
	leases, err := s.ListLeases(ctx, v7LeaseQuery{})
	if err != nil {
		return v7LeaseRecord{}, err
	}
	for _, lease := range leases {
		if lease.ID == leaseID || lease.Task == leaseID {
			return lease, nil
		}
	}
	return v7LeaseRecord{}, tuskerError(errorNotFound, "V7 lease not found: "+leaseID)
}

func (s v7FileRuntimeStore) findActiveLease(ctx context.Context, leaseID string) (v7LeaseRecord, error) {
	lease, err := s.findLease(ctx, leaseID)
	if err != nil {
		return v7LeaseRecord{}, err
	}
	if lease.Status != "active" {
		return v7LeaseRecord{}, tuskerError(errorInvalidTransition, "V7 lease is not active: "+leaseID, withContext(map[string]any{"lease": lease.ID, "task": lease.Task, "status": lease.Status}))
	}
	return lease, nil
}

func (s v7FileRuntimeStore) writeLease(ctx context.Context, taskID, leaseID, owner, workspace, branch, status string, ttl time.Duration) (v7LeaseRecord, error) {
	if err := ctx.Err(); err != nil {
		return v7LeaseRecord{}, err
	}
	if taskID == "" {
		return v7LeaseRecord{}, tuskerError(errorMissingArg, "Missing task id")
	}
	if ttl <= 0 {
		ttl = v7LeaseTTL(s.VaultPath)
	}
	now := time.Now().UTC()
	path := filepath.Join(v7LeaseDir(s.VaultPath), taskID+".json")
	lease := v7LeaseRecord{
		Schema:      "tusker.lease/v1",
		ID:          fallback(leaseID, taskID),
		Project:     v7ProjectID(s.VaultPath),
		Task:        taskID,
		Owner:       fallback(owner, "agent:"+defaultActorName()),
		Workspace:   workspace,
		Branch:      fallback(branch, currentGitBranch()),
		Status:      status,
		ClaimedAt:   now.Format(time.RFC3339),
		ExpiresAt:   now.Add(ttl).Format(time.RFC3339),
		HeartbeatAt: now.Format(time.RFC3339),
	}
	if fileExists(path) {
		raw, err := readText(path)
		if err != nil {
			return v7LeaseRecord{}, err
		}
		var existing v7LeaseRecord
		if err := json.Unmarshal([]byte(raw), &existing); err != nil {
			return v7LeaseRecord{}, err
		}
		if status == "active" && owner != "" && existing.Status == "active" && existing.Owner != "" && existing.Owner != owner {
			return v7LeaseRecord{}, tuskerError(errorAlreadyExists, taskID+" already has an active lease owned by "+existing.Owner, withHint("release the lease or wait for it to become stale before claiming"), withContext(map[string]any{"task": taskID, "owner": existing.Owner, "requested_owner": owner}))
		}
		lease.ID = fallback(leaseID, fallback(existing.ID, taskID))
		lease.Owner = fallback(owner, fallback(existing.Owner, lease.Owner))
		lease.Workspace = fallback(workspace, existing.Workspace)
		lease.Branch = fallback(branch, fallback(existing.Branch, lease.Branch))
		lease.ClaimedAt = fallback(existing.ClaimedAt, lease.ClaimedAt)
	}
	if err := writeJSON(path, lease); err != nil {
		return v7LeaseRecord{}, err
	}
	return lease, nil
}

func (b v7GitStateBackend) Sync(ctx context.Context, opts v7StateSyncOptions) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return syncV7GitStateBranch(b.VaultPath, fallback(opts.Branch, v7StateBranch(b.VaultPath)), opts.Remote, opts.Message)
}

func (b v7GitStateBackend) Import(ctx context.Context, opts v7StateSyncOptions) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return importV7GitStateBranch(b.VaultPath, fallback(opts.Branch, v7StateBranch(b.VaultPath)), opts.Remote)
}

func (b v7GitStateBackend) Export(ctx context.Context, opts v7StateSyncOptions) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	dir := opts.Dir
	if dir == "" {
		dir = filepath.Join(filepath.Dir(b.VaultPath), ".tusker-runtime", "state")
	}
	return exportV7StateDir(b.VaultPath, dir)
}

func isV7StoreObject(data map[string]any) bool {
	schema := stringField(data, "schema")
	if schema == "" {
		return false
	}
	return strings.HasSuffix(schema, "/v7") ||
		schema == "tusker.gate/v1" ||
		schema == "tusker.evidence/v1" ||
		schema == "tusker.attempt/v1" ||
		schema == "tusker.decision/v1" ||
		schema == "tusker.proposal/v1"
}

func isV7NewEpicSpecForm(args Args) bool {
	if args.String("acronym") != "" {
		return false
	}
	return epicAcronymPattern.MatchString(strings.ToUpper(args.String("_pos0")))
}

func shouldDefaultNewTaskToV7(args Args) bool {
	epic := strings.ToUpper(strings.TrimSpace(args.String("epic")))
	if !epicAcronymPattern.MatchString(epic) {
		return false
	}
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return false
	}
	v7EpicPath := filepath.Join(vaultPath, "work", "epics", epic+".md")
	v5EpicDir := filepath.Join(vaultPath, "epics", epic)
	v5EpicExists := fileExists(filepath.Join(v5EpicDir, epic+".md")) || fileExists(filepath.Join(v5EpicDir, "index.md"))
	return fileExists(v7EpicPath) && !v5EpicExists
}

func newV7Epic(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	acronym := strings.ToUpper(firstNonEmpty(args.String("acronym"), args.String("_pos0")))
	if acronym == "" {
		return tuskerError(errorMissingArg, "Missing required --acronym")
	}
	if !epicAcronymPattern.MatchString(acronym) {
		return tuskerError(errorInvalidArg, fmt.Sprintf(`--acronym must be 3 uppercase letters, got "%s"`, acronym))
	}
	title, err := requireArg(args, "title")
	if err != nil {
		return err
	}
	path := filepath.Join(vaultPath, "work", "epics", acronym+".md")
	if fileExists(path) {
		return tuskerError(errorAlreadyExists, "V7 epic already exists: "+acronym, withPath(path))
	}
	now := time.Now().UTC().Format(time.RFC3339)
	specRefs := splitCSV(firstNonEmpty(args.String("spec-refs"), args.String("spec_refs")))
	data := map[string]any{
		"schema":   "tusker.epic/v7",
		"kind":     "epic",
		"id":       acronym,
		"project":  v7ProjectID(vaultPath),
		"title":    title,
		"status":   fallback(args.String("status"), "ready"),
		"owner":    fallback(args.String("owner"), "human:"+defaultActorName()),
		"priority": strings.ToLower(fallback(args.String("priority"), "p2")),
		"domains":  splitCSV(args.String("domains")),
		"capsule": v7CapsuleOrdered(
			acronym+" epic: "+title+".",
			"Use to triage this workstream's scope, active tasks, and durable direction.",
			"Skip when you need a specific task contract, proof row, gate, or attempt.",
		),
		"next_task_number":     1,
		"next_gate_number":     1,
		"next_decision_number": 1,
		"created_at":           now,
		"updated_at":           now,
	}
	if len(specRefs) > 0 {
		data["spec_refs"] = specRefs
	}
	body := fmt.Sprintf(`# %s · %s

## Thesis

%s

## Success criteria

- [ ] Define success criteria.

## Current decision

TBD.

## Open gates

<!-- tusker:generated open-gates -->

## Active work

<!-- tusker:generated active-work -->

## Recently completed

<!-- tusker:generated recently-completed -->
`, acronym, title, fallback(args.String("summary"), "TBD."))
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["epic"])
	if err != nil {
		return err
	}
	if err := writeText(path, content); err != nil {
		return err
	}
	if !args.Bool("quiet") {
		fmt.Printf("Created V7 epic %s at %s\n", acronym, path)
	}
	return emitV7Event(vaultPath, acronym, "epic", "created", "agent:"+defaultActorName(), nil)
}

func newV7Task(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	epic, err := requireArg(args, "epic")
	if err != nil {
		return err
	}
	epic = strings.ToUpper(epic)
	if !epicAcronymPattern.MatchString(epic) {
		return tuskerError(errorInvalidArg, fmt.Sprintf(`--epic must be 3 uppercase letters, got "%s"`, epic))
	}
	title, err := requireArg(args, "title")
	if err != nil {
		return err
	}
	id := strings.ToUpper(args.String("id"))
	if id == "" {
		id = nextSafeV7TaskID(vaultPath, epic)
	}
	if !v7TaskIDPattern.MatchString(id) {
		return tuskerError(errorInvalidArg, "invalid V7 task id: "+id)
	}
	path := filepath.Join(vaultPath, "work", "tasks", id+".md")
	if conflicts := v7TaskIDCollisionPaths(vaultPath, id); len(conflicts) > 0 {
		nextSafe := nextSafeV7TaskID(vaultPath, firstNonEmpty(v7EpicFromTaskID(id), epic))
		conflictingPaths := uniqueStrings(append([]string{v7PathForMessage(vaultPath, path) + " (new V7 task target)"}, conflicts...))
		return tuskerError(
			errorAlreadyExists,
			fmt.Sprintf("Task ID %s collides before create; conflicting paths: %s; next safe candidate: %s", id, strings.Join(conflictingPaths, ", "), nextSafe),
			withPath(path),
			withHint(fmt.Sprintf("rerun with `--id %s` or repair the existing mixed-layout duplicate before creating this task", nextSafe)),
			withContext(map[string]any{"id": id, "conflicting_paths": conflictingPaths, "next_safe_id": nextSafe}),
		)
	}
	defaultStatus := "backlog"
	if args.Bool("ready") {
		defaultStatus = "ready"
	}
	status := strings.ToLower(fallback(args.String("status"), defaultStatus))
	if _, ok := v7TaskStatuses[status]; !ok {
		return tuskerError(errorInvalidField, "invalid V7 task status: "+status)
	}
	defaultReadiness := "held"
	if status == "ready" || status == "rework" || args.Bool("ready") {
		defaultReadiness = "ready"
	}
	readiness := strings.ToLower(fallback(args.String("readiness"), defaultReadiness))
	if _, ok := v7Readiness[readiness]; !ok {
		return tuskerError(errorInvalidField, "invalid V7 readiness: "+readiness)
	}
	risk := strings.ToLower(fallback(args.String("risk"), "medium"))
	if _, ok := risks[risk]; !ok {
		return tuskerError(errorInvalidField, "invalid risk: "+risk)
	}
	priority := strings.ToLower(fallback(args.String("priority"), "p2"))
	if _, ok := priorities[priority]; !ok {
		return tuskerError(errorInvalidField, "invalid priority: "+priority)
	}
	size := strings.ToLower(fallback(args.String("size"), "m"))
	if _, ok := sizes[size]; !ok {
		return tuskerError(errorInvalidField, "invalid size: "+size)
	}
	requestedEvidenceRequired := splitCSV(args.String("evidence-required"))
	defaultProofMode := defaultV7ProofMode(risk)
	if args.String("proof-mode") == "" && len(requestedEvidenceRequired) > 0 && defaultProofMode == "inline" {
		defaultProofMode = "card"
	}
	proofMode := strings.ToLower(fallback(args.String("proof-mode"), defaultProofMode))
	if _, ok := v7ProofModes[proofMode]; !ok {
		return tuskerError(errorInvalidField, "invalid proof_mode: "+proofMode)
	}
	proofRequired := splitCSV(firstNonEmpty(args.String("proof-required"), args.String("required")))
	if len(proofRequired) == 0 {
		proofRequired = defaultV7ProofRequired(proofMode)
	}
	proofRequiredOwner := v7DefaultProofRequiredOwners(proofRequired)
	for required, owner := range parseV7ProofRequiredOwnerArg(firstNonEmpty(args.String("proof-required-owner"), args.String("proof-owner"))) {
		proofRequiredOwner[required] = owner
	}
	evidenceBudget := defaultV7EvidenceBudget(proofMode)
	if budget := strings.TrimSpace(args.String("evidence-budget")); budget != "" {
		evidenceBudget = atoiSafe(budget)
		if evidenceBudget < 0 {
			return tuskerError(errorInvalidArg, "--evidence-budget must be >= 0")
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	specRefs := splitCSV(firstNonEmpty(args.String("spec-refs"), args.String("spec_refs")))
	data := map[string]any{
		"schema":                "tusker.task/v7",
		"kind":                  "task",
		"id":                    id,
		"project":               v7ProjectID(vaultPath),
		"title":                 title,
		"epic":                  epic,
		"status":                status,
		"readiness":             readiness,
		"priority":              priority,
		"risk":                  risk,
		"size":                  size,
		"proof_mode":            proofMode,
		"proof_status":          "pending",
		"proof_required":        proofRequired,
		"evidence_budget":       evidenceBudget,
		"raw_artifacts_allowed": args.Bool("raw-artifacts-allowed"),
		"next_owner":            fallback(args.String("next-owner"), "agent"),
		"next_source":           fallback(args.String("next-source"), "task"),
		"next_ref":              fallback(args.String("next-ref"), id),
		"next_action":           fallback(args.String("next-action"), "Execute the task contract and satisfy proof mode."),
		"domains":               splitCSV(args.String("domains")),
		"gates":                 splitCSV(args.String("gates")),
		"dependencies":          splitCSV(args.String("dependencies")),
		"evidence_required":     requestedEvidenceRequired,
		"created_at":            now,
		"created_by":            fallback(args.String("by"), "agent:"+defaultActorName()),
		"updated_at":            now,
		"updated_by":            fallback(args.String("by"), "agent:"+defaultActorName()),
	}
	if workKind := strings.ToLower(strings.TrimSpace(args.String("work-kind"))); workKind != "" {
		if workKind != "implementation" && workKind != "integrator" {
			return tuskerError(errorInvalidField, "work_kind must be implementation or integrator")
		}
		data["work_kind"] = workKind
	}
	if complexity := strings.ToLower(strings.TrimSpace(args.String("complexity"))); complexity != "" {
		if complexity != "routine" && complexity != "standard" && complexity != "complex" && complexity != "frontier" {
			return tuskerError(errorInvalidArg, "complexity must be routine, standard, complex, or frontier")
		}
		data["complexity"] = complexity
	}
	if len(specRefs) > 0 {
		data["spec_refs"] = specRefs
	}
	if len(proofRequiredOwner) > 0 {
		data["proof_required_owner"] = proofRequiredOwner
	}
	if reason := strings.TrimSpace(args.String("raw-artifacts-reason")); reason != "" {
		data["raw_artifacts_reason"] = reason
	}
	body := v7TaskBody(id, title)
	if status == "ready" && !args.Bool("force-ready") {
		synthetic := Note{Data: data, Body: body}
		if reasons := v7TaskDispatchBlockers(vaultPath, synthetic); len(reasons) > 0 {
			return tuskerError(
				errorInvalidArg,
				"ready V7 task is not dispatchable: "+strings.Join(reasons, "; "),
				withHint("create it as backlog/held, or pass --force-ready only after replacing placeholder acceptance and verification"),
				withContext(map[string]any{"id": id, "dispatch_blockers": reasons}),
			)
		}
	}
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err != nil {
		return err
	}
	if err := writeText(path, content); err != nil {
		return err
	}
	if !args.Bool("quiet") {
		fmt.Printf("Created V7 task %s at %s\n", id, path)
	}
	return emitV7Event(vaultPath, id, "task", "created", fallback(args.String("by"), "agent:"+defaultActorName()), map[string]any{"path": filepath.ToSlash(filepath.Join("work", "tasks", id+".md"))})
}

func v7DefaultProofRequiredOwners(required []string) map[string]string {
	owners := map[string]string{}
	for _, item := range required {
		normalized := strings.ToLower(strings.TrimSpace(item))
		normalized = strings.ReplaceAll(normalized, "-", "_")
		switch normalized {
		case "human_signoff", "manual_smoke", "physical_smoke", "release_smoke", "security_review", "privacy_review", "accessibility_review":
			owners[normalized] = "human:" + defaultActorName()
		}
	}
	return owners
}

func parseV7ProofRequiredOwnerArg(value string) map[string]string {
	owners := map[string]string{}
	for _, item := range splitCSV(value) {
		key, owner, ok := strings.Cut(item, "=")
		if !ok {
			key, owner, ok = strings.Cut(item, ":")
		}
		key = strings.ToLower(strings.TrimSpace(key))
		key = strings.ReplaceAll(key, "-", "_")
		owner = strings.TrimSpace(owner)
		if key == "" || owner == "" {
			continue
		}
		owners[key] = owner
	}
	return owners
}

func v7GateWhyAgentCannotArg(args Args) string {
	return strings.TrimSpace(firstNonEmpty(
		args.String("why-agent-cannot"),
		args.String("why-agent-cannot-do-this"),
		args.String("why_agent_cannot"),
		args.String("agent-boundary"),
	))
}

func v7GateSuggestionArg(args Args) string {
	return strings.TrimSpace(firstNonEmpty(
		args.String("suggestion"),
		args.String("recommendation"),
		args.String("suggested-resolution"),
		args.String("suggested_resolution"),
	))
}

func validateV7GateCreationPolicy(gateKind, owner string, blocking bool, action, verification, whyAgentCannot, suggestion string) error {
	if owner == "" {
		return tuskerError(errorMissingArg, "Missing required --owner <owner>")
	}
	if action == "" {
		return tuskerError(errorMissingArg, "Missing required --action <needed action>")
	}
	if verification == "" {
		return tuskerError(errorMissingArg, "Missing required --verification <proof>")
	}
	if v7GateTextIsPlaceholder(action) {
		return tuskerError(errorInvalidArg, "gate action is placeholder text", withHint("state the exact owner action that unblocks the task"))
	}
	if v7GateTextIsPlaceholder(verification) {
		return tuskerError(errorInvalidArg, "gate verification is placeholder text", withHint("state the concrete command, artifact, or owner decision that proves the gate is satisfied"))
	}
	if !blocking || !v7GateOwnerNeedsAgentBoundary(owner) {
		return nil
	}
	if whyAgentCannot == "" {
		return tuskerError(errorMissingArg, "human/external blocking gate requires --why-agent-cannot", withHint("explain the capability boundary, not just that work is blocked"))
	}
	if v7HumanGateOwnsAgentCapableWork(gateKind, owner, action, verification, whyAgentCannot, suggestion) {
		return tuskerError(errorInvalidArg, "human gate appears to own agent-capable review work", withHint("use an independent reviewer/subagent for code review, diffs, test inspection, or implementation judgment; use a decision gate only for a human product/spec choice and include --suggestion"))
	}
	if gateKind == "decision" && suggestion == "" {
		return tuskerError(errorMissingArg, "human/external decision gate requires --suggestion", withHint("include the agent's recommended choice or repair path"))
	}
	return nil
}

func v7GateDefaultTitle(action string, blocks []string) string {
	title := strings.TrimSpace(strings.TrimSuffix(action, "."))
	if title == "" {
		return "Gate for " + strings.Join(blocks, ", ")
	}
	if len(title) > 80 {
		title = strings.TrimSpace(title[:80])
	}
	return title
}

func newV7Gate(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	blocks := splitCSV(firstNonEmpty(args.String("blocks"), args.String("block"), args.String("_pos0")))
	if len(blocks) == 0 {
		return tuskerError(errorMissingArg, "Missing required --blocks <task-id>")
	}
	epic := v7EpicFromTaskID(blocks[0])
	if epic == "" {
		return tuskerError(errorInvalidArg, "first blocked object must look like ABC-T-0001")
	}
	id := strings.ToUpper(args.String("id"))
	if id == "" {
		id = fmt.Sprintf("%s-G-%s", epic, padNumber(nextV7Sequence(vaultPath, epic, "gate")))
	}
	if !v7GateIDPattern.MatchString(id) {
		return tuskerError(errorInvalidArg, "invalid gate id: "+id)
	}
	gateKindArg := firstNonEmpty(args.String("kind"), args.String("gate-kind"))
	if gateKindArg == "" {
		return tuskerError(errorMissingArg, "Missing required --kind <gate-kind>")
	}
	gateKind := strings.ToLower(gateKindArg)
	if _, ok := v7GateKinds[gateKind]; !ok {
		return tuskerError(errorInvalidField, "invalid gate kind: "+gateKind)
	}
	path := filepath.Join(vaultPath, "work", "gates", id+".md")
	if fileExists(path) {
		return tuskerError(errorAlreadyExists, "Gate already exists: "+id, withPath(path))
	}
	now := time.Now().UTC().Format(time.RFC3339)
	owner := strings.TrimSpace(args.String("owner"))
	action := strings.TrimSpace(args.String("action"))
	verification := strings.TrimSpace(args.String("verification"))
	whyAgentCannot := v7GateWhyAgentCannotArg(args)
	suggestion := v7GateSuggestionArg(args)
	blocking := !args.Bool("non-blocking")
	if err := validateV7GateCreationPolicy(gateKind, owner, blocking, action, verification, whyAgentCannot, suggestion); err != nil {
		return err
	}
	title := fallback(args.String("title"), v7GateDefaultTitle(action, blocks))
	data := map[string]any{
		"schema":       "tusker.gate/v1",
		"kind":         "gate",
		"id":           id,
		"project":      v7ProjectID(vaultPath),
		"title":        title,
		"gate_kind":    gateKind,
		"status":       "open",
		"owner":        owner,
		"priority":     strings.ToLower(fallback(args.String("priority"), "p2")),
		"blocking":     blocking,
		"blocks":       blocks,
		"covers":       normalizeV7Covers(splitCSV(args.String("covers"))),
		"action":       action,
		"verification": verification,
		"created_at":   now,
		"created_by":   fallback(args.String("by"), "agent:"+defaultActorName()),
		"updated_at":   now,
		"updated_by":   fallback(args.String("by"), "agent:"+defaultActorName()),
	}
	if whyAgentCannot != "" {
		data["why_agent_cannot"] = whyAgentCannot
	}
	if suggestion != "" {
		data["suggestion"] = suggestion
	}
	body := v7GateBody(id, title, action, verification, blocks, gateKind, whyAgentCannot, suggestion)
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["gate"])
	if err != nil {
		return err
	}
	materialLock, err := acquireV7MaterialEpochLock(vaultPath)
	if err != nil {
		return err
	}
	// Publishing the gate record is the material epoch change. Once visible,
	// arm fingerprints it even if the derived task projection follows later.
	if fileExists(path) {
		_ = materialLock.Close()
		return tuskerError(errorAlreadyExists, "Gate already exists: "+id, withPath(path))
	}
	if err := writeText(path, content); err != nil {
		_ = materialLock.Close()
		return err
	}
	if err := materialLock.Close(); err != nil {
		return err
	}
	if !args.Bool("quiet") {
		fmt.Printf("Created gate %s at %s\n", id, path)
	}
	actor := fallback(args.String("by"), "agent:"+defaultActorName())
	if err := emitV7Event(vaultPath, id, "gate", "created", actor, map[string]any{"blocks": blocks}); err != nil {
		return err
	}
	if err := applyV7OpenGateProjection(vaultPath, data, actor, "gate:"+id); err != nil {
		return err
	}
	if _, err = reconcileV7ControlProjections(vaultPath, blocks, actor, "gate:"+id); err != nil {
		return err
	}
	for _, taskID := range blocks {
		if err := updateV7TaskProofStatus(vaultPath, taskID, actor); err != nil {
			return err
		}
	}
	return nil
}

func applyV7OpenGateProjection(vaultPath string, gateData map[string]any, actor, source string) error {
	if stringField(gateData, "status") != "open" || !boolField(gateData, "blocking") {
		return nil
	}
	gateID := stringField(gateData, "id")
	for _, taskID := range normalizeList(gateData["blocks"]) {
		task, err := resolveV7Note(vaultPath, taskID, "task")
		if err != nil {
			continue
		}
		data, body, err := parseFrontmatterMustRead(task.AbsolutePath)
		if err != nil {
			return err
		}
		if status := stringField(data, "status"); status == "done" || status == "cancelled" || status == "superseded" {
			continue
		}
		next := map[string]any{
			"readiness":   "blocked_by_gate",
			"next_owner":  stringField(gateData, "owner"),
			"next_source": "gate",
			"next_ref":    gateID,
			"next_action": stringField(gateData, "action"),
		}
		changed := false
		changes := map[string]any{}
		for key, value := range next {
			if toString(data[key]) != toString(value) {
				changes[key] = map[string]any{"from": data[key], "to": value}
				data[key] = value
				changed = true
			}
		}
		if !changed {
			continue
		}
		baseRev := stringField(data, "state_rev")
		data["updated_at"] = time.Now().UTC().Format(time.RFC3339)
		data["updated_by"] = actor
		if _, err := saveV7DocumentCAS(task.AbsolutePath, data, body, v7FrontmatterOrder["task"], baseRev); err != nil {
			return err
		}
		if err := emitV7Event(vaultPath, taskID, "task", "updated", actor, map[string]any{"changes": changes, "source": source}); err != nil {
			return err
		}
	}
	return nil
}

func newV7Decision(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	epic, err := requireArg(args, "epic")
	if err != nil {
		return err
	}
	epic = strings.ToUpper(epic)
	title, err := requireArg(args, "title")
	if err != nil {
		return err
	}
	id := strings.ToUpper(args.String("id"))
	if id == "" {
		id = fmt.Sprintf("%s-D-%s", epic, padNumber(nextV7Sequence(vaultPath, epic, "decision")))
	}
	if !v7DecisionIDPattern.MatchString(id) {
		return tuskerError(errorInvalidArg, "invalid decision id: "+id)
	}
	path := filepath.Join(vaultPath, "work", "decisions", id+".md")
	if fileExists(path) {
		return tuskerError(errorAlreadyExists, "Decision already exists: "+id, withPath(path))
	}
	now := time.Now().UTC().Format(time.RFC3339)
	status := strings.ToLower(fallback(args.String("status"), "proposed"))
	data := map[string]any{
		"schema":     "tusker.decision/v1",
		"kind":       "decision",
		"id":         id,
		"project":    v7ProjectID(vaultPath),
		"epic":       epic,
		"title":      title,
		"status":     status,
		"supersedes": splitCSV(args.String("supersedes")),
		"created_at": now,
		"created_by": fallback(args.String("by"), "agent:"+defaultActorName()),
		"updated_at": now,
		"updated_by": fallback(args.String("by"), "agent:"+defaultActorName()),
	}
	if status == "accepted" {
		data["decided_by"] = fallback(args.String("by"), "human:"+defaultActorName())
		data["decided_at"] = now
	}
	body := fmt.Sprintf("# %s · %s\n\n## Decision\n\n%s\n\n## Context\n\nTBD.\n\n## Work streams\n\n- None yet.\n\n## Consequences\n\nTBD.\n", id, title, fallback(args.String("decision"), "TBD."))
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["decision"])
	if err != nil {
		return err
	}
	if err := writeText(path, content); err != nil {
		return err
	}
	if !args.Bool("quiet") {
		fmt.Printf("Created decision %s at %s\n", id, path)
	}
	return emitV7Event(vaultPath, id, "decision", "created", fallback(args.String("by"), "agent:"+defaultActorName()), nil)

}

func v7ProposalTargetKind(target, action string) string {
	if action == "create_gate" && v7TaskIDPattern.MatchString(target) {
		return "task"
	}
	if strings.HasPrefix(action, "create_") {
		return "epic"
	}
	switch {
	case v7TaskIDPattern.MatchString(target):
		return "task"
	case v7GateIDPattern.MatchString(target):
		return "gate"
	case v7DecisionIDPattern.MatchString(target):
		return "decision"
	case epicAcronymPattern.MatchString(target):
		return "epic"
	default:
		return "object"
	}
}

func v7EpicFromProposalTarget(target string) string {
	for _, pattern := range []*regexp.Regexp{v7TaskIDPattern, v7GateIDPattern, v7DecisionIDPattern, v7ProposalIDPattern} {
		if match := pattern.FindStringSubmatch(target); match != nil {
			return match[1]
		}
	}
	if epicAcronymPattern.MatchString(target) {
		return target
	}
	return ""
}

func redactV7Cmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	objectID := strings.ToUpper(firstNonEmpty(args.String("id"), args.String("_pos0")))
	if objectID == "" {
		return tuskerError(errorMissingArg, "redact requires an object id")
	}
	reason := args.String("reason")
	if reason == "" {
		return tuskerError(errorMissingArg, "redact requires --reason")
	}
	objectKind := strings.ToLower(firstNonEmpty(args.String("object-kind"), args.String("kind"), inferV7ObjectKind(objectID)))
	if _, ok := v7EventObjectKinds[objectKind]; !ok {
		return tuskerError(errorInvalidField, "invalid redaction object kind: "+objectKind)
	}
	actor := fallback(args.String("by"), "human:"+defaultActorName())
	payload := map[string]any{"reason": reason}
	if target := args.String("target"); target != "" {
		payload["target"] = target
	}
	if err := emitV7Event(vaultPath, objectID, objectKind, "redaction", actor, payload); err != nil {
		return err
	}
	replacement := firstNonEmpty(args.String("replacement"), args.String("replacement-note"), args.String("summary"))
	if replacement != "" {
		if err := emitV7Event(vaultPath, objectID, objectKind, "redacted_replacement", actor, map[string]any{"reason": reason, "replacement": replacement}); err != nil {
			return err
		}
	}
	if !args.Bool("quiet") {
		fmt.Printf("Recorded redaction for %s\n", objectID)
	}
	return nil
}

func validateV7ClosePolicyConfig(configPath, risk string, policy v7ClosePolicy) error {
	if policy.RequiredAcceptor == "human" {
		return tuskerError(errorConfigInvalid, "legacy close_policy."+risk+".required_acceptor: human treats risk as human authority", withPath(configPath), withHint("run `tusker migrate close-policy --write`; preserve genuine authority and subjective acceptance as explicit gates"))
	}
	if policy.RequiredAcceptor != "reviewer_agent" {
		return tuskerError(errorConfigInvalid, "invalid close_policy."+risk+".required_acceptor: "+policy.RequiredAcceptor, withPath(configPath))
	}
	for _, kind := range policy.RequiredEvidence {
		if _, ok := v7EvidenceKinds[kind]; !ok {
			return tuskerError(errorConfigInvalid, "invalid close_policy."+risk+".required_evidence: "+kind, withPath(configPath))
		}
	}
	for _, kind := range policy.RequiredGates {
		if _, ok := v7GateKinds[kind]; !ok {
			return tuskerError(errorConfigInvalid, "invalid close_policy."+risk+".required_gates: "+kind, withPath(configPath))
		}
	}
	return nil
}

func attemptV7Cmd(args Args) error {
	switch strings.ToLower(args.String("_pos0")) {
	case "start":
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos1"))
		if vaultPath, err := resolveVaultPath(args, false); err == nil && !fileExists(workflowPath(vaultPath)) {
			return attemptV7StartCmd(args)
		}
		args["owner"] = firstNonEmpty(args.String("owner"), args.String("by"), "agent:"+defaultActorName())
		args["source"] = firstNonEmpty(args.String("source"), "tusker_cli")
		return workSessionStartCmd(args)
	case "handoff":
		args["id"] = firstNonEmpty(args.String("id"), args.String("_pos1"))
		return attemptV7HandoffCmd(args)
	default:
		return tuskerError(errorMissingArg, "Usage: tusker attempt start|handoff <task-id> ...")
	}
}

func attemptV7StartCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	taskID, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	if _, err := resolveV7Note(vaultPath, taskID, "task"); err != nil {
		return err
	}
	attemptID := strings.ToUpper(args.String("attempt-id"))
	if attemptID == "" {
		attemptID = fmt.Sprintf("%s-A-%s", taskID, padNumber(nextV7AttemptSequence(vaultPath, taskID)))
	}
	now := time.Now().UTC().Format(time.RFC3339)
	data := map[string]any{
		"schema":             "tusker.attempt/v1",
		"kind":               "attempt",
		"id":                 attemptID,
		"runtime_attempt_id": strings.TrimSpace(args.String("runtime-attempt-id")),
		"lane":               strings.TrimSpace(args.String("lane")),
		"project":            v7ProjectID(vaultPath),
		"task":               taskID,
		"runner":             fallback(args.String("runner"), "codex"),
		"agent_model":        args.String("agent-model"),
		"workspace_kind":     fallback(args.String("workspace-kind"), "same_checkout"),
		"workspace_path":     args.String("workspace-path"),
		"branch":             fallback(args.String("branch"), currentGitBranch()),
		"status":             "started",
		"started_at":         now,
		"pr_url":             args.String("pr-url"),
		"evidence":           splitCSV(args.String("evidence")),
	}
	body := fmt.Sprintf("# %s · Agent attempt summary\n\n## Outcome\n\nStarted.\n\n## Changed areas\n\nPending.\n\n## Verification\n\nPending.\n\n## Handoff\n\nPending.\n\n## Follow-ups proposed\n\nNone.\n", attemptID)
	data["state_rev"] = v7StateRev(data, body)
	path := filepath.Join(vaultPath, "attempts", taskID, attemptID+".md")
	content, err := serializeDocument(data, body, v7FrontmatterOrder["attempt"])
	if err != nil {
		return err
	}
	if err := writeText(path, content); err != nil {
		return err
	}
	if !args.Bool("quiet") {
		fmt.Printf("Started attempt %s at %s\n", attemptID, path)
	}
	return emitV7Event(vaultPath, taskID, "task", "attempt_started", "agent:"+defaultActorName(), map[string]any{"attempt": attemptID})
}

func attemptV7HandoffCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	taskID, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	if err := requireAgentWorkSession(vaultPath, taskID, fallback(args.String("by"), "agent:"+defaultActorName()), args); err != nil {
		return err
	}
	attemptID := args.String("attempt-id")
	if attemptID == "" {
		attemptID, err = latestV7AttemptID(vaultPath, taskID)
		if err != nil {
			return err
		}
	}
	note, err := resolveV7Note(vaultPath, attemptID, "attempt")
	if err != nil {
		return err
	}
	data, body, err := parseFrontmatterMustRead(note.AbsolutePath)
	if err != nil {
		return err
	}
	baseRev := stringField(data, "state_rev")
	summary, err := v7HandoffSummary(args)
	if err != nil {
		return err
	}
	if !args.Bool("no-review-proposal") {
		if err := ensureV7FinishProofReady(vaultPath, taskID); err != nil {
			return err
		}
		if err := updateV7TaskProofStatus(vaultPath, taskID, fallback(args.String("by"), "agent:"+defaultActorName())); err != nil {
			return err
		}
	}
	if strings.TrimSpace(summary) != "" {
		body = replaceSection(body, "## Handoff", strings.TrimSpace(summary))
	}
	now := time.Now().UTC().Format(time.RFC3339)
	data["status"] = "handoff"
	data["ended_at"] = now
	data["evidence"] = firstNonEmptyList(normalizeList(data["evidence"]), splitCSV(args.String("evidence")))
	if _, err := saveV7DocumentCAS(note.AbsolutePath, data, body, v7FrontmatterOrder["attempt"], baseRev); err != nil {
		return err
	}
	if !args.Bool("quiet") {
		fmt.Printf("Attempt %s moved to handoff\n", attemptID)
	}
	if err := emitV7Event(vaultPath, taskID, "task", "attempt_handoff", "agent:"+defaultActorName(), map[string]any{"attempt": attemptID}); err != nil {
		return err
	}
	if args.Bool("no-review-proposal") {
		return nil
	}
	return requestV7ReviewAfterHandoff(vaultPath, taskID, args)
}

func finishV7Cmd(args Args) error {
	args["id"] = firstNonEmpty(args.String("id"), args.String("_pos0"))
	args["finish"] = "true"
	if attemptID := firstNonEmpty(args.String("attempt"), args.String("attempt-id")); attemptID != "" {
		args["attempt-id"] = attemptID
	}
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	taskID, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	if err := requireAgentWorkSession(vaultPath, taskID, fallback(args.String("by"), "agent:"+defaultActorName()), args); err != nil {
		return err
	}
	if verify := strings.TrimSpace(args.String("verify")); verify != "" {
		rows, err := parseV7FinishVerificationRows(verify)
		if err != nil {
			return err
		}
		actor := fallback(args.String("by"), "agent:"+defaultActorName())
		for _, row := range rows {
			if _, err := upsertV7Verification(vaultPath, taskID, row, actor); err != nil {
				return err
			}
		}
	}
	if err := ensureV7FinishProofReady(vaultPath, taskID); err != nil {
		return err
	}
	if err := updateV7TaskProofStatus(vaultPath, taskID, fallback(args.String("by"), "agent:"+defaultActorName())); err != nil {
		return err
	}
	return attemptV7HandoffCmd(args)
}

// requireAgentWorkSession prevents model-facing finish/review mutations from
// becoming a second active-work protocol. Humans retain their existing local
// control path; a human may also state --break-glass explicitly for an
// attributable emergency override.
func requireAgentWorkSession(vaultPath, taskID, actor string, args Args) error {
	actor = strings.TrimSpace(actor)
	if args.Bool("break-glass") {
		if !strings.HasPrefix(actor, "human:") || strings.TrimSpace(args.String("reason")) == "" {
			return tuskerError(errorInvalidArg, "break-glass requires --by human:<name> and --reason")
		}
		return nil
	}
	if !strings.HasPrefix(actor, "agent:") {
		return nil
	}
	// V7 predates the automation workflow/runtime store. A vault with no
	// WORKFLOW.md, or a project configured for local-only mutation, has no daemon
	// ownership surface to compare against. Keep that attributable compatibility
	// path without letting an agent self-authorize through a command-line flag.
	if !fileExists(workflowPath(vaultPath)) || v7SingleUserLocalMutationMode(vaultPath) {
		return nil
	}
	note, err := resolveV7Note(vaultPath, taskID, "task")
	if err != nil {
		return err
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	run, err := store.FindRun(trackerRecordID(note))
	if err != nil {
		return err
	}
	if args.Bool("normalized-work-submit") && run != nil && LeaseState(run.LeaseState) == LeaseStateReleased && run.AttemptOutcome == string(AttemptOutcomeSucceeded) && run.LeaseGeneration == intArg(args, "lease-generation") {
		auth, authErr := store.LatestRunAuthorization(run.ProjectID, run.RecordID)
		if authErr == nil && auth != nil && auth.Actor == actor && auth.LeaseGeneration == run.LeaseGeneration {
			return nil
		}
	}
	if dispatchedWorkerWorkSessionMatches(vaultPath, taskID, note, run, store) {
		return nil
	}
	if run == nil || run.LeaseOwner != actor ||
		(LeaseState(run.LeaseState) != LeaseStateClaimed && LeaseState(run.LeaseState) != LeaseStateRunning) ||
		runFreshness(run, time.Now().UTC()) != "fresh" {
		return tuskerError("WORK_SESSION_REQUIRED", "agent mutation requires a live work session owned by "+actor, withHint("run `tusker work start "+taskID+" --by "+actor+"` or use explicit human --break-glass"))
	}
	if data, _, readErr := parseFrontmatterMustRead(note.AbsolutePath); readErr != nil {
		return readErr
	} else if run.WorkRevision != intField(data, "work_revision") {
		return tuskerError("WORK_SESSION_STALE", "agent mutation refused: work session revision is stale")
	}
	return nil
}

func dispatchedWorkerWorkSessionMatches(vaultPath, taskID string, task Note, run *RunStatus, store *RuntimeStore) bool {
	attemptID := strings.TrimSpace(os.Getenv("TUSKER_ATTEMPT_ID"))
	projectID := strings.TrimSpace(os.Getenv("TUSKER_PROJECT_ID"))
	recordID := strings.TrimSpace(os.Getenv("TUSKER_RECORD_ID"))
	workspace := strings.TrimSpace(os.Getenv("TUSKER_WORKSPACE"))
	repoRoot := strings.TrimSpace(os.Getenv("TUSKER_REPO_ROOT"))
	canonicalVault := strings.TrimSpace(os.Getenv("TUSKER_VAULT"))
	statusPath := strings.TrimSpace(os.Getenv("TUSKER_STATUS_PATH"))
	generation, generationErr := strconv.Atoi(strings.TrimSpace(os.Getenv("TUSKER_LEASE_GENERATION")))
	workRevision, revisionErr := strconv.Atoi(strings.TrimSpace(os.Getenv("TUSKER_WORK_REVISION")))
	if attemptID == "" || projectID == "" || recordID == "" || workspace == "" || repoRoot == "" || canonicalVault == "" || statusPath == "" ||
		generationErr != nil || revisionErr != nil || generation <= 0 ||
		!strings.EqualFold(recordID, trackerRecordID(task)) ||
		intField(task.Data, "work_revision") != workRevision ||
		!workspacePathsCompatible(v7RepoRoot(vaultPath), repoRoot) ||
		!workspacePathsCompatible(vaultPath, canonicalVault) ||
		fileExists(statusPath) {
		return false
	}
	attemptVault := runnerWorktreeVaultPath(workspace, vaultPath)
	if attemptVault == "" {
		return false
	}
	idx, err := loadV7Index(attemptVault)
	if err != nil {
		return false
	}
	var bound []Note
	for _, attempt := range idx.Attempts[taskID] {
		if strings.EqualFold(stringField(attempt.Data, "runtime_attempt_id"), attemptID) {
			bound = append(bound, attempt)
		}
	}
	if len(bound) != 1 {
		return false
	}
	attempt := bound[0]
	if stringField(attempt.Data, "status") != "started" ||
		!strings.EqualFold(stringField(attempt.Data, "task"), taskID) ||
		!workspacePathsCompatible(stringField(attempt.Data, "workspace_path"), workspace) ||
		!strings.EqualFold(stringField(attempt.Data, "lane"), os.Getenv("TUSKER_RUN_LANE")) {
		return false
	}
	// Sandboxed runners intentionally do not receive TUSKER_STATE_ROOT. Their
	// exact daemon-created attempt record plus the still-unfinished wrapper
	// status path is the narrow mutation capability; do not expose the runtime
	// database merely so the child can prove the lease it already owns.
	if strings.TrimSpace(os.Getenv("TUSKER_STATE_ROOT")) == "" {
		return true
	}
	if run == nil || store == nil ||
		projectID != run.ProjectID ||
		!strings.EqualFold(run.LeaseOwner, attemptID) ||
		!strings.EqualFold(run.ActiveAttemptID, attemptID) ||
		run.LeaseGeneration != generation ||
		run.WorkRevision != workRevision ||
		(LeaseState(run.LeaseState) != LeaseStateClaimed && LeaseState(run.LeaseState) != LeaseStateRunning) ||
		runFreshness(run, time.Now().UTC()) != "fresh" ||
		!workspacePathsCompatible(run.WorkspacePath, workspace) {
		return false
	}
	auth, err := store.LatestRunAuthorization(run.ProjectID, run.RecordID)
	return err == nil && auth != nil && auth.LeaseGeneration == generation &&
		(auth.Source == "daemon_auto" || auth.Source == "human_run_directive") &&
		strings.EqualFold(stringField(attempt.Data, "task"), taskID) &&
		workspacePathsCompatible(stringField(attempt.Data, "workspace_path"), workspace)
}

func ensureV7FinishProofReady(vaultPath, taskID string) error {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return err
	}
	task, ok := idx.Tasks[taskID]
	if !ok {
		return tuskerError(errorNotFound, "V7 task not found: "+taskID)
	}
	status := stringField(task.Data, "status")
	if status == "review" || status == "done" || status == "cancelled" || status == "superseded" {
		return nil
	}
	projected := v7ProjectedTaskState(vaultPath, task, idx)
	switch stringField(projected, "readiness") {
	case "blocked_by_gate", "blocked_by_dependency", "waiting_on_human", "waiting_on_ci", "held":
		return nil
	}
	report := computeV7ProofReport(vaultPath, task, idx)
	if len(v7PacketStubAcceptanceItems(task.Body)) > 0 && len(v7AcceptanceWaivers(task.Data)) == 0 {
		return tuskerError(errorEvidenceGate, taskID+": finish blocked by placeholder acceptance", withHint("replace stub acceptance with observable outcomes and proof mapping, or record an explicit waiver"))
	}
	missing := append([]string{}, report.Missing...)
	missing = append(missing, report.ModeMissing...)
	if len(missing) > 0 {
		if len(report.OpenGates) > 0 {
			return nil
		}
		return tuskerError(errorEvidenceGate, taskID+": finish proof incomplete: "+strings.Join(missing, ", "), withHint("remaining proof gaps: "+v7ProofRemainingGapSummary(report)+"; use `tusker verify add` for inline proof, add required evidence, or create a blocking gate for human/device/env/CI proof"))
	}
	return nil
}

func requestV7ReviewAfterHandoff(vaultPath, taskID string, args Args) error {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return err
	}
	task, ok := idx.Tasks[taskID]
	if !ok {
		return tuskerError(errorNotFound, "V7 task not found: "+taskID)
	}
	status := stringField(task.Data, "status")
	if status == "review" || status == "done" || status == "cancelled" || status == "superseded" {
		if status == "review" || status == "done" {
			_, _, err := ensureV7ImplicitSingletonDeliveryUnit(vaultPath, taskID, args)
			return err
		}
		return nil
	}
	projected := v7ProjectedTaskState(vaultPath, task, idx)
	switch stringField(projected, "readiness") {
	case "blocked_by_gate", "blocked_by_dependency", "waiting_on_human", "waiting_on_ci", "held":
		if !args.Bool("quiet") {
			fmt.Printf("%s handoff recorded; review request skipped because task is %s via %s\n", taskID, stringField(projected, "readiness"), stringField(projected, "next_ref"))
		}
		return nil
	}
	report := computeV7ProofReport(vaultPath, task, idx)
	if len(v7PacketStubAcceptanceItems(task.Body)) > 0 && len(v7AcceptanceWaivers(task.Data)) == 0 {
		return tuskerError(errorEvidenceGate, taskID+": finish blocked by placeholder acceptance", withHint("replace stub acceptance with observable outcomes and proof mapping, or record an explicit waiver"))
	}
	missing := append([]string{}, report.Missing...)
	missing = append(missing, report.ModeMissing...)
	if len(missing) > 0 {
		return tuskerError(errorEvidenceGate, taskID+": finish proof incomplete: "+strings.Join(missing, ", "), withHint("remaining proof gaps: "+v7ProofRemainingGapSummary(report)+"; use `tusker verify add` for inline proof, add required evidence, or create a blocking gate"))
	}
	actor := fallback(fallback(args.String("actor"), args.String("by")), "agent:"+defaultActorName())
	reason := firstNonEmpty(args.String("reason"), "Ready for independent review after attempt handoff.")
	if args.Bool("propose") {
		return proposeV7ReviewStatusIfMissing(vaultPath, taskID, actor, reason, args)
	}
	statusArgs := Args{
		"vault":  vaultPath,
		"quiet":  "true",
		"id":     taskID,
		"status": "review",
		"by":     actor,
		"reason": reason,
	}
	if args.Bool("local") {
		statusArgs["local"] = "true"
	}
	if args.Bool("force") {
		statusArgs["force"] = "true"
	}
	if err := statusV7Cmd(statusArgs); err != nil {
		if isV7ProtectedStateMutationError(err) {
			return proposeV7ReviewStatusIfMissing(vaultPath, taskID, actor, reason, args)
		}
		return err
	}
	if !args.Bool("quiet") {
		fmt.Printf("%s moved to review\n", taskID)
	}
	// This is intentionally after review has been requested. The synthetic
	// delivery unit inherits completed execution provenance; it cannot make a
	// ready task dispatchable, arm a wave, or authorize a release.
	if _, _, err := ensureV7ImplicitSingletonDeliveryUnit(vaultPath, taskID, args); err != nil {
		return err
	}
	return emitV7Event(vaultPath, taskID, "task", "review_requested", actor, map[string]any{"reason": reason})
}

func proposeV7ReviewStatusIfMissing(vaultPath, taskID, actor, reason string, args Args) error {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return err
	}
	for _, proposal := range idx.Proposals {
		fields := v7ProposalFieldMap(proposal.Data["proposed_fields"])
		if stringField(proposal.Data, "status") == "proposed" &&
			stringField(proposal.Data, "action") == "status" &&
			stringField(proposal.Data, "target") == taskID &&
			strings.ToLower(toString(fields["status"])) == "review" {
			if !args.Bool("quiet") {
				fmt.Printf("%s already has review proposal %s\n", taskID, stringField(proposal.Data, "id"))
			}
			return nil
		}
	}
	proposalArgs := Args{
		"vault":  vaultPath,
		"quiet":  "true",
		"_pos0":  "status",
		"_pos1":  taskID,
		"status": "review",
		"by":     actor,
		"reason": reason,
	}
	if err := proposalV7Cmd(proposalArgs); err != nil {
		return err
	}
	if !args.Bool("quiet") {
		fmt.Printf("%s review proposal created\n", taskID)
	}
	return emitV7Event(vaultPath, taskID, "task", "review_requested", actor, map[string]any{"reason": reason, "mode": "proposal"})
}

func isV7ProtectedStateMutationError(err error) bool {
	te, ok := err.(*TuskerError)
	if !ok || te.Code != errorInvalidTransition {
		return false
	}
	return strings.Contains(te.Message, "protected Tusker state")
}

func v7HandoffSummary(args Args) (string, error) {
	if summaryFile := strings.TrimSpace(args.String("summary-file")); summaryFile != "" {
		return readText(summaryFile)
	}
	summary := args.String("summary")
	if strings.TrimSpace(summary) == "" {
		return "", nil
	}
	if info, err := os.Stat(summary); err == nil && !info.IsDir() {
		return readText(summary)
	}
	return summary, nil
}

func heartbeatV7Cmd(args Args) error {
	lease, err := updateExistingV7Lease(args, "active")
	if err != nil {
		return err
	}
	if !args.Bool("quiet") {
		fmt.Printf("Active lease for %s\n", lease.Task)
	}
	return err
}

func releaseV7Cmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	lease, err := updateExistingV7Lease(args, "released")
	if err != nil {
		return err
	}
	return emitV7Event(vaultPath, lease.Task, "task", "claim_released", lease.Owner, map[string]any{"lease": lease.ID})
}

func updateExistingV7Lease(args Args, status string) (v7LeaseRecord, error) {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return v7LeaseRecord{}, err
	}
	leaseID := firstNonEmpty(args.String("id"), args.String("_pos0"))
	if leaseID == "" {
		return v7LeaseRecord{}, tuskerError(errorMissingArg, "Missing task id")
	}
	store := v7FileRuntimeStore{VaultPath: vaultPath}
	lease, err := store.findActiveLease(context.Background(), leaseID)
	if err != nil {
		return v7LeaseRecord{}, err
	}
	updated, err := store.writeLease(context.Background(), lease.Task, lease.ID, lease.Owner, lease.Workspace, lease.Branch, status, 0)
	if err != nil {
		return v7LeaseRecord{}, err
	}
	return updated, nil
}

func writeV7Lease(args Args, status string) (v7LeaseRecord, error) {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return v7LeaseRecord{}, err
	}
	taskID := firstNonEmpty(args.String("id"), args.String("_pos0"))
	if taskID == "" {
		return v7LeaseRecord{}, tuskerError(errorMissingArg, "Missing task id")
	}
	store := v7FileRuntimeStore{VaultPath: vaultPath}
	lease, err := store.writeLease(context.Background(), taskID, args.String("lease-id"), args.String("owner"), args.String("workspace"), args.String("branch"), status, 0)
	if err != nil {
		return v7LeaseRecord{}, err
	}
	if !args.Bool("quiet") {
		fmt.Printf("%s lease for %s\n", capitalize(status), taskID)
	}
	return lease, nil
}

func reconcileV7Cmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	if err := ensureV7ControlMutation(vaultPath, args); err != nil {
		return err
	}
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return err
	}
	changed := 0
	revRepairs := 0
	waveChanges := 0
	taskWavePointers := 0
	staleLeases := 0
	revRepairs, err = reconcileV7ObjectStateRevs(vaultPath)
	if err != nil {
		return err
	}
	if revRepairs > 0 {
		idx, err = loadV7Index(vaultPath)
		if err != nil {
			return err
		}
	}
	waveChanges, taskWavePointers, err = reconcileV7WaveProjections(vaultPath, idx, "tusker:reconcile", "reconcile")
	if err != nil {
		return err
	}
	if waveChanges > 0 || taskWavePointers > 0 {
		idx, err = loadV7Index(vaultPath)
		if err != nil {
			return err
		}
	}
	for _, task := range idx.Tasks {
		next := v7ProjectedTaskState(vaultPath, task, idx)
		projectionChanges := map[string]any{}
		taskID := ""
		nextRev, updated, err := mutateV7DocumentLocked(task.AbsolutePath, v7FrontmatterOrder["task"], func(data map[string]any, body string) (map[string]any, string, bool, error) {
			taskID = stringField(data, "id")
			for key, value := range next {
				if toString(data[key]) != toString(value) {
					projectionChanges[key] = map[string]any{"from": data[key], "to": value}
					data[key] = value
				}
			}
			if len(projectionChanges) == 0 {
				return data, body, false, nil
			}
			data["updated_at"] = time.Now().UTC().Format(time.RFC3339)
			data["updated_by"] = "tusker:reconcile"
			return data, body, true, nil
		})
		if err != nil {
			return err
		}
		if !updated {
			continue
		}
		if err := emitV7Event(vaultPath, taskID, "task", "updated", "tusker:reconcile", map[string]any{"changes": projectionChanges, "source": "projection", "state_rev": nextRev}); err != nil {
			return err
		}
		changed++
	}
	if changed > 0 {
		idx, err = loadV7Index(vaultPath)
		if err != nil {
			return err
		}
	}
	epicBlocks, err := reconcileV7EpicManagedBlocks(vaultPath, idx)
	if err != nil {
		return err
	}
	staleLeases, err = reconcileV7Leases(vaultPath)
	if err != nil {
		return err
	}
	if err := buildV7Dashboards(vaultPath, idx); err != nil {
		return err
	}
	if err := refreshStreamBoardForVault(vaultPath); err != nil {
		return err
	}
	if !args.Bool("quiet") {
		openDoneGates := len(v7DoneTaskOpenGateViolations(idx))
		fmt.Printf("Reconciled %d V7 task projection%s, %d wave projection%s, %d task wave pointer%s, repaired %d stale object rev%s, %d epic managed block%s, %d stale lease%s, and detected %d done/open-gate violation%s.\n", changed, plural(changed), waveChanges, plural(waveChanges), taskWavePointers, plural(taskWavePointers), revRepairs, plural(revRepairs), epicBlocks, plural(epicBlocks), staleLeases, plural(staleLeases), openDoneGates, plural(openDoneGates))
	}
	return nil
}

func reconcileV7ControlProjections(vaultPath string, taskIDs []string, actor, source string) (int, error) {
	if len(taskIDs) == 0 {
		idx, err := loadV7Index(vaultPath)
		if err != nil {
			return 0, err
		}
		if err := buildV7Dashboards(vaultPath, idx); err != nil {
			return 0, err
		}
		return 0, nil
	}
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return 0, err
	}
	changed := 0
	waveChanges, taskWavePointers, err := reconcileV7WaveProjections(vaultPath, idx, actor, source)
	if err != nil {
		return changed, err
	}
	changed += taskWavePointers
	if waveChanges > 0 || taskWavePointers > 0 {
		idx, err = loadV7Index(vaultPath)
		if err != nil {
			return changed, err
		}
	}
	for _, taskID := range uniqueStrings(taskIDs) {
		task, ok := idx.Tasks[taskID]
		if !ok {
			continue
		}
		next := v7ProjectedTaskState(vaultPath, task, idx)
		data, body := cloneNoteData(task.Data), task.Body
		baseRev := stringField(data, "state_rev")
		updated := false
		projectionChanges := map[string]any{}
		for key, value := range next {
			if toString(data[key]) != toString(value) {
				projectionChanges[key] = map[string]any{"from": data[key], "to": value}
				data[key] = value
				updated = true
			}
		}
		if !updated {
			continue
		}
		data["updated_at"] = time.Now().UTC().Format(time.RFC3339)
		data["updated_by"] = actor
		if _, err := saveV7DocumentCAS(task.AbsolutePath, data, body, v7FrontmatterOrder["task"], baseRev); err != nil {
			return changed, err
		}
		if err := emitV7Event(vaultPath, stringField(data, "id"), "task", "updated", actor, map[string]any{"changes": projectionChanges, "source": source}); err != nil {
			return changed, err
		}
		changed++
	}
	idx, err = loadV7Index(vaultPath)
	if err != nil {
		return changed, err
	}
	if _, err := reconcileV7EpicManagedBlocks(vaultPath, idx); err != nil {
		return changed, err
	}
	idx, err = loadV7Index(vaultPath)
	if err != nil {
		return changed, err
	}
	if err := buildV7Dashboards(vaultPath, idx); err != nil {
		return changed, err
	}
	return changed, nil
}

func v7TaskIDsForGateControl(vaultPath, gateID string, blocks []string) ([]string, error) {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return nil, err
	}
	ids := append([]string{}, blocks...)
	for _, task := range idx.Tasks {
		taskID := stringField(task.Data, "id")
		if containsString(normalizeList(task.Data["gates"]), gateID) {
			ids = append(ids, taskID)
		}
	}
	return uniqueStrings(ids), nil
}

func v7TaskIDsForTaskControl(vaultPath, taskID string) ([]string, error) {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return nil, err
	}
	ids := []string{taskID}
	for _, task := range idx.Tasks {
		currentID := stringField(task.Data, "id")
		if currentID != taskID && v7TaskDependsOnID(task, taskID, idx) {
			ids = append(ids, currentID)
		}
	}
	return uniqueStrings(ids), nil
}

func reconcileV7EpicManagedBlocks(vaultPath string, idx v7Index) (int, error) {
	changed := 0
	for _, epic := range sortedV7Epics(idx) {
		_, updated, err := mutateV7DocumentLocked(epic.AbsolutePath, v7FrontmatterOrder["epic"], func(data map[string]any, body string) (map[string]any, string, bool, error) {
			epicID := stringField(data, "id")
			nextBody := replaceSection(body, "## Open gates", v7EpicOpenGatesBlock(idx, epicID))
			nextBody = replaceSection(nextBody, "## Active work", v7EpicActiveWorkBlock(idx, epicID))
			nextBody = replaceSection(nextBody, "## Recently completed", v7EpicRecentlyCompletedBlock(idx, epicID))
			if v7CanonicalBody(nextBody) == v7CanonicalBody(body) {
				return data, body, false, nil
			}
			data["updated_at"] = time.Now().UTC().Format(time.RFC3339)
			return data, nextBody, true, nil
		})
		if err != nil {
			return changed, err
		}
		if updated {
			changed++
		}
	}
	return changed, nil
}

func reconcileV7ObjectStateRevs(vaultPath string) (int, error) {
	notes, err := listAllNotes(vaultPath)
	if err != nil {
		return 0, err
	}
	repaired := 0
	for _, note := range notes {
		if !isV7StoreObject(note.Data) {
			continue
		}
		storedRev := stringField(note.Data, "state_rev")
		if storedRev == "" || v7StateRevMatches(note.Data, note.Body, storedRev) {
			continue
		}
		kind := effectiveV7Kind(note.Data)
		order := v7FrontmatterOrder[kind]
		if len(order) == 0 {
			order = frontmatterOrderForType(kind)
		}
		currentID := ""
		previousRev := ""
		nextRev, updated, err := mutateV7DocumentLocked(note.AbsolutePath, order, func(data map[string]any, body string) (map[string]any, string, bool, error) {
			previousRev = stringField(data, "state_rev")
			if previousRev == "" || v7StateRevMatches(data, body, previousRev) {
				return data, body, false, nil
			}
			currentNote := note
			currentNote.Data = data
			currentNote.Body = body
			if err := guardV7ReconcileTerminalTaskStateRevRepair(vaultPath, currentNote, data); err != nil {
				return nil, "", false, err
			}
			if _, ok := data["updated_at"]; ok {
				data["updated_at"] = time.Now().UTC().Format(time.RFC3339)
			}
			if _, ok := data["updated_by"]; ok {
				data["updated_by"] = "tusker:reconcile"
			}
			currentID = stringField(data, "id")
			kind = effectiveV7Kind(data)
			return data, body, true, nil
		})
		if err != nil {
			return repaired, err
		}
		if !updated {
			continue
		}
		if _, ok := v7EventObjectKinds[kind]; ok {
			if err := emitV7Event(vaultPath, currentID, kind, "updated", "tusker:reconcile", map[string]any{"source": "state_rev_repair", "previous_state_rev": previousRev, "state_rev": nextRev, "path": note.RelativePath}); err != nil {
				return repaired, err
			}
		}
		repaired++
	}
	return repaired, nil
}

func briefV7Cmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return err
	}
	id := firstNonEmpty(args.String("id"), args.String("_pos0"))
	if id != "" {
		task, ok := idx.Tasks[id]
		if !ok {
			return tuskerError(errorNotFound, "V7 task not found: "+id)
		}
		fmt.Print(v7Brief(task, idx))
		return nil
	}
	owner := args.String("owner")
	for _, task := range sortedV7Tasks(idx) {
		if owner != "" && stringField(task.Data, "next_owner") != owner {
			continue
		}
		fmt.Print(v7Brief(task, idx))
	}
	return nil
}

func compactMigratedSection(text, fallbackText string, maxLines int) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	var kept []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" && len(kept) == 0 {
			continue
		}
		if strings.Contains(trimmed, "_No ") && strings.Contains(trimmed, "yet") {
			continue
		}
		kept = append(kept, line)
		if len(kept) >= maxLines {
			kept = append(kept, "- Migrated section truncated; inspect source task for old detail if needed.")
			break
		}
	}
	if strings.TrimSpace(strings.Join(kept, "\n")) == "" {
		return fallbackText
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

func migratedV7EvidenceLinks(vaultPath, taskID string) []string {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return nil
	}
	var links []string
	for _, ev := range idx.Evidence[taskID] {
		links = append(links, fmt.Sprintf("- [[%s]] %s", stringField(ev.Data, "id"), stringField(ev.Data, "evidence_kind")))
	}
	sort.Strings(links)
	return links
}

func migratedV7EvidenceExists(vaultPath, taskID, source string) bool {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return false
	}
	for _, evidence := range idx.Evidence[taskID] {
		if stringField(evidence.Data, "created_by") == "tusker:migrate-v7" && strings.Contains(evidence.Body, "Migrated from the "+source) {
			return true
		}
	}
	return false
}

func writeMigratedV7Evidence(vaultPath string, task Note, evidenceID, evidenceText string) error {
	return writeMigratedV7EvidenceRecord(vaultPath, task, evidenceID, "manual_smoke", "V5 task Evidence section", evidenceText)
}

func writeMigratedV7EvidenceRecord(vaultPath string, task Note, evidenceID, evidenceKind, source, evidenceText string) error {
	taskID := stringField(task.Data, "id")
	now := time.Now().UTC().Format(time.RFC3339)
	data := map[string]any{
		"schema":         "tusker.evidence/v1",
		"kind":           "evidence",
		"id":             evidenceID,
		"project":        v7ProjectID(vaultPath),
		"task":           taskID,
		"epic":           wikiTarget(task.Data["epic"]),
		"evidence_kind":  evidenceKind,
		"status":         "accepted",
		"covers":         []string{},
		"artifact_paths": []string{},
		"created_by":     "tusker:migrate-v7",
		"created_at":     now,
		"accepted_by":    "tusker:migrate-v7",
		"accepted_at":    now,
	}
	body := fmt.Sprintf(`# %s · Migrated evidence summary

## Summary

Migrated from the %s for %s.

## Commands

Not recorded during migration.

## Result

%s

## Covers

- Unmapped V5 evidence.

## Artifact links

- None.
`, evidenceID, source, taskID, strings.TrimSpace(evidenceText))
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["evidence"])
	if err != nil {
		return err
	}
	if err := writeText(filepath.Join(vaultPath, "evidence", taskID, evidenceID+".md"), content); err != nil {
		return err
	}
	return emitV7Event(vaultPath, taskID, "task", "evidence_added", "tusker:migrate-v7", map[string]any{"evidence": evidenceID, "kind": evidenceKind})
}

func writeMigratedV7Attempt(vaultPath string, task Note, attemptID, workLog string) error {
	taskID := stringField(task.Data, "id")
	now := time.Now().UTC().Format(time.RFC3339)
	data := map[string]any{
		"schema":         "tusker.attempt/v1",
		"kind":           "attempt",
		"id":             attemptID,
		"project":        v7ProjectID(vaultPath),
		"task":           taskID,
		"runner":         "migration",
		"agent_model":    "",
		"workspace_kind": "same_checkout",
		"workspace_path": "",
		"branch":         "",
		"status":         "handoff",
		"started_at":     now,
		"ended_at":       now,
		"pr_url":         "",
		"evidence":       []string{},
	}
	body := fmt.Sprintf(`# %s · Migrated attempt summary

## Outcome

Migrated from the V5 task Work log for %s.

## Changed areas

Unknown from migrated task log.

## Verification

See migrated evidence records when present.

## Handoff

%s

## Follow-ups proposed

None.
`, attemptID, taskID, strings.TrimSpace(workLog))
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["attempt"])
	if err != nil {
		return err
	}
	if err := writeText(filepath.Join(vaultPath, "attempts", taskID, attemptID+".md"), content); err != nil {
		return err
	}
	return emitV7Event(vaultPath, taskID, "task", "attempt_handoff", "tusker:migrate-v7", map[string]any{"attempt": attemptID})
}

func migrateV7GatesCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	if !args.Bool("from-blocked-reason") {
		return tuskerError(errorMissingArg, "migrate gates requires --from-blocked-reason")
	}
	notes, err := listAllNotes(vaultPath)
	if err != nil {
		return err
	}
	proposals := v7GateProposalsFromV5(vaultPath, notes)
	if !args.Bool("write") {
		if args.Bool("json") {
			emitJSON(map[string]any{"ok": true, "write": false, "count": len(proposals), "gates": proposals})
			return nil
		}
		if !args.Bool("json") {
			fmt.Printf("Would create %d gate%s from blocked V5 task metadata.\n", len(proposals), plural(len(proposals)))
		}
		return nil
	}
	created := 0
	for _, current := range proposals {
		if fileExists(filepath.Join(vaultPath, "work", "gates", current.GateID+".md")) {
			continue
		}
		if err := newV7Gate(Args{
			"vault":            vaultPath,
			"quiet":            "true",
			"id":               current.GateID,
			"blocks":           current.TaskID,
			"kind":             "manual_hold",
			"owner":            fallback(args.String("owner"), "human:"+defaultActorName()),
			"title":            current.Title,
			"action":           current.Action,
			"verification":     current.Verification,
			"why-agent-cannot": "Migrated from V5 blocked-task metadata; the agent cannot safely infer or complete the human-owned blocker without owner input.",
		}); err != nil {
			return err
		}
		created++
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "write": true, "count": len(proposals), "created": created, "gates": proposals})
		return nil
	}
	fmt.Printf("Created %d gate%s from blocked V5 task metadata.\n", created, plural(created))
	return nil
}

func bootstrapV7Dirs(vaultPath string) error {
	for _, relative := range []string{
		"work/epics", "work/tasks", "work/gates", "work/waves", "work/decisions", "work/inbox", "work/closeouts", "work/archive",
		"knowledge/domains", "evidence", "events", "attempts", "dashboards",
		"_generated/indexes", "_generated/packets", "_generated/bases",
	} {
		if err := ensureDir(filepath.Join(vaultPath, relative)); err != nil {
			return err
		}
	}
	return nil
}

func loadV7Index(vaultPath string) (v7Index, error) {
	if loadV7IndexObserver != nil {
		loadV7IndexObserver()
	}
	notes, err := listAllNotes(vaultPath)
	if err != nil {
		return v7Index{}, err
	}
	idx := v7Index{
		Tasks:       map[string]Note{},
		Gates:       map[string]Note{},
		Waves:       map[string]Note{},
		Escalations: map[string]Note{},
		Evidence:    map[string][]Note{},
		Attempts:    map[string][]Note{},
		Decisions:   map[string]Note{},
		Epics:       map[string]Note{},
		Proposals:   map[string]Note{},
		Closeouts:   map[string][]Note{},
	}
	for _, note := range notes {
		kind := effectiveV7Kind(note.Data)
		id := stringField(note.Data, "id")
		switch kind {
		case "task":
			if strings.HasSuffix(stringField(note.Data, "schema"), "/v7") {
				idx.Tasks[id] = note
			}
		case "gate":
			idx.Gates[id] = note
		case "wave":
			if strings.HasSuffix(stringField(note.Data, "schema"), "/v7") {
				idx.Waves[id] = note
			}
		case "escalation":
			if strings.HasSuffix(stringField(note.Data, "schema"), "/v7") {
				idx.Escalations[id] = note
			}
		case "evidence":
			idx.Evidence[stringField(note.Data, "task")] = append(idx.Evidence[stringField(note.Data, "task")], note)
		case "attempt":
			idx.Attempts[stringField(note.Data, "task")] = append(idx.Attempts[stringField(note.Data, "task")], note)
		case "decision":
			idx.Decisions[id] = note
		case "epic":
			if strings.HasSuffix(stringField(note.Data, "schema"), "/v7") {
				idx.Epics[id] = note
			}
		case "proposal":
			idx.Proposals[id] = note
		case "closeout":
			idx.Closeouts[stringField(note.Data, "task")] = append(idx.Closeouts[stringField(note.Data, "task")], note)
		}
	}
	return idx, nil
}

func resolveV7Note(vaultPath, id, kind string) (Note, error) {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return Note{}, err
	}
	switch kind {
	case "task":
		if note, ok := idx.Tasks[id]; ok {
			return note, nil
		}
	case "gate":
		if note, ok := idx.Gates[id]; ok {
			return note, nil
		}
	case "wave":
		if note, ok := idx.Waves[id]; ok {
			return note, nil
		}
	case "escalation":
		if note, ok := idx.Escalations[id]; ok {
			return note, nil
		}
	case "decision":
		if note, ok := idx.Decisions[id]; ok {
			return note, nil
		}
	case "epic":
		if note, ok := idx.Epics[id]; ok {
			return note, nil
		}
	case "attempt":
		for _, attempts := range idx.Attempts {
			for _, note := range attempts {
				if stringField(note.Data, "id") == id {
					return note, nil
				}
			}
		}
	case "evidence":
		for _, evidence := range idx.Evidence {
			for _, note := range evidence {
				if stringField(note.Data, "id") == id {
					return note, nil
				}
			}
		}
	case "proposal":
		if note, ok := idx.Proposals[id]; ok {
			return note, nil
		}
	}
	return Note{}, tuskerError(errorNotFound, fmt.Sprintf("V7 %s not found: %s", kind, id))
}

func v7ProjectedTaskState(vaultPath string, task Note, idx v7Index) map[string]any {
	status := stringField(task.Data, "status")
	projected := map[string]any{}
	if status == "done" || status == "cancelled" || status == "superseded" {
		projected["readiness"] = status
		projected["next_owner"] = "none"
		projected["next_source"] = "status"
		projected["next_ref"] = ""
		projected["next_action"] = ""
		return finalizeV7ProjectedTaskState(projected, task)
	}
	if _, ok := v7LatestValidTerminalCloseout(vaultPath, task, idx); ok {
		return finalizeV7ProjectedTaskState(v7HumanWaitProjection(task, computeV7ProofReport(vaultPath, task, idx), idx, "closeout"), task)
	}
	for _, gateID := range normalizeList(task.Data["gates"]) {
		if gate, ok := idx.Gates[gateID]; ok && stringField(gate.Data, "status") == "open" && boolField(gate.Data, "blocking") {
			if classifyV7GateOwner(gate) == "human" {
				report := computeV7ProofReport(vaultPath, task, idx)
				return finalizeV7ProjectedTaskState(v7HumanWaitProjection(task, report, idx, "human_gate"), task)
			}
			projected["readiness"] = "blocked_by_gate"
			projected["next_owner"] = stringField(gate.Data, "owner")
			projected["next_source"] = "gate"
			projected["next_ref"] = gateID
			projected["next_action"] = stringField(gate.Data, "action")
			return finalizeV7ProjectedTaskState(projected, task)
		}
	}
	for _, gate := range idx.Gates {
		if stringField(gate.Data, "status") == "open" && boolField(gate.Data, "blocking") && containsString(normalizeList(gate.Data["blocks"]), stringField(task.Data, "id")) {
			if classifyV7GateOwner(gate) == "human" {
				report := computeV7ProofReport(vaultPath, task, idx)
				return finalizeV7ProjectedTaskState(v7HumanWaitProjection(task, report, idx, "human_gate"), task)
			}
			projected["readiness"] = "blocked_by_gate"
			projected["next_owner"] = stringField(gate.Data, "owner")
			projected["next_source"] = "gate"
			projected["next_ref"] = stringField(gate.Data, "id")
			projected["next_action"] = stringField(gate.Data, "action")
			return finalizeV7ProjectedTaskState(projected, task)
		}
	}
	// Precedence: work already sitting in review must stay in the review queue.
	// An upstream build failure holds not-yet-started work; it must not pull a
	// task that has been done and is awaiting review back out of review.
	if status == "review" {
		projected["readiness"] = "waiting_on_review"
		projected["next_owner"] = "reviewer"
		projected["next_source"] = "review_policy"
		projected["next_ref"] = ""
		projected["next_action"] = "Review evidence and close or return to rework."
		return finalizeV7ProjectedTaskState(projected, task)
	}
	if upstreamID, held := v7HeldByFailedUpstream(task, idx); held {
		projected["readiness"] = "held"
		projected["next_owner"] = "blocked_dependency"
		projected["next_source"] = "upstream_build"
		projected["next_ref"] = upstreamID
		projected["next_action"] = "Held: upstream " + upstreamID + " failed its build-and-test; wait for it to go green."
		return finalizeV7ProjectedTaskState(projected, task)
	}
	if edge, blocked := v7BlockingDependencyForReadiness(task, idx); blocked {
		projected["readiness"] = "blocked_by_dependency"
		projected["next_owner"] = "blocked_dependency"
		projected["next_source"] = "dependency"
		projected["next_ref"] = edge.ID
		projected["next_action"] = v7DependencyWaitAction(edge)
		return finalizeV7ProjectedTaskState(projected, task)
	}
	projected["readiness"] = "ready"
	nextSource := stringField(task.Data, "next_source")
	if nextSource == "" || nextSource == "gate" || nextSource == "human_gate" || nextSource == "human_review" || strings.HasPrefix(nextSource, "closeout:") || nextSource == "dependency" || nextSource == "upstream_build" || nextSource == "status" || nextSource == "review_policy" {
		projected["next_owner"] = "agent"
		projected["next_source"] = "task"
		projected["next_ref"] = stringField(task.Data, "id")
	} else {
		projected["next_owner"] = fallback(stringField(task.Data, "next_owner"), "agent")
		projected["next_source"] = nextSource
		projected["next_ref"] = fallback(stringField(task.Data, "next_ref"), stringField(task.Data, "id"))
	}
	projected["next_action"] = fallback(stringField(task.Data, "next_action"), "Execute the task contract and satisfy proof mode.")
	return finalizeV7ProjectedTaskState(projected, task)
}

func finalizeV7ProjectedTaskState(projected map[string]any, task Note) map[string]any {
	if stringField(projected, "agent_action") == "stop_until_human_response" {
		return projected
	}
	for _, key := range []string{"agent_action", "machine_status", "human_status", "closeout_status"} {
		if stringField(task.Data, key) != "" {
			projected[key] = ""
		}
	}
	return projected
}

func v7HumanWaitProjection(task Note, report v7ProofReport, idx v7Index, source string) map[string]any {
	refs := append([]string{}, report.OpenHumanGates...)
	if len(refs) == 0 {
		refs = append(refs, report.HumanMissing...)
	}
	refs = uniqueStrings(refs)
	if len(refs) == 0 && stringField(task.Data, "next_ref") != "" {
		refs = normalizeList(task.Data["next_ref"])
	}
	nextOwner := v7HumanWaitOwner(task, report, idx)
	nextSource := source
	if nextSource == "" {
		nextSource = "human_gate"
	}
	actionRefs := fallback(strings.Join(refs, ", "), "human review")
	return map[string]any{
		"readiness":    "waiting_on_human",
		"next_owner":   nextOwner,
		"next_source":  nextSource,
		"next_ref":     strings.Join(refs, ", "),
		"next_action":  "Accept, waive, or return rework for " + actionRefs + ".",
		"agent_action": "stop_until_human_response",
	}
}

func v7HumanWaitOwner(task Note, report v7ProofReport, idx v7Index) string {
	for _, gateID := range report.OpenHumanGates {
		if strings.TrimSpace(gateID) == "" {
			continue
		}
		if gate, ok := idx.Gates[gateID]; ok {
			if owner := stringField(gate.Data, "owner"); v7ProofOwnerClass(owner) == "human" {
				return owner
			}
		}
	}
	for _, owner := range v7TaskProofOwnerHints(task) {
		if v7ProofOwnerClass(owner) == "human" {
			return owner
		}
	}
	if owner := stringField(task.Data, "next_owner"); v7ProofOwnerClass(owner) == "human" {
		return owner
	}
	return "human:" + defaultActorName()
}

func v7Brief(task Note, idx v7Index) string {
	id := stringField(task.Data, "id")
	var openGates []string
	for _, gate := range idx.Gates {
		if stringField(gate.Data, "status") == "open" && containsString(normalizeList(gate.Data["blocks"]), id) {
			openGates = append(openGates, stringField(gate.Data, "id"))
		}
	}
	sort.Strings(openGates)
	attempt := fallback(v7LatestAttemptRuntimeSummary(idx, id), "none")
	return fmt.Sprintf("%s · %s\nStatus: %s\nReadiness: %s\nNext owner: %s\nNext action: %s\nAttempt: %s\nOpen gates: %s\nAcceptance: %d item%s\nProof: %s/%s\nProof required: %s\nEvidence budget: %d\n\n",
		id,
		stringField(task.Data, "title"),
		stringField(task.Data, "status"),
		stringField(task.Data, "readiness"),
		stringField(task.Data, "next_owner"),
		stringField(task.Data, "next_action"),
		attempt,
		fallback(strings.Join(openGates, ", "), "none"),
		v7AcceptanceCount(task.Body),
		plural(v7AcceptanceCount(task.Body)),
		fallback(stringField(task.Data, "proof_mode"), defaultV7ProofMode(stringField(task.Data, "risk"))),
		fallback(stringField(task.Data, "proof_status"), "pending"),
		fallback(strings.Join(normalizeList(task.Data["proof_required"]), ", "), "none"),
		intField(task.Data, "evidence_budget"),
	)
}

func v7TaskAttemptRuntimeLines(vaultPath string, task Note) []string {
	if strings.TrimSpace(vaultPath) == "" || noteDisplayKind(task.Data) != "task" {
		return nil
	}
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return nil
	}
	summary := v7LatestAttemptRuntimeSummary(idx, stringField(task.Data, "id"))
	if summary == "" {
		return nil
	}
	return []string{"- Attempt: " + summary}
}

func v7LatestAttemptRuntimeSummary(idx v7Index, taskID string) string {
	attempts := append([]Note{}, idx.Attempts[taskID]...)
	if len(attempts) == 0 {
		return ""
	}
	sort.Slice(attempts, func(i, j int) bool {
		return stringField(attempts[i].Data, "id") < stringField(attempts[j].Data, "id")
	})
	latest := attempts[len(attempts)-1]
	status := stringField(latest.Data, "status")
	if status != "started" && status != "handoff" {
		return ""
	}
	fields := []string{stringField(latest.Data, "id") + " " + status}
	if runner := strings.TrimSpace(stringField(latest.Data, "runner")); runner != "" {
		fields = append(fields, "runner="+runner)
	}
	if branch := strings.TrimSpace(stringField(latest.Data, "branch")); branch != "" {
		fields = append(fields, "branch="+branch)
	}
	return strings.Join(fields, " ")
}

func v7Packet(vaultPath string, task Note, idx v7Index, audience string) string {
	id := stringField(task.Data, "id")
	var b strings.Builder
	switch strings.ToLower(strings.TrimSpace(audience)) {
	case "explainer", "understanding":
		return v7ExplainerPacket(vaultPath, task, idx)
	}
	if audience == "reviewer" {
		fmt.Fprintf(&b, "# %s reviewer packet\n\n", id)
		writeV7PacketWarnings(&b, vaultPath, task)
		fmt.Fprintf(&b, "## Project skill routing\n\n%s\n\n", v7ProjectSkillRouting(vaultPath, task))
		fmt.Fprintf(&b, "## Governing specs / decisions\n\n%s\n\n", v7SpecRefsPacketSection(vaultPath, task))
		fmt.Fprintf(&b, "## Domain context\n\n%s\n\n", v7DomainContext(vaultPath, task))
		fmt.Fprintf(&b, "## Intent\n\n%s\n\n", sectionContent(task.Body, "## Intent"))
		fmt.Fprintf(&b, "## Acceptance\n\n%s\n\n", sectionContent(task.Body, "## Acceptance"))
		report := computeV7ProofReport(vaultPath, task, idx)
		fmt.Fprintf(&b, "## Proof status\n\nMode: %s\nStatus: %s\nMissing: %s\n\n", report.Mode, report.Status, fallback(strings.Join(append(append([]string{}, report.Missing...), report.ModeMissing...), ", "), "none"))
		fmt.Fprintf(&b, "## Proof required\n\n%s\n\n", v7BulletList(normalizeList(task.Data["proof_required"])))
		fmt.Fprintf(&b, "## Verification\n\n%s\n\n", v7PacketSnippet(sectionContent(task.Body, "## Verification"), 12))
		fmt.Fprintf(&b, "## Evidence\n\n")
		for _, ev := range idx.Evidence[id] {
			fmt.Fprintf(&b, "- [[%s]] %s\n", stringField(ev.Data, "id"), stringField(ev.Data, "evidence_kind"))
		}
		fmt.Fprintf(&b, "\n## Known gates / waivers\n\n%s\n\n", v7GateSummaryForTask(idx, id))
		fmt.Fprintf(&b, "## Soft dependency blast radius\n\n%s\n\n", v7SoftDependencyBlastRadius(idx, id))
		fmt.Fprintf(&b, "## Risk policy\n\n%s\n", v7ClosePolicySummary(vaultPath, task))
		return b.String()
	}
	fmt.Fprintf(&b, "# %s agent packet\n\n", id)
	writeV7PacketWarnings(&b, vaultPath, task)
	fmt.Fprintf(&b, "## Project skill routing\n\n%s\n\n", v7ProjectSkillRouting(vaultPath, task))
	fmt.Fprintf(&b, "## Governing specs / decisions\n\n%s\n\n", v7SpecRefsPacketSection(vaultPath, task))
	fmt.Fprintf(&b, "## Task contract\n\nIntent:\n%s\n\nAcceptance:\n%s\n\n", v7PacketSnippet(sectionContent(task.Body, "## Intent"), 8), v7PacketSnippet(sectionContent(task.Body, "## Acceptance"), 18))
	fmt.Fprintf(&b, "## Open gates\n\n")
	for _, gate := range idx.Gates {
		if stringField(gate.Data, "status") == "open" && containsString(normalizeList(gate.Data["blocks"]), id) {
			fmt.Fprintf(&b, "- %s: %s Owner: %s\n", stringField(gate.Data, "id"), stringField(gate.Data, "action"), stringField(gate.Data, "owner"))
		}
	}
	fmt.Fprintf(&b, "\n## Dependencies\n\n%s\n\n", v7BulletList(normalizeList(task.Data["dependencies"])))
	fmt.Fprintf(&b, "## Routed file capsules\n\n%s\n\n", v7RoutedFileCapsules(vaultPath, task))
	fmt.Fprintf(&b, "## Domain context\n\n%s\n\n", v7DomainContext(vaultPath, task))
	fmt.Fprintf(&b, "## Verification\n\n%s\n\n", v7PacketSnippet(sectionContent(task.Body, "## Verification"), 12))
	fmt.Fprintf(&b, "## Proof requirements\n\nMode: %s\nRequired:\n%s\n\n", fallback(stringField(task.Data, "proof_mode"), defaultV7ProofMode(stringField(task.Data, "risk"))), v7BulletList(normalizeList(task.Data["proof_required"])))
	fmt.Fprintf(&b, "## Branch policy\n\nProtected task/gate state fields must be changed through Tusker control operations on a control branch.\n\n")
	fmt.Fprintf(&b, "## Close policy\n\n%s\n", v7ClosePolicySummary(vaultPath, task))
	return b.String()
}

func v7ExplainerPacket(vaultPath string, task Note, idx v7Index) string {
	id := stringField(task.Data, "id")
	report := computeV7ProofReport(vaultPath, task, idx)
	var b strings.Builder
	fmt.Fprintf(&b, "# %s explainer packet\n\n", id)
	fmt.Fprintf(&b, "- Purpose: help a human understand and participate in this change.\n")
	fmt.Fprintf(&b, "- Boundary: this packet is not proof, approval, or a replacement for code review.\n")
	fmt.Fprintf(&b, "- Task: %s\n", stringField(task.Data, "title"))
	fmt.Fprintf(&b, "- Risk: %s\n", fallback(stringField(task.Data, "risk"), "medium"))
	fmt.Fprintf(&b, "- Proof: %s/%s\n\n", report.Mode, report.Status)
	writeV7PacketWarnings(&b, vaultPath, task)
	fmt.Fprintf(&b, "## Background\n\n")
	fmt.Fprintf(&b, "Project route:\n%s\n\n", v7ProjectSkillRouting(vaultPath, task))
	fmt.Fprintf(&b, "Routed file capsules:\n%s\n\n", v7RoutedFileCapsules(vaultPath, task))
	fmt.Fprintf(&b, "Domain mental model:\n%s\n\n", v7DomainContext(vaultPath, task))
	fmt.Fprintf(&b, "## Intuition\n\n")
	fmt.Fprintf(&b, "%s\n\n", v7ExplainerSnippet(task.Body, "## Intent", "Understand why this task exists before reading a diff.", 8))
	fmt.Fprintf(&b, "The acceptance contract is the anchor: every implementation detail should explain how it moves one of those outcomes from pending to proved.\n\n")
	fmt.Fprintf(&b, "## Task Walkthrough\n\n")
	fmt.Fprintf(&b, "### Acceptance\n\n%s\n\n", v7ExplainerSnippet(task.Body, "## Acceptance", "No acceptance section found.", 18))
	fmt.Fprintf(&b, "### Non-goals\n\n%s\n\n", v7ExplainerSnippet(task.Body, "## Non-goals", "No non-goals section found.", 8))
	fmt.Fprintf(&b, "### Knowledge delta\n\n%s\n\n", v7ExplainerSnippet(task.Body, "## Knowledge delta", "No durable knowledge delta declared.", 10))
	fmt.Fprintf(&b, "## Literate Diff Guide\n\n")
	fmt.Fprintf(&b, "- Read the current diff in conceptual order, not file-name order.\n")
	fmt.Fprintf(&b, "- Start with the smallest changed interface or state transition.\n")
	fmt.Fprintf(&b, "- Then inspect adapters, commands, docs, and tests that hang off that concept.\n")
	fmt.Fprintf(&b, "- If a generated daemon review packet exists, use its changed-file and command summaries as the raw fact list.\n\n")
	fmt.Fprintf(&b, "## Proof Map\n\n")
	fmt.Fprintf(&b, "- Required proof: %s\n", fallback(strings.Join(normalizeList(task.Data["proof_required"]), ", "), "none"))
	fmt.Fprintf(&b, "- Missing proof: %s\n", fallback(strings.Join(append(append([]string{}, report.Missing...), report.ModeMissing...), ", "), "none"))
	fmt.Fprintf(&b, "- Machine gaps: %s\n", fallback(strings.Join(report.MachineMissing, ", "), "none"))
	fmt.Fprintf(&b, "- Human gaps: %s\n", fallback(strings.Join(report.HumanMissing, ", "), "none"))
	fmt.Fprintf(&b, "- Open gates: %s\n\n", fallback(strings.Join(report.OpenGates, ", "), "none"))
	fmt.Fprintf(&b, "## Review Focus\n\n")
	fmt.Fprintf(&b, "- Check whether the implementation preserves the task scope and non-goals.\n")
	fmt.Fprintf(&b, "- Check whether each acceptance row has behavior-level proof.\n")
	fmt.Fprintf(&b, "- Check whether any knowledge delta needs a docs/canon update before close.\n")
	fmt.Fprintf(&b, "- Independent reviewers may close every risk tier after objective proof and explicit gates pass.\n\n")
	fmt.Fprintf(&b, "## Comprehension Check\n\n%s", v7ExplainerQuiz(task, report))
	return b.String()
}

func v7ExplainerSnippet(body, heading, fallbackText string, maxLines int) string {
	content := strings.TrimSpace(sectionContent(body, heading))
	if content == "" {
		return fallbackText
	}
	return v7PacketSnippet(content, maxLines)
}

func v7ExplainerQuiz(task Note, report v7ProofReport) string {
	acceptance := v7ExplainerSnippet(task.Body, "## Acceptance", "No acceptance section found.", 8)
	proofGaps := fallback(strings.Join(append(append([]string{}, report.Missing...), report.ModeMissing...), ", "), "none")
	domains := fallback(strings.Join(normalizeList(task.Data["domains"]), ", "), "none declared")
	knowledgeDelta := v7ExplainerSnippet(task.Body, "## Knowledge delta", "No durable knowledge delta declared.", 6)
	questions := []struct {
		Question string
		Answer   string
	}{
		{
			Question: "Which observable outcomes define success for this task?",
			Answer:   acceptance,
		},
		{
			Question: "What proof gaps, if any, still prevent confident review or close?",
			Answer:   proofGaps,
		},
		{
			Question: "Which domain canon should shape your mental model before reading the diff?",
			Answer:   domains,
		},
		{
			Question: "What durable understanding changes if this task succeeds?",
			Answer:   knowledgeDelta,
		},
		{
			Question: "Does this explainer packet count as evidence or approval?",
			Answer:   "No. It is an understanding aid. Proof still comes from verification rows, evidence records, gates, and review/close policy.",
		},
	}
	var lines []string
	for i, question := range questions {
		lines = append(lines, fmt.Sprintf("%d. %s", i+1, question.Question))
		lines = append(lines, "")
		lines = append(lines, "<details>")
		lines = append(lines, "<summary>Answer</summary>")
		lines = append(lines, "")
		lines = append(lines, question.Answer)
		lines = append(lines, "")
		lines = append(lines, "</details>")
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func writeV7PacketWarnings(b *strings.Builder, vaultPath string, task Note) {
	warnings := v7PacketWarnings(vaultPath, task)
	if len(warnings) == 0 {
		return
	}
	fmt.Fprintf(b, "## Packet warnings\n\n%s\n\n", v7BulletList(warnings))
}

func v7PacketWarnings(vaultPath string, task Note) []string {
	var warnings []string
	skillPath := vaultDisplayPath(vaultPath, "SKILL.md")
	if !fileExists(filepath.Join(vaultPath, "SKILL.md")) {
		if fileExists(filepath.Join(vaultPath, "README.md")) {
			warnings = append(warnings, "`"+skillPath+"` is missing; use `"+vaultDisplayPath(vaultPath, "README.md")+"` and this task packet as the fallback project route.")
		} else {
			warnings = append(warnings, "`"+skillPath+"` is missing; rely on the task contract until project knowledge is installed.")
		}
	}
	for _, warning := range v7PacketDomainRouteWarnings(vaultPath, task) {
		warnings = append(warnings, warning)
	}
	for _, item := range v7PacketStubAcceptanceItems(task.Body) {
		warnings = append(warnings, "Acceptance looks vague or placeholder: "+item)
	}
	return warnings
}

func v7PacketDomainRouteWarnings(vaultPath string, task Note) []string {
	var warnings []string
	for _, domain := range normalizeList(task.Data["domains"]) {
		var missing []string
		indexPath := filepath.Join(vaultPath, "knowledge", "domains", domain, "INDEX.md")
		canonPath := filepath.Join(vaultPath, "knowledge", "domains", domain, "CANON.md")
		if !fileExists(indexPath) {
			missing = append(missing, "knowledge/domains/"+domain+"/INDEX.md")
		}
		if !fileExists(canonPath) {
			missing = append(missing, "knowledge/domains/"+domain+"/CANON.md")
		}
		if len(missing) > 0 {
			warnings = append(warnings, "`"+domain+"` domain route missing: "+strings.Join(missing, ", "))
		}
	}
	return warnings
}

func v7PacketStubAcceptanceItems(body string) []string {
	items := v7VagueAcceptanceItems(body)
	content := sectionContent(body, "## Acceptance")
	seen := map[string]bool{}
	for _, item := range items {
		seen[strings.ToLower(item)] = true
	}
	for _, line := range strings.Split(content, "\n") {
		item := v7AcceptanceOutcomeFromLine(line)
		if item == "" {
			continue
		}
		normalized := strings.ToLower(strings.Trim(strings.TrimSpace(item), ".!"))
		normalized = strings.Join(strings.Fields(normalized), " ")
		switch normalized {
		case "define the accepted outcome", "tbd", "todo", "placeholder":
			if !seen[normalized] {
				items = append(items, item)
				seen[normalized] = true
			}
		}
	}
	return items
}

func v7ProjectSkillRouting(vaultPath string, task Note) string {
	var lines []string
	skillPath := vaultDisplayPath(vaultPath, "SKILL.md")
	if fileExists(filepath.Join(vaultPath, "SKILL.md")) {
		lines = append(lines, "- Load the repo project knowledge skill at `"+skillPath+"`.")
		if data, body, err := parseFrontmatterMustRead(filepath.Join(vaultPath, "SKILL.md")); err == nil {
			note := Note{AbsolutePath: filepath.Join(vaultPath, "SKILL.md"), RelativePath: "SKILL.md", Data: data, Body: body}
			if capsule := capsuleOneLine(note); capsule != "" {
				lines = append(lines, "- Project skill capsule: "+capsule)
			}
		}
	} else if fileExists(filepath.Join(vaultPath, "README.md")) {
		lines = append(lines, "- `"+skillPath+"` is missing; use `"+vaultDisplayPath(vaultPath, "README.md")+"` plus this packet until the project skill is installed.")
	} else {
		lines = append(lines, "- `"+skillPath+"` is missing; use the task contract as the project route.")
	}
	lines = append(lines, "- Use the installed Tusker operator skill only for task mechanics, gates, evidence, lifecycle, and CLI semantics.")
	declaredDomains := normalizeList(task.Data["domains"])
	domains := v7PacketDomains(vaultPath, task)
	if len(declaredDomains) == 0 {
		if len(domains) == 0 {
			lines = append(lines, "- No task domains declared; use the task contract and add a domain when the work creates durable canon.")
			return strings.Join(lines, "\n")
		}
		lines = append(lines, "- No task domains declared; default to the `project` domain route for repo-wide canon.")
	} else if len(domains) == 0 {
		lines = append(lines, "- Task domains were declared, but no routed domain files are available; use the task contract and packet warnings.")
		return strings.Join(lines, "\n")
	}
	for _, domain := range domains {
		indexPath := filepath.Join(vaultPath, "knowledge", "domains", domain, "INDEX.md")
		canonPath := filepath.Join(vaultPath, "knowledge", "domains", domain, "CANON.md")
		if fileExists(indexPath) && fileExists(canonPath) {
			lines = append(lines, fmt.Sprintf("- `%s`: read `knowledge/domains/%s/INDEX.md`, then `knowledge/domains/%s/CANON.md`.", domain, domain, domain))
			if index, indexErr := readV7DomainIndex(vaultPath, domain); indexErr == nil {
				if capsule := capsuleOneLine(index); capsule != "" {
					lines = append(lines, fmt.Sprintf("- `%s` INDEX capsule: %s", domain, capsule))
				}
			}
			if canon, canonErr := readV7DomainCanon(vaultPath, domain); canonErr == nil {
				if capsule := capsuleOneLine(canon); capsule != "" {
					lines = append(lines, fmt.Sprintf("- `%s` CANON capsule: %s", domain, capsule))
				}
			}
			continue
		}
		lines = append(lines, fmt.Sprintf("- `%s`: domain route is missing; use the task contract and packet warning instead of reading dead knowledge links.", domain))
	}
	return strings.Join(lines, "\n")
}

func v7GateSummaryForTask(idx v7Index, taskID string) string {
	var rows []string
	for _, gate := range idx.Gates {
		if !containsString(normalizeList(gate.Data["blocks"]), taskID) {
			continue
		}
		status := stringField(gate.Data, "status")
		detail := stringField(gate.Data, "action")
		if status == "waived" {
			detail = "waived: " + stringField(gate.Data, "waive_reason")
		}
		rows = append(rows, fmt.Sprintf("- [[%s]] %s owner=%s: %s", stringField(gate.Data, "id"), status, stringField(gate.Data, "owner"), detail))
	}
	sort.Strings(rows)
	if len(rows) == 0 {
		return "- None."
	}
	return strings.Join(rows, "\n")
}

func v7DomainContext(vaultPath string, task Note) string {
	domains := v7PacketDomains(vaultPath, task)
	if len(domains) == 0 {
		return "- None declared."
	}
	var sections []string
	for _, domain := range domains {
		index, indexErr := readV7DomainIndex(vaultPath, domain)
		canon, canonErr := readV7DomainCanon(vaultPath, domain)
		if indexErr != nil || canonErr != nil {
			sections = append(sections, fmt.Sprintf("### %s\n\n- Domain context not found in V7 knowledge/domains.", domain))
			continue
		}
		var capsuleLines []string
		if capsule := capsuleOneLine(index); capsule != "" {
			capsuleLines = append(capsuleLines, "- INDEX: "+capsule)
		}
		if capsule := capsuleOneLine(canon); capsule != "" {
			capsuleLines = append(capsuleLines, "- CANON: "+capsule)
		}
		capsules := ""
		if len(capsuleLines) > 0 {
			capsules = "\n\nCapsules:\n" + strings.Join(capsuleLines, "\n")
		}
		section := fmt.Sprintf("### %s · %s\n\nSummary: %s%s\n\nRead this when:\n%s",
			domain,
			stringField(index.Data, "title"),
			stringField(index.Data, "summary"),
			capsules,
			v7PacketSnippet(sectionContent(index.Body, "## Read This When"), 8),
		)
		if prohibitions := renderV7DomainCanonProhibitions(canon.Body); prohibitions != "" {
			section += "\n\nProhibitions:\n" + v7PacketSnippet(prohibitions, 8)
		}
		section += "\n\nCurrent truth:\n" + v7PacketSnippet(sectionContent(canon.Body, "## Current Truth"), 8)
		sections = append(sections, section)
	}
	return strings.Join(sections, "\n\n")
}

func v7ClosePolicySummary(vaultPath string, task Note) string {
	risk := strings.ToLower(fallback(stringField(task.Data, "risk"), "medium"))
	policy, err := v7ClosePolicyFor(vaultPath, risk)
	if err != nil {
		policy = defaultV7ClosePolicy(risk)
	}
	requiredEvidence := mergeUniqueStrings(normalizeList(task.Data["evidence_required"]), policy.RequiredEvidence)
	return fmt.Sprintf("Risk: %s.\nRequired acceptor: %s.\nRequired evidence: %s.\nRequired gates: %s.",
		risk,
		policy.RequiredAcceptor,
		fallback(strings.Join(requiredEvidence, ", "), "none"),
		fallback(strings.Join(policy.RequiredGates, ", "), "none"),
	)
}

func syncV7GitStateBranch(vaultPath, branch, remote, message string) (string, error) {
	repoRoot := filepath.Dir(vaultPath)
	if err := exec.Command("git", "-C", repoRoot, "rev-parse", "--git-dir").Run(); err != nil {
		return "", tuskerError(errorInvalidArg, "V7 state sync requires a Git repository", withPath(repoRoot))
	}
	remoteRef := ""
	remoteRev := ""
	hasRemoteRev := false
	if remote != "" {
		if err := fetchV7StateBranch(repoRoot, remote, branch); err != nil {
			return "", err
		}
		remoteRef = "refs/remotes/" + remote + "/" + branch
		remoteRev, hasRemoteRev = gitRevParse(repoRoot, remoteRef)
	}
	files, err := v7StateFiles(vaultPath)
	if err != nil {
		return "", err
	}
	tree, err := buildV7GitTree(repoRoot, files)
	if err != nil {
		return "", err
	}
	ref := "refs/heads/" + branch
	oldRev, hasOld := gitRevParse(repoRoot, ref)
	parentRev := oldRev
	hasParent := hasOld
	if hasRemoteRev {
		parentRev = remoteRev
		hasParent = true
	}
	commitArgs := []string{"-C", repoRoot, "commit-tree", tree, "-m", fallback(message, "tusker state sync")}
	if hasParent {
		commitArgs = append(commitArgs, "-p", parentRev)
	}
	commitOut, err := exec.Command("git", commitArgs...).Output()
	if err != nil {
		return "", err
	}
	commit := strings.TrimSpace(string(commitOut))
	updateArgs := []string{"-C", repoRoot, "update-ref", ref, commit}
	if hasOld {
		updateArgs = append(updateArgs, oldRev)
	}
	if out, err := exec.Command("git", updateArgs...).CombinedOutput(); err != nil {
		return "", tuskerError(errorInvalidTransition, "failed to update "+ref+": "+strings.TrimSpace(string(out)))
	}
	if remote != "" {
		if err := pushV7StateBranch(repoRoot, remote, branch, commit, remoteRev, hasRemoteRev); err != nil {
			return "", err
		}
	}
	return commit, nil
}

func importV7GitStateBranch(vaultPath, branch, remote string) (int, error) {
	repoRoot := filepath.Dir(vaultPath)
	ref := branch
	if remote != "" {
		if err := fetchV7StateBranch(repoRoot, remote, branch); err != nil {
			return 0, err
		}
		ref = "refs/remotes/" + remote + "/" + branch
	}
	filesOut, err := exec.Command("git", "-C", repoRoot, "ls-tree", "-r", "--name-only", ref, "--", "leases").Output()
	if err != nil {
		return 0, err
	}
	count := 0
	for _, rel := range strings.Split(strings.TrimSpace(string(filesOut)), "\n") {
		rel = strings.TrimSpace(rel)
		if rel == "" || !strings.HasPrefix(rel, "leases/") || !strings.HasSuffix(rel, ".json") {
			continue
		}
		raw, err := exec.Command("git", "-C", repoRoot, "show", ref+":"+rel).Output()
		if err != nil {
			return count, err
		}
		taskID := strings.TrimSuffix(filepath.Base(rel), ".json")
		if err := writeText(filepath.Join(v7LeaseDir(vaultPath), taskID+".json"), string(raw)); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func fetchV7StateBranch(repoRoot, remote, branch string) error {
	refspec := fmt.Sprintf("refs/heads/%s:refs/remotes/%s/%s", branch, remote, branch)
	if out, err := exec.Command("git", "-C", repoRoot, "fetch", "--quiet", remote, refspec).CombinedOutput(); err != nil {
		trimmed := strings.TrimSpace(string(out))
		if strings.Contains(trimmed, "couldn't find remote ref") || strings.Contains(trimmed, "could not find remote ref") {
			return nil
		}
		return tuskerError(errorInvalidTransition, "failed to fetch "+remote+"/"+branch+": "+trimmed)
	}
	return nil
}

func pushV7StateBranch(repoRoot, remote, branch, commit, oldRemoteRev string, hasOldRemote bool) error {
	args := []string{"-C", repoRoot, "push", remote, commit + ":refs/heads/" + branch}
	if hasOldRemote {
		args = append(args, "--force-with-lease=refs/heads/"+branch+":"+oldRemoteRev)
	}
	if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
		return tuskerError(errorInvalidTransition, "failed to push "+remote+"/"+branch+": "+strings.TrimSpace(string(out)))
	}
	return nil
}

type v7GitTreeNode struct {
	Files map[string]string
	Dirs  map[string]*v7GitTreeNode
}

func buildV7GitTree(repoRoot string, files []v7StateFile) (string, error) {
	root := &v7GitTreeNode{Files: map[string]string{}, Dirs: map[string]*v7GitTreeNode{}}
	for _, file := range files {
		parts := strings.Split(filepath.ToSlash(file.Path), "/")
		node := root
		for _, part := range parts[:len(parts)-1] {
			if node.Dirs[part] == nil {
				node.Dirs[part] = &v7GitTreeNode{Files: map[string]string{}, Dirs: map[string]*v7GitTreeNode{}}
			}
			node = node.Dirs[part]
		}
		blob, err := gitHashObject(repoRoot, file.Content)
		if err != nil {
			return "", err
		}
		node.Files[parts[len(parts)-1]] = blob
	}
	return writeV7GitTree(repoRoot, root)
}

func writeV7GitTree(repoRoot string, node *v7GitTreeNode) (string, error) {
	var names []string
	for name := range node.Dirs {
		names = append(names, name)
	}
	for name := range node.Files {
		names = append(names, name)
	}
	sort.Strings(names)
	var input strings.Builder
	for _, name := range names {
		if dir, ok := node.Dirs[name]; ok {
			tree, err := writeV7GitTree(repoRoot, dir)
			if err != nil {
				return "", err
			}
			fmt.Fprintf(&input, "040000 tree %s\t%s\n", tree, name)
			continue
		}
		fmt.Fprintf(&input, "100644 blob %s\t%s\n", node.Files[name], name)
	}
	out, err := gitCommandInput(repoRoot, input.String(), "mktree")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func gitHashObject(repoRoot, content string) (string, error) {
	out, err := gitCommandInput(repoRoot, content, "hash-object", "-w", "--stdin")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func gitCommandInput(repoRoot, input string, args ...string) (string, error) {
	fullArgs := append([]string{"-C", repoRoot}, args...)
	cmd := exec.Command("git", fullArgs...)
	cmd.Stdin = bytes.NewBufferString(input)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func gitRevParse(repoRoot, ref string) (string, bool) {
	out, err := exec.Command("git", "-C", repoRoot, "rev-parse", "--verify", "--quiet", ref).Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}

func appendV7EvidenceLink(taskPath, evidenceID, kind, summary string) error {
	data, body, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		return err
	}
	baseRev := stringField(data, "state_rev")
	bullet := fmt.Sprintf("- [[%s]] %s — %s", evidenceID, kind, summary)
	if !strings.Contains(body, bullet) {
		body = appendSectionBullet(body, "## Evidence", bullet, true)
	}
	data["updated_at"] = time.Now().UTC().Format(time.RFC3339)
	data["updated_by"] = "agent:" + defaultActorName()
	_, err = saveV7DocumentCAS(taskPath, data, body, v7FrontmatterOrder["task"], baseRev)
	return err
}

func emitV7Event(vaultPath, objectID, objectKind, eventKind, actor string, payload map[string]any) error {
	now := time.Now().UTC()
	eventID := newRecordID()
	event := map[string]any{
		"schema":      "tusker.event/v1",
		"id":          eventID,
		"project":     v7ProjectID(vaultPath),
		"object":      objectID,
		"object_kind": objectKind,
		"event_kind":  eventKind,
		"actor":       actor,
		"at":          now.Format(time.RFC3339),
	}
	if payload != nil {
		event["payload"] = payload
	}
	name := fmt.Sprintf("%s--%s--%s.json", objectID, now.Format("20060102T150405Z"), eventID)
	path := filepath.Join(vaultPath, "events", now.Format("2006"), now.Format("01"), name)
	return writeJSON(path, event)
}

func nextV7Sequence(vaultPath, epic, kind string) int {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return 1
	}
	maxSeq := 0
	pattern := v7TaskIDPattern
	source := map[string]Note{}
	switch kind {
	case "task":
		pattern = v7TaskIDPattern
		source = idx.Tasks
	case "gate":
		pattern = v7GateIDPattern
		source = idx.Gates
	case "decision":
		pattern = v7DecisionIDPattern
		source = idx.Decisions
	case "proposal":
		pattern = v7ProposalIDPattern
		source = idx.Proposals
	}
	for id := range source {
		match := pattern.FindStringSubmatch(id)
		if match != nil && match[1] == epic {
			maxSeq = maxInt(maxSeq, atoiSafe(match[2]))
		}
	}
	return maxSeq + 1
}

func nextSafeV7TaskID(vaultPath, epic string) string {
	seq := nextV7Sequence(vaultPath, epic, "task")
	for {
		id := fmt.Sprintf("%s-T-%s", epic, padNumber(seq))
		if len(v7TaskIDCollisionPaths(vaultPath, id)) == 0 {
			return id
		}
		seq++
	}
}

func v7TaskIDCollisionPaths(vaultPath, id string) []string {
	id = strings.ToUpper(strings.TrimSpace(id))
	if id == "" {
		return nil
	}
	filename := id + ".md"
	roots := []string{
		filepath.Join(vaultPath, "work", "tasks"),
		filepath.Join(vaultPath, "epics"),
	}
	for _, root := range configuredLegacyTaskRoots(vaultPath) {
		roots = append(roots, root)
	}
	seen := map[string]bool{}
	var paths []string
	for _, root := range roots {
		root = filepath.Clean(root)
		if root == "." || root == "" || seen[root] || !dirExists(root) {
			continue
		}
		seen[root] = true
		_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil || entry == nil {
				return nil
			}
			if entry.IsDir() {
				name := entry.Name()
				if name == ".git" || name == "node_modules" || name == "vendor" || name == "_generated" || name == "events" || name == "attempts" || name == "evidence" || name == "Attachments" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(entry.Name(), ".md") {
				return nil
			}
			if entry.Name() != filename {
				raw, err := readText(path)
				if err != nil {
					return nil
				}
				data, _, err := parseFrontmatter(raw)
				if err != nil || strings.ToUpper(strings.TrimSpace(stringField(data, "id"))) != id {
					return nil
				}
			}
			paths = append(paths, v7PathForMessage(vaultPath, path))
			return nil
		})
	}
	return uniqueStrings(paths)
}

func configuredLegacyTaskRoots(vaultPath string) []string {
	var roots []string
	configPaths := []string{
		filepath.Join(v7RepoRoot(vaultPath), "tusker.yaml"),
		filepath.Join(vaultPath, "_system", "config.yaml"),
		workflowPath(vaultPath),
	}
	for _, configPath := range configPaths {
		raw, err := readText(configPath)
		if err != nil {
			continue
		}
		var data map[string]any
		if err := yaml.Unmarshal([]byte(raw), &data); err != nil {
			continue
		}
		for _, rel := range legacyTaskRootsFromConfigMap(data) {
			roots = append(roots, resolveLegacyTaskRoot(vaultPath, rel))
		}
	}
	return uniqueStrings(roots)
}

func legacyTaskRootsFromConfigMap(data map[string]any) []string {
	if data == nil {
		return nil
	}
	var roots []string
	roots = append(roots, normalizeList(data["legacy_task_roots"])...)
	roots = append(roots, normalizeList(data["legacyTaskRoots"])...)
	if nested, ok := data["task_roots"].(map[string]any); ok {
		roots = append(roots, normalizeList(nested["legacy"])...)
		roots = append(roots, normalizeList(nested["v5"])...)
		roots = append(roots, normalizeList(nested["tasks"])...)
	}
	if nested, ok := data["taskRoots"].(map[string]any); ok {
		roots = append(roots, normalizeList(nested["legacy"])...)
		roots = append(roots, normalizeList(nested["v5"])...)
		roots = append(roots, normalizeList(nested["tasks"])...)
	}
	for _, key := range []string{"legacy", "v5", "tracker"} {
		if nested, ok := data[key].(map[string]any); ok {
			roots = append(roots, normalizeList(nested["task_roots"])...)
			roots = append(roots, normalizeList(nested["taskRoots"])...)
			roots = append(roots, normalizeList(nested["tasks"])...)
		}
	}
	if nested, ok := data["validation"].(map[string]any); ok {
		roots = append(roots, legacyTaskRootsFromConfigMap(nested)...)
	}
	var cleaned []string
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root != "" {
			cleaned = append(cleaned, root)
		}
	}
	return cleaned
}

func resolveLegacyTaskRoot(vaultPath, root string) string {
	root = filepath.FromSlash(strings.TrimSpace(root))
	if root == "" {
		return ""
	}
	if filepath.IsAbs(root) {
		return filepath.Clean(root)
	}
	if strings.HasPrefix(root, "tusker"+string(filepath.Separator)) {
		return filepath.Join(v7RepoRoot(vaultPath), root)
	}
	return filepath.Join(vaultPath, root)
}

func v7PathForMessage(vaultPath, path string) string {
	if rel, err := filepath.Rel(v7RepoRoot(vaultPath), path); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	if rel, err := filepath.Rel(vaultPath, path); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(path)
}

func nextV7EvidenceSequence(vaultPath, taskID string) int {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return 1
	}
	maxSeq := 0
	for _, ev := range idx.Evidence[taskID] {
		match := v7EvidenceIDPattern.FindStringSubmatch(stringField(ev.Data, "id"))
		if match != nil {
			maxSeq = maxInt(maxSeq, atoiSafe(match[3]))
		}
	}
	return maxSeq + 1
}

func nextV7AttemptSequence(vaultPath, taskID string) int {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return 1
	}
	maxSeq := 0
	for _, attempt := range idx.Attempts[taskID] {
		match := v7AttemptIDPattern.FindStringSubmatch(stringField(attempt.Data, "id"))
		if match != nil {
			maxSeq = maxInt(maxSeq, atoiSafe(match[3]))
		}
	}
	return maxSeq + 1
}

func latestV7AttemptID(vaultPath, taskID string) (string, error) {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return "", err
	}
	attempts := idx.Attempts[taskID]
	if len(attempts) == 0 {
		return "", tuskerError(errorNotFound, "No attempt exists for "+taskID, withHint("attempts are runtime/session state; run `tusker attempt start "+taskID+"`, then retry `tusker finish "+taskID+" --request-review`"))
	}
	sort.Slice(attempts, func(i, j int) bool { return stringField(attempts[i].Data, "id") < stringField(attempts[j].Data, "id") })
	return stringField(attempts[len(attempts)-1].Data, "id"), nil
}

func sortedV7Tasks(idx v7Index) []Note {
	var tasks []Note
	for _, task := range idx.Tasks {
		tasks = append(tasks, task)
	}
	sort.Slice(tasks, func(i, j int) bool { return stringField(tasks[i].Data, "id") < stringField(tasks[j].Data, "id") })
	return tasks
}

func sortedV7Gates(idx v7Index) []Note {
	var gates []Note
	for _, gate := range idx.Gates {
		gates = append(gates, gate)
	}
	sort.Slice(gates, func(i, j int) bool { return stringField(gates[i].Data, "id") < stringField(gates[j].Data, "id") })
	return gates
}

func v7OpenHumanGates(idx v7Index) []Note {
	var gates []Note
	for _, gate := range sortedV7Gates(idx) {
		if v7ProofOwnerClass(stringField(gate.Data, "owner")) == "human" && stringField(gate.Data, "status") == "open" {
			gates = append(gates, gate)
		}
	}
	return gates
}

func v7ReadyAgentTasks(idx v7Index) []Note {
	var tasks []Note
	for _, task := range sortedV7Tasks(idx) {
		if !isV7RunnableAgentTask(task) {
			continue
		}
		tasks = append(tasks, task)
	}
	return tasks
}

func isV7RunnableAgentTask(task Note) bool {
	return len(v7BasicRunnableBlockers(task)) == 0
}

func v7BasicRunnableBlockers(task Note) []string {
	var reasons []string
	if noteListKind(task.Data) != "task" {
		reasons = append(reasons, "kind is not task")
	}
	status := stringField(task.Data, "status")
	if status != "ready" && status != "rework" {
		reasons = append(reasons, "status is "+fallback(status, "(missing)"))
	}
	if stringField(task.Data, "readiness") != "ready" {
		reasons = append(reasons, "readiness is "+fallback(stringField(task.Data, "readiness"), "(missing)"))
	}
	owner := stringField(task.Data, "next_owner")
	if owner != "agent" && !strings.HasPrefix(owner, "agent:") {
		reasons = append(reasons, "next_owner is "+fallback(owner, "(missing)"))
	}
	return reasons
}

func isV7DispatchableAgentTask(vaultPath string, task Note) bool {
	return len(v7TaskDispatchBlockers(vaultPath, task)) == 0
}

func v7TaskDispatchBlockers(vaultPath string, task Note) []string {
	return v7TaskDispatchBlockersWithAuthorization(vaultPath, task, true)
}

func v7TaskDispatchBlockersWithAuthorization(vaultPath string, task Note, includeAuthorizationState bool) []string {
	reasons := append([]string{}, v7BasicRunnableBlockers(task)...)
	if waveID := strings.TrimSpace(stringField(task.Data, "wave")); waveID != "" {
		if idx, err := loadV7Index(vaultPath); err == nil {
			if wave, ok := idx.Waves[waveID]; ok {
				if !containsString(normalizeList(wave.Data["members"]), stringField(task.Data, "id")) {
					reasons = append(reasons, stringField(task.Data, "id")+" is not an authorized member of wave "+waveID)
				} else if includeAuthorizationState {
					auth := waveAuthorizationProjection(vaultPath, idx, wave)
					if state := stringField(auth, "state"); state != "armed" {
						reasons = append(reasons, "wave "+waveID+" authorization is "+state+"; "+stringField(auth, "action"))
					}
				}
			} else {
				reasons = append(reasons, "wave does not resolve: "+waveID)
			}
		} else {
			reasons = append(reasons, "wave authorization cannot be verified: "+err.Error())
		}
	}
	if stub := v7PacketStubAcceptanceItems(task.Body); len(stub) > 0 {
		reasons = append(reasons, "placeholder acceptance: "+strings.Join(stub, ", "))
	}
	if !v7AcceptanceHasProof(task.Body) {
		reasons = append(reasons, "acceptance missing proof mapping")
	}
	verification := sectionContent(task.Body, "## Verification")
	if strings.TrimSpace(verification) == "" {
		reasons = append(reasons, "verification missing")
	} else if !v7TextHasExactVerificationProof(verification) {
		reasons = append(reasons, "verification missing exact command or manual proof ("+v7VerificationGrammarHint+")")
	}
	mode := strings.ToLower(strings.TrimSpace(stringField(task.Data, "proof_mode")))
	if mode == "" {
		reasons = append(reasons, "proof_mode missing")
	} else if mode != "none" && len(normalizeList(task.Data["proof_required"])) == 0 {
		reasons = append(reasons, "proof_required missing")
	}
	for _, warning := range v7PacketDomainRouteWarnings(vaultPath, task) {
		reasons = append(reasons, warning)
	}
	if idx, err := loadV7Index(vaultPath); err == nil {
		if upstreamID, held := v7HeldByFailedUpstream(task, idx); held {
			reasons = append(reasons, v7UpstreamFailureHoldReason(upstreamID))
		}
	} else {
		// Fail closed: if we cannot load the index we cannot rule out an
		// upstream build failure, so block dispatch rather than silently handing
		// out work that may be quarantined.
		reasons = append(reasons, "cannot verify upstream health: "+err.Error())
	}
	return uniqueStrings(reasons)
}

func v7ReviewTasks(idx v7Index) []Note {
	var tasks []Note
	for _, task := range sortedV7Tasks(idx) {
		if stringField(task.Data, "status") == "review" && stringField(task.Data, "readiness") != "waiting_on_human" {
			tasks = append(tasks, task)
		}
	}
	return tasks
}

func v7HumanWaitTasks(idx v7Index) []Note {
	var tasks []Note
	for _, task := range sortedV7Tasks(idx) {
		if stringField(task.Data, "readiness") == "waiting_on_human" || stringField(task.Data, "agent_action") == "stop_until_human_response" {
			tasks = append(tasks, task)
		}
	}
	return tasks
}

func v7IndexRecords(notes []Note) []map[string]any {
	records := make([]map[string]any, 0, len(notes))
	for _, note := range notes {
		record := map[string]any{}
		for key, value := range note.Data {
			record[key] = value
		}
		record["path"] = note.RelativePath
		records = append(records, record)
	}
	return records
}

func pickV7Next(vaultPath, epic, owner string) (Note, bool) {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return Note{}, false
	}
	for _, task := range sortedV7Tasks(idx) {
		if epic != "" && strings.ToUpper(stringField(task.Data, "epic")) != epic {
			continue
		}
		if !isV7DispatchableAgentTask(vaultPath, task) {
			continue
		}
		if owner != "" && stringField(task.Data, "next_owner") != owner {
			continue
		}
		return task, true
	}
	return Note{}, false
}

func validateV7DispatchableTasks(vaultPath string) []Issue {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return []Issue{issue("DISPATCHABLE_INDEX_FAILED", err.Error(), "", "", nil)}
	}
	var errs []Issue
	for _, task := range sortedV7Tasks(idx) {
		status := stringField(task.Data, "status")
		if status != "ready" && status != "rework" {
			continue
		}
		if stringField(task.Data, "readiness") != "ready" {
			continue
		}
		reasons := v7TaskDispatchBlockers(vaultPath, task)
		if len(reasons) == 0 {
			continue
		}
		where := task.RelativePath
		errs = append(errs, issue("TASK_NOT_DISPATCHABLE", stringField(task.Data, "id")+" is marked ready/rework but is not dispatchable: "+strings.Join(reasons, "; "), where, "fix the task contract or move it back to backlog/held", map[string]any{"id": stringField(task.Data, "id"), "dispatch_blockers": reasons}))
	}
	return errs
}

func v7NotesPayload(notes []Note) []map[string]any {
	out := make([]map[string]any, 0, len(notes))
	for _, note := range notes {
		out = append(out, note.Data)
	}
	return out
}

// defaultScaffoldAcceptanceOutcome is the A1 outcome text a freshly created,
// never-edited task carries. It is intentionally a placeholder: the feedback
// signal (v7_feedback_signal.go) matches on it so an unedited default
// acceptance registers as a gap. Keep the template and detector using this same
// constant so the two cannot silently drift apart.
const defaultScaffoldAcceptanceOutcome = "Someone can see the visible result this task delivers."

func v7TaskBody(id, title string) string {
	return fmt.Sprintf(`# %s · %s

## Intent

Write a few plain sentences a former product manager can read cold — no file
names, symbols, or commands here; those live in the appendix below.

- What this is: TBD.
- Why it matters: TBD.
- What "done" looks like: TBD — describe things someone can actually see.

## Acceptance

| ID | Outcome | Proof |
|---|---|---|
| A1 | %s | Inline verification, evidence, gate, or waiver |

## Non-goals

- TBD.

## Implementation notes

Builder appendix — only the person doing the hands-on work opens this. Keep the
plain top section above free of jargon; put the technical detail here instead.

- File map: TBD (verify the paths before editing).
- Moving parts: TBD.
- Exact commands: TBD.

## Verification

Each Check must start with %s or %s.

| Covers | Check | Result | Notes |
|---|---|---|---|
| A1 | command: <exact command that proves A1> | pending | Replace the placeholder with a real command or "manual proof: <steps>" before promoting to ready. |

## Evidence

Accepted:
- None.

Pending:
- None.

## Knowledge delta

None expected.
`, id, title, defaultScaffoldAcceptanceOutcome, "`command: <exact shell command>`", "`manual proof: <exact steps a human runs>`")
}

func v7GateBody(id, title, action, verification string, blocks []string, gateKind, whyAgentCannot, suggestion string) string {
	whySection := ""
	if strings.TrimSpace(whyAgentCannot) != "" {
		whySection = "\n## Why agent cannot do this\n\n" + strings.TrimSpace(whyAgentCannot) + "\n"
	}
	suggestionSection := ""
	if strings.TrimSpace(suggestion) != "" {
		suggestionSection = "\n## Suggested resolution\n\n" + strings.TrimSpace(suggestion) + "\n"
	}
	secretPolicy := ""
	if gateKind == "auth" || gateKind == "env" {
		secretPolicy = "\n## Secret policy\n\nDo not paste OAuth tokens, API keys, passwords, cookies, or session values into Tusker, task notes, logs, screenshots, or chat transcripts.\n"
	}
	return fmt.Sprintf(`# %s · %s
%s
## Action

%s
%s

## Verification

%s
%s
## Unblocks

%s
`, id, title, whySection, action, suggestionSection, verification, secretPolicy, v7BulletList(blocks))
}

func v7ProjectID(vaultPath string) string {
	projectID, err := resolveV7ProjectID(vaultPath)
	if err != nil {
		return ""
	}
	return projectID
}

func resolveV7ProjectID(vaultPath string) (string, error) {
	cfg, configPath, err := readV7TuskerConfig(vaultPath)
	if err != nil {
		return "", tuskerError(errorConfigInvalid, "failed to parse tusker.yaml project identity: "+err.Error(), withPath(configPath))
	}
	if strings.TrimSpace(cfg.ProjectID) != "" {
		return strings.TrimSpace(cfg.ProjectID), nil
	}
	projectPath := filepath.Join(vaultPath, "_system", "project.yaml")
	raw, err := readText(projectPath)
	if err == nil {
		var meta struct {
			ProjectID string `yaml:"project_id"`
			ID        string `yaml:"id"`
		}
		if err := yaml.Unmarshal([]byte(raw), &meta); err != nil {
			return "", tuskerError(errorConfigInvalid, "failed to parse V7 project metadata: "+err.Error(), withPath(projectPath))
		}
		if strings.TrimSpace(meta.ProjectID) != "" {
			return strings.TrimSpace(meta.ProjectID), nil
		}
		if strings.TrimSpace(meta.ID) != "" {
			return strings.TrimSpace(meta.ID), nil
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	repoRoot := filepath.Dir(vaultPath)
	if fileExists(filepath.Join(repoRoot, "tusker.yaml")) || dirExists(filepath.Join(repoRoot, ".git")) || fileExists(filepath.Join(repoRoot, ".git")) {
		return sanitizeProjectID(filepath.Base(repoRoot)), nil
	}
	return "", tuskerError(errorConfigInvalid, "V7 project_id is required in tusker.yaml", withPath(filepath.Join(repoRoot, "tusker.yaml")), withHint("run `tusker init --yes` from the repository root or add project_id to tusker.yaml"))
}

func v7StateRev(data map[string]any, body string) string {
	return v7schema.StateRev(data, body)
}

func v7CanonicalBody(body string) string {
	return strings.TrimRight(strings.TrimLeft(body, "\n"), "\n\t ")
}

func v7StateRevMatches(data map[string]any, body, rev string) bool {
	if strings.TrimSpace(rev) == "" {
		return true
	}
	if v7StateRev(data, body) == rev {
		return true
	}
	if strings.HasSuffix(body, "\n") && v7StateRev(data, strings.TrimSuffix(body, "\n")) == rev {
		return true
	}
	return false
}

func v7TerminalTaskStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "done", "cancelled", "superseded":
		return true
	default:
		return false
	}
}

func guardV7TerminalTaskRewind(filePath, source string, before, after map[string]any) error {
	if effectiveV7Kind(before) != "task" || effectiveV7Kind(after) != "task" {
		return nil
	}
	taskID := stringField(after, "id")
	if taskID == "" || stringField(before, "id") != taskID {
		return nil
	}
	fromStatus := stringField(before, "status")
	toStatus := stringField(after, "status")
	if !v7TerminalTaskStatus(fromStatus) || v7TerminalTaskStatus(toStatus) {
		return nil
	}
	return tuskerError("CAS_CONFLICT",
		fmt.Sprintf("terminal task state rewind refused for %s: %s -> %s", taskID, fromStatus, toStatus),
		withPath(filePath),
		withHint("resolve the task-file conflict explicitly with a Tusker control operation; merge and reconcile never rewind terminal task state"),
		withContext(map[string]any{
			"task":   taskID,
			"from":   fromStatus,
			"to":     toStatus,
			"source": source,
		}),
	)
}

func guardV7ReconcileTerminalTaskStateRevRepair(vaultPath string, note Note, data map[string]any) error {
	if effectiveV7Kind(data) != "task" || v7TerminalTaskStatus(stringField(data, "status")) {
		return nil
	}
	if stringField(data, "closed_at") != "" || stringField(data, "accepted_at") != "" || stringField(data, "accepted_by") != "" {
		return tuskerError("CAS_CONFLICT",
			fmt.Sprintf("terminal task metadata conflicts with non-terminal status during reconcile for %s", stringField(data, "id")),
			withPath(note.AbsolutePath),
			withHint("resolve the task-file conflict explicitly; reconcile will not certify mixed terminal/non-terminal task metadata"),
		)
	}
	repoRoot := filepath.Dir(vaultPath)
	if _, err := gitCombined(repoRoot, "rev-parse", "--is-inside-work-tree"); err != nil {
		return nil
	}
	rel, err := filepath.Rel(repoRoot, note.AbsolutePath)
	if err != nil {
		return err
	}
	headData, ok, err := v7GitFrontmatterAtRef(repoRoot, "HEAD", filepath.ToSlash(rel))
	if err != nil || !ok {
		return err
	}
	return guardV7TerminalTaskRewind(note.AbsolutePath, "reconcile:state_rev_repair", headData, data)
}

func v7GitFrontmatterAtRef(repoRoot, ref, rel string) (map[string]any, bool, error) {
	object := ref + ":" + rel
	if _, err := gitCombined(repoRoot, "cat-file", "-e", object); err != nil {
		return nil, false, nil
	}
	raw, err := gitCombined(repoRoot, "show", object)
	if err != nil {
		return nil, false, err
	}
	data, _, err := parseFrontmatter(raw)
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

type v7LockedDocumentMutation func(data map[string]any, body string) (map[string]any, string, bool, error)

// mutateV7DocumentLocked reloads Markdown only after acquiring the document
// lock, then computes the reconciliation change from that current content.
// Cached Note values are discovery hints and never become write authority.
func mutateV7DocumentLocked(filePath string, order []string, mutate v7LockedDocumentMutation) (string, bool, error) {
	materialLock, err := acquireV7MaterialEpochLockForDocument(filePath)
	if err != nil {
		return "", false, err
	}
	if materialLock != nil {
		defer func() { _ = materialLock.Close() }()
	}
	return mutateV7DocumentUnderMaterialLock(filePath, order, mutate)
}

func mutateV7DocumentUnderMaterialLock(filePath string, order []string, mutate v7LockedDocumentMutation) (string, bool, error) {
	lock, err := acquireV7DocumentLock(filePath, v7DocumentLockTimeout)
	if err != nil {
		return "", false, err
	}
	defer func() { _ = lock.Close() }()

	currentData, currentBody, err := parseFrontmatterMustRead(filePath)
	if err != nil {
		return "", false, err
	}
	nextData, nextBody, changed, err := mutate(currentData, currentBody)
	if err != nil || !changed {
		return stringField(currentData, "state_rev"), false, err
	}
	nextRev := v7StateRev(nextData, nextBody)
	nextData["state_rev"] = nextRev
	content, err := serializeDocument(nextData, nextBody, order)
	if err != nil {
		return "", false, err
	}
	if err := atomicReplaceV7Document(filePath, content); err != nil {
		return "", false, err
	}
	invalidateCachedNote(filePath)
	return nextRev, true, nil
}

func saveV7DocumentCAS(filePath string, data map[string]any, body string, order []string, baseRev string) (string, error) {
	materialLock, err := acquireV7MaterialEpochLockForDocument(filePath)
	if err != nil {
		return "", err
	}
	if materialLock != nil {
		defer func() { _ = materialLock.Close() }()
	}
	return saveV7DocumentCASUnderMaterialLock(filePath, data, body, order, baseRev)
}

func saveV7DocumentCASUnderMaterialLock(filePath string, data map[string]any, body string, order []string, baseRev string) (string, error) {
	lock, err := acquireV7DocumentLock(filePath, v7DocumentLockTimeout)
	if err != nil {
		return "", err
	}
	defer func() { _ = lock.Close() }()

	currentData, currentBody, err := parseFrontmatterMustRead(filePath)
	if err != nil {
		return "", err
	}
	currentRev := stringField(currentData, "state_rev")
	if strings.TrimSpace(currentRev) != "" {
		actualRev := v7StateRev(currentData, currentBody)
		if !v7StateRevMatches(currentData, currentBody, currentRev) {
			return "", tuskerError("CAS_CONFLICT", "V7 object content changed without a refreshed state_rev: "+filepath.Base(filePath), withPath(filePath), withHint("run `tusker reconcile` to repair the object metadata before retrying the control operation"), withContext(map[string]any{"current_rev": currentRev, "actual_rev": actualRev}))
		}
	}
	if strings.TrimSpace(currentRev) != strings.TrimSpace(baseRev) {
		return "", tuskerError("CAS_CONFLICT", "V7 object changed since it was loaded: "+filepath.Base(filePath), withPath(filePath), withHint("reload the object and retry the Tusker control operation"), withContext(map[string]any{"base_rev": baseRev, "current_rev": currentRev}))
	}
	nextData := make(map[string]any, len(data)+1)
	for key, value := range data {
		nextData[key] = value
	}
	nextRev := v7StateRev(nextData, body)
	nextData["state_rev"] = nextRev
	content, err := serializeDocument(nextData, body, order)
	if err != nil {
		return "", err
	}
	if err := atomicReplaceV7Document(filePath, content); err != nil {
		return "", err
	}
	invalidateCachedNote(filePath)
	data["state_rev"] = nextRev
	return nextRev, nil
}

func effectiveV7Kind(data map[string]any) string {
	return v7schema.EffectiveKind(data)
}

func v7EpicFromTaskID(id string) string {
	return v7schema.EpicFromTaskID(id)
}

func v7AcceptanceCount(body string) int {
	return v7schema.AcceptanceCount(body)
}

func v7MaybeCodeBlock(value string) string {
	return v7schema.MaybeCodeBlock(value)
}

func readV7TuskerConfig(vaultPath string) (v7TuskerConfigFile, string, error) {
	configPath := filepath.Join(filepath.Dir(vaultPath), "tusker.yaml")
	var cfg v7TuskerConfigFile
	if !fileExists(configPath) {
		return cfg, configPath, nil
	}
	raw, err := readText(configPath)
	if err != nil {
		return cfg, configPath, err
	}
	var top map[string]any
	if err := yaml.Unmarshal([]byte(raw), &top); err != nil {
		return cfg, configPath, err
	}
	if _, ok := top["orchestration"]; ok {
		return cfg, configPath, tuskerError(errorConfigInvalid, "tusker.yaml uses deprecated top-level orchestration; use automation", withPath(configPath), withHint("rename orchestration: to automation: and keep trigger_states ready,rework"))
	}
	if err := yaml.Unmarshal([]byte(raw), &cfg); err != nil {
		return cfg, configPath, err
	}
	return cfg, configPath, nil
}

func v7BulletList(items []string) string {
	return v7schema.BulletList(items)
}

func v7WikiLinks(items []string) string {
	return v7schema.WikiLinks(items)
}

func currentGitBranch() string {
	branch, err := currentGitBranchIn(mustGetwd())
	if err != nil {
		return ""
	}
	return branch
}

func currentGitBranchIn(repoRoot string) (string, error) {
	out, err := exec.Command("git", "-C", repoRoot, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func v7RepoRoot(vaultPath string) string {
	repoRoot, err := filepath.Abs(filepath.Dir(vaultPath))
	if err != nil {
		return filepath.Dir(vaultPath)
	}
	return repoRoot
}

func ensureV7ControlMutation(vaultPath string, args Args) error {
	if args.Bool("force") || args.Bool("local") {
		return nil
	}
	if v7SingleUserLocalMutationMode(vaultPath) {
		return nil
	}
	repoRoot := v7RepoRoot(vaultPath)
	branch, err := currentGitBranchIn(repoRoot)
	if err != nil || branch == "" || branch == "HEAD" {
		return tuskerError(
			errorInvalidTransition,
			"protected Tusker state requires a checked-out Git branch in team mode",
			withHint(v7ProtectedImplementationFlowHint(args)),
			withContext(map[string]any{"repo_root": repoRoot, "branch": branch}),
		)
	}
	if isV7ControlBranch(vaultPath, branch) {
		return nil
	}
	if v7ReviewerControlMutationAllowed(vaultPath, args, branch) {
		return nil
	}
	return tuskerError(
		errorInvalidTransition,
		"protected Tusker state cannot be mutated from branch "+branch,
		withHint(v7ProtectedImplementationFlowHint(args)),
		withContext(map[string]any{"branch": branch}),
	)
}

func v7ReviewerControlMutationAllowed(vaultPath string, args Args, branch string) bool {
	taskID := strings.ToUpper(strings.TrimSpace(firstNonEmpty(args.String("id"), args.String("_pos0"))))
	actor := strings.ToLower(strings.TrimSpace(firstNonEmpty(args.String("by"), args.String("actor"))))
	runtimeAttemptID := strings.ToUpper(strings.TrimSpace(os.Getenv("TUSKER_ATTEMPT_ID")))
	workspace := strings.TrimSpace(os.Getenv("TUSKER_WORKSPACE"))
	if taskID == "" || runtimeAttemptID == "" || workspace == "" || !strings.HasPrefix(actor, "reviewer:") || !strings.EqualFold(os.Getenv("TUSKER_RUN_LANE"), runLaneReview) {
		return false
	}
	if !strings.EqualFold(branch, v7TaskBranchName(taskID)) || !workspacePathsCompatible(v7RepoRoot(vaultPath), workspace) {
		return false
	}
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return false
	}
	var bound []Note
	for _, attempt := range idx.Attempts[taskID] {
		if strings.EqualFold(stringField(attempt.Data, "runtime_attempt_id"), runtimeAttemptID) {
			bound = append(bound, attempt)
		}
	}
	if len(bound) != 1 {
		return false
	}
	attempt := bound[0]
	return stringField(attempt.Data, "status") == "started" &&
		strings.EqualFold(stringField(attempt.Data, "lane"), runLaneReview) &&
		strings.EqualFold(stringField(attempt.Data, "task"), taskID) &&
		strings.EqualFold(stringField(attempt.Data, "branch"), branch) &&
		workspacePathsCompatible(stringField(attempt.Data, "workspace_path"), workspace)
}

func v7ProtectedImplementationFlowHint(args Args) string {
	taskID := firstNonEmpty(args.String("id"), args.String("_pos0"), "<TASK-ID>")
	if strings.EqualFold(args.String("status"), "active") || strings.EqualFold(args.String("_pos1"), "active") {
		return "no durable `active` status; use `tusker attempt start " + taskID + "` → `tusker verify add " + taskID + " …` → `tusker attempt handoff " + taskID + "` → `tusker finish " + taskID + " --request-review` (auto-proposes on protected branches)."
	}
	return "durable state changes on this branch go through `tusker finish " + taskID + " --request-review` (auto-proposes on implementation branches)."
}

func v7SingleUserLocalMutationMode(vaultPath string) bool {
	cfg, _, err := readV7TuskerConfig(vaultPath)
	if err != nil {
		return false
	}
	for _, mode := range []string{cfg.MutationMode, cfg.Branches.MutationMode, cfg.Runtime.MutationMode} {
		switch normalizeV7MutationMode(mode) {
		case "single_user_local", "local":
			return true
		}
	}
	return false
}

func normalizeV7MutationMode(mode string) string {
	return v7schema.NormalizeMutationMode(mode)
}
