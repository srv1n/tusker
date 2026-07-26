package main

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

const factoryOperationsSchema = "tusker.factory-operations/v1"

var factoryOperationsSectionOrder = []string{
	"delivered",
	"workingNow",
	"reviewOrRework",
	"blocked",
	"needsYourDecision",
	"nextFrontier",
}

var factoryOperationsNow = func() time.Time { return time.Now().UTC() }

// factoryOperationsProjection is the single read-only operations contract
// consumed by `tusker factory operations --json` and Serve. It deliberately
// carries bounded product and control-plane facts, never runner exhaust.
type factoryOperationsProjection struct {
	Schema            string                           `json:"schema"`
	ReadOnly          bool                             `json:"readOnly"`
	GeneratedAt       string                           `json:"generatedAt"`
	Project           factoryOperationsProject         `json:"project"`
	Authority         factoryOperationsAuthority       `json:"authority"`
	Capacity          factoryOperationsCapacity        `json:"capacity"`
	SectionOrder      []string                         `json:"sectionOrder"`
	Delivered         []factoryOperationsItem          `json:"delivered"`
	WorkingNow        []factoryOperationsItem          `json:"workingNow"`
	ReviewOrRework    []factoryOperationsItem          `json:"reviewOrRework"`
	Blocked           []factoryOperationsItem          `json:"blocked"`
	NeedsYourDecision []factoryOperationsHumanDecision `json:"needsYourDecision"`
	NextFrontier      []factoryOperationsItem          `json:"nextFrontier"`
}

type factoryOperationsProject struct {
	ID                   string                            `json:"id"`
	Name                 string                            `json:"name"`
	Registered           bool                              `json:"registered"`
	Enabled              bool                              `json:"enabled"`
	Health               string                            `json:"health"`
	AutomationEnabled    bool                              `json:"automationEnabled"`
	AutomationProvenance string                            `json:"automationProvenance"`
	DispatchScope        automationDispatchScopeProjection `json:"dispatchScope"`
	CompletionMode       completionReactorModeProjection   `json:"completionMode"`
	PromotionMode        factoryOperationsPromotionMode    `json:"promotionMode"`
}

type factoryOperationsPromotionMode struct {
	Configured bool   `json:"configured"`
	Mode       string `json:"mode"`
	Provenance string `json:"provenance"`
	Observe    bool   `json:"observe"`
	Stage      bool   `json:"stage"`
	Promote    bool   `json:"promote"`
	Release    bool   `json:"release"`
}

type factoryOperationsAuthority struct {
	DefaultRef string                           `json:"defaultRef"`
	DefaultSHA string                           `json:"defaultSha,omitempty"`
	Waves      []factoryOperationsWaveAuthority `json:"waves"`
}

type factoryOperationsWaveAuthority struct {
	WaveID                string `json:"waveId"`
	Title                 string `json:"title"`
	State                 string `json:"state"`
	FingerprintHealth     string `json:"fingerprintHealth"`
	CurrentFingerprint    string `json:"currentFingerprint,omitempty"`
	AuthorizedFingerprint string `json:"authorizedFingerprint,omitempty"`
	IntegrationRef        string `json:"integrationRef"`
	IntegrationSHA        string `json:"integrationSha,omitempty"`
	SafeAction            string `json:"safeAction"`
	Href                  string `json:"href"`
}

type factoryOperationsCapacity struct {
	Global        factoryOperationsCapacityLimit  `json:"global"`
	Project       factoryOperationsCapacityLimit  `json:"project"`
	ResourceHolds []factoryOperationsResourceHold `json:"resourceHolds"`
}

type factoryOperationsCapacityLimit struct {
	Active    int `json:"active"`
	Limit     int `json:"limit"`
	Available int `json:"available"`
}

type factoryOperationsResourceHold struct {
	Name      string `json:"name"`
	Purpose   string `json:"purpose"`
	ProjectID string `json:"projectId"`
	TaskID    string `json:"taskId,omitempty"`
}

type factoryOperationsRevisions struct {
	StateRevision     string `json:"stateRevision,omitempty"`
	WorkRevision      int    `json:"workRevision,omitempty"`
	ImplementationSHA string `json:"implementationSha,omitempty"`
	ResultRevision    string `json:"resultRevision,omitempty"`
	IntegrationRef    string `json:"integrationRef,omitempty"`
	IntegrationSHA    string `json:"integrationSha,omitempty"`
	DefaultRef        string `json:"defaultRef,omitempty"`
	DefaultSHA        string `json:"defaultSha,omitempty"`
}

type factoryOperationsItem struct {
	ID                  string                     `json:"id"`
	Kind                string                     `json:"kind"`
	TaskID              string                     `json:"taskId,omitempty"`
	WaveID              string                     `json:"waveId,omitempty"`
	Title               string                     `json:"title"`
	State               string                     `json:"state"`
	ProductOutcome      string                     `json:"productOutcome"`
	Cause               string                     `json:"cause,omitempty"`
	AffectedTaskIDs     []string                   `json:"affectedTaskIds"`
	AutomaticNextAction string                     `json:"automaticNextAction"`
	SafeAction          string                     `json:"safeAction"`
	AcceptedArtifacts   []waveBriefArtifact        `json:"acceptedArtifacts"`
	Revisions           factoryOperationsRevisions `json:"revisions"`
	Href                string                     `json:"href"`
}

type factoryOperationsHumanDecision struct {
	GateID              string   `json:"gateId"`
	Owner               string   `json:"owner"`
	Action              string   `json:"action"`
	Verification        string   `json:"verification"`
	WhyHuman            string   `json:"whyHuman"`
	AffectedTaskIDs     []string `json:"affectedTaskIds"`
	AutomaticNextAction string   `json:"automaticNextAction"`
	SafeAction          string   `json:"safeAction"`
	Href                string   `json:"href"`
}

type factoryOperationsWaveFact struct {
	State                 string
	Stale                 bool
	CurrentFingerprint    string
	AuthorizedFingerprint string
	IntegrationRef        string
	IntegrationSHA        string
}

type factoryOperationsCompletionFact struct {
	Result      ReviewResult
	Transaction *completionTransaction
	Repair      string
}

type factoryOperationsFacts struct {
	VaultPath           string
	RepoRoot            string
	Project             RegisteredProject
	ProjectRegistered   bool
	Workflow            Workflow
	AutomationSource    string
	Index               v7Index
	Runs                map[string]RunStatus
	AllRuns             []RunStatus
	Completions         map[string]factoryOperationsCompletionFact
	Departures          []DepartureRun
	ResourceLeases      []ResourceLease
	GlobalCapacityLimit int
	DefaultRef          string
	DefaultSHA          string
	WaveFacts           map[string]factoryOperationsWaveFact
	Now                 time.Time
}

