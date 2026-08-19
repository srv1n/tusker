package main

// Historical actor correction is deliberately a projection, not a rewrite.
// The original event/frontmatter bytes remain immutable; a human-gated,
// append-only actor_correction event records the correction and its exact
// source hash.  Corrections never become review results or acceptance proof.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	actorCorrectionSchema              = "tusker.actor-correction/v1"
	humanControlReceiptSchema          = "tusker.human-control-receipt/v1"
	humanControlReceiptUnavailableCode = "HUMAN_CONTROL_RECEIPT_UNAVAILABLE"
	actorCorrectionEventKind           = "actor_correction"
	actorCorrectionProjectionID        = "actor-corrections"
)

type actorCorrectionTarget struct {
	EventID        string `json:"event_id"`
	OriginalSHA256 string `json:"original_sha256"`
	OriginalActor  string `json:"original_actor"`
	CorrectedActor string `json:"corrected_actor"`
}

// actorCorrectionReceipt is carried as the exact JSON string in a satisfied
// human decision gate's satisfaction_evidence field. Free text is not a
// receipt: the typed shape binds the gate, actor, source hashes, and target
// correction values as one human decision.
type actorCorrectionReceipt struct {
	Schema      string                  `json:"schema"`
	Kind        string                  `json:"kind"`
	GateID      string                  `json:"gate_id"`
	Actor       string                  `json:"actor"`
	IssuedAt    string                  `json:"issued_at"`
	ScopeDigest string                  `json:"scope_digest"`
	Targets     []actorCorrectionTarget `json:"targets"`
}

type actorCorrectionProjection struct {
	TargetEventID     string `json:"target_event_id"`
	TargetObject      string `json:"target_object"`
	TargetObjectKind  string `json:"target_object_kind"`
	OriginalSHA256    string `json:"original_sha256"`
	OriginalActor     string `json:"original_actor"`
	CorrectedActor    string `json:"corrected_actor"`
	CorrectionEventID string `json:"correction_event_id"`
	CorrectedBy       string `json:"corrected_by"`
	CorrectedAt       string `json:"corrected_at"`
	GateID            string `json:"gate_id"`
	CountsAsReview    bool   `json:"counts_as_review"`
}

func actorCorrectionV7Cmd(args Args) error {
	switch strings.ToLower(strings.TrimSpace(args.String("_pos0"))) {
	case "plan":
		return actorCorrectionPlanCmd(args)
	case "apply":
		return actorCorrectionApplyCmd(args)
	case "list", "status":
		return actorCorrectionListCmd(args)
	default:
		return tuskerError(errorMissingArg, "Usage: tusker actor correction plan|apply|list <event-id[,event-id...]> --original-sha256 <sha256:...> --corrected-actor <agent:...>")
	}
}

func actorCorrectionPlanCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	targets, err := resolveActorCorrectionTargets(vaultPath, args)
	if err != nil {
		return err
	}
	receipt := actorCorrectionReceipt{
		Schema:   humanControlReceiptSchema,
		Kind:     actorCorrectionSchema,
		GateID:   strings.TrimSpace(args.String("gate")),
		Actor:    strings.TrimSpace(args.String("by")),
		IssuedAt: time.Now().UTC().Format(time.RFC3339),
		Targets:  targets,
	}
	receipt.ScopeDigest = actorCorrectionScopeDigest(receipt.Targets)
	if args.Bool("json") {
		emitJSON(map[string]any{
			"ok":               true,
			"read_only":        true,
			"scope_digest":     receipt.ScopeDigest,
			"targets":          receipt.Targets,
			"receipt_template": receipt,
			"counts_as_review": false,
		})
		return nil
	}
	fmt.Printf("Actor correction plan (%s; read-only)\n", receipt.ScopeDigest)
	for _, target := range receipt.Targets {
		fmt.Printf("  %s %s -> %s\n", target.EventID, target.OriginalActor, target.CorrectedActor)
	}
	fmt.Println("Human must satisfy the named decision gate with the typed receipt, then rerun actor correction apply.")
	return nil
}

