package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const deliveryReviewSchema = "tusker.delivery-review/v1"
const deliveryCrossScopeReviewSchema = "tusker.cross-scope-dependency-review/v1"

// deliveryReview is deliberately a product projection. It contains no task
// frontmatter, runner instructions, logs, or lifecycle authority.
type deliveryReview struct {
	Schema   string `json:"schema"`
	ReadOnly bool   `json:"readOnly"`
	// Ready is the legacy Start projection. New callers must choose a phase
	// explicitly instead of treating it as a universal delivery verdict.
	Ready         bool                      `json:"ready"`
	PlanValid     bool                      `json:"planValid"`
	ImportReady   bool                      `json:"importReady"`
	StartReady    bool                      `json:"startReady"`
	Readiness     ReadinessContract         `json:"readiness"`
	Compatibility ReadinessLegacyProjection `json:"compatibility"`
	Title         string                    `json:"title"`
	Summary       string                    `json:"summary,omitempty"`
	What          []deliveryReviewOutcome   `json:"whatWillBeDelivered"`
	Proof         []deliveryReviewProof     `json:"howItWillBeProven"`
	Flow          deliveryReviewFlow        `json:"howWorkFlows"`
	Decisions     []deliveryReviewDecision  `json:"whatNeedsYourDecision"`
	Start         deliveryReviewStart       `json:"startBoundary"`
	NonGoals      []string                  `json:"nonGoals"`

	// Phase-local causes are intentionally kept out of the wire contract until
	// they are assembled into Readiness. This stops Start-only observations from
	// changing review/import validity.
	contractIssues []string
	importIssues   []string
	startIssues    []string
	startBlockers  []ReadinessBlocker
}
type deliveryReviewOutcome struct {
	Requirement string               `json:"requirement"`
	Outcome     string               `json:"outcome"`
	NonGoals    []string             `json:"nonGoals"`
	Links       []deliveryReviewLink `json:"links"`
}
type deliveryReviewProof struct {
	Requirements []string                 `json:"requirements"`
	Title        string                   `json:"title"`
	Outcome      string                   `json:"outcome"`
	Acceptance   []string                 `json:"acceptance"`
	Tests        []string                 `json:"tests"`
	Artifacts    []string                 `json:"artifacts"`
	SourceKey    string                   `json:"sourceKey"`
	TaskID       string                   `json:"taskId,omitempty"`
	TaskHref     string                   `json:"taskHref,omitempty"`
	Checks       []deliveryReviewCheck    `json:"checks"`
	ArtifactRefs []deliveryReviewArtifact `json:"artifactRefs"`
	ResourceRefs []string                 `json:"resourceRefs"`
}
type deliveryReviewCheck struct {
	Covers string `json:"covers"`
	Check  string `json:"check"`
	Notes  string `json:"notes,omitempty"`
	Href   string `json:"href,omitempty"`
}
type deliveryReviewArtifact struct {
	Kind          string   `json:"kind"`
	Path          string   `json:"path"`
	Summary       string   `json:"summary"`
	AcceptanceIDs []string `json:"acceptanceIds"`
	Href          string   `json:"href,omitempty"`
}
type deliveryReviewLink struct {
	Label string `json:"label"`
	Href  string `json:"href"`
}
type deliveryReviewFlow struct {
	Frontiers              [][]string                           `json:"frontiers"`
	ExpectedConcurrency    int                                  `json:"expectedConcurrency"`
	Integration            string                               `json:"integration"`
	SharedResources        []deliveryReviewResource             `json:"sharedResources"`
	CrossScopeDependencies []deliveryCrossScopeReviewDependency `json:"crossScopeDependencies"`
	Warnings               []string                             `json:"warnings"`
	WaveID                 string                               `json:"waveId,omitempty"`
	WaveHref               string                               `json:"waveHref,omitempty"`
}

// deliveryCrossScopeReviewProjection is the sole operator-facing join between
// a persisted semantic dependency and its current producer. The ordinary task
// ID edge remains authoritative; this projection is deterministic and read-only.
type deliveryCrossScopeReviewProjection struct {
	Schema       string                               `json:"schema"`
	ReadOnly     bool                                 `json:"readOnly"`
	Dependencies []deliveryCrossScopeReviewDependency `json:"dependencies"`
}

type deliveryCrossScopeReviewDependency struct {
	ConsumerTaskID               string `json:"consumerTaskId,omitempty"`
	ConsumerSourceKey            string `json:"consumerSourceKey"`
	Scope                        string `json:"scope"`
	SourceKey                    string `json:"sourceKey"`
	TaskID                       string `json:"taskId,omitempty"`
	Kind                         string `json:"kind"`
	PersistedContractFingerprint string `json:"persistedContractFingerprint,omitempty"`
	ContractProvenance           string `json:"contractProvenance"`
	TargetIntegrity              string `json:"targetIntegrity"`
	ProducerState                string `json:"producerState"`
	ProducerLifecycle            string `json:"producerLifecycle"`
	BlockerClass                 string `json:"blockerClass"`
	Satisfied                    bool   `json:"satisfied"`
	Repair                       string `json:"repair,omitempty"`
	Implication                  string `json:"implication"`
	TaskHref                     string `json:"taskHref,omitempty"`
}
type deliveryReviewResource struct {
	SourceKey      string               `json:"sourceKey"`
	Kind           string               `json:"kind"`
	Capacity       *int                 `json:"capacity,omitempty"`
	CapacityStatus string               `json:"capacityStatus"`
	Constraints    []string             `json:"constraints"`
	ReferencedBy   []string             `json:"referencedBy"`
	TaskLinks      []deliveryReviewLink `json:"taskLinks"`
}
type deliveryReviewDecision struct {
	Title         string   `json:"title"`
	Action        string   `json:"action"`
	Why           string   `json:"why"`
	SourceKey     string   `json:"sourceKey,omitempty"`
	GateID        string   `json:"gateId,omitempty"`
	GateHref      string   `json:"gateHref,omitempty"`
	TaskSourceKey string   `json:"taskSourceKey,omitempty"`
	TaskID        string   `json:"taskId,omitempty"`
	AcceptanceIDs []string `json:"acceptanceIds"`
	Verification  string   `json:"verification,omitempty"`
}
type deliveryReviewStart struct {
	PlanFingerprint    string   `json:"planFingerprint"`
	PlanIdentity       string   `json:"planIdentity,omitempty"`
	ContextFingerprint string   `json:"contextFingerprint,omitempty"`
	Authorization      string   `json:"authorization"`
	Readiness          string   `json:"readiness"`
	Blockers           []string `json:"blockers"`
	NextAction         string   `json:"nextAction"`
	State              string   `json:"state"`
	StateLabel         string   `json:"stateLabel"`
	ActionHref         string   `json:"actionHref,omitempty"`
}

