package main

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

const scheduledPromotionMorningBriefSchema = "tusker.scheduled-promotion-morning-brief/v1"

type scheduledPromotionMorningBriefFeature struct {
	Enabled             bool   `json:"enabled"`
	Mode                string `json:"mode"`
	Configured          bool   `json:"configured"`
	Provenance          string `json:"provenance"`
	PromotionAuthorized bool   `json:"promotionAuthorized"`
	ReleaseAuthorized   bool   `json:"releaseAuthorized"`
	Summary             string `json:"summary"`
}

type scheduledPromotionMorningBriefEmptyStates struct {
	LandedLastNight    string `json:"landedLastNight"`
	BlockedOrRepairing string `json:"blockedOrRepairing"`
	NeedsYourDecision  string `json:"needsYourDecision"`
}

type scheduledPromotionMorningBriefLanded struct {
	ID                string              `json:"id"`
	Summary           string              `json:"summary,omitempty"`
	PromotedRef       string              `json:"promotedRef"`
	PromotedSHA       string              `json:"promotedSha"`
	PromotedAt        string              `json:"promotedAt"`
	ReleasedRevision  string              `json:"releasedRevision,omitempty"`
	ReleasedAt        string              `json:"releasedAt,omitempty"`
	ReleaseProfile    string              `json:"releaseProfile,omitempty"`
	TaskIDs           []string            `json:"taskIds"`
	AcceptedArtifacts []waveBriefArtifact `json:"acceptedArtifacts"`
	Href              string              `json:"href"`
}

type scheduledPromotionMorningBriefBlocked struct {
	ID                   string   `json:"id"`
	Kind                 string   `json:"kind"`
	State                string   `json:"state"`
	FirstActionableCause string   `json:"firstActionableCause"`
	AffectedScope        []string `json:"affectedScope"`
	AutomaticAction      string   `json:"automaticAction"`
	RepairTaskID         string   `json:"repairTaskId,omitempty"`
	ArtifactRefs         []string `json:"artifactRefs,omitempty"`
	Href                 string   `json:"href"`
}

type scheduledPromotionMorningBriefDecision struct {
	GateID        string   `json:"gateId"`
	Action        string   `json:"action"`
	WhyHuman      string   `json:"whyHuman"`
	Verification  string   `json:"verification"`
	AffectedScope []string `json:"affectedScope"`
	ResumeID      string   `json:"resumeId"`
	Href          string   `json:"href"`
}

// scheduledPromotionMorningBrief is the one versioned wire projection used by
// both the logbook CLI and Serve. The three slices below are deliberately the
// only primary lists; everything else explains their scope or honest emptiness.
type scheduledPromotionMorningBrief struct {
	Schema             string                                    `json:"schema"`
	ProjectID          string                                    `json:"projectId"`
	Night              string                                    `json:"night"`
	GeneratedAt        string                                    `json:"generatedAt"`
	Feature            scheduledPromotionMorningBriefFeature     `json:"feature"`
	EmptyStates        scheduledPromotionMorningBriefEmptyStates `json:"emptyStates"`
	LandedLastNight    []scheduledPromotionMorningBriefLanded    `json:"landedLastNight"`
	BlockedOrRepairing []scheduledPromotionMorningBriefBlocked   `json:"blockedOrRepairing"`
	NeedsYourDecision  []scheduledPromotionMorningBriefDecision  `json:"needsYourDecision"`
}

type scheduledPromotionMorningBriefFacts struct {
	Index      v7Index
	Logbook    tuskerLogbook
	Departures []DepartureRun
	WaveBriefs []waveBrief
}