func factoryOperationsCmd(args Args) error {
	if err := validateFactoryOperationsArgs(args); err != nil {
		return err
	}
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	workflow, err := loadWorkflow(vaultPath)
	if err != nil {
		return err
	}
	store, err := OpenRuntimeStoreReadOnly(DefaultStateRoot())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if store != nil {
		defer store.Close()
	}
	project := RegisteredProject{
		ProjectID:    v7ProjectID(vaultPath),
		ProjectKey:   projectKeyFromPath(v7RepoRoot(vaultPath)),
		Name:         v7ProjectID(vaultPath),
		RepoRoot:     v7RepoRoot(vaultPath),
		VaultRoot:    vaultPath,
		WorkflowPath: workflow.Path,
		Enabled:      false,
		Health:       projectHealthDisabled,
	}
	projection, err := buildFactoryOperations(vaultPath, project, workflow.Data, store, factoryOperationsNow())
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(projection)
		return nil
	}
	fmt.Print(renderFactoryOperations(projection))
	return nil
}

func printFactoryOperationsHelp() {
	fmt.Println(`Usage:
  tusker factory operations [--json]

Purpose:
  Read one bounded operations projection shared with Serve and desktop.

The projection is read-only. It reports delivered work, live work, objective
review/rework, machine blockers, genuine human gates, and the next frontier.
It never dispatches work, changes lifecycle state, or updates Git refs.`)
}

func validateFactoryOperationsArgs(args Args) error {
	for key, value := range args {
		switch key {
		case "json":
			if value != "true" {
				return tuskerError(errorInvalidArg, "factory operations accepts --json as a switch without a value")
			}
		case "_pos", "_pos0", "_pos1", "_pos2":
			return tuskerError(errorInvalidArg, "factory operations accepts no positional arguments")
		default:
			if strings.HasPrefix(key, "_pos") {
				return tuskerError(errorInvalidArg, "factory operations accepts no positional arguments")
			}
			return tuskerError(errorInvalidArg, "factory operations is read-only and does not support --"+key)
		}
	}
	return nil
}

func buildFactoryOperations(vaultPath string, project RegisteredProject, workflow Workflow, store *RuntimeStore, now time.Time) (factoryOperationsProjection, error) {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return factoryOperationsProjection{}, err
	}
	facts := factoryOperationsFacts{
		VaultPath: vaultPath, RepoRoot: firstNonEmpty(project.RepoRoot, v7RepoRoot(vaultPath)),
		Project: project, Workflow: workflow, Index: idx, Runs: map[string]RunStatus{},
		Completions: map[string]factoryOperationsCompletionFact{}, WaveFacts: map[string]factoryOperationsWaveFact{},
		Now: now.UTC(),
	}
	autoReport, reportErr := configResolveForRepo(facts.RepoRoot, true, "automation.enabled")
	if reportErr == nil {
		facts.AutomationSource = autoReport.Source
	}
	if store != nil {
		projects, listErr := store.ListProjects()
		if listErr != nil {
			return factoryOperationsProjection{}, listErr
		}
		for _, registered := range projects {
			if registered.ProjectID == project.ProjectID || sameCleanPath(registered.VaultRoot, vaultPath) {
				facts.Project, facts.ProjectRegistered = registered, true
				break
			}
		}
		facts.AllRuns, err = store.ListRuns()
		if err != nil {
			return factoryOperationsProjection{}, err
		}
		for _, run := range facts.AllRuns {
			if run.ProjectID == facts.Project.ProjectID || run.ProjectID == "" {
				facts.Runs[firstNonEmpty(run.ItemID, run.RecordID)] = run
			}
		}
		facts.Departures, err = store.ListDepartureRuns(facts.Project.ProjectID)
		if err != nil {
			return factoryOperationsProjection{}, err
		}
		facts.ResourceLeases, err = store.ListResourceLeases()
		if err != nil {
			return factoryOperationsProjection{}, err
		}
		facts.GlobalCapacityLimit, err = store.GlobalActiveRunLimit()
		if err != nil {
			return factoryOperationsProjection{}, err
		}
		results, resultsErr := store.ListReviewResults(facts.Project.ProjectID)
		if resultsErr != nil {
			return factoryOperationsProjection{}, resultsErr
		}
		for _, row := range results {
			if row.Repair != nil {
				current, exists := facts.Completions[row.TaskID]
				if !exists || current.Result.WorkRevision <= row.WorkRevision {
					facts.Completions[row.TaskID] = factoryOperationsCompletionFact{
						Result: ReviewResult{TaskID: row.TaskID, WorkRevision: row.WorkRevision},
						Repair: safePacketText(row.Repair.Error(), 320),
					}
				}
				continue
			}
			current, exists := facts.Completions[row.TaskID]
			if exists && current.Result.WorkRevision > row.Result.WorkRevision {
				continue
			}
			transaction, transactionErr := store.CompletionTransactionForResult(row.ProjectID, row.TaskID, row.Result.ResultRevision)
			if transactionErr != nil {
				if errorToIssue(transactionErr).Code == completionRepairRequiredError {
					facts.Completions[row.TaskID] = factoryOperationsCompletionFact{
						Result: row.Result,
						Repair: safePacketText(transactionErr.Error(), 320),
					}
					continue
				}
				return factoryOperationsProjection{}, transactionErr
			}
			facts.Completions[row.TaskID] = factoryOperationsCompletionFact{Result: row.Result, Transaction: transaction}
		}
	}
	if !facts.ProjectRegistered {
		facts.Project.Enabled = false
		facts.Project.Health = projectHealthDisabled
	}
	if facts.GlobalCapacityLimit <= 0 {
		facts.GlobalCapacityLimit = 2
	}
	facts.DefaultRef = v7DefaultBranch(vaultPath)
	facts.DefaultSHA, _ = gitOutputTrim(facts.RepoRoot, "rev-parse", "--verify", facts.DefaultRef)
	for waveID, wave := range idx.Waves {
		auth := waveAuthorizationProjection(vaultPath, idx, wave)
		integrationRef := v7WaveIntegrationBranch(wave)
		integrationSHA, _ := gitOutputTrim(facts.RepoRoot, "rev-parse", "--verify", integrationRef)
		facts.WaveFacts[waveID] = factoryOperationsWaveFact{
			State:                 fallback(stringField(auth, "state"), "disarmed"),
			Stale:                 boolField(auth, "stale"),
			CurrentFingerprint:    stringField(auth, "fingerprint"),
			AuthorizedFingerprint: stringField(auth, "authorizedFingerprint"),
			IntegrationRef:        integrationRef,
			IntegrationSHA:        integrationSHA,
		}
	}
	return composeFactoryOperations(facts), nil
}