func deliveryReviewCmd(args Args) error {
	vault, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	path := strings.TrimSpace(firstNonEmpty(args.String("plan"), args.String("_pos0")))
	if path == "" {
		return tuskerError(errorMissingArg, "Usage: tusker delivery review --plan <plan.yaml> [--json]")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(v7RepoRoot(vault), path)
	}
	review, err := buildDeliveryReview(vault, path)
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(review)
	} else {
		fmt.Print(renderDeliveryReview(review))
	}
	if !review.PlanValid || !review.ImportReady {
		return tuskerError(errorInvalidTransition, "delivery review is not ready: "+review.Start.NextAction, withContext(map[string]any{"delivery_review": review}))
	}
	return nil
}

func buildDeliveryReview(vault, path string) (deliveryReview, error) {
	return buildDeliveryReviewWithInspector(vault, path, inspectWavePreflightEnvironmentReadOnly)
}

func buildDeliveryReviewWithInspector(vault, path string, inspector wavePreflightEnvironmentInspector) (deliveryReview, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return deliveryReview{}, err
	}
	return buildDeliveryReviewBytes(vault, path, raw, inspector)
}

func buildDeliveryReviewBytes(vault, path string, raw []byte, inspector wavePreflightEnvironmentInspector) (deliveryReview, error) {
	plan, preparationIssues, err := readDeliveryReviewPlanBytes(vault, raw)
	if err != nil {
		return deliveryReview{}, err
	}
	issues, frontiers := validateDeliveryPlan(vault, plan)
	issues = uniqueStrings(append(preparationIssues, issues...))
	sort.Strings(issues)
	r := deliveryReview{Schema: deliveryReviewSchema, ReadOnly: true, Title: plan.Title, What: []deliveryReviewOutcome{}, Proof: []deliveryReviewProof{}, Decisions: []deliveryReviewDecision{}, NonGoals: []string{}, Flow: deliveryReviewFlow{Frontiers: deliveryReviewFrontiers(plan, frontiers), ExpectedConcurrency: deliveryExpectedConcurrency(plan, frontiers), Integration: "Reviewed work joins one serialized integration phase before landing.", SharedResources: []deliveryReviewResource{}, CrossScopeDependencies: []deliveryCrossScopeReviewDependency{}, Warnings: []string{}}, Start: deliveryReviewStart{PlanFingerprint: deliveryFingerprint(raw), Authorization: "not imported", Readiness: "review only", Blockers: []string{}, State: "held", StateLabel: "Held for review"}}
	if plan.v2 != nil {
		r.Summary = plan.v2.Summary
	}
	integrationBaseSHA := ""
	for _, issue := range issues {
		r.Start.Blockers = append(r.Start.Blockers, issue)
	}
	if plan.v2 != nil {
		findings := deliveryReviewDoctorFindings(vault, path, raw)
		for _, finding := range findings {
			r.Start.Blockers = append(r.Start.Blockers, finding.Message)
			if strings.Contains(finding.Code, "CONFLICT") || strings.Contains(finding.Code, "RESOURCE") {
				r.Flow.Warnings = append(r.Flow.Warnings, finding.Message)
			}
		}
		r.Flow.SharedResources = deliveryReviewSharedResources(plan.v2.SharedResources, plan.Tasks, findings)
		context, contextErr := buildDeliveryPlanningContextForScope(vault, strings.Join(plan.SpecRefs, ","), deliveryPlanScope(plan))
		if contextErr != nil {
			r.Start.Blockers = append(r.Start.Blockers, "planning context could not be recomputed: "+contextErr.Error())
		} else {
			integrationBaseSHA = context.IntegrationBase.SHA
			r.Start.ContextFingerprint = plan.v2.ContextFingerprint
			if context.ContextFingerprint != plan.v2.ContextFingerprint {
				r.Start.Blockers = append(r.Start.Blockers, "planning context fingerprint differs; regenerate the plan from current delivery context")
				r.Start.State = "changed"
				r.Start.StateLabel = "Delivery changed"
				r.Start.NextAction = "Regenerate the plan from current delivery context, then review its new exact fingerprint."
			}
		}
	}
	if integrationBaseSHA == "" {
		var integrationBaseErr error
		integrationBaseSHA, integrationBaseErr = deliveryIntegrationBaseSHA(vault)
		if integrationBaseErr != nil {
			r.Start.Blockers = append(r.Start.Blockers, "configured integration base could not be inspected: "+integrationBaseErr.Error())
		}
	}
	projectID := v7ProjectID(vault)
	for _, task := range plan.Tasks {
		proof := deliveryReviewProof{
			Requirements: append([]string(nil), task.RequirementRefs...),
			Title:        task.Title,
			Outcome:      task.Outcome, Acceptance: []string{}, Tests: []string{}, Artifacts: []string{},
			SourceKey: task.SourceKey, Checks: []deliveryReviewCheck{}, ArtifactRefs: []deliveryReviewArtifact{},
			ResourceRefs: append([]string(nil), task.ResourceRefs...),
		}
		for _, a := range task.Acceptance {
			proof.Acceptance = append(proof.Acceptance, a.Outcome)
		}
		for _, v := range task.Verification {
			check := strings.TrimSpace(strings.TrimPrefix(v.Check, "command: "))
			proof.Tests = append(proof.Tests, check)
			proof.Checks = append(proof.Checks, deliveryReviewCheck{Covers: v.Covers, Check: check, Notes: v.Notes})
		}
		if task.Artifact.Summary != "" {
			proof.Artifacts = append(proof.Artifacts, task.Artifact.Summary)
			artifact := deliveryReviewArtifact{
				Kind: task.Artifact.Kind, Path: task.Artifact.Path, Summary: task.Artifact.Summary,
				AcceptanceIDs: append([]string(nil), task.Artifact.AcceptanceIDs...),
			}
			if deliveryReviewRepoPathExists(vault, task.Artifact.Path) {
				artifact.Href = docDeepLink(projectID, task.Artifact.Path)
			}
			proof.ArtifactRefs = append(proof.ArtifactRefs, artifact)
		}
		r.Proof = append(r.Proof, proof)
	}
	if plan.v2 != nil {
		for _, req := range plan.v2.Requirements {
			links := []deliveryReviewLink{}
			for _, specRef := range plan.SpecRefs {
				if deliverySpecRefExists(vault, specRef) {
					links = append(links, deliveryReviewLink{Label: specRef, Href: docDeepLink(projectID, specRef)})
				}
			}
			r.What = append(r.What, deliveryReviewOutcome{Requirement: req.ID, Outcome: req.Outcome, NonGoals: []string{}, Links: links})
		}
		r.NonGoals = deliveryContextCleanStrings(plan.v2.NonGoals)
		sort.Strings(r.NonGoals)
		for _, gate := range plan.v2.HumanGates {
			r.Decisions = append(r.Decisions, deliveryReviewDecision{
				Title: gate.Title, Action: gate.Action, Why: gate.WhyAgentCannot,
				SourceKey: gate.SourceKey, TaskSourceKey: gate.TaskSourceKey,
				AcceptanceIDs: append([]string(nil), gate.AcceptanceIDs...), Verification: gate.Verification,
			})
		}
		for _, decision := range plan.v2.UnresolvedDecisions {
			r.Decisions = append(r.Decisions, deliveryReviewDecision{Title: "Product decision", Action: decision.Question, Why: "The plan marks this as unresolved.", SourceKey: decision.SourceKey, AcceptanceIDs: []string{}})
		}
	} else {
		for _, task := range plan.Tasks {
			r.What = append(r.What, deliveryReviewOutcome{Outcome: task.Outcome, NonGoals: []string{}, Links: []deliveryReviewLink{}})
		}
	}
	if len(r.Start.Blockers) > 0 && r.Start.State == "held" {
		r.Start.State = "invalid"
		r.Start.StateLabel = "Delivery plan is invalid"
		r.Start.NextAction = "Resolve the first blocker: " + r.Start.Blockers[0]
	}
	// Everything accumulated so far is a semantic planning/context fact. The
	// canonical projection below contributes either held-import or Start-only
	// facts; it must not overwrite this phase boundary.
	r.contractIssues = uniqueStrings(append(r.contractIssues, r.Start.Blockers...))
	r.Start.Blockers = []string{}
	deliveryReviewCanonical(vault, plan, integrationBaseSHA, inspector, &r)
	r.Flow.Warnings = uniqueStrings(r.Flow.Warnings)
	sort.Strings(r.Flow.Warnings)
	if len(r.Decisions) == 0 {
		r.Decisions = []deliveryReviewDecision{}
	}
	normalizeDeliveryReviewCollections(&r)
	if err := finalizeDeliveryReviewReadiness(&r, vault, plan, path); err != nil {
		return deliveryReview{}, err
	}
	return r, nil
}