func scheduledPromotionMorningBriefCmd(args Args) error {
	if args.Bool("write") {
		return tuskerError(errorInvalidArg, "the scheduled-promotion morning brief is a read-only projection; --write is not supported")
	}
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	now := time.Now()
	night, err := scheduledPromotionMorningBriefNight(args.String("date"), now)
	if err != nil {
		return err
	}
	wfFile, err := loadWorkflow(vaultPath)
	if err != nil {
		return err
	}
	projectID := firstNonEmpty(strings.TrimSpace(args.String("project")), v7ProjectID(vaultPath))

	var store *RuntimeStore
	if wfFile.Data.ScheduledPromotion.Effective.Observe {
		store, err = OpenRuntimeStoreReadOnly(firstNonEmpty(strings.TrimSpace(args.String("state-root")), DefaultStateRoot()))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if store != nil {
			defer store.Close()
		}
	}
	brief, err := buildScheduledPromotionMorningBrief(vaultPath, projectID, wfFile.Data, store, night, now)
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(brief)
		return nil
	}
	fmt.Print(renderScheduledPromotionMorningBrief(brief))
	return nil
}

func scheduledPromotionMorningBriefNight(raw string, now time.Time) (time.Time, error) {
	location := now.Location()
	if location == nil {
		location = time.Local
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		local := now.In(location).AddDate(0, 0, -1)
		return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location), nil
	}
	parsed, err := time.ParseInLocation("2006-01-02", raw, location)
	if err != nil {
		return time.Time{}, tuskerError(errorInvalidArg, "logbook --scheduled-promotion --date must be YYYY-MM-DD: "+raw)
	}
	return parsed, nil
}

func buildScheduledPromotionMorningBrief(vaultPath, projectID string, wf Workflow, store *RuntimeStore, night, now time.Time) (scheduledPromotionMorningBrief, error) {
	projection := wf.ScheduledPromotion.Effective
	brief := newScheduledPromotionMorningBrief(projectID, projection, night, now)
	// Feature-off is a first-class result, not a degraded runtime read. Returning
	// here also prevents stale departure rows from looking like pending alarms.
	if !projection.Observe {
		return brief, nil
	}

	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return scheduledPromotionMorningBrief{}, err
	}
	logbook, err := buildTuskerLogbook(vaultPath, night, now.UTC())
	if err != nil {
		return scheduledPromotionMorningBrief{}, err
	}
	facts := scheduledPromotionMorningBriefFacts{Index: idx, Logbook: logbook, Departures: []DepartureRun{}, WaveBriefs: []waveBrief{}}
	runsByTask := map[string]RunStatus{}
	if store != nil {
		facts.Departures, err = store.ListDepartureRuns(projectID)
		if err != nil {
			return scheduledPromotionMorningBrief{}, err
		}
		runs, listErr := store.ListRuns()
		if listErr != nil {
			return scheduledPromotionMorningBrief{}, listErr
		}
		for _, run := range runs {
			if projectID == "" || run.ProjectID == projectID {
				runsByTask[firstNonEmpty(run.ItemID, run.RecordID)] = run
			}
		}
	}
	waveIDs := make([]string, 0, len(idx.Waves))
	for id := range idx.Waves {
		waveIDs = append(waveIDs, id)
	}
	sort.Strings(waveIDs)
	for _, id := range waveIDs {
		facts.WaveBriefs = append(facts.WaveBriefs, buildWaveBriefWithRuns(idx, idx.Waves[id], runsByTask))
	}
	return composeScheduledPromotionMorningBrief(brief, facts, night), nil
}