func composeFactoryOperations(facts factoryOperationsFacts) factoryOperationsProjection {
	projectID := firstNonEmpty(facts.Project.ProjectID, v7ProjectID(facts.VaultPath))
	registered := facts.ProjectRegistered
	if facts.Project.ProjectID == "" {
		facts.Project.ProjectID = projectID
	}
	if facts.Project.Name == "" {
		facts.Project.Name = projectID
	}
	if !registered {
		facts.Project.Enabled, facts.Project.Health = false, projectHealthDisabled
	}
	projectEnabled, projectHealth := facts.Project.Enabled, firstNonEmptyProjectHealth(facts.Project.Health, projectHealthHealthy)
	promotion := facts.Workflow.ScheduledPromotion.Effective
	projection := factoryOperationsProjection{
		Schema: factoryOperationsSchema, ReadOnly: true, GeneratedAt: facts.Now.UTC().Format(time.RFC3339),
		Project: factoryOperationsProject{
			ID: projectID, Name: facts.Project.Name, Registered: registered, Enabled: projectEnabled,
			Health:            string(projectHealth),
			AutomationEnabled: facts.Workflow.AutomationEnabled, AutomationProvenance: safePacketText(firstNonEmpty(facts.AutomationSource, configSourceBuiltIn), 120),
			DispatchScope: facts.Workflow.DispatchScope, CompletionMode: facts.Workflow.CompletionReactor,
			PromotionMode: factoryOperationsPromotionMode{
				Configured: promotion.Configured, Mode: firstNonEmpty(promotion.Mode, scheduledPromotionDisabled),
				Provenance: safePacketText(promotion.Provenance, 160), Observe: promotion.Observe,
				Stage: promotion.Stage, Promote: promotion.Promote, Release: promotion.Release,
			},
		},
		Authority: factoryOperationsAuthority{DefaultRef: facts.DefaultRef, DefaultSHA: facts.DefaultSHA, Waves: []factoryOperationsWaveAuthority{}},
		Capacity: factoryOperationsCapacity{
			Global:        factoryOperationsCapacityLimit{Limit: facts.GlobalCapacityLimit},
			Project:       factoryOperationsCapacityLimit{Limit: projectActiveRunLimit(facts.Workflow)},
			ResourceHolds: []factoryOperationsResourceHold{},
		},
		SectionOrder: append([]string{}, factoryOperationsSectionOrder...),
		Delivered:    []factoryOperationsItem{}, WorkingNow: []factoryOperationsItem{},
		ReviewOrRework: []factoryOperationsItem{}, Blocked: []factoryOperationsItem{},
		NeedsYourDecision: []factoryOperationsHumanDecision{}, NextFrontier: []factoryOperationsItem{},
	}

	for _, run := range facts.AllRuns {
		if runConsumesDispatchCapacity(run) && runFreshness(&run, facts.Now) == "fresh" {
			projection.Capacity.Global.Active++
			if run.ProjectID == projectID || run.ProjectID == "" {
				projection.Capacity.Project.Active++
			}
		}
	}
	projection.Capacity.Global.Available = maxInt(0, projection.Capacity.Global.Limit-projection.Capacity.Global.Active)
	projection.Capacity.Project.Available = maxInt(0, projection.Capacity.Project.Limit-projection.Capacity.Project.Active)

	heldResources := map[string]ResourceLease{}
	for _, lease := range facts.ResourceLeases {
		if !factoryOperationsResourceLeaseFresh(lease, facts.Now) {
			continue
		}
		taskID, taskLease := fairDispatchResourceRecordID(lease)
		projection.Capacity.ResourceHolds = append(projection.Capacity.ResourceHolds, factoryOperationsResourceHold{
			Name: safePacketText(lease.Name, 120), Purpose: safePacketText(lease.Purpose, 160),
			ProjectID: lease.ProjectID, TaskID: func() string {
				if taskLease {
					return taskID
				}
				return ""
			}(),
		})
		heldResources[lease.Name] = lease
	}
	sort.Slice(projection.Capacity.ResourceHolds, func(i, j int) bool {
		return projection.Capacity.ResourceHolds[i].Name < projection.Capacity.ResourceHolds[j].Name
	})

	waveIDs := make([]string, 0, len(facts.Index.Waves))
	for waveID := range facts.Index.Waves {
		waveIDs = append(waveIDs, waveID)
	}
	sort.Strings(waveIDs)
	for _, waveID := range waveIDs {
		wave := facts.Index.Waves[waveID]
		waveFact := facts.WaveFacts[waveID]
		health := "current"
		if waveFact.Stale || waveFact.State == "stale" {
			health = "stale"
		} else if waveFact.State != "armed" && waveFact.State != "paused" {
			health = "not_armed"
		} else if waveFact.AuthorizedFingerprint == "" {
			health = "not_authorized"
		}
		projection.Authority.Waves = append(projection.Authority.Waves, factoryOperationsWaveAuthority{
			WaveID: waveID, Title: safePacketText(stringField(wave.Data, "title"), 180),
			State: waveFact.State, FingerprintHealth: health,
			CurrentFingerprint: waveFact.CurrentFingerprint, AuthorizedFingerprint: waveFact.AuthorizedFingerprint,
			IntegrationRef: waveFact.IntegrationRef, IntegrationSHA: waveFact.IntegrationSHA,
			SafeAction: factoryOperationsWaveSafeAction(waveID, waveFact.State),
			Href:       waveDeepLink(projectID, waveID),
		})
	}

	decisionTasks := map[string]bool{}
	allTaskIDs := make([]string, 0, len(facts.Index.Tasks))
	for id := range facts.Index.Tasks {
		allTaskIDs = append(allTaskIDs, id)
	}
	sort.Strings(allTaskIDs)
	for _, action := range validWaveHumanActions(facts.Index, allTaskIDs) {
		gate := facts.Index.Gates[action.GateID]
		affected := []string{}
		for _, blockedID := range action.BlockedTaskIDs {
			affected = append(affected, waveDependentClosure(facts.Index, blockedID)...)
		}
		affected = dedupeSortedStrings(affected)
		for _, id := range affected {
			decisionTasks[id] = true
		}
		projection.NeedsYourDecision = append(projection.NeedsYourDecision, factoryOperationsHumanDecision{
			GateID: action.GateID, Owner: safePacketText(stringField(gate.Data, "owner"), 120),
			Action: safePacketText(action.Action, 280), Verification: safePacketText(stringField(gate.Data, "verification"), 280),
			WhyHuman: safePacketText(v7GateBoundaryText(gate), 280), AffectedTaskIDs: affected,
			AutomaticNextAction: "Tusker will re-evaluate the affected closure after the gate record changes.",
			SafeAction:          safePacketText(action.Action, 280), Href: action.GateHref,
		})
	}

	for _, taskID := range allTaskIDs {
		task := facts.Index.Tasks[taskID]
		if strings.EqualFold(stringField(task.Data, "status"), "cancelled") || strings.EqualFold(stringField(task.Data, "status"), "superseded") {
			continue
		}
		if decisionTasks[taskID] {
			continue
		}
		item := factoryOperationsTaskItem(facts, task)
		status := strings.ToLower(stringField(task.Data, "status"))
		run := facts.Runs[taskID]
		switch {
		case factoryOperationsRunStale(run, facts.Now):
			item.State = "stale_run"
			item.Cause = factoryOperationsStaleRunCause(run)
			item.AutomaticNextAction = "Tusker will reconcile the expired lease and permit a safely fenced reclaim according to run ownership policy."
			item.SafeAction = "tusker runs inspect " + taskID + " --json"
			projection.Blocked = append(projection.Blocked, item)
		case status == "done" || status == "closed":
			item.State = factoryOperationsDeliveredState(facts, taskID)
			item.AutomaticNextAction = factoryOperationsDeliveredNextAction(item.State)
			item.SafeAction = "tusker show " + taskID + " --capsule"
			projection.Delivered = append(projection.Delivered, item)
		case runConsumesDispatchCapacity(run) && runFreshness(&run, facts.Now) == "fresh":
			item.SafeAction = "tusker runs inspect " + taskID + " --json"
			if strings.Contains(strings.ToLower(run.Lane), "review") || status == "review" {
				item.State = "in_review"
				item.Cause = "An objective review lease is active for this work revision."
				item.AutomaticNextAction = "Tusker will persist the signed review result and apply the configured completion mode."
				projection.ReviewOrRework = append(projection.ReviewOrRework, item)
			} else {
				item.State = "running"
				item.Cause = "An implementation lease is active."
				item.AutomaticNextAction = "Tusker will continue the claimed implementation and hand the resulting revision to objective review."
				projection.WorkingNow = append(projection.WorkingNow, item)
			}
		case factoryOperationsRunParked(run):
			item.State = "parked"
			item.Cause = safePacketText(firstNonEmpty(run.LastError, "The run is parked by machine policy."), 320)
			item.AutomaticNextAction = "Tusker will keep this task out of dispatch until its recorded machine blocker is repaired."
			item.SafeAction = "tusker runs inspect " + taskID + " --json"
			projection.Blocked = append(projection.Blocked, item)
		case LeaseState(strings.TrimSpace(run.LeaseState)) == LeaseStateRetryQueued:
			item.Cause = safePacketText(firstNonEmpty(run.LastError, "The existing run is queued for a bounded retry."), 320)
			item.SafeAction = "tusker runs inspect " + taskID + " --json"
			if !facts.Project.Enabled || !facts.Workflow.AutomationEnabled {
				item.State = "idle"
				item.Cause = "The existing retry is durable, but background pickup is disabled."
				item.AutomaticNextAction = "Tusker will leave the retry queued until project pickup and workflow automation are enabled."
				projection.NextFrontier = append(projection.NextFrontier, item)
				continue
			}
			if facts.Project.Health != "" && facts.Project.Health != projectHealthHealthy {
				item.State = "blocked"
				item.Cause = "The existing retry is durable, but registered project health is " + string(facts.Project.Health) + "."
				item.AutomaticNextAction = "Tusker will leave the retry queued until the registered project is healthy."
				projection.Blocked = append(projection.Blocked, item)
				continue
			}
			if strings.Contains(strings.ToLower(run.Lane), "review") || status == "review" {
				item.State = "review_queued"
				item.AutomaticNextAction = "Tusker will retry the existing objective review when its retry policy and capacity permit."
				projection.ReviewOrRework = append(projection.ReviewOrRework, item)
			} else {
				item.State = "retry_queued"
				item.AutomaticNextAction = "Tusker will retry the existing implementation when its retry policy and capacity permit."
				projection.NextFrontier = append(projection.NextFrontier, item)
			}
		case status == "review" && factoryOperationsCompletionNeedsRepair(facts.Completions[taskID]):
			item.State = "completion_repair"
			item.Cause = factoryOperationsReviewCause(facts.Completions[taskID])
			item.AutomaticNextAction = "Tusker will keep this revision out of integration until the bounded completion repair is resolved."
			item.SafeAction = "tusker show " + taskID + " --capsule"
			projection.Blocked = append(projection.Blocked, item)
		case status == "review":
			item.State = "in_review"
			item.Cause = factoryOperationsReviewCause(facts.Completions[taskID])
			item.AutomaticNextAction = "Tusker will complete objective review or deterministic integration without asking for a product decision."
			item.SafeAction = "tusker show " + taskID + " --capsule"
			projection.ReviewOrRework = append(projection.ReviewOrRework, item)
		case status == "rework":
			item.State = "rework"
			item.Cause = safePacketText(firstNonEmpty(stringField(task.Data, "next_action"), run.LastError, "Objective review requested machine-addressable changes."), 320)
			item.AutomaticNextAction = "Tusker will claim the recorded rework when authorization, capacity, and resources permit."
			item.SafeAction = "tusker show " + taskID + " --capsule"
			projection.ReviewOrRework = append(projection.ReviewOrRework, item)
		default:
			state, cause, automatic, action, blocked := factoryOperationsFrontierState(facts, task, heldResources, projection.Capacity)
			item.State, item.Cause, item.AutomaticNextAction, item.SafeAction = state, cause, automatic, action
			if blocked {
				projection.Blocked = append(projection.Blocked, item)
			} else {
				projection.NextFrontier = append(projection.NextFrontier, item)
			}
		}
	}

	for _, departure := range facts.Departures {
		item, section := factoryOperationsDepartureItem(facts, departure)
		switch section {
		case "delivered":
			projection.Delivered = append(projection.Delivered, item)
		case "working":
			projection.WorkingNow = append(projection.WorkingNow, item)
		case "blocked":
			projection.Blocked = append(projection.Blocked, item)
		}
	}

	sortFactoryOperationsItems(projection.Delivered)
	sortFactoryOperationsItems(projection.WorkingNow)
	sortFactoryOperationsItems(projection.ReviewOrRework)
	sortFactoryOperationsItems(projection.Blocked)
	sortFactoryOperationsItems(projection.NextFrontier)
	sort.Slice(projection.NeedsYourDecision, func(i, j int) bool {
		return projection.NeedsYourDecision[i].GateID < projection.NeedsYourDecision[j].GateID
	})
	return projection
}