func normalizeDeliveryReviewCollections(r *deliveryReview) {
	r.What = deliveryReviewNonNil(r.What)
	for i := range r.What {
		r.What[i].NonGoals = deliveryReviewNonNil(r.What[i].NonGoals)
		r.What[i].Links = deliveryReviewNonNil(r.What[i].Links)
	}
	r.Proof = deliveryReviewNonNil(r.Proof)
	for i := range r.Proof {
		proof := &r.Proof[i]
		proof.Requirements = deliveryReviewNonNil(proof.Requirements)
		proof.Acceptance = deliveryReviewNonNil(proof.Acceptance)
		proof.Tests = deliveryReviewNonNil(proof.Tests)
		proof.Artifacts = deliveryReviewNonNil(proof.Artifacts)
		proof.Checks = deliveryReviewNonNil(proof.Checks)
		proof.ArtifactRefs = deliveryReviewNonNil(proof.ArtifactRefs)
		proof.ResourceRefs = deliveryReviewNonNil(proof.ResourceRefs)
		for j := range proof.ArtifactRefs {
			proof.ArtifactRefs[j].AcceptanceIDs = deliveryReviewNonNil(proof.ArtifactRefs[j].AcceptanceIDs)
		}
	}
	r.Flow.Frontiers = deliveryReviewNonNil(r.Flow.Frontiers)
	for i := range r.Flow.Frontiers {
		r.Flow.Frontiers[i] = deliveryReviewNonNil(r.Flow.Frontiers[i])
	}
	r.Flow.SharedResources = deliveryReviewNonNil(r.Flow.SharedResources)
	for i := range r.Flow.SharedResources {
		resource := &r.Flow.SharedResources[i]
		resource.Constraints = deliveryReviewNonNil(resource.Constraints)
		resource.ReferencedBy = deliveryReviewNonNil(resource.ReferencedBy)
		resource.TaskLinks = deliveryReviewNonNil(resource.TaskLinks)
	}
	r.Flow.CrossScopeDependencies = deliveryReviewNonNil(r.Flow.CrossScopeDependencies)
	r.Flow.Warnings = deliveryReviewNonNil(r.Flow.Warnings)
	r.Decisions = deliveryReviewNonNil(r.Decisions)
	for i := range r.Decisions {
		r.Decisions[i].AcceptanceIDs = deliveryReviewNonNil(r.Decisions[i].AcceptanceIDs)
	}
	r.Start.Blockers = deliveryReviewNonNil(r.Start.Blockers)
	r.NonGoals = deliveryReviewNonNil(r.NonGoals)
}

func deliveryReviewNonNil[T any](values []T) []T {
	if values == nil {
		return []T{}
	}
	return values
}

func deliveryReviewStartCommand(vault, path, fingerprint string) string {
	return "tusker delivery start --plan " + deliveryReviewPlanArg(vault, path) + " --confirm " + fingerprint + " --by human:<name>"
}