func actorCorrectionApplyCmd(_ Args) error {
	// No exact-verification human-control producer/validator exists in this
	// checkout. A gate file, --by string, TTY, or absent agent markers are not
	// an authenticity boundary, so apply is intentionally fail-closed.
	return tuskerError(humanControlReceiptUnavailableCode,
		"actor correction apply is unavailable: no non-forgeable typed human-control receipt producer/validator is installed",
		withHint("use actor correction plan/list; install the exact-verification human-control authority before applying"))
}

func validateActorCorrectionObject(object string) error {
	object = strings.TrimSpace(object)
	if object == "" || filepath.IsAbs(object) || filepath.Clean(object) != object || strings.ContainsAny(object, `/\\`) || strings.IndexFunc(object, func(r rune) bool { return r < 0x20 }) >= 0 {
		return tuskerError(errorInvalidField, "actor correction target object must be a safe vault-local identifier")
	}
	return nil
}

func actorCorrectionListCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	projection, err := loadV7ActorCorrectionProjection(vaultPath)
	if err != nil {
		return err
	}
	items := make([]actorCorrectionProjection, 0, len(projection))
	for _, item := range projection {
		if object := strings.TrimSpace(args.String("object")); object != "" && item.TargetObject != object {
			continue
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].TargetEventID < items[j].TargetEventID })
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "read_only": true, "projection": actorCorrectionProjectionID, "corrections": items})
		return nil
	}
	for _, item := range items {
		fmt.Printf("%s\t%s\t%s\t%s\n", item.TargetEventID, item.OriginalActor, item.CorrectedActor, item.GateID)
	}
	return nil
}

type v7ActorCorrectionRawEvent struct {
	ID         string
	Object     string
	ObjectKind string
	Actor      string
	At         string
	RawSHA256  string
}

func resolveActorCorrectionTargets(vaultPath string, args Args) ([]actorCorrectionTarget, error) {
	ids := splitCSV(firstNonEmpty(args.String("event-ids"), args.String("event-id"), args.String("_pos1")))
	hashes := splitCSV(firstNonEmpty(args.String("original-sha256"), args.String("original-hashes"), args.String("hash")))
	corrected := splitCSV(firstNonEmpty(args.String("corrected-actors"), args.String("corrected-actor")))
	if len(ids) == 0 {
		return nil, tuskerError(errorMissingArg, "actor correction requires --event-id <event-id[,event-id...]> ")
	}
	if len(ids) != len(hashes) || len(ids) != len(corrected) {
		return nil, tuskerError(errorInvalidArg, "actor correction requires one --original-sha256 and --corrected-actor per event id")
	}
	targets := make([]actorCorrectionTarget, 0, len(ids))
	seen := map[string]bool{}
	for i, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			return nil, tuskerError(errorInvalidArg, "actor correction event ids must be non-blank and unique")
		}
		seen[id] = true
		hash := canonicalActorCorrectionHash(hashes[i])
		if hash == "" {
			return nil, tuskerError(errorInvalidArg, "actor correction requires sha256:<64 hex> source hashes")
		}
		actor, err := canonicalActorCorrectionActor(corrected[i])
		if err != nil {
			return nil, err
		}
		event, err := readV7ActorCorrectionTarget(vaultPath, id)
		if err != nil {
			return nil, err
		}
		if err := validateActorCorrectionObject(event.Object); err != nil {
			return nil, err
		}
		targets = append(targets, actorCorrectionTarget{EventID: id, OriginalSHA256: hash, OriginalActor: event.Actor, CorrectedActor: actor})
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].EventID < targets[j].EventID })
	return targets, nil
}

func canonicalActorCorrectionHash(raw string) string {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "sha256:") || len(raw) != len("sha256:")+64 {
		return ""
	}
	if _, err := hex.DecodeString(strings.TrimPrefix(raw, "sha256:")); err != nil {
		return ""
	}
	return "sha256:" + strings.ToLower(strings.TrimPrefix(raw, "sha256:"))
}