func factoryOperationsTaskItem(facts factoryOperationsFacts, task Note) factoryOperationsItem {
	taskID := stringField(task.Data, "id")
	projectID := firstNonEmpty(stringField(task.Data, "project"), facts.Project.ProjectID)
	waveID := stringField(task.Data, "wave")
	completion := facts.Completions[taskID]
	revisions := factoryOperationsRevisions{
		StateRevision: stringField(task.Data, "state_rev"), WorkRevision: intField(task.Data, "work_revision"),
	}
	if run, ok := facts.Runs[taskID]; ok && run.WorkRevision > revisions.WorkRevision {
		revisions.WorkRevision = run.WorkRevision
	}
	if completion.Result.TaskID != "" {
		revisions.WorkRevision = maxInt(revisions.WorkRevision, completion.Result.WorkRevision)
		revisions.ImplementationSHA = completion.Result.ImplementationSHA
		revisions.ResultRevision = completion.Result.ResultRevision
	}
	if completion.Transaction != nil {
		revisions.IntegrationRef = completion.Transaction.IntegrationRef
		if completionPhaseHasCommittedRef(completion.Transaction.Phase) {
			revisions.IntegrationSHA = completion.Transaction.StagedSHA
		}
		if revisions.ImplementationSHA == "" {
			revisions.ImplementationSHA = completion.Transaction.ImplementationSHA
		}
	}
	if waveFact, ok := facts.WaveFacts[waveID]; ok {
		revisions.IntegrationRef = firstNonEmpty(revisions.IntegrationRef, waveFact.IntegrationRef)
		revisions.IntegrationSHA = firstNonEmpty(waveFact.IntegrationSHA, revisions.IntegrationSHA)
	}
	for _, departure := range facts.Departures {
		if containsString(departure.Candidate.CargoTaskIDs, taskID) && factoryOperationsDepartureCommitted(departure) {
			revisions.DefaultRef = departure.Promotion.CommittedRef
			revisions.DefaultSHA = departure.Promotion.CommittedSHA
		}
	}
	return factoryOperationsItem{
		ID: taskID, Kind: "task", TaskID: taskID, WaveID: waveID,
		Title:           safePacketText(firstNonEmpty(stringField(task.Data, "title"), taskID), 180),
		ProductOutcome:  factoryOperationsProductOutcome(task),
		AffectedTaskIDs: waveDependentClosure(facts.Index, taskID),
		AcceptedArtifacts: func() []waveBriefArtifact {
			return factoryOperationsArtifacts(facts.Index, task)
		}(),
		Revisions: revisions, Href: taskDeepLink(projectID, taskID),
	}
}