func newScheduledPromotionMorningBrief(projectID string, projection ScheduledPromotionProjection, night, now time.Time) scheduledPromotionMorningBrief {
	mode := firstNonEmpty(strings.TrimSpace(projection.Mode), scheduledPromotionDisabled)
	feature := scheduledPromotionMorningBriefFeature{
		Enabled:             projection.Observe,
		Mode:                mode,
		Configured:          projection.Configured,
		Provenance:          safePacketText(projection.Provenance, 160),
		PromotionAuthorized: projection.Promote,
		ReleaseAuthorized:   projection.Release,
		Summary:             scheduledPromotionFeatureSummary(projection),
	}
	empty := scheduledPromotionMorningBriefEmptyStates{
		LandedLastNight:    "No scheduled promotion landed for this night.",
		BlockedOrRepairing: "No scheduled-promotion run is blocked or repairing.",
		NeedsYourDecision:  "No human decision is needed.",
	}
	if !projection.Observe {
		empty = scheduledPromotionMorningBriefEmptyStates{
			LandedLastNight:    "Scheduled promotion is off, so no overnight promotion is projected.",
			BlockedOrRepairing: "Scheduled promotion is off; there are no scheduled-promotion blockers to act on.",
			NeedsYourDecision:  "Scheduled promotion is off; no decision is requested.",
		}
	}
	return scheduledPromotionMorningBrief{
		Schema: scheduledPromotionMorningBriefSchema, ProjectID: projectID, Night: night.Format("2006-01-02"),
		GeneratedAt: now.UTC().Format(time.RFC3339), Feature: feature, EmptyStates: empty,
		LandedLastNight: []scheduledPromotionMorningBriefLanded{}, BlockedOrRepairing: []scheduledPromotionMorningBriefBlocked{},
		NeedsYourDecision: []scheduledPromotionMorningBriefDecision{},
	}
}

func scheduledPromotionFeatureSummary(projection ScheduledPromotionProjection) string {
	if !projection.Observe {
		return "Scheduled promotion is off for this project."
	}
	switch projection.Mode {
	case scheduledPromotionShadow:
		return "Scheduled promotion is observing this project; it cannot stage, promote, or release."
	case scheduledPromotionStage:
		return "Scheduled promotion may stage candidates; it cannot promote or release."
	case scheduledPromotionPromote:
		if projection.Release {
			return "Scheduled promotion may promote full-green candidates and run the separately authorized release profile."
		}
		return "Scheduled promotion may promote full-green candidates; release is not authorized."
	default:
		return "Scheduled promotion is enabled with the recorded workflow permissions."
	}
}

