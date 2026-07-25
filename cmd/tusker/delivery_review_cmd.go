package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const deliveryReviewSchema = "tusker.delivery-review/v1"

// deliveryReview is deliberately a product projection. It contains no task
// frontmatter, runner instructions, logs, or lifecycle authority.
type deliveryReview struct {
	Schema    string                   `json:"schema"`
	ReadOnly  bool                     `json:"readOnly"`
	Ready     bool                     `json:"ready"`
	What      []deliveryReviewOutcome  `json:"whatWillBeDelivered"`
	Proof     []deliveryReviewProof    `json:"howItWillBeProven"`
	Flow      deliveryReviewFlow       `json:"howWorkFlows"`
	Decisions []deliveryReviewDecision `json:"whatNeedsYourDecision"`
	Start     deliveryReviewStart      `json:"startBoundary"`
}
type deliveryReviewOutcome struct {
	Requirement string   `json:"requirement"`
	Outcome     string   `json:"outcome"`
	NonGoals    []string `json:"nonGoals"`
}
type deliveryReviewProof struct {
	Requirements []string `json:"requirements"`
	Outcome      string   `json:"outcome"`
	Acceptance   []string `json:"acceptance"`
	Tests        []string `json:"tests"`
	Artifacts    []string `json:"artifacts"`
}
type deliveryReviewFlow struct {
	Frontiers           [][]string `json:"frontiers"`
	ExpectedConcurrency int        `json:"expectedConcurrency"`
	Integration         string     `json:"integration"`
	Warnings            []string   `json:"warnings"`
}
type deliveryReviewDecision struct {
	Title  string `json:"title"`
	Action string `json:"action"`
	Why    string `json:"why"`
}
type deliveryReviewStart struct {
	PlanFingerprint string   `json:"planFingerprint"`
	Authorization   string   `json:"authorization"`
	Readiness       string   `json:"readiness"`
	Blockers        []string `json:"blockers"`
	NextAction      string   `json:"nextAction"`
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
	if !review.Ready {
		return tuskerError(errorInvalidTransition, "delivery review is not ready: "+review.Start.NextAction, withContext(map[string]any{"delivery_review": review}))
	}
	return nil
}

func buildDeliveryReview(vault, path string) (deliveryReview, error) {
	plan, raw, err := readDeliveryReviewPlan(vault, path)
	if err != nil {
		return deliveryReview{}, err
	}
	issues, frontiers := validateDeliveryPlan(vault, plan)
	r := deliveryReview{Schema: deliveryReviewSchema, ReadOnly: true, What: []deliveryReviewOutcome{}, Proof: []deliveryReviewProof{}, Decisions: []deliveryReviewDecision{}, Flow: deliveryReviewFlow{Frontiers: deliveryReviewFrontiers(plan, frontiers), ExpectedConcurrency: deliveryExpectedConcurrency(plan, frontiers), Integration: "Reviewed work joins one serialized integration phase before landing.", Warnings: []string{}}, Start: deliveryReviewStart{PlanFingerprint: deliveryFingerprint(raw), Authorization: "not imported", Readiness: "review only", Blockers: []string{}}}
	for _, issue := range issues {
		r.Start.Blockers = append(r.Start.Blockers, issue)
	}
	if plan.v2 != nil {
		for _, finding := range deliveryReviewDoctorFindings(vault, path) {
			r.Start.Blockers = append(r.Start.Blockers, finding.Message)
			if strings.Contains(finding.Code, "CONFLICT") || strings.Contains(finding.Code, "RESOURCE") {
				r.Flow.Warnings = append(r.Flow.Warnings, finding.Message)
			}
		}
	}
	for _, task := range plan.Tasks {
		proof := deliveryReviewProof{Requirements: append([]string(nil), task.RequirementRefs...), Outcome: task.Outcome, Acceptance: []string{}, Tests: []string{}, Artifacts: []string{}}
		for _, a := range task.Acceptance {
			proof.Acceptance = append(proof.Acceptance, a.Outcome)
		}
		for _, v := range task.Verification {
			proof.Tests = append(proof.Tests, strings.TrimSpace(strings.TrimPrefix(v.Check, "command: ")))
		}
		if task.Artifact.Summary != "" {
			proof.Artifacts = append(proof.Artifacts, task.Artifact.Summary)
		}
		r.Proof = append(r.Proof, proof)
	}
	if plan.v2 != nil {
		for _, req := range plan.v2.Requirements {
			r.What = append(r.What, deliveryReviewOutcome{Requirement: req.ID, Outcome: req.Outcome, NonGoals: []string{}})
		}
		for _, gate := range plan.v2.HumanGates {
			r.Decisions = append(r.Decisions, deliveryReviewDecision{Title: gate.Title, Action: gate.Action, Why: gate.WhyAgentCannot})
		}
		for _, decision := range plan.v2.UnresolvedDecisions {
			r.Decisions = append(r.Decisions, deliveryReviewDecision{Title: "Product decision", Action: decision.Question, Why: "The plan marks this as unresolved."})
		}
	} else {
		for _, task := range plan.Tasks {
			r.What = append(r.What, deliveryReviewOutcome{Outcome: task.Outcome, NonGoals: []string{}})
		}
	}
	deliveryReviewCanonical(vault, path, plan, &r)
	r.Start.Blockers = uniqueStrings(r.Start.Blockers)
	sort.Strings(r.Start.Blockers)
	r.Flow.Warnings = uniqueStrings(r.Flow.Warnings)
	sort.Strings(r.Flow.Warnings)
	if len(r.Decisions) == 0 {
		r.Decisions = []deliveryReviewDecision{}
	}
	if len(r.Start.Blockers) > 0 {
		r.Start.Readiness = "blocked"
		if strings.Contains(r.Start.Blockers[0], "canonical import drift") {
			r.Start.NextAction = "Re-import the reviewed plan: tusker delivery import --plan " + deliveryReviewPlanArg(vault, path)
		} else {
			r.Start.NextAction = "Resolve the first blocker: " + r.Start.Blockers[0]
		}
		return r, nil
	}
	r.Ready = true
	if r.Start.Authorization == "not imported" {
		r.Start.Readiness, r.Start.NextAction = "ready to import held work", "Import the reviewed plan: tusker delivery import --plan "+deliveryReviewPlanArg(vault, path)
	} else {
		r.Start.Readiness, r.Start.NextAction = "imported and "+r.Start.Authorization, "Review the start boundary; importing and review do not start delivery."
	}
	return r, nil
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

func readDeliveryReviewPlan(vault, path string) (deliveryPlan, []byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return deliveryPlan{}, nil, err
	}
	if schema, err := deliveryPlanSchemaAt(path); err != nil {
		return deliveryPlan{}, raw, err
	} else if schema == deliveryPlanV2Schema {
		var v2 deliveryPlanV2
		d := yaml.NewDecoder(bytes.NewReader(raw))
		d.KnownFields(true)
		if err := d.Decode(&v2); err != nil {
			return deliveryPlan{}, raw, tuskerError(errorInvalidArg, "invalid V2 delivery plan YAML: "+err.Error())
		}
		plan, _ := deliveryV2Prepare(vault, v2)
		return plan, raw, nil
	}
	plan, _, err := readDeliveryPlan(path)
	return plan, raw, err
}