func factoryOperationsArtifacts(idx v7Index, task Note) []waveBriefArtifact {
	artifacts := normalizeWaveArtifacts(idx, task)
	if artifacts == nil {
		return []waveBriefArtifact{}
	}
	for i := range artifacts {
		artifacts[i].TaskID = safePacketText(artifacts[i].TaskID, 120)
		artifacts[i].Kind = safePacketText(artifacts[i].Kind, 80)
		artifacts[i].Summary = safePacketText(artifacts[i].Summary, 240)
		artifacts[i].AcceptanceIDs = dedupeSortedStrings(artifacts[i].AcceptanceIDs)
		artifacts[i].EvidenceRef = safePacketText(artifacts[i].EvidenceRef, 160)
		artifacts[i].ArtifactRef = safePacketText(artifacts[i].ArtifactRef, 240)
	}
	return artifacts
}

func factoryOperationsProductOutcome(task Note) string {
	outcomes := []string{}
	for _, row := range serveAcceptanceRows(task) {
		if text := safePacketText(row.Text, 180); text != "" {
			outcomes = append(outcomes, text)
		}
	}
	if len(outcomes) > 3 {
		outcomes = outcomes[:3]
	}
	return safePacketText(firstNonEmpty(strings.Join(outcomes, "; "), stringField(task.Data, "title"), stringField(task.Data, "id")), 420)
}

func factoryOperationsDeliveredState(facts factoryOperationsFacts, taskID string) string {
	for _, departure := range facts.Departures {
		if containsString(departure.Candidate.CargoTaskIDs, taskID) && factoryOperationsDepartureCommitted(departure) {
			return "promoted"
		}
	}
	if completion := facts.Completions[taskID]; completion.Transaction != nil &&
		completion.Transaction.Disposition == "" && completionPhaseHasCommittedRef(completion.Transaction.Phase) {
		return "integrated"
	}
	return "delivered"
}

func factoryOperationsDeliveredNextAction(state string) string {
	switch state {
	case "promoted":
		return "No automatic work remains; the accepted revision is on the configured default branch."
	case "integrated":
		return "Tusker will keep the integrated revision eligible for the configured promotion policy."
	default:
		return "No automatic work remains for this canonical task."
	}
}

func factoryOperationsReviewCause(completion factoryOperationsCompletionFact) string {
	if completion.Repair != "" {
		return completion.Repair
	}
	if completion.Result.TaskID == "" {
		return "Canonical work is awaiting objective review."
	}
	switch completion.Result.Verdict {
	case "changes_requested":
		return safePacketText(firstNonEmpty(strings.Join(completion.Result.Findings, "; "), completion.Result.Summary), 320)
	case "blocked":
		return safePacketText(firstNonEmpty(completion.Result.Blocker, completion.Result.Summary), 320)
	case "pass":
		if completion.Transaction != nil && completion.Transaction.Failure != "" {
			return safePacketText(completion.Transaction.Failure, 320)
		}
		return "Objective review passed; deterministic integration is pending."
	default:
		return "The latest objective review fact is recorded."
	}
}

func factoryOperationsCompletionNeedsRepair(completion factoryOperationsCompletionFact) bool {
	if completion.Repair != "" {
		return true
	}
	return completion.Transaction != nil && completion.Transaction.Disposition == "park" && completion.Transaction.Failure != ""
}

func factoryOperationsRunParked(run RunStatus) bool {
	switch LeaseState(strings.TrimSpace(run.LeaseState)) {
	case LeaseStateParkedNoProgress, LeaseStateParkedBudget, LeaseStateInterrupted:
		return true
	}
	return !run.Terminal && (run.AttemptOutcome == string(AttemptOutcomeBlocked) || run.AttemptOutcome == string(AttemptOutcomeBudgetExceeded))
}

func factoryOperationsRunStale(run RunStatus, now time.Time) bool {
	return runFreshness(&run, now) == "stale"
}

func factoryOperationsStaleRunCause(run RunStatus) string {
	if expiresAt := strings.TrimSpace(run.LeaseExpiresAt); expiresAt != "" {
		return safePacketText("The "+fallback(run.LeaseState, "claimed")+" lease expired at "+expiresAt+" and is not live.", 320)
	}
	return "The claimed run has no valid lease expiry and is not live."
}

func factoryOperationsResourceLeaseFresh(lease ResourceLease, now time.Time) bool {
	if lease.State != resourceLeaseHeld {
		return false
	}
	expires, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(lease.ExpiresAt))
	if err != nil {
		expires, err = time.Parse(time.RFC3339, strings.TrimSpace(lease.ExpiresAt))
	}
	return err == nil && expires.After(now)
}