func composeScheduledPromotionMorningBrief(brief scheduledPromotionMorningBrief, facts scheduledPromotionMorningBriefFacts, night time.Time) scheduledPromotionMorningBrief {
	artifactsByTask := scheduledPromotionArtifactsByTask(facts.WaveBriefs)
	outcomeByTask := map[string]string{}
	for _, item := range facts.Logbook.Shipped {
		if outcome := safePacketText(item.Outcome, 240); outcome != "" {
			outcomeByTask[item.TaskID] = outcome
		}
	}

	blockedTaskIDs := map[string]bool{}
	for _, departure := range facts.Departures {
		if scheduledPromotionDepartureLandedOnNight(departure, night) {
			item := scheduledPromotionLandedItem(brief.ProjectID, departure, artifactsByTask, outcomeByTask)
			brief.LandedLastNight = append(brief.LandedLastNight, item)
		}
		if scheduledPromotionDepartureBlockedForNight(departure, night) {
			item := scheduledPromotionBlockedDeparture(brief.ProjectID, departure)
			brief.BlockedOrRepairing = append(brief.BlockedOrRepairing, item)
			for _, id := range item.AffectedScope {
				blockedTaskIDs[id] = true
			}
		}
	}

	for _, waveBrief := range facts.WaveBriefs {
		for _, rework := range waveBrief.Rework {
			if blockedTaskIDs[rework.TaskID] {
				continue
			}
			scope := scheduledPromotionScope(rework.AffectedTasks, brief.ProjectID)
			brief.BlockedOrRepairing = append(brief.BlockedOrRepairing, scheduledPromotionMorningBriefBlocked{
				ID: rework.TaskID, Kind: "wave_rework", State: safePacketText(rework.State, 80),
				FirstActionableCause: safePacketText(firstNonEmpty(rework.Failure, "Resolve the recorded task failure before the next promotion attempt."), 320),
				AffectedScope:        scope,
				AutomaticAction:      "Tusker has parked this scope for machine rework and will not promote it while the failure remains.",
				Href:                 firstNonEmpty(rework.TaskHref, taskDeepLink(brief.ProjectID, rework.TaskID)),
			})
			for _, id := range scope {
				blockedTaskIDs[id] = true
			}
		}
	}
	for _, repair := range facts.Logbook.Meaning.Repairs {
		if blockedTaskIDs[repair.TaskID] {
			continue
		}
		task, ok := facts.Index.Tasks[repair.TaskID]
		scope := []string{repair.TaskID}
		cause := "A machine repair task was opened from the night's recorded failure."
		href := firstNonEmpty(repair.Link, taskDeepLink(brief.ProjectID, repair.TaskID))
		if ok {
			scope = waveDependentClosure(facts.Index, repair.TaskID)
			cause = firstNonEmpty(strings.TrimSpace(stringField(task.Data, "next_action")), cause)
			href = taskDeepLink(firstNonEmpty(stringField(task.Data, "project"), brief.ProjectID), repair.TaskID)
		}
		brief.BlockedOrRepairing = append(brief.BlockedOrRepairing, scheduledPromotionMorningBriefBlocked{
			ID: repair.TaskID, Kind: "repair", State: "repairing", FirstActionableCause: safePacketText(cause, 320),
			AffectedScope:   scheduledPromotionScope(scope, brief.ProjectID),
			AutomaticAction: "The repair task owns machine follow-up; promotion remains stopped for its affected scope.",
			RepairTaskID:    repair.TaskID, Href: href,
		})
	}

	allTaskIDs := make([]string, 0, len(facts.Index.Tasks))
	for id := range facts.Index.Tasks {
		allTaskIDs = append(allTaskIDs, id)
	}
	sort.Strings(allTaskIDs)
	for _, action := range validWaveHumanActions(facts.Index, allTaskIDs) {
		gate := facts.Index.Gates[action.GateID]
		brief.NeedsYourDecision = append(brief.NeedsYourDecision, scheduledPromotionMorningBriefDecision{
			GateID: action.GateID, Action: safePacketText(action.Action, 280),
			WhyHuman:      safePacketText(v7GateBoundaryText(gate), 280),
			Verification:  safePacketText(stringField(gate.Data, "verification"), 280),
			AffectedScope: scheduledPromotionScope(action.BlockedTaskIDs, brief.ProjectID),
			ResumeID:      action.ResumeID, Href: action.GateHref,
		})
	}

	sort.SliceStable(brief.LandedLastNight, func(i, j int) bool {
		if brief.LandedLastNight[i].PromotedAt != brief.LandedLastNight[j].PromotedAt {
			return brief.LandedLastNight[i].PromotedAt > brief.LandedLastNight[j].PromotedAt
		}
		return brief.LandedLastNight[i].ID < brief.LandedLastNight[j].ID
	})
	sort.SliceStable(brief.BlockedOrRepairing, func(i, j int) bool {
		if brief.BlockedOrRepairing[i].ID != brief.BlockedOrRepairing[j].ID {
			return brief.BlockedOrRepairing[i].ID < brief.BlockedOrRepairing[j].ID
		}
		return brief.BlockedOrRepairing[i].Kind < brief.BlockedOrRepairing[j].Kind
	})
	sort.SliceStable(brief.NeedsYourDecision, func(i, j int) bool {
		return brief.NeedsYourDecision[i].GateID < brief.NeedsYourDecision[j].GateID
	})
	return brief
}

func scheduledPromotionArtifactsByTask(briefs []waveBrief) map[string][]waveBriefArtifact {
	out := map[string][]waveBriefArtifact{}
	seen := map[string]bool{}
	for _, brief := range briefs {
		for _, artifact := range brief.SeeIt {
			key := strings.Join([]string{artifact.TaskID, artifact.EvidenceRef, artifact.ArtifactRef, artifact.Kind}, "\x00")
			if seen[key] {
				continue
			}
			seen[key] = true
			artifact.Summary = safePacketText(artifact.Summary, 240)
			artifact.AcceptanceIDs = dedupeSortedStrings(artifact.AcceptanceIDs)
			out[artifact.TaskID] = append(out[artifact.TaskID], artifact)
		}
	}
	for taskID := range out {
		sort.SliceStable(out[taskID], func(i, j int) bool {
			left, right := out[taskID][i], out[taskID][j]
			if left.Priority != right.Priority {
				return left.Priority < right.Priority
			}
			if left.EvidenceRef != right.EvidenceRef {
				return left.EvidenceRef < right.EvidenceRef
			}
			return left.ArtifactRef < right.ArtifactRef
		})
	}
	return out
}

