package main

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// PromotionFailurePacket is the durable, bounded explanation of a red full
// promotion gate. Raw command output belongs in ArtifactRefs, never here: a
// packet is deliberately safe to put in a repair task, brief, or model prompt.
type PromotionFailurePacket struct {
	CandidateSHA       string         `json:"candidate_sha"`
	CandidateTreeHash  string         `json:"candidate_tree_hash"`
	ExpectedMainSHA    string         `json:"expected_main_sha"`
	GateCommand        string         `json:"gate_command"`
	GateProfile        string         `json:"gate_profile"`
	Toolchain          string         `json:"toolchain"`
	Runner             string         `json:"runner"`
	Defects            []GateDefect   `json:"defects"`
	LastGreenRef       string         `json:"last_green_ref,omitempty"`
	LastGreenStatus    string         `json:"last_green_status"`
	BisectionRef       string         `json:"bisection_ref,omitempty"`
	BisectionStatus    string         `json:"bisection_status"`
	OwningTaskID       string         `json:"owning_task_id,omitempty"`
	TouchedPaths       []string       `json:"touched_paths,omitempty"`
	Reproduction       string         `json:"reproduction"`
	ArtifactRefs       []string       `json:"artifact_refs,omitempty"`
	GateResult         GateTierResult `json:"gate_result"`
	ClassificationText string         `json:"-"`
}

type promotionFailureClass string

const (
	promotionFailureInfrastructure promotionFailureClass = "infrastructure"
	promotionFailureFlake          promotionFailureClass = "flake"
	promotionFailureIsolated       promotionFailureClass = "isolated_defect"
	promotionFailureAmbiguous      promotionFailureClass = "ambiguous"
)

type promotionFailureRoute struct {
	Class          promotionFailureClass
	Retry          bool
	Quarantine     bool
	Repair         bool
	ModelTriage    bool
	StableIdentity string
}

func promotionFailureOwner(candidate DepartureCandidate) string {
	if len(candidate.TaskSourceSHAs) != 1 || len(candidate.TaskStateRevisions) != 1 {
		return ""
	}
	for id := range candidate.TaskSourceSHAs {
		if _, ok := candidate.TaskStateRevisions[id]; ok {
			return id
		}
	}
	return ""
}

func promotionFailureTouchedPaths(candidate DepartureCandidate, owner string) ([]string, string) {
	if owner == "" || len(candidate.TaskSourceSHAs) != 1 {
		return nil, "unknown"
	}
	return []string{}, "none_proven"
}

// classifyPromotionFailure is intentionally deterministic. The caller only
// spends a model when the bounded evidence is genuinely ambiguous.
func classifyPromotionFailure(packet PromotionFailurePacket, policy GateTierPolicy) promotionFailureRoute {
	text := packet.ClassificationText
	identity := promotionFailureIdentity(packet)
	if matchesConfiguredFailurePattern(text, policy.InfrastructureFailurePatterns) {
		return promotionFailureRoute{Class: promotionFailureInfrastructure, Retry: true, Repair: true, StableIdentity: identity}
	}
	if matchesConfiguredFailurePattern(text, policy.FlakeFailurePatterns) {
		return promotionFailureRoute{Class: promotionFailureFlake, Quarantine: true, Retry: true, StableIdentity: identity}
	}
	if strings.TrimSpace(packet.OwningTaskID) != "" || (strings.TrimSpace(packet.BisectionStatus) == "proven" && strings.TrimSpace(packet.BisectionRef) != "") {
		return promotionFailureRoute{Class: promotionFailureIsolated, Repair: true, StableIdentity: identity}
	}
	return promotionFailureRoute{Class: promotionFailureAmbiguous, Repair: true, ModelTriage: true, StableIdentity: identity}
}

func matchesConfiguredFailurePattern(text string, patterns []string) bool {
	for _, pattern := range patterns {
		if pattern != "" && strings.Contains(text, pattern) {
			return true
		}
	}
	return false
}

func promotionFailureIdentity(packet PromotionFailurePacket) string {
	parts := []string{strings.TrimSpace(packet.GateCommand), strings.TrimSpace(packet.GateProfile), strings.TrimSpace(packet.Toolchain), strings.TrimSpace(packet.OwningTaskID)}
	for _, defect := range packet.Defects {
		parts = append(parts, strings.TrimSpace(defect.Target))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "promotion/" + hex.EncodeToString(sum[:8])
}

func promotionFailurePacket(candidate DepartureCandidate, gate DepartureGate, runner, output string, runErr error, policy GateTierPolicy, lastGreenRef, bisectionRef, owningTask string, touched, artifacts []string) PromotionFailurePacket {
	defects := harvestGateDefects(gate.Command, output, runErr, policy)
	return PromotionFailurePacket{
		CandidateSHA: candidate.CandidateSHA, CandidateTreeHash: candidate.CandidateTreeHash, ExpectedMainSHA: candidate.ExpectedDefaultBranchSHA,
		GateCommand: gate.Command, GateProfile: gate.Profile, Toolchain: gate.Toolchain, Runner: runner, Defects: defects,
		LastGreenRef: lastGreenRef, BisectionRef: bisectionRef, OwningTaskID: owningTask,
		LastGreenStatus: "unknown", BisectionStatus: "not_run",
		TouchedPaths: uniqueStrings(touched), Reproduction: strings.TrimSpace(gate.Command), ArtifactRefs: uniqueStrings(artifacts),
		ClassificationText: output,
	}
}

func containsAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}
