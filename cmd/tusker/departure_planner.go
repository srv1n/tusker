package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"
)

const departurePlannerSchema = "tusker.departure-decision/v1"

type DepartureReason struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type DepartureTaskFact struct {
	ID               string `json:"id"`
	State            string `json:"state"`
	StateRevision    string `json:"state_revision"`
	SourceSHA        string `json:"source_sha,omitempty"`
	ProofFingerprint string `json:"proof_fingerprint"`
	WaveID           string `json:"wave_id,omitempty"`
	EligibleForCargo bool   `json:"eligible_for_cargo"`
}

type DepartureWaveFact struct {
	ID                       string `json:"id"`
	Authorization            string `json:"authorization"`
	AuthorizationFingerprint string `json:"authorization_fingerprint,omitempty"`
	AuthorizationStale       bool   `json:"authorization_stale"`
	IntegrationRef           string `json:"integration_ref,omitempty"`
	ImplicitSingleton        bool   `json:"implicit_singleton,omitempty"`
}

type DepartureFetchFact struct {
	Attempted bool   `json:"attempted"`
	Remote    string `json:"remote,omitempty"`
	Ref       string `json:"ref,omitempty"`
	Succeeded bool   `json:"succeeded"`
	Error     string `json:"error,omitempty"`
}

type DepartureRefFact struct {
	WaveID string `json:"wave_id,omitempty"`
	Name   string `json:"name"`
	SHA    string `json:"sha,omitempty"`
}

type DepartureDecision struct {
	Schema          string                       `json:"schema"`
	ProjectID       string                       `json:"project_id"`
	Policy          ScheduledPromotionProjection `json:"policy"`
	Disposition     string                       `json:"disposition"`
	Reasons         []DepartureReason            `json:"reasons"`
	Tasks           []DepartureTaskFact          `json:"tasks"`
	Waves           []DepartureWaveFact          `json:"waves"`
	Candidate       DepartureCandidate           `json:"candidate"`
	DefaultRef      DepartureRefFact             `json:"default_ref"`
	IntegrationRefs []DepartureRefFact           `json:"integration_refs"`
	GateIntent      DepartureGate                `json:"gate_intent"`
	ResourceNeeds   []string                     `json:"resource_needs"`
	ReleaseEligible bool                         `json:"release_eligible"`
	Fetch           DepartureFetchFact           `json:"fetch"`
}

type departurePlanner struct {
	fetch      func(context.Context, string, string, string) error
	rev        func(string, string) (string, bool)
	remote     func(string) (string, bool)
	now        func() time.Time
	gateLookup func(projectID, treeHash string, commands []string, profile, toolchain string) bool
}

func defaultDeparturePlanner() departurePlanner {
	return departurePlanner{fetch: departureFetch, rev: gitRevParse, remote: departureRemote, now: time.Now, gateLookup: departureGateLedgerHit}
}