func factoryOperationsFrontierState(facts factoryOperationsFacts, task Note, held map[string]ResourceLease, capacity factoryOperationsCapacity) (state, cause, automatic, safeAction string, blocked bool) {
	taskID := stringField(task.Data, "id")
	status := strings.ToLower(stringField(task.Data, "status"))
	readiness := strings.ToLower(stringField(task.Data, "readiness"))
	waveID := stringField(task.Data, "wave")
	if status != "ready" && status != "backlog" && status != "draft" {
		return "blocked", safePacketText(firstNonEmpty(stringField(task.Data, "next_action"), "Canonical status is "+fallback(status, "unknown")+"."), 320),
			"Tusker will re-evaluate this item after its canonical task state changes.", "tusker show " + taskID + " --capsule", true
	}
	if readiness != "ready" && status == "ready" {
		return "blocked", safePacketText(firstNonEmpty(stringField(task.Data, "next_action"), "Canonical readiness is "+fallback(readiness, "missing")+"."), 320),
			"Tusker will re-evaluate the affected dependency and gate closure after canonical proof changes.", "tusker show " + taskID + " --capsule", true
	}
	if edge, waiting := v7BlockingDependencyForReadiness(task, facts.Index); waiting {
		return "blocked", "Dependency " + edge.ID + " has not satisfied its canonical proof contract.",
			"Tusker will re-evaluate this downstream closure automatically when the dependency becomes green.", "tusker show " + edge.ID + " --capsule", true
	}
	if gateID, gate, found := factoryOperationsBlockingGate(facts.Index, task); found {
		return "blocked", safePacketText("Gate "+gateID+" is open; owner "+fallback(stringField(gate.Data, "owner"), "unknown")+" must satisfy its recorded proof contract.", 320),
			"Tusker will re-evaluate the affected closure automatically after the gate record changes.", "tusker show " + gateID + " --capsule", true
	}
	if status == "backlog" || status == "draft" {
		return "blocked", safePacketText(firstNonEmpty(stringField(task.Data, "next_action"), "The task is not admitted to the ready frontier."), 320),
			"Tusker will leave backlog work idle until its canonical status becomes ready.", "tusker show " + taskID + " --capsule", true
	}
	nextOwner := stringField(task.Data, "next_owner")
	if nextOwner != "agent" && !strings.HasPrefix(nextOwner, "agent:") {
		return "blocked", "Canonical next owner is " + fallback(nextOwner, "missing") + ", but no valid human decision gate owns this work.",
			"Tusker will keep the task out of machine dispatch until its canonical ownership contract is repaired.", "tusker show " + taskID + " --capsule", true
	}
	if !facts.Project.Enabled {
		return "idle", "The project is not registered and enabled for background pickup.",
			"No background claim will occur; interactive task work remains available.", "tusker projects list --json", false
	}
	if !facts.Workflow.AutomationEnabled {
		return "idle", "Background automation is disabled for this project.",
			"No background claim will occur; interactive task work remains available.", "tusker config resolve automation.enabled --json", false
	}
	if facts.Project.Health != "" && facts.Project.Health != projectHealthHealthy {
		return "blocked", "Registered project health is " + string(facts.Project.Health) + ".",
			"Tusker will keep background pickup stopped until the project registry is healthy.", "tusker projects list --json", true
	}
	if strings.EqualFold(stringField(task.Data, "risk"), "critical") && !factoryOperationsCriticalRiskAuthorized(facts, task) {
		action := "tusker show " + taskID + " --capsule"
		if waveID != "" {
			action = factoryOperationsWaveSafeAction(waveID, facts.WaveFacts[waveID].State)
		}
		return "critical_authorization", "Critical-risk work requires explicit authority from a current armed wave.",
			"Tusker will keep this task out of background dispatch until that authority is current.", action, true
	}
	requiresWaveAuthorization := facts.Workflow.DispatchScope.isArmedWaves() ||
		(facts.Workflow.DispatchScope.preservesLegacyWaveConstraints() && waveID != "")
	if requiresWaveAuthorization && !automationDispatchScopeContinuation(task, facts.Runs) {
		waveFact, ok := facts.WaveFacts[waveID]
		if !ok || waveID == "" {
			return "disarmed", "Dispatch scope requires membership in a currently armed wave.",
				"Tusker will not make a background claim until a valid wave authorization admits this task.", "tusker show " + taskID + " --capsule", true
		}
		if waveFact.Stale || waveFact.State == "stale" {
			return "stale_authorization", "The wave material no longer matches its armed fingerprint.",
				"Tusker will refuse new claims until the wave is preflighted and explicitly re-armed.", factoryOperationsWaveSafeAction(waveID, "stale"), true
		}
		if waveFact.State != "armed" {
			return waveFact.State, "Wave authorization is " + fallback(waveFact.State, "disarmed") + ".",
				"Tusker will not make a background claim until the wave is armed.", factoryOperationsWaveSafeAction(waveID, waveFact.State), true
		}
	}
	for _, name := range fairDispatchNamedResources(task) {
		if lease, occupied := held[name]; occupied {
			holder, taskLease := fairDispatchResourceRecordID(lease)
			if !taskLease || holder != taskID {
				return "waiting_resource", "Named resource " + name + " is currently held.",
					"Tusker will reconsider this task automatically when the resource is released.", "tusker runs inspect " + taskID + " --json", true
			}
		}
	}
	if capacity.Global.Available == 0 || capacity.Project.Available == 0 {
		return "waiting_capacity", "The configured active-run capacity is full.",
			"Tusker will reconsider this ready task automatically when a live run releases capacity.", "tusker runs inspect " + taskID + " --json", false
	}
	return "ready", "Canonical dependencies, gates, authorization, capacity, and resource checks are green.",
		"Tusker will claim this frontier task on a later daemon reconciliation tick.", "tusker automation explain " + taskID + " --json", false
}

func factoryOperationsCriticalRiskAuthorized(facts factoryOperationsFacts, task Note) bool {
	waveID := stringField(task.Data, "wave")
	wave, exists := facts.Index.Waves[waveID]
	waveFact, hasFact := facts.WaveFacts[waveID]
	return exists && hasFact && containsString(normalizeList(wave.Data["members"]), stringField(task.Data, "id")) &&
		waveFact.State == "armed" && !waveFact.Stale
}

func factoryOperationsBlockingGate(idx v7Index, task Note) (string, Note, bool) {
	taskID := stringField(task.Data, "id")
	gateIDs := make([]string, 0, len(idx.Gates))
	for gateID := range idx.Gates {
		gateIDs = append(gateIDs, gateID)
	}
	sort.Strings(gateIDs)
	for _, gateID := range gateIDs {
		gate := idx.Gates[gateID]
		if !strings.EqualFold(stringField(gate.Data, "status"), "open") || !boolField(gate.Data, "blocking") {
			continue
		}
		if containsString(normalizeList(gate.Data["blocks"]), taskID) || containsString(normalizeList(task.Data["gates"]), gateID) {
			return gateID, gate, true
		}
	}
	return "", Note{}, false
}