func deliveryReviewSharedResources(resources []deliverySharedResource, tasks []deliveryPlanTask, findings []deliveryDoctorFinding) []deliveryReviewResource {
	out := make([]deliveryReviewResource, 0, len(resources))
	for _, resource := range resources {
		row := deliveryReviewResource{SourceKey: resource.SourceKey, Kind: resource.Kind, CapacityStatus: "not declared", Constraints: []string{}, ReferencedBy: []string{}, TaskLinks: []deliveryReviewLink{}}
		if resource.Capacity > 0 {
			capacity := resource.Capacity
			row.Capacity = &capacity
			row.CapacityStatus = "declared"
		}
		for _, task := range tasks {
			if containsString(task.ResourceRefs, resource.SourceKey) {
				row.ReferencedBy = append(row.ReferencedBy, task.SourceKey)
			}
		}
		for _, finding := range findings {
			if !strings.Contains(finding.Code, "RESOURCE") && !strings.Contains(finding.Code, "CONFLICT") {
				continue
			}
			if containsString(finding.SourceKeys, resource.SourceKey) {
				row.Constraints = append(row.Constraints, finding.Message)
			}
		}
		row.Constraints = uniqueStrings(row.Constraints)
		sort.Strings(row.Constraints)
		row.ReferencedBy = uniqueStrings(row.ReferencedBy)
		sort.Strings(row.ReferencedBy)
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SourceKey != out[j].SourceKey {
			return out[i].SourceKey < out[j].SourceKey
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

func deliveryCrossScopeReviewForPlan(idx v7Index, plan deliveryPlan) deliveryCrossScopeReviewProjection {
	out := newDeliveryCrossScopeReviewProjection()
	consumerScope := deliveryPlanScope(plan)
	for _, plannedTask := range plan.Tasks {
		consumerMatches := deliveryCrossScopeReviewSemanticMatches(idx, consumerScope, plannedTask.SourceKey)
		for _, dependency := range plannedTask.Dependencies {
			scope := strings.TrimSpace(dependency.scope)
			if scope == "" {
				continue
			}
			switch len(consumerMatches) {
			case 0:
				out.Dependencies = append(out.Dependencies, deliveryCrossScopeReviewProspectiveRow(idx, plannedTask.SourceKey, scope, dependency.Task))
			case 1:
				out.Dependencies = append(out.Dependencies, deliveryCrossScopeReviewPersistedPlanRow(idx, consumerMatches[0], scope, dependency.Task))
			default:
				out.Dependencies = append(out.Dependencies, deliveryCrossScopeReviewStructuralRow(
					"", plannedTask.SourceKey, scope, dependency.Task, "", "", "invalid", "corrupt", "unknown",
				))
			}
		}
	}
	sortDeliveryCrossScopeReviewDependencies(out.Dependencies)
	return out
}

func deliveryCrossScopeReviewForTask(idx v7Index, task Note) deliveryCrossScopeReviewProjection {
	out := newDeliveryCrossScopeReviewProjection()
	projections, err := deliveryCrossScopeProjections(task)
	if err != nil {
		for _, edge := range v7TaskDependencyEdges(task, idx) {
			producer, ok := idx.Tasks[edge.ID]
			if !ok {
				continue
			}
			scope := stringField(producer.Data, "delivery_plan_scope")
			if scope == "" || scope == stringField(task.Data, "delivery_plan_scope") {
				continue
			}
			dependency := deliveryCrossScopeDependency{
				Scope: scope, Task: stringField(producer.Data, "delivery_source_key"), TaskID: edge.ID,
				Kind: edge.Hardness,
			}
			out.Dependencies = append(out.Dependencies, deliveryCrossScopeReviewRow(idx, task, dependency, "invalid", true))
		}
		if len(out.Dependencies) == 0 {
			out.Dependencies = append(out.Dependencies, deliveryCrossScopeReviewStructuralRow(
				stringField(task.Data, "id"), stringField(task.Data, "delivery_source_key"), "", "", "", "", "invalid", "corrupt", "unknown",
			))
		}
		sortDeliveryCrossScopeReviewDependencies(out.Dependencies)
		return out
	}

	projected := map[string]bool{}
	for _, dependency := range projections {
		projected[dependency.TaskID] = true
		out.Dependencies = append(out.Dependencies, deliveryCrossScopeReviewRow(idx, task, dependency, "persisted", true))
	}
	for _, edge := range v7TaskDependencyEdges(task, idx) {
		if projected[edge.ID] {
			continue
		}
		producer, ok := idx.Tasks[edge.ID]
		if !ok {
			continue
		}
		scope := stringField(producer.Data, "delivery_plan_scope")
		if scope == "" || scope == stringField(task.Data, "delivery_plan_scope") {
			continue
		}
		dependency := deliveryCrossScopeDependency{
			Scope: scope, Task: stringField(producer.Data, "delivery_source_key"), TaskID: edge.ID,
			Kind: edge.Hardness,
		}
		out.Dependencies = append(out.Dependencies, deliveryCrossScopeReviewRow(idx, task, dependency, "missing", true))
	}
	sortDeliveryCrossScopeReviewDependencies(out.Dependencies)
	return out
}

func deliveryCrossScopeReviewForTaskIDs(idx v7Index, taskIDs []string) deliveryCrossScopeReviewProjection {
	out := newDeliveryCrossScopeReviewProjection()
	ids := uniqueStrings(taskIDs)
	sort.Strings(ids)
	for _, id := range ids {
		task, ok := idx.Tasks[id]
		if !ok {
			continue
		}
		projected := deliveryCrossScopeReviewForTask(idx, task)
		out.Dependencies = append(out.Dependencies, projected.Dependencies...)
	}
	sortDeliveryCrossScopeReviewDependencies(out.Dependencies)
	return out
}

func deliveryCrossScopeReviewForTaskAtVault(vault string, task Note) (deliveryCrossScopeReviewProjection, error) {
	idx, err := loadV7Index(vault)
	if err != nil {
		return newDeliveryCrossScopeReviewProjection(), err
	}
	return deliveryCrossScopeReviewForTask(idx, task), nil
}

func newDeliveryCrossScopeReviewProjection() deliveryCrossScopeReviewProjection {
	return deliveryCrossScopeReviewProjection{
		Schema: deliveryCrossScopeReviewSchema, ReadOnly: true,
		Dependencies: []deliveryCrossScopeReviewDependency{},
	}
}

func deliveryCrossScopeReviewPersistedPlanRow(idx v7Index, consumer Note, scope, sourceKey string) deliveryCrossScopeReviewDependency {
	projections, err := deliveryCrossScopeProjections(consumer)
	if err != nil {
		return deliveryCrossScopeReviewStructuralRow(
			stringField(consumer.Data, "id"), stringField(consumer.Data, "delivery_source_key"),
			scope, sourceKey, "", "", "invalid", "corrupt", "unknown",
		)
	}
	for _, dependency := range projections {
		if dependency.Scope == scope && dependency.Task == sourceKey {
			return deliveryCrossScopeReviewRow(idx, consumer, dependency, "persisted", true)
		}
	}
	var edgeMatches []Note
	for _, edge := range v7TaskDependencyEdges(consumer, idx) {
		producer, ok := idx.Tasks[edge.ID]
		if !ok || edge.Hardness != v7DependencyHardnessHard {
			continue
		}
		if stringField(producer.Data, "delivery_plan_scope") == scope &&
			stringField(producer.Data, "delivery_source_key") == sourceKey {
			edgeMatches = append(edgeMatches, producer)
		}
	}
	if len(edgeMatches) == 1 {
		dependency := deliveryCrossScopeDependency{
			Scope: scope, Task: sourceKey, TaskID: stringField(edgeMatches[0].Data, "id"), Kind: v7DependencyHardnessHard,
		}
		return deliveryCrossScopeReviewRow(idx, consumer, dependency, "missing", true)
	}
	return deliveryCrossScopeReviewStructuralRow(
		stringField(consumer.Data, "id"), stringField(consumer.Data, "delivery_source_key"),
		scope, sourceKey, "", "", "missing", "corrupt", "unknown",
	)
}

func deliveryCrossScopeReviewProspectiveRow(idx v7Index, consumerSourceKey, scope, sourceKey string) deliveryCrossScopeReviewDependency {
	matches := deliveryCrossScopeReviewSemanticMatches(idx, scope, sourceKey)
	switch len(matches) {
	case 0:
		return deliveryCrossScopeReviewStructuralRow("", consumerSourceKey, scope, sourceKey, "", "", "prospective", "missing", "missing")
	case 1:
		producer := matches[0]
		if !deliveryCrossScopeProducerCurrent(producer, idx) {
			return deliveryCrossScopeReviewStructuralRow(
				"", consumerSourceKey, scope, sourceKey, "", "", "prospective", "missing",
				deliveryCrossScopeReviewProducerState(producer),
			)
		}
		dependency := deliveryCrossScopeDependency{
			Scope: scope, Task: sourceKey, TaskID: stringField(producer.Data, "id"), Kind: v7DependencyHardnessHard,
			TargetContractFingerprint: stringField(producer.Data, "delivery_contract_fingerprint"),
		}
		return deliveryCrossScopeReviewRow(idx, Note{Data: map[string]any{"delivery_source_key": consumerSourceKey}}, dependency, "prospective", false)
	default:
		return deliveryCrossScopeReviewStructuralRow("", consumerSourceKey, scope, sourceKey, "", "", "prospective", "corrupt", "unknown")
	}
}

func deliveryCrossScopeReviewRow(idx v7Index, consumer Note, dependency deliveryCrossScopeDependency, provenance string, requireEdge bool) deliveryCrossScopeReviewDependency {
	row := deliveryCrossScopeReviewDependency{
		ConsumerTaskID: stringField(consumer.Data, "id"), ConsumerSourceKey: stringField(consumer.Data, "delivery_source_key"),
		Scope: dependency.Scope, SourceKey: dependency.Task, TaskID: dependency.TaskID, Kind: dependency.Kind,
		PersistedContractFingerprint: dependency.TargetContractFingerprint, ContractProvenance: provenance,
		TargetIntegrity: "resolved", ProducerState: "missing", ProducerLifecycle: "unknown", BlockerClass: "structural",
	}
	row.Implication = deliveryCrossScopeReviewImplication(row)
	if strings.TrimSpace(row.Scope) == "" || strings.TrimSpace(row.SourceKey) == "" || row.Kind != v7DependencyHardnessHard {
		return deliveryCrossScopeReviewMarkStructural(row, "corrupt")
	}
	if requireEdge {
		consumerScope := stringField(consumer.Data, "delivery_plan_scope")
		if consumerScope == "" || consumerScope == row.Scope {
			return deliveryCrossScopeReviewMarkStructural(row, "corrupt")
		}
	}
	if strings.TrimSpace(row.TaskID) == "" {
		return deliveryCrossScopeReviewMarkStructural(row, "missing")
	}
	producer, ok := idx.Tasks[row.TaskID]
	if !ok {
		return deliveryCrossScopeReviewMarkStructural(row, "missing")
	}
	row.ProducerState = deliveryCrossScopeReviewProducerState(producer)
	row.ProducerLifecycle = deliveryCrossScopeReviewProducerLifecycle(producer)
	projectID := firstNonEmpty(stringField(producer.Data, "project"), stringField(consumer.Data, "project"))
	if projectID != "" {
		row.TaskHref = taskDeepLink(projectID, row.TaskID)
	}
	if requireEdge && (stringField(producer.Data, "project") == "" ||
		stringField(producer.Data, "project") != stringField(consumer.Data, "project")) {
		return deliveryCrossScopeReviewMarkStructural(row, "corrupt")
	}
	if provenance == "invalid" || provenance == "missing" ||
		strings.TrimSpace(row.PersistedContractFingerprint) == "" ||
		stringField(producer.Data, "delivery_plan_scope") != row.Scope ||
		stringField(producer.Data, "delivery_source_key") != row.SourceKey ||
		stringField(producer.Data, "delivery_contract_fingerprint") != row.PersistedContractFingerprint {
		return deliveryCrossScopeReviewMarkStructural(row, "corrupt")
	}
	if requireEdge {
		matches := 0
		hard := false
		for _, edge := range v7TaskDependencyEdges(consumer, idx) {
			if edge.ID != row.TaskID {
				continue
			}
			matches++
			hard = edge.Hardness == v7DependencyHardnessHard
		}
		if matches != 1 || !hard {
			return deliveryCrossScopeReviewMarkStructural(row, "corrupt")
		}
	}
	if row.ProducerLifecycle == "unknown" {
		return deliveryCrossScopeReviewMarkStructural(row, "corrupt")
	}
	if row.ProducerLifecycle == "failed" {
		row.BlockerClass = "lifecycle"
		row.Repair = deliveryCrossScopeReviewLifecycleRepair(row, true)
		return row
	}
	if !deliveryCrossScopeProducerCurrent(producer, idx) {
		return deliveryCrossScopeReviewMarkStructural(row, "corrupt")
	}
	if row.ProducerLifecycle == "complete" {
		row.BlockerClass = "none"
		row.Satisfied = true
		return row
	}
	row.BlockerClass = "lifecycle"
	row.Repair = deliveryCrossScopeReviewLifecycleRepair(row, false)
	return row
}

func deliveryCrossScopeReviewStructuralRow(consumerID, consumerSourceKey, scope, sourceKey, taskID, fingerprint, provenance, integrity, producerState string) deliveryCrossScopeReviewDependency {
	row := deliveryCrossScopeReviewDependency{
		ConsumerTaskID: consumerID, ConsumerSourceKey: consumerSourceKey,
		Scope: scope, SourceKey: sourceKey, TaskID: taskID, Kind: v7DependencyHardnessHard,
		PersistedContractFingerprint: fingerprint, ContractProvenance: provenance,
		TargetIntegrity: integrity, ProducerState: fallback(producerState, "unknown"), ProducerLifecycle: "unknown",
		BlockerClass: "structural",
	}
	row.Implication = deliveryCrossScopeReviewImplication(row)
	row.Repair = deliveryCrossScopeReviewStructuralRepair(row, integrity)
	return row
}

func deliveryCrossScopeReviewMarkStructural(row deliveryCrossScopeReviewDependency, integrity string) deliveryCrossScopeReviewDependency {
	row.TargetIntegrity = integrity
	row.BlockerClass = "structural"
	row.Satisfied = false
	row.Repair = deliveryCrossScopeReviewStructuralRepair(row, integrity)
	return row
}

func deliveryCrossScopeReviewSemanticMatches(idx v7Index, scope, sourceKey string) []Note {
	var matches []Note
	for _, candidate := range idx.Tasks {
		if stringField(candidate.Data, "delivery_plan_scope") == scope &&
			stringField(candidate.Data, "delivery_source_key") == sourceKey {
			matches = append(matches, candidate)
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		return stringField(matches[i].Data, "id") < stringField(matches[j].Data, "id")
	})
	return matches
}

func deliveryCrossScopeReviewProducerState(producer Note) string {
	if stringField(producer.Data, "discarded_at") != "" {
		return "discarded"
	}
	return fallback(strings.ToLower(strings.TrimSpace(stringField(producer.Data, "status"))), "unknown")
}

func deliveryCrossScopeReviewProducerLifecycle(producer Note) string {
	state := deliveryCrossScopeReviewProducerState(producer)
	switch state {
	case "done":
		return "complete"
	case "cancelled", "discarded", "superseded":
		return "failed"
	case "idea", "backlog", "ready", "review", "rework":
		return "incomplete"
	default:
		return "unknown"
	}
}

func deliveryCrossScopeReviewTarget(row deliveryCrossScopeReviewDependency) string {
	if row.Scope != "" && row.SourceKey != "" {
		return row.Scope + "/" + row.SourceKey
	}
	return "the named producer"
}

func deliveryCrossScopeReviewConsumer(row deliveryCrossScopeReviewDependency) string {
	return firstNonEmpty(row.ConsumerTaskID, row.ConsumerSourceKey, "the consumer")
}

func deliveryCrossScopeReviewImplication(row deliveryCrossScopeReviewDependency) string {
	targetID := fallback(row.TaskID, "its durable producer task")
	return "Import producer " + deliveryCrossScopeReviewTarget(row) + " before " + deliveryCrossScopeReviewConsumer(row) +
		"; the consumer can run only after hard target " + targetID + " reaches done."
}

func deliveryCrossScopeReviewStructuralRepair(row deliveryCrossScopeReviewDependency, integrity string) string {
	if integrity == "missing" {
		return "Import or restore exactly one producer " + deliveryCrossScopeReviewTarget(row) +
			", then re-import the consumer plan before retrying " + deliveryCrossScopeReviewConsumer(row) + "."
	}
	return "Restore the exact durable hard edge and contract fingerprint for " + deliveryCrossScopeReviewTarget(row) +
		", then re-import the producer and consumer plans together before retrying " + deliveryCrossScopeReviewConsumer(row) + "."
}

func deliveryCrossScopeReviewLifecycleRepair(row deliveryCrossScopeReviewDependency, failed bool) string {
	if failed {
		return "Repair or reopen producer " + deliveryCrossScopeReviewTarget(row) + " (" + row.TaskID + ") in its owning workflow, then complete it; " +
			deliveryCrossScopeReviewConsumer(row) + " remains blocked until that ordinary hard dependency reaches done."
	}
	return "Complete producer " + deliveryCrossScopeReviewTarget(row) + " (" + row.TaskID + "); " +
		deliveryCrossScopeReviewConsumer(row) + " remains blocked until its ordinary hard dependency reaches done."
}

func sortDeliveryCrossScopeReviewDependencies(rows []deliveryCrossScopeReviewDependency) {
	sort.Slice(rows, func(i, j int) bool {
		left := rows[i].Scope + "\x00" + rows[i].SourceKey + "\x00" + rows[i].TaskID + "\x00" + rows[i].ConsumerSourceKey + "\x00" + rows[i].ConsumerTaskID
		right := rows[j].Scope + "\x00" + rows[j].SourceKey + "\x00" + rows[j].TaskID + "\x00" + rows[j].ConsumerSourceKey + "\x00" + rows[j].ConsumerTaskID
		return left < right
	})
}

func renderDeliveryCrossScopeReview(rows []deliveryCrossScopeReviewDependency) string {
	if len(rows) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Cross-scope hard dependencies\n")
	for _, row := range rows {
		fmt.Fprintf(&b, "- Hard dependency: %s\n", deliveryCrossScopeReviewTarget(row))
		fmt.Fprintf(&b, "  Durable target: %s (%s; %s; contract %s)\n",
			fallback(row.TaskID, "missing"), row.ProducerState, row.Kind, fallback(row.PersistedContractFingerprint, "not recorded"))
		fmt.Fprintf(&b, "  Classification: target=%s lifecycle=%s blocker=%s provenance=%s\n",
			row.TargetIntegrity, row.ProducerLifecycle, row.BlockerClass, row.ContractProvenance)
		fmt.Fprintf(&b, "  Producer before consumer: %s\n", row.Implication)
		if row.Repair != "" {
			fmt.Fprintf(&b, "  Repair: %s\n", row.Repair)
		}
	}
	return b.String()
}

func deliveryReviewRepoPathExists(vault, path string) bool {
	full, ok := safeRepoPath(v7RepoRoot(vault), filepath.ToSlash(strings.TrimSpace(path)))
	return ok && fileExists(full)
}

func deliveryReviewFrontiers(plan deliveryPlan, frontiers [][]string) [][]string {
	outcomes := map[string]string{}
	for _, task := range plan.Tasks {
		outcomes[task.SourceKey] = task.Outcome
	}
	result := make([][]string, 0, len(frontiers))
	for _, frontier := range frontiers {
		row := make([]string, 0, len(frontier))
		for _, key := range frontier {
			row = append(row, fallback(outcomes[key], "Planned outcome"))
		}
		result = append(result, row)
	}
	return result
}

func readDeliveryReviewPlan(vault, path string) (deliveryPlan, []byte, []string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return deliveryPlan{}, nil, nil, err
	}
	plan, issues, err := readDeliveryReviewPlanBytes(vault, raw)
	return plan, raw, issues, err
}

func readDeliveryReviewPlanBytes(vault string, raw []byte) (deliveryPlan, []string, error) {
	if schema, err := deliveryPlanSchemaBytes(raw); err != nil {
		return deliveryPlan{}, nil, err
	} else if schema == deliveryPlanV2Schema {
		var v2 deliveryPlanV2
		d := yaml.NewDecoder(bytes.NewReader(raw))
		d.KnownFields(true)
		if err := d.Decode(&v2); err != nil {
			return deliveryPlan{}, nil, tuskerError(errorInvalidArg, "invalid V2 delivery plan YAML: "+err.Error())
		}
		plan, issues := deliveryV2Prepare(vault, v2)
		return plan, issues, nil
	}
	plan, err := readDeliveryPlanBytes(raw)
	return plan, nil, err
}

func deliveryReviewDoctorFindings(vault, path string, raw []byte) []deliveryDoctorFinding {
	report, err := deliveryPlanDoctorBytes(vault, path, raw)
	if err != nil {
		return []deliveryDoctorFinding{{Code: "PLAN_UNREADABLE", Message: err.Error()}}
	}
	return report.Findings
}

func deliveryReviewCanonical(vault string, plan deliveryPlan, integrationBaseSHA string, inspector wavePreflightEnvironmentInspector, r *deliveryReview) {
	idx, err := loadV7Index(vault)
	if err != nil {
		r.importIssues = append(r.importIssues, err.Error())
		return
	}
	r.Flow.CrossScopeDependencies = deliveryCrossScopeReviewForPlan(idx, plan).Dependencies
	var waves []Note
	for _, wave := range idx.Waves {
		if stringField(wave.Data, "delivery_plan_scope") == deliveryPlanScope(plan) {
			waves = append(waves, wave)
		}
	}
	if len(waves) == 0 {
		if r.Start.State == "invalid" || r.Start.State == "changed" {
			return
		}
		prospective := deliveryReviewProspectiveWave(vault, plan, r.Start.PlanFingerprint, integrationBaseSHA, r.Flow.ExpectedConcurrency)
		env := deliveryReviewInspectEnvironment(vault, prospective, inspector)
		if state, blocked := deliveryReviewEnvironmentState(env, ""); blocked {
			deliveryReviewApplyState(r, state)
			deliveryReviewAddEnvironmentStartBlockers(r, env, v7ProjectID(vault), "")
		}
		return
	}
	if len(waves) != 1 {
		r.Start.State = "changed"
		r.Start.StateLabel = "Delivery changed"
		r.Start.NextAction = "Resolve duplicate plan-scope ownership, then regenerate delivery review."
		r.importIssues = append(r.importIssues, "canonical import drift: more than one wave owns this plan scope; re-import the reviewed plan after resolving duplicate scope ownership")
		return
	}
	wave := waves[0]
	projectID := firstNonEmpty(stringField(wave.Data, "project"), v7ProjectID(vault))
	waveID := stringField(wave.Data, "id")
	if waveID != "" {
		r.Flow.WaveID = waveID
		r.Flow.WaveHref = waveDeepLink(projectID, waveID)
	}
	r.Start.Authorization = fallback(stringField(wave.Data, "authorization"), "disarmed")
	if stringField(wave.Data, "delivery_plan_fingerprint") != r.Start.PlanFingerprint {
		r.Start.State = "changed"
		r.Start.StateLabel = "Delivery changed"
		r.Start.NextAction = "Regenerate delivery review, then confirm the new exact plan fingerprint."
		r.importIssues = append(r.importIssues, "canonical import drift: the plan fingerprint differs; regenerate delivery review, then Start the exact reviewed plan")
		return
	}
	members := map[string]bool{}
	for _, id := range normalizeList(wave.Data["members"]) {
		members[id] = true
	}
	for _, task := range plan.Tasks {
		found := ""
		for id, note := range idx.Tasks {
			if stringField(note.Data, "delivery_plan_scope") == deliveryPlanScope(plan) && stringField(note.Data, "delivery_source_key") == task.SourceKey {
				found = id
				break
			}
		}
		if found == "" || !members[found] {
			r.Start.State = "changed"
			r.Start.StateLabel = "Delivery changed"
			r.Start.NextAction = "Regenerate delivery review, then confirm the new exact plan fingerprint."
			r.importIssues = append(r.importIssues, "canonical import drift: a planned outcome is missing from the imported wave; regenerate delivery review, then Start the exact reviewed plan")
			return
		}
		taskHref := taskDeepLink(projectID, found)
		for i := range r.Proof {
			if r.Proof[i].SourceKey == task.SourceKey {
				r.Proof[i].TaskID = found
				r.Proof[i].TaskHref = taskHref
				for checkIndex := range r.Proof[i].Checks {
					r.Proof[i].Checks[checkIndex].Href = taskHref
				}
			}
		}
		for i := range r.Flow.SharedResources {
			if containsString(task.ResourceRefs, r.Flow.SharedResources[i].SourceKey) {
				r.Flow.SharedResources[i].TaskLinks = append(r.Flow.SharedResources[i].TaskLinks, deliveryReviewLink{Label: found, Href: taskHref})
			}
		}
	}
	for _, gate := range idx.Gates {
		if stringField(gate.Data, "delivery_plan_scope") != deliveryPlanScope(plan) {
			continue
		}
		sourceKey := stringField(gate.Data, "delivery_source_key")
		gateID := stringField(gate.Data, "id")
		blocked := normalizeList(gate.Data["blocks"])
		taskID := ""
		if len(blocked) > 0 {
			taskID = blocked[0]
		}
		for i := range r.Decisions {
			if r.Decisions[i].SourceKey != sourceKey {
				continue
			}
			r.Decisions[i].GateID = gateID
			r.Decisions[i].TaskID = taskID
			if gateID != "" {
				if _, ok := idx.Tasks[taskID]; !ok {
					continue
				}
				r.Decisions[i].GateHref = gateDeepLink(projectID, taskID, gateID)
			}
		}
	}
	env := deliveryReviewInspectEnvironment(vault, wave, inspector)
	preflight := buildWavePreflight(vault, idx, wave, env)
	if r.Start.State != "invalid" && r.Start.State != "changed" {
		deliveryReviewProjectState(vault, idx, wave, env, preflight, r)
	}
	deliveryReviewAddPreflightPhaseBlockers(r, env, preflight, projectID, waveID)
}

type deliveryReviewState struct {
	State  string
	Label  string
	Action string
	Href   string
}

func deliveryReviewProspectiveWave(vault string, plan deliveryPlan, planFingerprint, integrationBaseSHA string, concurrency int) Note {
	materialID := strings.TrimPrefix(strings.TrimSpace(planFingerprint), "sha256:")
	if len(materialID) > 12 {
		materialID = materialID[:12]
	}
	prospectiveID := "PROSPECTIVE-" + strings.ToUpper(materialID)
	data := map[string]any{
		"schema":                    "tusker.wave/v7",
		"kind":                      "wave",
		"id":                        prospectiveID,
		"project":                   v7ProjectID(vault),
		"delivery_plan_schema":      plan.Schema,
		"delivery_plan_scope":       deliveryPlanScope(plan),
		"delivery_plan_fingerprint": planFingerprint,
		"runner_profile":            plan.RunnerProfile,
		"concurrency":               maxInt(1, concurrency),
		"integration_branch":        v7IntegrationBranchName(prospectiveID),
	}
	if integrationBaseSHA != "" {
		data["integration_base_sha"] = integrationBaseSHA
	}
	// This record is inspection material only. In particular, it deliberately
	// carries no authorization claim and is never persisted or linked.
	return Note{Data: data}
}

func deliveryReviewInspectEnvironment(vault string, wave Note, inspector wavePreflightEnvironmentInspector) wavePreflightEnvironment {
	if inspector != nil {
		return inspector(vault, wave)
	}
	return inspectWavePreflightEnvironmentReadOnly(vault, wave)
}

func deliveryReviewEnvironmentState(env wavePreflightEnvironment, statusHref string) (deliveryReviewState, bool) {
	switch {
	case !env.ProjectRegistered:
		return deliveryReviewState{"disabled", "Project is not registered", "Register this project in Project Settings, then review the delivery again.", ""}, true
	case !env.ProjectEnabled:
		return deliveryReviewState{"disabled", "Project automation is off", "Enable this project's automation in Project Settings, then review the delivery again.", ""}, true
	case !env.ProjectHealthy:
		return deliveryReviewState{"disabled", "Project health is blocked", "Repair the project's reported health issue before Start.", ""}, true
	case !env.WorkflowCompatible:
		return deliveryReviewState{"invalid", "Workflow is incompatible", "Repair the project workflow version and tracker schema before Start.", ""}, true
	case !env.SkillCompatible:
		return deliveryReviewState{"invalid", "Project skill is incompatible", "Install or repair the compatible Tusker project skill before Start.", ""}, true
	case !env.DaemonAlive:
		return deliveryReviewState{"daemon-off", "Resident daemon is off", "Start the resident daemon, then review the delivery again.", ""}, true
	case !env.DaemonReconciling:
		return deliveryReviewState{"daemon-off", "Resident daemon is not reconciling", "Repair the resident daemon's project polling before Start.", ""}, true
	case !env.RunnerCompatible:
		return deliveryReviewState{"runner-blocked", "Runner is incompatible", "Configure a supported unattended runner for this wave, then review again.", ""}, true
	case !env.ApprovalFree:
		return deliveryReviewState{"runner-blocked", "Runner requires approval", "Configure this runner for approval-free unattended execution before Start.", ""}, true
	case !env.IsolatedWorkspace:
		return deliveryReviewState{"shared-workspace", "Workspace is shared", "Select an isolated workspace strategy in Project Settings, then review again.", ""}, true
	case !env.IntegrationClean:
		return deliveryReviewState{"shared-workspace", "Integration lane is not clean", "Repair the wave integration lane before Start.", statusHref}, true
	}
	return deliveryReviewState{}, false
}

func deliveryReviewApplyState(r *deliveryReview, state deliveryReviewState) {
	r.Start.State = state.State
	r.Start.StateLabel = state.Label
	r.Start.NextAction = state.Action
	r.Start.ActionHref = state.Href
}

func deliveryReviewEnvironmentPreflightBlocker(blocker string) bool {
	for _, key := range []string{"project", "daemon", "runner", "skill", "workflow", "approvalPolicy", "workspaceIsolation"} {
		if blocker == waveEnvironmentBlocker(key) {
			return true
		}
	}
	return false
}

func deliveryReviewProjectState(vault string, idx v7Index, wave Note, env wavePreflightEnvironment, preflight wavePreflightReport, r *deliveryReview) {
	projectID := firstNonEmpty(stringField(wave.Data, "project"), v7ProjectID(vault))
	waveID := stringField(wave.Data, "id")
	statusHref := ""
	if waveID != "" {
		statusHref = waveDeepLink(projectID, waveID)
	}
	set := func(state, label, action, href string) {
		deliveryReviewApplyState(r, deliveryReviewState{State: state, Label: label, Action: action, Href: href})
	}
	if preflight.AuthorizationStale {
		set("changed", "Delivery changed", "Regenerate delivery review, then confirm and Start the new exact fingerprint.", "")
		return
	}
	members := normalizeList(wave.Data["members"])
	allDone := len(members) > 0
	for _, memberID := range members {
		task, ok := idx.Tasks[memberID]
		allDone = allDone && ok && stringField(task.Data, "status") == "done"
	}
	if armedWaveIntegrated(wave) || allDone {
		set("completed", "Delivery completed", "Review the delivered artifacts and integration outcome.", statusHref)
		return
	}
	if state, blocked := deliveryReviewEnvironmentState(env, statusHref); blocked {
		deliveryReviewApplyState(r, state)
		return
	}

	runs := deliveryReviewRuntimeRuns(vault, projectID)
	snapshot := buildArmedWaveSnapshot(vault, idx, wave, runs, time.Now())
	hasRunning, hasParked := false, false
	hasGate := preflight.Authorization == "armed" && len(preflight.HumanGates) > 0
	firstGateID := ""
	if hasGate {
		firstGateID = stringField(preflight.HumanGates[0], "id")
	}
	firstParked := ""
	for _, member := range snapshot.Members {
		switch member.State {
		case armedWaveRunning:
			hasRunning = true
		case armedWaveMachineParked:
			hasParked = true
			if firstParked == "" {
				firstParked = member.Reason
			}
		case armedWaveHumanBlocked:
			hasGate = true
		}
	}
	switch {
	case hasParked:
		set("parked", "Delivery parked", "Resolve the first parked outcome: "+fallback(firstParked, "inspect the wave status for its recovery action"), statusHref)
	case hasGate:
		action := "Open the blocking human gate, record the decision, then resume delivery."
		if firstGateID != "" {
			action = "Open " + firstGateID + ", record the decision, then resume delivery."
		}
		set("gated", "Waiting on a human decision", action, statusHref)
		for _, decision := range r.Decisions {
			if decision.GateHref != "" && (firstGateID == "" || decision.GateID == firstGateID) {
				r.Start.ActionHref = decision.GateHref
				break
			}
		}
	case hasRunning:
		set("running", "Delivery running", "Observe the current wave; no additional Start action is needed.", statusHref)
	case preflight.Authorization == "armed":
		set("armed", "Delivery armed", "Observe the armed wave; the resident daemon owns the next runnable frontier.", statusHref)
	default:
		set("held", "Held for review", "", "")
		r.Start.ActionHref = ""
	}
}

func deliveryReviewRuntimeRuns(vault, projectID string) map[string]RunStatus {
	out := map[string]RunStatus{}
	store, err := OpenRuntimeStoreReadOnly(DefaultStateRoot())
	if err != nil {
		return out
	}
	defer store.Close()
	if loaded, projectErr := loadRegisteredProjects(store, registeredProjectLoadOptions{MetadataOnly: true, LoadDisabled: true}); projectErr == nil {
		projects := loadedRegisteredProjects(loaded)
		selectedProjectID := ""
		for _, project := range projects {
			vaultMatch := sameCanonicalProjectPath(project.VaultRoot, vault)
			repoMatch := sameCanonicalProjectPath(project.RepoRoot, v7RepoRoot(vault))
			if !vaultMatch && !repoMatch {
				continue
			}
			if projectID != "" && project.ProjectID == projectID {
				selectedProjectID = project.ProjectID
				break
			}
			if selectedProjectID == "" && vaultMatch {
				selectedProjectID = project.ProjectID
			}
			if selectedProjectID == "" && repoMatch {
				selectedProjectID = project.ProjectID
			}
		}
		if selectedProjectID != "" {
			projectID = selectedProjectID
		}
	}
	runs, err := store.ListRuns()
	if err != nil {
		return out
	}
	for _, run := range runs {
		if projectID == "" || run.ProjectID == projectID {
			out[firstNonEmpty(run.ItemID, run.RecordID)] = run
		}
	}
	return out
}

func deliveryReviewPlanArg(vault, path string) string {
	if rel, err := filepath.Rel(v7RepoRoot(vault), path); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return "<plan.yaml>"
}

func renderDeliveryReview(r deliveryReview) string {
	var b strings.Builder
	b.WriteString("Delivery review (read-only; does not start delivery)\n\nWhat will be delivered\n")
	if len(r.What) == 0 {
		b.WriteString("- No requirement outcomes declared.\n")
	}
	for _, v := range r.What {
		b.WriteString("- " + v.Outcome + "\n")
	}
	if len(r.NonGoals) == 0 {
		b.WriteString("- Non-goals: None declared.\n")
	} else {
		b.WriteString("- Non-goals: " + strings.Join(r.NonGoals, "; ") + "\n")
	}
	b.WriteString("\nHow it will be proven\n")
	if len(r.Proof) == 0 {
		b.WriteString("- No proof coverage declared.\n")
	}
	for _, p := range r.Proof {
		b.WriteString("- " + p.Outcome + "\n")
	}
	b.WriteString("\nHow work flows\n")
	for i, f := range r.Flow.Frontiers {
		b.WriteString(fmt.Sprintf("- Phase %d: %s\n", i+1, strings.Join(f, ", ")))
	}
	b.WriteString(fmt.Sprintf("- Expected parallel work: %d; %s\n", r.Flow.ExpectedConcurrency, r.Flow.Integration))
	for _, resource := range r.Flow.SharedResources {
		capacity := "capacity not declared"
		if resource.Capacity != nil {
			capacity = fmt.Sprintf("capacity %d", *resource.Capacity)
		}
		b.WriteString(fmt.Sprintf("- Shared resource %s (%s): %s.\n", resource.SourceKey, resource.Kind, capacity))
	}
	if crossScope := renderDeliveryCrossScopeReview(r.Flow.CrossScopeDependencies); crossScope != "" {
		b.WriteString(crossScope)
	}
	b.WriteString("\nWhat needs your decision\n")
	if len(r.Decisions) == 0 {
		b.WriteString("- Nothing.\n")
	}
	for _, d := range r.Decisions {
		b.WriteString("- " + d.Title + ": " + d.Action + "\n")
	}
	b.WriteString("\nStart boundary\n")
	b.WriteString("- Plan: " + r.Start.PlanFingerprint)
	if r.Start.ContextFingerprint != "" {
		b.WriteString("; context: " + r.Start.ContextFingerprint)
	}
	b.WriteString("; authorization: " + r.Start.Authorization + "; readiness: " + r.Start.Readiness + "\n- Next action: " + r.Start.NextAction + "\n")
	return b.String()
}