// PlanDeparture is read-only apart from the deliberately bounded refresh of a
// configured remote-tracking ref in shadow (or a more permissive) mode.
func (p departurePlanner) PlanDeparture(vaultPath, projectID string, wf WorkflowFile) (DepartureDecision, error) {
	policy := wf.Data.ScheduledPromotion.Effective
	decision := DepartureDecision{Schema: departurePlannerSchema, ProjectID: projectID, Policy: policy, Reasons: []DepartureReason{}, Tasks: []DepartureTaskFact{}, Waves: []DepartureWaveFact{}, IntegrationRefs: []DepartureRefFact{}, ResourceNeeds: []string{}, Candidate: DepartureCandidate{TaskStateRevisions: map[string]string{}, TaskSourceSHAs: map[string]string{}}}
	if !policy.Observe {
		decision.Disposition = "disabled"
		decision.Reasons = append(decision.Reasons, DepartureReason{Code: "scheduled_promotion_disabled", Message: "Scheduled promotion is off; nothing will be staged or promoted."})
		return decision, nil
	}

	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return decision, err
	}
	repoRoot := v7RepoRoot(vaultPath)
	defaultBranch := firstNonEmpty(strings.TrimSpace(wf.Data.Orchestration.DefaultBranch), "main")
	for _, task := range sortedV7Tasks(idx) {
		fact := departureTaskFact(task)
		decision.Tasks = append(decision.Tasks, fact)
		decision.Candidate.TaskStateRevisions[fact.ID] = fact.StateRevision
		if fact.SourceSHA != "" {
			decision.Candidate.TaskSourceSHAs[fact.ID] = fact.SourceSHA
		}
		if fact.EligibleForCargo {
			decision.Candidate.CargoTaskIDs = append(decision.Candidate.CargoTaskIDs, fact.ID)
			if fact.WaveID != "" {
				decision.Candidate.WaveIDs = append(decision.Candidate.WaveIDs, fact.WaveID)
			}
		}
	}
	decision.Candidate.WaveIDs = uniqueDepartureStrings(decision.Candidate.WaveIDs)
	for _, wave := range sortedV7Waves(idx) {
		decision.Waves = append(decision.Waves, departureWaveFact(vaultPath, idx, wave))
	}
	cargoWaves := map[string]bool{}
	for _, task := range decision.Tasks {
		if task.EligibleForCargo && task.WaveID != "" {
			cargoWaves[task.WaveID] = true
		}
	}
	// Exact landing audits are durable source provenance too. Use them only as
	// positive discovery for an authorized, fully completed delivery unit;
	// incomplete or unrelated waves must not become cargo by historical
	// membership alone.
	for _, fact := range decision.Waves {
		if cargoWaves[fact.ID] ||
			(!fact.ImplicitSingleton && (fact.Authorization != "armed" || fact.AuthorizationStale)) {
			continue
		}
		wave := idx.Waves[fact.ID]
		if departureWaveDiscoverableFromExactAudits(repoRoot, fact.IntegrationRef, wave, idx, p.rev) {
			cargoWaves[fact.ID] = true
			decision.Candidate.WaveIDs = append(decision.Candidate.WaveIDs, fact.ID)
		}
	}
	decision.Candidate.WaveIDs = uniqueDepartureStrings(decision.Candidate.WaveIDs)
	remote, hasRemote := p.remote(repoRoot)
	if hasRemote {
		decision.Fetch = DepartureFetchFact{Attempted: true, Remote: remote, Ref: defaultBranch}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := p.fetch(ctx, repoRoot, remote, defaultBranch)
		cancel()
		if err != nil {
			decision.Fetch.Error = err.Error()
			decision.Disposition = "indeterminate"
			decision.Reasons = append(decision.Reasons, DepartureReason{Code: "remote_refresh_failed", Message: "Cargo is indeterminate because the configured remote could not be refreshed."})
			return decision, nil
		}
		decision.Fetch.Succeeded = true
	}
	decision.DefaultRef.Name = defaultBranch
	if sha, ok := p.rev(repoRoot, "refs/remotes/"+remote+"/"+defaultBranch); ok {
		decision.Candidate.ExpectedDefaultBranchSHA = sha
	} else if sha, ok := p.rev(repoRoot, defaultBranch); ok {
		decision.Candidate.ExpectedDefaultBranchSHA = sha
	}
	for _, fact := range decision.Tasks {
		if !fact.EligibleForCargo || fact.WaveID != "" {
			continue
		}
		task := idx.Tasks[fact.ID]
		if err := scheduledPromotionTaskAcceptedReview(vaultPath, task); err != nil {
			decision.Disposition = "blocked"
			decision.Reasons = append(decision.Reasons, DepartureReason{Code: "task_review_provenance_invalid", Message: err.Error()})
			return decision, nil
		}
		sourceSHA, sourceErr := scheduledPromotionExactTaskSourceSHA(repoRoot, "", Note{}, task, p.rev)
		if sourceErr != nil {
			decision.Disposition = "blocked"
			decision.Reasons = append(decision.Reasons, DepartureReason{Code: "task_source_unavailable", Message: sourceErr.Error()})
			return decision, nil
		}
		decision.Candidate.TaskSourceSHAs[fact.ID] = sourceSHA
	}
	for _, fact := range decision.Waves {
		ref := DepartureRefFact{WaveID: fact.ID, Name: fact.IntegrationRef}
		if sha, ok := p.rev(repoRoot, fact.IntegrationRef); ok {
			ref.SHA = sha
		}
		decision.IntegrationRefs = append(decision.IntegrationRefs, ref)
		if cargoWaves[fact.ID] && fact.IntegrationRef != "" && decision.Candidate.IntegrationBaseSHA == "" {
			if ref.SHA != "" {
				decision.Candidate.IntegrationBaseSHA = ref.SHA
			}
		}
		if !cargoWaves[fact.ID] {
			continue
		}
		wave := idx.Waves[fact.ID]
		members := normalizeList(wave.Data["members"])
		if len(members) == 0 {
			decision.Disposition = "blocked"
			decision.Reasons = append(decision.Reasons, DepartureReason{Code: "wave_members_missing", Message: "Cargo is blocked because wave " + fact.ID + " has no atomic member set."})
			return decision, nil
		}
		for _, taskFact := range decision.Tasks {
			if taskFact.EligibleForCargo && taskFact.WaveID == fact.ID && !containsString(members, taskFact.ID) {
				decision.Disposition = "blocked"
				decision.Reasons = append(decision.Reasons, DepartureReason{Code: "wave_membership_mismatch", Message: "Cargo is blocked because done task " + taskFact.ID + " points at wave " + fact.ID + " but is not in its member set."})
				return decision, nil
			}
		}
		memberSources := make(map[string]string, len(members))
		for _, taskID := range members {
			task, ok := idx.Tasks[taskID]
			if !ok {
				decision.Disposition = "blocked"
				decision.Reasons = append(decision.Reasons, DepartureReason{Code: "wave_member_missing", Message: "Cargo is blocked because wave " + fact.ID + " references missing task " + taskID + "."})
				return decision, nil
			}
			if strings.TrimSpace(stringField(task.Data, "wave")) != fact.ID {
				decision.Disposition = "blocked"
				decision.Reasons = append(decision.Reasons, DepartureReason{Code: "wave_member_assignment_mismatch", Message: "Cargo is blocked because wave " + fact.ID + " member " + taskID + " is assigned to another delivery unit."})
				return decision, nil
			}
			if strings.ToLower(strings.TrimSpace(stringField(task.Data, "status"))) != "done" {
				decision.Disposition = "blocked"
				decision.Reasons = append(decision.Reasons, DepartureReason{Code: "wave_member_not_done", Message: "Cargo is blocked because ordinary wave " + fact.ID + " member " + taskID + " is not done."})
				return decision, nil
			}
			if reviewErr := scheduledPromotionTaskAcceptedReview(vaultPath, task); reviewErr != nil {
				decision.Disposition = "blocked"
				decision.Reasons = append(decision.Reasons, DepartureReason{Code: "wave_member_review_provenance_invalid", Message: reviewErr.Error()})
				return decision, nil
			}
			sourceSHA, sourceErr := scheduledPromotionExactTaskSourceSHA(repoRoot, fact.IntegrationRef, wave, task, p.rev)
			if sourceErr != nil {
				decision.Disposition = "blocked"
				decision.Reasons = append(decision.Reasons, DepartureReason{Code: "wave_member_source_unavailable", Message: sourceErr.Error()})
				return decision, nil
			}
			memberSources[taskID] = sourceSHA
		}
		// Membership supplies atomic scope, never eligibility. Only after every
		// member independently proves done + accepted review + immutable source
		// provenance does the whole wave become cargo.
		for _, taskID := range members {
			decision.Candidate.CargoTaskIDs = append(decision.Candidate.CargoTaskIDs, taskID)
			decision.Candidate.TaskStateRevisions[taskID] = stringField(idx.Tasks[taskID].Data, "state_rev")
			sourceSHA := memberSources[taskID]
			decision.Candidate.TaskSourceSHAs[taskID] = sourceSHA
		}
	}
	decision.Candidate.CargoTaskIDs = uniqueDepartureStrings(decision.Candidate.CargoTaskIDs)
	decision.Candidate.WaveIDs = uniqueDepartureStrings(decision.Candidate.WaveIDs)
	decision.DefaultRef.SHA = decision.Candidate.ExpectedDefaultBranchSHA
	if len(decision.Candidate.CargoTaskIDs) > 0 {
		decision.ResourceNeeds = append(decision.ResourceNeeds, "gate:full")
	}
	decision.ResourceNeeds = uniqueDepartureStrings(decision.ResourceNeeds)
	decision.GateIntent = departureGateIntent(wf.Data, repoRoot, firstNonEmpty(decision.Candidate.IntegrationBaseSHA, decision.Candidate.ExpectedDefaultBranchSHA))
	decision.ReleaseEligible = policy.Release && len(decision.ResourceNeeds) > 0 && decision.Candidate.ExpectedDefaultBranchSHA != ""

	if !hasRemote {
		decision.Disposition = "indeterminate"
		decision.Reasons = append(decision.Reasons, DepartureReason{Code: "remote_not_configured", Message: "Cargo is indeterminate because no Git remote is configured."})
		return decision, nil
	}
	if len(decision.ResourceNeeds) == 0 {
		decision.Disposition = "empty"
		decision.Reasons = append(decision.Reasons, DepartureReason{Code: "no_eligible_cargo", Message: "No reviewed work is ready for the next departure."})
		return decision, nil
	}
	if stale := firstStaleDepartureWave(decision.Waves, cargoWaves); stale != "" {
		decision.Disposition = "blocked"
		decision.Reasons = append(decision.Reasons, DepartureReason{Code: "stale_wave_authorization", Message: "Cargo is blocked because wave " + stale + " authorization is stale."})
		return decision, nil
	}
	if held := firstHeldDepartureWave(decision.Waves, cargoWaves); held != "" {
		decision.Disposition = "blocked"
		decision.Reasons = append(decision.Reasons, DepartureReason{Code: "wave_authorization_held", Message: "Cargo is held because wave " + held + " is not authorized for departure."})
		return decision, nil
	}
	if commands := departureGateCommands(wf.Data); p.gateLookup != nil && len(commands) > 0 && decision.GateIntent.TreeHash != "" && p.gateLookup(projectID, decision.GateIntent.TreeHash, commands, decision.GateIntent.Profile, decision.GateIntent.Toolchain) {
		decision.GateIntent.Status = "already_passed"
	}
	if decision.GateIntent.Status == "already_passed" {
		decision.Disposition = "already_gated"
		decision.Reasons = append(decision.Reasons, DepartureReason{Code: "matching_gate_ledger", Message: "The frozen candidate already has matching full-gate proof."})
		return decision, nil
	}
	decision.Disposition = "ready"
	decision.Reasons = append(decision.Reasons, DepartureReason{Code: "eligible_cargo", Message: "Reviewed work is ready to be evaluated at the next departure."})
	return decision, nil
}