func factoryOperationsWaveSafeAction(waveID, state string) string {
	switch state {
	case "armed":
		return "tusker wave pause " + waveID + " --reason \"operator requested pause\""
	case "paused":
		return "tusker wave resume " + waveID + " --by human:$USER"
	case "stale":
		return "tusker wave preflight " + waveID + " --json"
	default:
		return "tusker wave preflight " + waveID + " --json"
	}
}

func factoryOperationsDepartureItem(facts factoryOperationsFacts, departure DepartureRun) (factoryOperationsItem, string) {
	taskIDs := dedupeSortedStrings(departure.Candidate.CargoTaskIDs)
	artifacts := []waveBriefArtifact{}
	outcomes := []string{}
	for _, taskID := range taskIDs {
		if task, ok := facts.Index.Tasks[taskID]; ok {
			outcomes = append(outcomes, factoryOperationsProductOutcome(task))
			artifacts = append(artifacts, factoryOperationsArtifacts(facts.Index, task)...)
		}
	}
	item := factoryOperationsItem{
		ID: departure.ID, Kind: "promotion", Title: "Scheduled promotion",
		State: string(departure.State), ProductOutcome: safePacketText(firstNonEmpty(strings.Join(dedupeSortedStrings(outcomes), "; "), fmt.Sprintf("Promote %d accepted task revision(s).", len(taskIDs))), 420),
		Cause:           safePacketText(firstNonEmpty(departure.BlockReason, departure.ExecutionLastError, departure.SkipReason), 320),
		AffectedTaskIDs: taskIDs, AcceptedArtifacts: artifacts,
		Revisions: factoryOperationsRevisions{},
		Href:      projectOpsDeepLink(facts.Project.ProjectID) + "#promotion-" + departure.ID,
	}
	if factoryOperationsDepartureCommitted(departure) {
		item.Revisions.DefaultRef = departure.Promotion.CommittedRef
		item.Revisions.DefaultSHA = departure.Promotion.CommittedSHA
	}
	switch departure.State {
	case DepartureStatePassed:
		switch {
		case factoryOperationsDepartureCommitted(departure):
			item.State = "promotion_committed"
			item.ProductOutcome = safePacketText("The accepted revisions were promoted to "+departure.Promotion.CommittedRef+" at "+departure.Promotion.CommittedSHA+".", 420)
			item.AutomaticNextAction = "No automatic promotion work remains; release still requires its separately configured authority."
		case factoryOperationsDepartureMode(departure) == scheduledPromotionStage:
			item.State = "staged_only"
			item.ProductOutcome = safePacketText("The accepted revisions were staged to integration only; the default ref was not promoted.", 420)
			item.AutomaticNextAction = "No automatic default-ref promotion follows from staged-only mode."
		case factoryOperationsDepartureMode(departure) == scheduledPromotionShadow:
			item.State = "shadow_validated"
			item.ProductOutcome = safePacketText("Shadow validation passed for the accepted revisions; no integration or default ref was changed.", 420)
			item.AutomaticNextAction = "No automatic staging or promotion follows from shadow mode."
		default:
			item.State = "promotion_truth_missing"
			item.Cause = "The terminal departure has no committed default ref/SHA and its policy does not explain a shadow or staged-only result."
			item.AutomaticNextAction = "Tusker will keep this result out of promoted reporting until its durable promotion identity is repaired."
			item.SafeAction = "tusker logbook --scheduled-promotion --json"
			return item, "blocked"
		}
		item.SafeAction = "tusker logbook --scheduled-promotion --json"
		return item, "delivered"
	case DepartureStatePromoted:
		if !factoryOperationsDepartureCommitted(departure) {
			item.State = "promotion_recovery_required"
			item.Cause = "The departure says promotion started but has no committed default ref/SHA."
			item.AutomaticNextAction = "Tusker will keep release and terminalization stopped until the committed promotion identity is recovered."
			item.SafeAction = "tusker logbook --scheduled-promotion --json"
			return item, "blocked"
		}
		item.State = "promotion_committed"
		item.ProductOutcome = safePacketText("The default ref advanced to "+departure.Promotion.CommittedRef+" at "+departure.Promotion.CommittedSHA+"; terminalization or authorized release remains.", 420)
		item.AutomaticNextAction = "Tusker will terminalize this committed promotion or continue separately authorized release work."
		item.SafeAction = "tusker logbook --scheduled-promotion --json"
		return item, "working"
	case DepartureStateBlocked, DepartureStateFailed:
		item.State = "promotion_blocked"
		item.AutomaticNextAction = "Tusker will keep promotion stopped until the recorded machine-owned repair is green."
		item.SafeAction = "tusker logbook --scheduled-promotion --json"
		return item, "blocked"
	case DepartureStateSkipped:
		return factoryOperationsItem{}, ""
	default:
		item.State = "promotion_" + string(departure.State)
		item.AutomaticNextAction = "Tusker will continue the deterministic promotion transaction from its recorded phase."
		item.SafeAction = "tusker logbook --scheduled-promotion --json"
		return item, "working"
	}
}

func factoryOperationsDepartureCommitted(departure DepartureRun) bool {
	return strings.TrimSpace(departure.Promotion.CommittedRef) != "" &&
		strings.TrimSpace(departure.Promotion.CommittedSHA) != ""
}

func factoryOperationsDepartureMode(departure DepartureRun) string {
	prefix := fmt.Sprintf("scheduled-promotion/v%d/", scheduledPromotionPolicyVersion)
	policyID := strings.TrimSpace(departure.PolicyID)
	if strings.HasPrefix(policyID, prefix) {
		if mode := strings.TrimPrefix(policyID, prefix); mode != "" {
			return mode
		}
	}
	return ""
}

func sortFactoryOperationsItems(items []factoryOperationsItem) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Kind != items[j].Kind {
			return items[i].Kind < items[j].Kind
		}
		return items[i].ID < items[j].ID
	})
}