func scheduledPromotionLandedItem(projectID string, run DepartureRun, artifactsByTask map[string][]waveBriefArtifact, outcomeByTask map[string]string) scheduledPromotionMorningBriefLanded {
	taskIDs := scheduledPromotionDepartureTaskIDs(run)
	artifacts := []waveBriefArtifact{}
	summaries := []string{}
	for _, taskID := range taskIDs {
		artifacts = append(artifacts, artifactsByTask[taskID]...)
		if summary := outcomeByTask[taskID]; summary != "" {
			summaries = append(summaries, summary)
		}
	}
	item := scheduledPromotionMorningBriefLanded{
		ID: run.ID, Summary: safePacketText(strings.Join(dedupeSortedStrings(summaries), "; "), 320),
		PromotedRef: run.Promotion.CommittedRef, PromotedSHA: run.Promotion.CommittedSHA, PromotedAt: run.Promotion.CommittedAt,
		TaskIDs: taskIDs, AcceptedArtifacts: artifacts, Href: scheduledPromotionDepartureHref(projectID, run.ID),
	}
	if scheduledPromotionReleaseSucceeded(run.Release) {
		item.ReleasedRevision = run.Release.Revision
		item.ReleasedAt = run.Release.CompletedAt
		item.ReleaseProfile = run.Release.Profile
	}
	return item
}

func scheduledPromotionDepartureBlockedForNight(run DepartureRun, night time.Time) bool {
	if !scheduledPromotionDepartureIsBlocked(run) {
		return false
	}
	if run.State == DepartureStateRepairing {
		return true
	}
	for _, at := range []string{
		run.ScheduledWindow, run.UpdatedAt, run.Gate.FinishedAt, run.Promotion.AttemptedAt,
		run.Promotion.CommittedAt, run.Release.AttemptedAt, run.Release.CompletedAt,
	} {
		if scheduledPromotionTimestampOnNight(at, night) {
			return true
		}
	}
	return false
}

func scheduledPromotionDepartureLandedOnNight(run DepartureRun, night time.Time) bool {
	return strings.TrimSpace(run.Promotion.CommittedRef) != "" &&
		strings.TrimSpace(run.Promotion.CommittedSHA) != "" &&
		scheduledPromotionTimestampOnNight(run.Promotion.CommittedAt, night)
}

func scheduledPromotionTimestampOnNight(raw string, night time.Time) bool {
	parsed, ok := parseTuskerTime(strings.TrimSpace(raw))
	if !ok {
		return false
	}
	return parsed.In(night.Location()).Format("2006-01-02") == night.Format("2006-01-02")
}

func scheduledPromotionDepartureIsBlocked(run DepartureRun) bool {
	switch run.State {
	case DepartureStateBlocked, DepartureStateFailed, DepartureStateRepairing:
		return true
	}
	gateStatus := strings.ToLower(strings.TrimSpace(run.Gate.Status))
	if gateStatus == "failed" || gateStatus == "red" {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(run.Release.Status), "failed") {
		return true
	}
	hasRef := strings.TrimSpace(run.Promotion.CommittedRef) != ""
	hasSHA := strings.TrimSpace(run.Promotion.CommittedSHA) != ""
	return hasRef != hasSHA || (strings.TrimSpace(run.Promotion.AttemptedAt) != "" && !hasRef && !hasSHA)
}