func departureTaskFact(task Note) DepartureTaskFact {
	state := stringField(task.Data, "status")
	fact := DepartureTaskFact{ID: stringField(task.Data, "id"), State: state, StateRevision: stringField(task.Data, "state_rev"), SourceSHA: firstNonEmpty(stringField(task.Data, "source_sha"), stringField(task.Data, "source_commit"), stringField(task.Data, "source_branch_sha")), WaveID: stringField(task.Data, "wave")}
	fact.EligibleForCargo = state == "done" && fact.SourceSHA != ""
	fact.ProofFingerprint = departureFingerprint(fact.ID, fact.StateRevision, fact.SourceSHA, state)
	return fact
}

func departureWaveDiscoverableFromExactAudits(repoRoot, integrationBranch string, wave Note, idx v7Index, resolve func(string, string) (string, bool)) bool {
	members := uniqueDepartureStrings(normalizeList(wave.Data["members"]))
	if len(members) == 0 {
		return false
	}
	if resolve == nil {
		resolve = gitRevParse
	}
	for _, taskID := range members {
		task, ok := idx.Tasks[taskID]
		if !ok ||
			strings.TrimSpace(stringField(task.Data, "wave")) != stringField(wave.Data, "id") ||
			!strings.EqualFold(strings.TrimSpace(stringField(task.Data, "status")), "done") {
			return false
		}
		if _, authenticated := authenticatedV7LandingAuditSource(repoRoot, integrationBranch, wave, taskID, resolve); !authenticated {
			return false
		}
	}
	return true
}