func firstNonEmptyProjectHealth(values ...ProjectHealth) ProjectHealth {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func renderFactoryOperations(projection factoryOperationsProjection) string {
	var out strings.Builder
	fmt.Fprintf(&out, "# Factory operations · %s\n\n", projection.Project.Name)
	fmt.Fprintf(&out, "Registry: registered=%t enabled=%t health=%s · automation: %t (%s)\n",
		projection.Project.Registered, projection.Project.Enabled, projection.Project.Health,
		projection.Project.AutomationEnabled, projection.Project.AutomationProvenance,
	)
	fmt.Fprintf(&out, "Dispatch: configured=%s effective=%s provenance=%s · completion: configured=%s effective=%s provenance=%s\n",
		fallback(projection.Project.DispatchScope.Configured, "unset"),
		projection.Project.DispatchScope.Effective, projection.Project.DispatchScope.Provenance,
		fallback(projection.Project.CompletionMode.Configured, "unset"),
		projection.Project.CompletionMode.Effective, projection.Project.CompletionMode.Provenance)
	renderFactoryOperationsModeAdvisory(&out, "Dispatch scope", projection.Project.DispatchScope.Warning, projection.Project.DispatchScope.Repair)
	renderFactoryOperationsModeAdvisory(&out, "Completion reactor", projection.Project.CompletionMode.Warning, projection.Project.CompletionMode.Repair)
	fmt.Fprintf(&out, "Promotion: mode=%s configured=%t provenance=%s observe=%t stage=%t promote=%t release=%t\n",
		projection.Project.PromotionMode.Mode, projection.Project.PromotionMode.Configured,
		fallback(projection.Project.PromotionMode.Provenance, "default"),
		projection.Project.PromotionMode.Observe, projection.Project.PromotionMode.Stage,
		projection.Project.PromotionMode.Promote, projection.Project.PromotionMode.Release)
	fmt.Fprintf(&out, "Capacity: project %d/%d active (%d free) · global %d/%d active (%d free)\n\n",
		projection.Capacity.Project.Active, projection.Capacity.Project.Limit, projection.Capacity.Project.Available,
		projection.Capacity.Global.Active, projection.Capacity.Global.Limit, projection.Capacity.Global.Available)

	out.WriteString("### Authorization and resources\n\n")
	if len(projection.Authority.Waves) == 0 {
		out.WriteString("- No waves.\n")
	}
	for _, wave := range projection.Authority.Waves {
		fmt.Fprintf(&out, "- %s · %s/%s · %s", wave.WaveID, wave.State, wave.FingerprintHealth, wave.IntegrationRef)
		if wave.IntegrationSHA != "" {
			out.WriteString(" @ " + wave.IntegrationSHA)
		}
		fmt.Fprintf(&out, "\n  Fingerprints: current=%s authorized=%s\n  Safe action: %s\n",
			fallback(wave.CurrentFingerprint, "-"), fallback(wave.AuthorizedFingerprint, "-"), wave.SafeAction)
	}
	for _, hold := range projection.Capacity.ResourceHolds {
		fmt.Fprintf(&out, "- Resource %s held for %s", hold.Name, hold.Purpose)
		if hold.TaskID != "" {
			out.WriteString(" · " + hold.TaskID)
		}
		out.WriteString("\n")
	}
	out.WriteString("\n")

	sections := []struct {
		title string
		items []factoryOperationsItem
	}{
		{"Delivered", projection.Delivered},
		{"Working now", projection.WorkingNow},
		{"In review or rework", projection.ReviewOrRework},
		{"Blocked", projection.Blocked},
	}
	for _, section := range sections {
		fmt.Fprintf(&out, "## %s\n\n", section.title)
		renderFactoryOperationsItems(&out, section.items)
		out.WriteString("\n")
	}
	out.WriteString("## Needs your decision\n\n")
	if len(projection.NeedsYourDecision) == 0 {
		out.WriteString("- None.\n\n")
	} else {
		for _, decision := range projection.NeedsYourDecision {
			fmt.Fprintf(&out, "- %s · %s — %s\n", decision.GateID, decision.Owner, decision.Action)
			fmt.Fprintf(&out, "  Why human: %s\n  Verify: %s\n", decision.WhyHuman, decision.Verification)
			fmt.Fprintf(&out, "  Affected: %s\n  Automatic next: %s\n  Safe action: %s\n",
				strings.Join(decision.AffectedTaskIDs, ", "), decision.AutomaticNextAction, decision.SafeAction)
		}
		out.WriteString("\n")
	}
	out.WriteString("## Next frontier\n\n")
	renderFactoryOperationsItems(&out, projection.NextFrontier)
	return out.String()
}

func renderFactoryOperationsModeAdvisory(out *strings.Builder, label, warning, repair string) {
	if warning == "" && repair == "" {
		return
	}
	if warning != "" {
		fmt.Fprintf(out, "%s warning: %s\n", label, warning)
	}
	if repair != "" {
		fmt.Fprintf(out, "%s repair: %s\n", label, repair)
	}
}

func renderFactoryOperationsItems(out *strings.Builder, items []factoryOperationsItem) {
	if len(items) == 0 {
		out.WriteString("- None.\n")
		return
	}
	for _, item := range items {
		fmt.Fprintf(out, "- %s · %s — %s\n", item.ID, item.State, item.ProductOutcome)
		if item.Cause != "" {
			fmt.Fprintf(out, "  Cause: %s\n", item.Cause)
		}
		if len(item.AffectedTaskIDs) > 0 {
			fmt.Fprintf(out, "  Affected: %s\n", strings.Join(item.AffectedTaskIDs, ", "))
		}
		fmt.Fprintf(out, "  Automatic next: %s\n  Safe action: %s\n", item.AutomaticNextAction, item.SafeAction)
		for _, artifact := range item.AcceptedArtifacts {
			fmt.Fprintf(out, "  Artifact: %s · %s · %s", artifact.Kind, artifact.EvidenceRef, artifact.Summary)
			if artifact.ArtifactRef != "" {
				out.WriteString(" · " + artifact.ArtifactRef)
			}
			out.WriteString("\n")
		}
		revisions := []string{}
		if item.Revisions.StateRevision != "" {
			revisions = append(revisions, "state="+item.Revisions.StateRevision)
		}
		if item.Revisions.WorkRevision > 0 {
			revisions = append(revisions, fmt.Sprintf("work=%d", item.Revisions.WorkRevision))
		}
		if item.Revisions.ImplementationSHA != "" {
			revisions = append(revisions, "implementation="+item.Revisions.ImplementationSHA)
		}
		if item.Revisions.ResultRevision != "" {
			revisions = append(revisions, "review="+item.Revisions.ResultRevision)
		}
		if item.Revisions.IntegrationRef != "" || item.Revisions.IntegrationSHA != "" {
			revisions = append(revisions, "integration="+firstNonEmpty(item.Revisions.IntegrationRef, "-")+"@"+firstNonEmpty(item.Revisions.IntegrationSHA, "-"))
		}
		if item.Revisions.DefaultRef != "" || item.Revisions.DefaultSHA != "" {
			revisions = append(revisions, "default="+firstNonEmpty(item.Revisions.DefaultRef, "-")+"@"+firstNonEmpty(item.Revisions.DefaultSHA, "-"))
		}
		if len(revisions) > 0 {
			fmt.Fprintf(out, "  Revisions: %s\n", strings.Join(revisions, " · "))
		}
	}
}