func scheduledPromotionBlockedDeparture(projectID string, run DepartureRun) scheduledPromotionMorningBriefBlocked {
	kind := "departure"
	if strings.EqualFold(strings.TrimSpace(run.Release.Status), "failed") {
		kind = "release"
	} else if strings.EqualFold(strings.TrimSpace(run.Gate.Status), "failed") || strings.EqualFold(strings.TrimSpace(run.Gate.Status), "red") {
		kind = "promotion_gate"
	}
	scope := scheduledPromotionScope(firstNonEmptyStringSlice(
		run.Gate.Failure.AffectedTaskIDs,
		run.Gate.Failure.Packet.CandidateTaskIDs,
		scheduledPromotionDepartureTaskIDs(run),
		[]string{run.Gate.Failure.OwningTaskID},
	), projectID)
	cause := scheduledPromotionDepartureCause(run, kind)
	action := scheduledPromotionDepartureAutomaticAction(run, kind)
	href := scheduledPromotionDepartureHref(projectID, run.ID)
	if repairID := strings.TrimSpace(run.Gate.Failure.RepairTaskID); repairID != "" {
		href = taskDeepLink(projectID, repairID)
	}
	artifactRefs := append([]string{}, run.Gate.Failure.ArtifactRefs...)
	artifactRefs = append(artifactRefs, run.Gate.Failure.Packet.ArtifactRefs...)
	artifactRefs = append(artifactRefs, run.Gate.ArtifactRef, run.Release.ArtifactRef)
	artifactRefs = nonEmptyDedupeSortedStrings(artifactRefs)
	return scheduledPromotionMorningBriefBlocked{
		ID: run.ID, Kind: kind, State: string(run.State),
		FirstActionableCause: cause, AffectedScope: scope, AutomaticAction: action,
		RepairTaskID: strings.TrimSpace(run.Gate.Failure.RepairTaskID), ArtifactRefs: artifactRefs, Href: href,
	}
}

func scheduledPromotionDepartureCause(run DepartureRun, kind string) string {
	if kind == "release" {
		return safePacketText(firstNonEmpty(
			run.BlockReason,
			fmt.Sprintf("Release profile %s failed after the promoted revision was recorded.", firstNonEmpty(run.Release.Profile, "the authorized profile")),
		), 320)
	}
	if len(run.Gate.Failure.Packet.Defects) > 0 {
		defect := run.Gate.Failure.Packet.Defects[0]
		return safePacketText(firstNonEmpty(
			strings.TrimSpace(strings.Join(nonEmptyStrings(defect.Target, defect.Excerpt), ": ")),
			defect.Command,
			run.BlockReason,
			run.Gate.Failure.Packet.Reproduction,
			"Inspect the first recorded gate defect before retrying promotion.",
		), 320)
	}
	if reason := safePacketText(run.BlockReason, 320); reason != "" {
		return reason
	}
	hasRef := strings.TrimSpace(run.Promotion.CommittedRef) != ""
	hasSHA := strings.TrimSpace(run.Promotion.CommittedSHA) != ""
	if hasRef != hasSHA || (run.Promotion.AttemptedAt != "" && !hasRef && !hasSHA) {
		return "The promotion outcome is incomplete or ambiguous; inspect the durable departure before retrying."
	}
	return safePacketText(firstNonEmpty(
		run.Gate.Failure.Packet.Reproduction,
		"Inspect the departure record and clear its recorded failure before retrying promotion.",
	), 320)
}

func scheduledPromotionDepartureAutomaticAction(run DepartureRun, kind string) string {
	if kind == "release" {
		return "Release is stopped; the already promoted revision remains unchanged while the release failure is repaired."
	}
	switch strings.ToLower(strings.TrimSpace(run.Gate.Failure.Action)) {
	case "owner_rework":
		return "Tusker returned the isolated owner and its affected scope to machine rework."
	case "infrastructure_repair":
		return "Tusker opened or resumed infrastructure repair and keeps promotion stopped."
	case "flake_quarantine":
		return "Tusker is quarantining the classified flake before a deterministic retry."
	case "ambiguous_repair":
		if run.Gate.Failure.ModelTriage {
			return "Tusker opened bounded repair; optional model triage is separately authorized for this ambiguous failure."
		}
		return "Tusker opened bounded repair without spending on model triage."
	}
	if run.State == DepartureStateRepairing || run.Gate.Failure.RepairTaskID != "" {
		return "The recorded repair task owns machine follow-up; promotion remains stopped for its scope."
	}
	return "No automatic retry is recorded; promotion remains stopped until the durable failure is reconciled."
}