func departureWaveFact(vaultPath string, idx v7Index, wave Note) DepartureWaveFact {
	projection := waveAuthorizationProjection(vaultPath, idx, wave)
	auth := stringField(projection, "state")
	return DepartureWaveFact{ID: stringField(wave.Data, "id"), Authorization: auth, AuthorizationFingerprint: stringField(projection, "fingerprint"), AuthorizationStale: boolField(projection, "stale") || auth == "stale", IntegrationRef: firstNonEmpty(stringField(wave.Data, "integration_branch"), "integration/"+stringField(wave.Data, "id")), ImplicitSingleton: v7ImplicitDeliveryUnit(wave)}
}

func departureGateIntent(wf Workflow, repoRoot, treeHash string) DepartureGate {
	commands := departureGateCommands(wf)
	command := strings.Join(commands, " && ")
	return DepartureGate{Command: command, Profile: firstNonEmpty(wf.Orchestration.Gate.Profile, wf.Orchestration.BatchGate.FeatureProfile), Toolchain: scheduledPromotionToolchainFingerprint(repoRoot, commands), TreeHash: treeHash, Status: "required"}
}

func departureGateCommands(wf Workflow) []string {
	commands := wf.Orchestration.Gate.HarvestCommands
	if len(commands) == 0 {
		commands = wf.Orchestration.BatchGate.Commands
	}
	seen := map[string]bool{}
	var normalized []string
	for _, command := range commands {
		command = strings.TrimSpace(command)
		if command != "" && !seen[command] {
			normalized, seen[command] = append(normalized, command), true
		}
	}
	return normalized
}