func canonicalActorCorrectionActor(raw string) (string, error) {
	parts := strings.SplitN(strings.TrimSpace(raw), ":", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[1]) == "" {
		return "", tuskerError(errorInvalidField, "corrected actor must be a qualified non-human actor")
	}
	kind, name := strings.ToLower(strings.TrimSpace(parts[0])), strings.TrimSpace(parts[1])
	// A correction may preserve an agent attribution or explicitly mark the
	// historical source as unknown. It may never mint a human/reviewer verdict.
	if kind != "agent" && kind != "unknown" && kind != "unverified" && kind != "historical" {
		return "", tuskerError(errorInvalidField, "corrected actor must use agent:, unknown:, unverified:, or historical: (human/reviewer authority is not correction data)")
	}
	return kind + ":" + name, nil
}

func readV7ActorCorrectionTarget(vaultPath, eventID string) (v7ActorCorrectionRawEvent, error) {
	var found *v7ActorCorrectionRawEvent
	eventsRoot := filepath.Join(vaultPath, "events")
	err := filepath.WalkDir(eventsRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !strings.HasSuffix(entry.Name(), ".json") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var data map[string]any
		if json.Unmarshal(raw, &data) != nil || stringField(data, "id") != eventID {
			return nil
		}
		if found != nil {
			return fmt.Errorf("event id %s is not unique", eventID)
		}
		digest := sha256.Sum256(raw)
		found = &v7ActorCorrectionRawEvent{ID: eventID, Object: stringField(data, "object"), ObjectKind: stringField(data, "object_kind"), Actor: stringField(data, "actor"), At: stringField(data, "at"), RawSHA256: "sha256:" + hex.EncodeToString(digest[:])}
		return nil
	})
	if err != nil {
		return v7ActorCorrectionRawEvent{}, err
	}
	if found == nil {
		return v7ActorCorrectionRawEvent{}, tuskerError(errorNotFound, "V7 event not found: "+eventID)
	}
	if found.Object == "" || found.ObjectKind == "" || found.Actor == "" {
		return v7ActorCorrectionRawEvent{}, tuskerError(errorInvalidField, "V7 event "+eventID+" is missing correction identity fields")
	}
	return *found, nil
}

func actorCorrectionScopeDigest(targets []actorCorrectionTarget) string {
	copyTargets := append([]actorCorrectionTarget(nil), targets...)
	sort.Slice(copyTargets, func(i, j int) bool { return copyTargets[i].EventID < copyTargets[j].EventID })
	raw, _ := json.Marshal(copyTargets)
	digest := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func loadV7ActorCorrectionProjection(vaultPath string) (map[string]actorCorrectionProjection, error) {
	projection := map[string]actorCorrectionProjection{}
	eventsRoot := filepath.Join(vaultPath, "events")
	err := filepath.WalkDir(eventsRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !strings.HasSuffix(entry.Name(), ".json") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var data map[string]any
		if json.Unmarshal(raw, &data) != nil || stringField(data, "event_kind") != actorCorrectionEventKind {
			return nil
		}
		payload := mapField(data, "payload")
		targetID := stringField(payload, "target_event_id")
		if targetID == "" || stringField(payload, "schema") != actorCorrectionSchema || boolField(payload, "counts_as_review") {
			return fmt.Errorf("invalid actor correction event %s", stringField(data, "id"))
		}
		item := actorCorrectionProjection{
			TargetEventID: targetID, TargetObject: stringField(payload, "target_object"), TargetObjectKind: stringField(payload, "target_object_kind"),
			OriginalSHA256: stringField(payload, "target_event_sha256"), OriginalActor: stringField(payload, "original_actor"), CorrectedActor: stringField(payload, "corrected_actor"),
			CorrectionEventID: stringField(data, "id"), CorrectedBy: stringField(data, "actor"), CorrectedAt: stringField(data, "at"), GateID: stringField(payload, "gate_id"), CountsAsReview: false,
		}
		if prior, exists := projection[targetID]; exists && (prior.OriginalSHA256 != item.OriginalSHA256 || prior.CorrectedActor != item.CorrectedActor) {
			return fmt.Errorf("conflicting actor correction projection for %s", targetID)
		}
		projection[targetID] = item
		return nil
	})
	return projection, err
}