func scheduledPromotionReleaseSucceeded(release DepartureRelease) bool {
	return strings.TrimSpace(release.Revision) != "" &&
		strings.TrimSpace(release.CompletedAt) != "" &&
		strings.EqualFold(strings.TrimSpace(release.Status), "released")
}

func scheduledPromotionDepartureTaskIDs(run DepartureRun) []string {
	ids := make([]string, 0, len(run.Candidate.TaskStateRevisions)+len(run.Candidate.TaskSourceSHAs))
	for id := range run.Candidate.TaskStateRevisions {
		ids = append(ids, id)
	}
	for id := range run.Candidate.TaskSourceSHAs {
		ids = append(ids, id)
	}
	ids = append(ids, run.Gate.Failure.Packet.CandidateTaskIDs...)
	return nonEmptyDedupeSortedStrings(ids)
}

func scheduledPromotionScope(values []string, projectID string) []string {
	values = nonEmptyDedupeSortedStrings(values)
	if len(values) == 0 {
		return []string{firstNonEmpty(strings.TrimSpace(projectID), "project")}
	}
	return values
}

func firstNonEmptyStringSlice(values ...[]string) []string {
	for _, value := range values {
		if len(nonEmptyDedupeSortedStrings(value)) > 0 {
			return value
		}
	}
	return nil
}

func nonEmptyDedupeSortedStrings(values []string) []string {
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			filtered = append(filtered, value)
		}
	}
	return dedupeSortedStrings(filtered)
}

func nonEmptyStrings(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func scheduledPromotionDepartureHref(projectID, departureID string) string {
	return projectOpsDeepLink(projectID) + "?brief=morning&departure=" + url.QueryEscape(departureID)
}

func renderScheduledPromotionMorningBrief(brief scheduledPromotionMorningBrief) string {
	var b strings.Builder
	b.WriteString("# Scheduled promotion morning brief — " + brief.Night + "\n\n")
	b.WriteString(brief.Feature.Summary + "\n\n")

	b.WriteString("## Landed last night\n\n")
	if len(brief.LandedLastNight) == 0 {
		b.WriteString(brief.EmptyStates.LandedLastNight + "\n\n")
	} else {
		for _, item := range brief.LandedLastNight {
			b.WriteString(fmt.Sprintf("- Promoted `%s` to `%s`", item.PromotedSHA, item.PromotedRef))
			if item.ReleasedRevision != "" {
				b.WriteString(fmt.Sprintf("; released revision `%s`", item.ReleasedRevision))
			}
			if item.Summary != "" {
				b.WriteString(". " + item.Summary)
			}
			b.WriteString(fmt.Sprintf(" ([open departure](%s))\n", item.Href))
		}
		b.WriteString("\n")
	}

	b.WriteString("## Blocked or repairing\n\n")
	if len(brief.BlockedOrRepairing) == 0 {
		b.WriteString(brief.EmptyStates.BlockedOrRepairing + "\n\n")
	} else {
		for _, item := range brief.BlockedOrRepairing {
			b.WriteString(fmt.Sprintf("- %s — %s Scope: %s. Tusker: %s ([open](%s))\n",
				item.ID, item.FirstActionableCause, strings.Join(item.AffectedScope, ", "), item.AutomaticAction, item.Href))
		}
		b.WriteString("\n")
	}

	b.WriteString("## Needs your decision\n\n")
	if len(brief.NeedsYourDecision) == 0 {
		b.WriteString(brief.EmptyStates.NeedsYourDecision + "\n\n")
	} else {
		for _, item := range brief.NeedsYourDecision {
			b.WriteString(fmt.Sprintf("- %s — %s Verify: %s. ([open gate](%s))\n", item.GateID, item.Action, item.Verification, item.Href))
		}
		b.WriteString("\n")
	}
	return b.String()
}