func departureFingerprint(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(h[:])
}
func uniqueDepartureStrings(items []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if !seen[item] {
			seen[item] = true
			out = append(out, item)
		}
	}
	sort.Strings(out)
	return out
}
func firstStaleDepartureWave(waves []DepartureWaveFact, cargoWaves map[string]bool) string {
	for _, wave := range waves {
		if cargoWaves[wave.ID] && wave.AuthorizationStale {
			return wave.ID
		}
	}
	return ""
}

func firstHeldDepartureWave(waves []DepartureWaveFact, cargoWaves map[string]bool) string {
	for _, wave := range waves {
		if cargoWaves[wave.ID] && !wave.ImplicitSingleton && wave.Authorization != "armed" {
			return wave.ID
		}
	}
	return ""
}

func departureRemote(repoRoot string) (string, bool) {
	out, err := exec.Command("git", "-C", repoRoot, "remote").Output()
	if err != nil {
		return "", false
	}
	for _, remote := range strings.Fields(string(out)) {
		return remote, true
	}
	return "", false
}
func departureFetch(ctx context.Context, repoRoot, remote, branch string) error {
	out, err := exec.CommandContext(ctx, "git", "-C", repoRoot, "fetch", "--quiet", remote, "refs/heads/"+branch+":refs/remotes/"+remote+"/"+branch).CombinedOutput()
	if err != nil {
		return fmt.Errorf("git fetch %s/%s: %s", remote, branch, strings.TrimSpace(string(out)))
	}
	return nil
}

func departureGateLedgerHit(projectID, treeHash string, commands []string, profile, toolchain string) bool {
	if strings.TrimSpace(toolchain) == "" || len(commands) == 0 {
		return false
	}
	if !fileExists(runtimeStoreDBPath(DefaultStateRoot())) {
		return false
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return false
	}
	defer store.Close()
	return gateLedgerCommandsHit(store, projectID, treeHash, commands, profile, toolchain)
}

func gateLedgerCommandsHit(store *RuntimeStore, projectID, treeHash string, commands []string, profile, toolchain string) bool {
	if store == nil || strings.TrimSpace(toolchain) == "" || len(commands) == 0 {
		return false
	}
	for _, command := range commands {
		entry, err := store.FindGateLedger(projectID, treeHash, command, profile, toolchain)
		if err != nil || entry == nil {
			return false
		}
	}
	return true
}