func deliveryReviewDoctorFindings(vault, path string) []deliveryDoctorFinding {
	report, err := deliveryPlanDoctor(vault, path)
	if err != nil {
		return []deliveryDoctorFinding{{Code: "PLAN_UNREADABLE", Message: err.Error()}}
	}
	return report.Findings
}

func deliveryReviewCanonical(vault, path string, plan deliveryPlan, r *deliveryReview) {
	idx, err := loadV7Index(vault)
	if err != nil {
		r.Start.Blockers = append(r.Start.Blockers, err.Error())
		return
	}
	var waves []Note
	for _, wave := range idx.Waves {
		if stringField(wave.Data, "delivery_plan_scope") == deliveryPlanScope(plan) {
			waves = append(waves, wave)
		}
	}
	if len(waves) == 0 {
		return
	}
	if len(waves) != 1 {
		r.Start.Blockers = append(r.Start.Blockers, "canonical import drift: more than one wave owns this plan scope; re-import the reviewed plan after resolving duplicate scope ownership")
		return
	}
	wave := waves[0]
	r.Start.Authorization = fallback(stringField(wave.Data, "authorization"), "disarmed")
	if stringField(wave.Data, "delivery_plan_fingerprint") != r.Start.PlanFingerprint {
		r.Start.Blockers = append(r.Start.Blockers, "canonical import drift: the plan fingerprint differs; re-import the reviewed plan: tusker delivery import --plan "+deliveryReviewPlanArg(vault, path))
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
			r.Start.Blockers = append(r.Start.Blockers, "canonical import drift: a planned outcome is missing from the imported wave; re-import the reviewed plan: tusker delivery import --plan "+deliveryReviewPlanArg(vault, path))
			return
		}
	}
	preflight := buildWavePreflight(vault, idx, wave, inspectWavePreflightEnvironment(vault, wave))
	for _, blocker := range preflight.Blockers {
		r.Start.Blockers = append(r.Start.Blockers, "operational preflight: "+blocker)
	}
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
	b.WriteString("\nWhat needs your decision\n")
	if len(r.Decisions) == 0 {
		b.WriteString("- Nothing.\n")
	}
	for _, d := range r.Decisions {
		b.WriteString("- " + d.Title + ": " + d.Action + "\n")
	}
	b.WriteString("\nStart boundary\n")
	b.WriteString("- Plan: " + r.Start.PlanFingerprint + "; authorization: " + r.Start.Authorization + "; readiness: " + r.Start.Readiness + "\n- Next action: " + r.Start.NextAction + "\n")
	return b.String()
}
